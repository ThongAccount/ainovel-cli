package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartCommandLoadsPromptFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "outline files")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "story outline.md")
	want := "Bối cảnh thế giới\n\nDàn ý tập một"
	if err := os.WriteFile(path, []byte("  "+want+"  "), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel(nil, "")
	cmd, ok := parseSlashCommand("/start " + path)
	if !ok {
		t.Fatal("/start should parse as slash command")
	}
	prompt, err := prepareFileStart(cmd.args)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != want {
		t.Fatalf("prompt = %q, want full file content", prompt)
	}
	next, startCmd := m.handleSlashCommand(cmd)
	got := next.(Model)
	if startCmd == nil || !got.starting || got.mode != modeRunning {
		t.Fatalf("start state = mode %v, starting %v, cmd %v", got.mode, got.starting, startCmd)
	}
}

func TestEnterStartingSwitchesToWorkbenchImmediately(t *testing.T) {
	m := NewModel(nil, "")
	m.width = 120
	m.height = 40
	m.resizeTextarea()
	m.updateViewportSize()

	m.enterStarting("viết một tiểu thuyết tiên hiệp phương Đông dài")

	if m.mode != modeRunning {
		t.Fatalf("mode = %v, want modeRunning", m.mode)
	}
	if !m.starting {
		t.Fatal("starting should be true while host startup command is running")
	}
	if !m.snapshot.IsRunning {
		t.Fatal("snapshot should render as running during local startup")
	}
	if got := m.textarea.Placeholder; got != "Đang khởi tạo sáng tác..." {
		t.Fatalf("placeholder = %q", got)
	}
	if len(m.events) != 2 {
		t.Fatalf("events = %+v, want startup user + system events", m.events)
	}
	if m.events[0].Category != "USER" || !strings.HasPrefix(m.events[0].Summary, "Yêu cầu sáng tác: ") {
		t.Fatalf("first event = %+v, want USER prompt event", m.events[0])
	}
}

func TestStartupFailureStaysInWorkbench(t *testing.T) {
	m := NewModel(nil, "")
	m.width = 120
	m.height = 40
	m.resizeTextarea()
	m.updateViewportSize()

	m.enterStarting("viết một tiểu thuyết tiên hiệp phương Đông dài")

	next, _ := m.handleStartResultMsg(startResultMsg{err: errors.New("tài khoản model chưa kích hoạt")})
	got := next.(Model)
	if got.mode != modeRunning {
		t.Fatalf("sau khi khởi động thất bại mode = %v, want modeRunning", m.mode)
	}
	if got.starting {
		t.Fatal("sau khi khởi động thất bại starting phải được đặt lại")
	}
	if got.snapshot.IsRunning {
		t.Fatal("sau khi khởi động thất bại snapshot không nên còn hiện đang chạy")
	}
	if !strings.Contains(got.textarea.Placeholder, "gián đoạn") && !strings.Contains(got.textarea.Placeholder, "thất bại") {
		t.Fatalf("placeholder = %q", got.textarea.Placeholder)
	}
	if len(got.events) == 0 || got.events[len(got.events)-1].Category != "ERROR" {
		t.Fatalf("bàn làm việc phải giữ lại sự kiện lỗi lúc khởi động: %+v", got.events)
	}
}

func TestApplyStartupPromptEventTruncatesSummaryButKeepsDetail(t *testing.T) {
	m := NewModel(nil, "")
	prompt := strings.Repeat("x", maxPromptEventCols+50)

	m.applyStartupPromptEvent(prompt)

	if len(m.events) != 1 {
		t.Fatalf("events = %+v, want one event", m.events)
	}
	ev := m.events[0]
	if ev.Detail != prompt {
		t.Fatalf("detail should keep full prompt, got len=%d want=%d", len([]rune(ev.Detail)), len([]rune(prompt)))
	}
	maxSummaryRunes := len([]rune("Yêu cầu sáng tác: ")) + maxPromptEventCols
	if got := len([]rune(ev.Summary)); got > maxSummaryRunes {
		t.Fatalf("summary runes = %d, want <= %d", got, maxSummaryRunes)
	}
	if !strings.HasSuffix(ev.Summary, "...") {
		t.Fatalf("summary should be truncated with ellipsis, got %q", ev.Summary)
	}
}

func TestStreamFlushTimerRunsOnlyForPendingData(t *testing.T) {
	m := NewModel(nil, "")
	next, cmd, handled := m.handleRuntimeMsg(streamDeltaMsg("nội dung chương"))
	if !handled || cmd == nil {
		t.Fatal("phần tăng dần của luồng phải khởi chạy một lần đẩy")
	}
	got := next.(Model)
	if !got.streamDirty || !got.flushPending {
		t.Fatal("phần tăng dần của luồng phải được đánh dấu chờ đẩy")
	}
	next, cmd, handled = got.handleRuntimeMsg(streamFlushTickMsg{})
	got = next.(Model)
	if !handled || cmd != nil || got.streamDirty || got.flushPending {
		t.Fatal("sau khi đẩy xong thì timer phải dừng")
	}
}
