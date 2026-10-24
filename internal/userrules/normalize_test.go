package userrules

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/llm"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
)

func TestExtractJSON_StripsCodeFences(t *testing.T) {
	cases := []struct{ in, wantHas string }{
		{"```json\n{\"a\":1}\n```", `"a":1`},
		{"```\n{\"a\":1}\n```", `"a":1`},
		{"tiền tố giải thích\n{\"a\":1}\nhậu tố", `"a":1`},
		{"{\"a\":1}", `"a":1`},
	}
	for _, c := range cases {
		got := llmcontract.ExtractJSONObject(c.in)
		if got == "" {
			t.Fatalf("extractJSON(%q) rỗng", c.in)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(got), &m); err != nil {
			t.Fatalf("extractJSON(%q)=%q không phải JSON hợp lệ: %v", c.in, got, err)
		}
	}
	if llmcontract.ExtractJSONObject("không có JSON nào") != "" {
		t.Fatal("không có JSON thì phải trả chuỗi rỗng")
	}
}

func TestParseNormalizerJSON_FullOutput(t *testing.T) {
	raw := "```json\n" + `{
  "structured": {
    "genre": "đô thị",
    "forbidden_chars": [],
    "forbidden_phrases": ["đôi khi"],
    "fatigue_words": [{"word": "vậy mà", "max_per_chapter": 2}]
  },
  "preferences": "nhân vật chính lạnh lùng kiềm chế",
  "uncertain": ["bớt dùng ẩn dụ: không có ngưỡng"]
}` + "\n```"
	body := llmcontract.ExtractJSONObject(raw)
	if err := llmcontract.ValidateJSON(normalizeContractFor(i18n.Active()).Schema, []byte(body)); err != nil {
		t.Fatalf("phải phân tích được: %v", err)
	}
	var out normalizerOutput
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("phải giải mã được: %v", err)
	}
	cand, err := out.toCandidate("startup_prompt", ruleTextsFor(i18n.Active()))
	if err != nil {
		t.Fatalf("toCandidate: %v", err)
	}
	if cand.Structured.Genre != "đô thị" {
		t.Fatalf("genre phân tích sai: %+v", cand.Structured)
	}
	if len(cand.Structured.ForbiddenPhrases) != 1 || cand.Structured.ForbiddenPhrases[0] != "đôi khi" {
		t.Fatalf("forbidden_phrases phân tích sai: %v", cand.Structured.ForbiddenPhrases)
	}
	if cand.Structured.FatigueWords["vậy mà"] != 2 {
		t.Fatalf("mảng fatigue_words phải chuyển thành map: %v", cand.Structured.FatigueWords)
	}
	if cand.Preferences != "nhân vật chính lạnh lùng kiềm chế" {
		t.Fatalf("preferences phân tích sai: %q", cand.Preferences)
	}
	if len(cand.Uncertain) != 1 {
		t.Fatalf("uncertain phải có 1 mục, got %v", cand.Uncertain)
	}
}

// Kiểm tra mục fatigue: từ rỗng và ngưỡng không phải số nguyên dương đều là lỗi nghiệp vụ có thể phản hồi để model sửa.
func TestToCandidateRejectsInvalidFatigueEntries(t *testing.T) {
	bad := normalizerOutput{Structured: normalizerStructured{
		FatigueWords: []fatigueWordEntry{{Word: " ", MaxPerChapter: 2}},
	}}
	if _, err := bad.toCandidate("x", ruleTextsFor(i18n.Active())); err == nil {
		t.Fatal("mục từ rỗng phải báo lỗi")
	}
	bad = normalizerOutput{Structured: normalizerStructured{
		FatigueWords: []fatigueWordEntry{{Word: "vậy mà", MaxPerChapter: 0}},
	}}
	if _, err := bad.toCandidate("x", ruleTextsFor(i18n.Active())); err == nil {
		t.Fatal("ngưỡng không phải số nguyên dương phải báo lỗi")
	}
}

func TestParseNormalizerJSON_GarbageFails(t *testing.T) {
	if body := llmcontract.ExtractJSONObject("mô hình chỉ trả lời một câu, không có JSON"); body != "" {
		t.Fatal("không có JSON thì phải phân tích hỏng (kích hoạt hạ cấp)")
	}
	if body := llmcontract.ExtractJSONObject("{ không hoàn chỉnh"); body != "" {
		t.Fatal("JSON thiếu phải phân tích hỏng")
	}
}

// Kiểm thử hợp đồng (RFC §11.1): gốc là object, mọi thuộc tính (kể cả structured/fatigue_words lồng) required.
// Hợp đồng dựng theo cả hai ngôn ngữ nên cả hai nhánh đều phải thoả.
func TestNormalizeContractIsStrictReady(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.LangVI, i18n.LangZH} {
		contract := normalizeContractFor(lang)
		if contract.Schema["type"] != "object" {
			t.Fatalf("%s: gốc phải là object", lang)
		}
		if err := llmcontract.ValidateStrictReady(contract.Schema); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
	}
}

func TestNormalize_NilModelErrors(t *testing.T) {
	// Không có mô hình: trả về lỗi rõ ràng, lớp Service sẽ hạ cấp thành raw preferences.
	var n *Normalizer = NewNormalizer(nil)
	if _, err := n.Normalize(t.Context(), "startup_prompt", "mỗi chương 1200 chữ, nhân vật chính lạnh lùng"); err == nil {
		t.Fatal("không có mô hình thì phải trả lỗi")
	}
}

// scriptedModel là fake ChatModel tối thiểu: trả về các câu trả lời định sẵn theo thứ tự lời gọi và ghi lại messages của lượt cuối, để khẳng định việc thử lại theo phản hồi có ghém câu nhắc sửa vào lượt kế tiếp hay không. Hết câu trả lời thì lặp lại câu cuối.
type scriptedModel struct {
	replies  []string
	calls    int
	lastMsgs []agentcore.Message
	lastCfg  agentcore.CallConfig
	err      error // Khi khác nil, Generate luôn trả về lỗi này
	cancel   context.CancelFunc
	cancelAt int
}

func (m *scriptedModel) Generate(_ context.Context, messages []agentcore.Message, _ []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	var cfg agentcore.CallConfig
	for _, o := range opts {
		o(&cfg)
	}
	m.lastCfg = cfg
	m.lastMsgs = messages
	m.calls++
	if m.cancel != nil && m.cancelAt > 0 && m.calls >= m.cancelAt {
		m.cancel()
	}
	if m.err != nil {
		return nil, m.err
	}
	i := m.calls - 1
	if i >= len(m.replies) {
		i = len(m.replies) - 1
	}
	return &agentcore.LLMResponse{Message: agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(m.replies[i])},
	}}, nil
}

func (m *scriptedModel) GenerateStream(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	return nil, nil
}

func (m *scriptedModel) SupportsTools() bool { return false }

// Thử lại theo phản hồi: lượt đầu trả JSON hỏng, lượt sau mới hợp lệ. Normalize phải thành công và lượt sau phải mang theo đầu ra hỏng của lượt trước cùng câu nhắc sửa (phản hồi chứ không phải thử lại mù).
func TestNormalize_FeedbackRetryRecovers(t *testing.T) {
	model := &scriptedModel{replies: []string{
		"đây không phải JSON",
		`{"structured":{"genre":"","forbidden_chars":[],"forbidden_phrases":["đôi khi"],"fatigue_words":[]},"preferences":"","uncertain":[]}`,
	}}
	n := NewNormalizer(model)

	cand, err := n.Normalize(t.Context(), "startup_prompt", "不要出现某种程度上")
	if err != nil {
		t.Fatalf("lượt sau đã trả JSON hợp lệ, không được thất bại: %v", err)
	}
	if len(cand.Structured.ForbiddenPhrases) != 1 {
		t.Fatalf("phải phân tích ra forbidden_phrases, got %+v", cand.Structured)
	}
	if model.calls != 2 {
		t.Fatalf("phải thành công ở lượt 2, thực tế gọi %d lần", model.calls)
	}

	var sawBad, sawHint bool
	for _, msg := range model.lastMsgs {
		text := msg.TextContent()
		if text == "đây không phải JSON" {
			sawBad = true
		}
		if strings.Contains(text, "JSON Schema") &&
			(strings.Contains(text, "Lỗi:") || strings.Contains(text, "错误：")) {
			sawHint = true
		}
	}
	if !sawBad || !sawHint {
		t.Errorf("lượt sau phải ghém đầu ra hỏng và câu nhắc sửa, sawBad=%v sawHint=%v", sawBad, sawHint)
	}
	system := model.lastMsgs[0].TextContent()
	if !strings.Contains(system, "<output-json-schema>") || !strings.Contains(system, `"fatigue_words"`) {
		t.Fatalf("prompt contract phải tự gắn schema từ Contract:\n%s", system)
	}
}

// Chuẩn hóa không ghi đè mặc định thinking của mô hình; mô hình chat thường sẽ từ chối giá trị off tường minh.
func TestNormalize_LeavesThinkingUnspecifiedAndReservesTokens(t *testing.T) {
	model := &scriptedModel{replies: []string{`{"structured":{"genre":"","forbidden_chars":[],"forbidden_phrases":[],"fatigue_words":[]},"preferences":"x","uncertain":[]}`}}
	n := NewNormalizer(model)

	if _, err := n.Normalize(t.Context(), "startup_prompt", "một quy tắc bất kỳ"); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if model.lastCfg.ThinkingLevel != agentcore.ThinkingAuto {
		t.Errorf("không được gửi tham số thinking, got %q", model.lastCfg.ThinkingLevel)
	}
	if model.lastCfg.MaxTokens != normalizeMaxTokens {
		t.Errorf("max_tokens phải là %d, got %d", normalizeMaxTokens, model.lastCfg.MaxTokens)
	}
}

// JSON hỏng suốt: không có trần số lần cố định, cứ phản hồi hỏi lại cho tới khi context bị huỷ.
func TestNormalize_FeedbackRetryContinuesUntilContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	model := &scriptedModel{replies: []string{"hỏng"}, cancel: cancel, cancelAt: 4}
	n := NewNormalizer(model)

	_, err := n.Normalize(ctx, "startup_prompt", "mỗi chương 1200 chữ")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("vòng tự lành phải dừng theo context, got %v", err)
	}
	if model.calls != 4 {
		t.Fatalf("phải gọi liên tục trước khi context bị huỷ, thực tế %d", model.calls)
	}
}

type terminalTestError struct{}

func (terminalTestError) Error() string   { return "401 authentication failed" }
func (terminalTestError) Retryable() bool { return false }

type retryableTestError struct{}

func (retryableTestError) Error() string             { return "provider unavailable" }
func (retryableTestError) Retryable() bool           { return true }
func (retryableTestError) RetryAfter() time.Duration { return time.Millisecond }

// Lỗi dừng (401…) không được thử lại mù: đúng 1 lời gọi rồi trả lỗi.
func TestNormalize_TerminalErrorStopsImmediately(t *testing.T) {
	model := &scriptedModel{err: terminalTestError{}}
	n := NewNormalizer(model)

	_, err := n.Normalize(t.Context(), "startup_prompt", "quy tắc")
	if err == nil || !errors.As(err, &terminalTestError{}) {
		t.Fatalf("phải lộ lỗi dừng: %v", err)
	}
	if model.calls != 1 {
		t.Fatalf("lỗi dừng không được thử lại, thực tế gọi %d lần", model.calls)
	}
}

// Lỗi yêu cầu có thể thử lại được do llmretry lùi bước rồi thử lại.
type flakyModel struct {
	scriptedModel
	failures int
}

func (m *flakyModel) Generate(ctx context.Context, msgs []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	if m.scriptedModel.calls < m.failures {
		m.scriptedModel.calls++
		return nil, retryableTestError{}
	}
	return m.scriptedModel.Generate(ctx, msgs, tools, opts...)
}

func TestNormalize_RetryableErrorRecovers(t *testing.T) {
	model := &flakyModel{
		scriptedModel: scriptedModel{replies: []string{`{"structured":{"genre":"","forbidden_chars":[],"forbidden_phrases":[],"fatigue_words":[]},"preferences":"x","uncertain":[]}`}},
		failures:      2,
	}
	n := NewNormalizer(model)
	cand, err := n.Normalize(t.Context(), "startup_prompt", "quy tắc")
	if err != nil || cand.Preferences != "x" {
		t.Fatalf("sau khi lùi bước phải thành công: %+v %v", cand, err)
	}
}

// nativeRulesModel khai báo hỗ trợ JSON Schema gốc.
type nativeRulesModel struct {
	*scriptedModel
}

func (m *nativeRulesModel) Capabilities() llm.Capabilities {
	return llm.Capabilities{
		Provider:   "openai",
		Model:      "gpt-test",
		Structured: llm.StructuredCapabilities{JSONSchema: llm.SupportYes, Strict: llm.SupportYes},
	}
}

func TestNormalize_NativeSendsSchemaAndRejectsFences(t *testing.T) {
	// Chế độ native: schema đi kèm yêu cầu; JSON trần thành công.
	model := &nativeRulesModel{&scriptedModel{replies: []string{
		`{"structured":{"genre":"","forbidden_chars":[],"forbidden_phrases":[],"fatigue_words":[]},"preferences":"x","uncertain":[]}`,
	}}}
	n := NewNormalizer(model)
	cand, err := n.Normalize(t.Context(), "startup_prompt", "quy tắc")
	if err != nil || cand.Preferences != "x" {
		t.Fatalf("chuẩn hóa native thất bại: %+v %v", cand, err)
	}
	rf := model.lastCfg.ResponseFormat
	if rf == nil || rf.JSONSchema == nil || rf.JSONSchema.Name != "userrules_normalize" {
		t.Fatalf("chế độ native phải gửi schema: %+v", rf)
	}
	if got := model.lastMsgs[0].TextContent(); got != ruleTextsFor(i18n.Active()).systemPrompt {
		t.Fatalf("chế độ native không được chèn schema vào prompt:\n%s", got)
	}

	// Đầu ra có hàng rào = vi phạm hợp đồng: báo lỗi ngay, không dùng extractJSON, không hỏi lại.
	fenced := &nativeRulesModel{&scriptedModel{replies: []string{
		"```json\n{\"structured\":{},\"preferences\":\"x\",\"uncertain\":[]}\n```",
	}}}
	n = NewNormalizer(fenced)
	_, err = n.Normalize(t.Context(), "startup_prompt", "quy tắc")
	if err == nil || !strings.Contains(err.Error(), "vi phạm hợp đồng") {
		t.Fatalf("cần lỗi vi phạm hợp đồng, got %v", err)
	}
	if fenced.calls != 1 {
		t.Fatalf("vi phạm hợp đồng không được hỏi lại, thực tế %d lần", fenced.calls)
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

// TestNormalizePromptFollowsWorkLanguage kiểm tra prompt hệ thống và hợp đồng
// của bộ chuẩn hóa đổi theo ngôn ngữ tác phẩm — đây là chỉ dẫn model đọc, nên
// phải đúng ngôn ngữ đang viết.
func TestNormalizePromptFollowsWorkLanguage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lang     string
		wantHead string
		wantTask string
	}{
		{"tiếng Việt", "vi", "Trình chuẩn hóa quy tắc", "Nâng lên có thận trọng"},
		{"tiếng Trung", "zh", "规则归一化器", "保守提升"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setActiveLang(t, tc.lang)
			model := &scriptedModel{replies: []string{
				`{"structured":{"genre":"","forbidden_chars":[],"forbidden_phrases":[],"fatigue_words":[]},"preferences":"x","uncertain":[]}`,
			}}
			n := NewNormalizer(model)
			if _, err := n.Normalize(t.Context(), "startup_prompt", "quy tắc"); err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			system := model.lastMsgs[0].TextContent()
			if !strings.Contains(system, tc.wantHead) {
				t.Fatalf("prompt hệ thống thiếu phần đúng ngôn ngữ:\n%s", system)
			}
			if !strings.Contains(system, tc.wantTask) {
				t.Fatalf("prompt hệ thống thiếu phần đúng ngôn ngữ:\n%s", system)
			}
			if !strings.Contains(system, "<output-json-schema>") {
				t.Fatalf("prompt contract phải gắn schema:\n%s", system)
			}
		})
	}
}

// TestNormalizeContractDescriptionFollowsWorkLanguage kiểm tra mô tả hợp
// đồng — vốn được đưa vào response_format — đổi theo ngôn ngữ tác phẩm.
func TestNormalizeContractDescriptionFollowsWorkLanguage(t *testing.T) {
	for _, tc := range []struct {
		name string
		lang i18n.Lang
		want string
	}{
		{"tiếng Việt", i18n.LangVI, "Chuẩn hóa quy tắc viết bằng ngôn ngữ tự nhiên"},
		{"tiếng Trung", i18n.LangZH, "把用户自然语言写作规则归一化为结构化字段"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contract := normalizeContractFor(tc.lang)
			if !strings.Contains(contract.Description, tc.want) {
				t.Fatalf("mô tả hợp đồng sai ngôn ngữ: %q", contract.Description)
			}
		})
	}
}
