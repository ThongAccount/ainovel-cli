package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

func TestDraftChapterRejectsUnfinishedPendingRewrite(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(80); err != nil {
		t.Fatalf("Progress.Init: %v", err)
	}
	for ch := 1; ch <= 58; ch++ {
		if err := s.Progress.MarkChapterComplete(ch, 3000, "", ""); err != nil {
			t.Fatalf("MarkChapterComplete(%d): %v", ch, err)
		}
	}

	p, _ := s.Progress.Load()
	p.Flow = domain.FlowPolishing
	p.PendingRewrites = []int{65}
	if err := s.Progress.Save(p); err != nil {
		t.Fatalf("Save corrupt progress: %v", err)
	}

	tool := NewDraftChapterTool(s)
	args, err := json.Marshal(map[string]any{
		"chapter": 65,
		"content": "Ghi nhầm vào chương tương lai.",
		"mode":    "write",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := tool.Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "pending_rewrites chỉ được chứa chương đã hoàn thành") {
		t.Fatalf("expected invalid pending_rewrites rejection, got %v", err)
	}
	progress, _ := s.Progress.Load()
	if progress.InProgressChapter == 65 {
		t.Fatalf("chương tương lai không được trở thành chương đang viết")
	}
}

func TestDraftChapterRejectsUnexpandedLayeredChapter(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(5); err != nil {
		t.Fatalf("Progress.Init: %v", err)
	}
	if err := s.Outline.SaveLayeredOutline([]domain.VolumeOutline{{
		Index: 1,
		Title: "Tập một",
		Arcs: []domain.ArcOutline{{
			Index: 1,
			Title: "Cung một",
			Chapters: []domain.OutlineEntry{
				{Chapter: 1, Title: "Một"},
				{Chapter: 2, Title: "Hai"},
			},
		}, {
			Index:             2,
			Title:             "Cung hai",
			EstimatedChapters: 3,
		}},
	}}); err != nil {
		t.Fatalf("SaveLayeredOutline: %v", err)
	}
	if err := s.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}
	if err := s.Progress.SetLayered(true); err != nil {
		t.Fatalf("SetLayered: %v", err)
	}

	tool := NewDraftChapterTool(s)
	args, err := json.Marshal(map[string]any{
		"chapter": 3,
		"content": "Nội dung vượt phạm vi.",
		"mode":    "write",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := tool.Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "expand_arc") {
		t.Fatalf("expected unexpanded chapter rejection, got %v", err)
	}
	progress, _ := s.Progress.Load()
	if progress.InProgressChapter == 3 {
		t.Fatalf("chương chưa khai triển không được trở thành chương đang viết")
	}
}

// TestDraftChapterTextIsLanguageDependent kiểm tra nhánh ngôn ngữ của văn bản
// mô hình đọc: Description, Label, mô tả tham số schema và next_step trong kết
// quả phải đổi theo ngôn ngữ tác phẩm.
func TestDraftChapterTextIsLanguageDependent(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(5); err != nil {
		t.Fatalf("Progress.Init: %v", err)
	}
	if err := s.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}

	tool := NewDraftChapterTool(s)

	s.SetLanguage("vi")
	viDesc := tool.Description()
	viLabel := tool.Label()
	viSchema := tool.Schema()
	viNext := tool.texts().draftChapterNextStep

	s.SetLanguage("zh")
	zhDesc := tool.Description()
	zhLabel := tool.Label()
	zhSchema := tool.Schema()
	zhNext := tool.texts().draftChapterNextStep

	if viDesc == zhDesc {
		t.Fatalf("Description phải khác nhau giữa vi và zh: %q", viDesc)
	}
	if viLabel == zhLabel {
		t.Fatalf("Label phải khác nhau giữa vi và zh: %q", viLabel)
	}
	if viNext == zhNext {
		t.Fatalf("next_step phải khác nhau giữa vi và zh: %q", viNext)
	}
	if !strings.Contains(viDesc, "bản nháp") {
		t.Fatalf("mô tả tiếng Việt phải nói về bản nháp, thực tế: %q", viDesc)
	}
	if !strings.Contains(zhDesc, "草稿") {
		t.Fatalf("mô tả tiếng Trung phải nói về 草稿, thực tế: %q", zhDesc)
	}

	viProp := viSchema["properties"].(map[string]any)["chapter"].(map[string]any)["description"].(string)
	zhProp := zhSchema["properties"].(map[string]any)["chapter"].(map[string]any)["description"].(string)
	if viProp == zhProp {
		t.Fatalf("mô tả tham số schema phải khác nhau giữa vi và zh: %q", viProp)
	}
	if !strings.Contains(strings.ToLower(viProp), "chương") {
		t.Fatalf("mô tả tham số tiếng Việt phải nói về chương, thực tế: %q", viProp)
	}
	if !strings.Contains(zhProp, "章") {
		t.Fatalf("mô tả tham số tiếng Trung phải nói về 章, thực tế: %q", zhProp)
	}
}

// TestDraftChapterResultCarriesNextStep kiểm tra rằng kết quả thực thi chứa
// next_step đúng theo ngôn ngữ tác phẩm.
func TestDraftChapterResultCarriesNextStep(t *testing.T) {
	for _, tc := range []struct {
		lang string
		want string
	}{
		{lang: "vi", want: "read_chapter"},
		{lang: "zh", want: "read_chapter"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			s := store.NewStore(t.TempDir())
			if err := s.Init(); err != nil {
				t.Fatalf("Init: %v", err)
			}
			if err := s.Progress.Init(5); err != nil {
				t.Fatalf("Progress.Init: %v", err)
			}
			if err := s.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
				t.Fatalf("UpdatePhase: %v", err)
			}
			s.SetLanguage(tc.lang)

			tool := NewDraftChapterTool(s)
			args, err := json.Marshal(map[string]any{
				"chapter": 1,
				"content": "Nội dung chương một.",
				"mode":    "write",
			})
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			out, err := tool.Execute(context.Background(), args)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			var result map[string]any
			if err := json.Unmarshal(out, &result); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			next, ok := result["next_step"].(string)
			if !ok || next == "" {
				t.Fatalf("kết quả phải có next_step, thực tế: %v", result)
			}
			if !strings.Contains(next, tc.want) {
				t.Fatalf("next_step phải nhắc tới %q, thực tế: %q", tc.want, next)
			}
			if next != tool.texts().draftChapterNextStep {
				t.Fatalf("next_step phải khớp bảng văn bản của ngôn ngữ hiện tại: %q", next)
			}
		})
	}
}
