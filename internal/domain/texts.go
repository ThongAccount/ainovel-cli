package domain

import (
	"fmt"

	"github.com/voocel/ainovel-cli/internal/i18n"
)

// reviewText trả về lý do kích hoạt thẩm duyệt theo ngôn ngữ của tác phẩm.
// Lý do này đi vào JSON kết quả commit_chapter và các chỉ dẫn cho model, nên phải
// song ngữ thay vì cố định một ngôn ngữ.
func reviewText(kind string, volume, arc, completed int) string {
	switch kind {
	case "completed":
		return i18n.Text{
			VI: fmt.Sprintf("Đã hoàn thành %d chương, kích hoạt thẩm duyệt toàn cục", completed),
			ZH: fmt.Sprintf("已完成 %d 章，触发全局审阅", completed),
		}.Resolve(i18n.Active())
	case "volume_end":
		return i18n.Text{
			VI: fmt.Sprintf("Tập %d, cung %d kết thúc (kết thúc tập), kích hoạt thẩm duyệt cấp cung và cấp tập", volume, arc),
			ZH: fmt.Sprintf("第 %d 卷第 %d 弧结束（卷结束），触发弧级+卷级评审", volume, arc),
		}.Resolve(i18n.Active())
	case "arc_end":
		return i18n.Text{
			VI: fmt.Sprintf("Tập %d, cung %d kết thúc, kích hoạt thẩm duyệt cấp cung", volume, arc),
			ZH: fmt.Sprintf("第 %d 卷第 %d 弧结束，触发弧级评审", volume, arc),
		}.Resolve(i18n.Active())
	default:
		return ""
	}
}
