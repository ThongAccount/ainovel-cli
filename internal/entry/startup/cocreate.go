package startup

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/host"
)

// CoCreateSession chứa trạng thái ngoài UI của chế độ đồng sáng tác.
type CoCreateSession struct {
	history        []host.CoCreateMessage
	draftPrompt    string
	ready          bool
	streamReply    string
	streamThinking string
	suggestions    []string
}

func NewCoCreateSession(initial string) *CoCreateSession {
	return &CoCreateSession{
		history: []host.CoCreateMessage{
			{Role: "user", Content: strings.TrimSpace(initial)},
		},
	}
}

func (s *CoCreateSession) History() []host.CoCreateMessage {
	if s == nil {
		return nil
	}
	return append([]host.CoCreateMessage(nil), s.history...)
}

func (s *CoCreateSession) ApplyReply(reply host.CoCreateReply) {
	if s == nil {
		return
	}
	s.streamReply = ""
	s.streamThinking = ""
	// Trong history, phía assistant lưu đủ ba đoạn Raw (kèm [DRAFT]), nhờ đó lượt sau
	// mô hình mới thấy được bản nháp nó vừa viết và tiếp tục tích luỹ trên nền đó;
	// nếu chỉ lưu Message thì [DRAFT] hoàn toàn không vào ngữ cảnh, mô hình mỗi lượt
	// chỉ còn tự tổng hợp lại từ hội thoại và dễ đánh mất chi tiết sớm.
	// Ở nhánh giảm cấp thì Raw == Message nên tương đương.
	text := strings.TrimSpace(reply.Raw)
	if text == "" {
		text = strings.TrimSpace(reply.Message)
	}
	if text != "" {
		s.history = append(s.history, host.CoCreateMessage{Role: "assistant", Content: text})
	}
	// Chỉ ghi đè draft khi Prompt khác rỗng: nhánh parse giảm cấp trả về Prompt="",
	// lúc đó phải giữ lại draft của lượt trước, nếu không "chỉ dẫn sáng tác hiện tại"
	// mà người dùng đã tích luỹ sẽ bị câu trả lời bị cắt xoá sạch.
	if prompt := strings.TrimSpace(reply.Prompt); prompt != "" {
		s.draftPrompt = prompt
	}
	s.ready = reply.Ready
	// suggestions ghi đè thẳng (kể cả khi ghi đè bằng danh sách rỗng): gợi ý của mỗi lượt
	// chỉ có ý nghĩa cho thời điểm hiện tại.
	s.suggestions = append(s.suggestions[:0], reply.Suggestions...)
}

func (s *CoCreateSession) AppendUser(text string) {
	if s == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	// Người dùng đã quyết định câu tiếp theo sẽ nói gì, suggestions lập tức mất hiệu lực,
	// tránh gợi ý cũ vẫn treo ở ô nhập và gây hiểu nhầm khi AI còn chưa trả lời.
	s.suggestions = nil
	s.history = append(s.history, host.CoCreateMessage{Role: "user", Content: text})
}

// ApplyDelta nhận phần tích luỹ từ luồng; kind="thinking" ghi vào luồng suy luận,
// "reply" ghi vào bản xem trước của câu trả lời.
// Hai luồng tích luỹ riêng, UI có thể tô màu theo từng khối để người dùng thấy LLM
// đang làm việc ngay trong giai đoạn thinking.
func (s *CoCreateSession) ApplyDelta(kind, text string) {
	if s == nil {
		return
	}
	text = strings.TrimSpace(text)
	switch kind {
	case host.CoCreateProgressThinking:
		s.streamThinking = text
	case host.CoCreateProgressReply:
		s.streamReply = text
	}
}

func (s *CoCreateSession) StreamReply() string {
	if s == nil {
		return ""
	}
	return s.streamReply
}

func (s *CoCreateSession) StreamThinking() string {
	if s == nil {
		return ""
	}
	return s.streamThinking
}

func (s *CoCreateSession) DraftPrompt() string {
	if s == nil {
		return ""
	}
	return s.draftPrompt
}

func (s *CoCreateSession) Suggestions() []string {
	if s == nil {
		return nil
	}
	return s.suggestions
}

func (s *CoCreateSession) Ready() bool {
	if s == nil {
		return false
	}
	return s.ready
}

func (s *CoCreateSession) CanStart() bool {
	return strings.TrimSpace(s.DraftPrompt()) != ""
}

func (s *CoCreateSession) InitialInput() string {
	if s == nil || len(s.history) == 0 {
		return ""
	}
	return strings.TrimSpace(s.history[0].Content)
}

func (s *CoCreateSession) BuildPrompt() (string, error) {
	if s == nil || !s.CanStart() {
		return "", fmt.Errorf("cocreate draft prompt is required")
	}
	return s.DraftPrompt(), nil
}
