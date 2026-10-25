package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

func planArgs(chapter int) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"chapter":     chapter,
		"title":       "Chương thử nghiệm",
		"goal":        "Đẩy tiến cốt truyện",
		"conflict":    "Trở lực bên ngoài",
		"hook":        "Để lại tình tiết hấp dẫn",
		"emotion_arc": "Từ căng thẳng đến mong đợi",
	})
	return b
}

func TestPlanChapterRejectsUnexpandedLayeredChapter(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := st.Progress.Init(5); err != nil {
		t.Fatalf("Progress.Init: %v", err)
	}
	if err := st.Outline.SaveLayeredOutline([]domain.VolumeOutline{{
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
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}
	if err := st.Progress.SetLayered(true); err != nil {
		t.Fatalf("SetLayered: %v", err)
	}

	tool := NewPlanChapterTool(st)
	if _, err := tool.Execute(context.Background(), planArgs(3)); err == nil || !strings.Contains(err.Error(), "expand_arc") {
		t.Fatalf("expected unexpanded chapter rejection, got %v", err)
	}
	if p, _ := st.Progress.Load(); p != nil && p.InProgressChapter == 3 {
		t.Fatal("chương chưa khai triển không được trở thành chương đang viết")
	}
}

func TestPlanChapterAllowsExpandedLayeredChapter(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := st.Progress.Init(2); err != nil {
		t.Fatalf("Progress.Init: %v", err)
	}
	if err := st.Outline.SaveLayeredOutline([]domain.VolumeOutline{{
		Index: 1,
		Title: "Tập một",
		Arcs: []domain.ArcOutline{{
			Index: 1,
			Title: "Cung một",
			Chapters: []domain.OutlineEntry{
				{Chapter: 1, Title: "Một"},
				{Chapter: 2, Title: "Hai"},
			},
		}},
	}}); err != nil {
		t.Fatalf("SaveLayeredOutline: %v", err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}
	if err := st.Progress.SetLayered(true); err != nil {
		t.Fatalf("SetLayered: %v", err)
	}

	tool := NewPlanChapterTool(st)
	if _, err := tool.Execute(context.Background(), planArgs(2)); err != nil {
		t.Fatalf("expected expanded chapter to plan, got %v", err)
	}
}

// Chương đã viết xong nhưng đang nằm trong hàng chờ viết lại phải lập kế hoạch được:
// đây là chỗ duy nhất ghi được "viết lại thành cái gì". Trước đây nó bị từ chối, khiến
// revise_outline / save_foundation(outline) / plan_chapter bịt cả ba đường, và kiến trúc
// sư quay vòng 4 lượt rồi bị ngắt mạch.
func TestPlanChapterAllowsChapterQueuedForRewrite(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := st.Progress.Init(3); err != nil {
		t.Fatalf("Progress.Init: %v", err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}
	if err := st.Progress.StartChapter(1); err != nil {
		t.Fatalf("StartChapter: %v", err)
	}
	if err := st.Progress.MarkChapterComplete(1, 1000, "mystery", "quest"); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := st.Progress.SetPendingRewrites([]int{1}, "đoạn mở đầu trùng chương trước"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}

	raw, err := NewPlanChapterTool(st).Execute(context.Background(), planArgs(1))
	if err != nil {
		t.Fatalf("chương trong hàng viết lại phải lập kế hoạch được, nhận: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if skipped, _ := got["skipped"].(bool); skipped {
		t.Fatalf("không được bỏ qua, nhận: %v", got)
	}
	if plan, err := st.Drafts.LoadChapterPlan(1); err != nil || plan == nil {
		t.Fatalf("kế hoạch phải được ghi xuống, err=%v plan=%v", err, plan)
	}
}

// Chương đã xong và KHÔNG chờ viết lại thì vẫn phải bị chặn — hành vi cũ giữ nguyên.
func TestPlanChapterStillSkipsSettledCompletedChapter(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := st.Progress.Init(3); err != nil {
		t.Fatalf("Progress.Init: %v", err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}
	if err := st.Progress.StartChapter(1); err != nil {
		t.Fatalf("StartChapter: %v", err)
	}
	if err := st.Progress.MarkChapterComplete(1, 1000, "mystery", "quest"); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}

	raw, err := NewPlanChapterTool(st).Execute(context.Background(), planArgs(1))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if skipped, _ := got["skipped"].(bool); !skipped {
		t.Fatalf("chương đã chốt phải bị bỏ qua, nhận: %v", got)
	}
}
