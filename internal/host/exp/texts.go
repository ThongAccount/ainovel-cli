package exp

import "github.com/voocel/ainovel-cli/internal/i18n"

// expTexts là bộ văn bản bản xuất theo ngôn ngữ tác phẩm.
//
// Bản xuất phải khai báo đúng ngôn ngữ: một cuốn tiếng Việt mà vẫn ghi
// xml:lang="zh-CN" và "第 N 章" là bản xuất hỏng — trình đọc hiển thị sai
// ngữ, và người đọc thấy tiêu đề chương lạc lõng giữa bản Việt.
//
// Dùng đúng thuật ngữ đã thống nhất trong internal/store/labels.go:
// tập / cung / chương (xem docs/i18n-vi-glossary.md).
type expTexts struct {
	chapterFmt string // "Chương %d" / "第 %d 章"
	volumeFmt  string // "Tập %d" / "第 %d 卷"
	bookTitle  string // "《%s》" / "%s"
	cover      string // "Bìa" / "封面"
	toc        string // "Mục lục" / "目录"
	xmlLang    string // "vi" / "zh-CN"
}

var expTextsVI = expTexts{
	chapterFmt: "Chương %d",
	volumeFmt:  "Tập %d",
	bookTitle:  "%s",
	cover:      "Bìa",
	toc:        "Mục lục",
	xmlLang:    "vi",
}

var expTextsZH = expTexts{
	chapterFmt: "第 %d 章",
	volumeFmt:  "第 %d 卷",
	bookTitle:  "《%s》",
	cover:      "封面",
	toc:        "目录",
	xmlLang:    "zh-CN",
}

func expTextsFor(lang string) expTexts {
	return i18n.Table(expTextsVI, expTextsZH, i18n.Normalize(lang))
}
