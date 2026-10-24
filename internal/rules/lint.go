package rules

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Lint thực hiện các kiểm tra giới hạn sản phẩm tích hợp: quét dấu vết cơ chế còn
// sót trong nội dung, không phụ thuộc quy tắc người dùng và luôn chạy khi commit.
// Cùng hợp đồng với Check — chỉ trả về sự thật (Nguyên tắc Một), không chặn quy
// trình; việc thẩm duyệt hoặc người dùng sẽ phán quyết.
//
// Hiện có ba loại (đều xuất phát từ khiếm khuyết quan sát được trong sản phẩm của
// các lần chạy dài thực tế):
//   - markdown_residue: nội dung còn ** in đậm hoặc dòng tiêu đề # ngoài dòng đầu
//     (khi xuất txt, các ký hiệu sẽ lộ ra)
//   - non_cjk_fragments / cjk_leak: các đoạn trộn lẫn hệ chữ; hướng phát hiện được
//     tự động quyết định theo hệ chữ chính của nội dung (nội dung Trung Quốc báo
//     đoạn Latin; nội dung Latin như tiếng Việt báo đoạn Hán)
func Lint(text string) []Violation {
	var vs []Violation
	vs = appendMarkdownResidue(vs, text)
	vs = appendScriptMixing(vs, text)
	vs = appendBrokenWords(vs, text)
	vs = appendSelfDuplication(vs, text)
	vs = appendEnglishResidue(vs, text)
	return vs
}

func appendMarkdownResidue(vs []Violation, text string) []Violation {
	if n := strings.Count(text, "**"); n > 0 {
		vs = append(vs, Violation{
			Rule:     "markdown_residue",
			Target:   "**",
			Actual:   n,
			Severity: SeverityWarning,
		})
	}
	headings := 0
	seenContent := false
	for line := range strings.SplitSeq(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		// Tiêu đề # ở dòng đầu tiên không rỗng là định dạng hợp lệ của tệp chương
		// (không cố định theo số dòng, để chấp nhận dòng trống đầu tệp)
		first := !seenContent
		seenContent = true
		if !first && strings.HasPrefix(t, "#") {
			headings++
		}
	}
	if headings > 0 {
		vs = append(vs, Violation{
			Rule:     "markdown_residue",
			Target:   "#",
			Actual:   headings,
			Severity: SeverityWarning,
		})
	}
	return vs
}

var (
	latinFragmentRe = regexp.MustCompile(`[A-Za-z]{2,}`)
	// Bao gồm dấu câu CJK: thứ lọt vào nội dung tiếng Việt không chỉ là chữ Hán mà
	// còn là dấu câu toàn cục như「。，、？！」.
	// Thực nghiệm: nội dung chương 4 từng có một dấu「。」đứng lẻ; quy tắc chỉ nhận
	// chữ Hán hoàn toàn không thấy.
	cjkFragmentRe = regexp.MustCompile(`[\p{Han}\x{3000}-\x{303f}\x{ff01}-\x{ff5e}]+`)
)

// appendScriptMixing báo các đoạn thuộc "hệ chữ khác" lẫn vào nội dung.
//
// Hệ chữ nào được coi là lẫn tạp do chính nội dung quyết định, không đọc cấu hình:
// trong nội dung Trung Quốc, "pattern" trần là lỗi; còn mọi từ trong nội dung tiếng
// Việt đều dùng chữ Latin, nếu áp cùng quy tắc sẽ báo sai hàng nghìn lần mỗi chương
// và làm ngập ngữ cảnh thẩm duyệt — thực nghiệm một chương từng trúng 1021 lần dù
// không có chữ Hán nào. Ngược lại, "根系之力" lọt vào nội dung tiếng Việt mới là lỗi
// thật mà quy tắc cũ không thấy.
//
// Vì vậy, trước tiên xác định hệ chữ chính theo tỷ lệ ký tự, rồi chỉ báo nhóm thiểu
// số. Cách này đúng cho cả hai loại chủ đề và không hỏng nếu cấu hình sai. Từ ngoại
// hợp lệ (tên thương hiệu/viết tắt) vẫn có thể trúng; đây là sự thật mức warning
// để bộ thẩm duyệt phán quyết.
func appendScriptMixing(vs []Violation, text string) []Violation {
	latin := latinFragmentRe.FindAllString(text, -1)
	han := cjkFragmentRe.FindAllString(text, -1)

	// So sánh "số chữ Hán" với "số từ Latin", không so sánh số ký tự của hai bên:
	// một chữ Hán tương đương khoảng một từ, trong khi một từ Latin có nhiều chữ cái.
	// Nếu so số ký tự, chỉ vài "pattern"/"DNA" lẫn vào nội dung Trung Quốc cũng đủ
	// làm số ký tự Latin vượt số chữ Hán, khiến ngôn ngữ nội dung bị nhận sai.
	rule, matches := "non_cjk_fragments", latin
	if runeCount(han) <= len(latin) {
		// Nội dung dùng chữ Latin (tiếng Việt, v.v.): chữ Hán mới là phần lẫn tạp.
		rule, matches = "cjk_leak", han
	}
	if len(matches) == 0 {
		return vs
	}

	seen := make(map[string]struct{})
	var examples []string
	for _, m := range matches {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		if len(examples) < 3 {
			examples = append(examples, m)
		}
	}
	return append(vs, Violation{
		Rule:     rule,
		Target:   strings.Join(examples, "、"),
		Actual:   len(matches),
		Severity: SeverityWarning,
	})
}

func runeCount(ss []string) int {
	n := 0
	for _, s := range ss {
		n += utf8.RuneCountInString(s)
	}
	return n
}

// brokenWordRe khớp từ bị ngắt bởi ranh giới đoạn: một dòng kết thúc bằng chữ cái,
// sau một dòng trống lại bắt đầu bằng chữ thường.
// Thực nghiệm với nội dung tiếng Việt: 「n Tông, Ng」+ dòng trống +「ọc Lâm dừng bước」
// — tên Ngọc bị chia thành hai nửa. Một từ trong chữ Latin không thể vắt qua đoạn,
// nên hình dạng này không có cách viết chính đáng và có thể bị coi là khiếm khuyết ngay.
var brokenWordRe = regexp.MustCompile(`(?m)[\p{L}]\n\s*\n[[:space:]]*[\p{Ll}]`)

func appendBrokenWords(vs []Violation, text string) []Violation {
	matches := brokenWordRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return vs
	}
	return append(vs, Violation{
		Rule:     "broken_word",
		Target:   strings.Join(strings.Fields(matches[0]), "⏎"),
		Actual:   len(matches),
		Severity: SeverityWarning,
	})
}

// paraMinRunes là số rune tối thiểu của đoạn được đưa vào so sánh trùng lặp.
// Đoạn ngắn vốn dễ lặp ("Hắn gật đầu.", một tiếng "Bắt đầu!"); chỉ khi văn bản
// thành đoạn hiện lại nguyên văn từng chữ mới là sự cố sinh nội dung.
const paraMinRunes = 20

// paragraphs cắt ra các đoạn nội dung đủ dài để so sánh và bỏ qua dòng tiêu đề.
func paragraphs(text string) []string {
	var out []string
	for _, p := range strings.Split(text, "\n\n") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "#") {
			continue
		}
		if utf8.RuneCountInString(p) >= paraMinRunes {
			out = append(out, p)
		}
	}
	return out
}

// selfDupThreshold / crossDupThreshold là tỷ lệ ký tự trùng lặp tối đa trong một chương.
//
// Ngưỡng đến từ số đo thực tế: trong một cuốn 22 chương, 19 chương có cả hai tỷ lệ
// bằng 0,0%; ba chương có sự cố lần lượt là tự lặp 43,5% (chương 21, cùng một đoạn
// xuất hiện 5 lần), lặp xuyên chương 32,2% (chương 14 sao chép chương 13) và 12,9%
// (chương 22 sao chép chương 21). Tín hiệu có tính nhị phân, không có vùng xám, nên
// lấy 10% để dành đủ dư địa cho đoạn lặp có chủ đích như điệp khúc hay chú thuật.
const (
	selfDupThreshold  = 0.10
	crossDupThreshold = 0.10
)

func appendSelfDuplication(vs []Violation, text string) []Violation {
	paras := paragraphs(text)
	if len(paras) == 0 {
		return vs
	}
	count := make(map[string]int, len(paras))
	total := 0
	for _, p := range paras {
		count[p]++
		total += utf8.RuneCountInString(p)
	}
	dup, worst, worstN := 0, "", 0
	for p, n := range count {
		if n > 1 {
			dup += utf8.RuneCountInString(p) * (n - 1)
			if n > worstN {
				worst, worstN = p, n
			}
		}
	}
	if total == 0 || float64(dup)/float64(total) < selfDupThreshold {
		return vs
	}
	return append(vs, Violation{
		Rule:     "self_duplication",
		Target:   fmt.Sprintf("×%d %s", worstN, truncateRunes(worst, 50)),
		Limit:    fmt.Sprintf("<%.0f%%", selfDupThreshold*100),
		Actual:   fmt.Sprintf("%.0f%%", 100*float64(dup)/float64(total)),
		Severity: SeverityError,
	})
}

// CheckAgainstPrevious phát hiện trường hợp "chương mới thực ra là bản sao của chương trước".
//
// Writer có read_chapter nên thường xem chương trước đã viết gì, rồi sao chép làm
// phần tiếp nối — thực nghiệm chương 14 có 32,2% ký tự lấy nguyên văn từ chương 13,
// chương 22 có 12,9% từ chương 21.
//
// self_duplication không nhìn thấy loại sự cố này: nó chỉ thống kê trong một chương,
// trong khi chương bị sao chép hoàn toàn sạch bên trong. Vì vậy phải so riêng với
// "phần trên". Nếu previous rỗng thì bỏ qua.
func CheckAgainstPrevious(text string, previous []string) []Violation {
	cur := paragraphs(text)
	if len(cur) == 0 || len(previous) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	for _, prev := range previous {
		for _, p := range paragraphs(prev) {
			seen[p] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}

	dup, total, sample := 0, 0, ""
	for _, p := range unique(cur) {
		n := utf8.RuneCountInString(p)
		total += n
		if _, ok := seen[p]; ok {
			dup += n
			if sample == "" {
				sample = p
			}
		}
	}
	if total == 0 || float64(dup)/float64(total) < crossDupThreshold {
		return nil
	}
	return []Violation{{
		Rule:     "copied_previous_chapter",
		Target:   truncateRunes(sample, 60),
		Limit:    fmt.Sprintf("<%.0f%%", crossDupThreshold*100),
		Actual:   fmt.Sprintf("%.0f%%", 100*float64(dup)/float64(total)),
		Severity: SeverityError,
	}}
}

func unique(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0:0]
	for _, x := range in {
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	return out
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// englishFunctionWordRe chỉ thu tập các từ chức năng tiếng Anh — chúng không bao giờ
// được mượn vào nội dung tiếng Việt.
// Cố ý không thu từ nội dung (flow / lesson / footsteps, v.v.): trong chủ đề hiện đại,
// chúng có thể là từ ngoại hợp lệ; các cụm như "the flow" vốn đã bị từ the trong đó
// bắt, không cần tăng rủi ro báo sai.
var englishFunctionWordRe = regexp.MustCompile(
	`(?i)\b(not|the|and|but|for|from|with|they|this|that|just|one|was|were|have|when|which|while|into|over)\b`)

// vietnameseMarkRe khớp các chữ riêng của tiếng Việt: 7 chữ cơ sở
// (ăâđêôơư, gồm chữ hoa) cộng vùng Latin Extended Additional U+1EA0-U+1EF9 —
// vùng gần như dành riêng cho chữ có dấu tiếng Việt.
// Chỉ liệt kê vài ký tự dựng sẵn là chưa đủ: phần lớn chữ có dấu trong một câu
// tiếng Việt thông thường đều nằm trong vùng đó.
var vietnameseMarkRe = regexp.MustCompile(`[ăâđêôơưĂÂĐÊÔƠƯ\x{1ea0}-\x{1ef9}]`)

// vietnameseMarkFloor là số chữ riêng tối thiểu để coi nội dung là tiếng Việt.
// Một chương thật có hàng trăm đến hàng nghìn chữ như vậy; đặt ngưỡng chỉ để quy
// tắc hoàn toàn im lặng trong tác phẩm tiếng Anh hoặc tiếng Trung.
const vietnameseMarkFloor = 12

// appendEnglishResidue báo các từ chức năng tiếng Anh lẫn vào nội dung tiếng Việt.
//
// cjk_leak hoàn toàn không có tác dụng ở đây: tiếng Việt và tiếng Anh cùng dùng chữ
// Latin, nên "not"/"the" lẫn vào không thể phân biệt với nội dung ở tầng ký tự.
// Đây không phải chuyện nhỏ — thực nghiệm chương 18 có 27 lần not, 9 lần the và
// 3 lần from, ví dụ「Lá cây bắt đầu chuyển động—not nhanh chóng mà nhẹ nhàng」.
func appendEnglishResidue(vs []Violation, text string) []Violation {
	if len(vietnameseMarkRe.FindAllString(text, vietnameseMarkFloor)) < vietnameseMarkFloor {
		return vs
	}
	matches := englishFunctionWordRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return vs
	}
	seen := make(map[string]struct{})
	var examples []string
	for _, m := range matches {
		low := strings.ToLower(m)
		if _, ok := seen[low]; ok {
			continue
		}
		seen[low] = struct{}{}
		if len(examples) < 4 {
			examples = append(examples, low)
		}
	}
	return append(vs, Violation{
		Rule:     "english_residue",
		Target:   strings.Join(examples, ", "),
		Actual:   len(matches),
		Severity: SeverityError,
	})
}
