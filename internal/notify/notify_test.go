package notify

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAllowsFilter(t *testing.T) {
	if New("", nil).allows(KindDeadlock) != true {
		t.Error("mặc định events phải cho phép tất cả")
	}
	n := New("", []string{KindRunEnd, KindBudget})
	if !n.allows(KindRunEnd) || !n.allows(KindBudget) {
		t.Error("kind đã liệt kê phải được cho phép")
	}
	if n.allows(KindDeadlock) {
		t.Error("kind chưa liệt kê phải bị chặn")
	}
	var nilN *Notifier
	if nilN.allows(KindRunEnd) {
		t.Error("nil Notifier phải chặn tất cả")
	}
	nilN.Send(Notification{Kind: KindRunEnd}) // không được panic
}

func TestKindsAreUniqueAndKnown(t *testing.T) {
	seen := map[string]bool{}
	for _, kind := range Kinds() {
		if kind == "" || seen[kind] {
			t.Fatalf("tên sự kiện thông báo phải khác rỗng và duy nhất: %q", kind)
		}
		seen[kind] = true
		if !IsKnownKind(kind) {
			t.Fatalf("Kinds không khớp với IsKnownKind: %q", kind)
		}
	}
	if IsKnownKind("repeat") {
		t.Fatal("sự kiện repeat cũ không nên còn xuất hiện trong hợp đồng mới")
	}
}

func TestCommandChannelEnvAndStdin(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env.txt")
	jsonFile := filepath.Join(dir, "stdin.json")

	command := `echo "$NOTIFY_KIND|$NOTIFY_LEVEL|$NOTIFY_TITLE|$NOTIFY_BODY" > ` + shellQuote(envFile) + ` && cat > ` + shellQuote(jsonFile)
	if runtime.GOOS == "windows" {
		// Ép UTF-8 (không BOM) để tiêu đề/nội dung tiếng Việt sống sót qua code page mặc định của PowerShell.
		command = `$utf8 = New-Object System.Text.UTF8Encoding $false; ` +
			`$line = "$env:NOTIFY_KIND|$env:NOTIFY_LEVEL|$env:NOTIFY_TITLE|$env:NOTIFY_BODY"; ` +
			`[System.IO.File]::WriteAllText(` + powerShellQuote(envFile) + `, $line, $utf8); ` +
			`$reader = New-Object System.IO.StreamReader([Console]::OpenStandardInput(), $utf8); ` +
			`$payload = $reader.ReadToEnd(); ` +
			`[System.IO.File]::WriteAllText(` + powerShellQuote(jsonFile) + `, $payload, $utf8)`
	}
	n := New(command, nil)
	nt := Notification{Kind: KindBudget, Level: "warn", Title: "ainovel: ngân sách", Body: "đã chi $8.00"}
	if err := n.deliverError(nt); err != nil {
		t.Fatalf("command thực thi thất bại: %v", err)
	}

	env, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("command chưa được thực thi: %v", err)
	}
	if got := strings.TrimSpace(string(env)); got != "budget|warn|ainovel: ngân sách|đã chi $8.00" {
		t.Errorf("biến môi trường truyền sai: %q", got)
	}

	raw, err := os.ReadFile(jsonFile)
	if err != nil {
		t.Fatalf("stdin chưa được truyền: %v", err)
	}
	var decoded Notification
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("stdin không phải JSON hợp lệ: %v", err)
	}
	if decoded != nt {
		t.Errorf("stdin JSON không khớp: %+v", decoded)
	}
}

func TestCommandChannelTimeoutKill(t *testing.T) {
	command := "sleep 30"
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 30"
	}
	n := New(command, nil)
	n.timeout = 200 * time.Millisecond

	start := time.Now()
	err := n.deliverError(Notification{Kind: KindRunEnd})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("command quá thời gian phải trả về context deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("command quá thời gian không bị giết cưỡng chế, chặn %v", elapsed)
	}
}

func TestFindPowerShellPrefersPwsh(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("lựa chọn PowerShell chỉ áp dụng cho Windows")
	}
	want, err := exec.LookPath("pwsh.exe")
	if err != nil {
		t.Skip("pwsh.exe chưa cài đặt, chỉ kiểm tra đường tương thích Windows PowerShell")
	}
	got, err := findPowerShell()
	if err != nil {
		t.Fatalf("findPowerShell: %v", err)
	}
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(want)) {
		t.Fatalf("phải ưu tiên pwsh.exe, got %q, want %q", got, want)
	}
}

func TestWindowsNotificationScriptUsesEnvironmentWithoutInterpolation(t *testing.T) {
	for _, want := range []string{"$env:NOTIFY_TITLE", "$env:NOTIFY_BODY", "$env:NOTIFY_LEVEL", "ShowBalloonTip"} {
		if !strings.Contains(windowsNotificationScript, want) {
			t.Fatalf("Windows notification script missing %q", want)
		}
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func powerShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
