package utils

import "strings"

// JSONFieldExtractor trích xuất giá trị chuỗi của một trường chỉ định từ các mảnh JSON đến luồng.

// Khi LLM sinh tool call theo luồng, tham số đến dần từng mảnh (OpenAI/Anthropic)
// hoặc đến một lần (Gemini). Bộ trích này quét từng ký tự bằng máy trạng thái,
// phát hiện key đích thì lấy giá trị chuỗi của nó, đồng thời xử lý escape của JSON.
type JSONFieldExtractor struct {
	key      string // key đích cần khớp, ví dụ `"content"` hoặc `"task"`
	state    extractState
	matchPos int
	escape   bool
	buf      strings.Builder
}

type extractState int

const (
	stateScan    extractState = iota // đang quét, tìm key đích
	stateColon                       // đã khớp key, chờ dấu hai chấm và dấu nháy mở đầu
	stateExtract                     // đang lấy giá trị chuỗi
)

func NewFieldExtractor(fieldName string) *JSONFieldExtractor {
	return &JSONFieldExtractor{key: `"` + fieldName + `"`}
}

// Feed xử lý một đoạn delta, trả về văn bản đã trích (có thể rỗng).
func (e *JSONFieldExtractor) Feed(delta string) string {
	e.buf.Reset()
	for _, r := range delta {
		switch e.state {
		case stateScan:
			e.feedScan(r)
		case stateColon:
			e.feedColon(r)
		case stateExtract:
			e.feedExtract(r)
		}
	}
	return e.buf.String()
}

func (e *JSONFieldExtractor) feedScan(r rune) {
	if e.matchPos < len(e.key) && byte(r) == e.key[e.matchPos] {
		e.matchPos++
		if e.matchPos == len(e.key) {
			e.state = stateColon
			e.matchPos = 0
		}
		return
	}
	e.matchPos = 0
	if byte(r) == e.key[0] {
		e.matchPos = 1
	}
}

func (e *JSONFieldExtractor) feedColon(r rune) {
	switch r {
	case ':', ' ', '\t':
		// Bỏ qua
	case '"':
		e.state = stateExtract
		e.escape = false
	default:
		e.state = stateScan
		e.matchPos = 0
		if byte(r) == e.key[0] {
			e.matchPos = 1
		}
	}
}

func (e *JSONFieldExtractor) feedExtract(r rune) {
	if e.escape {
		e.escape = false
		switch r {
		case 'n':
			e.buf.WriteByte('\n')
		case 't':
			e.buf.WriteByte('\t')
		case 'r':
			e.buf.WriteByte('\r')
		case '"', '\\', '/':
			e.buf.WriteRune(r)
		default:
			e.buf.WriteByte('\\')
			e.buf.WriteRune(r)
		}
		return
	}
	switch r {
	case '\\':
		e.escape = true
	case '"':
		e.state = stateScan
		e.matchPos = 0
	default:
		e.buf.WriteRune(r)
	}
}

// Reset đặt lại trạng thái (gọi khi bắt đầu lượt message LLM mới).
func (e *JSONFieldExtractor) Reset() {
	e.state = stateScan
	e.matchPos = 0
	e.escape = false
}

// ThinkingSep là dấu phân cách giữa văn bản suy luận và nội dung.
// StreamFilter chèn dấu này trước mỗi đoạn suy luận, TUI dựa vào đó để đổi kiểu hiển thị.
const ThinkingSep = "\x02"

// StreamFilter phân biệt câu trả lời văn bản của SubAgent với lời gọi công cụ dạng JSON.
// Câu trả lời văn bản được đánh dấu là nội dung suy luận (tiền tố ThinkingSep); lời gọi công cụ
// dạng JSON chỉ trích xuất trường chỉ định.

// Căn cứ: gặp { thì vào chế độ JSON (theo dõi độ sâu dấu ngoặc nhọn),
// khi độ sâu về 0 thì trở lại chế độ văn bản.
type StreamFilter struct {
	fieldExt   *JSONFieldExtractor
	mode       filterMode
	braceDepth int
	inString   bool // đang trong chuỗi JSON (dấu ngoặc nhọn không được đếm)
	escJSON    bool // đang trong escape của chuỗi JSON
	thinking   bool // hiện đang ở đoạn văn bản suy luận
	buf        strings.Builder
}

type filterMode int

const (
	filterText filterMode = iota // câu trả lời văn bản, truyền thẳng qua
	filterJSON                   // lời gọi công cụ dạng JSON, trích trường đích
)

func NewStreamFilter(fieldName string) *StreamFilter {
	return &StreamFilter{fieldExt: NewFieldExtractor(fieldName)}
}

// Feed xử lý một đoạn delta, trả về văn bản có thể hiển thị.
// Câu trả lời văn bản được xuất thẳng; giá trị của trường đích trong JSON được trích ra để xuất;
// phần cấu trúc JSON còn lại bị bỏ.
func (f *StreamFilter) Feed(delta string) string {
	f.buf.Reset()
	for _, r := range delta {
		switch f.mode {
		case filterText:
			if r == '{' {
				f.thinking = false
				f.mode = filterJSON
				f.braceDepth = 1
				f.inString = false
				f.escJSON = false
				f.fieldExt.Reset()
				f.feedExtractor(r)
			} else {
				if !f.thinking {
					f.thinking = true
					f.buf.WriteString(ThinkingSep)
				}
				f.buf.WriteRune(r)
			}
		case filterJSON:
			f.feedExtractor(r)
			f.trackBraces(r)
		}
	}
	return f.buf.String()
}

// feedExtractor đưa từng ký tự vào fieldExt, kết quả trích được ghi vào buf.
func (f *StreamFilter) feedExtractor(r rune) {
	if text := f.fieldExt.Feed(string(r)); text != "" {
		f.buf.WriteString(text)
	}
}

// trackBraces theo dõi độ sâu dấu ngoặc nhọn của JSON, khi độ sâu về 0 thì chuyển lại chế độ văn bản.
func (f *StreamFilter) trackBraces(r rune) {
	if f.escJSON {
		f.escJSON = false
		return
	}
	if f.inString {
		switch r {
		case '\\':
			f.escJSON = true
		case '"':
			f.inString = false
		}
		return
	}
	switch r {
	case '"':
		f.inString = true
	case '{':
		f.braceDepth++
	case '}':
		f.braceDepth--
		if f.braceDepth <= 0 {
			f.mode = filterText
		}
	}
}

// Reset đặt lại trạng thái.
func (f *StreamFilter) Reset() {
	f.mode = filterText
	f.braceDepth = 0
	f.inString = false
	f.escJSON = false
	f.thinking = false
	f.fieldExt.Reset()
}
