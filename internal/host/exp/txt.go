package exp

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// chapterTitleIndex tra tiêu đề theo số chương, trả về chuỗi rỗng nếu thiếu.
type chapterTitleIndex map[int]string

func buildTitleIndex(outline []domain.OutlineEntry) chapterTitleIndex {
	idx := make(chapterTitleIndex, len(outline))
	for _, e := range outline {
		if e.Title != "" {
			idx[e.Chapter] = e.Title
		}
	}
	return idx
}

// chapterLocation mô tả vị trí của một chương trong dàn ý phân cấp. Chỉ giữ
// thông tin tập cần cho bố cục xuất — cung không đưa vào bản xuất (theo góc nhìn
// của người đọc, cung là cấu trúc nội bộ quá chi tiết).
type chapterLocation struct {
	VolumeIdx       int
	VolumeTitle     string
	IsFirstOfVolume bool
}

// buildLocations dựng ánh xạ {chapter -> location} theo thứ tự chương toàn cục
// của dàn ý phân cấp. Số chương được dựng lại theo đúng quy tắc của FlattenOutline
// (cộng dồn theo thứ tự cung trong tập), để giữ khớp với số chương trong
// Progress.CompletedChapters. Vẫn phải duyệt lớp cung (điều kiện cần để tính số
// chương toàn cục), nhưng cung không được đưa vào location — bản xuất chỉ chèn
// dấu phân cách ở đầu tập.
func buildLocations(volumes []domain.VolumeOutline) map[int]chapterLocation {
	if len(volumes) == 0 {
		return nil
	}
	locs := make(map[int]chapterLocation)
	ch := 0
	for _, v := range volumes {
		firstOfVol := true
		for _, a := range v.Arcs {
			for range a.Chapters {
				ch++
				locs[ch] = chapterLocation{
					VolumeIdx:       v.Index,
					VolumeTitle:     v.Title,
					IsFirstOfVolume: firstOfVol,
				}
				firstOfVol = false
			}
		}
	}
	return locs
}

// chapterHeaderRe khớp dòng tiêu đề Markdown có số chương ở đầu (# 第N章 /
// ## 第 12 章 ... / # Chương 3). Chấp nhận cả hai ngôn ngữ: nội dung truyện có
// thể dao động giữa hai cách viết khi sáng tác, và bộ xuất đều phải bỏ tiêu đề
// đó để không lặp với tiêu đề chương thống nhất do bộ xuất tạo.
var chapterHeaderRe = regexp.MustCompile(`(?i)^#+\s+(第.*章|chương\s+\d+)`)

// atxTitleRe trích phần chữ của tiêu đề ATX (# tiêu đề).
var atxTitleRe = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*$`)

// stripChapterTitleHeader bỏ dòng tiêu đề chương ở đầu nếu nó sẽ lặp với tiêu đề
// thống nhất do bộ xuất tạo. Có hai trường hợp: ① "# 第N章 …" (có số chương);
// ② tiêu đề Markdown có nội dung đúng bằng tiêu đề chương (Người viết thường
// đặt tên chương thuần vào dòng đầu nội dung, như "# 边村浮生", lặp với
// "第 N 章 边村浮生" do bộ xuất tạo). H1 khác (như "# 序章") được coi là một phần
// của nội dung và được giữ lại. Bên gọi chịu trách nhiệm TrimSpace trước, nên
// dòng trống đầu tiên không nằm trong phạm vi xét.
func stripChapterTitleHeader(content, title string) string {
	first, rest, hasNewline := strings.Cut(content, "\n")
	if !isChapterTitleLine(first, title) {
		return content
	}
	if !hasNewline {
		return ""
	}
	return strings.TrimLeft(rest, "\n")
}

func isChapterTitleLine(line, title string) bool {
	if chapterHeaderRe.MatchString(line) {
		return true
	}
	if title = strings.TrimSpace(title); title == "" {
		return false
	}
	m := atxTitleRe.FindStringSubmatch(line)
	return len(m) == 2 && strings.TrimSpace(m[1]) == title
}

// renderTXT ghép nối văn bản cuối cùng.
//
// Thứ tự chương do chapters quyết định (bên gọi đã sắp xếp tăng dần số chương và
// loại trùng). bodies/titleIdx/locations đều xử lý theo nguyên tắc "thiếu thì hạ
// cấp": thiếu tiêu đề chỉ xuất "Chương N"; thiếu vị trí phân cấp thì coi như dàn ý
// phẳng.
func renderTXT(
	novelName string,
	chapters []int,
	titleIdx chapterTitleIndex,
	locations map[int]chapterLocation,
	bodies map[int]string,
	texts expTexts,
) string {
	var b strings.Builder

	if name := strings.TrimSpace(novelName); name != "" {
		fmt.Fprintf(&b, texts.bookTitle+"\n\n", name)
	}

	useLayered := len(locations) > 0

	for i, ch := range chapters {
		if useLayered {
			if loc, ok := locations[ch]; ok && loc.IsFirstOfVolume {
				b.WriteString("\n═══════════════════════════════════════════\n")
				fmt.Fprintf(&b, "           "+texts.volumeFmt+"  %s\n", loc.VolumeIdx, strings.TrimSpace(loc.VolumeTitle))
				b.WriteString("═══════════════════════════════════════════\n\n")
			}
		}

		title := strings.TrimSpace(titleIdx[ch])
		if title != "" {
			fmt.Fprintf(&b, texts.chapterFmt+"  %s\n\n", ch, title)
		} else {
			fmt.Fprintf(&b, texts.chapterFmt+"\n\n", ch)
		}

		body := stripChapterTitleHeader(strings.TrimSpace(bodies[ch]), title)
		b.WriteString(body)
		b.WriteString("\n")
		if i < len(chapters)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}
