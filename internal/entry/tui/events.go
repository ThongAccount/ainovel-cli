package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/voocel/ainovel-cli/internal/diag"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/store"
)

// Loại message
type (
	eventMsg       host.Event
	snapshotMsg    host.UISnapshot
	doneMsg        struct{ complete bool } // complete=true nghĩa là cả sách xong, false nghĩa là dừng vì lỗi
	abortResultMsg struct{ stopped bool }
	bootstrapMsg   struct {
		existing  bool // đã có tác phẩm; dù khôi phục thành công hay không vẫn phải vào bàn làm việc
		resumed   bool
		completed bool // thư mục chứa cuốn sách đã hoàn thành: vào bàn làm việc ở trạng thái hoàn tất thay vì trang chào
		err       error
	}
	reportLoadedMsg struct {
		reqID      int
		report     diag.Report
		exportPath string // đường dẫn tuyệt đối của tệp chẩn đoán đã khử định danh; rỗng = xuất thất bại
		exportErr  error
		finishedAt time.Time
	}
	startResultMsg   struct{ err error }
	cocreateDeltaMsg struct {
		reqID int
		kind  string // host.CoCreateProgressThinking | host.CoCreateProgressReply
		text  string
	}
	// cocreateStreamItem là tải trọng bên trong deltaCh, đưa cả kind của luồng và văn bản
	// tích luỹ đến TUI cùng lúc.
	cocreateStreamItem struct {
		kind string
		text string
	}
	cocreateDoneMsg struct {
		reqID int
		reply host.CoCreateReply
		err   error
	}
	steerResultMsg     struct{ err error }
	continueResultMsg  struct{ err error }
	spinnerTickMsg     time.Time
	toolSpinnerTickMsg time.Time // tick độc lập cho spinner công cụ trong luồng sự kiện (nhanh hơn, tách khỏi thanh trên/sao)
	streamDeltaMsg     string    // phần tăng dần của token trong luồng
	streamClearMsg     struct{}  // xoá bộ đệm luồng (bắt đầu message mới)
	streamFlushTickMsg struct{}  // nhịp giãn việc đẩy luồng (chỉ hẹn khi còn dữ liệu chờ đẩy)
	quitResetMsg       struct{}  // đặt lại sau khi nhấn Ctrl+C hai lần quá hạn
)

// --- Hàm Cmd ---

func listenEvents(rt *host.Host) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-rt.Events()
		if !ok {
			return nil
		}
		return eventMsg(ev)
	}
}

func listenDone(rt *host.Host) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-rt.Done()
		if !ok {
			return nil
		}
		snap := rt.Snapshot()
		return doneMsg{complete: snap.Phase == "complete"}
	}
}

func tickSnapshot(rt *host.Host) tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return snapshotMsg(rt.Snapshot())
	})
}

func fetchSnapshot(rt *host.Host) tea.Cmd {
	return func() tea.Msg {
		return snapshotMsg(rt.Snapshot())
	}
}

func bootstrapRuntime(rt *host.Host) tea.Cmd {
	return func() tea.Msg {
		snapshot := rt.Snapshot()
		msg := bootstrapMsg{
			existing:  snapshot.Phase != "" || snapshot.BookTitle != "",
			completed: snapshot.Phase == "complete",
		}
		label, err := rt.Resume()
		if err != nil {
			msg.err = err
			return msg
		}
		if label == "" {
			if msg.existing {
				return msg
			}
			return nil
		}
		msg.resumed = true
		return msg
	}
}

// resumeBook chạy lại một lần cổng khôi phục trong phiên (Resume của bootstrap chỉ chạy
// một lần lúc khởi động):
// sau khi nhập xong đóng panel, và sau /reopen, đều nhờ đây quay về bàn làm việc sáng tác.
// Không phát lại hàng đợi sự kiện — sự kiện của phiên này đã được listenEvents thường trực
// hiển thị, phát lại sẽ lặp lại. Các can thiệp còn treo (ví dụ hướng tiếp tục do /reopen
// đăng ký) được Resume xử lý qua Arbiter trước, rồi mới chạy tiếp engine.
func resumeBook(rt *host.Host) tea.Cmd {
	return func() tea.Msg {
		snapshot := rt.Snapshot()
		label, err := rt.Resume()
		return bootstrapMsg{
			existing: snapshot.Phase != "" || snapshot.BookTitle != "", completed: snapshot.Phase == "complete",
			resumed: label != "", err: err,
		}
	}
}

func startRuntime(rt *host.Host, prompt string) tea.Cmd {
	return func() tea.Msg {
		// Phía khởi động sinh tất định ảnh chụp quy tắc người dùng của cuốn sách này
		// (chuẩn hoá từ prompt gốc), phải làm trước StartPrepared.
		if err := rt.PrepareUserRules(prompt); err != nil {
			return startResultMsg{err: err}
		}
		err := rt.StartPrepared(prompt)
		return startResultMsg{err: err}
	}
}

func runCoCreate(rt *host.Host, state *cocreateState) tea.Cmd {
	history := state.session.History()
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	state.deltaCh = make(chan cocreateStreamItem, 64)
	state.doneCh = make(chan cocreateDoneMsg, 1)
	// Đồng sáng tác theo giai đoạn kèm tóm tắt trạng thái câu chuyện và tạo ra "brief định hướng
	// tiếp theo"; khởi động lạnh thì làm rõ yêu cầu từ đầu. Hai đường có chữ ký giống nhau.
	stream := rt.CoCreateStream
	if state.stage {
		stream = rt.StageCoCreateStream
	}
	start := func() tea.Msg {
		go func() {
			reply, err := stream(ctx, history, func(kind, text string) {
				select {
				case state.deltaCh <- cocreateStreamItem{kind: kind, text: text}:
				default:
				}
			})
			state.doneCh <- cocreateDoneMsg{reply: reply, err: err}
			close(state.deltaCh)
			close(state.doneCh)
		}()
		return nil
	}
	return tea.Batch(start, listenCoCreateDelta(state), listenCoCreateDone(state))
}

func listenCoCreateDelta(state *cocreateState) tea.Cmd {
	if state == nil || state.deltaCh == nil {
		return nil
	}
	// Giữ tham chiếu cục bộ của channel: tránh việc state.deltaCh bị gán lại khiến
	// closure listen cũ đọc nhầm channel mới (dù luồng hiện tại không kích hoạt, để ngỏ
	// cũng là một cái bẫy bảo trì không nên có).
	reqID := state.reqID
	ch := state.deltaCh
	return func() tea.Msg {
		item, ok := <-ch
		if !ok {
			return nil
		}
		return cocreateDeltaMsg{reqID: reqID, kind: item.kind, text: item.text}
	}
}

func listenCoCreateDone(state *cocreateState) tea.Cmd {
	if state == nil || state.doneCh == nil {
		return nil
	}
	reqID := state.reqID
	ch := state.doneCh
	return func() tea.Msg {
		result, ok := <-ch
		if !ok {
			return nil
		}
		result.reqID = reqID
		return result
	}
}

func steerRuntime(rt *host.Host, text string) tea.Cmd {
	return func() tea.Msg {
		return steerResultMsg{err: rt.Steer(text)}
	}
}

func continueRuntime(rt *host.Host, text string) tea.Cmd {
	return func() tea.Msg {
		err := rt.Continue(text)
		return continueResultMsg{err: err}
	}
}

// resumeFromCoCreate đưa brief định hướng tiếp theo do đồng sáng tác theo giai đoạn tạo ra
// vào và khôi phục việc sáng tác.
// Tái dùng continueResultMsg: thành công thì nối listenDone để chạy tiếp, thất bại thì
// hiển thị lỗi.
func resumeFromCoCreate(rt *host.Host, draft string) tea.Cmd {
	return func() tea.Msg {
		err := rt.ResumeFromCoCreate(draft)
		return continueResultMsg{err: err}
	}
}

// cancelCoCreate huỷ đồng sáng tác theo giai đoạn: xoá cờ đang chiếm và giữ trạng thái tạm dừng.
// Sự kiện chảy ngược về qua kênh events, không cần trả message.
func cancelCoCreate(rt *host.Host) tea.Cmd {
	return func() tea.Msg {
		rt.CancelCoCreate()
		return nil
	}
}

func abortRuntime(rt *host.Host) tea.Cmd {
	return func() tea.Msg {
		return abortResultMsg{stopped: rt.Abort()}
	}
}

func loadReport(dir string, reqID int) tea.Cmd {
	return func() tea.Msg {
		s := store.NewStore(dir)
		// Diagnose = chẩn đoán sáng tác + phát hiện lúc runtime, các Finding lúc runtime
		// cũng đưa vào báo cáo trên màn hình.
		rep, rc := diag.Diagnose(s)
		// Tái dùng rep+rc để ghi tệp chẩn đoán đã khử định danh (xuất thất bại không ảnh hưởng
		// báo cáo trên màn hình).
		exportPath, exportErr := diag.WriteExport(s, rep, rc)
		return reportLoadedMsg{
			reqID:      reqID,
			report:     rep,
			exportPath: exportPath,
			exportErr:  exportErr,
			finishedAt: time.Now(),
		}
	}
}

func tickSpinner() tea.Cmd {
	return tea.Tick(350*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

// tickToolSpinner điều khiển spinner của dòng "đang thực hiện" trong luồng sự kiện.
// Tách khỏi tickSpinner, nhịp nhanh hơn (150ms).
func tickToolSpinner() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return toolSpinnerTickMsg(t)
	})
}

// tickStreamFlush gộp các phần tăng dần trong luồng của một cửa sổ 16ms. Nó được khởi động
// bởi delta đầu tiên đang chờ đẩy, đẩy xong thì dừng, khi rảnh sẽ không liên tục đánh thức TUI.
func tickStreamFlush() tea.Cmd {
	return tea.Tick(16*time.Millisecond, func(t time.Time) tea.Msg {
		return streamFlushTickMsg{}
	})
}

func listenStream(rt *host.Host) tea.Cmd {
	return func() tea.Msg {
		delta, ok := <-rt.Stream()
		if !ok {
			return nil
		}
		// sentinel được phát thành streamClearMsg, bảo đảm nó đến TUI trên cùng kênh với
		// delta bình thường và đúng thứ tự emit. Khi có hai kênh, clearCh và streamCh không
		// có thứ tự với nhau, ✻ header thường bị nhét nhầm vào cuối đoạn thinking trước.
		if delta == host.StreamClearSentinel {
			return streamClearMsg{}
		}
		return streamDeltaMsg(delta)
	}
}
