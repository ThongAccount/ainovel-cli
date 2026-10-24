package host

import (
	"fmt"
	"math"
	"sync/atomic"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
)

// Máy trạng thái ngân sách: tiến triển đơn điệu, mỗi lần chuyển trạng thái kích hoạt đúng một tác dụng phụ, không lùi.
// Tăng ngân sách = người dùng cấp phép lại = khởi động lại / tạo Host instance mới sau khi đổi cấu hình, không lùi trạng thái trong instance hiện tại.
const (
	budgetNormal      int32 = iota // chưa tới mực cảnh báo
	budgetWarned                   // đã phát cảnh báo, chưa vượt hạn mức
	budgetStopPending              // đã vượt hạn mức, chờ dừng tại ranh giới sub-agent
	budgetStopped                  // đã thực hiện dừng
)

// BudgetSentinel theo dõi chi phí tích luỹ và thực thi chính sách ngân sách của người dùng (khối budget trong config).
//
// Định vị hợp lệ (architecture.md §8.3/§10): không đánh giá hành vi mô hình — dừng khi vượt hạn mức tương đương người dùng
// tự tay bấm Abort đúng thời điểm đó, Host chỉ thay người dùng thực thi một chỉ thị đã ký trước. Vì nó ảnh hưởng dòng điều khiển nên
// không phải là observer, mà là thành phần chính sách của Host ngang hàng với flow.Dispatcher; tầng Route/công cụ không nhận biết.
//
// Thời điểm dừng: mặc định tại ranh giới sub-agent (Host gọi HandleBoundary đồng bộ), không lãng phí chương đang chạy dở;
// khi hardStop=true thì vượt hạn mức là dừng ngay. Xử lý ranh giới diễn ra trước khi flow.Dispatcher phát bước kế tiếp, tầng Route/công cụ không nhận biết ngân sách.
type BudgetSentinel struct {
	limit     float64
	warnRatio float64
	hardStop  bool

	costNow func() float64              // chi phí tích luỹ hiện tại (bọc usage.Totals; có thể tiêm test stub)
	abort   func(reason string)         // bọc dừng của Host (kèm sự kiện lý do)
	report  func(level, summary string) // lối ra cảnh báo (emitEvent + notify, do Host tiêm)

	state atomic.Int32

	// Phát hiện vùng mù tính phí: mô hình không có giá trong sổ đăng ký và provider không tự báo cost sẽ cho mỗi lần ghi $0,
	// khiến ngân sách hỏng lặng lẽ. Đánh giá theo "nhiều lần liên tiếp tăng bằng 0" chứ không theo total==0 — cách sau không bắt được trường hợp
	// giữa lúc chạy lâu người dùng /model chuyển sang mô hình không có giá (total đứng ở giá trị lịch sử khác 0 nhưng không tăng).
	// Mô hình miễn phí cũng rơi vào trường hợp này, và cảnh báo "ngân sách sẽ không kích hoạt" vẫn đúng với chúng.
	lastTotal   atomic.Uint64 // math.Float64bits(chi phí tích luỹ của lần gọi trước)
	zeroStreak  atomic.Int32
	blindWarned atomic.Bool
}

// blindZeroStreak số lần ghi tăng bằng 0 liên tiếp thì mới cảnh báo. Mô hình tính phí bình thường mỗi lần tăng chắc chắn > 0
// (cost là float tích luỹ không làm tròn), lấy 5 chỉ để tránh nhiễu lạ, không phải ngưỡng chính sách có thể chỉnh.
const blindZeroStreak = 5

// NewBudgetSentinel tạo sentinel ngân sách; khi chính sách chưa bật trả về nil (mọi phương thức an toàn với nil).
func NewBudgetSentinel(cfg bootstrap.BudgetConfig, costNow func() float64, abort func(reason string), report func(level, summary string)) *BudgetSentinel {
	if !cfg.Enabled() {
		return nil
	}
	return &BudgetSentinel{
		limit:     cfg.BookUSD,
		warnRatio: cfg.WarnRatio,
		hardStop:  cfg.HardStop,
		costNow:   costNow,
		abort:     abort,
		report:    report,
	}
}

// OnCost do UsageTracker gọi sau mỗi lần ghi, mang theo chi phí tích luỹ mới nhất (ngoài khoá).
// Một lần gọi có thể vượt qua hai bậc (normal→warned→stopPending), mỗi tác dụng phụ kích hoạt đúng một lần.
func (s *BudgetSentinel) OnCost(total float64) {
	if s == nil {
		return
	}
	if prev := s.lastTotal.Swap(math.Float64bits(total)); total == math.Float64frombits(prev) {
		if s.zeroStreak.Add(1) >= blindZeroStreak && s.blindWarned.CompareAndSwap(false, true) {
			s.report("warn", fmt.Sprintf("vùng mù ngân sách: liên tục ghi nhận nhưng chi phí tích luỹ đứng ở $%.2f không tăng (mô hình hiện tại không có giá trong sổ đăng ký và provider không tự báo cost, hoặc là mô hình miễn phí) — hạn mức ngân sách sẽ không kích hoạt", total))
		}
	} else {
		s.zeroStreak.Store(0)
	}
	if total >= s.limit*s.warnRatio && s.state.CompareAndSwap(budgetNormal, budgetWarned) {
		s.report("warn", fmt.Sprintf("cảnh báo ngân sách: đã chi $%.2f, đạt %.0f%% của ngân sách $%.2f", total, s.limit, s.warnRatio*100))
	}
	if total >= s.limit && s.state.CompareAndSwap(budgetWarned, budgetStopPending) {
		if s.hardStop {
			s.report("error", fmt.Sprintf("ngân sách đã cạn: đã chi $%.2f, vượt ngân sách $%.2f, dừng ngay", total, s.limit))
			s.stop(total)
			return
		}
		s.report("error", fmt.Sprintf("ngân sách đã cạn: đã chi $%.2f, vượt ngân sách $%.2f, sẽ dừng sau khi nhiệm vụ sub-agent hiện tại kết thúc", total, s.limit))
	}
}

// HandleEvent thực hiện lệnh dừng đang chờ tại ranh giới sub-agent. Đăng ký phải diễn ra trước Dispatcher.
// Không bỏ qua IsError — trả về lỗi cũng là một ranh giới, lệnh dừng không nên hoãn vì sub-agent thất bại.
func (s *BudgetSentinel) HandleEvent(ev agentcore.Event) {
	if s == nil {
		return
	}
	if ev.Type != agentcore.EventToolExecEnd || ev.Tool != "subagent" {
		return
	}
	s.HandleBoundary()
}

func (s *BudgetSentinel) HandleBoundary() bool {
	if s == nil || s.state.Load() != budgetStopPending {
		return false
	}
	s.stop(s.costNow())
	return true
}

func (s *BudgetSentinel) stop(total float64) {
	if s.state.CompareAndSwap(budgetStopPending, budgetStopped) {
		s.abort(fmt.Sprintf("dừng vì ngân sách: đã chi $%.2f, vượt ngân sách $%.2f; tăng budget.book_usd rồi chạy tiếp được", total, s.limit))
	}
}

// Refuse kiểm tra trước khi khởi động: nếu đã vượt ngân sách thì trả lỗi từ chối (được gọi ở các đường khôi phục Start/Resume/Continue).
// Người dùng tăng ngân sách = cấp phép lại, với cấu hình mới Refuse sẽ tự động cho qua.
func (s *BudgetSentinel) Refuse() error {
	if s == nil {
		return nil
	}
	if cost := s.costNow(); cost >= s.limit {
		return fmt.Errorf("tác phẩm này đã chi $%.2f, đạt hạn mức ngân sách $%.2f; hãy tăng cấu hình budget.book_usd rồi thử lại", cost, s.limit)
	}
	return nil
}

// Limit trả về hạn mức ngân sách (dùng để hiển thị trên UI); nếu chưa bật trả về 0.
func (s *BudgetSentinel) Limit() float64 {
	if s == nil {
		return 0
	}
	return s.limit
}
