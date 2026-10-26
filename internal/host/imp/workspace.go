package imp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// workspaceSchemaVersion là phiên bản schema chung của workspace nhập.
// Khi không khớp, yêu cầu tường minh tiếp tục bằng phiên bản tương ứng hoặc nhập lại, không
// đoán mà di chuyển dữ liệu (RFC §6.1).
const workspaceSchemaVersion = 1

// Digest tính tóm tắt nội dung, theo quy ước sẵn có của kho "sha256:"+hex (xem store/checkpoints.go).
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Artifact là danh tính thống nhất của mỗi sản phẩm ngữ nghĩa trong workspace: phiên bản schema +
// tóm tắt đầu vào + payload.
// Chỉ tái sử dụng được khi dựng lại được cùng InputDigest từ đầu vào ngữ nghĩa thực sự hiện tại
// (RFC §6.3 / bất biến 1).
// Không dựng đồ thị phụ thuộc: LoadState đi theo pipeline tuyến tính cố định và so từng bước
// InputDigest để quyết định tái sử dụng hay mất hiệu lực, NextAction suy ra bước kế tiếp theo đó.
type Artifact[T any] struct {
	SchemaVersion int    `json:"schema_version"`
	InputDigest   string `json:"input_digest"`
	Payload       T      `json:"payload"`
}

// Manifest ứng với ảnh chụp nguồn đã chuẩn hoá duy nhất, là danh tính của workspace chứ không phải
// sản phẩm dẫn xuất (RFC §6.1).
// Không lưu đường dẫn nguồn tuyệt đối, để tránh làm lộ thư mục máy và loại bỏ vấn đề khôi phục
// khi tệp bị di chuyển.
type Manifest struct {
	Version          int    `json:"version"`
	SourceName       string `json:"source_name"`
	RawSHA256        string `json:"raw_sha256"`
	NormalizedSHA256 string `json:"normalized_sha256"`
	Encoding         string `json:"encoding"`
	SizeBytes        int64  `json:"size_bytes"`
	CreatedAt        string `json:"created_at"`
}

// Intent lưu quyền tường minh của người dùng lúc khởi chạy import; sau khi khôi phục vẫn phải
// tuân theo, không suy ra từ sản phẩm, Runner không âm thầm ghi đè (RFC §6.1).
type Intent struct {
	Version             int    `json:"version"`
	AutoConfirm         bool   `json:"auto_confirm,omitempty"`
	StoryResolution     string `json:"story_resolution,omitempty"` // open / closed
	ContinueAfterImport bool   `json:"continue_after_import,omitempty"`
}

// Đường dẫn tương đối của các sản phẩm chuẩn trong workspace.
const (
	fileManifest     = "manifest.json"
	fileIntent       = "intent.json"
	fileSource       = "source.txt"
	fileGuidance     = "guidance.txt"
	fileSegmentation = "segmentation.json"
	fileConfirmation = "confirmation.json"
	fileSynthesis    = "synthesis.json"
	fileStoryResolve = "story-resolution.json"
	dirAnalyses      = "analyses"
	dirRangeDigests  = "range-digests"
	dirSegmentChunks = "segment-chunks"
	dirFailures      = "failures"
)

// Workspace là handle đọc ghi sản phẩm theo nguyên tử cho thư mục <thư mục gốc sách>/meta/import/.
type Workspace struct {
	dir string
}

// OpenWorkspace trả về handle trỏ tới meta/import/ dưới thư mục gốc sách; không bảo đảm thư mục đã
// tồn tại, dùng Active() để kiểm tra.
func OpenWorkspace(bookDir string) *Workspace {
	return &Workspace{dir: filepath.Join(bookDir, "meta", "import")}
}

// Dir trả về đường dẫn tuyệt đối của workspace (dùng cho chẩn đoán và để ghi sản phẩm thất bại).
func (w *Workspace) Dir() string { return w.dir }

func (w *Workspace) path(rel string) string { return filepath.Join(w.dir, rel) }

// Active kiểm tra có tồn tại workspace hoạt động đã phát hành hay không. meta/import/ không tồn
// tại thì coi như không hoạt động;
// thư mục khởi tạo dở dang tồn tại ở dạng meta/import.init-* nên không bị nhận nhầm là hoạt động
// (RFC §6.1).
func (w *Workspace) Active() bool {
	fi, err := os.Stat(w.dir)
	return err == nil && fi.IsDir()
}

func (w *Workspace) has(rel string) bool {
	_, err := os.Stat(w.path(rel))
	return err == nil
}

// writeAtomic ghi rel (tương đối so với workspace) một cách nguyên tử theo mô hình "tệp tạm + fsync + rename".
func (w *Workspace) writeAtomic(rel string, data []byte) error {
	full := w.path(rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), filepath.Base(full)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, full); err != nil {
		return err
	}
	syncDir(filepath.Dir(full))
	return nil
}

// syncDir cố gắng fsync các mục thư mục (best-effort) để rename vừa hoàn tất vẫn còn bền sau khi
// mất điện.
// Các nền tảng như Windows có thể không hỗ trợ Sync trên thư mục, lỗi đó bị bỏ qua—tính an toàn
// khi tiến trình sập không phụ thuộc vào nó, nó chỉ bù thêm cho tình huống mất điện (RFC §12.3).
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

func (w *Workspace) writeJSON(rel string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return w.writeAtomic(rel, append(data, '\n'))
}

func (w *Workspace) readJSON(rel string, v any) error {
	data, err := os.ReadFile(w.path(rel))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// LoadManifest đọc danh tính ảnh chụp nguồn của workspace.
func (w *Workspace) LoadManifest() (*Manifest, error) {
	var m Manifest
	if err := w.readJSON(fileManifest, &m); err != nil {
		return nil, err
	}
	if m.Version != workspaceSchemaVersion {
		return nil, fmt.Errorf("manifest schema phiên bản %d != %d, hãy tiếp tục bằng phiên bản khớp hoặc nhập lại", m.Version, workspaceSchemaVersion)
	}
	return &m, nil
}

// LoadIntent đọc quyền khởi chạy của người dùng.
func (w *Workspace) LoadIntent() (*Intent, error) {
	var in Intent
	if err := w.readJSON(fileIntent, &in); err != nil {
		return nil, err
	}
	return &in, nil
}

// LoadSource đọc văn bản ảnh chụp nguồn đã chuẩn hoá.
func (w *Workspace) LoadSource() ([]byte, error) {
	return os.ReadFile(w.path(fileSource))
}

// LoadGuidance đọc hướng dẫn cắt của người dùng (RFC §18.3); thiếu thì coi như không có hướng dẫn.
// Hướng dẫn cùng source.txt là đầu vào ngữ nghĩa của bước cắt chứ không phải sản phẩm dẫn xuất,
// được cập nhật bằng --guide một cách tường minh;
// đổi nội dung làm segmentation và InputDigest phía dưới của nó lệch một cách tự nhiên.
func (w *Workspace) LoadGuidance() (string, error) {
	data, err := os.ReadFile(w.path(fileGuidance))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// readBytes đọc byte gốc của sản phẩm, dùng để gắn InputDigest phía dưới.
func (w *Workspace) readBytes(rel string) ([]byte, error) {
	return os.ReadFile(w.path(rel))
}

// writeArtifact ghi sản phẩm ngữ nghĩa mang danh tính thống nhất.
func writeArtifact[T any](w *Workspace, rel, inputDigest string, payload T) error {
	return w.writeJSON(rel, Artifact[T]{
		SchemaVersion: workspaceSchemaVersion,
		InputDigest:   inputDigest,
		Payload:       payload,
	})
}

// readArtifact đọc sản phẩm ngữ nghĩa và kiểm tra phiên bản schema; việc InputDigest có khớp hay
// không do bên gọi quyết định theo đầu vào hiện tại.
func readArtifact[T any](w *Workspace, rel string) (*Artifact[T], error) {
	var a Artifact[T]
	if err := w.readJSON(rel, &a); err != nil {
		return nil, err
	}
	if a.SchemaVersion != workspaceSchemaVersion {
		return nil, fmt.Errorf("%s schema phiên bản %d != %d, hãy tiếp tục bằng phiên bản khớp hoặc nhập lại", rel, a.SchemaVersion, workspaceSchemaVersion)
	}
	return &a, nil
}

// clearDir xoá một thư mục bộ nhớ đệm trung gian trong workspace. Lỗi phải được giao cho bên gọi xử
// lý: nuốt lỗi sẽ khiến câu chữ "đã dọn dẹp" nói dối—lần chạy lại sau vẫn dùng lại bộ nhớ đệm
// hỏng (Windows có chống virus / handle bị giữ là tình huống có thật, Debug-First).
func (w *Workspace) clearDir(rel string) error {
	return os.RemoveAll(w.path(rel))
}

// FailureMeta là siêu dữ liệu chẩn đoán của lần thất bại gần nhất (RFC §14.2).
type FailureMeta struct {
	Stage         string `json:"stage"`
	Detail        string `json:"detail"`
	StopReason    string `json:"stop_reason,omitempty"`
	PrefixSalvage string `json:"prefix_salvage,omitempty"` // available:N / unavailable
}

// writeFailure lưu (best-effort) siêu dữ liệu của lần thất bại gần nhất cùng phản hồi gốc của mô
// hình chưa cắt ngắn vào failures/ (RFC §14.2).
// Phản hồi gốc có thể chứa nội dung truyện, nó chỉ nằm trong thư mục sách của chính người dùng,
// không đi vào nhật ký thường hay bản xuất chẩn đoán đã che thông tin.
func (w *Workspace) writeFailure(meta FailureMeta, rawResponse string) {
	_ = w.writeJSON(filepath.Join(dirFailures, "last.json"), meta)
	_ = w.writeAtomic(filepath.Join(dirFailures, "last-response.txt"), []byte(rawResponse))
}

// createWorkspace viết đủ manifest/intent/source vào thư mục tạm, kiểm tra xong rồi phát hành
// nguyên tử thành meta/import/ bằng cách rename thư mục.
// Nhờ vậy bộ ba khởi tạo không đi vào NextAction ở dạng khởi tạo dở dang, cũng không cần
// stage=initializing (RFC §6.1).
func createWorkspace(bookDir string, m Manifest, in Intent, normalized []byte) (*Workspace, error) {
	base := filepath.Join(bookDir, "meta")
	final := filepath.Join(base, "import")
	if fi, err := os.Stat(final); err == nil && fi.IsDir() {
		return nil, fmt.Errorf("không gian làm việc nhập đã tồn tại: %s (không tham số /import để khôi phục từ đó)", final)
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(base, "import.init-*")
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(tmp)
		}
	}()

	tw := &Workspace{dir: tmp}
	if err := tw.writeAtomic(fileSource, normalized); err != nil {
		return nil, err
	}
	if err := tw.writeJSON(fileManifest, m); err != nil {
		return nil, err
	}
	if err := tw.writeJSON(fileIntent, in); err != nil {
		return nil, err
	}
	// Trước khi phát hành, kiểm tra bộ ba đọc được và ảnh chụp nguồn khớp với manifest, loại bỏ
	// workspace bị ghi dở.
	got, err := tw.LoadManifest()
	if err != nil {
		return nil, fmt.Errorf("kiểm tra manifest ban đầu: %w", err)
	}
	src, err := tw.LoadSource()
	if err != nil {
		return nil, fmt.Errorf("kiểm tra ảnh chụp nguồn ban đầu: %w", err)
	}
	if d := Digest(src); d != got.NormalizedSHA256 {
		return nil, fmt.Errorf("tóm tắt ảnh chụp nguồn ban đầu không khớp: %s != %s", d, got.NormalizedSHA256)
	}
	if _, err := tw.LoadIntent(); err != nil {
		return nil, fmt.Errorf("kiểm tra intent ban đầu: %w", err)
	}

	if err := os.Rename(tmp, final); err != nil {
		return nil, err
	}
	syncDir(base)
	committed = true
	return &Workspace{dir: final}, nil
}
