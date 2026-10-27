package rules

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRawFileSources_ScansAllMarkdownInOrder kiểm tra nhiều tệp .md trong thư mục đều được quét,
// trả về theo thứ tự từ điển của tên tệp; tệp không phải .md bị bỏ qua; nguyên văn được giữ nguyên.
func TestRawFileSources_ScansAllMarkdownInOrder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("b.md", "# B 偏好")
	write("a.md", "# A 偏好")
	write("ignore.txt", "not a rule")
	write("empty.md", "   ") // tệp trắng nên bị bỏ qua

	srcs := RawFileSources(LoadOptions{HomeRulesDir: dir})
	if len(srcs) != 2 {
		t.Fatalf("应扫到 a.md / b.md 两个来源（.txt 与空白跳过），得到 %d：%+v", len(srcs), srcs)
	}
	// thứ tự từ điển: a trước b sau
	if srcs[0].Label != "global:a.md" || srcs[1].Label != "global:b.md" {
		t.Errorf("应按字典序返回，得到 %q, %q", srcs[0].Label, srcs[1].Label)
	}
	for _, s := range srcs {
		if s.Kind != SourceGlobal {
			t.Errorf("HomeRulesDir 来源应为 SourceGlobal，得到 %v", s.Kind)
		}
	}
}

// TestRawFileSources_DirMissing kiểm tra thư mục không tồn tại thì bỏ qua im lặng (trả nil).
func TestRawFileSources_DirMissing(t *testing.T) {
	srcs := RawFileSources(LoadOptions{HomeRulesDir: filepath.Join(t.TempDir(), "nope")})
	if len(srcs) != 0 {
		t.Errorf("缺失目录应返回 0 来源，得到 %d", len(srcs))
	}
	if len(RawFileSources(LoadOptions{})) != 0 {
		t.Error("空 LoadOptions 应返回 0 来源")
	}
}

// TestRawFileSources_IgnoresHiddenAndSubdirs chốt hồi quy: tệp ẩn/tệp tạm của trình soạn thảo
// (bắt đầu bằng .) bị bỏ qua, thư mục con không được đệ quy — tránh nội dung nhị phân của
// tệp bẩn bị chèn vào LLM như nội dung ưu tiên.
func TestRawFileSources_IgnoresHiddenAndSubdirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rules")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real.md"), []byte("# real"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dirty := range []string{"._real.md", ".#lock.md", ".hidden.md"} {
		if err := os.WriteFile(filepath.Join(dir, dirty), []byte("\x00binary garbage\x00"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "nested.md"), []byte("# nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	srcs := RawFileSources(LoadOptions{HomeRulesDir: dir})
	if len(srcs) != 1 || srcs[0].Label != "global:real.md" {
		t.Fatalf("应只扫到 real.md（隐藏/脏/子目录忽略），得到 %+v", srcs)
	}
}

// TestRawFileSources_GlobalThenProject kiểm tra nguồn toàn cục ở trước, nguồn dự án ở sau.
func TestRawFileSources_GlobalThenProject(t *testing.T) {
	base := t.TempDir()
	global := filepath.Join(base, "global")
	project := filepath.Join(base, "project")
	for _, d := range []string{global, project} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(global, "g.md"), []byte("# 全局"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "p.md"), []byte("# 本书"), 0o644); err != nil {
		t.Fatal(err)
	}

	srcs := RawFileSources(LoadOptions{HomeRulesDir: global, ProjectRulesDir: project})
	if len(srcs) != 2 || srcs[0].Kind != SourceGlobal || srcs[1].Kind != SourceProject {
		t.Fatalf("应先全局后项目，得到 %+v", srcs)
	}
}
