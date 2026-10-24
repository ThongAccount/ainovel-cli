package agents

// Kiểm thử đầu-cuối hành vi kết hợp của dừng cứng save_review và StopGuard
// có nhận biết nhiệm vụ (đúng cách nối thật trong cấu hình editor ở build.go:
// StopAfterToolResult chạm save_review/save_*_summary, StopGuardFactory dùng
// guard thật, công cụ ghi checkpoint thật).
//
// Kịch bản một (nhiệm vụ tóm tắt nhưng soát duyệt trước): editor được phân
// công sinh tóm tắt cung nhưng lại gọi save_review — dừng cứng có hiệu lực
// nhưng guard bác, sau khi chèn thông điệp thúc thì editor mới đi tới
// save_arc_summary rồi mới thoát thật.
// Đây là tiền đề an toàn cho việc khôi phục dừng cứng save_review, chặn vòng
// lặp vô hạn khi tóm tắt cung không bao giờ được ghi xuống đĩa.
//
// Kịch bản hai (nhiệm vụ biên tập chốt sau một bước): editor được phân công
// biên tập, save_review ghi xuống đĩa là dừng cứng cho qua, không chạy thêm
// một lượt LLM để chốt.

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/subagent"
	"github.com/voocel/ainovel-cli/internal/agents/guard"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// editorStopAfterToolResult giữ cùng tiêu chí với cấu hình editor trong
// build.go.
func editorStopAfterToolResult(toolName string, _ json.RawMessage) bool {
	return toolName == "save_review" || toolName == "save_arc_summary" || toolName == "save_volume_summary"
}

func checkpointTool(t *testing.T, st *store.Store, name, step string) agentcore.Tool {
	t.Helper()
	return agentcore.NewFuncTool(name, "fake "+name, map[string]any{"type": "object"},
		func(context.Context, json.RawMessage) (json.RawMessage, error) {
			if _, err := st.Checkpoints.Append(domain.ArcScope(1, 1), step, "artifact", "digest"); err != nil {
				t.Fatalf("ghi checkpoint %s: %v", step, err)
			}
			return json.RawMessage(`"saved"`), nil
		})
}

func runEditorLike(t *testing.T, st *store.Store, task string, model agentcore.ChatModel, tools []agentcore.Tool) {
	t.Helper()
	cfg := subagent.Config{
		Name:                "editor",
		Description:         "test editor",
		Model:               model,
		SystemPrompt:        "test",
		Tools:               tools,
		MaxTurns:            10,
		StopAfterToolResult: editorStopAfterToolResult,
		StopGuardFactory: func(_, task string) agentcore.StopGuard {
			return guard.NewEditorStopGuard(st, task, nil)
		},
	}
	tool := subagent.NewRunner(cfg).AsTool()
	args, _ := json.Marshal(map[string]string{"agent": "editor", "task": task})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("chạy subagent: %v", err)
	}
}

func TestEditorFlow_SummaryTaskSurvivesEarlyReview(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	var calls atomic.Int32
	model := &contractModel{fn: func(i int, _ []agentcore.Message) (*agentcore.LLMResponse, error) {
		switch i {
		case 0:
			// Lệch hướng: nhiệm vụ sinh tóm tắt nhưng lại soát duyệt trước.
			return &agentcore.LLMResponse{Message: assistantToolCall("save_review", `{}`)}, nil
		default:
			// Guard bác lần dừng cứng và chèn lời thúc, lượt này mới ra tóm tắt.
			calls.Add(1)
			return &agentcore.LLMResponse{Message: assistantToolCall("save_arc_summary", `{}`)}, nil
		}
	}}

	runEditorLike(t, st, "Sinh tóm tắt cung 1 của tập 1 (save_arc_summary)", model, []agentcore.Tool{
		checkpointTool(t, st, "save_review", "review"),
		checkpointTool(t, st, "save_arc_summary", "arc_summary"),
	})

	if calls.Load() == 0 {
		t.Fatal("sau khi guard bác lần dừng cứng save_review, editor phải đi tiếp tới save_arc_summary — nếu run kết thúc ngay sau khi soát duyệt thì lần thoát trạng thái cuối đã đi vòng qua guard và vòng lặp vô hạn tóm tắt cung sẽ quay lại")
	}
	all := st.Checkpoints.All()
	var hasSummary bool
	for _, cp := range all {
		if cp.Step == "arc_summary" {
			hasSummary = true
		}
	}
	if !hasSummary {
		t.Fatal("tóm tắt cung phải cuối cùng được ghi xuống đĩa")
	}
}

func TestEditorFlow_ReviewTaskStopsAtSaveReview(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	model := &contractModel{fn: func(i int, _ []agentcore.Message) (*agentcore.LLMResponse, error) {
		if i == 0 {
			return &agentcore.LLMResponse{Message: assistantToolCall("save_review", `{}`)}, nil
		}
		t.Fatal("nhiệm vụ biên tập phải dừng cứng sau khi save_review ghi xuống đĩa, model không được có thêm lượt")
		return nil, nil
	}}

	runEditorLike(t, st, "Biên tập cấp cung 1 của tập 1 (scope=arc)", model, []agentcore.Tool{
		checkpointTool(t, st, "save_review", "review"),
	})

	if got := model.calls(); got != 1 {
		t.Fatalf("nhiệm vụ biên tập phải chốt sau đúng một lượt gọi model, got %d", got)
	}
}
