package revision

import "github.com/voocel/ainovel-cli/internal/i18n"

// revisionTexts chứa toàn bộ văn bản mô hình đọc trong package revision.
//
// Mọi chuỗi ở đây đi vào prompt của mô hình (mô tả hợp đồng trả về có cấu trúc
// và mô tả từng thuộc tính JSON Schema), nên phải theo ngôn ngữ tác phẩm: mô hình
// đang viết tiếng Việt mà nhận mô tả tiếng Trung sẽ bị ngữ cảnh liên tục kéo về
// giọng Trung — cùng lý do mà nhãn Markdown của internal/store đã theo ngôn ngữ
// tác phẩm (xem internal/store/labels.go).
//
// Văn bản chỉ người dùng đọc (lỗi, dòng log) không nằm ở đây: bản port dùng tiếng
// Việt cố định cho giao diện vận hành.
type revisionTexts struct {
	// Mô tả hợp đồng phân tích sửa đổi chương.
	contractDesc string

	// Mô tả thuộc tính JSON Schema của gói sự thật chương do chính package này
	// định nghĩa (phần sự thật dùng chung lấy từ internal/chapterfacts).
	voiceName     string
	dialoguePrefs string
	deviation     string
	suggestion    string
	changeSummary string
	storyChanged  string
	prosePrefs    string
	voicePrefs    string
	taboos        string
	downstream    string
}

// revisionTextsVI là bản tiếng Việt — ngôn ngữ mặc định của bản port.
var revisionTextsVI = &revisionTexts{
	contractDesc: "Phân tích bản sửa đổi của người dùng đối với chương đã hoàn thành và dựng lại toàn bộ sự thật của chương",

	voiceName:     "Tên nhân vật",
	dialoguePrefs: "Sở thích đối thoại",
	deviation:     "Thay đổi của cốt truyện đã xảy ra so với kế hoạch hiện có",
	suggestion:    "Đề xuất điều chỉnh cho phần dàn ý chưa hoàn thành",
	changeSummary: "Tóm tắt thay đổi",
	storyChanged:  "Có thay đổi sự thật cốt truyện hay không",
	prosePrefs:    "Sở thích văn phong được xác nhận từ lần sửa đổi này",
	voicePrefs:    "Sở thích đối thoại của nhân vật",
	taboos:        "Điều cấm thể hiện qua các chỉnh sửa chủ động của người dùng",
	downstream:    "Xung đột tiềm ẩn với các chương đã hoàn thành về sau",
}

// revisionTextsZH là bản tiếng Trung — dùng khi tác phẩm viết bằng tiếng Trung.
var revisionTextsZH = &revisionTexts{
	contractDesc: "分析用户对已完成章节的修订并重建完整章节事实",

	voiceName:     "角色名",
	dialoguePrefs: "对白偏好",
	deviation:     "已发生剧情相对现有计划的变化",
	suggestion:    "对未完成大纲的调整建议",
	changeSummary: "修改概述",
	storyChanged:  "是否改变剧情事实",
	prosePrefs:    "从本次修改确认的叙述偏好",
	voicePrefs:    "角色对白偏好",
	taboos:        "用户主动删改所体现的禁忌",
	downstream:    "与后续已完成章节的潜在冲突",
}

// revisionTextsFor chọn bản văn bản theo ngôn ngữ tác phẩm.
func revisionTextsFor(lang i18n.Lang) *revisionTexts {
	return i18n.Table(revisionTextsVI, revisionTextsZH, lang)
}
