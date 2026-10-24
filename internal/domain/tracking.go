package domain

// StateChange ghi lại thay đổi trạng thái của nhân vật/thực thể.
type StateChange struct {
	Chapter  int    `json:"chapter"`
	Entity   string `json:"entity"`              // tên nhân vật hoặc thực thể
	Field    string `json:"field"`               // thuộc tính thay đổi: realm/location/status/power/relation, v.v.
	OldValue string `json:"old_value,omitempty"` // giá trị trước (có thể rỗng ở lần xuất hiện đầu)
	NewValue string `json:"new_value"`           // giá trị sau
	Reason   string `json:"reason,omitempty"`    // lý do thay đổi
}
