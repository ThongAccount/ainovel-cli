package host

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/store"
)

// buildStoryStateSummary dựng một đoạn tóm tắt gọn về trạng thái truyện, để trợ lý đồng sáng tác theo giai đoạn biết “đã viết được gì”.
// Dùng lại các điểm truy cập của store, chỉ lấy sự thật cấp cao cần cho hướng quy hoạch (tiến độ / la bàn / tập gần nhất / nhân vật chính / phục bút đang hoạt động);
// không kéo nội dung truyện, không nạp JSON đầy đủ của novel_context — đồng sáng tác là hội thoại, cần bản tóm tắt dễ đọc chứ không phải ngữ cảnh viết.
// Mục nào thiếu thì bỏ qua (best-effort), trả về chuỗi rỗng nghĩa là chưa có tiến độ dùng được.
//
// Tóm tắt được ghép vào system prompt như một phụ lục dữ liệu, nên nhãn phải theo ngôn ngữ tác phẩm: t đến từ
// hostTextsFor(store.Language()), phía gọi chọn vi / zh tuỳ nhu cầu.
func buildStoryStateSummary(s *store.Store, t *hostTexts) string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	var warnings []string
	warn := func(scope string, err error) {
		if err != nil {
			warnings = append(warnings, fmt.Sprintf(t.stateReadFailed, scope, err))
		}
	}

	if book, err := s.Book.Load(); book != nil {
		fmt.Fprintf(&b, t.stateBookTitle, book.Title)
	} else {
		warn("book", err)
	}

	if progress, err := s.Progress.Load(); progress != nil {
		fmt.Fprintf(&b, t.stateProgress, len(progress.CompletedChapters))
		if progress.Layered {
			outline, outlineErr := s.Outline.LoadOutline()
			if outlineErr != nil {
				warn("outline", outlineErr)
			} else if len(outline) > 0 {
				fmt.Fprintf(&b, t.stateRefined, len(outline))
			}
		} else if progress.TotalChapters > 0 {
			fmt.Fprintf(&b, t.statePlanned, progress.TotalChapters)
		}
		fmt.Fprintf(&b, t.stateWordCount, progress.TotalWordCount, progress.NextChapter())
		if progress.Layered && progress.CurrentVolume > 0 {
			fmt.Fprintf(&b, t.statePosition, progress.CurrentVolume, progress.CurrentArc)
		}
	} else {
		warn("progress", err)
	}

	if compass, err := s.Outline.LoadCompass(); compass != nil {
		if dir := strings.TrimSpace(compass.EndingDirection); dir != "" {
			fmt.Fprintf(&b, t.stateEnding, dir)
		}
		if compass.EstimatedScale != "" {
			fmt.Fprintf(&b, t.stateScale, compass.EstimatedScale)
		}
		if len(compass.OpenThreads) > 0 {
			fmt.Fprintf(&b, t.stateOpenThreads, strings.Join(compass.OpenThreads, t.stateItemSep))
		}
	} else {
		warn("story_compass", err)
	}

	// Tóm tắt tập gần nhất, để trợ lý biết truyện vừa đi tới đâu
	if vols, err := s.Summaries.LoadAllVolumeSummaries(); len(vols) > 0 {
		last := vols[len(vols)-1]
		fmt.Fprintf(&b, t.stateLastVolume, last.Title, truncate(last.Summary, 200))
	} else {
		warn("volume_summaries", err)
	}

	// Nhân vật chính (core/important), tối đa 8 người
	if chars, err := s.Characters.Load(); len(chars) > 0 {
		var names []string
		for _, c := range chars {
			if c.Tier == "secondary" || c.Tier == "decorative" {
				continue
			}
			line := c.Name
			if role := strings.TrimSpace(c.Role); role != "" {
				line += fmt.Sprintf(t.stateRoleParen, role)
			}
			names = append(names, line)
			if len(names) >= 8 {
				break
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(&b, t.stateMainChars, strings.Join(names, t.stateListSep))
		}
	} else {
		warn("characters", err)
	}

	// Phục bút chưa thu, tối đa 6 mục
	if fs, err := s.World.LoadActiveForeshadow(); len(fs) > 0 {
		var items []string
		for _, f := range fs {
			items = append(items, truncate(f.Description, 40))
			if len(items) >= 6 {
				break
			}
		}
		fmt.Fprintf(&b, t.stateForeshadow, strings.Join(items, t.stateItemSep))
	} else {
		warn("foreshadow", err)
	}

	if len(warnings) > 0 {
		fmt.Fprintf(&b, t.stateWarnings, strings.Join(warnings, t.stateItemSep))
	}

	return strings.TrimSpace(b.String())
}

// stageSystemPrompt dựng system prompt đầy đủ cho đồng sáng tác theo giai đoạn: prompt giai đoạn + tóm tắt trạng thái truyện hiện tại.
// Tóm tắt được gắn ở cuối như phụ lục dữ liệu (ngăn cách bằng đường kẻ với quy tắc định dạng), khớp với chỉ dẫn “tiến độ nằm ở bên dưới” trong prompt.
func stageSystemPrompt(s *store.Store, t *hostTexts) string {
	prompt := t.stageCoCreateSystemPrompt
	if summary := buildStoryStateSummary(s, t); summary != "" {
		prompt += "\n\n---\n" + t.stageStateTitle + "\n" + t.stageStateNote + "\n" + summary
	}
	return prompt
}
