package store

import (
	"os"

	"github.com/voocel/ainovel-cli/internal/rules"
)

// UserRulesStore quản lý ảnh chụp quy tắc người dùng đã chuẩn hóa cho tác phẩm này
// (meta/user_rules.json).
//
// Nguồn sự thật duy nhất lúc chạy: cả việc tiêm vào novel_context và kiểm tra ở
// commit_chapter đều chỉ đọc bản này, không đọc lại tệp rules nhiều lần (tránh trôi
// dữ liệu và hai bên đọc lệch nhau). Ảnh chụp được sinh khi chuẩn hóa lúc mở sách /
// nhập / làm mới.
type UserRulesStore struct{ io *IO }

func NewUserRulesStore(io *IO) *UserRulesStore { return &UserRulesStore{io: io} }

// Load đọc meta/user_rules.json. Trả về nil nếu không tồn tại (bên gọi tự sinh theo yêu cầu).
func (s *UserRulesStore) Load() (*rules.Snapshot, error) {
	s.io.mu.RLock()
	defer s.io.mu.RUnlock()
	var snap rules.Snapshot
	if err := s.io.ReadJSONUnlocked("meta/user_rules.json", &snap); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &snap, nil
}

// Save lưu ảnh chụp.
func (s *UserRulesStore) Save(snap *rules.Snapshot) error {
	s.io.mu.Lock()
	defer s.io.mu.Unlock()
	return s.io.WriteJSONUnlocked("meta/user_rules.json", snap)
}
