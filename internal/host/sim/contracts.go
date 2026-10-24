package sim

import (
	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
)

func textList(description string) map[string]any {
	return schema.Array(description, schema.String(description))
}

func sourceReportContract(texts *simTexts) llmcontract.Contract {
	return llmcontract.Contract{
		Name:        "simulation_source_report",
		Description: texts.sourceReportDescription,
		Schema: schema.Object(
			schema.Property("title", llmcontract.Nullable(schema.String(texts.sourceReportTitle))).Required(),
			schema.Property("summary", schema.String(texts.sourceReportSummary)).Required(),
			schema.Property("style_observations", textList(texts.sourceReportStyle)).Required(),
			schema.Property("common_words", textList(texts.sourceReportCommonWords)).Required(),
			schema.Property("plot_patterns", textList(texts.sourceReportPlot)).Required(),
			schema.Property("hook_patterns", textList(texts.sourceReportHooks)).Required(),
			schema.Property("pacing_notes", textList(texts.sourceReportPacing)).Required(),
			schema.Property("reader_appeal", textList(texts.sourceReportAppeal)).Required(),
			schema.Property("reusable_techniques", textList(texts.sourceReportTechniques)).Required(),
			schema.Property("warnings", textList(texts.sourceReportWarnings)).Required(),
		),
	}
}

func synthesisContract(texts *simTexts) llmcontract.Contract {
	return llmcontract.Contract{
		Name:        "simulation_synthesis",
		Description: texts.synthesisDescription,
		Schema: schema.Object(
			schema.Property("style", schema.Object(
				schema.Property("narrative_voice", textList(texts.style.narrativeVoice)).Required(),
				schema.Property("sentence_rhythm", textList(texts.style.sentenceRhythm)).Required(),
				schema.Property("prose_texture", textList(texts.style.proseTexture)).Required(),
				schema.Property("perspective", textList(texts.style.perspective)).Required(),
				schema.Property("mood", textList(texts.style.mood)).Required(),
				schema.Property("do_not_copy", textList(texts.style.doNotCopy)).Required(),
			)).Required(),
			schema.Property("lexicon", schema.Object(
				schema.Property("common_words", textList(texts.lexicon.commonWords)).Required(),
				schema.Property("emotion_words", textList(texts.lexicon.emotionWords)).Required(),
				schema.Property("scene_words", textList(texts.lexicon.sceneWords)).Required(),
				schema.Property("transition_words", textList(texts.lexicon.transitionWords)).Required(),
				schema.Property("signature_phrases", textList(texts.lexicon.signaturePhrases)).Required(),
			)).Required(),
			schema.Property("plot_design", schema.Object(
				schema.Property("opening_patterns", textList(texts.plot.openingPatterns)).Required(),
				schema.Property("escalation_patterns", textList(texts.plot.escalationPatterns)).Required(),
				schema.Property("turning_point_patterns", textList(texts.plot.turningPointPatterns)).Required(),
				schema.Property("payoff_patterns", textList(texts.plot.payoffPatterns)).Required(),
			)).Required(),
			schema.Property("hook_design", schema.Object(
				schema.Property("hook_types", textList(texts.hooks.hookTypes)).Required(),
				schema.Property("placement", textList(texts.hooks.placement)).Required(),
				schema.Property("cliffhanger_patterns", textList(texts.hooks.cliffhangerPatterns)).Required(),
				schema.Property("payoff_rules", textList(texts.hooks.payoffRules)).Required(),
			)).Required(),
			schema.Property("pacing_density", schema.Object(
				schema.Property("scene_density", textList(texts.pacing.sceneDensity)).Required(),
				schema.Property("information_release", textList(texts.pacing.informationRelease)).Required(),
				schema.Property("dialogue_action_ratio", textList(texts.pacing.dialogueActionRatio)).Required(),
				schema.Property("compression_rules", textList(texts.pacing.compressionRules)).Required(),
			)).Required(),
			schema.Property("reader_engagement", schema.Object(
				schema.Property("methods", textList(texts.engagement.methods)).Required(),
				schema.Property("emotional_drivers", textList(texts.engagement.emotionalDrivers)).Required(),
				schema.Property("progression_rewards", textList(texts.engagement.progressionRewards)).Required(),
				schema.Property("anti_patterns", textList(texts.engagement.antiPatterns)).Required(),
			)).Required(),
			schema.Property("role_guidance", schema.Object(
				schema.Property("architect", textList(texts.roles.architect)).Required(),
				schema.Property("writer", textList(texts.roles.writer)).Required(),
				schema.Property("editor", textList(texts.roles.editor)).Required(),
			)).Required(),
		),
	}
}
