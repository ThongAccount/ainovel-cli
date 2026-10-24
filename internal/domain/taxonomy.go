package domain

import "slices"

var (
	hookTypes       = []string{"crisis", "mystery", "desire", "emotion", "choice"}
	dominantStrands = []string{"quest", "fire", "constellation"}
)

// HookTypes trả về phân loại điểm móc chương. Hàm trả về bản sao để bên gọi không
// thể sửa bảng từ vựng của domain.
func HookTypes() []string { return slices.Clone(hookTypes) }

// DominantStrands trả về phân loại tuyến tình tiết chi phối của chương.
func DominantStrands() []string { return slices.Clone(dominantStrands) }

func ValidHookType(value string) bool       { return slices.Contains(hookTypes, value) }
func ValidDominantStrand(value string) bool { return slices.Contains(dominantStrands, value) }
