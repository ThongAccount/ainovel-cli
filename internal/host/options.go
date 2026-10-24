package host

import "log/slog"

type newOptions struct {
	logFile       string
	logAlsoStderr bool
	logAttrs      []slog.Attr
}

// NewOption cấu hình quá trình khởi tạo Host; tài nguyên thời gian chạy vẫn do Host nắm giữ.
type NewOption func(*newOptions)

// WithFileLog để Host giữ một phiên nhật ký thời gian chạy. Nhật ký chỉ mở sau khi đã có lease thư mục tiểu thuyết,
// và đóng sau khi mọi thao tác đóng nhật ký của Host hoàn tất. Nếu mở nhật ký thất bại thì tiếp tục dùng logger của tiến trình hiện tại,
// và phía gọi phải xử lý tường minh lỗi đó qua FileLogError.
func WithFileLog(filename string, alsoStderr bool, attrs ...slog.Attr) NewOption {
	return func(opts *newOptions) {
		opts.logFile = filename
		opts.logAlsoStderr = alsoStderr
		opts.logAttrs = append([]slog.Attr(nil), attrs...)
	}
}
