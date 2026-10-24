package arbiter

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/llm"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// scriptedModel trả về văn bản định sẵn theo thứ tự lời gọi.
type scriptedModel struct {
	outputs        []string
	idx            int64
	lastCfg        agentcore.CallConfig
	lastMsgs       []agentcore.Message
	rejectThinking bool
	cancel         context.CancelFunc
	cancelAt       int
}

func (m *scriptedModel) take() string {
	i := int(atomic.AddInt64(&m.idx, 1) - 1)
	if m.cancel != nil && m.cancelAt > 0 && i+1 >= m.cancelAt {
		m.cancel()
	}
	if i >= len(m.outputs) {
		return m.outputs[len(m.outputs)-1]
	}
	return m.outputs[i]
}

func (m *scriptedModel) Generate(_ context.Context, messages []agentcore.Message, _ []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	m.lastCfg = agentcore.ResolveCallConfig(opts)
	m.lastMsgs = messages
	if m.rejectThinking && m.lastCfg.ThinkingLevel != agentcore.ThinkingAuto {
		return nil, errors.New("thinking is only supported for reasoning chat models")
	}
	return &agentcore.LLMResponse{Message: agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(m.take())},
	}}, nil
}

func TestDecidePlanStartDoesNotSendThinkingToChatModel(t *testing.T) {
	m := &scriptedModel{outputs: []string{
		`{"planner":"architect_short","task":"lập dàn ý truyện ngắn","reason":"yêu cầu ngắn"}`,
	}, rejectThinking: true}
	if _, err := DecidePlanStart(t.Context(), m, "sys", "viết một truyện ngắn", ""); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if m.lastCfg.ThinkingLevel != agentcore.ThinkingAuto {
		t.Fatalf("Arbiter không được gửi tham số thinking cho mô hình chat thường, got %q", m.lastCfg.ThinkingLevel)
	}
	if m.lastCfg.MaxTokens != decideMaxTokens {
		t.Fatalf("max_tokens = %d, want %d", m.lastCfg.MaxTokens, decideMaxTokens)
	}
}

func TestDecidePromptContractAppendsSchema(t *testing.T) {
	m := &scriptedModel{outputs: []string{
		`{"planner":"architect_short","task":"lập dàn ý truyện ngắn","reason":"yêu cầu ngắn"}`,
	}}
	const semanticPrompt = "Chỉ căn cứ yêu cầu để chọn cách quy hoạch."
	if _, err := DecidePlanStart(t.Context(), m, semanticPrompt, "viết một truyện ngắn", ""); err != nil {
		t.Fatal(err)
	}
	got := m.lastMsgs[0].TextContent()
	for _, want := range []string{semanticPrompt, "<output-json-schema>", `"planner"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt contract thiếu %q:\n%s", want, got)
		}
	}
}

func (m *scriptedModel) GenerateStream(ctx context.Context, msgs []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	resp, _ := m.Generate(ctx, msgs, tools, opts...)
	ch := make(chan agentcore.StreamEvent, 1)
	ch <- agentcore.StreamEvent{Type: agentcore.StreamEventDone, Message: resp.Message}
	close(ch)
	return ch, nil
}

func (m *scriptedModel) SupportsTools() bool { return true }

type retryableTestError struct {
	retryable bool
}

func (e retryableTestError) Error() string             { return "provider unavailable" }
func (e retryableTestError) Retryable() bool           { return e.retryable }
func (e retryableTestError) RetryAfter() time.Duration { return time.Millisecond }

type failingThenValidModel struct {
	failures int64
	calls    int64
}

func (m *failingThenValidModel) Generate(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	call := atomic.AddInt64(&m.calls, 1)
	if call <= m.failures {
		return nil, retryableTestError{retryable: true}
	}
	return &agentcore.LLMResponse{Message: agentcore.Message{
		Role: agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(
			`{"planner":"architect_short","task":"lập dàn ý truyện ngắn","reason":"yêu cầu ngắn"}`)},
	}}, nil
}

func (m *failingThenValidModel) GenerateStream(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	return nil, errors.New("unused")
}

func (m *failingThenValidModel) SupportsTools() bool { return true }

func TestDecidePlanStart_ValidAndFeedbackRetry(t *testing.T) {
	// Lần đầu sai (planner không hợp lệ), lần sau hợp lệ nhưng có hàng rào — cả thử lại theo phản hồi lẫn trích JSON đều phải chạy.
	m := &scriptedModel{outputs: []string{
		`{"planner":"writer","task":"x","reason":"r"}`,
		"```json\n{\"planner\":\"architect_short\",\"task\":\"viết một truyện ngắn trinh thám 20 chương……\",\"reason\":\"người dùng yêu cầu truyện ngắn\"}\n```",
	}}
	d, err := DecidePlanStart(context.Background(), m, "sys", "truyện ngắn trinh thám 20 chương", "trinh thám")
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if d.Planner != "architect_short" || !strings.Contains(d.Task, "trinh thám") {
		t.Fatalf("phán quyết sai: %+v", d)
	}
	if got := atomic.LoadInt64(&m.idx); got != 2 {
		t.Fatalf("phải đúng 2 lời gọi (1 sai + 1 thử lại thành công), got %d", got)
	}
}

func TestDecide_InvalidOutputContinuesUntilContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &scriptedModel{outputs: []string{"không phải JSON"}, cancel: cancel, cancelAt: 4}
	if _, err := DecidePlanStart(ctx, m, "sys", "yêu cầu", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("vòng tự lành phải dừng theo context, got %v", err)
	}
	if got := atomic.LoadInt64(&m.idx); got != 4 {
		t.Fatalf("phải gọi liên tục trước khi context bị huỷ, got %d", got)
	}
}

func TestDecide_RetryableModelErrorReportsSharedProgress(t *testing.T) {
	m := &failingThenValidModel{failures: 8}
	var progress []agentcore.ProgressPayload
	ctx := agentcore.WithToolProgress(context.Background(), func(p agentcore.ProgressPayload) {
		progress = append(progress, p)
	})

	if _, err := DecidePlanStart(ctx, m, "sys", "yêu cầu", ""); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if got := atomic.LoadInt64(&m.calls); got != 9 {
		t.Fatalf("model calls = %d, want 9", got)
	}
	if len(progress) != 8 || progress[7].Kind != agentcore.ProgressRetry || progress[7].Agent != "arbiter" || progress[7].Attempt != 8 || progress[7].MaxRetries != 0 {
		t.Fatalf("progress = %+v", progress)
	}
}

func TestDecide_NonRetryableModelErrorFailsImmediately(t *testing.T) {
	m := &errorModel{err: retryableTestError{retryable: false}}
	if _, err := DecidePlanStart(context.Background(), m, "sys", "yêu cầu", ""); err == nil {
		t.Fatal("lỗi mô hình không thử lại được phải làm thất bại")
	}
	if got := atomic.LoadInt64(&m.calls); got != 1 {
		t.Fatalf("model calls = %d, want 1", got)
	}
}

type errorModel struct {
	err   error
	calls int64
}

func (m *errorModel) Generate(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	atomic.AddInt64(&m.calls, 1)
	return nil, m.err
}

func (m *errorModel) GenerateStream(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	return nil, m.err
}

func (m *errorModel) SupportsTools() bool { return true }

func TestInterventionDecision_ValidateAgainst(t *testing.T) {
	writing := InterventionFacts{Phase: string(domain.PhaseWriting), CompletedChapters: 10}
	complete := InterventionFacts{Phase: string(domain.PhaseComplete), CompletedChapters: 10}

	cases := []struct {
		name    string
		d       InterventionDecision
		f       InterventionFacts
		wantErr bool
	}{
		{"quyết định rỗng", InterventionDecision{Reason: "r"}, writing, true},
		{"thiếu reason", InterventionDecision{Answer: "được"}, writing, true},
		{"loại tra cứu", InterventionDecision{Answer: "đã xong 10 chương", Reason: "tra cứu"}, writing, false},
		{"tổ hợp làm lại", InterventionDecision{
			Hold:     &AdvanceHoldOp{After: domain.AdvanceHoldAfterRewritesDrained, Reason: "viết lại chương 3 cho lạnh"},
			Dispatch: &DispatchOp{Agent: "editor", Task: "viết lại chương 3: giọng lạnh hơn"},
			Reason:   "sửa chương đã viết",
		}, writing, false},
		{"đích phân công không hợp lệ", InterventionDecision{Dispatch: &DispatchOp{Agent: "coordinator", Task: "x"}, Reason: "r"}, writing, true},
		{"reopen ở giai đoạn sáng tác", InterventionDecision{Reopen: &ReopenOp{Chapters: []int{3}}, Reason: "r"}, writing, true},
		{"reopen ở giai đoạn hoàn thành", InterventionDecision{Reopen: &ReopenOp{Chapters: []int{3}}, Reason: "làm lại"}, complete, false},
		{"reopen vượt phạm vi", InterventionDecision{Reopen: &ReopenOp{Chapters: []int{99}}, Reason: "r"}, complete, true},
		{"phân công trực tiếp ở giai đoạn hoàn thành", InterventionDecision{Dispatch: &DispatchOp{Agent: "writer", Task: "x"}, Reason: "r"}, complete, true},
		{"giai đoạn quy hoạch cấm writer", InterventionDecision{Dispatch: &DispatchOp{Agent: "writer", Task: "viết chương 1"}, Reason: "r"}, InterventionFacts{Phase: string(domain.PhaseOutline)}, true},
		{"giai đoạn quy hoạch cho phép architect", InterventionDecision{Dispatch: &DispatchOp{Agent: "architect_long", Task: "bổ sung dàn ý"}, Reason: "r"}, InterventionFacts{Phase: string(domain.PhaseOutline)}, false},
		{"tạm dừng một lần thiếu điều kiện", InterventionDecision{Hold: &AdvanceHoldOp{Reason: "dừng"}, Reason: "r"}, writing, true},
		{"tạm dừng một lần thiếu tóm tắt", InterventionDecision{Hold: &AdvanceHoldOp{After: domain.AdvanceHoldAtBoundary}, Reason: "r"}, writing, true},
		{"tạm dừng tại chương đích", InterventionDecision{Hold: &AdvanceHoldOp{After: domain.AdvanceHoldAtChapter, TargetChapter: 15, Reason: "viết tới chương 15"}, Reason: "r"}, writing, false},
		{"tạm dừng chưa điền chương đích", InterventionDecision{Hold: &AdvanceHoldOp{After: domain.AdvanceHoldAtChapter, Reason: "viết tới chương đích"}, Reason: "r"}, writing, true},
		{"chương đích đã hoàn thành", InterventionDecision{Hold: &AdvanceHoldOp{After: domain.AdvanceHoldAtChapter, TargetChapter: 10, Reason: "viết tới chương 10"}, Reason: "r"}, writing, true},
		{"tạm dừng không theo chương lại mang chương", InterventionDecision{Hold: &AdvanceHoldOp{After: domain.AdvanceHoldAtBoundary, TargetChapter: 15, Reason: "dừng"}, Reason: "r"}, writing, true},
		{"huỷ tạm dừng một lần", InterventionDecision{Hold: &AdvanceHoldOp{Cancel: true}, Answer: "tiếp tục", Reason: "r"}, writing, false},
		{"đặt tạm dừng một lần ở giai đoạn hoàn thành", InterventionDecision{Hold: &AdvanceHoldOp{After: domain.AdvanceHoldAtBoundary, Reason: "dừng"}, Reason: "r"}, complete, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.d.ValidateAgainst(tc.f)
			if (err != nil) != tc.wantErr {
				t.Fatalf("wantErr=%v got %v", tc.wantErr, err)
			}
		})
	}
}

func TestDecideInterventionAcceptsTargetChapterHold(t *testing.T) {
	m := &scriptedModel{outputs: []string{`{
		"answer":"sẽ viết liên tục tới chương 15 rồi dừng",
		"rules":null,
		"hold":{"cancel":false,"after":"chapter","target_chapter":15,"reason":"viết tới chương 15 rồi dừng"},
		"reopen":null,
		"dispatch":null,
		"reason":"người dùng chỉ định điểm dừng một lần"
	}`}}
	d, err := DecideIntervention(t.Context(), m, "sys", InterventionFacts{
		Phase: string(domain.PhaseWriting), CompletedChapters: 10, NextChapter: 11,
	}, "viết tới chương 15")
	if err != nil {
		t.Fatal(err)
	}
	if d.Hold == nil || d.Hold.After != domain.AdvanceHoldAtChapter || d.Hold.TargetChapter != 15 {
		t.Fatalf("hold chương đích giải mã sai: %+v", d.Hold)
	}
}

func TestFailureDecision_Validate(t *testing.T) {
	facts := FailureFacts{Kind: "worker_failure", Phase: string(domain.PhaseWriting)}
	ok := FailureDecision{Action: "reroute", Dispatch: &DispatchOp{Agent: "architect_long", Task: "expand_arc trước đã"}, Reason: "lỗi cần chỉ ra lối thoát"}
	if err := ok.ValidateAgainst(facts); err != nil {
		t.Fatalf("reroute hợp lệ bị từ chối: %v", err)
	}
	bad := FailureDecision{Action: "reroute", Reason: "r"}
	if err := bad.ValidateAgainst(facts); err == nil {
		t.Fatal("reroute thiếu dispatch phải bị từ chối")
	}
	if err := (&FailureDecision{Action: "escalate", Reason: "r"}).ValidateAgainst(facts); err == nil {
		t.Fatal("action không hợp lệ phải bị từ chối")
	}
	planning := FailureFacts{Kind: "worker_failure", Phase: string(domain.PhaseOutline)}
	writer := FailureDecision{Action: "reroute", Dispatch: &DispatchOp{Agent: "writer", Task: "viết chương 1"}, Reason: "cố vòng qua quy hoạch"}
	if err := writer.ValidateAgainst(planning); err == nil {
		t.Fatal("phán quyết thất bại không được phân công writer ở giai đoạn quy hoạch")
	}
}

func TestCollectInterventionFacts(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := st.Progress.Init(30); err != nil {
		t.Fatalf("progress: %v", err)
	}
	if err := st.Book.Save(domain.BookMetadata{Title: "sách thử nghiệm", Synopsis: "tóm tắt thử nghiệm"}); err != nil {
		t.Fatalf("book: %v", err)
	}
	if err := st.RunMeta.Init("default", "openrouter", "m"); err != nil {
		t.Fatalf("run meta: %v", err)
	}
	if err := st.RunMeta.SetAdvanceMode(domain.ChapterAdvanceReview); err != nil {
		t.Fatalf("advance mode: %v", err)
	}
	if err := st.RunMeta.SetAdvanceHold(domain.AdvanceHold{After: domain.AdvanceHoldAtChapter, TargetChapter: 20, Reason: "viết tới chương 20"}); err != nil {
		t.Fatalf("advance hold: %v", err)
	}
	if _, err := st.Decisions.Append(storepkg.DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: "can thiệp trước", Reason: "đã vào hàng đợi"}); err != nil {
		t.Fatalf("append decision: %v", err)
	}

	f, err := CollectInterventionFacts(st)
	if err != nil {
		t.Fatalf("CollectInterventionFacts: %v", err)
	}
	if f.Title != "sách thử nghiệm" {
		t.Fatalf("facts phải có tên sách, got %+v", f)
	}
	if len(f.RecentDecisions) != 1 || f.RecentDecisions[0].Input != "can thiệp trước" {
		t.Fatalf("ký ức can thiệp thiếu: %+v", f.RecentDecisions)
	}
	if f.AdvanceMode != string(domain.ChapterAdvanceReview) || !f.HasAdvanceHold || f.AdvanceHoldAfter != string(domain.AdvanceHoldAtChapter) || f.AdvanceHoldTargetChapter != 20 {
		t.Fatalf("thiếu sự thật điều khiển tiến độ: %+v", f)
	}
	if len(f.FoundationMissing) == 0 {
		t.Fatal("sách mới phải còn các mục thiết lập nền tảng bị thiếu")
	}

	// /reopen là sự thật có thể liệt kê, bắt buộc phải vào facts: sau khi mở
	// lại, số chương đã viết đã đầy, thiếu nó thì model sẽ suy ra "đã hoàn thành"
	// từ completed=total và bỏ qua phase=writing (sự cố đã đo được).
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("phase: %v", err)
	}
	if err := st.Progress.MarkComplete(); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := st.Progress.ReopenContinue(); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	f, err = CollectInterventionFacts(st)
	if err != nil {
		t.Fatalf("CollectInterventionFacts after reopen: %v", err)
	}
	if f.ReopenCount != 1 || f.Phase != string(domain.PhaseWriting) {
		t.Fatalf("thiếu sự thật mở lại: phase=%s reopen_count=%d", f.Phase, f.ReopenCount)
	}
}

func TestCollectInterventionFactsDoesNotExposeLayeredEstimateAsTotal(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.Init(66); err != nil {
		t.Fatal(err)
	}
	volumes := []domain.VolumeOutline{{
		Index: 1, Title: "tập một", Arcs: []domain.ArcOutline{
			{Index: 1, Title: "cung hiện tại", Chapters: []domain.OutlineEntry{{Title: "một"}, {Title: "hai"}}},
			{Index: 2, Title: "cung khung", EstimatedChapters: 64},
		},
	}}
	if err := st.Outline.SaveLayeredOutline(volumes); err != nil {
		t.Fatal(err)
	}
	p, err := st.Progress.Load()
	if err != nil {
		t.Fatal(err)
	}
	p.Layered = true
	if err := st.Progress.Save(p); err != nil {
		t.Fatal(err)
	}

	facts, err := CollectInterventionFacts(st)
	if err != nil {
		t.Fatal(err)
	}
	if !facts.DynamicPlanning || facts.OutlinedChapters != 2 {
		t.Fatalf("sự thật quy hoạch động sai: %+v", facts)
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "total_chapters") || strings.Contains(string(raw), `:66`) {
		t.Fatalf("ước tính nội bộ không được vào Arbiter như tổng số chương: %s", raw)
	}
}

func TestExtractJSON(t *testing.T) {
	cases := map[string]string{
		`{"a":1}`: `{"a":1}`,
		"tiền tố ```json\n{\"a\":\"}\"}\n```": `{"a":"}"}`, // dấu ngoặc nhọn trong chuỗi không làm lệch cân bằng
		"không có object":                     "",
		`{"nested":{"b":2},"c":3} đuôi`:       `{"nested":{"b":2},"c":3}`,
	}
	for in, want := range cases {
		if got := llmcontract.ExtractJSONObject(in); got != want {
			t.Errorf("extractJSON(%q) = %q, want %q", in, got, want)
		}
	}
}

// nativeModel khai báo hỗ trợ JSON Schema gốc: decide phải đi nhánh native.
type nativeModel struct {
	*scriptedModel
	stop agentcore.StopReason
}

func (m *nativeModel) Capabilities() llm.Capabilities {
	return llm.Capabilities{
		Provider:   "openai",
		Model:      "gpt-test",
		Structured: llm.StructuredCapabilities{JSONSchema: llm.SupportYes, Strict: llm.SupportYes},
	}
}

func (m *nativeModel) Generate(ctx context.Context, msgs []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	resp, err := m.scriptedModel.Generate(ctx, msgs, tools, opts...)
	if resp != nil && m.stop != "" {
		resp.Message.StopReason = m.stop
	}
	return resp, err
}

// Kiểm thử hợp đồng (RFC §11.1): gốc là object, mọi thuộc tính (kể cả lồng) required, dispatch là đối tượng có thể null.
func TestContractSchemasAreStrictReady(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.LangVI, i18n.LangZH} {
		for _, c := range []llmcontract.Contract{
			planStartContractFor(lang),
			failureContractFor(lang),
			interventionContractFor(lang),
		} {
			if c.Schema["type"] != "object" {
				t.Fatalf("%s: gốc phải là object", c.Name)
			}
			if err := llmcontract.ValidateStrictReady(c.Schema); err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
			if len(c.Fingerprint()) != 12 {
				t.Fatalf("%s: vân tay bất thường: %q", c.Name, c.Fingerprint())
			}
		}
		dispatch := failureContractFor(lang).Schema["properties"].(map[string]any)["dispatch"].(map[string]any)
		types, ok := dispatch["type"].([]string)
		if !ok || !slices.Contains(types, "null") || !slices.Contains(types, "object") {
			t.Fatalf("dispatch phải là đối tượng có thể null: %v", dispatch["type"])
		}
	}
	var d FailureDecision
	if err := json.Unmarshal([]byte(`{"action":"retry","dispatch":null,"reason":"lỗi tạm thời"}`), &d); err != nil {
		t.Fatalf("mẫu có dispatch:null phải giải mã được: %v", err)
	}
	if err := d.ValidateAgainst(FailureFacts{Phase: "writing"}); err != nil {
		t.Fatalf("mẫu phải qua kiểm tra: %v", err)
	}
}

func TestDecideNativeSendsSchemaAndDecodesFullOutput(t *testing.T) {
	m := &nativeModel{scriptedModel: &scriptedModel{outputs: []string{
		`{"planner":"architect_short","task":"lập dàn ý truyện ngắn","reason":"yêu cầu ngắn"}`,
	}}}
	const semanticPrompt = "Chỉ căn cứ yêu cầu để chọn cách quy hoạch."
	d, err := DecidePlanStart(t.Context(), m, semanticPrompt, "viết một truyện ngắn", "")
	if err != nil || d.Planner != "architect_short" {
		t.Fatalf("phán quyết native thất bại: %+v %v", d, err)
	}
	rf := m.lastCfg.ResponseFormat
	if rf == nil || rf.Type != agentcore.ResponseFormatJSONSchema || rf.JSONSchema == nil {
		t.Fatalf("chế độ native phải gửi response_format: %+v", rf)
	}
	if rf.JSONSchema.Name != "arbiter_plan_start" || rf.JSONSchema.Strict == nil || !*rf.JSONSchema.Strict {
		t.Fatalf("tham số schema không khớp: %+v", rf.JSONSchema)
	}
	if got := m.lastMsgs[0].TextContent(); got != semanticPrompt {
		t.Fatalf("chế độ native không được chèn schema vào prompt:\n%s", got)
	}
}

// Chế độ native mà giải mã hỏng = provider vi phạm hợp đồng: báo lỗi ngay, không dùng extractJSON và không hỏi lại.
func TestDecideNativeFencedOutputIsContractViolation(t *testing.T) {
	m := &nativeModel{scriptedModel: &scriptedModel{outputs: []string{
		"```json\n{\"planner\":\"architect_short\",\"task\":\"x\",\"reason\":\"y\"}\n```",
	}}}
	_, err := DecidePlanStart(t.Context(), m, "sys", "viết một truyện ngắn", "")
	if err == nil || !strings.Contains(err.Error(), "vi phạm hợp đồng") {
		t.Fatalf("cần lỗi vi phạm hợp đồng, got %v", err)
	}
	if m.idx != 1 {
		t.Fatalf("vi phạm hợp đồng không được hỏi lại, đã gọi %d lần", m.idx)
	}
}

// Chế độ native mà kiểm tra nghiệp vụ hỏng vẫn phản hồi để hỏi lại, và lượt hỏi lại giữ nguyên schema.
func TestDecideNativeValidateFailureFeedbackKeepsSchema(t *testing.T) {
	m := &nativeModel{scriptedModel: &scriptedModel{outputs: []string{
		`{"action":"reroute","dispatch":null,"reason":"cần đổi hướng"}`,
		`{"action":"retry","dispatch":null,"reason":"lỗi tạm thời có thể thử lại"}`,
	}}}
	d, err := DecideFailure(t.Context(), m, "sys", FailureFacts{Kind: "worker_failure", Phase: "writing"})
	if err != nil || d.Action != "retry" {
		t.Fatalf("hỏi lại theo phản hồi phải thành công: %+v %v", d, err)
	}
	if m.idx != 2 {
		t.Fatalf("phải đúng hai lời gọi, got %d", m.idx)
	}
	if m.lastCfg.ResponseFormat == nil {
		t.Fatal("lượt hỏi lại đã mất schema")
	}
}

// Chế độ native phân loại lý do dừng trước: cắt ngắn/từ chối/nội dung rỗng là sự thật lỗi riêng, không vào vòng hỏi lại.
func TestDecideNativeStopReasonClassification(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		stop    agentcore.StopReason
		wantErr string
	}{
		{"length cắt ngắn", `{"planner":`, agentcore.StopReasonLength, "giới hạn độ dài"},
		{"safety từ chối", `không thể hỗ trợ`, agentcore.StopReasonSafety, "từ chối"},
		{"nội dung rỗng", ``, agentcore.StopReasonStop, "nội dung rỗng"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &nativeModel{scriptedModel: &scriptedModel{outputs: []string{tc.output}}, stop: tc.stop}
			_, err := DecidePlanStart(t.Context(), m, "sys", "viết một truyện ngắn", "")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("cần lỗi %q, got %v", tc.wantErr, err)
			}
			if m.idx != 1 {
				t.Fatalf("lý do dừng không được hỏi lại, đã gọi %d lần", m.idx)
			}
		})
	}
}

// marshalPayload hỏng phải phơi bày: tự bịa {} sẽ khiến model phán đoán trên sự thật giả.
func TestMarshalPayloadErrors(t *testing.T) {
	if _, err := marshalPayload(func() {}); err == nil {
		t.Fatal("tải không serialize được phải báo lỗi")
	}
	s, err := marshalPayload(map[string]int{"a": 1})
	if err != nil || !strings.Contains(s, `"a"`) {
		t.Fatalf("tải bình thường: %q %v", s, err)
	}
}

// setActiveLang đặt ngôn ngữ tác phẩm ở cấp tiến trình và tự khôi phục sau
// test. Không dùng t.Parallel() vì biến này là trạng thái toàn cục.
func setActiveLang(t *testing.T, lang string) {
	t.Helper()
	previous := i18n.Active()
	i18n.SetActive(lang)
	t.Cleanup(func() { i18n.SetActive(string(previous)) })
}

// TestPlanStartPayloadFollowsWorkLanguage kiểm tra hợp đồng và nhãn schema của
// kịch bản khởi chạy đổi theo ngôn ngữ tác phẩm.
func TestPlanStartPayloadFollowsWorkLanguage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		lang       string
		wantSchema string
		wantTask   string
	}{
		{"tiếng Việt", "vi", "Hợp đồng đầu ra", "Nhiệm vụ đầy đủ giao cho kiến trúc sư"},
		{"tiếng Trung", "zh", "输出契约", "交给规划师的完整任务"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setActiveLang(t, tc.lang)
			m := &scriptedModel{outputs: []string{
				`{"planner":"architect_short","task":"t","reason":"r"}`,
			}}
			if _, err := DecidePlanStart(t.Context(), m, "sys", "yêu cầu", ""); err != nil {
				t.Fatal(err)
			}
			prompt := m.lastMsgs[0].TextContent()
			if !strings.Contains(prompt, tc.wantSchema) {
				t.Fatalf("prompt thiếu hợp đồng đầu ra đúng ngôn ngữ:\n%s", prompt)
			}
			if !strings.Contains(prompt, tc.wantTask) {
				t.Fatalf("prompt thiếu nhãn task đúng ngôn ngữ:\n%s", prompt)
			}
			// Tải người dùng vẫn là JSON sự thật, không phụ thuộc ngôn ngữ.
			if !strings.Contains(m.lastMsgs[1].TextContent(), `"requirement"`) {
				t.Fatalf("tải người dùng thiếu trường requirement:\n%s", m.lastMsgs[1].TextContent())
			}
		})
	}
}

// TestDispatchTaskSchemaFollowsWorkLanguage kiểm tra nhãn của trường dispatch
// (đích phân công + mô tả nhiệm vụ) đổi theo ngôn ngữ tác phẩm, vì đây là
// mô tả mà model đọc để biết mình phải làm gì.
func TestDispatchTaskSchemaFollowsWorkLanguage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lang     string
		wantTask string
	}{
		{"tiếng Việt", "vi", "Mô tả nhiệm vụ đầy đủ giao cho worker này"},
		{"tiếng Trung", "zh", "交给该 worker 的完整任务描述"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setActiveLang(t, tc.lang)
			m := &scriptedModel{outputs: []string{
				`{"action":"reroute","dispatch":{"agent":"writer","task":"t"},"reason":"r"}`,
			}}
			if _, err := DecideFailure(t.Context(), m, "sys", FailureFacts{Kind: "worker_failure", Phase: "writing"}); err != nil {
				t.Fatal(err)
			}
			prompt := m.lastMsgs[0].TextContent()
			if !strings.Contains(prompt, tc.wantTask) {
				t.Fatalf("prompt thiếu nhãn nhiệm vụ phân công đúng ngôn ngữ:\n%s", prompt)
			}
		})
	}
}

// TestPlanStartContractDescriptionFollowsWorkLanguage kiểm tra mô tả hợp
// đồng — vốn được đưa vào response_format của provider — đổi theo ngôn ngữ.
func TestPlanStartContractDescriptionFollowsWorkLanguage(t *testing.T) {
	for _, tc := range []struct {
		name string
		lang string
		want string
	}{
		{"tiếng Việt", "vi", "Phán quyết khởi chạy"},
		{"tiếng Trung", "zh", "启动裁定"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setActiveLang(t, tc.lang)
			contract := planStartContractFor(i18n.Active())
			if !strings.Contains(contract.Description, tc.want) {
				t.Fatalf("mô tả hợp đồng sai ngôn ngữ: %q", contract.Description)
			}
		})
	}
}
