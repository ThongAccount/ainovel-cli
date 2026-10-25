package flow

import (
	"fmt"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// StartsForwardChapter cho biết một chỉ dẫn có bắt đầu chương mới tiến về
// phía trước hay không.
// Nó chỉ đọc sự thật, không quyết định có cho đi hay không; văn bản Task/Reason
// không tham gia phán đoán.
func StartsForwardChapter(inst *Instruction, progress *domain.Progress, pending *domain.PendingCommit) bool {
	if inst == nil || inst.Agent != "writer" || progress == nil || progress.Phase != domain.PhaseWriting {
		return false
	}
	if pending != nil || len(progress.PendingRewrites) > 0 || progress.InProgressChapter > 0 {
		return false
	}
	target := inst.Chapter
	if target == 0 {
		target = progress.NextChapter()
	}
	return target > 0 && target == progress.NextChapter()
}

// AdvanceHoldResolution là kết quả xử lý tạm dừng một lần dưới sự thật hiện tại.
type AdvanceHoldResolution int

const (
	AdvanceHoldKeep AdvanceHoldResolution = iota
	AdvanceHoldConsume
	AdvanceHoldConsumeAndStop
)

// ResolveAdvanceHold phân tích tạm dừng một lần bằng hàm thuần. Điều kiện lạ và
// sự thật thiếu đều báo lỗi tường minh, không được hạ cấp âm thầm thành
// "tiếp tục chạy".
func ResolveAdvanceHold(hold *domain.AdvanceHold, progress *domain.Progress) (AdvanceHoldResolution, error) {
	if hold == nil {
		return AdvanceHoldKeep, nil
	}
	if err := hold.Validate(); err != nil {
		return AdvanceHoldKeep, err
	}
	if progress == nil {
		return AdvanceHoldKeep, fmt.Errorf("thiếu Progress, không phân tích được tạm dừng một lần")
	}
	if progress.Phase == domain.PhaseComplete {
		return AdvanceHoldConsume, nil
	}
	if progress.Phase != domain.PhaseWriting {
		return AdvanceHoldKeep, fmt.Errorf("tạm dừng một lần chỉ áp dụng cho giai đoạn writing/complete (hiện tại %s)", progress.Phase)
	}
	switch hold.After {
	case domain.AdvanceHoldAtBoundary:
		return AdvanceHoldConsumeAndStop, nil
	case domain.AdvanceHoldAfterRewritesDrained:
		if len(progress.PendingRewrites) > 0 {
			return AdvanceHoldKeep, nil
		}
		return AdvanceHoldConsumeAndStop, nil
	case domain.AdvanceHoldAtChapter:
		if progress.LatestCompleted() < hold.TargetChapter {
			return AdvanceHoldKeep, nil
		}
		return AdvanceHoldConsumeAndStop, nil
	default:
		return AdvanceHoldKeep, fmt.Errorf("điều kiện tạm dừng một lần không được hỗ trợ %q", hold.After)
	}
}
