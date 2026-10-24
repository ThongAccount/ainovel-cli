package chapterfacts

import "github.com/voocel/ainovel-cli/internal/i18n"

// factsTexts chứa mô tả schema và thông báo lỗi mà model đọc khi trích xuất
// sự thật chương.
//
// Lớp này không có store nên ngôn ngữ lấy từ i18n.Active(), tức ngôn ngữ tác
// phẩm do store thiết lập và đồng bộ vào biến toàn cục.
type factsTexts struct {
	storyTime         string
	event             string
	involvedCharacter string
	foreshadowID      string
	operation         string
	plantDescription  string
	characterA        string
	characterB        string
	relationAtEnd     string
	entity            string
	field             string
	oldValue          string
	newValue          string
	reason            string
	finalTitle        string
	chapterSummary    string
	presentCharacters string
	keyEvents         string
	timelineEvents    string
	foreshadowOps     string
	relationshipMoves string
	stateChanges      string
	newCast           string
	name              string
	briefRole         string
	hookType          string
	dominantStrand    string
	deviation         string
	suggestion        string
	feedbackObject    string

	// placeholderID báo rõ id manh mối là giá trị giữ chỗ — lỗi này đưa lại
	// cho model để tự sửa.
	placeholderID string
}

var factsTextsVI = &factsTexts{
	storyTime:         "Thời gian trong truyện",
	event:             "Sự kiện",
	involvedCharacter: "Các nhân vật liên quan",
	foreshadowID:      "ID manh mối",
	operation:         "Thao tác",
	plantDescription:  "Mô tả plant, các thao tác khác là null",
	characterA:        "Nhân vật A",
	characterB:        "Nhân vật B",
	relationAtEnd:     "Mối quan hệ khi kết thúc chương",
	entity:            "Thực thể",
	field:             "Thuộc tính",
	oldValue:          "Giá trị trước khi thay đổi",
	newValue:          "Giá trị sau khi thay đổi",
	reason:            "Lý do",
	finalTitle:        "Tiêu đề cuối cùng",
	chapterSummary:    "Tóm tắt chương",
	presentCharacters: "Các nhân vật xuất hiện",
	keyEvents:         "Sự kiện then chốt",
	timelineEvents:    "Sự kiện dòng thời gian",
	foreshadowOps:     "Thao tác manh mối",
	relationshipMoves: "Thay đổi quan hệ",
	stateChanges:      "Thay đổi trạng thái",
	newCast:           "Nhân vật phụ mới",
	name:              "Tên",
	briefRole:         "Vai trò",
	hookType:          "Móc cuối chương",
	dominantStrand:    "Tuyến kể chính",
	deviation:         "Mô tả lệch khỏi dàn ý",
	suggestion:        "Đề xuất điều chỉnh dàn ý về sau",
	feedbackObject:    "Đối tượng gợi ý cho dàn ý về sau; phải truyền trực tiếp JSON object, không truyền JSON đã chuyển thành chuỗi",

	placeholderID: "foreshadow_updates[%d].id = %q là giá trị giữ chỗ, không phải định danh. " +
		"Mỗi manh mối cần một id duy nhất và ổn định (ví dụ FORESHADOW_OBSERVER, jade-pendant), " +
		"các chương sau dựa vào nó để advance/resolve; " +
		"dùng lại giá trị giữ chỗ sẽ khiến manh mối mới đụng mục với mục cũ",
}

var factsTextsZH = &factsTexts{
	storyTime:         "故事内时间",
	event:             "事件",
	involvedCharacter: "涉及角色",
	foreshadowID:      "伏笔 ID",
	operation:         "操作",
	plantDescription:  "plant 描述，其它操作为 null",
	characterA:        "角色 A",
	characterB:        "角色 B",
	relationAtEnd:     "本章结束时关系",
	entity:            "实体",
	field:             "属性",
	oldValue:          "变化前值",
	newValue:          "变化后值",
	reason:            "原因",
	finalTitle:        "最终标题",
	chapterSummary:    "章节摘要",
	presentCharacters: "出场角色",
	keyEvents:         "关键事件",
	timelineEvents:    "时间线事件",
	foreshadowOps:     "伏笔操作",
	relationshipMoves: "关系变化",
	stateChanges:      "状态变化",
	newCast:           "新配角",
	name:              "姓名",
	briefRole:         "定位",
	hookType:          "章末钩子",
	dominantStrand:    "主导叙事线",
	deviation:         "偏离大纲的描述",
	suggestion:        "对后续大纲的调整建议",
	feedbackObject:    "对后续大纲的建议对象；必须直接传 JSON object，不要传字符串化 JSON",

	placeholderID: "foreshadow_updates[%d].id = %q 是占位符，不是标识符。每条伏笔要一个唯一且稳定的 id" +
		"（如 FORESHADOW_OBSERVER、jade-pendant），后续章节靠它 advance/resolve；" +
		"复用占位符会让新伏笔撞上旧条目",
}

// factsTextsFor chọn bảng văn bản theo ngôn ngữ tác phẩm.
func factsTextsFor(lang i18n.Lang) *factsTexts {
	return i18n.Table(factsTextsVI, factsTextsZH, lang)
}

// activeTexts trả bảng văn bản theo ngôn ngữ tác phẩm hiện hành.
func activeTexts() *factsTexts {
	return factsTextsFor(i18n.Active())
}
