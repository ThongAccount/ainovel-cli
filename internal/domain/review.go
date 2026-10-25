package domain

// TimelineEvent là một sự kiện trên dòng thời gian.
type TimelineEvent struct {
	Chapter    int      `json:"chapter"`
	Time       string   `json:"time"`
	Event      string   `json:"event"`
	Characters []string `json:"characters,omitempty"`
}

// ForeshadowEntry là một mục phục bút.
type ForeshadowEntry struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	PlantedAt   int    `json:"planted_at"`
	Status      string `json:"status"` // planted / advanced / resolved
	ResolvedAt  int    `json:"resolved_at,omitempty"`
}

// ForeshadowUpdate là thao tác tăng dần trên phục bút.
type ForeshadowUpdate struct {
	ID          string `json:"id"`
	Action      string `json:"action"` // plant / advance / resolve
	Description string `json:"description,omitempty"`
}

// RestoreOwnPlants đưa lại các phục bút plant mà bản ghi cũ ghi trong chương này
// nhưng bản ghi mới không khai báo lại vào đầu danh sách.
// Những phục bút một chương đã gieo là sự thật lịch sử của chính chương đó; viết
// lại nội dung không thay đổi điều này. Nếu làm mất, khi phát lại toàn bộ bản ghi
// chương, advance/resolve ở chương này và các chương sau sẽ không tìm thấy plant
// tiền thân và cả chuỗi sẽ báo lỗi.
func RestoreOwnPlants(prev, next []ForeshadowUpdate) []ForeshadowUpdate {
	declared := make(map[string]struct{}, len(next))
	for _, u := range next {
		if u.Action == "plant" {
			declared[u.ID] = struct{}{}
		}
	}
	var restored []ForeshadowUpdate
	for _, u := range prev {
		if u.Action != "plant" {
			continue
		}
		if _, ok := declared[u.ID]; ok {
			continue
		}
		declared[u.ID] = struct{}{}
		restored = append(restored, u)
	}
	if len(restored) == 0 {
		return next
	}
	// plant phải đứng trước advance/resolve của cùng chương để khi phát lại có thể
	// dựng mục trước.
	return append(restored, next...)
}

// RelationshipEntry là một mục quan hệ nhân vật.
type RelationshipEntry struct {
	CharacterA string `json:"character_a"`
	CharacterB string `json:"character_b"`
	Relation   string `json:"relation"`
	Chapter    int    `json:"chapter"`
}

// ConsistencyIssue là một vấn đề nhất quán.
type ConsistencyIssue struct {
	Type           string `json:"type"`     // chiều vấn đề cụ thể do model chỉ ra theo rubric
	Severity       string `json:"severity"` // critical / error / warning
	Description    string `json:"description"`
	Evidence       string `json:"evidence,omitempty"` // bằng chứng: trích đoạn, tình tiết cụ thể hoặc dữ liệu trạng thái
	Suggestion     string `json:"suggestion,omitempty"`
	Chapters       []int  `json:"chapters,omitempty"` // các chương thực sự chứa bằng chứng
	RequiresChange bool   `json:"requires_change"`    // có nên vào hàng đợi làm lại ngay hay không, do Editor phán quyết theo ngữ nghĩa
}

// DimensionScore là điểm thẩm duyệt của một chiều.
type DimensionScore struct {
	Dimension string `json:"dimension"`         // do rubric thẩm duyệt định nghĩa, có thể mở rộng theo nhiệm vụ
	Score     int    `json:"score"`             // 0-100
	Verdict   string `json:"verdict,omitempty"` // tương thích thẩm duyệt cũ; runtime không còn dùng ngưỡng để ghi đè phán quyết của model
	Comment   string `json:"comment,omitempty"` // kết luận ngắn về chiều này
}

// ReviewEntry là một mục thẩm duyệt của Editor.
type ReviewEntry struct {
	Chapter          int                `json:"chapter"`
	Scope            string             `json:"scope"` // chapter / global / arc
	Issues           []ConsistencyIssue `json:"issues"`
	Dimensions       []DimensionScore   `json:"dimensions,omitempty"`      // điểm theo từng chiều
	ContractStatus   string             `json:"contract_status,omitempty"` // met / partial / missed
	ContractMisses   []string           `json:"contract_misses,omitempty"` // các mục contract chưa đạt
	ContractNotes    string             `json:"contract_notes,omitempty"`  // mô tả ngắn mức độ thực hiện contract
	Verdict          string             `json:"verdict"`                   // accept / polish / rewrite
	Summary          string             `json:"summary"`
	AffectedChapters []int              `json:"affected_chapters,omitempty"` // các chương cần viết lại/hoàn thiện
}

// CriticalCount trả về số vấn đề mức critical.
func (r *ReviewEntry) CriticalCount() int {
	n := 0
	for _, issue := range r.Issues {
		if issue.Severity == "critical" {
			n++
		}
	}
	return n
}

// ErrorCount trả về số vấn đề mức error.
func (r *ReviewEntry) ErrorCount() int {
	n := 0
	for _, issue := range r.Issues {
		if issue.Severity == "error" {
			n++
		}
	}
	return n
}

// Dimension trả về điểm của chiều đã chỉ định; trả về nil nếu không tồn tại.
func (r *ReviewEntry) Dimension(name string) *DimensionScore {
	if r == nil {
		return nil
	}
	for i := range r.Dimensions {
		if r.Dimensions[i].Dimension == name {
			return &r.Dimensions[i]
		}
	}
	return nil
}
