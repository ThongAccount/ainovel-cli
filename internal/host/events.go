package host

import (
	"time"
)

// Event là sự kiện có cấu trúc mà TUI tiêu thụ.
//
// Với ba loại sự kiện gọi (TOOL / DISPATCH / DECISION), lúc bắt đầu và lúc kết
// thúc của cùng một lần gọi dùng chung một ID:
// lúc bắt đầu phát trước một sự kiện có FinishedAt bằng giá trị 0 (TUI hiển thị theo
// kiểu "đang chạy");
// lúc kết thúc phát thêm một sự kiện cùng ID, điền FinishedAt + Duration (+ Failed),
// TUI định vị dòng gốc theo ID để cập nhật tại chỗ, tránh "một dòng bắt đầu rồi lại
// thêm một dòng hoàn thành" dư thừa.
//
// Sự kiện không thuộc loại gọi (SYSTEM / ERROR / CONTEXT ...) để trống ID,
// mỗi sự kiện được thêm riêng.
type Event struct {
	ID         string    // dùng chung cho lúc bắt đầu/kết thúc của một lần gọi; sự kiện không thuộc loại gọi thì rỗng
	Time       time.Time // thời điểm phát đầu tiên (lúc bắt đầu)
	FinishedAt time.Time // giá trị 0 = đang chạy; khác 0 = đã hoàn thành
	Failed     bool      // đã hoàn thành nhưng thất bại (chỉ có ý nghĩa ở trạng thái hoàn thành)
	Category   string    // DISPATCH / TOOL / DECISION / SYSTEM / REVIEW / CHECK / ERROR / CONTEXT
	Agent      string    // agent sinh ra sự kiện
	Summary    string
	Detail     string        // văn bản đầy đủ, ghi vào nhật ký không cắt bớt để phục vụ truy vết; rỗng thì lùi về Summary. UI chỉ đọc Summary
	Kind       string        // phân loại lỗi (ví dụ stream_idle), đi kèm khi ghi nhật ký để lọc/cảnh báo; rỗng thì không xuất
	Level      string        // info / warn / error / success
	Depth      int           // 0 = tầng Engine, 1 = tầng Worker
	Duration   time.Duration // thời gian thực thi khi hoàn thành
	RetryAt    time.Time     // sự kiện thử lại: mốc chốt của lần thử tiếp theo; UI đếm ngược từng giây theo mốc này, tới hạn thì xoá (yêu cầu đã được gửi đi)
}

// Running trả về sự kiện có đang chạy hay không.
// Chỉ sự kiện loại gọi (TOOL / DISPATCH / DECISION có ID) mới có thể đang chạy;
// các loại khác luôn trả về false.
func (e Event) Running() bool {
	return e.hasLifecycle() && e.FinishedAt.IsZero()
}

func (e Event) hasLifecycle() bool {
	if e.ID == "" {
		return false
	}
	switch e.Category {
	case "TOOL", "DISPATCH", "DECISION":
		return true
	default:
		return false
	}
}

// UISnapshot là ảnh chụp trạng thái tổng hợp mà TUI dùng để dựng giao diện.
type UISnapshot struct {
	Provider             string
	BookTitle            string
	ModelName            string
	ModelContextWindow   int // cửa sổ ngữ cảnh của mô hình mặc định hiện tại (phân giải lại theo thời gian thực khi đổi bằng /model)
	ThinkingLevel        string
	Style                string
	RuntimeState         string // idle / running / pausing / paused / completed
	StatusLabel          string
	Phase                string
	Flow                 string
	CurrentChapter       int
	TotalChapters        int
	CompletedCount       int
	TotalWordCount       int
	InProgressChapter    int
	PendingRewrites      []int
	RewriteReason        string
	PendingSteer         string
	AdvanceMode          string
	AdvancePermitChapter int
	HasAdvanceHold       bool
	AdvanceHoldReason    string
	RecoveryLabel        string
	IsRunning            bool
	Agents               []AgentSnapshot

	// Lượng dùng tích luỹ (toàn bộ phiên, xuyên suốt mọi agent và các lần đổi mô hình)
	TotalInputTokens      int
	TotalOutputTokens     int
	TotalCacheReadTokens  int
	TotalCacheWriteTokens int
	TotalCostUSD          float64
	TotalSavedUSD         float64 // số USD tiết kiệm được nhờ trúng CacheRead (so với cách tính đều theo giá đầu vào không dùng cache)
	BudgetLimitUSD        float64 // trần ngân sách (config budget.book_usd); 0 = không bật

	// Chẩn đoán cache
	OverallCacheCapable    bool // có ít nhất một role từng chạy mô hình hỗ trợ prompt cache (phân biệt "chưa bật" với "trúng 0%")
	OverallRecentCacheRead int  // tổng cacheRead của N lần gần nhất trong cửa sổ trượt
	OverallRecentInput     int  // tổng input của N lần gần nhất trong cửa sổ trượt
	OverallRecentSamples   int  // số mẫu trong cửa sổ trượt (≤ recentSampleCap)
	TotalCacheBreaks       int  // số lần chuỗi cache bị đứt phát hiện lúc chạy (tiền tố không ngắn lại mà tỷ lệ trúng giảm mạnh), xem usage.go noteCacheBreak

	// MissingAssistantUsage > 0 thường có nghĩa là upstream streaming không gửi
	// final usage chunk đúng giao thức OpenAI stream_options.include_usage (hay
	// gặp ở proxy tự dựng), khiến UsageTracker không nhận được dữ liệu tích luỹ nào.
	// UI dựa vào đó để chỉ thẳng nguyên nhân cho người dùng, tránh tưởng rằng
	// module cache tự nó đã hỏng.
	MissingAssistantUsage int

	// Số liệu cache theo từng role, sắp xếp giảm dần theo CacheRead, đã lọc bỏ
	// role chưa dùng token
	CachePerAgent []AgentCacheStat
	CachePerModel []AgentCacheStat

	// Thiết lập cơ bản
	Synopsis         string
	Premise          string
	Outline          []OutlineSnapshot
	Characters       []string
	SupportingCount  int      // tổng số nhân vật phụ trong danh mục nhân vật phụ
	RecentSupporting []string // nhân vật phụ hoạt động gần đây (tối đa 5, sắp theo LastSeenChapter giảm dần)
	Layered          bool
	CurrentVolumeArc string
	NextVolumeTitle  string
	CompassDirection string
	CompassScale     string

	// Chi tiết
	LastCommitSummary  string
	LastReviewSummary  string
	LastCheckpointName string
	RecentSummaries    []string
}

// OutlineSnapshot là tóm tắt hiển thị của một mục dàn ý.
type OutlineSnapshot struct {
	Chapter   int
	Title     string
	CoreEvent string
}

// AgentSnapshot là hình chiếu trạng thái của agent để hiển thị.
type AgentSnapshot struct {
	Name      string
	State     string
	TaskID    string
	TaskKind  string
	Summary   string
	Tool      string
	Turn      int
	Context   AgentContextSnapshot
	UpdatedAt time.Time
}

// AgentCacheStat là số liệu trúng cache tích luỹ của một agent (chiếu sang cột trái).
// HitRate = CacheRead / Input; ở tầng litellm, Input đã được thống nhất theo ngữ nghĩa
// "bao gồm CacheRead".
//
// CacheCapable dùng để phân biệt hai trường hợp trúng 0%:
//   - true  → mô hình hỗ trợ prompt cache, 0% là do thiết kế prompt kém hoặc tiền tố
//     không ổn định, cần tối ưu
//   - false → mô hình/provider không hỗ trợ prompt cache, 0% là điều đã lường trước,
//     không cần truy vết
//
// Recent* là dữ liệu trúng của cửa sổ trượt (N lần gọi gần nhất); so với số liệu
// tích luỹ để nhận ra "giai đoạn đầu kéo tụt" và "trạng thái ổn định trúng ít".
type AgentCacheStat struct {
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

// AgentContextSnapshot là mức dùng ngữ cảnh của agent.
type AgentContextSnapshot struct {
	Tokens          int
	ContextWindow   int
	Percent         float64
	Scope           string
	Strategy        string
	ActiveMessages  int
	SummaryMessages int
	CompactedCount  int
	KeptCount       int
}

// CoCreateMessage là một tin nhắn trong hội thoại đồng sáng tác.
type CoCreateMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CoCreateReply là câu trả lời của LLM trong hội thoại đồng sáng tác. Raw giữ nguyên
// bốn đoạn gốc của mô hình, dùng để ghi lại vào history để vòng sau mô hình nhìn
// thấy [DRAFT] của chính nó ở vòng trước, nhờ đó thực sự tích luỹ và cập nhật trên
// bản nháp đã có (chỉ có Message, thiếu [DRAFT], sẽ khiến mô hình mỗi vòng lại tự
// tổng hợp từ hội thoại). Suggestions là danh sách "tiếp theo bạn có thể muốn nói"
// do AI chủ động đưa ra, giúp người dùng khi bí nhập liệu chỉ cần bấm phím số để
// điền nhanh vào ô nhập.
type CoCreateReply struct {
	Message     string
	Prompt      string
	Ready       bool
	Suggestions []string
	Raw         string
}
