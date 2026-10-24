package chapterfacts

import (
	"fmt"
	"strings"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
)

// Properties trả về các trường JSON Schema dùng chung cho đầy đủ sự thật
// chương.
//
// Mô tả schema là văn bản model đọc nên được dịch song ngữ theo ngôn ngữ tác
// phẩm; tên trường và giá trị enum là ký hiệu kỹ thuật nên giữ nguyên.
func Properties(includeFeedback bool) []schema.Prop {
	texts := activeTexts()
	textList := func(description string) map[string]any {
		return schema.Array(description, schema.String(description))
	}
	timeline := schema.Object(
		schema.Property("time", schema.String(texts.storyTime)).Required(),
		schema.Property("event", schema.String(texts.event)).Required(),
		schema.Property("characters", textList(texts.involvedCharacter)).Required(),
	)
	foreshadow := schema.Object(
		schema.Property("id", schema.String(texts.foreshadowID)).Required(),
		schema.Property("action", schema.Enum(texts.operation, "plant", "advance", "resolve")).Required(),
		schema.Property("description", llmcontract.Nullable(schema.String(texts.plantDescription))).Required(),
	)
	relationship := schema.Object(
		schema.Property("character_a", schema.String(texts.characterA)).Required(),
		schema.Property("character_b", schema.String(texts.characterB)).Required(),
		schema.Property("relation", schema.String(texts.relationAtEnd)).Required(),
	)
	stateChange := schema.Object(
		schema.Property("entity", schema.String(texts.entity)).Required(),
		schema.Property("field", schema.String(texts.field)).Required(),
		schema.Property("old_value", llmcontract.Nullable(schema.String(texts.oldValue))).Required(),
		schema.Property("new_value", schema.String(texts.newValue)).Required(),
		schema.Property("reason", llmcontract.Nullable(schema.String(texts.reason))).Required(),
	)
	props := []schema.Prop{
		schema.Property("title", schema.String(texts.finalTitle)).Required(),
		schema.Property("summary", schema.String(texts.chapterSummary)).Required(),
		schema.Property("characters", textList(texts.presentCharacters)).Required(),
		schema.Property("key_events", textList(texts.keyEvents)).Required(),
		schema.Property("timeline_events", schema.Array(texts.timelineEvents, timeline)).Required(),
		schema.Property("foreshadow_updates", schema.Array(texts.foreshadowOps, foreshadow)).Required(),
		schema.Property("relationship_changes", schema.Array(texts.relationshipMoves, relationship)).Required(),
		schema.Property("state_changes", schema.Array(texts.stateChanges, stateChange)).Required(),
		schema.Property("cast_intros", schema.Array(texts.newCast, schema.Object(
			schema.Property("name", schema.String(texts.name)).Required(),
			schema.Property("brief_role", schema.String(texts.briefRole)).Required(),
		))).Required(),
		schema.Property("hook_type", llmcontract.Nullable(schema.Enum(texts.hookType, domain.HookTypes()...))).Required(),
		schema.Property("dominant_strand", llmcontract.Nullable(schema.Enum(texts.dominantStrand, domain.DominantStrands()...))).Required(),
	}
	if includeFeedback {
		feedback := schema.Object(
			schema.Property("deviation", schema.String(texts.deviation)).Required(),
			schema.Property("suggestion", schema.String(texts.suggestion)).Required(),
		)
		feedback["description"] = texts.feedbackObject
		props = append(props, schema.Property("feedback", llmcontract.Nullable(feedback)).Required())
	}
	return props
}

// Validate kiểm tra các ràng buộc tất định dùng chung cho cả đề nghị gửi bình
// thường lẫn bản sửa thủ công.
//
// Các thông báo kỹ thuật ở đây là lỗi lập trình, giữ nguyên tiếng Anh để
// không lẫn với lỗi nghiệp vụ mà model phải tự sửa; lỗi "id manh mối là giá
// trị giữ chỗ" thì được dịch song ngữ vì nó đưa lại cho model.
func Validate(facts domain.ChapterFacts) error {
	texts := activeTexts()
	if strings.TrimSpace(facts.Title) == "" {
		return fmt.Errorf("title is required")
	}
	if strings.TrimSpace(facts.Summary) == "" {
		return fmt.Errorf("summary is required")
	}
	if len(facts.KeyEvents) == 0 {
		return fmt.Errorf("key_events must contain at least one event")
	}
	if err := validateTextItems("characters", facts.Characters); err != nil {
		return err
	}
	if err := validateTextItems("key_events", facts.KeyEvents); err != nil {
		return err
	}
	for i, event := range facts.TimelineEvents {
		if strings.TrimSpace(event.Time) == "" || strings.TrimSpace(event.Event) == "" {
			return fmt.Errorf("timeline_events[%d] requires time and event", i)
		}
		if err := validateTextItems(fmt.Sprintf("timeline_events[%d].characters", i), event.Characters); err != nil {
			return err
		}
	}
	for i, update := range facts.ForeshadowUpdates {
		if strings.TrimSpace(update.ID) == "" {
			return fmt.Errorf("foreshadow_updates[%d].id is required", i)
		}
		if placeholderForeshadowID(update.ID) {
			return fmt.Errorf(texts.placeholderID, i, update.ID)
		}
		switch update.Action {
		case "plant":
			if strings.TrimSpace(update.Description) == "" {
				return fmt.Errorf("foreshadow_updates[%d] plant requires description", i)
			}
		case "advance", "resolve":
		default:
			return fmt.Errorf("foreshadow_updates[%d].action invalid: %q", i, update.Action)
		}
	}
	for i, change := range facts.RelationshipChanges {
		if strings.TrimSpace(change.CharacterA) == "" || strings.TrimSpace(change.CharacterB) == "" || strings.TrimSpace(change.Relation) == "" {
			return fmt.Errorf("relationship_changes[%d] requires character_a, character_b and relation", i)
		}
		if change.CharacterA == change.CharacterB {
			return fmt.Errorf("relationship_changes[%d] cannot relate a character to itself", i)
		}
	}
	for i, change := range facts.StateChanges {
		if strings.TrimSpace(change.Entity) == "" || strings.TrimSpace(change.Field) == "" || strings.TrimSpace(change.NewValue) == "" {
			return fmt.Errorf("state_changes[%d] requires entity, field and new_value", i)
		}
	}
	for i, intro := range facts.CastIntros {
		if strings.TrimSpace(intro.Name) == "" || strings.TrimSpace(intro.BriefRole) == "" {
			return fmt.Errorf("cast_intros[%d] requires name and brief_role", i)
		}
	}
	if facts.HookType != "" && !domain.ValidHookType(facts.HookType) {
		return fmt.Errorf("invalid hook_type %q", facts.HookType)
	}
	if facts.DominantStrand != "" && !domain.ValidDominantStrand(facts.DominantStrand) {
		return fmt.Errorf("invalid dominant_strand %q", facts.DominantStrand)
	}
	if facts.Feedback != nil && (strings.TrimSpace(facts.Feedback.Deviation) == "" || strings.TrimSpace(facts.Feedback.Suggestion) == "") {
		return fmt.Errorf("feedback requires deviation and suggestion")
	}
	return nil
}

func validateTextItems(name string, items []string) error {
	for i, item := range items {
		if strings.TrimSpace(item) == "" {
			return fmt.Errorf("%s[%d] cannot be empty", name, i)
		}
	}
	return nil
}

// placeholderForeshadowIDs là các từ giữ chỗ mà model hay điền vào trường id.
// Bản thân chúng không mang ý nghĩa định danh, mà id manh mối phải ổn định
// xuyên suốt các chương — đo được việc model đặt mọi manh mối mới là "new",
// từ mục thứ hai trở đi đâm vào bản ghi cũ và bị nuốt lặng: 22 chương gieo 6
// lần, vào sổ 2.
var placeholderForeshadowIDs = map[string]struct{}{
	"new": {}, "none": {}, "null": {}, "nil": {}, "tbd": {}, "todo": {},
	"auto": {}, "id": {}, "-": {}, "n/a": {}, "na": {}, "unknown": {}, "?": {},
}

func placeholderForeshadowID(id string) bool {
	_, ok := placeholderForeshadowIDs[strings.ToLower(strings.TrimSpace(id))]
	return ok
}
