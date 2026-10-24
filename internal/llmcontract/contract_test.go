package llmcontract

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/llm"
	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

type baseModel struct{}

func (baseModel) Generate(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	return nil, nil
}

func (baseModel) GenerateStream(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	return nil, nil
}

func (baseModel) SupportsTools() bool { return true }

type capsModel struct {
	baseModel
	caps llm.Capabilities
}

func (m capsModel) Capabilities() llm.Capabilities { return m.caps }

type overrideModel struct {
	capsModel
	override *bool
}

func (m overrideModel) JSONSchemaOverride() *bool { return m.override }

type infoModel struct {
	baseModel
	info llm.ModelInfo
}

func (m infoModel) Info() llm.ModelInfo { return m.info }

type factsModel struct {
	baseModel
	facts ModelFacts
}

func (m factsModel) StructuredOutputFacts() ModelFacts { return m.facts }

func structured(jsonSchema, strict llm.Support) llm.Capabilities {
	return llm.Capabilities{
		Provider:   "prov",
		Model:      "mod",
		Structured: llm.StructuredCapabilities{JSONSchema: jsonSchema, Strict: strict},
	}
}

func boolPtr(v bool) *bool { return &v }

// setActiveLang đặt ngôn ngữ tác phẩm ở cấp tiến trình và tự khôi phục
// sau test. Không dùng t.Parallel() vì biến này là trạng thái toàn cục.
func setActiveLang(t *testing.T, lang string) {
	t.Helper()
	previous := i18n.Active()
	i18n.SetActive(lang)
	t.Cleanup(func() { i18n.SetActive(string(previous)) })
}

// TestResolveMatrix phủ toàn bộ tổ hợp ba trạng thái của cấu hình × khả năng
// của adapter.
func TestResolveMatrix(t *testing.T) {
	cases := []struct {
		name       string
		model      agentcore.ChatModel
		wantMode   Mode
		wantSource Source
		wantStrict bool
	}{
		{"không có giao diện khả năng nào", baseModel{}, ModePromptContract, SourceUnknown, false},
		{"adapter yes + strict yes", capsModel{caps: structured(llm.SupportYes, llm.SupportYes)}, ModeNativeJSONSchema, SourceAdapter, true},
		{"adapter yes + strict no (hình thức Gemini)", capsModel{caps: structured(llm.SupportYes, llm.SupportNo)}, ModeNativeJSONSchema, SourceAdapter, false},
		{"adapter yes + strict unknown", capsModel{caps: structured(llm.SupportYes, llm.SupportUnknown)}, ModeNativeJSONSchema, SourceAdapter, false},
		{"adapter no", capsModel{caps: structured(llm.SupportNo, llm.SupportNo)}, ModePromptContract, SourceAdapter, false},
		{"adapter unknown", capsModel{caps: structured(llm.SupportUnknown, llm.SupportUnknown)}, ModePromptContract, SourceUnknown, false},
		{"config true, không có thông tin khả năng thì strict mặc định", overrideModel{override: boolPtr(true)}, ModeNativeJSONSchema, SourceConfig, true},
		{"config true + adapter strict no thì tắt strict", overrideModel{capsModel{caps: structured(llm.SupportUnknown, llm.SupportNo)}, boolPtr(true)}, ModeNativeJSONSchema, SourceConfig, false},
		{"config false ghi đè adapter yes", overrideModel{capsModel{caps: structured(llm.SupportYes, llm.SupportYes)}, boolPtr(false)}, ModePromptContract, SourceConfig, false},
		{"config chưa đặt thì rơi về adapter", overrideModel{capsModel{caps: structured(llm.SupportYes, llm.SupportYes)}, nil}, ModeNativeJSONSchema, SourceAdapter, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Resolve(tc.model)
			if res.Mode != tc.wantMode || res.Source != tc.wantSource || res.Strict != tc.wantStrict {
				t.Fatalf("Resolve = %+v, want mode=%s source=%s strict=%v", res, tc.wantMode, tc.wantSource, tc.wantStrict)
			}
		})
	}
}

func TestResolveIdentity(t *testing.T) {
	res := Resolve(capsModel{caps: structured(llm.SupportYes, llm.SupportYes)})
	if res.Provider != "prov" || res.Model != "mod" {
		t.Fatalf("chưa lấy được danh tính từ caps: %+v", res)
	}
	res = Resolve(infoModel{info: llm.ModelInfo{Provider: "p2", Name: "m2"}})
	if res.Provider != "p2" || res.Model != "m2" {
		t.Fatalf("chưa lấy được danh tính từ info: %+v", res)
	}
}

func TestResolveUsesAtomicFactsSnapshot(t *testing.T) {
	res := Resolve(factsModel{facts: ModelFacts{
		Capabilities:       structured(llm.SupportYes, llm.SupportYes),
		Info:               llm.ModelInfo{Provider: "snapshot-provider", Name: "snapshot-model"},
		JSONSchemaOverride: boolPtr(false),
	}})
	if res.Mode != ModePromptContract || res.Source != SourceConfig {
		t.Fatalf("config false trong ảnh chụp phải tắt schema gốc: %+v", res)
	}
	if res.Provider != "prov" || res.Model != "mod" {
		t.Fatalf("danh tính khả năng phải lấy từ cùng một ảnh chụp: %+v", res)
	}
}

func testContract() Contract {
	return Contract{
		Name:        "test_decision",
		Description: "hợp đồng thử nghiệm",
		Schema: schema.Object(
			schema.Property("action", schema.Enum("hành động", "a", "b")).Required(),
			schema.Property("reason", schema.String("lý do")).Required(),
		),
	}
}

func TestPlanNativeOptions(t *testing.T) {
	opts, res := Plan(capsModel{caps: structured(llm.SupportYes, llm.SupportYes)}, testContract())
	if res.Mode != ModeNativeJSONSchema || len(opts) == 0 {
		t.Fatalf("chế độ native phải sinh opts: res=%+v opts=%d", res, len(opts))
	}
	cfg := agentcore.ResolveCallConfig(opts)
	rf := cfg.ResponseFormat
	if rf == nil || rf.Type != agentcore.ResponseFormatJSONSchema || rf.JSONSchema == nil {
		t.Fatalf("ResponseFormat = %+v", rf)
	}
	if rf.JSONSchema.Name != "test_decision" || rf.JSONSchema.Strict == nil || !*rf.JSONSchema.Strict {
		t.Fatalf("JSONSchema = %+v", rf.JSONSchema)
	}
}

func TestPlanPromptContractNoOptions(t *testing.T) {
	opts, res := Plan(baseModel{}, testContract())
	if res.Mode != ModePromptContract || opts != nil {
		t.Fatalf("chế độ prompt không được sinh opts: res=%+v opts=%v", res, opts)
	}
}

func TestPreparePromptUsesSchemaOnlyForPromptContract(t *testing.T) {
	contract := testContract()
	base := "Chỉ chịu trách nhiệm phán đoán hành động và lý do."

	prompt, err := PreparePrompt(base, contract, Resolution{Mode: ModePromptContract})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{base, "<output-json-schema>", `"action"`, `"required"`} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt contract thiếu %q:\n%s", want, prompt)
		}
	}

	native, err := PreparePrompt(base, contract, Resolution{Mode: ModeNativeJSONSchema})
	if err != nil {
		t.Fatal(err)
	}
	if native != base {
		t.Fatalf("chế độ native không được sửa prompt ngữ nghĩa: %q", native)
	}
}

// TestPreparePromptRendersContractPerLanguage kiểm tra nội dung hợp đồng được
// sinh đúng ngôn ngữ: Lang rõ ràng của hợp đồng thắng, còn Lang rỗng thì rơi
// về ngôn ngữ tác phẩm ở cấp tiến trình.
func TestPreparePromptRendersContractPerLanguage(t *testing.T) {
	base := "Yêu cầu."

	viPrompt, err := PreparePrompt(base, Contract{
		Name:   "c",
		Schema: map[string]any{"type": "object"},
		Lang:   i18n.LangVI,
	}, Resolution{Mode: ModePromptContract})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(viPrompt, "## Hợp đồng đầu ra") {
		t.Fatalf("hợp đồng tiếng Việt thiếu tiêu đề:\n%s", viPrompt)
	}
	if strings.Contains(viPrompt, "输出契约") {
		t.Fatalf("hợp đồng tiếng Việt không được lẫn tiếng Trung:\n%s", viPrompt)
	}

	zhPrompt, err := PreparePrompt(base, Contract{
		Name:   "c",
		Schema: map[string]any{"type": "object"},
		Lang:   i18n.LangZH,
	}, Resolution{Mode: ModePromptContract})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(zhPrompt, "## 输出契约") {
		t.Fatalf("hợp đồng tiếng Trung thiếu tiêu đề:\n%s", zhPrompt)
	}
	if strings.Contains(zhPrompt, "Hợp đồng đầu ra") {
		t.Fatalf("hợp đồng tiếng Trung không được lẫn tiếng Việt:\n%s", zhPrompt)
	}

	// Lang rỗng → ngôn ngữ tác phẩm ở cấp tiến trình.
	setActiveLang(t, "zh")
	fallback, err := PreparePrompt(base, Contract{
		Name:   "c",
		Schema: map[string]any{"type": "object"},
	}, Resolution{Mode: ModePromptContract})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fallback, "## 输出契约") {
		t.Fatalf("hợp đồng phải rơi về ngôn ngữ tác phẩm:\n%s", fallback)
	}
}

func TestPreparePromptRejectsUnmarshalableSchema(t *testing.T) {
	_, err := PreparePrompt("semantic", Contract{
		Name:   "bad",
		Schema: map[string]any{"bad": make(chan int)},
	}, Resolution{Mode: ModePromptContract})
	if err == nil || !strings.Contains(err.Error(), "marshal bad prompt schema") {
		t.Fatalf("phải phơi bày lỗi serialize schema, got %v", err)
	}
}

func TestNullableCopies(t *testing.T) {
	orig := schema.String("trường cho phép null")
	out := Nullable(orig)
	got, ok := out["type"].([]string)
	if !ok || len(got) != 2 || got[0] != "string" || got[1] != "null" {
		t.Fatalf("Nullable type = %v", out["type"])
	}
	if orig["type"] != "string" {
		t.Fatalf("Nullable đã sửa map được truyền vào: %v", orig["type"])
	}
}

func TestNullableExtendsEnumWithNull(t *testing.T) {
	orig := schema.Enum("enum cho phép null", "a", "b")
	out := Nullable(orig)
	enum, ok := out["enum"].([]any)
	if !ok || len(enum) != 3 || enum[2] != nil {
		t.Fatalf("Nullable enum = %#v", out["enum"])
	}
	if _, ok := orig["enum"].([]string); !ok {
		t.Fatalf("Nullable đã sửa enum được truyền vào: %#v", orig["enum"])
	}
	if err := ValidateJSON(out, []byte("null")); err != nil {
		t.Fatalf("enum cho phép null phải nhận null: %v", err)
	}
	if err := ValidateJSON(out, []byte(`"other"`)); err == nil {
		t.Fatal("enum cho phép null không được nhận chuỗi ngoài enum")
	}
}

func TestFingerprintStable(t *testing.T) {
	a, b := testContract(), testContract()
	if a.Fingerprint() != b.Fingerprint() || len(a.Fingerprint()) != 12 {
		t.Fatalf("fingerprint của cùng một hợp đồng phải ổn định: %s vs %s", a.Fingerprint(), b.Fingerprint())
	}
	c := testContract()
	c.Schema = schema.Object(schema.Property("other", schema.String("x")).Required())
	if a.Fingerprint() == c.Fingerprint() {
		t.Fatalf("hai schema khác nhau không được trùng fingerprint")
	}
}

func TestValidateJSONEnforcesStrictContract(t *testing.T) {
	contract := testContract()
	if err := ValidateJSON(contract.Schema, []byte(`{"action":"a","reason":"ok"}`)); err != nil {
		t.Fatalf("JSON hợp lệ bị từ chối: %v", err)
	}
	for name, raw := range map[string]string{
		"thiếu required":  `{"action":"a"}`,
		"enum sai":        `{"action":"other","reason":"x"}`,
		"sai kiểu trường": `{"action":1,"reason":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateJSON(contract.Schema, []byte(raw)); err == nil {
				t.Fatalf("phải từ chối %s", raw)
			}
		})
	}
}

func TestValidateJSONRejectsInvalidEnumContract(t *testing.T) {
	contract := testContract()
	contract.Schema["enum"] = []any{1}
	if err := ValidateJSON(contract.Schema, []byte(`{"action":"a","reason":"ok"}`)); err == nil || !strings.Contains(err.Error(), "hợp đồng không hợp lệ") {
		t.Fatalf("phải phơi bày hợp đồng enum không hợp lệ, err=%v", err)
	}
}

// TestValidateJSONMessageFollowsActiveLanguage kiểm tra thông báo lỗi kiểm tra
// hợp lệ — vốn được đưa lại cho model — đổi theo ngôn ngữ tác phẩm.
func TestValidateJSONMessageFollowsActiveLanguage(t *testing.T) {
	contract := testContract()
	raw := []byte(`{"action":"a"}`)

	setActiveLang(t, "vi")
	viErr := ValidateJSON(contract.Schema, raw)
	if viErr == nil || !strings.Contains(viErr.Error(), "trường bắt buộc") {
		t.Fatalf("thông báo tiếng Việt thiếu nhãn trường bắt buộc, err=%v", viErr)
	}

	setActiveLang(t, "zh")
	zhErr := ValidateJSON(contract.Schema, raw)
	if zhErr == nil || !strings.Contains(zhErr.Error(), "是必填字段") {
		t.Fatalf("thông báo tiếng Trung thiếu nhãn trường bắt buộc, err=%v", zhErr)
	}
}

func TestExtractJSONObjectBalanced(t *testing.T) {
	raw := "tiền tố ```json\n{\"a\":\"}\",\"nested\":{\"b\":1}}\n``` hậu tố"
	want := `{"a":"}","nested":{"b":1}}`
	if got := ExtractJSONObject(raw); got != want {
		t.Fatalf("ExtractJSONObject = %q, want %q", got, want)
	}
}

type executionModel struct {
	responses []string
	stops     []agentcore.StopReason
	calls     int
	messages  []agentcore.Message
	config    agentcore.CallConfig
}

func (m *executionModel) Generate(_ context.Context, messages []agentcore.Message, _ []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	m.messages = messages
	m.config = agentcore.ResolveCallConfig(opts)
	response := m.responses[m.calls]
	var stop agentcore.StopReason
	if m.calls < len(m.stops) {
		stop = m.stops[m.calls]
	}
	m.calls++
	return &agentcore.LLMResponse{Message: agentcore.Message{
		Role:       agentcore.RoleAssistant,
		Content:    []agentcore.ContentBlock{agentcore.TextBlock(response)},
		StopReason: stop,
	}}, nil
}

type nativeExecutionModel struct{ *executionModel }

func (m *nativeExecutionModel) Capabilities() llm.Capabilities {
	return structured(llm.SupportYes, llm.SupportYes)
}

func TestExecutePromptModeSelfHealsSchemaViolation(t *testing.T) {
	model := &executionModel{responses: []string{
		`{}`,
		`{"action":"a","reason":"fixed"}`,
	}}
	type output struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	out, err := Execute(t.Context(), model, Request[output]{
		Contract: testContract(), SystemPrompt: "Phán đoán.", Payload: "Đầu vào", Agent: "test",
	})
	if err != nil || out.Reason != "fixed" {
		t.Fatalf("tự lành thất bại: %+v %v", out, err)
	}
	if model.calls != 2 {
		t.Fatalf("phải thành công ở lần thứ hai, calls=%d", model.calls)
	}
	if len(model.messages) != 4 || !strings.Contains(model.messages[3].TextContent(), "$.action") {
		t.Fatalf("lần hỏi lại không mang lỗi schema chính xác: %+v", model.messages)
	}
}

// TestExecuteSelfHealMessageFollowsContractLanguage kiểm tra câu nhắc tự sửa và
// thông báo lỗi đưa lại cho model đổi theo ngôn ngữ của hợp đồng.
func TestExecuteSelfHealMessageFollowsContractLanguage(t *testing.T) {
	type output struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}

	for _, tc := range []struct {
		name string
		lang i18n.Lang
		want string
	}{
		{"tiếng Việt", i18n.LangVI, "Đầu ra ở trên không khớp với JSON Schema"},
		{"tiếng Trung", i18n.LangZH, "上面的输出不符合 JSON Schema"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &executionModel{responses: []string{
				`{}`,
				`{"action":"a","reason":"fixed"}`,
			}}
			contract := testContract()
			contract.Lang = tc.lang
			if _, err := Execute(t.Context(), model, Request[output]{
				Contract: contract, SystemPrompt: "Phán đoán.", Payload: "Đầu vào",
			}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if len(model.messages) != 4 {
				t.Fatalf("phải có lượt hỏi lại, messages=%d", len(model.messages))
			}
			hint := model.messages[3].TextContent()
			if !strings.Contains(hint, tc.want) {
				t.Fatalf("câu nhắc tự sửa không đúng ngôn ngữ:\n%s", hint)
			}
		})
	}
}

func TestExecuteNativeSchemaViolationFailsImmediately(t *testing.T) {
	model := &nativeExecutionModel{executionModel: &executionModel{responses: []string{`{}`}}}
	_, err := Execute(t.Context(), model, Request[map[string]any]{
		Contract: testContract(), SystemPrompt: "Phán đoán.", Payload: "Đầu vào",
	})
	var failure *Failure
	if !errors.As(err, &failure) || failure.Kind != FailureContract {
		t.Fatalf("phải trả lỗi hợp đồng gốc, got %T %v", err, err)
	}
	if model.calls != 1 {
		t.Fatalf("vi phạm hợp đồng gốc không được hỏi lại, calls=%d", model.calls)
	}
}

func TestExecuteExposesModelErrorStopReason(t *testing.T) {
	model := &executionModel{
		responses: []string{`{"action":"a","reason":"unused"}`},
		stops:     []agentcore.StopReason{agentcore.StopReasonError},
	}
	_, err := Execute(t.Context(), model, Request[map[string]any]{
		Contract: testContract(), SystemPrompt: "Phán đoán.", Payload: "Đầu vào",
	})
	var failure *Failure
	if !errors.As(err, &failure) || failure.Kind != FailureProtocol || !strings.Contains(err.Error(), "stop_reason=error") {
		t.Fatalf("phải phơi bày lý do dừng lỗi của mô hình, got %T %v", err, err)
	}
	if model.calls != 1 {
		t.Fatalf("lý do dừng lỗi không được coi là lỗi JSON để hỏi lại, calls=%d", model.calls)
	}
}
