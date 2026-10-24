package tui

import "github.com/charmbracelet/lipgloss"

// Bảng màu chủ đề — tông ấm mang vẻ sách cuốn
// AdaptiveColor: Light = màu nền sáng, Dark = màu nền tối
//
// Nguyên tắc thiết kế: bảng Light giữ nguyên (nền sáng đã chỉnh ra hiệu quả hài lòng);
// bảng Dark đều đặn sáng hơn Light ~25% lightness và tăng nhẹ độ bão hoà, đảm bảo nền tối
// đủ tương phản (colorDim trước đây là #6b6355 trên nền đen #1c1c1c gần như không thấy,
// đường phân cách và chữ phụ biến mất hoàn toàn).
//
// colorAccent2 ở nền tối đổi từ #7a9e7e sang xanh ngọc #5fb8a3, tách xa khỏi "xanh khỏe"
// của colorSuccess — trước đó hai màu trùng nhau hoàn toàn, làm màu đánh dấu của architect
// agent lẫn với niềm vui "trúng cao".
// bodyTextColor là chiến lược màu chữ cho "nội dung trung tính":
//   - terminal nền tối → NoColor, kế thừa màu chữ mặc định của terminal, tránh ép cứng
//     màu kem #e8e0d0 đụng màu với nền ấm/lạnh do người dùng tự cấu hình
//     (thực tế đo được màu mặc định trên nền tối dễ đọc hơn).
//   - terminal nền sáng → dùng bảng Light của colorText (nâu đậm #3d3529), giữ tông ấm
//     của thương hiệu; màu đen mặc định trên nền sáng tương phản gắt quá, nâu đậm đã
//     chỉnh trước đó nhìn dịu hơn trên nền sáng.
//
// AdaptiveColor bắt buộc phải có giá trị ở cả hai đầu, không có bậc "không màu", nên ở đây
// ta xác định nền một lần lúc khởi động, sau đó mọi giá trị tổng quan / nội dung chương /
// mô tả lệnh thuộc "nội dung trung tính" đều dùng chung bodyTextColor.
var bodyTextColor lipgloss.TerminalColor = func() lipgloss.TerminalColor {
	if lipgloss.HasDarkBackground() {
		return lipgloss.NoColor{}
	}
	return lipgloss.Color("#3d3529")
}()

var (
	colorText    = lipgloss.AdaptiveColor{Light: "#3d3529", Dark: "#e8e0d0"}
	colorDim     = lipgloss.AdaptiveColor{Light: "#8a7e6b", Dark: "#8a8175"}
	colorMuted   = lipgloss.AdaptiveColor{Light: "#7a7060", Dark: "#b8b09c"}
	colorAccent  = lipgloss.AdaptiveColor{Light: "#b8860b", Dark: "#e5b449"}
	colorAccent2 = lipgloss.AdaptiveColor{Light: "#3d7a42", Dark: "#5fb8a3"}
	colorRunning = lipgloss.AdaptiveColor{Light: "#6f8641", Dark: "#b5d075"}
	colorSuccess = lipgloss.AdaptiveColor{Light: "#3d7a42", Dark: "#7ec488"}
	colorError   = lipgloss.AdaptiveColor{Light: "#b5433a", Dark: "#e07060"}
	colorReview  = lipgloss.AdaptiveColor{Light: "#b07530", Dark: "#e09b5a"}
	colorContext = lipgloss.AdaptiveColor{Light: "#6b5a9e", Dark: "#a890d8"}
	colorTool    = lipgloss.AdaptiveColor{Light: "#3a7a8a", Dark: "#7ec5d8"}
)

// Bảng màu cho nhãn trạng thái
var statusColors = map[string]lipgloss.AdaptiveColor{
	"READY":    colorDim,
	"PAUSING":  colorAccent,
	"PAUSED":   colorAccent,
	"RUNNING":  colorRunning,
	"REVIEW":   colorReview,
	"REWRITE":  colorReview,
	"COMPLETE": colorSuccess,
	"ERROR":    colorError,
}

// Hiển thị trạng thái: biểu tượng + nhãn. Đồng nhất với tông chủ đề ấm tổng thể, tránh các
// khối màu đặc gây lạc.
// icon của RUNNING để trống, để frame spinner điền vào, giúp cảm giác chuyển động hoà vào
// chính chỉ báo trạng thái.
var statusDisplay = map[string]struct {
	icon  string
	label string
}{
	"READY":    {"○", "SẴN SÀNG"},
	"RUNNING":  {"", "ĐANG CHẠY"},
	"REVIEW":   {"◆", "ĐANG DUYỆT"},
	"REWRITE":  {"◆", "LÀM LẠI"},
	"COMPLETE": {"●", "HOÀN TẤT"},
	"PAUSED":   {"⏸", "TẠM DỪNG"},
	"PAUSING":  {"⏸", "ĐANG TẠM DỪNG"},
	"ERROR":    {"✕", "LỖI"},
}

// Bảng màu cho phân loại sự kiện
var categoryColors = map[string]lipgloss.AdaptiveColor{
	"DISPATCH": colorAccent,
	"DECISION": colorContext,
	"TOOL":     colorTool,
	"SYSTEM":   colorAccent,
	"USER":     colorAccent2,
	"REVIEW":   colorReview,
	"CHECK":    colorSuccess,
	"ERROR":    colorError,
	"AGENT":    colorMuted,
	"CONTEXT":  colorContext,
	"COMPACT":  colorContext,
}

// Style nền tảng
var (
	baseBorder = lipgloss.RoundedBorder()

	topBarStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Padding(0, 1)

	statusIconStyle = lipgloss.NewStyle().
			Bold(true)

	statusLabelStyle = lipgloss.NewStyle().
				Foreground(colorText)

	panelTitleStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	fieldLabelStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Width(10)

	// fieldValueStyle / cardContentStyle dùng bodyTextColor — các giá trị trong vùng tổng quan
	// (trạng thái chạy, số chương đã hoàn thành, số từ...), các mục dàn ý, danh sách nhân vật,
	// tóm tắt chương... tức là "nội dung trung tính" sẽ theo màu chữ mặc định của terminal
	// trên nền tối (tránh ép cứng màu kem đụng nền), còn trên nền sáng thì dùng nâu đậm
	// để giữ tông ấm.
	// Các phần tử mang tính ngữ nghĩa mạnh (tiêu đề, giá trị làm nổi bật, trạng thái, lỗi,
	// tô màu tỉ lệ trúng...) vẫn dùng colorAccent / colorError và các màu chủ đề khác.
	fieldValueStyle = lipgloss.NewStyle().Foreground(bodyTextColor)

	highlightValueStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	contextUsageMetaStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	cardTitleStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	cardContentStyle = lipgloss.NewStyle().Foreground(bodyTextColor)
)
