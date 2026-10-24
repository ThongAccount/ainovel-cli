// Package exp triển khai khả năng xuất các chương đã hoàn thành.
//
// Đối xứng với imp/: chỉ dùng IO cục bộ, không phụ thuộc LLM và không thay đổi
// trạng thái store. Quá trình xuất có thể chạy đồng thời với Engine (chỉ đọc
// Progress + bản chính của chương), nên thuộc nhóm năng lực ngang.
//
// Hiện hỗ trợ TXT và EPUB.
package exp

import "github.com/voocel/ainovel-cli/internal/store"

// Format định danh định dạng xuất.
type Format string

const (
	// FormatTXT đầu ra văn bản thuần.
	FormatTXT Format = "txt"
	// FormatEPUB container EPUB 3 tiêu chuẩn (zip + xhtml).
	FormatEPUB Format = "epub"
)

// Options điều khiển hành vi xuất. Giá trị zero tương đương "xuất toàn bộ sách
// ra đường dẫn mặc định và báo lỗi nếu tệp đã tồn tại".
//
// Bố cục: «Tên sách» → dấu phân tách tập → nội dung chương. Hai loại dữ liệu nội
// bộ không đưa vào bản xuất: premise (bản thiết kế sáng táo, chứa thông tin nền
// như đối tượng đọc, điểm tiêu thụ cốt lõi và khu vực cấm viết, dành cho tác giả
// và Engine chứ không phải lời dẫn của người đọc); dấu phân tách cung (theo góc
// nhìn của người đọc, cung là cấu trúc nội bộ quá chi tiết). Tên sách và dấu phân
// tách tập luôn được giữ lại.
type Options struct {
	// Format: khi rỗng, định dạng được suy ra từ phần mở rộng của OutPath
	// (.txt → TXT, .epub → EPUB); khi OutPath cũng rỗng thì lùi về FormatTXT.
	// Bên gọi SDK có thể chỉ định rõ để bỏ qua bước suy ra.
	Format Format

	// OutPath là đường dẫn tệp đầu ra; rỗng nghĩa là
	// {novelDir}/{BookMetadata.Title}.{ext}.
	OutPath string

	// From / To là phạm vi chương, gồm hai đầu. 0 nghĩa là từ chương 1 / đến
	// chương cuối cùng. Chương chưa hoàn thành trong phạm vi sẽ bị bỏ qua và ghi
	// vào Result.Skipped, không coi là lỗi.
	From, To int

	// Overwrite cho biết có ghi đè tệp đã tồn tại hay không; mặc định là từ chối.
	Overwrite bool
}

// Deps là các phụ thuộc của Run. Chỉ cần store; quá trình xuất không cần LLM,
// prompt hay bundle.
type Deps struct {
	Store *store.Store
}

// Result là bản tóm tắt sản phẩm của một lần xuất thành công.
type Result struct {
	// Path là đường dẫn tệp thực sự được ghi (tuyệt đối hoặc tương đối do bên gọi truyền vào).
	Path string
	// Chapters là số chương thực sự được ghi.
	Chapters int
	// Bytes là số byte của tệp (UTF-8).
	Bytes int
	// Skipped là các số chương nằm trong phạm vi yêu cầu nhưng chưa hoàn thành.
	Skipped []int
}
