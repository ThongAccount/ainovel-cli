package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureRulesDirAt kiểm tra việc chuẩn bị thư mục + README.txt: ghi hướng dẫn,
// luôn ghi đè bằng mẫu mới nhất, và README.txt (không phải .md) không bị quét thành quy tắc.
func TestEnsureRulesDirAt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rules")
	if err := ensureRulesDirAt(dir); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(dir, "README.txt")
	data, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("README.txt should be written: %v", err)
	}
	// Sau khi bỏ YAML, hướng dẫn chuyển sang "ngôn ngữ tự nhiên + tự động chuẩn hóa",
	// không còn dạy front matter.
	if !strings.Contains(string(data), "chuẩn hóa") {
		t.Errorf("README.txt phải nói rõ ngôn ngữ tự nhiên sẽ được chuẩn hóa, got %q", data)
	}
	if strings.Contains(string(data), "front matter") {
		t.Errorf("README.txt không được dạy YAML front matter, got %q", data)
	}

	// Luôn ghi đè bằng mẫu mới nhất: văn bản lỗi thời do bản cũ viết sẽ được làm mới lần ensure sau
	if err := os.WriteFile(readme, []byte("旧版本写的过时文案"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureRulesDirAt(dir); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(readme); string(again) != homeRulesReadme {
		t.Errorf("README.txt should be refreshed to latest template, got %q", again)
	}

	// README.txt không bị coi là quy tắc (quét chỉ nhận .md)
	if srcs := RawFileSources(LoadOptions{HomeRulesDir: dir}); len(srcs) != 0 {
		t.Errorf("README.txt must not be scanned as a rule, got %d sources", len(srcs))
	}
}

// TestDefaultProjectRulesDir chốt hồi quy: thư mục quy tắc cấp dự án phản chiếu cấp toàn cục: ./.ainovel/rules/.
func TestDefaultProjectRulesDir(t *testing.T) {
	proj := filepath.Join("/tmp", "demo-book")
	want := filepath.Join(proj, ".ainovel", "rules")
	if got := DefaultProjectRulesDir(proj); got != want {
		t.Errorf("DefaultProjectRulesDir=%q, want %q", got, want)
	}
	if got := DefaultProjectRulesDir(""); got != "" {
		t.Errorf("空项目根应返回空串，得到 %q", got)
	}
}

// TestDefaultOptions_ScansProjectRulesFromDotAinovel kiểm chứng đầu-cuối:
// DefaultOptions nối ./.ainovel/rules/ dưới cwd vào nguồn SourceProject.
func TestDefaultOptions_ScansProjectRulesFromDotAinovel(t *testing.T) {
	proj := t.TempDir()
	t.Chdir(proj)
	rulesDir := filepath.Join(proj, ".ainovel", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "book.md"), []byte("# 本书偏好\n每章 4000 字"), 0o644); err != nil {
		t.Fatal(err)
	}

	srcs := RawFileSources(DefaultOptions())
	var got *RawSource
	for i := range srcs {
		if srcs[i].Kind == SourceProject {
			got = &srcs[i]
		}
	}
	if got == nil {
		t.Fatalf("应从 ./.ainovel/rules/ 扫到项目规则来源，得到 %+v", srcs)
	}
	if !strings.Contains(got.Text, "本书偏好") {
		t.Errorf("项目规则原文应被原样返回，得到 %q", got.Text)
	}
	if got.Label != "project:book.md" {
		t.Errorf("来源标签应为 project:book.md，得到 %q", got.Label)
	}
}
