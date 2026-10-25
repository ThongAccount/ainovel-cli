package host

import (
	"strings"
	"unicode/utf8"
)

// toolDisplays cấu hình chiến lược hiển thị của từng công cụ trên bảng luồng. Công cụ
// không có trong bảng này sẽ không tham gia dựng dạng luồng
// (observer bỏ thẳng DeltaToolCall).
//
// Nhãn tiêu đề không nằm trong bảng này: nó phụ thuộc ngôn ngữ tác phẩm nên được
// tra từ bảng văn bản quan sát (xem observer_texts.go) lúc tạo extractor.
//
// Chế độ chung (nakedKey rỗng): tokenizer biến args JSON do LLM xuất thành văn bản
// "key: value" có thụt lề, object/mảng lồng nhau thụt lề theo tầng,
// string/number/bool hiển thị dạng luồng.
// Hoàn toàn tách khỏi schema — LLM thừa ra một trường là trên bảng xuất hiện thêm
// một dòng, không cần sửa mã.
//
// Chế độ luồng trần (nakedKey khác rỗng): chỉ trả nguyên văn giá trị chuỗi của
// trường cấp ngoài cùng đích, mọi trường khác đều bị bỏ qua. Dùng cho
// draft_chapter để markdown của cả chương không bị trang trí thành "content: # …".
// header luôn bắt đầu bằng "✻ ": đây là tiền tố quy ước để TUI renderStreamContent
// đi theo đường dựng nổi bật renderAgentBlock (✻ vàng + label nền xanh chân trời có
// gạch chân + vạch ngăn mờ), giống hệt fallback header (streamHeaderFallback); nếu
// đổi thành chữ thường sẽ rơi vào đường nội dung dùng màu mặc định của terminal và
// tiêu đề không còn nổi bật.
var toolDisplays = map[string]toolDisplay{
	"draft_chapter": {nakedKey: "content"},

	"plan_chapter":        {},
	"edit_chapter":        {},
	"commit_chapter":      {},
	"save_review":         {},
	"save_arc_summary":    {},
	"save_volume_summary": {},
	"save_foundation":     {},
	"revise_outline":      {},
	"read_chapter":        {},
	"check_consistency":   {},
	"novel_context":       {},
}

// toolDisplay mô tả cách trình bày args JSON của một công cụ trên bảng luồng.
type toolDisplay struct {
	nakedKey string
}

// jsonFieldExtractor là tokenizer JSON dạng luồng. Nó dẫn máy trạng thái từng byte,
// biến luồng args công cụ của LLM thành văn bản dễ đọc. Một thể hiện chỉ phục vụ
// một lần gọi công cụ, sau khi container cấp ngoài cùng đóng thì Done()=true.
type jsonFieldExtractor struct {
	cfg    toolDisplay
	header string // nhãn tiêu đề theo ngôn ngữ tác phẩm; rỗng nếu công cụ không có nhãn

	state pState
	stack []byte // ngăn xếp container: 'O' obj / 'A' arr

	keyBuf strings.Builder

	escape bool
	uHex   []byte

	started bool // đã emit ký tự nào chưa (dùng cho xuống dòng giữa header và key đầu tiên)

	done bool
}

type pState int

const (
	psRoot         pState = iota
	psBeforeKey           // trong obj: chờ key tiếp theo hoặc }
	psInKey               // trong obj: phân tích key
	psAfterKey            // trong obj: chờ :
	psBeforeValue         // chờ ký tự bắt đầu của value
	psStringStream        // giá trị string, emit cooked theo luồng
	psStringSkip          // giá trị string, bỏ qua (trường không phải đích ở chế độ luồng trần)
	psNumberStream        // số, emit theo luồng
	psNumberSkip          // số, bỏ qua
	psPrimStream          // true/false/null, emit theo luồng
	psPrimSkip            // true/false/null, bỏ qua
	psDone                // container cấp ngoài cùng đã đóng
)

// newToolExtractor tạo bộ trích cho một công cụ theo bảng văn bản đã chọn.
// Nhãn tiêu đề lấy theo ngôn ngữ tác phẩm; trả về nil nếu công cụ không có trong
// bảng hiển thị.
func newToolExtractor(tool string, texts *observerTexts) *jsonFieldExtractor {
	cfg, ok := toolDisplays[tool]
	if !ok {
		return nil
	}
	if texts == nil {
		texts = observerTextsVI
	}
	return &jsonFieldExtractor{cfg: cfg, header: texts.toolHeader(tool)}
}

func (e *jsonFieldExtractor) Done() bool { return e.done }

func (e *jsonFieldExtractor) Feed(chunk string) string {
	if e.done || chunk == "" {
		return ""
	}
	var out strings.Builder
	for i := 0; i < len(chunk); i++ {
		e.step(chunk[i], &out)
		if e.done {
			break
		}
	}
	return out.String()
}

// ── Ngăn xếp container / Thụt lề ──

func (e *jsonFieldExtractor) push(kind byte) {
	e.stack = append(e.stack, kind)
}

func (e *jsonFieldExtractor) pop() {
	if len(e.stack) == 0 {
		return
	}
	e.stack = e.stack[:len(e.stack)-1]
}

func (e *jsonFieldExtractor) parent() byte {
	if len(e.stack) == 0 {
		return 0
	}
	return e.stack[len(e.stack)-1]
}

// writeIndent ghi khoảng thụt lề hiện tại. Độ sâu = số tầng lồng = len(stack)-1
// (bên trong container gốc không thụt lề).
func (e *jsonFieldExtractor) writeIndent(out *strings.Builder) {
	depth := len(e.stack) - 1
	for range depth {
		out.WriteString("  ")
	}
}

// ── Máy trạng thái ──

func (e *jsonFieldExtractor) step(c byte, out *strings.Builder) {
	switch e.state {
	case psRoot:
		switch c {
		case '{':
			e.push('O')
			e.state = psBeforeKey
		case '[':
			// thực tế không xảy ra (tool args luôn là obj); vẫn chấp nhận: khi root là arr
			e.push('A')
			e.state = psBeforeValue
		}
	case psBeforeKey:
		switch c {
		case '"':
			e.keyBuf.Reset()
			e.escape = false
			e.state = psInKey
		case '}':
			e.closeContainer(out)
		case ' ', '\t', '\n', '\r', ',':
		}
	case psInKey:
		if e.escape {
			e.keyBuf.WriteByte(c)
			e.escape = false
			return
		}
		if c == '\\' {
			e.escape = true
			return
		}
		if c == '"' {
			e.emitKeyLine(out, e.keyBuf.String())
			e.state = psAfterKey
			return
		}
		e.keyBuf.WriteByte(c)
	case psAfterKey:
		if c == ':' {
			e.state = psBeforeValue
		}
	case psBeforeValue:
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' {
			return
		}
		switch c {
		case '"':
			e.beginString(out)
		case '{':
			e.beginNested('O', out)
		case '[':
			e.beginNested('A', out)
		case ']', '}':
			e.closeContainer(out)
		case 't', 'f', 'n':
			e.beginPrim(c, out)
		default:
			if c == '-' || (c >= '0' && c <= '9') {
				e.beginNumber(c, out)
			}
		}
	case psStringStream:
		e.handleStringByte(c, out, false)
	case psStringSkip:
		e.handleStringByte(c, out, true)
	case psNumberStream:
		if isNumberByte(c) {
			out.WriteByte(c)
			return
		}
		e.afterValueChar(c, out)
	case psNumberSkip:
		if isNumberByte(c) {
			return
		}
		e.afterValueChar(c, out)
	case psPrimStream:
		if c >= 'a' && c <= 'z' {
			out.WriteByte(c)
			return
		}
		e.afterValueChar(c, out)
	case psPrimSkip:
		if c >= 'a' && c <= 'z' {
			return
		}
		e.afterValueChar(c, out)
	case psDone:
	}
}

// ── Dựng dòng ──

// emitKeyLine được gọi khi đã phân tích xong key trong obj, ghi ra tiền tố
// "<lf><indent>key:".
// Ở chế độ luồng trần không ghi tiền tố key (key được lưu trong keyBuf để
// beginString đối chiếu).
func (e *jsonFieldExtractor) emitKeyLine(out *strings.Builder, key string) {
	if e.cfg.nakedKey != "" {
		return
	}
	if !e.started {
		if e.header != "" {
			out.WriteString(e.header)
			out.WriteByte('\n')
		}
		e.started = true
	} else {
		out.WriteByte('\n')
	}
	e.writeIndent(out)
	out.WriteString(key)
	out.WriteByte(':')
}

// emitArrayItem được gọi ở đầu mỗi phần tử trong arr, ghi ra "<lf><indent>-". Phần tử
// primitive được emit giá trị ngay sau một khoảng trắng; phần tử struct được xử lý
// bằng các lần lồng sau, vốn tự sinh xuống dòng.
func (e *jsonFieldExtractor) emitArrayItem(out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		return
	}
	if !e.started {
		if e.header != "" {
			out.WriteString(e.header)
			out.WriteByte('\n')
		}
		e.started = true
	} else {
		out.WriteByte('\n')
	}
	e.writeIndent(out)
	out.WriteByte('-')
}

// ── Bắt đầu value ──

func (e *jsonFieldExtractor) beginString(out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		// Luồng trần: chỉ giá trị string của key đích trong obj cấp ngoài cùng mới được xuất
		if e.cfg.nakedKey == e.keyBuf.String() && len(e.stack) == 1 && e.stack[0] == 'O' {
			e.state = psStringStream
		} else {
			e.state = psStringSkip
		}
		e.escape = false
		e.uHex = nil
		return
	}
	// Chế độ chung: field của obj bám ngay sau "key: " (đã emit "key:", thêm khoảng
	// trắng); phần tử arr bám ngay sau "- "
	if e.parent() == 'A' {
		e.emitArrayItem(out)
		out.WriteByte(' ')
	} else {
		out.WriteByte(' ')
	}
	e.state = psStringStream
	e.escape = false
	e.uHex = nil
}

func (e *jsonFieldExtractor) beginNumber(first byte, out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		e.state = psNumberSkip
		return
	}
	if e.parent() == 'A' {
		e.emitArrayItem(out)
		out.WriteByte(' ')
	} else {
		out.WriteByte(' ')
	}
	out.WriteByte(first)
	e.state = psNumberStream
}

func (e *jsonFieldExtractor) beginPrim(first byte, out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		e.state = psPrimSkip
		return
	}
	if e.parent() == 'A' {
		e.emitArrayItem(out)
		out.WriteByte(' ')
	} else {
		out.WriteByte(' ')
	}
	out.WriteByte(first)
	e.state = psPrimStream
}

func (e *jsonFieldExtractor) beginNested(kind byte, out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		// Chế độ luồng trần không mở rộng phần lồng nhau; dùng độ sâu ngăn xếp để theo dõi
		// tới } / ] khớp
		e.push(kind)
		if kind == 'O' {
			e.state = psBeforeKey
		} else {
			e.state = psBeforeValue
		}
		return
	}
	// Chế độ chung: khi phần tử arr là cấu trúc lồng nhau, trước tiên emit riêng một
	// dòng "<indent>-"
	// (sau ":" của key obj không có khoảng trắng, để các key con lồng nhau tự xuống
	// dòng kế tiếp)
	if e.parent() == 'A' {
		e.emitArrayItem(out)
	}
	e.push(kind)
	if kind == 'O' {
		e.state = psBeforeKey
	} else {
		e.state = psBeforeValue
	}
}

// closeContainer xử lý } hoặc ].
func (e *jsonFieldExtractor) closeContainer(out *strings.Builder) {
	e.pop()
	if len(e.stack) == 0 {
		// Xử lý dự phòng cho args rỗng (ví dụ novel_context không truyền tham số):
		// emitKeyLine không có cơ hội xuất header, ở đây bổ sung một lần để tránh
		// rơi vào trạng thái "không tiêu đề lẫn không nội dung".
		if !e.started && e.cfg.nakedKey == "" && e.header != "" {
			out.WriteString(e.header)
			out.WriteByte('\n')
			e.started = true
		}
		// Xuống dòng kết thúc để bảng có ranh giới rõ ràng với đoạn xuất kế tiếp
		if e.started {
			out.WriteByte('\n')
		}
		e.state = psDone
		e.done = true
		return
	}
	if e.parent() == 'O' {
		e.state = psBeforeKey
	} else {
		e.state = psBeforeValue
	}
}

// ── String dạng luồng ──

func (e *jsonFieldExtractor) handleStringByte(c byte, out *strings.Builder, skipping bool) {
	if e.uHex != nil {
		e.uHex = append(e.uHex, c)
		if len(e.uHex) == 4 {
			if r, ok := parseHex4(e.uHex); ok && !skipping {
				var buf [4]byte
				n := utf8.EncodeRune(buf[:], r)
				out.Write(buf[:n])
			}
			e.uHex = nil
		}
		return
	}
	if e.escape {
		e.escape = false
		if !skipping {
			writeEscapedByte(out, c)
		}
		if c == 'u' {
			e.uHex = make([]byte, 0, 4)
		}
		return
	}
	if c == '\\' {
		e.escape = true
		return
	}
	if c == '"' {
		e.afterValueDone()
		return
	}
	if !skipping {
		out.WriteByte(c)
	}
}

func writeEscapedByte(out *strings.Builder, c byte) {
	switch c {
	case 'n':
		out.WriteByte('\n')
	case 't':
		out.WriteByte('\t')
	case 'r':
		out.WriteByte('\r')
	case '"':
		out.WriteByte('"')
	case '\\':
		out.WriteByte('\\')
	case '/':
		out.WriteByte('/')
	case 'b', 'f':
		// backspace / form feed: bỏ qua
	case 'u':
		// bộ đệm uHex do bên gọi tạo; ở đây không xuất
	default:
		out.WriteByte('\\')
		out.WriteByte(c)
	}
}

// ── Kết thúc ──

// afterValueDone chuyển sang trạng thái kế tiếp sau khi string đóng (đọc tới dấu `"` ở cuối).
func (e *jsonFieldExtractor) afterValueDone() {
	e.escape = false
	e.uHex = nil
	if len(e.stack) == 0 {
		e.state = psDone
		e.done = true
		return
	}
	if e.parent() == 'O' {
		e.state = psBeforeKey
	} else {
		e.state = psBeforeValue
	}
}

// afterValueChar quyết định trạng thái kế tiếp theo ký tự khi "ký tự kết thúc" của
// number / primitive đã được đọc.
// Ký tự này có thể là , / } / ] / khoảng trắng, và hàm này chuyển tiếp để phân tán.
func (e *jsonFieldExtractor) afterValueChar(c byte, out *strings.Builder) {
	switch c {
	case '}', ']':
		e.closeContainer(out)
	case ',', ' ', '\t', '\n', '\r':
		if len(e.stack) == 0 {
			e.state = psDone
			e.done = true
			return
		}
		if e.parent() == 'O' {
			e.state = psBeforeKey
		} else {
			e.state = psBeforeValue
		}
	}
}

// ── Tiện ích ──

func isNumberByte(c byte) bool {
	switch c {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
		'-', '+', '.', 'e', 'E':
		return true
	}
	return false
}

func parseHex4(b []byte) (rune, bool) {
	var r rune
	for _, d := range b {
		var v rune
		switch {
		case d >= '0' && d <= '9':
			v = rune(d - '0')
		case d >= 'a' && d <= 'f':
			v = rune(d-'a') + 10
		case d >= 'A' && d <= 'F':
			v = rune(d-'A') + 10
		default:
			return 0, false
		}
		r = r*16 + v
	}
	return r, true
}
