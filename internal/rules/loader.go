package rules

import (
	"os"
	"path/filepath"
)

// LoadOptions liệt kê các thư mục nguồn của tệp rules để RawFileSources quét và
// chuẩn hóa.
//
// Không có thư mục không phải lỗi; bước quét sẽ bỏ qua âm thầm.
type LoadOptions struct {
	// HomeRulesDir là thư mục ~/.ainovel/rules/; quét mọi tệp .md ở cấp cao nhất
	// (hợp nhất theo thứ tự tên tệp). Giá trị rỗng nghĩa là bỏ qua.
	HomeRulesDir string

	// ProjectRulesDir là thư mục ./.ainovel/rules/ (đối xứng với thư mục toàn cục,
	// cũng quét mọi tệp .md ở cấp cao nhất). Giá trị rỗng nghĩa là bỏ qua.
	ProjectRulesDir string
}

// ainovelDirName là tên dotdir dùng chung cho ainovel ở cấp user và project.
// Nhờ đó ~/.ainovel/rules/ và ./.ainovel/rules/ đối xứng nhau.
const ainovelDirName = ".ainovel"

// DefaultProjectRulesDir tạo đường dẫn tuyệt đối ./.ainovel/rules/ dựa trên
// thư mục dự án đã cho.
// Bên gọi truyền thư mục gốc để loader không phụ thuộc cwd; đối xứng với
// DefaultHomeRulesDir.
func DefaultProjectRulesDir(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	return filepath.Join(projectDir, ainovelDirName, "rules")
}

// DefaultHomeRulesDir tạo đường dẫn tuyệt đối ~/.ainovel/rules/.
// Trả về chuỗi rỗng nếu không phân giải được home (bên gọi dùng giá trị này để bỏ qua nguồn).
func DefaultHomeRulesDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ainovelDirName, "rules")
}

// homeRulesReadme là hướng dẫn được ghi vào ~/.ainovel/rules/README.txt lần đầu.
// Cố ý dùng phần mở rộng .txt thay vì .md: bước quét chỉ nhận .md nên hướng dẫn này
// không bị coi là quy tắc cần chuẩn hóa.
const homeRulesReadme = `Đặt tùy chọn viết toàn cục vào đây; chúng áp dụng cho mọi tác phẩm.

Tạo một tệp .md (ví dụ my-style.md) và viết yêu cầu bằng ngôn ngữ tự nhiên —
không cần định dạng, không cần YAML:

    # Vai trò
    - Đừng viết nhân vật chính thành người quá hiền; lạnh bên ngoài và ấm bên trong là được
    # Văn phong
    - Dùng nhiều cảm giác thân xác (khớp ngón tay trắng bệch) thay cho nhãn cảm xúc (lo lắng)
    - Đối thoại đừng quá văn viết, mỗi chương khoảng 3000 chữ
    - Không nên xuất hiện câu kiểu AI như "某种程度上"

Xong thì không cần quan tâm định dạng: hệ thống sẽ dùng model để chuẩn hóa các yêu cầu
bằng ngôn ngữ tự nhiên này thành ràng buộc có cấu trúc (phạm vi số chữ, từ cấm, ngưỡng
từ mệt mỏi, v.v.), tự động tuân thủ khi viết và tự kiểm tra khi gửi.

Nhiều tệp .md được hợp nhất theo thứ tự tên tệp; tệp ẩn bắt đầu bằng dấu chấm và tệp
không phải .md đều bị bỏ qua (vì vậy README.txt này không bị coi là quy tắc).

Cơ sở cơ chế cho các câu khuôn mẫu AI thường gặp và từ mệt mỏi đã được tích hợp,
dùng ngay mà không cần cấu hình.

Thứ tự ưu tiên tải (cao → thấp): ./.ainovel/rules/*.md (tác phẩm này) >
~/.ainovel/rules/*.md (tại đây) > mặc định tích hợp
`

// EnsureHomeRulesDir cố gắng tạo thư mục ~/.ainovel/rules/ và ghi hướng dẫn
// README.txt để người dùng biết về điểm mở rộng tùy chọn toàn cục và cách viết.
// Đây là tiện ích phụ, không nằm trên đường chính: lỗi phân giải home hoặc lỗi ghi
// đều bị nuốt âm thầm và tuyệt đối không chặn quá trình khởi động.
func EnsureHomeRulesDir() {
	if dir := DefaultHomeRulesDir(); dir != "" {
		_ = ensureRulesDirAt(dir)
	}
}

// ensureRulesDirAt tạo thư mục và ghi README.txt bằng mẫu hướng dẫn hiện tại;
// đây là lõi có thể kiểm thử của EnsureHomeRulesDir.
// README.txt do hệ thống sinh ra (tùy chọn người dùng nằm trong *.md và tệp này
// không được quét), nên mỗi lần đều bị ghi đè bằng mẫu mới nhất; không giữ nội dung
// cũ nên không cần logic tương thích phiên bản nào.
func ensureRulesDirAt(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "README.txt"), []byte(homeRulesReadme), 0o644)
}

// DefaultOptions tạo bộ LoadOptions thông dụng từ thư mục làm việc hiện tại.
//
// Phù hợp để Host gọi một lần khi khởi động, để dịch vụ quy tắc người dùng dùng lại
// cùng cấu hình nguồn. Nếu phân giải cwd thất bại thì ProjectRulesDir để rỗng
// (bước quét sẽ bỏ qua nguồn này).
//
// Ngữ nghĩa đường dẫn: ProjectRulesDir gắn với **thư mục làm việc hiện tại (cwd)**,
// không phải outputDir. Khi người dùng cd sang thư mục khác để viết tác phẩm khác,
// ./.ainovel/rules/ cũng đi theo cwd; nếu cần chia sẻ giữa nhiều tác phẩm, hãy đặt
// tệp trong thư mục toàn cục ~/.ainovel/rules/ (mọi tệp .md bên trong đều được tải).
func DefaultOptions() LoadOptions {
	cwd, _ := os.Getwd()
	return LoadOptions{
		HomeRulesDir:    DefaultHomeRulesDir(),
		ProjectRulesDir: DefaultProjectRulesDir(cwd),
	}
}
