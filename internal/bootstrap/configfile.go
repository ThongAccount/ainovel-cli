package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const configDirName = ".ainovel"

// DefaultConfigPath trả về đường dẫn tệp cấu hình toàn cục ~/.ainovel/config.json.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, configDirName, "config.json")
}

// DefaultConfigDir trả về đường dẫn thư mục ~/.ainovel; nếu không lấy được thư mục home thì trả về chuỗi rỗng.
// Chỉ dùng để đọc/ghi các tệp không bắt buộc phải tồn tại (ví dụ bộ nhớ đệm mô hình), không tự tạo thư mục.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, configDirName)
}

// configDir trả về đường dẫn thư mục ~/.ainovel, tạo ra nếu chưa có.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, configDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	return dir, nil
}

// projectConfigPath trả về đường dẫn tương đối của tệp cấu hình cấp dự án ./.ainovel/config.json.
// Dotdir cấp dự án mirror thư mục toàn cục ~/.ainovel/, dùng lại cùng configDirName; phân giải theo cwd.
func projectConfigPath() string {
	return filepath.Join(configDirName, "config.json")
}

// EffectiveConfigPath trả về tệp cấu hình mà thay đổi từ TUI (/config, /model) sẽ ghi lại:
// nếu thư mục dự án có ./.ainovel/config.json thì ghi vào đó — cùng chiều với việc lúc đọc, lớp dự án
// ghi đè lớp toàn cục, đảm bảo "sửa đúng bản đang có hiệu lực" và sửa xong là áp dụng ngay;
// nếu không thì ghi vào ~/.ainovel/config.json toàn cục.
// Chỉ sửa cấu hình dự án đã tồn tại, không tự sinh ra (tạo lớp ghi đè cấp dự án là hành động chủ động
// đặt tệp của người dùng).
func EffectiveConfigPath() string {
	rel := projectConfigPath()
	if _, err := os.Stat(rel); err == nil {
		if abs, err := filepath.Abs(rel); err == nil {
			return abs
		}
		return rel
	}
	return DefaultConfigPath()
}

// LoadConfig nạp và gộp cấu hình theo thứ tự ưu tiên:
//  1. ~/.ainovel/config.json (toàn cục)
//  2. ./.ainovel/config.json (ghi đè cấp dự án)
func LoadConfig() (Config, error) {
	var cfg Config

	// 1. Cấu hình toàn cục. Đây là nền tảng ưu tiên thấp nhất, tệp hỏng chỉ hạ xuống cảnh báo
	//    thay vì chặn — vì lớp cấp dự án còn ghi đè được; nếu thất bại cứng thì người dùng
	//    "toàn cục hỏng + cấu hình dự án hợp lệ" sẽ bị chặn ngoài cửa.
	if p := DefaultConfigPath(); p != "" {
		global, found, err := loadOptionalJSON(p)
		switch {
		case err != nil:
			slog.Warn("Cấu hình toàn cục phân tích thất bại, đã bỏ qua (có thể bị cấp dự án ghi đè)", "module", "config", "path", p, "err", err)
		case found:
			cfg = global
		}
	}

	// 2. Lớp ghi đè cấp dự án. Tệp hỏng thì báo lỗi rõ ràng: cấu hình người dùng chủ động đặt
	//    trong thư mục hiện tại mà nuốt im lặng sẽ khiến "đã cấu hình mà không có tác dụng"
	//    không có đường truy (issue #37).
	project, found, err := loadOptionalJSON(projectConfigPath())
	if err != nil {
		return cfg, fmt.Errorf("cấu hình cấp dự án ./.ainovel/config.json phân tích thất bại (vui lòng kiểm tra cú pháp JSON): %w", err)
	}
	if found {
		cfg = mergeConfig(cfg, project)
	}

	return cfg, nil
}

// loadOptionalJSON đọc một tệp cấu hình tuỳ chọn:
//   - tệp không tồn tại → (zero, false, nil), để lời gọi quyết định dùng giá trị mặc định/lớp trên
//   - tệp có nhưng phân tích thất bại → trả về lỗi (không còn nuốt im lặng — nếu không thì cấu hình
//     "đã cấu hình mà không có tác dụng" của người dùng không có đường truy, đúng là nguyên nhân gốc của issue #37)
func loadOptionalJSON(path string) (Config, bool, error) {
	cfg, err := loadJSONFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, false, nil
		}
		return Config{}, false, err
	}
	return cfg, true, nil
}

// LoadConfigFile đọc một tệp cấu hình JSON, hỗ trợ chú thích // trên từng dòng.
// Không gộp gì, chỉ trả về cấu hình của chính tệp đó. Nếu tệp không tồn tại thì trả về lỗi.
func LoadConfigFile(path string) (Config, error) {
	return loadJSONFile(path)
}

// loadJSONFile đọc tệp cấu hình JSON, hỗ trợ chú thích // trên từng dòng.
// Nếu tệp không tồn tại thì trả về lỗi (lời gọi quyết định có bỏ qua hay không).
func loadJSONFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cleaned := stripJSONComments(data)
	var cfg Config
	if err := json.Unmarshal(cleaned, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// mergeConfig gộp overlay vào base. Trường có giá trị khác 0 ghi đè, map gộp theo key.
func mergeConfig(base, overlay Config) Config {
	if overlay.Provider != "" {
		base.Provider = overlay.Provider
	}
	if overlay.ModelName != "" {
		base.ModelName = overlay.ModelName
	}
	if overlay.ReasoningEffort != "" {
		base.ReasoningEffort = overlay.ReasoningEffort
	}
	if overlay.Style != "" {
		base.Style = overlay.Style
	}
	if overlay.ContextWindow > 0 {
		base.ContextWindow = overlay.ContextWindow
	}

	// Providers: key của overlay ghi đè key cùng tên trong base
	if len(overlay.Providers) > 0 {
		if base.Providers == nil {
			base.Providers = make(map[string]ProviderConfig)
		}
		for k, v := range overlay.Providers {
			existing := base.Providers[k]
			if v.Type != "" {
				existing.Type = v.Type
			}
			if v.API != "" {
				existing.API = v.API
			}
			if v.APIKey != "" {
				existing.APIKey = v.APIKey
			}
			if v.BaseURL != "" {
				existing.BaseURL = v.BaseURL
			}
			if len(v.Models) > 0 {
				existing.Models = append([]ModelConfig(nil), v.Models...)
			}
			if len(v.ExtraBody) > 0 {
				existing.ExtraBody = cloneMap(v.ExtraBody)
			}
			if len(v.Extra) > 0 {
				existing.Extra = cloneMap(v.Extra)
			}
			base.Providers[k] = existing
		}
	}

	// Roles: key của overlay ghi đè key cùng tên trong base
	if len(overlay.Roles) > 0 {
		if base.Roles == nil {
			base.Roles = make(map[string]RoleConfig)
		}
		for k, v := range overlay.Roles {
			existing := base.Roles[k]
			if v.Provider != "" {
				existing.Provider = v.Provider
			}
			if v.Model != "" {
				existing.Model = v.Model
			}
			if len(v.Fallbacks) > 0 {
				existing.Fallbacks = append([]ModelRef(nil), v.Fallbacks...)
			}
			if v.ReasoningEffort != "" {
				existing.ReasoningEffort = v.ReasoningEffort
			}
			base.Roles[k] = existing
		}
	}

	// Budget / Notify: ghi đè cả khối (ngân sách/cảnh báo cấp dự án là tuyên bố chính sách độc lập,
	// không ghép từng trường với bản toàn cục)
	if overlay.Budget != (BudgetConfig{}) {
		base.Budget = overlay.Budget
	}
	if overlay.Notify.Enabled != nil || overlay.Notify.Command != "" || len(overlay.Notify.Events) > 0 {
		base.Notify = overlay.Notify
	}

	return base
}

func cloneMap(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// CloneConfig sao chép sâu các map/slice trong cấu hình sẽ bị sửa lúc runtime, để cấu hình ứng viên
// không làm bẩn cấu hình hiện hành.
func CloneConfig(cfg Config) Config {
	clone := cfg
	clone.Providers = make(map[string]ProviderConfig, len(cfg.Providers))
	for name, pc := range cfg.Providers {
		pc.Models = append([]ModelConfig(nil), pc.Models...)
		pc.Extra = cloneMap(pc.Extra)
		pc.ExtraBody = cloneMap(pc.ExtraBody)
		clone.Providers[name] = pc
	}
	clone.Roles = make(map[string]RoleConfig, len(cfg.Roles))
	for role, rc := range cfg.Roles {
		rc.Fallbacks = append([]ModelRef(nil), rc.Fallbacks...)
		clone.Roles[role] = rc
	}
	clone.Notify.Events = append([]string(nil), cfg.Notify.Events...)
	return clone
}

// SaveProviderConfig cập nhật thông tin xác thực và thư viện mô hình của một provider trong lớp cấu hình đích.
// Chỉ đụng vào mục providers, tuyệt đối không đụng lựa chọn provider/model ở cấp cao nhất —
// "đang dùng cái gì" thuộc về /model.
// Nếu đích chưa tồn tại thì tạo cấu hình tối thiểu; nếu đích hỏng thì từ chối ghi đè.
func SaveProviderConfig(path string, provider string, pc ProviderConfig) error {
	target, found, err := loadOptionalJSON(path)
	if err != nil {
		return err
	}
	if !found {
		target = Config{}
	}
	if target.Providers == nil {
		target.Providers = make(map[string]ProviderConfig)
	}
	target.Providers[provider] = pc
	return SaveConfig(path, target)
}

// stripJSONComments loại bỏ chú thích // trong JSON, theo dõi trạng thái dấu nháy để không xoá nhầm nội dung chuỗi.
func stripJSONComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false

	for i := 0; i < len(data); i++ {
		b := data[i]

		if escaped {
			out = append(out, b)
			escaped = false
			continue
		}

		if inString {
			out = append(out, b)
			if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
			continue
		}

		// Không nằm trong chuỗi
		if b == '"' {
			inString = true
			out = append(out, b)
			continue
		}

		// Phát hiện chú thích //
		if b == '/' && i+1 < len(data) && data[i+1] == '/' {
			// Nhảy tới cuối dòng
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
			continue
		}

		out = append(out, b)
	}

	return out
}

// WriteStartupError ghi nối lỗi chí tử trong giai đoạn khởi động vào ~/.ainovel/last-error.log
// và trả về đường dẫn tệp đó (best-effort, thất bại thì trả về chuỗi rỗng). Khi chạy bằng
// cách nhấp đúp, cửa sổ console đóng ngay khi tiến trình kết thúc nên lỗi chớp qua mất;
// ghi xuống đĩa là con đường duy nhất để người dùng loại đó truy vết về sau.
func WriteStartupError(msg string) string {
	dir := DefaultConfigDir()
	if dir == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, "last-error.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return ""
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "[%s] %s\n", time.Now().Format(time.RFC3339), msg); err != nil {
		return ""
	}
	return path
}

// SaveConfig ghi cấu hình vào đường dẫn chỉ định (định dạng JSON, thụt lề đẹp).
func SaveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}
