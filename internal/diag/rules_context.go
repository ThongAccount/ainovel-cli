package diag

import (
	"fmt"
	"strings"
)

// GhostCharacter phát hiện nhân vật core/important lâu không xuất hiện.
func GhostCharacter(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Characters) == 0 || len(snap.Summaries) == 0 {
		return nil
	}
	completed := snap.CompletedCount()
	if completed < 5 {
		return nil
	}

	// Tính số chương xuất hiện cuối cùng của từng nhân vật
	lastSeen := make(map[string]int)
	for ch, s := range snap.Summaries {
		for _, name := range s.Characters {
			if ch > lastSeen[name] {
				lastSeen[name] = ch
			}
		}
	}

	threshold := completed / 3
	if threshold < 5 {
		threshold = 5
	}
	latest := snap.LatestCompleted()

	var ghosts []string
	for _, c := range snap.Characters {
		if c.Tier != "core" && c.Tier != "important" {
			continue
		}
		seen, ok := lastSeen[c.Name]
		if !ok {
			// Cũng kiểm tra các bí danh
			for _, alias := range c.Aliases {
				if s, exists := lastSeen[alias]; exists && s > seen {
					seen = s
					ok = true
				}
			}
		}
		gap := latest - seen
		if !ok {
			ghosts = append(ghosts, fmt.Sprintf("%s(chưa từng xuất hiện trong tóm tắt)", c.Name))
		} else if gap > threshold {
			ghosts = append(ghosts, fmt.Sprintf("%s(xuất hiện cuối ở ch%d, vắng mặt %d chương)", c.Name, seen, gap))
		}
	}
	if len(ghosts) == 0 {
		return nil
	}
	return []Finding{{
		Rule:       "GhostCharacter",
		Category:   CatContext,
		Severity:   SevInfo,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "context.characters",
		Title:      fmt.Sprintf("nhân vật biến mất: %d nhân vật chính vắng mặt lâu", len(ghosts)),
		Evidence:   strings.Join(ghosts, "; "),
		Suggestion: "Có thể Người viết đã mất dấu nhân vật này. Cân nhắc trực tiếp gửi lệnh can thiệp trong ô nhập để đưa nhân vật trở lại, hoặc hạ tier của nhân vật trong characters.json.",
	}}
}

// TimelineGaps phát hiện chương đã hoàn thành nhưng thiếu sự kiện dòng thời gian.
func TimelineGaps(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Progress.CompletedChapters) == 0 {
		return nil
	}
	if len(snap.Timeline) == 0 && snap.CompletedCount() > 0 {
		return []Finding{{
			Rule:       "TimelineGaps",
			Category:   CatContext,
			Severity:   SevInfo,
			Confidence: ConfMedium,
			AutoLevel:  AutoNone,
			Target:     "context.timeline",
			Title:      "dòng thời gian trống",
			Evidence:   fmt.Sprintf("completed=%d, timeline_events=0", snap.CompletedCount()),
			Suggestion: "Có thể phần trích xuất dòng thời gian của commit_chapter chưa có tác dụng. Kiểm tra output của Người viết có trường timeline hay không.",
		}}
	}

	// Lập bản đồ chương → sự kiện
	chaptersWithEvents := make(map[int]bool)
	for _, e := range snap.Timeline {
		chaptersWithEvents[e.Chapter] = true
	}

	var missing []int
	for _, ch := range snap.Progress.CompletedChapters {
		if !chaptersWithEvents[ch] {
			missing = append(missing, ch)
		}
	}
	// Cho phép thiếu ở mức ít (một số chương chuyển tiếp quả thực không có sự kiện lớn)
	if len(missing) == 0 || float64(len(missing))/float64(snap.CompletedCount()) < ThresholdTimelineGapRate {
		return nil
	}
	return []Finding{{
		Rule:       "TimelineGaps",
		Category:   CatContext,
		Severity:   SevInfo,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "context.timeline",
		Title:      fmt.Sprintf("lỗ hổng dòng thời gian: %d chương không có sự kiện", len(missing)),
		Evidence:   fmt.Sprintf("missing=[%s]", intsToStr(missing)),
		Suggestion: "Có thể phần trích xuất dòng thời gian của commit_chapter chỉ hoạt động một phần. Kiểm tra định dạng trường timeline trong output của Người viết.",
	}}
}

// RelationshipStagnation phát hiện dữ liệu quan hệ ngừng cập nhật.
func RelationshipStagnation(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Relationships) == 0 {
		return nil
	}
	completed := snap.CompletedCount()
	if completed < 6 {
		return nil
	}

	// Tìm chương mới nhất có dữ liệu quan hệ
	latestRelCh := 0
	for _, r := range snap.Relationships {
		if r.Chapter > latestRelCh {
			latestRelCh = r.Chapter
		}
	}

	// Nếu dữ liệu quan hệ mới nhất nằm trong 1/3 đầu thì coi là trì trệ
	cutoff := snap.LatestCompleted() - completed/3
	if latestRelCh >= cutoff {
		return nil
	}
	return []Finding{{
		Rule:       "RelationshipStagnation",
		Category:   CatContext,
		Severity:   SevInfo,
		Confidence: ConfLow,
		AutoLevel:  AutoNone,
		Target:     "context.relationships",
		Title:      fmt.Sprintf("dữ liệu quan hệ trì trệ: cập nhật mới nhất ở chương %d", latestRelCh),
		Evidence:   fmt.Sprintf("relationship_entries=%d, latest_update=ch%d, latest_completed=ch%d", len(snap.Relationships), latestRelCh, snap.LatestCompleted()),
		Suggestion: "Có thể phần cập nhật quan hệ của commit_chapter đã ngừng hoạt động, hoặc quan hệ trong truyện thật sự không thay đổi. Kiểm tra trường relationships trong output của Người viết.",
	}}
}
