package imp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// ChapterCommitter là giao diện tối thiểu cần cho việc phát hành chương, do tools.CommitChapterTool thỏa mãn.
// Tái sử dụng saga PendingCommit, checkpoint và kiểm tra idempotent của chương đã hoàn thành, không nhân bản thêm một bộ logic phát hành (RFC §12.3).
type ChapterCommitter interface {
	Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
}

// publishFoundation phát hành Foundation theo thứ tự phụ thuộc chính thức, khớp với thứ tự ghi xuống đĩa của Architect cho tiểu thuyết dài (RFC §12.2).
// Phát hành lại cùng nội dung là idempotent (Store ghi đè cùng nội dung + checkpoint khử trùng lặp).
func publishFoundation(st *store.Store, f *Foundation) error {
	// Đối chiếu xung đột trước khi phát hành: sản phẩm chính thức đã tồn tại và khác nội dung thì từ chối ghi đè (§12.2 / bất biến 6).
	// Nội dung giống nhau thì tiếp tục ghi theo nguyên tắc idempotent (Store ghi đè cùng nội dung + checkpoint khử trùng lặp).
	if err := checkFoundationConflicts(st, f); err != nil {
		return err
	}
	if err := st.Book.Save(f.Book); err != nil {
		return fmt.Errorf("book: %w", err)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "book", "meta/book.json"); err != nil {
		return fmt.Errorf("checkpoint book: %w", err)
	}
	if err := st.RunMeta.SetPlanningTier(f.PlanningTier); err != nil {
		return fmt.Errorf("planning tier: %w", err)
	}
	// premise
	if err := st.Outline.SavePremise(f.Premise); err != nil {
		return fmt.Errorf("premise: %w", err)
	}
	if err := st.Progress.UpdatePhase(domain.PhasePremise); err != nil {
		return fmt.Errorf("phase premise: %w", err)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "premise", "premise.md"); err != nil {
		return fmt.Errorf("checkpoint premise: %w", err)
	}
	// characters
	if err := st.Characters.Save(f.Characters); err != nil {
		return fmt.Errorf("characters: %w", err)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "characters", "characters.json"); err != nil {
		return fmt.Errorf("checkpoint characters: %w", err)
	}
	// world rules
	if err := st.World.SaveWorldRules(f.WorldRules); err != nil {
		return fmt.Errorf("world_rules: %w", err)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "world_rules", "world_rules.json"); err != nil {
		return fmt.Errorf("checkpoint world_rules: %w", err)
	}
	// layered outline là nguồn duy nhất, Store dựng lại đồng bộ flat outline.
	if err := st.Outline.SaveLayeredOutline(f.Volumes); err != nil {
		return fmt.Errorf("layered outline: %w", err)
	}
	// Tiến độ ở giai đoạn quy hoạch là căn cứ để engine tính lại định tuyến (dung lượng chương/phân lớp/cung của tập hiện tại);
	// ghi thất bại sẽ để lại trạng thái đã phát hành không nhất quán, phải lộ ra chứ không nuốt đi (RFC §12.2).
	if err := st.Progress.UpdatePhase(domain.PhaseOutline); err != nil {
		return fmt.Errorf("phase outline: %w", err)
	}
	if err := st.Progress.SetTotalChapters(domain.EstimatedChapterCapacity(f.Volumes)); err != nil {
		return fmt.Errorf("total chapters: %w", err)
	}
	if err := st.Progress.SetLayered(true); err != nil {
		return fmt.Errorf("set layered: %w", err)
	}
	if len(f.Volumes) > 0 && len(f.Volumes[0].Arcs) > 0 {
		if err := st.Progress.UpdateVolumeArc(f.Volumes[0].Index, f.Volumes[0].Arcs[0].Index); err != nil {
			return fmt.Errorf("volume arc: %w", err)
		}
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "layered_outline", "layered_outline.json"); err != nil {
		return fmt.Errorf("checkpoint layered outline: %w", err)
	}
	// compass
	if err := st.Outline.SaveCompass(f.Compass); err != nil {
		return fmt.Errorf("compass: %w", err)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "compass", "meta/compass.json"); err != nil {
		return fmt.Errorf("checkpoint compass: %w", err)
	}
	// Toàn bộ ghi chính thức của Foundation trong quá trình nhập đã thành công, có thể chuyển tường minh sang writing.
	// Không được tái sử dụng FoundationMissing của quy trình sáng tác thường: nhập cho phép world_rules rỗng,
	// coi “giá trị rỗng hợp lệ” là thiếu sẽ khiến tiến độ đứng vĩnh ở outline, rồi StartChapter bị cổng giai đoạn từ chối.
	p, err := st.Progress.Load()
	if err != nil {
		return fmt.Errorf("load progress: %w", err)
	}
	if p == nil {
		return fmt.Errorf("load progress: progress chưa khởi tạo")
	}
	if p.Phase != domain.PhaseWriting && p.Phase != domain.PhaseComplete {
		if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
			return fmt.Errorf("phase writing: %w", err)
		}
	}
	return nil
}

// checkFoundationConflicts kiểm tra hợp lệ sự nhất quán giữa Foundation chờ phát hành và các sản phẩm chính thức đã có:
// hiện có rỗng thì coi là lần phát hành đầu tiên; giống nhau thì coi là idempotent; khác nhau thì báo xung đột và không ghi đè (RFC §12.2 / bất biến 6).
// compass và dàn ý phẳng đều suy ra từ dàn ý phân lớp, dàn ý phân lớp nhất quán thì phần suy ra cũng nhất quán, nên không kiểm tra riêng các sản phẩm suy ra.
// Lỗi đọc không được bị nuốt thành "tệp không tồn tại": bộ nạp của store trả về (giá trị 0, nil) khi thiếu, nên bất kỳ giá trị khác nil nào cũng là lỗi thật
// (hỏng dữ liệu/sai quyền/JSON không hợp lệ); coi như giá trị rỗng rồi đi tiếp sẽ ghi đè sản phẩm chính thức không đọc được (RFC §12.2).
func checkFoundationConflicts(st *store.Store, f *Foundation) error {
	wantBook := f.Book.Normalized()
	book, err := st.Book.Load()
	if err != nil {
		return fmt.Errorf("đọc book chính thức: %w", err)
	}
	if book != nil && !jsonEqual(book, wantBook) {
		return fmt.Errorf("book chính thức xung đột với tổng hợp nhập (đã có phiên bản khác), từ chối ghi đè")
	}
	cur, err := st.Outline.LoadPremise()
	if err != nil {
		return fmt.Errorf("đọc premise chính thức: %w", err)
	}
	if cur != "" && cur != f.Premise {
		return fmt.Errorf("premise chính thức xung đột với tổng hợp nhập (đã có phiên bản khác), từ chối ghi đè")
	}
	chars, err := st.Characters.Load()
	if err != nil {
		return fmt.Errorf("đọc characters chính thức: %w", err)
	}
	if len(chars) > 0 && !jsonEqual(chars, f.Characters) {
		return fmt.Errorf("characters chính thức xung đột với tổng hợp nhập (đã có phiên bản khác), từ chối ghi đè")
	}
	rules, err := st.World.LoadWorldRules()
	if err != nil {
		return fmt.Errorf("đọc world_rules chính thức: %w", err)
	}
	if len(rules) > 0 && !jsonEqual(rules, f.WorldRules) {
		return fmt.Errorf("world_rules chính thức xung đột với tổng hợp nhập (đã có phiên bản khác), từ chối ghi đè")
	}
	layered, err := st.Outline.LoadLayeredOutline()
	if err != nil {
		return fmt.Errorf("đọc layered_outline chính thức: %w", err)
	}
	if len(layered) > 0 && !jsonEqual(layered, f.Volumes) {
		return fmt.Errorf("layered_outline chính thức xung đột với tổng hợp nhập (đã có phiên bản khác), từ chối ghi đè")
	}
	return nil
}

// jsonEqual so sánh hai giá trị có tương đương hay không theo byte JSON đã chuẩn hoá.
func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(ab, bb)
}

// publishChapter tái sử dụng commit_chapter để phát hành một chương; chương đã hoàn thành bị bỏ qua nhờ kiểm tra idempotent của nó (RFC §12.3).
func publishChapter(ctx context.Context, st *store.Store, commit ChapterCommitter, chapter int, content string, f ImportedChapterFacts) error {
	completed, err := st.Progress.IsChapterCompleted(chapter)
	if err != nil {
		return fmt.Errorf("load progress ch%d: %w", chapter, err)
	}
	if completed {
		// Sự cố có thể xảy ra giữa MarkChapterComplete và ClearPendingCommit: pending_commit còn sót
		// và trỏ về chương này. Bỏ qua thẳng sẽ né nhánh dọn dẹp mà công cụ commit chuẩn bị riêng cho khoảng cửa sổ này
		// (bổ sung checkpoint + dọn phần sót), và Execute của chương sau sẽ từ chối với lý do «tồn tại chương phát hành chưa khôi phục»,
		// khiến mỗi lần chạy lại quá trình nhập chết ở cùng một chỗ và phải xoá tay meta/pending_commit.json mới mở khoá được.
		// Khi trúng phần sót thì vẫn đi qua đường idempotent của công cụ để hoàn tất dọn dẹp.
		pending, err := st.Signals.LoadPendingCommit()
		if err != nil {
			return fmt.Errorf("load pending commit ch%d: %w", chapter, err)
		}
		if pending != nil && pending.Chapter == chapter {
			raw, err := json.Marshal(commitArgs(chapter, f))
			if err != nil {
				return fmt.Errorf("marshal commit ch%d: %w", chapter, err)
			}
			if _, err := commit.Execute(ctx, raw); err != nil {
				return fmt.Errorf("commit ch%d: %w", chapter, err)
			}
		}
		return nil
	}
	if err := st.Drafts.SaveDraft(chapter, content); err != nil {
		return fmt.Errorf("save draft ch%d: %w", chapter, err)
	}
	if err := st.Progress.StartChapter(chapter); err != nil {
		return fmt.Errorf("start ch%d: %w", chapter, err)
	}
	raw, err := json.Marshal(commitArgs(chapter, f))
	if err != nil {
		return fmt.Errorf("marshal commit ch%d: %w", chapter, err)
	}
	if _, err := commit.Execute(ctx, raw); err != nil {
		return fmt.Errorf("commit ch%d: %w", chapter, err)
	}
	return nil
}

// commitArgs ánh xạ sự thật theo từng chương thành tham số đầu vào của commit_chapter.
func commitArgs(chapter int, f ImportedChapterFacts) map[string]any {
	keyEvents := f.KeyEvents
	if len(keyEvents) == 0 {
		keyEvents = []string{f.CoreEvent} // core_event đã được kiểm tra là không rỗng
	}
	args := map[string]any{
		"chapter":         chapter,
		"title":           f.Title,
		"summary":         f.Summary,
		"characters":      f.Characters,
		"key_events":      keyEvents,
		"hook_type":       f.HookType,
		"dominant_strand": f.DominantStrand,
	}
	if len(f.TimelineEvents) > 0 {
		args["timeline_events"] = f.TimelineEvents
	}
	if len(f.ForeshadowUpdates) > 0 {
		args["foreshadow_updates"] = f.ForeshadowUpdates
	}
	if len(f.RelationshipChanges) > 0 {
		args["relationship_changes"] = f.RelationshipChanges
	}
	if len(f.StateChanges) > 0 {
		args["state_changes"] = f.StateChanges
	}
	return args
}

// isPublished kiểm tra trạng thái chính thức đã phản ánh toàn bộ quá trình nhập chưa: Foundation đã ghi xuống đĩa và số chương đã hoàn thành đạt kỳ vọng.
// Chỉ đối chiếu những sản phẩm mà quá trình nhập thực sự tạo ra — book, premise, dàn ý phẳng phủ toàn bộ chương, các chương đã hoàn thành — chứ không tái sử dụng
// FoundationMissing(): hàm sau là cổng “có thể viết” của quy trình sáng tác thường, sẽ nhầm world_rules rỗng hợp lệ
// là chưa hoàn thành, khiến đối chiếu phát hành không bao giờ hội tụ (RFC §12.3).
func isPublished(st *store.Store, expected int) (bool, error) {
	if expected == 0 {
		return false, nil
	}
	book, err := st.Book.Load()
	if err != nil {
		return false, fmt.Errorf("đọc book chính thức: %w", err)
	}
	if book == nil {
		return false, nil
	}
	p, err := st.Outline.LoadPremise()
	if err != nil {
		return false, fmt.Errorf("đọc premise chính thức: %w", err)
	}
	if p == "" {
		return false, nil
	}
	o, err := st.Outline.LoadOutline()
	if err != nil {
		return false, fmt.Errorf("đọc outline chính thức: %w", err)
	}
	if len(o) < expected {
		return false, nil
	}
	prog, err := st.Progress.Load()
	if err != nil {
		return false, fmt.Errorf("đọc progress chính thức: %w", err)
	}
	return prog != nil && len(prog.CompletedChapters) >= expected, nil
}
