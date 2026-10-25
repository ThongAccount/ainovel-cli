package tools

import (
	"context"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
	"github.com/voocel/ainovel-cli/internal/store"
)

func TestResolveOutlineFeedbackClearsReviewedFeedback(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Outline.AppendOutlineFeedback(store.ChapterFeedback{Chapter: 1, StoryChanged: true, ChangeSummary: "Nhân vật chính rời thành sớm"}); err != nil {
		t.Fatal(err)
	}
	tool := NewResolveOutlineFeedbackTool(st)
	if err := llmcontract.ValidateStrictReady(tool.Schema()); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), []byte(`{"reason":"Kế hoạch sau không phụ thuộc vào thành phố của nhân vật chính"}`)); err != nil {
		t.Fatal(err)
	}
	feedback, err := st.Outline.LoadPendingOutlineFeedback()
	if err != nil || len(feedback) != 0 {
		t.Fatalf("feedback=%+v err=%v", feedback, err)
	}
	if checkpoint := st.Checkpoints.LatestByStep(domain.GlobalScope(), "resolve_outline_feedback"); checkpoint == nil {
		t.Fatal("缺少反馈处理 checkpoint")
	}
}
