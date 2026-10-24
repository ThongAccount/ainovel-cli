package domain

import "time"

// UsageSchemaVersion là phiên bản tương thích của meta/usage.json.
// Nếu ngữ nghĩa trường AgentUsageTotals thay đổi trong tương lai, tăng giá trị này;
// UsageStore.Load gặp phiên bản khác phải bỏ qua và kích hoạt replay để dựng lại.
const UsageSchemaVersion = 2

// UsageState là ảnh chụp có thể lưu bền của mức dùng token / cost tích lũy.
// Trong bộ nhớ, UsageTracker duy trì và định kỳ ghi xuống meta/usage.json sau khi
// debounce.
//
// Lưu ý: các samples trong cửa sổ trượt bên trong UsageTracker ("tỷ lệ trúng của N
// lần gần nhất") **không được lưu bền** — chúng chỉ phục vụ chẩn đoán ngắn hạn
// trên UI; sau khi tiến trình khởi động lại, bộ đếm tích lũy lại từ đầu và ngữ nghĩa
// được phục hồi sau vài vòng. MissingAssistantUsage vẫn được lưu bền vì giá trị tích
// lũy xuyên các lần khởi động có giá trị chẩn đoán cao hơn.
type UsageState struct {
	Schema       int                         `json:"schema"`
	UpdatedAt    time.Time                   `json:"updated_at"`
	Overall      AgentUsageTotals            `json:"overall"`
	PerAgent     map[string]AgentUsageTotals `json:"per_agent"`
	PerModel     map[string]AgentUsageTotals `json:"per_model,omitempty"`
	MissingUsage int                         `json:"missing_assistant_usage"`
}

// AgentUsageTotals là dạng có thể lưu bền của bộ đếm tích lũy cho một vai trò
// (hoặc overall).
type AgentUsageTotals struct {
	Input        int     `json:"input"`
	Output       int     `json:"output"`
	CacheRead    int     `json:"cache_read"`
	CacheWrite   int     `json:"cache_write"`
	Cost         float64 `json:"cost_usd"`
	Saved        float64 `json:"saved_usd"`
	CacheCapable bool    `json:"cache_capable"`
	// CacheBreaks là số lần phát hiện live chuỗi bộ nhớ đệm bị đứt (tiền tố không
	// rút ngắn trong khi tỷ lệ trúng giảm mạnh).
	// Chỉ tích lũy trên đường live; session replay không phát lại phép phát hiện.
	CacheBreaks int `json:"cache_breaks,omitempty"`
}
