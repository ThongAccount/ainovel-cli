package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

func TestBookStorePersistsCanonicalDataAndReadableProjection(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Book.Save(domain.BookMetadata{Title: " Đêm dài sẽ sáng ", Synopsis: " Thiếu niên giữ lấy ngọn đèn cuối cùng. "}); err != nil {
		t.Fatal(err)
	}
	book, err := s.Book.Load()
	if err != nil {
		t.Fatal(err)
	}
	if book.Title != "Đêm dài sẽ sáng" || book.Synopsis != "Thiếu niên giữ lấy ngọn đèn cuối cùng." {
		t.Fatalf("unexpected book metadata: %+v", book)
	}
	projection, err := os.ReadFile(filepath.Join(dir, "book.md"))
	if err != nil {
		t.Fatal(err)
	}
	// store chưa đặt ngôn ngữ tường minh → fork này mặc định tiếng Việt, nhãn của Markdown
	// dẫn xuất đi theo labelsVI.
	if text := string(projection); !strings.Contains(text, "# Đêm dài sẽ sáng") ||
		!strings.Contains(text, "## Giới thiệu") ||
		!strings.Contains(text, "Thiếu niên giữ lấy ngọn đèn cuối cùng.") {
		t.Fatalf("unexpected book projection: %s", text)
	}
	if err := s.Book.Save(domain.BookMetadata{Title: "Tóm tắt rỗng"}); err == nil {
		t.Fatal("empty synopsis must be rejected")
	}
}
