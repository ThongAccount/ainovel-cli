package sim

import "github.com/voocel/ainovel-cli/internal/i18n"

type simStyleTexts struct {
	narrativeVoice string
	sentenceRhythm string
	proseTexture   string
	perspective    string
	mood           string
	doNotCopy      string
}

type simLexiconTexts struct {
	commonWords      string
	emotionWords     string
	sceneWords       string
	transitionWords  string
	signaturePhrases string
}

type simPlotTexts struct {
	openingPatterns      string
	escalationPatterns   string
	turningPointPatterns string
	payoffPatterns       string
}

type simHookTexts struct {
	hookTypes           string
	placement           string
	cliffhangerPatterns string
	payoffRules         string
}

type simPacingTexts struct {
	sceneDensity        string
	informationRelease  string
	dialogueActionRatio string
	compressionRules    string
}

type simEngagementTexts struct {
	methods            string
	emotionalDrivers   string
	progressionRewards string
	antiPatterns       string
}

type simRoleTexts struct {
	architect string
	writer    string
	editor    string
}

// simTexts chứa toàn bộ văn bản mô hình đọc trong package sim.
//
// Hợp đồng JSON và dấu hiệu cắt ngắn trong payload đi vào ngữ cảnh gửi mô hình,
// nên phải theo ngôn ngữ tác phẩm. Mô hình đang viết tiếng Việt mà nhận mô tả
// tiếng Trung sẽ bị ngữ cảnh kéo sang giọng Trung, giống lý do các nhãn Markdown
// trong internal/store đã theo ngôn ngữ tác phẩm.
//
// Văn bản chỉ người dùng đọc (thông báo tiến trình, log, lỗi) không nằm ở đây:
// bản port dùng tiếng Việt cố định cho giao diện vận hành.
type simTexts struct {
	sourceReportDescription string
	sourceReportTitle       string
	sourceReportSummary     string
	sourceReportStyle       string
	sourceReportCommonWords string
	sourceReportPlot        string
	sourceReportHooks       string
	sourceReportPacing      string
	sourceReportAppeal      string
	sourceReportTechniques  string
	sourceReportWarnings    string

	synthesisDescription string
	style                simStyleTexts
	lexicon              simLexiconTexts
	plot                 simPlotTexts
	hooks                simHookTexts
	pacing               simPacingTexts
	engagement           simEngagementTexts
	roles                simRoleTexts

	truncatedMarker string
}

var simTextsVI = &simTexts{
	sourceReportDescription: "Tổng hợp các phương pháp viết có thể tái sử dụng từ một văn bản mẫu mà không sao chép nội dung gốc",
	sourceReportTitle:       "Tiêu đề tùy chọn; dùng null nếu không xác định được",
	sourceReportSummary:     "Tóm tắt giá trị về cách viết của văn bản mẫu",
	sourceReportStyle:       "Quan sát về góc kể, cấu trúc câu và chất liễu tả",
	sourceReportCommonWords: "Các từ thường dùng, hình ảnh và nhóm từ chuyển cảnh",
	sourceReportPlot:        "Các mô hình thúc đẩy tình tiết, bước ngoặt và leo thang xung đột",
	sourceReportHooks:       "Các mô hình móc câu ở đầu, cuối chương và thông tin bất đối xứng",
	sourceReportPacing:      "Mật độ cảnh và nhịp giải phóng thông tin",
	sourceReportAppeal:      "Cách thu hút người đọc tiếp tục đọc",
	sourceReportTechniques:  "Kỹ thuật có cấu trúc có thể học hỏi",
	sourceReportWarnings:    "Rủi ro sao chép và áp dụng máy móc phải tránh",

	synthesisDescription: "Tổng hợp hồ sơ hiện có và báo cáo ngữ liệu thành hồ sơ phương pháp lấy cảm hứng có thể thực thi",
	style: simStyleTexts{
		narrativeVoice: "Người kể, khoảng cách và kiểm soát thông tin",
		sentenceRhythm: "Nhịp điệu câu",
		proseTexture:   "Chất liễu tả",
		perspective:    "Quy tắc góc nhìn",
		mood:           "Tinh thần và giọng điệu",
		doNotCopy:      "Nội dung không được sao chép",
	},
	lexicon: simLexiconTexts{
		commonWords:      "Nhóm từ thường dùng",
		emotionWords:     "Nhóm từ cảm xúc",
		sceneWords:       "Nhóm từ miêu tả cảnh",
		transitionWords:  "Nhóm từ chuyển cảnh",
		signaturePhrases: "Đặc trưng giọng văn sau trừu xuất, không chứa câu gốc",
	},
	plot: simPlotTexts{
		openingPatterns:      "Cách mở đầu",
		escalationPatterns:   "Cách leo thang xung đột",
		turningPointPatterns: "Thiết kế bước ngoặt",
		payoffPatterns:       "Cách thu hồi và hiện thực hóa",
	},
	hooks: simHookTexts{
		hookTypes:           "Loại móc câu",
		placement:           "Vị trí móc câu",
		cliffhangerPatterns: "Cách dừng lại tạo tò mò",
		payoffRules:         "Quy tắc hiện thực hóa móc câu",
	},
	pacing: simPacingTexts{
		sceneDensity:        "Mật độ thông tin trong một cảnh",
		informationRelease:  "Nhịp giải phóng thông tin",
		dialogueActionRatio: "Tỷ lệ đối thoại, hành động và nội tâm",
		compressionRules:    "Quy tắc triển khai và nén nội dung",
	},
	engagement: simEngagementTexts{
		methods:            "Cách thu hút người đọc",
		emotionalDrivers:   "Động lực cảm xúc",
		progressionRewards: "Phần thưởng cho tiến bộ theo từng giai đoạn",
		antiPatterns:       "Mẫu hình phản tác dụng làm giảm sức hút",
	},
	roles: simRoleTexts{
		architect: "Quy tắc Kiến trúc sư dùng hồ sơ",
		writer:    "Quy tắc Người viết học hỏi nhưng không sao chép",
		editor:    "Quy tắc Biên tập viên kiểm tra hướng và rủi ro sao chép",
	},

	truncatedMarker: "[...nội dung bị cắt ngắn...]",
}

var simTextsZH = &simTexts{
	sourceReportDescription: "从单篇语料提炼可复用且不复制原文的写作方法",
	sourceReportTitle:       "可选标题；无法确认时为 null",
	sourceReportSummary:     "样本文本的写法价值概括",
	sourceReportStyle:       "叙述视角、句式与描写纹理观察",
	sourceReportCommonWords: "高频词、意象与转场词类别",
	sourceReportPlot:        "情节推进、转折与冲突升级模式",
	sourceReportHooks:       "开篇、章末与信息差钩子模式",
	sourceReportPacing:      "场景密度与信息释放节奏",
	sourceReportAppeal:      "吸引读者继续阅读的方法",
	sourceReportTechniques:  "可借鉴的结构性技巧",
	sourceReportWarnings:    "必须避免的复制与套用风险",

	synthesisDescription: "把既有画像和语料报告合成为可执行的仿写方法画像",
	style: simStyleTexts{
		narrativeVoice: "叙述人称、距离与信息控制",
		sentenceRhythm: "句式节奏",
		proseTexture:   "描写质感",
		perspective:    "视角规则",
		mood:           "情绪调性",
		doNotCopy:      "禁止复制的内容",
	},
	lexicon: simLexiconTexts{
		commonWords:      "常用词类别",
		emotionWords:     "情绪词类别",
		sceneWords:       "场景词类别",
		transitionWords:  "转场词类别",
		signaturePhrases: "抽象后的口吻特征，不含原句",
	},
	plot: simPlotTexts{
		openingPatterns:      "开局方式",
		escalationPatterns:   "冲突升级方式",
		turningPointPatterns: "转折设计",
		payoffPatterns:       "回收与兑现方式",
	},
	hooks: simHookTexts{
		hookTypes:           "钩子类型",
		placement:           "钩子位置",
		cliffhangerPatterns: "悬念停顿方式",
		payoffRules:         "钩子兑现规则",
	},
	pacing: simPacingTexts{
		sceneDensity:        "单场景信息密度",
		informationRelease:  "信息释放节奏",
		dialogueActionRatio: "对白、动作与心理比例",
		compressionRules:    "内容展开与压缩规则",
	},
	engagement: simEngagementTexts{
		methods:            "吸引读者的方法",
		emotionalDrivers:   "情绪驱动力",
		progressionRewards: "阶段性进展奖励",
		antiPatterns:       "削弱吸引力的反模式",
	},
	roles: simRoleTexts{
		architect: "Architect 使用画像的规则",
		writer:    "Writer 借鉴但不复制的规则",
		editor:    "Editor 检查方向与侵权风险的规则",
	},

	truncatedMarker: "[...已截断...]",
}

func simTextsFor(lang string) *simTexts {
	return i18n.Table(simTextsVI, simTextsZH, i18n.Normalize(lang))
}
