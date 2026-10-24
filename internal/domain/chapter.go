package domain

import (
	"unicode/utf8"
)

// ReviewInterval là khoảng cách giữa các lần thẩm duyệt toàn cục (mỗi N chương).
const ReviewInterval = 5

// ShouldReview dựa trên số chương đã hoàn thành để xác định có cần thẩm duyệt toàn cục
// hay không (chế độ truyện ngắn/trung bình).
func ShouldReview(completedCount int) (bool, string) {
	if completedCount > 0 && completedCount%ReviewInterval == 0 {
		return true, reviewText("completed", completedCount, 0, 0)
	}
	return false, ""
}

// ShouldArcReview trong chế độ truyện dài xác định có cần thẩm duyệt cấp cung/cấp tập
// hay không.
func ShouldArcReview(isArcEnd, isVolumeEnd bool, volume, arc int) (bool, string) {
	if isVolumeEnd {
		return true, reviewText("volume_end", volume, arc, 0)
	}
	if isArcEnd {
		return true, reviewText("arc_end", volume, arc, 0)
	}
	return false, ""
}

// WordCount đếm số chữ theo rune.
func WordCount(content string) int {
	return utf8.RuneCountInString(content)
}
