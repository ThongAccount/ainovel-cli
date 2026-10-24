package arbiter

import (
	"context"
	"fmt"
	"strings"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// InterventionFacts là gói sự thật của chẩn đoán can thiệp (ảnh chụp lúc Collect).
// Engine đối chiếu Phase/QueueHead trước khi thi hành Dispatch ở ranh giới
// (giữa lúc hỏi và lúc thi hành có một worker chạy, sự thật có thể đã tiến;
// không khớp → bỏ qua và hỏi lại với sự thật mới).
type InterventionFacts struct {
	Phase                    string           `json:"phase,omitempty"`
	Flow                     string           `json:"flow,omitempty"`
	Title                    string           `json:"title,omitempty"`
	CompletedChapters        int              `json:"completed_chapters"`
	OutlinedChapters         int              `json:"outlined_chapters,omitempty"`
	DynamicPlanning          bool             `json:"dynamic_planning"`
	NextChapter              int              `json:"next_chapter,omitempty"`
	PendingRewrites          []int            `json:"pending_rewrites,omitempty"`
	ReopenCount              int              `json:"reopen_count,omitempty"` // Số lần người dùng chủ động /reopen mở lại một cuốn sách đã hoàn thành
	FoundationMissing        []string         `json:"foundation_missing,omitempty"`
	PlanningTier             string           `json:"planning_tier,omitempty"`
	AdvanceMode              string           `json:"advance_mode,omitempty"`
	HasAdvanceHold           bool             `json:"has_advance_hold"`
	AdvanceHoldAfter         string           `json:"advance_hold_after,omitempty"`
	AdvanceHoldTargetChapter int              `json:"advance_hold_target_chapter,omitempty"`
	AdvanceHoldReason        string           `json:"advance_hold_reason,omitempty"`
	Running                  bool             `json:"running"`                  // Có run đang diễn ra tại thời điểm can thiệp tới không
	CheckpointSeq            int64            `json:"checkpoint_seq,omitempty"` // Checkpoint mới nhất tại thời điểm Collect; Engine dùng để đối soát…
	RecentDecisions          []RecentDecision `json:"recent_decisions,omitempty"`
}

// RecentDecision là ký ức can thiệp: tóm tắt các phán quyết gần nhất, phủ
// các tham chiếu xuyên can thiệp kiểu "lần trước sửa thế nào".
type RecentDecision struct {
	At     string `json:"at"`
	Input  string `json:"input"`
	Reason string `json:"reason,omitempty"`
}

// QueueHead trả về đầu hàng đợi viết lại (0 nếu không có), Engine dùng để đối chiếu.
func (f InterventionFacts) QueueHead() int {
	if len(f.PendingRewrites) > 0 {
		return f.PendingRewrites[0]
	}
	return 0
}

// CollectInterventionFacts đọc đủ sự thật chẩn đoán từ store. Mọi lỗi đọc sự
// thật điều khiển đều được trả về tường minh, cấm Arbiter ra quyết định ngữ
// nghĩa trên một ảnh chụp không đầy đủ do ghép từ giá trị rỗng.
func CollectInterventionFacts(st *storepkg.Store) (InterventionFacts, error) {
	var f InterventionFacts
	if st == nil {
		return f, fmt.Errorf("store không được để trống")
	}
	missing, err := st.FoundationMissing()
	if err != nil {
		return f, fmt.Errorf("đọc trạng thái thiết lập nền tảng: %w", err)
	}
	f.FoundationMissing = missing
	book, err := st.Book.Load()
	if err != nil {
		return f, fmt.Errorf("đọc thông tin tác phẩm: %w", err)
	}
	if book != nil {
		f.Title = book.Title
	}
	p, err := st.Progress.Load()
	if err != nil {
		return f, fmt.Errorf("đọc tiến độ: %w", err)
	}
	if p != nil {
		f.Phase = string(p.Phase)
		f.Flow = string(p.Flow)
		f.CompletedChapters = len(p.CompletedChapters)
		f.DynamicPlanning = p.Layered
		if p.Layered {
			outline, outlineErr := st.Outline.LoadOutline()
			if outlineErr != nil {
				return f, fmt.Errorf("đọc dàn ý chi tiết hiện tại: %w", outlineErr)
			}
			f.OutlinedChapters = len(outline)
		} else {
			f.OutlinedChapters = p.TotalChapters
		}
		f.NextChapter = p.NextChapter()
		f.PendingRewrites = append([]int(nil), p.PendingRewrites...)
		f.ReopenCount = p.ReopenCount
	}
	meta, err := st.RunMeta.Load()
	if err != nil {
		return f, fmt.Errorf("đọc siêu dữ liệu chạy: %w", err)
	}
	if meta != nil {
		f.PlanningTier = string(meta.PlanningTier)
		f.AdvanceMode = string(meta.AdvanceMode)
		if meta.AdvanceHold != nil {
			f.HasAdvanceHold = true
			f.AdvanceHoldAfter = string(meta.AdvanceHold.After)
			f.AdvanceHoldTargetChapter = meta.AdvanceHold.TargetChapter
			f.AdvanceHoldReason = meta.AdvanceHold.Reason
		}
	}
	if cp := st.Checkpoints.LatestGlobal(); cp != nil {
		f.CheckpointSeq = cp.Seq
	}
	recent, err := st.Decisions.Recent(5)
	if err != nil {
		return f, fmt.Errorf("đọc các phán quyết gần đây: %w", err)
	}
	for _, r := range recent {
		if r.Kind != "intervention" {
			continue
		}
		f.RecentDecisions = append(f.RecentDecisions, RecentDecision{
			At: r.At, Input: truncateRunes(r.Input, 80), Reason: r.Reason,
		})
	}
	return f, nil
}

// AdvanceHoldOp là hành động tạm dừng một lần: dừng ở ranh giới công việc, khi
// hàng đợi viết lại đã rỗng, hoặc sau khi chương đích hoàn thành; cũng có thể huỷ.
type AdvanceHoldOp struct {
	Cancel        bool                    `json:"cancel,omitempty"`
	After         domain.AdvanceHoldAfter `json:"after,omitempty"`
	TargetChapter int                     `json:"target_chapter,omitempty"`
	Reason        string                  `json:"reason,omitempty"`
}

// ReopenOp là làm lại sau khi hoàn thành: mở lại toàn bộ sách vào trạng thái làm
// lại và đưa chương đích vào hàng đợi (chỉ hợp lệ ở phase=complete).
type ReopenOp struct {
	Chapters []int  `json:"chapters"`
	Reason   string `json:"reason,omitempty"`
}

// InterventionDecision là phán quyết can thiệp. Tổ hợp hành động tự do, thứ tự
// thi hành do Engine cố định:
// answer → rules → hold → reopen → dispatch; tối đa một dispatch (sự thật kiểu).
type InterventionDecision struct {
	Answer   string         `json:"answer,omitempty"`
	Rules    string         `json:"rules,omitempty"`
	Hold     *AdvanceHoldOp `json:"hold,omitempty"`
	Reopen   *ReopenOp      `json:"reopen,omitempty"`
	Dispatch *DispatchOp    `json:"dispatch,omitempty"`
	Reason   string         `json:"reason"`
}

// interventionContractFor dựng hợp đồng can thiệp theo ngôn ngữ tác phẩm: mọi
// trường required, các nhánh tùy chọn dùng hợp nhất null.
func interventionContractFor(lang i18n.Lang) llmcontract.Contract {
	texts := arbiterTextsFor(lang)
	return llmcontract.Contract{
		Name:        "arbiter_intervention",
		Description: texts.interventionDesc,
		Schema: schema.Object(
			schema.Property("answer", llmcontract.Nullable(schema.String(texts.answerLabel))).Required(),
			schema.Property("rules", llmcontract.Nullable(schema.String(texts.rulesLabel))).Required(),
			schema.Property("hold", llmcontract.Nullable(schema.Object(
				schema.Property("cancel", schema.Bool(texts.holdCancelLabel)).Required(),
				schema.Property("after", llmcontract.Nullable(schema.Enum(texts.holdAfterLabel, string(domain.AdvanceHoldAtBoundary), string(domain.AdvanceHoldAfterRewritesDrained), string(domain.AdvanceHoldAtChapter)))).Required(),
				schema.Property("target_chapter", llmcontract.Nullable(schema.Int(texts.holdTargetLabel))).Required(),
				schema.Property("reason", llmcontract.Nullable(schema.String(texts.holdReasonLabel))).Required(),
			))).Required(),
			schema.Property("reopen", llmcontract.Nullable(schema.Object(
				schema.Property("chapters", schema.Array(texts.reopenChaptersLabel, schema.Int(texts.reopenChapterLabel))).Required(),
				schema.Property("reason", llmcontract.Nullable(schema.String(texts.reopenReasonLabel))).Required(),
			))).Required(),
			schema.Property("dispatch", dispatchSchema(texts, texts.dispatchLabel)).Required(),
			schema.Property("reason", schema.String(texts.interventionReasonLbl)).Required(),
		),
		Lang: lang,
	}
}

// ValidateAgainst kiểm tra cơ học theo sự thật (tính hợp lệ trong kịch bản;
// kiểu dữ liệu đã loại trừ hành động vượt kịch bản).
func (d *InterventionDecision) ValidateAgainst(f InterventionFacts) error {
	texts := activeTexts()
	if strings.TrimSpace(d.Reason) == "" {
		return fmt.Errorf("%s", texts.reasonRequired)
	}
	if d.Answer == "" && d.Rules == "" && d.Hold == nil && d.Reopen == nil && d.Dispatch == nil {
		return fmt.Errorf("%s", texts.emptyDecision)
	}
	if err := d.Dispatch.validate(texts); err != nil {
		return err
	}
	if err := validateDispatchAgainst(d.Dispatch, f.Phase, texts); err != nil {
		return err
	}
	complete := f.Phase == string(domain.PhaseComplete)
	if d.Reopen != nil {
		if !complete {
			return fmt.Errorf(texts.reopenOnlyComplete, f.Phase)
		}
		if len(d.Reopen.Chapters) == 0 {
			return fmt.Errorf("%s", texts.reopenChaptersRequired)
		}
		for _, ch := range d.Reopen.Chapters {
			if ch < 1 || ch > f.CompletedChapters {
				return fmt.Errorf(texts.reopenChapterOutOfRange, ch, f.CompletedChapters)
			}
		}
	}
	if complete && d.Dispatch != nil {
		return fmt.Errorf("%s", texts.completeNoDispatch)
	}
	if d.Hold != nil && !d.Hold.Cancel {
		if f.Phase != string(domain.PhaseWriting) {
			return fmt.Errorf(texts.holdOnlyWriting, f.Phase)
		}
		hold := domain.AdvanceHold{After: d.Hold.After, TargetChapter: d.Hold.TargetChapter, Reason: d.Hold.Reason}
		if err := hold.Validate(); err != nil {
			return fmt.Errorf(texts.holdInvalid, err)
		}
		nextChapter := f.NextChapter
		if nextChapter == 0 {
			nextChapter = f.CompletedChapters + 1
		}
		if hold.After == domain.AdvanceHoldAtChapter && hold.TargetChapter < nextChapter {
			return fmt.Errorf(texts.targetChapterTooEarly, hold.TargetChapter, nextChapter)
		}
	}
	return nil
}

// validateDispatchAgainst biến kỷ luật giai đoạn trong prompt thành phòng tuyến cơ
// học. Architect có thể duy trì cấu trúc ở giai đoạn quy hoạch và giai đoạn sáng
// tác; Writer/Editor chỉ được dùng sự thật tác phẩm đã hoàn chỉnh và đã vào
// giai đoạn writing.
func validateDispatchAgainst(dispatch *DispatchOp, phase string, texts *arbiterTexts) error {
	if dispatch == nil {
		return nil
	}
	if phase == "" {
		return fmt.Errorf("%s", texts.missingPhase)
	}
	if phase == string(domain.PhaseComplete) {
		return fmt.Errorf("%s", texts.completeNoDispatch)
	}
	switch dispatch.Agent {
	case "writer", "editor":
		if phase != string(domain.PhaseWriting) {
			return fmt.Errorf(texts.workerPhaseOnly, dispatch.Agent, phase)
		}
	}
	return nil
}

// DecideIntervention chẩn đoán can thiệp. Ngữ nghĩa thất bại: trả về error →
// nơi gọi hiển thị lại đúng lý do thất bại và không ghi gì (thà không động còn
// hơn động sai).
func DecideIntervention(ctx context.Context, model agentcore.ChatModel, systemPrompt string, facts InterventionFacts, text string) (InterventionDecision, error) {
	payload, err := marshalPayload(struct {
		Intervention string            `json:"intervention"`
		Facts        InterventionFacts `json:"facts"`
	}{Intervention: text, Facts: facts})
	if err != nil {
		return InterventionDecision{}, err
	}
	lang := i18n.Active()
	return decide(ctx, model, interventionContractFor(lang), systemPrompt, payload, func(d *InterventionDecision) error {
		return d.ValidateAgainst(facts)
	})
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
