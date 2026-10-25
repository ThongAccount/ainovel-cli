package tools

import "github.com/voocel/ainovel-cli/internal/i18n"

// toolTexts chứa toàn bộ văn bản mô hình đọc trong internal/tools (nửa "sách",
// phần A1): novel_context, save_foundation, save_review, save_arc_summary,
// save_volume_summary, save_book.
//
// Mô tả công cụ đi thẳng vào ngữ cảnh LLM. Nếu mô hình đang viết tiếng Việt
// mà mô tả vẫn bằng tiếng Trung, ngữ cảnh sẽ liên tục kéo mô hình trôi về
// Trung Quốc — cùng lý do mà nhãn Markdown trong internal/store đã theo ngôn
// ngữ tác phẩm (xem internal/store/labels.go).
//
// Bảng này thuộc phần A1 (nửa "sách") của track A; phần A2 dùng bảng riêng
// trong internal/tools/texts_write.go (struct writeTexts). Hai bảng độc lập
// để các nhánh chạy song song không đụng vào nhau.
type toolTexts struct {
	// Nhóm novel_context.
	novelContextDesc       string
	novelContextLabel      string
	novelContextArgChapter string

	// Nhóm save_foundation.
	saveFoundationDesc       string
	saveFoundationLabel      string
	saveFoundationArgType    string
	saveFoundationArgContent string
	saveFoundationArgScale   string
	saveFoundationArgVolume  string
	saveFoundationArgArc     string
	saveFoundationArgReason  string

	// Nhóm save_review.
	saveReviewDesc  string
	saveReviewLabel string

	// Schema mô tả bài đánh giá (schema descriptions — [L], đi vào ngữ cảnh LLM).
	saveReviewIssueType        string
	saveReviewIssueSeverity    string
	saveReviewIssueDescription string
	saveReviewIssueEvidence    string
	saveReviewIssueSuggestion  string
	saveReviewIssueChapters    string
	saveReviewIssueRequiresChg string
	saveReviewDimension        string
	saveReviewDimensionScore   string
	saveReviewDimensionComment string
	saveReviewChapter          string
	saveReviewScope            string
	saveReviewDimensions       string
	saveReviewIssues           string
	saveReviewContractStatus   string
	saveReviewContractMisses   string
	saveReviewContractNotes    string
	saveReviewVerdict          string
	saveReviewSummary          string

	// Nhóm save_arc_summary.
	saveArcSummaryDesc  string
	saveArcSummaryLabel string

	// Schema mô tả tóm tắt cung.
	saveArcSnapshotName      string
	saveArcSnapshotStatus    string
	saveArcSnapshotPower     string
	saveArcSnapshotMotiv     string
	saveArcSnapshotRelations string
	saveArcVoiceName         string
	saveArcVoiceRules        string
	saveArcProse             string
	saveArcDialogue          string
	saveArcTaboos            string
	saveArcVolume            string
	saveArcArc               string
	saveArcTitle             string
	saveArcSummaryField      string
	saveArcKeyEvents         string
	saveArcCharacterSnaps    string
	saveArcStyleRules        string

	// Nhóm save_volume_summary.
	saveVolumeSummaryDesc  string
	saveVolumeSummaryLabel string

	// Schema mô tả tóm tắt tập.
	saveVolVolume  string
	saveVolTitle   string
	saveVolSummary string
	saveVolKeyEvts string

	// Nhóm save_book.
	saveBookDesc  string
	saveBookLabel string

	// Schema mô tả thông tin tác phẩm.
	saveBookTitle    string
	saveBookSynopsis string
}

var toolTextsVI = &toolTexts{
	novelContextDesc:       "Lấy trạng thái hiện tại và ngữ cảnh sáng tác của tác phẩm. Không truyền chapter: trả về progress_status (phase/flow/next_chapter/pending_rewrites và các trường tiến độ) + cơ sở thiết lập, dùng để biết bước tiếp theo nên làm gì. Truyền chapter=N: thêm trả về tóm tắt bối cảnh chương trước, phục bút, trạng thái nhân vật, quy tắc phong cách và ngữ cảnh sáng tác",
	novelContextLabel:      "Tải ngữ cảnh",
	novelContextArgChapter: "Số chương. Không truyền thì trả về trạng thái tiến độ và cơ sở thiết lập (Kiến trúc sư dùng); truyền vào thì thêm trả về ngữ cảnh sáng tác của chương đó (Người viết/Biên tập viên dùng)",

	saveFoundationDesc:       "Lưu cơ sở thiết lập tiểu thuyết (premise/outline/characters/world_rules/compass và các thiết lập khác). Đây là duy nhất là điểm bền hóa: nội dung chưa được lưu qua công cụ này chỉ xuất ra Markdown/JSON trong tin nhắn là đã mất. Tham số cố định là {type, content, scale?, volume?, arc?}. type có thể premise / outline / layered_outline / characters / world_rules / expand_arc / append_volume / update_compass / complete_book. premise thì content phải là chuỗi Markdown; các kiểu khác ưu tiên truyền trực tiếp mảng hoặc đối tượng JSON. expand_arc điều chỉnh và mở rộng một cung xương chưa viết (chi tiết chương đơn cung không quá 8 chương — đánh giá cuối cung cần đọc hết cung, quá dài không đánh giá được; vượt giới hạn hãy tách thành nhiều cung, cần volume + arc, content là {title, goal, chapters}, có thể điều chỉnh mục tiêu cung gốc dựa trên nội dung đã viết); append_volume thêm tập mới (content là JSON VolumeOutline đầy đủ, chứa cấu trúc cung; cấp cao nhất mang \"final\": true là tuyên bố tập thu thanh — toàn bộ truyện kết thúc ở tập đó, mọi chương viết xong sau đó tự động hoàn thành, không cần gọi complete_book); update_compass cập nhật hướng kết cục (content là JSON StoryCompass, trường chỉ {ending_direction: string bắt buộc, open_threads?: string[], estimated_scale?: string}, các trường khác đều không nhận); complete_book tuyên bố toàn bộ truyện hoàn thành (content truyền đối tượng rỗng {}, trực tiếp đẩy Phase=Complete; công cụ sẽ kiểm tra: các chương trong đề cương đều đã viết xong, không còn hàng chờ làm lại, compass không còn open_threads chưa thu hồi — xác nhận mọi đường hạch đã thu hồi trước khi update_compass xóa open_threads và bền vào đĩa, muốn thu thanh sớm dùng append_volume với tập thu thanh final). append_volume / complete_book phải mang reason (một câu lý do phán quyết, đối chiếu danh sách kiểm tra hoàn thành, ghi vào kiểm tra phán quyết). scale tùy chọn, chỉ cho phép short / mid / long.",
	saveFoundationLabel:      "Lưu thiết lập",
	saveFoundationArgType:    "Loại thiết lập",
	saveFoundationArgContent: "Nội dung. premise truyền chuỗi Markdown; các kiểu khác truyền trực tiếp mảng hoặc đối tượng JSON, cũng chấp nhận chuỗi JSON. expand_arc thì truyền {title, goal, chapters}, title/goal là mục tiêu quy hoạch cung đã điều chỉnh dựa trên sự thật đã hoàn thành.",
	saveFoundationArgScale:   "Mức quy hoạch",
	saveFoundationArgVolume:  "Số thứ tự tập mục tiêu, tính từ 1 (chỉ bắt buộc khi expand_arc)",
	saveFoundationArgArc:     "Số thứ tự cung mục tiêu trong tập, tính từ 1 (chỉ bắt buộc khi expand_arc)",
	saveFoundationArgReason:  "Lý do phán quyết cuối tập (append_volume / complete_book phải điền): đối chiếu danh sách kiểm tra hoàn thành, dùng một câu giải thích lý do tục tập, tuyên bố thu thanh hoặc hoàn thành",

	saveReviewDesc:  "Lưu kết quả đánh giá và cập nhật trạng thái luồng. verdict là accept/polish/rewrite. Biên tập viên dựa trên ngữ cảnh đầy đủ đưa ra verdict, công cụ chỉ kiểm tra sự thật và cập nhật Progress nguyên tử. Trả về sự thật có cấu trúc: verdict / affected_chapters / next_flow / next_chapter",
	saveReviewLabel: "Lưu đánh giá",

	saveReviewIssueType:        "Loại vấn đề; dùng maintennance dimension từ rubric hoặc viết cụ thể hơn",
	saveReviewIssueSeverity:    "Mức độ nghiêm trọng: critical, error, warning",
	saveReviewIssueDescription: "Mô tả vấn đề",
	saveReviewIssueEvidence:    "Bằng chứng: đoạn gốc, sự kiện cụ thể hoặc dữ liệu trạng thái",
	saveReviewIssueSuggestion:  "Đề xuất sửa đổi; không cần đề xuất thì null",
	saveReviewIssueChapters:    "Chương mà bằng chứng của vấn đề nằm ở; đánh giá cung phải nằm trong khoảng nhiệm vụ",
	saveReviewIssueRequiresChg: "Vấn đề có phải khắc phục ngay các chương liệt kê không, Biên tập viên phán quyết",
	saveReviewDimension:        "Chỉ số đánh giá; do nhiệm vụ đánh giá và rubric quyết định",
	saveReviewDimensionScore:   "Điểm (0-100)",
	saveReviewDimensionComment: "Kết luận ngắn gọn và bằng chứng cho chỉ số này; mỗi chỉ số bắt buộc",
	saveReviewChapter:          "Số chương đánh giá (đánh giá toàn cầu điền số chương mới nhất)",
	saveReviewScope:            "Phạm vi đánh giá: chapter, global, arc",
	saveReviewDimensions:       "Đánh giá theo từng chỉ số; rubric cơ sở do Biên tập viên cung cấp",
	saveReviewIssues:           "Các vấn đề phát hiện",
	saveReviewContractStatus:   "Độ hoàn thành hiệp đồng chương; không dùng thì null",
	saveReviewContractMisses:   "Các điều kiện hiệp đồng chưa hoàn thành hoặc vi phạm; không thì mảng rỗng",
	saveReviewContractNotes:    "Mô tả ngắn gọn việc thực hiện hiệp đồng; không thì null",
	saveReviewVerdict:          "Kết luận đánh giá: accept, polish, rewrite",
	saveReviewSummary:          "Tóm tắt đánh giá",

	saveArcSummaryDesc:  "Lưu tóm tắt cấp cung, ảnh trạng thái nhân vật và quy tắc viết (chế độ dài, gọi khi kết thúc cung)",
	saveArcSummaryLabel: "Lưu tóm tắt cung",

	saveArcSnapshotName:      "Tên nhân vật",
	saveArcSnapshotStatus:    "Trạng thái hiện tại (sống/ bị thương/ mất tích…)",
	saveArcSnapshotPower:     "Thay đổi khả năng",
	saveArcSnapshotMotiv:     "Động lực hiện tại",
	saveArcSnapshotRelations: "Thay đổi quan hệ chính",
	saveArcVoiceName:         "Tên nhân vật",
	saveArcVoiceRules:        "2-3 quy tắc đặc trưng ngôn ngữ (mỗi condition ≤30 ký tự)",
	saveArcProse:             "3-5 quy tắc phong cách kể chuyện (mỗi condition ≤50 ký tự, cụ thể và thi hành được)",
	saveArcDialogue:          "Quy tắc đặc trưng đối thoại nhân vật chính",
	saveArcTaboos:            "Cách viết tiểu thuyết này cần tránh",
	saveArcVolume:            "Số tập",
	saveArcArc:               "Số cung",
	saveArcTitle:             "Tiêu đề cung",
	saveArcSummaryField:      "Tóm tắt cung (dưới 500 ký tự)",
	saveArcKeyEvents:         "Sự kiện chính trong cung",
	saveArcCharacterSnaps:    "Ảnh trạng thái nhân vật",
	saveArcStyleRules:        "Quy tắc phong cách",

	saveVolumeSummaryDesc:  "Lưu tóm tắt cấp tập (chế độ dài, gọi khi kết thúc tập)",
	saveVolumeSummaryLabel: "Lưu tóm tắt tập",

	saveVolVolume:  "Số tập",
	saveVolTitle:   "Tiêu đề tập",
	saveVolSummary: "Tóm tắt tập (dưới 500 ký tự)",
	saveVolKeyEvts: "Sự kiện chính trong tập",

	saveBookDesc:  "Lưu tên tác phẩm và giới thiệu dành cho độc giả không tiết lộ nội dung. Giới thiệu nên trình bày nhân vật chính, xung đột cốt lõi và điểm móc, không được viết thành đề cương nội bộ hoặc tóm tắt kết cục.",
	saveBookLabel: "Lưu thông tin tác phẩm",

	saveBookTitle:    "Tên sách chính, không dấu sách",
	saveBookSynopsis: "Giới thiệu dành cho độc giả không tiết lộ nội dung",
}

var toolTextsZH = &toolTexts{
	novelContextDesc:       "获取小说当前状态和创作上下文。不传 chapter：返回 progress_status（phase/flow/next_chapter/pending_rewrites 等进度字段）+ 基础设定，用于判断下一步该做什么。传 chapter=N：额外返回该章的前情摘要、伏笔、角色状态、风格规则等写作上下文",
	novelContextLabel:      "加载上下文",
	novelContextArgChapter: "章节号。不传则返回进度状态和基础设定（Architect 用）；传入则额外返回该章的写作上下文（Writer/Editor 用）",

	saveFoundationDesc:       "保存小说基础设定（premise/outline/characters/world_rules/compass 等）。**这是唯一持久化入口**：未经此工具调用保存的内容不会进入 store，只在消息里输出 Markdown/JSON 等于丢失。参数固定为 {type, content, scale?, volume?, arc?}。type 可选 premise / outline / layered_outline / characters / world_rules / expand_arc / append_volume / update_compass / complete_book。premise 时 content 必须是 Markdown 字符串；其他类型 content 优先直接传 JSON 数组或对象。expand_arc 校准并展开一个未写骨架弧（单弧详细章节不得超过 8 章——弧末评审需一次读完整弧，过长无法审阅；超限请拆成多个弧，需 volume + arc，content 为 {title, goal, chapters}，可依据已完成正文修订原骨架目标）；append_volume 追加新卷（content 为完整 VolumeOutline JSON，含弧结构；顶层带 \"final\": true 即宣告收官卷——全书在该卷收束，所有章节写完后自动完结，无需再调 complete_book）；update_compass 更新终局方向（content 为 StoryCompass JSON，字段仅 {ending_direction: string 必填, open_threads?: string[], estimated_scale?: string}，其余字段一律不认）；complete_book 宣告全书完结（content 传空对象 {}，直接推 Phase=Complete；工具会校验：大纲内章节已全部写完、无返工队列、compass 无未收束 open_threads——确认长线已收束须先 update_compass 清空 open_threads 落盘，想提前收束用 append_volume 的 final 收官卷）。append_volume / complete_book 必须带 reason 参数（一句话判定理由，对照完结判定清单，记入裁定审计）。scale 可选，仅允许 short / mid / long。",
	saveFoundationLabel:      "保存设定",
	saveFoundationArgType:    "设定类型",
	saveFoundationArgContent: "内容。premise 传 Markdown 字符串；其他类型直接传 JSON 数组或对象即可，也兼容传 JSON 字符串。expand_arc 时传 {title, goal, chapters}，title/goal 是结合已完成事实校准后的目标弧规划。",
	saveFoundationArgScale:   "规划级别",
	saveFoundationArgVolume:  "目标卷序号，从 1 起算（仅 expand_arc 时必传）",
	saveFoundationArgArc:     "卷内目标弧序号，从 1 起算（仅 expand_arc 时必传）",
	saveFoundationArgReason:  "卷末判定理由（append_volume / complete_book 时必填）：对照完结判定清单，一句话说明为何续卷、宣告收官或完结",

	saveReviewDesc:  "保存审阅结果并更新流程状态。verdict 为 accept/polish/rewrite 之一。Editor 依据完整上下文作出 verdict，工具只校验事实并原子更新 Progress。返回结构化事实：verdict / affected_chapters / next_flow / next_chapter",
	saveReviewLabel: "保存审阅",

	saveReviewIssueType:        "问题维度；可使用评审提示中的基础维度，也可写更准确的具体维度",
	saveReviewIssueSeverity:    "严重程度: critical, error, warning",
	saveReviewIssueDescription: "问题描述",
	saveReviewIssueEvidence:    "证据：原文片段、具体情节或状态数据",
	saveReviewIssueSuggestion:  "修改建议；无需建议时为 null",
	saveReviewIssueChapters:    "该问题证据实际所在的章节；弧评审必须落在任务给定区间",
	saveReviewIssueRequiresChg: "该问题是否应立即触发所列章节返工，由 Editor 结合整体阅读体验判断",
	saveReviewDimension:        "评价维度；由当前评审任务和 rubric 决定",
	saveReviewDimensionScore:   "评分（0-100）",
	saveReviewDimensionComment: "该维度的简要结论和证据；每个维度必填",
	saveReviewChapter:          "审阅的章节号（全局审阅填最新章节号）",
	saveReviewScope:            "审阅范围: chapter, global, arc",
	saveReviewDimensions:       "分维度评分；基础 rubric 由 Editor 提示提供，可按任务补充更具体维度",
	saveReviewIssues:           "发现的问题",
	saveReviewContractStatus:   "章节契约完成度；不适用时为 null",
	saveReviewContractMisses:   "未完成或违背的 contract 条目；无则为空数组",
	saveReviewContractNotes:    "对 contract 履行情况的简要说明；无则为 null",
	saveReviewVerdict:          "审阅结论: accept, polish, rewrite",
	saveReviewSummary:          "审阅总结",

	saveArcSummaryDesc:  "保存弧级摘要、角色状态快照和写作规则（长篇模式，弧结束时调用）",
	saveArcSummaryLabel: "保存弧摘要",

	saveArcSnapshotName:      "角色名",
	saveArcSnapshotStatus:    "当前状态（存活/受伤/失踪等）",
	saveArcSnapshotPower:     "能力变化",
	saveArcSnapshotMotiv:     "当前动机",
	saveArcSnapshotRelations: "关键关系变化",
	saveArcVoiceName:         "角色名",
	saveArcVoiceRules:        "2-3 条语言特征规则（每条 ≤30 字）",
	saveArcProse:             "3-5 条叙述风格规则（每条 ≤50 字，要具体可执行）",
	saveArcDialogue:          "核心角色的对话特征规则",
	saveArcTaboos:            "本小说需避免的写法",
	saveArcVolume:            "卷号",
	saveArcArc:               "弧号",
	saveArcTitle:             "弧标题",
	saveArcSummaryField:      "弧摘要（500字以内）",
	saveArcKeyEvents:         "弧内关键事件",
	saveArcCharacterSnaps:    "角色状态快照",
	saveArcStyleRules:        "写作规则",

	saveVolumeSummaryDesc:  "保存卷级摘要（长篇模式，卷结束时调用）",
	saveVolumeSummaryLabel: "保存卷摘要",

	saveVolVolume:  "卷号",
	saveVolTitle:   "卷标题",
	saveVolSummary: "卷摘要（500字以内）",
	saveVolKeyEvts: "卷内关键事件",

	saveBookDesc:  "保存作品书名和面向读者的无剧透简介。简介应呈现主角、核心冲突和阅读钩子，不得写成内部大纲或结局梗概。",
	saveBookLabel: "保存作品信息",

	saveBookTitle:    "正式书名，不带书名号",
	saveBookSynopsis: "面向读者的无剧透小说简介",
}

func toolTextsFor(lang string) *toolTexts {
	return i18n.Table(toolTextsVI, toolTextsZH, i18n.Normalize(lang))
}
