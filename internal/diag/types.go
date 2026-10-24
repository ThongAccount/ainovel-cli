package diag

// Severity thể hiện mức độ nghiêm trọng của một phát hiện.
type Severity string

const (
	SevCritical Severity = "critical" // chặn tiến độ hoặc làm hỏng dữ liệu
	SevWarning  Severity = "warning"  // có thể làm giảm chất lượng hoặc lãng phí token
	SevInfo     Severity = "info"     // mục có thể tối ưu
)

// Category nhóm các phát hiện theo chiều.
type Category string

const (
	CatFlow     Category = "flow"     // quy trình bị nghẽn, trạng thái bất thường, vấn đề khôi phục
	CatQuality  Category = "quality"  // điểm thẩm duyệt, mức độ thực hiện hợp đồng, tính nhất quán
	CatPlanning Category = "planning" // thiếu sót đề cương, phục bút trôi, la bàn lỗi thời
	CatContext  Category = "context"  // bất thường về nhân vật / dòng thời gian / quan hệ
)

// Confidence thể hiện độ tin cậy của phán quyết từ quy tắc.
type Confidence string

const (
	ConfHigh   Confidence = "high"   // chắc chắn, đáng tin
	ConfMedium Confidence = "medium" // suy luận theo kinh nghiệm, có thể sai
	ConfLow    Confidence = "low"    // tín hiệu thô, chỉ để tham khảo
)

// AutoLevel cho biết một Finding có thể chuyển thành hành động tự động hay không.
type AutoLevel string

const (
	AutoNone    AutoLevel = "none"    // chỉ báo cáo, không tự động
	AutoSuggest AutoLevel = "suggest" // đề xuất hành động nhưng cần người xác nhận
	AutoSafe    AutoLevel = "safe"    // có thể tự động thực hiện an toàn
)

// Finding là một kết quả chẩn đoán có thể hành động.
type Finding struct {
	Rule       string     // tên quy tắc, ví dụ "StaleForeshadow"
	Category   Category   // nhóm
	Severity   Severity   // mức độ nghiêm trọng
	Confidence Confidence // độ tin cậy của phán quyết
	AutoLevel  AutoLevel  // mức tự động hóa
	Target     string     // phạm vi tác động đề xuất, ví dụ "runtime.flow"
	Title      string     // tóm tắt một dòng
	Evidence   string     // bằng chứng dữ liệu cụ thể
	Suggestion string     // đề xuất cải thiện (hướng tới prompt/flow/config)
}

// RuleFunc là chữ ký thống nhất của các quy tắc chẩn đoán.
type RuleFunc func(snap *Snapshot) []Finding

// ActionKind thể hiện loại hành động chẩn đoán.
type ActionKind string

const (
	ActionEmitNotice      ActionKind = "emit_notice"       // phát thông báo hệ thống
	ActionEnqueueFollowUp ActionKind = "enqueue_follow_up" // sinh đề xuất xử lý tiếp theo
)

// Action là hành động có thể thực thi do Planner sinh ra từ Finding độ tin cậy cao.
type Action struct {
	SourceRule  string     // tên quy tắc nguồn
	Kind        ActionKind // loại hành động
	Severity    Severity   // kế thừa từ Finding
	Summary     string     // mô tả ngắn
	Message     string     // thông điệp truyền cho luồng điều khiển
	Fingerprint string     // dấu vân tay ổn định của Finding nguồn, dùng để khử trùng lặp lúc chạy
}

// Stats là các chỉ số tổng quan hiển thị song song với phát hiện.
type Stats struct {
	CompletedChapters int
	TotalChapters     int
	TotalWords        int
	AvgWordsPerCh     int
	Phase             string
	Flow              string
	PlanningTier      string
	ReviewCount       int
	RewriteCount      int
	AvgReviewScore    float64
	ForeshadowOpen    int
	ForeshadowStale   int
}

// Report là toàn bộ kết quả của một lần chẩn đoán.
type Report struct {
	Stats    Stats
	Findings []Finding
	Actions  []Action
}
