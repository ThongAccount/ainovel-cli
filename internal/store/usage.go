package store

import (
	"os"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// UsageStore lưu bền mức dùng token / cost tích lũy vào meta/usage.json.
// Việc ghi đi qua atomic write của IO (tmp + rename); đường Save ghi đè toàn bộ state.
type UsageStore struct{ io *IO }

func NewUsageStore(io *IO) *UsageStore { return &UsageStore{io: io} }

// Load đọc usage.json. Nếu tệp không tồn tại hoặc phiên bản schema không khớp thì
// trả về (nil, nil), để bên gọi quyết định có phát lại session để điền một lần hay không.
func (s *UsageStore) Load() (*domain.UsageState, error) {
	var state domain.UsageState
	if err := s.io.ReadJSON("meta/usage.json", &state); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if state.Schema != domain.UsageSchemaVersion {
		return nil, nil
	}
	return &state, nil
}

// Save ghi đè toàn bộ state xuống. Bên gọi chịu trách nhiệm debounce / giới hạn tần suất.
func (s *UsageStore) Save(state domain.UsageState) error {
	state.Schema = domain.UsageSchemaVersion
	return s.io.WriteJSON("meta/usage.json", state)
}
