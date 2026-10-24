package stylestat

import (
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/i18n"
)

func chapterWith(body string) string {
	return "# 标题\n" + body
}

// setActiveLang đặt ngôn ngữ tác phẩm ở cấp tiến trình và tự khôi phục sau
// test. Không dùng t.Parallel() vì biến này là trạng thái toàn cục.
func setActiveLang(t *testing.T, lang i18n.Lang) {
	t.Helper()
	previous := i18n.Active()
	i18n.SetActive(string(lang))
	t.Cleanup(func() { i18n.SetActive(string(previous)) })
}

func TestComputeBelowMinChapters(t *testing.T) {
	in := Input{Chapters: []string{"a", "b", "c", "d"}}
	if Compute(in) != nil {
		t.Fatal("dưới minChapters phải trả nil")
	}
}

// TestComputePatterns kiểm tra các lớp mẫu câu: biểu thức phát hiện giữ nguyên
// trên văn bản Trung, còn tên hiển thị đổi theo ngôn ngữ tác phẩm.
func TestComputePatterns(t *testing.T) {
	body := "他不是愤怒，而是恐惧。沉默了几息。像一盏灯。她眼中闪过慌乱，心头一紧。他觉得这是一种说不出的寒意。\n正文。\n"
	chapters := make([]string, 6)
	for i := range chapters {
		chapters[i] = chapterWith(body)
	}
	for _, lang := range []i18n.Lang{i18n.LangVI, i18n.LangZH} {
		t.Run(string(lang), func(t *testing.T) {
			setActiveLang(t, lang)
			s := Compute(Input{Chapters: chapters})
			if s == nil {
				t.Fatal("phải có thống kê")
			}
			names := styleTextsFor(lang).patterns
			if len(names) != len(patternDefs) {
				t.Fatalf("bảng tên phải khớp số lớp mẫu: %d vs %d", len(names), len(patternDefs))
			}
			if len(s.Patterns) != len(patternDefs) {
				t.Fatalf("phải có %d lớp mẫu, got %d: %+v", len(patternDefs), len(s.Patterns), s.Patterns)
			}
			for i, p := range s.Patterns {
				if p.Name != names[i] {
					t.Errorf("tên lớp mẫu %d sai ngôn ngữ: got %q want %q", i, p.Name, names[i])
				}
				if p.Total != 6 {
					t.Errorf("%s total: got %d want 6", p.Name, p.Total)
				}
				if p.PerChapter != 1.0 {
					t.Errorf("%s per_chapter: got %v want 1.0", p.Name, p.PerChapter)
				}
			}
		})
	}
}

func TestComputeTopPhrasesWithStopwords(t *testing.T) {
	// 「青云山巅」 xuất hiện nhiều; 「陆九渊」 là tên nhân vật nên bị lọc
	line := "众人望向青云山巅，陆九渊负手而立。\n"
	chapters := make([]string, 10)
	for i := range chapters {
		chapters[i] = chapterWith(strings.Repeat(line, 3))
	}
	s := Compute(Input{Chapters: chapters, Stopwords: []string{"陆九渊"}})
	if s == nil {
		t.Fatal("phải có thống kê")
	}
	var hasMountain, hasName bool
	for _, p := range s.TopPhrases {
		if strings.Contains(p.Text, "青云山") {
			hasMountain = true
		}
		if strings.Contains(p.Text, "九渊") || strings.Contains(p.Text, "陆九") {
			hasName = true
		}
	}
	if !hasMountain {
		t.Errorf("phải đào được cụt 青云山, got %+v", s.TopPhrases)
	}
	if hasName {
		t.Errorf("tên nhân vật phải bị lọc, got %+v", s.TopPhrases)
	}
}

func TestComputeRepeatedSentences(t *testing.T) {
	motto := "此生未能远行，望你替我看看远方的山海。"
	chapters := make([]string, 6)
	for i := range chapters {
		body := "平常正文，没有什么重复。\n"
		if i%2 == 0 {
			body += motto + "\n"
		}
		chapters[i] = chapterWith(body)
	}
	s := Compute(Input{Chapters: chapters})
	if s == nil {
		t.Fatal("phải có thống kê")
	}
	if len(s.RepeatedSentences) == 0 {
		t.Fatalf("phải có câu lặp, không có")
	}
	got := s.RepeatedSentences[0]
	if got.Chapters != 3 || got.Count != 3 {
		t.Errorf("câu lặp: %+v", got)
	}
	if !strings.HasPrefix(got.Text, "此生未能远行") {
		t.Errorf("nội dung: %q", got.Text)
	}
}

func TestComputeEndingAndOpening(t *testing.T) {
	short := chapterWith("一整夜没有睡。\n正文很长很长很长。\n他走了。")
	long := chapterWith("白天的事。\n正文。\n这是一个非常非常非常长的结尾句子，远远超过三十个字符的阈值长度，用来测试中位数。")
	chapters := []string{short, short, short, long, long}
	s := Compute(Input{Chapters: chapters})
	if s == nil {
		t.Fatal("phải có thống kê")
	}
	if s.Ending.ShortRatio != 0.6 {
		t.Errorf("short_ratio: got %v want 0.6", s.Ending.ShortRatio)
	}
	if s.OpeningTimeRate != 0.6 {
		t.Errorf("opening_time_rate: got %v want 0.6", s.OpeningTimeRate)
	}
}

func TestComputeTitleFormats(t *testing.T) {
	chapters := make([]string, 5)
	for i := range chapters {
		chapters[i] = chapterWith("正文。")
	}
	// Trộn lẫn → phải báo
	s := Compute(Input{Chapters: chapters, Titles: []string{"第一章 风起", "云涌", "第3章 雷动"}})
	if s.TitleFormats == nil || s.TitleFormats.WithPrefix != 2 || s.TitleFormats.WithoutPrefix != 1 {
		t.Errorf("định dạng tiêu đề: %+v", s.TitleFormats)
	}
	// Thống nhất → không báo
	s = Compute(Input{Chapters: chapters, Titles: []string{"风起", "云涌"}})
	if s.TitleFormats != nil {
		t.Errorf("tiêu đề thống nhất không được báo: %+v", s.TitleFormats)
	}
}

// TestComputeTitleFormatsVietnamese kiểm tra định dạng tiêu đề tiếng Việt
// "Chương N" cũng được tính là có tiền tố, để một cuốn sách tiếng Việt trộn
// "Chương 1" với tiêu đề trần vẫn bị báo trộn định dạng.
func TestComputeTitleFormatsVietnamese(t *testing.T) {
	chapters := make([]string, 5)
	for i := range chapters {
		chapters[i] = chapterWith("正文。")
	}
	s := Compute(Input{Chapters: chapters, Titles: []string{"Chương 1 Phong lên", " mây dâng", "Chương 3 Sấm động"}})
	if s.TitleFormats == nil || s.TitleFormats.WithPrefix != 2 || s.TitleFormats.WithoutPrefix != 1 {
		t.Errorf("định dạng tiêu đề tiếng Việt: %+v", s.TitleFormats)
	}
	// Thống nhất "Chương N" → không báo
	s = Compute(Input{Chapters: chapters, Titles: []string{"Chương 1", "Chương 2"}})
	if s.TitleFormats != nil {
		t.Errorf("tiêu đề «Chương N» thống nhất không được báo: %+v", s.TitleFormats)
	}
}
