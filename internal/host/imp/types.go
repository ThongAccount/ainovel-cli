// Package imp hiện thực pipeline nhập ngữ nghĩa theo giai đoạn cho tiểu thuyết bên ngoài
// (docs/import-pipeline.md).
//
// Mô hình phụ trách hiểu ngữ nghĩa mở, mã nguồn phụ trách toạ độ, độ phủ, kiểu, hash, thứ tự và
// tính lũy thừa; toàn bộ sản phẩm ngữ nghĩa chỉ được xác minh xong trong workspace riêng
// (meta/import/) rồi mới phát hành sang trạng thái sách chính thức. Hành động kế tiếp chỉ
// được suy ra từ sản phẩm (NextAction), không lưu bộ enum giai đoạn có thể trôi lệch, việc
// khôi phục không phụ thuộc from=N.
package imp

import "time"

// Options điều khiển một lần nhập. Khi khôi phục các trường có thể rỗng và được suy ra trực tiếp
// từ workspace đang hoạt động cùng Intent đã lưu.
type Options struct {
	SourcePath      string // bắt buộc với import mới; có thể rỗng khi khôi phục
	AutoConfirm     bool   // --yes: tự động chấp nhận kết quả cắt sau khi kiểm tra xác minh ghi đè đạt
	StoryResolution string // --story=open|closed: chỉ chọn trước khi synthesis trả về uncertain
	ContinueAfter   bool   // --continue: không tạo Hold khi import hoàn tất
	Guidance        string // --guide: hướng dẫn cắt bằng ngôn ngữ tự nhiên; ghi xuống đĩa workspace rồi làm kết quả cắt cũ lệch và được nhận diện lại một cách tự nhiên
	// AcceptSegmentation: xác nhận thủ công tường minh sau bản xem trước ở TUI (y). Cho qua một
	// lần kết quả cắt hiện tại, không ghi intent;
	// khác với --yes: --yes là quyền cho phép mù khi chưa xem bản xem trước, không cho qua kết quả
	// cắt kèm ghi chú dung sai (Notes), còn y là phán quyết sau khi đã xem bản xem trước.
	AcceptSegmentation bool
}

// intent rút ra từ Options các quyền của người dùng cần lưu lâu dài.
func (o Options) intent() Intent {
	return Intent{
		Version:             workspaceSchemaVersion,
		AutoConfirm:         o.AutoConfirm,
		StoryResolution:     o.StoryResolution,
		ContinueAfterImport: o.ContinueAfter,
	}
}

// Stage biểu thị giai đoạn hiện tại của quy trình nhập, chỉ dùng để hiển thị UI, không phải nguồn
// sự thật khi khôi phục (RFC §14.1).
type Stage string

const (
	StageIngesting            Stage = "ingesting"
	StageSegmenting           Stage = "segmenting"
	StageAwaitingConfirmation Stage = "awaiting_confirmation"
	StageAnalyzing            Stage = "analyzing"
	StageSynthesizing         Stage = "synthesizing"
	StageAwaitingStoryStatus  Stage = "awaiting_story_status"
	StageValidating           Stage = "validating"
	StagePublishing           Stage = "publishing"
	StageDone                 Stage = "done"
	StageError                Stage = "error"
)

// Event là sự kiện tiến trình mà quy trình nhập phát ra ra ngoài. Event là một phép chiếu, không
// tham gia vào việc khôi phục.
type Event struct {
	Time      time.Time
	Stage     Stage
	Current   int       // tiến trình chương / đoạn
	Total     int       // tổng số
	Message   string    // mô tả dễ đọc cho con người
	Level     string    // ""=普通进度；"warn"=cảnh báo như lùi rồi thử lại / hỏi lại khi kiểm tra
	Key       string    // khi khác rỗng, UI cập nhật tại chỗ các sự kiện liên tiếp cùng Key (ví dụ 7 lần lùi thay đổi trên một dòng), khớp cơ chế ID của bảng sự kiện
	RetryAt   time.Time // khác 0 = thời điểm hết hạn của lần thử lại kế tiếp; UI đếm ngược từng giây theo đó, tới hạn là xoá (yêu cầu đã được gửi đi)
	Err       error     // mang theo khi StageError
	Continued bool      // do Host đặt khi StageDone: đã tự động tiếp tục nối khởi chạy Engine (--continue × auto)
}
