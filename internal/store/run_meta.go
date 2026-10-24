package store

import (
	"fmt"
	"os"
	"time"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// RunMetaStore quản lý siêu dữ liệu lần chạy (mô hình, lịch sử can thiệp, mức quy hoạch...).
type RunMetaStore struct{ io *IO }

func NewRunMetaStore(io *IO) *RunMetaStore { return &RunMetaStore{io: io} }

// Save lưu siêu dữ liệu lần chạy vào meta/run.json.
func (s *RunMetaStore) Save(meta domain.RunMeta) error {
	s.io.mu.Lock()
	defer s.io.mu.Unlock()
	return s.saveUnlocked(meta)
}

// Load đọc siêu dữ liệu lần chạy.
func (s *RunMetaStore) Load() (*domain.RunMeta, error) {
	s.io.mu.RLock()
	defer s.io.mu.RUnlock()
	return s.loadUnlocked()
}

func (s *RunMetaStore) loadUnlocked() (*domain.RunMeta, error) {
	var meta domain.RunMeta
	if err := s.io.ReadJSONUnlocked("meta/run.json", &meta); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &meta, nil
}

func (s *RunMetaStore) saveUnlocked(meta domain.RunMeta) error {
	return s.io.WriteJSONUnlocked("meta/run.json", meta)
}

// Init khởi tạo hoặc cập nhật siêu dữ liệu lần chạy; giữ lại toàn bộ sự thật ý định
// của lần chạy qua các lần khởi động lại —
// PlanStart đặc biệt quan trọng: nếu giai đoạn quy hoạch hỏng (phán quyết khởi động đã
// ghi xuống, artifact foundation đầu tiên chưa ghi xuống), nó là căn cứ duy nhất để
// khôi phục danh tính người lập kế hoạch; nếu Init ghi đè thì việc khôi phục dừng
// ngay lập tức.
func (s *RunMetaStore) Init(style, provider, model string) error {
	return s.io.WithWriteLock(func() error {
		existing, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		meta := domain.RunMeta{
			StartedAt: time.Now().Format(time.RFC3339),
			Provider:  provider,
			Style:     style,
			Model:     model,
		}
		if existing != nil {
			meta.PendingSteer = existing.PendingSteer
			meta.PlanningTier = existing.PlanningTier
			meta.PlanStart = existing.PlanStart
			meta.StartPrompt = existing.StartPrompt
			meta.AdvanceMode = existing.AdvanceMode
			meta.AdvancePermitChapter = existing.AdvancePermitChapter
			meta.AdvanceHold = existing.AdvanceHold
		}
		if meta.AdvanceMode == "" {
			meta.AdvanceMode = domain.ChapterAdvanceAuto
		}
		if err := validateAdvanceControl(meta); err != nil {
			return err
		}
		return s.saveUnlocked(meta)
	})
}

func validateAdvanceControl(meta domain.RunMeta) error {
	if !meta.AdvanceMode.Valid() {
		return &domain.UnsupportedAdvanceModeError{Mode: meta.AdvanceMode}
	}
	if meta.AdvancePermitChapter < 0 {
		return fmt.Errorf("giấy phép chương không được âm: %d", meta.AdvancePermitChapter)
	}
	if meta.AdvanceMode == domain.ChapterAdvanceAuto && meta.AdvancePermitChapter != 0 {
		return fmt.Errorf("chế độ auto không được giữ giấy phép chương: %d", meta.AdvancePermitChapter)
	}
	if meta.AdvanceHold != nil {
		if err := meta.AdvanceHold.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// SetStartPrompt cố định nhu cầu sáng tác gốc của người dùng — sự thật đầu vào,
// được ghi xuống **trước** phán quyết khởi động.
// Khi phán quyết thất bại (ví dụ do lỗi mô hình), nó vẫn còn, nên việc khôi phục /
// tiếp tục có thể phán quyết bù dựa trên đó (engine.planStartFallback); thất bại lúc
// khởi động không còn là ngõ cụt.
func (s *RunMetaStore) SetStartPrompt(prompt string) error {
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil {
			meta = &domain.RunMeta{}
		}
		meta.StartPrompt = prompt
		return s.saveUnlocked(*meta)
	})
}

// SetPendingSteer ghi lại chỉ thị Steer chưa hoàn tất.
func (s *RunMetaStore) SetPendingSteer(input string) error {
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil {
			meta = &domain.RunMeta{}
		}
		meta.PendingSteer = input
		return s.saveUnlocked(*meta)
	})
}

// ClearPendingSteer xóa chỉ thị Steer đã xử lý.
func (s *RunMetaStore) ClearPendingSteer() error {
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil || meta.PendingSteer == "" {
			return nil
		}
		meta.PendingSteer = ""
		return s.saveUnlocked(*meta)
	})
}

// SetAdvanceMode đổi chế độ tiến triển chương. Khi chuyển lại auto, giấy phép chương
// được xóa trong cùng một khóa ghi.
func (s *RunMetaStore) SetAdvanceMode(mode domain.ChapterAdvanceMode) error {
	if !mode.Valid() {
		return &domain.UnsupportedAdvanceModeError{Mode: mode}
	}
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil {
			return fmt.Errorf("run meta chưa được khởi tạo")
		}
		meta.AdvanceMode = mode
		if mode == domain.ChapterAdvanceAuto {
			meta.AdvancePermitChapter = 0
		}
		return s.saveUnlocked(*meta)
	})
}

// GrantAdvancePermit lưu bền một giấy phép cho đúng một chương ở chế độ review.
func (s *RunMetaStore) GrantAdvancePermit(chapter int) error {
	if chapter <= 0 {
		return fmt.Errorf("giấy phép chương phải lớn hơn 0: %d", chapter)
	}
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil {
			return fmt.Errorf("run meta chưa được khởi tạo")
		}
		if meta.AdvanceMode != domain.ChapterAdvanceReview {
			return fmt.Errorf("chỉ chế độ duyệt từng chương mới cấp quyền cho chương kế tiếp (hiện tại %s)", meta.AdvanceMode)
		}
		if meta.AdvancePermitChapter == chapter {
			return nil
		}
		if meta.AdvancePermitChapter != 0 {
			return fmt.Errorf("đã có giấy phép cho chương %d, từ chối ghi đè thành chương %d", meta.AdvancePermitChapter, chapter)
		}
		meta.AdvancePermitChapter = chapter
		return s.saveUnlocked(*meta)
	})
}

// ClearAdvancePermit chỉ tiêu thụ giấy phép chương khớp; nếu mục tiêu không còn thì idempotent.
func (s *RunMetaStore) ClearAdvancePermit(chapter int) error {
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil || meta.AdvancePermitChapter == 0 {
			return nil
		}
		if meta.AdvancePermitChapter != chapter {
			return fmt.Errorf("giấy phép chương đã thay đổi: mong đợi chương %d, thực tế là chương %d", chapter, meta.AdvancePermitChapter)
		}
		meta.AdvancePermitChapter = 0
		return s.saveUnlocked(*meta)
	})
}

// SetAdvanceHold ghi nhận ý định tạm dừng một lần; ý định đang bay không được bị một
// ý định khác ghi đè trong im lặng.
func (s *RunMetaStore) SetAdvanceHold(hold domain.AdvanceHold) error {
	if err := hold.Validate(); err != nil {
		return err
	}
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil {
			return fmt.Errorf("run meta chưa được khởi tạo")
		}
		if meta.AdvanceHold != nil {
			if *meta.AdvanceHold == hold {
				return nil
			}
			return fmt.Errorf("đã có ý định tạm dừng một lần (%s: %s), từ chối ghi đè", meta.AdvanceHold.After, meta.AdvanceHold.Reason)
		}
		meta.AdvanceHold = &hold
		return s.saveUnlocked(*meta)
	})
}

// ClearAdvanceHold chỉ tiêu thụ đúng ý định mà bên gọi vừa đọc; nếu mục tiêu không còn
// thì idempotent.
func (s *RunMetaStore) ClearAdvanceHold(expected domain.AdvanceHold) error {
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil || meta.AdvanceHold == nil {
			return nil
		}
		if *meta.AdvanceHold != expected {
			return fmt.Errorf("ý định tạm dừng một lần đã thay đổi, từ chối xóa nhầm")
		}
		meta.AdvanceHold = nil
		return s.saveUnlocked(*meta)
	})
}

// SetPlanningTier ghi lại mức quy hoạch của tác phẩm hiện tại.
func (s *RunMetaStore) SetPlanningTier(tier domain.PlanningTier) error {
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil {
			meta = &domain.RunMeta{}
		}
		meta.PlanningTier = tier
		return s.saveUnlocked(*meta)
	})
}

// SetPlanStart cố định sự thật phán quyết khởi động (phán quyết được ghi xuống trước,
// sau đó mới thực thi; khôi phục giai đoạn quy hoạch dựa vào đó để chạy tiếp).
func (s *RunMetaStore) SetPlanStart(rec domain.PlanStartRecord) error {
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil {
			meta = &domain.RunMeta{}
		}
		meta.PlanStart = &rec
		return s.saveUnlocked(*meta)
	})
}
