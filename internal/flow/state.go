package flow

import (
	"fmt"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// LoadState đọc từ Store toàn bộ sự thật mà Route cần.
// Đây là "biên IO" của định tuyến: mọi lượt đọc tập trung ở đây để Route giữ
// nguyên tính thuần.
// Mọi lỗi đọc đều được trả về; công cụ hỏng và "chưa sinh ra" là hai sự thật
// khác nhau, Router không được tiếp tục phái nhiệm vụ trên ảnh chụp không đầy đủ.
func LoadState(store *storepkg.Store) (State, error) {
	var s State
	// Ngôn ngữ tác phẩm quyết định giọng văn của nhiệm vụ gửi cho Worker.
	s.Lang = i18n.Normalize(store.Language())
	missing, err := store.FoundationMissing()
	if err != nil {
		return s, fmt.Errorf("load foundation state: %w", err)
	}
	s.FoundationMissing = missing
	// Quy mô quy hoạch: save_foundation ghi scale vào RunMeta, nhánh bổ sung dựa
	// vào đó để suy ra kiến trúc sư.
	// Lỗi đọc được coi như chưa biết (tier rỗng → giao phần bổ sung cho LLM phán
	// quyết), nhất quán với lựa chọn mặc định thận trọng của các sự thật khác.
	meta, err := store.RunMeta.Load()
	if err != nil {
		return s, fmt.Errorf("load run meta: %w", err)
	}
	if meta != nil {
		s.PlanningTier = meta.PlanningTier
	}
	progress, err := store.Progress.Load()
	if err != nil {
		return s, fmt.Errorf("load progress: %w", err)
	}
	if progress == nil {
		return s, nil
	}
	s.Progress = progress
	feedback, err := store.Outline.LoadPendingOutlineFeedback()
	if err != nil {
		return s, fmt.Errorf("load outline feedback: %w", err)
	}
	for _, item := range feedback {
		if item.RequiresImmediateReview() {
			s.ImmediateFeedbackCount++
		}
	}

	s.LastCompleted = progress.LatestCompleted()

	// Nếu chương đầu hàng đợi làm lại chưa có chapter_contract thì để kiến trúc sư
	// bổ sung một bản trước khi phái writer.
	// Lỗi đọc được coi như "đã có chỉ dẫn": đây là nhánh định hướng, không được
	// để một lần đọc đĩa hỏng chặn cả hàng đợi làm lại.
	if len(progress.PendingRewrites) > 0 {
		head := progress.PendingRewrites[0]
		plan, err := store.Drafts.LoadChapterPlan(head)
		if err == nil {
			s.RewriteHeadNeedsDirective = plan == nil || !plan.HasDirective()
		}
	}

	// Ranh giới cung chỉ được tính ở chế độ phân lớp và khi đã có chương hoàn thành
	if progress.Layered && s.LastCompleted > 0 {
		boundaries, err := store.Outline.CompletedArcBoundaries(s.LastCompleted)
		if err != nil {
			return s, fmt.Errorf("load completed arc boundaries: %w", err)
		}
		for i := range boundaries {
			boundary := &boundaries[i]
			hasReview, err := store.World.HasArcReview(boundary.EndChapter)
			if err != nil {
				return s, fmt.Errorf("load arc review: %w", err)
			}
			if !hasReview {
				s.AggregateRefresh = aggregateRefresh(AggregateArcReview, boundary)
				break
			}
			hasArcSummary, err := store.Summaries.HasArcSummary(boundary.Volume, boundary.Arc)
			if err != nil {
				return s, fmt.Errorf("load arc summary: %w", err)
			}
			if !hasArcSummary {
				s.AggregateRefresh = aggregateRefresh(AggregateArcSummary, boundary)
				break
			}
			if boundary.IsVolumeEnd {
				hasVolumeSummary, err := store.Summaries.HasVolumeSummary(boundary.Volume)
				if err != nil {
					return s, fmt.Errorf("load volume summary: %w", err)
				}
				if !hasVolumeSummary {
					s.AggregateRefresh = aggregateRefresh(AggregateVolumeSummary, boundary)
					break
				}
			}
		}

		boundary, err := store.Outline.CheckArcBoundary(s.LastCompleted)
		if err != nil {
			return s, fmt.Errorf("check arc boundary: %w", err)
		}
		if boundary != nil {
			s.ArcBoundary = boundary
			if boundary.IsArcEnd {
				s.HasArcReview, err = store.World.HasArcReview(s.LastCompleted)
				if err != nil {
					return s, fmt.Errorf("load arc review: %w", err)
				}
				s.HasArcSummary, err = store.Summaries.HasArcSummary(boundary.Volume, boundary.Arc)
				if err != nil {
					return s, fmt.Errorf("load arc summary: %w", err)
				}
				if boundary.IsVolumeEnd {
					s.HasVolumeSummary, err = store.Summaries.HasVolumeSummary(boundary.Volume)
					if err != nil {
						return s, fmt.Errorf("load volume summary: %w", err)
					}
				}
			}
		}
	}

	// Sự thật thẩm duyệt toàn cục của sách không phân lớp: chỉ đọc đĩa tại điểm
	// kích hoạt (các tổ hợp khác Route không dùng trường này).
	if !progress.Layered && s.LastCompleted > 0 {
		for completed := domain.ReviewInterval; completed <= len(progress.CompletedChapters); completed += domain.ReviewInterval {
			chapter := progress.CompletedChapters[completed-1]
			hasReview, err := store.World.HasGlobalReview(chapter)
			if err != nil {
				return s, fmt.Errorf("load global review: %w", err)
			}
			if !hasReview {
				s.AggregateRefresh = &AggregateRefresh{Kind: AggregateGlobalReview, EndChapter: chapter}
				break
			}
		}
		if due, _ := domain.ShouldReview(len(progress.CompletedChapters)); due {
			s.HasGlobalReview, err = store.World.HasGlobalReview(s.LastCompleted)
			if err != nil {
				return s, fmt.Errorf("load global review: %w", err)
			}
		}
	}

	return s, nil
}

func aggregateRefresh(kind AggregateKind, boundary *storepkg.ArcBoundary) *AggregateRefresh {
	return &AggregateRefresh{
		Kind: kind, Volume: boundary.Volume, Arc: boundary.Arc,
		StartChapter: boundary.StartChapter, EndChapter: boundary.EndChapter,
	}
}
