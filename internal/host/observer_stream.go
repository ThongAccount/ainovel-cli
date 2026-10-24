package host

import (
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/utils"
)

// handleSubagentDelta phân luồng văn bản và tham số gọi công cụ của subagent:
// - DeltaText được trả ra thẳng dưới dạng markdown
// - DeltaToolCall chỉ rút trường ra cho công cụ có nội dung dài đã biết (ví dụ draft_chapter.content), tham số JSON của các công cụ khác bị bỏ qua
func (o *observer) handleSubagentDelta(p *agentcore.ProgressPayload) {
	if p.DeltaKind != agentcore.DeltaToolCall {
		o.emitStreamDelta(p.Delta, false)
		return
	}
	if p.Tool == "" {
		return // tên công cụ chưa sẵn sàng, thử lại ở delta kế tiếp
	}

	// Khi nhận diện được tên công cụ trong luồng, phát sớm sự kiện TOOL đang chạy để
	// spinner phủ kín cả khoảng thời gian LLM đang sinh
	// (nếu không, trạng thái "đang chạy" của các công cụ như draft_chapter chỉ hiện
	// trong vài chục mili giây của lúc Execute thật).
	// Khi ProgressToolStart thật sự đến, toolStarts đã có bản ghi nên chỉ bổ sung summary.
	o.ensureSubagentToolStarted(p.Agent, p.Tool)
	o.updateToolCallSummaryFromDelta(p.Agent, p.Tool, p.Delta)

	cur, ok := o.streamExtractors[p.Agent]
	// Sau khi args của cùng một lần gọi công cụ đã đóng (khớp `}` ở cấp ngoài cùng),
	// vẫn có thể nhận được delta đuôi:
	// một số provider (đo thực tế với deepseek-v4-flash) chia một lần args thành
	// nhiều chunk, chunk cuối còn kèm khoảng trắng hoặc ký tự lặp sau `}`. Lúc này nếu
	// xử lý theo kiểu "khớp tên công cụ + Done là dựng lại", extractor mới sẽ emit
	// thêm một ✻ header và coi đoạn token đuôi là args mới để phân tích.
	// Các delta này chỉ là phần đuôi thừa, bỏ đi là được.
	if ok && cur.tool == p.Tool && cur.ext.Done() {
		return
	}
	// Tên công cụ đổi hoặc chưa từng tạo: tạo mới.
	if !ok || cur.tool != p.Tool {
		ext := newToolExtractor(p.Tool, o.texts())
		if ext == nil {
			delete(o.streamExtractors, p.Agent)
			return
		}
		cur = &agentExtractor{tool: p.Tool, ext: ext}
		o.streamExtractors[p.Agent] = cur
	}
	if emitted := cur.ext.Feed(p.Delta); emitted != "" {
		if !cur.emittedAny {
			cur.emittedAny = true
			// streamClear đặt ✻ header của extractor vào điểm khởi đầu của round mới,
			// phối hợp với kiểm tra HasPrefix("✻") của renderStreamContent để đi theo
			// đường dựng nổi bật renderAgentBlock; ensureStreamParagraphBreak chỉ chèn
			// dòng trống mà không mở round, nên ✻ vẫn bị đoạn thinking/nội dung trước
			// đó bọc lại và rơi vào renderChapterBlock, bị vẽ bằng màu mặc định.
			o.streamClear()
			// streamClear đã dọn sạch streamExtractors một cách phòng thủ. cur hiện tại
			// còn phải tiếp tục Feed các delta còn lại của lần gọi công cụ này, nên phải
			// đăng ký lại ngay; nếu không, đoạn delta kế tiếp sẽ tạo extractor mới và
			// phân tích từ giữa args (chỉ khi gặp `{` của object lồng mới vào
			// psBeforeKey), khiến timeline_events.time / foreshadow_updates.id bị coi
			// là trường cấp ngoài cùng và ✻ header hiện lặp trên TUI.
			o.streamExtractors[p.Agent] = cur
		}
		o.emitStreamDelta(emitted, false)
	}
}

func (o *observer) emitStreamDelta(delta string, thinking bool) {
	if delta == "" {
		return
	}
	if thinking != o.streamThinking {
		o.emitD(utils.ThinkingSep)
		o.streamThinking = thinking
	}
	o.emitD(delta)
	o.streamHasContent = true
	o.streamLastByte = delta[len(delta)-1]
}

// ensureSubagentToolStarted đăng ký trước một lần gọi TOOL đang chạy cho agent đó
// khi tool_call lần đầu xuất hiện trong luồng, để spinner của luồng sự kiện phủ kín
// khoảng thời gian "LLM đang sinh tool_call theo luồng" (thường chiếm 99% tổng
// thời gian của lần gọi). Lúc này args còn chưa đầy đủ nên tạm dùng tên công cụ
// thuần làm summary; khi ProgressToolStart thật sự đến sẽ bổ sung summary kèm tham số.
func (o *observer) ensureSubagentToolStarted(agent, tool string) {
	if agent == "" || tool == "" {
		return
	}
	if _, ok := o.toolStarts[agent]; ok {
		return // đã có lần gọi đang chạy, thao tác lặp lại không đổi kết quả
	}
	o.resetStreamArgLabel(agent, tool)
	id := nextEventID()
	o.toolStarts[agent] = &activeCall{
		id:      id,
		start:   time.Now(),
		summary: tool, // tạm dùng tên công cụ thuần, có thể đổi thành tool(Chương N) khi ProgressToolStart đến
		depth:   1,
	}
	o.emitAndLog(Event{
		ID:       id,
		Time:     time.Now(),
		Category: "TOOL",
		Agent:    agent,
		Summary:  tool,
		Level:    "info",
		Depth:    1,
	})
	o.updateAgent(agent, func(a *agentState) {
		a.state = "working"
		a.tool = tool
	})
	o.emitFallbackStreamHeader(tool)
}

func (o *observer) resetStreamArgLabel(agent, tool string) {
	key := streamArgKey(agent, tool)
	delete(o.streamArgPrefixes, key)
	delete(o.streamArgLabels, key)
}

// emitFallbackStreamHeader bổ sung một dòng ✻ tiêu đề vào bảng luồng cho các công cụ
// chưa cấu hình extractor.
// Cả hai đường đều phải gọi để hành vi nhất quán:
//  1. ensureSubagentToolStarted — subagent với tool args dạng luồng (DeltaToolCall)
//  2. handleToolUpdate ProgressToolStart — subagent với tool args không dạng luồng
//
// Thiếu bất kỳ đường nào, tiêu đề công cụ của mô hình dạng luồng và không dạng luồng
// sẽ hiển thị không nhất quán.
func (o *observer) emitFallbackStreamHeader(tool string) {
	if _, has := toolDisplays[tool]; has {
		return // đã có extractor, header do extractor tự xuất
	}
	o.streamClear()
	o.emitStreamDelta(streamHeaderFallback(tool)+"\n", false)
}

// streamHeaderFallback tạo văn bản header dạng luồng cho các công cụ chưa cấu hình
// extractor, để người dùng luôn thấy được "đang gọi cái gì", kể cả với các công cụ
// đọc nhẹ.
//
// Tiền tố "✻ " là dấu hiệu quy ước của "khối điều phối agent" — khi thấy tiền tố này,
// renderStreamContent của TUI sẽ đi theo đường dựng renderAgentBlock (biểu tượng +
// label nổi bật + vạch ngăn), nếu không sẽ rơi vào đường dựng khối nội dung dùng màu
// mặc định của terminal và header trông chỉ như nội dung thường, không nổi bật.
func streamHeaderFallback(tool string) string {
	return "✻ " + tool
}

// streamClear báo cho TUI mở streamRound mới, đồng thời đặt lại các trạng thái liên
// quan tới phân tách đoạn.
// Về mặt logic, round mới là "stream rỗng", nếu không thì lần emit đầu tiên của
// extractor kế tiếp sẽ cố chèn thêm dòng trống ở đầu.
//
// streamThinking cũng phải được đặt lại: emitStreamDelta dùng streamThinking để
// theo dõi xuyên suốt các lần gọi rằng đoạn trước có phải là suy nghĩ hay không.
// Trong round mới chưa có nội dung nào được xuất, nên lần emit(thinking=false) kế
// tiếp không nên chèn ThinkingSep. Nếu không, fallback header (ví dụ ✻ Đọc chương)
// sẽ bị \x02 chiếm mất vị trí đầu, HasPrefix("✻") của renderStreamContent không
// khớp, cả đoạn rơi vào đường nội dung rồi bị ThinkingSep cắt thành đoạn suy nghĩ,
// khiến màu tiêu đề bị vẽ như màu suy nghĩ.
func (o *observer) streamClear() {
	o.emitC()
	o.streamHasContent = false
	o.streamLastByte = 0
	o.streamThinking = false
	// ProgressToolEnd của subagent vòng trước đã delete trước khi kết thúc, ở đây dọn sạch một cách phòng thủ.
	if len(o.streamExtractors) > 0 {
		o.streamExtractors = make(map[string]*agentExtractor)
	}
}
