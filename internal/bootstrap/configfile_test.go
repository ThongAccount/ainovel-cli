package bootstrap

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/voocel/ainovel-cli/internal/errs"
)

const validGlobal = `{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": { "openrouter": { "api_key": "sk-test-123456" } }
}`

// writeGlobal ghi cấu hình toàn cục dưới một HOME cách ly và trả về HOME đó.
func writeGlobal(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	// os.UserHomeDir trên Windows đọc USERPROFILE; không đặt biến này sẽ đọc ~/.ainovel thật của máy.
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".ainovel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if content != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o644); err != nil {
			t.Fatalf("write global: %v", err)
		}
	}
	return home
}

// writeProjectConfig ghi cấu hình cấp dự án vào ./.ainovel/ dưới thư mục làm việc hiện tại.
// Trước khi gọi phải t.Chdir tới thư mục đích.
func writeProjectConfig(t *testing.T, content string) {
	t.Helper()
	if err := os.MkdirAll(".ainovel", 0o755); err != nil {
		t.Fatalf("mkdir .ainovel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(".ainovel", "config.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write project: %v", err)
	}
}

// Nguyên nhân gốc 3: ./.ainovel/config.json cấp dự án tồn tại nhưng là JSON hỏng, phải báo lỗi,
// không được nuốt im lặng rồi rơi về cấu hình toàn cục.
func TestLoadConfig_CorruptProjectFailsLoud(t *testing.T) {
	writeGlobal(t, validGlobal)
	proj := t.TempDir()
	t.Chdir(proj)
	// Bản chép tay thừa một dấu phẩy cuối — dạng JSON hỏng hay gặp nhất.
	writeProjectConfig(t, `{ "model": "x", }`)

	if _, err := LoadConfig(); err == nil {
		t.Fatal("tệp ./.ainovel/config.json hỏng phải báo lỗi, nhưng đã bị bỏ qua im lặng")
	}
}

// Cấu hình toàn cục là nền tảng ưu tiên thấp nhất: tệp hỏng không được chặn lớp ghi đè cấp dự án
// có ưu tiên cao hơn (chốt hồi quy — bản trước nhầm làm cả toàn cục fail-loud, khiến người dùng
// "toàn cục hỏng + cấu hình dự án hợp lệ" bị một tệp không liên quan chặn cửa).
func TestLoadConfig_CorruptGlobalDoesNotBlockProjectOverride(t *testing.T) {
	writeGlobal(t, `{ not json`)
	proj := t.TempDir()
	t.Chdir(proj)
	writeProjectConfig(t, validGlobal)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("toàn cục hỏng không nên chặn cấu hình cấp dự án hợp lệ, được: %v", err)
	}
	if cfg.Provider != "openrouter" {
		t.Errorf("phải dùng giá trị của cấu hình cấp dự án, được provider=%q", cfg.Provider)
	}
}

// Sửa tại chỗ: khi thư mục dự án có ./.ainovel/config.json thì EffectiveConfigPath trỏ tới nó (đường dẫn tuyệt đối),
// nếu không thì rơi về toàn cục — /config và /model đều dựa vào đó để chọn nơi ghi tệp.
func TestEffectiveConfigPathPrefersProject(t *testing.T) {
	writeGlobal(t, validGlobal)

	t.Chdir(t.TempDir()) // không có cấu hình dự án
	if got := EffectiveConfigPath(); got != DefaultConfigPath() {
		t.Fatalf("không có cấu hình dự án thì phải rơi về toàn cục, got %q want %q", got, DefaultConfigPath())
	}

	proj := t.TempDir()
	t.Chdir(proj)
	writeProjectConfig(t, validGlobal)
	wantAbs, err := filepath.Abs(filepath.Join(".ainovel", "config.json"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if got := EffectiveConfigPath(); got != wantAbs {
		t.Fatalf("có cấu hình dự án thì phải ghi vào dự án, got %q want %q", got, wantAbs)
	}
}

// Tệp không tồn tại là tình huống bình thường (bản di động/lần đầu), không được báo lỗi.
func TestLoadConfig_MissingFilesNoError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // ~/.ainovel/config.json không tồn tại
	t.Setenv("USERPROFILE", home)
	t.Chdir(t.TempDir()) // cũng không có ./.ainovel/config.json

	if _, err := LoadConfig(); err != nil {
		t.Fatalf("thiếu tệp cấu hình không nên báo lỗi, được: %v", err)
	}
}

// Luồng bình thường: cấu hình toàn cục + lớp ghi đè cấp dự án có hiệu lực.
func TestLoadConfig_ValidMergeWorks(t *testing.T) {
	writeGlobal(t, validGlobal)
	proj := t.TempDir()
	t.Chdir(proj)
	writeProjectConfig(t, `{
  "model": "google/gemini-2.5-pro",
  "reasoning_effort": "high",
  "roles": {
    "writer": {
      "provider": "openrouter",
      "model": "google/gemini-2.5-flash",
      "reasoning_effort": "low"
    }
  }
}`)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("cấu hình hợp lệ không nên báo lỗi: %v", err)
	}
	if cfg.Provider != "openrouter" {
		t.Errorf("provider phải giữ giá trị toàn cục openrouter, được %q", cfg.Provider)
	}
	if cfg.ModelName != "google/gemini-2.5-pro" {
		t.Errorf("model phải bị cấp dự án ghi đè, được %q", cfg.ModelName)
	}
	if cfg.ReasoningEffort != "high" {
		t.Errorf("reasoning_effort phải bị cấp dự án ghi đè, được %q", cfg.ReasoningEffort)
	}
	if got := cfg.Roles["writer"].ReasoningEffort; got != "low" {
		t.Errorf("roles.writer.reasoning_effort phải bị cấp dự án ghi đè, được %q", got)
	}
}

func TestMergeConfig_ProviderExtraFields(t *testing.T) {
	base := Config{
		Provider:  "openrouter",
		ModelName: "google/gemini-2.5-flash",
		Providers: map[string]ProviderConfig{
			"openrouter": {
				API:    "chat",
				APIKey: "sk-test-123456",
				ExtraBody: map[string]any{
					"temperature": 0.8,
				},
				Extra: map[string]any{
					"user_agent": "base-client/1.0",
				},
			},
		},
	}
	overlay := Config{
		Providers: map[string]ProviderConfig{
			"openrouter": {
				API:     "responses",
				BaseURL: "https://proxy.example.com/v1",
				ExtraBody: map[string]any{
					"min_p": 0.05,
				},
				Extra: map[string]any{
					"user_agent": "override-client/1.0",
					"headers": map[string]any{
						"X-Custom-Client": "ainovel",
					},
				},
			},
		},
	}

	cfg := mergeConfig(base, overlay)
	pc := cfg.Providers["openrouter"]
	if pc.APIKey != "sk-test-123456" {
		t.Fatalf("APIKey = %q, want inherited key", pc.APIKey)
	}
	if pc.API != "responses" {
		t.Fatalf("API = %q, want responses", pc.API)
	}
	if pc.BaseURL != "https://proxy.example.com/v1" {
		t.Fatalf("BaseURL = %q, want overlay URL", pc.BaseURL)
	}
	if _, ok := pc.ExtraBody["temperature"]; ok {
		t.Fatalf("ExtraBody should be replaced by overlay, got %#v", pc.ExtraBody)
	}
	if got := pc.ExtraBody["min_p"]; got != 0.05 {
		t.Fatalf("ExtraBody[min_p] = %#v, want 0.05", got)
	}
	if got := pc.Extra["user_agent"]; got != "override-client/1.0" {
		t.Fatalf("Extra[user_agent] = %#v, want override-client/1.0", got)
	}
	headers, ok := pc.Extra["headers"].(map[string]any)
	if !ok {
		t.Fatalf("Extra[headers] missing or invalid: %#v", pc.Extra["headers"])
	}
	if got := headers["X-Custom-Client"]; got != "ainovel" {
		t.Fatalf("Extra.headers[X-Custom-Client] = %#v, want ainovel", got)
	}
}

// Nguyên nhân gốc 2 (tái hiện trọng tâm của issue #37): lớp cấp dự án ghi đè provider mà không khai báo
// thông tin xác thực tương ứng trong providers, ValidateBase phải báo lỗi cấu hình
// (thay vì cho qua rồi sập sâu hơn bên trong).
func TestValidateBase_ProviderOverrideWithoutCredentials(t *testing.T) {
	cfg := Config{
		Provider:  "mimo",
		ModelName: "mimo-v2.5-pro",
		Providers: map[string]ProviderConfig{
			"openrouter": {APIKey: "sk-test-123456"},
		},
	}
	cfg.FillDefaults()
	err := cfg.ValidateBase()
	if err == nil {
		t.Fatal("provider thiếu thông tin xác thực phải báo lỗi")
	}
	if !errors.Is(err, errs.ErrConfig) {
		t.Errorf("phải bọc errs.ErrConfig, được: %v", err)
	}
}

func TestValidateBaseRejectsInvalidProviderAPI(t *testing.T) {
	cfg := Config{
		Provider:  "openai",
		ModelName: "gpt-5.1",
		Providers: map[string]ProviderConfig{
			"openai": {APIKey: "sk-test-123456", API: "legacy"},
		},
	}
	cfg.FillDefaults()
	err := cfg.ValidateBase()
	if err == nil {
		t.Fatal("provider api không hợp lệ phải báo lỗi")
	}
	if !errors.Is(err, errs.ErrConfig) {
		t.Errorf("phải bọc errs.ErrConfig, được: %v", err)
	}
}

func TestValidateBaseRejectsProviderAPIOnNonOpenAIProvider(t *testing.T) {
	cfg := Config{
		Provider:  "anthropic",
		ModelName: "claude-sonnet-4",
		Providers: map[string]ProviderConfig{
			"anthropic": {APIKey: "sk-test-123456", API: "responses"},
		},
	}
	cfg.FillDefaults()
	err := cfg.ValidateBase()
	if err == nil {
		t.Fatal("provider không phải OpenAI mà khai báo api phải báo lỗi")
	}
	if !errors.Is(err, errs.ErrConfig) {
		t.Errorf("phải bọc errs.ErrConfig, được: %v", err)
	}
}

// Cấu hình mẫu phải tự nhất quán: sau khi bỏ chú thích phải là JSON hợp lệ,
// con trỏ provider ở cấp cao nhất không bị treo, và phải nói thẳng ra ý tưởng "con trỏ" —
// đây là bản mẫu người dùng chép theo, bản thân nó hỏng là họ cũng hỏng theo.
func TestExampleConfigIsValidAndSelfConsistent(t *testing.T) {
	if exampleConfig == "" {
		t.Fatal("go:embed không có hiệu lực, exampleConfig rỗng")
	}
	rootExample, err := os.ReadFile(filepath.Join("..", "..", "config.example.jsonc"))
	if err != nil {
		t.Fatalf("đọc config.example.jsonc ở thư mục gốc: %v", err)
	}
	if string(rootExample) != exampleConfig {
		t.Fatal("config.example.jsonc ở thư mục gốc khác internal/bootstrap/config.example.jsonc")
	}
	var cfg Config
	if err := json.Unmarshal(stripJSONComments([]byte(exampleConfig)), &cfg); err != nil {
		t.Fatalf("bản mẫu nhúng sau khi bỏ chú thích không phải JSON hợp lệ (người dùng chép là họ cũng hỏng): %v", err)
	}
	if cfg.Provider == "" || cfg.ModelName == "" {
		t.Fatal("bản mẫu phải nêu provider/model mặc định")
	}
	if _, ok := cfg.Providers[cfg.Provider]; !ok {
		t.Errorf("provider %q ở cấp cao nhất của bản mẫu không trỏ tới mục nào trong providers — chính bản mẫu minh hoạ con trỏ đã bị treo", cfg.Provider)
	}
	if !contains(exampleConfig, "指针") {
		t.Error("bản mẫu phải nói thẳng \"provider là con trỏ\" — đừng để bẫy nhận thức #37 quay trở lại")
	}
}

func TestWriteStartupError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	path := WriteStartupError("boom: provider not configured")
	if path == "" {
		t.Fatal("phải trả về đường dẫn đã ghi xuống đĩa")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("đọc last-error.log: %v", err)
	}
	if want := "boom: provider not configured"; !contains(string(data), want) {
		t.Errorf("nhật ký phải chứa %q, thực tế: %s", want, data)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
