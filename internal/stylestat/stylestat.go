// Package stylestat thống kê văn phong ở cấp toàn sách trên phần văn đã viết
// và chỉ xuất ra sự thật thuần túy.
//
// Động cơ: cửa sổ đánh giá trong một cung (~10 chương) vốn mù với hiện tượng
// đóng khuôn ở cấp toàn sách — tic câu cú xuất hiện hàng chục lần mỗi chương,
// hình thức cuối chương đồng dạng, lặp lại qua các chương; xem từng chương thì
// mọi chỗ đều "bình thường", chỉ thống kê toàn sách mới lộ ra. Phần đếm giao
// cho code (tất định, không bịa đặt), phần phán quyết giao cho LLM (editor chấm
// điểm chiều theo số liệu, writer tự tránh theo số liệu đó).
// Compute dùng cho đánh giá offline một lần toàn bộ; lúc chạy, Tracker bảo trì
// tăng dần theo từng chương.
package stylestat

import (
	"regexp"
	"sort"
	"strings"
)

// minChapters: dưới số chương này không xuất thống kê — mẫu quá nhỏ thì tần
// suất không mang ý nghĩa.
const minChapters = 5

// phraseWindow: đào mỹ từ động chỉ nhìn N chương gần nhất — writer cần tránh là
// "vật nói nhiều hiện nay".
const phraseWindow = 20

// Input là đầu vào thống kê. Chapters xếp theo số chương tăng dần; Stopwords là
// các danh từ riêng như tên nhân vật, bị bỏ qua khi đào mỹ từ động (tên xuất
// hiện thường cao, không phải vấn đề văn phong).
type Input struct {
	Chapters  []string
	Titles    []string
	Stopwords []string
}

// Stats là kết quả thống kê văn phong toàn sách. Mọi trường đều là đếm sự
// thật, không chứa phán quyết hay chỉ dẫn nào.
type Stats struct {
	Chapters          int            `json:"chapters"`
	Patterns          []PatternStat  `json:"patterns,omitempty"`
	TopPhrases        []PhraseStat   `json:"top_phrases,omitempty"`
	RepeatedSentences []SentenceStat `json:"repeated_sentences,omitempty"`
	Ending            EndingStat     `json:"ending"`
	OpeningTimeRate   float64        `json:"opening_time_rate"`
	TitleFormats      *TitleStat     `json:"title_formats,omitempty"`
}

// PatternStat là đếm toàn sách của một lớp mẫu câu cố định (tic văn phong AI
// chung).
type PatternStat struct {
	Name       string  `json:"name"`
	Total      int     `json:"total"`
	PerChapter float64 `json:"per_chapter"`
}

// PhraseStat là cụm từ cao tần đào được trong phraseWindow chương gần nhất.
type PhraseStat struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// SentenceStat là câu dài lặp nguyên văn qua nhiều chương (bằng chứng trực
// tiếp của việc lặp lại phần kể).
type SentenceStat struct {
	Text     string `json:"text"`
	Chapters int    `json:"chapters"`
	Count    int    `json:"count"`
}

// EndingStat là phân bố hình thức dòng cuối chương. Kết chương ngắn bản thân vẫn
// hợp lệ, toàn sách đồng dạng mới là vấn đề.
type EndingStat struct {
	ShortRatio  float64 `json:"short_ratio"`
	MedianRunes int     `json:"median_runes"`
}

// TitleStat đếm việc trộn lẫn tiền tố "Chương N" trong tiêu đề chương
// (trộn lẫn = dấu vết cơ chế lộ ra trong sản phẩm).
type TitleStat struct {
	WithPrefix    int `json:"with_prefix"`
	WithoutPrefix int `json:"without_prefix"`
}

// patternDefs là các lớp mẫu câu văn phong AI chung. Biểu thức chính quy là
// đường cơ sở phát hiện trên văn bản Trung, giữ nguyên theo quy ước [P].
// Số đếm là xấp xỉ (biểu thức không phân tích ngữ pháp), dùng để so đường cơ
// sở dọc của chính cuốn sách này, độ chính xác tuyệt đối không quan trọng.
//
// Tên hiển thị của từng lớp nằm trong bảng song ngữ (texts.go), theo đúng thứ
// tự của slice này.
var patternDefs = []struct {
	re *regexp.Regexp
}{
	{regexp.MustCompile(`不是[^。！？\n]{1,24}?[，、]?(?:而)?是`)},
	{regexp.MustCompile(`[一两二三四五六七八九十几数半][息瞬]`)},
	{regexp.MustCompile(`像一|仿佛|如同|宛如`)},
	{regexp.MustCompile(`沉默了|没有说话|没有回头`)},
	{regexp.MustCompile(`眼[中底]闪过|目光一凝|瞳孔一缩|眼眶微红|嘴角[微轻一]?[勾扬翘]|咬了咬唇|不可置信`)},
	{regexp.MustCompile(`心头一[紧沉颤]|身子一[颤震僵]|倒吸(?:了)?一口凉气`)},
	{regexp.MustCompile(`心想|意识到|感到|觉得`)},
	{regexp.MustCompile(`一种说不出的|说不清[的道]|的意义在于|真正的[^。！？\n]{1,10}是`)},
}

var (
	sentenceSplit = regexp.MustCompile(`[。！？\n]+`)
	openingTimeRe = regexp.MustCompile(`夜|清晨|黎明|天亮|醒来|晨光|一整夜`)
	titlePrefixRe = regexp.MustCompile(`^#{0,2}\s*第[零〇一二三四五六七八九十百千万\d]+章`)
)

// shortEndingRunes: dòng cuối không quá số ký tự này thì tính là "kết chương
// ngắn".
const shortEndingRunes = 30

// Compute tính thống kê văn phong toàn sách; khi số chương không đủ thì trả
// nil.
func Compute(in Input) *Stats {
	n := len(in.Chapters)
	if n < minChapters {
		return nil
	}
	all := strings.Join(in.Chapters, "\n")

	s := &Stats{Chapters: n}
	texts := activeTexts()
	for i, def := range patternDefs {
		total := len(def.re.FindAllStringIndex(all, -1))
		if total == 0 {
			continue
		}
		s.Patterns = append(s.Patterns, PatternStat{
			Name:       texts.patterns[i],
			Total:      total,
			PerChapter: round1(float64(total) / float64(n)),
		})
	}
	s.TopPhrases = minePhrases(recentWindow(in.Chapters), in.Stopwords)
	s.RepeatedSentences = repeatedSentences(in.Chapters)
	s.Ending = endingShape(in.Chapters)
	s.OpeningTimeRate = openingTimeRate(in.Chapters)
	s.TitleFormats = titleFormats(in.Titles)
	return s
}

func recentWindow(chapters []string) []string {
	if len(chapters) <= phraseWindow {
		return chapters
	}
	return chapters[len(chapters)-phraseWindow:]
}

// minePhrases đào mỹ từ cao tần dài 3-6 chữ trong cửa sổ.
// Lọc: có dấu câu/khoảng trắng, đầu cuối là từ chức, trùng danh từ riêng; khử
// trùng: bỏ cụt làm viết con của cụt đã chọn.
func minePhrases(chapters []string, stopwords []string) []PhraseStat {
	text := strings.Join(chapters, "\n")
	runes := []rune(text)
	threshold := max(8, len(chapters)/2)

	counts := make(map[string]int)
	for size := 3; size <= 6; size++ {
		for i := 0; i+size <= len(runes); i++ {
			gram := runes[i : i+size]
			if !validGram(gram) {
				continue
			}
			counts[string(gram)]++
		}
	}

	stopGrams := stopwordBigrams(stopwords)
	type cand struct {
		text  string
		count int
	}
	var cands []cand
	for g, c := range counts {
		if c < threshold || hitStopword(g, stopGrams) {
			continue
		}
		cands = append(cands, cand{g, c})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].count != cands[j].count {
			return cands[i].count > cands[j].count
		}
		// Cùng tần suất thì lấy cụt dài hơn (thông tin nhiều hơn), rồi sắp
		// theo thứ tự từ điển để ổn định
		if len(cands[i].text) != len(cands[j].text) {
			return len(cands[i].text) > len(cands[j].text)
		}
		return cands[i].text < cands[j].text
	})

	var out []PhraseStat
	for _, c := range cands {
		if len(out) >= 8 {
			break
		}
		dup := false
		for _, picked := range out {
			if strings.Contains(picked.Text, c.text) || strings.Contains(c.text, picked.Text) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, PhraseStat{Text: c.text, Count: c.count})
		}
	}
	return out
}

// gramEdgeStop: n-gram có đầu hoặc cuối là một trong các từ chức/đại từ này
// không phải cụt văn phong, bỏ qua.
const gramEdgeStop = "的了着是在和与就也都还又把被他她它我你这那"

func validGram(gram []rune) bool {
	for _, r := range gram {
		if r < 0x4E00 || r > 0x9FFF { // chỉ nhận đoạn toàn chữ Hán
			return false
		}
	}
	if strings.ContainsRune(gramEdgeStop, gram[0]) || strings.ContainsRune(gramEdgeStop, gram[len(gram)-1]) {
		return false
	}
	return true
}

// stopwordBigrams tách danh từ riêng thành các đoạn 2 chữ: tên nhân vật
// thường xuất hiện dạng cắt ("九渊负手" chứa "九渊"), đối chiếu theo cả tên sẽ
// lọt. Thà lọc hơi chặt — thiếu một cụt tần suất không sao, có tên người lọt
// vào danh sách vật nói nhiều mới là nhiễu.
func stopwordBigrams(stopwords []string) []string {
	var grams []string
	for _, w := range stopwords {
		runes := []rune(strings.TrimSpace(w))
		if len(runes) < 2 {
			continue
		}
		for i := 0; i+2 <= len(runes); i++ {
			grams = append(grams, string(runes[i:i+2]))
		}
	}
	return grams
}

func hitStopword(gram string, stopGrams []string) bool {
	for _, g := range stopGrams {
		if strings.Contains(gram, g) {
			return true
		}
	}
	return false
}

// repeatedSentences tìm câu dài ≥12 chữ lặp nguyên văn qua ≥3 chương, lấy
// top 5 theo số lần.
func repeatedSentences(chapters []string) []SentenceStat {
	type rec struct {
		count    int
		chapters map[int]struct{}
	}
	seen := make(map[string]*rec)
	for ci, text := range chapters {
		for sent, count := range chapterSentenceCounts(text) {
			r := seen[sent]
			if r == nil {
				r = &rec{chapters: make(map[int]struct{})}
				seen[sent] = r
			}
			r.count += count
			r.chapters[ci] = struct{}{}
		}
	}

	var out []SentenceStat
	for sent, r := range seen {
		if len(r.chapters) < 3 {
			continue
		}
		out = append(out, SentenceStat{Text: truncateRunes(sent, 40), Chapters: len(r.chapters), Count: r.count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Text < out[j].Text
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// trimWrappedQuotes bóc dấu nháy bao ngoài: cùng một câu thoại có/không có
// dấu nháy đầu không được tính thành hai câu.
func trimWrappedQuotes(sentence string) string {
	return strings.Trim(strings.TrimSpace(sentence), `"“”‘’「」『』`)
}

func endingShape(chapters []string) EndingStat {
	var lengths []int
	short := 0
	for _, text := range chapters {
		line := lastNonEmptyLine(text)
		if line == "" {
			continue
		}
		n := len([]rune(line))
		lengths = append(lengths, n)
		if n <= shortEndingRunes {
			short++
		}
	}
	if len(lengths) == 0 {
		return EndingStat{}
	}
	sort.Ints(lengths)
	return EndingStat{
		ShortRatio:  round2(float64(short) / float64(len(lengths))),
		MedianRunes: lengths[len(lengths)/2],
	}
}

func openingTimeRate(chapters []string) float64 {
	hit := 0
	for _, text := range chapters {
		if openingTimeRe.MatchString(firstParagraph(text)) {
			hit++
		}
	}
	return round2(float64(hit) / float64(len(chapters)))
}

func titleFormats(titles []string) *TitleStat {
	if len(titles) == 0 {
		return nil
	}
	t := &TitleStat{}
	for _, title := range titles {
		if strings.TrimSpace(title) == "" {
			continue
		}
		if titlePrefixRe.MatchString(title) {
			t.WithPrefix++
		} else {
			t.WithoutPrefix++
		}
	}
	// Chỉ trộn lẫn mới đáng báo; thống nhất định dạng không phải vấn đề trên
	// phương diện sự thật
	if t.WithPrefix == 0 || t.WithoutPrefix == 0 {
		return nil
	}
	return t
}

func lastNonEmptyLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// firstParagraph lấy dòng đầu tiên không rỗng và không phải tiêu đề Markdown
// (dòng đầu tệp chương thường là tiêu đề #).
func firstParagraph(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }
func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
