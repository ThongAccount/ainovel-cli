package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/voocel/ainovel-cli/internal/host/imp"
)

// TestImportHistoryCoalescesRetryLines bảo vệ việc cập nhật tại chỗ của dòng thử lại:
// các sự kiện liên tiếp cùng Key chỉ chiếm một dòng
// ("lần N" nhảy trong cùng một dòng), bị một dòng tiến trình thường cắt ngang thì bắt đầu dòng
// mới, giữ đúng thứ tự thời gian.
func TestImportHistoryCoalescesRetryLines(t *testing.T) {
	s := newImportState(1, "book.txt", 100, 40, nil)
	base := len(s.history)
	retry := func(msg string) imp.Event {
		return imp.Event{Time: time.Now(), Stage: imp.StageSegmenting, Message: msg, Level: "warn", Key: "retry:segmenting"}
	}
	s.appendEvent(retry("sẽ thử lại sau 1s (lần 1)"), 80)
	s.appendEvent(retry("sẽ thử lại sau 2s (lần 2)"), 80)
	s.appendEvent(retry("sẽ thử lại sau 4s (lần 3)"), 80)
	if got := len(s.history) - base; got != 1 {
		t.Fatalf("các lần thử lại liên tiếp cùng Key phải gộp thành 1 dòng, được %d", got)
	}
	if last := s.history[len(s.history)-1]; last.message != "sẽ thử lại sau 4s (lần 3)" {
		t.Fatalf("dòng đã gộp phải cập nhật thành message mới nhất, được %q", last.message)
	}
	// Dòng tiến trình thường cắt ngang thì lần thử lại mới bắt đầu dòng riêng.
	s.appendEvent(imp.Event{Time: time.Now(), Stage: imp.StageAnalyzing, Message: "phân tích các lô liên tiếp kể từ chương 1..."}, 80)
	s.appendEvent(retry("sẽ thử lại sau 1s (lần 1)"), 80)
	if got := len(s.history) - base; got != 3 {
		t.Fatalf("sau khi bị cắt ngang, lần thử lại phải bắt đầu dòng riêng, tổng 3 dòng, được %d", got)
	}
}

// TestRenderImportLineWrapsWithoutClipping bảo vệ việc chi tiết lỗi luôn hiện đủ: phần nội dung
// được xuống dòng theo bề rộng còn lại sau khi trừ tiền tố, các dòng nối can thẳng, không dòng
// nào được vượt quá contentW — viewport cắt cứng dòng quá rộng,
// mã trạng thái HTTP / provider / model trong lỗi chính là manh mối để điều tra, cắt mất
// thì coi như báo lỗi vô nghĩa.
func TestRenderImportLineWrapsWithoutClipping(t *testing.T) {
	ln := importLine{
		at:      time.Now(),
		stage:   imp.StageSegmenting,
		message: "vùng cắt L1..L171",
		err: errors.New("imp: lời gọi model thất bại (tham số yêu cầu không hợp lệ, HTTP 400, openrouter, deepseek/deepseek-chat): " +
			"Provider returned error: invalid request payload with a very long gateway message tail"),
	}
	const contentW = 80
	out := renderImportLine(ln, contentW, time.Now())
	// Chỗ xuống dòng có thể rơi vào bất kỳ ký tự nào, nên so sánh sau khi bỏ khoảng trắng, chỉ kiểm tra nội dung không bị mất chữ nào.
	norm := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == ' ' || r == '\n' {
				return -1
			}
			return r
		}, s)
	}
	for _, want := range []string{"HTTP 400", "openrouter", "gateway message tail"} {
		if !strings.Contains(norm(out), norm(want)) {
			t.Fatalf("nội dung dòng thiếu %q: %q", want, out)
		}
	}
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > contentW {
			t.Fatalf("dòng %d rộng %d vượt quá %d, viewport sẽ cắt mất: %q", i, w, contentW, line)
		}
	}
	// Terminal hẹp: tiền tố (dấu thời gian + biểu tượng + tên giai đoạn dài) chiếm gần hết bề rộng dòng, phần nội dung phải xuống dòng riêng chứ không được ép sát xuống giới hạn làm dòng vượt rộng.
	ln.stage = imp.StageAwaitingConfirmation
	const narrowW = 40
	for i, line := range strings.Split(renderImportLine(ln, narrowW, time.Now()), "\n") {
		if w := lipgloss.Width(line); w > narrowW {
			t.Fatalf("ở terminal hẹp, dòng %d rộng %d vượt quá %d: %q", i, w, narrowW, line)
		}
	}
}

// TestRenderImportLineMultilineBlock bảo vệ bố cục của khối tin nhắn nhiều dòng (xem trước xác nhận
// sau khi cắt): các dòng nối thụt đều nhẹ (2 cột), không được can theo bề rộng tiền tố —
// tiền tố 40+ cột sẽ đẩy cả danh sách chương sang nửa phải của panel, nửa trái trống không.
func TestRenderImportLineMultilineBlock(t *testing.T) {
	ln := importLine{
		at:      time.Now(),
		stage:   imp.StageAwaitingConfirmation,
		current: 157, total: 157,
		message: "đã cắt 157 chương, xin kiểm tra:\n  chương 1 phần mở đầu\n  chương 2 cố tình\n",
	}
	const contentW = 100
	out := strings.Split(renderImportLine(ln, contentW, time.Now()), "\n")
	if len(out) != 3 {
		t.Fatalf("phải là dòng tiền tố + 2 dòng nội dung, được %d dòng: %q", len(out), out)
	}
	for i, line := range out[1:] {
		if w := lipgloss.Width(line); w > contentW {
			t.Fatalf("dòng %d rộng %d vượt quá: %q", i+1, w, line)
		}
		if strings.HasPrefix(line, strings.Repeat(" ", 20)) {
			t.Fatalf("dòng nối của khối nhiều dòng không nên can theo bề rộng tiền tố: %q", line)
		}
		if !strings.HasPrefix(line, "  ") {
			t.Fatalf("dòng nối của khối nhiều dòng phải thụt nhẹ 2 cột: %q", line)
		}
	}
}

// TestWrapTextResetsAtNewlines bảo vệ việc xuống dòng của tin nhắn nhiều dòng: bộ đếm bề rộng dòng
// phải đặt lại ở mỗi '\n', nếu không thì chỉ cần một dòng kích hoạt xuống dòng là mọi dòng
// sau đó bị đánh dấu quá rộng, chèn xuống dòng giả kèm thụt lề, làm vỡ cả bản xem trước xác nhận.
func TestWrapTextResetsAtNewlines(t *testing.T) {
	in := strings.Repeat("rộng", 30) + "\ndòng ngắn 1\ndòng ngắn 2"
	out := wrapText(in, 20)
	for i, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w > 20 {
			t.Fatalf("dòng %d rộng %d vượt quá 20: %q", i, w, l)
		}
	}
	if !strings.Contains(out, "\ndòng ngắn 1\ndòng ngắn 2") {
		t.Fatalf("các dòng ngắn sẵn có không được bị phá vỡ: %q", out)
	}
}

// TestImportEscResumeGate bảo vệ điểm hạ cánh của Esc ở panel import: sau khi một lần import
// phát ra từ màn chào kết thúc thành công, việc đóng panel phải chạy lại một lần khôi phục
// (Resume của bootstrap chỉ chạy lúc khởi động), nếu không người dùng bị bỏ lại ở màn chào
// không có lối vào để viết tiếp; trạng thái kết thúc lỗi và trường hợp đang ở bàn làm việc chỉ
// đóng panel; Esc khi đang chạy vẫn là huỷ chứ không phải đóng.
func TestImportEscResumeGate(t *testing.T) {
	esc := tea.KeyMsg{Type: tea.KeyEsc}
	// Sau khi tea.Batch chạy xong sẽ trả về BatchMsg (lệnh con không được thực thi), dùng để phân biệt "tiêu điểm + khôi phục" với chỉ tiêu điểm.
	isBatch := func(cmd tea.Cmd) bool {
		_, ok := cmd().(tea.BatchMsg)
		return ok
	}
	newM := func(mode appMode, st *importState) Model {
		return Model{mode: mode, importer: st, textarea: textarea.New()}
	}

	m := newM(modeNew, &importState{done: true, stage: imp.StageDone})
	next, cmd := m.handleImportKey(esc)
	if next.(Model).importer != nil {
		t.Fatal("Esc ở trạng thái kết thúc phải đóng panel")
	}
	if !isBatch(cmd) {
		t.Fatal("đóng panel sau khi import thành công ở màn chào phải kèm lệnh khôi phục")
	}

	m = newM(modeNew, &importState{done: true, stage: imp.StageError, err: errors.New("boom")})
	if _, cmd := m.handleImportKey(esc); isBatch(cmd) {
		t.Fatal("trạng thái kết thúc lỗi không nên kích hoạt khôi phục (có thể cuốn sách chưa hề import thành công)")
	}

	m = newM(modeRunning, &importState{done: true, stage: imp.StageDone})
	if _, cmd := m.handleImportKey(esc); isBatch(cmd) {
		t.Fatal("bàn làm việc đã có cổng riêng, không nên kích hoạt khôi phục lần nữa")
	}

	canceled := false
	m = newM(modeNew, &importState{cancel: func() { canceled = true }})
	next, _ = m.handleImportKey(esc)
	if !canceled || next.(Model).importer == nil {
		t.Fatal("Esc khi đang chạy phải huỷ import và giữ lại panel cho runner kết thúc")
	}
}

// TestRetryCountdown bảo vệ hợp đồng render của đồng hồ đếm ngược (dùng chung cho panel sự kiện
// và panel import): chưa đặt mốc chặn hoặc đã tới mốc thì trả về rỗng (request đã bay); thời gian
// còn lại làm tròn lên giây, giảm dần từng giây và không bao giờ hiện 0s.
func TestRetryCountdown(t *testing.T) {
	now := time.Now()
	if got := retryCountdown(time.Time{}, now); got != "" {
		t.Fatalf("mốc chặn rỗng phải trả về rỗng, được %q", got)
	}
	if got := retryCountdown(now.Add(-time.Second), now); got != "" {
		t.Fatalf("đã tới mốc phải trả về rỗng, được %q", got)
	}
	if got := retryCountdown(now.Add(7500*time.Millisecond), now); got != "Thử lại sau 8s" {
		t.Fatalf("7.5s phải làm tròn lên 8s, được %q", got)
	}
	if got := retryCountdown(now.Add(300*time.Millisecond), now); got != "Thử lại sau 1s" {
		t.Fatalf("dưới 1s phải hiện 1s, được %q", got)
	}
}

// TestParseImportArgsGuide bảo vệ việc phân tích --guide: hướng dẫn bằng ngôn ngữ tự nhiên có thể
// chứa khoảng trắng (mọi token sau đó đều được gộp vào), có thể kết hợp với tuỳ chọn khác
// (đặt cuối), nội dung rỗng thì báo lỗi.
func TestParseImportArgsGuide(t *testing.T) {
	opts, err := parseImportArgs([]string{"--guide=mục·X", "cũng", "là một chương riêng"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Guidance != "mục·X cũng là một chương riêng" {
		t.Fatalf("hướng dẫn có khoảng trắng phải được gộp trọn, được %q", opts.Guidance)
	}
	opts, err = parseImportArgs([]string{"book.txt", "--yes", "--guide=gộp chương mở đầu vào chương một"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.AutoConfirm || opts.SourcePath != "book.txt" || opts.Guidance != "gộp chương mở đầu vào chương một" {
		t.Fatalf("kết quả phân tích khi kết hợp với tuỳ chọn khác không đúng: %+v", opts)
	}
	if _, err := parseImportArgs([]string{"--guide="}); err == nil {
		t.Fatal("--guide rỗng phải báo lỗi")
	}
}
