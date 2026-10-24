package store

import "testing"

// TestStoreLanguage ghim dạng chuẩn của ngôn ngữ tác phẩm: mặc định là tiếng Việt,
// đặt tường minh thì phản ánh nguyên trạng.
// internal/tools và internal/host/exp đều đọc ngôn ngữ từ đây — đây là nguồn sự thật
// duy nhất để chọn ngôn ngữ cho "văn bản hướng tới model".
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
