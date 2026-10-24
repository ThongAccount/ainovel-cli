package store

import "testing"

// TestStoreLanguage 钉死作品语种的规范形式：默认越南语，显式设置后原样反映。
// internal/tools 与 internal/host/exp 都从这里读语种，它是"模型面向文本"
// 选择语种的唯一事实源。
func TestStoreLanguage(t *testing.T) {
	s := NewStore(t.TempDir())
	if got := s.Language(); got != "vi" {
		t.Errorf("default language = %q want vi", got)
	}

	s.SetLanguage("zh")
	if got := s.Language(); got != "zh" {
		t.Errorf("language after SetLanguage(zh) = %q want zh", got)
	}

	s.SetLanguage("chinese")
	if got := s.Language(); got != "zh" {
		t.Errorf("language after SetLanguage(chinese) = %q want zh (canonical)", got)
	}

	s.SetLanguage("vi")
	if got := s.Language(); got != "vi" {
		t.Errorf("language after SetLanguage(vi) = %q want vi", got)
	}

	// rỗng → vi, không bao giờ rơi về tiếng Trung
	s.SetLanguage("")
	if got := s.Language(); got != "vi" {
		t.Errorf("language after SetLanguage(\"\") = %q want vi", got)
	}
}
