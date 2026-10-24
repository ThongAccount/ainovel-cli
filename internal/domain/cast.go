package domain

// CastEntry là một bản ghi nhân vật phụ trong danh mục nhân vật phụ.
//
// Được tách khỏi Character (characters.json, hồ sơ lõi do Architect duy trì):
//   - CastEntry do công cụ commit_chapter tự động tích lũy, ghi lại "những nhân vật
//     phụ có tên đã xuất hiện"
//   - Character do Architect thiết kế rõ ràng, ghi lại nhân vật chính và nhân vật
//     phụ trọng yếu cùng cung nhân vật/traits/tier
//
// Khi trùng tên, Character là chuẩn (nhân vật lõi không vào cast_ledger), tránh lặp.
type CastEntry struct {
	Name string `json:"name"`
	// Aliases hiện chưa có đường ghi; dành cho công cụ "hợp nhất bí danh theo steer
	// của người dùng" trong tương lai (ví dụ khai báo 'Lý Chủ Trang' và 'Lão Lý'
	// là cùng một người). MergeAppearances đã hỗ trợ tra cứu theo bí danh.
	Aliases          []string `json:"aliases,omitempty"`
	BriefRole        string   `json:"brief_role,omitempty"` // định vị bằng một câu (Writer điền lần xuất hiện đầu, có thể bổ sung sau; không bị ghi đè)
	FirstSeenChapter int      `json:"first_seen_chapter"`
	LastSeenChapter  int      `json:"last_seen_chapter"`
	// AppearanceCount được suy ra từ len(AppearanceChapters) và giữ đồng bộ khi merge.
	// Giữ trường tường minh để UI/JSON đọc trực tiếp, không phải tính lại mỗi lần.
	AppearanceCount    int   `json:"appearance_count"`
	AppearanceChapters []int `json:"appearance_chapters"`
	// Promoted đánh dấu mục này đã được nâng lên characters.json. RecentActive bỏ qua
	// các mục này để tránh gọi lại trùng với hồ sơ lõi. Hiện chưa triển khai đường
	// nâng cấp; trường là hook dự phòng.
	Promoted bool `json:"promoted,omitempty"`
}

// CastIntro là phần giới thiệu ngắn về nhân vật mới xuất hiện do Writer khai báo khi
// commit_chapter.
// Chỉ được dùng khi tên xuất hiện lần đầu hoặc BriefRole trong ledger vẫn còn rỗng.
type CastIntro struct {
	Name      string `json:"name"`
	BriefRole string `json:"brief_role"`
}
