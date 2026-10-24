package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/voocel/ainovel-cli/internal/i18n"
)

// IO bọc các thao tác đọc ghi hệ thống tệp, cung cấp khóa và ghi nguyên tử.
// Mỗi kho con giữ một thể hiện IO riêng với sync.RWMutex riêng.
type IO struct {
	dir  string
	lang string
	mu   sync.RWMutex
}

// SetLanguage đặt ngôn ngữ tác phẩm ("vi" / "zh"), ảnh hưởng nhãn của các bản
// Markdown dẫn xuất.
// Gọi một lần lúc khởi động; giá trị rỗng dùng mặc định tiếng Việt của fork này.
func (io *IO) SetLanguage(lang string) { io.lang = string(i18n.Normalize(lang)) }

// Language trả về dạng chuẩn của ngôn ngữ tác phẩm hiện tại ("vi" / "zh").
// Nếu chưa đặt tường minh thì trả về "vi" — fork này mặc định là tiếng Việt.
func (io *IO) Language() string { return string(i18n.Normalize(io.lang)) }

func (io *IO) labels() mdLabels { return labelsFor(io.lang) }

func newIO(dir string) *IO {
	return &IO{dir: dir}
}

func (io *IO) path(rel string) string {
	return filepath.Join(io.dir, rel)
}

func (io *IO) ReadFile(rel string) ([]byte, error) {
	io.mu.RLock()
	defer io.mu.RUnlock()
	return io.ReadFileUnlocked(rel)
}

func (io *IO) ReadFileUnlocked(rel string) ([]byte, error) {
	return os.ReadFile(io.path(rel))
}

func (io *IO) WriteFileUnlocked(rel string, data []byte) error {
	p := io.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, p)
}

func (io *IO) ReadJSON(rel string, v any) error {
	io.mu.RLock()
	defer io.mu.RUnlock()
	return io.ReadJSONUnlocked(rel, v)
}

func (io *IO) ReadJSONUnlocked(rel string, v any) error {
	data, err := io.ReadFileUnlocked(rel)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func (io *IO) WriteJSON(rel string, v any) error {
	io.mu.Lock()
	defer io.mu.Unlock()
	return io.WriteJSONUnlocked(rel, v)
}

func (io *IO) WriteJSONUnlocked(rel string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return io.WriteFileUnlocked(rel, data)
}

func (io *IO) WriteMarkdown(rel string, content string) error {
	io.mu.Lock()
	defer io.mu.Unlock()
	return io.WriteFileUnlocked(rel, []byte(content))
}

// WriteMarkdownUnlocked ghi tệp .md đi kèm. Quy ước: mỗi .md chỉ là bản hiển thị
// cho người đọc, dựng theo best-effort từ .json tương ứng, tuyệt đối không phải nguồn
// dữ liệu — mọi lần chạy và xuất đều render lại từ .json.
// Các phương thức Save ghi .json trước rồi ghi .md này trong cùng một khóa ghi, qua
// hai lần tmp+rename độc lập; nếu sập giữa chừng thì .md sẽ chậm hơn .json, điều này
// chấp nhận được (không ai đọc .md như dữ liệu, lần ghi tiếp theo cùng phạm vi sẽ tự
// lành). Cố ý không thêm giao dịch nguyên tử cho hai tệp — như vậy là thiết kế thừa.
func (io *IO) WriteMarkdownUnlocked(rel string, content string) error {
	return io.WriteFileUnlocked(rel, []byte(content))
}

func (io *IO) AppendLine(rel string, data []byte) error {
	io.mu.Lock()
	defer io.mu.Unlock()
	return io.AppendLineUnlocked(rel, data)
}

func (io *IO) AppendLineUnlocked(rel string, data []byte) error {
	p := io.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err = f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

// syncFileUnlocked xác nhận bản ghi ghi nối đã có được lưu bền khi phát lại idempotent.
// Bên gọi chịu trách nhiệm giữ khóa ghi io.mu.
func (io *IO) syncFileUnlocked(rel string) error {
	f, err := os.OpenFile(io.path(rel), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (io *IO) RemoveFile(rel string) error {
	io.mu.Lock()
	defer io.mu.Unlock()
	return io.RemoveFileUnlocked(rel)
}

func (io *IO) RemoveFileUnlocked(rel string) error {
	err := os.Remove(io.path(rel))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (io *IO) WithWriteLock(fn func() error) error {
	io.mu.Lock()
	defer io.mu.Unlock()
	return fn()
}

// EnsureDirs tạo các thư mục con chỉ định.
func (io *IO) EnsureDirs(dirs []string) error {
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(io.dir, d), 0o755); err != nil {
			return fmt.Errorf("tạo thư mục %s: %w", d, err)
		}
	}
	return nil
}
