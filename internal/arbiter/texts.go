package arbiter

import "github.com/voocel/ainovel-cli/internal/i18n"

// arbiterTexts chứa văn bản mô hình đọc trong lớp phán quyết: mô tả hợp
// đồng, nhãn của từng trường schema và các thông báo lỗi nghiệp vụ được đưa
// lại cho model để tự sửa.
//
// Lớp phán quyết không có store nên lấy ngôn ngữ từ i18n.Active() — ngôn ngữ
// tác phẩm ở cấp tiến trình.
type arbiterTexts struct {
	// Mô tả hợp đồng.
	planStartDesc    string
	interventionDesc string
	failureDesc      string

	// Nhãn trường schema.
	plannerLabel          string
	planStartTaskLabel    string
	planStartReasonLabel  string
	answerLabel           string
	rulesLabel            string
	holdCancelLabel       string
	holdAfterLabel        string
	holdTargetLabel       string
	holdReasonLabel       string
	reopenChaptersLabel   string
	reopenChapterLabel    string
	reopenReasonLabel     string
	dispatchLabel         string
	interventionReasonLbl string
	failureActionLabel    string
	failureDispatchLabel  string
	failureReasonLabel    string
	dispatchAgentLabel    string
	dispatchTaskLabel     string

	// Thông báo lỗi nghiệp vụ đưa lại cho model.
	reasonRequired          string
	dispatchAgentInvalid    string
	dispatchTaskRequired    string
	rerouteNeedsDispatch    string
	actionInvalid           string
	plannerInvalid          string
	taskRequired            string
	emptyDecision           string
	reopenOnlyComplete      string
	reopenChaptersRequired  string
	reopenChapterOutOfRange string
	completeNoDispatch      string
	holdOnlyWriting         string
	holdInvalid             string
	targetChapterTooEarly   string
	missingPhase            string
	workerPhaseOnly         string
}

var arbiterTextsVI = &arbiterTexts{
	planStartDesc:    "Phán quyết khởi chạy: chọn kiến trúc sư và tạo văn bản nhiệm vụ đầy đủ",
	interventionDesc: "Phán quyết can thiệp của người dùng: trả lời, quy tắc, tạm dừng, mở lại và phân công",
	failureDesc:      "Phán quyết thất bại/bế tắc: chỉ ra lối thoát",

	plannerLabel:          "Kiến trúc sư",
	planStartTaskLabel:    "Nhiệm vụ đầy đủ giao cho kiến trúc sư (bao gồm yêu cầu đã bổ sung)",
	planStartReasonLabel:  "Lý do chọn",
	answerLabel:           "Nội dung hiển thị lại cho người dùng; không có thì null",
	rulesLabel:            "Nguyên văn quy tắc viết dài hạn cần ghi xuống đĩa; không có thì null",
	holdCancelLabel:       "Có huỷ lần tạm dừng một lần đang có hay không",
	holdAfterLabel:        "Điểm kích hoạt tạm dừng; khi huỷ thì null",
	holdTargetLabel:       "Chương đích khi after=chapter; các trường hợp khác là null",
	holdReasonLabel:       "Tóm tắt yêu cầu của người dùng; khi huỷ có thể là null",
	reopenChaptersLabel:   "Số chương cần mở lại",
	reopenChapterLabel:    "Số chương",
	reopenReasonLabel:     "Lý do mở lại",
	dispatchLabel:         "Đích phân công; không cần phân công thì null",
	interventionReasonLbl: "Lý do phán quyết trong một câu",
	failureActionLabel:    "Lối thoát",
	failureDispatchLabel:  "Đích phân công (chỉ nêu khi reroute, nếu không thì null)",
	failureReasonLabel:    "Lý do phán quyết",
	dispatchAgentLabel:    "Đích phân công",
	dispatchTaskLabel:     "Mô tả nhiệm vụ đầy đủ giao cho worker này",

	reasonRequired:          "reason không được để trống",
	dispatchAgentInvalid:    "dispatch.agent không hợp lệ: %q",
	dispatchTaskRequired:    "dispatch.task không được để trống",
	rerouteNeedsDispatch:    "reroute phải kèm dispatch",
	actionInvalid:           "action không hợp lệ: %q (có thể chọn retry / reroute / abort)",
	plannerInvalid:          "planner không hợp lệ: %q (có thể chọn architect_long / architect_short)",
	taskRequired:            "task không được để trống",
	emptyDecision:           "quyết định rỗng: phải có ít nhất một hành động hoặc answer",
	reopenOnlyComplete:      "reopen chỉ hợp lệ ở giai đoạn hoàn thành (phase hiện tại=%s)",
	reopenChaptersRequired:  "reopen.chapters không được để trống",
	reopenChapterOutOfRange: "chương reopen %d vượt phạm vi (đã hoàn thành %d chương)",
	completeNoDispatch:      "giai đoạn hoàn thành cấm phân công trực tiếp; làm lại dùng reopen (sau khi vào hàng đợi, Router tự phân công)",
	holdOnlyWriting:         "tạm dừng một lần chỉ hợp lệ ở giai đoạn sáng tác (phase hiện tại=%s)",
	holdInvalid:             "hold không hợp lệ: %w",
	targetChapterTooEarly:   "chương đích %d sớm hơn chương kế tiếp hiện tại %d",
	missingPhase:            "thiếu phase, cấm thực thi phân công",
	workerPhaseOnly:         "%s chỉ được phân công ở giai đoạn writing (phase hiện tại=%s)",
}

var arbiterTextsZH = &arbiterTexts{
	planStartDesc:    "启动裁定:选规划师并产出完整任务文本",
	interventionDesc: "用户干预裁定：回答、规则、暂停、重开与派单",
	failureDesc:      "失败/僵局裁定:给出出路",

	plannerLabel:          "规划师",
	planStartTaskLabel:    "交给规划师的完整任务(含扩充后的需求)",
	planStartReasonLabel:  "选择理由",
	answerLabel:           "回显给用户的文字；无则为 null",
	rulesLabel:            "要落盘的长效写作规则原文；无则为 null",
	holdCancelLabel:       "是否取消既有一次性暂停",
	holdAfterLabel:        "暂停触发点；取消时为 null",
	holdTargetLabel:       "after=chapter 时的目标章节；其他情况为 null",
	holdReasonLabel:       "用户诉求摘要；取消时可为 null",
	reopenChaptersLabel:   "需要重开的章节号",
	reopenChapterLabel:    "章节号",
	reopenReasonLabel:     "重开理由",
	dispatchLabel:         "派单目标；无需派单时为 null",
	interventionReasonLbl: "一句话裁定理由",
	failureActionLabel:    "出路",
	failureDispatchLabel:  "派单目标(仅 reroute 时给出,否则为 null)",
	failureReasonLabel:    "裁定理由",
	dispatchAgentLabel:    "派单目标",
	dispatchTaskLabel:     "交给该 worker 的完整任务描述",

	reasonRequired:          "reason 不能为空",
	dispatchAgentInvalid:    "dispatch.agent 非法: %q",
	dispatchTaskRequired:    "dispatch.task 不能为空",
	rerouteNeedsDispatch:    "reroute 必须附 dispatch",
	actionInvalid:           "action 非法: %q（可选 retry / reroute / abort）",
	plannerInvalid:          "planner 非法: %q（可选 architect_long / architect_short）",
	taskRequired:            "task 不能为空",
	emptyDecision:           "空决策：至少要有一个动作或 answer",
	reopenOnlyComplete:      "reopen 仅限完本期（当前 phase=%s）",
	reopenChaptersRequired:  "reopen.chapters 不能为空",
	reopenChapterOutOfRange: "reopen 章节 %d 越界（已完成 %d 章）",
	completeNoDispatch:      "完本期禁止直接派单；返工用 reopen（入队后由 Router 自动派发）",
	holdOnlyWriting:         "一次性暂停仅限写作期（当前 phase=%s）",
	holdInvalid:             "hold 无效: %w",
	targetChapterTooEarly:   "目标章节 %d 早于当前下一章 %d",
	missingPhase:            "缺少 phase，禁止执行派单",
	workerPhaseOnly:         "%s 仅能在 writing 阶段派发（当前 phase=%s）",
}

// arbiterTextsFor chọn bảng văn bản theo ngôn ngữ tác phẩm.
func arbiterTextsFor(lang i18n.Lang) *arbiterTexts {
	return i18n.Table(arbiterTextsVI, arbiterTextsZH, lang)
}

// activeTexts trả về bảng văn bản theo ngôn ngữ tác phẩm ở cấp tiến trình.
// Dùng cho các hàm kiểm tra có chữ ký đã công khai, không thể đổi.
func activeTexts() *arbiterTexts {
	return arbiterTextsFor(i18n.Active())
}
