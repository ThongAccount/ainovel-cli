package exp

import (
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

func TestStripChapterTitleHeader(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		title string
		want  string
	}{
		{"plain body untouched", "他望着窗外。", "雨夜归人", "他望着窗外。"},
		{"strip h1 chinese title", "# 第 1 章  雨夜归人\n\n他望着窗外。", "雨夜归人", "他望着窗外。"},
		{"strip h2 with chapter token", "## 第二章\n\n他望着窗外。", "", "他望着窗外。"},
		{"strip h1 vietnamese title", "# Chương 1  Người trở về\n\nAnh nhìn ra cửa sổ.", "Người trở về", "Anh nhìn ra cửa sổ."},
		{"strip h2 vietnamese chapter token", "## Chương 2\n\nAnh nhìn ra cửa sổ.", "", "Anh nhìn ra cửa sổ."},
		{"keep body even if no header", "正文第一句。\n第二句。", "", "正文第一句。\n第二句。"},
		{"do not strip non-chapter heading", "# 序章\n他望着窗外。", "边村浮生", "# 序章\n他望着窗外。"},
		{"single line header only", "# 第 1 章", "", ""},
		// Người viết đặt tên chương thuần vào dòng đầu → trùng với tiêu đề thống nhất
		// của bộ xuất, nên phải bị bỏ
		{"strip h1 matching chapter title", "# 边村浮生\n\n天还没亮。", "边村浮生", "天还没亮。"},
		// Dòng đầu là h1 nhưng chữ không bằng tiêu đề chương → coi là nội dung, giữ lại
		{"keep h1 not matching title", "# 别的小标题\n正文。", "边村浮生", "# 别的小标题\n正文。"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripChapterTitleHeader(c.in, c.title)
			if got != c.want {
				t.Fatalf("stripChapterTitleHeader\nđầu vào = %q\ntiêu đề = %q\ncần     = %q\nnhận    = %q", c.in, c.title, c.want, got)
			}
		})
	}
}

func TestBuildTitleIndex(t *testing.T) {
	outline := []domain.OutlineEntry{
		{Chapter: 1, Title: "Người trở về trong đêm mưa"},
		{Chapter: 2, Title: ""}, // tiêu đề rỗng phải bị lọc
		{Chapter: 3, Title: "Bình minh"},
	}
	idx := buildTitleIndex(outline)
	if got := idx[1]; got != "Người trở về trong đêm mưa" {
		t.Errorf("tiêu đề ch1: nhận %q, cần Người trở về trong đêm mưa", got)
	}
	if _, ok := idx[2]; ok {
		t.Errorf("ch2 phải không có mặt (tiêu đề rỗng)")
	}
	if got := idx[3]; got != "Bình minh" {
		t.Errorf("tiêu đề ch3: nhận %q, cần Bình minh", got)
	}
}

func TestBuildLocations(t *testing.T) {
	volumes := []domain.VolumeOutline{
		{Index: 1, Title: "Khởi nguồn", Arcs: []domain.ArcOutline{
			{Index: 1, Title: "Người trẻ lần đầu xuất hiện", Chapters: []domain.OutlineEntry{{}, {}}}, // 2 chương
			{Index: 2, Title: "Thử thách tông môn", Chapters: []domain.OutlineEntry{{}}},              // 1 chương
		}},
		{Index: 2, Title: "Trỗi lên", Arcs: []domain.ArcOutline{
			{Index: 1, Title: "Trận đấu đầu tiên", Chapters: []domain.OutlineEntry{{}}},
		}},
	}
	locs := buildLocations(volumes)

	// Chỉ kiểm tra thuộc tính tập: cung không còn nằm trong location, nhưng lớp cung
	// vẫn tham gia việc cộng dồn số chương toàn cục.
	if loc := locs[1]; !loc.IsFirstOfVolume || loc.VolumeIdx != 1 {
		t.Errorf("ch1 phải là chương đầu của tập 1: %+v", loc)
	}
	if loc := locs[2]; loc.IsFirstOfVolume || loc.VolumeIdx != 1 {
		t.Errorf("ch2 phải thuộc tập 1, không phải chương đầu: %+v", loc)
	}
	// ch3 là chương đầu của cung 2 nhưng vẫn nằm trong tập 1 → không phải đầu tập.
	if loc := locs[3]; loc.IsFirstOfVolume || loc.VolumeIdx != 1 {
		t.Errorf("ch3 (cung 2, cùng tập) không được là chương đầu tập: %+v", loc)
	}
	if loc := locs[4]; !loc.IsFirstOfVolume || loc.VolumeIdx != 2 {
		t.Errorf("ch4 phải mở đầu tập 2: %+v", loc)
	}
}

func TestRenderTXT_TitleAndChapter(t *testing.T) {
	got := renderTXT(
		"光斑",
		[]int{1, 2},
		chapterTitleIndex{1: "雨夜归人", 2: "破晓"},
		nil,
		map[int]string{
			1: "# 第 1 章 雨夜归人\n\n他望着窗外。",
			2: "她推开门。",
		},
		expTextsZH,
	)
	if !strings.HasPrefix(got, "《光斑》\n\n") {
		t.Errorf("thiếu tên sách ở đầu bản xuất:\n%s", got)
	}
	// premise không đưa vào bản xuất: sau tên sách phải đến thẳng chương, không có
	// phần tóm tắt dẫn dắt nào
	if !strings.Contains(got, "第 1 章  雨夜归人") {
		t.Errorf("thiếu tiêu đề chương 1")
	}
	if !strings.Contains(got, "他望着窗外。") {
		t.Errorf("thiếu nội dung chương 1")
	}
	if strings.Contains(got, "# 第 1 章") {
		t.Errorf("tiêu đề Markdown trong nội dung chưa được bỏ:\n%s", got)
	}
	if !strings.Contains(got, "第 2 章  破晓") {
		t.Errorf("thiếu tiêu đề chương 2")
	}
}

func TestRenderTXT_EmptyBookTitleNoTitleLine(t *testing.T) {
	got := renderTXT(
		"",
		[]int{1},
		chapterTitleIndex{1: "雨夜归人"},
		nil,
		map[int]string{1: "正文。"},
		expTextsZH,
	)
	if strings.Contains(got, "《") {
		t.Errorf("không được chứa dấu ngoặc kép quanh tên sách: %s", got)
	}
	if !strings.HasPrefix(got, "第 1 章  雨夜归人") {
		t.Errorf("chờ tiêu đề chương ngay ở đầu: %s", got)
	}
}

// TestRenderTXT_LayeredVolume kiểm chứng Đề cương phân tầng chỉ chèn dấu phân tách
// tập ở đầu tập và không bao giờ hiện dấu phân tách cung
// (issue #27: bố cục "«Tên sách» → dấu phân tách tập → nội dung chương").
func TestRenderTXT_LayeredVolume(t *testing.T) {
	locs := map[int]chapterLocation{
		1: {VolumeIdx: 1, VolumeTitle: "起源", IsFirstOfVolume: true},
		2: {VolumeIdx: 1, VolumeTitle: "起源"},
	}
	got := renderTXT(
		"X", []int{1, 2},
		chapterTitleIndex{1: "A", 2: "B"},
		locs,
		map[int]string{1: "正文一。", 2: "正文二。"},
		expTextsZH,
	)
	if !strings.Contains(got, "第 1 卷  起源") {
		t.Errorf("thiếu tiêu đề tập: %s", got)
	}
	if strings.Contains(got, "弧") {
		t.Errorf("dấu phân tách cung không được xuất hiện: %s", got)
	}
	// Tiêu đề tập chỉ xuất hiện một lần trước chương đầu tiên
	if strings.Count(got, "第 1 卷") != 1 {
		t.Errorf("tiêu đề tập phải xuất hiện đúng một lần: %s", got)
	}
}

// TestRenderTXT_VietnameseLanguage kiểm chứng bố cục tiếng Việt: tên sách không
// dùng «», tiêu đề chương và dấu phân tách tập theo ngôn ngữ tác phẩm, không được
// còn dấu hiệu tiếng Trung.
func TestRenderTXT_VietnameseLanguage(t *testing.T) {
	locs := map[int]chapterLocation{
		1: {VolumeIdx: 1, VolumeTitle: "Khởi nguồn", IsFirstOfVolume: true},
	}
	got := renderTXT(
		"Quang vệ",
		[]int{1},
		chapterTitleIndex{1: "Người trở về"},
		locs,
		map[int]string{1: "Anh nhìn ra cửa sổ."},
		expTextsVI,
	)
	if !strings.HasPrefix(got, "Quang vệ\n\n") {
		t.Errorf("tên sách tiếng Việt không được dùng 《》: %s", got)
	}
	if !strings.Contains(got, "Chương 1  Người trở về") {
		t.Errorf("thiếu tiêu đề chương tiếng Việt: %s", got)
	}
	if !strings.Contains(got, "Tập 1  Khởi nguồn") {
		t.Errorf("thiếu tiêu đề tập tiếng Việt: %s", got)
	}
	if strings.Contains(got, "第") || strings.Contains(got, "卷") {
		t.Errorf("bản xuất tiếng Việt không được chứa dấu hiệu tiếng Trung: %s", got)
	}
}

func TestRenderTXT_ChapterWithoutTitleFallsBackToNumberOnly(t *testing.T) {
	got := renderTXT(
		"", []int{5},
		chapterTitleIndex{}, // không có tiêu đề
		nil,
		map[int]string{5: "正文。"},
		expTextsZH,
	)
	if !strings.Contains(got, "第 5 章\n\n") {
		t.Errorf("chờ tiêu đề dự phòng dạng 'chương 5': %s", got)
	}
}
