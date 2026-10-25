package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// PlanChapterTool lưu ý tưởng sáng tác của chương, Agent tự quyết định độ mịn
// của việc quy hoạch.
type PlanChapterTool struct {
	store *store.Store
}

func NewPlanChapterTool(store *store.Store) *PlanChapterTool {
	return &PlanChapterTool{store: store}
}

// texts lấy bảng văn bản theo ngôn ngữ tác phẩm, đọc lười mỗi lần gọi.
func (t *PlanChapterTool) texts() *writeTexts { return writeTextsFor(t.store.Language()) }

func (t *PlanChapterTool) Name() string { return "plan_chapter" }
func (t *PlanChapterTool) Description() string {
	return t.texts().planChapterDesc
}
func (t *PlanChapterTool) Label() string { return t.texts().planChapterLabel }

// Công cụ ghi, cấm chạy song song.
func (t *PlanChapterTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *PlanChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *PlanChapterTool) Schema() map[string]any {
	texts := t.texts()
	return schema.Object(
		schema.Property("chapter", schema.Int(texts.planChapterArgChapter)).Required(),
		schema.Property("title", schema.String(texts.planChapterArgTitle)).Required(),
		schema.Property("goal", schema.String(texts.planChapterArgGoal)).Required(),
		schema.Property("conflict", schema.String(texts.planChapterArgConflict)).Required(),
		schema.Property("hook", schema.String(texts.planChapterArgHook)).Required(),
		schema.Property("emotion_arc", schema.String(texts.planChapterArgEmotionArc)),
		schema.Property("notes", schema.String(texts.planChapterArgNotes)),
		schema.Property("required_beats", schema.Array(texts.planChapterArgRequiredBeats, schema.String(""))),
		schema.Property("forbidden_moves", schema.Array(texts.planChapterArgForbiddenMoves, schema.String(""))),
		schema.Property("continuity_checks", schema.Array(texts.planChapterArgContinuityChecks, schema.String(""))),
		schema.Property("evaluation_focus", schema.Array(texts.planChapterArgEvaluationFocus, schema.String(""))),
		schema.Property("emotion_target", schema.String(texts.planChapterArgEmotionTarget)),
		schema.Property("payoff_points", schema.Array(texts.planChapterArgPayoffPoints, schema.String(""))),
		schema.Property("hook_goal", schema.String(texts.planChapterArgHookGoal)),
	)
}

func (t *PlanChapterTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	plan, err := decodeChapterPlanArgs(args)
	if err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if plan.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}
	progress, err := t.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	completed := progress != nil && slices.Contains(progress.CompletedChapters, plan.Chapter)
	// Chương nằm trong hàng chờ làm lại là ngoại lệ duy nhất: khi đã hoàn thành
	// mà phải viết lại, "viết lại thành cái gì" cần có chỗ để ghi xuống, và
	// Contract (goal / payoff_points / continuity_checks / hook_goal) chính là
	// chỉ dẫn đó. Trước đây ở đây luôn từ chối, khiến revise_outline (chỉ cho
	// sửa chương chưa viết), save_foundation(outline) (cấm ghi đè toàn bộ trong
	// giai đoạn sáng tác) và công cụ này chặn kín cả ba đường — kiến trúc sư
	// không còn chỗ để viết hướng làm lại, thực đo cho thấy quay vòng 4 lượt
	// rồi bị ngắt mạch.
	queuedForRewrite := progress != nil && slices.Contains(progress.PendingRewrites, plan.Chapter)
	if completed && !queuedForRewrite {
		return json.Marshal(map[string]any{
			"chapter":   plan.Chapter,
			"skipped":   true,
			"completed": true,
			"reason":    fmt.Sprintf(t.texts().planChapterCompleted, plan.Chapter),
		})
	}
	if err := t.store.Progress.ValidateChapterWork(plan.Chapter); err != nil {
		return nil, err
	}
	if err := EnsureChapterExpanded(t.store, plan.Chapter); err != nil {
		return nil, err
	}

	if err := t.store.Drafts.SaveChapterPlan(plan); err != nil {
		return nil, fmt.Errorf("save chapter plan: %w", err)
	}
	// Ghi lệnh làm lại không đồng nghĩa với bắt đầu viết chương đó: Engine đã
	// đánh dấu "đang thực hiện" ngay khi phân phối tới Người viết
	// (engine.go), và draft_chapter lại đánh dấu thêm một lần khi chạm bút, nên
	// ở đây thừa với chương đang xếp hàng làm lại.
	// Mà StartChapter vô điều kiện ghi đè InProgressChapter và xóa
	// CompletedScenes — nếu người quy hoạch viết lệnh cho chương 20 trong hàng
	// chờ trong khi Người viết đang ở chương 13, con trỏ sẽ bị kéo đi.
	if !queuedForRewrite || progress.InProgressChapter == plan.Chapter {
		if err := t.store.Progress.StartChapter(plan.Chapter); err != nil {
			return nil, fmt.Errorf("mark chapter in progress: %w", err)
		}
	}

	if _, err := t.store.Checkpoints.AppendArtifact(
		domain.ChapterScope(plan.Chapter), "plan",
		fmt.Sprintf("drafts/%02d.plan.json", plan.Chapter),
	); err != nil {
		return nil, fmt.Errorf("checkpoint chapter plan: %w", err)
	}

	return json.Marshal(map[string]any{
		"planned":   true,
		"chapter":   plan.Chapter,
		"next_step": t.texts().planChapterNextStep,
	})
}

func decodeChapterPlanArgs(args json.RawMessage) (domain.ChapterPlan, error) {
	var a struct {
		Chapter          int      `json:"chapter"`
		Title            string   `json:"title"`
		Goal             string   `json:"goal"`
		Conflict         string   `json:"conflict"`
		Hook             string   `json:"hook"`
		EmotionArc       string   `json:"emotion_arc"`
		Notes            string   `json:"notes"`
		RequiredBeats    []string `json:"required_beats"`
		ForbiddenMoves   []string `json:"forbidden_moves"`
		ContinuityChecks []string `json:"continuity_checks"`
		EvaluationFocus  []string `json:"evaluation_focus"`
		EmotionTarget    string   `json:"emotion_target"`
		PayoffPoints     []string `json:"payoff_points"`
		HookGoal         string   `json:"hook_goal"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return domain.ChapterPlan{}, err
	}

	return domain.ChapterPlan{
		Chapter:    a.Chapter,
		Title:      a.Title,
		Goal:       a.Goal,
		Conflict:   a.Conflict,
		Hook:       a.Hook,
		EmotionArc: a.EmotionArc,
		Notes:      a.Notes,
		Contract: domain.ChapterContract{
			RequiredBeats:    a.RequiredBeats,
			ForbiddenMoves:   a.ForbiddenMoves,
			ContinuityChecks: a.ContinuityChecks,
			EvaluationFocus:  a.EvaluationFocus,
			EmotionTarget:    a.EmotionTarget,
			PayoffPoints:     a.PayoffPoints,
			HookGoal:         a.HookGoal,
		},
	}, nil
}
