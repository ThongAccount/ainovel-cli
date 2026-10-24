package tui

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/voocel/ainovel-cli/internal/host"
)

// renderStatusBar vẽ thanh trạng thái dùng lượng ở đáy màn hình, chiếm lấy dòng trống
// cuối cùng sẵn có của vùng nhập (không tốn thêm chiều cao):
//
//	◆ provider model(cửa sổ,think) │ ↑nhập ↓xuất ⚡cache gần đây │ chi phí(/ngân sách) tiết kiệmX    ./thư mục truyện
//
// Định vị là "liếc một cái là thấy chi phí": danh tính mô hình đang trả tiền, token tích luỹ
// trong phiên, chi phí và cảnh báo sắp chạm ngân sách.
// Dữ liệu lấy từ UISnapshot thăm dò mỗi 3s (mỗi lần gọi mô hình xong, usage được cộng vào);
// chi tiết theo vai trò/theo mô hình và chẩn đoán cache vẫn do thanh bên trái gánh, ở đây không lặp.
func renderStatusBar(snap host.UISnapshot, outputDir string, width int) string {
	dim := lipgloss.NewStyle().Foreground(colorDim)
	val := lipgloss.NewStyle().Foreground(colorMuted)

	var segs []string
	if snap.ModelName != "" {
		s := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("◆") + " "
		if snap.Provider != "" {
			s += dim.Render(snap.Provider) + " "
		}
		s += val.Render(snap.ModelName)
		if suffix := modelInfoSuffix(snap); suffix != "" {
			s += dim.Render("(" + suffix + ")")
		}
		segs = append(segs, s)
	}
	if snap.TotalInputTokens > 0 || snap.TotalOutputTokens > 0 {
		s := dim.Render("↑") + val.Render(formatTokensCompact(snap.TotalInputTokens)) +
			" " + dim.Render("↓") + val.Render(formatTokensCompact(snap.TotalOutputTokens))
		// Tỉ lệ trúng gần đây chỉ hiện khi mô hình thật sự hỗ trợ prompt cache và đã có mẫu,
		// tránh gây hiểu nhầm "0% cần điều tra".
		if snap.OverallCacheCapable && snap.OverallRecentSamples > 0 && snap.OverallRecentInput > 0 {
			rate := cacheHitRate(snap.OverallRecentCacheRead, snap.OverallRecentInput)
			s += " " + lipgloss.NewStyle().Foreground(cacheHitColor(rate)).Render("⚡"+formatPercent(rate))
		}
		segs = append(segs, s)
	}
	if snap.TotalCostUSD > 0 || snap.BudgetLimitUSD > 0 {
		cost := formatCostUSD(snap.TotalCostUSD)
		if cost == "" {
			cost = "$0"
		}
		style := val
		if snap.BudgetLimitUSD > 0 {
			// Sắp chạm hoặc vượt ngân sách thì dùng màu cảnh báo — thanh trạng thái luôn hiện,
			// đây là chỗ ngân sách cần được nhìn thấy nhất.
			switch ratio := snap.TotalCostUSD / snap.BudgetLimitUSD; {
			case ratio >= 1:
				style = lipgloss.NewStyle().Foreground(colorError).Bold(true)
			case ratio >= 0.8:
				style = lipgloss.NewStyle().Foreground(colorReview)
			}
		}
		s := style.Render(cost)
		if snap.BudgetLimitUSD > 0 {
			s += dim.Render("/" + formatCostUSD(snap.BudgetLimitUSD))
		}
		if saved := formatCostUSD(snap.TotalSavedUSD); saved != "" {
			s += dim.Render(" (tiết kiệm " + saved + ")")
		}
		segs = append(segs, s)
	}

	left := strings.Join(segs, dim.Render(" │ "))
	var right string
	if outputDir != "" {
		right = dim.Render("./" + filepath.Base(outputDir))
	}
	if left == "" && right == "" {
		return dim.Render("SẴN SÀNG")
	}
	return joinInlineSides(left, right, width)
}

// modelInfoSuffix ghép phần ghi trong ngoặc của mô hình: cửa sổ ngữ cảnh + mức suy luận,
// ví dụ "200K,med".
func modelInfoSuffix(snap host.UISnapshot) string {
	var parts []string
	if w := formatContextWindow(snap.ModelContextWindow); w != "" {
		parts = append(parts, w)
	}
	if t := formatThinkingLevel(snap.ThinkingLevel); t != "" {
		parts = append(parts, t)
	}
	return strings.Join(parts, ",")
}

func formatThinkingLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "":
		return "auto"
	case "medium":
		return "med"
	default:
		return strings.ToLower(strings.TrimSpace(level))
	}
}
