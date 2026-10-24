package host

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/revision"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

func upgradeProject(st *storepkg.Store) error {
	version, err := st.LoadProjectFormatVersion()
	if err != nil {
		return fmt.Errorf("đọc phiên bản định dạng dự án: %w", err)
	}
	if version > storepkg.CurrentProjectFormatVersion {
		return fmt.Errorf("phiên bản định dạng dự án v%d cao hơn v%d mà chương trình hiện tại hỗ trợ, vui lòng nâng cấp ainovel-cli", version, storepkg.CurrentProjectFormatVersion)
	}
	for version < storepkg.CurrentProjectFormatVersion {
		next := version + 1
		switch version {
		case storepkg.LegacyProjectFormatVersion:
			if err := migrateLegacyBook(st); err != nil {
				return fmt.Errorf("nâng cấp dữ liệu dự án v%d→v%d: %w", version, next, err)
			}
			if err := revision.MigrateLegacyBaseline(st); err != nil {
				return fmt.Errorf("nâng cấp dữ liệu dự án v%d→v%d: %w", version, next, err)
			}
		default:
			return fmt.Errorf("không hỗ trợ nâng cấp từ định dạng dự án v%d", version)
		}
		if err := st.SaveProjectFormatVersion(next); err != nil {
			return fmt.Errorf("lưu phiên bản định dạng dự án v%d: %w", next, err)
		}
		slog.Info("nâng cấp dữ liệu dự án hoàn tất", "module", "migration", "from", version, "to", next)
		version = next
	}
	return nil
}

func migrateLegacyBook(st *storepkg.Store) error {
	book, err := st.Book.Load()
	if err != nil {
		return err
	}
	if book == nil {
		book, err = loadLegacyBook(st)
		if err != nil || book == nil {
			return err
		}
	}
	if err := st.Book.Save(*book); err != nil {
		return fmt.Errorf("lưu thông tin tác phẩm cũ: %w", err)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "book", "meta/book.json"); err != nil {
		return fmt.Errorf("ghi thông tin tác phẩm cũ: %w", err)
	}
	return nil
}

func loadLegacyBook(st *storepkg.Store) (*domain.BookMetadata, error) {
	data, err := os.ReadFile(filepath.Join(st.Dir(), "meta", "progress.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("đọc tiến độ tác phẩm cũ: %w", err)
	}
	var legacy struct {
		NovelName string `json:"novel_name"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("phân tích tiến độ tác phẩm cũ: %w", err)
	}
	legacy.NovelName = strings.TrimSpace(legacy.NovelName)
	if legacy.NovelName == "" {
		return nil, nil
	}
	premise, err := st.Outline.LoadPremise()
	if err != nil {
		return nil, fmt.Errorf("đọc tiền đề truyện cũ: %w", err)
	}
	title := legacyPremiseTitle(premise)
	if title == "" {
		return nil, fmt.Errorf("tiền đề truyện cũ thiếu tiêu đề tên sách")
	}
	if title != legacy.NovelName {
		return nil, fmt.Errorf("tên sách tác phẩm cũ xung đột: progress=%q, premise=%q", legacy.NovelName, title)
	}
	// Mục "xung đột cốt lõi" là tên mà prompt kiến trúc sư yêu cầu, nên nó theo
	// ngôn ngữ tác phẩm; nửa tiếng Trung được thử kèm để dự án cũ vẫn nâng cấp được.
	t := hostTextsFor(st.Language())
	synopsis := legacyPremiseSection(premise, t.premiseConflictHeading, hostTextsZH.premiseConflictHeading)
	if synopsis == "" {
		return nil, fmt.Errorf("tiền đề truyện cũ thiếu mục %q, không thể sinh giới thiệu tác phẩm", t.premiseConflictHeading)
	}
	return &domain.BookMetadata{Title: title, Synopsis: synopsis}, nil
}

func legacyPremiseTitle(premise string) string {
	for _, line := range strings.Split(premise, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "# ")), "《》\"")
		}
	}
	return ""
}

// legacyPremiseSection trích nội dung mục tiền đề theo một trong các tiêu đề đưa ra.
// Nhiều tiêu đề cho phép dùng cả biến thể theo ngôn ngữ tác phẩm lẫn biến thể
// gốc của dự án cũ.
func legacyPremiseSection(premise string, headings ...string) string {
	var body []string
	matched := false
	for _, line := range strings.Split(premise, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			if matched {
				break
			}
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			for _, heading := range headings {
				if title == heading {
					matched = true
					break
				}
			}
			continue
		}
		if matched {
			body = append(body, line)
		}
	}
	return strings.TrimSpace(strings.Join(body, "\n"))
}

// resumeLabel sinh nhãn Resume cho giao diện dựa trên sự thật.
// Nhãn rỗng nghĩa là không có trạng thái nào để khôi phục (nên đi tạo mới).
// Bản thân việc khôi phục không cần prompt nào — Engine chỉ khôi phục sự thật:
// tính lại tuyến đi từ store để chạy tiếp (docs/engine-rfc.md §6).
func resumeLabel(store *storepkg.Store) (string, error) {
	progress, err := store.Progress.Load()
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if progress == nil || progress.Phase == domain.PhaseComplete {
		return "", nil
	}
	return describeResume(store, progress)
}

// describeResume sinh nhãn khôi phục dễ đọc cho con người; không ảnh hưởng tuyến đi của Engine.
// Mọi tuyến thực thi do Flow Router suy ra từ sự thật; ở đây chỉ phục vụ UI "khôi phục: xxx".
func describeResume(store *storepkg.Store, progress *domain.Progress) (string, error) {
	switch progress.Phase {
	case domain.PhasePremise, domain.PhaseOutline:
		return fmt.Sprintf("khôi phục: giai đoạn quy hoạch (%s)", progress.Phase), nil
	case domain.PhaseWriting:
		// Thứ tự ưu tiên khớp với thứ tự ưu tiên quyết định của Router, để nhãn
		// và chỉ dẫn sắp phát lệnh trùng nhau.
		pending, err := store.Signals.LoadPendingCommit()
		if err != nil {
			return "", fmt.Errorf("đọc bản ghi chờ khôi phục: %w", err)
		}
		if pending != nil {
			return fmt.Sprintf("khôi phục: bản ghi chương %d bị gián đoạn", pending.Chapter), nil
		}
		if len(progress.PendingRewrites) > 0 {
			verb := "viết lại"
			if progress.Flow == domain.FlowPolishing {
				verb = "hoàn thiện"
			}
			return fmt.Sprintf("khôi phục: %s %d chương đang chờ", verb, len(progress.PendingRewrites)), nil
		}
		if progress.Flow == domain.FlowReviewing {
			return "khôi phục: đang duyệt bị gián đoạn", nil
		}
		if progress.InProgressChapter > 0 {
			return fmt.Sprintf("khôi phục: chương %d đang thực hiện", progress.InProgressChapter), nil
		}
		label, err := describeArcEndLabel(store, progress)
		if err != nil {
			return "", err
		}
		if label != "" {
			return label, nil
		}
		return fmt.Sprintf("khôi phục: tiếp tục từ chương %d", progress.NextChapter()), nil
	}
	return "khôi phục", nil
}

// describeArcEndLabel sinh nhãn bám sát giao diện cho nhiều trạng thái trung gian
// ở cuối cung / cuối tập.
// Giữ cùng thứ tự với nhánh cuối cung của flow.Route, để nhãn khớp với chỉ dẫn
// đầu tiên của Router.
func describeArcEndLabel(store *storepkg.Store, progress *domain.Progress) (string, error) {
	if !progress.Layered || len(progress.CompletedChapters) == 0 {
		return "", nil
	}
	lastCh := progress.CompletedChapters[len(progress.CompletedChapters)-1]
	boundary, err := store.Outline.CheckArcBoundary(lastCh)
	if err != nil {
		return "", fmt.Errorf("kiểm tra ranh giới cung: %w", err)
	}
	if boundary == nil || !boundary.IsArcEnd {
		return "", nil
	}
	vol, arc := boundary.Volume, boundary.Arc
	hasArcReview, err := store.World.HasArcReview(lastCh)
	if err != nil {
		return "", fmt.Errorf("đọc đánh giá cung: %w", err)
	}
	hasArcSummary, err := store.Summaries.HasArcSummary(vol, arc)
	if err != nil {
		return "", fmt.Errorf("đọc tóm tắt cung: %w", err)
	}
	hasVolumeSummary := false
	if boundary.IsVolumeEnd {
		hasVolumeSummary, err = store.Summaries.HasVolumeSummary(vol)
		if err != nil {
			return "", fmt.Errorf("đọc tóm tắt tập: %w", err)
		}
	}
	switch {
	case !hasArcReview:
		return fmt.Sprintf("khôi phục: còn chờ duyệt cuối cung (V%d A%d)", vol, arc), nil
	case !hasArcSummary:
		return fmt.Sprintf("khôi phục: cần sinh tóm tắt cung (V%d A%d)", vol, arc), nil
	case boundary.IsVolumeEnd && !hasVolumeSummary:
		return fmt.Sprintf("khôi phục: cần sinh tóm tắt tập (V%d)", vol), nil
	case boundary.NeedsExpansion && boundary.NextArc > 0:
		return fmt.Sprintf("khôi phục: cần khai triển cung kế tiếp (V%d A%d)", boundary.NextVolume, boundary.NextArc), nil
	case boundary.NeedsNewVolume:
		return fmt.Sprintf("khôi phục: cần quyết định tập kế tiếp (cuối V%d)", vol), nil
	}
	return "", nil
}
