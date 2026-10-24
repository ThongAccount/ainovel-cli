package flow

import "github.com/voocel/ainovel-cli/internal/i18n"

// flowTexts chứa toàn bộ văn bản mô hình đọc trong package flow.
//
// Task của Instruction là câu lệnh truyền thẳng cho Worker, nên phải theo ngôn
// ngữ tác phẩm: mô hình đang viết tiếng Việt mà nhận nhiệm vụ tiếng Trung sẽ bị
// ngữ cảnh liên tục kéo về giọng Trung — cùng lý do mà nhãn Markdown của
// internal/store đã theo ngôn ngữ tác phẩm (xem internal/store/labels.go).
//
// Reason thì chỉ người vận hành đọc (sự kiện, nhật ký, thông báo trên TUI) nên
// dùng tiếng Việt cố định, không nằm ở đây.
type flowTexts struct {
	// Nhánh bổ sung thiết lập nền tảng trong giai đoạn quy hoạch.
	foundationMissing string
	foundationAudit   string
	// missingSep là dấu phân cách danh sách mục còn thiếu (mục là định danh kỹ
	// thuật nên dấu phẩy tiếng Việt vẫn đọc được).
	missingSep string

	// Nhánh viết lại / hoàn thiện chương trong hàng đợi làm lại.
	rewriteChapter        string
	polishChapter         string
	rewriteNeedsDirective string

	// Nhánh làm mới các công cụ tổng hợp (arc/volume/global).
	refreshArcReview     string
	refreshArcSummary    string
	refreshVolumeSummary string
	refreshGlobalReview  string

	// Nhánh xử lý phản hồi sửa đổi bên ngoài.
	feedbackOnly string

	// Nhánh hậu xử lý cuối cung trong sách phân lớp.
	arcReview     string
	arcSummary    string
	volumeSummary string
	expandArc     string
	appendVolume  string

	// Nhánh thẩm duyệt toàn cục và nhánh dàn ý đã cạn (sách không phân lớp).
	globalReview     string
	outlineExhausted string

	// Nhánh viết tiếp chương kế tiếp.
	writeChapter string
}

// flowTextsVI là bản tiếng Việt — ngôn ngữ mặc định của bản port.
var flowTextsVI = &flowTexts{
	foundationMissing: "Bổ sung các mục còn thiếu của thiết lập nền tảng và thông tin tác phẩm: %s; book dùng save_book, các thiết lập nền tảng còn lại dùng save_foundation để ghi xuống đĩa",
	foundationAudit:   "Thiết lập nền tảng đã đầy đủ: gọi lại novel_context để đọc toàn bộ công cụ đã ghi xuống đĩa và foundation_status.fingerprint, kiểm tra tính nhất quán ngữ nghĩa giữa các tệp rồi gọi audit_foundation; nếu có vấn đề thì sửa trước và kiểm tra lại",
	missingSep:        ", ",

	rewriteChapter:        "Viết lại chương %d",
	polishChapter:         "Hoàn thiện chương %d",
	rewriteNeedsDirective: ".Chương này chưa có chapter_contract: hãy gọi plan_chapter(chapter=%d) để viết rõ mục tiêu viết lại (goal phải trả lời \"viết thành thế nào\", chứ không phải lặp lại \"sai ở đâu\"; nếu cần thì bổ sung payoff_points / continuity_checks / hook_goal), rồi viết lại nội dung theo đó",

	refreshArcReview:     "Thẩm duyệt mức cung cho tập %d cung %d (chương %d-%d): gọi novel_context(chapter=%d), save_review dùng scope=arc, chapter=%d",
	refreshArcSummary:    "Tạo tóm tắt cung, ảnh chụp nhân vật và quy tắc viết cho tập %d cung %d (save_arc_summary)",
	refreshVolumeSummary: "Tạo tóm tắt tập %d (save_volume_summary)",
	refreshGlobalReview:  "Thẩm duyệt toàn cục %d chương đầu: gọi novel_context(chapter=%d), save_review dùng scope=global, chapter=%d",

	feedbackOnly: "Chỉ xử lý writer_feedback từ các sửa đổi bên ngoài trong novel_context: đối chiếu cốt truyện đã xảy ra với kế hoạch về sau, khi cần điều chỉnh thì gọi revise_outline hoặc công cụ cấu trúc tương ứng, khi không cần thì gọi resolve_outline_feedback; không được xử lý foundation_status hay các phần quy hoạch khác, sau khi ghi xuống đĩa thì kết thúc bằng một câu",

	arcReview:     "Thẩm duyệt mức cung cho tập %d cung %d (chương %d-%d): gọi novel_context(chapter=%d), save_review dùng scope=arc, chapter=%d; issues[].chapters chỉ được nằm trong khoảng này",
	arcSummary:    "Tạo tóm tắt cung, ảnh chụp nhân vật và quy tắc viết cho tập %d cung %d (save_arc_summary)",
	volumeSummary: "Tạo tóm tắt tập %d (save_volume_summary)",
	expandArc:     "Triển khai cung %d của tập %d (save_foundation type=expand_arc)",
	appendVolume:  "Tạo tập tiếp theo: đánh giá theo danh sách điều kiện kết thúc rồi gọi save_foundation — câu chuyện còn tiếp → type=append_volume; câu chuyện đã gần đến đoạn kết → type=append_volume kèm \"final\": true ở cấp JSON gốc của tập (tập chốt, chốt trọn tập, viết xong sẽ tự hoàn thành); toàn bộ điều kiện kết thúc đã thỏa mãn ngay lúc này → type=complete_book. Cả ba lựa chọn đều phải kèm tham số reason nói rõ lý do phán quyết",

	globalReview:     "Thẩm duyệt toàn cục %d chương đầu (save_review scope=global, chapter=%d)",
	outlineExhausted: "Dàn ý không phân lớp đã viết xong (đã hoàn thành %d chương, tổng %d chương): nếu câu chuyện đã khép lại thì gọi save_foundation(type=complete_book); nếu vẫn cần tiếp thì dùng revise_outline để nối tiếp kế hoạch từ chương %d",

	writeChapter: "Viết chương %d",
}

// flowTextsZH là bản tiếng Trung — giữ nguyên văn bản gốc của dự án.
var flowTextsZH = &flowTexts{
	foundationMissing: "补齐基础设定与作品信息缺项：%s；book 使用 save_book，其余基础设定使用 save_foundation 落盘",
	foundationAudit:   "基础设定已齐全：重新调用 novel_context 读取全部已落盘工件与 foundation_status.fingerprint，审查跨文件语义一致性后调用 audit_foundation；有问题先修正并重新审查",
	missingSep:        "、",

	rewriteChapter:        "重写第 %d 章",
	polishChapter:         "打磨第 %d 章",
	rewriteNeedsDirective: "。本章尚无 chapter_contract：请先调 plan_chapter(chapter=%d) 写清重写目标（goal 要回答\"改成什么样\"，而不是复述\"哪里错了\"；必要时补 payoff_points / continuity_checks / hook_goal），再据此重写正文",

	refreshArcReview:     "审阅第 %d 卷第 %d 弧（第 %d-%d 章）：调用 novel_context(chapter=%d)，save_review 使用 scope=arc、chapter=%d",
	refreshArcSummary:    "生成第 %d 卷第 %d 弧摘要、角色快照与写作规则（save_arc_summary）",
	refreshVolumeSummary: "生成第 %d 卷卷摘要（save_volume_summary）",
	refreshGlobalReview:  "审阅前 %d 章：调用 novel_context(chapter=%d)，save_review 使用 scope=global、chapter=%d",

	feedbackOnly: "仅处理 novel_context 中的外部修订 writer_feedback：核对已发生剧情与后续计划，需要调整时调用 revise_outline 或相应结构工具，无需调整时调用 resolve_outline_feedback；不得处理 foundation_status 或其它规划，落盘后用一句话结束",

	arcReview:     "对第 %d 卷第 %d 弧（第 %d-%d 章）做弧级评审：调用 novel_context(chapter=%d)，save_review 使用 scope=arc、chapter=%d；issues[].chapters 只能落在该区间",
	arcSummary:    "生成第 %d 卷第 %d 弧摘要、角色快照与写作规则（save_arc_summary）",
	volumeSummary: "生成第 %d 卷卷摘要（save_volume_summary）",
	expandArc:     "展开第 %d 卷第 %d 弧（save_foundation type=expand_arc）",
	appendVolume:  "创建下一卷：按完结判定清单评估后调用 save_foundation——故事继续 → type=append_volume；故事接近终点 → type=append_volume 且卷 JSON 顶层带 \"final\": true（收官卷，整卷收线，写完自动完结）；全部完结条件当下已满足 → type=complete_book。三选一均须附 reason 参数写明判定理由",

	globalReview:     "对前 %d 章做全局审阅（save_review scope=global, chapter=%d）",
	outlineExhausted: "非分层大纲已写完（已完成 %d 章，共 %d 章）：若故事已收束，调用 save_foundation(type=complete_book)；若仍需继续，用 revise_outline 从第 %d 章续接后续计划",

	writeChapter: "写第 %d 章",
}

// flowTextsFor chọn bản văn bản theo ngôn ngữ tác phẩm.
func flowTextsFor(lang i18n.Lang) *flowTexts {
	return i18n.Table(flowTextsVI, flowTextsZH, lang)
}
