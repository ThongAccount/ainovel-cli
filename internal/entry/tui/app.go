package tui

import (
	"fmt"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/host"
	buildversion "github.com/voocel/ainovel-cli/internal/version"
)

// Run khởi động TUI.
// Quy ước phân tầng các chế độ khởi động:
// 1. Chế độ nhanh và chế độ đồng sáng tác thuộc về "điều phối khởi động";
// 2. Phiên sáng tác chính thức đi vào host.Host;
// 3. Nếu sau này bổ sung chế độ dùng chung như "tiếp tục truyện đã có", gom về
//    internal/entry/startup.
func Run(cfg bootstrap.Config, bundle assets.Bundle, build buildversion.Info) error {
	rt, err := host.New(cfg, bundle, host.WithFileLog("tui.log", false,
		slog.String("version", build.Version),
		slog.String("commit", build.Commit),
		slog.String("built", build.Date),
	))
	if err != nil {
		return err
	}
	defer rt.Close()

	m := NewModel(rt, build.Version)
	if logErr := rt.FileLogError(); logErr != nil {
		logWarning := fmt.Errorf("nhật ký tệp không dùng được, đã tiếp tục dùng nhật ký terminal: %w", logErr)
		m.err = logWarning
		m.applyEvent(host.Event{
			Time: time.Now(), Category: "SYSTEM", Level: "warn",
			Summary: logWarning.Error(), Detail: logWarning.Error(),
		})
	}
	// Không bật báo cáo chuột toàn cục lúc khởi động: trang chào không cần chuột, tắt báo cáo
	// giữ được thao tác chọn-kéo sao chép native của terminal. Khi vào bàn làm việc sáng tác
	// (modeRunning) thì enterRunning mới bật báo cáo, để hỗ trợ bấm đổi panel /
	// lăn chuột / kéo thanh bên.
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
