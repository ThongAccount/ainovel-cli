// Package models cung cấp sổ đăng ký metadata model LLM (cửa sổ ngữ cảnh, giới hạn đầu ra, giá),
// lấy dữ liệu từ OpenRouter API, gồm baseline thời gian biên dịch + làm mới lúc runtime.
package models

//go:generate go run gen_models.go

import (
	"strings"
	"sync"
)

// ModelEntry mô tả một model LLM đã biết.
type ModelEntry struct {
	Provider            string  `json:"provider"`               // tên nhà sản xuất sau khi OpenRouter chuẩn hoá (anthropic/openai/gemini/...)
	ID                  string  `json:"id"`                     // ID của model (không kèm tiền tố nhà sản xuất)
	Name                string  `json:"name"`                   // tên hiển thị
	ContextWindow       int     `json:"context_window"`         // cửa sổ đầu vào
	MaxTokens           int     `json:"max_tokens"`             // giới hạn đầu ra mỗi lần
	InputCostPer1M      float64 `json:"input_cost_per_1m"`      // giá đầu vào (USD/1M token)
	OutputCostPer1M     float64 `json:"output_cost_per_1m"`     // giá đầu ra
	CacheReadCostPer1M  float64 `json:"cache_read_cost_per_1m"` // giá đọc cache
	CacheWriteCostPer1M float64 `json:"cache_write_cost_per_1m"`
}

// ModelRegistry lưu các model đã biết, hỗ trợ phân giải mờ và gộp lúc runtime.
type ModelRegistry struct {
	mu     sync.RWMutex
	models []ModelEntry
}

// NewModelRegistry trả về một sổ đăng ký đã nạp baseline thời gian biên dịch.
func NewModelRegistry() *ModelRegistry {
	r := &ModelRegistry{}
	r.models = append(r.models, generatedModels...)
	return r
}

var (
	defaultRegistry     *ModelRegistry
	defaultRegistryOnce sync.Once
)

// DefaultRegistry trả về sổ đăng ký toàn cục (nạp trễ, an toàn khi chạy đa luồng).
// Gọi StartPricingRefresh ở giai đoạn khởi động để nền làm mới giá/thông tin cửa sổ.
func DefaultRegistry() *ModelRegistry {
	defaultRegistryOnce.Do(func() {
		defaultRegistry = NewModelRegistry()
	})
	return defaultRegistry
}

// Resolve tìm mục theo một định danh model (có thể là "provider/model", ID đầy đủ, hoặc tên rút gọn).
//
// Thứ tự khớp:
//  1. nếu chứa "/", tra chính xác theo "provider/model"
//  2. khớp chính xác / khớp kèm hậu tố ngày
//  3. khớp chuỗi con (ID hoặc Name chứa pattern)
//
// Khi trúng nhiều mục, ưu tiên trả về bí danh không có hậu tố ngày (ví dụ claude-sonnet-4 được ưu tiên hơn claude-sonnet-4-20250514).
func (r *ModelRegistry) Resolve(pattern string) (*ModelEntry, bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if idx := strings.Index(pattern, "/"); idx > 0 {
		prov := pattern[:idx]
		modelID := pattern[idx+1:]
		if entry, ok := lookupModelEntry(r.models, prov, modelID); ok {
			return &entry, true
		}
		// Tiền tố vendor của OpenRouter (google/, x-ai/) không nhất thiết trùng tên Provider cục bộ,
		// nên lùi về tra bằng modelID, đảm bảo "google/gemini-2.5-pro" vẫn trúng mục gemini.
		if entry, ok := lookupModelEntry(r.models, "", modelID); ok {
			return &entry, true
		}
	}

	if entry, ok := lookupModelEntry(r.models, "", pattern); ok {
		return &entry, true
	}

	lower := strings.ToLower(pattern)
	normalized := normalizeModelLookupID(pattern)
	var candidates []int
	for i := range r.models {
		if strings.Contains(normalizeModelLookupID(r.models[i].ID), normalized) ||
			strings.Contains(strings.ToLower(r.models[i].ID), lower) ||
			strings.Contains(strings.ToLower(r.models[i].Name), lower) {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return nil, false
	}

	best := candidates[0]
	for _, i := range candidates[1:] {
		if !hasDatedSuffix(r.models[i].ID) && hasDatedSuffix(r.models[best].ID) {
			best = i
		}
	}
	entry := r.models[best]
	return &entry, true
}

// ResolveContextWindow trả về cửa sổ ngữ cảnh của một model; không trúng thì trả về 0.
func (r *ModelRegistry) ResolveContextWindow(pattern string) int {
	if e, ok := r.Resolve(pattern); ok {
		return e.ContextWindow
	}
	return 0
}

// List trả về toàn bộ model (có thể lọc theo filter, chuỗi rỗng nghĩa là lấy tất cả).
func (r *ModelRegistry) List(filter string) []ModelEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if filter == "" {
		return append([]ModelEntry{}, r.models...)
	}
	lower := strings.ToLower(filter)
	normalized := normalizeModelLookupID(filter)
	var out []ModelEntry
	for _, m := range r.models {
		if strings.Contains(strings.ToLower(m.Provider), lower) ||
			strings.Contains(normalizeModelLookupID(m.ID), normalized) ||
			strings.Contains(strings.ToLower(m.ID), lower) ||
			strings.Contains(strings.ToLower(m.Name), lower) {
			out = append(out, m)
		}
	}
	return out
}

// MergeModels gộp theo provider+id, không phân biệt hoa thường.
// Giá/cửa sổ/MaxTokens/Name khác 0 sẽ ghi đè mục đã có; mục mới thì thêm thẳng vào.
func (r *ModelRegistry) MergeModels(fetched []ModelEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx := make(map[string]int, len(r.models))
	for i, m := range r.models {
		idx[strings.ToLower(m.Provider+"/"+m.ID)] = i
	}
	for _, f := range fetched {
		key := strings.ToLower(f.Provider + "/" + f.ID)
		if i, ok := idx[key]; ok {
			if f.InputCostPer1M > 0 || f.OutputCostPer1M > 0 {
				r.models[i].InputCostPer1M = f.InputCostPer1M
				r.models[i].OutputCostPer1M = f.OutputCostPer1M
				r.models[i].CacheReadCostPer1M = f.CacheReadCostPer1M
				r.models[i].CacheWriteCostPer1M = f.CacheWriteCostPer1M
			}
			if f.ContextWindow > 0 {
				r.models[i].ContextWindow = f.ContextWindow
			}
			if f.MaxTokens > 0 {
				r.models[i].MaxTokens = f.MaxTokens
			}
			if f.Name != "" {
				r.models[i].Name = f.Name
			}
		} else {
			r.models = append(r.models, f)
			idx[key] = len(r.models) - 1
		}
	}
}
