package host

import "github.com/voocel/ainovel-cli/internal/i18n"

// observerTexts chứa các nhãn hiển thị của cụm quan sát (observer) cần theo ngôn
// ngữ tác phẩm.
//
// Đây là những nhãn gọi tên bước trong quy trình (quy hoạch, chốt chương, thẩm
// duyệt...) và các đối tượng của tác phẩm (chương, cung, bản nháp). Chúng đi cùng
// với nội dung mà mô hình sinh ra và với tên công cụ mà mô hình gọi, nên phải
// dùng đúng từ vựng mà mô hình nhìn thấy — nếu không, bảng theo dõi sẽ lệch ngôn
// ngữ với nội dung truyện. Cùng lý do đó, nhãn chương ở đây dùng đúng dạng
// "Chương %d" của internal/store/labels.go.
//
// Văn bản chỉ người dùng đọc (lỗi, dòng log, thông báo thử lại) không nằm ở đây:
// bản port dùng tiếng Việt cố định cho giao diện vận hành.
type observerTexts struct {
	// Nhãn tiêu đề của dòng công cụ trên bảng luồng.
	toolPlan          string
	toolEdit          string
	toolCommit        string
	toolReview        string
	toolArcSummary    string
	toolVolumeSummary string
	toolFoundation    string
	toolReviseOutline string
	toolReadChapter   string
	toolConsistency   string
	toolContextQuery  string

	// Nhãn tham số hiển thị trên dòng công cụ.
	toolArgChapter      string
	toolArgChapterExtra string
	chapterLabel        string
	arcScope            string
	globalScope         string
	dialogSuffix        string
	draftSuffix         string
}

// observerTextsVI là bản tiếng Việt — ngôn ngữ mặc định của bản port.
var observerTextsVI = &observerTexts{
	toolPlan:          "✻ Quy hoạch",
	toolEdit:          "✻ Hoàn thiện",
	toolCommit:        "✻ Chốt chương",
	toolReview:        "✻ Thẩm duyệt",
	toolArcSummary:    "✻ Tóm tắt cung",
	toolVolumeSummary: "✻ Tóm tắt tập",
	toolFoundation:    "✻ Thiết lập",
	toolReviseOutline: "✻ Sửa dàn ý",
	toolReadChapter:   "✻ Đọc chương",
	toolConsistency:   "✻ Kiểm tra tính nhất quán",
	toolContextQuery:  "✻ Truy vấn ngữ cảnh",

	toolArgChapter:      "%s(Chương %d)",
	toolArgChapterExtra: "%s(Chương %d%s)",
	chapterLabel:        "Chương %d",
	arcScope:            "Cung này",
	globalScope:         "Toàn cục",
	dialogSuffix:        "·Đối thoại với %s",
	draftSuffix:         "·Bản nháp",
}

// observerTextsZH là bản tiếng Trung — giữ nguyên văn bản gốc của dự án.
var observerTextsZH = &observerTexts{
	toolPlan:          "✻ 规划",
	toolEdit:          "✻ 打磨",
	toolCommit:        "✻ 章节提交",
	toolReview:        "✻ 审阅",
	toolArcSummary:    "✻ 弧摘要",
	toolVolumeSummary: "✻ 卷摘要",
	toolFoundation:    "✻ 设定",
	toolReviseOutline: "✻ 修订大纲",
	toolReadChapter:   "✻ 读章节",
	toolConsistency:   "✻ 一致性检查",
	toolContextQuery:  "✻ 查询上下文",

	toolArgChapter:      "%s(第%d章)",
	toolArgChapterExtra: "%s(第%d章%s)",
	chapterLabel:        "第%d章",
	arcScope:            "本弧",
	globalScope:         "全局",
	dialogSuffix:        "·%s对话",
	draftSuffix:         "·草稿",
}

// observerTextsFor chọn bảng văn bản theo ngôn ngữ tác phẩm.
func observerTextsFor(lang string) *observerTexts {
	return i18n.Table(observerTextsVI, observerTextsZH, i18n.Normalize(lang))
}

// toolHeader trả về nhãn tiêu đề của một công cụ; rỗng nếu công cụ không có nhãn
// riêng (ví dụ draft_chapter dùng chế độ luồng trần, không có tiêu đề).
func (t *observerTexts) toolHeader(tool string) string {
	switch tool {
	case "plan_chapter":
		return t.toolPlan
	case "edit_chapter":
		return t.toolEdit
	case "commit_chapter":
		return t.toolCommit
	case "save_review":
		return t.toolReview
	case "save_arc_summary":
		return t.toolArcSummary
	case "save_volume_summary":
		return t.toolVolumeSummary
	case "save_foundation":
		return t.toolFoundation
	case "revise_outline":
		return t.toolReviseOutline
	case "read_chapter":
		return t.toolReadChapter
	case "check_consistency":
		return t.toolConsistency
	case "novel_context":
		return t.toolContextQuery
	}
	return ""
}
