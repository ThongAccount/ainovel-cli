package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Setup khởi tạo logger mặc định của slog.
// w là đích ghi nhật ký, level là mức nhật ký tối thiểu.
func Setup(w io.Writer, level slog.Level) {
	slog.SetDefault(slog.New(newTextHandler(w, level)))
}

func newTextHandler(w io.Writer, level slog.Level) slog.Handler {
	return slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Giữ nguyên ngày, mili giây và múi giờ; khi nhật ký được ghi nối qua nhiều tiến trình
			// vẫn khớp chính xác với phiên bản mã nguồn và phiên làm việc.
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().Format("2006-01-02T15:04:05.000Z07:00"))
			}
			return a
		},
	})
}

func newSessionLogger(w io.Writer, level slog.Level, sessionAttrs ...slog.Attr) (*slog.Logger, string) {
	sessionID := fmt.Sprintf("%s-p%d", time.Now().Format("20060102T150405.000Z0700"), os.Getpid())
	attrs := make([]slog.Attr, 0, len(sessionAttrs)+1)
	attrs = append(attrs, slog.String("session", sessionID))
	attrs = append(attrs, sessionAttrs...)
	handler := newTextHandler(w, level).WithAttrs(attrs)
	return slog.New(handler), sessionID
}

// FileLogger trả về logger độc lập ghi vào outputDir/logs/filename cùng hàm dọn dẹp,
// dành cho các hệ thống con cần tệp nhật ký riêng (ví dụ luồng nhập). Nếu mở tệp thất bại
// thì rơi về logger mặc định mà không ngắt nghiệp vụ, nhưng lỗi vẫn phải được trả về để
// lời gọi hiển thị cho người dùng — nếu không, UI sẽ bảo người dùng đi xem một tệp nhật ký
// không hề tồn tại.
func FileLogger(outputDir, filename string) (*slog.Logger, func(), error) {
	f, err := openLogFile(outputDir, filename)
	if err != nil {
		return slog.Default(), func() {}, err
	}
	logger, sessionID := newSessionLogger(f, slog.LevelDebug)
	logger.Info("Bắt đầu phiên nhật ký", "module", "logger", "session_id", sessionID)
	return logger, func() {
		logger.Info("Kết thúc phiên nhật ký", "module", "logger", "session_id", sessionID)
		_ = f.Close()
	}, nil
}

// SetupFile khởi tạo logger mặc định ghi ra tệp và trả về hàm dọn dẹp.
// alsoStderr=true thì đồng thời xuất ra stderr.
// Nếu không mở được thư mục hoặc tệp nhật ký thì trả về lỗi, lời gọi phải xử lý tường minh;
// cấm chuyển sang io.Discard rồi chạy tiếp, vì như vậy đúng lúc cần truy vết lại mất sạch
// toàn bộ nhật ký runtime.
func SetupFile(outputDir, filename string, alsoStderr bool, sessionAttrs ...slog.Attr) (func(), error) {
	f, err := openLogFile(outputDir, filename)
	if err != nil {
		return nil, err
	}

	var w io.Writer = f
	if alsoStderr {
		w = io.MultiWriter(os.Stderr, f)
	}
	previous := slog.Default()
	logger, sessionID := newSessionLogger(w, slog.LevelDebug, sessionAttrs...)
	slog.SetDefault(logger)
	logger.Info("Bắt đầu phiên nhật ký", "module", "logger", "session_id", sessionID)

	return func() {
		logger.Info("Kết thúc phiên nhật ký", "module", "logger", "session_id", sessionID)
		slog.SetDefault(previous)
		_ = f.Close()
	}, nil
}

func openLogFile(outputDir, filename string) (*os.File, error) {
	logPath := filepath.Join(outputDir, "logs", filename)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory %q: %w", filepath.Dir(logPath), err)
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %q: %w", logPath, err)
	}
	return f, nil
}
