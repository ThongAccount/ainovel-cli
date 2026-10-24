package agents

import "github.com/voocel/ainovel-cli/internal/i18n"

// workerTexts chứa mô tả của các subagent. Mô tả này được ghép thành mô tả
// công cụ mà model đọc khi spawn agent, nên phải theo ngôn ngữ tác phẩm.
//
// Nguồn ngôn ngữ ở đây là bundle.Language (BuildWorkers nhận sẵn bundle),
// không phải biến toàn cục.
type workerTexts struct {
	architectShortDesc string
	architectLongDesc  string
	writerDesc         string
	editorDesc         string
}

var workerTextsVI = &workerTexts{
	architectShortDesc: "Kiến trúc sư truyện ngắn: dựng thiết lập cô đọng và dàn ý phẳng cho truyện một tập, một xung đột, mật độ cao",
	architectLongDesc:  "Kiến trúc sư truyện dài: dựng thiết lập phân tầng và dàn ý tập–cung cho truyện nối tiếp, nâng cấp bền vững",
	writerDesc:         "Người viết: tự hoàn thành việc lên ý, viết, tự soát và gửi một chương",
	editorDesc:         "Biên tập viên: đọc nguyên văn, phát hiện vấn đề ở cả tầng cấu trúc lẫn tầng thẩm mỹ",
}

var workerTextsZH = &workerTexts{
	architectShortDesc: "短篇规划师：为单卷、单冲突、高密度故事生成紧凑设定与扁平大纲",
	architectLongDesc:  "长篇规划师：为连载型、可持续升级的故事生成分层设定与卷弧大纲",
	writerDesc:         "创作者：自主完成一章的构思、写作、自审和提交",
	editorDesc:         "审阅者：阅读原文，从结构和审美两个层面发现问题",
}

// workerTextsFor chọn bảng mô tả theo ngôn ngữ tác phẩm.
func workerTextsFor(lang i18n.Lang) *workerTexts {
	return i18n.Table(workerTextsVI, workerTextsZH, lang)
}
