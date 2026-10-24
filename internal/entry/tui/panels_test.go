package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/voocel/ainovel-cli/internal/host"
)

func TestRenderTopBarShowsVersion(t *testing.T) {
	out := renderTopBar(host.UISnapshot{
		Provider:  "openrouter",
		ModelName: "test-model",
		BookTitle: "Tiểu thuyết thử nghiệm",
	}, 120, "", "v1.2.3")
	if !strings.Contains(out, "ainovel-cli v1.2.3") {
		t.Fatalf("top bar missing version: %q", out)
	}
}

func TestRenderDetailContentShowsSynopsis(t *testing.T) {
	out := ansi.Strip(renderDetailContent(host.UISnapshot{Synopsis: "Tìm bình minh giữa đêm vĩnh cửu."}, 40))
	if !strings.Contains(out, "Tóm Tắt") || !strings.Contains(out, "Tìm bình minh giữa đêm vĩnh cửu.") {
		t.Fatalf("detail panel missing synopsis: %q", out)
	}
}

func TestSameDetailSnapshotDetectsOutlineStateChanges(t *testing.T) {
	base := host.UISnapshot{Outline: []host.OutlineSnapshot{{Chapter: 1, Title: "Chương một"}}}
	if !sameDetailSnapshot(base, base) {
		t.Fatal("chi tiết giống nhau không nên kích hoạt dựng lại")
	}
	changed := base
	changed.InProgressChapter = 1
	if sameDetailSnapshot(base, changed) {
		t.Fatal("thay đổi trạng thái chương phải kích hoạt dựng lại chi tiết")
	}
}

func TestRenderErrorEventKeepsOneLineSummary(t *testing.T) {
	out := ansi.Strip(renderEventLine(host.Event{
		Time:     time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
		Category: "ERROR",
		Summary:  "commit_chapter tham số sai: " + strings.Repeat("Qin Việt phát hiện manh mối trong tài liệu", 20),
	}, 60, 0))
	if strings.Contains(out, "\n") {
		t.Fatalf("sự kiện ERROR phải giữ tóm tắt một dòng, got %q", out)
	}
	if !strings.HasSuffix(out, "...") {
		t.Fatalf("tóm tắt ERROR quá rộng phải bị TUI cắt, got %q", out)
	}
}

// TestRenderStatusBar bảo vệ hợp đồng thông tin của thanh trạng thái dưới cùng: định danh model
// (cửa sổ + mức suy luận), token của phiên, chi phí/ngân sách và thư mục truyện đều phải có
// (sau khi bóc style thì assert theo văn bản thuần).
func TestRenderStatusBar(t *testing.T) {
	out := ansi.Strip(renderStatusBar(host.UISnapshot{
		Provider:           "openrouter",
		ModelName:          "test-model",
		ModelContextWindow: 200000,
		ThinkingLevel:      "medium",
		TotalInputTokens:   1_234_000,
		TotalOutputTokens:  89_300,
		TotalCostUSD:       0.31,
		BudgetLimitUSD:     5,
		TotalSavedUSD:      0.12,
	}, "/tmp/output", 120))
	for _, want := range []string{"test-model(200K,med)", "↑1.2M", "↓89.3k", "$0.31/$5.00", "tiết kiệm $0.12", "./output"} {
		if !strings.Contains(out, want) {
			t.Fatalf("thanh trạng thái thiếu %q: %q", want, out)
		}
	}
}

func TestRenderStatusBarAutoThinkingAndEmpty(t *testing.T) {
	out := ansi.Strip(renderStatusBar(host.UISnapshot{
		ModelName:          "test-model",
		ModelContextWindow: 128000,
	}, "", 120))
	if !strings.Contains(out, "test-model(128K,auto)") {
		t.Fatalf("thiếu ghi chú mức suy luận auto: %q", out)
	}
	if out := ansi.Strip(renderStatusBar(host.UISnapshot{}, "", 120)); out != "SẴN SÀNG" {
		t.Fatalf("snapshot rỗng phải rơi về READY, được %q", out)
	}
}

func TestRenderUsageLineSeparatesFullWidthNameAndTokens(t *testing.T) {
	out := renderUsageLine("gpt-5.6-sol", bodyTextColor, 5300, 0, 0.23, 32)
	if !strings.Contains(out, "gpt-5.6-sol 5.3k") {
		t.Fatalf("model name and tokens should have a visible gap: %q", out)
	}
}

func TestTruncateByDisplayWidth(t *testing.T) {
	// Chữ Trung cắt theo bề rộng hiển thị: ngân sách 10 cột = 3 chữ Hán (6 cột) + "..." (3 cột), cắt theo rune sẽ tràn ra 17 cột
	got := truncate("临港市公共算法伦理审计员", 10)
	if w := lipgloss.Width(got); w > 10 {
		t.Errorf("truncate vượt bề rộng cột: %d > 10 (%q)", w, got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("cắt quá rộng phải kèm dấu chấm lửng: %q", got)
	}
	// Hành vi với ASCII giữ nguyên như bản triển khai cũ
	if got := truncate("abcdef", 6); got != "abcdef" {
		t.Errorf("chưa vượt rộng thì không cắt: %q", got)
	}
	if got := truncate("abcdefgh", 6); got != "abc..." {
		t.Errorf("cắt ASCII: got %q want %q", got, "abc...")
	}
}

func TestRenderDetailContentWrapsCJK(t *testing.T) {
	long := "沈砚（主角；临港市公共算法伦理审计员，台风夜事故的调查负责人，坚持程序正义）"
	const contentW = 40
	out := renderDetailContent(host.UISnapshot{
		Characters:       []string{long},
		SupportingCount:  1,
		RecentSupporting: []string{long},
		RecentSummaries:  []string{"第6章：" + long},
	}, contentW)
	for line := range strings.SplitSeq(out, "\n") {
		if w := lipgloss.Width(line); w > contentW {
			t.Errorf("dòng tràn bề rộng panel: %d > %d (%q)", w, contentW, line)
		}
	}
	// Mô tả dài phải được xuống dòng (dòng nối thụt lề treo), chứ không cắt bỏ mất thông tin
	joined := strings.ReplaceAll(strings.ReplaceAll(out, "\n", ""), " ", "")
	if !strings.Contains(joined, "坚持程序正义") {
		t.Errorf("sau khi xuống dòng phải giữ nguyên mô tả, output thực tế:\n%s", out)
	}
}
