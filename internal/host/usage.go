package host

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/models"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// recentSampleCap là kích thước cửa sổ trượt: chỉ giữ (cacheRead, input) của N lần
// gọi gần nhất cho mỗi role, dùng để so sánh tỷ lệ trúng "tích luỹ với N lần gần
// nhất" ở cột trái, nhận ra "giai đoạn đầu kéo tụt" và "trạng thái ổn định trúng ít".
const recentSampleCap = 10

// Ngưỡng kép để phán đoán đứt chuỗi cache (theo kinh nghiệm thực nghiệm của Claude
// Code): lượng trung phải giảm quá 5% so với lần trước (tương đối) và giảm tối thiểu
// 2000 token (tuyệt đối) mới coi là đứt — một ngưỡng tương đối đơn lẻ sẽ bị nhiễu
// tiền tố nhỏ dập tắt, một ngưỡng tuyệt đối đơn lẻ sẽ bỏ lọt sự suy giảm rõ rệt ở
// tiền tố lớn.
const (
	cacheBreakKeepRatio     = 0.95
	cacheBreakMinDropTokens = 2000
)

// UsageTracker cộng dồn token LLM đầu vào/đầu ra và chi phí USD của mọi agent trong
// toàn bộ phiên.
//
// Cơ chế hoạt động:
//   - Mỗi khi callback OnMessage của agent kích hoạt thì gọi Record(agentName, msg)
//   - agentName được ánh xạ về role (architect_* chuẩn hóa thành architect), tra ModelSet hiện tại để lấy model gắn với role đó
//   - Dùng models.DefaultRegistry tra giá model, nhân với bốn khoản: đầu vào không cache / đầu ra / đọc cache / ghi cache
//   - Khi registry không có model đó thì lùi về msg.Usage.Cost.Total (provider tự tính, có thể bằng 0)
//   - Khi đổi model nóng (với /model), các tin nhắn sau tự tính giá theo model mới, còn tin nhắn cũ giữ nguyên chi phí cũ
//
// Đồng thời duy trì chiều per-role (writer/editor/architect):
//   - Dữ liệu trúng tích luỹ → hiệu quả tối ưu tổng thể
//   - N lần gần nhất trong cửa sổ trượt → phân biệt giai đoạn đầu kéo tụt với trạng thái ổn định trúng ít
//   - Cờ CacheCapable → phân biệt "chưa bật" với "thật sự trúng 0%"
//
// An toàn khi chạy đa luồng.
type UsageTracker struct {
	mu       sync.Mutex
	overall  agentTotals
	perAgent map[string]*agentTotals // key là tên role sau khi agentRoleName chuẩn hóa
	perModel map[string]*agentTotals // key là provider/model; khi provider không rõ thì lùi về model
	modelSet *bootstrap.ModelSet
	store    *storepkg.Store // có thể là nil (kịch bản kiểm thử), khi nil mọi phương thức ghi xuống đĩa là noop im lặng

	// cacheTrack là đường cơ sở của chuỗi cache theo per-role (độ dài tiền tố / lượng
	// trung / thời điểm của lần gọi trước), dùng để phát hiện đứt chuỗi.
	// Chỉ được cập nhật trên đường Record lúc chạy — replay lịch sử không phát hiện,
	// nếu không mỗi lần khởi động lại báo đứt chuỗi cũ thành cảnh báo giả.
	// Không được ghi xuống đĩa.
	cacheTrack map[string]*cacheTrackState

	// missingAssistantUsage cộng dồn số lần "nhận được tin nhắn assistant nhưng Usage
	// là nil".
	// Đo thực tế thì phần lớn xảy ra khi backend tương thích OpenAI tự dựng không
	// gửi final usage chunk ở cuối streaming theo giao thức OpenAI
	// stream_options.include_usage — partial.Usage luôn là nil, mọi trường tích luỹ
	// đều đứng ở 0. Bộ đếm này giúp UI nói thẳng với người dùng rằng "lỗi ở upstream
	// không trả usage chứ không phải module cache hỏng", thay vì cứ mổ code bảng cache.
	missingAssistantUsage int
	loggedMissingUsage    bool // cả phiên chỉ warn một lần, tránh làm tui.log bị lấp đầy

	// saveCh do Record kích hoạt không chặn sau mỗi lần cộng dồn; autoSaveLoop lắng
	// nghe và ghi xuống đĩa theo debounce.
	// buffered=1: nhiều lần Record liên tiếp được gộp thành một tín hiệu ghi; khi
	// kênh đã đầy thì bỏ qua, tick sau ghi chung.
	saveCh       chan struct{}
	autoSaveMu   sync.Mutex
	autoSaveDone chan struct{}

	// onCost được gọi sau mỗi lần ghi sổ, ngoài vùng khóa, với tổng chi phí tích luỹ
	// mới nhất (phát hiện BudgetSentinel vượt ngưỡng).
	// Bắt buộc phải đặt qua SetOnCost trước khi các lần Record chạy đồng thời, sau đó
	// chỉ đọc.
	onCost func(total float64)

	// onMissingUsage được gọi một lần khi phát hiện lần đầu "tin nhắn assistant không
	// có Usage" (cùng thời điểm với slog warn).
	// Khi bật ngân sách, điều này có nghĩa là có vùng mù tính phí — chi phí luôn bằng
	// 0, ngân sách không bao giờ kích hoạt, phải cảnh báo người dùng.
	onMissingUsage func()
}

// usageSample là mẫu trúng của một lần OnMessage, chỉ ghi tử số và mẫu số của tỷ lệ trúng.
type usageSample struct {
	CacheRead int
	Input     int
}

// cacheTrackState là đường cơ sở của chuỗi cache cho phiên hiện tại của một role.
// task (văn bản nhiệm vụ spawn) chính là danh tính phiên: đổi task = spawn mới =
// dòng dõi cache mới (prompt_cache_key có #seq), lần gọi đầu tiên trung thấp là
// chuyện thường, nên đổi đường cơ sở luôn mà không so sánh — nếu không, khi "phiên
// trước rất ngắn, tiền tố lần gọi đầu của phiên mới lại dài hơn" sẽ báo đứt nhầm.
// Ngữ nghĩa của Input (đã bao gồm CacheRead, xem chú thích computeCost) trùng khớp
// với "độ dài tiền tố mà server đã xử lý", nhờ đó phân biệt được ba hướng đi: tiền tố
// ngắn lại = nén trong phiên (hợp lệ, đặt lại đường cơ sở); tiền tố dài ra và lượng
// trung tăng theo = đường dẫn khỏe; tiền tố dài ra mà lượng trung giảm mạnh = đứt.
type cacheTrackState struct {
	task          string
	lastPrefix    int
	lastCacheRead int
	lastAt        time.Time
}

// agentTotals là bộ đếm tích luỹ của một agent.
//   - Saved là chênh lệch "nếu tính phí theo giá đầu vào không dùng cache", tính ngược từ dữ liệu trúng hiện tại
//   - CacheCapable chỉ được đặt true sau khi role đó chạy ít nhất một lần với "model đã biết có hỗ trợ cache"
//   - samples là ring buffer cố định; recentSampleCap lần đầu nối thẳng, sau đó xoay vòng theo sampleIdx
type agentTotals struct {
	Input        int
	Output       int
	CacheRead    int
	CacheWrite   int
	Cost         float64
	Saved        float64
	CacheCapable bool
	CacheBreaks  int // số lần đứt chuỗi cache phát hiện lúc chạy (replay không tính)
	samples      []usageSample
	sampleIdx    int
}

func NewUsageTracker(set *bootstrap.ModelSet, store *storepkg.Store) *UsageTracker {
	return &UsageTracker{
		modelSet:   set,
		store:      store,
		perAgent:   make(map[string]*agentTotals, 4),
		perModel:   make(map[string]*agentTotals, 4),
		cacheTrack: make(map[string]*cacheTrackState, 4),
		saveCh:     make(chan struct{}, 1),
	}
}

// Record chia một tin nhắn agent cho hai đường: cộng dồn và chẩn đoán.
//
// Đường cộng dồn chỉ xem Usage có tồn tại hay không — "tin nhắn nào mang Usage" là
// chi tiết lắp ráp của adapter agentcore/litellm (giao thức upstream đặt usage ở
// cấp ngoài cùng của response), nếu quy tắc lắp ráp sau này đổi thì cũng không phải
// sửa ở đây.
// Đường chẩn đoán đòi hỏi Role=Assistant và Content khác rỗng, để AbortMsg / khôi
// phục bất thường / tool / tin nhắn user không làm nhiễu bộ đếm
// missingAssistantUsage.
func (t *UsageTracker) Record(agentName, task string, msg agentcore.AgentMessage) {
	if t == nil {
		return
	}
	m, ok := msg.(agentcore.Message)
	if !ok {
		return
	}
	if m.Usage == nil {
		if m.Role == agentcore.RoleAssistant && len(m.Content) > 0 {
			t.flagMissingUsage(agentName)
		}
		return
	}
	role := agentRoleName(agentName)
	t.noteCacheBreak(role, task, *m.Usage)
	provider, modelName := usageActualModel(m.Usage)
	t.accumulate(role, provider, modelName, *m.Usage)
}

// noteCacheBreak phát hiện đứt chuỗi cache (thuần quan sát, không sửa, chỉ gọi trên
// đường Record lúc chạy).
//
// Điều kiện: trong cùng một phiên (role+task), tiền tố (Input, đã bao gồm CacheRead)
// không ngắn lại, mà lượng trung giảm >5% so với lần trước và giảm tối thiểu 2000
// token. Đổi task = spawn mới = dòng dõi cache mới, nên đổi đường cơ sở luôn mà
// không so sánh; tiền tố ngắn lại là do nén ngữ cảnh, thuộc dạng giảm hợp lệ, chỉ
// đặt lại đường cơ sở mà không cảnh báo. Nguyên nhân được gợi ý theo thứ tự ưu tiên:
// quãng cách vượt TTL → nghi do hết hạn; quãng cách rất ngắn trong khi số byte phía
// client lẽ ra phải ổn định → nghi do server loại bỏ/routing trôi lệch (trạm trung
// chuyển luân phiên upstream là nguyên nhân thường gặp).
func (t *UsageTracker) noteCacheBreak(role, task string, u agentcore.Usage) {
	now := time.Now()
	prefix := u.Input // các provider của litellm bảo đảm Input đã bao gồm CacheRead

	t.mu.Lock()
	st := t.cacheTrack[role]
	if st == nil || st.task != task {
		t.cacheTrack[role] = &cacheTrackState{task: task, lastPrefix: prefix, lastCacheRead: u.CacheRead, lastAt: now}
		t.mu.Unlock()
		return
	}
	prevPrefix, prevRead, prevAt := st.lastPrefix, st.lastCacheRead, st.lastAt
	st.lastPrefix, st.lastCacheRead, st.lastAt = prefix, u.CacheRead, now

	broke := prevPrefix > 0 && prefix >= prevPrefix &&
		float64(u.CacheRead) < float64(prevRead)*cacheBreakKeepRatio &&
		prevRead-u.CacheRead >= cacheBreakMinDropTokens
	if broke {
		t.overall.CacheBreaks++
		per := t.perAgent[role]
		if per == nil {
			per = &agentTotals{}
			t.perAgent[role] = per
		}
		per.CacheBreaks++
	}
	t.mu.Unlock()

	if !broke {
		return
	}
	gap := now.Sub(prevAt).Round(time.Second)
	hint := "nghi do server loại bỏ/routing trôi lệch (trạm trung chuyển luân phiên upstream là nguyên nhân thường gặp)"
	if gap > time.Hour {
		hint = "nghi do hết hạn TTL 1h"
	} else if gap > 5*time.Minute {
		hint = "nghi do hết hạn TTL 5m"
	}
	slog.Warn("đứt chuỗi cache: tiền tố không ngắn lại nhưng lượng trung giảm mạnh",
		"module", "usage", "role", role,
		"cache_read", fmt.Sprintf("%d→%d", prevRead, u.CacheRead),
		"prefix", fmt.Sprintf("%d→%d", prevPrefix, prefix),
		"gap", gap.String(), "hint", hint)
	t.notifyDirty()
}

func usageActualModel(u *agentcore.Usage) (provider, modelName string) {
	if u == nil {
		return "", ""
	}
	return strings.TrimSpace(u.Provider), strings.TrimSpace(u.Model)
}

// flagMissingUsage cộng dồn một lần sự kiện "trông như phản hồi LLM thật nhưng không
// lấy được usage", cả phiên chỉ ghi một dòng warn để tránh làm tui.log bị lấp đầy.
func (t *UsageTracker) flagMissingUsage(agentName string) {
	t.mu.Lock()
	t.missingAssistantUsage++
	shouldLog := !t.loggedMissingUsage
	t.loggedMissingUsage = true
	t.mu.Unlock()
	if shouldLog {
		slog.Warn("phản hồi LLM không mang dữ liệu usage, bảng cache/chi phí sẽ không có số liệu — thường do upstream streaming không gửi final usage chunk theo giao thức OpenAI include_usage",
			"module", "usage", "agent", agentName)
		if t.onMissingUsage != nil {
			t.onMissingUsage()
		}
	}
	t.notifyDirty()
}

// SetOnMissingUsage đăng ký callback một lần cho sự kiện "phát hiện thiếu usage lần
// đầu".
// Bắt buộc phải gọi một lần trong lúc khởi tạo Host, trước khi các lần Record
// chạy đồng thời.
func (t *UsageTracker) SetOnMissingUsage(cb func()) {
	if t == nil {
		return
	}
	t.onMissingUsage = cb
}

// notifyDirty kích hoạt không chặn một tín hiệu ghi xuống đĩa, autoSaveLoop sẽ ghi
// thật theo debounce.
// Kênh tín hiệu buffered=1: nhiều lần Record liên tiếp gộp lại thành một yêu cầu
// lưu là đủ.
func (t *UsageTracker) notifyDirty() {
	if t == nil || t.saveCh == nil {
		return
	}
	select {
	case t.saveCh <- struct{}{}:
	default:
	}
}

// accumulate cộng dồn một tin nhắn có Usage vào ba bộ đếm: overall / per-role /
// per-model.
// provider/model rỗng nghĩa là "dùng ModelSet hiện tại để lấy model của role"
// (đường thời gian thực); khác rỗng nghĩa là "ép tính giá theo model đã chỉ định"
// (đường replay lấy _meta trong session jsonl).
// resolveCost chạy ngoài vùng khóa (nó chỉ đọc modelSet/Registry), trong khóa chỉ làm
// phép cộng.
func (t *UsageTracker) accumulate(role, provider, modelName string, u agentcore.Usage) {
	provider, modelName = t.effectiveModel(role, provider, modelName)
	cost, saved, capable := t.resolveCost(modelName, u)

	t.mu.Lock()
	addUsage(&t.overall, u, cost, saved, capable)

	per := t.perAgent[role]
	if per == nil {
		per = &agentTotals{}
		t.perAgent[role] = per
	}
	addUsage(per, u, cost, saved, capable)

	if key := modelUsageKey(provider, modelName); key != "" {
		perModel := t.perModel[key]
		if perModel == nil {
			perModel = &agentTotals{}
			t.perModel[key] = perModel
		}
		addUsage(perModel, u, cost, saved, capable)
	}
	total := t.overall.Cost
	t.mu.Unlock()

	t.notifyDirty()
	if t.onCost != nil {
		t.onCost(total)
	}
}

// SetOnCost đăng ký callback ghi sổ (mang theo tổng chi phí tích luỹ mới nhất, được
// gọi ngoài vùng khóa).
// Bắt buộc phải gọi một lần trong lúc khởi tạo Host, trước khi các lần Record
// chạy đồng thời.
func (t *UsageTracker) SetOnCost(cb func(total float64)) {
	if t == nil {
		return
	}
	t.onCost = cb
}

func (t *UsageTracker) effectiveModel(role, provider, modelName string) (string, string) {
	provider = strings.TrimSpace(provider)
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		if t != nil && t.modelSet != nil {
			p, m, _ := t.modelSet.CurrentSelection(role)
			return p, m
		}
		return "", ""
	}
	if provider == "" && t != nil && t.modelSet != nil {
		p, m, _ := t.modelSet.CurrentSelection(role)
		if m == modelName {
			provider = p
		}
	}
	return provider, modelName
}

func modelUsageKey(provider, modelName string) string {
	provider = strings.TrimSpace(provider)
	modelName = strings.TrimSpace(modelName)
	switch {
	case modelName == "":
		return ""
	case provider == "":
		return modelName
	default:
		return provider + "/" + modelName
	}
}

// addUsage cộng token và chi phí của một lần gọi vào một bộ totals.
// Bắt buộc phải gọi khi đang giữ UsageTracker.mu.
//
// CacheCapable ưu tiên phán đoán theo "sự thật": chỉ cần từng thấy CacheRead hoặc
// CacheWrite > 0 là đã chứng minh upstream thực sự làm prompt caching.
// CacheReadCostPer1M trong registry chỉ là fallback, vì các model backend tự dựng
// (mimo-v2.5-pro / các proxy trong nước...) thường không có trong bảng giá
// BerriAI/litellm, nhưng Usage thực tế lại có đầy đủ dữ liệu cache, UI không nên
// nhầm thành "chưa bật".
func addUsage(t *agentTotals, u agentcore.Usage, cost, saved float64, capable bool) {
	t.Input += u.Input
	t.Output += u.Output
	t.CacheRead += u.CacheRead
	t.CacheWrite += u.CacheWrite
	t.Cost += cost
	t.Saved += saved
	if capable || u.CacheRead > 0 || u.CacheWrite > 0 {
		t.CacheCapable = true
	}
	pushSample(t, u.CacheRead, u.Input)
}

// pushSample đẩy một mẫu vào ring buffer. recentSampleCap lần đầu nối thẳng, sau đó xoay vòng ghi đè.
func pushSample(t *agentTotals, cacheRead, input int) {
	s := usageSample{CacheRead: cacheRead, Input: input}
	if len(t.samples) < recentSampleCap {
		t.samples = append(t.samples, s)
		return
	}
	t.samples[t.sampleIdx] = s
	t.sampleIdx = (t.sampleIdx + 1) % recentSampleCap
}

// recentSums trả về tổng cacheRead và input trong cửa sổ trượt, làm tử số/mẫu số cho
// "tỷ lệ trúng của N lần gần nhất".
// Dùng sum/sum thay vì "trung bình của các tỷ lệ đơn lẻ" để tránh mẫu nhỏ
// (input vài trăm token) khuếch đại nhiễu.
func recentSums(t *agentTotals) (cacheRead, input int) {
	for _, s := range t.samples {
		cacheRead += s.CacheRead
		input += s.Input
	}
	return cacheRead, input
}

// Totals trả về ảnh chụp tổng số tích luỹ.
func (t *UsageTracker) Totals() (cost float64, input, output, cacheRead, cacheWrite int) {
	if t == nil {
		return 0, 0, 0, 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.overall.Cost, t.overall.Input, t.overall.Output, t.overall.CacheRead, t.overall.CacheWrite
}

// SavedUSD trả về số USD tích luỹ tiết kiệm được nhờ trúng cache.
func (t *UsageTracker) SavedUSD() float64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.overall.Saved
}

// OverallRecent trả về tổng cacheRead, tổng input và số mẫu trong cửa sổ trượt (≤ recentSampleCap lần).
func (t *UsageTracker) OverallRecent() (cacheRead, input, samples int) {
	if t == nil {
		return 0, 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	r, in := recentSums(&t.overall)
	return r, in, len(t.overall.samples)
}

// OverallCacheBreaks trả về tổng số lần đứt chuỗi cache phát hiện lúc chạy.
func (t *UsageTracker) OverallCacheBreaks() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.overall.CacheBreaks
}

// OverallCacheCapable cho biết toàn cục đã từng chạy ít nhất một model đã biết có hỗ trợ cache hay chưa.
func (t *UsageTracker) OverallCacheCapable() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.overall.CacheCapable
}

// MissingAssistantUsage trả về số lần tích luỹ của "nhận được tin nhắn assistant nhưng
// Usage là nil".
// Lớn hơn 0 thường có nghĩa là upstream streaming không gửi final usage chunk của
// OpenAI, UI dựa vào đó để hiện lời nhắc thay vì tưởng module cache tự nó đã hỏng.
func (t *UsageTracker) MissingAssistantUsage() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.missingAssistantUsage
}

// ── Ghi xuống đĩa ──

// Snapshot sao chép trạng thái tích luỹ hiện tại thành domain.UsageState có thể
// tuần tự hóa.
// samples của cửa sổ trượt không đưa vào snapshot — đó là cửa sổ chẩn đoán ngắn hạn,
// ý nghĩa qua nhiều tiến trình không lớn.
func (t *UsageTracker) Snapshot() domain.UsageState {
	if t == nil {
		return domain.UsageState{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := domain.UsageState{
		Schema:       domain.UsageSchemaVersion,
		UpdatedAt:    time.Now(),
		Overall:      totalsSnapshot(&t.overall),
		PerAgent:     make(map[string]domain.AgentUsageTotals, len(t.perAgent)),
		PerModel:     make(map[string]domain.AgentUsageTotals, len(t.perModel)),
		MissingUsage: t.missingAssistantUsage,
	}
	for role, v := range t.perAgent {
		state.PerAgent[role] = totalsSnapshot(v)
	}
	for model, v := range t.perModel {
		state.PerModel[model] = totalsSnapshot(v)
	}
	return state
}

// LoadFromStore đọc ảnh chụp đã ghi xuống đĩa từ store.Usage rồi điền lại vào bộ nhớ.
// Trả về true nghĩa là đã nạp được một trạng thái khác rỗng (khớp schema); false
// nghĩa là không có tệp hoặc không dùng được, lúc đó bên gọi nên đi tiếp qua
// session replay để điền một lần.
func (t *UsageTracker) LoadFromStore() (bool, error) {
	if t == nil || t.store == nil {
		return false, nil
	}
	state, err := t.store.Usage.Load()
	if err != nil {
		return false, err
	}
	if state == nil {
		return false, nil
	}
	t.applyState(*state)
	return true, nil
}

// SaveNow ghi ảnh chụp hiện tại xuống đĩa ngay. Đường autoSaveLoop / Close đều ghi qua đây.
func (t *UsageTracker) SaveNow() error {
	if t == nil || t.store == nil {
		return nil
	}
	return t.store.Usage.Save(t.Snapshot())
}

// StartAutoSave dựng một goroutine, lắng nghe saveCh và ghi xuống đĩa theo debounce.
// Trước khi ctx kết thúc, nó flush trạng thái chưa lưu lần cuối.
// Close kích hoạt flush + thoát bằng cách huỷ ctx.
func (t *UsageTracker) StartAutoSave(ctx context.Context) {
	if t == nil || t.store == nil {
		return
	}
	done := make(chan struct{})
	t.autoSaveMu.Lock()
	t.autoSaveDone = done
	t.autoSaveMu.Unlock()
	go func() {
		defer close(done)
		t.autoSaveLoop(ctx)
	}()
}

// WaitAutoSave chờ lần flush cuối sau khi huỷ hoàn tất. Host.Close gọi cancel trước,
// rồi mới chờ ở đây, để tránh autoSaveLoop và SaveNow lúc thoát ghi cùng một ảnh
// chụp đồng thời.
func (t *UsageTracker) WaitAutoSave() {
	if t == nil {
		return
	}
	t.autoSaveMu.Lock()
	done := t.autoSaveDone
	t.autoSaveMu.Unlock()
	if done != nil {
		<-done
	}
}

// autoSaveLoop biến tín hiệu dirty tần suất cao thành lần ghi xuống đĩa mỗi 500ms.
//
// Giải thích thiết kế: 500ms là giá trị kinh nghiệm — mỗi chương có 1-2 LLM turn,
// ghi xuống đĩa 1-2 lần là hoàn toàn chấp nhận được; dù người dùng bấm ctrl+C để
// thoát mà timer chưa kịp kích hoạt, đường huỷ ctx cũng sẽ flush lần cuối.
// Sự cố thật (OS kill -9) sẽ mất phần tích luỹ trong 0.5s gần nhất — session jsonl
// phía upstream vẫn là sự thật đầy đủ, lần khởi động sau sẽ replay từ sessions/ để
// bù chênh lệch.
func (t *UsageTracker) autoSaveLoop(ctx context.Context) {
	const debounce = 500 * time.Millisecond
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()

	var pending bool
	flush := func() {
		if err := t.SaveNow(); err != nil {
			slog.Warn("ghi usage xuống đĩa thất bại", "module", "usage", "err", err)
		}
		pending = false
	}
	for {
		select {
		case <-ctx.Done():
			if pending {
				flush()
			}
			return
		case <-t.saveCh:
			if pending {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			}
			timer.Reset(debounce)
			pending = true
		case <-timer.C:
			flush()
		}
	}
}

// applyState ghi ảnh chụp đã lưu trở lại vào bộ nhớ. Chỉ gọi lúc khởi động (sau
// LoadFromStore / replay), khi đó autoSaveLoop chưa chạy và Record cũng chưa có thể
// chạy đồng thời nên không cần giữ khóa; vẫn giữ mu để phòng khi thứ tự gọi trong
// kiểm thử hoặc tương lai thay đổi sinh ra truy cập đồng thời.
func (t *UsageTracker) applyState(state domain.UsageState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.overall = totalsFromState(state.Overall)
	if state.PerAgent == nil {
		t.perAgent = make(map[string]*agentTotals, 4)
	} else {
		t.perAgent = make(map[string]*agentTotals, len(state.PerAgent))
		for role, v := range state.PerAgent {
			tot := totalsFromState(v)
			t.perAgent[role] = &tot
		}
	}
	if state.PerModel == nil {
		t.perModel = make(map[string]*agentTotals, 4)
	} else {
		t.perModel = make(map[string]*agentTotals, len(state.PerModel))
		for model, v := range state.PerModel {
			tot := totalsFromState(v)
			t.perModel[model] = &tot
		}
	}
	t.missingAssistantUsage = state.MissingUsage
}

// totalsSnapshot sao chép agentTotals trong bộ nhớ thành domain.AgentUsageTotals có
// thể lưu trữ.
// ring buffer samples cố ý không mang theo — xem chú thích UsageState.
func totalsSnapshot(t *agentTotals) domain.AgentUsageTotals {
	if t == nil {
		return domain.AgentUsageTotals{}
	}
	return domain.AgentUsageTotals{
		Input:        t.Input,
		Output:       t.Output,
		CacheRead:    t.CacheRead,
		CacheWrite:   t.CacheWrite,
		Cost:         t.Cost,
		Saved:        t.Saved,
		CacheCapable: t.CacheCapable,
		CacheBreaks:  t.CacheBreaks,
	}
}

// totalsFromState khôi phục dạng đã lưu trữ về agentTotals trong bộ nhớ. samples để
// trống, sau khi khởi động lại sẽ tích luỹ lại từ 0, vài lần Record là khôi phục
// được ngữ nghĩa "tỷ lệ trúng của N lần gần nhất".
func totalsFromState(s domain.AgentUsageTotals) agentTotals {
	return agentTotals{
		Input:        s.Input,
		Output:       s.Output,
		CacheRead:    s.CacheRead,
		CacheWrite:   s.CacheWrite,
		Cost:         s.Cost,
		Saved:        s.Saved,
		CacheCapable: s.CacheCapable,
		CacheBreaks:  s.CacheBreaks,
	}
}

// AgentUsage là ảnh chụp lượng dùng tích luỹ của một agent (cung cấp cho UI).
type AgentUsage struct {
	Role            string
	Model           string
	Input           int
	Output          int
	CacheRead       int
	CacheWrite      int
	Cost            float64
	Saved           float64
	CacheCapable    bool
	RecentCacheRead int
	RecentInput     int
	RecentSamples   int
}

// PerAgent trả về lượng dùng tích luỹ của từng role. Kết quả sắp theo số lượng CacheRead giảm dần, role chưa dùng token thì bị bỏ qua.
func (t *UsageTracker) PerAgent() []AgentUsage {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]AgentUsage, 0, len(t.perAgent))
	for role, v := range t.perAgent {
		if v.Input == 0 && v.Output == 0 {
			continue
		}
		recentRead, recentInput := recentSums(v)
		out = append(out, AgentUsage{
			Role:            role,
			Input:           v.Input,
			Output:          v.Output,
			CacheRead:       v.CacheRead,
			CacheWrite:      v.CacheWrite,
			Cost:            v.Cost,
			Saved:           v.Saved,
			CacheCapable:    v.CacheCapable,
			RecentCacheRead: recentRead,
			RecentInput:     recentInput,
			RecentSamples:   len(v.samples),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CacheRead != out[j].CacheRead {
			return out[i].CacheRead > out[j].CacheRead
		}
		return out[i].Input > out[j].Input
	})
	return out
}

// PerModel trả về lượng dùng tích luỹ của từng model. Kết quả sắp theo chi phí giảm dần, sau đó theo lượng đầu vào giảm dần.
func (t *UsageTracker) PerModel() []AgentUsage {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]AgentUsage, 0, len(t.perModel))
	for model, v := range t.perModel {
		if v.Input == 0 && v.Output == 0 {
			continue
		}
		out = append(out, AgentUsage{
			Model:        model,
			Input:        v.Input,
			Output:       v.Output,
			CacheRead:    v.CacheRead,
			CacheWrite:   v.CacheWrite,
			Cost:         v.Cost,
			Saved:        v.Saved,
			CacheCapable: v.CacheCapable,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Cost != out[j].Cost {
			return out[i].Cost > out[j].Cost
		}
		return out[i].Input > out[j].Input
	})
	return out
}

// resolveCost trả về đồng thời cost / saved / capable của tin nhắn này.
//   - cost: khi registry có khớp thì nhân với 4 khoản; không khớp thì lùi về cost do provider tự tính
//   - saved: chỉ lớn hơn 0 khi registry có khớp, CacheRead > 0 và InputCost > CacheReadCost
//   - capable: registry có khớp và CacheReadCostPer1M của model đó > 0 → đã biết model hỗ trợ prompt caching
//
// modelName ưu tiên giá trị do bên gọi truyền vào (khi replay lấy từ _meta.model
// trong session jsonl).
func (t *UsageTracker) resolveCost(modelName string, u agentcore.Usage) (cost, saved float64, capable bool) {
	if entry, ok := models.DefaultRegistry().Resolve(modelName); ok {
		c := computeCost(u, *entry)
		s := computeSaved(u, *entry)
		canCache := entry.CacheReadCostPer1M > 0
		if c > 0 {
			return c, s, canCache
		}
	}
	if u.Cost != nil {
		return u.Cost.Total, 0, false
	}
	return 0, 0, false
}

// agentRoleName chuẩn hóa tên subagent về tên role.
// architect_short/mid/long đều quy về architect; các tên khác trả về nguyên dạng.
func agentRoleName(agentName string) string {
	if strings.HasPrefix(agentName, "architect_") {
		return "architect"
	}
	return agentName
}

// computeCost tính chi phí USD của lần gọi này theo đơn giá $/1M token.
//
// Tiền đề về ngữ nghĩa (được các provider của litellm bảo đảm thống nhất, xem các
// điểm lắp ráp Usage trong anthropic.go / bedrock.go / openai.go / gemini.go /
// compat.go):
//
//	u.Input  = toàn bộ token đầu vào, **bao gồm** CacheRead; không gồm CacheWrite
//	u.Output = token đầu ra
//
// Vì vậy nonCachedInput = u.Input - u.CacheRead đúng với mọi provider.
// Nhánh dự phòng được giữ lại để không sập khi một provider nào đó trong tương lai
// trả về dữ liệu bẩn.
func computeCost(u agentcore.Usage, e models.ModelEntry) float64 {
	nonCachedInput := u.Input - u.CacheRead
	if nonCachedInput < 0 {
		nonCachedInput = u.Input
	}
	c := 0.0
	c += float64(nonCachedInput) * e.InputCostPer1M / 1_000_000
	c += float64(u.Output) * e.OutputCostPer1M / 1_000_000
	c += float64(u.CacheRead) * e.CacheReadCostPer1M / 1_000_000
	c += float64(u.CacheWrite) * e.CacheWriteCostPer1M / 1_000_000
	return c
}

// computeSaved ước lượng số USD tiết kiệm được của lượng trúng CacheRead so với cách
// tính phí theo giá đầu vào thông thường.
// Lưu ý phần phí trộn của CacheWrite không được bù trừ — đó là khoản đầu tư tất
// yếu "để mở đường cho các lần trúng sau", lợi nhuận thật được thu hồi qua các
// lần CacheRead tích luỹ về sau.
func computeSaved(u agentcore.Usage, e models.ModelEntry) float64 {
	if u.CacheRead <= 0 || e.InputCostPer1M <= 0 {
		return 0
	}
	delta := e.InputCostPer1M - e.CacheReadCostPer1M
	if delta <= 0 {
		return 0
	}
	return float64(u.CacheRead) * delta / 1_000_000
}
