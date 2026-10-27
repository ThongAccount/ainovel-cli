package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/flow"
	"github.com/voocel/ainovel-cli/internal/store"
)

// SaveArcSummaryTool lưu tóm tắt cấp cung, ảnh trạng thái nhân vật và quy tắc viết; Editor gọi khi kết thúc cung.
type SaveArcSummaryTool struct {
	store *store.Store
}

func NewSaveArcSummaryTool(store *store.Store) *SaveArcSummaryTool {
	return &SaveArcSummaryTool{store: store}
}

func (t *SaveArcSummaryTool) Name() string      { return "save_arc_summary" }
func (t *SaveArcSummaryTool) texts() *toolTexts { return toolTextsFor(t.store.Language()) }
func (t *SaveArcSummaryTool) Description() string {
	return t.texts().saveArcSummaryDesc
}
func (t *SaveArcSummaryTool) Label() string { return t.texts().saveArcSummaryLabel }

// Công cụ ghi, cấm chạy song song.
func (t *SaveArcSummaryTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *SaveArcSummaryTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *SaveArcSummaryTool) Schema() map[string]any {
	snapshotSchema := schema.Object(
		schema.Property("name", schema.String(t.texts().saveArcSnapshotName)).Required(),
		schema.Property("status", schema.String(t.texts().saveArcSnapshotStatus)).Required(),
		schema.Property("power", schema.String(t.texts().saveArcSnapshotPower)),
		schema.Property("motivation", schema.String(t.texts().saveArcSnapshotMotiv)).Required(),
		schema.Property("relations", schema.String(t.texts().saveArcSnapshotRelations)),
	)
	voiceSchema := schema.Object(
		schema.Property("name", schema.String(t.texts().saveArcVoiceName)).Required(),
		schema.Property("rules", schema.Array(t.texts().saveArcVoiceRules, schema.String(""))).Required(),
	)
	styleRulesSchema := schema.Object(
		schema.Property("prose", schema.Array(t.texts().saveArcProse, schema.String(""))).Required(),
		schema.Property("dialogue", schema.Array(t.texts().saveArcDialogue, voiceSchema)).Required(),
		schema.Property("taboos", schema.Array(t.texts().saveArcTaboos, schema.String(""))),
	)
	return schema.Object(
		schema.Property("volume", schema.Int(t.texts().saveArcVolume)).Required(),
		schema.Property("arc", schema.Int(t.texts().saveArcArc)).Required(),
		schema.Property("title", schema.String(t.texts().saveArcTitle)).Required(),
		schema.Property("summary", schema.String(t.texts().saveArcSummaryField)).Required(),
		schema.Property("key_events", schema.Array(t.texts().saveArcKeyEvents, schema.String(""))).Required(),
		schema.Property("character_snapshots", schema.Array(t.texts().saveArcCharacterSnaps, snapshotSchema)).Required(),
		schema.Property("style_rules", styleRulesSchema).Required(),
	)
}

func (t *SaveArcSummaryTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Volume             int                        `json:"volume"`
		Arc                int                        `json:"arc"`
		Title              string                     `json:"title"`
		Summary            string                     `json:"summary"`
		KeyEvents          []string                   `json:"key_events"`
		CharacterSnapshots []domain.CharacterSnapshot `json:"character_snapshots"`
		StyleRules         *arcSummaryStyleRules      `json:"style_rules"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		if strings.Contains(err.Error(), "style_rules.dialogue") {
			return nil, fmt.Errorf("invalid args: style_rules.dialogue must be an array of objects {name, rules}, not strings: %w: %w", errs.ErrToolArgs, err)
		}
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if a.Volume <= 0 || a.Arc <= 0 {
		return nil, fmt.Errorf("volume and arc must be > 0: %w", errs.ErrToolArgs)
	}
	if strings.TrimSpace(a.Title) == "" || strings.TrimSpace(a.Summary) == "" {
		return nil, fmt.Errorf("title and summary are required: %w", errs.ErrToolArgs)
	}
	if err := validateArcSummaryStyleRules(a.StyleRules); err != nil {
		return nil, err
	}
	for i := range a.CharacterSnapshots {
		a.CharacterSnapshots[i].Volume = a.Volume
		a.CharacterSnapshots[i].Arc = a.Arc
	}
	arcSummary := domain.ArcSummary{
		Volume: a.Volume, Arc: a.Arc, Title: a.Title, Summary: a.Summary, KeyEvents: a.KeyEvents,
	}
	rules := domain.WritingStyleRules{
		Volume:    a.Volume,
		Arc:       a.Arc,
		Prose:     a.StyleRules.Prose,
		Dialogue:  a.StyleRules.Dialogue,
		Taboos:    a.StyleRules.Taboos,
		UpdatedAt: time.Now().Format(time.RFC3339),
	}
	replay, err := t.arcSummaryReplay(arcSummary, a.CharacterSnapshots, rules)
	if err != nil {
		return nil, err
	}
	if !replay {
		if err := requireAggregateTarget(t.store, flow.AggregateArcSummary, a.Volume, a.Arc, 0); err != nil {
			return nil, err
		}
		if len(a.CharacterSnapshots) > 0 {
			if err := t.store.Characters.SaveSnapshots(a.Volume, a.Arc, a.CharacterSnapshots); err != nil {
				return nil, fmt.Errorf("save character snapshots: %w: %w", errs.ErrStoreWrite, err)
			}
		}
		if err := t.store.World.SaveStyleRules(rules); err != nil {
			return nil, fmt.Errorf("save style rules: %w: %w", errs.ErrStoreWrite, err)
		}

		// Tóm tắt cung là dấu hiệu hoàn tất của Router, được ghi như bản ghi ngữ nghĩa cuối cùng. Nếu một bước trước đó
		// thất bại thì tóm tắt vẫn còn thiếu, sau khi khôi phục Router vẫn phân phối lại nhiệm vụ này.
		if err := t.store.Summaries.SaveArcSummary(arcSummary); err != nil {
			return nil, fmt.Errorf("save arc summary: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	artifacts := []string{fmt.Sprintf("summaries/arc-v%02da%02d.json", a.Volume, a.Arc)}
	if len(a.CharacterSnapshots) > 0 {
		artifacts = append(artifacts, fmt.Sprintf("meta/snapshots/v%02da%02d.json", a.Volume, a.Arc))
	}
	artifacts = append(artifacts, "meta/style_rules.json")

	if _, err := t.store.Checkpoints.AppendArtifacts(
		domain.ArcScope(a.Volume, a.Arc), "arc_summary", artifacts...,
	); err != nil {
		return nil, fmt.Errorf("checkpoint arc summary: %w: %w", errs.ErrStoreWrite, err)
	}

	return json.Marshal(map[string]any{
		"saved": true, "type": "arc_summary",
		"volume": a.Volume, "arc": a.Arc,
		"snapshots":         len(a.CharacterSnapshots),
		"style_rules_saved": true,
	})
}

// arcSummaryReplay chỉ cho phép phần chốt idempotent khi nội dung hoàn toàn giống nhau, dùng cho lần thử lại khi bản ghi
// ngữ nghĩa đã ghi xuống nhưng bước thêm checkpoint thất bại. Bất kỳ khác biệt nào cũng là xung đột tường minh, không được lấy lần thử lại để ghi đè các sự thật tổng hợp trong quá khứ.
func (t *SaveArcSummaryTool) arcSummaryReplay(
	summary domain.ArcSummary,
	snapshots []domain.CharacterSnapshot,
	rules domain.WritingStyleRules,
) (bool, error) {
	existing, err := t.store.Summaries.LoadArcSummary(summary.Volume, summary.Arc)
	if err != nil {
		return false, fmt.Errorf("load arc summary: %w: %w", errs.ErrStoreRead, err)
	}
	if existing == nil {
		return false, nil
	}
	storedSnapshots, err := t.store.Characters.LoadSnapshots(summary.Volume, summary.Arc)
	if err != nil {
		return false, fmt.Errorf("load character snapshots: %w: %w", errs.ErrStoreRead, err)
	}
	storedRules, err := t.store.World.LoadStyleRules()
	if err != nil {
		return false, fmt.Errorf("load style rules: %w: %w", errs.ErrStoreRead, err)
	}
	if storedRules != nil {
		rules.UpdatedAt = storedRules.UpdatedAt
	}
	if !reflect.DeepEqual(*existing, summary) ||
		!slices.Equal(storedSnapshots, snapshots) ||
		storedRules == nil || !reflect.DeepEqual(*storedRules, rules) {
		return false, fmt.Errorf("Tập %d cung %d đã có tóm tắt tồn tại nhưng công cụ liên kết khác, từ chối ghi đè: %w", summary.Volume, summary.Arc, errs.ErrToolConflict)
	}
	return true, nil
}

type arcSummaryStyleRules struct {
	Prose    []string                `json:"prose"`
	Dialogue []domain.CharacterVoice `json:"dialogue"`
	Taboos   []string                `json:"taboos"`
}

func validateArcSummaryStyleRules(rules *arcSummaryStyleRules) error {
	if rules == nil {
		return fmt.Errorf("style_rules is required: %w", errs.ErrToolArgs)
	}
	if len(rules.Prose) == 0 {
		return fmt.Errorf("style_rules.prose is required: %w", errs.ErrToolArgs)
	}
	if len(rules.Dialogue) == 0 {
		return fmt.Errorf("style_rules.dialogue is required; expected array of objects {name, rules}: %w", errs.ErrToolArgs)
	}
	for i, voice := range rules.Dialogue {
		if strings.TrimSpace(voice.Name) == "" {
			return fmt.Errorf("style_rules.dialogue[%d].name is required: %w", i, errs.ErrToolArgs)
		}
		if len(voice.Rules) == 0 {
			return fmt.Errorf("style_rules.dialogue[%d].rules is required: %w", i, errs.ErrToolArgs)
		}
		for j, rule := range voice.Rules {
			if strings.TrimSpace(rule) == "" {
				return fmt.Errorf("style_rules.dialogue[%d].rules[%d] is empty: %w", i, j, errs.ErrToolArgs)
			}
		}
	}
	return nil
}
