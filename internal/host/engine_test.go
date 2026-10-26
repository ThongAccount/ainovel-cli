package host

// Kiểm thử tích hợp end-to-end của Engine (nghiệm thu prototype theo
// engine-rfc.md §7):
// store thật + công cụ Worker thật + ChatModel theo kịch bản, kiểm tra
//  1. Chuỗi viết sách đầy đủ do Route dẫn: viết chương 1 → viết chương 2 →
//     hoàn thành → engine tự dừng
//  2. Đường lỗi Worker: thử lại một lần → Tài phán phán quyết abort cho
//     worker_failure → tạm dừng + ghi kiểm toán xuống đĩa
//  3. Đường bế tắc: cùng chỉ dẫn không tiến triển ×3 → Tài phán phán quyết
//     deadlock → ghi kiểm toán xuống đĩa → abort dừng máy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/subagent"
	"github.com/voocel/ainovel-cli/internal/arbiter"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/flow"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/tools"
)

// scriptedChatModel là ChatModel tối thiểu sinh phản hồi theo callback.
type scriptedChatModel struct {
	fn func(msgs []agentcore.Message) agentcore.Message
}

func TestFailureFactsKeepPartialStateAndWarnings(t *testing.T) {
	dir := t.TempDir()
	st := storepkg.NewStore(dir)
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.Init(3); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "premise.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	e := &engine{store: st}
	workerErr := fmt.Errorf("writer exhausted: %w", agentcore.ErrMaxTurns)
	facts := e.failureFacts("worker_failure", &flow.Instruction{Agent: "writer", Task: "viết tiếp"}, workerErr)
	if facts.ErrorKind != "max_turns" || facts.Phase != string(domain.PhaseInit) {
		t.Fatalf("phải giữ loại lỗi và sự thật tiến độ đọc được: %+v", facts)
	}
	if len(facts.FactWarnings) == 0 {
		t.Fatalf("sự thật nền tảng không đọc được phải được giao cho Tài phán dưới dạng cảnh báo: %+v", facts)
	}
}

func TestIsNonSemanticWorkerFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "context overflow", err: agentcore.ErrContextOverflow, want: true},
		{name: "partial stream", err: agentcore.ErrStreamPartial, want: true},
		{name: "stream idle", err: agentcore.ErrProviderStreamIdle, want: true},
		{name: "quota", err: agentcore.ErrProviderQuota, want: true},
		{name: "rate limit", err: agentcore.ErrProviderRateLimit, want: true},
		{name: "timeout", err: agentcore.ErrProviderTimeout, want: true},
		{name: "auth", err: agentcore.ErrProviderAuth, want: true},
		{name: "network wrapped", err: fmt.Errorf("provider: %w", agentcore.ErrProviderNetwork), want: true},
		{name: "raw EOF", err: fmt.Errorf("upstream closed: EOF"), want: true},
		{name: "overloaded", err: agentcore.ErrProviderOverloaded, want: true},
		{name: "flattened overloaded", err: fmt.Errorf("bad_response_status_code: Too many concurrent requests [provider, HTTP 500, openai]"), want: true},
		{name: "content filter", err: agentcore.ErrProviderContentFilter, want: false},
		{name: "max turns", err: agentcore.ErrMaxTurns, want: false},
		{name: "stop guard", err: agentcore.ErrStopGuard, want: false},
		{name: "canceled", err: context.Canceled, want: false},
		{name: "tool validation", err: agentcore.ErrToolValidation, want: false},
		{name: "unknown", err: fmt.Errorf("unknown failure"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNonSemanticWorkerFailure(tt.err); got != tt.want {
				t.Fatalf("isNonSemanticWorkerFailure(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestInterventionDispatchTaskPreservesOriginalAuthority(t *testing.T) {
	const task = "kiểm tra nội dung trùng lặp và sắp xếp làm lại cần thiết"
	const original = "  đừng giải thích lại nguồn gốc năng lực; đừng sửa nội dung không liên quan.\n"

	got := interventionDispatchTask(task, original, hostTextsVI)
	if !strings.Contains(got, task) {
		t.Fatalf("mất nhiệm vụ phân công: %q", got)
	}
	if !strings.Contains(got, original) {
		t.Fatalf("can thiệp gốc của người dùng không được giữ nguyên từng chữ: %q", got)
	}
	if !strings.Contains(got, "nguồn duy nhất cho phép sửa đổi") {
		t.Fatalf("thiếu phần mô tả ranh giới ủy quyền: %q", got)
	}

	// bản tiếng Trung phải giữ nguyên câu gốc của dự án
	gotZH := interventionDispatchTask(task, original, hostTextsZH)
	if !strings.Contains(gotZH, "修改授权的唯一来源") {
		t.Fatalf("bản tiếng Trung thiếu giới hạn ủy quyền: %q", gotZH)
	}
	if !strings.Contains(gotZH, original) {
		t.Fatalf("bản tiếng Trung mất can thiệp gốc: %q", gotZH)
	}
}

func (m *scriptedChatModel) Generate(_ context.Context, msgs []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	return &agentcore.LLMResponse{Message: m.fn(msgs)}, nil
}

func (m *scriptedChatModel) GenerateStream(ctx context.Context, msgs []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	resp, _ := m.Generate(ctx, msgs, tools, opts...)
	ch := make(chan agentcore.StreamEvent, 1)
	ch <- agentcore.StreamEvent{Type: agentcore.StreamEventDone, Message: resp.Message, StopReason: resp.Message.StopReason}
	close(ch)
	return ch, nil
}

func (m *scriptedChatModel) SupportsTools() bool { return true }

// editThenCancelModel tái hiện #84: mỗi Worker đều thành công tạo ra một
// edit checkpoint khác nội dung, rồi trả về context canceled trong cùng một
// run, không bao giờ commit.
type editThenCancelModel struct {
	edits atomic.Int32
}

func (m *editThenCancelModel) Generate(_ context.Context, msgs []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	if len(msgs) > 0 && msgs[len(msgs)-1].Role == agentcore.RoleTool {
		return nil, context.Canceled
	}
	n := int(m.edits.Add(1))
	return &agentcore.LLMResponse{Message: testToolCallMsg("edit_chapter", map[string]any{
		"chapter":    1,
		"old_string": fmt.Sprintf("phiên bản %d", n-1),
		"new_string": fmt.Sprintf("phiên bản %d", n),
	})}, nil
}

func (m *editThenCancelModel) GenerateStream(ctx context.Context, msgs []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	resp, err := m.Generate(ctx, msgs, tools, opts...)
	if err != nil {
		return nil, err
	}
	ch := make(chan agentcore.StreamEvent, 1)
	ch <- agentcore.StreamEvent{Type: agentcore.StreamEventDone, Message: resp.Message, StopReason: resp.Message.StopReason}
	close(ch)
	return ch, nil
}

func (m *editThenCancelModel) SupportsTools() bool { return true }

// providerNetworkModel mô phỏng Worker gặp lỗi mạng tức thời trước khi có
// bất kỳ đầu ra mô hình nào. Khi MaxRetries=0 thì mỗi subagent.Run tương ứng
// với một lần gọi, thuận tiện để kiểm chứng bộ đếm thử lại của Engine.
type providerNetworkModel struct {
	calls atomic.Int32
}

func (m *providerNetworkModel) Generate(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	m.calls.Add(1)
	return nil, fmt.Errorf("test provider EOF: %w", agentcore.ErrProviderNetwork)
}

func (m *providerNetworkModel) GenerateStream(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	m.calls.Add(1)
	return nil, fmt.Errorf("test provider EOF: %w", agentcore.ErrProviderNetwork)
}

func (m *providerNetworkModel) SupportsTools() bool { return true }

func testToolCallMsg(name string, args any) agentcore.Message {
	data, _ := json.Marshal(args)
	return agentcore.Message{
		Role: agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.ToolCallBlock(agentcore.ToolCall{
			ID: "tc-" + name, Name: name, Args: data,
		})},
		StopReason: agentcore.StopReasonToolUse,
	}
}

func testTextMsg(text string) agentcore.Message {
	return agentcore.Message{
		Role:       agentcore.RoleAssistant,
		Content:    []agentcore.ContentBlock{agentcore.TextBlock(text)},
		StopReason: agentcore.StopReasonStop,
	}
}

// chapterRe khớp số chương trong nhiệm vụ ghi chương: bản tiếng Trung
// ("写第 3 章") và bản tiếng Việt mà internal/flow dùng ("Viết chương 3",
// "Viết lại chương 3", "Hoàn thiện chương 3").
var chapterRe = regexp.MustCompile(`写第 (\d+) 章|(?:[Vv]iết(?:\s+\S+){0,2}\s+|[Hh]oàn thiện\s+)chương (\d+)`)

// scriptedWriterModel quyết định bước kế tiếp theo số kết quả tool đã có
// trong hội thoại, đi qua trọn vẹn chuỗi plan → draft → check → commit
// (công cụ thật, ghi xuống đĩa thật).
func scriptedWriterModel() *scriptedChatModel {
	return &scriptedChatModel{fn: func(msgs []agentcore.Message) agentcore.Message {
		chapter := 0
		toolResults := 0
		for _, m := range msgs {
			if m.Role == agentcore.RoleUser {
				if match := chapterRe.FindStringSubmatch(m.TextContent()); match != nil {
					num := match[1]
					if num == "" {
						num = match[2]
					}
					chapter, _ = strconv.Atoi(num)
				}
			}
			if m.Role == agentcore.RoleTool {
				toolResults++
			}
		}
		switch toolResults {
		case 0:
			return testToolCallMsg("plan_chapter", map[string]any{
				"chapter": chapter, "title": fmt.Sprintf("Chương %d", chapter),
				"goal": "đẩy mạnh tuyến chính", "conflict": "nhân vật chính gặp trở ngại", "hook": "kết chương bằng tình huống treo",
			})
		case 1:
			return testToolCallMsg("draft_chapter", map[string]any{
				"chapter": chapter, "mode": "write",
				"content": strings.Repeat(fmt.Sprintf("Đoạn nội dung của chương %d, nhân vật chính mò mò tiến trong bóng tối.", chapter), 20),
			})
		case 2:
			return testToolCallMsg("check_consistency", map[string]any{"chapter": chapter})
		default:
			return testToolCallMsg("commit_chapter", map[string]any{
				"chapter": chapter, "title": fmt.Sprintf("Chương %d", chapter), "summary": fmt.Sprintf("Tóm tắt chương %d", chapter),
				"characters": []string{"Nhân vật chính"}, "key_events": []string{"Tiến triển"},
				"timeline_events": []any{}, "foreshadow_updates": []any{},
				"relationship_changes": []any{}, "state_changes": []any{}, "cast_intros": []any{},
				"hook_type": "crisis", "dominant_strand": "quest", "feedback": nil,
			})
		}
	}}
}

// newTestEngine lắp ráp engine với store/observer thật; trả về engine, bộ thu
// sự kiện và tín hiệu hoàn tất.
func newTestEngine(t *testing.T, st *storepkg.Store, workers *subagent.Runner, arbiterModel agentcore.ChatModel) (*engine, *[]Event, chan struct{}) {
	t.Helper()
	if err := st.RunMeta.Init("default", "test", "test"); err != nil {
		t.Fatalf("init run meta: %v", err)
	}
	var mu sync.Mutex
	events := &[]Event{}
	done := make(chan struct{}, 1)
	obs := newObserver(st, func(ev Event) {
		mu.Lock()
		*events = append(*events, ev)
		mu.Unlock()
	}, func(string) {}, func() {})
	e := &engine{
		store:           st,
		workers:         workers,
		arbiterModel:    arbiterModel,
		failurePrompt:   "sys",
		planStartPrompt: "sys",
		style:           "default",
		observer:        obs,
		refresh:         func() {},
		emitEvent: func(ev Event) {
			mu.Lock()
			*events = append(*events, ev)
			mu.Unlock()
		},
		notify: func(string, string, string, string) {},
		onDone: func() {
			select {
			case done <- struct{}{}:
			default:
			}
		},
	}
	e.gate = NewChapterAdvanceGate(st, func(string) { e.abort() }, func(string, string) {})
	return e, events, done
}

func waitEngineDone(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("engine không dừng trong thời hạn")
	}
}

func mustInterventionFacts(t *testing.T, st *storepkg.Store) arbiter.InterventionFacts {
	t.Helper()
	facts, err := arbiter.CollectInterventionFacts(st)
	if err != nil {
		t.Fatalf("CollectInterventionFacts: %v", err)
	}
	return facts
}

func TestEngine_ReviewPermitWritesExactlyOneNewChapter(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.Init(3); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatal(err)
	}
	if err := st.Outline.SaveOutline([]domain.OutlineEntry{
		{Chapter: 1, Title: "Một", CoreEvent: "a"},
		{Chapter: 2, Title: "Hai", CoreEvent: "b"},
		{Chapter: 3, Title: "Ba", CoreEvent: "c"},
	}); err != nil {
		t.Fatal(err)
	}
	writer := subagent.Config{
		Name: "writer", Description: "test writer", Model: scriptedWriterModel(), SystemPrompt: "test",
		Tools: []agentcore.Tool{
			tools.NewPlanChapterTool(st), tools.NewDraftChapterTool(st),
			tools.NewCheckConsistencyTool(st), tools.NewCommitChapterTool(st, tools.NewStyleStatsIndex(st)),
		},
		MaxTurns: 10, StopAfterTools: []string{"commit_chapter"},
	}
	e, _, done := newTestEngine(t, st, subagent.NewRunner(writer), nil)
	if err := st.RunMeta.SetAdvanceMode(domain.ChapterAdvanceReview); err != nil {
		t.Fatal(err)
	}
	if err := st.RunMeta.GrantAdvancePermit(1); err != nil {
		t.Fatal(err)
	}
	if !e.start(nil) {
		t.Fatal("engine start")
	}
	waitEngineDone(t, done)

	progress, err := st.Progress.Load()
	if err != nil || progress == nil {
		t.Fatalf("load progress: %v", err)
	}
	if len(progress.CompletedChapters) != 1 || progress.CompletedChapters[0] != 1 {
		t.Fatalf("một giấy phép phải đúng và chỉ ổn định một chương mới: %v", progress.CompletedChapters)
	}
	meta, _ := st.RunMeta.Load()
	if meta.AdvancePermitChapter != 0 {
		t.Fatalf("sau khi commit ổn định thì giấy phép phải được tiêu thụ: %+v", meta)
	}
}

func TestEngine_StalePairedDispatchDoesNotBypassHold(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.Init(3); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatal(err)
	}
	e, _, _ := newTestEngine(t, st, subagent.NewRunner(), nil)
	e.pending = []controlOp{{
		hold:     &arbiter.AdvanceHoldOp{After: domain.AdvanceHoldAtBoundary, Reason: "Dừng trước đã"},
		dispatch: &arbiter.DispatchOp{Agent: "editor", Task: "Nhiệm vụ quá hạn"},
		facts:    arbiter.InterventionFacts{Phase: string(domain.PhaseOutline)},
	}}

	if e.applyPendingOps(context.Background()) {
		t.Fatal("phải phân công đi kèm sự thật đã hết hạn mà chưa rơi vào next thì không được lách Gate")
	}
	if e.next != nil || e.deferGateForNext {
		t.Fatalf("phải phân công quá hạn không được để lại chỉ dẫn có thể thực thi: next=%+v defer=%v", e.next, e.deferGateForNext)
	}
	meta, _ := st.RunMeta.Load()
	if meta.AdvanceHold != nil {
		t.Fatalf("khi phản công đi kèm đã quá hạn thì không được để lại hold lẻ loi: %+v", meta.AdvanceHold)
	}
	if e.gate.HandleBoundary() {
		t.Fatal("khi không có hold lẻ loi, Gate không được tự bịa ra lệnh tạm dừng")
	}
}

// TestEngine_WritesBookToCompletion chuỗi đầy đủ: cuốn sách hai chương không
// phân tầng đi từ writing tới complete.
func TestEngine_WritesBookToCompletion(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := st.Progress.Init(2); err != nil {
		t.Fatalf("progress: %v", err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("phase: %v", err)
	}
	if err := st.Outline.SaveOutline([]domain.OutlineEntry{
		{Chapter: 1, Title: "Chương 1", CoreEvent: "Mở đầu"},
		{Chapter: 2, Title: "Chương 2", CoreEvent: "Kết cục"},
	}); err != nil {
		t.Fatalf("outline: %v", err)
	}

	writer := subagent.Config{
		Name: "writer", Description: "test writer",
		Model:        scriptedWriterModel(),
		SystemPrompt: "test",
		Tools: []agentcore.Tool{
			tools.NewPlanChapterTool(st),
			tools.NewDraftChapterTool(st),
			tools.NewCheckConsistencyTool(st),
			tools.NewCommitChapterTool(st, tools.NewStyleStatsIndex(st)),
		},
		MaxTurns:       10,
		StopAfterTools: []string{"commit_chapter"},
	}
	e, events, done := newTestEngine(t, st, subagent.NewRunner(writer), nil)

	if !e.start(nil) {
		t.Fatal("engine start")
	}
	waitEngineDone(t, done)

	progress, err := st.Progress.Load()
	if err != nil || progress == nil {
		t.Fatalf("load progress: %v", err)
	}
	if progress.Phase != domain.PhaseComplete {
		t.Fatalf("viết đủ hai chương thì phải hoàn thành, got phase=%s completed=%v", progress.Phase, progress.CompletedChapters)
	}
	if len(progress.CompletedChapters) != 2 {
		t.Fatalf("phải hoàn thành 2 chương, got %v", progress.CompletedChapters)
	}
	// Hình dạng sự kiện: mỗi chương một DISPATCH (engine phát ra), dòng TOOL đến
	// từ bộ rút tiến độ
	var dispatches, toolRows int
	for _, ev := range *events {
		switch ev.Category {
		case "DISPATCH":
			dispatches++
		case "TOOL":
			toolRows++
		}
	}
	if dispatches < 2 {
		t.Fatalf("phải có ít nhất 2 sự kiện DISPATCH, got %d", dispatches)
	}
	if toolRows == 0 {
		t.Fatal("tiến độ công cụ Worker chưa được chiếu qua bộ rút (thiếu dòng TOOL)")
	}
}

// TestEngine_WorkerFailureConsultsArbiterAndAborts đường lỗi:
// writer mắc kẹt bị StopGuard nâng cấp → thử lại một lần → Tài phán phán quyết
// abort → tạm dừng + kiểm toán.
func TestEngine_WorkerFailureConsultsArbiterAndAborts(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := st.Progress.Init(2); err != nil {
		t.Fatalf("progress: %v", err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("phase: %v", err)
	}
	if err := st.Outline.SaveOutline([]domain.OutlineEntry{{Chapter: 1, Title: "Một", CoreEvent: "s"}}); err != nil {
		t.Fatalf("outline: %v", err)
	}

	var runs atomic.Int32
	// writer mỗi vòng chỉ trả lời chữ, không ghi xuống đĩa → guard.NewWriterStopGuard
	// chặn liên tiếp rồi nâng cấp → Execute báo lỗi
	idle := &scriptedChatModel{fn: func([]agentcore.Message) agentcore.Message {
		return testTextMsg("Tôi đã viết xong (thực ra chẳng làm gì cả)")
	}}
	writer := subagent.Config{
		Name: "writer", Description: "idle writer",
		Model: idle, SystemPrompt: "test", MaxTurns: 20,
		StopGuardFactory: func(_, _ string) agentcore.StopGuard {
			runs.Add(1)
			return failNTimesGuard()
		},
	}
	// Tài phán phán quyết abort
	arb := &scriptedChatModel{fn: func([]agentcore.Message) agentcore.Message {
		return testTextMsg(`{"action":"abort","dispatch":null,"reason":"writer lặp lại không tiến triển, nên kiểm tra cấu hình mô hình bằng tay"}`)
	}}
	e, _, done := newTestEngine(t, st, subagent.NewRunner(writer), arb)

	if !e.start(nil) {
		t.Fatal("engine start")
	}
	waitEngineDone(t, done)

	if got := runs.Load(); got != 2 {
		t.Fatalf("lần thất bại đầu phải được thử lại một lần (tổng 2 lần spawn), got %d", got)
	}
	recs, err := st.Decisions.Recent(10)
	if err != nil {
		t.Fatalf("decisions: %v", err)
	}
	var found bool
	for _, r := range recs {
		if r.Kind == "worker_failure" && r.Decider == "arbiter" {
			found = true
			if !strings.Contains(string(r.Decision), "abort") {
				t.Fatalf("nội dung phán quyết phải chứa abort: %s", r.Decision)
			}
		}
	}
	if !found {
		t.Fatalf("phán quyết worker_failure phải được ghi xuống đĩa: %+v", recs)
	}
}

// seedStuckRewrite dựng hiện trường "chương 2 đã hoàn thành và được xếp vào
// hàng đợi làm lại".
func seedStuckRewrite(t *testing.T, st *storepkg.Store) {
	t.Helper()
	if err := st.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := st.Progress.Init(5); err != nil {
		t.Fatalf("progress: %v", err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("phase: %v", err)
	}
	if err := st.Progress.MarkChapterComplete(2, 3000, "", ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := st.Progress.SetPendingRewrites([]int{2}, "Thẩm duyệt yêu cầu viết lại"); err != nil {
		t.Fatalf("pending: %v", err)
	}
	if err := st.Progress.SetFlow(domain.FlowRewriting); err != nil {
		t.Fatalf("flow: %v", err)
	}
}

// TestEngine_DeadlockAbortDropsStuckRewrite ghim mặt bế tắc của issue #110: khi
// ngắt mạch bế tắc, chương làm lại bị kẹt phải ra khỏi hàng đợi. PendingRewrites là
// sự thật đã lưu — chỉ tạm dừng mà không ra khỏi hàng đợi thì lần khởi động sau sẽ
// phát lại ngay chỉ dẫn chết đó, khóa vĩnh viễn cả tác phẩm.
func TestEngine_DeadlockAbortDropsStuckRewrite(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	seedStuckRewrite(t, st)
	e, events, _ := newTestEngine(t, st, subagent.NewRunner(), nil)

	inst := &flow.Instruction{Agent: "writer", Task: "Viết lại chương 2", Chapter: 2}
	e.lastKey, e.repeats = instructionKey(inst), deadlockAbortAt-1

	if stop := e.trackDeadlock(context.Background(), &inst); !stop {
		t.Fatal("ngắt mạch bế tắc vẫn phải dừng chờ người vận hành can thiệp")
	}
	p, err := st.Progress.Load()
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if len(p.PendingRewrites) != 0 {
		t.Fatalf("chương làm lại bị kẹt phải ra khỏi hàng đợi khi ngắt mạch: %v", p.PendingRewrites)
	}
	if p.Flow != domain.FlowWriting {
		t.Fatalf("hàng đợi rỗng thì flow phải về writing, thực tế %s", p.Flow)
	}
	var notified bool
	for _, ev := range *events {
		if strings.Contains(ev.Summary, "đã bị chuyển khỏi hàng đợi làm lại") {
			notified = true
		}
	}
	if !notified {
		t.Fatalf("bỏ qua làm lại phải nói rõ với người dùng: %+v", *events)
	}
}

// TestEngine_DropStuckRewriteOnlyTouchesQueuedChapter ra khỏi hàng đợi là hành
// động phá hủy, phạm vi ảnh hưởng phải được đóng đinh: chỉ "chương đang nằm
// trong hàng đợi làm lại" mới được rút ra, mọi chỉ dẫn khác đều không được đụng
// tới hàng đợi.
func TestEngine_DropStuckRewriteOnlyTouchesQueuedChapter(t *testing.T) {
	cases := []struct {
		name string
		inst *flow.Instruction
	}{
		{"Chỉ thị không phải writer", &flow.Instruction{Agent: "editor", Task: "Thẩm duyệt mức cung"}},
		{"Chỉ thị writer không liên quan chương nào", &flow.Instruction{Agent: "writer", Task: "Viết tiếp"}},
		{"Chương viết tiếp không có trong hàng đợi", &flow.Instruction{Agent: "writer", Task: "Viết chương 3", Chapter: 3}},
		{"Chỉ thị rỗng", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := storepkg.NewStore(t.TempDir())
			seedStuckRewrite(t, st)
			e, _, _ := newTestEngine(t, st, subagent.NewRunner(), nil)
			if e.dropStuckRewrite(tc.inst) {
				t.Fatal("không nên ra khỏi hàng đợi")
			}
			p, err := st.Progress.Load()
			if err != nil {
				t.Fatalf("progress: %v", err)
			}
			if len(p.PendingRewrites) != 1 || p.PendingRewrites[0] != 2 {
				t.Fatalf("hàng đợi làm lại không được bị ảnh hưởng nhầm: %v", p.PendingRewrites)
			}
		})
	}
}

// TestEngine_TransientProviderFailuresDoNotBecomeDeadlock hồi quy chuỗi lỗi ở
// chương 135: worker_failure=retry sau hai vòng lỗi mạng không được bị
// trackDeadlock ở vòng kế tiếp coi là “cùng một nhiệm vụ viết liên tục không
// tiến triển” rồi kích hoạt đổi phân công do bế tắc.
func TestEngine_TransientProviderFailuresDoNotBecomeDeadlock(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := st.Progress.Init(1); err != nil {
		t.Fatalf("progress: %v", err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("phase: %v", err)
	}
	if err := st.Outline.SaveOutline([]domain.OutlineEntry{{Chapter: 1, Title: "Chương 1", CoreEvent: "Mở đầu"}}); err != nil {
		t.Fatalf("outline: %v", err)
	}

	network := &providerNetworkModel{}
	writer := subagent.Config{
		Name: "writer", Description: "network failing writer",
		Model: network, SystemPrompt: "test", MaxTurns: 5, MaxRetries: 0,
	}
	var arbiterCalls atomic.Int32
	arb := &scriptedChatModel{fn: func([]agentcore.Message) agentcore.Message {
		if arbiterCalls.Add(1) == 1 {
			return testTextMsg(`{"action":"retry","dispatch":null,"reason":"Lỗi mạng tức thời, thử lại nhiệm vụ gốc"}`)
		}
		return testTextMsg(`{"action":"abort","dispatch":null,"reason":"Mạng tiếp tục không dùng được, tạm dừng chờ phục hồi"}`)
	}}
	e, events, done := newTestEngine(t, st, subagent.NewRunner(writer), arb)

	if !e.start(nil) {
		t.Fatal("engine start")
	}
	waitEngineDone(t, done)

	if got := network.calls.Load(); got != 4 {
		t.Fatalf("hai chu kỳ lỗi của Engine chạy 2 lần Worker mỗi chu kỳ, got %d", got)
	}
	recs, err := st.Decisions.Recent(10)
	if err != nil {
		t.Fatalf("decisions: %v", err)
	}
	var workerFailures, deadlocks int
	for _, rec := range recs {
		switch rec.Kind {
		case "worker_failure":
			workerFailures++
		case "deadlock":
			deadlocks++
		}
	}
	if workerFailures != 2 || deadlocks != 0 {
		t.Fatalf("lỗi mạng chỉ được ghi vào worker_failure, got worker_failure=%d deadlock=%d records=%+v", workerFailures, deadlocks, recs)
	}
	var failedDispatches, duplicateErrors int
	for _, ev := range *events {
		if ev.Category == "DISPATCH" && ev.Failed && ev.Kind == "network" && strings.Contains(ev.Detail, "test provider EOF") {
			failedDispatches++
		}
		if ev.Category == "ERROR" && strings.Contains(ev.Detail, "test provider EOF") {
			duplicateErrors++
		}
	}
	if failedDispatches != 4 || duplicateErrors != 0 {
		t.Fatalf("mỗi lần Worker thất bại chỉ được cập nhật DISPATCH, got dispatch=%d duplicate_error=%d events=%+v", failedDispatches, duplicateErrors, *events)
	}
}

// failNTimesGuard là StopGuard nâng cấp ngay lập tức (mô phỏng ngắt mạch do
// mắc kẹt).
func failNTimesGuard() agentcore.StopGuard {
	return func(context.Context, agentcore.StopInfo) agentcore.StopDecision {
		return agentcore.StopDecision{Allow: false, Escalate: true}
	}
}

// TestEngine_RetriesUnfinishedPlanStart đường tự lành sau khi phán quyết khởi động
// thất bại: StartPrompt đã lưu, PlanStart thiếu (lỗi mô hình lúc khởi động) → engine
// bổ sung phán quyết tại chỗ khi vận hành → ghim PlanStartRecord → phái nhà quy hoạch.
// Nhà quy hoạch không ghi xuống đĩa → đi đường bế tắc sẵn có để dừng, chứng minh sau khi
// bổ sung phán quyết engine đã về đúng đường.
func TestEngine_RetriesUnfinishedPlanStart(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := st.Progress.Init(0); err != nil {
		t.Fatalf("progress: %v", err)
	}
	// Mô phỏng hiện trường StartPrepared thất bại: sự thật đầu vào có, sự thật
	// phán quyết thì thiếu.
	if err := st.RunMeta.SetStartPrompt("Tiểu Đường Vấn Cốc"); err != nil {
		t.Fatalf("start prompt: %v", err)
	}

	// Arbiter: lần gọi đầu là bổ sung phán quyết (plan_start), sau đó là hỏi ý
	// kiến bế tắc (abort để chốt).
	var arbCalls atomic.Int32
	arb := &scriptedChatModel{fn: func([]agentcore.Message) agentcore.Message {
		if arbCalls.Add(1) == 1 {
			return testTextMsg(`{"planner":"architect_long","task":"Lên kế hoạch ba tập cho Tiểu Đường Vấn Cốc","reason":"Tiểu thuyết tu tiên nhiều tập"}`)
		}
		return testTextMsg(`{"action":"abort","dispatch":null,"reason":"Nhà quy hoạch đứng yên, dừng"}`)
	}}
	// Nhà quy hoạch trả lời thành công nhưng không ghi gì xuống đĩa → Route luôn
	// trả cùng một chỉ dẫn bổ sung → bế tắc.
	architect := subagent.Config{
		Name: "architect_long", Description: "idle planner",
		Model: &scriptedChatModel{fn: func([]agentcore.Message) agentcore.Message {
			return testTextMsg("Đã lên kế hoạch (thực ra không ghi gì xuống đĩa)")
		}},
		SystemPrompt: "test", MaxTurns: 3,
	}
	e, events, done := newTestEngine(t, st, subagent.NewRunner(architect), arb)

	if !e.start(nil) {
		t.Fatal("engine start")
	}
	waitEngineDone(t, done)

	meta, err := st.RunMeta.Load()
	if err != nil || meta == nil || meta.PlanStart == nil {
		t.Fatalf("sau khi bổ sung phán quyết, PlanStart phải được ghim, meta=%+v err=%v", meta, err)
	}
	if meta.PlanStart.Planner != "architect_long" || meta.PlanStart.RawPrompt != "Tiểu Đường Vấn Cốc" || meta.PlanStart.DecisionID == "" {
		t.Fatalf("PlanStartRecord thiếu trường: %+v", meta.PlanStart)
	}
	recs, err := st.Decisions.Recent(10)
	if err != nil {
		t.Fatalf("decisions: %v", err)
	}
	var planStartRec bool
	for _, r := range recs {
		if r.Kind == "plan_start" && strings.Contains(string(r.Decision), "architect_long") {
			planStartRec = true
		}
	}
	if !planStartRec {
		t.Fatalf("bổ sung phán quyết phải để lại kiểm toán plan_start: %+v", recs)
	}
	var dispatched, healed bool
	for _, ev := range *events {
		if ev.Category == "DISPATCH" {
			dispatched = true
		}
		if strings.Contains(ev.Summary, "đã bổ sung phán quyết khởi động") {
			healed = true
		}
	}
	if !dispatched || !healed {
		t.Fatalf("sau khi bổ sung phán quyết phải phái nhà quy hoạch và phát sự kiện bổ sung, dispatched=%v healed=%v", dispatched, healed)
	}
}

// TestEngine_PlanStartRetryFailurePauses bổ sung phán quyết thất bại không được dừng
// im lặng: Arbiter liên tục không dùng được → phải có thông báo tạm dừng tường minh +
// kiểm toán plan_start mang error + không phái worker nào.
func TestEngine_PlanStartRetryFailurePauses(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := st.Progress.Init(0); err != nil {
		t.Fatalf("progress: %v", err)
	}
	if err := st.RunMeta.SetStartPrompt("Tiểu Đường Vấn Cốc"); err != nil {
		t.Fatalf("start prompt: %v", err)
	}

	var e *engine
	arb := &scriptedChatModel{fn: func([]agentcore.Message) agentcore.Message {
		e.abort() // Mô phỏng host huỷ lệnh gọi liên tục thất bại; nhánh lỗi do
		// context kết thúc dứt khoát.
		return testTextMsg("Đây không phải JSON")
	}}
	e, events, done := newTestEngine(t, st, subagent.NewRunner(), arb)

	if !e.start(nil) {
		t.Fatal("engine start")
	}
	waitEngineDone(t, done)

	for _, ev := range *events {
		if ev.Category == "DISPATCH" {
			t.Fatal("bổ sung phán quyết thất bại không được phái worker nào")
		}
	}
	var paused bool
	for _, ev := range *events {
		if strings.Contains(ev.Summary, "phán quyết khởi động thất bại") {
			paused = true
		}
	}
	if !paused {
		t.Fatalf("bổ sung phán quyết thất bại phải phát lại rõ lý do tạm dừng, events=%+v", *events)
	}
	recs, err := st.Decisions.Recent(5)
	if err != nil {
		t.Fatalf("decisions: %v", err)
	}
	var errRec bool
	for _, r := range recs {
		if r.Kind == "plan_start" && r.Error != "" && len(r.Decision) == 0 {
			errRec = true
		}
	}
	if !errRec {
		t.Fatalf("phán quyết thất bại phải ghi xuống đĩa kèm error: %+v", recs)
	}
}

// TestEngine_DeadlockConsultsArbiter đường bế tắc: chỉ dẫn bổ sung quy hoạch
// lặp lại liên tiếp → lần thứ 3 hỏi ý kiến Tài phán → abort dừng máy +
// kiểm toán deadlock.
func TestEngine_DeadlockConsultsArbiter(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := st.Progress.Init(3); err != nil {
		t.Fatalf("progress: %v", err)
	}
	// Giai đoạn quy hoạch + tier đã biết + thiếu mục vẫn còn → Route mỗi vòng
	// vẫn sinh ra cùng một chỉ dẫn bổ sung
	if err := st.RunMeta.SetPlanningTier(domain.PlanningTierLong); err != nil {
		t.Fatalf("tier: %v", err)
	}

	// architect không có guard, trả lời thành công nhưng không ghi gì xuống đĩa
	// → chỉ dẫn Route không đổi
	lazy := &scriptedChatModel{fn: func([]agentcore.Message) agentcore.Message {
		return testTextMsg("Biết rồi (chẳng làm gì cả)")
	}}
	architect := subagent.Config{
		Name: "architect_long", Description: "lazy architect",
		Model: lazy, SystemPrompt: "test", MaxTurns: 5,
	}
	arb := &scriptedChatModel{fn: func([]agentcore.Message) agentcore.Message {
		return testTextMsg(`{"action":"abort","dispatch":null,"reason":"Nhà quy hoạch lặp lại không có sản phẩm"}`)
	}}
	e, _, done := newTestEngine(t, st, subagent.NewRunner(architect), arb)

	if !e.start(nil) {
		t.Fatal("engine start")
	}
	waitEngineDone(t, done)

	recs, err := st.Decisions.Recent(10)
	if err != nil {
		t.Fatalf("decisions: %v", err)
	}
	var found bool
	for _, r := range recs {
		if r.Kind == "deadlock" && r.Decider == "arbiter" {
			found = true
		}
	}
	if !found {
		t.Fatalf("phán quyết deadlock phải được ghi xuống đĩa: %+v", recs)
	}
}

// TestEngine_IntermediateCheckpointsDoNotMaskDeadlock ghim #84: Writer sửa bản
// nháp nhiều lần sẽ tạo ra digest mới và edit checkpoint mới, nhưng chừng nào
// Route vẫn là cùng một “Hoàn thiện chương 1” thì hậu điều kiện ở cấp Engine
// (commit) vẫn chưa xong, phải tiếp tục cộng dồn bế tắc.
func TestEngine_IntermediateCheckpointsDoNotMaskDeadlock(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := st.Progress.Init(1); err != nil {
		t.Fatalf("progress: %v", err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("phase: %v", err)
	}
	if err := st.Outline.SaveOutline([]domain.OutlineEntry{{Chapter: 1, Title: "Chương 1", CoreEvent: "Mở đầu"}}); err != nil {
		t.Fatalf("outline: %v", err)
	}
	if err := st.Drafts.SaveDraft(1, "phiên bản 0 bản nháp đầu tiên"); err != nil {
		t.Fatalf("draft: %v", err)
	}
	if err := st.Progress.MarkChapterComplete(1, len([]rune("phiên bản 0 bản nháp đầu tiên")), "mystery", "quest"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := st.Progress.SetPendingRewrites([]int{1}, "Thử hoàn thiện mà không commit"); err != nil {
		t.Fatalf("pending rewrite: %v", err)
	}
	if err := st.Progress.SetFlow(domain.FlowPolishing); err != nil {
		t.Fatalf("flow: %v", err)
	}

	writerModel := &editThenCancelModel{}
	writer := subagent.Config{
		Name: "writer", Description: "edit then cancel writer",
		Model: writerModel, SystemPrompt: "test",
		Tools:    []agentcore.Tool{tools.NewEditChapterTool(st)},
		MaxTurns: 5,
	}
	// Dù Tài phán luôn yêu cầu retry cho worker_failure / deadlock, lần ngắt
	// mạch cứng thứ 5 hiện có vẫn phải cắt trước khi phân công, không được bị
	// edit checkpoint đặt lại.
	arb := &scriptedChatModel{fn: func([]agentcore.Message) agentcore.Message {
		return testTextMsg(`{"action":"retry","dispatch":null,"reason":"Tiếp tục thử lại"}`)
	}}
	e, _, done := newTestEngine(t, st, subagent.NewRunner(writer), arb)

	if !e.start(nil) {
		t.Fatal("engine start")
	}
	waitEngineDone(t, done)

	if got := writerModel.edits.Load(); got != deadlockAbortAt-1 {
		t.Fatalf("bế tắc phải bị ngắt mạch cứng trước lần phân công thứ %d, thực tế edit %d lần", deadlockAbortAt, got)
	}
	var edits int
	for _, cp := range st.Checkpoints.All() {
		if cp.Scope.Matches(domain.ChapterScope(1)) && cp.Step == "edit" {
			edits++
		}
	}
	if edits != deadlockAbortAt-1 {
		t.Fatalf("phải giữ %d edit checkpoint khác nhau, thực tế %d", deadlockAbortAt-1, edits)
	}
	recs, err := st.Decisions.Recent(10)
	if err != nil {
		t.Fatalf("decisions: %v", err)
	}
	var hasWorkerFailure, hasDeadlockWithCause bool
	for _, rec := range recs {
		switch rec.Kind {
		case "worker_failure":
			hasWorkerFailure = true
		case "deadlock":
			var facts arbiter.FailureFacts
			if err := json.Unmarshal(rec.Facts, &facts); err != nil {
				t.Fatalf("decode deadlock facts: %v", err)
			}
			if facts.ErrorKind == "canceled" && strings.Contains(facts.Error, "context canceled") {
				hasDeadlockWithCause = true
			}
		}
	}
	if !hasWorkerFailure || !hasDeadlockWithCause {
		t.Fatalf("phải ghi worker_failure trước, deadlock phải giữ lại lỗi cuối cùng: %+v", recs)
	}
}

// TestEngine_PauseWithEditorDispatchWaitsForRewriteQueue kiểm chứng bản vá
// (chặn thẩm duyệt 2): phán quyết làm lại của Tài phán = điểm dừng + phân
// công editor để xếp hàng. Điểm dừng chỉ được tiêu thụ sau khi editor đã lập
// hàng đợi làm lại và writer đã viết lại rút hết — không được bị “hàng đợi đã
// rút hết” nhận nhầm rồi tiêu thụ trước khi editor chạy.
func TestEngine_PauseWithEditorDispatchWaitsForRewriteQueue(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := st.Progress.Init(3); err != nil {
		t.Fatalf("progress: %v", err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("phase: %v", err)
	}
	if err := st.Outline.SaveOutline([]domain.OutlineEntry{
		{Chapter: 1, Title: "Một", CoreEvent: "a"},
		{Chapter: 2, Title: "Hai", CoreEvent: "b"},
		{Chapter: 3, Title: "Ba", CoreEvent: "c"},
	}); err != nil {
		t.Fatalf("outline: %v", err)
	}
	// Chương 1 đã hoàn thành (sẽ bị làm lại); writer worker sẽ viết lại nó trước,
	// rồi điểm dừng mới được tiêu thụ.
	if err := st.Progress.StartChapter(1); err != nil {
		t.Fatalf("start ch1: %v", err)
	}
	if err := st.Progress.MarkChapterComplete(1, 1200, "crisis", "quest"); err != nil {
		t.Fatalf("complete ch1: %v", err)
	}

	// editor: một lần save_review (verdict=rewrite, affected=[1]) đưa chương 1
	// vào hàng đợi.
	editorModel := &scriptedChatModel{fn: func(msgs []agentcore.Message) agentcore.Message {
		toolResults := 0
		for _, m := range msgs {
			if m.Role == agentcore.RoleTool {
				toolResults++
			}
		}
		if toolResults == 0 {
			return testToolCallMsg("save_review", map[string]any{
				"chapter": 1, "scope": "chapter",
				"dimensions": []map[string]any{
					{"dimension": "consistency", "score": 85, "comment": "Đạt (trích dẫn: nguyên văn)"},
					{"dimension": "character", "score": 85, "comment": "Đạt (trích dẫn: nguyên văn)"},
					{"dimension": "pacing", "score": 85, "comment": "Đạt (trích dẫn: nguyên văn)"},
					{"dimension": "continuity", "score": 85, "comment": "Đạt (trích dẫn: nguyên văn)"},
					{"dimension": "foreshadow", "score": 85, "comment": "Đạt (trích dẫn: nguyên văn)"},
					{"dimension": "hook", "score": 85, "comment": "Đạt (trích dẫn: nguyên văn)"},
					{"dimension": "aesthetic", "score": 55, "comment": "Giọng văn không phù hợp (trích dẫn: đoạn đầu nguyên văn)"},
				},
				"issues": []map[string]any{{
					"type": "aesthetic", "severity": "error", "description": "Giọng văn", "evidence": "Nguyên văn", "suggestion": "Làm lạnh hơn",
					"chapters": []int{1}, "requires_change": true,
				}},
				"contract_status": nil, "contract_misses": []string{}, "contract_notes": nil,
				"verdict": "rewrite", "summary": "Cần viết lại giọng văn chương 1",
			})
		}
		return testTextMsg("done")
	}}
	editor := subagent.Config{
		Name: "editor", Description: "test editor", Model: editorModel,
		SystemPrompt: "test", MaxTurns: 6,
		Tools:          []agentcore.Tool{tools.NewSaveReviewTool(st)},
		StopAfterTools: []string{"save_review"},
	}
	writer := subagent.Config{
		Name: "writer", Description: "test writer", Model: scriptedWriterModel(),
		SystemPrompt: "test",
		Tools: []agentcore.Tool{
			tools.NewPlanChapterTool(st),
			tools.NewDraftChapterTool(st),
			tools.NewCheckConsistencyTool(st),
			tools.NewCommitChapterTool(st, tools.NewStyleStatsIndex(st)),
		},
		MaxTurns: 10, StopAfterTools: []string{"commit_chapter"},
	}

	e, _, done := newTestEngine(t, st, subagent.NewRunner(editor, writer), nil)
	// Mô phỏng phán quyết làm lại của Tài phán: hold + dispatch editor (engine
	// chưa chạy → áp dụng ngay).
	e.applyControlOp(context.Background(), controlOp{
		hold:     &arbiter.AdvanceHoldOp{After: domain.AdvanceHoldAfterRewritesDrained, Reason: "Viết lại giọng văn chương 1, xong thì dừng lại để nghiệm thu"},
		dispatch: &arbiter.DispatchOp{Agent: "editor", Task: "Rà soát chương 1: làm giọng văn lạnh hơn, dùng issues[].chapters và requires_change để xếp hàng"},
		facts:    mustInterventionFacts(t, st),
	})
	if !e.start(nil) {
		t.Fatal("engine start")
	}
	waitEngineDone(t, done)

	progress, err := st.Progress.Load()
	if err != nil || progress == nil {
		t.Fatalf("load progress: %v", err)
	}
	// Khẳng định cốt lõi ①: điểm dừng không bị tiêu thụ trước khi editor xếp
	// hàng — chương 1 thực sự đã trải qua lần viết lại (commit viết lại sẽ rút
	// nó khỏi hàng đợi).
	if len(progress.PendingRewrites) != 0 {
		t.Fatalf("hàng đợi làm lại phải đã rút hết, got %v", progress.PendingRewrites)
	}
	if progress.ChapterWordCounts[1] == 1200 {
		t.Fatal("chương 1 phải được viết lại thật (số từ phải thay đổi)")
	}
	// Khẳng định cốt lõi ②: sau khi rút hết thì điểm dừng được tiêu thụ, engine
	// tạm dừng — chương 2 không được viết tiếp.
	if len(progress.CompletedChapters) != 1 {
		t.Fatalf("điểm dừng phải tạm dừng trước khi viết tiếp chương 2, completed=%v", progress.CompletedChapters)
	}
	meta, _ := st.RunMeta.Load()
	if meta != nil && meta.AdvanceHold != nil {
		t.Fatalf("lệnh tạm dừng một lần phải đã được tiêu thụ, got %+v", meta.AdvanceHold)
	}
}

// TestEngine_BoundaryHoldDoesNotDispatchAnotherWorker hồi quy:
// khi can thiệp của người dùng chỉ phán quyết ra boundary hold (không có
// phân công), engine phải tiêu thụ hold và tạm dừng ngay tại biên hiện tại,
// không được viết thêm một chương nào.
func TestEngine_BoundaryHoldDoesNotDispatchAnotherWorker(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := st.Progress.Init(3); err != nil {
		t.Fatalf("progress: %v", err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("phase: %v", err)
	}
	if err := st.Outline.SaveOutline([]domain.OutlineEntry{
		{Chapter: 1, Title: "Một", CoreEvent: "a"},
		{Chapter: 2, Title: "Hai", CoreEvent: "b"},
		{Chapter: 3, Title: "Ba", CoreEvent: "c"},
	}); err != nil {
		t.Fatalf("outline: %v", err)
	}

	writer := subagent.Config{
		Name: "writer", Description: "test writer", Model: scriptedWriterModel(),
		SystemPrompt: "test",
		Tools: []agentcore.Tool{
			tools.NewPlanChapterTool(st),
			tools.NewDraftChapterTool(st),
			tools.NewCheckConsistencyTool(st),
			tools.NewCommitChapterTool(st, tools.NewStyleStatsIndex(st)),
		},
		MaxTurns: 10, StopAfterTools: []string{"commit_chapter"},
	}
	e, _, done := newTestEngine(t, st, subagent.NewRunner(writer), nil)
	if !e.start(nil) {
		t.Fatal("engine start")
	}
	// Trong lúc viết chương 1, can thiệp chỉ có hold đến (khớp với thứ tự thời
	// gian thật của Steer).
	e.enqueue(controlOp{
		hold:  &arbiter.AdvanceHoldOp{After: domain.AdvanceHoldAtBoundary, Reason: "Dừng lại một chút để tôi xem"},
		facts: mustInterventionFacts(t, st),
	})
	waitEngineDone(t, done)

	progress, err := st.Progress.Load()
	if err != nil || progress == nil {
		t.Fatalf("load progress: %v", err)
	}
	// Can thiệp đến khi chương 1 đang chạy → chương 1 viết xong; điểm dừng
	// được tiêu thụ ngay tại biên → chương 2 không được bắt đầu viết.
	if n := len(progress.CompletedChapters); n > 1 {
		t.Fatalf("sau boundary hold không được viết thêm một chương, completed=%v", progress.CompletedChapters)
	}
	meta, _ := st.RunMeta.Load()
	if meta != nil && meta.AdvanceHold != nil {
		t.Fatalf("lệnh tạm dừng một lần phải đã được tiêu thụ, got %+v", meta.AdvanceHold)
	}
}

func TestEngine_TargetChapterHoldStopsAtRequestedChapter(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.Init(3); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatal(err)
	}
	if err := st.Outline.SaveOutline([]domain.OutlineEntry{
		{Chapter: 1, Title: "Một", CoreEvent: "a"},
		{Chapter: 2, Title: "Hai", CoreEvent: "b"},
		{Chapter: 3, Title: "Ba", CoreEvent: "c"},
	}); err != nil {
		t.Fatal(err)
	}

	writer := subagent.Config{
		Name: "writer", Description: "test writer", Model: scriptedWriterModel(), SystemPrompt: "test",
		Tools: []agentcore.Tool{
			tools.NewPlanChapterTool(st), tools.NewDraftChapterTool(st),
			tools.NewCheckConsistencyTool(st), tools.NewCommitChapterTool(st, tools.NewStyleStatsIndex(st)),
		},
		MaxTurns: 10, StopAfterTools: []string{"commit_chapter"},
	}
	e, _, done := newTestEngine(t, st, subagent.NewRunner(writer), nil)
	if err := st.RunMeta.SetAdvanceHold(domain.AdvanceHold{
		After: domain.AdvanceHoldAtChapter, TargetChapter: 2, Reason: "Viết đến chương 2",
	}); err != nil {
		t.Fatal(err)
	}
	if !e.start(nil) {
		t.Fatal("engine start")
	}
	waitEngineDone(t, done)

	progress, err := st.Progress.Load()
	if err != nil || progress == nil {
		t.Fatalf("load progress: %v", err)
	}
	if !slices.Equal(progress.CompletedChapters, []int{1, 2}) {
		t.Fatalf("phải dừng chính xác ở chương 2, completed=%v", progress.CompletedChapters)
	}
	meta, _ := st.RunMeta.Load()
	if meta.AdvanceHold != nil {
		t.Fatalf("hold mục tiêu chương phải đã được tiêu thụ: %+v", meta.AdvanceHold)
	}
}

// TestEngine_ExitRaceRestoresPendingDispatch hồi quy (chặn thẩm duyệt 3):
// khi can thiệp xếp hàng đua với việc engine thoát, phần phân công còn sót
// từ phán quyết không được bị vứt bỏ im lặng — PendingSteer phải được lưu
// lại, các hành động sự thật loại pause phải được thực thi bù.
func TestEngine_ExitRaceRestoresPendingDispatch(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := st.Progress.Init(2); err != nil {
		t.Fatalf("progress: %v", err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("phase: %v", err)
	}

	// worker treo tới khi ctx bị hủy: tạo ra cửa sổ "sau khi xếp hàng thì engine
	// bị abort".
	blocked := &scriptedChatModel{fn: func([]agentcore.Message) agentcore.Message {
		time.Sleep(50 * time.Millisecond)
		return testTextMsg("...")
	}}
	writer := subagent.Config{Name: "writer", Description: "slow", Model: blocked, SystemPrompt: "t", MaxTurns: 100}
	// Cần outline để Route phân công writer
	if err := st.Outline.SaveOutline([]domain.OutlineEntry{{Chapter: 1, Title: "Một", CoreEvent: "a"}, {Chapter: 2, Title: "Hai", CoreEvent: "b"}}); err != nil {
		t.Fatalf("outline: %v", err)
	}
	e, _, done := newTestEngine(t, st, subagent.NewRunner(writer), nil)

	if !e.start(nil) {
		t.Fatal("engine start")
	}
	// worker đang chạy: xếp hàng pause+dispatch, rồi abort ngay (hành động không
	// bao giờ kịp chờ biên kế tiếp).
	e.enqueue(controlOp{
		hold:     &arbiter.AdvanceHoldOp{After: domain.AdvanceHoldAfterRewritesDrained, Reason: "nghiệm thu"},
		dispatch: &arbiter.DispatchOp{Agent: "writer", Task: "Viết lại chương 1"},
		text:     "Viết lại chương 1 rồi dừng lại",
		facts:    mustInterventionFacts(t, st),
	})
	e.abort()
	waitEngineDone(t, done)

	meta, err := st.RunMeta.Load()
	if err != nil || meta == nil {
		t.Fatalf("load meta: %v", err)
	}
	if meta.PendingSteer != "Viết lại chương 1 rồi dừng lại" {
		t.Fatalf("phần phân công còn sót phải được lưu lại vào PendingSteer để phát lại khi phục hồi, got %q", meta.PendingSteer)
	}
	if meta.AdvanceHold == nil {
		t.Fatal("hành động sự thật loại hold phải được thực thi bù trong bước dọn dẹp lúc thoát")
	}
}
