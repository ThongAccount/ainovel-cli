package eval

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/voocel/ainovel-cli/internal/host"
)

// fakeEngine mô phỏng host: sau Abort, gửi một lần vào done như waitDone. done có buffer 1,
// test dựa vào đó để khẳng định drive đã drain Done hay chưa (len(done)==0 tức là đã tiêu thụ) —
// đây là bất biến then chốt chống panic send-on-closed-channel.
type fakeEngine struct {
	events chan host.Event
	stream chan string
	done   chan struct{}

	mu      sync.Mutex
	snap    host.UISnapshot
	aborted bool
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		events: make(chan host.Event, 4),
		stream: make(chan string),
		done:   make(chan struct{}, 1),
	}
}

func (f *fakeEngine) Events() <-chan host.Event { return f.events }
func (f *fakeEngine) Stream() <-chan string     { return f.stream }
func (f *fakeEngine) Done() <-chan struct{}     { return f.done }

func (f *fakeEngine) Snapshot() host.UISnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap
}

func (f *fakeEngine) Abort() bool {
	f.mu.Lock()
	f.aborted = true
	f.mu.Unlock()
	select { // mô phỏng waitDone: sau khi abort, gửi một lần vào done
	case f.done <- struct{}{}:
	default:
	}
	return true
}

func (f *fakeEngine) wasAborted() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.aborted
}

// Đường quá thời gian phải Abort rồi drain đến Done trước khi trả lỗi quá thời gian — nếu không,
// Close của RunCase sẽ tranh chấp với waitDone khi đóng kênh done và gây panic (Codex review #1).
func TestDriveTimeoutDrainsToDone(t *testing.T) {
	f := newFakeEngine()
	err := drive(f, 1, RunOptions{Timeout: 30 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "quá thời gian") {
		t.Fatalf("超时应返回超时错误，得到 %v", err)
	}
	if !f.wasAborted() {
		t.Fatal("超时应触发 Abort")
	}
	if len(f.done) != 0 {
		t.Fatal("drive 必须 drain Done 后才返回（否则与 Close 竞争关闭通道 panic）")
	}
}

// Đạt giới hạn số chương: Abort rồi drain đến Done, trả nil (cắt ngắn bình thường, không phải quá thời gian).
func TestDriveCapStopsAndDrains(t *testing.T) {
	f := newFakeEngine()
	f.mu.Lock()
	f.snap = host.UISnapshot{CompletedCount: 1}
	f.mu.Unlock()
	f.events <- host.Event{Category: "SYSTEM", Summary: "committed"} // kích hoạt kiểm tra cap

	err := drive(f, 1, RunOptions{Timeout: time.Second})
	if err != nil {
		t.Fatalf("正常截停应返回 nil，得到 %v", err)
	}
	if !f.wasAborted() {
		t.Fatal("达到章数上限应 Abort")
	}
	if len(f.done) != 0 {
		t.Fatal("应 drain Done 后返回")
	}
}

// Engine tự Done (viết xong): không cần Abort, trả nil.
func TestDriveNaturalDoneReturnsNil(t *testing.T) {
	f := newFakeEngine()
	f.done <- struct{}{}

	err := drive(f, 1, RunOptions{Timeout: time.Second})
	if err != nil {
		t.Fatalf("自然完成应返回 nil，得到 %v", err)
	}
	if f.wasAborted() {
		t.Fatal("自然完成不应 Abort")
	}
}
