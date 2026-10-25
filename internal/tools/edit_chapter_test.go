package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

func enterEditWritingPhase(t *testing.T, s *store.Store) {
	t.Helper()
	if err := s.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}
}

func queueCompletedChapterForEdit(t *testing.T, s *store.Store, chapter int, wordCount int) {
	t.Helper()
	if err := s.Progress.MarkChapterComplete(chapter, wordCount, "mystery", "quest"); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := s.Progress.SetPendingRewrites([]int{chapter}, "Thử hoàn thiện"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := s.Progress.SetFlow(domain.FlowPolishing); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}
}

// TestEditChapterAppliesEdit đường thường: drafts đã có nội dung, thay thế khớp duy nhất thành công.
func TestEditChapterAppliesEdit(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	enterEditWritingPhase(t, s)
	if err := s.Drafts.SaveDraft(2, "Anh siết chặt nắm đấm, các khớp ngón trắng bệch."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	queueCompletedChapterForEdit(t, s, 2, len([]rune("Anh siết chặt nắm đấm, các khớp ngón trắng bệch.")))

	tool := NewEditChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"old_string": "các khớp ngón trắng bệch",
		"new_string": "các khớp ngón lấm lét xanh",
	})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, err := s.Drafts.LoadDraft(2)
	if err != nil {
		t.Fatalf("LoadDraft: %v", err)
	}
	if !strings.Contains(got, "các khớp ngón lấm lét xanh") {
		t.Fatalf("bản nháp phải chứa văn bản mới, thực tế: %q", got)
	}
	if strings.Contains(got, "các khớp ngón trắng bệch") {
		t.Fatalf("văn bản cũ phải được thay thế, thực tế: %q", got)
	}
}

func TestEditChapterRejectsIncompleteChapter(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	enterEditWritingPhase(t, s)
	original := "Bản nháp đầu của chương mới phải được ghi đè toàn chương."
	if err := s.Drafts.SaveDraft(2, original); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"old_string": "phải",
		"new_string": "nên",
	})
	_, err := NewEditChapterTool(s).Execute(context.Background(), args)
	if err == nil || !errors.Is(err, errs.ErrToolPrecondition) {
		t.Fatalf("chương chưa hoàn thành phải bị từ chối rõ ràng, thực tế: %v", err)
	}
	if !strings.Contains(err.Error(), `draft_chapter(mode="write"`) {
		t.Fatalf("lỗi phải chỉ đường ghi đè toàn chương, thực tế: %v", err)
	}
	got, loadErr := s.Drafts.LoadDraft(2)
	if loadErr != nil {
		t.Fatalf("LoadDraft: %v", loadErr)
	}
	if got != original {
		t.Fatalf("sau khi từ chối không được sửa bản nháp, thực tế: %q", got)
	}
}

// TestEditChapterSeedsFromFinalChapter drafts không tồn tại nhưng chapters có → tự động lấy chapters làm điểm xuất phát.
func TestEditChapterSeedsFromFinalChapter(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	enterEditWritingPhase(t, s)

	// Mô phỏng chương 3 đã được nộp và nằm trong hàng chờ hoàn thiện
	original := "Gió lùa qua khe cửa sổ, mang theo mùi đất ẩm."
	if err := s.Drafts.SaveFinalChapter(3, original); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}
	if err := s.Progress.MarkChapterComplete(3, len([]rune(original)), "mystery", "quest"); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := s.Progress.SetPendingRewrites([]int{3}, "Thử hoàn thiện"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := s.Progress.SetFlow(domain.FlowPolishing); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}

	tool := NewEditChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    3,
		"old_string": "mùi đất ẩm",
		"new_string": "mùi đất lẫn mùi sắt",
	})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// drafts phải được lấy làm điểm xuất phát và chứa văn bản mới
	draft, err := s.Drafts.LoadDraft(3)
	if err != nil {
		t.Fatalf("LoadDraft: %v", err)
	}
	if !strings.Contains(draft, "mùi đất lẫn mùi sắt") {
		t.Fatalf("bản nháp phải được lấy điểm xuất phát và đã sửa, thực tế: %q", draft)
	}

	// chapters giữ nguyên (edit_chapter không đụng tới bản chính)
	final, err := s.Drafts.LoadChapterText(3)
	if err != nil {
		t.Fatalf("LoadChapterText: %v", err)
	}
	if final != original {
		t.Fatalf("bản chính phải được giữ nguyên, thực tế: %q", final)
	}
}

// TestEditChapterRejectsCompletedWithoutQueue đã hoàn thành mà không nằm trong hàng chờ viết lại → từ chối.
func TestEditChapterRejectsCompletedWithoutQueue(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	enterEditWritingPhase(t, s)
	original := "Nội dung truyện gốc của chương hai."
	if err := s.Drafts.SaveDraft(2, original); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := s.Drafts.SaveFinalChapter(2, original); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}
	if err := s.Progress.MarkChapterComplete(2, len([]rune(original)), "mystery", "quest"); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}

	tool := NewEditChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"old_string": "nội dung truyện gốc",
		"new_string": "nội dung bị sửa đổi",
	})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("cần từ chối chương đã hoàn thành nhưng không nằm trong PendingRewrites")
	}
	if !errors.Is(err, errs.ErrToolPrecondition) {
		t.Fatalf("cần ErrToolPrecondition, thực tế: %v", err)
	}
}

// TestEditChapterRejectsAmbiguousMatch khớp nhiều chỗ mà không bật replace_all → báo lỗi.
func TestEditChapterRejectsAmbiguousMatch(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	enterEditWritingPhase(t, s)
	if err := s.Drafts.SaveDraft(2, "Anh cười. Cô ấy cũng cười."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	queueCompletedChapterForEdit(t, s, 2, len([]rune("Anh cười. Cô ấy cũng cười.")))

	tool := NewEditChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"old_string": "cười",
		"new_string": "im lặng",
	})
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Fatal("cần từ chối trường hợp khớp mơ hồ")
	}
}

// TestEditChapterReplaceAll khi replace_all=true thì mọi chỗ khớp đều bị thay.
func TestEditChapterReplaceAll(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	enterEditWritingPhase(t, s)
	if err := s.Drafts.SaveDraft(2, "Anh cười. Cô ấy cũng cười."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	queueCompletedChapterForEdit(t, s, 2, len([]rune("Anh cười. Cô ấy cũng cười.")))

	tool := NewEditChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":     2,
		"old_string":  "cười",
		"new_string":  "im lặng",
		"replace_all": true,
	})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, _ := s.Drafts.LoadDraft(2)
	if strings.Contains(got, "cười") {
		t.Fatalf("mọi lần xuất hiện phải được thay thế, thực tế: %q", got)
	}
	if strings.Count(got, "im lặng") != 2 {
		t.Fatalf("cần 2 lần thay thế, thực tế: %q", got)
	}
}

// TestEditChapterRejectsEmptyOldString old_string rỗng → tham số không hợp lệ.
func TestEditChapterRejectsEmptyOldString(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	enterEditWritingPhase(t, s)

	tool := NewEditChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"old_string": "",
		"new_string": "xxx",
	})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("cần từ chối old_string rỗng")
	}
	if !errors.Is(err, errs.ErrToolArgs) {
		t.Fatalf("cần ErrToolArgs, thực tế: %v", err)
	}
}

// TestEditChapterRejectsNoDraftNoFinal cả drafts lẫn chapters đều không tồn tại → báo lỗi nhắc gọi draft_chapter trước.
func TestEditChapterRejectsNoDraftNoFinal(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	enterEditWritingPhase(t, s)
	queueCompletedChapterForEdit(t, s, 5, 0)

	tool := NewEditChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    5,
		"old_string": "bất kỳ",
		"new_string": "thay thế",
	})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("cần từ chối khi không có draft lẫn chapter")
	}
	if !errors.Is(err, errs.ErrToolPrecondition) {
		t.Fatalf("cần ErrToolPrecondition, thực tế: %v", err)
	}
}

// TestEditChapterWorksWithCommitValidation cả chuỗi: edit_chapter → commit_chapter rút thành công hàng chờ.
// Kiểm tra công cụ mới phối hợp tốt với kiểm tra cứng drafts≠chapters của commit_chapter.
func TestEditChapterWorksWithCommitValidation(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	enterEditWritingPhase(t, s)

	original := "Gió lùa qua khe cửa sổ, mang theo mùi đất ẩm."
	if err := s.Drafts.SaveDraft(2, original); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := s.Drafts.SaveFinalChapter(2, original); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}
	if err := s.Progress.MarkChapterComplete(2, len([]rune(original)), "mystery", "quest"); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := s.Progress.SetPendingRewrites([]int{2}, "Hoàn thiện"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := s.Progress.SetFlow(domain.FlowPolishing); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}

	editTool := NewEditChapterTool(s)
	editArgs, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"old_string": "mùi đất ẩm",
		"new_string": "mùi đất lẫn mùi sắt",
	})
	if _, err := editTool.Execute(context.Background(), editArgs); err != nil {
		t.Fatalf("edit_chapter: %v", err)
	}

	commitTool := newTestCommitChapterTool(s)
	commitArgs, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"title":      "Chương hai",
		"summary":    "Tóm tắt sau khi hoàn thiện",
		"characters": []string{"Nhân vật chính"},
		"key_events": []string{"Hoàn tất hoàn thiện"},
	})
	if _, err := commitTool.Execute(context.Background(), commitArgs); err != nil {
		t.Fatalf("commit_chapter after edit: %v", err)
	}

	progress, err := s.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if len(progress.PendingRewrites) != 0 {
		t.Fatalf("cần hàng chờ đã được rút, thực tế: %v", progress.PendingRewrites)
	}
}

func TestEditChapterRejectsPlanningPhaseBeforeMutation(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	original := "Bản nháp thời gian quy hoạch không được sửa."
	if err := s.Drafts.SaveDraft(1, original); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	args, err := json.Marshal(map[string]any{
		"chapter": 1, "old_string": "không được", "new_string": "đã",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if _, err := NewEditChapterTool(s).Execute(context.Background(), args); err == nil {
		t.Fatal("chỉnh sửa ở thời gian quy hoạch phải bị từ chối")
	}
	got, err := s.Drafts.LoadDraft(1)
	if err != nil {
		t.Fatalf("LoadDraft: %v", err)
	}
	if got != original {
		t.Fatalf("chỉnh sửa ở thời gian quy hoạch đã làm thay đổi bản nháp: %q", got)
	}
}
