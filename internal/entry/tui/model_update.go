package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/voocel/ainovel-cli/internal/entry/startup"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/host/imp"
	"github.com/voocel/ainovel-cli/internal/utils"
)

const maxPromptEventCols = 160

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// body phụ thuộc chiều cao thực tế của thanh trên/dưới (thanh chế độ ở trang mới, ô nhập
	// nhiều dòng đều làm thay đổi nó), nên đồng bộ một lần trước mỗi message để viewport
	// không kẹt ở chiều cao cũ và không bị lấp dòng trống ở đáy panel. Thao tác này idempotent
	// và rẻ.
	if m.width > 0 {
		m.updateViewportSize()
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeTextarea()
		m.updateViewportSize()
		m.refreshDetailViewport()
		m.refreshStateViewport()
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case tea.MouseMsg:
		return m.handleMouseMsg(msg)
	default:
		if next, cmd, handled := m.handleRuntimeMsg(msg); handled {
			return next, cmd
		}
		return m.handleTextareaMsg(msg)
	}
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.handleOverlayKeyMsg(msg); handled {
		return next, cmd
	}

	if msg.Type == tea.KeyCtrlC {
		if m.quitPending {
			return m, tea.Quit
		}
		m.quitPending = true
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return quitResetMsg{} })
	}
	m.quitPending = false

	if next, cmd, handled := m.handleCommandPaletteKey(msg); handled {
		return next, cmd
	}

	return m.handleBaseKeyMsg(msg)
}

func (m Model) handleOverlayKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case m.cocreate != nil:
		return m.handleBlockingModalKey(msg, m.handleCoCreateKey)
	case m.modelConfig != nil:
		return m.handleBlockingModalKey(msg, m.handleModelConfigKey)
	case m.help != nil:
		return m.handleBlockingModalKey(msg, m.handleHelpKey)
	case m.modelSwitch != nil:
		return m.handleBlockingModalKey(msg, m.handleModelSwitchKey)
	case m.report != nil:
		return m.handleBlockingModalKey(msg, m.handleReportKey)
	case m.importer != nil:
		return m.handleBlockingModalKey(msg, m.handleImportKey)
	case m.simulator != nil:
		return m.handleBlockingModalKey(msg, m.handleSimulationKey)
	default:
		return m, nil, false
	}
}

func (m Model) handleBlockingModalKey(msg tea.KeyMsg, next func(tea.KeyMsg) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd, bool) {
	if msg.Type == tea.KeyCtrlC {
		if m.quitPending {
			return m, tea.Quit, true
		}
		m.quitPending = true
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return quitResetMsg{} }), true
	}
	m.quitPending = false
	// Phím tắt toàn cục xuyên modal: phải bật/tắt được báo cáo chuột ngay cả khi modal đang mở,
	// nếu không người dùng không thể kéo chọn và sao chép native trong các modal kiểu chặn màn
	// hình như đồng sáng tác / help / report.
	if msg.Type == tea.KeyCtrlR {
		next, cmd := m.toggleMouseReporting()
		return next, cmd, true
	}
	model, cmd := next(msg)
	return model, cmd, true
}

// toggleMouseReporting đảo công tắc báo cáo chuột. Bật → tắt để người dùng kéo chọn và
// sao chép native; tắt → bật để khôi phục bấm đổi tiêu điểm / lăn chuột. Đường base và đường
// modal chặn cùng dùng chung hàm này.
func (m Model) toggleMouseReporting() (Model, tea.Cmd) {
	// Màn chào (modeNew) vốn không bật báo cáo chuột, kéo native là copy được; ở đây bỏ qua
	// Ctrl+R để tránh bật nhầm báo cáo rồi lại phá mất thao tác copy native. Báo cáo chuột do
	// enterRunning bật khi vào bàn làm việc.
	if m.mode == modeNew {
		return m, nil
	}
	m.mouseOff = !m.mouseOff
	if m.mouseOff {
		return m, tea.DisableMouse
	}
	return m, tea.EnableMouseCellMotion
}

// donePlaceholder hoàn thành tác phẩm
const donePlaceholder = "Truyện đã hoàn thành · Có thể nhập yêu cầu sửa đổi (vd: \"viết lại chương 3\"), /reopen để viết tiếp tập mới, hoặc /export để xuất truyện"

// enterRunning đưa vào bàn làm việc sáng tác: bật báo cáo chuột (bàn làm việc cần bấm đổi
// panel / lăn chuột / kéo thanh bên). Lệnh trả về phải được lời gọi Batch vào giá trị cuối.
func (m *Model) enterRunning() tea.Cmd {
	m.mode = modeRunning
	m.mouseOff = false
	return tea.EnableMouseCellMotion
}

func (m Model) handleCommandPaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !m.compActive {
		return m, nil, false
	}

	switch msg.Type {
	case tea.KeyEsc:
		m.clearCommandPalette()
		return m, nil, true
	case tea.KeyUp:
		if m.compIdx > 0 {
			m.compIdx--
		}
		return m, nil, true
	case tea.KeyDown:
		if m.compIdx < len(m.compItems)-1 {
			m.compIdx++
		}
		return m, nil, true
	case tea.KeyTab:
		m.acceptCommandCompletion()
		return m, nil, true
	case tea.KeyEnter:
		item, ok := m.acceptCommandCompletion()
		if !ok {
			return m, nil, true
		}
		if item.AutoExecute {
			m.textarea.Reset()
			next, cmd := m.handleSlashCommand(slashCommand{name: item.Name})
			return next, cmd, true
		}
		return m, nil, true
	default:
		return m, nil, false
	}
}

func (m Model) handleBaseKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Phòng thủ throttle: dán \n trên terminal không hỗ trợ bracketed paste sẽ thoái hoá
	// thành chuỗi KeyEnter liên tiếp; người thật bấm Enter thường cách phím ký tự trước > 100ms,
	// <50ms gần như chắc chắn là mảnh dòng dán.
	// Chỉ ghi KeyRunes (luồng ký tự) — phím chức năng (↑↓/Tab/Ctrl-x) không nên làm nhiễu
	// throttle, nếu không người dùng duyệt lịch sử rồi bấm Enter ngay sẽ bị nuốt nhầm.
	if msg.Type == tea.KeyRunes {
		m.lastKeyAt = time.Now()
	}
	switch msg.Type {
	case tea.KeyEscape:
		if m.mode == modeRunning && m.snapshot.IsRunning {
			return m, abortRuntime(m.runtime)
		}
		m.textarea.Reset()
		m.historyIdx = len(m.inputHistory)
		m.historyDraft = ""
		m.refitTextareaHeight()
		m.clearCommandPalette()
		return m, nil
	case tea.KeyCtrlL:
		m.resetOutputPanels()
		return m, nil
	case tea.KeyCtrlU:
		// Xoá nội dung đang nhập; đồng thời thoát khỏi trạng thái duyệt lịch sử.
		m.textarea.Reset()
		m.historyIdx = len(m.inputHistory)
		m.historyDraft = ""
		m.refitTextareaHeight()
		m.clearCommandPalette()
		return m, nil
	case tea.KeyCtrlR:
		return m.toggleMouseReporting()
	case tea.KeyTab:
		if m.mode == modeNew {
			if m.cocreate != nil {
				return m, nil
			}
			if m.startupMode == startupModeQuick {
				m.startupMode = startupModeCoCreate
			} else {
				m.startupMode = startupModeQuick
			}
			m.textarea.Placeholder = placeholderForNewMode(m.startupMode)
			return m, nil
		}
		m.focusPane = (m.focusPane + 1) % focusPaneCount
		return m, nil
	case tea.KeyEnter:
		// Alt+Enter là xuống dòng chủ động, để textarea.Update tiếp quản (KeyMap.InsertNewline đã gán phím này).
		if msg.Alt {
			break
		}
		// Khoảng cách từ phím khác Enter gần nhất quá ngắn → coi là mảnh \n của dòng dán:
		// thay bằng dấu cách để giữ khoảng cách nhìn thấy, đúng ngữ nghĩa của đường cleanHumanKeyRunes
		// ("abc\ndef" → "abc def").
		// Phòng thủ cho môi trường terminal mà bracketed paste không hoạt động (SSH cũ/một số cấu hình tmux).
		if !m.lastKeyAt.IsZero() && time.Since(m.lastKeyAt) < 50*time.Millisecond {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
			m.refitTextareaHeight()
			return m, cmd
		}
		return m.handleEnterKey()
	case tea.KeyUp:
		// Nhập nhiều dòng: để textarea tự xử lý việc di chuyển con trỏ trong dòng (rơi vào textarea.Update sau switch)
		if m.textareaIsMultiline() {
			break
		}
		// Nhập một dòng: ưu tiên duyệt lịch sử, không có lịch sử dùng thì lùi về cuộn luồng sự kiện
		if m.tryHistoryUp() {
			return m, nil
		}
		return m.handleVerticalScrollKey(msg, true)
	case tea.KeyDown:
		if m.textareaIsMultiline() {
			break
		}
		if m.tryHistoryDown() {
			return m, nil
		}
		return m.handleVerticalScrollKey(msg, false)
	case tea.KeyPgUp:
		return m.handleVerticalScrollKey(msg, true)
	case tea.KeyPgDown:
		return m.handleVerticalScrollKey(msg, false)
	case tea.KeyEnd:
		switch m.focusPane {
		case focusStream:
			m.streamScroll = true
			m.streamVP.GotoBottom()
		case focusDetail:
			m.detailVP.GotoBottom()
		case focusState:
			m.stateVP.GotoBottom()
		default:
			m.autoScroll = true
			m.viewport.GotoBottom()
		}
		return m, nil
	}

	if msg.Type == tea.KeyRunes && (containsSGRFragment(string(msg.Runes)) || isCSILeak(msg.Runes)) {
		return m, nil
	}
	var ok bool
	if msg, ok = cleanHumanKeyRunes(msg); !ok {
		return m, nil
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.refitTextareaHeight()
	m.updateCommandPalette()
	return m, cmd
}

func (m Model) handleEnterKey() (tea.Model, tea.Cmd) {
	text := utils.CleanInputLine(m.textarea.Value())
	if text == "" {
		return m, nil
	}
	m.clearCommandPalette()
	if cmd, ok := parseSlashCommand(text); ok {
		m.pushInputHistory(text)
		m.textarea.Reset()
		m.refitTextareaHeight()
		return m.handleSlashCommand(cmd)
	}

	m.pushInputHistory(text)
	m.textarea.Reset()
	m.refitTextareaHeight()
	switch m.mode {
	case modeNew:
		m.err = nil
		if m.startupMode == startupModeQuick {
			prompt, err := startup.PrepareQuick(text)
			if err != nil {
				m.err = err
				return m, nil
			}
			cmd := m.enterStarting(prompt)
			return m, tea.Batch(startRuntime(m.runtime, prompt), cmd)
		}
		m.cocreate = newCoCreateState(text)
		return m, m.sendCoCreate()
	case modeRunning:
		// Không hiện lại cục bộ sự kiện USER — Host.Continue/Steer đã emit sự kiện "USER" ở đầu
		// vào, nó chảy ngược về TUI qua kênh events. Kiến trúc §2.3: tầng quan sát chỉ quan sát,
		// không sinh ra sự thật.
		if !m.snapshot.IsRunning {
			return m, continueRuntime(m.runtime, text)
		}
		return m, steerRuntime(m.runtime, text)
	case modeDone:
		// Người dùng nhập sau khi đã hoàn thành (yêu cầu làm lại / tiếp tục): đánh thức một vòng run
		// mới. Continue ở trạng thái dừng sẽ đi qua Inject để tự phục hồi, Arbiter phân xử can thiệp
		// của người dùng; khi làm lại mà đã có chương được viết thì Engine mở lại toàn bộ sách và
		// đưa vào hàng đợi.
		// Chuyển lại modeRunning để vào bàn làm việc; khi vòng này chạy xong
		// doneMsg(complete) sẽ đặt lại modeDone. Lệnh bắt đầu bằng dấu gạch chéo đã được xử lý
		// phía trên, không đi qua nhánh này.
		m.mode = modeRunning
		return m, continueRuntime(m.runtime, text)
	default:
		return m, nil
	}
}

func (m Model) handleVerticalScrollKey(msg tea.KeyMsg, upward bool) (tea.Model, tea.Cmd) {
	if m.focusPane == focusStream {
		if upward {
			m.streamScroll = false
		}
		var cmd tea.Cmd
		m.streamVP, cmd = m.streamVP.Update(msg)
		if !upward && m.streamVP.AtBottom() {
			m.streamScroll = true
		}
		return m, cmd
	}
	if m.focusPane == focusDetail {
		var cmd tea.Cmd
		m.detailVP, cmd = m.detailVP.Update(msg)
		return m, cmd
	}
	if m.focusPane == focusState {
		var cmd tea.Cmd
		m.stateVP, cmd = m.stateVP.Update(msg)
		return m, cmd
	}
	if upward {
		m.autoScroll = false
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	if !upward && m.viewport.AtBottom() {
		m.autoScroll = true
	}
	return m, cmd
}

func (m Model) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.cocreate != nil {
		// Phân tuyến chuột theo toạ độ X: nửa trái màn hình = panel conv, nửa phải = panel prompt.
		// Modal nằm giữa và conv chiếm ~58% bên trái, nên phân định bằng đường giữa màn hình đã đủ
		// chính xác.
		// Người dùng lăn chuột trong vùng conv sẽ tự dừng follow (để họ dừng ổn định ở một vị trí
		// lịch sử).
		var cmd tea.Cmd
		if msg.X < m.width/2 {
			m.cocreate.convFollow = false
			m.cocreate.convVP, cmd = m.cocreate.convVP.Update(msg)
			if m.cocreate.convVP.AtBottom() {
				m.cocreate.convFollow = true
			}
		} else {
			m.cocreate.promptVP, cmd = m.cocreate.promptVP.Update(msg)
		}
		return m, cmd
	}
	if m.modelSwitch != nil || m.modelConfig != nil {
		return m, nil
	}
	if pane, ok := m.paneAtMouse(msg.X, msg.Y); ok {
		m.hoverPane = pane
		m.hoverActive = true
		if msg.Action == tea.MouseActionPress {
			m.focusPane = pane
		}
	} else {
		m.hoverActive = false
	}

	var cmd tea.Cmd
	if m.focusPane == focusStream {
		m.streamVP, cmd = m.streamVP.Update(msg)
		if msg.Action == tea.MouseActionPress {
			m.streamScroll = m.streamVP.AtBottom()
		}
		return m, cmd
	}
	if m.focusPane == focusDetail {
		m.detailVP, cmd = m.detailVP.Update(msg)
		return m, cmd
	}
	if m.focusPane == focusState {
		m.stateVP, cmd = m.stateVP.Update(msg)
		return m, cmd
	}
	m.viewport, cmd = m.viewport.Update(msg)
	if msg.Action == tea.MouseActionPress {
		m.autoScroll = m.viewport.AtBottom()
	}
	return m, cmd
}

func (m Model) handleRuntimeMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case eventMsg:
		hadRunningEvent := m.hasRunningEvent()
		ev := host.Event(msg)
		m.applyEventProjection(ev)
		m.refreshEventViewport()
		cmd := listenEvents(m.runtime)
		if !hadRunningEvent && m.hasRunningEvent() && !m.toolTicking {
			m.toolTicking = true
			cmd = tea.Batch(cmd, tickToolSpinner())
		}
		return m, cmd, true
	case bootstrapMsg:
		// Đã có tác phẩm hay không quyết định giao diện dừng ở đâu, khôi phục thành công chỉ quyết
		// định engine có chạy hay không. Khi nâng cấp dữ liệu, ngân sách hoặc cổng chặn sửa đổi
		// thất bại thì vẫn ở lại bàn làm việc để hiện cuốn sách gốc, không được lùi về màn chào.
		if (msg.existing || msg.resumed) && m.mode == modeNew && !msg.completed {
			enableMouse := m.enterRunning()
			m.resizeTextarea()
			m.textarea.Placeholder = defaultSteerPlaceholder()
			if msg.err != nil {
				m.err = msg.err
			}
			return m, tea.Batch(fetchSnapshot(m.runtime), enableMouse), true
		}
		// Sách đã hoàn thành: dừng ở bàn làm việc trạng thái hoàn tất (enterRunning bật chuột rồi
		// đổi sang modeDone), không dừng ở màn chào —
		// màn chào không nói gì về sách đã có, người dùng sẽ tưởng sách bị mất; /reopen, /export và
		// thao tác làm lại đều nằm ở bàn làm việc.
		if msg.completed && m.mode == modeNew {
			enableMouse := m.enterRunning()
			m.mode = modeDone
			m.resizeTextarea()
			m.textarea.Placeholder = donePlaceholder
			if msg.err != nil {
				m.err = msg.err
			}
			return m, tea.Batch(fetchSnapshot(m.runtime), enableMouse, m.textarea.Focus()), true
		}
		// /reopen và các lệnh khôi phục trong phiên đưa từ trạng thái hoàn tất trở lại bàn sáng tác.
		if msg.resumed && m.mode == modeDone {
			enableMouse := m.enterRunning()
			m.resizeTextarea()
			m.textarea.Placeholder = defaultSteerPlaceholder()
			return m, tea.Batch(fetchSnapshot(m.runtime), enableMouse), true
		}
		if msg.err != nil {
			m.err = msg.err
		}
		return m, fetchSnapshot(m.runtime), true
	case snapshotMsg:
		next := host.UISnapshot(msg)
		detailChanged := !sameDetailSnapshot(m.snapshot, next)
		runningChanged := m.snapshot.IsRunning != next.IsRunning
		m.snapshot = next
		m.syncRuntimePlaceholder()
		m.refreshEventViewport()
		if runningChanged {
			m.refreshStreamViewport()
		}
		if detailChanged {
			m.refreshDetailViewport()
		}
		m.refreshStateViewport()
		return m, tickSnapshot(m.runtime), true
	case doneMsg:
		m.snapshot.IsRunning = false
		m.refreshEventViewport()
		m.refreshStreamViewport()
		m.refreshStateViewport()
		if msg.complete {
			m.abortPending = false
			m.mode = modeDone
			// Trạng thái hoàn tất không khóa ô nhập: dừng tự động viết tiếp, nhưng người dùng vẫn
			// nhập được yêu cầu làm lại (input ở modeDone đi qua
			// Continue để đánh thức một vòng run mới, Arbiter phân xử làm lại hay tiếp tục sáng tác;
			// /export, /model và các lệnh khác cũng phải dùng được, ô nhập buộc phải giữ tiêu điểm
			// (issue #27, #38).
			m.textarea.Placeholder = donePlaceholder
			return m, tea.Batch(fetchSnapshot(m.runtime), listenDone(m.runtime), m.textarea.Focus()), true
		}
		if m.abortPending {
			m.abortPending = false
			m.snapshot.RuntimeState = "paused"
			m.syncRuntimePlaceholder()
		} else {
			m.textarea.Placeholder = "Sáng tác bị gián đoạn, nhập nội dung bất kỳ để tiếp tục"
		}
		return m, tea.Batch(fetchSnapshot(m.runtime), listenDone(m.runtime)), true
	case abortResultMsg:
		if msg.stopped {
			m.abortPending = true
			m.textarea.Placeholder = "Đang tạm dừng sáng tác..."
		}
		return m, nil, true
	case reportLoadedMsg:
		if m.report == nil || msg.reqID != m.report.reqID {
			return m, nil, true
		}
		boxW, _ := reportModalSize(m.width, m.height)
		m.report.load(msg.report, paddedModalContentWidth(boxW), msg.exportPath, msg.exportErr, msg.finishedAt)
		return m, nil, true
	case importEventMsg:
		if m.importer == nil || msg.reqID != m.importer.reqID {
			return m, nil, true
		}
		boxW, _ := reportModalSize(m.width, m.height)
		m.importer.appendEvent(msg.ev, paddedModalContentWidth(boxW))
		if msg.ev.Stage == imp.StageError {
			return m, nil, true
		}
		if msg.ev.Stage == imp.StageDone {
			if msg.ev.Continued {
				m.importer = nil
				enableMouse := m.enterRunning()
				m.resizeTextarea()
				m.textarea.Placeholder = defaultSteerPlaceholder()
				return m, tea.Batch(enableMouse, m.textarea.Focus()), true
			}
			return m, nil, true
		}
		return m, listenImportEvent(msg.reqID, msg.ch), true
	case importClosedMsg:
		if m.importer == nil || msg.reqID != m.importer.reqID || m.importer.done {
			return m, nil, true
		}
		m.importer.paused = true
		boxW, _ := reportModalSize(m.width, m.height)
		m.importer.refresh(paddedModalContentWidth(boxW))
		return m, nil, true
	case simEventMsg:
		if m.simulator == nil || msg.reqID != m.simulator.reqID {
			return m, nil, true
		}
		boxW, _ := reportModalSize(m.width, m.height)
		m.simulator.appendEvent(msg.ev, paddedModalContentWidth(boxW))
		if msg.terminal() {
			return m, nil, true
		}
		return m, listenSimulationEvent(msg.reqID, msg.ch), true
	case exportDoneMsg:
		if msg.err != nil {
			m.applyEvent(host.Event{
				Time: time.Now(), Category: "ERROR", Summary: "Xuất file thất bại: " + msg.err.Error(), Level: "error",
			})
		} else if msg.result != nil {
			m.applyEvent(host.Event{
				Time: time.Now(), Category: "SYSTEM", Summary: formatExportSuccess(msg.result), Level: "success",
			})
		}
		m.refreshEventViewport()
		return m, nil, true
	case revisionDoneMsg:
		if msg.err != nil {
			m.applyEvent(host.Event{Time: time.Now(), Category: "ERROR", Summary: "Đồng bộ chương thất bại: " + msg.err.Error(), Level: "error"})
		} else if msg.checkOnly {
			summary := "Không phát hiện chương nào bị chỉnh sửa từ bên ngoài"
			if len(msg.chapters) > 0 {
				summary = fmt.Sprintf("Phát hiện chính văn chương đã bị chỉnh sửa từ bên ngoài: %v; Chạy /sync để tiếp nhận", msg.chapters)
			}
			m.applyEvent(host.Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: "info"})
		} else {
			m.applyEvent(host.Event{Time: time.Now(), Category: "SYSTEM", Summary: formatRevisionResult(msg.result), Level: "success"})
		}
		m.refreshEventViewport()
		return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus()), true
	case modelConfigSavedMsg:
		if m.modelConfig == nil {
			return m, nil, true
		}
		if msg.err != nil {
			m.modelConfig.saving = false
			m.modelConfig.message = msg.err.Error()
			return m, nil, true
		}
		m.modelConfig = nil
		return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus()), true
	case modelConfigConnectionMsg:
		if m.modelConfig == nil {
			return m, nil, true
		}
		m.modelConfig.testing = false
		m.modelConfig.testCancel = nil
		if errors.Is(msg.err, context.Canceled) {
			m.modelConfig.message = "Kiểm tra kết nối đã bị hủy"
		} else if msg.err != nil {
			m.modelConfig.message = msg.err.Error()
		} else {
			m.modelConfig.message = "Kiểm tra kết nối thành công: " + msg.model
		}
		return m, nil, true
	case startResultMsg:
		next, cmd := m.handleStartResultMsg(msg)
		return next, cmd, true
	case cocreateDeltaMsg:
		if m.cocreate == nil || msg.reqID != m.cocreate.reqID {
			return m, nil, true
		}
		m.cocreate.applyDelta(msg.kind, msg.text)
		return m, listenCoCreateDelta(m.cocreate), true
	case cocreateDoneMsg:
		next, cmd := m.handleCoCreateDoneMsg(msg)
		return next, cmd, true
	case steerResultMsg:
		if msg.err != nil {
			m.err = msg.err
			m.applyEvent(host.Event{Time: time.Now(), Category: "ERROR", Summary: msg.err.Error(), Level: "error"})
			m.refreshEventViewport()
			return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus()), true
		}
		return m, tea.Batch(fetchSnapshot(m.runtime), listenDone(m.runtime)), true
	case continueResultMsg:
		if msg.err != nil {
			m.err = msg.err
			m.applyEvent(host.Event{
				Time: time.Now(), Category: "ERROR", Summary: msg.err.Error(), Level: "error",
			})
			m.refreshEventViewport()
			return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus()), true
		}
		m.err = nil
		m.textarea.Placeholder = defaultSteerPlaceholder()
		return m, tea.Batch(fetchSnapshot(m.runtime), listenDone(m.runtime), m.textarea.Focus()), true
	case spinnerTickMsg:
		m.spinnerIdx = (m.spinnerIdx + 1) % len(spinnerFrames)
		m.cursorIdx++
		if m.snapshot.IsRunning {
			// Thanh trên, gợi ý hoạt ảnh và con trỏ luồng dùng chung hoạt ảnh tần số thấp, tránh nhiều timer thường trú cùng kích hoạt lại toàn màn hình View.
			m.refreshEventViewport()
			m.refreshStreamViewport()
			m.streamDirty = false
		}
		if s := m.importer; s != nil && !s.done && !s.paused {
			s.frame = m.cursorIdx
			boxW, _ := reportModalSize(m.width, m.height)
			s.refresh(paddedModalContentWidth(boxW))
		}
		return m, tickSpinner(), true
	case toolSpinnerTickMsg:
		m.toolSpinnerIdx = (m.toolSpinnerIdx + 1) % len(toolSpinnerFrames)
		if m.hasRunningEvent() {
			m.refreshEventViewport()
			return m, tickToolSpinner(), true
		}
		m.toolTicking = false
		return m, nil, true
	case streamDeltaMsg:
		if len(m.streamRounds) == 0 {
			m.streamRounds = append(m.streamRounds, "")
		}
		m.streamRounds[len(m.streamRounds)-1] += string(msg)
		// Không đẩy ngay; delta đầu tiên mở một cửa sổ gộp 16ms, các delta sau dùng lại timer đó.
		m.streamDirty = true
		cmd := listenStream(m.runtime)
		if !m.flushPending {
			m.flushPending = true
			cmd = tea.Batch(cmd, tickStreamFlush())
		}
		return m, cmd, true
	case streamClearMsg:
		// Ranh giới round: phải đẩy phần delta tích luỹ trước, round mới mới căn được về mặt thị giác
		if m.flushStreamIfDirty() && m.streamScroll {
			m.streamVP.GotoBottom()
		}
		if len(m.streamRounds) == 0 {
			m.streamRounds = append(m.streamRounds, "")
		} else if strings.TrimSpace(m.streamRounds[len(m.streamRounds)-1]) != "" {
			m.streamRounds = append(m.streamRounds, "")
		}
		m.trimStreamRounds()
		m.streamRound = len(m.streamRounds)
		m.refreshStreamViewport()
		if m.streamScroll {
			m.streamVP.GotoBottom()
		}
		return m, listenStream(m.runtime), true
	case streamFlushTickMsg:
		m.flushPending = false
		if m.flushStreamIfDirty() && m.streamScroll {
			m.streamVP.GotoBottom()
		}
		return m, nil, true
	case quitResetMsg:
		m.quitPending = false
		return m, nil, true
	default:
		return m, nil, false
	}
}

func sameDetailSnapshot(a, b host.UISnapshot) bool {
	return a.Synopsis == b.Synopsis &&
		a.Premise == b.Premise &&
		a.Layered == b.Layered &&
		a.CurrentVolumeArc == b.CurrentVolumeArc &&
		a.NextVolumeTitle == b.NextVolumeTitle &&
		a.CompassDirection == b.CompassDirection &&
		a.CompassScale == b.CompassScale &&
		a.SupportingCount == b.SupportingCount &&
		a.CompletedCount == b.CompletedCount &&
		a.InProgressChapter == b.InProgressChapter &&
		a.LastCommitSummary == b.LastCommitSummary &&
		a.LastReviewSummary == b.LastReviewSummary &&
		slices.Equal(a.Outline, b.Outline) &&
		slices.Equal(a.Characters, b.Characters) &&
		slices.Equal(a.RecentSupporting, b.RecentSupporting) &&
		slices.Equal(a.RecentSummaries, b.RecentSummaries)
}

func (m Model) handleStartResultMsg(msg startResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		wasStarting := m.starting
		m.starting = false
		if m.mode != modeNew {
			m.applyEvent(host.Event{
				Time: time.Now(), Category: "ERROR", Summary: msg.err.Error(), Level: "error",
			})
			m.refreshEventViewport()
		}
		if m.cocreate != nil {
			m.cocreate.awaiting = false
			m.textarea.Placeholder = placeholderForCoCreate(m.cocreate)
			return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus())
		}
		if wasStarting {
			// Sau khi bấm Enter đã vào bàn làm việc; lỗi LLM ở giai đoạn khởi động hiển thị ngay tại
			// bàn làm việc hiện tại, không lùi về màn chào nữa.
			m.mode = modeRunning
			m.snapshot.IsRunning = false
			m.snapshot.RuntimeState = "idle"
			m.textarea.Placeholder = "Khởi tạo thất bại, vui lòng kiểm tra cấu hình hoặc dùng /model để đổi model"
			m.refreshStreamViewport()
			m.refreshStateViewport()
			return m, m.textarea.Focus()
		}
		if m.mode == modeNew {
			m.textarea.Placeholder = placeholderForNewMode(m.startupMode)
			return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus())
		}
		return m, fetchSnapshot(m.runtime)
	}
	m.starting = false

	if m.mode == modeNew {
		m.cocreate = nil
		enableMouse := m.enterRunning()
		m.resizeTextarea()
		m.textarea.Placeholder = defaultSteerPlaceholder()
		return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus(), enableMouse)
	}

	return m, fetchSnapshot(m.runtime)
}

func (m *Model) enterStarting(rawPrompt string) tea.Cmd {
	m.cocreate = nil
	m.err = nil
	m.starting = true
	m.snapshot.IsRunning = true
	m.snapshot.RuntimeState = "running"
	enableMouse := m.enterRunning()
	m.resetOutputPanels()
	m.resizeTextarea()
	m.textarea.Placeholder = "Đang khởi tạo sáng tác..."
	m.applyStartupPromptEvent(rawPrompt)
	m.applyEvent(host.Event{
		Time: time.Now(), Category: "SYSTEM", Summary: "Đang khởi tạo sáng tác", Level: "info",
	})
	m.refreshEventViewport()
	m.refreshStreamViewport()
	m.refreshStateViewport()
	return tea.Batch(m.textarea.Focus(), enableMouse)
}

func (m *Model) applyStartupPromptEvent(rawPrompt string) {
	text := utils.CleanInputLine(rawPrompt)
	if text == "" {
		return
	}
	m.applyEvent(host.Event{
		Time:     time.Now(),
		Category: "USER",
		Summary:  "Yêu cầu sáng tác: " + truncate(text, maxPromptEventCols),
		Detail:   text,
		Level:    "info",
	})
}

func (m Model) handleCoCreateDoneMsg(msg cocreateDoneMsg) (tea.Model, tea.Cmd) {
	if m.cocreate == nil || msg.reqID != m.cocreate.reqID {
		return m, nil
	}
	if msg.err != nil {
		m.err = msg.err
		m.cocreate.awaiting = false
		m.textarea.Placeholder = placeholderForCoCreate(m.cocreate)
		return m, m.textarea.Focus()
	}
	m.err = nil
	m.cocreate.apply(msg.reply)
	m.textarea.Placeholder = placeholderForCoCreate(m.cocreate)
	return m, m.textarea.Focus()
}

func (m Model) handleTextareaMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.refitTextareaHeight()
	m.updateCommandPalette()
	return m, cmd
}

// applyEvent ghi lại một sự kiện do TUI tự sinh và cập nhật phần chiếu. Sự kiện của Host đã
// được ghi log ngay nơi sinh ra, nên đường đăng ký sự kiện gọi thẳng applyEventProjection để
// tránh ghi trùng.
func (m *Model) applyEvent(ev host.Event) {
	host.LogEvent(ev)
	m.applyEventProjection(ev)
}

// applyEventProjection áp dụng một sự kiện vào m.events:
// - có ID và đã tồn tại → cập nhật tại chỗ (gộp các trường trạng thái hoàn tất, giữ Time / Summary đầu tiên)
// - sự kiện mới → thêm vào cuối, ghi eventIndex nếu cần
// - vượt quá maxEvents thì cắt trượt và dựng lại chỉ mục
func (m *Model) applyEventProjection(ev host.Event) {
	if ev.ID != "" {
		if idx, ok := m.eventIndex[ev.ID]; ok && idx >= 0 && idx < len(m.events) {
			existing := &m.events[idx]
			if !ev.FinishedAt.IsZero() {
				existing.FinishedAt = ev.FinishedAt
			}
			if ev.Duration > 0 {
				existing.Duration = ev.Duration
			}
			if ev.Failed {
				existing.Failed = true
			}
			if ev.Level != "" {
				existing.Level = ev.Level
			}
			if ev.Detail != "" {
				existing.Detail = ev.Detail
			}
			if ev.Kind != "" {
				existing.Kind = ev.Kind
			}
			// Khi Summary khác rỗng thì cho phép ghi đè (trạng thái kết thúc có thể mang thông tin bổ sung); nếu rỗng thì giữ bản đầu
			if ev.Summary != "" {
				existing.Summary = ev.Summary
			}
			// Sự kiện retry có cùng ID cập nhật qua nhiều attempt, mốc chặn mới phải đi theo thì đồng hồ đếm ngược mới được đặt lại
			if !ev.RetryAt.IsZero() {
				existing.RetryAt = ev.RetryAt
			}
			return
		}
	}

	m.events = append(m.events, ev)
	if ev.ID != "" {
		m.eventIndex[ev.ID] = len(m.events) - 1
	}
	if len(m.events) > maxEvents {
		drop := len(m.events) - maxEvents
		m.events = m.events[drop:]
		m.rebuildEventIndex()
	}
}

// trimStreamRounds cắt streamRounds còn maxStreamRounds phần; phần vượt bị bỏ từ đầu.
// Thời điểm gọi: sau mỗi lần streamClear mở vòng mới.
func (m *Model) trimStreamRounds() {
	if len(m.streamRounds) <= maxStreamRounds {
		return
	}
	drop := len(m.streamRounds) - maxStreamRounds
	m.streamRounds = m.streamRounds[drop:]
}

func (m *Model) rebuildEventIndex() {
	m.eventIndex = make(map[string]int, len(m.events))
	for i, e := range m.events {
		if e.ID != "" {
			m.eventIndex[e.ID] = i
		}
	}
}

func (m *Model) resetOutputPanels() {
	m.events = nil
	m.eventIndex = make(map[string]int)
	m.viewport.SetContent("")
	m.viewport.GotoTop()
	m.streamBuf.Reset()
	m.streamRounds = nil
	m.streamVP.SetContent("")
	m.streamVP.GotoTop()
	m.streamRound = 0
}
