package imp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDigestStableAndDistinct(t *testing.T) {
	a := Digest([]byte("第一章"))
	if a != Digest([]byte("第一章")) {
		t.Fatal("digest của cùng đầu vào không ổn định")
	}
	if a == Digest([]byte("第二章")) {
		t.Fatal("digest của các đầu vào khác nhau lại giống nhau")
	}
	if len(a) < 8 || a[:7] != "sha256:" {
		t.Fatalf("tiền tố digest không khớp: %s", a)
	}
}

func TestWorkspaceAtomicRoundtrip(t *testing.T) {
	w := &Workspace{dir: t.TempDir()}
	if err := w.writeAtomic("nested/x.txt", []byte("hello")); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}
	got, err := os.ReadFile(w.path("nested/x.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("đọc lại không khớp: %q %v", got, err)
	}
}

func TestArtifactRoundtripPreservesIdentity(t *testing.T) {
	w := &Workspace{dir: t.TempDir()}
	type payload struct {
		N int `json:"n"`
	}
	if err := writeArtifact(w, "seg.json", "sha256:abc", payload{N: 7}); err != nil {
		t.Fatalf("writeArtifact: %v", err)
	}
	a, err := readArtifact[payload](w, "seg.json")
	if err != nil {
		t.Fatalf("readArtifact: %v", err)
	}
	if a.InputDigest != "sha256:abc" || a.Payload.N != 7 || a.SchemaVersion != workspaceSchemaVersion {
		t.Fatalf("định danh chưa được giữ lại: %+v", a)
	}
}

func TestReadArtifactRejectsSchemaMismatch(t *testing.T) {
	w := &Workspace{dir: t.TempDir()}
	// Ghi thẳng một sản phẩm có schema không khớp phiên bản.
	raw := Artifact[string]{SchemaVersion: 999, InputDigest: "sha256:x", Payload: "y"}
	if err := w.writeJSON("seg.json", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := readArtifact[string](w, "seg.json"); err == nil {
		t.Fatal("schema không khớp phiên bản phải bị từ chối")
	}
}

func TestCreateWorkspacePublishesAtomically(t *testing.T) {
	book := t.TempDir()
	norm := []byte("第一章\n正文\n")
	m := Manifest{
		Version:          workspaceSchemaVersion,
		SourceName:       "book.txt",
		NormalizedSHA256: Digest(norm),
		Encoding:         encodingUTF8,
	}
	ws, err := createWorkspace(book, m, Intent{Version: workspaceSchemaVersion}, norm)
	if err != nil {
		t.Fatalf("createWorkspace: %v", err)
	}
	if !ws.Active() {
		t.Fatal("sau khi phát hành, không gian làm việc phải ở trạng thái hoạt động")
	}
	for _, f := range []string{fileManifest, fileIntent, fileSource} {
		if !ws.has(f) {
			t.Fatalf("thiếu sản phẩm %s", f)
		}
	}
	// Sau khi createWorkspace thành công không được rò rỉ thư mục tạm nửa khởi tạo (meta/import.init-*).
	if dirs, _ := filepath.Glob(filepath.Join(book, "meta", "import.init-*")); len(dirs) != 0 {
		t.Fatalf("sau khi phát hành thành công không nên còn thư mục init: %v", dirs)
	}
	// Tạo lại phải thất bại vì đã tồn tại.
	if _, err := createWorkspace(book, m, Intent{}, norm); err == nil {
		t.Fatal("khi đã có không gian làm việc hoạt động thì tạo lại phải thất bại")
	}
}

func TestCreateWorkspaceRejectsInconsistentSnapshot(t *testing.T) {
	book := t.TempDir()
	m := Manifest{Version: workspaceSchemaVersion, NormalizedSHA256: Digest([]byte("A"))}
	// Tóm tắt mà manifest khai báo không khớp với normalized thực sự ghi → kiểm tra trước khi phát hành phải chặn lại.
	if _, err := createWorkspace(book, m, Intent{}, []byte("B")); err == nil {
		t.Fatal("khi ảnh chụp nguồn và tóm tắt trong manifest không khớp thì phải từ chối phát hành")
	}
	if _, err := os.Stat(filepath.Join(book, "meta", "import")); !os.IsNotExist(err) {
		t.Fatal("sau khi phát hành thất bại không nên để lại không gian làm việc hoạt động")
	}
}
