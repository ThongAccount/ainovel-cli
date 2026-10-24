// Package notify cung cấp kênh cảnh báo không giám sát.
//
// Định vị hợp lệ (architecture.md §2.3): hành động thuần quan sát — cảnh báo không bao giờ
// can thiệp vào luồng điều khiển (không thử lại, không đổi phân công, không dừng máy),
// chỉ "cất tiếng" những sự kiện đã có sẵn trong TUI ra ngoài màn hình.
// Send chạy bất đồng bộ, không bao giờ chặn Host, thất bại thì chỉ ghi slog.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Notification là toàn bộ sự thật của một cảnh báo.
type Notification struct {
	Kind  string `json:"kind"`  // tên sự kiện ổn định do Kinds trả về
	Level string `json:"level"` // info / warn / error
	Title string `json:"title"`
	Body  string `json:"body"`
}

const (
	KindRunEnd        = "run_end"
	KindBudget        = "budget"
	KindAdvanceGate   = "advance_gate"
	KindStopGuard     = "stop_guard"
	KindPlanStart     = "plan_start"
	KindDeadlock      = "deadlock"
	KindWorkerFailure = "worker_failure"
)

// Kinds trả về toàn bộ tên sự kiện mà phiên bản hiện tại cho phép dùng trong notify.events.
// Đây là nguồn sự thật duy nhất của hợp đồng sự kiện thông báo.
func Kinds() []string {
	return []string{
		KindRunEnd,
		KindBudget,
		KindAdvanceGate,
		KindStopGuard,
		KindPlanStart,
		KindDeadlock,
		KindWorkerFailure,
	}
}

func IsKnownKind(kind string) bool {
	for _, known := range Kinds() {
		if kind == known {
			return true
		}
	}
	return false
}

// Notifier phân phối thông báo theo cấu hình. Giá trị không dùng được, phải tạo qua New;
// an toàn với nil (Send là noop).
type Notifier struct {
	command string          // khi khác rỗng thì thay thế kênh system (đẩy điện thoại đi qua đây)
	events  map[string]bool // nil = cho phép toàn bộ kind
	timeout time.Duration
}

// New tạo Notifier. Khi command rỗng sẽ dùng kênh system dựng sẵn (bong bóng thông báo Windows /
// osascript trên macOS / notify-send trên Linux); khi events khác rỗng thì chỉ cho phép các kind đã liệt kê.
func New(command string, events []string) *Notifier {
	n := &Notifier{command: strings.TrimSpace(command), timeout: 10 * time.Second}
	if len(events) > 0 {
		n.events = make(map[string]bool, len(events))
		for _, ev := range events {
			n.events[ev] = true
		}
	}
	return n
}

// Send gửi bất đồng bộ một thông báo. Lọc, thực thi và xử lý thất bại đều không ảnh hưởng lời gọi.
func (n *Notifier) Send(nt Notification) {
	if !n.allows(nt.Kind) {
		return
	}
	go n.deliver(nt)
}

// allows cho biết kind đó có được phép hay không (chặn khi Notifier là nil hoặc kind không có trong events).
func (n *Notifier) allows(kind string) bool {
	if n == nil {
		return false
	}
	return n.events == nil || n.events[kind]
}

// deliver thực hiện đồng bộ một lần gửi và ghi lại lỗi, được Send gọi trong goroutine.
func (n *Notifier) deliver(nt Notification) {
	if err := n.deliverError(nt); err != nil {
		slog.Warn("Gửi thông báo thất bại", "module", "notify", "kind", nt.Kind, "err", err)
	}
}

// deliverError thực hiện đồng bộ một lần gửi và trả về lỗi gốc. Send gọi deliver trong
// goroutine để ghi lỗi; test gọi thẳng hàm này để lỗi không bị triệu chứng thứ cấp che mất.
func (n *Notifier) deliverError(nt Notification) error {
	ctx, cancel := context.WithTimeout(context.Background(), n.timeout)
	defer cancel()

	if n.command != "" {
		return runCommand(ctx, n.command, nt)
	}
	return runSystem(ctx, nt)
}

// runCommand chạy lệnh do người dùng cấu hình: các trường được truyền qua biến môi trường
// (một dòng curl, không phụ thuộc gì, không có rủi ro chèn lệnh), đồng thời ghi JSON đầy đủ
// vào stdin (kịch bản phân phối phức tạp tự phân tích). Hết giờ bị ctx cưỡng chế.
func runCommand(ctx context.Context, command string, nt Notification) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		powershell, err := findPowerShell()
		if err != nil {
			return err
		}
		cmd = exec.CommandContext(ctx, powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Env = notificationEnv(nt)
	payload, _ := json.Marshal(nt)
	cmd.Stdin = strings.NewReader(string(payload))
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("lệnh thông báo hết giờ: %w", ctxErr)
		}
		return err
	}
	return nil
}

func notificationEnv(nt Notification) []string {
	return append(os.Environ(),
		"NOTIFY_KIND="+nt.Kind,
		"NOTIFY_LEVEL="+nt.Level,
		"NOTIFY_TITLE="+nt.Title,
		"NOTIFY_BODY="+nt.Body,
	)
}

// runSystem gửi thông báo desktop dựng sẵn: chỉ phủ kịch bản "người dùng ngồi cạnh máy",
// không tìm thấy lệnh thì hạ xuống im lặng.
func runSystem(ctx context.Context, nt Notification) error {
	switch runtime.GOOS {
	case "windows":
		return runWindowsNotification(ctx, nt)
	case "darwin":
		script := "display notification " + appleScriptString(nt.Body) + " with title " + appleScriptString(nt.Title)
		return exec.CommandContext(ctx, "osascript", "-e", script).Run()
	case "linux":
		if _, err := exec.LookPath("notify-send"); err != nil {
			slog.Info("Thông báo hạ xuống thành nhật ký (không có notify-send)", "module", "notify", "title", nt.Title, "body", nt.Body)
			return nil
		}
		return exec.CommandContext(ctx, "notify-send", nt.Title, nt.Body).Run()
	default:
		slog.Info("Thông báo hạ xuống thành nhật ký (nền tảng không có kênh system)", "module", "notify", "title", nt.Title, "body", nt.Body)
		return nil
	}
}

// runWindowsNotification dùng PowerShell sẵn có của hệ thống cùng WinForms NotifyIcon.
// Windows 10/11 hiển thị bong bóng ở góc trên bên phải và đưa nó vào trải nghiệm thông báo
// của hệ thống; không cần cài module, đăng ký ứng dụng hay mang theo binary phụ.
// Lời gọi vốn đã chạy bất đồng bộ, thời gian giữ tiến trình sống ngắn chỉ để hệ thống
// kịp nhận bong bóng.
func runWindowsNotification(ctx context.Context, nt Notification) error {
	powershell, err := findPowerShell()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, powershell,
		"-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-Command", windowsNotificationScript)
	cmd.Env = notificationEnv(nt)
	return cmd.Run()
}

func findPowerShell() (string, error) {
	// Ưu tiên PowerShell 7: trên GitHub Windows runner và các môi trường Windows hiện đại,
	// pwsh xử lý việc chuyển hướng stdin ổn định hơn; Windows PowerShell 5.1 chỉ làm
	// phương án dự phòng để tương thích.
	for _, name := range []string{"pwsh.exe", "pwsh", "powershell.exe", "powershell"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("Thông báo Windows cần PowerShell, nhưng không tìm thấy powershell.exe hay pwsh.exe trên hệ thống")
}

const windowsNotificationScript = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$notify = New-Object System.Windows.Forms.NotifyIcon
$notify.Icon = [System.Drawing.SystemIcons]::Information
$notify.BalloonTipTitle = $env:NOTIFY_TITLE
$notify.BalloonTipText = $env:NOTIFY_BODY
$notify.BalloonTipIcon = switch ($env:NOTIFY_LEVEL) {
  'error' { [System.Windows.Forms.ToolTipIcon]::Error; break }
  'warn'  { [System.Windows.Forms.ToolTipIcon]::Warning; break }
  default { [System.Windows.Forms.ToolTipIcon]::Info }
}
$notify.Visible = $true
$notify.ShowBalloonTip(4000)
Start-Sleep -Milliseconds 4500
$notify.Dispose()`

// appleScriptString bọc mọi văn bản thành một literal chuỗi của AppleScript.
func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
