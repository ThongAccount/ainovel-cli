package imp

import "github.com/voocel/ainovel-cli/internal/i18n"

// importTexts chứa toàn bộ văn bản mô hình đọc trong gói imp: mô tả hợp đồng,
// mô tả thuộc tính JSON và các lệnh trong payload gửi cho mô hình.
//
// Mô hình nhận mô tả tiếng Trung khi đang viết tiếng Việt sẽ bị kéo lệch giọng
// và sinh nội dung lệch giọng, nên bảng này phải theo ngôn ngữ tác phẩm.
// internal/host có *store.Store nên nguồn ngôn ngữ là store.Language().
type importTexts struct {
	// Hợp đồng nhận diện ranh giới.
	segmentDesc    string
	boundariesDesc string
	unitIDDesc     string
	anchorDesc     string
	kindDesc       string
	titleDesc      string
	uncertainDesc  string
	reasonDesc     string

	// Hợp đồng trích xuất sự thật theo chương.
	analysisDesc          string
	chaptersDesc          string
	evidenceChapterDesc   string
	characterNameDesc     string
	characterNoteDesc     string
	worldCategoryDesc     string
	worldFactDesc         string
	chapterNoDesc         string
	storyTimeDesc         string
	eventDesc             string
	relatedCharactersDesc string
	foreshadowIDDesc      string
	foreshadowActionDesc  string
	plantDescriptionDesc  string
	charADesc             string
	charBDesc             string
	relationChangeDesc    string
	entityDesc            string
	changedFieldDesc      string
	oldValueDesc          string
	newValueDesc          string
	changeReasonDesc      string
	chapterTitleDesc      string
	summaryDesc           string
	keyEventsDesc         string
	coreEventDesc         string
	hookDesc              string
	scenesDesc            string
	castDesc              string
	characterEvidenceDesc string
	worldEvidenceDesc     string
	timelineEventsDesc    string
	foreshadowUpdatesDesc string
	stateChangesDesc      string
	hookTypeDesc          string
	dominantStrandDesc    string

	// Hợp đồng tóm tắt một đoạn chương liên tục.
	rangeDesc         string
	rangeStartDesc    string
	rangeEndDesc      string
	rangePlotDesc     string
	rangeCharsDesc    string
	rangeWorldDesc    string
	rangeOpenedDesc   string
	rangeResolvedDesc string

	// Hợp đồng tổng hợp toàn tác phẩm.
	synthesisDesc       string
	bookTitleDesc       string
	synopsisDesc        string
	premiseDesc         string
	mainCharactersDesc  string
	aliasesDesc         string
	narrativeRoleDesc   string
	characterDescDesc   string
	characterArcDesc    string
	characterTraitsDesc string
	characterTierDesc   string
	worldRulesDesc      string
	ruleCategoryDesc    string
	ruleDesc            string
	boundaryDesc        string
	structureDesc       string
	volumeTitleDesc     string
	volumeThemeDesc     string
	volumeArcsDesc      string
	arcTitleDesc        string
	arcGoalDesc         string
	arcStartDesc        string
	arcEndDesc          string
	endingDirectionDesc string
	openThreadsDesc     string
	estimatedScaleDesc  string
	lastUpdatedDesc     string
	planningTierDesc    string
	storyStatusDesc     string
	statusReasonDesc    string

	// Nhãn ledger và lệnh trong payload gửi mô hình.
	ledgerKnownCharsDesc   string
	ledgerActiveForeshadow string
	ledgerRecentStateDesc  string
	ledgerListSep          string
	ledgerItemSep          string
	ledgerForeshadowSep    string
	analyzeInstruction     string
	analyzeLedgerHeader    string
	analyzeChapterHeader   string
	rangeInstruction       string
	reduceInstruction      string
	bookInstruction        string
}

// importTextsVI là bản tiếng Việt — ngôn ngữ mặc định của bản port.
var importTextsVI = &importTexts{
	segmentDesc:    "nhận diện ranh giới chương, tập/phần và vùng văn bản phụ trong văn bản nhập",
	boundariesDesc: "ranh giới theo thứ tự văn bản gốc",
	unitIDDesc:     "unit id trong khoảng owned",
	anchorDesc:     "đoạn định vị trong văn bản gốc khi một unit có nhiều ranh giới; null nếu không",
	kindDesc:       "loại ranh giới",
	titleDesc:      "tiêu đề gốc; null nếu không có tiêu đề",
	uncertainDesc:  "có cần người dùng xác nhận không",
	reasonDesc:     "lý do không chắc chắn; null nếu không cần giải thích",

	analysisDesc:          "trích xuất sự thật truyện có thể truy vết từ các chương liên tục",
	chaptersDesc:          "sự thật theo từng chương, cùng thứ tự với số chương đầu vào",
	evidenceChapterDesc:   "chương chứng cứ nằm ở",
	characterNameDesc:     "tên nhân vật",
	characterNoteDesc:     "sự thật về nhân vật; null nếu không có",
	worldCategoryDesc:     "loại sự thật thế giới; null nếu không phân loại được",
	worldFactDesc:         "sự thật thế giới được nội dung truyện hé lộ rõ",
	chapterNoDesc:         "số chương",
	storyTimeDesc:         "thời gian trong truyện",
	eventDesc:             "sự kiện",
	relatedCharactersDesc: "nhân vật liên quan",
	foreshadowIDDesc:      "ID manh mối tái dùng từ ledger",
	foreshadowActionDesc:  "hành động manh mối",
	plantDescriptionDesc:  "mô tả manh mối lúc plant; null ở trường hợp khác",
	charADesc:             "nhân vật A",
	charBDesc:             "nhân vật B",
	relationChangeDesc:    "thay đổi quan hệ",
	entityDesc:            "nhân vật hoặc thực thể",
	changedFieldDesc:      "thuộc tính đã thay đổi",
	oldValueDesc:          "trạng thái trước thay đổi; null ở lần xuất hiện đầu",
	newValueDesc:          "trạng thái sau thay đổi",
	changeReasonDesc:      "lý do thay đổi; null nếu nội dung truyện không nêu",
	chapterTitleDesc:      "tiêu đề chương",
	summaryDesc:           "tóm tắt chương",
	keyEventsDesc:         "Sự kiện quan trọng",
	coreEventDesc:         "việc quan trọng nhất của chương",
	hookDesc:              "điểm móc cuối chương; null nếu không có",
	scenesDesc:            "chuỗi cảnh",
	castDesc:              "nhân vật xuất hiện",
	characterEvidenceDesc: "bằng chứng về nhân vật",
	worldEvidenceDesc:     "bằng chứng sự thật thế giới",
	timelineEventsDesc:    "sự kiện dòng thời gian",
	foreshadowUpdatesDesc: "cập nhật manh mối",
	stateChangesDesc:      "thay đổi trạng thái",
	hookTypeDesc:          "loại điểm móc cuối chương",
	dominantStrandDesc:    "dòng tường thuật chính",

	rangeDesc:         "tổng hợp cốt truyện và sự thật của một đoạn chương liên tục",
	rangeStartDesc:    "chương đầu đoạn",
	rangeEndDesc:      "chương cuối đoạn",
	rangePlotDesc:     "diễn tiến cốt truyện chính xuyên nhiều chương",
	rangeCharsDesc:    "nhân vật có tiến triển thực chất",
	rangeWorldDesc:    "sự thật thế giới đã xác lập",
	rangeOpenedDesc:   "mạch dài mở mới trong đoạn này",
	rangeResolvedDesc: "mạch dài khép lại trong đoạn này",

	synthesisDesc:       "tổng hợp sự thật toàn tác phẩm và đưa ra phạm vi tập/cung liên tục hoàn chỉnh",
	bookTitleDesc:       "tên sách chính thức trong nội dung truyện; null nếu không xác nhận được",
	synopsisDesc:        "giới thiệu tiểu thuyết không tiết lộ nội dung dành cho người đọc",
	premiseDesc:         "mô tả tiền đề truyện dạng Markdown",
	mainCharactersDesc:  "nhân vật chính",
	aliasesDesc:         "bí danh và danh xưng",
	narrativeRoleDesc:   "vai trò tường thuật",
	characterDescDesc:   "mô tả nhân vật",
	characterArcDesc:    "cung nhân vật",
	characterTraitsDesc: "đặc điểm nhân vật",
	characterTierDesc:   "tầng nhân vật; null nếu không phân định được",
	worldRulesDesc:      "luật thế giới do nội dung truyện xác lập",
	ruleCategoryDesc:    "loại luật",
	ruleDesc:            "mô tả luật",
	boundaryDesc:        "ranh giới không thể vi phạm",
	structureDesc:       "phạm vi chương liên tục của tập và cung",
	volumeTitleDesc:     "tiêu đề tập",
	volumeThemeDesc:     "Xung đột cốt lõi hoặc chủ đề của tập",
	volumeArcsDesc:      "cung truyện trong tập",
	arcTitleDesc:        "tiêu đề cung",
	arcGoalDesc:         "mục tiêu cung",
	arcStartDesc:        "chương bắt đầu",
	arcEndDesc:          "chương kết thúc",
	endingDirectionDesc: "Hướng kết cục",
	openThreadsDesc:     "mạch dài chưa khép lại",
	estimatedScaleDesc:  "quy mô ước chừng; null nếu không đoán được",
	lastUpdatedDesc:     "số chương mới nhất làm căn cứ; null nếu không cần điền",
	planningTierDesc:    "tầng quy hoạch",
	storyStatusDesc:     "truyện đã kết thúc chưa",
	statusReasonDesc:    "lý do đánh giá trạng thái",

	ledgerKnownCharsDesc:   "Nhân vật đã biết: ",
	ledgerActiveForeshadow: "Manh mối đang hoạt động (tái dùng ID, không tự tạo mới):\n",
	ledgerRecentStateDesc:  "Trạng thái gần nhất: ",
	ledgerListSep:          ", ",
	ledgerItemSep:          "; ",
	ledgerForeshadowSep:    ": ",
	analyzeInstruction:     "Hãy phân tích chương %d-%d, trả về {\"chapters\":[một đối tượng sự thật cho mỗi chương]}, thứ tự mảng khớp với số chương.\n\n",
	analyzeLedgerHeader:    "## Ledger tính liên tục (tham chiếu)\n\n",
	analyzeChapterHeader:   "## Chương %d: %s\n\n",
	rangeInstruction:       "Hãy tạo một RangeDigest (tóm tắt đoạn liên tục) cho chương %d-%d. Sự thật theo từng chương:\n%s",
	reduceInstruction:      "Hãy gộp nhiều tóm tắt đoạn cấp dưới của chương %d-%d thành một RangeDigest (tóm tắt đoạn liên tục). Các tóm tắt cấp dưới:\n%s",
	bookInstruction:        "Dưới đây là sự thật/tóm tắt đoạn rút gọn của toàn bộ %d chương. Hãy tạo BookSynthesis: title, synopsis, premise, characters, world_rules, structure (phạm vi tập/cung), compass, planning_tier, story_status.\n\n%s",
}

// importTextsZH là bản tiếng Trung — giữ nguyên văn bản gốc của dự án.
var importTextsZH = &importTexts{
	segmentDesc:    "识别导入文本中的章节、卷篇与附属文本边界",
	boundariesDesc: "按原文顺序排列的边界",
	unitIDDesc:     "owned 区间内的 unit id",
	anchorDesc:     "同一 unit 多边界时的原文定位片段；否则为 null",
	kindDesc:       "边界类型",
	titleDesc:      "标题原文；没有标题时为 null",
	uncertainDesc:  "是否需要用户确认",
	reasonDesc:     "不确定原因；无需说明时为 null",

	analysisDesc:          "提取连续章节的可追溯故事事实",
	chaptersDesc:          "与输入章号顺序一致的逐章事实",
	evidenceChapterDesc:   "证据所在章",
	characterNameDesc:     "人物名",
	characterNoteDesc:     "人物事实；无则为 null",
	worldCategoryDesc:     "世界事实类别；无法归类时为 null",
	worldFactDesc:         "正文明确揭示的世界事实",
	chapterNoDesc:         "章号",
	storyTimeDesc:         "故事内时间",
	eventDesc:             "事件",
	relatedCharactersDesc: "相关人物",
	foreshadowIDDesc:      "复用 ledger 中的伏笔 ID",
	foreshadowActionDesc:  "伏笔动作",
	plantDescriptionDesc:  "plant 时的伏笔说明；其他情况可为 null",
	charADesc:             "人物 A",
	charBDesc:             "人物 B",
	relationChangeDesc:    "关系变化",
	entityDesc:            "角色或实体",
	changedFieldDesc:      "发生变化的属性",
	oldValueDesc:          "变化前状态；首次出现时为 null",
	newValueDesc:          "变化后状态",
	changeReasonDesc:      "变化原因；正文未说明时为 null",
	chapterTitleDesc:      "章节标题",
	summaryDesc:           "本章概要",
	keyEventsDesc:         "关键事件",
	coreEventDesc:         "本章最关键的一件事",
	hookDesc:              "章末钩子；无则为 null",
	scenesDesc:            "场景序列",
	castDesc:              "出场人物",
	characterEvidenceDesc: "人物证据",
	worldEvidenceDesc:     "世界事实证据",
	timelineEventsDesc:    "时间线事件",
	foreshadowUpdatesDesc: "伏笔增量",
	stateChangesDesc:      "状态变化",
	hookTypeDesc:          "章末钩子类型",
	dominantStrandDesc:    "主导叙事线",

	rangeDesc:         "归纳一个连续章节区间的剧情与事实",
	rangeStartDesc:    "区间首章",
	rangeEndDesc:      "区间末章",
	rangePlotDesc:     "跨章主线剧情推进",
	rangeCharsDesc:    "有实质进展的人物",
	rangeWorldDesc:    "已确立的世界事实",
	rangeOpenedDesc:   "本区间新开的长线",
	rangeResolvedDesc: "本区间收束的长线",

	synthesisDesc:       "综合全书事实并给出连续完整的卷弧范围",
	bookTitleDesc:       "正文中的正式书名；无法确认时为 null",
	synopsisDesc:        "面向读者的无剧透小说简介",
	premiseDesc:         "故事前提的 Markdown 描述",
	mainCharactersDesc:  "主要人物",
	aliasesDesc:         "别名与称号",
	narrativeRoleDesc:   "叙事角色",
	characterDescDesc:   "人物描述",
	characterArcDesc:    "人物弧",
	characterTraitsDesc: "人物特质",
	characterTierDesc:   "人物层级；无法判断时为 null",
	worldRulesDesc:      "正文确立的世界规则",
	ruleCategoryDesc:    "规则类别",
	ruleDesc:            "规则描述",
	boundaryDesc:        "不可违反的边界",
	structureDesc:       "卷与弧的连续章节范围",
	volumeTitleDesc:     "卷标题",
	volumeThemeDesc:     "卷核心冲突或主题",
	volumeArcsDesc:      "卷内故事弧",
	arcTitleDesc:        "弧标题",
	arcGoalDesc:         "弧目标",
	arcStartDesc:        "起始章",
	arcEndDesc:          "结束章",
	endingDirectionDesc: "终局方向",
	openThreadsDesc:     "仍未收束的长线",
	estimatedScaleDesc:  "模糊规模；无法判断时为 null",
	lastUpdatedDesc:     "依据的最新章号；无需填写时为 null",
	planningTierDesc:    "规划层级",
	storyStatusDesc:     "故事是否完结",
	statusReasonDesc:    "状态判断理由",

	ledgerKnownCharsDesc:   "已知人物：",
	ledgerActiveForeshadow: "活跃伏笔（复用 ID，勿新造）：\n",
	ledgerRecentStateDesc:  "最近状态：",
	ledgerListSep:          "、",
	ledgerItemSep:          "；",
	ledgerForeshadowSep:    "：",
	analyzeInstruction:     "请分析第 %d-%d 章，返回 {\"chapters\":[每章一个事实对象]}，数组顺序与章号一致。\n\n",
	analyzeLedgerHeader:    "## 连续性 ledger（参考）\n\n",
	analyzeChapterHeader:   "## 第 %d 章：%s\n\n",
	rangeInstruction:       "请为第 %d-%d 章生成一个 RangeDigest（连续区间摘要）。逐章事实：\n%s",
	reduceInstruction:      "请把第 %d-%d 章的多个下层区间摘要合并为一个 RangeDigest（连续区间摘要）。下层摘要：\n%s",
	bookInstruction:        "以下是全书 %d 章的紧凑事实/区间摘要。请生成 BookSynthesis：title、synopsis、premise、characters、world_rules、卷弧范围 structure、compass、planning_tier、story_status。\n\n%s",
}

// importTextsFor chọn bảng văn bản theo ngôn ngữ tác phẩm.
func importTextsFor(lang string) *importTexts {
	return i18n.Table(importTextsVI, importTextsZH, i18n.Normalize(lang))
}

// importTextsOrDefault trả về bảng văn bản đã chọn; nil thì mặc định tiếng Việt.
// Dùng ở các đường gọi không có store (ví dụ test gọi trực tiếp hàm cấp thấp).
func importTextsOrDefault(t *importTexts) *importTexts {
	if t != nil {
		return t
	}
	return importTextsVI
}
