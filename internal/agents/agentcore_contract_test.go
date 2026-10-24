package agents

// Hợp đồng kiểm thử agentcore: đóng băng hành vi khung mà dự án này dựa vào
// thành các khẳng định chạy được.
// Mỗi kiểm thử đều ghi rõ bên phụ thuộc; phải xanh hết trước khi nâng cấp
// agentcore — comment thì lỗi thời, còn kiểm thử thì không.
// Tất cả đều chạy qua subagent.Runner.Run — đó là kênh phân công thật của
// Engine.
//
// Các hợp đồng đã đóng băng:
//  1. Lần thoát trạng thái cuối qua StopAfterTools/StopAfterToolResult sẽ đi
//     qua StopGuard (StopTriggerAfterTool), guard bác (InjectMessage) kéo được
//     run quay lại tiếp tục — EditorStopGuard có nhận biết nhiệm vụ trong
//     guard/subagent_guards.go dựa vào hành vi này để bắt lần thoát sớm kiểu
//     "được phân công sinh tóm tắt nhưng chỉ làm soát duyệt".
//  2. StopReasonError / StopReasonAborted dừng run trực tiếp, không chạm
//     StopGuard — vì vậy hardStopReasons trong guard/subagent_guards.go chỉ
//     cần liệt kê safety/content_filter.
//  3. Provider từ chối (safety và các dừng không phải error) đi tới StopGuard
//     qua đường end_turn, và info.Message.StopReason giữ nguyên giá trị —
//     nâng cấp ngay của hardStopReasons phụ thuộc đúng đường này.
//  4. StopGuard trả InjectMessage thì model có được một lượt mới; trả
//     Escalate thì dừng ngay, và chuỗi lỗi so khớp được bằng
//     errors.Is(err, agentcore.ErrStopGuard) — ngữ nghĩa "không thể dừng vật
//     lý" và nâng cấp quá ngưỡng trong guard/stop_guard.go phụ thuộc điều này.
//  5. Lỗi của Runner.Run giữ chuỗi kiểu: agent chưa đăng ký khớp
//     subagent.ErrUnknownAgent — isDeterministicWorkerError trong
//     host/engine.go dựa vào phân loại này chứ không dựa vào văn bản lỗi.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/subagent"
)

// contractModel là mô hình giả trả về đáp án định sẵn theo thứ tự gọi.
type contractModel struct {
	fn  func(i int, msgs []agentcore.Message) (*agentcore.LLMResponse, error)
	idx int64
}

func (m *contractModel) take(msgs []agentcore.Message) (*agentcore.LLMResponse, error) {
	i := int(atomic.AddInt64(&m.idx, 1) - 1)
	return m.fn(i, msgs)
}

func (m *contractModel) calls() int { return int(atomic.LoadInt64(&m.idx)) }

func (m *contractModel) Generate(_ context.Context, msgs []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	return m.take(msgs)
}

func (m *contractModel) GenerateStream(_ context.Context, msgs []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	resp, err := m.take(msgs)
	if err != nil {
		return nil, err
	}
	ch := make(chan agentcore.StreamEvent, 1)
	ch <- agentcore.StreamEvent{Type: agentcore.StreamEventDone, Message: resp.Message, StopReason: resp.Message.StopReason}
	close(ch)
	return ch, nil
}

func (m *contractModel) SupportsTools() bool { return true }

func assistantText(text string, stop agentcore.StopReason) agentcore.Message {
	return agentcore.Message{
		Role:       agentcore.RoleAssistant,
		Content:    []agentcore.ContentBlock{agentcore.TextBlock(text)},
		StopReason: stop,
	}
}

func assistantToolCall(name string, args string) agentcore.Message {
	return agentcore.Message{
		Role: agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.ToolCallBlock(agentcore.ToolCall{
			ID: "tc-" + name, Name: name, Args: json.RawMessage(args),
		})},
		StopReason: agentcore.StopReasonToolUse,
	}
}

func okTool(name string) agentcore.Tool {
	return agentcore.NewFuncTool(name, "contract test tool", map[string]any{"type": "object"},
		func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`"ok"`), nil
		})
}

// runSubagent chạy một lượt phân công qua Runner.Run (kênh phân công của
// Engine) với cấu hình cho trước.
// Trả về lỗi thực thi — lần nâng cấp của StopGuard sẽ nổi lên dưới dạng
// error (điều này cũng là hợp đồng); các ca kỳ vọng kết thúc bình thường tự
// khẳng định nil.
func runSubagent(t *testing.T, cfg subagent.Config) error {
	t.Helper()
	_, err := subagent.NewRunner(cfg).Run(context.Background(), cfg.Name, "contract")
	return err
}

// Hợp đồng 1: lần thoát bằng công cụ trạng thái cuối đi qua StopGuard; guard bác
// (InjectMessage) thì run tiếp tục.
// Bên phụ thuộc: EditorStopGuard — khi các công cụ trạng thái cuối như
// save_review được chạm tới, guard có nhận biết nhiệm vụ phải có cơ hội kéo
// lại lần thoát sớm kiểu "sản phẩm chưa ghi xuống đĩa".
func TestContract_TerminalToolExitConsultsStopGuard(t *testing.T) {
	var guardCalls atomic.Int32
	var trigger atomic.Value

	model := &contractModel{fn: func(i int, _ []agentcore.Message) (*agentcore.LLMResponse, error) {
		switch i {
		case 0:
			return &agentcore.LLMResponse{Message: assistantToolCall("finish", `{}`)}, nil
		default:
			// Sau khi guard bác lần thoát trạng thái cuối, model phải có được
			// một lượt mới; lượt này kết thúc bình thường.
			return &agentcore.LLMResponse{Message: assistantText("done", agentcore.StopReasonStop)}, nil
		}
	}}

	if err := runSubagent(t, subagent.Config{
		Name:           "editorish",
		Description:    "contract",
		Model:          model,
		SystemPrompt:   "test",
		Tools:          []agentcore.Tool{okTool("finish")},
		MaxTurns:       5,
		StopAfterTools: []string{"finish"},
		StopGuardFactory: func(_, _ string) agentcore.StopGuard {
			return func(_ context.Context, info agentcore.StopInfo) agentcore.StopDecision {
				n := guardCalls.Add(1)
				if n == 1 {
					trigger.Store(info.Trigger)
					return agentcore.StopDecision{Allow: false, InjectMessage: "chưa ghi xuống đĩa, tiếp tục"}
				}
				return agentcore.StopDecision{Allow: true}
			}
		},
	}); err != nil {
		t.Fatalf("chạy subagent: %v", err)
	}

	if guardCalls.Load() < 2 {
		t.Fatalf("lần thoát bằng công cụ trạng thái cuối phải chạm StopGuard và tiếp tục sau khi bị bác (kỳ vọng ≥2 lần hỏi), got %d", guardCalls.Load())
	}
	if got := trigger.Load(); got != agentcore.StopTriggerAfterTool {
		t.Fatalf("Trigger của lần thoát trạng thái cuối phải là StopTriggerAfterTool, got %v", got)
	}
	if model.calls() < 2 {
		t.Fatalf("sau khi guard bác, model phải có một lượt mới, got %d calls", model.calls())
	}
}

// Hợp đồng 2: StopReasonError / StopReasonAborted dừng trực tiếp, không chạm
// StopGuard.
// Bên phụ thuộc: chú thích hardStopReasons — chỉ cần xử lý ngữ nghĩa từ chối
// thật sự đi tới guard.
func TestContract_ErrorAndAbortedStopSkipStopGuard(t *testing.T) {
	for _, stop := range []agentcore.StopReason{agentcore.StopReasonError, agentcore.StopReasonAborted} {
		t.Run(string(stop), func(t *testing.T) {
			var guardCalls atomic.Int32
			model := &contractModel{fn: func(int, []agentcore.Message) (*agentcore.LLMResponse, error) {
				return &agentcore.LLMResponse{Message: assistantText("dead", stop)}, nil
			}}
			_ = runSubagent(t, subagent.Config{
				Name: "dying", Description: "contract", Model: model,
				SystemPrompt: "test", MaxTurns: 5,
				StopGuardFactory: func(_, _ string) agentcore.StopGuard {
					return func(context.Context, agentcore.StopInfo) agentcore.StopDecision {
						guardCalls.Add(1)
						return agentcore.StopDecision{Allow: true}
					}
				},
			}) // Ngữ nghĩa lỗi của lần dừng error/aborted do lớp subagent định nghĩa, ở đây chỉ quan tâm guard có được chạm tới hay không
			if guardCalls.Load() != 0 {
				t.Fatalf("lần dừng %s không được chạm StopGuard, got %d lần hỏi", stop, guardCalls.Load())
			}
		})
	}
}

// Hợp đồng 3: provider từ chối (safety…) đi tới StopGuard qua đường end_turn, và
// info.Message.StopReason giữ nguyên giá trị.
// Bên phụ thuộc: nâng cấp ngay của hardStopReasons.
func TestContract_SafetyStopReachesStopGuardWithReason(t *testing.T) {
	var seen atomic.Value
	model := &contractModel{fn: func(int, []agentcore.Message) (*agentcore.LLMResponse, error) {
		return &agentcore.LLMResponse{Message: assistantText("refused", agentcore.StopReason("safety"))}, nil
	}}
	err := runSubagent(t, subagent.Config{
		Name: "refused", Description: "contract", Model: model,
		SystemPrompt: "test", MaxTurns: 5,
		StopGuardFactory: func(_, _ string) agentcore.StopGuard {
			return func(_ context.Context, info agentcore.StopInfo) agentcore.StopDecision {
				seen.Store(info.Message.StopReason)
				return agentcore.StopDecision{Allow: false, Escalate: true}
			}
		},
	})
	if got := seen.Load(); got != agentcore.StopReason("safety") {
		t.Fatalf("StopGuard phải thấy stop reason safety gốc, got %v", got)
	}
	if !errors.Is(err, agentcore.ErrStopGuard) {
		t.Fatalf("Escalate phải nổi lên dưới dạng lỗi so khớp được bằng errors.Is(agentcore.ErrStopGuard), got %v", err)
	}
}

// Hợp đồng 4: khi end_turn, InjectMessage cho model một lượt mới và nội dung
// chèn phải có mặt;
// Escalate dừng ngay, model không được gọi nữa. Bên phụ thuộc: StopGuard của
// Worker với ngữ nghĩa "không thể dừng vật lý + nâng cấp quá ngưỡng".
func TestContract_StopGuardInjectContinuesEscalateTerminates(t *testing.T) {
	var sawInject atomic.Bool
	model := &contractModel{fn: func(i int, msgs []agentcore.Message) (*agentcore.LLMResponse, error) {
		if i > 0 {
			for _, m := range msgs {
				if strings.Contains(m.TextContent(), "Cấm kết thúc-hợp đồng") {
					sawInject.Store(true)
				}
			}
		}
		return &agentcore.LLMResponse{Message: assistantText("try stop", agentcore.StopReasonStop)}, nil
	}}

	var guardCalls atomic.Int32
	err := runSubagent(t, subagent.Config{
		Name: "stubborn", Description: "contract", Model: model,
		SystemPrompt: "test", MaxTurns: 10,
		StopGuardFactory: func(_, _ string) agentcore.StopGuard {
			return func(context.Context, agentcore.StopInfo) agentcore.StopDecision {
				switch guardCalls.Add(1) {
				case 1:
					return agentcore.StopDecision{Allow: false, InjectMessage: "Cấm kết thúc-hợp đồng"}
				default:
					return agentcore.StopDecision{Allow: false, Escalate: true}
				}
			}
		},
	})
	if !errors.Is(err, agentcore.ErrStopGuard) {
		t.Fatalf("Escalate 应以可 errors.Is(agentcore.ErrStopGuard) 的错误浮出，got %v", err)
	}

	if !sawInject.Load() {
		t.Fatal("lượt kế tiếp của model phải chứa thông điệp được chèn")
	}
	if guardCalls.Load() != 2 {
		t.Fatalf("guard phải được hỏi đúng 2 lần (1 chèn + 1 nâng cấp), got %d", guardCalls.Load())
	}
	if model.calls() != 2 {
		t.Fatalf("sau khi Escalate, model không được gọi nữa, kỳ vọng đúng 2 lần, got %d", model.calls())
	}
}

// Hợp đồng 5: lỗi của Runner.Run giữ chuỗi kiểu — agent chưa đăng ký nổi lên
// dưới dạng subagent.ErrUnknownAgent.
// Bên phụ thuộc: isDeterministicWorkerError trong host/engine.go (phân loại
// "thử lại chắc chắn lỗi lặp lại → dừng" dựa vào errors.Is chứ không dựa vào
// văn bản lỗi).
func TestContract_RunUnknownAgentIsTyped(t *testing.T) {
	runner := subagent.NewRunner(subagent.Config{
		Name: "writer", Description: "contract",
		Model: &contractModel{fn: func(int, []agentcore.Message) (*agentcore.LLMResponse, error) {
			return &agentcore.LLMResponse{Message: assistantText("ok", agentcore.StopReasonStop)}, nil
		}},
		SystemPrompt: "test", MaxTurns: 3,
	})
	_, err := runner.Run(context.Background(), "ghost", "contract")
	if !errors.Is(err, subagent.ErrUnknownAgent) {
		t.Fatalf("agent chưa đăng ký phải khớp subagent.ErrUnknownAgent, got %v", err)
	}
}
