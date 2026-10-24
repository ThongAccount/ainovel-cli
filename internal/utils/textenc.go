package utils

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// DecodeText giải mã byte của tệp văn bản do người dùng cung cấp thành UTF-8: gặp byte không hợp lệ
// theo UTF-8 thì chuyển mã theo GB18030
// (siêu tập của GBK) — rất nhiều tệp txt tiểu thuyết Trung Quốc lan truyền trên mạng dùng mã GBK,
// đọc thẳng như UTF-8 sẽ thành toàn ký tự hỏng. Dãy byte không thuộc GBK sẽ bị bộ giải mã thay
// bằng U+FFFD (vốn đã hỏng, lời gọi sẽ dựa vào lỗi dự phòng khi không khớp để hướng dẫn người dùng).
// Cuối cùng loại bỏ BOM của UTF-8 (nếu không thì khớp ở đầu dòng sẽ mang theo nó).
func DecodeText(data []byte) string {
	if !utf8.Valid(data) {
		if decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data); err == nil {
			data = decoded
		}
	}
	return strings.TrimPrefix(string(data), "\uFEFF")
}
