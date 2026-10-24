package userrules

import "github.com/voocel/ainovel-cli/internal/i18n"

// ruleTexts chứa văn bản mô hình đọc trong lớp chuẩn hóa quy tắc: mô tả hợp
// đồng, nhãn schema, prompt hệ thống của bộ chuẩn hóa và các thông báo lỗi
// được đưa lại cho model để tự sửa.
//
// Lớp này có store nên ngôn ngữ lấy từ st.Language(); bộ chuẩn hóa dựng
// độc lập (NewNormalizer) rơi về i18n.Active().
type ruleTexts struct {
	// Mô tả hợp đồng và nhãn schema.
	contractDesc       string
	genreLabel         string
	forbiddenCharsLbl  string
	charLabel          string
	forbiddenPhrasesLb string
	phraseLabel        string
	fatigueWordsLabel  string
	fatigueWordLabel   string
	maxPerChapterLabel string
	preferencesLabel   string
	uncertainLabel     string
	uncertainItemLabel string

	// Prompt hệ thống của bộ chuẩn hóa.
	systemPrompt string

	// Thông báo lỗi kiểm tra hợp lệ — đưa lại cho model để tự sửa.
	emptyFatigueWord     string
	invalidMaxPerChapter string
}

var ruleTextsVI = &ruleTexts{
	contractDesc:       "Chuẩn hóa quy tắc viết bằng ngôn ngữ tự nhiên của người dùng thành trường có cấu trúc",
	genreLabel:         "Thể loại; không có thì chuỗi rỗng",
	forbiddenCharsLbl:  "Ký tự bị cấm xuất hiện",
	charLabel:          "Ký tự",
	forbiddenPhrasesLb: "Cụm từ bị cấm xuất hiện (khớp chính xác theo nghĩa đen)",
	phraseLabel:        "Cụm từ",
	fatigueWordsLabel:  "Từ mệt mỏi và giới hạn số lần xuất hiện mỗi chương",
	fatigueWordLabel:   "Từ mệt mỏi",
	maxPerChapterLabel: "Giới hạn số lần xuất hiện mỗi chương (số nguyên dương)",
	preferencesLabel:   "Sở thích về văn phong/nhân vật/thẩm mỹ bằng ngôn ngữ tự nhiên; không có thì chuỗi rỗng",
	uncertainLabel:     "Các mục cố ý không nâng lên structured kèm lý do",
	uncertainItemLabel: "Mục",

	systemPrompt: `Bạn là «Trình chuẩn hóa quy tắc» của hệ thống viết tiểu thuyết bằng AI. Bạn đọc các quy tắc viết dài hạn của người dùng (bằng ngôn ngữ tự nhiên) từ một nguồn, nâng những quy tắc rõ ràng và có thể kiểm tra bằng máy lên structured, phần còn lại xếp vào preferences hoặc uncertain.

【Nâng lên có thận trọng — quan trọng nhất】
- Chỉ ghi vào structured khi người dùng nói rõ ràng, không mơ hồ.
- forbidden_chars/forbidden_phrases là mức error: chỉ nâng khi có lệnh cấm rõ ràng ("không được xuất hiện X / cấm X / đừng viết X").
- fatigue_words: chỉ nâng khi có đồng thời "từ cụ thể" và "ngưỡng số lần rõ ràng"; "bớt dùng X / đừng dùng X nhiều" mà không nêu số thì đưa vào preferences, tuyệt đối không tự bịa ngưỡng.
- Ý định về độ dài/chương ("mỗi chương 3000 chữ", "ngắn hơn một chút") luôn đưa vào preferences: độ dài chương là vấn đề nhịp truyện, để khi sáng tác tự nắm, không kiểm tra bằng máy.
- Không kiểm tra được bằng máy, không có ngưỡng rõ ràng, phụ thuộc ngữ cảnh thì đưa vào preferences.
- Nguyên tắc: thà bỏ sót vào preferences còn hơn nâng sai (nâng sai sẽ báo lỗi sai mỗi chương).

preferences giữ lại sở thích văn phong, nhân vật và thẩm mỹ dưới dạng một đoạn văn xuôi dễ đọc.
uncertain nêu các mục bạn cố ý không nâng lên structured và lý do.`,

	emptyFatigueWord:     "fatigue_words chứa mục từ rỗng",
	invalidMaxPerChapter: "fatigue_words[%q].max_per_chapter phải là số nguyên dương, nhận %d",
}

var ruleTextsZH = &ruleTexts{
	contractDesc:       "把用户自然语言写作规则归一化为结构化字段",
	genreLabel:         "题材;无则空字符串",
	forbiddenCharsLbl:  "禁止出现的字符",
	charLabel:          "字符",
	forbiddenPhrasesLb: "禁止出现的短语(字面精确匹配)",
	phraseLabel:        "短语",
	fatigueWordsLabel:  "疲劳词及每章出现上限",
	fatigueWordLabel:   "疲劳词",
	maxPerChapterLabel: "每章出现次数上限(正整数)",
	preferencesLabel:   "自然语言风格/人物/审美偏好;无则空字符串",
	uncertainLabel:     "故意未提升到 structured 的项+原因",
	uncertainItemLabel: "条目",

	systemPrompt: `你是 AI 小说写作系统的「规则归一化器」。你读取用户某一个来源的长期写作规则（自然语言），把明确且可机械检查的规则提升到 structured，其余内容归入 preferences 或 uncertain。

【保守提升——最重要】
- 只有用户明确、无歧义时才写入 structured。
- forbidden_chars/forbidden_phrases 是 error 级:只有「不要出现X/禁用X/别写X」这类明确禁止才提升。
- fatigue_words:只有同时给出「明确的词」和「明确的次数阈值」才提升;「少用X/别老用X」没给数字的放进 preferences,绝不自己发明阈值。
- 字数/篇幅类意愿(「每章3000字」「短一点」)一律放 preferences:章节长度是叙事节奏问题,由创作时自然把握,不做机械检查。
- 不可机械检查、无明确阈值、依赖语境的,一律放 preferences。
- 原则:宁可漏进 structured,也不要错误提升(那会每章误报)。

preferences 用一段可读的自然语言保留风格、人物与审美偏好。
uncertain 说明你故意没有提升到 structured 的项目及原因。`,

	emptyFatigueWord:     "fatigue_words 含空词条目",
	invalidMaxPerChapter: "fatigue_words[%q].max_per_chapter 必须是正整数, got %d",
}

// ruleTextsFor chọn bảng văn bản theo ngôn ngữ tác phẩm.
func ruleTextsFor(lang i18n.Lang) *ruleTexts {
	return i18n.Table(ruleTextsVI, ruleTextsZH, lang)
}
