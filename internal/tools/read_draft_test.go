package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

func TestReadChapterFinal(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.Drafts.SaveFinalChapter(1, "Nội dung truyện bản chính của chương một."); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}

	tool := NewReadChapterTool(store)
	args, _ := json.Marshal(map[string]any{"chapter": 1, "source": "final"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var payload struct {
		Chapter   int    `json:"chapter"`
		Content   string `json:"content"`
		WordCount int    `json:"word_count"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload.Content == "" {
		t.Fatal("cần nội dung không rỗng")
	}
	if payload.WordCount == 0 {
		t.Fatal("cần số từ khác 0")
	}
}

func TestReadChapterDraft(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.Drafts.SaveDraft(3, "Nội dung bản nháp của chương ba."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	tool := NewReadChapterTool(store)
	args, _ := json.Marshal(map[string]any{"chapter": 3, "source": "draft"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload.Content == "" {
		t.Fatal("cần có nội dung bản nháp")
	}
}

func TestReadChapterFinalDoesNotSubstituteDraft(t *testing.T) {
	dir := t.TempDir()
	st := store.NewStore(dir)
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Drafts.SaveDraft(2, "Chỉ có bản nháp, không phải bản chính"); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]any{"chapter": 2, "source": "final"})
	raw, err := NewReadChapterTool(st).Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Exists  bool   `json:"exists"`
		Source  string `json:"source"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Exists || got.Source != "final" || got.Content != "" {
		t.Fatalf("yêu cầu final không được âm thầm thay bằng draft: %+v", got)
	}
}

func TestReadChapterDialogue(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.Characters.Save([]domain.Character{
		{Name: "Trương Tam", Aliases: []string{"Lão Trương"}},
	}); err != nil {
		t.Fatalf("SaveCharacters: %v", err)
	}
	if err := store.Drafts.SaveFinalChapter(1, "Trương Tam đứng dậy. \u201cTôi không đồng ý với phương án này,\u201d Trương Tam lạnh lùng nói."); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}

	tool := NewReadChapterTool(store)
	args, _ := json.Marshal(map[string]any{"source": "final", "character": "Trương Tam"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var payload struct {
		Character string   `json:"character"`
		Samples   []string `json:"samples"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload.Character != "Trương Tam" {
		t.Fatalf("cần nhân vật Trương Tam, thực tế: %s", payload.Character)
	}
}

func TestReadChapterRange(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for i := 1; i <= 3; i++ {
		if err := store.Drafts.SaveFinalChapter(i, "Đây là một đoạn nội dung truyện."); err != nil {
			t.Fatalf("SaveFinalChapter(%d): %v", i, err)
		}
	}

	tool := NewReadChapterTool(store)
	args, _ := json.Marshal(map[string]any{"from": 1, "to": 3, "source": "final"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var payload struct {
		Chapters map[string]string `json:"chapters"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(payload.Chapters) != 3 {
		t.Fatalf("cần 3 chương, thực tế: %d", len(payload.Chapters))
	}
}

func TestDraftChapterWrite(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := store.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}

	tool := NewDraftChapterTool(store)
	args, _ := json.Marshal(map[string]any{
		"chapter": 1,
		"content": "Đây là toàn bộ nội dung truyện của chương, viết một lạt.",
		"mode":    "write",
	})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var payload struct {
		Written   bool `json:"written"`
		WordCount int  `json:"word_count"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !payload.Written {
		t.Fatal("cần written=true")
	}
	if payload.WordCount == 0 {
		t.Fatal("cần số từ khác 0")
	}

	// Kiểm tra đọc lại được
	content, err := store.Drafts.LoadDraft(1)
	if err != nil {
		t.Fatalf("LoadDraft: %v", err)
	}
	if content == "" {
		t.Fatal("cần bản nháp không rỗng")
	}
	progress, err := store.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if progress.InProgressChapter != 1 {
		t.Fatalf("cần chương 1 đang viết, thực tế: %d", progress.InProgressChapter)
	}
	if progress.Phase != domain.PhaseWriting {
		t.Fatalf("cần phase writing, thực tế: %s", progress.Phase)
	}
}

func TestDraftChapterAppend(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := store.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}
	if err := store.Drafts.SaveDraft(2, "Phần trước."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	tool := NewDraftChapterTool(store)
	args, _ := json.Marshal(map[string]any{
		"chapter": 2,
		"content": "Phần sau.",
		"mode":    "append",
	})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var payload struct {
		Mode      string `json:"mode"`
		WordCount int    `json:"word_count"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload.Mode != "append" {
		t.Fatalf("cần mode=append, thực tế: %s", payload.Mode)
	}

	content, _ := store.Drafts.LoadDraft(2)
	if content == "" || content == "Phần trước." {
		t.Fatal("cần nội dung đã được nối thêm")
	}
}

func TestReadChapterMissingReturnsJSON(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	tool := NewReadChapterTool(store)
	args, _ := json.Marshal(map[string]any{"chapter": 1, "source": "final"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("không được lỗi khi thiếu chương, thực tế: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if parsed["exists"] != false {
		t.Fatal("cần exists=false")
	}
}

func TestPlanChapterMarksInProgress(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := store.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}

	tool := NewPlanChapterTool(store)
	args, _ := json.Marshal(map[string]any{
		"chapter":  1,
		"title":    "Mở đầu",
		"goal":     "Xác lập hoàn cảnh",
		"conflict": "Nợ bên cạnh",
		"hook":     "Phát hiện manh mối",
	})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	progress, err := store.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if progress.Phase != domain.PhaseWriting {
		t.Fatalf("cần phase writing, thực tế: %s", progress.Phase)
	}
	if progress.InProgressChapter != 1 {
		t.Fatalf("cần chương 1 đang viết, thực tế: %d", progress.InProgressChapter)
	}
}

func TestDraftChapterRejectsCompleted(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	_ = s.Drafts.SaveDraft(1, "Nội dung truyện chương một")
	_ = s.Progress.StartChapter(1)
	_ = s.Progress.MarkChapterComplete(1, 3000, "", "")

	tool := NewDraftChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter": 1,
		"content": "Cố ghi đè chương đã nộp",
	})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("cần từ chối mềm, thực tế là lỗi: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload["skipped"] != true {
		t.Fatalf("cần skipped=true, thực tế: %+v", payload)
	}
}
