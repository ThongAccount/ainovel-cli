package tui

import (
	"errors"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
)

func TestBootstrapExistingBookFailureStaysInWorkbench(t *testing.T) {
	m := Model{mode: modeNew, textarea: textarea.New()}
	next, cmd, handled := m.handleRuntimeMsg(bootstrapMsg{existing: true, err: errors.New("di chuyển thất bại")})
	if !handled || cmd == nil {
		t.Fatal("khôi phục tác phẩm đã có thất bại vẫn phải làm mới bàn làm việc")
	}
	got := next.(Model)
	if got.mode != modeRunning {
		t.Fatalf("khôi phục tác phẩm đã có thất bại thì vẫn ở bàn làm việc, được mode=%v", got.mode)
	}
	if got.err == nil || got.err.Error() != "di chuyển thất bại" {
		t.Fatalf("bàn làm việc phải hiện lỗi gốc, được %v", got.err)
	}
}

// TestBootstrapCompletedBookLandsOnDoneWorkbench bảo vệ điểm hạ cánh lúc khởi động với
// sách đã hoàn thành: resumeLabel trả về nhãn rỗng cho
// complete, hành vi cũ rơi về màn chào — màn chào không nói gì về sách đã có, người dùng sẽ
// tưởng sách bị mất, và vị trí tự nhiên của /reopen, /export, thao tác làm lại đều là
// bàn làm việc trạng thái hoàn tất.
func TestBootstrapCompletedBookLandsOnDoneWorkbench(t *testing.T) {
	m := Model{mode: modeNew, textarea: textarea.New()}
	next, cmd, handled := m.handleRuntimeMsg(bootstrapMsg{completed: true})
	if !handled || cmd == nil {
		t.Fatal("bootstrap completed phải được xử lý và trả về lệnh")
	}
	got := next.(Model)
	if got.mode != modeDone {
		t.Fatalf("sách đã hoàn thành phải rơi vào bàn làm việc hoàn tất, được mode=%v", got.mode)
	}
	if got.textarea.Placeholder != donePlaceholder {
		t.Fatalf("phải đưa ra lời dẫn trạng thái hoàn tất (kèm /reopen), được %q", got.textarea.Placeholder)
	}

	// Đã ở bàn làm việc (ví dụ phiên vừa hoàn thành lại nhận bootstrap) không được bị đổi trạng thái lần nữa.
	m = Model{mode: modeRunning, textarea: textarea.New()}
	next, _, _ = m.handleRuntimeMsg(bootstrapMsg{completed: true})
	if next.(Model).mode != modeRunning {
		t.Fatal("trang không phải màn chào không nên bị bootstrap completed đổi trạng thái")
	}
}
