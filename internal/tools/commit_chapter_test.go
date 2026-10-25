package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
	"github.com/voocel/ainovel-cli/internal/store"
)

func newTestCommitChapterTool(st *store.Store) *CommitChapterTool {
	return NewCommitChapterTool(st, NewStyleStatsIndex(st))
}

func saveTestChapterRecord(t *testing.T, st *store.Store, chapter int, content string) {
	t.Helper()
	if _, err := st.ChapterRecords.Accept(chapter, domain.ChapterOriginGenerated, content, domain.ChapterFacts{
		Title: fmt.Sprintf("Chương %d", chapter), Summary: "Tóm tắt có sẵn", KeyEvents: []string{"Sự kiện có sẵn"},
	}, domain.StyleDelta{}); err != nil {
		t.Fatalf("SaveChapterRecord %d: %v", chapter, err)
	}
}

func TestCommitChapterSchemaDescribesFeedbackAsObject(t *testing.T) {
	tool := newTestCommitChapterTool(store.NewStore(t.TempDir()))
	if !tool.StrictSchema() {
		t.Fatal("commit_chapter must use strict schema")
	}
	if err := llmcontract.ValidateStrictReady(tool.Schema()); err != nil {
		t.Fatalf("commit_chapter schema is not strict-ready: %v", err)
	}
	schema := tool.Schema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing: %#v", schema["properties"])
	}
	feedback, ok := props["feedback"].(map[string]any)
	if !ok {
		t.Fatalf("feedback schema missing: %#v", props["feedback"])
	}
	desc, _ := feedback["description"].(string)
	if !strings.Contains(desc, "JSON object") || !strings.Contains(desc, "JSON đã chuyển thành chuỗi") {
		t.Fatalf("feedback description should warn against stringified JSON, got %q", desc)
	}
	if got := fmt.Sprint(feedback["type"]); got != "[object null]" {
		t.Fatalf("feedback type = %v, want nullable object", feedback["type"])
	}
}

func TestCommitChapterRejectsUnknownForeshadowReferenceBeforePending(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.Init(1); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{
		"chapter": 1, "title": "Chương một", "summary": "Đẩy tiến", "characters": []string{"Nhân vật chính"}, "key_events": []string{"Phát hiện manh mối"},
		"foreshadow_updates": []map[string]any{{"id": "missing", "action": "resolve"}},
	})
	if _, err := newTestCommitChapterTool(s).Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "unknown id") {
		t.Fatalf("expected unknown foreshadow rejection, got %v", err)
	}
	if pending, err := s.Signals.LoadPendingCommit(); err != nil || pending != nil {
		t.Fatalf("invalid args must not create pending commit: pending=%+v err=%v", pending, err)
	}
}

func TestCommitChapterRejectsSkippedNormalChapter(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]any{
		"chapter": 2, "title": "Chương hai", "summary": "Bỏ qua chương một", "characters": []string{"Nhân vật chính"}, "key_events": []string{"Sự kiện"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := newTestCommitChapterTool(s).Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "chỉ nộp được chương kế tiếp 1") {
		t.Fatalf("expected skipped chapter rejection, got %v", err)
	}
	if pending, err := s.Signals.LoadPendingCommit(); err != nil || pending != nil {
		t.Fatalf("rejected commit must not create pending state, pending=%+v err=%v", pending, err)
	}
}

func TestCommitChapterRejectsInvalidNestedFields(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.Init(1); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{
		"chapter": 1, "title": "Chương một", "summary": "Đẩy tiến", "characters": []string{"Nhân vật chính"}, "key_events": []string{"Phát hiện manh mối"},
		"relationship_changes": []map[string]any{{"character_a": "Nhân vật chính", "character_b": "", "relation": "Thù địch"}},
	})
	if _, err := newTestCommitChapterTool(s).Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "relationship_changes[0]") {
		t.Fatalf("expected nested field rejection, got %v", err)
	}
}

func TestCommitChapterRejectsNonPendingRewrite(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := store.Progress.MarkChapterComplete(2, 3000, "", ""); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := store.Progress.SetPendingRewrites([]int{2}, "Kiểm thử viết lại"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := store.Progress.SetFlow(domain.FlowRewriting); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}
	if err := store.Drafts.SaveDraft(3, "Đây là nội dung truyện của chương sai."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	tool := newTestCommitChapterTool(store)
	args, err := json.Marshal(map[string]any{
		"chapter":         3,
		"title":           "Chương ba",
		"summary":         "Nộp sai",
		"characters":      []string{"Nhân vật chính"},
		"key_events":      []string{"Nộp nhầm"},
		"timeline_events": []any{},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Fatal("expected commit to be rejected during rewrite flow")
	}

	if _, err := os.Stat(dir + "/chapters/03.md"); !os.IsNotExist(err) {
		t.Fatalf("chapter should not be persisted, stat err=%v", err)
	}

	progress, err := store.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if len(progress.CompletedChapters) != 1 || progress.CompletedChapters[0] != 2 {
		t.Fatalf("completed chapters should only contain original chapter 2, got %v", progress.CompletedChapters)
	}
	if progress.CurrentChapter != 3 {
		t.Fatalf("current chapter should not advance beyond original progress, got %d", progress.CurrentChapter)
	}
}

func TestCommitChapterAllowsPendingRewrite(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := store.Progress.MarkChapterComplete(2, 3000, "", ""); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := store.Progress.SetPendingRewrites([]int{2}, "Kiểm thử viết lại"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := store.Progress.SetFlow(domain.FlowRewriting); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}
	if err := store.Drafts.SaveDraft(2, "Đây là nội dung truyện của chương đúng cần viết lại."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	tool := newTestCommitChapterTool(store)
	args, err := json.Marshal(map[string]any{
		"chapter":         2,
		"title":           "Chương hai",
		"summary":         "Nộp đúng",
		"characters":      []string{"Nhân vật chính"},
		"key_events":      []string{"Hoàn tất viết lại"},
		"timeline_events": []any{},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, err := os.Stat(dir + "/chapters/02.md"); err != nil {
		t.Fatalf("chapter should be persisted: %v", err)
	}

	progress, err := store.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if len(progress.CompletedChapters) != 1 || progress.CompletedChapters[0] != 2 {
		t.Fatalf("unexpected completed chapters: %v", progress.CompletedChapters)
	}
	pending, err := store.Signals.LoadPendingCommit()
	if err != nil {
		t.Fatalf("LoadPendingCommit: %v", err)
	}
	if pending != nil {
		t.Fatalf("expected pending commit cleared, got %+v", pending)
	}
}

// TestCommitChapterRewriteKeepsOwnForeshadowPlant khóa issue #112: khi viết lại "chương gieo manh mối" của một manh mối thì
// Writer thấy sổ phục bút đã có manh mối đó nên tự nhiên chỉ ghi advance; bản cũ ghi đè toàn bộ bản ghi chương nên plant bị mất,
// Projector phát lại toàn bộ thì báo "đẩy tiếp manh mối chưa biết" và khóa hàng chờ làm lại. Sự thật gieo manh mối phải được giữ lại.
func TestCommitChapterRewriteKeepsOwnForeshadowPlant(t *testing.T) {
	const foreshadowID = "f_spillway_photo"
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	// trạng thái đã ghi sau lần nộp đầu tiên của chương 2: bản ghi là plant, sổ phục bút đã tạo mục.
	if _, err := s.ChapterRecords.Accept(2, domain.ChapterOriginGenerated, "Nội dung truyện phiên bản cũ.", domain.ChapterFacts{
		Title: "Chương hai", Summary: "Gieo manh mối", KeyEvents: []string{"Phát hiện ảnh cũ"},
		ForeshadowUpdates: []domain.ForeshadowUpdate{{ID: foreshadowID, Action: "plant", Description: "Ảnh cũ ở đường thoát hiểm"}},
	}, domain.StyleDelta{}); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if err := s.World.SaveForeshadowLedger([]domain.ForeshadowEntry{
		{ID: foreshadowID, Description: "Ảnh cũ ở đường thoát hiểm", PlantedAt: 2, Status: "planted"},
	}); err != nil {
		t.Fatalf("SaveForeshadowLedger: %v", err)
	}
	if err := s.Progress.MarkChapterComplete(2, 3000, "", ""); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := s.Progress.SetPendingRewrites([]int{2}, "Kiểm thử viết lại"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := s.Progress.SetFlow(domain.FlowRewriting); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}
	if err := s.Drafts.SaveDraft(2, "Đây là nội dung truyện chương hai sau khi viết lại."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	args, err := json.Marshal(map[string]any{
		"chapter": 2, "title": "Chương hai", "summary": "Gieo manh mối sau khi viết lại",
		"characters": []string{"Nhân vật chính"}, "key_events": []string{"Phát hiện lại ảnh cũ"},
		"foreshadow_updates": []map[string]any{{"id": foreshadowID, "action": "advance"}},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if _, err := newTestCommitChapterTool(s).Execute(context.Background(), args); err != nil {
		t.Fatalf("viết lại chương gieo manh mốikhông được thất bại: %v", err)
	}

	record, err := s.ChapterRecords.Load(2)
	if err != nil || record == nil {
		t.Fatalf("Load record: %+v err=%v", record, err)
	}
	var planted bool
	for _, u := range record.Facts.ForeshadowUpdates {
		if u.ID == foreshadowID && u.Action == "plant" {
			planted = true
		}
	}
	if !planted {
		t.Fatalf("sau khi viết lại phải giữ lại plant của chương này，thực tế %+v", record.Facts.ForeshadowUpdates)
	}

	ledger, err := s.World.LoadForeshadowLedger()
	if err != nil {
		t.Fatalf("LoadForeshadowLedger: %v", err)
	}
	if len(ledger) != 1 || ledger[0].ID != foreshadowID || ledger[0].PlantedAt != 2 {
		t.Fatalf("sổ phục bút phải giữ sự thật gieo manh mối của chương 2，thực tế %+v", ledger)
	}

	progress, err := s.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if len(progress.PendingRewrites) != 0 {
		t.Fatalf("hàng chờ làm lạiphải đã được rút cạn，thực tế %v", progress.PendingRewrites)
	}
}

func TestCommitChapterRewriteRepairsPlantLostByPreviousFailure(t *testing.T) {
	const foreshadowID = "F24_LICENSE_SUSPENSION"
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatal(err)
	}
	// mô phỏng trạng thái sau khi lần làm lại đầu tiên của phiên bản cũ thất bại: sổ phục bút dẫn xuất vẫn giữ đúng sự thật gieo manh mối,
	// nhưng bản ghi chương đã bị advance ghi đè nên thiếu plant cùng chương。
	oldContent := "Bản chính còn sót lại sau khi phiên bản cũ thất bại."
	if _, err := s.ChapterRecords.Accept(2, domain.ChapterOriginGenerated, oldContent, domain.ChapterFacts{
		Title: "Chương hai", Summary: "Bản ghi hỏng", KeyEvents: []string{"Đẩy tiếp manh mối"},
		ForeshadowUpdates: []domain.ForeshadowUpdate{{ID: foreshadowID, Action: "advance"}},
	}, domain.StyleDelta{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Drafts.SaveFinalChapter(2, oldContent); err != nil {
		t.Fatal(err)
	}
	if err := s.World.SaveForeshadowLedger([]domain.ForeshadowEntry{{
		ID: foreshadowID, Description: "Manh mối giấy phép bị tạm dừng", PlantedAt: 2, Status: "planted",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.MarkChapterComplete(2, 3000, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.SetPendingRewrites([]int{2}, "Khôi phục bản ghi hỏng của phiên bản cũ"); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.SetFlow(domain.FlowPolishing); err != nil {
		t.Fatal(err)
	}
	if err := s.Drafts.SaveDraft(2, "Nội dung truyện chương hai đã sửa."); err != nil {
		t.Fatal(err)
	}

	args, err := json.Marshal(map[string]any{
		"chapter": 2, "title": "Chương hai", "summary": "Khôi phục chuỗi manh mối",
		"characters": []string{"Nhân vật chính"}, "key_events": []string{"Đẩy tiếp manh mối"},
		"foreshadow_updates": []map[string]any{{"id": foreshadowID, "action": "advance"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newTestCommitChapterTool(s).Execute(context.Background(), args); err != nil {
		t.Fatalf("plant cùng chương bị phiên bản cũ mất phải khôi phục được một cách tất định: %v", err)
	}

	record, err := s.ChapterRecords.Load(2)
	if err != nil || record == nil {
		t.Fatalf("Load record: %+v err=%v", record, err)
	}
	if got := record.Facts.ForeshadowUpdates; len(got) != 2 || got[0].Action != "plant" || got[0].ID != foreshadowID || got[1].Action != "advance" {
		t.Fatalf("repaired foreshadow chain = %+v", got)
	}
	ledger, err := s.World.LoadForeshadowLedger()
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger) != 1 || ledger[0].Status != "advanced" || ledger[0].PlantedAt != 2 {
		t.Fatalf("reprojected ledger = %+v", ledger)
	}
}

func TestCommitChapterRewriteValidatesRecordSetBeforeWriting(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChapterRecords.Accept(1, domain.ChapterOriginGenerated, "Bản chính chương một.", domain.ChapterFacts{
		Title: "Chương một", Summary: "Đường cơ sở hỏng", KeyEvents: []string{"Đẩy tiếp sai"},
		ForeshadowUpdates: []domain.ForeshadowUpdate{{ID: "missing", Action: "advance"}},
	}, domain.StyleDelta{}); err != nil {
		t.Fatal(err)
	}
	oldContent := "Bản chính cũ của chương hai."
	if _, err := s.ChapterRecords.Accept(2, domain.ChapterOriginGenerated, oldContent, domain.ChapterFacts{
		Title: "Chương hai", Summary: "Tóm tắt gốc", KeyEvents: []string{"Sự kiện gốc"},
	}, domain.StyleDelta{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Drafts.SaveFinalChapter(2, oldContent); err != nil {
		t.Fatal(err)
	}
	for _, chapter := range []int{1, 2} {
		if err := s.Progress.MarkChapterComplete(chapter, 3000, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Progress.SetPendingRewrites([]int{2}, "Kiểm thử kiểm tra trước khi ghi"); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.SetFlow(domain.FlowRewriting); err != nil {
		t.Fatal(err)
	}
	if err := s.Drafts.SaveDraft(2, "Nội dung truyện mới của chương hai."); err != nil {
		t.Fatal(err)
	}

	args, err := json.Marshal(map[string]any{
		"chapter": 2, "title": "Chương hai", "summary": "Tóm tắt mới",
		"characters": []string{"Nhân vật chính"}, "key_events": []string{"Sự kiện mới"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = newTestCommitChapterTool(s).Execute(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "đã gỡ đóng băng và chưa ghi kết quả làm lại") {
		t.Fatalf("expected preflight projection error, got %v", err)
	}
	if strings.Contains(err.Error(), errs.ErrStoreWrite.Error()) {
		t.Fatalf("projection invariant must not be classified as a store write: %v", err)
	}
	final, err := s.Drafts.LoadChapterText(2)
	if err != nil || final != oldContent {
		t.Fatalf("final chapter changed before validation: %q err=%v", final, err)
	}
	record, err := s.ChapterRecords.Load(2)
	if err != nil || record == nil || record.Revision != 1 || record.Content != oldContent {
		t.Fatalf("chapter record changed before validation: %+v err=%v", record, err)
	}
	if pending, err := s.Signals.LoadPendingCommit(); err != nil || pending != nil {
		t.Fatalf("invalid frozen commit must be cleared: %+v err=%v", pending, err)
	}
}

// TestCommitChapterRewriteRejectsForwardForeshadowReference cùng nguồn vớitrường hợp trên (issue #112):
// sổ phục bút là hình chiếu của cả cuốn, khi viết lại chương đầu thì bên trong vẫn còn manh mối chỉ gieo ở các chương về sau. Bản cũ cho qua → Projector
// phát lại theo thứ tự chương thì báo "đẩy tiếp manh mối chưa biết", và lúc đó bản ghi chương đã bị ghi đè, hàng chờ làm lại bị khóa.
// phải chặn trước khi ghi xuống và nói rõ "manh mối được gieo ở chương nào" thì mô hình mới tự sửa được.
func TestCommitChapterRewriteRejectsForwardForeshadowReference(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	for _, ch := range []int{2, 7} {
		if _, err := s.ChapterRecords.Accept(ch, domain.ChapterOriginGenerated, "Nội dung truyện phiên bản cũ.", domain.ChapterFacts{
			Title: fmt.Sprintf("Chương %d", ch), Summary: "Tóm tắt", KeyEvents: []string{"Sự kiện"},
		}, domain.StyleDelta{}); err != nil {
			t.Fatalf("Accept %d: %v", ch, err)
		}
		if err := s.Progress.MarkChapterComplete(ch, 3000, "", ""); err != nil {
			t.Fatalf("MarkChapterComplete %d: %v", ch, err)
		}
	}
	if err := s.World.SaveForeshadowLedger([]domain.ForeshadowEntry{
		{ID: "f_late", Description: "Manh mối chỉ gieo ở chương bảy", PlantedAt: 7, Status: "planted"},
	}); err != nil {
		t.Fatalf("SaveForeshadowLedger: %v", err)
	}
	if err := s.Progress.SetPendingRewrites([]int{2}, "Kiểm thử viết lại"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := s.Progress.SetFlow(domain.FlowRewriting); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}
	if err := s.Drafts.SaveDraft(2, "Đây là nội dung truyện chương hai sau khi viết lại."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	args, err := json.Marshal(map[string]any{
		"chapter": 2, "title": "Chương hai", "summary": "s",
		"characters": []string{"Nhân vật chính"}, "key_events": []string{"e"},
		"foreshadow_updates": []map[string]any{{"id": "f_late", "action": "advance"}},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	_, err = newTestCommitChapterTool(s).Execute(context.Background(), args)
	if err == nil {
		t.Fatal("phải từ chối manh mối chỉ được gieo ở các chương về sau")
	}
	if !strings.Contains(err.Error(), "được gieo ở chương 7") {
		t.Fatalf("lỗi phải chỉ rõ chương gieo manh mối, thì mô hình mới tự sửa được，thực tế: %v", err)
	}
	// mấu chốt: chặn trước khi ghi xuống — bản ghi chương và hàng chờ làm lại không được bị lần thất bại này làm bẩn.
	if pending, err := s.Signals.LoadPendingCommit(); err != nil || pending != nil {
		t.Fatalf("kiểm tra thất bại không được để lại pending commit: pending=%+v err=%v", pending, err)
	}
	record, err := s.ChapterRecords.Load(2)
	if err != nil || record == nil {
		t.Fatalf("Load record: %+v err=%v", record, err)
	}
	if record.Content != "Nội dung truyện phiên bản cũ." {
		t.Fatalf("kiểm tra thất bại không được ghi đè bản ghi chương，thực tế %q", record.Content)
	}
	p, err := s.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if len(p.PendingRewrites) != 1 || p.PendingRewrites[0] != 2 {
		t.Fatalf("hàng chờ làm lạiphải được giữ nguyên để thử lại: %v", p.PendingRewrites)
	}
}

func TestCommitChapterClearsInvalidLegacyRewritePending(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatal(err)
	}
	oldContent := "Bản chính cũ."
	if _, err := s.ChapterRecords.Accept(2, domain.ChapterOriginGenerated, oldContent, domain.ChapterFacts{
		Title: "Chương hai", Summary: "Tóm tắt cũ", KeyEvents: []string{"Sự kiện cũ"},
	}, domain.StyleDelta{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Drafts.SaveFinalChapter(2, oldContent); err != nil {
		t.Fatal(err)
	}
	if err := s.World.SaveForeshadowLedger([]domain.ForeshadowEntry{{
		ID: "f_late", Description: "Manh mối về sau", PlantedAt: 7, Status: "planted",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.MarkChapterComplete(2, 3000, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.SetPendingRewrites([]int{2}, "Khôi phục bài nộp cũ bị đóng băng"); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.SetFlow(domain.FlowRewriting); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"chapter": 2, "title": "Chương hai", "summary": "Bài nộp cũ không hợp lệ",
		"characters": []string{"Nhân vật chính"}, "key_events": []string{"Đẩy tiếp sớm"},
		"foreshadow_updates": []map[string]any{{"id": "f_late", "action": "advance"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Signals.SavePendingCommit(domain.PendingCommit{
		Chapter: 2, Stage: domain.CommitStageStarted, Rewrite: true,
		Payload: payload, DraftContent: "Nội dung truyện bị đóng băng.",
	}); err != nil {
		t.Fatal(err)
	}

	_, err = newTestCommitChapterTool(s).Execute(context.Background(), payload)
	if err == nil || !strings.Contains(err.Error(), "đã gỡ đóng băng") || !strings.Contains(err.Error(), "được gieo ở chương 7") {
		t.Fatalf("expected actionable legacy pending error, got %v", err)
	}
	if pending, err := s.Signals.LoadPendingCommit(); err != nil || pending != nil {
		t.Fatalf("invalid legacy pending must be cleared: %+v err=%v", pending, err)
	}
	record, err := s.ChapterRecords.Load(2)
	if err != nil || record == nil || record.Revision != 1 || record.Content != oldContent {
		t.Fatalf("clearing pending changed chapter record: %+v err=%v", record, err)
	}
}

func TestCommitChapterRefreshesSharedStyleStatsAfterRewrite(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatal(err)
	}
	completed := []int{1, 2, 3, 4, 5}
	for _, chapter := range completed {
		content := fmt.Sprintf("# 第%d章\n普通正文。\n故事继续。", chapter)
		if err := s.Drafts.SaveFinalChapter(chapter, content); err != nil {
			t.Fatal(err)
		}
		saveTestChapterRecord(t, s, chapter, content)
		if err := s.Progress.MarkChapterComplete(chapter, 100, "", ""); err != nil {
			t.Fatal(err)
		}
	}

	styleStats := NewStyleStatsIndex(s)
	before, err := styleStats.Snapshot(completed, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if before == nil {
		t.Fatal("expected initialized style stats")
	}

	if err := s.Progress.SetPendingRewrites([]int{2}, "测试增量风格统计"); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.SetFlow(domain.FlowRewriting); err != nil {
		t.Fatal(err)
	}
	if err := s.Drafts.SaveDraft(2, "# 第二章\n他不是退缩，而是在等待。\n改写后的故事继续。"); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"title":      "第二章",
		"summary":    "完成增量统计测试重写",
		"characters": []string{"主角"},
		"key_events": []string{"完成重写"},
	})
	if _, err := NewCommitChapterTool(s, styleStats).Execute(context.Background(), args); err != nil {
		t.Fatal(err)
	}

	after, err := styleStats.Snapshot(completed, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, pattern := range after.Patterns {
		if strings.HasPrefix(pattern.Name, "Câu chữ đính chính") && pattern.Total == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("rewrite did not refresh shared style stats: %+v", after.Patterns)
	}
}

func TestCommitChapterRewriteRecoveryUsesFrozenDraft(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := s.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}
	if err := s.Drafts.SaveFinalChapter(2, "Bản chính cũ của chương hai"); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}
	if err := s.Progress.MarkChapterComplete(2, 100, "", ""); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := s.Progress.SetPendingRewrites([]int{2}, "Kiểm thử khôi phục viết lại"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := s.Progress.SetFlow(domain.FlowRewriting); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}

	persistedArgs, err := json.Marshal(map[string]any{
		"chapter": 2, "title": "Tiêu đề đóng băng", "summary": "Tóm tắt đóng băng", "characters": []string{"Nhân vật chính"}, "key_events": []string{"Sự kiện đóng băng"},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := s.Signals.SavePendingCommit(domain.PendingCommit{
		Chapter: 2, Stage: domain.CommitStageStarted, Rewrite: true, RewriteMode: "rewrite",
		Payload: persistedArgs, DraftContent: "Nội dung truyện đã viết lại xong của chương hai",
	}); err != nil {
		t.Fatalf("SavePendingCommit: %v", err)
	}
	if err := s.Drafts.SaveDraft(2, "Bản nháp bị ghi đè sai sau khi khởi động lại"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	tool := newTestCommitChapterTool(s)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"chapter":2,"summary":"Tham số mới không được dùng"}`)); err != nil {
		t.Fatalf("Execute recovery: %v", err)
	}
	final, err := s.Drafts.LoadChapterText(2)
	if err != nil {
		t.Fatalf("LoadChapterText: %v", err)
	}
	if final != "# Tiêu đề đóng băng\n\nNội dung truyện đã viết lại xong của chương hai" {
		t.Fatalf("rewrite recovery used overwritten draft: %q", final)
	}
	summary, err := s.Summaries.LoadSummary(2)
	if err != nil {
		t.Fatalf("LoadSummary: %v", err)
	}
	if summary == nil || summary.Summary != "Tóm tắt đóng băng" {
		t.Fatalf("rewrite recovery used regenerated args: %+v", summary)
	}
}

// TestCommitChapterUpdatesCastLedger xác minh: commit_chapter cộng dồn characters của chương này vào cast_ledger,
// brief_role do cast_intros cung cấp được sử dụng, và các nhân vật cốt lõi trong characters.json không đi vào ledger.
func TestCommitChapterUpdatesCastLedger(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := s.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}
	// thiết lập hồ sơ nhân vật cốt lõi (những người này không được đi vào cast_ledger)
	if err := s.Characters.Save([]domain.Character{
		{Name: "Lâm Mặc", Role: "Nhân vật chính", Tier: "core"},
		{Name: "Lý Thanh Nghiên", Role: "Sư phụ", Tier: "important"},
	}); err != nil {
		t.Fatalf("Save core characters: %v", err)
	}
	if err := s.Drafts.SaveDraft(1, "Nội dung truyện chương một, Lâm Mặc gặp chủ quán trọ Lão Chu và tên nhân viên A Vân."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	tool := newTestCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    1,
		"title":      "Chương một",
		"summary":    "Lâm Mặc nhận phòng ở quán trọ",
		"characters": []string{"Lâm Mặc", "Lý Thanh Nghiên", "Lão Chu", "A Vân"},
		"key_events": []string{"Nhận phòng"},
		"cast_intros": []any{
			map[string]any{"name": "Lão Chu", "brief_role": "Chủ quán trọ"},
			map[string]any{"name": "A Vân", "brief_role": "Tên nhân viên trọ"},
		},
	})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	summary, err := s.Summaries.LoadSummary(1)
	if err != nil {
		t.Fatal(err)
	}
	if summary == nil || summary.Title != "Chương một" {
		t.Fatalf("committed title = %+v", summary)
	}

	entries, err := s.Cast.Load()
	if err != nil {
		t.Fatalf("Cast.Load: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 ledger entries (Lão Chu/A Vân), got %d: %+v", len(entries), entries)
	}
	byName := map[string]domain.CastEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if e, ok := byName["Lão Chu"]; !ok || e.BriefRole != "Chủ quán trọ" || e.FirstSeenChapter != 1 {
		t.Errorf("Lão Chu entry wrong: %+v", e)
	}
	if e, ok := byName["A Vân"]; !ok || e.BriefRole != "Tên nhân viên trọ" || e.AppearanceCount != 1 {
		t.Errorf("A Vân entry wrong: %+v", e)
	}
	if _, ok := byName["Lâm Mặc"]; ok {
		t.Errorf("nhân vật cốt lõi Lâm Mặc không được đi vào ledger")
	}
	if _, ok := byName["Lý Thanh Nghiên"]; ok {
		t.Errorf("nhân vật cốt lõi Lý Thanh Nghiên không được đi vào ledger")
	}
}

func TestCommitChapterReplayAfterPartialCommitDoesNotDuplicateWorldState(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := s.Drafts.SaveDraft(1, "Nội dung truyện chương một, Lâm Mặc gặp bóng đen và thăng cấp."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	timeline := []domain.TimelineEvent{{
		Chapter:    1,
		Time:       "Buổi sớm",
		Event:      "Lâm Mặc gặp bóng đen",
		Characters: []string{"Lâm Mặc"},
	}}
	stateChanges := []domain.StateChange{{
		Chapter:  1,
		Entity:   "Lâm Mặc",
		Field:    "realm",
		OldValue: "Phàm tử",
		NewValue: "Tu luyện khí kỳ",
	}}
	foreshadow := []domain.ForeshadowUpdate{{
		ID:          "f1",
		Action:      "plant",
		Description: "Thân phận của bóng đen",
	}}

	// mô phỏng tiến trình sập sau khi commit_chapter đã ghi trạng thái thế giới nhưng chưa MarkChapterComplete。
	if err := s.World.AppendTimelineEvents(timeline); err != nil {
		t.Fatalf("AppendTimelineEvents seed: %v", err)
	}
	if err := s.World.AppendStateChanges(stateChanges); err != nil {
		t.Fatalf("AppendStateChanges seed: %v", err)
	}
	if err := s.World.UpdateForeshadow(1, foreshadow); err != nil {
		t.Fatalf("UpdateForeshadow seed: %v", err)
	}
	persistedArgs, _ := json.Marshal(map[string]any{
		"chapter":            1,
		"title":              "Chương một",
		"summary":            "Lâm Mặc gặp bóng đen và thăng cấp",
		"characters":         []string{"Lâm Mặc"},
		"key_events":         []string{"gặp bóng đen", "thăng cấp"},
		"timeline_events":    timeline,
		"state_changes":      stateChanges,
		"foreshadow_updates": foreshadow,
	})
	if err := s.Signals.SavePendingCommit(domain.PendingCommit{
		Chapter:      1,
		Stage:        domain.CommitStageStarted,
		Summary:      "Tóm tắt nửa chừng",
		Payload:      persistedArgs,
		DraftContent: "Nội dung truyện chương một, Lâm Mặc gặp bóng đen và thăng cấp.",
	}); err != nil {
		t.Fatalf("SavePendingCommit: %v", err)
	}
	if err := s.Drafts.SaveDraft(1, "Bản nháp bị Worker mới ghi đè sau khi khởi động lại, tuyệt đối không được trộn vào bài đã nộp cũ."); err != nil {
		t.Fatalf("overwrite draft: %v", err)
	}

	tool := newTestCommitChapterTool(s)
	// mô phỏng Writer sau khi khởi động lại sinh ra tham số khác; quá trình khôi phục phải bỏ qua nó và dùng persistedArgs。
	args, _ := json.Marshal(map[string]any{
		"chapter":         1,
		"title":           "Tiêu đề sai",
		"summary":         "Tóm tắt mới sai",
		"characters":      []string{"Lâm Mặc"},
		"key_events":      []string{"Sự kiện sai"},
		"timeline_events": []domain.TimelineEvent{{Time: "Ban đêm", Event: "Sự kiện mới không được ghi"}},
	})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute replay: %v", err)
	}

	events, _ := s.World.LoadTimeline()
	if len(events) != 1 {
		t.Fatalf("timeline duplicated after replay, got %d: %+v", len(events), events)
	}
	changes, _ := s.World.LoadStateChanges()
	if len(changes) != 1 {
		t.Fatalf("state changes duplicated after replay, got %d: %+v", len(changes), changes)
	}
	ledger, _ := s.World.LoadForeshadowLedger()
	if len(ledger) != 1 {
		t.Fatalf("foreshadow duplicated after replay, got %d: %+v", len(ledger), ledger)
	}
	pending, _ := s.Signals.LoadPendingCommit()
	if pending != nil {
		t.Fatalf("pending commit should be cleared, got %+v", pending)
	}
	if cp := s.Checkpoints.LatestByStep(domain.ChapterScope(1), "commit"); cp == nil {
		t.Fatal("commit checkpoint should be written")
	}
	final, err := s.Drafts.LoadChapterText(1)
	if err != nil {
		t.Fatalf("LoadChapterText: %v", err)
	}
	if final != "# Chương một\n\nNội dung truyện chương một, Lâm Mặc gặp bóng đen và thăng cấp." {
		t.Fatalf("recovery used overwritten draft: %q", final)
	}
}

func TestCommitChapterRecoversProgressMarkedWindowWithExactOutput(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(2); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := s.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}
	if err := s.Progress.StartChapter(1); err != nil {
		t.Fatalf("StartChapter: %v", err)
	}
	if err := s.Drafts.SaveFinalChapter(1, "Bản chính chương một"); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}
	if err := s.Summaries.SaveSummary(domain.ChapterSummary{Chapter: 1, Title: "Chương một", Summary: "Tóm tắt"}); err != nil {
		t.Fatalf("SaveSummary: %v", err)
	}
	if _, err := s.ChapterRecords.Accept(1, domain.ChapterOriginGenerated, "Bản chính chương một", domain.ChapterFacts{
		Title: "Chương một", Summary: "Tóm tắt", KeyEvents: []string{"Sự kiện"},
	}, domain.StyleDelta{}); err != nil {
		t.Fatalf("SaveChapterRecord: %v", err)
	}
	if err := s.Progress.MarkChapterComplete(1, 100, "mystery", "quest"); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}

	want := json.RawMessage(`{"chapter":1,"committed":true,"recovered":"exact"}`)
	if err := s.Signals.SavePendingCommit(domain.PendingCommit{
		Chapter: 1,
		Stage:   domain.CommitStageProgressMarked,
		Output:  want,
	}); err != nil {
		t.Fatalf("SavePendingCommit: %v", err)
	}

	tool := newTestCommitChapterTool(s)
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"chapter":1}`))
	if err != nil {
		t.Fatalf("Execute recovery: %v", err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, got); err != nil {
		t.Fatalf("compact recovered output: %v", err)
	}
	if compact.String() != string(want) {
		t.Fatalf("recovered output = %s, want exact document %s", got, want)
	}
	if pending, err := s.Signals.LoadPendingCommit(); err != nil || pending != nil {
		t.Fatalf("pending commit should be cleared, pending=%+v err=%v", pending, err)
	}
	if cp := s.Checkpoints.LatestByStep(domain.ChapterScope(1), "commit"); cp == nil {
		t.Fatal("commit checkpoint should be repaired")
	}
	p, err := s.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if p.InProgressChapter != 0 {
		t.Fatalf("in-progress chapter should be cleared, got %d", p.InProgressChapter)
	}
}

// TestCommitChapterRejectsPolishWithoutDraftChange xác minh: khi chương đã hoàn thành bước vào hàng chờ hoàn thiện / viết lại thì
// nếu cả nội dung truyện và tiêu đề đều không đổi, commit_chapter phải từ chối lần làm lại rỗng.
// TestCommitChapterNonLayeredRecompletesAfterRework xác minh sách không phân tầng sau khi hoàn thành đi qua reopen làm lại,
// khi commit chương đã sửa và hàng chờ rút cạn thì tự động quay lại complete (nhánh không phân tầng xác định hoàn thành sau drain).
func TestCommitChapterNonLayeredRecompletesAfterRework(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(2); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// hai chương viết xong và hoàn thành. chương 2 có đủ drafts/chapters để nộp làm lại.
	ch1 := "Nội dung truyện gốc của chương một."
	ch2 := "Nội dung truyện gốc của chương hai, mô phỏng bản chính đã nộp."
	if err := s.Drafts.SaveFinalChapter(1, ch1); err != nil {
		t.Fatalf("SaveFinalChapter(1): %v", err)
	}
	if err := s.Drafts.SaveDraft(2, ch2); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := s.Drafts.SaveFinalChapter(2, ch2); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}
	saveTestChapterRecord(t, s, 1, ch1)
	saveTestChapterRecord(t, s, 2, ch2)
	if err := s.Progress.MarkChapterComplete(1, 100, "", ""); err != nil {
		t.Fatalf("MarkChapterComplete(1): %v", err)
	}
	if err := s.Progress.MarkChapterComplete(2, len([]rune(ch2)), "", ""); err != nil {
		t.Fatalf("MarkChapterComplete(2): %v", err)
	}
	if err := s.Progress.MarkComplete(); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}

	// reopen chương 2 → phase về writing, PendingRewrites=[2], flow=rewriting
	if err := s.Progress.Reopen([]int{2}, "Làm lại"); err != nil {
		t.Fatalf("Reopen: %v", err)
	}

	// nộp làm lại (bản nháp phải khác bản chính thì mới được qua)
	if err := s.Drafts.SaveDraft(2, ch2+"\n\nĐoạn văn thêm sau khi làm lại."); err != nil {
		t.Fatalf("SaveDraft (reworked): %v", err)
	}
	tool := newTestCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"title":      "Chương hai",
		"summary":    "Tóm tắt sau khi làm lại",
		"characters": []string{"Nhân vật chính"},
		"key_events": []string{"Dọn dẹp"},
	})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute rework commit: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload["book_complete"] != true {
		t.Errorf("book_complete = %v, want true", payload["book_complete"])
	}

	p, _ := s.Progress.Load()
	if p.Phase != domain.PhaseComplete {
		t.Errorf("phase = %s, want complete (cần tự động thu gọn lại)", p.Phase)
	}
	if len(p.PendingRewrites) != 0 {
		t.Errorf("PendingRewrites = %v, want empty", p.PendingRewrites)
	}
}

// TestCommitChapterLayeredReopenRecompletesDespiteOpenThread xác minh chặn cuối: sách phân tầng đi qua reopen
// làm lại, dù compass vẫn còn tuyến dài chưa khép lại (làm lại có thể làm nhiễu), sau khi rút cạn vẫn hoàn thành lại theo "cấu trúc hoàn chỉnh" —
// không kẹt ở writing, loại bỏ vòng lặp vô hạn do viết tiếp vượt phạm vi ở cuối tập chốt hạt (§6.5 / họ known_outline_exhaustion).
// phản chứng: nếu đường reopen vẫn dùng layeredBookComplete ở mức chất lượng, open thread của ví dụ này sẽ khiến nó trả false,
// book_complete là giả, và test sẽ thất bại.
func TestCommitChapterLayeredReopenRecompletesDespiteOpenThread(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// một tập một cung hai chương, khai triển toàn bộ
	foundation := NewSaveFoundationTool(s)
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "Tập một", "theme": "Chủ đề",
			"arcs": []map[string]any{{
				"index": 1, "title": "Cung một", "goal": "Mục tiêu",
				"chapters": []map[string]any{
					{"title": "Chương đầu", "core_event": "Khởi", "hook": "Nối tiếp"},
					{"title": "Chương kế", "core_event": "Nối tiếp", "hook": "Kết"},
				},
			}},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}

	// hai chương viết xong, ghi xuống và hoàn thành
	ch2 := "Nội dung truyện gốc của chương hai, mô phỏng bản chính đã nộp."
	for ch, body := range map[int]string{1: "Nội dung truyện chương một.", 2: ch2} {
		if err := s.Drafts.SaveDraft(ch, body); err != nil {
			t.Fatalf("SaveDraft %d: %v", ch, err)
		}
		if err := s.Drafts.SaveFinalChapter(ch, body); err != nil {
			t.Fatalf("SaveFinalChapter %d: %v", ch, err)
		}
		saveTestChapterRecord(t, s, ch, body)
		if err := s.Progress.MarkChapterComplete(ch, len([]rune(body)), "", ""); err != nil {
			t.Fatalf("MarkChapterComplete %d: %v", ch, err)
		}
	}
	if err := s.Progress.MarkComplete(); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}

	// mô phỏng "làm lại đã làm nhiễu tuyến dài": compass vẫn còn open thread chưa khép lại
	if err := s.Outline.SaveCompass(domain.StoryCompass{EndingDirection: "Nhân vật chính trở về quê", OpenThreads: []string{"Kẻ thù chưa trừ"}}); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}

	// reopen chương 2 → nộp làm lại (bản nháp phải khác bản chính thì mới được qua)
	if err := s.Progress.Reopen([]int{2}, "Làm lại"); err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if err := s.Drafts.SaveDraft(2, ch2+"\n\nĐoạn văn thêm sau khi làm lại."); err != nil {
		t.Fatalf("SaveDraft reworked: %v", err)
	}
	tool := newTestCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter": 2, "title": "Chương hai", "summary": "Tóm tắt làm lại", "characters": []string{"Nhân vật chính"}, "key_events": []string{"Dọn dẹp"},
	})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute rework commit: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if bc, _ := out["book_complete"].(bool); !bc {
		t.Error("sau khi hàng chờ làm lại của reopen rút cạn thì phải hoàn thành lại theo \"cấu trúc hoàn chỉnh\" (dù tuyến dài chưa khép lại)")
	}
	p, _ := s.Progress.Load()
	if p.Phase != domain.PhaseComplete {
		t.Errorf("phase = %s, want complete", p.Phase)
	}
	if p.ReopenedFromComplete {
		t.Error("sau khi hoàn thành lại, ReopenedFromComplete phải được xóa")
	}
}

func TestCommitChapterRejectsPolishWithoutDraftChange(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// mô phỏng chương 2 đã hoàn thành bình thường: drafts và chapters có nội dung giống nhau.
	original := "Nội dung truyện gốc của chương hai, mô phỏng bản chính đã nộp."
	if err := s.Drafts.SaveDraft(2, original); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := s.Drafts.SaveFinalChapter(2, original); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}
	if err := s.Progress.MarkChapterComplete(2, len([]rune(original)), "mystery", "quest"); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := s.Summaries.SaveSummary(domain.ChapterSummary{Chapter: 2, Title: "Chương hai", Summary: "Tóm tắt gốc"}); err != nil {
		t.Fatalf("SaveSummary: %v", err)
	}

	// đưa vào hàng chờ hoàn thiện：Flow=Polishing, PendingRewrites=[2]
	if err := s.Progress.SetPendingRewrites([]int{2}, "Kiểm thử hoàn thiện"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := s.Progress.SetFlow(domain.FlowPolishing); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}

	tool := newTestCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"title":      "Chương hai",
		"summary":    "Giả vờ đã hoàn thiện",
		"characters": []string{"Nhân vật chính"},
		"key_events": []string{"Không thay đổi"},
	})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected commit to be rejected when drafts equals final content")
	}

	// viết lại một bản nháp khác → thì phải được qua
	polished := original + "\n\nĐoạn văn thêm sau khi hoàn thiện."
	if err := s.Drafts.SaveDraft(2, polished); err != nil {
		t.Fatalf("SaveDraft (polished): %v", err)
	}
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute after real polish: %v", err)
	}
}

func TestCommitChapterAllowsTitleOnlyRewrite(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatal(err)
	}
	body := "Nội dung truyện không cần sửa, chỉ tiêu đề cần hoàn thiện."
	if err := s.Drafts.SaveDraft(2, body); err != nil {
		t.Fatal(err)
	}
	if err := s.Drafts.SaveFinalChapter(2, body); err != nil {
		t.Fatal(err)
	}
	if err := s.Summaries.SaveSummary(domain.ChapterSummary{Chapter: 2, Title: "Tiêu đề cũ", Summary: "Tóm tắt gốc"}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Checkpoints.AppendArtifacts(
		domain.ChapterScope(2), "commit", "chapters/02.md", "summaries/02.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.MarkChapterComplete(2, len([]rune(body)), "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.SetPendingRewrites([]int{2}, "Tối ưu tiêu đề"); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.SetFlow(domain.FlowPolishing); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]any{
		"chapter": 2, "title": "Tiêu đề mới chính xác hơn", "summary": "Tóm tắt gốc",
		"characters": []string{"Nhân vật chính"}, "key_events": []string{"Sự kiện có sẵn"},
	})
	if _, err := newTestCommitChapterTool(s).Execute(context.Background(), args); err != nil {
		t.Fatalf("title-only rewrite failed: %v", err)
	}
	summary, err := s.Summaries.LoadSummary(2)
	if err != nil {
		t.Fatal(err)
	}
	if summary == nil || summary.Title != "Tiêu đề mới chính xác hơn" {
		t.Fatalf("committed title = %+v", summary)
	}
	after := s.Checkpoints.LatestByStep(domain.ChapterScope(2), "commit")
	if after == nil || after.Seq <= before.Seq {
		t.Fatalf("title-only rewrite did not produce a new checkpoint: before=%+v after=%+v", before, after)
	}
	final, err := s.Drafts.LoadChapterText(2)
	if err != nil {
		t.Fatal(err)
	}
	// Tiêu đề được engine kết xuất vào dòng đầu nội dung truyện, nên chỉ đổi tiêu đề cũng làm đổi tệp — phần đổi phải chỉ là dòng tiêu đề.
	if final != "# Tiêu đề mới chính xác hơn\n\n"+body {
		t.Fatalf("title-only rewrite changed body: %q", final)
	}
}

// TestCommitChapterLayeredRejectsOutOfRangeChapter xác minh ở chế độ phân tầng,，
// commit có số chương vượt ra ngoài layered_outline phải thất bại cứng, không được slog.Warn cho qua.
// Đây là phanh cứng chặn "sau khi quyết định sai, writer chạy thẳng không có chặn" (ca 《凡骨》ch204..347).
func TestCommitChapterLayeredRejectsOutOfRangeChapter(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// dựng một layered_outline chỉ có 1 tập 1 cung 1 chương
	foundation := NewSaveFoundationTool(s)
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "Tập một", "theme": "Chủ đề",
			"arcs": []map[string]any{{
				"index": 1, "title": "Cung một", "goal": "Mục tiêu",
				"chapters": []map[string]any{
					{"title": "Chương đầu", "core_event": "Khởi", "hook": "Nối tiếp"},
				},
			}},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}
	_ = s.Progress.UpdatePhase(domain.PhaseWriting)

	// commit của chương vượt phạm vi 2 phải thất bại cứng
	if err := s.Drafts.SaveDraft(2, "Nội dung truyện chương vượt phạm vi, phải bị chặn lại."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	tool := newTestCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"title":      "Chương hai",
		"summary":    "Chương vượt phạm vi",
		"characters": []string{"Nhân vật chính"},
		"key_events": []string{"không được phép"},
	})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected commit to fail when chapter out of layered outline range")
	}

	// tệp chương không được ghi xuống、Progress không được được đẩy tiếp
	if _, statErr := os.Stat(dir + "/chapters/02.md"); !os.IsNotExist(statErr) {
		t.Fatalf("chapter 2 should not be persisted, stat err=%v", statErr)
	}
	progress, _ := s.Progress.Load()
	if len(progress.CompletedChapters) != 0 {
		t.Fatalf("CompletedChapters should stay empty, got %v", progress.CompletedChapters)
	}
}

// TestCommitChapterLayeredAutoCompletesWhenDone xác minh lớp chốt an toàn hoàn thành tất định ở chế độ phân tầng：
// khi dàn ý đã khai triển và viết xong toàn bộ + không có cung khung + không có làm lại + manh mối hoạt động bằng 0 + tuyến dài của la bàn đã khép lại thì，
// commit chương cuối tự đẩy Phase=Complete, không phụ thuộc kiến trúc sư chủ động gọi complete_book.
// Đây là bản sửa livelock do 9bf26a5 xóa hoàn thành tự động phân tầng (ở cuối tập chốt hạt mô hình vừa không append
// vừa không complete → viết tay chạy trần vượt phạm vi rồi kẹt vòng lặp).
func TestCommitChapterLayeredAutoCompletesWhenDone(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// một tập một cung hai chương, khai triển toàn bộ (không có cung khung)
	foundation := NewSaveFoundationTool(s)
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "Tập một", "theme": "Chủ đề",
			"arcs": []map[string]any{{
				"index": 1, "title": "Cung một", "goal": "Mục tiêu",
				"chapters": []map[string]any{
					{"title": "Chương đầu", "core_event": "Khởi", "hook": "Nối tiếp"},
					{"title": "Chương kế", "core_event": "Nối tiếp", "hook": "Kết"},
				},
			}},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}
	// tuyến dài của la bàn đã khép lại (OpenThreads rỗng)
	if err := s.Outline.SaveCompass(domain.StoryCompass{EndingDirection: "Nhân vật chính trở về quê"}); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}
	_ = s.Progress.UpdatePhase(domain.PhaseWriting)

	tool := newTestCommitChapterTool(s)
	commit := func(ch int) map[string]any {
		if err := s.Drafts.SaveDraft(ch, fmt.Sprintf("Chương %d nội dung truyện, dùng để kiểm thử hoàn thành tất định.", ch)); err != nil {
			t.Fatalf("SaveDraft %d: %v", ch, err)
		}
		args, _ := json.Marshal(map[string]any{
			"chapter": ch, "title": fmt.Sprintf("Chương %d", ch), "summary": "Tóm tắt", "characters": []string{"Nhân vật chính"}, "key_events": []string{"Sự kiện"},
		})
		raw, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute ch%d: %v", ch, err)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("Unmarshal ch%d: %v", ch, err)
		}
		return out
	}

	// Chương 1: chưa viết xong thì không được hoàn thành
	if bc, _ := commit(1)["book_complete"].(bool); bc {
		t.Fatal("Viết xong chương 1 không được kích hoạt hoàn thành")
	}
	if p, _ := s.Progress.Load(); p.Phase == domain.PhaseComplete {
		t.Fatal("Viết xong chương 1 thì phase không được là complete")
	}

	// Chương 2 (chương cuối): phải tự động hoàn thành
	if bc, _ := commit(2)["book_complete"].(bool); !bc {
		t.Fatal("Viết xong chương cuối thì phải tự động hoàn thành")
	}
	if p, _ := s.Progress.Load(); p.Phase != domain.PhaseComplete {
		t.Fatalf("expected phase=complete, got %s", p.Phase)
	}
}

// TestCommitChapterFinaleVolumeCompletesDespiteOpenThreads xác minh toàn bộ chuỗi tập chốt hạt：
// sau khi đã tuyên bố tập chốt hạt (append_volume kèm final:true) —
//  1. commit chương cuối không hoàn thành: việc hoàn thành không được vượt trước bộ ba chốt cuối tập (đánh giá cung / tóm tắt cung / tóm tắt tập),
//     kết cục phải qua chất cổng chất lượng của editor;
//  2. khi bộ ba đủ và tóm tắt tập được lưu (điểm kích hoạt của save_volume_summary) thì hoàn thành, không còn yêu cầu
//     manh mối và tuyến dài cùng về 0 — nếu không thì những cuốn bị estimated_scale đánh giá quá cao sẽ không bao giờ hoàn thành được.
//
// đối chiếu với NoAutoCompleteWithOpenThreads bên dưới: cùng có tuyến dài chưa khép, chưa tuyên bố thì không hoàn thành, đã tuyên bố thì hoàn thành.
func TestCommitChapterFinaleVolumeCompletesDespiteOpenThreads(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	foundation := NewSaveFoundationTool(s)
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "Tập một", "theme": "Chủ đề",
			"arcs": []map[string]any{{
				"index": 1, "title": "Cung một", "goal": "Mục tiêu",
				"chapters": []map[string]any{{"title": "Chương đầu", "core_event": "Khởi", "hook": "Nối tiếp"}},
			}},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}

	// tuyên bố tập chốt hạt ở cuối tập: append_volume kèm final:true
	appendArgs, _ := json.Marshal(map[string]any{
		"type":   "append_volume",
		"reason": "Tuyến dài có thể khép trong một tập, tuyên bố tập chốt hạt",
		"content": map[string]any{
			"index": 2, "title": "Tập chốt hạt", "theme": "Khép lại", "final": true,
			"arcs": []map[string]any{{
				"index": 1, "title": "Cung chốt hạt", "goal": "Thu về mọi tuyến dài",
				"chapters": []map[string]any{{"title": "Chương cuối", "core_event": "Hợp", "hook": "Kết"}},
			}},
		},
	})
	raw, err := foundation.Execute(context.Background(), appendArgs)
	if err != nil {
		t.Fatalf("Execute append_volume: %v", err)
	}
	var appendOut map[string]any
	if err := json.Unmarshal(raw, &appendOut); err != nil {
		t.Fatalf("Unmarshal append result: %v", err)
	}
	if appendOut["final_volume"] != true {
		t.Fatalf("append_volume phải trả về sự thật final_volume=true, thực tế: %v", appendOut)
	}

	// tuyến dài chưa khép lại (khi chưa tuyên bố thì điều này chặn hoàn thành, xem test đối chiếu)
	if err := s.Outline.SaveCompass(domain.StoryCompass{EndingDirection: "Nhân vật chính trở về quê", OpenThreads: []string{"Kẻ thù chưa trừ"}}); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}
	_ = s.Progress.UpdatePhase(domain.PhaseWriting)

	tool := newTestCommitChapterTool(s)
	commit := func(ch int) map[string]any {
		if err := s.Drafts.SaveDraft(ch, fmt.Sprintf("Chương %d nội dung truyện, dùng để kiểm thử hoàn thành tập chốt hạt.", ch)); err != nil {
			t.Fatalf("SaveDraft %d: %v", ch, err)
		}
		args, _ := json.Marshal(map[string]any{
			"chapter": ch, "title": fmt.Sprintf("Chương %d", ch), "summary": "Tóm tắt", "characters": []string{"Nhân vật chính"}, "key_events": []string{"Sự kiện"},
		})
		raw, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute ch%d: %v", ch, err)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("Unmarshal ch%d: %v", ch, err)
		}
		return out
	}

	// Chương 1 (không phải chương cuối tập chốt hạt): không được hoàn thành
	if bc, _ := commit(1)["book_complete"].(bool); bc {
		t.Fatal("Tập chốt hạt chưa viết xong thì không được hoàn thành")
	}
	// tổng hợp của tập một phải hoàn tất trước, tóm tắt tập ở cuối tập hai mới là Mục tiêu hiện tại của Router.
	if err := s.World.SaveReview(domain.ReviewEntry{Chapter: 1, Scope: "arc", Verdict: "accept", Summary: "Đánh giá tập một"}); err != nil {
		t.Fatalf("SaveReview v1: %v", err)
	}
	if err := s.Summaries.SaveArcSummary(domain.ArcSummary{Volume: 1, Arc: 1, Title: "Cung một", Summary: "Hoàn tất", KeyEvents: []string{"Khởi"}}); err != nil {
		t.Fatalf("SaveArcSummary v1: %v", err)
	}
	if err := s.Summaries.SaveVolumeSummary(domain.VolumeSummary{Volume: 1, Title: "Tập một", Summary: "Hoàn tất", KeyEvents: []string{"Khởi"}}); err != nil {
		t.Fatalf("SaveVolumeSummary v1: %v", err)
	}
	// Chương 2 (chương cuối tập chốt hạt): bộ ba chốt cuối tập chưa đủ, việc hoàn thành không được vượt trước đánh giá của editor / tóm tắt
	if bc, _ := commit(2)["book_complete"].(bool); bc {
		t.Fatal("Khi commit chương cuối mà ba điều kiện chưa đủ thì không được hoàn thành")
	}
	if p, _ := s.Progress.Load(); p.Phase == domain.PhaseComplete {
		t.Fatal("Không được hoàn thành trước khi đánh giá cuối tập và tóm tắt")
	}

	// bộ ba chốt cuối tập: đánh giá cung + tóm tắt cung đã lưu, tóm tắt tập (save_volume_summary) là điểm kích hoạt hoàn thành
	if err := s.World.SaveReview(domain.ReviewEntry{Chapter: 2, Scope: "arc", Verdict: "accept", Summary: "Đánh giá cung cuối"}); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}
	if err := s.Summaries.SaveArcSummary(domain.ArcSummary{Volume: 2, Arc: 1, Title: "Cung chốt hạt", Summary: "Khép lại", KeyEvents: []string{"Kết cục"}}); err != nil {
		t.Fatalf("SaveArcSummary: %v", err)
	}
	volTool := NewSaveVolumeSummaryTool(s)
	volArgs, _ := json.Marshal(map[string]any{
		"volume": 2, "title": "Tập chốt hạt", "summary": "Khép lại toàn tập", "key_events": []string{"Kết cục"},
	})
	volRaw, err := volTool.Execute(context.Background(), volArgs)
	if err != nil {
		t.Fatalf("Execute save_volume_summary: %v", err)
	}
	var volOut map[string]any
	if err := json.Unmarshal(volRaw, &volOut); err != nil {
		t.Fatalf("Unmarshal volume summary result: %v", err)
	}
	if volOut["book_complete"] != true {
		t.Fatalf("Lưu tóm tắt tập phải kích hoạt hoàn thành chốt hạt và trả lại book_complete, thực tế: %v", volOut)
	}
	if p, _ := s.Progress.Load(); p.Phase != domain.PhaseComplete {
		t.Fatalf("expected phase=complete, got %s", p.Phase)
	}
}

// TestCommitChapterFinaleSkeletonArcBlocksCompletion xác minh chặn cấu trúc cho việc hoàn thành chốt hạt：
// khi tập chốt hạt vẫn còn cung khung (phần quy hoạch chưa viết) thì dù bộ ba đủ cũng không được hoàn thành — đây là cách ngăn
// "hoàn thành quá sớm" (điều kiện 2 của layeredStructurallyComplete).
func TestCommitChapterFinaleSkeletonArcBlocksCompletion(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	foundation := NewSaveFoundationTool(s)
	// tập chốt hạt: cung một khai triển 1 chương, cung hai vẫn là khung
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "Tập chốt hạt", "theme": "Khép lại", "final": true,
			"arcs": []map[string]any{
				{"index": 1, "title": "Cung chốt hạt", "goal": "Gom mối",
					"chapters": []map[string]any{{"title": "Chương đầu", "core_event": "Khởi", "hook": "Nối tiếp"}}},
				{"index": 2, "title": "Cung khung", "goal": "Chờ khai triển", "estimated_chapters": 5},
			},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}
	if err := s.Outline.SaveCompass(domain.StoryCompass{EndingDirection: "Trở về quê"}); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}
	_ = s.Progress.UpdatePhase(domain.PhaseWriting)

	tool := newTestCommitChapterTool(s)
	if err := s.Drafts.SaveDraft(1, "Nội dung truyện chương một."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	args, _ := json.Marshal(map[string]any{
		"chapter": 1, "title": "Chương một", "summary": "Tóm tắt", "characters": []string{"Nhân vật chính"}, "key_events": []string{"Sự kiện"},
	})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// bộ ba đủ cũng không được cho qua: cung khung nghĩa là phần quy hoạch chưa viết
	if err := s.World.SaveReview(domain.ReviewEntry{Chapter: 1, Scope: "arc", Verdict: "accept", Summary: "Đánh giá cung"}); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}
	if err := s.Summaries.SaveArcSummary(domain.ArcSummary{Volume: 1, Arc: 1, Title: "Cung chốt hạt", Summary: "s", KeyEvents: []string{"e"}}); err != nil {
		t.Fatalf("SaveArcSummary: %v", err)
	}
	volTool := NewSaveVolumeSummaryTool(s)
	volArgs, _ := json.Marshal(map[string]any{
		"volume": 1, "title": "Tập chốt hạt", "summary": "s", "key_events": []string{"e"},
	})
	if _, err := volTool.Execute(context.Background(), volArgs); err == nil || !strings.Contains(err.Error(), "hiện không có hạng mục") {
		t.Fatalf("khi cung khung chưa khai triển thì tập chưa kết thúc, tóm tắt tập phải bị từ chối，got %v", err)
	}
	if p, _ := s.Progress.Load(); p.Phase == domain.PhaseComplete {
		t.Fatal("cung khung chưa khai triển, phase không được là complete")
	}
}

// TestCommitChapterLayeredNoAutoCompleteWithOpenThreads xác minh tính thận trọng: khi vẫn còn tuyến dài hoạt động thì
// dù đã viết đủ chương cũng không tự động hoàn thành, giữ quyền quyết "có tiếp tục hay không" cho kiến trúc sư.

// TestCommitChapterLayeredNoAutoCompleteWithOpenThreads xác minh tính thận trọng: khi vẫn còn tuyến dài hoạt động thì
// dù đã viết đủ chương cũng không tự động hoàn thành, giữ quyền quyết "có tiếp tục hay không" cho kiến trúc sư.
func TestCommitChapterLayeredNoAutoCompleteWithOpenThreads(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	foundation := NewSaveFoundationTool(s)
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "Tập một", "theme": "Chủ đề",
			"arcs": []map[string]any{{
				"index": 1, "title": "Cung một", "goal": "Mục tiêu",
				"chapters": []map[string]any{{"title": "Chương đầu", "core_event": "Khởi", "hook": "Nối tiếp"}},
			}},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}
	// vẫn còn tuyến dài hoạt động chưa khép lại
	if err := s.Outline.SaveCompass(domain.StoryCompass{EndingDirection: "Nhân vật chính trở về quê", OpenThreads: []string{"Kẻ thù chưa trừ"}}); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}
	_ = s.Progress.UpdatePhase(domain.PhaseWriting)

	if err := s.Drafts.SaveDraft(1, "Nội dung truyện của chương duy nhất, nhưng tuyến dài chưa khép lại."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	tool := newTestCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter": 1, "title": "Chương một", "summary": "Tóm tắt", "characters": []string{"Nhân vật chính"}, "key_events": []string{"Sự kiện"},
	})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if p, _ := s.Progress.Load(); p.Phase == domain.PhaseComplete {
		t.Fatal("khi tuyến dài hoạt động chưa khép lạithì không được tự động hoàn thành")
	}
}
