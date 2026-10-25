package tools

import (
	"fmt"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// EnsureChapterExpanded kiểm tra rằng công việc của chương đang ở giai đoạn
// sáng tác và, với sách phân tầng, nằm trong dàn Ý đang được khai triển.
func EnsureChapterExpanded(st *store.Store, chapter int) error {
	if st == nil {
		return fmt.Errorf("store không được để trống: %w", errs.ErrToolPrecondition)
	}
	if chapter <= 0 {
		return fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}
	progress, err := st.Progress.Load()
	if err != nil {
		return fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	if progress == nil {
		return fmt.Errorf("progress chưa khởi tạo: %w", errs.ErrToolPrecondition)
	}
	if progress.Phase != domain.PhaseWriting {
		return fmt.Errorf("viết chương chỉ được phép ở giai đoạn writing (phase hiện tại=%s): %w", progress.Phase, errs.ErrToolPrecondition)
	}
	if !progress.Layered {
		return nil
	}
	boundary, err := st.Outline.CheckArcBoundary(chapter)
	if err != nil {
		return fmt.Errorf("check layered outline: %w: %w", errs.ErrStoreRead, err)
	}
	if boundary != nil {
		return nil
	}
	return fmt.Errorf(
		"chương %d không nằm trong phạm vi dàn ý phân tầng: trước khi viết phải gọi expand_arc để khai triển cung hoặc append_volume để thêm tập; nếu toàn bộ sách đã hoàn thành thì gọi save_foundation type=complete_book: %w",
		chapter, errs.ErrToolPrecondition)
}
