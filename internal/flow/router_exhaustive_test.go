package flow

// Kiểm thử liệt kê toàn bộ không gian trạng thái của Route.
//
// expectedInstruction là bản sao độc lập của bảng quyết định (đặc tả có thể
// chạy, tương ứng luật số hai trong architecture.md với 11 nhánh ưu tiên), cố
// tình không dùng lại bất kỳ mã nào của phần hiện thực: nếu hành vi lệch sau khi
// refactor, tệp này đỏ ngay lập tức; muốn đổi hành vi thì phải sửa đặc tả và
// để lại diff. Các ca nhánh đơn trong router_test.go giữ vai trò tài liệu ý
// định dễ đọc, còn tệp này giữ tính ưu tiên và tính bất biến trên toàn bộ tổ
// hợp.

import (
	"reflect"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// expectKind là kết quả phán quyết ở tầng đặc tả: định tuyến tới ai, làm việc
// thuộc nhóm nào.
type expectKind int

const (
	expectNil expectKind = iota
	expectRewrite
	expectArcReview
	expectArcSummary
	expectVolumeSummary
	expectExpandArc
	expectNewVolume
	expectNextChapter
	expectFoundationFill
	expectGlobalReview
	expectOutlineFeedback
)

// Marker nhận diện nhánh trong nhiệm vụ, viết cho cả hai ngôn ngữ tác phẩm:
// nhiệm vụ gửi cho Worker phải theo ngôn ngữ, nên đặc tả phải phân biệt được
// cả bản tiếng Việt lẫn bản tiếng Trung.
var (
	rewriteMarkers      = []string{"重写", "打磨", "Viết lại", "Hoàn thiện"}
	nextChapterMarkers  = []string{"写第", "Viết chương"}
	arcReviewMarkers    = []string{"弧级评审", "Thẩm duyệt mức cung"}
	globalReviewMarkers = []string{"全局审阅", "Thẩm duyệt toàn cục"}
	foundationMarkers   = []string{"补齐基础设定", "thiết lập nền tảng"}
)

// containsAny trả về true nếu chuỗi chứa ít nhất một trong các marker.
func containsAny(s string, markers ...string) bool {
	for _, marker := range markers {
		if contains(s, marker) {
			return true
		}
	}
	return false
}

// expectedInstruction tính phán quyết mà một State phải nhận theo đặc tả kiến
// trúc.
// Ưu tiên (nhánh đầu tiên khớp từ trên xuống):
//  1. Thiếu Progress / Phase cuối → phán quyết của LLM (nil)
//  2. Giai đoạn quy hoạch (không phải giai đoạn sáng tác): thiếu thiết lập và
//     xác định được kiến trúc sư (save_foundation đã ghi scale) → phái tiếp cùng
//     kiến trúc sư đó; nếu không → phán quyết của LLM (nil, gồm cả lần chọn
//     kiến trúc sư đầu tiên)
//  3. Hàng đợi viết lại/hoàn thiện khác rỗng → writer theo đầu hàng đợi (ưu tiên
//     tuyệt đối, đè lên mọi việc cuối cung)
//  4. Flow=Reviewing / Steering → phán quyết của LLM (nil)
//  5. Thiếu công cụ tổng hợp → Biên tập viên dựng lại
//  6. Sửa đổi bên ngoài ảnh hưởng quy hoạch về sau → Kiến trúc sư tiêu thụ
//  7. Cuối cung ở chế độ phân lớp → thẩm duyệt → tóm tắt cung → (cuối tập) tóm
//     tắt tập → mở rộng cung kế tiếp → thêm tập mới
//  8. Còn lại → writer viết tiếp chương kế tiếp
func expectedInstruction(s State) expectKind {
	p := s.Progress
	if p == nil || p.Phase == domain.PhaseComplete {
		return expectNil
	}
	if p.Phase != domain.PhaseWriting {
		if len(s.FoundationMissing) > 0 && s.PlanningTier != "" {
			return expectFoundationFill
		}
		return expectNil
	}
	if len(p.PendingRewrites) > 0 {
		return expectRewrite
	}
	if p.Flow == domain.FlowReviewing || p.Flow == domain.FlowSteering {
		return expectNil
	}
	if s.AggregateRefresh != nil {
		return expectArcSummary
	}
	if s.ImmediateFeedbackCount > 0 {
		return expectOutlineFeedback
	}
	if p.Layered && s.ArcBoundary != nil && s.ArcBoundary.IsArcEnd {
		b := s.ArcBoundary
		switch {
		case !s.HasArcReview:
			return expectArcReview
		case !s.HasArcSummary:
			return expectArcSummary
		case b.IsVolumeEnd && !s.HasVolumeSummary:
			return expectVolumeSummary
		case b.NeedsExpansion && b.NextArc > 0:
			return expectExpandArc
		case b.NeedsNewVolume:
			return expectNewVolume
		}
	}
	// Không phân lớp: mỗi ReviewInterval chương một lần thẩm duyệt toàn cục
	// (chưa làm thì thẩm duyệt trước rồi mới viết tiếp).
	if !p.Layered && s.LastCompleted > 0 {
		if due, _ := domain.ShouldReview(len(p.CompletedChapters)); due && !s.HasGlobalReview {
			return expectGlobalReview
		}
	}
	return expectNextChapter
}

// classify gom Instruction mà phần hiện thực trả về vào nhóm đặc tả; tổ hợp
// không nhận ra được thì báo lỗi ngay.
func classify(t *testing.T, inst *Instruction) expectKind {
	t.Helper()
	if inst == nil {
		return expectNil
	}
	switch inst.Agent {
	case "writer":
		switch {
		case containsAny(inst.Task, rewriteMarkers...):
			return expectRewrite
		case containsAny(inst.Task, nextChapterMarkers...):
			return expectNextChapter
		}
	case "editor":
		switch {
		case containsAny(inst.Task, arcReviewMarkers...):
			return expectArcReview
		case containsAny(inst.Task, globalReviewMarkers...):
			return expectGlobalReview
		case contains(inst.Task, "save_arc_summary"):
			return expectArcSummary
		case contains(inst.Task, "save_volume_summary"):
			return expectVolumeSummary
		}
	case "architect_long":
		switch {
		case containsAny(inst.Task, foundationMarkers...):
			return expectFoundationFill
		case contains(inst.Task, "writer_feedback"):
			return expectOutlineFeedback
		case contains(inst.Task, "expand_arc"):
			return expectExpandArc
		case contains(inst.Task, "append_volume"):
			return expectNewVolume
		}
	case "architect_short":
		if containsAny(inst.Task, foundationMarkers...) {
			return expectFoundationFill
		}
		if contains(inst.Task, "writer_feedback") {
			return expectOutlineFeedback
		}
	}
	t.Fatalf("không phân loại được chỉ dẫn: agent=%q task=%q", inst.Agent, inst.Task)
	return expectNil
}

// boundaryCase là một điểm liệt kê của chiều ranh giới cung: hình dạng ranh giới
// + ba sự thật về tóm tắt.
type boundaryCase struct {
	name             string
	boundary         *storepkg.ArcBoundary
	hasArcReview     bool
	hasArcSummary    bool
	hasVolumeSummary bool
}

func enumerateBoundaryCases() []boundaryCase {
	cases := []boundaryCase{
		{name: "no-boundary"},
		{name: "mid-arc", boundary: &storepkg.ArcBoundary{Volume: 1, Arc: 1}},
	}
	type volCase struct {
		name       string
		volumeEnd  bool
		volSummary bool
	}
	type followCase struct {
		name      string
		expansion bool
		nextArc   int
		newVolume bool
	}
	volCases := []volCase{
		{name: "vol-mid", volumeEnd: false},
		{name: "vol-end-nosum", volumeEnd: true, volSummary: false},
		{name: "vol-end-sum", volumeEnd: true, volSummary: true},
	}
	followCases := []followCase{
		{name: "settled"},
		{name: "expand", expansion: true, nextArc: 4},
		{name: "expand-no-nextarc", expansion: true, nextArc: 0}, // thiếu vị trí mở rộng → không mở rộng được
		{name: "new-volume", newVolume: true},
	}
	for _, review := range []bool{false, true} {
		for _, summary := range []bool{false, true} {
			for _, vc := range volCases {
				for _, fc := range followCases {
					cases = append(cases, boundaryCase{
						name: fmtBool("rev", review) + fmtBool("+sum", summary) + "+" + vc.name + "+" + fc.name,
						boundary: &storepkg.ArcBoundary{
							IsArcEnd:       true,
							IsVolumeEnd:    vc.volumeEnd,
							Volume:         2,
							Arc:            3,
							NextVolume:     2,
							NextArc:        fc.nextArc,
							NeedsExpansion: fc.expansion,
							NeedsNewVolume: fc.newVolume,
						},
						hasArcReview:     review,
						hasArcSummary:    summary,
						hasVolumeSummary: vc.volSummary,
					})
				}
			}
		}
	}
	return cases
}

func fmtBool(label string, v bool) string {
	if v {
		return label
	}
	return label + "!"
}

func TestRoute_ExhaustiveAgainstSpec(t *testing.T) {
	phases := []domain.Phase{domain.PhaseInit, domain.PhasePremise, domain.PhaseOutline, domain.PhaseWriting, domain.PhaseComplete}
	flows := []domain.FlowState{domain.FlowWriting, domain.FlowReviewing, domain.FlowRewriting, domain.FlowPolishing, domain.FlowSteering}
	queues := [][]int{nil, {7, 9}}
	// {1..5} trúng điểm kích hoạt thẩm duyệt toàn cục ReviewInterval(=5)
	completedSets := [][]int{nil, {1, 2, 3}, {1, 2, 3, 4, 5}}
	missingSets := [][]string{nil, {"characters", "world_rules"}}
	tiers := []domain.PlanningTier{"", domain.PlanningTierShort, domain.PlanningTierLong}
	globalReviews := []bool{false, true}
	feedbackCounts := []int{0, 1}
	aggregates := []*AggregateRefresh{nil, {Kind: AggregateArcSummary, Volume: 1, Arc: 1, EndChapter: 5}}
	// Nhiệm vụ gửi cho Worker theo ngôn ngữ tác phẩm, nên phải kiểm cả hai.
	langs := []i18n.Lang{i18n.LangVI, i18n.LangZH}

	total := 0
	for _, lang := range langs {
		for _, phase := range phases {
			for _, fl := range flows {
				for _, queue := range queues {
					for _, layered := range []bool{false, true} {
						for _, completed := range completedSets {
							for _, missing := range missingSets {
								for _, tier := range tiers {
									for _, hasGlobal := range globalReviews {
										for _, feedbackCount := range feedbackCounts {
											for _, aggregate := range aggregates {
												for _, bc := range enumerateBoundaryCases() {
													total++
													p := &domain.Progress{
														Phase:             phase,
														Flow:              fl,
														Layered:           layered,
														CompletedChapters: append([]int(nil), completed...),
														PendingRewrites:   append([]int(nil), queue...),
													}
													last := 0
													if n := len(completed); n > 0 {
														last = completed[n-1]
													}
													s := State{
														Lang:                   lang,
														Progress:               p,
														LastCompleted:          last,
														ArcBoundary:            bc.boundary,
														HasArcReview:           bc.hasArcReview,
														HasArcSummary:          bc.hasArcSummary,
														HasVolumeSummary:       bc.hasVolumeSummary,
														FoundationMissing:      append([]string(nil), missing...),
														PlanningTier:           tier,
														HasGlobalReview:        hasGlobal,
														ImmediateFeedbackCount: feedbackCount,
														AggregateRefresh:       aggregate,
													}

													before := snapshotState(s)
													inst := Route(s)
													want := expectedInstruction(s)
													got := classify(t, inst)
													if got != want {
														t.Fatalf("phase=%s flow=%s queue=%v layered=%v completed=%v missing=%v tier=%q global=%v boundary=%s lang=%s:\nđặc tả cần %d, hiện thực trả %d (inst=%+v)",
															phase, fl, queue, layered, completed, missing, tier, hasGlobal, bc.name, lang, want, got, inst)
													}
													assertConservation(t, s, inst)
													if !reflect.DeepEqual(before, snapshotState(s)) {
														t.Fatalf("Route phải là hàm thuần, không được sửa State đầu vào (boundary=%s)", bc.name)
													}
													if again := Route(s); !reflect.DeepEqual(inst, again) {
														t.Fatalf("Route phải tất định: hai lần gọi cho kết quả khác nhau (boundary=%s)", bc.name)
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if total < 10000 {
		t.Fatalf("không gian liệt kê tự nhiên co lại (%d tổ hợp), kiểm tra các chiều liệt kê", total)
	}
}

// assertConservation kiểm tra các bất biến không phụ thuộc nhánh cụ thể.
func assertConservation(t *testing.T, s State, inst *Instruction) {
	t.Helper()
	if inst == nil {
		return
	}
	p := s.Progress
	if p == nil || p.Phase == domain.PhaseComplete {
		t.Fatalf("trạng thái cuối hoặc không có tiến độ thì không được sinh chỉ dẫn: %+v", inst)
	}
	if p.Phase != domain.PhaseWriting {
		// Chỉ dẫn hợp lệ duy nhất ở giai đoạn quy hoạch: phái bổ sung, và kiến
		// trúc sư phải khớp tier đã ghi xuống đĩa.
		wantPlanner := "architect_long"
		if s.PlanningTier == domain.PlanningTierShort {
			wantPlanner = "architect_short"
		}
		if inst.Agent != wantPlanner || !containsAny(inst.Task, foundationMarkers...) || inst.Chapter != 0 {
			t.Fatalf("chỉ dẫn giai đoạn quy hoạch phải là phái bổ sung và kiến trúc sư khớp tier=%q: %+v", s.PlanningTier, inst)
		}
		return
	}
	switch inst.Agent {
	case "writer":
		if inst.Chapter <= 0 {
			t.Fatalf("chỉ dẫn writer phải mang số chương: %+v", inst)
		}
		if len(p.PendingRewrites) > 0 {
			if inst.Chapter != p.PendingRewrites[0] {
				t.Fatalf("hàng đợi viết lại khác rỗng thì phải phái đầu hàng đợi %d, nhận %d", p.PendingRewrites[0], inst.Chapter)
			}
			wantVerbs := []string{"重写"}
			if p.Flow == domain.FlowPolishing {
				wantVerbs = []string{"打磨"}
			}
			if s.Lang == i18n.LangVI {
				wantVerbs = []string{"Viết lại"}
				if p.Flow == domain.FlowPolishing {
					wantVerbs = []string{"Hoàn thiện"}
				}
			}
			if !containsAny(inst.Task, wantVerbs...) {
				t.Fatalf("động từ của nhiệm vụ hàng đợi phải là %q: %q", wantVerbs, inst.Task)
			}
		} else if inst.Chapter != p.NextChapter() {
			t.Fatalf("chỉ dẫn viết tiếp phải có số chương NextChapter=%d, nhận %d", p.NextChapter(), inst.Chapter)
		}
	case "editor", "architect_long", "architect_short":
		if inst.Chapter != 0 {
			t.Fatalf("chỉ dẫn %s không được mang số chương: %+v", inst.Agent, inst)
		}
	default:
		t.Fatalf("đích định tuyến không xác định %q", inst.Agent)
	}
	if inst.Task == "" || inst.Reason == "" {
		t.Fatalf("cả Task lẫn Reason của chỉ dẫn đều không được rỗng: %+v", inst)
	}
}

// snapshotState sao chép sâu State để khẳng định tính thuần.
func snapshotState(s State) State {
	cp := s
	if s.Progress != nil {
		p := *s.Progress
		p.CompletedChapters = append([]int(nil), s.Progress.CompletedChapters...)
		p.PendingRewrites = append([]int(nil), s.Progress.PendingRewrites...)
		cp.Progress = &p
	}
	if s.ArcBoundary != nil {
		b := *s.ArcBoundary
		cp.ArcBoundary = &b
	}
	if s.AggregateRefresh != nil {
		refresh := *s.AggregateRefresh
		cp.AggregateRefresh = &refresh
	}
	cp.FoundationMissing = append([]string(nil), s.FoundationMissing...)
	return cp
}
