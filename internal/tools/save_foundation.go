package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// SaveFoundationTool lưu cơ sở thiết lập (premise/outline/characters), Architect dùng.
type SaveFoundationTool struct {
	store *store.Store
}

func NewSaveFoundationTool(store *store.Store) *SaveFoundationTool {
	return &SaveFoundationTool{store: store}
}

func (t *SaveFoundationTool) Name() string      { return "save_foundation" }
func (t *SaveFoundationTool) texts() *toolTexts { return toolTextsFor(t.store.Language()) }
func (t *SaveFoundationTool) Description() string {
	return t.texts().saveFoundationDesc
}
func (t *SaveFoundationTool) Label() string { return t.texts().saveFoundationLabel }

// Viết tool (cập nhật Across Domain Outline/Progress/Characters), cấm song song.
func (t *SaveFoundationTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *SaveFoundationTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *SaveFoundationTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("type", schema.Enum(t.texts().saveFoundationArgType, "premise", "outline", "layered_outline", "characters", "world_rules", "expand_arc", "append_volume", "update_compass", "complete_book")).Required(),
		schema.Property("content", map[string]any{
			"description": t.texts().saveFoundationArgContent,
		}).Required(),
		schema.Property("scale", schema.Enum(t.texts().saveFoundationArgScale, "short", "mid", "long")),
		schema.Property("volume", schema.Int(t.texts().saveFoundationArgVolume)),
		schema.Property("arc", schema.Int(t.texts().saveFoundationArgArc)),
		schema.Property("reason", schema.String(t.texts().saveFoundationArgReason)),
	)
}

func (t *SaveFoundationTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
		Scale   string          `json:"scale"`
		Volume  int             `json:"volume"`
		Arc     int             `json:"arc"`
		Reason  string          `json:"reason"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	content, err := normalizeFoundationContent(a.Content)
	if err != nil {
		return nil, err
	}
	if a.Scale != "" {
		switch domain.PlanningTier(a.Scale) {
		case domain.PlanningTierShort, domain.PlanningTierMid, domain.PlanningTierLong:
		default:
			return nil, fmt.Errorf("invalid scale %q, expected short/mid/long: %w", a.Scale, errs.ErrToolArgs)
		}
	}

	result := map[string]any{"saved": true, "type": a.Type, "scale": a.Scale}

	// Toàn bộ đề cương chỉ thuộc về giai đoạn kế hoạch. Giai đoạn viết phải dùng các thao tác tăng dần được bảo vệ, sau khi hoàn tất phải mở lại tác phẩm trước;
	// nếu không sẽ bỏ qua bảo vệ các chương đã hoàn thành, phá vỡ tính nhất quán giữa Progress và sự thật chương.
	progress, err := t.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("check foundation phase: %w: %w", errs.ErrStoreRead, err)
	}
	if (a.Type == "outline" || a.Type == "layered_outline") && progress != nil {
		switch progress.Phase {
		case domain.PhaseWriting:
			return nil, fmt.Errorf(
				"Giai đoạn viết không được dùng %s để ghi đè toàn bộ đề cương. Hãy dùng revise_outline để sửa chương chưa có, expand_arc để mở rộng cung xương, hoặc append_volume để thêm tập mới: %w",
				a.Type, errs.ErrToolPrecondition)
		case domain.PhaseComplete:
			return nil, fmt.Errorf(
				"Toàn bộ truyện đã hoàn tất, không được dùng %s để ghi đè toàn bộ đề cương. Hãy mở lại tác phẩm trước, rồi dùng các thao tác sửa đề cương hoặc viết tiếp được bảo vệ: %w",
				a.Type, errs.ErrToolPrecondition)
		}
	}
	if a.Scale != "" {
		if err := t.store.RunMeta.SetPlanningTier(domain.PlanningTier(a.Scale)); err != nil {
			return nil, fmt.Errorf("save planning tier: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	// Ba lựa chọn cuối tập (tiếp tục tập / thu thanh / hoàn tất) là phán đoán ngữ nghĩa nặng nề nhất của toàn bộ truyện, lý do phải trở thành sự thật kiểm toán
	// (decisions.jsonl, cùng dòng chảy với plan_start/intervention), nếu không thu thanh sớm/
	// tiếp tục tập sai chỉ có thể quay lại nhật ký phiên làm việc để khắc phục. Chụp nhanh sự thật lấy thời điểm phán đoán (trước khi thay đổi được lưu vào đĩa) là tiến độ.
	volumeEnd := a.Type == "append_volume" || a.Type == "complete_book"
	if volumeEnd && strings.TrimSpace(a.Reason) == "" {
		return nil, fmt.Errorf("%s phải có tham số reason: đối chiếu danh sách kiểm tra hoàn thành, dùng một câu giải thích lý do tục tập, tuyên bố thu thanh hoặc hoàn thành: %w", a.Type, errs.ErrToolArgs)
	}
	var volumeEndFacts json.RawMessage
	if volumeEnd {
		p, err := t.store.Progress.Load()
		if err != nil {
			return nil, fmt.Errorf("load progress for volume-end facts: %w: %w", errs.ErrStoreRead, err)
		}
		if p != nil {
			facts := map[string]any{"completed_chapters": len(p.CompletedChapters)}
			if p.Layered {
				outline, outlineErr := t.store.Outline.LoadOutline()
				if outlineErr != nil {
					return nil, fmt.Errorf("load outlined chapters for volume-end facts: %w: %w", errs.ErrStoreRead, outlineErr)
				}
				facts["dynamic_planning"] = true
				facts["outlined_chapters"] = len(outline)
			} else {
				facts["total_chapters"] = p.TotalChapters
			}
			volumeEndFacts, err = json.Marshal(facts)
			if err != nil {
				return nil, fmt.Errorf("marshal volume-end facts: %w", err)
			}
		}
	}

	decode := func(typeName string, out any) error {
		return decodeFoundationJSON(typeName, content, out)
	}

	switch a.Type {
	case "premise":
		if err := t.store.Outline.SavePremise(content); err != nil {
			return nil, fmt.Errorf("save premise: %w: %w", errs.ErrStoreWrite, err)
		}
		if err := t.store.Progress.AdvancePhase(domain.PhasePremise); err != nil {
			return nil, fmt.Errorf("update premise phase: %w: %w", errs.ErrStoreWrite, err)
		}

	case "outline":
		var entries []domain.OutlineEntry
		if err := decode("outline", &entries); err != nil {
			return nil, err
		}
		if defect := domain.StalledOutline(entries); defect != "" {
			return nil, fmt.Errorf("%s: %w", defect, errs.ErrToolArgs)
		}
		if err := t.store.Outline.SaveOutline(entries); err != nil {
			return nil, fmt.Errorf("save outline: %w: %w", errs.ErrStoreWrite, err)
		}
		if err := t.store.Progress.AdvancePhase(domain.PhaseOutline); err != nil {
			return nil, fmt.Errorf("update outline phase: %w: %w", errs.ErrStoreWrite, err)
		}
		if err := t.store.Progress.SetTotalChapters(len(entries)); err != nil {
			return nil, fmt.Errorf("set total chapters: %w: %w", errs.ErrStoreWrite, err)
		}
		if domain.PlanningTier(a.Scale) != domain.PlanningTierLong {
			if err := t.store.Progress.SetLayered(false); err != nil {
				return nil, fmt.Errorf("disable layered mode: %w: %w", errs.ErrStoreWrite, err)
			}
			if err := t.store.Progress.UpdateVolumeArc(0, 0); err != nil {
				return nil, fmt.Errorf("reset volume/arc: %w: %w", errs.ErrStoreWrite, err)
			}
			if err := t.store.Outline.ClearLayeredOutline(); err != nil {
				return nil, fmt.Errorf("clear layered outline: %w: %w", errs.ErrStoreWrite, err)
			}
		}
		result["chapters"] = len(entries)

	case "layered_outline":
		var volumes []domain.VolumeOutline
		if err := decode("layered_outline", &volumes); err != nil {
			return nil, err
		}
		for vi := range volumes {
			for ai := range volumes[vi].Arcs {
				arc := &volumes[vi].Arcs[ai]
				if defect := domain.OversizedArc(
					fmt.Sprintf("Tập %d cung %d", volumes[vi].Index, arc.Index), arcPlannedSize(arc)); defect != "" {
					return nil, fmt.Errorf("%s: %w", defect, errs.ErrToolArgs)
				}
			}
		}
		if defect := domain.StalledOutline(domain.FlattenOutline(volumes)); defect != "" {
			return nil, fmt.Errorf("%s: %w", defect, errs.ErrToolArgs)
		}
		if err := t.store.Outline.SaveLayeredOutline(volumes); err != nil {
			return nil, fmt.Errorf("save layered_outline: %w: %w", errs.ErrStoreWrite, err)
		}
		total := domain.EstimatedChapterCapacity(volumes)
		if err := t.store.Progress.AdvancePhase(domain.PhaseOutline); err != nil {
			return nil, fmt.Errorf("update outline phase: %w: %w", errs.ErrStoreWrite, err)
		}
		if err := t.store.Progress.SetTotalChapters(total); err != nil {
			return nil, fmt.Errorf("set total chapters: %w: %w", errs.ErrStoreWrite, err)
		}
		if err := t.store.Progress.SetLayered(true); err != nil {
			return nil, fmt.Errorf("enable layered mode: %w: %w", errs.ErrStoreWrite, err)
		}
		if len(volumes) > 0 && len(volumes[0].Arcs) > 0 {
			if err := t.store.Progress.UpdateVolumeArc(volumes[0].Index, volumes[0].Arcs[0].Index); err != nil {
				return nil, fmt.Errorf("set initial volume/arc: %w: %w", errs.ErrStoreWrite, err)
			}
		}
		result["volumes"] = len(volumes)
		result["dynamic_planning"] = true
		result["outlined_chapters"] = len(domain.FlattenOutline(volumes))

	case "characters":
		var chars []domain.Character
		if err := decode("characters", &chars); err != nil {
			return nil, err
		}
		if err := t.store.Characters.Save(chars); err != nil {
			return nil, fmt.Errorf("save characters: %w: %w", errs.ErrStoreWrite, err)
		}
		result["count"] = len(chars)

	case "world_rules":
		var rules []domain.WorldRule
		if err := decode("world_rules", &rules); err != nil {
			return nil, err
		}
		if err := t.store.World.SaveWorldRules(rules); err != nil {
			return nil, fmt.Errorf("save world_rules: %w: %w", errs.ErrStoreWrite, err)
		}
		result["count"] = len(rules)

	case "expand_arc":
		if a.Volume <= 0 || a.Arc <= 0 {
			return nil, fmt.Errorf(
				"expand_arc cần volume và arc (số tập/số cung đều tính từ 1, nhận volume=%d arc=%d);"+
					"hãy gọi novel_context trước, dùng giá trị index của cung mục tiêu trong layered_outline để điền vào: %w",
				a.Volume, a.Arc, errs.ErrToolArgs)
		}
		var expansion domain.ArcExpansion
		if err := decode("expand_arc", &expansion); err != nil {
			return nil, err
		}
		if defect := domain.OversizedArc(
			fmt.Sprintf("Tập %d cung %d", a.Volume, a.Arc), len(expansion.Chapters)); defect != "" {
			return nil, fmt.Errorf("%s: %w", defect, errs.ErrToolArgs)
		}
		if err := t.store.ExpandArc(a.Volume, a.Arc, expansion); err != nil {
			return nil, fmt.Errorf("expand arc: %w: %w", errs.ErrStoreWrite, err)
		}
		result["volume"] = a.Volume
		result["arc"] = a.Arc
		result["title"] = expansion.Title
		result["goal"] = expansion.Goal
		result["chapters"] = len(expansion.Chapters)
		if err := t.consumeWriterFeedback(); err != nil {
			return nil, err
		}

	case "append_volume":
		p, err := t.store.Progress.Load()
		if err != nil {
			return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
		}
		if p != nil && p.Phase == domain.PhaseComplete {
			return nil, fmt.Errorf("Toàn bộ truyện đã hoàn tất (phase=complete), không được thêm tập mới: %w", errs.ErrToolPrecondition)
		}
		var vol domain.VolumeOutline
		if err := decode("append_volume", &vol); err != nil {
			return nil, err
		}
		for i := range vol.Arcs {
			arc := &vol.Arcs[i]
			if defect := domain.OversizedArc(
				fmt.Sprintf("Tập mới cung %d", arc.Index), arcPlannedSize(arc)); defect != "" {
				return nil, fmt.Errorf("%s: %w", defect, errs.ErrToolArgs)
			}
		}
		prior, err := t.store.Outline.LoadLayeredOutline()
		if err != nil {
			return nil, fmt.Errorf("load layered outline: %w: %w", errs.ErrStoreRead, err)
		}
		if err := t.store.AppendVolume(vol); err != nil {
			return nil, fmt.Errorf("append volume: %w: %w", errs.ErrStoreWrite, err)
		}
		result["volume"] = vol.Index
		if vol.Final {
			result["final_volume"] = true
		} else if domain.FinaleVolume(prior) > 0 {
			// Thực trạng phản ánh: trạng thái thu thanh đã khai báo trước bị hủy bỏ do thêm tập mới thông thường (tập mới trở thành tập cuối)
			result["finale_released"] = true
		}
		result["arcs"] = len(vol.Arcs)
		chCount := 0
		for _, arc := range vol.Arcs {
			chCount += len(arc.Chapters)
		}
		if chCount > 0 {
			result["chapters"] = chCount
		}
		if err := t.consumeWriterFeedback(); err != nil {
			return nil, err
		}

	case "complete_book":
		// Điểm nhập duy nhất để hoàn thành toàn bộ truyện: trực tiếp đẩy Phase=Complete.
		// Chỉ cho phép trong giai đoạn Viết, ngăn giai đoạn Kế hoạch nhầm lẫn bỏ qua toàn bộ quá trình viết.
		// Từ chối gọi khi còn hàng làm lại—đảm bảo PendingRewrites chạy xong trước khi kết thúc.
		progress, perr := t.store.Progress.Load()
		if perr != nil {
			return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, perr)
		}
		if progress == nil {
			return nil, fmt.Errorf("progress chưa được khởi tạo: %w", errs.ErrToolPrecondition)
		}
		if progress.Phase != domain.PhaseWriting {
			return nil, fmt.Errorf("complete_book chỉ được gọi trong giai đoạn viết (phase hiện tại=%s): %w", progress.Phase, errs.ErrToolPrecondition)
		}
		if len(progress.PendingRewrites) > 0 {
			return nil, fmt.Errorf("còn %d chapter đang trong hàng làm lại, xong hàng rồi mới gọi complete_book: %w", len(progress.PendingRewrites), errs.ErrToolPrecondition)
		}
		// Các kiểm tra tiền đề hoàn tất có thể liệt kê được phải ở tầng mã (ba phân pháp), không chỉ dựa trên prompt
		// "danh sách kiểm tra hoàn tất"—tai nạn thực tế: giai đoạn kế hoạch vừa lưu phase chuyển sang viết, mô hình yếu nhầm tay
		// gọi complete_book, 0/68 chương bị đánh dấu hoàn tất trực tiếp.
		if len(progress.CompletedChapters) == 0 {
			return nil, fmt.Errorf("chưa có chương nào viết xong; khi kế hoạch xong hệ thống tự động đẩy tiến độ viết, không cần gọi complete_book: %w", errs.ErrToolPrecondition)
		}
		next := progress.NextChapter()
		if progress.Layered {
			outline, outlineErr := t.store.Outline.LoadOutline()
			if outlineErr != nil {
				return nil, fmt.Errorf("load outlined chapters: %w: %w", errs.ErrStoreRead, outlineErr)
			}
			if next <= len(outline) {
				return nil, fmt.Errorf("đề cương chi tiết hiện vẫn còn chương chưa viết (chương sau %d/đã chi tiết %d), không được hoàn tất; muốn sớm thu liễm hãy dùng append_volume và để \"final\": true ở cấp cao nhất của JSON tập để tuyên bố tập thu thanh: %w", next, len(outline), errs.ErrToolPrecondition)
			}
			// Đề cương phẳng chỉ chứa các cung đã mở rộng, cung xương đối với việc so sánh ở trên hoàn toàn vô hình; không ngăn riêng một lần,
			// toàn bộ tập chưa mở rộng cũng có thể tuyên bố hoàn tất.
			volumes, volErr := t.store.Outline.LoadLayeredOutline()
			if volErr != nil {
				return nil, fmt.Errorf("load layered outline: %w: %w", errs.ErrStoreRead, volErr)
			}
			if skeletons := domain.SkeletonArcs(volumes); len(skeletons) > 0 {
				return nil, fmt.Errorf("còn %d cung xương chưa mở rộng (ví dụ: %s), không được hoàn tất;"+
					"hãy expand_arc để mở rộng và viết xong, hoặc dùng append_volume kèm \"final\": true để tuyên bố tập thu thanh: %w",
					len(skeletons), skeletons[0], errs.ErrToolPrecondition)
			}
		} else if progress.TotalChapters > 0 && next <= progress.TotalChapters {
			return nil, fmt.Errorf("trong đề cương vẫn còn chương chưa viết (chương sau %d/tổng %d), không được hoàn tất; muốn sớm thu liễm hãy dùng append_volume và để \"final\": true ở cấp cao nhất của JSON tập để tuyên bố tập thu thanh: %w", next, progress.TotalChapters, errs.ErrToolPrecondition)
		}
		// Dòng chốt chính còn hoạt động không được hoàn tất—OpenThreads rõ ràng là "phải thu liễm trước khi kết cục". Điều này không
		// là phán đoán ngữ nghĩa lại: nếu thật sự đã thu liễm hết, hãy update_compass để xóa open_threads trước khi hoàn tất, biến
		// "miễn trừ trong bàn luận" thành hành động lưu trữ có thể kiểm toán (thử nghiệm nhập hoàn thành sách liên tiếp thấy, kiến trúc sư trích dẫn kinh điển
		// để bỏ qua điều 3 của danh sách kiểm tra hoàn tất trực tiếp hoàn tất, yêu cầu viết tiếp của người dùng bị quy tắc hoàn tất khóa lại).
		compass, err := t.store.Outline.LoadCompass()
		if err != nil {
			return nil, fmt.Errorf("load compass: %w: %w", errs.ErrStoreRead, err)
		}
		if compass != nil && len(compass.OpenThreads) > 0 {
			return nil, fmt.Errorf("compass còn %d dòng chốt chính còn đang hoạt động (ví dụ: %s), không được hoàn tất. Xác nhận đã thu liễm hết rồi hãy update_compass để xóa open_threads trước khi gọi complete_book; vẫn cần mở rộng hãy dùng append_volume (có thể kèm \"final\": true để tuyên bố tập thu thanh): %w",
				len(compass.OpenThreads), compass.OpenThreads[0], errs.ErrToolPrecondition)
		}
		if err := t.store.Progress.MarkComplete(); err != nil {
			return nil, fmt.Errorf("mark complete: %w: %w", errs.ErrStoreWrite, err)
		}
		result["book_complete"] = true
		result["phase"] = string(domain.PhaseComplete)

	case "update_compass":
		var compass domain.StoryCompass
		if err := decode("compass", &compass); err != nil {
			return nil, err
		}
		// Tầng công cụ bắt buộc ghi đè LastUpdated bằng số chương đã hoàn thành hiện tại, không tin LLM tự điền.
		// LLM thường quên điền hoặc để 0, khiến diag.CompassDrift báo sai, Router định tuyến sai.
		p, err := t.store.Progress.Load()
		if err != nil {
			return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
		}
		if p != nil {
			compass.LastUpdated = p.LatestCompleted()
		}
		if err := t.store.Outline.SaveCompass(compass); err != nil {
			return nil, fmt.Errorf("save compass: %w: %w", errs.ErrStoreWrite, err)
		}
		result["ending_direction"] = compass.EndingDirection
		result["last_updated"] = compass.LastUpdated
		if err := t.consumeWriterFeedback(); err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("unknown type %q, expected premise/outline/layered_outline/characters/world_rules/expand_arc/append_volume/update_compass/complete_book: %w", a.Type, errs.ErrToolArgs)
	}

	// checkpoint
	scope := domain.GlobalScope()
	if a.Type == "expand_arc" {
		scope = domain.ArcScope(a.Volume, a.Arc)
	} else if a.Type == "append_volume" {
		scope = domain.GlobalScope()
	}
	if _, err := t.store.Checkpoints.AppendArtifact(scope, a.Type, foundationArtifact(a.Type)); err != nil {
		return nil, fmt.Errorf("checkpoint foundation %s: %w: %w", a.Type, errs.ErrStoreWrite, err)
	}

	if volumeEnd {
		t.recordVolumeEndDecision(a.Type, a.Reason, volumeEndFacts, result)
	}

	// Trả về các mục chưa hoàn thành còn lại. Sau khi các tệp cơ sở ban đầu đầy đủ, vẫn còn foundation_audit; chỉ
	// khi audit_foundation cho phiên bản thực tế đã lưu cho ready=true, mới được vào giai đoạn viết.
	remaining, err := t.store.FoundationMissing()
	if err != nil {
		return nil, fmt.Errorf("load foundation state: %w: %w", errs.ErrStoreRead, err)
	}
	ready := len(remaining) == 0
	result["remaining"] = remaining
	result["foundation_ready"] = ready
	return json.Marshal(result)
}

func foundationArtifact(t string) string {
	switch t {
	case "premise":
		return "premise.md"
	case "outline":
		return "outline.json"
	case "layered_outline", "expand_arc", "append_volume":
		return "layered_outline.json"
	case "complete_book":
		return "meta/progress.json"
	case "characters":
		return "characters.json"
	case "world_rules":
		return "world_rules.json"
	case "update_compass":
		return "meta/compass.json"
	default:
		return ""
	}
}

// decodeFoundationJSON phân tích trường content của save_foundation, khi thất bại kèm theo vị trí hàng cột
// và gợi ý sửa lỗi phổ biến nhất, để LLM lần thử tiếp có thể định vị trực tiếp thay vì đoán mò.
func decodeFoundationJSON(typeName, content string, out any) error {
	err := json.Unmarshal([]byte(content), out)
	if err == nil {
		return nil
	}
	hint := `Nguyên nhân phổ biến: giá trị chuỗi chứa dấu ngoặc kép chưa được thoát thành \", dấu xuống dòng chưa được thoát thành \n, hoặc thiếu dấu phẩy giữa các trường đối tượng. Hãy tạo lại toàn bộ đoạn một lần.`
	if se, ok := err.(*json.SyntaxError); ok {
		line, col := offsetToLineCol(content, int(se.Offset))
		return fmt.Errorf("parse %s JSON (line %d col %d): %w — %s", typeName, line, col, err, hint)
	}
	return fmt.Errorf("parse %s JSON: %w — %s", typeName, err, hint)
}

func offsetToLineCol(s string, offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(s) {
		offset = len(s)
	}
	line, col := 1, 1
	for i := 0; i < offset; i++ {
		if s[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func normalizeFoundationContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("content is required: %w", errs.ErrToolArgs)
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}

	if !json.Valid(raw) {
		return "", fmt.Errorf("invalid content: expected Markdown string or valid JSON value: %w", errs.ErrToolArgs)
	}
	return string(raw), nil
}

// recordVolumeEndDecision đưa lý do phán đoán của ba lựa chọn cuối tập (tiếp tục tập / thu thanh / hoàn tất) vào nhật ký phán quyết kiểm toán.
// best-effort: thay đổi cấu trúc đã lưu vào đĩa, thất bại kiểm toán chỉ cảnh báo không hoàn tác—báo lỗi sẽ khiến mô hình thử lại
// các thao tác đã hoàn thành (thêm tập trùng lặp).
func (t *SaveFoundationTool) recordVolumeEndDecision(action, reason string, facts json.RawMessage, result map[string]any) {
	decision := map[string]any{"action": action}
	if v, ok := result["volume"]; ok {
		decision["volume"] = v
	}
	if _, ok := result["final_volume"]; ok {
		decision["final"] = true
	}
	raw, err := json.Marshal(decision)
	if err != nil {
		slog.Error("Serialization của phán quyết cuối tập thất bại", "module", "tools", "action", action, "err", err)
		return
	}
	if _, err := t.store.Decisions.Append(store.DecisionRecord{
		Kind:     "volume_end",
		Decider:  "architect",
		Facts:    facts,
		Decision: raw,
		Reason:   reason,
	}); err != nil {
		slog.Error("Lưu kiểm toán phán quyết cuối tập vào đĩa thất bại", "module", "tools", "action", action, "err", err)
	}
}

// consumeWriterFeedback xóa phản hồi kế hoạch đã xử lý sau khi thao tác cấu trúc thành công.
func (t *SaveFoundationTool) consumeWriterFeedback() error {
	if err := t.store.Outline.ClearOutlineFeedback(); err != nil {
		return fmt.Errorf("clear outline feedback: %w: %w", errs.ErrStoreWrite, err)
	}
	return nil
}

// arcPlannedSize lấy quy mô thực tế của cung: đã mở rộng dùng số chương chi tiết, vẫn là cung xương thì dùng số chương ước tính.
func arcPlannedSize(arc *domain.ArcOutline) int {
	if n := len(arc.Chapters); n > 0 {
		return n
	}
	return arc.EstimatedChapters
}
