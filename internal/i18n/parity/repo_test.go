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
