// Package i18n cung cấp lớp chọn ngôn ngữ dùng chung cho toàn bộ văn bản
// nhạy cảm ngôn ngữ trong dự án.
//
// Văn bản mô hình đọc (mô tả công cụ, hợp đồng đầu ra, gợi ý bước tiếp theo)
// phải theo ngôn ngữ tác phẩm. Nếu mô hình đang viết tiếng Việt mà mô tả
// công cụ vẫn bằng tiếng Trung, ngữ cảnh sẽ liên tục kéo mô hình trôi về
// Trung Quốc — cùng lý do mà nhãn Markdown trong internal/store đã theo ngôn
// ngữ tác phẩm (xem internal/store/labels.go).
//
// Văn bản chỉ người dùng đọc (thông báo CLI, dòng log, thông báo lỗi) không
// đi qua i18n: bản port này dùng tiếng Việt cố định cho giao diện vận hành.
package i18n

import (
	"strings"
	"sync/atomic"
)

// Lang là ngôn ngữ tác phẩm.
type Lang string

const (
	// LangVI là tiếng Việt — ngôn ngữ mặc định của bản port.
	LangVI Lang = "vi"
	// LangZH là tiếng Trung.
	LangZH Lang = "zh"
)

// Normalize chuẩn hóa ngôn ngữ tự do về Lang.
// "zh" / "chinese" / "cn" → LangZH; mọi giá trị khác (kể cả rỗng) → LangVI.
//
// Quy tắc này khớp với bootstrap.Config.NormalizedLanguage() và
// assets.LoadWithLanguage(): cấu hình rỗng luôn là tiếng Việt, không bao giờ
// lặng lẽ rơi về tiếng Trung.
func Normalize(s string) Lang {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "zh", "chinese", "cn":
		return LangZH
	default:
		return LangVI
	}
}

// Resolve chọn bản văn bản theo ngôn ngữ tác phẩm.
func (l Lang) Resolve(vi, zh string) string {
	if l == LangZH {
		return zh
	}
	return vi
}

// Table chọn giữa hai bảng văn bản theo ngôn ngữ tác phẩm.
// Dùng cho bảng struct lớn — cùng dạng với mdLabels của internal/store.
func Table[T any](vi, zh T, lang Lang) T {
	if lang == LangZH {
		return zh
	}
	return vi
}

// Text là cặp văn bản song ngữ cho nội dung hướng tới mô hình.
type Text struct {
	VI string
	ZH string
}

// Resolve chọn bản văn bản theo ngôn ngữ tác phẩm.
func (t Text) Resolve(lang Lang) string {
	if lang == LangZH {
		return t.ZH
	}
	return t.VI
}

// active là ngôn ngữ tác phẩm ở cấp tiến trình.
//
// Một lần chạy CLI = một tác phẩm = một ngôn ngữ. Vì vậy các package không
// có đường dẫn tới store (internal/arbiter, internal/llmcontract,
// internal/chapterfacts, internal/stylestat) vẫn cần biết ngôn ngữ đang viết
// để sinh văn bản hướng tới mô hình. Biến tiến trình giữ các package đó
// độc lập với store, nên không phải đổi chữ ký hàm ở biên giữa các package.
//
// Thứ tự ưu tiên khi đọc: store.Language() (nếu package có store) → Active().
var active atomic.Value // Lang

// SetActive đặt ngôn ngữ tác phẩm ở cấp tiến trình.
// internal/store.SetLanguage gọi hàm này, nên vận hành thực tế chỉ cần
// SetLanguage một lần lúc khởi động.
func SetActive(s string) {
	active.Store(Normalize(s))
}

// Active trả về ngôn ngữ tác phẩm ở cấp tiến trình.
// Chưa thiết lập thì mặc định là tiếng Việt.
func Active() Lang {
	if v, ok := active.Load().(Lang); ok {
		return v
	}
	return LangVI
}
