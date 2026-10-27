package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// References là tài liệu tham khảo được nhúng vào.
type References struct {
	// V0
	ChapterGuide      string
	HookTechniques    string
	QualityChecklist  string
	OutlineTemplate   string
	CharacterTemplate string
	ChapterTemplate   string
	// V1
	Consistency      string
	ContentExpansion string
	DialogueWriting  string
	// V2
	StyleReference   string // tài liệu tham khảo bổ sung theo phong cách (có thể rỗng)
	LongformPlanning string // tài liệu tham khảo quy hoạch truyện dài dùng chung
	Differentiation  string // tài liệu tham khảo thiết kế khác biệt dùng chung
	ArcTemplates     string // khuôn mẫu cung theo thể loại (nạp theo style, có thể rỗng)
	AntiAITone       string // thư viện tiêu chí khửi mùi AI (writer/editor dùng chung, luôn chèn)
}

// ContextTool lắp ráp ngữ cảnh cần thiết cho chương hiện tại.
type ContextTool struct {
	store      *store.Store
	refs       References
	style      string
	styleStats *StyleStatsIndex
}

type contextReads struct {
	warnings []string
	seen     map[string]struct{}
	err      error
}

func (r *contextReads) warn(scope string, err error) {
	if err == nil || os.IsNotExist(err) {
		return
	}
	msg := fmt.Sprintf("%s đọc thất bại: %v", scope, err)
	if r.seen == nil {
		r.seen = make(map[string]struct{})
	}
	if _, ok := r.seen[msg]; ok {
		return
	}
	r.seen[msg] = struct{}{}
	r.warnings = append(r.warnings, msg)
}

func (r *contextReads) require(scope string, err error) {
	if r.err != nil || err == nil || os.IsNotExist(err) || errors.Is(err, store.ErrOutlineChapterNotFound) {
		return
	}
	r.err = fmt.Errorf("%s đọc thất bại: %w", scope, err)
}

// NewContextTool tạo công cụ ngữ cảnh. styleStats bắt buộc phải dùng chung với
// commit_chapter, nếu không thì sau khi viết lại chương, ngữ cảnh vẫn đọc thống kê cũ.
// user_rules được buildUserRules chèn thẳng từ ảnh chụp của tác phẩm
// (meta/user_rules.json), không còn phụ thuộc tùy chọn tải.
func NewContextTool(
	store *store.Store,
	refs References,
	style string,
	styleStats *StyleStatsIndex,
) *ContextTool {
	if styleStats == nil {
		panic("tools: NewContextTool requires StyleStatsIndex")
	}
	return &ContextTool{store: store, refs: refs, style: style, styleStats: styleStats}
}

func (t *ContextTool) Name() string      { return "novel_context" }
func (t *ContextTool) texts() *toolTexts { return toolTextsFor(t.store.Language()) }
func (t *ContextTool) Description() string {
	return t.texts().novelContextDesc
}
func (t *ContextTool) Label() string { return t.texts().novelContextLabel }

// Công cụ chỉ đọc, có thể được lập lịch đồng thời.
func (t *ContextTool) ReadOnly(_ json.RawMessage) bool        { return true }
func (t *ContextTool) ConcurrencySafe(_ json.RawMessage) bool { return true }

func (t *ContextTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("chapter", schema.Int(t.texts().novelContextArgChapter)),
	)
}

func (t *ContextTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter int `json:"chapter"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}

	result := make(map[string]any)
	reads := &contextReads{}

	if a.Chapter > 0 {
		// Nhánh writer: tải toàn bộ dữ liệu nền tảng + ngữ cảnh chương
		t.buildBaseContext(result, reads)
		seed := newChapterContextEnvelope()
		state := t.prepareChapterContext(a.Chapter, &seed, reads)
		seed.apply(result)
		t.buildChapterContext(result, state, reads)
		// Sự thật vi phạm cơ học của chương (được kiểm tra theo user_rules lúc commit
		// rồi ghi xuống đĩa): biên tập viên ánh xạ chúng vào bảy chiều
		// (editor.md §3b Quy tắc người dùng); người viết tự kiểm khi làm lại.
		if violations := t.store.World.LoadRuleViolations(a.Chapter); len(violations) > 0 {
			result["rule_violations"] = violations
		}
		// episodic là ghi chú đã viết vào nội dung truyện, không phải nguyên liệu chờ viết.
		if epi, ok := result["episodic_memory"].(map[string]any); ok && len(epi) > 0 {
			epi["_usage"] = "Conteneur này là ghi chú sự kiện đã viết vào văn bản chính (dùng để đối chiếu tính nhất quán và nối nối); trích lại nguyên bản nội dung này trong văn bản chương mới là lỗi trùng lặp"
		}
	} else {
		// Nhánh architect: chỉ trả về trạng thái + dữ liệu có cấu trúc, không tải toàn bộ văn bản gốc
		t.buildProgressStatus(result, reads)
		t.buildArchitectContext(result, reads)
	}

	// Chèn working_memory.user_rules (đường canonical). Nhánh architect vốn không có
	// working_memory, buildUserRules tạo mới container chỉ chứa user_rules khi cần.
	// Khi thiếu ảnh chụp thì lùi về giá trị mặc định tích hợp, luôn cho ra cấu trúc
	// ổn định để LLM không thấy user_rules=null rồi rẽ vào nhánh ngoại lệ.
	if a.Chapter > 0 {
		t.buildSimulationProfile(result, "working_memory", reads)
	} else {
		t.buildSimulationProfile(result, "planning_memory", reads)
	}

	t.buildUserRules(result, reads)

	if reads.err != nil {
		return nil, reads.err
	}
	if len(reads.warnings) > 0 {
		result["_warnings"] = reads.warnings
	}

	// Ngân sách ưu tiên: khi tổng kích thước vượt ngưỡng thì cắt bớt dữ liệu ưu tiên thấp;
	// phần tóm tắt được dựng lại sau khi cắt, đảm bảo số trường hiển thị và _trimmed
	// khớp với payload cuối cùng.
	budget := 60 * 1024
	if a.Chapter > 0 {
		budget = 100 * 1024
	}
	return finalizeContextPayload(result, a.Chapter, budget)
}

func finalizeContextPayload(result map[string]any, chapter, budget int) (json.RawMessage, error) {
	if err := trimByBudget(result, budget); err != nil {
		return nil, err
	}
	result["_loading_summary"] = buildLoadingSummary(result, chapter)
	if err := trimByBudget(result, budget); err != nil {
		return nil, err
	}
	result["_loading_summary"] = buildLoadingSummary(result, chapter)

	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal context payload: %w", err)
	}
	if len(data) > budget {
		return nil, fmt.Errorf("context payload exceeds budget after summary rebuild: size=%d budget=%d", len(data), budget)
	}
	return data, nil
}

// buildLoadingSummary thống kê khối lượng từng loại dữ liệu trong result đã lắp ráp,
// sinh ra một dòng tóm tắt dễ đọc.
func buildLoadingSummary(result map[string]any, chapter int) string {
	var parts []string
	working, _ := result["working_memory"].(map[string]any)
	episodic, _ := result["episodic_memory"].(map[string]any)
	planning, _ := result["planning_memory"].(map[string]any)
	foundation, _ := result["foundation_memory"].(map[string]any)
	referencePack, _ := result["reference_pack"].(map[string]any)

	if chapter > 0 {
		parts = append(parts, fmt.Sprintf("ch=%d", chapter))
		if tier, ok := episodic["planning_tier"].(domain.PlanningTier); ok && tier != "" {
			parts = append(parts, fmt.Sprintf("tier=%s", tier))
		}
	} else {
		parts = append(parts, "architect")
		if tier, ok := planning["planning_tier"].(domain.PlanningTier); ok && tier != "" {
			parts = append(parts, fmt.Sprintf("tier=%s", tier))
		}
	}

	if pos, ok := episodic["position"].(map[string]any); ok {
		parts = append(parts, fmt.Sprintf("V%dA%d", pos["volume"], pos["arc"]))
	}

	var items []string

	if n := firstSliceLen(episodic["character_snapshots"], foundation["character_snapshots"]); n > 0 {
		items = append(items, fmt.Sprintf("Nhân vật:%d (chụp)", n))
	} else if n := firstSliceLen(episodic["characters"], foundation["characters"]); n > 0 {
		items = append(items, fmt.Sprintf("Nhân vật:%d", n))
	}

	if len(working) > 0 {
		items = append(items, fmt.Sprintf("Bộ nhớ làm việc:%d", len(working)))
	}
	if len(episodic) > 0 {
		items = append(items, fmt.Sprintf("Bộ nhớ sự kiện:%d", len(episodic)))
	}
	if len(planning) > 0 {
		items = append(items, fmt.Sprintf("Bộ nhớ kế hoạch:%d", len(planning)))
	}
	if len(foundation) > 0 {
		items = append(items, fmt.Sprintf("Bộ nhớ cơ sở:%d", len(foundation)))
	}

	if n := firstSliceLen(working["volume_summaries"], planning["volume_summaries"]); n > 0 {
		items = append(items, fmt.Sprintf("Tóm tắt tập:%d", n))
	}
	if n := firstSliceLen(working["arc_summaries"], planning["arc_summaries"]); n > 0 {
		items = append(items, fmt.Sprintf("Tóm tắt cung:%d", n))
	}
	if n := sliceLen(working["recent_summaries"]); n > 0 {
		items = append(items, fmt.Sprintf("Tóm tắt chapter:%d", n))
	}

	if n := sliceLen(planning["layered_outline"]); n > 0 {
		items = append(items, fmt.Sprintf("Đề cương phân tầng:%d tập", n))
	}

	if n := sliceLen(working["timeline"]); n > 0 {
		items = append(items, fmt.Sprintf("Dòng thời gian:%d", n))
	}
	if n := firstSliceLen(episodic["foreshadow_ledger"], foundation["foreshadow_ledger"]); n > 0 {
		items = append(items, fmt.Sprintf("Phục bút:%d", n))
	}
	if n := sliceLen(episodic["relationship_state"]); n > 0 {
		items = append(items, fmt.Sprintf("Mối quan hệ:%d", n))
	}
	if n := sliceLen(episodic["recent_state_changes"]); n > 0 {
		items = append(items, fmt.Sprintf("Thay đổi trạng thái:%d", n))
	}
	if _, ok := working["previous_tail"]; ok {
		items = append(items, "Cuối chương trước:ok")
	}
	if _, ok := referencePack["style_rules"]; ok {
		items = append(items, "Quy tắc phong cách:ok")
	}
	if n := sliceLen(episodic["related_chapters"]); n > 0 {
		items = append(items, fmt.Sprintf("Chương liên quan:%d", n))
	}
	if selected, ok := result["selected_memory"].(map[string]any); ok && len(selected) > 0 {
		if n := sliceLen(selected["story_threads"]); n > 0 {
			items = append(items, fmt.Sprintf("Gọi lại manh mối:%d", n))
		}
		if n := sliceLen(selected["review_lessons"]); n > 0 {
			items = append(items, fmt.Sprintf("Gọi lại đánh giá:%d", n))
		}
	}

	if refs, ok := referencePack["references"].(map[string]string); ok && len(refs) > 0 {
		items = append(items, fmt.Sprintf("Tài liệu tham khảo:%d mục", len(refs)))
	}
	if len(referencePack) > 0 {
		items = append(items, fmt.Sprintf("Gói tài liệu tham khảo:%d", len(referencePack)))
	}
	if _, ok := result["memory_policy"]; ok {
		items = append(items, "Chính sách bộ nhớ:ok")
	}
	if _, ok := working["simulation_profile"]; ok {
		items = append(items, "Hồ sơ mô phỏng:ok")
	} else if _, ok := planning["simulation_profile"]; ok {
		items = append(items, "Hồ sơ mô phỏng:ok")
	}
	if warnings, ok := result["_warnings"].([]string); ok && len(warnings) > 0 {
		items = append(items, fmt.Sprintf("Cảnh báo:%d", len(warnings)))
	}
	if trimmed, ok := result["_trimmed"].([]string); ok && len(trimmed) > 0 {
		items = append(items, fmt.Sprintf("Cắt giảm:%s", strings.Join(trimmed, ",")))
	}

	if len(items) > 0 {
		parts = append(parts, strings.Join(items, " "))
	}
	return strings.Join(parts, " | ")
}

// sliceLen thử lấy độ dài slice của một giá trị any.
func sliceLen(v any) int {
	switch s := v.(type) {
	case []domain.ChapterSummary:
		return len(s)
	case []domain.ArcSummary:
		return len(s)
	case []domain.VolumeSummary:
		return len(s)
	case []domain.CharacterSnapshot:
		return len(s)
	case []domain.TimelineEvent:
		return len(s)
	case []domain.ForeshadowEntry:
		return len(s)
	case []domain.RelationshipEntry:
		return len(s)
	case []domain.StateChange:
		return len(s)
	case []domain.VolumeOutline:
		return len(s)
	case []domain.Character:
		return len(s)
	case []domain.RelatedChapter:
		return len(s)
	case []domain.RecallItem:
		return len(s)
	case []planningVolumeOutline:
		return len(s)
	default:
		return 0
	}
}

func firstSliceLen(values ...any) int {
	for _, value := range values {
		if n := sliceLen(value); n > 0 {
			return n
		}
	}
	return 0
}

// loadFilteredCharacters lọc nhân vật theo Tier và theo việc xuất hiện trong bối cảnh.
// core/important luôn được trả về; secondary/decorative chỉ được trả về khi đề cương
// chương hiện tại nhắc tới.
func (t *ContextTool) loadFilteredCharacters(result map[string]any, chapter int, reads *contextReads) {
	chars, err := t.store.Characters.Load()
	if err != nil {
		reads.require("characters", err)
		return
	}
	if len(chars) == 0 {
		return
	}

	// Lấy mô tả bối cảnh trong đề cương chương hiện tại, dùng để khớp nhân vật phụ
	entry, err := t.store.Outline.GetChapterOutline(chapter)
	if err != nil {
		reads.require("current_chapter_outline", err)
		result["characters"] = chars
		return
	}
	if entry == nil {
		result["characters"] = chars
		return
	}
	sceneText := strings.Join(entry.Scenes, " ") + " " + entry.CoreEvent + " " + entry.Title

	var filtered []domain.Character
	for _, c := range chars {
		switch c.Tier {
		case "secondary", "decorative":
			if matchCharacter(sceneText, c) {
				filtered = append(filtered, c)
			}
		default: // core, important, hoặc chưa đặt
			filtered = append(filtered, c)
		}
	}
	result["characters"] = filtered
}

// matchCharacter kiểm tra văn bản bối cảnh có chứa tên chính thức hay bất kỳ bí danh
// nào của nhân vật không.
func matchCharacter(text string, c domain.Character) bool {
	if strings.Contains(text, c.Name) {
		return true
	}
	for _, alias := range c.Aliases {
		if strings.Contains(text, alias) {
			return true
		}
	}
	return false
}

// loadLayeredSummaries tải tóm tắt phân tầng: tóm tắt tập + tóm tắt cung của tập
// hiện tại + tóm tắt chương trong cung.
func (t *ContextTool) loadLayeredSummaries(result map[string]any, chapter, summaryWindow int, reads *contextReads) {
	vol, arc, err := t.store.Outline.LocateChapter(chapter)
	if err != nil {
		reads.require("layered_outline_position", err)
		return
	}

	// 1. Tóm tắt tập của các tập đã hoàn thành
	if volSummaries, err := t.store.Summaries.LoadAllVolumeSummaries(); err == nil && len(volSummaries) > 0 {
		result["volume_summaries"] = volSummaries
	} else {
		reads.require("volume_summaries", err)
	}

	// 2. Tóm tắt cung của các cung đã hoàn thành trong tập hiện tại (không gồm cung hiện tại)
	if arcSummaries, err := t.store.Summaries.LoadArcSummaries(vol); err == nil && len(arcSummaries) > 0 {
		var prior []domain.ArcSummary
		for _, s := range arcSummaries {
			if s.Arc < arc {
				prior = append(prior, s)
			}
		}
		if len(prior) > 0 {
			result["arc_summaries"] = prior
		}
	} else {
		reads.require("arc_summaries", err)
	}

	// 3. Tóm tắt chương của N chương gần nhất trong cung hiện tại
	if summaries, err := t.store.Summaries.LoadRecentSummaries(chapter, summaryWindow); err == nil && len(summaries) > 0 {
		result["recent_summaries"] = summaries
	} else {
		reads.require("recent_summaries", err)
	}
}

// loadLayeredCharacters tải nhân vật ở chế độ Layered: ưu tiên ảnh chụp gần nhất,
// lùi về thiết lập gốc + lọc theo Tier.
func (t *ContextTool) loadLayeredCharacters(result map[string]any, chapter int, reads *contextReads) {
	snapshots, err := t.store.Characters.LoadLatestSnapshots()
	if err == nil && len(snapshots) > 0 {
		result["character_snapshots"] = snapshots
		// Vẫn giữ các nhân vật core/important trong thiết lập gốc (ảnh chụp có thể không có
		// nhân vật mới xuất hiện)
		t.loadFilteredCharacters(result, chapter, reads)
		return
	}
	reads.require("character_snapshots", err)
	// Không có ảnh chụp thì lùi về thiết lập gốc
	t.loadFilteredCharacters(result, chapter, reads)
}

// writerReferences trả về tài liệu tham khảo cho việc viết. Chương 1 trả về toàn bộ,
// các chương sau cắt bớt những template không còn cần.
func (t *ContextTool) writerReferences(chapter int) map[string]string {
	refs := map[string]string{}
	add := func(k, v string) {
		if v != "" {
			refs[k] = v
		}
	}
	// Tải tiện dần: luôn giữ phần tham khảo lõi, 3 chương đầu còn tải thêm hướng dẫn viết đầy đủ
	add("consistency", t.refs.Consistency)
	add("hook_techniques", t.refs.HookTechniques)
	add("quality_checklist", t.refs.QualityChecklist)
	add("anti_ai_tone", t.refs.AntiAITone) // tiêu chí khửi mùi AI luôn được chèn đầy đủ, không bị cắt bớt theo chương
	if chapter <= 3 {
		add("chapter_guide", t.refs.ChapterGuide)
		add("dialogue_writing", t.refs.DialogueWriting)
		add("style_reference", t.refs.StyleReference)
	}

	// Tài liệu tham khảo bổ sung chỉ được tải ở chương đầu
	if chapter <= 1 {
		add("chapter_template", t.refs.ChapterTemplate)
		add("content_expansion", t.refs.ContentExpansion)
	}
	return refs
}

func (t *ContextTool) architectReferences() map[string]string {
	refs := map[string]string{}
	add := func(k, v string) {
		if v != "" {
			refs[k] = v
		}
	}
	add("outline_template", t.refs.OutlineTemplate)
	add("character_template", t.refs.CharacterTemplate)
	add("longform_planning", t.refs.LongformPlanning)
	add("differentiation", t.refs.Differentiation)
	add("style_reference", t.refs.StyleReference)
	add("arc_templates", t.refs.ArcTemplates)
	add("anti_ai_tone", t.refs.AntiAITone) // dàn ý của architect khửi mùi AI; cũng phủ cho editor đi theo nhánh Chapter=0
	return refs
}

// foundationStatus kiểm tra độ đầy đủ của thiết lập nền tảng, trả về danh sách phần
// còn thiếu.
// Dùng chung logic phán quyết store.FoundationMissing với công cụ save_foundation, bảo
// đảm ready/missing mà LLM thấy từ novel_context luôn khớp với foundation_ready mà
// save_foundation trả về (các chi tiết như mục bắt buộc của compass trong tiểu thuyết
// dài không bị trôi lệch).
func (t *ContextTool) foundationStatus() (map[string]any, error) {
	missing, err := t.store.FoundationMissing()
	if err != nil {
		return nil, err
	}
	status := map[string]any{"ready": len(missing) == 0}
	if len(missing) > 0 {
		status["missing"] = missing
	}
	if len(missing) == 1 && missing[0] == "foundation_audit" {
		fingerprint, err := t.store.FoundationFingerprint()
		if err != nil {
			return nil, err
		}
		status["fingerprint"] = fingerprint
	}
	if audit, err := t.store.Outline.LoadFoundationAudit(); err != nil {
		return nil, err
	} else if audit != nil && !audit.Ready {
		status["last_audit"] = audit
	}
	return status, nil
}

// trimByBudget cắt bớt result theo mức ưu tiên để tổng kích thước JSON không vượt budget
// byte.
// Mức ưu tiên (từ thấp đến cao): references < voice_samples < style_anchors < previous_tail
// < timeline
//
//	< recent_state_changes < foreshadow_ledger < relationship_state < phần còn lại (không cắt)
//
// style_stats là tín hiệu lõi ở cấp toàn truyện với thể tích có kiểm soát, không tham gia
// cắt bớt.
//
// Các key bị cắt được ghi lại vào result["_trimmed"] để truy khi trong nhật ký.
func trimByBudget(result map[string]any, budget int) error {
	// Đo kích thước hiện tại trước
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("measure context payload: %w", err)
	}
	if len(data) <= budget {
		return nil
	}

	// Liệt kê các key có thể cắt theo mức ưu tiên từ thấp đến cao
	trimOrder := []string{
		"references",
		"voice_samples",
		"style_anchors",
		"style_rules",
		"previous_tail",
		"timeline",
		"recent_state_changes",
		"foreshadow_ledger",
		"relationship_state",
	}

	trimmed, _ := result["_trimmed"].([]string)
	trimmed = append([]string(nil), trimmed...)
	for _, key := range trimOrder {
		if !deleteContextKey(result, key) {
			continue
		}
		trimmed = append(trimmed, key)
		result["_trimmed"] = append([]string(nil), trimmed...)
		data, err = json.Marshal(result)
		if err != nil {
			return fmt.Errorf("measure trimmed context payload: %w", err)
		}
		if len(data) <= budget {
			return nil
		}
	}
	return fmt.Errorf("context payload exceeds budget after trimming: size=%d budget=%d", len(data), budget)
}

func deleteContextKey(result map[string]any, key string) bool {
	deleted := false
	for _, containerKey := range []string{
		"working_memory",
		"episodic_memory",
		"planning_memory",
		"foundation_memory",
		"reference_pack",
		"selected_memory",
	} {
		section, ok := result[containerKey].(map[string]any)
		if !ok {
			continue
		}
		if _, ok := section[key]; ok {
			delete(section, key)
			deleted = true
		}
	}
	return deleted
}

// buildRelatedChapters dựa trên dữ liệu có cấu trúc để tra ngược các chương trước liên
// quan tới chương hiện tại.
// Đề xuất theo bốn chiều: phục bút, nhân vật xuất hiện, thay đổi trạng thái, quan hệ;
// khử trùng lặp rồi trả về tối đa 5 mục.
// Mọi dữ liệu đều truyền qua tham số, không thực hiện IO bổ sung.
func (t *ContextTool) buildRelatedChapters(
	chapter int,
	entry *domain.OutlineEntry,
	foreshadow []domain.ForeshadowEntry,
	relationships []domain.RelationshipEntry,
	stateChanges []domain.StateChange,
	reads *contextReads,
) []domain.RelatedChapter {
	const recentWindow = 10
	const maxResults = 5

	seen := make(map[int]struct{})
	var results []domain.RelatedChapter
	add := func(ch int, reason string) {
		if ch <= 0 || ch >= chapter {
			return
		}
		// Mấy chương gần nhất quá gần, không đề xuất
		if ch > chapter-recentWindow {
			return
		}
		if _, ok := seen[ch]; ok {
			return
		}
		seen[ch] = struct{}{}
		results = append(results, domain.RelatedChapter{Chapter: ch, Reason: reason})
	}

	// Ghép văn bản đề cương để khớp từ khoá
	outlineText := entry.Title + " " + entry.CoreEvent
	for _, s := range entry.Scenes {
		outlineText += " " + s
	}

	// 1. Tra ngược phục bút: mô tả của phục bút đang hoạt động có liên quan tới đề cương
	// chương hiện tại không
	for _, f := range foreshadow {
		if strings.Contains(outlineText, f.ID) || containsAny(outlineText, strings.Fields(f.Description)) {
			add(f.PlantedAt, fmt.Sprintf("Phục bút%s(%s) đã cấy tại chương", f.ID, truncateRunes(f.Description, 15)))
		}
		if len(results) >= maxResults {
			break
		}
	}

	// 2. Tra ngược nhân vật xuất hiện: quét hàng loạt trong một lượt, IO từ
	// O(số nhân vật×số chương) giảm còn O(số chương)
	chars, err := t.store.Characters.Load()
	if err != nil {
		reads.warn("related_chapters.characters", err)
	}
	outlineChars := matchOutlineCharacters(outlineText, chars)
	if len(outlineChars) > 0 {
		appearances, err := t.store.Summaries.FindCharacterAppearances(outlineChars, chapter, recentWindow)
		if err != nil {
			reads.warn("related_chapters.summaries", err)
		}
		for _, name := range outlineChars {
			if len(results) >= maxResults {
				break
			}
			if ch, ok := appearances[name]; ok {
				add(ch, fmt.Sprintf("Chương xuất hiện cuối của nhân vật '%s'", name))
			}
		}
	}

	// 3. Tra ngược thay đổi trạng thái: thao tác trên slice đã tải, không tốn IO
	for _, name := range outlineChars {
		if len(results) >= maxResults {
			break
		}
		ch := findLastStateChange(stateChanges, name, chapter)
		if ch > 0 && ch <= chapter-recentWindow {
			add(ch, fmt.Sprintf("Chương '%s' thay đổi trạng thái", name))
		}
	}

	// 4. Tra ngược quan hệ: lần thay đổi quan hệ gần nhất giữa các cặp nhân vật liên quan
	// tới chương hiện tại
	if len(relationships) > 0 && len(outlineChars) >= 2 {
		charSet := make(map[string]struct{}, len(outlineChars))
		for _, c := range outlineChars {
			charSet[c] = struct{}{}
		}
		for _, r := range relationships {
			if len(results) >= maxResults {
				break
			}
			_, aIn := charSet[r.CharacterA]
			_, bIn := charSet[r.CharacterB]
			if aIn && bIn {
				add(r.Chapter, fmt.Sprintf("%s-%s thay đổi mối quan hệ", r.CharacterA, r.CharacterB))
			}
		}
	}

	return results
}

// findLastStateChange tìm số chương của lần thay đổi gần nhất của một thực thể trong
// danh sách thay đổi trạng thái đã tải.
func findLastStateChange(changes []domain.StateChange, entity string, currentChapter int) int {
	for i := len(changes) - 1; i >= 0; i-- {
		if changes[i].Entity == entity && changes[i].Chapter < currentChapter {
			return changes[i].Chapter
		}
	}
	return 0
}

// matchOutlineCharacters khớp tên nhân vật xuất hiện từ văn bản đề cương.
func matchOutlineCharacters(text string, chars []domain.Character) []string {
	var matched []string
	for _, c := range chars {
		if strings.Contains(text, c.Name) {
			matched = append(matched, c.Name)
			continue
		}
		for _, alias := range c.Aliases {
			if strings.Contains(text, alias) {
				matched = append(matched, c.Name)
				break
			}
		}
	}
	return matched
}

// containsAny kiểm tra text có chứa bất kỳ từ nào trong words không (từ nào dưới 2 ký
// tự thì không khớp, để tránh nhiễu).
func containsAny(text string, words []string) bool {
	for _, w := range words {
		if len([]rune(w)) >= 2 && strings.Contains(text, w) {
			return true
		}
	}
	return false
}

func (t *ContextTool) selectStoryThreads(state contextBuildState) []domain.RecallItem {
	if state.currentEntry == nil {
		return nil
	}
	if len(state.foreshadow) < storyThreadRecallThreshold {
		return nil
	}

	const maxThreads = 5
	var items []domain.RecallItem
	seen := make(map[string]struct{})
	picked := make(map[string]struct{}) // ID phục bút đã chọn, dùng để khửi trùng lặp khi lấp đầy tuổi treo
	add := func(item domain.RecallItem) {
		key := item.Kind + "|" + item.Key + "|" + item.Summary
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		picked[item.Key] = struct{}{}
		items = append(items, item)
	}

	// 1. Gọi lại theo mức liên quan: các phục bút có từ focus của chương hiện tại trùng lặp.
	focusTerms := recallFocusTerms(state.currentEntry, state.chapterPlan)
	focusText := strings.Join(focusTerms, " ")
	for _, entry := range state.foreshadow {
		if !matchesRecallTerms(entry.ID+" "+entry.Description, focusTerms) && !strings.Contains(focusText, entry.ID) {
			continue
		}
		add(domain.RecallItem{
			Kind:    "story_thread",
			Key:     entry.ID,
			Chapter: entry.PlantedAt,
			Reason:  "Chương hiện tại có thể cần kế nối các phục bút hiện có",
			Summary: fmt.Sprintf("Phục bút “%s” đã được cấy tại chương %d：%s", entry.ID, entry.PlantedAt, truncateRunes(entry.Description, 30)),
		})
		if len(items) >= maxThreads {
			return items
		}
	}

	// 2. Lấp đầy theo tuổi treo: các phục bút không liên quan tới chương hiện tại nhưng đã
	//    treo quá lâu chưa được thu hồi (cũ nhất trước), lấp đủ số suất còn lại.
	//    Đây chính là vùng mù mà gọi lại theo mức liên quan không thấy — tuyến treo một
	//    mình quá lâu mà không chạm từ khoá ở chương này.
	for _, entry := range agingForeshadow(state.foreshadow, state.chapter, picked) {
		add(domain.RecallItem{
			Kind:    "story_thread",
			Key:     entry.ID,
			Chapter: entry.PlantedAt,
			Reason:  "Phục bút treo lâu chưa thu hồi, chú ý đẩy diễn tiến hoặc thu hồi kịp thời",
			Summary: fmt.Sprintf("Phục bút “%s” đã được cấy tại chương %d, đã %d chương chưa thu hồi：%s", entry.ID, entry.PlantedAt, state.chapter-entry.PlantedAt, truncateRunes(entry.Description, 30)),
		})
		if len(items) >= maxThreads {
			break
		}
	}

	return items
}

// agingForeshadow trả về các phục bút chưa thu hồi có tuổi treo ≥
// foreshadowAgingChapters, sắp xếp cũ nhất trước, bỏ qua những cái đã được gọi lại theo
// mức liên quan chọn trong picked.
// Tham số all vốn đã là danh sách active (chưa thu hồi) nên không cần lọc lại trạng thái.
func agingForeshadow(all []domain.ForeshadowEntry, chapter int, picked map[string]struct{}) []domain.ForeshadowEntry {
	var aging []domain.ForeshadowEntry
	for _, e := range all {
		if _, ok := picked[e.ID]; ok {
			continue
		}
		if e.PlantedAt <= 0 || chapter-e.PlantedAt < foreshadowAgingChapters {
			continue
		}
		aging = append(aging, e)
	}
	sort.SliceStable(aging, func(i, j int) bool {
		return aging[i].PlantedAt < aging[j].PlantedAt
	})
	return aging
}

func (t *ContextTool) selectReviewLessons(chapter int, reads *contextReads) []domain.RecallItem {
	if chapter <= 1 {
		return nil
	}

	var items []domain.RecallItem
	seen := make(map[string]struct{})
	add := func(item domain.RecallItem) {
		key := item.Summary
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		items = append(items, item)
	}

	appendReview := func(review *domain.ReviewEntry) bool {
		if review == nil {
			return false
		}
		for i, miss := range review.ContractMisses {
			add(domain.RecallItem{
				Kind:    "review_lesson",
				Key:     fmt.Sprintf("review-%d-contract-%d", review.Chapter, i),
				Chapter: review.Chapter,
				Reason:  "Đánh giá gần đây chỉ ra contract bị bỏ sót",
				Summary: fmt.Sprintf("Chương %d contract bị bỏ sót：%s", review.Chapter, miss),
			})
			if len(items) >= 3 {
				return true
			}
		}
		for i, issue := range review.Issues {
			switch issue.Severity {
			case "", "warning", "error", "critical":
				add(domain.RecallItem{
					Kind:    "review_lesson",
					Key:     fmt.Sprintf("review-%d-issue-%d", review.Chapter, i),
					Chapter: review.Chapter,
					Reason:  "Đánh giá gần đây chỉ ra cần tránh vấn đề trùng lặp",
					Summary: fmt.Sprintf("Chương %d nhắc nhở đánh giá：%s", review.Chapter, truncateRunes(issue.Description, 36)),
				})
			}
			if len(items) >= 3 {
				return true
			}
		}
		return false
	}

	for ch := chapter - 1; ch >= max(chapter-3, 1); ch-- {
		review, err := t.store.World.LoadReview(ch)
		if err != nil {
			reads.warn("review", err)
			continue
		}
		if appendReview(review) {
			return items
		}
	}

	globalReview, err := t.store.World.LoadLastReview(chapter - 1)
	if err != nil {
		reads.warn("global_review", err)
	} else if appendReview(globalReview) {
		return items
	}
	return items
}

func recallFocusTerms(entry *domain.OutlineEntry, plan *domain.ChapterPlan) []string {
	if entry == nil {
		return nil
	}
	var terms []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			terms = append(terms, v)
		}
	}

	add(entry.Title)
	add(entry.CoreEvent)
	add(entry.Hook)
	for _, scene := range entry.Scenes {
		add(scene)
	}
	if plan != nil {
		add(plan.Goal)
		add(plan.Hook)
		for _, point := range plan.Contract.PayoffPoints {
			add(point)
		}
		add(plan.Contract.HookGoal)
	}
	return terms
}

func matchesRecallTerms(text string, terms []string) bool {
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if len([]rune(term)) < 2 {
			continue
		}
		if strings.Contains(text, term) || strings.Contains(term, text) {
			return true
		}
		if hasMeaningfulOverlap(term, text) {
			return true
		}
	}
	return false
}

func hasMeaningfulOverlap(a, b string) bool {
	ar := []rune(strings.TrimSpace(a))
	br := []rune(strings.TrimSpace(b))
	if len(ar) < 5 || len(br) < 5 {
		return false
	}
	shorter := len(ar)
	if len(br) < shorter {
		shorter = len(br)
	}
	threshold := 5
	switch {
	case shorter >= 12:
		threshold = 7
	case shorter >= 9:
		threshold = 6
	}
	return longestCommonSubstringRunes(ar, br) >= threshold
}

const storyThreadRecallThreshold = 6
const storyThreadRecallMinSelected = 2

// foreshadowAgingChapters: một phục bút kể từ lúc gieo mà quá nhiêu chương vẫn chưa được
// thu hồi thì bị coi là "treo lâu".
// Loại phục bút này, dù không liên quan tới từ khoá chương hiện tại, vẫn được lấp đầy vào
// story_threads để khỏi bị quên hẳn trong tiểu thuyết dài (gọi lại theo mức liên quan
// vốn chỉ thấy các tuyến liên quan chương, không thấy tuyến treo một mình quá lâu).
// Tuổi treo là sự thật thuần do code suy ra (chương hiện tại - chương gieo), chỉ nêu
// "đã treo N chương chưa thu hồi", không đưa ra mệnh lệnh.
const foreshadowAgingChapters = 30

func longestCommonSubstringRunes(a, b []rune) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	prev := make([]int, len(b)+1)
	best := 0
	for i := 1; i <= len(a); i++ {
		curr := make([]int, len(b)+1)
		for j := 1; j <= len(b); j++ {
			if a[i-1] != b[j-1] {
				continue
			}
			curr[j] = prev[j-1] + 1
			if curr[j] > best {
				best = curr[j]
			}
		}
		prev = curr
	}
	return best
}

// truncateRunes cắt ngắn chuỗi còn đúng số rune đã chỉ định.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
