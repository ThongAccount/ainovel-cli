package rules

// Package rules hiện thực tầng đầu vào của tùy chọn người dùng (Policy): chuẩn hóa
// và hợp nhất quy tắc viết từ nhiều nguồn thành ảnh chụp của tác phẩm hiện tại
// (xem snapshot.go); khi chạy, novel_context chèn ảnh chụp này và commit_chapter
// kiểm tra nó theo cơ chế.
//
// Rule là loại sự thật thứ tư, đứng ngang hàng với Progress / Checkpoint / Artifact,
// nhưng có tính chất ngược lại: ba loại trước là đầu ra của hệ thống, còn Rule là
// đầu vào được lưu bền của ý định người dùng.
//
// Ràng buộc thiết kế (không thể nhượng bộ):
//   - Công cụ chỉ trả về sự thật, không trả về chỉ thị (Violation là sự thật,
//     để editor quyết định có kích hoạt viết lại hay không)
//   - Không thêm đường verdict mới (dùng lại PendingRewrites)
//   - Không thêm trường mức độ nghiêm trọng (severity được ánh xạ cố định theo loại
//     quy tắc, editor tự phán quyết theo ngữ nghĩa)
//   - Không thay đổi Flow Router (rule không tham gia định tuyến)

// SourceKind đánh dấu nguồn của tệp quy tắc, chỉ dùng để tạo nhãn nguồn
// (ví dụ global:my-style.md).
type SourceKind int

const (
	// SourceGlobal — tùy chọn toàn cục của người dùng (mọi tệp .md trong
	// ~/.ainovel/rules/, hợp nhất theo thứ tự tên tệp), dùng lại được cho nhiều tác phẩm.
	SourceGlobal SourceKind = iota
	// SourceProject — quy tắc của tác phẩm hiện tại (mọi tệp .md trong
	// ./.ainovel/rules/, hợp nhất theo thứ tự tên tệp), có ưu tiên cao nhất.
	SourceProject
)

// String trả về tên dễ đọc của nguồn, dùng làm tiền tố cho nhãn nguồn.
func (k SourceKind) String() string {
	switch k {
	case SourceGlobal:
		return "global"
	case SourceProject:
		return "project"
	default:
		return "unknown"
	}
}

// Structured chứa các trường quy tắc có cấu trúc để kiểm tra bằng cơ chế
// (ứng viên hoặc kết quả hợp nhất sau khi chuẩn hóa mọi nguồn).
// Số chữ của một chương cố ý không nằm ở đây: độ dài hợp lý của một chương là
// vấn đề tính toàn vẹn văn bản và thuộc phạm vi quyết định ngữ nghĩa của
// writer/editor. Biến nó thành một ranh giới cứng số có thể khiến model thêm
// chữ để vượt ngưỡng; ý muốn về độ dài nên đi qua kênh preferences bằng ngôn ngữ tự nhiên.
type Structured struct {
	Genre            string         `json:"genre,omitempty"`
	ForbiddenChars   []string       `json:"forbidden_chars,omitempty"`
	ForbiddenPhrases []string       `json:"forbidden_phrases,omitempty"`
	FatigueWords     map[string]int `json:"fatigue_words,omitempty"`
}

// IsEmpty dùng để xác định có hoàn toàn không có quy tắc có cấu trúc hay không;
// checker có thể bỏ qua khi kết quả là true.
func (s Structured) IsEmpty() bool {
	return s.Genre == "" &&
		len(s.ForbiddenChars) == 0 &&
		len(s.ForbiddenPhrases) == 0 &&
		len(s.FatigueWords) == 0
}

// Severity đánh dấu mức độ nghiêm trọng của Violation.
// Ánh xạ cố định (người dùng không thể cấu hình):
//
//	forbidden_chars xuất hiện       -> Error
//	forbidden_phrases xuất hiện     -> Error
//	fatigue_words vượt ngưỡng       -> Warning
type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Violation là đầu ra của checker: một sự thật rằng chương này đã vi phạm
// một quy tắc cơ chế.
//
// Lưu ý: commit_chapter truyền violations vào JSON trả về nhưng không chặn commit;
// khi thẩm duyệt, editor ánh xạ các sự thật này sang bảy chiều hiện có
// (aesthetic/pacing/character/consistency), rồi LLM tự quyết định có nâng verdict
// để kích hoạt polish/rewrite hay không.
type Violation struct {
	Rule     string   `json:"rule"`             // forbidden_chars / forbidden_phrases / fatigue_words
	Target   string   `json:"target,omitempty"` // đối tượng vi phạm cụ thể (từ/ký tự nào)
	Limit    any      `json:"limit,omitempty"`  // ngưỡng; fatigue_words=int / forbidden_*=rỗng
	Actual   any      `json:"actual"`           // giá trị thực tế: số lần xuất hiện
	Severity Severity `json:"severity"`         // error / warning
}
