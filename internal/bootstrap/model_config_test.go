package bootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestModelConfigAcceptsLegacyAndObjectEntries(t *testing.T) {
	var cfg Config
	input := `{
  "provider":"custom","model":"legacy-model",
  "providers":{"custom":{"type":"openai","models":[
    "legacy-model",
    {"name":"large-model","context_window":400000}
  ]}}
}`
	if err := json.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	models := cfg.Providers["custom"].Models
	if len(models) != 2 || models[0].Name != "legacy-model" || models[0].ContextWindow != 0 {
		t.Fatalf("legacy model decode = %#v", models)
	}
	if models[1].Name != "large-model" || models[1].ContextWindow != 400000 {
		t.Fatalf("object model decode = %#v", models[1])
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), `"models":["legacy-model"`) {
		t.Fatalf("models should be normalized to objects: %s", data)
	}
	if !strings.Contains(string(data), `"name":"legacy-model"`) {
		t.Fatalf("normalized model missing: %s", data)
	}
}

// json_schema ba trạng thái: chưa cấu hình = nil (tuỳ năng lực của adapter), true/false = khai báo tường minh;
// mục legacy dạng chuỗi đọc vào là nil; ghi lại rồi đọc lại không được làm đổi ba trạng thái.
func TestModelConfigJSONSchemaTriState(t *testing.T) {
	var cfg Config
	input := `{"providers":{"custom":{"models":[
    {"name":"a","json_schema":true},
    {"name":"b","json_schema":false},
    {"name":"c"},
    "legacy"
  ]}}}`
	if err := json.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	assertTriState := func(models []ModelConfig, stage string) {
		t.Helper()
		if models[0].JSONSchema == nil || !*models[0].JSONSchema {
			t.Fatalf("%s: a phải là true, got %v", stage, models[0].JSONSchema)
		}
		if models[1].JSONSchema == nil || *models[1].JSONSchema {
			t.Fatalf("%s: b phải là false, got %v", stage, models[1].JSONSchema)
		}
		if models[2].JSONSchema != nil || models[3].JSONSchema != nil {
			t.Fatalf("%s: c/legacy phải là nil, got %v %v", stage, models[2].JSONSchema, models[3].JSONSchema)
		}
	}
	assertTriState(cfg.Providers["custom"].Models, "decode")

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var again Config
	if err := json.Unmarshal(data, &again); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	assertTriState(again.Providers["custom"].Models, "round-trip")

	if v := cfg.ModelJSONSchema("custom", "a"); v == nil || !*v {
		t.Fatalf("ModelJSONSchema(custom,a) = %v", v)
	}
	if v := cfg.ModelJSONSchema("custom", "missing"); v != nil {
		t.Fatalf("mô hình không có trong danh sách phải là nil, got %v", v)
	}
	if v := cfg.ModelJSONSchema("nope", "a"); v != nil {
		t.Fatalf("provider lạ phải là nil, got %v", v)
	}
}

// Giá trị ghi đè json_schema của SwappableModel phải cập nhật nguyên tử theo chuyển mô hình nóng:
// sau khi chuyển sang mô hình khai báo khác, lần JSONSchemaOverride kế tiếp đọc tại chỗ sẽ thấy sự thật mới.
func TestSwappableModelJSONSchemaOverrideFollowsSwap(t *testing.T) {
	tr, fa := true, false
	cfg := Config{
		Provider: "proxy", ModelName: "a",
		Providers: map[string]ProviderConfig{"proxy": {
			Type: "openai", APIKey: "k", BaseURL: "https://example.com/v1",
			Models: []ModelConfig{{Name: "a", JSONSchema: &tr}, {Name: "b", JSONSchema: &fa}, {Name: "c"}},
		}},
	}
	ms, err := NewModelSet(cfg)
	if err != nil {
		t.Fatalf("new model set: %v", err)
	}
	if v := ms.Default.JSONSchemaOverride(); v == nil || !*v {
		t.Fatalf("ban đầu phải là true, got %v", v)
	}
	facts := ms.Default.StructuredOutputFacts()
	if facts.Info.Name != "a" || facts.Info.Provider != "openai" || facts.JSONSchemaOverride == nil || !*facts.JSONSchemaOverride {
		t.Fatalf("ảnh chụp sự thật có cấu trúc ban đầu không khớp: %+v", facts)
	}
	if err := ms.Swap("default", "proxy", "b"); err != nil {
		t.Fatalf("swap b: %v", err)
	}
	if v := ms.Default.JSONSchemaOverride(); v == nil || *v {
		t.Fatalf("sau khi chuyển sang b phải là false, got %v", v)
	}
	facts = ms.Default.StructuredOutputFacts()
	if facts.Info.Name != "b" || facts.JSONSchemaOverride == nil || *facts.JSONSchemaOverride {
		t.Fatalf("ảnh chụp sự thật có cấu trúc sau khi chuyển không khớp: %+v", facts)
	}
	if err := ms.Swap("default", "proxy", "c"); err != nil {
		t.Fatalf("swap c: %v", err)
	}
	if v := ms.Default.JSONSchemaOverride(); v != nil {
		t.Fatalf("sau khi chuyển sang c chưa khai báo phải là nil, got %v", v)
	}
}

func TestResolveContextWindowIsProviderAware(t *testing.T) {
	cfg := Config{
		ContextWindow: 300000,
		Providers: map[string]ProviderConfig{
			"one": {Models: []ModelConfig{{Name: "same", ContextWindow: 128000}}},
			"two": {Models: []ModelConfig{{Name: "same", ContextWindow: 900000}}},
		},
	}
	if got, source := cfg.ResolveContextWindow("one", "same"); got != 128000 || source != CtxWindowModelConfig {
		t.Fatalf("one/same = %d %s", got, source)
	}
	if got, source := cfg.ResolveContextWindow("two", "same"); got != 900000 || source != CtxWindowModelConfig {
		t.Fatalf("two/same = %d %s", got, source)
	}
	if got, source := cfg.ResolveContextWindow("one", "unknown"); got != 300000 || source != CtxWindowConfig {
		t.Fatalf("legacy fallback = %d %s", got, source)
	}
}

func TestSaveProviderConfigPreservesSelectionAndUsesPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ainovel", "config.json")
	original := Config{
		Provider: "old", ModelName: "old-model", Style: "fantasy",
		Providers: map[string]ProviderConfig{"old": {Type: "openai", Models: []ModelConfig{{Name: "old-model"}}}},
		Budget:    BudgetConfig{BookUSD: 20, WarnRatio: 0.8},
	}
	if err := SaveConfig(path, original); err != nil {
		t.Fatalf("seed: %v", err)
	}
	pc := ProviderConfig{Type: "openai", Models: []ModelConfig{{Name: "new-model", ContextWindow: 500000}}}
	if err := SaveProviderConfig(path, "new", pc); err != nil {
		t.Fatalf("save provider config: %v", err)
	}
	got, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Chỉ bổ sung mục providers: các trường không liên quan và lựa chọn provider/model ở cấp cao nhất phải giữ nguyên.
	if got.Style != "fantasy" || got.Budget.BookUSD != 20 || got.Provider != "old" || got.ModelName != "old-model" {
		t.Fatalf("selection or unrelated fields mutated: %#v", got)
	}
	if _, ok := got.Providers["old"]; !ok {
		t.Fatal("existing provider was removed")
	}
	if got.Providers["new"].Models[0].ContextWindow != 500000 {
		t.Fatalf("new provider not patched in: %#v", got.Providers["new"])
	}
	// Khẳng định quyền chỉ có ý nghĩa trên nền tảng có ngữ nghĩa bit quyền POSIX: Windows báo mọi thứ
	// là 0666/0444 nên khẳng định này luôn sai trên nền tảng đó (xem cách xử lý tương tự ở
	// version.TestReplaceExecutable).
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("config mode = %o, want 600", info.Mode().Perm())
		}
	}
}
