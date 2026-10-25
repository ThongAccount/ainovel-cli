package tools

import (
	"fmt"

	"github.com/voocel/ainovel-cli/internal/chapterfacts"
	"github.com/voocel/ainovel-cli/internal/errs"
)

// validateCommitArgs kiểm tra hợp lệ toàn bộ tải trọng ngữ nghĩa do model gửi
// lên trước khi tạo PendingCommit.
// Lỗi trả thẳng về cho model sửa; không sinh trạng thái nửa vời và không đoán
// giá trị bị thiếu.
func (t *CommitChapterTool) validateCommitArgs(a commitArgs) error {
	if err := chapterfacts.Validate(a.ChapterFacts); err != nil {
		return fmt.Errorf("%v: %w", err, errs.ErrToolArgs)
	}

	if len(a.ForeshadowUpdates) > 0 {
		ledger, err := t.store.World.LoadForeshadowLedger()
		if err != nil {
			return fmt.Errorf("load foreshadow ledger: %w: %w", errs.ErrStoreRead, err)
		}
		// Sổ phục bút là hình chiếu toàn bộ sách, còn Projector phát lại theo
		// thứ tự chương. Khi viết lại một chương sớm, trong sổ vẫn còn những manh
		// mối chỉ được gieo ở các chương sau — nếu cho qua, kiểm tra trước khi
		// nộp sẽ cho kết luận ngược với kết quả phát lại, model không còn cách
		// sửa và hàng chờ làm lại bị kẹt cứng. Vì vậy luôn lấy "nhìn thấy
		// được trong chương này" làm chuẩn.
		plantedAt := make(map[string]int, len(ledger))
		for _, entry := range ledger {
			plantedAt[entry.ID] = entry.PlantedAt
		}
		declared := make(map[string]struct{}, len(a.ForeshadowUpdates))
		for i, update := range a.ForeshadowUpdates {
			switch update.Action {
			case "plant":
				declared[update.ID] = struct{}{}
			case "advance", "resolve":
				if _, ok := declared[update.ID]; ok {
					continue
				}
				at, known := plantedAt[update.ID]
				if !known {
					return fmt.Errorf("foreshadow_updates[%d] references unknown id %q: %w", i, update.ID, errs.ErrToolPrecondition)
				}
				if at > a.Chapter {
					return fmt.Errorf("foreshadow_updates[%d]: manh mối %q được gieo ở chương %d, không thể tiến triển hoặc thu hồi ở chương %d: %w",
						i, update.ID, at, a.Chapter, errs.ErrToolPrecondition)
				}
			}
		}
	}
	return nil
}
