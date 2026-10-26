package exp

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

func TestRenderEPUB_StructuralInvariants(t *testing.T) {
	data, err := renderEPUB(
		domain.BookMetadata{Title: "光斑", Synopsis: "少年在光影交界处追索真相。"},
		[]int{1, 2},
		chapterTitleIndex{1: "雨夜归人", 2: "破晓"},
		nil,
		map[int]string{
			1: "# 第 1 章 雨夜归人\n\n他望着窗外。\n\n第二段。",
			2: "她推开门。",
		},
		expTextsZH,
	)
	if err != nil {
		t.Fatalf("renderEPUB: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("dữ liệu rỗng")
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("không phải zip hợp lệ: %v", err)
	}

	if len(zr.File) == 0 {
		t.Fatal("zip không có tệp nào")
	}
	first := zr.File[0]
	if first.Name != "mimetype" {
		t.Errorf("mục đầu tiên phải là mimetype, nhận %q", first.Name)
	}
	if first.Method != zip.Store {
		t.Errorf("mimetype phải không nén (Method=Store), nhận %d", first.Method)
	}

	files := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("mở %s: %v", f.Name, err)
		}
		buf, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("đọc %s: %v", f.Name, err)
		}
		files[f.Name] = string(buf)
	}

	if files["mimetype"] != "application/epub+zip" {
		t.Errorf("nội dung mimetype = %q", files["mimetype"])
	}

	for _, want := range []string{
		"META-INF/container.xml",
		"OEBPS/content.opf",
		"OEBPS/nav.xhtml",
		"OEBPS/style.css",
		"OEBPS/cover.xhtml",
		"OEBPS/chapter001.xhtml",
		"OEBPS/chapter002.xhtml",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("thiếu tệp bắt buộc %q", want)
		}
	}

	// container.xml trỏ tới OEBPS/content.opf
	if !strings.Contains(files["META-INF/container.xml"], `full-path="OEBPS/content.opf"`) {
		t.Errorf("container.xml không trỏ tới content.opf")
	}

	// content.opf bắt buộc có ba khối metadata + manifest + spine; thứ tự spine = thứ tự chương
	opf := files["OEBPS/content.opf"]
	for _, want := range []string{
		"<metadata", "</metadata>",
		"<manifest>", "</manifest>",
		"<spine>", "</spine>",
		"urn:uuid:",
		"<dc:title>光斑</dc:title>",
		"<dc:description>少年在光影交界处追索真相。</dc:description>",
		`href="chapter001.xhtml"`,
		`href="chapter002.xhtml"`,
		`idref="ch001"`,
		`idref="ch002"`,
	} {
		if !strings.Contains(opf, want) {
			t.Errorf("OPF thiếu %q", want)
		}
	}
	if idx1, idx2 := strings.Index(opf, `idref="ch001"`), strings.Index(opf, `idref="ch002"`); idx1 < 0 || idx1 > idx2 {
		t.Errorf("thứ tự spine sai: ch001=%d ch002=%d", idx1, idx2)
	}

	// XHTML chương chứa tiêu đề + đoạn văn đã escape; tiêu đề Markdown ở dòng đầu đã bị bỏ
	ch1 := files["OEBPS/chapter001.xhtml"]
	if !strings.Contains(ch1, "第 1 章 雨夜归人") {
		t.Errorf("chapter1 thiếu tiêu đề hiển thị")
	}
	if !strings.Contains(ch1, "<p>他望着窗外。</p>") {
		t.Errorf("chapter1 thiếu đoạn 1: %s", ch1)
	}
	if !strings.Contains(ch1, "<p>第二段。</p>") {
		t.Errorf("chapter1 thiếu đoạn 2: %s", ch1)
	}
	if strings.Contains(ch1, "# 第 1 章") {
		t.Errorf("chapter1 phải đã bỏ tiêu đề Markdown: %s", ch1)
	}

	// nav.xhtml liệt kê mọi chương
	nav := files["OEBPS/nav.xhtml"]
	if !strings.Contains(nav, `epub:type="toc"`) {
		t.Errorf("nav thiếu epub:type=toc")
	}
	if !strings.Contains(nav, `href="chapter001.xhtml"`) || !strings.Contains(nav, `href="chapter002.xhtml"`) {
		t.Errorf("nav thiếu liên kết chương")
	}
}

func TestRenderEPUB_HTMLEscape(t *testing.T) {
	data, err := renderEPUB(
		domain.BookMetadata{Title: "A & B", Synopsis: "E < F & G"}, // ký tự đặc biệt phải được escape
		[]int{1},
		chapterTitleIndex{1: "C \"D\""},
		nil,
		map[int]string{1: "正文 < & > 内容。"},
		expTextsZH,
	)
	if err != nil {
		t.Fatalf("renderEPUB: %v", err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	files := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		buf, _ := io.ReadAll(rc)
		_ = rc.Close()
		files[f.Name] = string(buf)
	}

	if !strings.Contains(files["OEBPS/cover.xhtml"], "A &amp; B") {
		t.Errorf("cover phải escape &: %s", files["OEBPS/cover.xhtml"])
	}
	if !strings.Contains(files["OEBPS/chapter001.xhtml"], "正文 &lt; &amp; &gt; 内容。") {
		t.Errorf("nội dung chương phải escape entity")
	}
	if !strings.Contains(files["OEBPS/content.opf"], "<dc:title>A &amp; B</dc:title>") {
		t.Errorf("opf phải escape & trong tiêu đề")
	}
	if !strings.Contains(files["OEBPS/content.opf"], "<dc:description>E &lt; F &amp; G</dc:description>") {
		t.Errorf("opf phải escape phần tóm tắt")
	}
}

// TestRenderEPUB_LayeredVolume kiểm chứng Đề cương phân tầng chỉ chèn dấu phân tách
// tập ở đầu tập và không bao giờ hiện dấu phân tách cung.
func TestRenderEPUB_LayeredVolume(t *testing.T) {
	locs := map[int]chapterLocation{
		1: {VolumeIdx: 1, VolumeTitle: "起源", IsFirstOfVolume: true},
		2: {VolumeIdx: 1, VolumeTitle: "起源"},
	}
	data, err := renderEPUB(
		domain.BookMetadata{Title: "X", Synopsis: "简介"},
		[]int{1, 2},
		chapterTitleIndex{1: "A", 2: "B"},
		locs,
		map[int]string{1: "正文一。", 2: "正文二。"},
		expTextsZH,
	)
	if err != nil {
		t.Fatalf("renderEPUB: %v", err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	files := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		buf, _ := io.ReadAll(rc)
		_ = rc.Close()
		files[f.Name] = string(buf)
	}

	ch1 := files["OEBPS/chapter001.xhtml"]
	if !strings.Contains(ch1, `class="volume-divider"`) || !strings.Contains(ch1, "第 1 卷 起源") {
		t.Errorf("ch1 phải có dấu phân tách tập: %s", ch1)
	}
	if strings.Contains(ch1, `class="arc-divider"`) {
		t.Errorf("dấu phân tách cung không được xuất hiện: %s", ch1)
	}

	ch2 := files["OEBPS/chapter002.xhtml"]
	if strings.Contains(ch2, `class="volume-divider"`) {
		t.Errorf("ch2 không được có dấu phân tách tập (cùng tập)")
	}
}

func TestRenderEPUB_NoCoverWhenNoTitle(t *testing.T) {
	data, err := renderEPUB(
		domain.BookMetadata{}, []int{1},
		chapterTitleIndex{1: "唯一一章"},
		nil,
		map[int]string{1: "正文。"},
		expTextsZH,
	)
	if err != nil {
		t.Fatalf("renderEPUB: %v", err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	for _, f := range zr.File {
		if f.Name == "OEBPS/cover.xhtml" {
			t.Errorf("cover.xhtml không được tồn tại khi tên sách rỗng")
		}
	}
	// content.opf không được tham chiếu cover
	for _, f := range zr.File {
		if f.Name != "OEBPS/content.opf" {
			continue
		}
		rc, _ := f.Open()
		buf, _ := io.ReadAll(rc)
		_ = rc.Close()
		if strings.Contains(string(buf), "cover.xhtml") || strings.Contains(string(buf), `idref="cover"`) {
			t.Errorf("OPF không được tham chiếu cover khi không có bìa: %s", buf)
		}
	}
}

// TestRenderEPUB_VietnameseLanguage kiểm chứng ngôn ngữ xuất hiện trong sản phẩm:
// xml:lang, dc:language, tiêu đề chương, nhãn bìa và mục lục đều theo ngôn ngữ
// tác phẩm; tác phẩm tiếng Việt không được còn "第 N 章".
func TestRenderEPUB_VietnameseLanguage(t *testing.T) {
	data, err := renderEPUB(
		domain.BookMetadata{Title: "Quang vệ", Synopsis: "Một câu chuyện về ánh sáng."},
		[]int{1},
		chapterTitleIndex{1: "Người trở về"},
		nil,
		map[int]string{1: "# Chương 1  Người trở về\n\nAnh nhìn ra cửa sổ."},
		expTextsVI,
	)
	if err != nil {
		t.Fatalf("renderEPUB: %v", err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	files := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		buf, _ := io.ReadAll(rc)
		_ = rc.Close()
		files[f.Name] = string(buf)
	}

	ch1 := files["OEBPS/chapter001.xhtml"]
	if !strings.Contains(ch1, `xml:lang="vi"`) {
		t.Errorf("xhtml chương phải khai báo vi: %s", ch1)
	}
	if !strings.Contains(ch1, "Chương 1 Người trở về") {
		t.Errorf("chương phải dùng tiêu đề tiếng Việt: %s", ch1)
	}
	if strings.Contains(ch1, "第") {
		t.Errorf("bản xuất tiếng Việt không được chứa dấu hiệu chương tiếng Trung: %s", ch1)
	}
	if strings.Contains(ch1, "# Chương 1") {
		t.Errorf("tiêu đề chương Markdown phải được bỏ: %s", ch1)
	}

	opf := files["OEBPS/content.opf"]
	if !strings.Contains(opf, "<dc:language>vi</dc:language>") {
		t.Errorf("OPF phải khai báo vi: %s", opf)
	}
	if !strings.Contains(opf, `xml:lang="vi"`) {
		t.Errorf("OPF phải khai báo vi: %s", opf)
	}

	nav := files["OEBPS/nav.xhtml"]
	if !strings.Contains(nav, "Mục lục") {
		t.Errorf("nav phải dùng tiêu đề mục lục tiếng Việt: %s", nav)
	}
	if !strings.Contains(nav, "Bìa") {
		t.Errorf("nav phải dùng nhãn bìa tiếng Việt: %s", nav)
	}
	if !strings.Contains(files["OEBPS/cover.xhtml"], "Bìa") {
		t.Errorf("cover phải dùng nhãn tiếng Việt: %s", files["OEBPS/cover.xhtml"])
	}
}

// TestRenderEPUB_ZHLanguage kiểm chứng tác phẩm tiếng Trung vẫn xuất theo bố cục
// tiếng Trung.
func TestRenderEPUB_ZHLanguage(t *testing.T) {
	data, err := renderEPUB(
		domain.BookMetadata{Title: "光斑", Synopsis: "简介"},
		[]int{1},
		chapterTitleIndex{1: "雨夜归人"},
		nil,
		map[int]string{1: "正文。"},
		expTextsZH,
	)
	if err != nil {
		t.Fatalf("renderEPUB: %v", err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	files := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		buf, _ := io.ReadAll(rc)
		_ = rc.Close()
		files[f.Name] = string(buf)
	}
	if !strings.Contains(files["OEBPS/content.opf"], "<dc:language>zh-CN</dc:language>") {
		t.Errorf("OPF phải khai báo zh-CN: %s", files["OEBPS/content.opf"])
	}
	if !strings.Contains(files["OEBPS/content.opf"], `xml:lang="zh-CN"`) {
		t.Errorf("OPF phải khai báo xml:lang zh-CN: %s", files["OEBPS/content.opf"])
	}
	if !strings.Contains(files["OEBPS/chapter001.xhtml"], `xml:lang="zh-CN"`) {
		t.Errorf("chương phải khai báo xml:lang zh-CN: %s", files["OEBPS/chapter001.xhtml"])
	}
	if !strings.Contains(files["OEBPS/chapter001.xhtml"], "第 1 章 雨夜归人") {
		t.Errorf("chương phải dùng tiêu đề tiếng Trung: %s", files["OEBPS/chapter001.xhtml"])
	}
}

func TestSplitParagraphs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a\n\nb", []string{"a", "b"}},
		{"a\n\n\n\nb", []string{"a", "b"}}, // nhiều dòng trống gộp thành một lần phân tách
		{"a\nb", []string{"a b"}},          // một dấu xuống dòng trong đoạn thành khoảng trắng
		{"  ", nil},                        // toàn khoảng trắng trả về nil
		{"a\r\n\r\nb", []string{"a", "b"}}, // tương thích CRLF
	}
	for _, c := range cases {
		got := splitParagraphs(c.in)
		if !equalStrings(got, c.want) {
			t.Errorf("splitParagraphs(%q) = %v, cần %v", c.in, got, c.want)
		}
	}
}

func TestBookIdentifier_StableAcrossChapterRanges(t *testing.T) {
	// Hai lần xuất cùng tên tác phẩm với phạm vi khác nhau phải trả về cùng ID —
	// nhờ đó trình đọc nhận ra đây là "phiên bản cập nhật"
	idFull := bookIdentifier("光斑")
	idAgain := bookIdentifier("光斑")
	if idFull != idAgain {
		t.Errorf("định danh không ổn định giữa các lần gọi: %s vs %s", idFull, idAgain)
	}
	if id := bookIdentifier("月相"); id == idFull {
		t.Errorf("hai tên khác nhau phải tạo ra định danh khác nhau")
	}
	if !strings.HasPrefix(idFull, "urn:uuid:") {
		t.Errorf("định danh phải có tiền tố urn:uuid:, nhận %s", idFull)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
