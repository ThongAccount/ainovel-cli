package tools

import (
	"fmt"

	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/flow"
	"github.com/voocel/ainovel-cli/internal/store"
)

// requireAggregateTarget gắn lần ghi tổng hợp mới của Biên tập viên vào đúng
// hạng mục duy nhất đang chờ Router bổ sung.
// Mục tiêu hoàn toàn suy ra từ sự thật đã ghi xuống đĩa, không phụ thuộc vào
// câu chữ nhiệm vụ và không tin số chương/tập/cung do model tự điền; phần chốt
// idempotent khi nội dung đã giống hệt được từng công cụ nhận diện trước khi
// gọi hàm này.
func requireAggregateTarget(st *store.Store, kind flow.AggregateKind, volume, arc, endChapter int) error {
	state, err := flow.LoadState(st)
	if err != nil {
		return fmt.Errorf("load aggregate state: %w: %w", errs.ErrStoreRead, err)
	}
	due := state.AggregateRefresh
	if due == nil {
		return fmt.Errorf("hiện không có hạng mục %s nào đang chờ xử lý: %w", kind, errs.ErrToolPrecondition)
	}
	targetMismatch := due.Kind != kind
	switch kind {
	case flow.AggregateArcReview, flow.AggregateArcSummary:
		targetMismatch = targetMismatch || due.Volume != volume || due.Arc != arc
	case flow.AggregateVolumeSummary:
		targetMismatch = targetMismatch || due.Volume != volume
	case flow.AggregateGlobalReview:
		// Đánh giá toàn cục không có tọa độ tập/cung, chỉ định vị bằng kind và
		// chương kết thúc.
	}
	endMismatch := endChapter > 0 && due.EndChapter != endChapter
	if targetMismatch || endMismatch {
		return fmt.Errorf(
			"mục tiêu ghi tổng hợp không khớp: hiện đang xử lý kind=%s volume=%d arc=%d end_chapter=%d, nhận được kind=%s volume=%d arc=%d end_chapter=%d: %w",
			due.Kind, due.Volume, due.Arc, due.EndChapter,
			kind, volume, arc, endChapter, errs.ErrToolConflict,
		)
	}
	return nil
}
