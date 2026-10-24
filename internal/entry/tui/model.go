package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/utils"
)

const maxEvents = 500

// maxStreamRounds giới hạn số lượt được giữ trong panel luồng. Mỗi lần LLM call kết
// thúc sẽ kích hoạt một streamClear mở lượt mới, một chương của writer khoảng 3~5 lượt
// (agent header / suy luận / bản nháp / commit), 32 lượt tương đương xem lại luồng đầu ra
// của 6~10 chương gần nhất. Nội dung chương đã commit nằm trong store/drafts, vượt quá
// thì bị bỏ để tránh mỗi token delta kích hoạt O(toàn văn) render lại.
// Trần bộ nhớ ở trạng thái ổn định khoảng 512KB, thấp hơn nhiều so với ngưỡng lag.
const maxStreamRounds = 32

type focusPane int

const (
	focusEvents focusPane = iota
	focusStream
	focusDetail
	focusState // thanh trạng thái bên trái (cuộn được)

	focusPaneCount // tổng số vùng tiêu điểm, dùng để luân chuyển bằng Tab
)

type appMode int

const (
	modeNew     appMode = iota // chờ người dùng nhập yêu cầu truyện
	modeRunning                // đang sáng tác (kể cả dừng vì lỗi, vẫn nhập để tiếp tục được)
	modeDone                   // đã sáng tác xong
)

// Dãy frame spinner dùng chung cho thanh trên và hoạt ảnh luồng (bubbles.Spinner.MiniDot).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Dãy frame spinner riêng cho dòng "đang thực hiện" trong luồng sự kiện (bubbles.Spinner.Dot).
// 7 chấm + 1 khoảng trống xoay theo chiều kim đồng hồ trên lưới 3×3, nhìn như một vòng
// tải tròn hoàn chỉnh.
// Dùng chỉ số frame riêng + tick nhanh hơn, không ảnh hưởng nhịp của thanh trên và
// hiệu ứng ngôi sao.
var toolSpinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// Model là trạng thái cấp cao nhất của TUI.
type Model struct {
	runtime        *host.Host
	cocreate       *cocreateState
	help           *helpState
	modelSwitch    *modelSwitchState
	modelConfig    *modelConfigState
	report         *reportState
	version        string
	importer       *importState
	importSeq      int
	simulator      *simulationState
	simSeq         int
	compItems      []commandPaletteItem
	compIdx        int
	compActive     bool
	commandToken   string // token lệnh đã đăng ký hiện tại; chỉ render đoạn này, không tô màu tham số
	snapshot       host.UISnapshot
	events         []host.Event
	eventIndex     map[string]int   // event.ID → chỉ số trong m.events; sự kiện loại lệnh gọi tới sẽ cập nhật tại chỗ
	viewport       viewport.Model   // viewport của luồng sự kiện
	streamVP       viewport.Model   // viewport của đầu ra luồng
	detailVP       viewport.Model   // viewport chi tiết bên phải
	stateVP        viewport.Model   // viewport của thanh trạng thái bên trái (cuộn được)
	streamBuf      *strings.Builder // bộ đệm tích luỹ văn bản luồng
	streamRounds   []string
	textarea       textarea.Model
	width          int
	height         int
	autoScroll     bool
	streamScroll   bool      // panel luồng tự bám theo
	streamDirty    bool      // streamRounds còn delta chưa được đẩy
	flushPending   bool      // đã hẹn một lần đẩy luồng, tránh mỗi delta lại bật timer mới
	lastKeyAt      time.Time // thời điểm phím khác Enter gần nhất; throttle KeyEnter chống dán \n gửi nhầm
	inputHistory   []string  // lịch sử các lượt đã gửi (khử trùng: hai lượt liền nhau không lặp)
	historyIdx     int       // chỉ số đang duyệt; == len(inputHistory) nghĩa là "chưa duyệt, đang soạn bản nháp"
	historyDraft   string    // bản nháp lưu trước khi vào chế độ duyệt lịch sử, khôi phục khi quay lại cuối
	focusPane      focusPane
	hoverPane      focusPane
	hoverActive    bool
	mode           appMode
	starting       bool // UI đã vào bàn làm việc, Host đang chạy phần khởi tạo lúc khởi động
	startupMode    startupMode
	importHint     string // gợi ý phát hiện import dở dang lúc khởi động (hiện ở màn chào; xoá khi bắt đầu import)
	cocreateSeq    int
	reportSeq      int
	err            error
	spinnerIdx     int
	toolSpinnerIdx int  // chỉ số frame riêng cho dòng đang thực hiện trong luồng sự kiện (tick 150ms, không ảnh hưởng thanh trên/ngôi sao)
	toolTicking    bool // đã bật timer hoạt ảnh công cụ; tự dừng khi không còn sự kiện đang chạy
	cursorIdx      int  // chỉ số frame con trỏ trong luồng (tiến theo hoạt ảnh chủ động)
	streamRound    int  // bộ đếm lượt của đầu ra luồng
	quitPending    bool // xác nhận thoát bằng hai lần Ctrl+C
	abortPending   bool // tạm dừng thủ công đang chờ Done quay lại
	mouseOff       bool // khi true đã tắt báo cáo chuột để người dùng kéo chọn và sao chép native; bật lại sẽ khôi phục
}

// NewModel tạo Model của TUI.
func NewModel(rt *host.Host, version string) Model {
	ta := textarea.New()
	ta.Placeholder = placeholderForNewMode(startupModeQuick)
	ta.CharLimit = 5000
	ta.SetHeight(1)
	// MaxHeight=6 khiến nội dung quá dài tự xuống dòng theo bề rộng (tối đa 6 dòng hiển thị).
	ta.MaxHeight = 6
	ta.ShowLineNumbers = false
	ta.Focus()

	// Mặc định Enter không xuống dòng (do handleEnterKey gửi đi);
	// xuống dòng chủ động được gán lại cho ctrl+j (unix \n) và alt+enter (thói quen GUI).
	// Tầng giao thức terminal không phân biệt được Shift+Enter với Enter, nên không hỗ trợ Shift+Enter.
	ta.KeyMap.InsertNewline.SetKeys("ctrl+j", "alt+enter")

	vp := viewport.New(80, 20)
	vp.SetContent("")

	svp := viewport.New(80, 10)
	svp.SetContent("")

	dvp := viewport.New(40, 20)
	dvp.SetContent("")

	stvp := viewport.New(32, 20)
	stvp.SetContent("")

	// Lúc khởi động dò một lần import dở dang (LoadState tính lại digest artifact, không đưa vào vòng thăm dò snapshot);
	// nếu không chủ động báo, người dùng chỉ phát hiện ra khi việc sáng tác bị cổng chặn từ chối (RFC §18.2).
	importHint := ""
	if rt != nil {
		importHint = rt.ImportResumeHint()
	}

	return Model{
		runtime:      rt,
		version:      strings.TrimSpace(version),
		autoScroll:   true,
		streamScroll: true,
		mode:         modeNew,
		startupMode:  startupModeQuick,
		importHint:   importHint,
		textarea:     ta,
		viewport:     vp,
		streamVP:     svp,
		detailVP:     dvp,
		stateVP:      stvp,
		streamBuf:    &strings.Builder{},
		eventIndex:   make(map[string]int),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		listenEvents(m.runtime),
		listenDone(m.runtime),
		listenStream(m.runtime),
		tickSnapshot(m.runtime),
		bootstrapRuntime(m.runtime),
		tickSpinner(),
	)
}

func (m *Model) paneAtMouse(x, y int) (focusPane, bool) {
	if m.width == 0 || m.height == 0 {
		return focusEvents, false
	}

	topH, _, bodyH := m.layoutHeights()
	if bodyH < 1 {
		return focusEvents, false
	}

	bodyStartY := topH
	bodyEndY := topH + bodyH
	if y < bodyStartY || y >= bodyEndY {
		return focusEvents, false
	}

	leftW := m.sidebarWidth()
	rightW := m.detailWidth()
	centerStartX := leftW
	rightStartX := m.width - rightW

	if x >= rightStartX {
		return focusDetail, true
	}
	if x < centerStartX {
		return focusState, true
	}

	eventH, _ := m.splitHeights(bodyH)
	if y-bodyStartY < eventH {
		return focusEvents, true
	}
	return focusStream, true
}

func (m *Model) paneHighlighted(pane focusPane) bool {
	if m.focusPane == pane {
		return true
	}
	return m.hoverActive && m.hoverPane == pane
}

// hasRunningEvent cho biết có sự kiện loại lệnh gọi nào chưa xong (spinner vẫn còn quay) hay không.
// toolSpinnerTick dùng điều này để quyết định có đáng render lại hay không: khi không có
// sự kiện đang chạy thì frame spinner không ảnh hưởng đầu ra, cả refreshEventViewport
// là công việc vô ích chắc chắn.
func (m *Model) hasRunningEvent() bool {
	for i := range m.events {
		if m.events[i].Running() {
			return true
		}
	}
	return false
}

// flushStreamIfDirty đưa các streamRounds đã tích luỹ vào viewport; đánh dấu là đã đẩy.
// Trả về việc thật sự có đẩy hay không, để lời gọi quyết định có cần GotoBottom.
func (m *Model) flushStreamIfDirty() bool {
	if !m.streamDirty {
		return false
	}
	m.refreshStreamViewport()
	m.streamDirty = false
	return true
}

// refreshEventViewport render lại nội dung luồng sự kiện và đặt vào viewport.
func (m *Model) refreshEventViewport() {
	centerW := m.eventFlowWidth()
	content := renderEventContent(m.events, centerW, m.toolSpinnerIdx)
	snap := m.snapshot
	if m.starting {
		snap.IsRunning = true
	}
	if activity := renderEventActivity(snap, m.spinnerIdx, centerW); activity != "" {
		if strings.TrimSpace(content) != "" {
			content += "\n" + activity
		} else {
			content = activity
		}
	}
	m.viewport.SetContent(content)
	if m.autoScroll {
		m.viewport.GotoBottom()
	}
}

func (m *Model) refreshStreamViewport() {
	cursor := ""
	if m.snapshot.IsRunning {
		cursor = renderStreamCursor(m.cursorIdx)
	}
	m.streamVP.SetContent(renderStreamContent(m.streamRounds, m.streamVP.Width, cursor))
}

func (m *Model) refreshDetailViewport() {
	rightW := m.detailWidth()
	if rightW <= 4 {
		return
	}
	m.detailVP.SetContent(renderDetailContent(m.snapshot, rightW-4))
}

// refreshStateViewport đẩy nội dung thanh trạng thái bên trái vào viewport.
// Nội dung thanh bên hoàn toàn suy ra từ snapshot, nên khi snapshot hoặc kích thước đổi
// đều phải đẩy lại.
func (m *Model) refreshStateViewport() {
	leftW := m.sidebarWidth()
	if leftW <= 4 {
		return
	}
	m.stateVP.SetContent(renderStateContent(m.snapshot, leftW-4))
}

// updateViewportSize cập nhật kích thước các viewport theo kích thước cửa sổ hiện tại.
func (m *Model) updateViewportSize() {
	centerW := m.eventFlowWidth()
	rightW := m.detailWidth()
	bodyH := m.bodyHeight()
	eventH, streamH := m.splitHeights(bodyH)
	m.viewport.Width = centerW - 2
	m.viewport.Height = eventH - 1 // -1 là dòng header của event panel
	m.streamVP.Width = centerW - 2
	m.streamVP.Height = streamH - 1 // -1 là dòng header của stream panel
	m.detailVP.Width = rightW - 2
	m.detailVP.Height = bodyH
	leftW := m.sidebarWidth()
	m.stateVP.Width = max(1, leftW-2)
	m.stateVP.Height = max(1, bodyH-1) // -1 là khoảng trống phía trên, dòng dưới hiện thẳng nội dung
	// Sau khi chiều cao hoặc nội dung ngắn lại, hai cột cuộn tự do có thể dừng ở lệch
	// vượt biên (SetContent của bubbles chỉ chặn vượt dòng cuối), viewport sẽ lấp đầy
	// bằng dòng trống. SetYOffset tự kẹp trong giới hạn.
	m.stateVP.SetYOffset(m.stateVP.YOffset)
	m.detailVP.SetYOffset(m.detailVP.YOffset)
}

// splitHeights tính phân bổ chiều cao giữa luồng sự kiện và đầu ra luồng.
func (m *Model) splitHeights(bodyH int) (eventH, streamH int) {
	eventH = bodyH * 40 / 100
	if eventH < 3 {
		eventH = 3
	}
	streamH = bodyH - eventH - 1 // -1 là đường phân cách
	if streamH < 3 {
		streamH = 3
	}
	return
}

func (m *Model) inputWidth() int {
	if m.width == 0 {
		return 60
	}
	return m.width - 6 // border + padding + dấu nhắc "❯ "
}

func (m *Model) currentInputWidth() int {
	if m.cocreate != nil {
		return coCreateInputWidth(m.width, m.height)
	}
	return m.inputWidth()
}

// refitTextareaHeight ước lượng số dòng hiển thị theo nội dung hiện tại rồi SetHeight cho hợp lý.
// Số dòng hiển thị = tổng số dòng sau khi mỗi đoạn logic (tách bằng \n) được wrap theo bề rộng.
// Kết hợp MaxHeight=6 để "nội dung quá dài hoặc xuống dòng chủ động tự hiện nhiều dòng, tối đa 6 dòng".
func (m *Model) refitTextareaHeight() {
	w := m.textarea.Width()
	if w <= 0 {
		return
	}
	// Ở chế độ đồng sáng tác, input cố định 1 dòng: nội dung nhiều dòng trong textarea sẽ được
	// textarea tự cuộn theo con trỏ. Nếu không, chiều cao inputBox đổi theo nội dung sẽ làm cột
	// conversation bên trái co lại và input trôi theo chiều dọc, phá vỡ độ ổn định của bố cục.
	if m.cocreate != nil {
		m.textarea.SetHeight(1)
		return
	}
	text := m.textarea.Value()
	if text == "" {
		m.textarea.SetHeight(1)
		return
	}
	// Trừ 2 cột dư (prompt symbol + cursor bên trong textarea), dư 1 dòng là chấp nhận được.
	contentW := w - 2
	if contentW < 1 {
		contentW = 1
	}
	total := 0
	for line := range strings.SplitSeq(text, "\n") {
		lw := lipgloss.Width(line)
		if lw == 0 {
			total++
			continue
		}
		total += (lw + contentW - 1) / contentW
	}
	if total < 1 {
		total = 1
	}
	m.textarea.SetHeight(total) // SetHeight tự kẹp theo MaxHeight bên trong
}

// resizeTextarea đặt đồng thời bề rộng và chiều cao tính theo nội dung.
// Thay thế các lệnh gọi SetWidth(currentInputWidth()) rải rác, bảo đảm khi bề rộng đổi
// thì chiều cao đi theo.
func (m *Model) resizeTextarea() {
	m.textarea.SetWidth(m.currentInputWidth())
	m.refitTextareaHeight()
}

// maxInputHistory giới hạn độ dài lịch sử, tránh bộ nhớ tăng trong phiên dài.
const maxInputHistory = 200

// pushInputHistory thêm nội dung gửi thành công vào lịch sử, khử trùng giữa hai lượt liền nhau. Đặt lại chỉ số duyệt.
func (m *Model) pushInputHistory(text string) {
	if text == "" {
		return
	}
	if n := len(m.inputHistory); n == 0 || m.inputHistory[n-1] != text {
		m.inputHistory = append(m.inputHistory, text)
		if len(m.inputHistory) > maxInputHistory {
			m.inputHistory = m.inputHistory[len(m.inputHistory)-maxInputHistory:]
		}
	}
	m.historyIdx = len(m.inputHistory)
	m.historyDraft = ""
}

// tryHistoryUp đi tới một mục lịch sử sớm hơn; trả về phím đã được xử lý hay chưa.
// Lần đầu vào chế độ duyệt lịch sử sẽ lưu nội dung textarea hiện tại làm draft, khôi phục khi quay lại cuối.
// Lời gọi phải tự quyết định có nên tránh đường này trong tình huống nhiều dòng (để textarea tự xử lý việc di chuyển con trỏ trong dòng).
func (m *Model) tryHistoryUp() bool {
	if len(m.inputHistory) == 0 || m.historyIdx <= 0 {
		return false
	}
	if m.historyIdx == len(m.inputHistory) {
		m.historyDraft = m.textarea.Value()
	}
	m.historyIdx--
	m.textarea.SetValue(m.inputHistory[m.historyIdx])
	m.textarea.CursorEnd()
	m.syncCommandInputHighlight()
	m.refitTextareaHeight()
	return true
}

// tryHistoryDown đi tới một mục lịch sử mới hơn; khi tới cuối sẽ khôi phục draft.
func (m *Model) tryHistoryDown() bool {
	if m.historyIdx >= len(m.inputHistory) {
		return false
	}
	m.historyIdx++
	if m.historyIdx == len(m.inputHistory) {
		m.textarea.SetValue(m.historyDraft)
		m.historyDraft = ""
	} else {
		m.textarea.SetValue(m.inputHistory[m.historyIdx])
	}
	m.textarea.CursorEnd()
	m.syncCommandInputHighlight()
	m.refitTextareaHeight()
	return true
}

// textareaIsMultiline cho biết nội dung textarea hiện tại có xuống dòng chủ động hay không; dùng để quyết định ↑↓ đi lịch sử hay di chuyển trong dòng.
func (m *Model) textareaIsMultiline() bool {
	return strings.Contains(m.textarea.Value(), "\n")
}

// inputHints sinh văn bản gợi ý ở đáy theo trạng thái hiện tại.
// Cuối chuỗi luôn nối copySuffix, để người dùng ở mọi trạng thái không khẩn cấp đều thấy
// cách chọn và sao chép; khi chuột đã tắt thì hiện chữ đỏ nổi bật nhắc nhấn phím lần nữa
// để bật lại tương tác chuột.
func (m *Model) inputHints() string {
	dimStyle := lipgloss.NewStyle().Foreground(colorDim)
	if m.quitPending {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Bold(true).Render("Nhấn Ctrl+C một lần nữa để thoát")
	}
	limitHint := m.inputLimitHint()
	suffix := limitHint + " · Ctrl+R Chế độ copy"
	if m.mode == modeNew {
		suffix = limitHint
	}
	if m.mouseOff && m.mode != modeNew {
		return lipgloss.NewStyle().Foreground(colorAccent).Bold(true).
			Render("✂ Chế độ bôi đen copy: Kéo chuột chọn văn bản để sao chép · Ctrl+R Quay lại")
	}
	if m.cocreate != nil {
		scrollHint := " · Tab Cuộn: Hội thoại"
		if m.cocreate.focusPrompt {
			scrollHint = " · Tab Cuộn: Chỉ thị"
		}
		switch {
		case m.cocreate.awaiting:
			return dimStyle.Render("Chờ AI phản hồi · Esc Thoát đồng sáng tác" + scrollHint + suffix)
		case m.cocreate.canStart():
			startLabel := "Ctrl+S Bắt đầu sáng tác"
			if m.cocreate.stage {
				startLabel = "Ctrl+S Áp dụng & Tiếp tục"
			}
			return dimStyle.Render("Enter Gửi · " + startLabel + " · Esc Thoát" + scrollHint + suffix)
		default:
			return dimStyle.Render("Enter Gửi · Esc Thoát đồng sáng tác" + scrollHint + suffix)
		}
	}
	if m.mode == modeNew {
		if m.startupMode == startupModeQuick {
			return dimStyle.Render("Tab Đổi chế độ · Gõ / để xem lệnh · Enter Bắt đầu viết ngay · Esc Xóa" + suffix)
		}
		return dimStyle.Render("Tab Đổi chế độ · Gõ / để xem lệnh · Enter Bắt đầu đồng sáng tác · Esc Xóa" + suffix)
	}
	switch m.snapshot.RuntimeState {
	case "pausing":
		return dimStyle.Render("Đang tạm dừng sáng tác · Vui lòng chờ vòng hiện tại kết thúc" + suffix)
	case "paused":
		return dimStyle.Render("Gõ / để xem lệnh · Enter Tiếp tục sáng tác · Esc Xóa" + suffix)
	}
	return dimStyle.Render("Gõ / để xem lệnh · Click/Tab Đổi panel · ↑↓ Cuộn · End Về cuối · Ctrl+L Xóa màn hình · Esc Tạm dừng · Enter Gửi" + suffix)
}

func (m *Model) inputLimitHint() string {
	limit := m.textarea.CharLimit
	if limit <= 0 {
		return ""
	}
	used := m.textarea.Length()
	if used < limit*4/5 {
		return ""
	}
	return fmt.Sprintf(" · Đã nhập %d/%d", used, limit)
}

func (m *Model) eventFlowWidth() int {
	if m.width == 0 {
		return 80
	}
	leftW := m.sidebarWidth()
	rightW := m.detailWidth()
	return m.width - leftW - rightW
}

func (m *Model) sidebarWidth() int {
	if m.width == 0 {
		return 32
	}
	return m.width * 23 / 100
}

func (m *Model) detailWidth() int {
	if m.width == 0 {
		return 40
	}
	return m.width * 27 / 100
}

func (m *Model) bodyHeight() int {
	_, _, bodyH := m.layoutHeights()
	return bodyH
}

func (m *Model) currentSpinnerFrame() string {
	if !m.snapshot.IsRunning && !m.starting {
		return ""
	}
	return spinnerFrames[m.spinnerIdx%len(spinnerFrames)]
}

func (m *Model) outputDir() string {
	if m.runtime == nil {
		return ""
	}
	return m.runtime.Dir()
}

func defaultSteerPlaceholder() string {
	return "Nhập can thiệp cốt truyện, ví dụ: đẩy tuyến tình cảm lên chương 4"
}

func (m *Model) syncRuntimePlaceholder() {
	if m.mode != modeRunning || m.cocreate != nil {
		return
	}
	if m.starting {
		m.textarea.Placeholder = "Đang khởi tạo sáng tác..."
		return
	}
	switch m.snapshot.RuntimeState {
	case "completed":
		m.textarea.Placeholder = donePlaceholder
	case "pausing":
		m.textarea.Placeholder = "Đang tạm dừng sáng tác..."
	case "paused":
		if m.snapshot.AdvanceMode == "review" && m.snapshot.Phase == "writing" {
			m.textarea.Placeholder = "Chờ nghiệm thu: Nhập ý kiến chỉnh sửa, hoặc /next để duyệt chương tiếp"
		} else {
			m.textarea.Placeholder = "Sáng tác đã tạm dừng, nhập nội dung bất kỳ để tiếp tục"
		}
	default:
		if !m.snapshot.IsRunning {
			if m.snapshot.AdvanceMode == "review" && m.snapshot.Phase == "writing" {
				m.textarea.Placeholder = "Chờ nghiệm thu: Nhập ý kiến chỉnh sửa, hoặc /next để duyệt chương tiếp"
			} else {
				m.textarea.Placeholder = "Sáng tác bị gián đoạn, nhập nội dung bất kỳ để tiếp tục"
			}
		} else {
			m.textarea.Placeholder = defaultSteerPlaceholder()
		}
	}
}

func (m *Model) renderBottomBar() string {
	inputView := highlightCommandToken(m.textarea.View(), m.textarea.Value(), m.commandToken)
	inputBox := renderInputBox(
		inputView,
		m.inputHints(),
		m.snapshot,
		m.outputDir(),
		m.width,
	)
	if m.mode != modeNew || m.cocreate != nil {
		return inputBox
	}
	return renderStartupModeBar(m.width, m.startupMode) + "\n" + inputBox
}

func (m *Model) layoutHeights() (topH, inputH, bodyH int) {
	if m.width == 0 || m.height == 0 {
		return 1, 4, 20
	}
	topH = lipgloss.Height(renderTopBar(m.snapshot, m.width, m.currentSpinnerFrame(), m.version))
	inputH = lipgloss.Height(m.renderBottomBar())
	bodyH = m.height - topH - inputH
	if bodyH < 3 {
		bodyH = 3
	}
	return
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Đang tải..."
	}
	if m.width < 100 {
		return lipgloss.NewStyle().
			Width(m.width).Height(m.height).
			AlignHorizontal(lipgloss.Center).
			AlignVertical(lipgloss.Center).
			Render("Độ rộng terminal quá nhỏ, vui lòng mở rộng tối thiểu 100 cột")
	}
	if m.cocreate != nil {
		return renderCoCreateModal(m.width, m.height, m.cocreate, errorText(m.err), m.textarea.View(), m.spinnerIdx, m.quitPending)
	}
	if m.help != nil {
		return renderHelpModal(m.width, m.height, m.help)
	}
	if m.report != nil {
		return renderReportModal(m.width, m.height, m.report)
	}
	if m.importer != nil {
		// Import không phụ thuộc trạng thái chạy của Engine, frame hoạt ảnh lấy trực tiếp từ spinnerIdx (currentSpinnerFrame trả về rỗng khi engine đã dừng).
		return renderImportModal(m.width, m.height, m.importer, m.spinnerIdx)
	}
	if m.simulator != nil {
		return renderSimulationModal(m.width, m.height, m.simulator)
	}

	topBar := renderTopBar(m.snapshot, m.width, m.currentSpinnerFrame(), m.version)
	inputBox := m.renderBottomBar()
	_, inputH, bodyH := m.layoutHeights()

	var body string
	if m.mode == modeNew {
		errMsg := ""
		if m.err != nil {
			errMsg = m.err.Error()
		}
		body = renderWelcome(m.width, bodyH, errMsg, m.startupMode, m.importHint)
	} else {
		leftW := m.sidebarWidth()
		rightW := m.detailWidth()
		centerW := m.width - leftW - rightW
		eventH, streamH := m.splitHeights(bodyH)

		if m.viewport.Width != centerW-2 || m.viewport.Height != eventH-1 {
			m.viewport.Width = centerW - 2
			m.viewport.Height = eventH - 1 // -1 là dòng header của event panel
		}
		if m.streamVP.Width != centerW-2 || m.streamVP.Height != streamH-1 {
			m.streamVP.Width = centerW - 2
			m.streamVP.Height = streamH - 1 // -1 là dòng header của stream panel
		}

		eventFlow := renderEventFlowViewport(m.viewport, centerW, eventH, m.paneHighlighted(focusEvents))
		streamPanel := renderStreamPanel(m.streamVP, centerW, streamH, m.paneHighlighted(focusStream), m.snapshot.IsRunning || m.starting, m.spinnerIdx)
		center := lipgloss.JoinVertical(lipgloss.Left, eventFlow, streamPanel)

		left := renderStatePanel(m.stateVP, leftW, bodyH, m.paneHighlighted(focusState))
		right := renderDetailPanel(m.detailVP, rightW, bodyH, m.paneHighlighted(focusDetail))
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
	}

	view := lipgloss.JoinVertical(lipgloss.Left, topBar, body, inputBox)

	// Lớp phủ modal nổi lên trên đáy body, không ảnh hưởng bố cục
	if m.modelSwitch != nil {
		commandBar := renderModelSwitchBar(m.width, m.modelSwitch)
		view = overlayAboveInput(view, commandBar, inputH)
	} else if m.modelConfig != nil {
		view = overlayAboveInput(view, renderModelConfigModal(m.width, m.modelConfig), inputH)
	} else if m.compActive {
		commandBar := renderCommandPalette(m.width, m.compItems, m.compIdx)
		view = overlayAboveInput(view, commandBar, inputH)
	}
	return view
}

// sendCoCreate phát một vòng yêu cầu đồng sáng tác, gom xử lý reqID, textarea và placeholder.
func (m *Model) sendCoCreate() tea.Cmd {
	m.cocreateSeq++
	m.cocreate.reqID = m.cocreateSeq
	m.cocreate.awaiting = true
	m.resizeTextarea()
	m.textarea.Placeholder = placeholderForCoCreate(m.cocreate)
	m.textarea.Blur()
	return runCoCreate(m.runtime, m.cocreate)
}

func (m Model) handleCoCreateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.cocreate == nil {
		return m, nil
	}
	state := m.cocreate

	// Phím ↑↓/PgUp/PgDn/Home/End để cuộn; Tab chuyển vùng cuộn giữa cột hội thoại bên trái ↔ cột chỉ dẫn sáng tác bên phải
	// (mặc định là cột trái, nơi người dùng xem lại thân bài). Màn chào đã tắt báo cáo chuột để giữ thao tác sao chép
	// native; khi cột phải tràn, chuyển tiêu điểm bằng Tab rồi dùng phím để cuộn. Cột trái: cuộn lên thì tắt follow,
	// cuộn tới đáy thì bật lại follow (bám theo luồng).
	switch msg.Type {
	case tea.KeyTab:
		state.focusPrompt = !state.focusPrompt
		return m, nil
	case tea.KeyUp, tea.KeyPgUp:
		if state.focusPrompt {
			var cmd tea.Cmd
			state.promptVP, cmd = state.promptVP.Update(msg)
			return m, cmd
		}
		state.convFollow = false
		var cmd tea.Cmd
		state.convVP, cmd = state.convVP.Update(msg)
		return m, cmd
	case tea.KeyDown, tea.KeyPgDown:
		if state.focusPrompt {
			var cmd tea.Cmd
			state.promptVP, cmd = state.promptVP.Update(msg)
			return m, cmd
		}
		var cmd tea.Cmd
		state.convVP, cmd = state.convVP.Update(msg)
		if state.convVP.AtBottom() {
			state.convFollow = true
		}
		return m, cmd
	case tea.KeyHome:
		if state.focusPrompt {
			state.promptVP.GotoTop()
			return m, nil
		}
		state.convFollow = false
		state.convVP.GotoTop()
		return m, nil
	case tea.KeyEnd:
		if state.focusPrompt {
			state.promptVP.GotoBottom()
			return m, nil
		}
		state.convFollow = true
		state.convVP.GotoBottom()
		return m, nil
	case tea.KeyEsc:
		return m.exitCoCreate()
	}

	// Khi đang chờ AI trả lời, các phím sửa (gõ ký tự/lùi/con trỏ/Ctrl+U/xuống dòng nhiều dòng) vẫn được cho qua —
	// người dùng có thể gõ trước câu tiếp theo trong lúc AI suy luận. Việc chặn nhóm phím gửi được đẩy
	// xuống trong từng case, để throttle Enter diễn ra trước lớp chặn awaiting — nhờ vậy mảnh \n
	// dán vào vẫn được bù thành dấu cách.

	switch msg.Type {
	case tea.KeyCtrlS:
		if state.awaiting {
			return m, nil
		}
		if !state.canStart() {
			return m, nil
		}
		// Đồng sáng tác theo giai đoạn: đưa "brief định hướng tiếp theo" vào và khôi phục việc sáng tác, quay lại bàn chạy.
		if state.stage {
			draft := state.draftPrompt()
			m.cocreate = nil
			m.err = nil
			m.resizeTextarea()
			m.textarea.Placeholder = defaultSteerPlaceholder()
			return m, tea.Batch(resumeFromCoCreate(m.runtime, draft), m.textarea.Focus())
		}
		// Đồng sáng tác khởi động lạnh: bắt đầu sáng tác với chỉ dẫn sáng tác đã chuẩn bị.
		prompt, err := state.buildPrompt()
		if err != nil {
			m.err = err
			return m, nil
		}
		cmd := m.enterStarting(prompt)
		return m, tea.Batch(startRuntime(m.runtime, prompt), cmd)
	case tea.KeyEnter:
		// Alt+Enter → xuống dòng chủ động, để textarea.Update tiếp quản (KeyMap.InsertNewline đã gán phím này)
		if msg.Alt {
			break
		}
		// Khoảng cách từ phím ký tự gần nhất quá ngắn → coi là mảnh \n của dòng dán: bù dấu cách thay vì gửi.
		// Phải kiểm tra trước lớp chặn awaiting — nếu không, mảnh \n dán vào lúc awaiting sẽ bị chặn,
		// khiến "abc\ndef" bị nuốt thành "abcdef", không khớp ngữ nghĩa của đường base.
		if !m.lastKeyAt.IsZero() && time.Since(m.lastKeyAt) < 50*time.Millisecond {
			var cmd tea.Cmd
			state.resetSuggestionInput()
			m.textarea, cmd = m.textarea.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
			m.refitTextareaHeight()
			return m, cmd
		}
		// Ý định gửi thật sự: bị chặn trong lúc awaiting (không được phát hai request song song)
		if state.awaiting {
			return m, nil
		}
		text := utils.CleanInputLine(m.textarea.Value())
		if text == "" {
			return m, nil
		}
		m.err = nil
		state.appendUser(text)
		m.textarea.Reset()
		m.refitTextareaHeight()
		cmd := m.sendCoCreate()
		return m, cmd
	case tea.KeyCtrlU:
		state.resetSuggestionInput()
		m.textarea.Reset()
		m.refitTextareaHeight()
		return m, nil
	}

	// Phím số 1/2/3 có thể ghép liên tiếp các gợi ý: lần đầu điền vào, các lần sau nối bằng dấu chấm phẩy, chọn lặp thì bỏ qua.
	// Bất kỳ thao tác sửa tay nào cũng sẽ thoát khỏi trạng thái ghép phím tắt, các phím số sau đó giữ nguyên ngữ nghĩa nhập thường.
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && !state.awaiting {
		if r := msg.Runes[0]; r >= '1' && r <= '3' {
			if value, handled := state.appendSuggestion(int(r-'1'), m.textarea.Value()); handled {
				m.textarea.SetValue(value)
				m.textarea.CursorEnd()
				m.refitTextareaHeight()
				return m, nil
			}
		}
	}

	// Chuyển tiếp phím nhập thường cho textarea
	if msg.Type == tea.KeyRunes && (containsSGRFragment(string(msg.Runes)) || isCSILeak(msg.Runes)) {
		return m, nil
	}
	var ok bool
	if msg, ok = cleanHumanKeyRunes(msg); !ok {
		return m, nil
	}
	state.resetSuggestionInput()
	if msg.Type == tea.KeyRunes {
		m.lastKeyAt = time.Now()
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.refitTextareaHeight()
	return m, cmd
}

// exitCoCreate thoát chế độ đồng sáng tác, huỷ request LLM đang chạy, khôi phục trạng thái ô nhập.
func (m Model) exitCoCreate() (tea.Model, tea.Cmd) {
	if m.cocreate.cancel != nil {
		m.cocreate.cancel()
	}
	stage := m.cocreate.stage
	initial := m.cocreate.initialInput()
	m.cocreate = nil
	m.resizeTextarea()
	// Huỷ đồng sáng tác theo giai đoạn: xoá cờ đang chiếm, giữ trạng thái tạm dừng, quay lại trạng thái nhập của bàn chạy (không lấp lại lời mở đầu tổng hợp).
	if stage {
		m.textarea.SetValue("")
		m.textarea.Placeholder = defaultSteerPlaceholder()
		return m, tea.Batch(cancelCoCreate(m.runtime), fetchSnapshot(m.runtime), m.textarea.Focus())
	}
	m.textarea.SetValue(initial)
	m.textarea.Placeholder = placeholderForNewMode(m.startupMode)
	return m, m.textarea.Focus()
}

// overlayAboveInput nổi overlay lên đáy của ảnh base (phía trên inputBox),
// không đổi chiều cao bố cục tổng thể. Chỉ che phần bề rộng riêng của thẻ overlay, phần bên phải lộ nội dung bên dưới.
func overlayAboveInput(base, overlay string, inputLineCount int) string {
	baseLines := strings.Split(base, "\n")
	overLines := strings.Split(strings.TrimRight(overlay, "\n"), "\n")

	endY := len(baseLines) - inputLineCount
	startY := endY - len(overLines)
	if startY < 0 {
		startY = 0
	}

	for i, ol := range overLines {
		y := startY + i
		if y >= 0 && y < endY {
			olW := lipgloss.Width(ol)
			// Cắt olW ký tự nhìn thấy ở bên trái của đường cơ sở, rồi ghép overlay + phần còn lại bên phải
			right := ansi.TruncateLeft(baseLines[y], olW, "")
			baseLines[y] = ol + right
		}
	}
	return strings.Join(baseLines, "\n")
}

// isCSILeak phát hiện KeyRunes có phải mảnh rò rỉ của chuỗi thoát CSI hay không.
// Khi terminal gửi phím mũi tên \x1b[A, gõ nhanh có thể làm chuỗi bị tách:
// \x1b bị phân tích thành Escape, còn "[" hoặc "[A" lọt vào textarea dưới dạng KeyRunes.
func isCSILeak(runes []rune) bool {
	if len(runes) == 0 || runes[0] != '[' {
		return false
	}
	for _, r := range runes[1:] {
		if (r >= '0' && r <= '9') || r == ';' ||
			(r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '~' {
			continue
		}
		return false
	}
	return true
}

// containsSGRFragment phát hiện văn bản có chứa mảnh rò rỉ chuỗi chuột SGR (mẫu "<số;số;") hay không.
func containsSGRFragment(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '<' {
			continue
		}
		j := i + 1
		if j >= len(s) || s[j] < '0' || s[j] > '9' {
			continue
		}
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j < len(s) && s[j] == ';' {
			return true
		}
	}
	return false
}

func cleanHumanKeyRunes(msg tea.KeyMsg) (tea.KeyMsg, bool) {
	if msg.Type != tea.KeyRunes {
		return msg, true
	}
	cleaned := utils.CleanInputRunes(msg.Runes)
	if cleaned == "" {
		return msg, false
	}
	msg.Runes = []rune(cleaned)
	return msg, true
}
