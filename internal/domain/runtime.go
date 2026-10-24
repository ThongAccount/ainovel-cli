package domain

import (
	"fmt"
	"strings"
)

// Phase biểu thị giai đoạn sáng tác tiểu thuyết.
type Phase string

const (
	PhaseInit     Phase = "init"
	PhasePremise  Phase = "premise"
	PhaseOutline  Phase = "outline"
	PhaseWriting  Phase = "writing"
	PhaseComplete Phase = "complete"
)

// FlowState là loại quy trình đang hoạt động hiện tại, dùng để khôi phục checkpoint.
type FlowState string

const (
	FlowWriting   FlowState = "writing"
	FlowReviewing FlowState = "reviewing"
	FlowRewriting FlowState = "rewriting"
	FlowPolishing FlowState = "polishing"
	FlowSteering  FlowState = "steering"
)

// PlanningTier biểu thị cấp độ dài của kế hoạch tác phẩm.
type PlanningTier string

const (
	PlanningTierShort PlanningTier = "short"
	PlanningTierMid   PlanningTier = "mid"
	PlanningTierLong  PlanningTier = "long"
)

// Progress theo dõi tiến độ, được lưu bền vào meta/progress.json.
type Progress struct {
	Phase          Phase `json:"phase"`
	CurrentChapter int   `json:"current_chapter"`
	// TotalChapters là số chương trong dàn ý chi tiết ở chế độ không phân tầng; ở
	// chế độ phân tầng, đây chỉ là giá trị sức chứa nội bộ có gồm ước lượng cho
	// các cung khung, dùng cho chiến lược ngữ cảnh và không phải tổng số chương cố
	// định của cả tác phẩm.
	TotalChapters     int         `json:"total_chapters"`
	CompletedChapters []int       `json:"completed_chapters"`
	TotalWordCount    int         `json:"total_word_count"`
	ChapterWordCounts map[int]int `json:"chapter_word_counts,omitempty"` // số chữ từng chương, hỗ trợ sửa tổng số chữ khi viết lại
	InProgressChapter int         `json:"in_progress_chapter,omitempty"` // chương đang viết (khôi phục ở cấp cảnh)
	CompletedScenes   []int       `json:"completed_scenes,omitempty"`    // số cảnh đã hoàn thành của chương hiện tại
	Flow              FlowState   `json:"flow,omitempty"`                // quy trình hiện tại
	PendingRewrites   []int       `json:"pending_rewrites,omitempty"`    // hàng đợi chương chờ viết lại
	RewriteReason     string      `json:"rewrite_reason,omitempty"`      // lý do viết lại
	StrandHistory     []string    `json:"strand_history,omitempty"`      // ghi dominant_strand theo thứ tự chương
	HookHistory       []string    `json:"hook_history,omitempty"`        // ghi hook_type theo thứ tự chương
	// Theo dõi phân tầng cho truyện dài (chỉ dùng ở chế độ truyện dài; truyện
	// ngắn/trung bình để giá trị zero)
	CurrentVolume int  `json:"current_volume,omitempty"`
	CurrentArc    int  `json:"current_arc,omitempty"`
	Layered       bool `json:"layered,omitempty"`
	// ReopenedFromComplete đánh dấu tác phẩm được mở lại từ trạng thái hoàn thành
	// để làm lại. Việc làm lại chỉ sửa các chương đã có, không thay đổi cấu trúc;
	// sau khi hàng đợi rỗng, tác phẩm được phép hoàn thành lại theo quy tắc "cấu trúc
	// đầy đủ là hoàn thành" (tránh manh mối ở tập cuối bị làm lại khiến tác phẩm mắc
	// ở writing → vòng lặp viết vượt phạm vi). Quá trình viết tiến không đặt cờ này;
	// phán quyết hoàn thành giữ ngữ nghĩa thận trọng về việc chốt tuyến kể.
	ReopenedFromComplete bool `json:"reopened_from_complete,omitempty"`
	// ReopenCount ghi số lần tác phẩm đã được mở lại từ trạng thái hoàn thành (sự
	// thật kiểm toán của /reopen). Nó đồng thời bảo đảm nội dung progress.json sau
	// khi hoàn thành lại khác lần hoàn thành trước: checkpoint khử trùng lặp theo
	// digest, nên lần hoàn thành lại có cùng byte sẽ không tạo checkpoint mới và
	// StopGuard sẽ hiểu nhầm complete_book thành công là vòng lặp rồi nâng mức dừng.
	ReopenCount int `json:"reopen_count,omitempty"`
}

// IsResumable kiểm tra có thể khôi phục từ điểm dừng hay không.
func (p *Progress) IsResumable() bool {
	return p.Phase == PhaseWriting && p.CurrentChapter > 0
}

// NextChapter trả về số chương tiếp theo cần viết.
func (p *Progress) NextChapter() int {
	return p.LatestCompleted() + 1
}

// LatestCompleted trả về số chương đã hoàn thành lớn nhất; trả về 0 nếu chưa có
// chương nào hoàn thành.
func (p *Progress) LatestCompleted() int {
	max := 0
	for _, ch := range p.CompletedChapters {
		if ch > max {
			max = ch
		}
	}
	return max
}

// ContextProfile là chiến lược nạp ngữ cảnh, thích nghi theo tổng số chương.
type ContextProfile struct {
	SummaryWindow  int  // nạp tóm tắt của N chương gần nhất
	TimelineWindow int  // nạp dòng thời gian của N chương gần nhất
	Layered        bool // true = bật nạp tóm tắt phân tầng (tập + cung + chương)
}

// MemoryPolicy biểu thị chiến lược dùng bộ nhớ dùng chung khi chạy.
// Nó vừa dùng cho đầu ra ngữ cảnh, vừa dùng cho quyết định handoff / reminder ở
// tầng host.
type MemoryPolicy struct {
	Mode                string `json:"mode,omitempty"`
	SummaryWindow       int    `json:"summary_window,omitempty"`
	TimelineWindow      int    `json:"timeline_window,omitempty"`
	LayeredSummaries    bool   `json:"layered_summaries,omitempty"`
	SummaryStrategy     string `json:"summary_strategy,omitempty"`
	WorkingRefresh      string `json:"working_refresh,omitempty"`
	EpisodicRefresh     string `json:"episodic_refresh,omitempty"`
	PlanningRefresh     string `json:"planning_refresh,omitempty"`
	FoundationRefresh   string `json:"foundation_refresh,omitempty"`
	PlanningFocus       string `json:"planning_focus,omitempty"`
	FoundationFocus     string `json:"foundation_focus,omitempty"`
	PreviousTailChars   int    `json:"previous_tail_chars,omitempty"`
	ChapterPlanEnabled  bool   `json:"chapter_plan_enabled,omitempty"`
	RelatedLookup       bool   `json:"related_chapter_lookup,omitempty"`
	CurrentOutlineBound bool   `json:"current_outline_bound,omitempty"`
	HandoffPreferred    bool   `json:"handoff_preferred,omitempty"`
	ReadOnlyThreshold   int    `json:"read_only_threshold,omitempty"`
}

// NewContextProfile tính chiến lược ngữ cảnh từ tổng số chương.
func NewContextProfile(totalChapters int) ContextProfile {
	switch {
	case totalChapters <= 15:
		return ContextProfile{SummaryWindow: 10, TimelineWindow: 10}
	case totalChapters <= 50:
		return ContextProfile{SummaryWindow: 5, TimelineWindow: 8}
	default:
		return ContextProfile{SummaryWindow: 3, TimelineWindow: 5, Layered: true}
	}
}

// NewChapterMemoryPolicy tạo chiến lược bộ nhớ runtime cho chương từ tiến độ và
// chiến lược ngữ cảnh.
func NewChapterMemoryPolicy(progress *Progress, profile ContextProfile, currentOutlineBound bool) MemoryPolicy {
	policy := MemoryPolicy{
		Mode:                "chapter",
		SummaryWindow:       profile.SummaryWindow,
		TimelineWindow:      profile.TimelineWindow,
		LayeredSummaries:    profile.Layered,
		WorkingRefresh:      "làm mới mỗi lần nạp theo chương",
		EpisodicRefresh:     "làm mới khi gửi chương, thẩm duyệt và trạng thái truyện dài thay đổi",
		PreviousTailChars:   800,
		ChapterPlanEnabled:  true,
		CurrentOutlineBound: currentOutlineBound,
		ReadOnlyThreshold:   5,
	}
	if profile.Layered {
		policy.SummaryStrategy = "tóm tắt tập + tóm tắt cung + tóm tắt chương gần nhất"
	} else {
		policy.SummaryStrategy = "tóm tắt chương gần nhất"
	}
	if progress != nil {
		if progress.TotalChapters > 30 {
			policy.RelatedLookup = true
		}
		if progress.Flow == FlowReviewing || progress.Flow == FlowRewriting || progress.Flow == FlowPolishing {
			policy.HandoffPreferred = true
		}
		if progress.Layered && len(progress.CompletedChapters) >= 6 {
			policy.HandoffPreferred = true
		}
		if len(progress.CompletedChapters) >= 12 {
			policy.HandoffPreferred = true
		}
		if progress.Layered && len(progress.CompletedChapters) >= 6 {
			policy.ReadOnlyThreshold = 4
		}
		if len(progress.CompletedChapters) >= 12 {
			policy.ReadOnlyThreshold = 4
		}
	}
	return policy
}

// NewArchitectMemoryPolicy trả về chiến lược bộ nhớ dùng trong giai đoạn quy hoạch.
func NewArchitectMemoryPolicy() MemoryPolicy {
	return MemoryPolicy{
		Mode:               "architect",
		PlanningRefresh:    "làm mới khi cấu trúc tập/cung, la bàn hoặc tóm tắt thay đổi",
		FoundationRefresh:  "làm mới khi nhân vật, manh mối hoặc thiết lập thay đổi",
		PlanningFocus:      "dàn ý phân tầng, la bàn, tóm tắt tập",
		FoundationFocus:    "thiết lập nhân vật, ảnh chụp nhân vật, sổ manh mối",
		HandoffPreferred:   true,
		ChapterPlanEnabled: false,
		ReadOnlyThreshold:  4,
	}
}

// RunMeta chứa siêu dữ liệu lần chạy, được lưu bền vào meta/run.json.
type RunMeta struct {
	StartedAt            string             `json:"started_at"`
	Provider             string             `json:"provider,omitempty"`
	Style                string             `json:"style"`
	Model                string             `json:"model"`
	PlanningTier         PlanningTier       `json:"planning_tier,omitempty"`
	StartPrompt          string             `json:"start_prompt,omitempty"`           // nhu cầu sáng tác gốc của người dùng (sự thật đầu vào, được ghi trước khi phán quyết khởi động; dùng để bù phán quyết nếu thất bại)
	PlanStart            *PlanStartRecord   `json:"plan_start,omitempty"`             // sự thật phán quyết khởi động, nguồn duy nhất để khôi phục giai đoạn quy hoạch
	PendingSteer         string             `json:"pending_steer,omitempty"`          // chỉ dẫn Steer chưa hoàn tất, được chèn lại khi khôi phục sau gián đoạn
	AdvanceMode          ChapterAdvanceMode `json:"advance_mode"`                     // chế độ tiến triển chương: auto / review
	AdvancePermitChapter int                `json:"advance_permit_chapter,omitempty"` // chương tiến triển được phép một lần trong chế độ review
	AdvanceHold          *AdvanceHold       `json:"advance_hold,omitempty"`           // ý định tạm dừng một lần do can thiệp hiện tại ký
}

// ChapterAdvanceMode quyết định chương mới có cần được cấp phép từng chương hay không.
type ChapterAdvanceMode string

const (
	ChapterAdvanceAuto   ChapterAdvanceMode = "auto"
	ChapterAdvanceReview ChapterAdvanceMode = "review"
)

// Valid báo cáo chế độ tiến triển chương có được phiên bản hiện tại hỗ trợ hay không.
func (m ChapterAdvanceMode) Valid() bool {
	return m == ChapterAdvanceAuto || m == ChapterAdvanceReview
}

// UnsupportedAdvanceModeError biểu thị chế độ điều khiển của tác phẩm không được
// binary hiện tại hỗ trợ.
// Bên gọi phải dừng việc dựng Host có thể ghi và nhắc người dùng dùng phiên bản khớp;
// tuyệt đối không đoán rồi hạ cấp.
type UnsupportedAdvanceModeError struct {
	Mode ChapterAdvanceMode
}

func (e *UnsupportedAdvanceModeError) Error() string {
	return fmt.Sprintf("chế độ tiến triển chương %q không được hỗ trợ, vui lòng dùng phiên bản ainovel mới hơn đã tạo dự án này", e.Mode)
}

// AdvanceHoldAfter là điều kiện kích hoạt tất định cho lần tạm dừng một lần.
type AdvanceHoldAfter string

const (
	AdvanceHoldAtBoundary           AdvanceHoldAfter = "boundary"
	AdvanceHoldAfterRewritesDrained AdvanceHoldAfter = "rewrites_drained"
	AdvanceHoldAtChapter            AdvanceHoldAfter = "chapter"
)

// Valid báo cáo điều kiện tạm dừng có được phiên bản hiện tại hỗ trợ hay không.
func (a AdvanceHoldAfter) Valid() bool {
	return a == AdvanceHoldAtBoundary || a == AdvanceHoldAfterRewritesDrained || a == AdvanceHoldAtChapter
}

// AdvanceHold là ý định tạm dừng một lần do can thiệp hiện tại ký, được biên Host tiêu thụ.
type AdvanceHold struct {
	After         AdvanceHoldAfter `json:"after"`
	TargetChapter int              `json:"target_chapter,omitempty"`
	Reason        string           `json:"reason"`
}

// Validate kiểm tra các ràng buộc cấu trúc của ý định tạm dừng một lần.
func (h AdvanceHold) Validate() error {
	if !h.After.Valid() {
		return fmt.Errorf("điều kiện tạm dừng một lần %q không được hỗ trợ", h.After)
	}
	if h.After == AdvanceHoldAtChapter {
		if h.TargetChapter <= 0 {
			return fmt.Errorf("chương đích phải lớn hơn 0")
		}
	} else if h.TargetChapter != 0 {
		return fmt.Errorf("điều kiện tạm dừng %q không được đặt chương đích", h.After)
	}
	if strings.TrimSpace(h.Reason) == "" {
		return fmt.Errorf("lý do tạm dừng một lần không được để trống")
	}
	return nil
}

// PlanStartRecord là sự thật được lưu bền của phán quyết khởi động (phán quyết được
// ghi trước, rồi mới thực thi; quá trình khôi phục không phán quyết lại).
// Sau khi lần save_foundation đầu tiên ghi scale, quá trình khôi phục giai đoạn quy
// hoạch chuyển sang suy ra từ PlanningTier; bản ghi này chỉ phủ khoảng thời gian từ
// lúc phán quyết hoàn tất đến lần ghi đầu tiên. DecisionID liên kết audit
// decisions.jsonl.
type PlanStartRecord struct {
	RawPrompt   string `json:"raw_prompt"`
	Planner     string `json:"planner"`
	PlannerTask string `json:"planner_task"`
	DecisionID  string `json:"decision_id,omitempty"`
}
