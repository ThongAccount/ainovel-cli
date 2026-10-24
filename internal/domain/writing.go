package domain

import "strings"

// ChapterPlan là dàn ý viết chương do Writer tự tạo.
// Không còn ép tách cảnh; Agent tự quyết định cách tổ chức nội dung.
type ChapterPlan struct {
	Chapter    int             `json:"chapter"`
	Title      string          `json:"title"`
	Goal       string          `json:"goal"`
	Conflict   string          `json:"conflict"`
	Hook       string          `json:"hook"`
	EmotionArc string          `json:"emotion_arc,omitempty"`
	Notes      string          `json:"notes,omitempty"` // ghi chú tự do của Agent
	Contract   ChapterContract `json:"contract,omitempty"`
}

// ChapterContract là hợp đồng nghiệm thu chương dùng chung giữa Writer và Editor.
// Nó xác định các bước tiến triển bắt buộc, các bước bị cấm vượt phạm vi và
// những điểm thẩm duyệt cần chú ý.
type ChapterContract struct {
	RequiredBeats    []string `json:"required_beats,omitempty"`    // các bước tiến triển bắt buộc trong chương
	ForbiddenMoves   []string `json:"forbidden_moves,omitempty"`   // các bước tiến triển tuyệt đối không được xảy ra
	ContinuityChecks []string `json:"continuity_checks,omitempty"` // các điểm tính liên tục cần kiểm tra đặc biệt
	EvaluationFocus  []string `json:"evaluation_focus,omitempty"`  // các điểm Editor cần kiểm tra trọng tâm
	EmotionTarget    string   `json:"emotion_target,omitempty"`    // tùy chọn: cảm xúc chính muốn người đọc cảm nhận trong chương
	PayoffPoints     []string `json:"payoff_points,omitempty"`     // tùy chọn: các điểm tình tiết/điểm hoàn lực chương then chốt cần đáp ứng
	HookGoal         string   `json:"hook_goal,omitempty"`         // tùy chọn: điểm móc cuối chương cần thúc đẩy khả năng đọc tiếp
}

// ChapterSummary là tóm tắt chương, dùng cho cửa sổ ngữ cảnh của các chương sau.
type ChapterSummary struct {
	Chapter    int      `json:"chapter"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Characters []string `json:"characters"`
	KeyEvents  []string `json:"key_events"`
}

// ArcSummary là tóm tắt cấp cung, do Editor tạo khi cung kết thúc.
type ArcSummary struct {
	Volume    int      `json:"volume"`
	Arc       int      `json:"arc"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	KeyEvents []string `json:"key_events"`
}

// VolumeSummary là tóm tắt cấp tập, được tạo khi tập kết thúc.
type VolumeSummary struct {
	Volume    int      `json:"volume"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	KeyEvents []string `json:"key_events"`
}

// CharacterSnapshot là ảnh chụp trạng thái nhân vật, được ghi tại ranh giới cung.
type CharacterSnapshot struct {
	Volume     int    `json:"volume"`
	Arc        int    `json:"arc"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Power      string `json:"power,omitempty"`
	Motivation string `json:"motivation"`
	Relations  string `json:"relations,omitempty"`
}

// OutlineFeedback là phản hồi của Writer về dàn ý, tùy chọn khi gửi chương.
type OutlineFeedback struct {
	Deviation  string `json:"deviation"`  // mô tả sai lệch
	Suggestion string `json:"suggestion"` // đề xuất điều chỉnh
}

// WritingStyleRules là các quy tắc viết rút ra từ những chương đã viết, do Editor
// tạo tại ranh giới cung.
// Thay thế các trích đoạn gốc (style_anchors / voice_samples) bằng quy tắc, không
// sao chép nguyên văn.
type WritingStyleRules struct {
	Volume    int              `json:"volume"`
	Arc       int              `json:"arc"`
	Prose     []string         `json:"prose"`      // 3-5 quy tắc văn phong tả, mỗi quy tắc ≤50 chữ
	Dialogue  []CharacterVoice `json:"dialogue"`   // quy tắc văn phong đối thoại của nhân vật
	Taboos    []string         `json:"taboos"`     // danh sách điều cấm
	UpdatedAt string           `json:"updated_at"` // dấu thời gian ISO8601
}

// CharacterVoice là các quy tắc văn phong đối thoại của một nhân vật.
type CharacterVoice struct {
	Name  string   `json:"name"`
	Rules []string `json:"rules"` // 2-3 quy tắc đặc trưng ngôn ngữ, mỗi quy tắc ≤30 chữ
}

// RelatedChapter là chương liên quan được đề xuất đọc lại.
type RelatedChapter struct {
	Chapter int    `json:"chapter"`
	Reason  string `json:"reason"`
}

// RecallItem là thông tin lịch sử dài hạn được gọi lại có chọn lọc theo nhiệm vụ
// hiện tại.
// Nó không thay thế các artifact chính thức; nhiệm vụ duy nhất là chèn lại cho
// model một ít lịch sử thực sự liên quan trong lượt hiện tại.
type RecallItem struct {
	Kind    string `json:"kind"`
	Key     string `json:"key,omitempty"`
	Chapter int    `json:"chapter,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// CommitResult là giá trị trả về có cấu trúc của công cụ commit_chapter.
// Chỉ chứa các trường sự thật; "bước tiếp theo là gì" do kênh Reminder tự sinh từ
// Progress hiện tại.
type CommitResult struct {
	Chapter        int              `json:"chapter"`
	Committed      bool             `json:"committed"`
	WordCount      int              `json:"word_count"`
	NextChapter    int              `json:"next_chapter"`
	ReviewRequired bool             `json:"review_required"`
	ReviewReason   string           `json:"review_reason,omitempty"`
	HookType       string           `json:"hook_type,omitempty"`
	DominantStrand string           `json:"dominant_strand,omitempty"`
	Feedback       *OutlineFeedback `json:"feedback,omitempty"`
	// Tín hiệu truyện dài phân tầng
	ArcEnd         bool `json:"arc_end,omitempty"`
	VolumeEnd      bool `json:"volume_end,omitempty"`
	Volume         int  `json:"volume,omitempty"`
	Arc            int  `json:"arc,omitempty"`
	NeedsExpansion bool `json:"needs_expansion,omitempty"`  // cung kế tiếp là khung, cần triển khai các chương
	NeedsNewVolume bool `json:"needs_new_volume,omitempty"` // cần Architect tạo tập kế tiếp
	NextVolume     int  `json:"next_volume,omitempty"`      // số thứ tự cung/tập kế tiếp
	NextArc        int  `json:"next_arc,omitempty"`         // số thứ tự cung kế tiếp
	// Sự thật trạng thái hoàn thành: sau lần commit này, toàn bộ tác phẩm đã hoàn thành hay chưa
	BookComplete bool `json:"book_complete,omitempty"`
	// Ảnh chụp Progress.Flow hiện tại (writing / reviewing / rewriting / polishing)
	Flow string `json:"flow,omitempty"`
}

// HasDirective kiểm tra dàn ý chương này có thực sự nói rõ "nên viết như thế nào"
// hay không.
//
// Dàn ý rỗng tương đương không có dàn ý: khi làm lại chỉ liệt kê khiếm khuyết mà
// không đưa hướng, Writer vẫn chỉ nhận được câu "viết lại chương N" — thực
// nghiệm cho thấy Editor xếp đây là vấn đề nghiêm trọng nhất trong hàng đợi.
func (p ChapterPlan) HasDirective() bool {
	return strings.TrimSpace(p.Goal) != "" ||
		len(p.Contract.RequiredBeats) > 0 ||
		len(p.Contract.PayoffPoints) > 0 ||
		len(p.Contract.ContinuityChecks) > 0
}
