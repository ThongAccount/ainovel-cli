package imp

import (
	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
)

func nullableString(description string) map[string]any {
	return llmcontract.Nullable(schema.String(description))
}

func stringList(description string) map[string]any {
	return schema.Array(description, schema.String(description))
}

// segmentContract là hợp đồng nhận diện ranh giới.
// Mô tả hợp đồng và mô tả thuộc tính đều là văn bản mô hình đọc nên lấy theo
// ngôn ngữ tác phẩm (t); tên hợp đồng và tên thuộc tính JSON giữ nguyên.
func segmentContract(t *importTexts) llmcontract.Contract {
	return llmcontract.Contract{
		Name:        "import_segment",
		Description: t.segmentDesc,
		Schema: schema.Object(
			schema.Property("boundaries", schema.Array(t.boundariesDesc, schema.Object(
				schema.Property("unit_id", schema.String(t.unitIDDesc)).Required(),
				schema.Property("anchor", nullableString(t.anchorDesc)).Required(),
				schema.Property("kind", schema.Enum(t.kindDesc, kindChapter, kindGroup, kindFrontMatter, kindBackMatter)).Required(),
				schema.Property("title", nullableString(t.titleDesc)).Required(),
				schema.Property("uncertain", schema.Bool(t.uncertainDesc)).Required(),
				schema.Property("reason", nullableString(t.reasonDesc)).Required(),
			))).Required(),
		),
	}
}

// analysisContract là hợp đồng trích xuất sự thật theo chương.
func analysisContract(t *importTexts) llmcontract.Contract {
	return llmcontract.Contract{
		Name:        "import_chapter_analysis",
		Description: t.analysisDesc,
		Schema: schema.Object(
			schema.Property("chapters", schema.Array(t.chaptersDesc, chapterFactsSchema(t))).Required(),
		),
	}
}

// chapterFactsSchema dựng schema của một đối tượng sự thật theo chương.
func chapterFactsSchema(t *importTexts) map[string]any {
	characterEvidence := schema.Object(
		schema.Property("chapter", schema.Int(t.evidenceChapterDesc)).Required(),
		schema.Property("name", schema.String(t.characterNameDesc)).Required(),
		schema.Property("note", nullableString(t.characterNoteDesc)).Required(),
	)
	worldEvidence := schema.Object(
		schema.Property("chapter", schema.Int(t.evidenceChapterDesc)).Required(),
		schema.Property("category", nullableString(t.worldCategoryDesc)).Required(),
		schema.Property("fact", schema.String(t.worldFactDesc)).Required(),
	)
	timelineEvent := schema.Object(
		schema.Property("chapter", schema.Int(t.chapterNoDesc)).Required(),
		schema.Property("time", schema.String(t.storyTimeDesc)).Required(),
		schema.Property("event", schema.String(t.eventDesc)).Required(),
		schema.Property("characters", stringList(t.relatedCharactersDesc)).Required(),
	)
	foreshadow := schema.Object(
		schema.Property("id", schema.String(t.foreshadowIDDesc)).Required(),
		schema.Property("action", schema.Enum(t.foreshadowActionDesc, "plant", "advance", "resolve")).Required(),
		schema.Property("description", nullableString(t.plantDescriptionDesc)).Required(),
	)
	relationship := schema.Object(
		schema.Property("character_a", schema.String(t.charADesc)).Required(),
		schema.Property("character_b", schema.String(t.charBDesc)).Required(),
		schema.Property("relation", schema.String(t.relationChangeDesc)).Required(),
		schema.Property("chapter", schema.Int(t.chapterNoDesc)).Required(),
	)
	stateChange := schema.Object(
		schema.Property("chapter", schema.Int(t.chapterNoDesc)).Required(),
		schema.Property("entity", schema.String(t.entityDesc)).Required(),
		schema.Property("field", schema.String(t.changedFieldDesc)).Required(),
		schema.Property("old_value", nullableString(t.oldValueDesc)).Required(),
		schema.Property("new_value", schema.String(t.newValueDesc)).Required(),
		schema.Property("reason", nullableString(t.changeReasonDesc)).Required(),
	)
	return schema.Object(
		schema.Property("chapter", schema.Int(t.chapterNoDesc)).Required(),
		schema.Property("title", schema.String(t.chapterTitleDesc)).Required(),
		schema.Property("summary", schema.String(t.summaryDesc)).Required(),
		schema.Property("key_events", stringList(t.keyEventsDesc)).Required(),
		schema.Property("core_event", schema.String(t.coreEventDesc)).Required(),
		schema.Property("hook", nullableString(t.hookDesc)).Required(),
		schema.Property("scenes", stringList(t.scenesDesc)).Required(),
		schema.Property("characters", stringList(t.castDesc)).Required(),
		schema.Property("character_evidence", schema.Array(t.characterEvidenceDesc, characterEvidence)).Required(),
		schema.Property("world_evidence", schema.Array(t.worldEvidenceDesc, worldEvidence)).Required(),
		schema.Property("timeline_events", schema.Array(t.timelineEventsDesc, timelineEvent)).Required(),
		schema.Property("foreshadow_updates", schema.Array(t.foreshadowUpdatesDesc, foreshadow)).Required(),
		schema.Property("relationship_changes", schema.Array(t.relationChangeDesc, relationship)).Required(),
		schema.Property("state_changes", schema.Array(t.stateChangesDesc, stateChange)).Required(),
		schema.Property("hook_type", schema.Enum(t.hookTypeDesc, domain.HookTypes()...)).Required(),
		schema.Property("dominant_strand", schema.Enum(t.dominantStrandDesc, domain.DominantStrands()...)).Required(),
	)
}

// rangeContract là hợp đồng tóm tắt một đoạn chương liên tục.
func rangeContract(t *importTexts) llmcontract.Contract {
	return llmcontract.Contract{
		Name:        "import_range_digest",
		Description: t.rangeDesc,
		Schema: schema.Object(
			schema.Property("start_chapter", schema.Int(t.rangeStartDesc)).Required(),
			schema.Property("end_chapter", schema.Int(t.rangeEndDesc)).Required(),
			schema.Property("plot", schema.String(t.rangePlotDesc)).Required(),
			schema.Property("characters", stringList(t.rangeCharsDesc)).Required(),
			schema.Property("world_facts", stringList(t.rangeWorldDesc)).Required(),
			schema.Property("opened_threads", stringList(t.rangeOpenedDesc)).Required(),
			schema.Property("resolved_threads", stringList(t.rangeResolvedDesc)).Required(),
		),
	}
}

// synthesisContract là hợp đồng tổng hợp toàn tác phẩm.
func synthesisContract(t *importTexts) llmcontract.Contract {
	return llmcontract.Contract{
		Name:        "import_book_synthesis",
		Description: t.synthesisDesc,
		Schema: schema.Object(
			schema.Property("title", nullableString(t.bookTitleDesc)).Required(),
			schema.Property("synopsis", schema.String(t.synopsisDesc)).Required(),
			schema.Property("premise", schema.String(t.premiseDesc)).Required(),
			schema.Property("characters", schema.Array(t.mainCharactersDesc, schema.Object(
				schema.Property("name", schema.String(t.characterNameDesc)).Required(),
				schema.Property("aliases", stringList(t.aliasesDesc)).Required(),
				schema.Property("role", schema.String(t.narrativeRoleDesc)).Required(),
				schema.Property("description", schema.String(t.characterDescDesc)).Required(),
				schema.Property("arc", schema.String(t.characterArcDesc)).Required(),
				schema.Property("traits", stringList(t.characterTraitsDesc)).Required(),
				schema.Property("tier", nullableString(t.characterTierDesc)).Required(),
			))).Required(),
			schema.Property("world_rules", schema.Array(t.worldRulesDesc, schema.Object(
				schema.Property("category", schema.String(t.ruleCategoryDesc)).Required(),
				schema.Property("rule", schema.String(t.ruleDesc)).Required(),
				schema.Property("boundary", schema.String(t.boundaryDesc)).Required(),
			))).Required(),
			schema.Property("structure", schema.Array(t.structureDesc, schema.Object(
				schema.Property("title", schema.String(t.volumeTitleDesc)).Required(),
				schema.Property("theme", schema.String(t.volumeThemeDesc)).Required(),
				schema.Property("arcs", schema.Array(t.volumeArcsDesc, schema.Object(
					schema.Property("title", schema.String(t.arcTitleDesc)).Required(),
					schema.Property("goal", schema.String(t.arcGoalDesc)).Required(),
					schema.Property("start_chapter", schema.Int(t.arcStartDesc)).Required(),
					schema.Property("end_chapter", schema.Int(t.arcEndDesc)).Required(),
				))).Required(),
			))).Required(),
			schema.Property("compass", schema.Object(
				schema.Property("ending_direction", schema.String(t.endingDirectionDesc)).Required(),
				schema.Property("open_threads", stringList(t.openThreadsDesc)).Required(),
				schema.Property("estimated_scale", nullableString(t.estimatedScaleDesc)).Required(),
				schema.Property("last_updated", llmcontract.Nullable(schema.Int(t.lastUpdatedDesc))).Required(),
			)).Required(),
			schema.Property("planning_tier", schema.Enum(t.planningTierDesc, "short", "mid", "long")).Required(),
			schema.Property("story_status", schema.Enum(t.storyStatusDesc, storyOpen, storyClosed, storyUncertain)).Required(),
			schema.Property("status_reason", nullableString(t.statusReasonDesc)).Required(),
		),
	}
}
