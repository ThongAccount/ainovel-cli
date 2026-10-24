package sim

import (
	"context"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/llm"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
)

func TestSimulationContractsAreStrictReady(t *testing.T) {
	for _, lang := range []string{"vi", "zh"} {
		for _, contract := range []llmcontract.Contract{
			sourceReportContract(simTextsFor(lang)),
			synthesisContract(simTextsFor(lang)),
		} {
			if err := llmcontract.ValidateStrictReady(contract.Schema); err != nil {
				t.Fatalf("%s (%s): %v", contract.Name, lang, err)
			}
		}
	}
}

func TestAnalyzeSourceContractFollowsActiveLanguage(t *testing.T) {
	previousLang := i18n.Active()
	t.Cleanup(func() { i18n.SetActive(string(previousLang)) })

	tests := []struct {
		lang      string
		want      string
		doNotWant string
	}{
		{lang: "vi", want: "Tổng hợp các phương pháp viết", doNotWant: "从单篇语料"},
		{lang: "zh", want: "从单篇语料", doNotWant: "Tổng hợp các phương pháp viết"},
	}
	for _, tc := range tests {
		t.Run(tc.lang, func(t *testing.T) {
			i18n.SetActive(tc.lang)
			model := &nativeSimulationModel{response: validSourceReportJSON("tóm tắt rõ ràng")}
			if _, err := AnalyzeSource(t.Context(), model, "Chỉ phân tích phương pháp viết.", scannedSource{}); err != nil {
				t.Fatalf("AnalyzeSource: %v", err)
			}
			format := model.config.ResponseFormat
			if format == nil || format.JSONSchema == nil {
				t.Fatalf("response format = %#v", format)
			}
			description := format.JSONSchema.Description
			if !strings.Contains(description, tc.want) {
				t.Fatalf("contract description %q does not contain %q", description, tc.want)
			}
			if strings.Contains(description, tc.doNotWant) {
				t.Fatalf("contract description %q contains text from the other language", description)
			}
		})
	}
}

type nativeSimulationModel struct {
	response string
	messages []agentcore.Message
	config   agentcore.CallConfig
}

func (m *nativeSimulationModel) Capabilities() llm.Capabilities {
	return llm.Capabilities{
		Provider:   "openai",
		Model:      "gpt-test",
		Structured: llm.StructuredCapabilities{JSONSchema: llm.SupportYes, Strict: llm.SupportYes},
	}
}

func (m *nativeSimulationModel) Generate(_ context.Context, messages []agentcore.Message, _ []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	m.messages = messages
	m.config = agentcore.ResolveCallConfig(opts)
	return &agentcore.LLMResponse{Message: agentcore.Message{
		Role:       agentcore.RoleAssistant,
		Content:    []agentcore.ContentBlock{agentcore.TextBlock(m.response)},
		StopReason: agentcore.StopReasonStop,
	}}, nil
}

func TestAnalyzeSourceUsesNativeSchema(t *testing.T) {
	model := &nativeSimulationModel{response: validSourceReportJSON("Tóm tắt rõ ràng")}
	report, err := AnalyzeSource(t.Context(), model, "Chỉ phân tích phương pháp viết.", scannedSource{})
	if err != nil {
		t.Fatalf("AnalyzeSource: %v", err)
	}
	if report.Summary == "" {
		t.Fatal("tóm tắt rỗng")
	}
	format := model.config.ResponseFormat
	if format == nil || format.JSONSchema == nil || format.JSONSchema.Name != sourceReportContract(simTextsFor(string(i18n.Active()))).Name {
		t.Fatalf("response format = %#v", format)
	}
	if strings.Contains(model.messages[0].TextContent(), "<output-json-schema>") {
		t.Fatalf("prompt native không được chèn schema: %s", model.messages[0].TextContent())
	}
}

func TestAnalyzeSourcePromptModeRepairsMissingRequiredFields(t *testing.T) {
	model := &scriptedLLM{responses: []string{
		`{}`,
		validSourceReportJSON("Tóm tắt đã sửa"),
	}}
	report, err := AnalyzeSource(t.Context(), model, "Chỉ phân tích phương pháp viết.", scannedSource{})
	if err != nil {
		t.Fatalf("AnalyzeSource: %v", err)
	}
	if report.Summary != "Tóm tắt đã sửa" || model.calls.Load() != 2 {
		t.Fatalf("sau khi thiếu trường phải tự sửa: report=%+v calls=%d", report, model.calls.Load())
	}
}

func TestCompactSourceContentUsesLanguageMarker(t *testing.T) {
	content := strings.Repeat("a", maxSourceRunes+1)
	tests := []struct {
		lang   string
		marker string
	}{
		{lang: "vi", marker: "[...nội dung bị cắt ngắn...]"},
		{lang: "zh", marker: "[...已截断...]"},
	}
	for _, tc := range tests {
		t.Run(tc.lang, func(t *testing.T) {
			texts := simTextsFor(tc.lang)
			got := compactSourceContent(content, texts.truncatedMarker)
			if !strings.Contains(got, tc.marker) {
				t.Fatalf("compact content does not contain %q", tc.marker)
			}
		})
	}
}

var _ LLMChat = (*nativeSimulationModel)(nil)
