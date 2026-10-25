// Package flow hiện thực định tuyến theo thể loại: Host dựa trên sự thật đã
// lưu để quyết định gọi sub-agent nào, làm việc gì.
//
// Nguyên tắc thiết kế:
//   - Route là hàm thuần: nhận State, trả về *Instruction. Không IO, không gọi
//     Store, nên đơn vị kiểm thử được.
//   - State do LoadState (không thuần) dựng từ Store, gom một lần toàn bộ sự
//     thật mà định tuyến cần.
//   - Trả về nil là hợp lệ: nghĩa là hiện chưa có chỉ dẫn Worker nào suy ra
//     được từ sự thật tất định; Engine xử lý tiếp theo trạng thái cuối, phán
//     quyết dự phòng lúc khởi động hoặc chờ người dùng can thiệp.
//
// Router chỉ phụ trách quyết định dạng "tra bảng" (bước kế tiếp mỗi chương,
// hậu xử lý cuối cung, hàng đợi), không phụ trách quyết định dạng "hiểu ngữ
// nghĩa" (chọn kiến trúc sư, xử lý Steer của người dùng, xuất tóm tắt).
package flow

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// plannerForTier suy ra danh tính kiến trúc sư từ quy mô quy hoạch đã ghi
// xuống đĩa: short thuộc kiến trúc sư truyện ngắn, mid/long thuộc kiến trúc sư
// truyện dài (cùng tiêu chí chọn với Arbiter lúc khởi động).
func plannerForTier(tier domain.PlanningTier) string {
	if tier == domain.PlanningTierShort {
		return "architect_short"
	}
	return "architect_long"
}

// Instruction chỉ định Worker cùng nhiệm vụ mà Engine sẽ chạy tiếp theo.
type Instruction struct {
	Agent   string // architect_long / architect_short / writer / editor
	Task    string // mô tả nhiệm vụ gửi cho sub-agent (theo ngôn ngữ tác phẩm)
	Reason  string // lý do định tuyến (dùng cho sự kiện, nhật ký và phán quyết)
	Chapter int    // số chương mà nhiệm vụ writer liên quan (viết tiếp/viết lại/hoàn thiện); 0 = không liên quan (nhiệm vụ editor/architect)
}

type AggregateKind string

const (
	AggregateArcReview     AggregateKind = "arc_review"
	AggregateArcSummary    AggregateKind = "arc_summary"
	AggregateVolumeSummary AggregateKind = "volume_summary"
	AggregateGlobalReview  AggregateKind = "global_review"
)

type AggregateRefresh struct {
	Kind         AggregateKind
	Volume       int
	Arc          int
	StartChapter int
	EndChapter   int
}

// State là đầu vào của Route: mọi sự thật phải được khai báo tường minh ở đây,
// Route không được tự đọc Store.
type State struct {
	// Lang là ngôn ngữ tác phẩm, dùng để sinh nhiệm vụ theo đúng ngôn ngữ mà
	// mô hình đang viết. Rỗng = tiếng Việt.
	Lang i18n.Lang

	Progress *domain.Progress

	// Số chương lớn nhất trong các chương đã hoàn thành; 0 = chưa bắt đầu sáng tác.
	LastCompleted int

	// Thông tin ranh giới cung của chương trước; các trường khác không có ý nghĩa
	// khi IsArcEnd=false. Phải là nil nếu LastCompleted=0 hoặc không phân lớp.
	ArcBoundary *storepkg.ArcBoundary

	// Ba sự thật của hậu xử lý cuối cung: đã thẩm duyệt / đã có tóm tắt cung /
	// đã có tóm tắt tập hay chưa.
	HasArcReview     bool
	HasArcSummary    bool
	HasVolumeSummary bool

	// Các mục thiết lập nền tảng còn thiếu (tín hiệu bổ sung trong giai đoạn quy hoạch).
	FoundationMissing []string

	// Quy mô quy hoạch đã ghi xuống đĩa (save_foundation ghi scale vào RunMeta).
	// Rỗng = lần quy hoạch đầu chưa sinh ra thiết lập nào, chưa xác định được
	// kiến trúc sư nào.
	PlanningTier domain.PlanningTier

	// Sách không phân lớp: chương vừa hoàn thành đã có thẩm duyệt scope=global
	// hay chưa (chỉ có ý nghĩa tại điểm kích hoạt ShouldReview; sách phân lớp
	// luôn là false).
	HasGlobalReview bool

	// Số ảnh hưởng của sửa đổi bên ngoài mà Kiến trúc sư phải xử lý trước khi
	// viết tiếp. Phản hồi thường của Người viết được để tới lần thao tác cấu
	// trúc tự nhiên kế tiếp để hấp thụ, không phái thêm kiến trúc sư cho từng
	// chương.
	ImmediateFeedbackCount int

	// Công cụ tổng hợp (cung/tập) sớm nhất cần Biên tập viên dựng lại sau sửa
	// đổi bên ngoài.
	AggregateRefresh *AggregateRefresh

	// Chương đầu hàng đợi làm lại đã có chỉ dẫn viết lại (chapter_contract) hay chưa.
	// Chỉ nói với writer "viết lại chương N" tức là không cho hướng: thực đo cho
	// thấy Biên tập viên xếp architect_directive_unclear là vấn đề nghiêm trọng
	// nhất trong hàng đợi — liệt kê đầy triệu chứng nhưng không nói nên viết
	// thành thế nào.
	RewriteHeadNeedsDirective bool
}

// Route trả về chỉ dẫn tất định cho bước kế tiếp dựa trên sự thật; trả về nil
// thì Engine xử lý theo ngữ cảnh gọi.
//
// Thứ tự ưu tiên quyết định (loại trừ nhau, khớp từ trên xuống):
//  1. Phase=Complete        → nil (Host tự xuất tóm tắt)
//  2. Thiếu thiết lập trong giai đoạn quy hoạch và xác định được kiến trúc sư →
//     cùng kiến trúc sư đó bổ sung; nếu không thì nil (Engine phán quyết dự phòng
//     lúc khởi động)
//  3. PendingRewrites khác rỗng → writer làm lại/hoàn thiện theo hàng đợi
//  4. Flow=Reviewing        → nil (dormant: hiện không có bên viết, trong thời gian
//     thẩm duyệt Flow thực ra là writing)
//  5. Flow=Steering         → nil (đang xử lý can thiệp của người dùng)
//  6. Sửa đổi bên ngoài làm công cụ tổng hợp mất hiệu lực → editor dựng lại
//  7. Sửa đổi bên ngoài ảnh hưởng quy hoạch về sau → kiến trúc sư xử lý
//  8. Sách phân lớp tới cuối cung → thẩm duyệt, tóm tắt, mở rộng cung hoặc thêm tập
//  9. Thẩm duyệt toàn cục của sách không phân lớp tới hạn → editor (global review)
//
// 10. Dàn ý không phân lớp đã cạn → kiến trúc sư (quyết định hoàn thành hay nối tiếp)
// 11. Còn lại                → writer (viết next_chapter)
func Route(s State) *Instruction {
	p := s.Progress
	if p == nil {
		return nil
	}
	t := flowTextsFor(s.Lang)

	// 1. Trạng thái cuối: Host tự sinh tóm tắt tất định từ sự thật trong store
	if p.Phase == domain.PhaseComplete {
		return nil
	}

	// 2. Bổ sung trong giai đoạn quy hoạch: quyết định dạng "tra bảng" — thiếu gì
	//    nằm trong store, danh tính kiến trúc sư suy ra từ scale đã ghi xuống đĩa
	//    (short → architect_short, còn lại → architect_long). tier rỗng nghĩa là
	//    lần quy hoạch đầu chưa ghi thiết lập nào (chọn kiến trúc sư là phán đoán
	//    ngữ nghĩa), Engine sẽ phán quyết dự phòng ở planStartFallback.
	if p.Phase != domain.PhaseWriting {
		if len(s.FoundationMissing) > 0 && s.PlanningTier != "" {
			task := fmt.Sprintf(t.foundationMissing, strings.Join(s.FoundationMissing, t.missingSep))
			if len(s.FoundationMissing) == 1 && s.FoundationMissing[0] == "foundation_audit" {
				task = t.foundationAudit
			}
			return &Instruction{
				Agent:  plannerForTier(s.PlanningTier),
				Task:   task,
				Reason: "Thiết lập nền tảng còn thiếu mục, tiếp tục phái cùng kiến trúc sư theo danh sách thiếu",
			}
		}
		return nil
	}

	// 3. Hàng đợi viết lại/hoàn thiện được ưu tiên (sự thật đã ghi xuống đĩa ở
	//    tầng công cụ, Router chỉ phái theo danh sách)
	if len(p.PendingRewrites) > 0 {
		ch := p.PendingRewrites[0]
		verb := t.rewriteChapter
		if p.Flow == domain.FlowPolishing {
			verb = t.polishChapter
		}
		task := fmt.Sprintf(verb, ch)
		if s.RewriteHeadNeedsDirective {
			// Chỉ nói "viết lại chương N" tức là không cho hướng: thực đo cho thấy
			// Biên tập viên xếp architect_directive_unclear là vấn đề nghiêm trọng
			// nhất trong hàng đợi làm lại. Ở đây không phái thêm kiến trúc sư —
			// thêm một lượt phái sẽ khiến hàng đợi không bao giờ ráo khi quy hoạch
			// thất bại — mà yêu cầu Người viết tự chốt hướng thành hợp đồng rồi
			// mới viết.
			task += fmt.Sprintf(t.rewriteNeedsDirective, ch)
		}
		return &Instruction{
			Agent:   "writer",
			Task:    task,
			Reason:  fmt.Sprintf("Hàng đợi PendingRewrites còn %d chương", len(p.PendingRewrites)),
			Chapter: ch,
		}
	}

	// 4. Đang thẩm duyệt → trả lại cho LLM. Đây là nhánh dormant: save_review chỉ
	//    đặt Flow thành writing/rewriting/polishing, không đường sản xuất nào đặt
	//    reviewing (trong thời gian thẩm duyệt Flow thực ra là writing, "thẩm duyệt
	//    trước khi viết tiếp" do thứ tự ưu tiên steering của agentcore bảo đảm,
	//    không dựa vào nhánh này). Giữ lại để đối xứng với Steering và để khi
	//    Biên tập viên đặt reviewing rõ ràng trong tương lai thì định tuyến nhường
	//    chỗ cho LLM.
	if p.Flow == domain.FlowReviewing {
		return nil
	}

	// 5. Đang xử lý can thiệp của người dùng: Tài phán đang phán quyết, Engine
	//    không giành quyền.
	if p.Flow == domain.FlowSteering {
		return nil
	}
	if refresh := s.AggregateRefresh; refresh != nil {
		switch refresh.Kind {
		case AggregateArcReview:
			return &Instruction{
				Agent: "editor",
				Task: fmt.Sprintf(
					t.refreshArcReview,
					refresh.Volume, refresh.Arc, refresh.StartChapter, refresh.EndChapter, refresh.EndChapter, refresh.EndChapter,
				),
				Reason: "Thiếu thẩm duyệt mức cung",
			}
		case AggregateArcSummary:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf(t.refreshArcSummary, refresh.Volume, refresh.Arc),
				Reason: "Thiếu tóm tắt cung",
			}
		case AggregateVolumeSummary:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf(t.refreshVolumeSummary, refresh.Volume),
				Reason: "Thiếu tóm tắt tập",
			}
		case AggregateGlobalReview:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf(t.refreshGlobalReview, refresh.EndChapter, refresh.EndChapter, refresh.EndChapter),
				Reason: "Thiếu thẩm duyệt toàn cục",
			}
		}
	}

	if s.ImmediateFeedbackCount > 0 {
		return &Instruction{
			Agent:  plannerForTier(s.PlanningTier),
			Task:   t.feedbackOnly,
			Reason: fmt.Sprintf("Còn %d ảnh hưởng của sửa đổi bên ngoài chưa lan tới quy hoạch về sau", s.ImmediateFeedbackCount),
		}
	}

	// 8. Hậu xử lý cuối cung trong chế độ phân lớp
	if p.Layered && s.ArcBoundary != nil && s.ArcBoundary.IsArcEnd {
		b := s.ArcBoundary
		switch {
		case !s.HasArcReview:
			return &Instruction{
				Agent: "editor",
				Task: fmt.Sprintf(
					t.arcReview,
					b.Volume, b.Arc, b.StartChapter, b.EndChapter, b.EndChapter, b.EndChapter,
				),
				Reason: "Thẩm duyệt cuối cung chưa hoàn tất",
			}
		case !s.HasArcSummary:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf(t.arcSummary, b.Volume, b.Arc),
				Reason: "Tóm tắt cung chưa hoàn tất",
			}
		case b.IsVolumeEnd && !s.HasVolumeSummary:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf(t.volumeSummary, b.Volume),
				Reason: "Tóm tắt tập chưa hoàn tất",
			}
		case b.NeedsExpansion && b.NextArc > 0:
			return &Instruction{
				Agent:  "architect_long",
				Task:   fmt.Sprintf(t.expandArc, b.NextVolume, b.NextArc),
				Reason: "Cung kế tiếp mới chỉ là khung, chờ triển khai",
			}
		case b.NeedsNewVolume:
			return &Instruction{
				Agent:  "architect_long",
				Task:   t.appendVolume,
				Reason: "Cuối tập cần quyết định thêm tập mới, tập chốt hay kết thúc toàn bộ",
			}
		}
	}

	// 11. Thẩm duyệt toàn cục của sách không phân lớp: mỗi ReviewInterval chương
	//     một lần (sự thật: global review của chương đó chưa ghi xuống đĩa).
	//     Trước đây lấy từ tín hiệu review_required trong giá trị trả về của
	//     commit_chapter, nay suy ra trực tiếp từ sự thật — giá trị trả về chỉ
	//     là ảnh phản chiếu của sự thật, Route nhìn thẳng cùng sự thật trong store.
	if !p.Layered && s.LastCompleted > 0 {
		if due, reason := domain.ShouldReview(len(p.CompletedChapters)); due && !s.HasGlobalReview {
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf(t.globalReview, s.LastCompleted, s.LastCompleted),
				Reason: reason,
			}
		}
	}

	// 12. Khi dàn ý không phân lớp đã cạn thì không được phái chương vượt phạm vi.
	//     Để Kiến trúc sư dựa trên sự thật truyện hiện tại quyết định hoàn thành,
	//     hoặc dùng revise_outline để nối tiếp kế hoạch từ chương kế tiếp.
	next := p.NextChapter()
	if next <= 0 {
		return nil
	}
	if !p.Layered && p.TotalChapters > 0 && next > p.TotalChapters {
		return &Instruction{
			Agent: plannerForTier(s.PlanningTier),
			Task: fmt.Sprintf(
				t.outlineExhausted,
				len(p.CompletedChapters), p.TotalChapters, next,
			),
			Reason: "Dàn ý không phân lớp đã cạn, cần quyết định hoàn thành hay nối tiếp",
		}
	}

	// 13. Viết tiếp bình thường
	return &Instruction{
		Agent:   "writer",
		Task:    fmt.Sprintf(t.writeChapter, next),
		Reason:  "Viết tiếp chương kế tiếp",
		Chapter: next,
	}
}
