package bootstrap

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/voocel/agentcore/llm"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/models"
	"github.com/voocel/ainovel-cli/internal/notify"
	"github.com/voocel/ainovel-cli/internal/utils"
)

// DefaultContextWindow là cửa sổ ngữ cảnh dự phòng khi mô hình chưa được đăng ký trong registry.
const DefaultContextWindow = 200000

// CompactRatio là ngưỡng tương đối kích hoạt nén ngữ cảnh: tokens >= window * CompactRatio thì nén.
// 0.85 là giá trị thực nghiệm, chừa 15% đầu cho "prompt lượt sau + kết quả tool lớn", đồng thời
// để mô hình cửa sổ lớn cũng nén chủ động ở mốc 85%, tránh chờ đầy mới nén trong cửa sổ danh nghĩa 1M
// (vùng suy giảm chú ý).
//
// Tỉ lệ nén không mở cho người dùng cấu hình; người dùng chỉ khai báo context_window thật của từng mô hình.
const CompactRatio = 0.85

// MinCompactReserve là mức sàn của ReserveTokens. Mô hình cửa sổ nhỏ (ví dụ qwen3:8b cục bộ 32k)
// tính reserve theo tỉ lệ 0.15 chỉ được 4800, trong khi một phản hồi tool commit_chapter
// đã chứa 5-8k và một chương văn bản 8-15k — sẽ xảy ra "nén xong lại vượt ngay".
// Mức sàn 8000 bảo đảm kịch bản xấu nhất vẫn còn nửa vòng đệm.
const MinCompactReserve = 8000

// CompactReserveTokens tính ngược ReserveTokens theo CompactRatio và áp dụng sàn MinCompactReserve:
//
//	threshold = window - reserve = window * CompactRatio
//	reserve   = max(MinCompactReserve, window * (1 - CompactRatio))
//
// Dùng cho EngineConfig.ReserveTokens của agentcore.context.Engine.
func CompactReserveTokens(window int) int {
	if window <= 0 {
		return 0
	}
	reserve := window - int(float64(window)*CompactRatio)
	if reserve < MinCompactReserve {
		return MinCompactReserve
	}
	return reserve
}

// ProviderConfig định nghĩa thông tin xác thực của một nhà cung cấp LLM.
type ProviderConfig struct {
	Type    string        `json:"type,omitempty"`     // Loại giao thức API (openai/anthropic/gemini), chỉ định khi dùng proxy tùy chỉnh
	API     string        `json:"api,omitempty"`      // Endpoint theo giao thức OpenAI: chat (mặc định) / responses
	APIKey  string        `json:"api_key,omitempty"`  // API Key
	BaseURL string        `json:"base_url,omitempty"` // API Base URL
	Models  []ModelConfig `json:"models,omitempty"`   // Danh sách mô hình tùy chọn, hiển thị khi TUI chuyển mô hình
	// ExtraBody là tham số phụ được chuyển tiếp vào mỗi request của provider đó
	// (ví dụ temperature/top_p/min_p/presence_penalty, hoặc khóa riêng của hãng như
	// chat_template_kwargs bật think của nvidia).
	// Endpoint tương thích OpenAI sẽ gộp nguyên văn vào thân request (đúng quy ước extra_body);
	// giá trị do người dùng tự chịu trách nhiệm.
	ExtraBody map[string]any `json:"extra_body,omitempty"`
	// Extra được chuyển tiếp tới cấu hình cấp provider (litellm.ProviderConfig.Extra), dùng cho
	// các tuỳ chọn phía client/transport như HTTP headers, user_agent, anthropic_beta.
	Extra map[string]any `json:"extra,omitempty"`
	// StreamIdleTimeout là watchdog nhị rảnh của luồng: quá thời gian này mà không nhận được
	// chunk nào thì cắt luồng (chuỗi duration của Go, ví dụ "900s" / "15m").
	// Để trống thì mặc định 5m — trần hợp lý cho dịch vụ đám mây;
	// LocalAI/ollama hay tự dựng với suy luận chậm có thể mất hơn 5 phút mới ra chunk đầu,
	// chỉ cần nới riêng theo provider, không kéo theo phát hiện treo của các kênh khác (#79).
	StreamIdleTimeout string `json:"stream_idle_timeout,omitempty"`
}

// ModelConfig mô tả một mô hình có thể chuyển đổi dưới provider và cửa sổ ngữ cảnh tùy chọn của nó.
// Để tương thích cấu hình cũ, có thể đọc từ chuỗi JSON ("model-name") lẫn từ object;
// khi ghi lại luôn chuẩn hoá về dạng object.
type ModelConfig struct {
	Name          string `json:"name"`
	ContextWindow int    `json:"context_window,omitempty"`
	// JSONSchema là khai báo ba trạng thái cho đầu ra có cấu trúc gốc (response_format json_schema):
	// chưa cấu hình = tuỳ năng lực ở cấp mô hình của provider adapter; true = người dùng khẳng định
	// endpoint/mô hình đó hỗ trợ (khi request bị từ chối thì lỗi hiện nguyên văn, không hạ cấp âm thầm);
	// false = bắt buộc đi qua prompt contract.
	// Năng lực của proxy tùy chỉnh và cổng tổng hợp phụ thuộc khai báo của người dùng, chương trình không tự dò.
	JSONSchema *bool `json:"json_schema,omitempty"`
}

func (m *ModelConfig) UnmarshalJSON(data []byte) error {
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		m.Name = legacy
		m.ContextWindow = 0
		m.JSONSchema = nil
		return nil
	}
	type modelConfigAlias ModelConfig
	var decoded modelConfigAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("model config must be a string or object: %w", err)
	}
	*m = ModelConfig(decoded)
	return nil
}

// ModelConfig trả về cấu hình tường minh của mô hình được chỉ định.
func (pc ProviderConfig) ModelConfig(name string) (ModelConfig, bool) {
	name = strings.TrimSpace(name)
	for _, model := range pc.Models {
		if strings.TrimSpace(model.Name) == name {
			return model, true
		}
	}
	return ModelConfig{}, false
}

// ModelJSONSchema trả về khai báo ba trạng thái json_schema của mô hình; nếu mô hình không có trong
// models hoặc chưa cấu hình thì trả về nil (sẽ tuỳ năng lực của adapter).
func (c Config) ModelJSONSchema(provider, model string) *bool {
	if pc, ok := c.Providers[provider]; ok {
		if mc, ok := pc.ModelConfig(model); ok {
			return mc.JSONSchema
		}
	}
	return nil
}

// defaultStreamIdleTimeout: trong kịch bản đầu ra dài + ctx dài, provider có reasoning
// (mimo / deepseek-r1...) ở giai đoạn suy luận mà server không phát reasoning delta theo luồng
// thì cả đoạn SSE sẽ im lặng. Watchdog mặc định của litellm là 2 phút, thường giết nhầm
// các chương viết 8000 chữ; 5 phút bao phủ hầu hết các ca đo thực tế
// (xem thống kê thời lượng suy luận plan→draft trong tasks/todo.md).
const defaultStreamIdleTimeout = 5 * time.Minute

// StreamIdleTimeoutValue phân tích thời gian chờ nhị rảnh luồng của provider; để trống thì dùng giá trị mặc định.
func (pc ProviderConfig) StreamIdleTimeoutValue() (time.Duration, error) {
	s := strings.TrimSpace(pc.StreamIdleTimeout)
	if s == "" {
		return defaultStreamIdleTimeout, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (use Go duration like \"900s\" / \"15m\")", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be positive, got %q", s)
	}
	return d, nil
}

// RequiresAPIKey cho biết provider đó có bắt buộc phải cấu hình api_key hay không.
// Quy ước:
// 1. ollama / bedrock được phép không có key;
// 2. cấu hình có Type tường minh được coi là proxy tùy chỉnh, được phép không có key;
// 3. các provider khác mặc định yêu cầu key, giữ kiểm tra thận trọng với API chính thức.
func (pc ProviderConfig) RequiresAPIKey(name string) bool {
	switch name {
	case "ollama", "bedrock":
		return false
	}
	return pc.Type == ""
}

// ProviderType trả về loại giao thức API hợp lệ.
// Ưu tiên Type tường minh; nếu không thì tên provider phải có sẵn trong bảng đăng ký của litellm.
func (pc ProviderConfig) ProviderType(name string) (string, error) {
	if pc.Type != "" {
		return pc.Type, nil
	}
	if llm.IsProviderRegistered(name) {
		return name, nil
	}
	return "", fmt.Errorf("provider %q thiếu type và không có trong danh sách provider đã biết của litellm: %w", name, errs.ErrConfig)
}

// ModelRef biểu diễn một tổ hợp provider/model.
type ModelRef struct {
	Provider string `json:"provider"` // tên provider (key trong map Providers)
	Model    string `json:"model"`    // tên mô hình (chuyển tiếp nguyên văn, không phân tích)
}

// RoleConfig định nghĩa phần ghi đè mô hình cho một vai trò.
type RoleConfig struct {
	Provider  string     `json:"provider"`            // tên provider chính (key trong map Providers)
	Model     string     `json:"model"`               // tên mô hình chính (chuyển tiếp nguyên văn, không phân tích)
	Fallbacks []ModelRef `json:"fallbacks,omitempty"` // danh sách provider/model dự phòng tường minh
	// ReasoningEffort là cường độ suy luận của vai trò (off/low/medium/high/xhigh/max), rỗng = kế thừa mặc định cấp cao nhất.
	// Được agents.ParseThinkingLevel kiểm tra rồi mới áp dụng, giá trị vượt cấp bị coi là rỗng.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// knownRoles là tập tên vai trò có thể cấu hình. Arbiter hiện chưa mở cấu hình ở cấp vai trò,
// dùng chung mô hình mặc định cấp cao nhất (host.arbiterModel dùng models.Default).
// import_* là nút xoay chọn mức mô hình cho các hàm ngữ nghĩa nhập (docs/import-pipeline.md §13.1):
// chưa cấu hình thì rơi về architect, đã cấu hình thì có thể chỉ các hàm mang tính cơ học sang mức rẻ hơn.
var knownRoles = map[string]bool{
	"architect":         true,
	"writer":            true,
	"editor":            true,
	"import_segment":    true,
	"import_analyze":    true,
	"import_synthesize": true,
}

// Config là cấu hình ứng dụng tiểu thuyết.
type Config struct {
	// Trường runtime (không serialize ra JSON)
	OutputDir string `json:"-"` // thư mục gốc đầu ra

	// Cấu hình LLM mặc định
	Provider  string `json:"provider"` // provider mặc định (key trong map Providers)
	ModelName string `json:"model"`    // tên mô hình mặc định
	// ReasoningEffort là cường độ suy luận mặc định ở cấp cao nhất (off/low/medium/high/xhigh/max), rỗng = không ghi đè (giữ mặc định của mô hình/provider).
	// Khi vai trò chưa cấu hình reasoning_effort riêng thì rơi về giá trị này.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// Kho thông tin xác thực của provider
	Providers map[string]ProviderConfig `json:"providers,omitempty"`

	// Ghi đè mô hình ở cấp vai trò
	Roles map[string]RoleConfig `json:"roles,omitempty"`

	// Tham số sáng tác
	Style    string `json:"style,omitempty"`
	Language string `json:"language,omitempty"` // Ngôn ngữ sáng tác tiểu thuyết: "vi" (Tiếng Việt, mặc định) hoặc "zh" (Tiếng Trung)

	// ContextWindow là cửa sổ ngữ cảnh toàn cục phiên bản cũ, giữ lại để tương thích.
	ContextWindow int `json:"context_window,omitempty"`

	// Budget là chính sách ngân sách chi phí cho một cuốn sách; chỉ bật khi book_usd > 0.
	Budget BudgetConfig `json:"budget,omitzero"`

	// Notify là cấu hình cảnh báo không giám sát; mặc định bật (kênh system làm dự phòng).
	Notify NotifyConfig `json:"notify,omitzero"`
}

// BudgetConfig là tuyên bố chính sách của người dùng cho ví tiền của một cuốn sách.
// Dừng máy khi vượt ngưỡng tương đương người dùng bấm Abort thủ công tại thời điểm đó —
// Host chỉ thay người dùng thực thi, không đánh giá hành vi của mô hình (ranh giới hợp lệ §10 của kiến trúc).
type BudgetConfig struct {
	BookUSD   float64 `json:"book_usd,omitempty"`   // bắt buộc mới bật; 0/không có = không giới hạn
	WarnRatio float64 `json:"warn_ratio,omitempty"` // mực cảnh báo, mặc định 0.8
	HardStop  bool    `json:"hard_stop,omitempty"`  // true = dừng ngay khi vượt; mặc định chờ tác vụ tiểu tác nhân hiện tại xong
}

// Enabled cho biết chính sách ngân sách có bật hay không.
func (b BudgetConfig) Enabled() bool { return b.BookUSD > 0 }

// NotifyConfig là cấu hình kênh cảnh báo không giám sát.
type NotifyConfig struct {
	Enabled *bool    `json:"enabled,omitempty"` // mặc định true (kênh system dùng được mà không cần cấu hình)
	Command string   `json:"command,omitempty"` // tuỳ chọn, khi cấu hình sẽ thay thế kênh system (đẩy điện thoại đi qua đây)
	Events  []string `json:"events,omitempty"`  // tuỳ chọn, lọc theo notify.Kinds; mặc định bật tất cả
}

// IsEnabled cho biết cảnh báo có bật hay không (mặc định true).
func (n NotifyConfig) IsEnabled() bool { return n.Enabled == nil || *n.Enabled }

// ValidateBase kiểm tra hợp lệ cấu hình cơ bản.
func (c *Config) ValidateBase() error {
	if err := validateConfigText("provider", c.Provider); err != nil {
		return err
	}
	if err := validateConfigText("model", c.ModelName); err != nil {
		return err
	}

	if c.Provider == "" {
		return fmt.Errorf("provider is required: %w", errs.ErrConfig)
	}
	if c.ModelName == "" {
		return fmt.Errorf("model is required: %w", errs.ErrConfig)
	}

	// provider mặc định bắt buộc phải có thông tin xác thực
	pc, ok := c.Providers[c.Provider]
	if !ok {
		return fmt.Errorf("provider %q chưa khai báo thông tin xác thực trong providers; nếu bạn ghi đè provider trong ./.ainovel/config.json thì phải khai báo kèm providers.%s (gồm api_key/base_url), không thể chỉ đổi provider ở cấp cao nhất: %w", c.Provider, c.Provider, errs.ErrConfig)
	}
	if pc.RequiresAPIKey(c.Provider) && pc.APIKey == "" {
		return fmt.Errorf("provider %q has no api_key configured: %w", c.Provider, errs.ErrConfig)
	}
	if err := validateProviderConfigText(c.Provider, pc); err != nil {
		return err
	}
	if err := c.validateProviderAPI("default", c.Provider, pc); err != nil {
		return err
	}
	for name, provider := range c.Providers {
		if err := validateConfigText("provider name", name); err != nil {
			return err
		}
		if err := validateProviderConfigText(name, provider); err != nil {
			return err
		}
		if err := c.validateProviderAPI(fmt.Sprintf("provider %q", name), name, provider); err != nil {
			return err
		}
	}

	// Kiểm tra phần ghi đè ở cấp vai trò
	for role, rc := range c.Roles {
		if err := validateConfigText("role name", role); err != nil {
			return err
		}
		if err := validateConfigText(fmt.Sprintf("role %q provider", role), rc.Provider); err != nil {
			return err
		}
		if err := validateConfigText(fmt.Sprintf("role %q model", role), rc.Model); err != nil {
			return err
		}
		if !knownRoles[role] {
			return fmt.Errorf("unknown role %q in roles config (valid: architect/writer/editor/import_segment/import_analyze/import_synthesize): %w", role, errs.ErrConfig)
		}
		if rc.Provider == "" || rc.Model == "" {
			return fmt.Errorf("role %q must have both provider and model: %w", role, errs.ErrConfig)
		}
		if err := c.validateModelRef(
			fmt.Sprintf("role %q", role),
			ModelRef{Provider: rc.Provider, Model: rc.Model},
		); err != nil {
			return err
		}
		for i, fallback := range rc.Fallbacks {
			if err := validateConfigText(fmt.Sprintf("role %q fallback[%d] provider", role, i), fallback.Provider); err != nil {
				return err
			}
			if err := validateConfigText(fmt.Sprintf("role %q fallback[%d] model", role, i), fallback.Model); err != nil {
				return err
			}
			if err := c.validateModelRef(
				fmt.Sprintf("role %q fallback[%d]", role, i),
				fallback,
			); err != nil {
				return err
			}
		}
	}

	// Kiểm tra chính sách ngân sách
	if c.Budget.BookUSD < 0 {
		return fmt.Errorf("budget.book_usd must be >= 0: %w", errs.ErrConfig)
	}
	if c.Budget.Enabled() && (c.Budget.WarnRatio <= 0 || c.Budget.WarnRatio >= 1) {
		return fmt.Errorf("budget.warn_ratio must be in (0, 1): %w", errs.ErrConfig)
	}

	// Kiểm tra cấu hình cảnh báo
	if err := validateConfigText("notify.command", c.Notify.Command); err != nil {
		return err
	}
	for _, ev := range c.Notify.Events {
		if !notify.IsKnownKind(ev) {
			return fmt.Errorf("unknown notify event %q (valid: %s): %w", ev, strings.Join(notify.Kinds(), "/"), errs.ErrConfig)
		}
	}

	return nil
}

func validateProviderConfigText(name string, pc ProviderConfig) error {
	fields := []struct {
		label string
		value string
	}{
		{label: fmt.Sprintf("provider %q type", name), value: pc.Type},
		{label: fmt.Sprintf("provider %q api", name), value: pc.API},
		{label: fmt.Sprintf("provider %q api_key", name), value: pc.APIKey},
		{label: fmt.Sprintf("provider %q base_url", name), value: pc.BaseURL},
	}
	for _, field := range fields {
		if err := validateConfigText(field.label, field.value); err != nil {
			return err
		}
	}
	seenModels := make(map[string]bool, len(pc.Models))
	for i, model := range pc.Models {
		modelName := strings.TrimSpace(model.Name)
		if err := validateConfigText(fmt.Sprintf("provider %q models[%d].name", name, i), model.Name); err != nil {
			return err
		}
		if modelName == "" {
			return fmt.Errorf("provider %q models[%d].name is required: %w", name, i, errs.ErrConfig)
		}
		if seenModels[modelName] {
			return fmt.Errorf("provider %q has duplicate model %q: %w", name, modelName, errs.ErrConfig)
		}
		seenModels[modelName] = true
		if model.ContextWindow < 0 {
			return fmt.Errorf("provider %q model %q context_window must be >= 0: %w", name, modelName, errs.ErrConfig)
		}
	}
	switch pc.API {
	case "", "chat", "responses":
	default:
		return fmt.Errorf("provider %q api must be chat or responses: %w", name, errs.ErrConfig)
	}
	if _, err := pc.StreamIdleTimeoutValue(); err != nil {
		return fmt.Errorf("provider %q stream_idle_timeout: %w: %w", name, err, errs.ErrConfig)
	}
	return nil
}

func validateConfigText(name, value string) error {
	if utils.ContainsControl(value) {
		return fmt.Errorf("%s contains control character: %w", name, errs.ErrConfig)
	}
	return nil
}

// DefaultProviderConfig trả về cấu hình thông tin xác thực của provider mặc định.
func (c *Config) DefaultProviderConfig() ProviderConfig {
	if c.Providers == nil {
		return ProviderConfig{}
	}
	return c.Providers[c.Provider]
}

// FillDefaults điền các giá trị mặc định.
func (c *Config) FillDefaults() {
	if c.OutputDir == "" {
		c.OutputDir = filepath.Join("output", "novel")
	}
	if c.Providers == nil {
		c.Providers = make(map[string]ProviderConfig)
	}
	if c.Roles == nil {
		c.Roles = make(map[string]RoleConfig)
	}
	if c.Style == "" {
		c.Style = "default"
	}
	if c.Language == "" {
		c.Language = "vi"
	} else {
		c.Language = strings.ToLower(strings.TrimSpace(c.Language))
	}
	if c.Budget.Enabled() && c.Budget.WarnRatio == 0 {
		c.Budget.WarnRatio = 0.8
	}
}

// NormalizedLanguage trả về ngôn ngữ nội dung tiểu thuyết đã chuẩn hóa ("vi" hoặc "zh").
func (c Config) NormalizedLanguage() string {
	lang := strings.ToLower(strings.TrimSpace(c.Language))
	if lang == "zh" || lang == "chinese" || lang == "cn" {
		return "zh"
	}
	return "vi"
}

// ContextWindowSource đánh dấu nguồn của giá trị cửa sổ, phục vụ nhật ký/chẩn đoán.
type ContextWindowSource string

const (
	CtxWindowModelConfig ContextWindowSource = "model_config" // chỉ định tường minh ở mục mô hình của provider
	CtxWindowConfig      ContextWindowSource = "config"       // chỉ định tường minh ở context_window cấp cao nhất (cũ)
	CtxWindowRegistry    ContextWindowSource = "registry"     // khớp đường cơ sở OpenRouter
	CtxWindowDefault     ContextWindowSource = "default"      // dự phòng (proxy tùy chỉnh / mô hình chưa biết)
)

// ResolveContextWindow phân giải cửa sổ ngữ cảnh hiệu lực dùng cho nén, theo thứ tự ưu tiên:
//  1. providers.<provider>.models[].context_window
//  2. ContextWindow cấp cao nhất (cũ, để tương thích cấu hình sẵn có)
//  3. models.DefaultRegistry tra theo tên mô hình (đường cơ sở OpenRouter + làm mới 24h)
//  4. DefaultContextWindow dự phòng (proxy tùy chỉnh / mô hình chưa biết)
//
// Lưu ý: giá trị trả về chỉ dùng để tính ngưỡng nén, không làm giảm độ dài request mà API LLM thực sự chấp nhận.
func (c Config) ResolveContextWindow(provider, modelName string) (int, ContextWindowSource) {
	if pc, ok := c.Providers[strings.TrimSpace(provider)]; ok {
		if model, found := pc.ModelConfig(modelName); found && model.ContextWindow > 0 {
			return model.ContextWindow, CtxWindowModelConfig
		}
	}
	if c.ContextWindow > 0 {
		return c.ContextWindow, CtxWindowConfig
	}
	if rw := models.DefaultRegistry().ResolveContextWindow(modelName); rw > 0 {
		return rw, CtxWindowRegistry
	}
	return DefaultContextWindow, CtxWindowDefault
}

// ResolveReasoningEffort trả về chuỗi cường độ suy luận gốc đang có hiệu lực cho một vai trò
// (off/low/medium/high/xhigh/max hoặc rỗng).
// Thứ tự ưu tiên: cấp vai trò Roles[role].ReasoningEffort → mặc định cấp cao nhất ReasoningEffort → "" (không ghi đè, giữ mặc định của mô hình/provider).
// Khi role rỗng hoặc "default" thì lấy thẳng mặc định cấp cao nhất. Tính hợp lệ của giá trị do agents.ParseThinkingLevel bảo đảm.
func (c Config) ResolveReasoningEffort(role string) string {
	if role != "" && role != "default" {
		if rc, ok := c.Roles[role]; ok && rc.ReasoningEffort != "" {
			return rc.ReasoningEffort
		}
	}
	return c.ReasoningEffort
}

// LogContextWindowChoice in ra quyết định chọn cửa sổ của một vai trò. Khi source=default thì ghi Warn
// để báo mô hình đó không khớp trong registry (OpenRouter cũng chưa thu thập), nén ngữ cảnh về sau sẽ
// kích hoạt theo cửa sổ dự phòng — nếu cửa sổ thật của mô hình lớn hơn, hãy chỉ định tường minh
// context_window trong tệp cấu hình để tránh nén sớm và mất lịch sử.
func LogContextWindowChoice(role, model string, window int, source ContextWindowSource) {
	attrs := []any{"module", "context", "role", role, "model", model, "window", window, "source", source}
	switch source {
	case CtxWindowModelConfig:
		slog.Info("Cửa sổ ngữ cảnh (từ cấu hình mô hình của provider)", attrs...)
	case CtxWindowDefault:
		slog.Warn("Mô hình chưa được nhận diện, dùng cửa sổ dự phòng (có thể chỉ định tường minh tại providers.<name>.models[].context_window)", attrs...)
	case CtxWindowConfig:
		slog.Info("Cửa sổ ngữ cảnh (từ context_window trong tệp cấu hình)", attrs...)
	default:
		slog.Info("Cửa sổ ngữ cảnh", attrs...)
	}
}

// CandidateModels trả về danh sách mô hình có thể chuyển đổi dưới một provider.
// Ưu tiên các models provider khai báo tường minh; đồng thời bổ sung những mô hình của provider đó
// đã xuất hiện trong cấu hình hiện tại.
func (c Config) CandidateModels(provider string) []string {
	if provider == "" {
		return nil
	}

	seen := make(map[string]bool)
	models := make([]string, 0, 4)
	add := func(model string) {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			return
		}
		seen[model] = true
		models = append(models, model)
	}

	if pc, ok := c.Providers[provider]; ok {
		for _, model := range pc.Models {
			add(model.Name)
		}
	}
	if c.Provider == provider {
		add(c.ModelName)
	}
	for _, rc := range c.Roles {
		if rc.Provider == provider {
			add(rc.Model)
		}
		for _, fallback := range rc.Fallbacks {
			if fallback.Provider == provider {
				add(fallback.Model)
			}
		}
	}
	return models
}

func (c Config) validateModelRef(owner string, ref ModelRef) error {
	if ref.Provider == "" || ref.Model == "" {
		return fmt.Errorf("%s must have both provider and model: %w", owner, errs.ErrConfig)
	}

	pc, ok := c.Providers[ref.Provider]
	if !ok {
		return fmt.Errorf("%s references provider %q which is not configured: %w", owner, ref.Provider, errs.ErrConfig)
	}
	if pc.RequiresAPIKey(ref.Provider) && pc.APIKey == "" {
		return fmt.Errorf("%s references provider %q which has no api_key: %w", owner, ref.Provider, errs.ErrConfig)
	}
	if err := c.validateProviderAPI(owner, ref.Provider, pc); err != nil {
		return err
	}
	return nil
}

func (c Config) validateProviderAPI(owner, providerName string, pc ProviderConfig) error {
	if pc.API == "" {
		return nil
	}
	providerType, err := pc.ProviderType(providerName)
	if err != nil {
		return fmt.Errorf("%s provider %q api không phân tích được loại giao thức: %w", owner, providerName, err)
	}
	if strings.ToLower(strings.TrimSpace(providerType)) != "openai" {
		return fmt.Errorf("%s provider %q api chỉ hỗ trợ provider theo giao thức OpenAI: %w", owner, providerName, errs.ErrConfig)
	}
	return nil
}
