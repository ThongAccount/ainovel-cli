package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	// Cách viết số chương chọn theo ngôn ngữ: tiếng Việt「Chương 12」, tiếng Trung
	//「第 12 章」, tiếng Anh「Chapter 12」.
	chapterNumberRe = regexp.MustCompile(`^\s*(?i:(chương|chapter)\s*(\d+)|第\s*(\d+)\s*章)`)

	// Trang trí tiêu đề dòng đầu do model tạo: dòng tiêu đề từ # đến ######, hoặc
	// cả dòng **in đậm**.
	headingDecorRe = regexp.MustCompile(`^\s*(?:#{1,6}\s*|\*\*)\s*|\s*\*\*\s*$`)
)

// FixChapterNumber sửa số chương trong tiêu đề thành số chương thật.
//
// Sự cố thực nghiệm: dàn ý sinh trong giai đoạn quy hoạch đặt tên chương là
// 「Chương 19..23」, nhưng khi ghi xuống thực tế là chương 1..5. Vì vậy ChapterRecord
// có chapter=2 trong khi title="Chương 20" — cùng một bản ghi tự mâu thuẫn và mục
// lục người đọc thấy bị nhảy số. Số chương là sự thật engine đã biết, không nên để
// trí nhớ của model quyết định.
//
// Nếu tiêu đề không chứa số chương nhận dạng được, trả về nguyên bản: không đoán,
// không cố nhồi số vào.
func FixChapterNumber(title string, chapter int) string {
	m := chapterNumberRe.FindStringSubmatchIndex(title)
	if m == nil || chapter <= 0 {
		return title
	}
	head := title[m[0]:m[1]]
	for _, g := range [][2]int{{m[4], m[5]}, {m[6], m[7]}} {
		if g[0] < 0 {
			continue
		}
		if n, err := strconv.Atoi(title[g[0]:g[1]]); err == nil && n != chapter {
			// Chỉ thay số, giữ nguyên cách viết và chữ hoa/thường (Chương / 第…章 / Chapter).
			head = head[:g[0]-m[0]] + strconv.Itoa(chapter) + head[g[1]-m[0]:]
		}
	}
	return head + title[m[1]:]
}

// ApplyChapterHeading bảo đảm dòng đầu nội dung là tiêu đề cấp một đúng chuẩn.
//
// Engine đã giữ đúng tiêu đề trong ChapterFacts.Title nhưng lại ghi nguyên nội dung
// do model trả về: thực nghiệm 9/15 chương không có dòng tiêu đề, một chương dùng
// ## cấp hai và một chương dùng **in đậm**. Tiêu đề là cấu trúc, không phải sáng
// tác — engine thống nhất render để model có viết hay không cũng không ảnh hưởng
// sản phẩm cuối.
//
// Chỉ thay dòng đầu khi nó đúng là tiêu đề (trùng title, hoặc có tiền tố số chương);
// nếu không thì luôn chèn tiêu đề phía trước, tránh nuốt mất một câu văn bắt đầu
// bằng phần in đậm.
func ApplyChapterHeading(content, title string, chapter int) string {
	title = strings.TrimSpace(FixChapterNumber(strings.TrimSpace(title), chapter))
	if title == "" {
		return content
	}

	lines := strings.Split(content, "\n")
	first := 0
	for first < len(lines) && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	if first < len(lines) && isChapterHeadingLine(lines[first], title) {
		lines = lines[first+1:]
	} else {
		lines = lines[first:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return fmt.Sprintf("# %s\n\n%s", title, strings.Join(lines, "\n"))
}

func isChapterHeadingLine(line, title string) bool {
	bare := strings.TrimSpace(headingDecorRe.ReplaceAllString(strings.TrimSpace(line), ""))
	if bare == "" {
		return false
	}
	if strings.EqualFold(bare, title) {
		return true
	}
	// Bắt đầu bằng số chương và đủ ngắn: đây là dòng tiêu đề, không phải đoạn văn.
	return chapterNumberRe.MatchString(bare) && len([]rune(bare)) <= 80
}
