package parity

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRoot tìm thư mục gốc repo (nơi có go.mod) bằng cách đi ngược từ vị trí
// file test hiện tại.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("không lấy được vị trí file test")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("không tìm thấy go.mod từ vị trí file test")
		}
		dir = parent
	}
}

// TestRepoBilingualTablesAreParity là cổng chặn của cả repo: mọi bảng văn
// bản song ngữ trong mã production phải khai báo đủ trường ở CẢ HAI bản và
// dùng cùng định dạng tham số.
//
// Lỗi biên dịch không bắt được thiếu trường: literal struct thiếu một trường
// vẫn hợp lệ với Go, trường đó chỉ rỗng lúc chạy. Chuỗi rỗng lọt vào prompt
// gửi mô hình hoặc vào thông báo người dùng mà không ai hay biết.
func TestRepoBilingualTablesAreParity(t *testing.T) {
	root := repoRoot(t)
	issues, err := Check(root)
	if err != nil {
		t.Fatalf("quét bảng song ngữ: %v", err)
	}
	if len(issues) == 0 {
		return
	}
	var sb strings.Builder
	for _, issue := range issues {
		sb.WriteString("\n  " + issue.String())
	}
	t.Errorf("bảng văn bản song ngữ không khớp (%d sai lệch):%s", len(issues), sb.String())
}

// TestRepoHasBilingualTables bảo đảm bước kiểm tra không "xanh giả": nếu
// không còn bảng song ngữ nào thì bước kiểm tra kia không bảo vệ được gì.
func TestRepoHasBilingualTables(t *testing.T) {
	root := repoRoot(t)
	files, err := goFiles(root)
	if err != nil {
		t.Fatalf("dò file Go: %v", err)
	}
	count := 0
	pairs := 0
	for _, file := range files {
		_, tables, filePairs, err := parseFile(file)
		if err != nil {
			t.Fatalf("đọc %s: %v", file, err)
		}
		count += len(tables)
		pairs += len(filePairs)
	}
	if count < 10 {
		t.Fatalf("chỉ tìm thấy %d bản bảng văn bản song ngữ, có vẻ bộ dò quét đã hỏng", count)
	}
	if pairs < 1 {
		t.Fatalf("không tìm thấy cặp i18n.Text nào, có vẻ bộ dò quét đã hỏng")
	}
	t.Logf("tìm thấy %d bản bảng văn bản song ngữ và %d cặp i18n.Text trong %d file",
		count, pairs, len(files))
}

// TestRepoZHHalvesKeepChinese là cổng chặn nhánh ZH của cả repo.
//
// Lỗi mà hai cổng cũ đều bỏ lọt: cổng CJK không thấy vì nhánh ZH đã hết
// chữ Trung, cổng song ngữ không thấy vì tập trường vẫn khớp. Khi một
// bảng "tiếng Trung" bị dịch hết sang tiếng Việt mà vẫn giữ tên xxxZH, công
// cụ không có cách nào phát hiện — đó là lỗ hổng của việc chỉ dựa vào hình
// thức câu lệnh.
//
// Quy tắc: nhánh ZH của mỗi bảng "...ZH" và mỗi cặp i18n.Text phải còn ít
// nhất một ký tự CJK. Kiểm tra theo cả bảng, không theo từng mục: một mục
// ASCII thuần (ví dụ "%d") là hợp lệ. Bảng "...ZH" không cần có bảng "...VI"
// đi kèm — nếu đòi thì chỉ cần đổi tên bản VI là nhánh ZH rơi khỏi phạm vi
// kiểm tra và cổng tự tắt trong im lặng.
func TestRepoZHHalvesKeepChinese(t *testing.T) {
	root := repoRoot(t)
	report, err := CheckZH(root)
	if err != nil {
		t.Fatalf("quét nhánh ZH: %v", err)
	}

	// Chặn "xanh giả": nếu không còn gì để kiểm tra thì cổng này không bảo vệ
	// được gì, và phải báo ngay thay vì im lặng.
	if report.Tables < 10 {
		t.Fatalf("chỉ kiểm tra được %d bản ZH, có vẻ bộ dò quét đã hỏng", report.Tables)
	}
	if report.Pairs < 1 {
		t.Fatalf("không kiểm tra được cặp i18n.Text nào, có vẻ bộ dò quét đã hỏng")
	}

	violations := report.Violations()
	if len(violations) > 0 {
		var sb strings.Builder
		for _, finding := range violations {
			sb.WriteString("\n  " + finding.String())
		}
		t.Errorf("nhánh ZH mất ký tự CJK (%d sai lệch):%s", len(violations), sb.String())
	}

	// Không đọc được thì cũng phải đỏ: im lặng ở đây là để lỗi đi qua.
	unknown := report.Unknown()
	if len(unknown) > 0 {
		var sb strings.Builder
		for _, finding := range unknown {
			sb.WriteString("\n  " + finding.String())
		}
		t.Errorf("không đọc được nội dung nhánh ZH (%d trường hợp), cần người kiểm tra:%s",
			len(unknown), sb.String())
	}

	t.Logf("đã kiểm tra %d bản ZH và %d cặp i18n.Text: %d vi phạm, %d không xác định",
		report.Tables, report.Pairs, len(violations), len(unknown))
}
