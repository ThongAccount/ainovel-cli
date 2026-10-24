package guard

import (
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
)

// guardTexts chứa các thông điệp mà guard chèn vào hội thoại để thúc model
// tiếp tục — đây là văn bản model đọc, nên phải theo ngôn ngữ tác phẩm.
//
// Nguồn ngôn ngữ: store.Language() (guard luôn có *store.Store).
type guardTexts struct {
	writerNoDraft   string
	writerNoCheck   string
	writerNeedFinal string

	architectNeedArtifact string

	editorVolumeSummary string
	editorArcSummary    string
	editorAnyArtifact   string
}

var guardTextsVI = &guardTexts{
	writerNoDraft: "Cấm kết thúc: lượt này chưa ghi xuống đĩa bất kỳ phần nội dung nào. " +
		"Hãy hoàn tất chương theo thứ tự plan_chapter → draft_chapter → check_consistency → commit_chapter; " +
		"nội dung chỉ nói trong chat là mất trắng, phải ghi xuống đĩa qua công cụ và gửi đi.",
	writerNoCheck: "Cấm kết thúc: nội dung đã ghi xuống đĩa nhưng chưa chốt. " +
		"Hãy gọi check_consistency để đối chiếu tính nhất quán, rồi gọi commit_chapter để gửi chương. " +
		"draft_chapter / edit_chapter chỉ lưu bản nháp, không tính là hoàn tất.",
	writerNeedFinal: "Cấm kết thúc: chương này chỉ còn thiếu bước gửi bằng commit_chapter. " +
		"Hãy gọi commit_chapter ngay; nếu nó trả về lỗi, hãy xử lý theo thông báo lỗi " +
		"(đối chiếu số chương, bổ sung bước trước đó theo gợi ý) rồi thử gửi lại, " +
		"đừng kết thúc ở trạng thái chưa gửi.",

	architectNeedArtifact: "Bạn phải gọi save_book, save_foundation, revise_outline, resolve_outline_feedback, audit_foundation " +
		"hoặc plan_chapter (viết lệnh viết lại cho chương trong hàng đợi làm lại) " +
		"để ghi sản phẩm xuống đĩa rồi mới được kết thúc. Chỉ nội dung Markdown/JSON bằng lời nói là mất trắng.",

	editorVolumeSummary: "Nhiệm vụ lượt này là sinh tóm tắt tập: bạn phải gọi save_volume_summary để ghi xuống đĩa " +
		"rồi mới được kết thúc, save_review (soát duyệt) không tính là hoàn tất.",
	editorArcSummary: "Nhiệm vụ lượt này là sinh tóm tắt cung: bạn phải gọi save_arc_summary để ghi xuống đĩa " +
		"rồi mới được kết thúc, save_review (soát duyệt) không tính là hoàn tất.",
	editorAnyArtifact: "Bạn phải gọi một trong save_review / save_arc_summary / save_volume_summary " +
		"để ghi kết quả xuống đĩa rồi mới được kết thúc.",
}

var guardTextsZH = &guardTexts{
	writerNoDraft:   "禁止结束：本轮尚未落盘任何正文。请按 plan_chapter → draft_chapter → check_consistency → commit_chapter 的顺序完成本章；正文只输出在聊天里等于丢失，必须通过工具落盘并提交。",
	writerNoCheck:   "禁止结束：正文已落盘但未收尾。请先调 check_consistency 核对一致性，再调 commit_chapter 提交本章。draft_chapter / edit_chapter 只是保存草稿，不算完成。",
	writerNeedFinal: "禁止结束：本章只差 commit_chapter 提交。请立即调用 commit_chapter；若它返回错误，先按错误信息处理（核对章节号、按提示补齐前置动作）再重试提交，不要在未提交的状态下结束。",

	architectNeedArtifact: "你必须调用 save_book、save_foundation、revise_outline、resolve_outline_feedback、audit_foundation 或 plan_chapter（为返工队列章写重写指令）将产出落盘后才能结束。只输出 Markdown/JSON 文字等于丢失。",

	editorVolumeSummary: "本次任务是生成卷摘要：你必须调用 save_volume_summary 落盘后才能结束，save_review 复核不算完成。",
	editorArcSummary:    "本次任务是生成弧摘要：你必须调用 save_arc_summary 落盘后才能结束，save_review 复核不算完成。",
	editorAnyArtifact:   "你必须调用 save_review / save_arc_summary / save_volume_summary 之一落盘结果后才能结束。",
}

// guardTextsFor chọn bảng thông điệp theo ngôn ngữ tác phẩm.
func guardTextsFor(lang i18n.Lang) *guardTexts {
	return i18n.Table(guardTextsVI, guardTextsZH, lang)
}

// storeLang trả về ngôn ngữ tác phẩm của store; store nil thì rơi về ngôn ngữ
// đang hiệu hành của tiến trình.
func storeLang(st *store.Store) i18n.Lang {
	if st == nil {
		return i18n.Active()
	}
	return i18n.Normalize(st.Language())
}
