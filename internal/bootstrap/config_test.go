package bootstrap

import (
	"errors"
	"testing"
	"time"

	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/notify"
)

func TestConfigResolveReasoningEffort(t *testing.T) {
	cfg := Config{
		ReasoningEffort: "low", // mặc định cấp cao nhất
		Roles: map[string]RoleConfig{
			"writer":    {Provider: "p", Model: "m", ReasoningEffort: "high"}, // ghi đè ở cấp vai trò
			"architect": {Provider: "p", Model: "m"},                          // không có reasoning_effort, phải rơi về mặc định
		},
	}

	cases := []struct {
		role string
		want string
	}{
		{"writer", "high"},   // ghi đè cấp vai trò được ưu tiên
		{"architect", "low"}, // vai trò chưa cấu hình → rơi về mặc định cấp cao nhất
		{"editor", "low"},    // vai trò không tồn tại → mặc định cấp cao nhất
		{"", "low"},          // rỗng → mặc định cấp cao nhất
		{"default", "low"},   // default → mặc định cấp cao nhất
		{"arbiter", "low"},   // vai trò không cấu hình được (Tài phán luôn theo mặc định cấp cao nhất)
	}
	for _, c := range cases {
		if got := cfg.ResolveReasoningEffort(c.role); got != c.want {
			t.Errorf("ResolveReasoningEffort(%q) = %q, want %q", c.role, got, c.want)
		}
	}

	// Khi mặc định cấp cao nhất cũng rỗng, vai trò chưa ghi đè trả về "" (không ghi đè).
	empty := Config{Roles: map[string]RoleConfig{"writer": {ReasoningEffort: "xhigh"}}}
	if got := empty.ResolveReasoningEffort("editor"); got != "" {
		t.Errorf("với mặc định rỗng, editor phải trả về \"\", được %q", got)
	}
	if got := empty.ResolveReasoningEffort("writer"); got != "xhigh" {
		t.Errorf("với mặc định rỗng, ghi đè của writer phải có hiệu lực, được %q", got)
	}
}

func TestValidateBaseRejectsNonConfigurableRoles(t *testing.T) {
	for _, role := range []string{"coordinator", "arbiter"} {
		t.Run(role, func(t *testing.T) {
			cfg := Config{
				Provider:  "openrouter",
				ModelName: "test-model",
				Providers: map[string]ProviderConfig{
					"openrouter": {APIKey: "sk-test-123456"},
				},
				Roles: map[string]RoleConfig{
					role: {Provider: "openrouter", Model: "test-model"},
				},
			}

			err := cfg.ValidateBase()
			if err == nil {
				t.Fatalf("roles.%s phải bị từ chối", role)
			}
			if !errors.Is(err, errs.ErrConfig) {
				t.Fatalf("phải bọc errs.ErrConfig, được: %v", err)
			}
		})
	}
}

func TestValidateBaseNotifyEventsMatchRuntimeContract(t *testing.T) {
	validConfig := func(events []string) Config {
		return Config{
			Provider:  "openrouter",
			ModelName: "test-model",
			Providers: map[string]ProviderConfig{
				"openrouter": {APIKey: "sk-test-123456"},
			},
			Notify: NotifyConfig{Events: events},
		}
	}

	cfg := validConfig(notify.Kinds())
	if err := cfg.ValidateBase(); err != nil {
		t.Fatalf("hợp đồng sự kiện thông báo hiện tại phải qua hết kiểm tra cấu hình: %v", err)
	}

	cfg = validConfig([]string{"repeat"})
	if err := cfg.ValidateBase(); !errors.Is(err, errs.ErrConfig) {
		t.Fatalf("sự kiện repeat cũ phải bị từ chối, được: %v", err)
	}
}

func TestProviderStreamIdleTimeoutValue(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", defaultStreamIdleTimeout, false},
		{"900s", 15 * time.Minute, false},
		{"15m", 15 * time.Minute, false},
		{"abc", 0, true},
		{"-5s", 0, true},
		{"0", 0, true}, // không cung cấp "tắt watchdog" — luồng chết thật vẫn cần chặn trên
	}
	for _, c := range cases {
		got, err := ProviderConfig{StreamIdleTimeout: c.in}.StreamIdleTimeoutValue()
		if c.wantErr {
			if err == nil {
				t.Errorf("%q phải báo lỗi", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q = (%v, %v), want %v", c.in, got, err, c.want)
		}
	}
}

func TestValidateBaseRejectsBadStreamIdleTimeout(t *testing.T) {
	cfg := Config{
		Provider:  "openrouter",
		ModelName: "test-model",
		Providers: map[string]ProviderConfig{
			"openrouter": {APIKey: "sk-test-123456", StreamIdleTimeout: "fast"},
		},
	}
	if err := cfg.ValidateBase(); !errors.Is(err, errs.ErrConfig) {
		t.Fatalf("stream_idle_timeout không hợp lệ phải bị từ chối và bọc ErrConfig, được: %v", err)
	}
}
