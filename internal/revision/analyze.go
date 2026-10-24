package revision

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/chapterfacts"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
)

// analysisContractFor dựng hợp đồng phân tích sửa đổi chương theo ngôn ngữ tác phẩm.
// Hợp đồng và schema đều đi vào prompt của mô hình nên phải song ngữ.
func analysisContractFor(lang i18n.Lang) llmcontract.Contract {
	return llmcontract.Contract{
		Name:        "chapter_revision_analysis",
		Description: revisionTextsFor(lang).contractDesc,
		Schema:      revisionAnalysisSchema(revisionTextsFor(lang)),
	}
}

func revisionAnalysisSchema(t *revisionTexts) map[string]any {
	textList := func(description string) map[string]any { return schema.Array(description, schema.String(description)) }
	voice := schema.Object(
		schema.Property("name", schema.String(t.voiceName)).Required(),
		schema.Property("rules", textList(t.dialoguePrefs)).Required(),
	)
	facts := schema.Object(chapterfacts.Properties(false)...)
	impact := schema.Object(
		schema.Property("deviation", schema.String(t.deviation)).Required(),
		schema.Property("suggestion", schema.String(t.suggestion)).Required(),
	)
	return schema.Object(
		schema.Property("change_summary", schema.String(t.changeSummary)).Required(),
		schema.Property("story_changed", schema.Bool(t.storyChanged)).Required(),
		schema.Property("facts", facts).Required(),
		schema.Property("style_delta", schema.Object(
			schema.Property("prose", textList(t.prosePrefs)).Required(),
			schema.Property("dialogue", schema.Array(t.voicePrefs, voice)).Required(),
			schema.Property("taboos", textList(t.taboos)).Required(),
		)).Required(),
		schema.Property("outline_impact", llmcontract.Nullable(impact)).Required(),
		schema.Property("downstream_issues", textList(t.downstream)).Required(),
	)
}

// Analyze phân tích bản sửa đổi chương bằng ngôn ngữ tác phẩm đang hoạt động ở
// cấp tiến trình. Lời gọi có sẵn store nên dùng store.Language() (xem Service).
func Analyze(ctx context.Context, model agentcore.ChatModel, systemPrompt string, change Change, previous domain.ChapterRecord, downstream []domain.ChapterSummary) (domain.RevisionAnalysis, error) {
	return analyze(ctx, model, systemPrompt, change, previous, downstream, i18n.Active())
}

func analyze(ctx context.Context, model agentcore.ChatModel, systemPrompt string, change Change, previous domain.ChapterRecord, downstream []domain.ChapterSummary, lang i18n.Lang) (domain.RevisionAnalysis, error) {
	if model == nil {
		return domain.RevisionAnalysis{}, fmt.Errorf("revision model is required")
	}
	if strings.TrimSpace(systemPrompt) == "" {
		return domain.RevisionAnalysis{}, fmt.Errorf("revision prompt is required")
	}
	payload, err := json.Marshal(map[string]any{
		"chapter": change.Chapter, "previous_facts": previous.Facts,
		"revised_content": change.After, "changed_excerpt": changedExcerpt(change.Before, change.After),
		"downstream_summaries": downstream,
	})
	if err != nil {
		return domain.RevisionAnalysis{}, err
	}
	analysis, err := llmcontract.Execute(ctx, model, llmcontract.Request[domain.RevisionAnalysis]{
		Contract: analysisContractFor(lang), SystemPrompt: systemPrompt, Payload: string(payload), Agent: "revision",
		Validate: validateAnalysis,
		Hooks: llmcontract.Hooks{
			Resolved: func(res llmcontract.Resolution) {
				slog.Debug("Chọn giao thức có cấu trúc cho sửa đổi chương", "mode", res.Mode, "provider", res.Provider, "model", res.Model)
			},
			Correction: func(ev llmcontract.Correction) {
				slog.Warn("Sửa kết quả phân tích sửa đổi chương", "attempt", ev.Attempt, "layer", ev.Layer, "err", ev.Err)
			},
		},
	})
	if err != nil {
		return domain.RevisionAnalysis{}, fmt.Errorf("phân tích bản sửa đổi chương %d: %w", change.Chapter, err)
	}
	analysis.Facts.Feedback = analysis.OutlineImpact
	return analysis, nil
}

func validateAnalysis(analysis *domain.RevisionAnalysis) error {
	if strings.TrimSpace(analysis.ChangeSummary) == "" {
		return fmt.Errorf("change_summary is required")
	}
	if err := chapterfacts.Validate(analysis.Facts); err != nil {
		return fmt.Errorf("facts: %w", err)
	}
	if analysis.OutlineImpact != nil && (strings.TrimSpace(analysis.OutlineImpact.Deviation) == "" || strings.TrimSpace(analysis.OutlineImpact.Suggestion) == "") {
		return fmt.Errorf("outline_impact requires deviation and suggestion")
	}
	if err := validateStyleDelta(analysis.StyleDelta); err != nil {
		return err
	}
	for i, issue := range analysis.DownstreamIssues {
		if strings.TrimSpace(issue) == "" {
			return fmt.Errorf("downstream_issues[%d] cannot be empty", i)
		}
	}
	return nil
}

func validateStyleDelta(style domain.StyleDelta) error {
	for name, items := range map[string][]string{"prose": style.Prose, "taboos": style.Taboos} {
		for i, item := range items {
			if strings.TrimSpace(item) == "" {
				return fmt.Errorf("style_delta.%s[%d] cannot be empty", name, i)
			}
		}
	}
	for i, voice := range style.Dialogue {
		if strings.TrimSpace(voice.Name) == "" || len(voice.Rules) == 0 {
			return fmt.Errorf("style_delta.dialogue[%d] requires name and rules", i)
		}
		for j, rule := range voice.Rules {
			if strings.TrimSpace(rule) == "" {
				return fmt.Errorf("style_delta.dialogue[%d].rules[%d] cannot be empty", i, j)
			}
		}
	}
	return nil
}

type excerpt struct {
	BeforeStart int    `json:"before_start_line"`
	BeforeEnd   int    `json:"before_end_line"`
	Before      string `json:"before"`
	AfterStart  int    `json:"after_start_line"`
	AfterEnd    int    `json:"after_end_line"`
	After       string `json:"after"`
}

func changedExcerpt(before, after string) excerpt {
	oldLines := strings.Split(domain.NormalizeChapterContent(before), "\n")
	newLines := strings.Split(domain.NormalizeChapterContent(after), "\n")
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}
	oldChanged := oldLines[prefix : len(oldLines)-suffix]
	newChanged := newLines[prefix : len(newLines)-suffix]
	return excerpt{
		BeforeStart: prefix + 1, BeforeEnd: prefix + len(oldChanged), Before: strings.Join(oldChanged, "\n"),
		AfterStart: prefix + 1, AfterEnd: prefix + len(newChanged), After: strings.Join(newChanged, "\n"),
	}
}
