package diag

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/store"
)

// SkelEvent là khung hành vi của một thông điệp phiên sau khi khử nhạy cảm: giữ lại
// tín hiệu cấu trúc (vai trò / tool / lỗi / dấu vân tay lặp), mọi văn bản tự do
// (văn bản chính, prompt, suy nghĩ) đều bị làm mờ. Đây là một lớp chiếu chặt hơn
// store.compactMessage — lớp kia nén theo dung lượng (>4KB), còn ở đây không xét
// dung lượng, không văn bản nào lọt ra ngoài.
type SkelEvent struct {
	Agent    string     // phiên nguồn: writer-ch07 / architect-arc02 …
	Role     string     // assistant / tool / user
	Tools    []SkelTool // các lệnh tool trong thông điệp
	ErrClass string     // role=tool và is_error: dòng lỗi đầu (chuỗi lỗi khung, không chứa văn bản chính)
	TextSha  string     // hash ngắn của văn bản đã làm mờ; cùng sha = lặp lại sinh cùng một đoạn (tín hiệu vòng lặp)
	Redacted int        // số khối văn bản/suy nghĩ đã làm mờ trong mục này (dùng cho tự kiểm tra khử nhạy cảm)
}

// SkelTool là chiếu khử nhạy cảm của một lệnh tool.
type SkelTool struct {
	Name     string            // tên tool (tín hiệu cấu trúc, không chứa văn bản chính)
	Args     map[string]string // key → giá trị gốc dạng vô hướng / chuỗi ngắn có dấu nháy / "<redacted len sha>"
	Invalid  bool              // ArgsInvalid: tham số model gửi lên không phân tích được (tín hiệu #34)
	ParseErr string            // ArgsParseError: lý do phân tích thất bại
}

// redactMessage chiếu một agentcore.Message thành khung hành vi.
func redactMessage(agent string, m agentcore.Message) SkelEvent {
	ev := SkelEvent{Agent: agent, Role: string(m.Role)}
	isErr, _ := m.Metadata["is_error"].(bool)

	var text strings.Builder
	for _, b := range m.Content {
		switch b.Type {
		case agentcore.ContentText:
			// Kết quả lỗi của tool chỉ giữ dòng đầu: đó là chuỗi lỗi do chính ta phát ra
			// (ví dụ InputValidationError), không chứa văn bản chính và là mấu chốt để định
			// vị vòng lặp. Mọi văn bản còn lại đều vào bể làm mờ.
			if m.Role == agentcore.RoleTool && isErr && ev.ErrClass == "" {
				ev.ErrClass = firstLine(b.Text, 160)
				continue
			}
			if strings.TrimSpace(b.Text) != "" {
				text.WriteString(b.Text)
				ev.Redacted++
			}
		case agentcore.ContentThinking:
			if strings.TrimSpace(b.Thinking) != "" {
				text.WriteString(b.Thinking)
				ev.Redacted++
			}
		case agentcore.ContentToolCall:
			if b.ToolCall != nil {
				ev.Tools = append(ev.Tools, redactToolCall(b.ToolCall))
			}
		}
	}
	if t := text.String(); t != "" {
		ev.TextSha = shortHash(t)
	}
	return ev
}

// redactToolCall chiếu một lệnh tool: tên tool + tham số (giá trị đã khử nhạy cảm) + cờ lỗi phân tích.
func redactToolCall(tc *agentcore.ToolCall) SkelTool {
	return SkelTool{
		Name:     tc.Name,
		Args:     redactArgs(tc.Args),
		Invalid:  tc.ArgsInvalid,
		ParseErr: tc.ArgsParseError,
	}
}

// redactArgs chiếu đối tượng tham số tool thành key → giá trị đã khử nhạy cảm. Tham số không phải
// đối tượng trả về nil (ArgsInvalid/ParseErr đã được ghi riêng trong SkelTool).
func redactArgs(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = projectValue(v)
	}
	return out
}

// projectValue chiếu một giá trị tham số theo kiểu JSON:
//   - giá trị vô hướng (số / bool / null): bản thân giá trị đã là tín hiệu cấu trúc, giữ lại (chapter: 7)
//   - chuỗi ngắn kiểu định danh: giữ kèm dấu nháy để lộ kiểu (chapter: "7" ← tín hiệu số bị
//     chuyển thành chuỗi của #34)
//   - chuỗi chứa tiếng Trung / khoảng trắng / văn bản dài, đối tượng, mảng: làm mờ thành
//     <redacted …> (văn bản chính không lọt ra ngoài)
//   - đã là nhãn giữ chỗ [session_compact: …]: an toàn và có thông tin, giữ nguyên
func projectValue(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return ""
	}
	switch s[0] {
	case '"':
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return redactPlaceholder(s)
		}
		if strings.HasPrefix(str, store.CompactTag) {
			return str
		}
		// Chỉ giữ giá trị ngắn "có vẻ là định danh / số / enum" (chapter:"7", type:"premise",
		// agent:"writer"); mọi chuỗi chứa tiếng Trung, khoảng trắng hoặc ký hiệu khác đều
		// được coi là văn bản chính và làm mờ.
		if utf8.RuneCountInString(str) <= 32 && isStructuralToken(str) {
			return strconv.Quote(str)
		}
		return redactPlaceholder(str)
	case '{':
		return fmt.Sprintf("<redacted object len=%d>", len(raw))
	case '[':
		return fmt.Sprintf("<redacted array len=%d>", len(raw))
	default:
		return s
	}
}

// isStructuralToken kiểm tra chuỗi có "giống định danh" hay không — chỉ chữ cái / số ASCII
// thuần và `_-.:/`, không khoảng trắng, không tiếng Trung. Dùng để tách tín hiệu cấu trúc
// (giữ lại) khỏi đoạn văn bản chính (làm mờ).
func isStructuralToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.' || r == ':' || r == '/':
		default:
			return false
		}
	}
	return true
}

func redactPlaceholder(s string) string {
	return fmt.Sprintf("<redacted len=%d sha=%s>", utf8.RuneCountInString(s), shortHash(s))
}

// shortHash lấy hash ngắn của văn bản; chỉ dùng để đánh giá "cùng một đoạn lặp lại", không phải mục đích mã hóa.
func shortHash(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}

// firstLine lấy dòng đầu và cắt theo rune, để tóm tắt chuỗi lỗi.
func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		s = s[:i]
	}
	if utf8.RuneCountInString(s) > max {
		r := []rune(s)
		s = string(r[:max]) + "…"
	}
	return s
}
