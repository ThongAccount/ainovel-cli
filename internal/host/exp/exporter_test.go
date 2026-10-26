package exp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
)

// newTestStore tạo store tối giản trên t.TempDir(), đã ghi bản chính của chương
// 1..n cùng progress.
func newTestStore(t *testing.T, novelName string, completed []int) (*store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("khởi tạo store: %v", err)
	}
	if err := s.Progress.Init(len(completed)); err != nil {
		t.Fatalf("khởi tạo progress: %v", err)
	}
	if novelName != "" {
		if err := s.Book.Save(domain.BookMetadata{Title: novelName, Synopsis: "Đoạn giới thiệu thử nghiệm dành cho người đọc."}); err != nil {
			t.Fatalf("lưu book: %v", err)
		}
	}
	if err := s.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatalf("đặt phase writing: %v", err)
	}
	for _, ch := range completed {
		if err := s.Drafts.SaveFinalChapter(ch, fmt.Sprintf("Nội dung chương %d.", ch)); err != nil {
			t.Fatalf("lưu chương %d: %v", ch, err)
		}
		if err := s.Progress.StartChapter(ch); err != nil {
			t.Fatalf("bắt đầu chương %d: %v", ch, err)
		}
		if err := s.Progress.MarkChapterComplete(ch, 5, "cliff", "main"); err != nil {
			t.Fatalf("đánh dấu hoàn thành %d: %v", ch, err)
		}
	}
	return s, dir
}

func TestRun_HappyPath_DefaultsToNovelDir(t *testing.T) {
	s, dir := newTestStore(t, "Quang vệ", []int{1, 2, 3})
	if err := s.Outline.SavePremise("Câu chuyện về ánh sáng và bóng tối."); err != nil {
		t.Fatalf("lưu premise: %v", err)
	}
	if err := s.Outline.SaveOutline([]domain.OutlineEntry{
		{Chapter: 1, Title: "Người trở về trong đêm mưa"},
		{Chapter: 2, Title: "Bình minh"},
		{Chapter: 3, Title: "Tro tàn"},
	}); err != nil {
		t.Fatalf("lưu outline: %v", err)
	}

	res, err := Run(context.Background(), Deps{Store: s}, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Chapters != 3 {
		t.Errorf("Chapters = %d, cần 3", res.Chapters)
	}
	if res.Path != filepath.Join(dir, "Quang vệ.txt") {
		t.Errorf("Path = %q, cần mặc định {dir}/Quang vệ.txt", res.Path)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("đọc bản xuất: %v", err)
	}
	text := string(data)
	// Store chưa đặt ngôn ngữ rõ ràng → bản port này mặc định tiếng Việt: tên sách
	// không dùng «», tiêu đề chương dùng "Chương N".
	if !strings.HasPrefix(text, "Quang vệ\n\n") {
		t.Errorf("thiếu tên sách tiếng Việt ở đầu:\n%s", text)
	}
	for _, want := range []string{"Chương 1  Người trở về trong đêm mưa", "Chương 3  Tro tàn"} {
		if !strings.Contains(text, want) {
			t.Errorf("bản xuất thiếu %q\ntoàn bộ:\n%s", want, text)
		}
	}
	// premise không đưa vào bản xuất (bản thiết kế sáng táo, không phải nội dung cho người đọc)
	if strings.Contains(text, "Câu chuyện về ánh sáng và bóng tối.") {
		t.Errorf("premise không được xuất hiện trong bản xuất:\n%s", text)
	}
}

// TestRun_ZHLanguageUsesChineseLayout kiểm chứng tác phẩm tiếng Trung vẫn xuất
// theo bố cục tiếng Trung.
func TestRun_ZHLanguageUsesChineseLayout(t *testing.T) {
	previousLang := i18n.Active()
	t.Cleanup(func() { i18n.SetActive(string(previousLang)) })

	s, _ := newTestStore(t, "光斑", []int{1})
	s.SetLanguage("zh")
	if err := s.Outline.SaveOutline([]domain.OutlineEntry{{Chapter: 1, Title: "雨夜归人"}}); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), Deps{Store: s}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "《光斑》\n\n") {
		t.Errorf("tên sách tiếng Trung phải dùng 《》: %s", text)
	}
	if !strings.Contains(text, "第 1 章  雨夜归人") {
		t.Errorf("thiếu tiêu đề chương tiếng Trung: %s", text)
	}
}

func TestRun_UsesCommittedTitleForCompletedChapter(t *testing.T) {
	s, _ := newTestStore(t, "Quang vệ", []int{1})
	if err := s.Outline.SaveOutline([]domain.OutlineEntry{{Chapter: 1, Title: "Tiêu đề dự kiến"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Summaries.SaveSummary(domain.ChapterSummary{
		Chapter: 1, Title: "Tiêu đề bản chính", Summary: "Tóm tắt",
	}); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), Deps{Store: s}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "Chương 1  Tiêu đề bản chính") || strings.Contains(text, "Tiêu đề dự kiến") {
		t.Fatalf("tiêu đề được xuất ra không đúng:\n%s", text)
	}
}

// TestRun_PremiseNotExported kiểm chứng đầu-cuối: premise.md tồn tại vẫn không
// được xuất, còn tên sách được giữ lại (issue #27).
func TestRun_PremiseNotExported(t *testing.T) {
	s, _ := newTestStore(t, "Quang vệ", []int{1})
	if err := s.Outline.SavePremise("# Quang vệ\n## Đối tượng đọc\nBản thiết kế sáng táo không được xuất."); err != nil {
		t.Fatalf("lưu premise: %v", err)
	}
	res, err := Run(context.Background(), Deps{Store: s}, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("đọc bản xuất: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "Bản thiết kế sáng táo không được xuất.") || strings.Contains(text, "Đối tượng đọc") {
		t.Errorf("premise không được xuất ra, nhận:\n%s", text)
	}
	if !strings.HasPrefix(text, "Quang vệ\n\n") {
		t.Errorf("tên sách phải được giữ lại: %s", text)
	}
}

func TestRun_NoCompletedChapters(t *testing.T) {
	s, _ := newTestStore(t, "X", nil)
	_, err := Run(context.Background(), Deps{Store: s}, Options{})
	if err == nil {
		t.Fatal("chờ lỗi khi không có chương hoàn thành")
	}
}

func TestRun_ExistingFile_NoOverwrite(t *testing.T) {
	s, dir := newTestStore(t, "X", []int{1})
	target := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(target, []byte("preexisting"), 0o644); err != nil {
		t.Fatalf("tạo tệp nền: %v", err)
	}
	_, err := Run(context.Background(), Deps{Store: s}, Options{OutPath: target})
	if err == nil {
		t.Fatal("chờ lỗi khi tệp đích đã tồn tại và !Overwrite")
	}
	if !strings.Contains(err.Error(), "đã tồn tại") {
		t.Errorf("lỗi không mong đợi: %v", err)
	}

	// Thêm Overwrite phải thành công
	res, err := Run(context.Background(), Deps{Store: s}, Options{OutPath: target, Overwrite: true})
	if err != nil {
		t.Fatalf("Overwrite Run: %v", err)
	}
	if res.Path != target {
		t.Errorf("Path = %q, cần %q", res.Path, target)
	}
	data, _ := os.ReadFile(target)
	if string(data) == "preexisting" {
		t.Error("tệp chưa được ghi đè")
	}
}

func TestRun_RangeWithSkipped(t *testing.T) {
	s, _ := newTestStore(t, "X", []int{1, 2, 3})
	res, err := Run(context.Background(), Deps{Store: s}, Options{From: 2, To: 5, Overwrite: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Chapters != 2 {
		t.Errorf("Chapters = %d, cần 2 (chỉ 2,3 hoàn thành trong khoảng 2..5)", res.Chapters)
	}
	if got := res.Skipped; len(got) != 2 || got[0] != 4 || got[1] != 5 {
		t.Errorf("Skipped = %v, cần [4 5]", got)
	}
}

func TestRun_FromGreaterThanTo(t *testing.T) {
	s, _ := newTestStore(t, "X", []int{1, 2})
	_, err := Run(context.Background(), Deps{Store: s}, Options{From: 5, To: 2})
	if err == nil {
		t.Fatal("chờ lỗi với khoảng không hợp lệ")
	}
}

func TestRun_UnsupportedFormat(t *testing.T) {
	s, _ := newTestStore(t, "X", []int{1})
	_, err := Run(context.Background(), Deps{Store: s}, Options{Format: Format("pdf")})
	if err == nil {
		t.Fatal("chờ lỗi với định dạng không được hỗ trợ")
	}
}

func TestRunRejectsMissingBookMetadata(t *testing.T) {
	s, _ := newTestStore(t, "", []int{1})
	if _, err := Run(context.Background(), Deps{Store: s}, Options{}); err == nil {
		t.Fatal("phải từ chối xuất khi thiếu thông tin tác phẩm")
	}
}

func TestInferFormat(t *testing.T) {
	cases := []struct {
		in      string
		want    Format
		wantErr bool
	}{
		{"", FormatTXT, false},
		{"book.txt", FormatTXT, false},
		{"book.TXT", FormatTXT, false},
		{"book.epub", FormatEPUB, false},
		{"book.EPUB", FormatEPUB, false},
		{"/abs/path/x.epub", FormatEPUB, false},
		{"book", FormatTXT, false}, // không có phần mở rộng thì dùng TXT
		{"book.dat", "", true},
		{"book.pdf", "", true},
	}
	for _, c := range cases {
		got, err := inferFormat(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("inferFormat(%q) cần lỗi", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("inferFormat(%q): lỗi không mong đợi: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("inferFormat(%q) = %q, cần %q", c.in, got, c.want)
		}
	}
}

func TestRun_EPUB_FromExtension(t *testing.T) {
	s, dir := newTestStore(t, "Quang vệ", []int{1})
	if err := s.Outline.SavePremise("Ánh sáng và bóng tối."); err != nil {
		t.Fatalf("lưu premise: %v", err)
	}
	if err := s.Outline.SaveOutline([]domain.OutlineEntry{{Chapter: 1, Title: "Đêm mưa"}}); err != nil {
		t.Fatalf("lưu outline: %v", err)
	}

	target := filepath.Join(dir, "out.epub")
	res, err := Run(context.Background(), Deps{Store: s}, Options{OutPath: target})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Path != target {
		t.Errorf("Path = %q, cần %q", res.Path, target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("đọc: %v", err)
	}
	// EPUB là zip, có chữ PK ở 4 byte đầu
	if len(data) < 4 || string(data[:2]) != "PK" {
		t.Errorf("bản xuất không giống file zip: %x", data[:min(8, len(data))])
	}
}

func TestRun_DefaultPathFollowsFormat(t *testing.T) {
	s, dir := newTestStore(t, "Quang vệ", []int{1})
	res, err := Run(context.Background(), Deps{Store: s}, Options{Format: FormatEPUB})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := filepath.Join(dir, "Quang vệ.epub")
	if res.Path != want {
		t.Errorf("Path = %q, cần %q", res.Path, want)
	}
}

func TestRun_UnknownExtension(t *testing.T) {
	s, _ := newTestStore(t, "X", []int{1})
	_, err := Run(context.Background(), Deps{Store: s}, Options{OutPath: "/tmp/foo.dat"})
	if err == nil {
		t.Fatal("chờ lỗi với phần mở rộng lạ")
	}
	if !strings.Contains(err.Error(), "phần mở rộng") {
		t.Errorf("lỗi phải nhắc phần mở rộng: %v", err)
	}
}

func TestSanitizeFileName(t *testing.T) {
	cases := map[string]string{
		"":                     "novel",
		"   ":                  "novel",
		"normal":               "normal",
		"a/b":                  "a_b",
		"a\\b":                 "a_b",
		"a:b*c?\"d<e>f|g\x00h": "a_b_c__d_e_f_g_h",
	}
	for in, want := range cases {
		if got := sanitizeFileName(in); got != want {
			t.Errorf("sanitizeFileName(%q) = %q, cần %q", in, got, want)
		}
	}
}
