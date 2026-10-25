package host

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/store"
)

// Đồng sáng tác lạnh: làm rõ nhu cầu từ số 0, sinh ra chỉ dẫn sáng tác cho toàn bộ tác phẩm.
const coCreateSystemPromptZH = `你是一个小说共创助手。你的任务不是直接开始写小说，而是通过多轮简短对话帮助用户澄清创作需求，并持续整理出一段可直接交给创作引擎的中文创作指令。

每一轮回复严格按以下 XML 格式输出，包含四个标签，依次出现，每个标签都必须有正确的开闭标签：

<reply>
给用户看的中文自然回复：先回应用户的输入，再最多提出 1 到 2 个当前最关键的问题。如果信息已足够开始创作，告诉用户可以按 Ctrl+S 开始。
</reply>

<draft>
当前完整的创作指令草稿，使用 Markdown：直接从二级标题开始，例如 "## 主题"、"## 关键要素"、"## 待澄清信息"；用项目符号列出要点。每一轮都要在已有结论上**累积更新**，吸收用户最新意图；即使本轮没有新增也要把完整草稿原样再写一次——不要省略、不要写"（保持上一轮）"之类的占位。
</draft>
` + coCreateProtocolTailZH

// Đồng sáng tác theo giai đoạn: tiểu thuyết đã viết được một phần, quy hoạch hướng đi của "giai đoạn tiếp theo". Phía gọi phải gắn tóm tắt trạng thái truyện hiện tại
// vào sau prompt này (mục "## Trạng thái truyện hiện tại"), để mô hình quy hoạch dựa trên nội dung đã viết.
const stageCoCreateSystemPromptZH = `你是一个小说"阶段共创"助手。这本小说已经写了一部分（进度见下方"当前故事状态"）。用户暂停下来，想和你一起规划"后续阶段"的走向，再继续创作。

你的任务不是续写正文，而是通过多轮简短对话帮用户想清楚后面这一段（接下来若干章 / 下一弧 / 下一卷）要往哪走，并持续整理出一段"后续方向 brief"，供创作引擎据此推进。

铁律：所有建议必须与"当前故事状态"里已发生的剧情、人物、伏笔一致，绝不推翻或忽略已写内容；只规划"后续怎么走"，不重新设计整本书。

每一轮回复严格按以下 XML 格式输出，包含四个标签，依次出现，每个标签都必须有正确的开闭标签：

<reply>
给用户看的中文自然回复：先回应用户的输入，再最多提出 1 到 2 个当前最关键的问题。如果后续方向已足够清晰，告诉用户可以按 Ctrl+S 把方向交给创作引擎、继续创作。
</reply>

<draft>
当前完整的"后续方向 brief"，使用 Markdown：直接从二级标题开始，例如 "## 后续走向"、"## 关键转折"、"## 要收的伏笔"、"## 节奏与篇幅"；用项目符号列出要点。每一轮都要在已有结论上**累积更新**，吸收用户最新意图；即使本轮没有新增也要把完整 brief 原样再写一次——不要省略、不要写"（保持上一轮）"之类的占位。
</draft>
` + coCreateProtocolTailZH

// coCreateProtocolTail là phần đuôi giao thức đầu ra dùng chung cho cả hai chế độ đồng sáng tác (<ready> / <suggestions> + quy tắc đầu ra).
// Hai chế độ chỉ khác ở ngữ cảnh mở đầu và ngữ nghĩa của <draft>, giao thức hoàn toàn giống nhau.
const coCreateProtocolTailZH = `
<ready>false</ready>

<suggestions>
1-3 条"用户接下来可能想说的话"，每行一条以 "- " 开头。这是用户卡壳时的引导，
按数字键填入输入框，用户可再编辑后发送。

要求：
- 站在用户口吻，像用户对你说的话，不要写成助手反问。
- 每条不超过 25 字，多样化句式，避免千篇一律。
- 给倾向 / 选择 / 补充意图，不要一句话替用户写完整设定。
</suggestions>

输出规范：
- 必须使用四个 XML 标签：<reply> / <draft> / <ready> / <suggestions>，每个都必须完整开闭。
- 标签名只能小写英文，不要改写成 <REPLY> / <REWRITE> / <回复> 等任何变体。
- 标签外不要添加任何说明、思考或代码围栏。
- <draft> 内允许多行 Markdown，直接换行书写，不需要任何转义。
- <ready> 只写 true 或 false。信息已足够时填 true。
- <ready>true</ready> 时 <suggestions> 可以为空（保留空标签 <suggestions></suggestions> 即可）。`

// coCreateSystemPromptVI là bản tiếng Việt của prompt đồng sáng tác lạnh.
const coCreateSystemPromptVI = `Bạn là trợ lý đồng sáng tác tiểu thuyết. Nhiệm vụ của bạn không phải bắt đầu viết tiểu thuyết ngay, mà là thông qua nhiều lượt trò chuyện ngắn giúp người dùng làm rõ nhu cầu sáng tác, đồng thời liên tục chỉnh lại một đoạn chỉ dẫn sáng tác có thể đưa thẳng cho công cụ sáng tác.

Mỗi lượt trả lời phải tuân thủ chính xác định dạng XML sau, gồm bốn thẻ, xuất hiện theo thứ tự, mỗi thẻ đều phải có thẻ mở và thẻ đóng đúng:

<reply>
Câu trả lời tự nhiên bằng tiếng Việt dành cho người dùng: trước hết đáp lại đầu vào của người dùng, sau đó hỏi tối đa 1 đến 2 câu hỏi quan trọng nhất ở thời điểm hiện tại. Nếu thông tin đã đủ để bắt đầu sáng tác, hãy bảo người dùng có thể nhấn Ctrl+S để bắt đầu.
</reply>

<draft>
Bản nháp chỉ dẫn sáng tác hiện tại đầy đủ, dùng Markdown: bắt đầu thẳng từ tiêu đề cấp hai, ví dụ "## Chủ đề", "## Yếu tố then chốt", "## Thông tin còn cần làm rõ"; liệt kê các ý bằng gạch đầu dòng. Mỗi lượt phải **cộng dồn cập nhật** dựa trên các kết luận đã có, hấp thụ ý định mới nhất của người dùng; kể cả khi lượt này không có gì mới thì vẫn phải viết lại nguyên vẹn bản nháp — đừng bỏ sót, đừng viết "(giữ nguyên lượt trước)" hay các chỗ trống tương tự.
</draft>
` + coCreateProtocolTailVI

// stageCoCreateSystemPromptVI là bản tiếng Việt của prompt đồng sáng tác theo giai đoạn.
const stageCoCreateSystemPromptVI = `Bạn là trợ lý "đồng sáng tác theo giai đoạn" cho tiểu thuyết. Tiểu thuyết này đã viết được một phần (tiến độ nằm ở "Trạng thái truyện hiện tại" bên dưới). Người dùng đã tạm dừng và muốn cùng bạn quy hoạch hướng đi của "giai đoạn tiếp theo", rồi tiếp tục sáng tác.

Nhiệm vụ của bạn không phải viết tiếp nội dung truyện, mà là thông qua nhiều lượt trò chuyện ngắn giúp người dùng làm rõ đoạn tiếp theo (vài chương kế tiếp / cung kế tiếp / tập kế tiếp) sẽ đi về đâu, đồng thời liên tục chỉnh lại một đoạn "brief hướng đi tiếp theo" để công cụ sáng tác dùng làm căn cứ triển khai.

Luật bất di bất dịch: mọi đề xuất phải nhất quán với cốt truyện, nhân vật và phục bút đã xảy ra trong "Trạng thái truyện hiện tại", tuyệt đối không lật đổ hay bỏ qua nội dung đã viết; chỉ quy hoạch "đi tiếp thế nào", không thiết kế lại toàn bộ tác phẩm.

Mỗi lượt trả lời phải tuân thủ chính xác định dạng XML sau, gồm bốn thẻ, xuất hiện theo thứ tự, mỗi thẻ đều phải có thẻ mở và thẻ đóng đúng:

<reply>
Câu trả lời tự nhiên bằng tiếng Việt dành cho người dùng: trước hết đáp lại đầu vào của người dùng, sau đó hỏi tối đa 1 đến 2 câu hỏi quan trọng nhất ở thời điểm hiện tại. Nếu hướng đi tiếp theo đã đủ rõ, hãy bảo người dùng có thể nhấn Ctrl+S để giao hướng đó cho công cụ sáng tác và tiếp tục sáng tác.
</reply>

<draft>
"brief hướng đi tiếp theo" hiện tại đầy đủ, dùng Markdown: bắt đầu thẳng từ tiêu đề cấp hai, ví dụ "## Hướng đi tiếp theo", "## Bước ngoặt then chốt", "## Manh mối cần thu", "## Nhịp điệu và độ dài"; liệt kê các ý bằng gạch đầu dòng. Mỗi lượt phải **cộng dồn cập nhật** dựa trên các kết luận đã có, hấp thụ ý định mới nhất của người dùng; kể cả khi lượt này không có gì mới thì vẫn phải viết lại nguyên vẹn bản brief — đừng bỏ sót, đừng viết "(giữ nguyên lượt trước)" hay các chỗ trống tương tự.
</draft>
` + coCreateProtocolTailVI

// coCreateProtocolTailVI là phần đuôi giao thức đầu ra dùng chung cho cả hai chế độ.
const coCreateProtocolTailVI = `
<ready>false</ready>

<suggestions>
1-3 câu "người dùng có thể nói tiếp", mỗi dòng một câu và bắt đầu bằng "- ". Đây là gợi ý dẫn dắt khi người dùng không biết nói gì,
nhấn phím số để điền vào ô nhập, người dùng có thể sửa lại rồi gửi.

Yêu cầu:
- Đóng vai người dùng, như người dùng đang nói với bạn, đừng viết thành câu hỏi ngược của trợ lý.
- Mỗi câu không quá 25 chữ, câu đa dạng, tránh sáo lặp.
- Nêu thiên hướng / lựa chọn / bổ sung ý định, đừng viết trọn bộ thiết lập thay người dùng trong một câu.
</suggestions>

Quy tắc đầu ra:
- Bắt buộc dùng bốn thẻ XML: <reply> / <draft> / <ready> / <suggestions>, mỗi thẻ đều phải có thẻ mở và thẻ đóng đầy đủ.
- Tên thẻ chỉ được là chữ thường tiếng Anh, không viết thành <REPLY> / <REWRITE> / <trảlời> hay bất kỳ biến thể nào.
- Ngoài thẻ không thêm bất kỳ giải thích, suy nghĩ hay khối code fence nào.
- Trong <draft> được phép có Markdown nhiều dòng, viết thẳng xuống dòng, không cần escape.
- <ready> chỉ ghi true hoặc false. Khi thông tin đã đủ thì điền true.
- Khi <ready>true</ready> thì <suggestions> có thể để trống (giữ nguyên thẻ rỗng <suggestions></suggestions>).`

// CoCreateProgressKind định danh loại nội dung của callback truyền phát.
const (
	CoCreateProgressThinking = "thinking"
	CoCreateProgressReply    = "reply"
)

// Đầu ra dùng bốn thẻ XML. Kiểu XML bền hơn marker dấu ngoặc vuông — trong dữ liệu huấn luyện của Claude/GPT
// có rất nhiều dạng <thinking>...</thinking>, mô hình hầu như không viết <reply> thành <REWRITE>
// hay biến thể khác; thẻ đóng cũng giúp cắt chính xác giữa chừng khi truyền phát (không phụ thuộc tìm marker kế tiếp để chặn đuôi).
const (
	tagReply       = "reply"
	tagDraft       = "draft"
	tagReady       = "ready"
	tagSuggestions = "suggestions"
)

func coCreateStream(ctx context.Context, models *bootstrap.ModelSet, sessions *store.SessionStore, sysPrompt string, history []CoCreateMessage, onProgress func(kind, text string)) (reply CoCreateReply, err error) {
	if len(history) == 0 {
		return CoCreateReply{}, fmt.Errorf("cocreate history is empty")
	}

	model := models.ForRole("thinking")

	msgs := []agentcore.Message{agentcore.SystemMsg(sysPrompt)}
	for _, item := range history {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.Role)) {
		case "assistant":
			msgs = append(msgs, assistantMsg(content))
		default:
			msgs = append(msgs, agentcore.UserMsg(content))
		}
	}

	var raw, thinking strings.Builder

	// Muốn truy ra các sự cố thoảng qua như "cocreate empty response" thì cần thấy mô hình thực sự trả về gì.
	// Mỗi lượt được ghi trọn vẹn xuống <output>/meta/sessions/cocreate.jsonl, cùng vị trí với nhật ký session của phiên sáng tác chính.
	start := time.Now()
	defer func() {
		if sessions == nil {
			return
		}
		if logErr := sessions.LogCoCreate(coCreateLogEntry{
			Time:         time.Now(),
			DurationMS:   time.Since(start).Milliseconds(),
			InputHistory: history,
			RawResponse:  raw.String(),
			RawLen:       len([]rune(raw.String())),
			Thinking:     thinking.String(),
			ParsedReply:  reply.Message,
			ParsedDraft:  reply.Prompt,
			ParsedReady:  reply.Ready,
			ParsedSugs:   reply.Suggestions,
			Error:        errString(err),
		}); logErr != nil {
			slog.Warn("ghi nhật ký phiên đồng sáng tác thất bại", "module", "cocreate", "err", logErr)
		}
	}()

	streamCh, err := model.GenerateStream(ctx, msgs, nil, agentcore.WithMaxTokens(2048))
	if err != nil {
		return CoCreateReply{}, fmt.Errorf("cocreate generate: %w", err)
	}

	var streamed bool
	for ev := range streamCh {
		switch ev.Type {
		case agentcore.StreamEventThinkingDelta:
			thinking.WriteString(ev.Delta)
			if onProgress != nil {
				onProgress(CoCreateProgressThinking, thinking.String())
			}
		case agentcore.StreamEventTextDelta:
			streamed = true
			raw.WriteString(ev.Delta)
			if onProgress != nil {
				onProgress(CoCreateProgressReply, extractReplyPreview(raw.String()))
			}
		case agentcore.StreamEventDone:
			if !streamed {
				raw.WriteString(ev.Message.TextContent())
			}
		case agentcore.StreamEventError:
			if ev.Err != nil {
				return CoCreateReply{}, fmt.Errorf("cocreate generate: %w", ev.Err)
			}
			return CoCreateReply{}, fmt.Errorf("cocreate generate failed")
		}
	}

	// Channel fallback: mô hình kiểu suy luận (R1/GLM-Z1/QwQ v.v.) đôi khi viết trọn đáp án vào
	// reasoning_content rồi không quay lại kênh final answer, khiến raw rỗng nhưng thinking chứa
	// đủ bốn đoạn. Thực đo thấy trong meta/sessions/cocreate.jsonl — cứ lấy thinking làm raw để phân tích,
	// tầng giao thức đã có xử lý giảm cấp (không có marker [REPLY] thì coi cả đoạn là reply), nên sau khi cứu vãn trải nghiệm UI không khác gì.
	rawText := raw.String()
	if strings.TrimSpace(rawText) == "" {
		if t := strings.TrimSpace(thinking.String()); t != "" {
			rawText = t
		}
	}
	reply, err = parseCoCreateResponse(rawText)
	return reply, err
}

// coCreateLogEntry là cấu trúc một dòng ghi vào meta/sessions/cocreate.jsonl.
// Tên trường bám sát thói quen tra cứu trực tiếp jsonl (snake_case) để tiện lọc bằng jq.
type coCreateLogEntry struct {
	Time         time.Time         `json:"time"`
	DurationMS   int64             `json:"duration_ms"`
	InputHistory []CoCreateMessage `json:"input_history"`
	RawResponse  string            `json:"raw_response"`
	RawLen       int               `json:"raw_len"`
	Thinking     string            `json:"thinking,omitempty"`
	ParsedReply  string            `json:"parsed_reply"`
	ParsedDraft  string            `json:"parsed_draft"`
	ParsedReady  bool              `json:"parsed_ready"`
	ParsedSugs   []string          `json:"parsed_sugs,omitempty"`
	Error        string            `json:"error,omitempty"`
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func assistantMsg(text string) agentcore.Message {
	return agentcore.Message{
		Role:      agentcore.RoleAssistant,
		Content:   []agentcore.ContentBlock{agentcore.TextBlock(text)},
		Timestamp: time.Now(),
	}
}

// parseCoCreateResponse phân tích đầu ra theo thẻ XML. Nếu mô hình không tuân thủ giao thức (viết thẳng bằng văn xuôi),
// cả đoạn hiển thị làm reply, draft để trống để phiên giữ lượt trước.
func parseCoCreateResponse(raw string) (CoCreateReply, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return CoCreateReply{}, fmt.Errorf("cocreate empty response")
	}

	reply, draft, ready, suggestions := splitCoCreateMarkers(raw)
	if reply == "" {
		// Mô hình không tuân thủ giao thức XML: cả đoạn làm reply.
		return CoCreateReply{Message: raw, Prompt: "", Ready: false, Raw: raw}, nil
	}
	return CoCreateReply{
		Message:     reply,
		Prompt:      draft,
		Ready:       ready,
		Suggestions: suggestions,
		Raw:         raw,
	}, nil
}

// splitCoCreateMarkers cắt văn bản theo bốn thẻ XML.
// Thẻ có thể thiếu (giữa chừng khi truyền phát hoặc do mô hình bỏ sót), phần thiếu ứng với trường rỗng / false / nil.
// Khi thiếu thẻ đóng, extractTagContent lấy tới cuối chuỗi và vẫn cố gắng phân tích.
func splitCoCreateMarkers(s string) (reply, draft string, ready bool, suggestions []string) {
	reply = extractTagContent(s, tagReply)
	draft = extractTagContent(s, tagDraft)
	readyStr := strings.ToLower(extractTagContent(s, tagReady))
	ready = readyStr == "true" || readyStr == "yes"
	suggestions = parseSuggestions(extractTagContent(s, tagSuggestions))
	return
}

// extractTagContent bóc phần văn bản giữa <tag>...</tag> trong s.
// Có ba tình huống lỗi thoảng qua cần dự phòng, để không phải rơi thẳng vào đường giảm cấp làm mất trường:
//  1. Có thẻ mở không có thẻ đóng (giữa chừng khi truyền phát) → cắt tới trước thẻ mở đã biết kế tiếp
//  2. Không có thẻ mở nhưng có thẻ đóng (mô hình sai chính tả, ví dụ <suggestions> viết thành <uggestions>) → bắt đầu từ thẻ đóng đầy đủ đã biết gần nhất
//     cho tới trước </tag>
//  3. reply hoàn toàn không có thẻ mở (mô hình mở đầu bằng văn xuôi rồi dán </reply> ở cuối) → từ đầu tới </reply>
func extractTagContent(s, tag string) string {
	open := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	oIdx := strings.Index(s, open)
	if oIdx >= 0 {
		rest := s[oIdx+len(open):]
		if cIdx := strings.Index(rest, closeTag); cIdx >= 0 {
			return strings.TrimSpace(rest[:cIdx])
		}
		// Có thẻ mở không có thẻ đóng → cắt tới trước thẻ mở đã biết kế tiếp
		for _, other := range []string{"<reply>", "<draft>", "<ready>", "<suggestions>"} {
			if other == open {
				continue
			}
			if idx := strings.Index(rest, other); idx >= 0 {
				rest = rest[:idx]
			}
		}
		return strings.TrimSpace(rest)
	}

	// Không có thẻ mở nhưng có thẻ đóng → bắt đầu từ cuối thẻ đóng đầy đủ đã biết gần nhất, tới trước </tag>.
	if cIdx := strings.Index(s, closeTag); cIdx >= 0 {
		prefix := s[:cIdx]
		start := 0
		for _, t := range []string{"</reply>", "</draft>", "</ready>", "</suggestions>"} {
			if t == closeTag {
				continue
			}
			if i := strings.LastIndex(prefix, t); i >= 0 {
				if end := i + len(t); end > start {
					start = end
				}
			}
		}
		return strings.TrimSpace(prefix[start:])
	}
	return ""
}

// parseSuggestions bóc từng dòng trong mục <suggestions> và bỏ tiền tố danh sách như "- " / "* " / "1. ".
// Giữ tối đa 3 mục; bỏ qua dòng trống, dòng quá ngắn (<2 ký tự), dòng trông như thẻ XML (thẻ mở sai chính tả còn sót lại do dự phòng,
// ví dụ <uggestions>).
func parseSuggestions(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Cả dòng trông như thẻ XML → bỏ qua (tránh thẻ mở sai chính tả làm nhiễu)
		if strings.HasPrefix(line, "<") && strings.HasSuffix(line, ">") {
			continue
		}
		// Bóc tiền tố danh sách
		switch {
		case strings.HasPrefix(line, "- "):
			line = strings.TrimSpace(line[2:])
		case strings.HasPrefix(line, "* "):
			line = strings.TrimSpace(line[2:])
		case isOrderedSuggestion(line):
			line = stripOrderedPrefix(line)
		}
		if len([]rune(line)) < 2 {
			continue
		}
		out = append(out, line)
		if len(out) >= 3 {
			break
		}
	}
	return out
}

// isOrderedSuggestion xác định đầu dòng có dạng "1. " / "12. " (số + dấu chấm + khoảng trắng) hay không.
func isOrderedSuggestion(line string) bool {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(line) && line[i] == '.' && line[i+1] == ' '
}

func stripOrderedPrefix(line string) string {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(line) {
		return line
	}
	return strings.TrimSpace(line[i+2:])
}

// extractReplyPreview phục vụ xem trước khi truyền phát: khi raw còn đang lớn dần, đưa cho UI một đoạn văn bản hiển thị được.
// Lấy phần sau <reply> và cắt tới </reply> hoặc trước thẻ mở kế tiếp <draft>.
// Khi mô hình tuân thủ nửa vời (thiếu thẻ mở <reply>), từ đầu tới </reply> hoặc <draft> đều tính là reply.
func extractReplyPreview(raw string) string {
	trimmed := strings.TrimSpace(raw)
	open := "<" + tagReply + ">"
	closeTag := "</" + tagReply + ">"
	draftOpen := "<" + tagDraft + ">"

	rest := trimmed
	if rIdx := strings.Index(trimmed, open); rIdx >= 0 {
		rest = trimmed[rIdx+len(open):]
	}
	if cIdx := strings.Index(rest, closeTag); cIdx >= 0 {
		return strings.TrimSpace(rest[:cIdx])
	}
	if dIdx := strings.Index(rest, draftOpen); dIdx >= 0 {
		rest = rest[:dIdx]
	}
	return strings.TrimSpace(rest)
}
