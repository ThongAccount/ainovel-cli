package userrules

import (
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/store"
)

// Mô hình nil + thư mục quy tắc rỗng: mọi nguồn hạ cấp, nhưng ảnh chụp vẫn sinh được
// (system_defaults đảm bảo) và vẫn ghi xuống đĩa.
// Hai thư mục của LoadOptions{} là chuỗi rỗng nên RawFileSources trả nil, test không
// chạm vào đĩa thật.
func newDegradedService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	st := store.NewStore(t.TempDir())
	return NewService(st, nil, rules.LoadOptions{}), st
}

func TestService_Build_DegradesButPersists(t *testing.T) {
	svc, st := newDegradedService(t)

	snap, err := svc.Build(t.Context(), "每章1200字，主角冷静克制")
	if err != nil {
		t.Fatalf("Build không được báo lỗi (hạ cấp chứ không chặn): %v", err)
	}
	if snap.Status != rules.StatusDegraded {
		t.Fatalf("không có mô hình thì phải hạ cấp, status=%q", snap.Status)
	}
	// system_defaults luôn đảm bảo đường cơ sở máy kiểm tra.
	if len(snap.Structured.FatigueWords) == 0 || len(snap.Structured.ForbiddenPhrases) == 0 {
		t.Fatalf("phải giữ đường cơ sở máy kiểm tra của system_defaults, got %+v", snap.Structured)
	}
	// Prompt khởi chạy hạ cấp thành raw preferences, không mất nguyên văn.
	if snap.Preferences == "" {
		t.Fatal("hạ cấp phải ghi nguyên văn prompt khởi chạy vào preferences")
	}

	// Đã ghi xuống đĩa: GetOrBuild đọc lại đúng bản đó chứ không dựng lại.
	reloaded, err := st.UserRules.Load()
	if err != nil || reloaded == nil {
		t.Fatalf("ảnh chụp phải nằm trên đĩa: err=%v snap=%v", err, reloaded)
	}
	if reloaded.Preferences != snap.Preferences {
		t.Fatal("nội dung trên đĩa khác với nội dung trả về")
	}
}

func TestService_GetOrBuildInitializesMissingSnapshot(t *testing.T) {
	svc, st := newDegradedService(t)

	if cur, _ := st.UserRules.Load(); cur != nil {
		t.Fatal("lúc đầu chưa có ảnh chụp")
	}
	snap, err := svc.GetOrBuild(t.Context())
	if err != nil {
		t.Fatalf("GetOrBuild không được báo lỗi: %v", err)
	}
	if len(snap.Structured.FatigueWords) == 0 {
		t.Fatal("sinh lười phải chứa system_defaults")
	}
	if cur, _ := st.UserRules.Load(); cur == nil {
		t.Fatal("GetOrBuild phải ghi xuống đĩa luôn")
	}
}

func TestService_AddRuntimeRule_PersistsAndReturnsCandidate(t *testing.T) {
	svc, st := newDegradedService(t)

	const text = "từ nay bớt dùng ẩn dụ"
	merged, cand, err := svc.AddRuntimeRule(t.Context(), text)
	if err != nil {
		t.Fatalf("AddRuntimeRule không được báo lỗi: %v", err)
	}
	// Ứng viên dùng để hiển thị lại: không có mô hình thì hạ cấp, nguyên văn vào preferences.
	if !cand.Degraded {
		t.Fatal("không có mô hình thì ứng viên lần này phải hạ cấp")
	}
	if cand.Preferences != text {
		t.Fatalf("ứng viên phải giữ nguyên văn, got %q", cand.Preferences)
	}
	// Ảnh chụp sau khi chồng chứa mục đó và đã nằm trên đĩa.
	if merged.Preferences == "" {
		t.Fatal("sau khi chồng, preferences không được rỗng")
	}
	reloaded, err := st.UserRules.Load()
	if err != nil || reloaded == nil {
		t.Fatalf("sau khi chồng phải nằm trên đĩa: err=%v", err)
	}
	if reloaded.Status != rules.StatusDegraded {
		t.Fatalf("có nguồn bị hạ cấp thì status phải là degraded, got %q", reloaded.Status)
	}
}

// TestService_UsesStoreLanguageForPrompt kiểm tra ngôn ngữ tác phẩm lấy từ
// store (st.Language()) chứ không phải từ biến toàn cục của tiến trình.
func TestService_UsesStoreLanguageForPrompt(t *testing.T) {
	previous := i18n.Active()
	t.Cleanup(func() { i18n.SetActive(string(previous)) })

	for _, tc := range []struct {
		lang string
		want string
	}{
		{"vi", "Trình chuẩn hóa quy tắc"},
		{"zh", "规则归一化器"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			st := store.NewStore(t.TempDir())
			st.SetLanguage(tc.lang)
			model := &scriptedModel{replies: []string{
				`{"structured":{"genre":"","forbidden_chars":[],"forbidden_phrases":[],"fatigue_words":[]},"preferences":"x","uncertain":[]}`,
			}}
			svc := NewService(st, model, rules.LoadOptions{})
			if _, err := svc.Build(t.Context(), "mỗi chương 1200 chữ"); err != nil {
				t.Fatalf("Build: %v", err)
			}
			system := model.lastMsgs[0].TextContent()
			if !strings.Contains(system, tc.want) {
				t.Fatalf("prompt phải theo ngôn ngữ của store (%s):\n%s", tc.lang, system)
			}
		})
	}
}
