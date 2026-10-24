package store

import (
	"fmt"
	"os"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// BookStore quản lý thông tin công khai của tác phẩm; meta/book.json là nguồn sự thật
// duy nhất, còn book.md là bản chiếu cho người đọc.
type BookStore struct{ io *IO }

func NewBookStore(io *IO) *BookStore { return &BookStore{io: io} }

// Load đọc thông tin tác phẩm; trả về nil nếu chưa sinh.
func (s *BookStore) Load() (*domain.BookMetadata, error) {
	var book domain.BookMetadata
	if err := s.io.ReadJSON("meta/book.json", &book); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	book = book.Normalized()
	if err := book.Validate(); err != nil {
		return nil, err
	}
	return &book, nil
}

// Save lưu thông tin tác phẩm đã chuẩn hóa cùng bản chiếu cho người đọc.
func (s *BookStore) Save(book domain.BookMetadata) error {
	book = book.Normalized()
	if err := book.Validate(); err != nil {
		return err
	}
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("meta/book.json", book); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("book.md", renderBook(book, s.io.labels()))
	})
}

func renderBook(book domain.BookMetadata, l mdLabels) string {
	return fmt.Sprintf("# "+l.bookTitleFmt+"\n\n## %s\n\n%s\n", book.Title, l.synopsis, book.Synopsis)
}
