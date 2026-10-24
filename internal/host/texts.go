package host

import "github.com/voocel/ainovel-cli/internal/i18n"

// hostTexts chứa toàn bộ văn bản mô hình đọc trong package host.
//
// Mọi chuỗi ở đây đi vào prompt hoặc gói sự thật gửi cho mô hình (Tài phán,
// Người viết, trợ lý đồng sáng tác). Chúng phải theo ngôn ngữ tác phẩm:
// mô hình đang viết tiếng Việt mà nhận chỉ dẫn tiếng Trung sẽ bị ngữ cảnh
// liên tục kéo về giọng Trung — cùng lý do mà nhãn Markdown của internal/store
// đã theo ngôn ngữ tác phẩm (xem internal/store/labels.go).
//
// Văn bản chỉ người dùng đọc (lỗi, dòng log, thông báo tiến trình) không nằm
// ở đây: bản port dùng tiếng Việt cố định cho giao diện vận hành.
type hostTexts struct {
	// Prompt đồng sáng tác (lạnh + theo giai đoạn) và phần đuôi giao thức dùng chung.
	coCreateSystemPrompt      string
	stageCoCreateSystemPrompt string
	coCreateProtocolTail      string

	// Phụ lục trạng thái truyện đính vào prompt giai đoạn.
	stageStateTitle string
	stageStateNote  string

	// Tiền tố can thiệp gửi Tài phán phán quyết.
	stagePlanPrefix string

	// Tiêu đề mục "xung đột cốt lõi" trong tiền đề truyện. Đây là tên mục mà
	// mô hình đọc (prompt kiến trúc sư yêu cầu mục này), nên phải theo ngôn ngữ
	// tác phẩm; nửa tiếng Trung giữ nguyên để dự án cũ vẫn nâng cấp được.
	premiseConflictHeading string

	// Nội dung công việc gửi Worker và đoạn can thiệp gốc của người dùng.
	expandArcTask        string
	interventionOriginal string

	// Cảnh báo gói sự thật khi Engine không đọc được dữ liệu nền.
	foundationReadFailed string
	progressReadFailed   string

	// Thông điệp lỗi mà Tài phán cũng đọc được (nằm trong gói sự thật).
	invalidWriteTarget   string
	writerPhaseError     string
	preMarkChapterFailed string

	// Nhãn dữ liệu trong bản tóm tắt trạng thái truyện.
	stateBookTitle   string
	stateProgress    string
	stateRefined     string
	statePlanned     string
	stateWordCount   string
	statePosition    string
	stateEnding      string
	stateScale       string
	stateOpenThreads string
	stateLastVolume  string
	stateMainChars   string
	stateForeshadow  string
	stateWarnings    string
	stateReadFailed  string
	stateRoleParen   string
	stateListSep     string
	stateItemSep     string
}

// hostTextsVI là bản tiếng Việt — ngôn ngữ mặc định của bản port.
var hostTextsVI = &hostTexts{
	coCreateSystemPrompt:      coCreateSystemPromptVI,
	stageCoCreateSystemPrompt: stageCoCreateSystemPromptVI,
	coCreateProtocolTail:      coCreateProtocolTailVI,

	stageStateTitle: "## Trạng thái truyện hiện tại",
	stageStateNote:  "(Dưới đây là tóm tắt khách quan của nội dung đã viết, dùng làm căn cứ khi quy hoạch phần tiếp theo; đừng chép lại nguyên văn trong <draft>)",

	stagePlanPrefix: "[Lập kế hoạch theo giai đoạn] Tôi tạm dừng viết và cùng trợ lý đồng sáng tác để sắp xếp hướng đi tiếp theo dưới đây. Hãy phán quyết theo phân loại can thiệp của bạn xem nên triển khai thế nào, rồi tiếp tục sáng tác. Hướng đi tiếp theo như sau:\n\n",

	premiseConflictHeading: "Xung đột cốt lõi",

	expandArcTask:        "Cung tiếp theo mới chỉ là khung (%s). Hãy gọi save_foundation(type=expand_arc) để triển khai cung tiếp theo; nếu tập hiện tại đã viết xong thì dùng type=append_volume để thêm tập và triển khai tập kế tiếp.",
	interventionOriginal: "\n\nCan thiệp gốc của người dùng (nguồn duy nhất cho phép sửa đổi lần này; ngữ cảnh chỉ để hiểu, không được mở rộng mục tiêu hay phạm vi):\n",

	foundationReadFailed: "Đọc trạng thái thiết lập nền tảng thất bại: ",
	progressReadFailed:   "Đọc tiến độ sáng tác thất bại: ",

	invalidWriteTarget:   "mục tiêu ghi không hợp lệ",
	writerPhaseError:     "writer chỉ có thể được phát trong giai đoạn writing (phase hiện tại=%s): %w",
	preMarkChapterFailed: "%w: đánh dấu chương %d đang thực hiện thất bại: %w",

	stateBookTitle:   "- Tên sách: «%s»\n",
	stateProgress:    "- Tiến độ: đã hoàn thành %d chương",
	stateRefined:     " / hiện đã chi tiết hóa %d chương (phần sau lập kế hoạch động theo cung)",
	statePlanned:     " / dự kiến %d chương",
	stateWordCount:   ", khoảng %d từ, chương kế tiếp là chương %d\n",
	statePosition:    "- Vị trí hiện tại: tập %d cung %d\n",
	stateEnding:      "- Hướng kết cục: %s\n",
	stateScale:       "- Quy mô ước chừng: %s\n",
	stateOpenThreads: "- Mạch dài đang hoạt động: %s\n",
	stateLastVolume:  "- Tập gần nhất «%s»: %s\n",
	stateMainChars:   "- Nhân vật chính: %s\n",
	stateForeshadow:  "- Manh mối chưa thu: %s\n",
	stateWarnings:    "- Cảnh báo dữ liệu: %s\n",
	stateReadFailed:  "%s đọc thất bại: %v",
	stateRoleParen:   " (%s)",
	stateListSep:     ", ",
	stateItemSep:     "; ",
}

// hostTextsZH là bản tiếng Trung — giữ nguyên văn bản gốc của dự án.
var hostTextsZH = &hostTexts{
	coCreateSystemPrompt:      coCreateSystemPromptZH,
	stageCoCreateSystemPrompt: stageCoCreateSystemPromptZH,
	coCreateProtocolTail:      coCreateProtocolTailZH,

	stageStateTitle: "## 当前故事状态",
	stageStateNote:  "（以下是已写内容的客观摘要，供你规划后续时参照，不要在 <draft> 里照抄原文）",

	stagePlanPrefix: "[阶段规划] 我暂停创作，和共创助手一起梳理了下面的后续方向，请按你的干预分类裁定如何落地，然后继续创作。后续方向如下：\n\n",

	premiseConflictHeading: "核心冲突",

	expandArcTask:        "下一弧为骨架(%s)。调用 save_foundation(type=expand_arc) 展开下一弧;若当前卷已写完,改用 type=append_volume 追加并展开下一卷。",
	interventionOriginal: "\n\n用户原始干预（本次修改授权的唯一来源；上下文只用于理解，不得扩大目标或范围）：\n",

	foundationReadFailed: "基础设定状态读取失败: ",
	progressReadFailed:   "创作进度读取失败: ",

	invalidWriteTarget:   "非法写作目标",
	writerPhaseError:     "writer 仅能在 writing 阶段派发（当前 phase=%s）: %w",
	preMarkChapterFailed: "%w: 预标第 %d 章进行中失败: %w",

	stateBookTitle:   "- 书名：《%s》\n",
	stateProgress:    "- 进度：已完成 %d 章",
	stateRefined:     " / 当前已细化 %d 章（后续按弧动态规划）",
	statePlanned:     " / 规划 %d 章",
	stateWordCount:   "，约 %d 字，下一章为第 %d 章\n",
	statePosition:    "- 当前位置：第 %d 卷 第 %d 弧\n",
	stateEnding:      "- 终局方向：%s\n",
	stateScale:       "- 预估规模：%s\n",
	stateOpenThreads: "- 活跃长线：%s\n",
	stateLastVolume:  "- 最近《%s》：%s\n",
	stateMainChars:   "- 主要人物：%s\n",
	stateForeshadow:  "- 未收伏笔：%s\n",
	stateWarnings:    "- 数据告警：%s\n",
	stateReadFailed:  "%s 读取失败: %v",
	stateRoleParen:   "（%s）",
	stateListSep:     "、",
	stateItemSep:     "；",
}

// hostTextsFor chọn bảng văn bản theo ngôn ngữ tác phẩm.
// internal/host có *store.Store nên nguồn ngôn ngữ là store.Language().
func hostTextsFor(lang string) *hostTexts {
	return i18n.Table(hostTextsVI, hostTextsZH, i18n.Normalize(lang))
}
