package tools

import "github.com/voocel/ainovel-cli/internal/i18n"

// writeTexts chứa toàn bộ văn bản mô hình đọc trong nhóm công cụ xử lý
// chương (draft / edit / plan / commit / read / reopen / revise / audit / check).
//
// Mô tả công cụ đi thẳng vào ngữ cảnh LLM. Nếu mô hình đang viết tiếng Việt mà
// mô tả vẫn bằng tiếng Trung, ngữ cảnh sẽ liên tục kéo mô hình trôi về Trung
// Quốc — cùng lý do mà nhãn Markdown trong internal/store đã theo ngôn ngữ tác
// phẩm (xem internal/store/labels.go).
//
// Bảng này thuộc phần A2 (nửa "chương") của track A; phần A1 dùng bảng riêng
// trong internal/tools/texts.go. Hai bảng độc lập để các nhánh chạy song song
// không đụng vào nhau.
type writeTexts struct {
	// Nhóm draft_chapter.
	draftChapterDesc       string
	draftChapterLabel      string
	draftChapterArgChapter string
	draftChapterArgContent string
	draftChapterArgMode    string
	draftChapterCompleted  string
	draftChapterNextStep   string

	// Nhóm edit_chapter.
	editChapterDesc          string
	editChapterLabel         string
	editChapterArgChapter    string
	editChapterArgOldString  string
	editChapterArgNewString  string
	editChapterArgReplaceAll string
	editChapterNextStep      string

	// Nhóm plan_chapter.
	planChapterDesc                string
	planChapterLabel               string
	planChapterArgChapter          string
	planChapterArgTitle            string
	planChapterArgGoal             string
	planChapterArgConflict         string
	planChapterArgHook             string
	planChapterArgEmotionArc       string
	planChapterArgNotes            string
	planChapterArgRequiredBeats    string
	planChapterArgForbiddenMoves   string
	planChapterArgContinuityChecks string
	planChapterArgEvaluationFocus  string
	planChapterArgEmotionTarget    string
	planChapterArgPayoffPoints     string
	planChapterArgHookGoal         string
	planChapterCompleted           string
	planChapterNextStep            string

	// Nhóm commit_chapter.
	commitChapterDesc       string
	commitChapterLabel      string
	commitChapterArgChapter string

	// Nhóm read_chapter.
	readChapterDesc         string
	readChapterLabel        string
	readChapterArgChapter   string
	readChapterArgFrom      string
	readChapterArgTo        string
	readChapterArgSource    string
	readChapterArgCharacter string
	readChapterArgMaxRunes  string
	readChapterNoSamples    string
	readChapterMissingHint  string

	// Nhóm reopen_book.
	reopenBookLabel       string
	reopenBookDesc        string
	reopenBookArgChapters string
	reopenBookArgReason   string
	reopenBookNextStep    string

	// Nhóm revise_outline.
	reviseOutlineLabel          string
	reviseOutlineDesc           string
	reviseOutlineArgTitle       string
	reviseOutlineArgCoreEvent   string
	reviseOutlineArgHook        string
	reviseOutlineArgScenes      string
	reviseOutlineArgFrom        string
	reviseOutlineArgReplacement string
	reviseOutlineArgReason      string

	// Nhóm resolve_outline_feedback.
	resolveFeedbackLabel     string
	resolveFeedbackDesc      string
	resolveFeedbackArgReason string

	// Nhóm audit_foundation.
	auditFoundationDesc           string
	auditFoundationLabel          string
	auditFoundationArgArtifact    string
	auditFoundationArgDescription string
	auditFoundationArgEvidence    string
	auditFoundationArgSuggestion  string
	auditFoundationArgFingerprint string
	auditFoundationArgReady       string
	auditFoundationArgSummary     string
	auditFoundationArgIssues      string
	auditFoundationNextAction     string

	// Nhóm check_consistency.
	checkConsistencyDesc       string
	checkConsistencyLabel      string
	checkConsistencyArgChapter string

	// Dùng chung: thông báo cảnh báo khi một nhóm dữ liệu phụ không nạp được.
	loadFailedFmt string
}

// writeTextsVI là bản tiếng Việt của văn bản mô hình đọc.
var writeTextsVI = &writeTexts{
	draftChapterDesc:       "Ghi nội dung truyện của chương. mode=write ghi đè toàn bộ chương, mode=append nối thêm vào bản nháp hiện có (viết tiếp/chỉnh sửa)",
	draftChapterLabel:      "Ghi chương",
	draftChapterArgChapter: "Số chương",
	draftChapterArgContent: "Nội dung truyện của chương",
	draftChapterArgMode:    "Chế độ ghi",
	draftChapterCompleted:  "Chương %d đã được nộp hoàn tất, không thể ghi đè",
	draftChapterNextStep:   "Trước tiên gọi read_chapter(source=draft) đọc lại bản nháp, rồi gọi check_consistency, cuối cùng gọi commit_chapter",

	editChapterDesc: "Chỉ thay thế chính xác các đoạn chuỗi trong bản nháp của những chương đã hoàn thành và đã vào hàng chờ PendingRewrites " +
		"(lựa chọn ưu tiên cho tình huống hoàn thiện, tiết kiệm token hơn viết lại cả chương bằng draft_chapter). " +
		"Không dùng được công cụ này cho bản nháp đầu tiên của chương mới; nếu bản nháp đầu có lỗi nặng, hãy gọi draft_chapter(mode=\"write\") để ghi đè toàn bộ chương. " +
		"Tìm old_string và thay bằng new_string, yêu cầu khớp chính xác và duy nhất (nhiều chỗ khớp thì cần replace_all=true). " +
		"old_string phải sao chép từng chữ từ kết quả trả về của lần read_chapter(source=\"draft\") gần nhất, không được tự dựng lại văn bản gốc từ trí nhớ; " +
		"lưu ý giá trị trả về là chuỗi JSON nên \\n phải được khôi phục thành ký tự xuống dòng thật. " +
		"Sau khi draft_chapter ghi đè bản nháp, phải gọi lại read_chapter trước khi chỉnh sửa. " +
		"Khi không khớp, thông báo lỗi kèm đoạn ứng viên gần nhất trong bản nháp; hãy sao chép từng chữ từ ứng viên đó rồi thử lại. " +
		"Ghi vào drafts/{ch}.draft.md; khi chưa có drafts thì tự động lấy từ chapters làm nguồn. " +
		"Từ chối thực thi khi chương đã hoàn thành mà không nằm trong hàng chờ PendingRewrites. " +
		"Mỗi lần gọi chỉ sửa một chỗ, cần sửa nhiều chỗ thì gọi nhiều lần.",
	editChapterLabel:         "Chỉnh sửa chương",
	editChapterArgChapter:    "Số chương",
	editChapterArgOldString:  "Đoạn văn bản gốc chính xác cần thay thế, văn bản nhiều dòng phải chứa ký tự xuống dòng; khi không dùng replace_all thì phải xuất hiện duy nhất trong bản nháp",
	editChapterArgNewString:  "Văn bản mới sau khi thay thế",
	editChapterArgReplaceAll: "Thay thế tất cả các chỗ khớp (mặc định false)",
	editChapterNextStep:      "Đã ghi thay đổi xuống đĩa. Nếu vẫn còn lỗi nặng thì gọi lại edit_chapter; nếu không thì gọi check_consistency rồi commit_chapter",

	planChapterDesc:                "Lưu ý tưởng sáng tác của chương. Agent tự quyết định độ mịn của việc quy hoạch, không bắt buộc tách cảnh",
	planChapterLabel:               "Quy hoạch chương",
	planChapterArgChapter:          "Số chương",
	planChapterArgTitle:            "Tiêu đề tạm thời của chương; sau khi viết có thể chỉnh theo nội dung truyện",
	planChapterArgGoal:             "Mục tiêu của chương này",
	planChapterArgConflict:         "Xung đột cốt lõi",
	planChapterArgHook:             "Điểm móc cuối chương",
	planChapterArgEmotionArc:       "Đường cong cảm xúc",
	planChapterArgNotes:            "Ghi chú tự do (bất cứ điều gì bạn thấy cần nhớ khi viết)",
	planChapterArgRequiredBeats:    "Các hạng mục tiến triển bắt buộc của chương này",
	planChapterArgForbiddenMoves:   "Các tiến triển tuyệt đối không được xảy ra trong chương này",
	planChapterArgContinuityChecks: "Các điểm tính liên tục cần đặc biệt đối chiếu trong chương này",
	planChapterArgEvaluationFocus:  "Các hạng mục Biên tập viên cần kiểm tra trọng tâm",
	planChapterArgEmotionTarget:    "Tùy chọn: cảm xúc chủ đạo mà bạn muốn người đọc cảm nhận ở chương này",
	planChapterArgPayoffPoints:     "Tùy chọn: các điểm tình tiết hoặc lời hứa cần được đáp ứng mà chương trọng yếu muốn giải quyết",
	planChapterArgHookGoal:         "Tùy chọn: mục tiêu tạo ham muốn đọc tiếp hoặc kịch tính ở cuối chương",
	planChapterCompleted:           "Chương %d đã được nộp hoàn tất, không thể quy hoạch lại",
	planChapterNextStep:            "Gọi ngay draft_chapter(chapter=số chương này, content=chuỗi nội dung truyện đầy đủ) để ghi nội dung, đừng quy hoạch lại cùng một chương",

	commitChapterDesc: "Nộp bản chính của chương. Nạp nội dung truyện từ bản nháp và lưu thành bản chính, cập nhật dòng thời gian, manh mối, quan hệ, trạng thái nhân vật và tiến độ. " +
		"Trả về các sự thật có cấu trúc: next_chapter / review_required / arc_end / volume_end / needs_expansion / book_complete / flow v.v.",
	commitChapterLabel:      "Nộp chương",
	commitChapterArgChapter: "Số chương",

	readChapterDesc:         "Đọc nguyên văn chương. Đọc được bản chính, bản nháp, hoặc trích xuất các đoạn thoại của nhân vật",
	readChapterLabel:        "Đọc chương",
	readChapterArgChapter:   "Số chương (bắt buộc khi đọc một chương)",
	readChapterArgFrom:      "Số chương bắt đầu (dùng khi đọc một dải chương)",
	readChapterArgTo:        "Số chương kết thúc (dùng khi đọc một dải chương)",
	readChapterArgSource:    "Nguồn",
	readChapterArgCharacter: "Tên nhân vật (dùng khi trích xuất đoạn thoại)",
	readChapterArgMaxRunes:  "Số ký tự tối đa mỗi chương (cắt ngắn khi đọc dải chương, mặc định 2000)",
	readChapterNoSamples:    "Nhân vật này chưa có mẫu thoại đã nộp nào để dùng",
	readChapterMissingHint:  "Nguồn đã yêu cầu không có chương này; nếu cần đọc nguồn khác, hãy chỉ định rõ source",

	reopenBookLabel: "Mở lại để làm lại",
	reopenBookDesc: "Mở lại toàn bộ cuốn sách đã hoàn thành (phase=complete) để vào trạng thái làm lại, dùng khi người dùng yêu cầu viết lại/hoàn thiện một vài chương sau khi đã xong. " +
		"chapters là danh sách số chương đã hoàn thành cần làm lại; sau khi gọi, các chương đó vào hàng chờ viết lại, Host sẽ cử Người viết viết lại từng chương và tự động hoàn thành lại khi sửa xong hết. " +
		"Chỉ dùng khi toàn bộ sách đã hoàn thành và người dùng rõ ràng yêu cầu sửa chương đã viết; yêu cầu thêm cốt truyện hoặc nới dài độ dài không thuộc làm lại, đừng dùng công cụ này.",
	reopenBookArgChapters: "Danh sách số chương đã hoàn thành cần làm lại (ít nhất một chương)",
	reopenBookArgReason:   "Lý do làm lại (tùy chọn, ví dụ \"dọn ký tự đặc biệt\")",
	reopenBookNextStep:    "Đã mở lại và đưa các chương mục tiêu vào hàng chờ. Hãy chờ Host chỉ đạo cử Người viết làm lại từng chương; khi sửa xong hết sẽ tự động hoàn thành lại.",

	reviseOutlineLabel:          "Sửa dàn ý",
	reviseOutlineDesc:           "Sửa dàn ý cho phần chưa xảy ra. Từ from_chapter trở đi, dùng replacement thay thế toàn bộ kế hoạch phía sau: với dàn ý phẳng thì thay cả đuôi sách, với dàn ý phân tầng thì thay đuôi của cung chứa chương đó; các chương đã hoàn thành hoặc đang viết không được di dời. Mọi chương phía sau cần giữ lại đều phải nằm trong replacement.",
	reviseOutlineArgTitle:       "Tiêu đề chương",
	reviseOutlineArgCoreEvent:   "Sự kiện cốt lõi của chương này",
	reviseOutlineArgHook:        "Điểm móc cuối chương",
	reviseOutlineArgScenes:      "Các cảnh dự kiến; nếu không có thì là mảng rỗng",
	reviseOutlineArgFrom:        "Bắt đầu thay thế kế hoạch chưa xảy ra từ chương này",
	reviseOutlineArgReplacement: "Thay thế trọn vẹn phần đuôi; các chương phía sau cần giữ lại cũng phải có trong danh sách",
	reviseOutlineArgReason:      "Lý do sửa đợt này",

	resolveFeedbackLabel:     "Xác nhận dàn ý không cần chỉnh",
	resolveFeedbackDesc:      "Xác nhận đã xem xét toàn bộ writer_feedback và kế hoạch phía sau hiện có vẫn còn phù hợp. Chỉ gọi khi không cần sửa dàn ý; khi cần sửa hãy dùng revise_outline hoặc các công cụ cấu trúc.",
	resolveFeedbackArgReason: "Lý do kế hoạch hiện tại vẫn còn áp dụng",

	auditFoundationDesc: "Kiểm tra xem book, premise, outline, characters, world_rules và compass đã ghi xuống đĩa có nhất quán về ngữ nghĩa hay không. " +
		"Phải gọi lại novel_context trước, rồi truyền nguyên vẹn foundation_status.fingerprint.",
	auditFoundationLabel:          "Kiểm tra bối cảnh",
	auditFoundationArgArtifact:    "Hạng mục có vấn đề, ví dụ book/premise/characters/layered_outline/world_rules/compass",
	auditFoundationArgDescription: "Vấn đề ngữ nghĩa xuyên suốt các tệp",
	auditFoundationArgEvidence:    "Bằng chứng xung đột cụ thể lấy từ nội dung đã ghi xuống đĩa",
	auditFoundationArgSuggestion:  "Hướng sửa đề xuất; khi không cần đề xuất thì để null",
	auditFoundationArgFingerprint: "Giá trị foundation_status.fingerprint do novel_context trả về",
	auditFoundationArgReady:       "Toàn bộ bối cảnh nền tảng đã nhất quán về ngữ nghĩa và có thể bước vào sáng tác chưa",
	auditFoundationArgSummary:     "Tóm tắt kết luận kiểm tra",
	auditFoundationArgIssues:      "Các vấn đề ngữ nghĩa xuyên suốt các tệp tìm được; khi ready=true thì là mảng rỗng",
	auditFoundationNextAction:     "Sửa bối cảnh nền tảng tương ứng theo issues, gọi lại novel_context rồi kiểm tra lần nữa",

	checkConsistencyDesc:       "Nạp bản nháp đã viết và dữ liệu đối chiếu (luật thế giới, manh mối, quan hệ, bí danh, tóm tắt gần đây) để bạn kiểm tra tính nhất quán. Phải gọi sau draft_chapter",
	checkConsistencyLabel:      "Kiểm tra tính nhất quán",
	checkConsistencyArgChapter: "Số chương cần kiểm tra",

	loadFailedFmt: "%s: đọc thất bại: %v",
}

// writeTextsZH là bản tiếng Trung của văn bản mô hình đọc.
var writeTextsZH = &writeTexts{
	draftChapterDesc:       "写入章节正文。mode=write 覆盖写入整章，mode=append 追加到现有草稿（续写/修改）",
	draftChapterLabel:      "写入章节",
	draftChapterArgChapter: "章节号",
	draftChapterArgContent: "章节正文",
	draftChapterArgMode:    "写入模式",
	draftChapterCompleted:  "第 %d 章已提交完成，不能覆盖",
	draftChapterNextStep:   "先 read_chapter(source=draft) 回读草稿，再调用 check_consistency，最后 commit_chapter",

	editChapterDesc: "仅对已完成且进入 PendingRewrites 队列的章节草稿做定点字符串替换（打磨场景首选，比 draft_chapter 整章重写省 token）。" +
		"新章初稿禁止使用本工具；初稿有硬伤请调用 draft_chapter(mode=\"write\") 整章覆盖。" +
		"找到 old_string 并替换为 new_string，要求精确匹配且唯一（多处匹配需 replace_all=true）。" +
		"old_string 必须从最近一次 read_chapter(source=\"draft\") 的返回中逐字复制，禁止凭记忆重构原文；" +
		"注意返回值是 JSON 字符串，\\n 须还原为真实换行。draft_chapter 改写过草稿后必须先重新 read_chapter 再编辑。" +
		"匹配失败的报错会附上草稿中最接近的候选片段，请从候选逐字复制后重试。" +
		"写入 drafts/{ch}.draft.md；drafts 不存在时自动从 chapters 播种。" +
		"章节已完成且不在 PendingRewrites 队列中时拒绝执行。每次调用只改一处，多处修改请多次调用。",
	editChapterLabel:         "编辑章节",
	editChapterArgChapter:    "章节号",
	editChapterArgOldString:  "要替换的原文精确片段，多行需包含换行；不加 replace_all 时必须在草稿中唯一出现",
	editChapterArgNewString:  "替换后的新文本",
	editChapterArgReplaceAll: "替换所有匹配（默认 false）",
	editChapterNextStep:      "edit 已落盘。仍有硬伤可再次 edit_chapter；否则 check_consistency 后 commit_chapter",

	planChapterDesc:                "保存章节写作构思。Agent 自主决定规划粒度，不强制场景拆分",
	planChapterLabel:               "规划章节",
	planChapterArgChapter:          "章节号",
	planChapterArgTitle:            "暂定章节标题；写作后可按正文调整",
	planChapterArgGoal:             "本章目标",
	planChapterArgConflict:         "核心冲突",
	planChapterArgHook:             "章末钩子",
	planChapterArgEmotionArc:       "情绪曲线",
	planChapterArgNotes:            "自由备忘（任何你觉得写作时需要记住的东西）",
	planChapterArgRequiredBeats:    "本章必须完成的推进项",
	planChapterArgForbiddenMoves:   "本章明确不能发生的推进",
	planChapterArgContinuityChecks: "本章需特别核对的连续性点",
	planChapterArgEvaluationFocus:  "Editor 重点检查项",
	planChapterArgEmotionTarget:    "可选：本章希望读者主要感受到的情绪",
	planChapterArgPayoffPoints:     "可选：关键章希望回应的情节点或兑现点",
	planChapterArgHookGoal:         "可选：章末希望驱动的追读欲望或悬念目标",
	planChapterCompleted:           "第 %d 章已提交完成，不能重新规划",
	planChapterNextStep:            "立即调用 draft_chapter(chapter=本章节号, content=完整正文字符串) 写入正文，不要重复规划同一章",

	commitChapterDesc: "提交章节终稿。加载草稿正文保存为终稿，更新时间线、伏笔、关系、角色状态和进度。" +
		"返回结构化事实：next_chapter / review_required / arc_end / volume_end / needs_expansion / book_complete / flow 等",
	commitChapterLabel:      "提交章节",
	commitChapterArgChapter: "章节号",

	readChapterDesc:         "读取章节原文。可读终稿、草稿，或提取角色对话片段",
	readChapterLabel:        "读取章节",
	readChapterArgChapter:   "章节号（读单章时必填）",
	readChapterArgFrom:      "起始章节号（读范围时使用）",
	readChapterArgTo:        "结束章节号（读范围时使用）",
	readChapterArgSource:    "来源",
	readChapterArgCharacter: "角色名（提取对话片段时使用）",
	readChapterArgMaxRunes:  "每章最大字符数（范围读取时截取，默认 2000）",
	readChapterNoSamples:    "该角色暂无可用的已提交对话样本",
	readChapterMissingHint:  "请求的来源中没有该章节；如需读取另一来源，请明确指定 source",

	reopenBookLabel: "重开返工",
	reopenBookDesc: "把已完结（phase=complete）的全书重新打开进入返工态，用于用户在完本后要求重写/打磨某几章。" +
		"chapters 是要返工的已完成章节号；调用后这些章进入重写队列，Host 会逐章派 writer 重写，全部改完自动重新完结。" +
		"仅在全书已完结、且用户明确要求修改已写章节时使用；用户要新增剧情/扩展篇幅不属返工，不要用本工具。",
	reopenBookArgChapters: "要返工的已完成章节号列表（至少一章）",
	reopenBookArgReason:   "返工原因（可选，如\"清理特殊字符\"）",
	reopenBookNextStep:    "已重新打开并把目标章入队。请等待 Host 指令派 writer 逐章返工；全部改完后会自动重新完结。",

	reviseOutlineLabel:          "修订大纲",
	reviseOutlineDesc:           "修订尚未发生的大纲。从 from_chapter 起，用 replacement 完整替换后续计划：扁平大纲替换全书尾段，分层大纲替换该章所在弧的尾段；已完成或正在写作的章节不可移动。需要保留的后续章节必须一并放入 replacement。",
	reviseOutlineArgTitle:       "章节标题",
	reviseOutlineArgCoreEvent:   "本章核心事件",
	reviseOutlineArgHook:        "章末钩子",
	reviseOutlineArgScenes:      "计划场景；无则为空数组",
	reviseOutlineArgFrom:        "从这一章开始替换尚未发生的计划",
	reviseOutlineArgReplacement: "完整替换尾段；需要保留的后续章节也必须包含",
	reviseOutlineArgReason:      "本次修订原因",

	resolveFeedbackLabel:     "确认大纲无需调整",
	resolveFeedbackDesc:      "确认已审查全部 writer_feedback，且现有后续计划仍然适用。只有无需修改大纲时调用；需要修改时使用 revise_outline 或结构工具。",
	resolveFeedbackArgReason: "现有计划仍然适用的理由",

	auditFoundationDesc: "审查已落盘的 book、premise、outline、characters、world_rules 与 compass 是否语义一致。" +
		"必须先重新调用 novel_context，并原样传入 foundation_status.fingerprint。",
	auditFoundationLabel:          "审查设定",
	auditFoundationArgArtifact:    "存在问题的工件，如 book/premise/characters/layered_outline/world_rules/compass",
	auditFoundationArgDescription: "跨文件语义问题",
	auditFoundationArgEvidence:    "来自已落盘内容的具体冲突证据",
	auditFoundationArgSuggestion:  "推荐修改方向；无需建议时为 null",
	auditFoundationArgFingerprint: "novel_context 返回的 foundation_status.fingerprint",
	auditFoundationArgReady:       "所有基础设定是否已语义一致，可以进入写作",
	auditFoundationArgSummary:     "审查结论摘要",
	auditFoundationArgIssues:      "发现的跨文件语义问题；ready=true 时为空数组",
	auditFoundationNextAction:     "按 issues 修正对应基础设定，重新调用 novel_context 后再次审查",

	checkConsistencyDesc:       "加载已写草稿和对照数据（世界规则、伏笔、关系、别名、最近摘要），供你检查一致性。必须在 draft_chapter 之后调用",
	checkConsistencyLabel:      "一致性检查",
	checkConsistencyArgChapter: "要检查的章节号",

	loadFailedFmt: "%s 读取失败: %v",
}

// writeTextsFor chọn bảng văn bản theo ngôn ngữ tác phẩm.
//
// Bảng là con trỏ để i18n.Table không copy struct lớn mỗi lần gọi.
func writeTextsFor(lang string) *writeTexts {
	return i18n.Table(writeTextsVI, writeTextsZH, i18n.Normalize(lang))
}
