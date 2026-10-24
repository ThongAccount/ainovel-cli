package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// Nhãn khuôn mẫu phải theo ngôn ngữ tác phẩm: các file .md này được đọc ngược
// vào ngữ cảnh model, nên nhãn tiếng Trung trong truyện tiếng Việt là nguồn kéo
// model trôi ngôn ngữ, không chỉ là chuyện hiển thị.
func TestRenderersFollowLanguage(t *testing.T) {
	entries := []domain.OutlineEntry{
		{Chapter: 1, Title: "Vô Căn Dược Giả", CoreEvent: "Mở sạp bán thuốc", Hook: "Cây mai rung lên"},
	}
	chars := []domain.Character{
		{Name: "Ngọc Lâm", Role: "Chính", Description: "Dược nông", Arc: "Từ đáy đi lên",
			Traits: []string{"Cần cù", "Điềm tĩnh"}},
	}
	rules := []domain.WorldRule{{Category: "tu_luyen", Rule: "Không linh căn", Boundary: "Chậm nhưng bền"}}

	vi := strings.Join([]string{
		renderOutline(entries, labelsVI),
		renderCharacters(chars, labelsVI),
		renderWorldRules(rules, labelsVI),
	}, "\n")
	for _, han := range []string{"大纲", "第", "核心事件", "钩子", "角色档案", "特征", "世界观规则", "边界", "、"} {
		if strings.Contains(vi, han) {
			t.Errorf("bản tiếng Việt còn nhãn tiếng Trung %q", han)
		}
	}
	for _, want := range []string{"Đề cương", "Chương 1", "Sự kiện chính", "Điểm móc",
		"Hồ sơ nhân vật", "Đặc điểm", "Luật thế giới", "Ranh giới"} {
		if !strings.Contains(vi, want) {
			t.Errorf("thiếu nhãn tiếng Việt %q", want)
		}
	}

	// Chế độ tiếng Trung phải giữ nguyên hành vi cũ.
	zh := renderOutline(entries, labelsZH)
	if !strings.Contains(zh, "# 大纲") || !strings.Contains(zh, "## 第 1 章：") {
		t.Errorf("chế độ zh bị đổi: %q", zh)
	}
}

func TestSetLanguageReachesEveryStore(t *testing.T) {
	s := NewStore(t.TempDir())
	s.SetLanguage("vi")
	for i, x := range s.ios {
		if x.lang != "vi" {
			t.Fatalf("IO thứ %d chưa được đặt ngôn ngữ", i)
		}
	}
	if len(s.ios) < 15 {
		t.Fatalf("chỉ gom được %d IO, nghi sót store", len(s.ios))
	}
}

// Ngôn ngữ tác phẩm phải chi phối nhãn của Markdown dẫn xuất: đặt "zh" thì tệp
// .md sinh ra mang nhãn tiếng Trung, đặt "vi" thì mang nhãn tiếng Việt.
//
// SetLanguage cũng gọi i18n.SetActive (biến toàn cục ở cấp tiến trình), nên test
// này phải khôi phục ngôn ngữ bằng t.Cleanup và không được chạy song song.
func TestStoreLanguageDrivesMarkdownLabels(t *testing.T) {
	prev := i18n.Active()
	s := NewStore(t.TempDir())
	t.Cleanup(func() { s.SetLanguage(string(prev)) })

	entries := []domain.OutlineEntry{
		{Chapter: 1, Title: "Vô Căn Dược Giả", CoreEvent: "Mở sạp bán thuốc", Hook: "Cây mai rung lên"},
	}

	s.SetLanguage("zh")
	if err := s.Outline.SaveOutline(entries); err != nil {
		t.Fatalf("SaveOutline (zh): %v", err)
	}
	zh, err := os.ReadFile(filepath.Join(s.Dir(), "outline.md"))
	if err != nil {
		t.Fatalf("đọc outline.md (zh): %v", err)
	}
	for _, want := range []string{"# 大纲", "## 第 1 章：", "**核心事件**："} {
		if !strings.Contains(string(zh), want) {
			t.Errorf("outline.md tiếng Trung thiếu %q: %q", want, zh)
		}
	}

	s.SetLanguage("vi")
	if err := s.Outline.SaveOutline(entries); err != nil {
		t.Fatalf("SaveOutline (vi): %v", err)
	}
	vi, err := os.ReadFile(filepath.Join(s.Dir(), "outline.md"))
	if err != nil {
		t.Fatalf("đọc outline.md (vi): %v", err)
	}
	for _, want := range []string{"# Đề cương", "## Chương 1:", "**Sự kiện chính**: "} {
		if !strings.Contains(string(vi), want) {
			t.Errorf("outline.md tiếng Việt thiếu %q: %q", want, vi)
		}
	}
}
