package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/models"
)

func TestUsageTrackerReplaySessionsReadsWorkerLogs(t *testing.T) {
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, "meta", "sessions", "agents")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	rec := sessionRecord{
		Role: agentcore.RoleAssistant,
		Usage: &agentcore.Usage{
			Input: 1200, Output: 300, CacheRead: 800,
		},
		Meta: &sessionRecordMeta{Provider: "openrouter", Model: "test-model"},
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(sessionsDir, "writer-ch01.jsonl"), data, 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	tk := NewUsageTracker(nil, nil)
	n, err := tk.ReplaySessions(dir)
	if err != nil {
		t.Fatalf("ReplaySessions: %v", err)
	}
	if n != 1 {
		t.Fatalf("replayed messages = %d, want 1", n)
	}
	_, input, output, cacheRead, _ := tk.Totals()
	if input != 1200 || output != 300 || cacheRead != 800 {
		t.Fatalf("replayed totals = input:%d output:%d cache:%d", input, output, cacheRead)
	}
}

// makeUsageMsg dựng một message mà callback OnMessage chấp nhận được (có Usage).
// Role bắt buộc phải đặt thành assistant: UsageTracker.Record nay lọc theo vai
// trò, chỉ message assistant mới được cộng dồn (vai trò khác vốn không mang
// usage).
func makeUsageMsg(input, cacheRead, cacheWrite, output int) agentcore.AgentMessage {
	return agentcore.Message{
		Role: agentcore.RoleAssistant,
		Usage: &agentcore.Usage{
			Input: input, Output: output, CacheRead: cacheRead, CacheWrite: cacheWrite,
		},
	}
}

// Test_pushSample_RingBuffer kiểm tra ngữ nghĩa xoay vòng của cửa sổ trượt:
// N lần đầu append thẳng; sau đó ghi đè mục cũ nhất theo sampleIdx.
// recentSums luôn phản ánh "N lần gần nhất".
func Test_pushSample_RingBuffer(t *testing.T) {
	var tot agentTotals

	for i := 1; i <= recentSampleCap; i++ {
		pushSample(&tot, i, i*100)
	}
	if got := len(tot.samples); got != recentSampleCap {
		t.Fatalf("after %d pushes, samples len=%d want %d", recentSampleCap, got, recentSampleCap)
	}

	pushSample(&tot, 999, 99900)
	if got := len(tot.samples); got != recentSampleCap {
		t.Fatalf("after overflow, samples len=%d want %d (no growth)", got, recentSampleCap)
	}
	cacheRead, input := recentSums(&tot)
	expectedCacheRead := 999
	expectedInput := 99900
	for i := 2; i <= recentSampleCap; i++ {
		expectedCacheRead += i
		expectedInput += i * 100
	}
	if cacheRead != expectedCacheRead || input != expectedInput {
		t.Fatalf("recentSums after overflow = (%d, %d), want (%d, %d)",
			cacheRead, input, expectedCacheRead, expectedInput)
	}
}

// Test_UsageTracker_RecordAccumulates kiểm tra Record cộng dồn đúng với nhiều
// role: tổng hợp = tổng của mọi role; per-role thì độc lập.
func Test_UsageTracker_RecordAccumulates(t *testing.T) {
	tk := NewUsageTracker(nil, nil) // modelSet=nil → rơi về provider Cost, không ảnh hưởng logic cộng dồn

	tk.Record("writer", "", makeUsageMsg(1000, 800, 0, 200))
	tk.Record("writer", "", makeUsageMsg(1500, 1200, 100, 300))
	tk.Record("editor", "", makeUsageMsg(500, 0, 0, 100))

	cost, in, out, cr, cw := tk.Totals()
	if in != 3000 || out != 600 || cr != 2000 || cw != 100 {
		t.Fatalf("totals = (in=%d out=%d cr=%d cw=%d), want (3000 600 2000 100)", in, out, cr, cw)
	}
	if cost != 0 {
		t.Errorf("cost should be 0 when modelSet=nil and no provider Cost, got %v", cost)
	}

	per := tk.PerAgent()
	if len(per) != 2 {
		t.Fatalf("per-agent len=%d want 2", len(per))
	}
	// PerAgent sắp theo CacheRead giảm dần: writer (2000) phải đứng trước editor (0)
	if per[0].Role != "writer" || per[1].Role != "editor" {
		t.Fatalf("per-agent order = %s,%s want writer,editor", per[0].Role, per[1].Role)
	}
	if per[0].Input != 2500 || per[0].CacheRead != 2000 {
		t.Errorf("writer totals = (in=%d cr=%d), want (2500 2000)", per[0].Input, per[0].CacheRead)
	}
}

// Test_UsageTracker_ArchitectAliasNormalized kiểm tra architect_short/mid/long
// đều quy về cùng một key "architect" (tránh bị /model tách vai trò con thành
// nhiều dòng).
func Test_UsageTracker_ArchitectAliasNormalized(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	tk.Record("architect_short", "", makeUsageMsg(100, 50, 0, 20))
	tk.Record("architect_mid", "", makeUsageMsg(200, 100, 0, 40))
	tk.Record("architect_long", "", makeUsageMsg(300, 150, 0, 60))

	per := tk.PerAgent()
	if len(per) != 1 {
		t.Fatalf("aliases must merge to single role, got %d entries: %+v", len(per), per)
	}
	if per[0].Role != "architect" {
		t.Fatalf("merged role name = %q, want architect", per[0].Role)
	}
	if per[0].Input != 600 || per[0].CacheRead != 300 {
		t.Errorf("merged totals = (in=%d cr=%d), want (600 300)", per[0].Input, per[0].CacheRead)
	}
}

func Test_UsageTracker_PerModelAccumulates(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	tk.accumulate("writer", "openrouter", "model-a", agentcore.Usage{Input: 1000, Output: 200, CacheRead: 700})
	tk.accumulate("editor", "openrouter", "model-b", agentcore.Usage{Input: 500, Output: 100})
	tk.accumulate("writer", "openrouter", "model-a", agentcore.Usage{Input: 300, Output: 80, CacheRead: 200})

	perModel := tk.PerModel()
	if len(perModel) != 2 {
		t.Fatalf("per-model len=%d want 2", len(perModel))
	}
	seen := map[string]AgentUsage{}
	for _, m := range perModel {
		seen[m.Model] = m
	}
	if seen["openrouter/model-a"].Input != 1300 || seen["openrouter/model-a"].CacheRead != 900 {
		t.Errorf("model-a totals = %+v", seen["openrouter/model-a"])
	}
	if seen["openrouter/model-b"].Output != 100 {
		t.Errorf("model-b totals = %+v", seen["openrouter/model-b"])
	}

	snap := tk.Snapshot()
	restored := NewUsageTracker(nil, nil)
	restored.applyState(snap)
	if got := restored.PerModel(); len(got) != 2 {
		t.Fatalf("restored per-model len=%d want 2: %+v", len(got), got)
	}
}

func Test_UsageTracker_RecordUsesActualUsageModel(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	tk.Record("writer", "", agentcore.Message{
		Role: agentcore.RoleAssistant,
		Usage: &agentcore.Usage{
			Provider: "openrouter",
			Model:    "google/gemini-2.5-pro",
			Input:    1000,
			Output:   200,
		},
	})

	perModel := tk.PerModel()
	if len(perModel) != 1 {
		t.Fatalf("per-model len=%d want 1: %+v", len(perModel), perModel)
	}
	if perModel[0].Model != "openrouter/google/gemini-2.5-pro" {
		t.Fatalf("model key = %q, want openrouter/google/gemini-2.5-pro", perModel[0].Model)
	}
	if perModel[0].Input != 1000 || perModel[0].Output != 200 {
		t.Fatalf("model totals = %+v", perModel[0])
	}
}

func Test_UsageTracker_ProviderOnlyDoesNotInventModelKey(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	tk.Record("writer", "", agentcore.Message{
		Role: agentcore.RoleAssistant,
		Usage: &agentcore.Usage{
			Provider: "openrouter",
			Input:    1000,
			Output:   200,
		},
	})

	if got := tk.PerModel(); len(got) != 0 {
		t.Fatalf("provider-only usage must not create model stats without a model, got %+v", got)
	}
}

// Test_UsageTracker_RecentWindowReflectsLatest kiểm tra cửa sổ trượt phản ánh
// "N lần gần nhất", không bị kéo lùi bởi tỉ lệ trúng thấp ở đầu — đây chính là
// vấn đề "pha đầu kéo lùi vs trạng thái ổn định trúng thấp" mà P1 cần giải.
func Test_UsageTracker_RecentWindowReflectsLatest(t *testing.T) {
	tk := NewUsageTracker(nil, nil)

	// 5 lần đầu trúng thấp cực thấp (kịch bản chương đầu)
	for i := 0; i < 5; i++ {
		tk.Record("writer", "", makeUsageMsg(1000, 0, 0, 200))
	}
	// 8 lần sau (>5) trúng thấp cao (kịch bản trạng thái ổn định)
	for i := 0; i < 8; i++ {
		tk.Record("writer", "", makeUsageMsg(1000, 900, 0, 200))
	}

	per := tk.PerAgent()
	if len(per) != 1 {
		t.Fatalf("len=%d want 1", len(per))
	}
	w := per[0]

	// Tích luỹ: 13 lần thì 8 lần có trúng → 7200/13000 ≈ 55.4%
	cumulativeRate := float64(w.CacheRead) / float64(w.Input) * 100
	if cumulativeRate < 50 || cumulativeRate > 60 {
		t.Errorf("cumulative hit rate = %.1f%%, want ~55%%", cumulativeRate)
	}

	// Cửa sổ trượt: 10 lần gần nhất gồm 8 lần trúng cao + 2 lần trúng bằng 0 →
	// 7200/10000 = 72%
	if w.RecentSamples != recentSampleCap {
		t.Errorf("recent samples = %d, want %d (window full)", w.RecentSamples, recentSampleCap)
	}
	recentRate := float64(w.RecentCacheRead) / float64(w.RecentInput) * 100
	if recentRate < 70 || recentRate > 75 {
		t.Errorf("recent hit rate = %.1f%%, want ~72%% (proves window dropped early misses)", recentRate)
	}
	// Quan trọng: N lần gần nhất cao hơn hẳn mức tích luỹ, chứng minh các lần 0
	// ở đầu đã bị loại khỏi cửa sổ
	if recentRate <= cumulativeRate {
		t.Errorf("recent (%.1f%%) must exceed cumulative (%.1f%%) once window slides past early misses",
			recentRate, cumulativeRate)
	}
}

// Test_computeSaved kiểm tra thuật toán saved: CacheRead × (giá Input - giá
// CacheRead); khi chênh lệch giá ≤ 0 hoặc InputCost ≤ 0 thì trả về 0 (chi phí
// phát sinh của CacheWrite không được bù trừ).
func Test_computeSaved(t *testing.T) {
	cases := []struct {
		name  string
		usage agentcore.Usage
		entry models.ModelEntry
		want  float64
	}{
		{
			name:  "anthropic 5m trúng thấp tiết kiệm 90%",
			usage: agentcore.Usage{Input: 100_000, CacheRead: 80_000},
			entry: models.ModelEntry{InputCostPer1M: 3.0, CacheReadCostPer1M: 0.3},
			want:  80_000 * (3.0 - 0.3) / 1_000_000, // 0.216
		},
		{
			name:  "không trúng saved=0",
			usage: agentcore.Usage{Input: 100_000, CacheRead: 0},
			entry: models.ModelEntry{InputCostPer1M: 3.0, CacheReadCostPer1M: 0.3},
			want:  0,
		},
		{
			name:  "model chưa có giá saved=0",
			usage: agentcore.Usage{Input: 100_000, CacheRead: 50_000},
			entry: models.ModelEntry{InputCostPer1M: 0, CacheReadCostPer1M: 0},
			want:  0,
		},
		{
			name:  "chênh lệch giá bất thường saved=0",
			usage: agentcore.Usage{Input: 100_000, CacheRead: 50_000},
			entry: models.ModelEntry{InputCostPer1M: 1.0, CacheReadCostPer1M: 2.0}, // ngược lại cache còn đắt hơn
			want:  0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeSaved(tc.usage, tc.entry)
			if got != tc.want {
				t.Errorf("computeSaved=%v want %v", got, tc.want)
			}
		})
	}
}

// Test_UsageTracker_CacheCapableSticky kiểm tra CacheCapable một khi đặt true
// thì không bao giờ lùi.
// Lịch sử từng chạy model có hỗ trợ cache → dữ liệu trúng thấp tích luỹ hợp lệ;
// đổi giữa chừng sang model không hỗ trợ không được làm cờ lùi.
//
// Mô phỏng bằng cách gán thẳng perAgent (đường resolveCost cần ModelSet+Registry,
// đã được lớp tích hợp phủ).
func Test_UsageTracker_CacheCapableSticky(t *testing.T) {
	tk := NewUsageTracker(nil, nil)

	// Mô phỏng "từng chạy model có hỗ trợ cache + đã trúng"
	tk.perAgent["writer"] = &agentTotals{
		Input: 1000, CacheRead: 500, Output: 200, CacheCapable: true,
	}
	// Thêm một lần "gọi model không hỗ trợ cache"
	tk.Record("writer", "", makeUsageMsg(500, 0, 0, 100))

	per := tk.PerAgent()
	if len(per) != 1 || per[0].Role != "writer" {
		t.Fatalf("expected single writer entry, got %+v", per)
	}
	if !per[0].CacheCapable {
		t.Errorf("CacheCapable must remain true after switching to non-capable model")
	}
	if per[0].CacheRead != 500 || per[0].Input != 1500 {
		t.Errorf("totals after merge = (in=%d cr=%d), want (1500 500)",
			per[0].Input, per[0].CacheRead)
	}
}

// Test_UsageTracker_PerAgentSkipsZero kiểm tra role không tiêu thụ token không
// xuất hiện trong PerAgent.
func Test_UsageTracker_PerAgentSkipsZero(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	// Dựng một role nhưng không tiêu thụ token (trường hợp biên)
	tk.perAgent["ghost"] = &agentTotals{}
	tk.Record("writer", "", makeUsageMsg(100, 50, 0, 20))

	per := tk.PerAgent()
	if len(per) != 1 || per[0].Role != "writer" {
		t.Fatalf("ghost role with zero tokens must be skipped, got %+v", per)
	}
}

// Test_UsageTracker_MissingAssistantUsageCounted kiểm tra ranh giới phán đoán của
// bộ đếm missingAssistantUsage:
//   - Đường cộng dồn chỉ xét Usage != nil (không buộc vào Role)
//   - Đường chẩn đoán đòi hỏi Role=Assistant và Content không rỗng — đó mới
//     giống "một phản hồi LLM thật nhưng không lấy được usage", tương ứng với
//     biểu hiện điển hình khi streaming phía trên không gửi chunk final của
//     OpenAI include_usage. Các trường hợp khác (message user/tool, assistant
//     có content rỗng) đều không tính là missing.
func Test_UsageTracker_MissingAssistantUsageCounted(t *testing.T) {
	tk := NewUsageTracker(nil, nil)

	withContent := func(text string) agentcore.Message {
		return agentcore.Message{
			Role:    agentcore.RoleAssistant,
			Content: []agentcore.ContentBlock{agentcore.TextBlock(text)},
		}
	}

	// assistant + có Content + nil Usage → trông như phản hồi thật nhưng thiếu
	// usage, tính vào chẩn đoán
	tk.Record("writer", "", withContent("hi"))
	tk.Record("writer", "", withContent("again"))
	// assistant nhưng Content rỗng → đường khôi phục bất thường hoặc message giữ
	// chỗ, không tính là missing
	tk.Record("writer", "", agentcore.Message{Role: agentcore.RoleAssistant})
	// message user/tool vốn không mang usage, Content rỗng hay không cũng không
	// tính là missing
	tk.Record("writer", "", agentcore.Message{Role: agentcore.RoleUser, Content: []agentcore.ContentBlock{agentcore.TextBlock("u")}})
	tk.Record("writer", "", agentcore.Message{Role: agentcore.RoleTool, Content: []agentcore.ContentBlock{agentcore.TextBlock("t")}})
	// Bình thường có usage → đi đường cộng dồn, không tính vào chẩn đoán
	tk.Record("writer", "", makeUsageMsg(100, 50, 0, 20))

	if got := tk.MissingAssistantUsage(); got != 2 {
		t.Errorf("MissingAssistantUsage=%d, want 2", got)
	}
	_, in, _, _, _ := tk.Totals()
	if in != 100 {
		t.Errorf("đường cộng dồn bình thường bị phá, input=%d want 100", in)
	}
}

// Test_UsageTracker_CacheCapableFromFacts kiểm tra CacheCapable vẫn được đánh
// dấu true theo "sự thật" khi không tra được model trong registry: model backend
// tự dựng / qua proxy trong nước thường không có trong pricing index của
// BerriAI/litellm nên resolveCost trả capable=false; nhưng chỉ cần backend thật
// sự trả về CacheRead hoặc CacheWrite > 0 là đã chứng minh model đó khách
// quan hỗ trợ prompt cache, dòng per-role không nên hiện "chưa bật".
func Test_UsageTracker_CacheCapableFromFacts(t *testing.T) {
	tk := NewUsageTracker(nil, nil) // modelSet=nil → resolveCost luôn capable=false

	// Một lần có CacheWrite (mô phỏng lần ghi cache đầu tiên; registry không đánh
	// dấu capable nhưng sự thật chứng minh là có hỗ trợ)
	tk.Record("writer", "", makeUsageMsg(1000, 0, 200, 100))
	per := tk.PerAgent()
	if len(per) != 1 || !per[0].CacheCapable {
		t.Fatalf("CacheWrite > 0 phải đánh dấu CacheCapable=true ngay, got %+v", per)
	}
	if !tk.OverallCacheCapable() {
		t.Errorf("overall CacheCapable cũng phải được đặt true đồng bộ")
	}

	// Chiều ngược lại: role hoàn toàn không có hoạt động cache thì CacheCapable phải
	// giữ false
	tk.Record("editor", "", makeUsageMsg(500, 0, 0, 100))
	per = tk.PerAgent()
	for _, a := range per {
		if a.Role == "editor" && a.CacheCapable {
			t.Errorf("editor xuyên suốt không có CacheRead/Write, CacheCapable không được bị đánh dấu nhầm là true")
		}
	}
}

// Test_UsageTracker_AccumulatesAnyRoleWithUsage kiểm tra đường cộng dồn tách rời
// khỏi Role:
// kể cả khi sau này một adapter gắn usage vào message của vai trò không phải
// assistant, vẫn phải cộng dồn đúng. Giữ contract "quy tắc lắp ráp tách rời quy
// tắc cộng dồn".
func Test_UsageTracker_AccumulatesAnyRoleWithUsage(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	// Dựng một message không phải assistant mà mang Usage — trường hợp lý thuyết
	// không phổ biến
	hypothetical := agentcore.Message{
		Role:  agentcore.RoleSystem,
		Usage: &agentcore.Usage{Input: 200, Output: 50, CacheRead: 100},
	}
	tk.Record("writer", "", hypothetical)

	_, in, out, cr, _ := tk.Totals()
	if in != 200 || out != 50 || cr != 100 {
		t.Errorf("chưa cộng dồn theo trường Usage, got (in=%d out=%d cr=%d) want (200 50 100)", in, out, cr)
	}
	if tk.MissingAssistantUsage() != 0 {
		t.Errorf("có Usage không được tính vào missing")
	}
}

// Test_UsageTracker_OnCostCallback kiểm tra điểm nối của sentinel ngân sách: sau
// mỗi lần ghi sổ, callback ngoài khóa mang theo tổng chi phí tích luỹ mới nhất
// (gồm cả đường cost do provider tự báo).
func Test_UsageTracker_OnCostCallback(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	var got []float64
	tk.SetOnCost(func(total float64) { got = append(got, total) })

	msg := func(cost float64) agentcore.AgentMessage {
		return agentcore.Message{
			Role:  agentcore.RoleAssistant,
			Usage: &agentcore.Usage{Input: 100, Output: 10, Cost: &agentcore.Cost{Total: cost}},
		}
	}
	tk.Record("writer", "", msg(0.5))
	tk.Record("writer", "", msg(0.25))

	if len(got) != 2 || got[0] != 0.5 || got[1] != 0.75 {
		t.Fatalf("onCost should carry growing totals, got %v", got)
	}
}

// Test_UsageTracker_OnMissingUsageOnce kiểm tra callback vùng mù chỉ nổ lần đầu
// tiên.
func Test_UsageTracker_OnMissingUsageOnce(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	fired := 0
	tk.SetOnMissingUsage(func() { fired++ })

	noUsage := agentcore.Message{Role: agentcore.RoleAssistant, Content: []agentcore.ContentBlock{agentcore.TextBlock("Nội dung truyện")}}
	tk.Record("writer", "", noUsage)
	tk.Record("writer", "", noUsage)
	tk.Record("editor", "", noUsage)

	if fired != 1 {
		t.Fatalf("onMissingUsage should fire exactly once, got %d", fired)
	}
}

// TestCacheBreakDetection kiểm tra bốn hướng đi của phát hiện đứt chuỗi cache:
// trong cùng phiên, tiền tố tăng + trúng giảm mạnh → đứt; đổi task (spawn mới)
// → đổi đường cơ sở nên không so sánh; tiền tố ngắn lại (nén trong phiên) → chỉ
// đặt lại đường cơ sở, không cảnh báo; mức giảm không đạt cả hai ngưỡng (tương
// đối 5% và tuyệt đối 2000) → không cảnh báo.
func TestCacheBreakDetection(t *testing.T) {
	tk := NewUsageTracker(nil, nil)

	// Dựng đường cơ sở: tiền tố 30k, trúng 28k.
	tk.Record("writer", "Viết chương 1", makeUsageMsg(30000, 28000, 0, 100))
	if got := tk.OverallCacheBreaks(); got != 0 {
		t.Fatalf("message đầu tiên không được kết luận đứt, got %d", got)
	}

	// Trong cùng phiên, tiền tố tăng còn trúng giảm mạnh (28k→4k) → đứt.
	tk.Record("writer", "Viết chương 1", makeUsageMsg(34000, 4096, 0, 100))
	if got := tk.OverallCacheBreaks(); got != 1 {
		t.Fatalf("tiền tố tăng + trúng giảm mạnh phải kết luận 1 lần đứt, got %d", got)
	}

	// Trong cùng phiên, tiền tố ngắn lại (nén ngữ cảnh, 4.4k < 34k) → đặt lại đường
	// cơ sở, không cảnh báo.
	tk.Record("writer", "Viết chương 1", makeUsageMsg(4400, 0, 0, 100))
	if got := tk.OverallCacheBreaks(); got != 1 {
		t.Fatalf("tiền tố ngắn lại phải được coi là nén đặt lại, got %d", got)
	}

	// Trên đường cơ sở mới, hồi phục nhỏ (giảm < ngưỡng tuyệt đối 2000) → không
	// cảnh báo.
	tk.Record("writer", "Viết chương 1", makeUsageMsg(36000, 30000, 0, 100))
	tk.Record("writer", "Viết chương 1", makeUsageMsg(38000, 28500, 0, 100))
	if got := tk.OverallCacheBreaks(); got != 1 {
		t.Fatalf("mức giảm 1.5k chưa vượt ngưỡng tuyệt đối thì không được cảnh báo, got %d", got)
	}

	// Đổi task = spawn mới = dòng cache mới: dù request đầu của phiên mới có
	// tiền tố không ngắn hơn request cuối phiên trước (38k → 40k) và trúng giảm
	// mạnh (28.5k→0), vẫn không so sánh, không cảnh báo. Đây là ca hồi quy
	// "báo động giả ở các phiên ngắn liên tiếp": chiều phát hiện phải khớp với
	// thang đo hạt của prompt_cache_key.
	tk.Record("writer", "Viết chương 2", makeUsageMsg(40000, 0, 0, 100))
	if got := tk.OverallCacheBreaks(); got != 1 {
		t.Fatalf("đổi task là đổi đường cơ sở, xuyên phiên không được so sánh, got %d", got)
	}

	// Trong phiên mới lại đứt → cảnh báo bình thường (chứng minh đường cơ sở mới
	// đã có hiệu lực).
	tk.Record("writer", "Viết chương 2", makeUsageMsg(45000, 38000, 0, 100))
	tk.Record("writer", "Viết chương 2", makeUsageMsg(48000, 5000, 0, 100))
	if got := tk.OverallCacheBreaks(); got != 2 {
		t.Fatalf("đứt trong phiên mới phải được phát hiện bình thường, got %d", got)
	}

	// Mức giảm tương đối <5% (100k→96k, giảm 4%) → không cảnh báo (dù giảm tuyệt
	// đối 4k > 2000).
	tk.Record("editor", "Thẩm duyệt cung 1", makeUsageMsg(120000, 100000, 0, 100))
	tk.Record("editor", "Thẩm duyệt cung 1", makeUsageMsg(125000, 96000, 0, 100))
	if got := tk.OverallCacheBreaks(); got != 2 {
		t.Fatalf("mức giảm tương đối 4%% chưa vượt ngưỡng 5%% thì không được cảnh báo, got %d", got)
	}

	// Quy về per-role: lần đứt được ghi dưới tên writer và đưa vào Snapshot.
	snap := tk.Snapshot()
	if snap.Overall.CacheBreaks != 2 || snap.PerAgent["writer"].CacheBreaks != 2 {
		t.Fatalf("bộ đếm đứt phải vào snapshot: overall=%d writer=%d", snap.Overall.CacheBreaks, snap.PerAgent["writer"].CacheBreaks)
	}
}
