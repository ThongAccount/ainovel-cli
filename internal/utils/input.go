package utils

import (
	"strings"
	"unicode"
)

// CleanInputText loại bỏ các ký tự điều khiển không mang ý nghĩa nghiệp vụ trong dữ liệu nhập từ
// terminal, giữ lại phần chữ người dùng nhìn thấy.
// Ở ngữ cảnh nhập một dòng, ký tự xuống dòng và tab trong văn bản dán sẽ được quy về dấu cách.
func CleanInputText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// CleanInputLine làm sạch dữ liệu nhập thủ công một dòng và bỏ khoảng trắng ở hai đầu.
func CleanInputLine(s string) string {
	return strings.TrimSpace(CleanInputText(s))
}

func CleanInputRunes(runes []rune) string {
	var b strings.Builder
	for _, r := range runes {
		if r == '\n' || r == '\r' || r == '\t' {
			b.WriteByte(' ')
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func ContainsControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
