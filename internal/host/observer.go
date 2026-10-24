package host

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/domain"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// errorKind classifies a runtime error into a stable, short label for log
// filtering and alert routing. Returns "" when no special tag applies.
//
// err is the live error chain (may be nil after JSON serialization); msg is
// the rendered string fallback used when the chain has been flattened
// (e.g. inside sub-agent JSON results).
func errorKind(err error, msg string) string {
	if kind := agentcore.ErrorKind(err); kind != "" && kind != "unknown" {
		return kind
	}
	if msg == "" {
		return ""
	}
	if kind := agentcore.ErrorKind(errors.New(msg)); kind != "unknown" {
		return kind
	}
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "tool argument validation failed"):
		return "tool_validation"
	case strings.Contains(lower, "too many concurrent requests"):
		return "overloaded"
	// providerError gắn loại có cấu trúc của litellm vào cuối văn bản.
	// Bản thân HTTP/2 INTERNAL_ERROR không có từ khóa để phân loại, nên giữ lại dấu
	// hiệu network tường minh này là đủ.
	case strings.Contains(lower, "[network,"):
		return "network"
	}
	return ""
}

// Bộ đếm ID sự kiện tăng đơn điệu; phối hợp với dấu thời gian để tạo ID ổn định.
var eventIDCounter uint64

func nextEventID() string {
	return fmt.Sprintf("e%d", atomic.AddUint64(&eventIDCounter, 1))
}

// activeCall ghi lại ID, thời điểm bắt đầu và summary của một lần gọi đang diễn ra
// (TOOL / DISPATCH).
// summary được điền lại vào Event lúc hoàn thành, đảm bảo replay (runtime queue)
// khôi phục được nội dung dòng.
type activeCall struct {
	id      string
	start   time.Time
	summary string
	depth   int
}

// observer chiếu các lần Engine phân công và tiến độ Worker vào kênh đầu ra của
// Host.
// Nó thuần túy là người quan sát, không tham gia quyết định điều khiển nào.
type observer struct {
	emitEv  func(Event)
	emitD   func(string)
	emitC   func()
	store   *storepkg.Store // dùng cho việc ghi runtime queue (ReplayQueue tiêu thụ)
	agents  map[string]*agentState
	agentMu sync.Mutex

	// aborting do Host đặt ở các cửa vào Abort()/Close(), xoá ở Start/Resume/Continue.
	// Trong lúc cờ này bật, mọi sự kiện lỗi phái sinh từ context-cancel đều bị chặn
	// (vừa đúng như người dùng mong đợi, vừa tránh trùng với sự kiện "người dùng tạm
	// dừng thủ công"). Sự cố thật (không phải cancel) vẫn được báo như thường.
	aborting atomic.Bool

	streamThinking      bool
	lastThinkingByAgent map[string]string          // agent → văn bản thinking tích luỹ gần nhất (dùng để trích phần delta tăng dần)
	dispatchStarts      map[string]*activeCall     // agent đã được phân công → lần gọi DISPATCH đang chạy
	toolStarts          map[string]*activeCall     // agent → lần gọi TOOL đang chạy
	streamExtractors    map[string]*agentExtractor // agent → bộ trích nội dung tham số JSON của lần gọi công cụ hiện tại
	streamArgPrefixes   map[string]string          // agent/tool → tiền tố luồng tham số, dùng để nhận diện sớm nhãn nhẹ
	streamArgLabels     map[string]string          // agent/tool → tên hiển thị đã nhận diện sớm từ luồng tham số
	retryEvents         map[string]string          // phạm vi thử lại → ID sự kiện, dùng để cập nhật tại chỗ trên cùng một dòng (2/7)
	streamHasContent    bool                       // streamRound hiện tại đã xuất nội dung hay chưa (để biết có cần phân tách đoạn không)
	streamLastByte      byte                       // byte cuối của lần xuất dạng luồng gần nhất (dùng để chèn xuống dòng chính xác)
}

// agentExtractor ghi lại tên công cụ đang được trích và thể hiện của bộ trích cho
// agent đó.
// Tên công cụ dùng để phát hiện "lần gọi công cụ mới đã bắt đầu", tránh để bộ nhớ đệm
// bị dữ liệu sót lại của vòng trước làm bẩn.
type agentExtractor struct {
	tool       string
	ext        *jsonFieldExtractor
	emittedAny bool // extractor này đã từng xuất nội dung; dùng để chèn phân tách đoạn trước lần xuất đầu tiên
}

type agentState struct {
	name    string
	state   string
	tool    string
	summary string
	turn    int
	context AgentContextSnapshot
	updated time.Time
}

func newObserver(s *storepkg.Store, emitEv func(Event), emitD func(string), emitC func()) *observer {
	return &observer{
		emitEv:              emitEv,
		emitD:               emitD,
		emitC:               emitC,
		store:               s,
		agents:              make(map[string]*agentState),
		lastThinkingByAgent: make(map[string]string),
		dispatchStarts:      make(map[string]*activeCall),
		toolStarts:          make(map[string]*activeCall),
		streamExtractors:    make(map[string]*agentExtractor),
		streamArgPrefixes:   make(map[string]string),
		streamArgLabels:     make(map[string]string),
		retryEvents:         make(map[string]string),
	}
}

// texts trả về bảng văn bản của cụm quan sát theo ngôn ngữ tác phẩm.
// Ngôn ngữ được đọc lười từ store.Language() ở mỗi lần dùng, không ghi cứng vào
// cấu trúc lúc khởi tạo, nên việc đổi ngôn ngữ giữa phiên vẫn có hiệu lực.
// Khi không có store (kịch bản kiểm thử) thì dùng bản tiếng Việt.
func (o *observer) texts() *observerTexts {
	if o == nil || o.store == nil {
		return observerTextsVI
	}
	return observerTextsFor(o.store.Language())
}

// ── Cửa vào Engine gọi trực tiếp ──
//
// Engine chạy Worker trực tiếp, nguồn sự kiện chia làm hai nhánh:
//  1. dispatchStart/dispatchFinish — Engine gọi trực tiếp ở ranh giới phân công (dòng DISPATCH)
//  2. workerProgress — bản trung chuyển tiến độ của Worker (ctx ToolProgress),
//     do handleToolUpdate xử lý thống nhất cho TOOL/nội dung luồng/thinking/retry/context

// dispatchStart ghi lại lúc bắt đầu phân công Worker và phát dòng DISPATCH.
func (o *observer) dispatchStart(agent, task, reason string) {
	summary := dispatchSummary(agent, task)
	o.updateAgent(agent, func(a *agentState) {
		a.state = "working"
		a.tool = ""
		a.summary = fmt.Sprintf("engine → %s", summary)
	})
	id := nextEventID()
	o.dispatchStarts[agent] = &activeCall{id: id, start: time.Now(), summary: summary}
	o.emitAndLog(Event{
		ID:       id,
		Time:     time.Now(),
		Category: "DISPATCH",
		Agent:    agent,
		Summary:  summary,
		Detail:   dispatchDetail(task, reason),
		Level:    "info",
	})
}

// dispatchFinish chuyển dòng DISPATCH sang trạng thái hoàn thành và đặt lại trạng thái
// của Worker;
// dọn các dòng TOOL mồ côi của Worker đó (ở đường abort/lỗi, ProgressToolEnd có thể
// không đến).
func (o *observer) dispatchFinish(agent string, runErr error) {
	o.updateAgent(agent, func(a *agentState) {
		a.state = "idle"
		a.tool = ""
	})
	delete(o.lastThinkingByAgent, agent)
	if call, ok := o.toolStarts[agent]; ok {
		delete(o.toolStarts, agent)
		delete(o.streamExtractors, agent)
		o.emitCallFinish(call, "TOOL", agent, runErr)
	}
	if call, ok := o.dispatchStarts[agent]; ok {
		delete(o.dispatchStarts, agent)
		o.emitCallFinish(call, "DISPATCH", agent, runErr)
	}
	o.streamClear()
}

// workerProgress chuyển bản trung chuyển tiến độ của Worker thành xử lý ToolExecUpdate sẵn có.
func (o *observer) workerProgress(p agentcore.ProgressPayload) {
	payload := p
	o.handleToolUpdate(agentcore.Event{Type: agentcore.EventToolExecUpdate, Progress: &payload})
}

func (o *observer) finalize() {
	o.agentMu.Lock()
	defer o.agentMu.Unlock()
	for _, a := range o.agents {
		a.state = "idle"
		a.tool = ""
	}
}

// setAborting do Host gọi ở các mốc chuyển vòng đời như Abort/Close/Start, để điều
// khiển việc có chặn các sự kiện phái sinh kiểu "context canceled" hay không (tránh
// trùng với "người dùng tạm dừng thủ công").
func (o *observer) setAborting(v bool) { o.aborting.Store(v) }

func (o *observer) retryEventID(scope string, attempt int) string {
	if strings.TrimSpace(scope) == "" {
		scope = "engine"
	}
	if o.retryEvents == nil {
		o.retryEvents = make(map[string]string)
	}
	if attempt <= 1 || o.retryEvents[scope] == "" {
		o.retryEvents[scope] = nextEventID()
	}
	return o.retryEvents[scope]
}

// emitAndLog dùng cho trạng thái "bắt đầu" của sự kiện loại gọi: gửi TUI nhưng không
// ghi vào runtime queue, tránh khi replay lặp lại "một dòng bắt đầu, một dòng hoàn
// thành". slog do host.emitEvent ghi nhất quán.
func (o *observer) emitAndLog(ev Event) {
	o.emitEv(ev)
}

// persistEvent ghi sự kiện vào runtime queue (slog do host.emitEvent ghi nhất quán).
func (o *observer) persistEvent(ev Event) {
	if o.store == nil || o.store.Runtime == nil {
		return
	}
	priority := domain.RuntimePriorityBackground
	switch {
	case ev.Level == "error":
		priority = domain.RuntimePriorityControl
	case ev.Category == "SYSTEM" || ev.Category == "ERROR":
		priority = domain.RuntimePriorityControl
	}
	if _, err := o.store.Runtime.AppendQueue(domain.RuntimeQueueItem{
		Time:     ev.Time,
		Priority: priority,
		Category: ev.Category,
		Summary:  ev.Summary,
		Payload:  ev,
	}); err != nil {
		slog.Warn("ghi sự kiện vận hành thất bại", "module", "observer", "category", ev.Category, "err", err)
	}
}

func (o *observer) updateAgent(name string, fn func(*agentState)) {
	if name == "" {
		return
	}
	o.agentMu.Lock()
	defer o.agentMu.Unlock()
	a, ok := o.agents[name]
	if !ok {
		a = &agentState{name: name, state: "idle"}
		o.agents[name] = a
	}
	fn(a)
	a.updated = time.Now()
}

func (o *observer) agentSnapshots() []AgentSnapshot {
	o.agentMu.Lock()
	defer o.agentMu.Unlock()
	snaps := make([]AgentSnapshot, 0, len(o.agents))
	for _, a := range o.agents {
		snaps = append(snaps, AgentSnapshot{
			Name:      a.name,
			State:     a.state,
			Summary:   a.summary,
			Tool:      a.tool,
			Turn:      a.turn,
			Context:   a.context,
			UpdatedAt: a.updated,
		})
	}
	return snaps
}
