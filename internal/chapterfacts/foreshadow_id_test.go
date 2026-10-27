package chapterfacts

import (
	"strings"
	"testing"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// Tái hiện sự cố thật: model đặt mọi manh mối mới là "new", nên từ lần thứ hai
// trở đi chúng đâm vào bản ghi cũ và bị nuốt lặng — 22 chương gieo 6 lần, vào sổ 2.
func TestRejectsPlaceholderForeshadowID(t *testing.T) {
	for _, id := range []string{"new", "New", " none ", "TBD", "-", "n/a", "auto"} {
		f := domain.ChapterFacts{
			Title:     "Chương 1: Dược Nông",
			Summary:   "Lê An lần đầu cảm nhận khí trời đất qua cây cỏ",
			KeyEvents: []string{"Lê An chạm vào cây thông già"},
			ForeshadowUpdates: []domain.ForeshadowUpdate{
				{ID: id, Action: "plant", Description: "Ánh mắt vô hình soi xét từ xa"},
			}}
		err := Validate(f)
		if err == nil {
			t.Errorf("id %q là giá trị giữ chỗ, phải bị bác", id)
			continue
		}
		if !strings.Contains(err.Error(), "giữ chỗ") {
			t.Errorf("id %q: thông báo phải giải thích lý do, nhận %v", id, err)
		}
	}
}

// setActiveLang đặt ngôn ngữ tác phẩm ở cấp tiến trình và tự khôi phục sau
// test. Không dùng t.Parallel() vì biến này là trạng thái toàn cục.
func setActiveLang(t *testing.T, lang string) {
	t.Helper()
	previous := i18n.Active()
	i18n.SetActive(lang)
	t.Cleanup(func() { i18n.SetActive(string(previous)) })
}

// TestPlaceholderForeshadowIDMessageFollowsWorkLanguage kiểm tra thông báo
// "id là giá trị giữ chỗ" đổi theo ngôn ngữ tác phẩm — đây là lỗi đưa lại cho
// model nên phải đúng ngôn ngữ đang viết.
func TestPlaceholderForeshadowIDMessageFollowsWorkLanguage(t *testing.T) {
	for _, tc := range []struct {
		lang string
		want string
	}{
		{"vi", "giá trị giữ chỗ"},
		{"zh", "占位符"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			setActiveLang(t, tc.lang)
			f := domain.ChapterFacts{
				Title:     "Chương 1: Dược Nông",
				Summary:   "Lê An lần đầu cảm nhận khí trời đất qua cây cỏ",
				KeyEvents: []string{"Lê An chạm vào cây thông già"},
				ForeshadowUpdates: []domain.ForeshadowUpdate{
					{ID: "new", Action: "plant", Description: "Ánh mắt vô hình soi xét từ xa"},
				}}
			err := Validate(f)
			if err == nil {
				t.Fatal("id giữ chỗ phải bị bác")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("thông báo sai ngôn ngữ: %v", err)
			}
		})
	}
}

// TestPropertiesDescriptionsFollowWorkLanguage kiểm tra mô tả schema đổi theo
// ngôn ngữ tác phẩm; tên trường và giá trị enum thì giữ nguyên vì là ký hiệu
// kỹ thuật.
func TestPropertiesDescriptionsFollowWorkLanguage(t *testing.T) {
	for _, tc := range []struct {
		lang       string
		wantTitle  string
		wantForesh string
		wantID     string
	}{
		{"vi", "Tiêu đề cuối cùng", "Thao tác phục bút", "ID phục bút"},
		{"zh", "最终标题", "伏笔操作", "伏笔 ID"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			setActiveLang(t, tc.lang)
			object := schema.Object(Properties(false)...)
			byName, ok := object["properties"].(map[string]any)
			if !ok {
				t.Fatal("schema không có properties")
			}
			title, ok := byName["title"].(map[string]any)
			if !ok {
				t.Fatal("thiếu trường title")
			}
			if desc, _ := title["description"].(string); desc != tc.wantTitle {
				t.Fatalf("mô tả title sai ngôn ngữ: %q", desc)
			}
			foresh, ok := byName["foreshadow_updates"].(map[string]any)
			if !ok {
				t.Fatal("thiếu trường foreshadow_updates")
			}
			if desc, _ := foresh["description"].(string); desc != tc.wantForesh {
				t.Fatalf("mô tả foreshadow_updates sai ngôn ngữ: %q", desc)
			}
			items, ok := foresh["items"].(map[string]any)
			if !ok {
				t.Fatal("foreshadow_updates không có items")
			}
			itemProps, ok := items["properties"].(map[string]any)
			if !ok {
				t.Fatal("items không có properties")
			}
			id, ok := itemProps["id"].(map[string]any)
			if !ok {
				t.Fatal("items thiếu trường id")
			}
			if desc, _ := id["description"].(string); desc != tc.wantID {
				t.Fatalf("mô tả foreshadow id sai ngôn ngữ: %q", desc)
			}
		})
	}
}

func TestAcceptsRealForeshadowID(t *testing.T) {
	for _, id := range []string{"FORESHADOW_OBSERVER", "jade-pendant", "f1", "quan-sat-vien"} {
		f := domain.ChapterFacts{
			Title:     "Chương 1: Dược Nông",
			Summary:   "Lê An lần đầu cảm nhận khí trời đất qua cây cỏ",
			KeyEvents: []string{"Lê An chạm vào cây thông già"},
			ForeshadowUpdates: []domain.ForeshadowUpdate{
				{ID: id, Action: "plant", Description: "Ánh mắt vô hình soi xét từ xa"},
			}}
		if err := Validate(f); err != nil {
			t.Errorf("id %q hợp lệ nhưng bị bác: %v", id, err)
		}
	}
}
