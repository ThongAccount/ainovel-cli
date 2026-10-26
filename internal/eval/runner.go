package eval

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/entry/startup"
	"github.com/voocel/ainovel-cli/internal/host"
)

// RunOptions điều khiển một lần chạy case.
type RunOptions struct {
	OutputDir string        // thư mục xuất cách ly (bắt buộc)
	Timeout   time.Duration // giới hạn thời gian thực tế của một case; 0 nghĩa là không giới hạn
	Progress  io.Writer     // dòng tiến độ (tùy chọn, nil thì không in)
}

// RunCase điều khiển một case: lắp ráp host → khởi chạy → đẩy tới trần số chương → Abort
// khi tới hạn. bundle đã được người gọi áp dụng override variant (nếu có). error trả về
// chính là "lỗi runtime" (cơ sở của hard fail); viết xong bình thường hoặc cắt ngắn bình
// thường đều trả về nil.
//
// RunCase chiếm quyền và đặt lại OutputDir: StartPrepared chỉ đặt lại progress/checkpoints,
// không xoá chapters/ foundation và các artifact khác, dùng lại thư mục cũ sẽ để cặn sót
// làm bẩn diag và novel_context. Vì vậy xoá sạch trước khi chạy để bảo đảm cách ly.
func RunCase(cfg bootstrap.Config, bundle assets.Bundle, c Case, opts RunOptions) error {
	if strings.TrimSpace(opts.OutputDir) == "" {
		return fmt.Errorf("RunCase: thiếu OutputDir")
	}
	if err := os.RemoveAll(opts.OutputDir); err != nil {
		return fmt.Errorf("dọn thư mục xuất: %w", err)
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return fmt.Errorf("tạo thư mục xuất: %w", err)
	}
	cfg.OutputDir = opts.OutputDir
	if c.Style != "" {
		cfg.Style = c.Style
	}

	eng, err := host.New(cfg, bundle, host.WithFileLog("headless.log", false))
	if err != nil {
		return fmt.Errorf("lắp ráp host: %w", err)
	}
	defer eng.Close()
	if logErr := eng.FileLogError(); logErr != nil {
		return fmt.Errorf("nhật ký tệp đánh giá không dùng được: %w", logErr)
	}

	prompt, err := startup.PrepareQuick(c.Prompt)
	if err != nil {
		return err
	}
	if err := eng.PrepareUserRules(prompt); err != nil {
		return fmt.Errorf("chuẩn bị quy tắc người dùng: %w", err)
	}
	if err := eng.StartPrepared(prompt); err != nil {
		return fmt.Errorf("khởi chạy: %w", err)
	}

	return drive(eng, c.MaxChapters, opts)
}

// driveEngine là giao diện engine tối thiểu mà drive tiêu thụ (*host.Host thỏa mãn sẵn).
// Tách ra để có kiểm thử tất định cho kỷ luật drain-to-Done — đoạn logic đồng thời này
// từng dính lỗi send-on-closed-channel.
type driveEngine interface {
	Events() <-chan host.Event
	Stream() <-chan string
	Done() <-chan struct{}
	Snapshot() host.UISnapshot
	Abort() bool
}

// drive tiêu thụ luồng sự kiện của engine, Abort khi tới trần số chương hoặc hết thời gian,
// chờ Done để kết thúc.
//
// Kỷ luật then chốt: dù hoàn thành bình thường, cắt ngắn theo trần hay hết thời gian, đều
// phải drain tới Done mới trả về. waitDone ở nền của host sẽ gửi một lần vào done, còn
// eng.Close() (defer của RunCase) sẽ close(done) — trả về sớm khiến Close cạnh tranh đóng
// channel với lần gửi của waitDone rồi panic (send on closed channel). headless cũng dựa
// vào "Done trước, Close sau". Đồng thời phải ráo hết Events và Stream để không chặn engine.
func drive(eng driveEngine, maxChapters int, opts RunOptions) error {
	var timeoutCh <-chan time.Time
	if opts.Timeout > 0 {
		t := time.NewTimer(opts.Timeout)
		defer t.Stop()
		timeoutCh = t.C
	}

	aborted, timedOut := false, false
	// finish được gọi sau khi drain tới Done (hoặc khi channel đóng): hết thời gian thì trả về error, nếu không thì kết thúc bình thường.
	finish := func() error {
		if timedOut {
			return fmt.Errorf("chạy quá thời gian (%s)", opts.Timeout)
		}
		return nil
	}
	for {
		select {
		case ev, ok := <-eng.Events():
			if !ok {
				return finish()
			}
			if opts.Progress != nil && strings.TrimSpace(ev.Summary) != "" {
				fmt.Fprintf(opts.Progress, "    [%s] %s\n", ev.Category, ev.Summary)
			}
			if !aborted && capReached(eng.Snapshot(), maxChapters) {
				eng.Abort()
				aborted = true
				timeoutCh = nil // đã tới điều kiện cắt ngắn, chuyển sang thu dọn bình thường, không còn chịu ràng buộc thời gian (tránh nhầm cắt ngắn thành công với hết thời gian)
			}
		case <-eng.Stream():
			// ráo luồng tăng dần, không tiêu thụ nội dung — eval không quan tâm luồng văn bản, chỉ nhìn sự thật đã ghi xuống đĩa.
		case _, ok := <-eng.Done():
			if !ok {
				return finish()
			}
			return finish()
		case <-timeoutCh:
			eng.Abort() // ở đây aborted chắc chắn là false (cắt ngắn theo trần sẽ đặt timeoutCh = nil)
			aborted, timedOut = true, true
			timeoutCh = nil // tắt bộ đếm thời gian, tiếp tục drain tới Done, rồi finish trả về lỗi hết thời gian
		}
	}
}

// capReached kiểm tra đã tới điều kiện cắt ngắn chưa. maxChapters>0 thì tính theo số chương
// đã hoàn thành; <=0 thì xem là "loại quy hoạch", quy hoạch xong (bước writing hoặc đã
// complete) là dừng.
func capReached(snap host.UISnapshot, maxChapters int) bool {
	if maxChapters <= 0 {
		return snap.Phase == string(domain.PhaseWriting) || snap.Phase == string(domain.PhaseComplete)
	}
	return snap.CompletedCount >= maxChapters
}
