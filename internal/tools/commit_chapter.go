package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/chapterfacts"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/revision"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/store"
)

// CommitChapterTool nộp chương: nạp nội dung truyện → lưu bản chính → sinh tóm
// tắt → cập nhật trạng thái → cập nhật tiến độ.
type CommitChapterTool struct {
	store      *store.Store
	styleStats *StyleStatsIndex
}

// NewCommitChapterTool tạo công cụ nộp chương. styleStats bắt buộc phải dùng
// chung với novel_context, đảm bảo việc thêm mới, viết lại và khôi phục đều làm
// mới cùng một chỉ mục thống kê.
func NewCommitChapterTool(store *store.Store, styleStats *StyleStatsIndex) *CommitChapterTool {
	if styleStats == nil {
		panic("tools: NewCommitChapterTool requires StyleStatsIndex")
	}
	return &CommitChapterTool{store: store, styleStats: styleStats}
}

// texts lấy bảng văn bản theo ngôn ngữ tác phẩm, đọc lười mỗi lần gọi.
func (t *CommitChapterTool) texts() *writeTexts { return writeTextsFor(t.store.Language()) }

func (t *CommitChapterTool) chapterStyleDelta(chapter int) (domain.StyleDelta, error) {
	record, err := t.store.ChapterRecords.Load(chapter)
	if err != nil || record == nil {
		return domain.StyleDelta{}, err
	}
	return record.StyleDelta, nil
}

// commitOutput nhúng thêm các trường mở rộng lên trên domain.CommitResult, giữ
// cho package domain không phụ thuộc rules.
// Vì các trường nhúng được JSON marshaler nâng lên (promoted), kết quả serialize
// giống hệt một cấu trúc phẳng.
type commitOutput struct {
	domain.CommitResult
	RuleViolations []rules.Violation `json:"rule_violations,omitempty"`
}

// commitArgs là tải trọng có cấu trúc đã chuẩn hóa của Saga nộp chương. Lần chạy
// đầu ghi nó cùng ảnh chụp nội dung truyện vào PendingCommit; khi khôi phục sau
// crash luôn phát lại đúng ý định đã đóng băng này, bỏ qua tham số và bản nháp mà
// Worker mới sinh ra.
type commitArgs struct {
	Chapter int `json:"chapter"`
	domain.ChapterFacts
}

func (t *CommitChapterTool) Name() string { return "commit_chapter" }
func (t *CommitChapterTool) Description() string {
	return t.texts().commitChapterDesc
}
func (t *CommitChapterTool) Label() string { return t.texts().commitChapterLabel }

// Công cụ ghi (Saga khôi phục được xuyên miền: tải trọng đầy đủ → bản chính/trạng
// thái → tiến độ → checkpoint), cấm chạy song song.
func (t *CommitChapterTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *CommitChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return false }
func (t *CommitChapterTool) StrictSchema() bool                     { return true }

func (t *CommitChapterTool) Schema() map[string]any {
	props := []schema.Prop{schema.Property("chapter", schema.Int(t.texts().commitChapterArgChapter)).Required()}
	props = append(props, chapterfacts.Properties(true)...)
	return schema.Object(props...)
}

func (t *CommitChapterTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var requested commitArgs
	if err := json.Unmarshal(args, &requested); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if requested.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}
	existingPending, err := t.store.Signals.LoadPendingCommit()
	if err != nil {
		return nil, fmt.Errorf("load pending commit: %w: %w", errs.ErrStoreRead, err)
	}
	if existingPending != nil && existingPending.Chapter != requested.Chapter {
		return nil, fmt.Errorf("đang tồn tại một lần nộp chương chưa khôi phục: chương %d (giai đoạn %s), vui lòng khôi phục hoặc nộp lại chương đó trước: %w", existingPending.Chapter, existingPending.Stage, errs.ErrToolConflict)
	}
	if existingPending != nil {
		switch existingPending.Stage {
		case domain.CommitStageStarted, domain.CommitStageStateApplied, domain.CommitStageProgressMarked, domain.CommitStageSignalSaved:
		default:
			return nil, fmt.Errorf("giai đoạn pending commit không hợp lệ: %q: %w", existingPending.Stage, errs.ErrToolConflict)
		}
	}

	a := requested
	if existingPending != nil && existingPending.Stage != domain.CommitStageProgressMarked && existingPending.Stage != domain.CommitStageSignalSaved {
		if len(existingPending.Payload) == 0 {
			return nil, fmt.Errorf("chương %d có lần nộp phiên bản cũ chưa hoàn tất nhưng thiếu payload có thể phát lại; từ chối ghi đè bằng tham số mới sinh ra, vui lòng khôi phục từ checkpoint gần nhất hoặc tự kiểm tra meta/pending_commit.json: %w",
				existingPending.Chapter, errs.ErrToolConflict)
		}
		if err := json.Unmarshal(existingPending.Payload, &a); err != nil {
			return nil, fmt.Errorf("decode pending commit payload: %w: %w", errs.ErrStoreRead, err)
		}
		if a.Chapter != existingPending.Chapter {
			return nil, fmt.Errorf("chương trong payload pending commit không khớp: bản ghi=%d payload=%d: %w", existingPending.Chapter, a.Chapter, errs.ErrToolConflict)
		}
	}

	progress, err := t.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	if progress == nil {
		return nil, fmt.Errorf("progress chưa khởi tạo: %w", errs.ErrToolPrecondition)
	}
	completed := slices.Contains(progress.CompletedChapters, a.Chapter)
	if existingPending != nil && (existingPending.Stage == domain.CommitStageProgressMarked || existingPending.Stage == domain.CommitStageSignalSaved) {
		if !completed {
			return nil, fmt.Errorf("pending commit đã tới %s, nhưng progress chưa đánh dấu chương %d hoàn thành: %w", existingPending.Stage, a.Chapter, errs.ErrToolConflict)
		}
		return t.finishPendingCommit(*existingPending, progress)
	}
	if existingPending == nil || existingPending.Stage == domain.CommitStageStarted {
		if err := t.validateCommitArgs(a); err != nil {
			// Phiên bản cũ có thể đã để lại một lần nộp đóng băng trước khi phát
			// hiện sự thật làm lại không hợp lệ. Giữ lại tải trọng bất biến này
			// chỉ khiến mọi lần thử lại lặp lại đúng một lỗi, nên ở đây chủ động
			// gỡ đóng băng để Người viết sửa tham số rồi nộp lại; nội dung truyện
			// và bản ghi chương không bị thay đổi ở bước này.
			if existingPending != nil && existingPending.Rewrite &&
				(errors.Is(err, errs.ErrToolArgs) || errors.Is(err, errs.ErrToolPrecondition)) {
				if clearErr := t.store.Signals.ClearPendingCommit(); clearErr != nil {
					return nil, fmt.Errorf("kiểm tra lần nộp làm lại thất bại (%v), đồng thời dọn lần nộp đóng băng cũng thất bại: %w: %w", err, errs.ErrStoreWrite, clearErr)
				}
				return nil, fmt.Errorf("lần nộp làm lại còn sót từ phiên bản cũ đã qua kiểm tra, đã gỡ đóng băng; vui lòng sửa lại rồi nộp: %w", err)
			}
			return nil, err
		}
	}

	if existingPending != nil && existingPending.Rewrite {
		if !completed {
			return nil, fmt.Errorf("lần nộp làm lại đòi hỏi chương %d đã có bản chính: %w", a.Chapter, errs.ErrToolConflict)
		}
		return t.executeRewriteCommit(a, progress, *existingPending, true)
	}
	if existingPending == nil && completed {
		if slices.Contains(progress.PendingRewrites, a.Chapter) {
			content, err := t.validateRewriteDraft(a.Chapter, a.Title, progress)
			if err != nil {
				return nil, err
			}
			payload, err := json.Marshal(a)
			if err != nil {
				return nil, fmt.Errorf("marshal rewrite payload: %w", err)
			}
			now := time.Now().Format(time.RFC3339)
			mode := "rewrite"
			if progress.Flow == domain.FlowPolishing {
				mode = "polish"
			}
			pending := domain.PendingCommit{Chapter: a.Chapter, Stage: domain.CommitStageStarted,
				Rewrite: true, RewriteMode: mode, Payload: payload, DraftContent: content,
				Summary: a.Summary, HookType: a.HookType,
				DominantStrand: a.DominantStrand, StartedAt: now, UpdatedAt: now}
			if err := t.store.Signals.SavePendingCommit(pending); err != nil {
				return nil, fmt.Errorf("save rewrite pending commit: %w: %w", errs.ErrStoreWrite, err)
			}
			return t.executeRewriteCommit(a, progress, pending, false)
		}
		return t.buildSkipResult(a.Chapter, progress)
	}

	// Lần nộp mới phải qua kiểm tra giai đoạn/hàng chờ làm lại hiện hành;
	// PendingCommit thường đã có là giao thức khôi phục, cho phép bỏ qua cửa
	// sổ "Progress đã ghi trước/Phase đã hoàn tất" để tiếp tục chốt.
	if existingPending == nil {
		if err := t.store.Progress.ValidateChapterWork(a.Chapter); err != nil {
			// Xung đột hàng chờ giữ nguyên (đã có phân loại ErrToolConflict);
			// các lỗi I/O khác quy về Precondition.
			if errors.Is(err, errs.ErrToolConflict) {
				return nil, err
			}
			return nil, fmt.Errorf("chương hiện không được phép nộp: %w: %w", errs.ErrToolPrecondition, err)
		}
		if progress.Flow != domain.FlowRewriting && progress.Flow != domain.FlowPolishing {
			expected := progress.NextChapter()
			if a.Chapter != expected {
				return nil, fmt.Errorf("viết tiếp bình thường chỉ nộp được chương kế tiếp %d, nhận chương %d: %w", expected, a.Chapter, errs.ErrToolConflict)
			}
		}
	}

	// Chặn vượt phạm vi ở chế độ phân tầng: phải diễn ra trước mọi thao tác ghi,
	// nếu không một lần commit vượt phạm vi sẽ làm hỏng cả tệp chương, tóm tắt
	// lẫn Progress. boundary được tái sử dụng cho bước 6b bên dưới để tính tín
	// hiệu cung/tập.
	var boundary *store.ArcBoundary
	if progress.Layered {
		b, bErr := t.store.Outline.CheckArcBoundary(a.Chapter)
		if bErr != nil {
			return nil, fmt.Errorf("kiểm tra ranh giới cung thất bại chapter=%d: %w: %w", a.Chapter, errs.ErrStoreRead, bErr)
		}
		if b == nil {
			return nil, fmt.Errorf(
				"chương %d không nằm trong phạm vi dàn ý phân tầng: trước khi viết phải gọi expand_arc để khai triển cung hoặc append_volume để thêm tập; nếu toàn bộ sách đã hoàn thành thì gọi save_foundation type=complete_book: %w",
				a.Chapter, errs.ErrToolPrecondition)
		}
		boundary = b
	}

	// 1. Đóng băng nội dung truyện của chương. Lần nộp đầu đọc từ bản nháp và
	// ghi kèm vào PendingCommit; khi khôi phục chỉ dùng đúng ảnh chụp đó, tránh
	// tạo ra tình trạng "sự thật cũ + nội dung mới" do Worker mới ghi đè bản
	// nháp trước khi thử lại.
	var content string
	if existingPending != nil {
		content = existingPending.DraftContent
		if content == "" {
			return nil, fmt.Errorf("lần nộp chưa hoàn tất của chương %d thiếu draft_content, không thể chứng minh nội dung khôi phục trùng với lần nộp gốc: %w",
				a.Chapter, errs.ErrToolConflict)
		}
	} else {
		var loadErr error
		content, _, loadErr = t.store.Drafts.LoadChapterContent(a.Chapter)
		if loadErr != nil {
			return nil, fmt.Errorf("load chapter content: %w: %w", errs.ErrStoreRead, loadErr)
		}
	}
	if content == "" {
		return nil, fmt.Errorf("no content found for chapter %d: %w", a.Chapter, errs.ErrToolPrecondition)
	}
	// Tiêu đề do engine dựng: model thường bỏ sót dòng tiêu đề, viết sai cấp
	// đề hoặc dùng lại số chương sai từ thời gian quy hoạch.
	content = domain.ApplyChapterHeading(content, a.Title, a.Chapter)
	wordCount := utf8.RuneCountInString(content)

	var pending domain.PendingCommit
	if existingPending != nil {
		pending = *existingPending
	} else {
		payload, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("marshal commit payload: %w", err)
		}
		now := time.Now().Format(time.RFC3339)
		pending = domain.PendingCommit{
			Chapter: a.Chapter, Stage: domain.CommitStageStarted, Payload: payload, DraftContent: content,
			Summary: a.Summary, HookType: a.HookType, DominantStrand: a.DominantStrand,
			StartedAt: now, UpdatedAt: now,
		}
		if err := t.store.Signals.SavePendingCommit(pending); err != nil {
			return nil, fmt.Errorf("save pending commit: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	// StageStarted có thể nghĩa là chưa ghi hạng mục nào, cũng có thể là crash
	// giữa chừng lúc cập nhật trạng thái; mọi thao tác của tải trọng đầy đủ đều
	// phải idempotent nên thống nhất phát lại. StageStateApplied thì đi thẳng
	// sang Progress.
	if pending.Stage == domain.CommitStageStarted {
		// 2. Lưu bản chính
		if err := t.store.Drafts.SaveFinalChapter(a.Chapter, content); err != nil {
			return nil, fmt.Errorf("save final chapter: %w: %w", errs.ErrStoreWrite, err)
		}
		style, err := t.chapterStyleDelta(a.Chapter)
		if err != nil {
			return nil, fmt.Errorf("load chapter style: %w: %w", errs.ErrStoreRead, err)
		}
		if _, err := t.store.ChapterRecords.Accept(a.Chapter, domain.ChapterOriginGenerated, content, a.ChapterFacts, style); err != nil {
			return nil, fmt.Errorf("save chapter record: %w: %w", errs.ErrStoreWrite, err)
		}

		// 3. Lưu tóm tắt
		summary := domain.ChapterSummary{
			Chapter: a.Chapter, Title: a.Title, Summary: a.Summary, Characters: a.Characters, KeyEvents: a.KeyEvents,
		}
		if err := t.store.Summaries.SaveSummary(summary); err != nil {
			return nil, fmt.Errorf("save summary: %w: %w", errs.ErrStoreWrite, err)
		}

		// 4. Cập nhật các gia tăng trạng thái
		if len(a.TimelineEvents) > 0 {
			for i := range a.TimelineEvents {
				a.TimelineEvents[i].Chapter = a.Chapter
			}
			if err := t.store.World.AppendTimelineEvents(a.TimelineEvents); err != nil {
				return nil, fmt.Errorf("append timeline: %w: %w", errs.ErrStoreWrite, err)
			}
		}
		if len(a.ForeshadowUpdates) > 0 {
			if err := t.store.World.UpdateForeshadow(a.Chapter, a.ForeshadowUpdates); err != nil {
				return nil, fmt.Errorf("update foreshadow: %w: %w", errs.ErrStoreWrite, err)
			}
		}
		if len(a.RelationshipChanges) > 0 {
			for i := range a.RelationshipChanges {
				a.RelationshipChanges[i].Chapter = a.Chapter
			}
			if err := t.store.World.UpdateRelationships(a.RelationshipChanges); err != nil {
				return nil, fmt.Errorf("update relationships: %w: %w", errs.ErrStoreWrite, err)
			}
		}
		if len(a.StateChanges) > 0 {
			for i := range a.StateChanges {
				a.StateChanges[i].Chapter = a.Chapter
			}
			if err := t.store.World.AppendStateChanges(a.StateChanges); err != nil {
				return nil, fmt.Errorf("append state changes: %w: %w", errs.ErrStoreWrite, err)
			}
		}

		// 4b. Cộng dồn danh mục nhân vật phụ: các nhân vật không phải lõi xuất
		// hiện trong chương được đưa vào cast_ledger để novel_context gọi lại.
		// Khi thất bại chỉ ghi cảnh báo, không chặn commit — danh mục này là dữ
		// liệu thứ cấp và có thể tự lành khi nộp chương kế tiếp.
		if len(a.Characters) > 0 {
			coreNames, err := loadCoreCharacterNameSet(t.store)
			if err != nil {
				return nil, fmt.Errorf("load core characters for cast ledger: %w: %w", errs.ErrStoreRead, err)
			}
			if err := t.store.Cast.MergeAppearances(a.Chapter, a.Characters, a.CastIntros, coreNames); err != nil {
				slog.Warn("cộng dồn danh mục nhân vật phụ thất bại, bỏ qua", "module", "commit", "chapter", a.Chapter, "err", err)
			}
		}

		pending.Stage = domain.CommitStageStateApplied
		pending.UpdatedAt = time.Now().Format(time.RFC3339)
		if err := t.store.Signals.SavePendingCommit(pending); err != nil {
			return nil, fmt.Errorf("update pending commit stage: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	// 5. Cập nhật tiến độ
	if !completed {
		if err := t.store.Progress.MarkChapterComplete(a.Chapter, wordCount, a.HookType, a.DominantStrand); err != nil {
			return nil, fmt.Errorf("mark chapter complete: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	// 6. Quyết định có cần thẩm duyệt hay không
	progress, err = t.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	completedCount := 0
	if progress != nil {
		completedCount = len(progress.CompletedChapters)
	}

	// 6b. Tín hiệu cung/tập ở chế độ dài: boundary đã được kiểm tra trước ở cổng
	// vào, khi Layered thì bảo đảm khác nil
	var arcEnd, volumeEnd, needsExpansion, needsNewVolume bool
	var vol, arc, nextVol, nextArc int
	if progress != nil && progress.Layered && boundary != nil {
		arcEnd = boundary.IsArcEnd
		volumeEnd = boundary.IsVolumeEnd
		vol = boundary.Volume
		arc = boundary.Arc
		needsExpansion = boundary.NeedsExpansion
		needsNewVolume = boundary.NeedsNewVolume
		nextVol = boundary.NextVolume
		nextArc = boundary.NextArc
		if err := t.store.Progress.UpdateVolumeArc(vol, arc); err != nil {
			return nil, fmt.Errorf("update volume/arc: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	var reviewRequired bool
	var reviewReason string
	if progress != nil && progress.Layered {
		reviewRequired, reviewReason = domain.ShouldArcReview(arcEnd, volumeEnd, vol, arc)
	} else {
		reviewRequired, reviewReason = domain.ShouldReview(completedCount)
	}

	// 7. Dựng tín hiệu có cấu trúc
	result := domain.CommitResult{
		Chapter:        a.Chapter,
		Committed:      true,
		WordCount:      wordCount,
		NextChapter:    a.Chapter + 1,
		ReviewRequired: reviewRequired,
		ReviewReason:   reviewReason,
		HookType:       a.HookType,
		DominantStrand: a.DominantStrand,
		Feedback:       a.Feedback,
		// (feedback đồng thời được lưu xuống hồ phản hồi, xem persistFeedback
		// bên dưới — giá trị trả về chỉ là bản sao, kiến trúc sư tiêu thụ
		// qua novel_context là sự thật trong store)
		ArcEnd:         arcEnd,
		VolumeEnd:      volumeEnd,
		Volume:         vol,
		Arc:            arc,
		NeedsExpansion: needsExpansion,
		NeedsNewVolume: needsNewVolume,
		NextVolume:     nextVol,
		NextArc:        nextArc,
	}

	// 8. Quyết định trạng thái hoàn thành: sách không phân tầng viết xong chương
	// cuối / sách phân tầng ở chương cuối của tập cuối → MarkComplete
	bookComplete, err := t.applyCompletion(&result, progress)
	if err != nil {
		return nil, err
	}
	if bookComplete {
		result.BookComplete = true
	}
	latestProgress, err := t.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("load progress after completion: %w: %w", errs.ErrStoreRead, err)
	}
	if latestProgress != nil {
		result.Flow = string(latestProgress.Flow)
	}

	// 8.5. Hồ phản hồi là sự thật lưu lâu dài cho các lần quy hoạch sau, được
	// Kiến trúc sư tiêu thụ ở lần thao tác cấu trúc kế tiếp.
	if a.Feedback != nil && (strings.TrimSpace(a.Feedback.Deviation) != "" || strings.TrimSpace(a.Feedback.Suggestion) != "") {
		if err := t.store.Outline.AppendOutlineFeedback(store.ChapterFeedback{
			Chapter: a.Chapter, Deviation: a.Feedback.Deviation, Suggestion: a.Feedback.Suggestion,
		}); err != nil {
			return nil, fmt.Errorf("persist outline feedback: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	// Luật cơ học là một phần của đầu ra, phải được đóng băng trước ProgressMarked
	// để khi khôi phục trả về đúng đầu ra cũ.
	violations := t.checkRules(content, a.Chapter)
	output, err := json.Marshal(commitOutput{CommitResult: result, RuleViolations: violations})
	if err != nil {
		return nil, fmt.Errorf("marshal commit output: %w", err)
	}

	pending.Stage = domain.CommitStageProgressMarked
	pending.Result = &result
	pending.Output = output
	pending.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := t.store.Signals.SavePendingCommit(pending); err != nil {
		return nil, fmt.Errorf("update pending commit result: %w: %w", errs.ErrStoreWrite, err)
	}

	// 9. Thêm checkpoint. Phải làm trước khi xóa pending_commit để bảo đảm
	// pending_commit còn nhìn thấy được sau khi khởi động lại luôn có thể dẫn
	// dắt lần chạy lại để bù các checkpoint còn thiếu.
	if err := t.appendCommitCheckpoint(a.Chapter); err != nil {
		return nil, fmt.Errorf("checkpoint commit: %w: %w", errs.ErrStoreWrite, err)
	}
	pending.Stage = domain.CommitStageSignalSaved
	pending.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := t.store.Signals.SavePendingCommit(pending); err != nil {
		return nil, fmt.Errorf("update pending commit checkpoint stage: %w: %w", errs.ErrStoreWrite, err)
	}

	// 10. Xóa trạng thái trung gian của tiến độ
	if err := t.store.Progress.ClearInProgress(); err != nil {
		return nil, fmt.Errorf("clear in-progress: %w: %w", errs.ErrStoreWrite, err)
	}
	if err := t.store.Signals.ClearPendingCommit(); err != nil {
		return nil, fmt.Errorf("clear pending commit: %w: %w", errs.ErrStoreWrite, err)
	}

	// Lưu sự thật vi phạm luật: biên tập viên tiêu thụ qua novel_context (giá
	// trị trả về chỉ là bản sao — sau commit Người viết dừng cứng ngay, không ai
	// đọc giá trị trả về). best-effort.
	if err := t.store.World.SaveRuleViolations(a.Chapter, violations); err != nil {
		slog.Warn("lưu vi phạm luật cơ học thất bại", "module", "tools", "chapter", a.Chapter, "err", err)
	}
	t.refreshStyleStats(a.Chapter, content)
	return output, nil
}

// finishPendingCommit chốt cửa sổ gián đoạn ProgressMarked/SignalSaved. Việc thêm
// checkpoint là idempotent theo digest; chỉ khi cả checkpoint lẫn dọn trạng thái
// trung gian đều thành công mới xóa bản ghi khôi phục.
func (t *CommitChapterTool) finishPendingCommit(pending domain.PendingCommit, progress *domain.Progress) (json.RawMessage, error) {
	if pending.Stage == domain.CommitStageProgressMarked {
		if err := t.appendCommitCheckpoint(pending.Chapter); err != nil {
			return nil, fmt.Errorf("checkpoint commit: %w: %w", errs.ErrStoreWrite, err)
		}
		pending.Stage = domain.CommitStageSignalSaved
		pending.UpdatedAt = time.Now().Format(time.RFC3339)
		if err := t.store.Signals.SavePendingCommit(pending); err != nil {
			return nil, fmt.Errorf("update pending commit checkpoint stage: %w: %w", errs.ErrStoreWrite, err)
		}
	}
	if err := t.store.Progress.ClearInProgress(); err != nil {
		return nil, fmt.Errorf("clear in-progress: %w: %w", errs.ErrStoreWrite, err)
	}
	if err := t.store.Signals.ClearPendingCommit(); err != nil {
		return nil, fmt.Errorf("clear pending commit: %w: %w", errs.ErrStoreWrite, err)
	}
	t.refreshStyleStats(pending.Chapter, pending.DraftContent)
	if len(pending.Output) > 0 {
		return append(json.RawMessage(nil), pending.Output...), nil
	}
	if pending.Result != nil {
		return json.Marshal(pending.Result)
	}
	return t.buildSkipResult(pending.Chapter, progress)
}

func (t *CommitChapterTool) validateRewriteDraft(chapter int, title string, progress *domain.Progress) (string, error) {
	content, _, err := t.store.Drafts.LoadChapterContent(chapter)
	if err != nil {
		return "", fmt.Errorf("rewrite: load chapter content: %w: %w", errs.ErrStoreRead, err)
	}
	if content == "" {
		return "", fmt.Errorf("no content found for chapter %d: %w", chapter, errs.ErrToolPrecondition)
	}
	changed, err := t.rewriteChanged(chapter, content, title)
	if err != nil {
		return "", err
	}
	if changed {
		return content, nil
	}
	mode := "viết lại"
	if progress != nil && progress.Flow == domain.FlowPolishing {
		mode = "hoàn thiện"
	}
	return "", fmt.Errorf("cả nội dung truyện và tiêu đề của chương %d đều không thay đổi, không phát hiện thay đổi %s: %w",
		chapter, mode, errs.ErrToolPrecondition)
}

func (t *CommitChapterTool) rewriteChanged(chapter int, content, title string) (bool, error) {
	existingFinal, err := t.store.Drafts.LoadChapterText(chapter)
	if err != nil {
		return false, fmt.Errorf("rewrite: load final chapter: %w: %w", errs.ErrStoreRead, err)
	}
	if existingFinal != content {
		return true, nil
	}
	summary, err := t.store.Summaries.LoadSummary(chapter)
	if err != nil {
		return false, fmt.Errorf("rewrite: load chapter summary: %w: %w", errs.ErrStoreRead, err)
	}
	return summary == nil || strings.TrimSpace(summary.Title) != strings.TrimSpace(title), nil
}

func (t *CommitChapterTool) appendCommitCheckpoint(chapter int) error {
	_, err := t.store.Checkpoints.AppendArtifacts(
		domain.ChapterScope(chapter), "commit",
		fmt.Sprintf("chapters/%02d.md", chapter),
		fmt.Sprintf("summaries/%02d.json", chapter),
		store.ChapterRecordPath(chapter),
	)
	return err
}

// checkRules kiểm tra cơ học nội dung truyện của chương: Lint đường đáy sản
// phẩm tích hợp (dấu vết cơ chế, luôn chạy) + Check theo quy tắc người dùng (đọc
// bản structured của cuốn sách này; nếu thiếu bản chụp thì lùi về mặc định
// tích hợp, bảo đảm đường đáy cơ học luôn tồn tại).
func (t *CommitChapterTool) checkRules(text string, chapter int) []rules.Violation {
	violations := rules.Lint(text)
	structured := rules.SystemDefaults().Structured
	if snap, err := t.store.UserRules.Load(); err == nil && snap != nil {
		structured = snap.Structured
	}
	violations = append(violations, rules.Check(text, structured)...)
	return append(violations, rules.CheckAgainstPrevious(text, t.previousChapters(chapter))...)
}

// previousChapters đọc vài chương bản chính liền kề trước, phục vụ đối chiếu
// "chương mới có sao chép chương trước không".
// Chương đọc lỗi hoặc rỗng đều bị bỏ qua: đây chỉ là phép đo bổ sung, không
// được làm sập nhánh chính của việc nộp.
func (t *CommitChapterTool) previousChapters(chapter int) []string {
	const lookback = 2
	var out []string
	for ch := chapter - 1; ch >= 1 && ch >= chapter-lookback; ch-- {
		text, err := t.store.Drafts.LoadChapterText(ch)
		if err != nil || strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

// executeRewriteCommit xử lý việc nộp cho chương được hoàn thiện/viết lại: ghi đè
// bản chính và tóm tắt, cập nhật số từ, rút hàng chờ.
// Bỏ qua mọi thao tác ghi trạng thái thế giới (timeline / foreshadow /
// relationship / state_changes) và kiểm tra ranh giới cung — những thứ này đã
// được áp dụng khi chương được nộp lần đầu.
func (t *CommitChapterTool) executeRewriteCommit(a commitArgs, progress *domain.Progress, pending domain.PendingCommit, recovering bool) (json.RawMessage, error) {
	chapter := a.Chapter
	// 1. Chỉ dùng nội dung truyện làm lại đã đóng băng ở lần nộp đầu tiên;
	// khi khôi phục sau crash không được lấy bản nháp bị ghi đè về sau.
	content := pending.DraftContent
	if content == "" {
		return nil, fmt.Errorf("lần nộp làm lại của chương %d thiếu draft_content, không thể khôi phục an toàn: %w", chapter, errs.ErrToolConflict)
	}
	content = domain.ApplyChapterHeading(content, a.Title, chapter)
	wordCount := utf8.RuneCountInString(content)

	// 2. Nội dung truyện hoặc tiêu đề phải thay đổi ít nhất một cái; hoàn thiện
	// tiêu đề không cần bịa ra thay đổi nội dung truyện.
	if !recovering {
		changed, err := t.rewriteChanged(chapter, content, a.Title)
		if err != nil {
			return nil, err
		}
		if !changed {
			mode := "viết lại"
			if progress != nil && progress.Flow == domain.FlowPolishing {
				mode = "hoàn thiện"
			}
			return nil, fmt.Errorf("cả nội dung truyện và tiêu đề của chương %d đều không thay đổi, không phát hiện thay đổi %s: %w",
				chapter, mode, errs.ErrToolPrecondition)
		}
	}

	if pending.Stage == domain.CommitStageStarted {
		// 3. Trước tiên dựng toàn bộ tập bản ghi ứng viên rồi phát lại kiểm tra.
		// Bản triển khai cũ ghi đè bản ghi rồi mới dựng lại hình chiếu; chỉ cần
		// chuỗi sự thật khép kín sai là tải trọng lỗi nằm lại trên đĩa và mọi
		// lần thử lại sau đó đều đọc một nền hỏng.
		existing, err := t.store.ChapterRecords.Load(chapter)
		if err != nil {
			return nil, fmt.Errorf("rewrite: load chapter record: %w: %w", errs.ErrStoreRead, err)
		}
		var existingUpdates []domain.ForeshadowUpdate
		var style domain.StyleDelta
		if existing != nil {
			existingUpdates = existing.Facts.ForeshadowUpdates
			style = existing.StyleDelta
		}
		recovered, err := t.restoreRewritePlants(chapter, existingUpdates, &a.ChapterFacts)
		if err != nil {
			return nil, err
		}
		candidate, err := t.store.ChapterRecords.Prepare(
			chapter, domain.ChapterOriginGenerated, content, a.ChapterFacts, style,
		)
		if err != nil {
			return nil, fmt.Errorf("rewrite: prepare chapter record: %w: %w", errs.ErrStoreRead, err)
		}
		chapters := slices.Clone(progress.CompletedChapters)
		slices.Sort(chapters)
		records := make([]domain.ChapterRecord, 0, len(chapters))
		for _, completedChapter := range chapters {
			if completedChapter == chapter {
				records = append(records, *candidate)
				continue
			}
			record, err := t.store.ChapterRecords.Load(completedChapter)
			if err != nil {
				return nil, fmt.Errorf("rewrite: load chapter record %d: %w: %w", completedChapter, errs.ErrStoreRead, err)
			}
			if record == nil {
				return nil, fmt.Errorf("rewrite: chương %d thiếu bản ghi đã nhận: %w", completedChapter, errs.ErrToolConflict)
			}
			records = append(records, *record)
		}
		if err := revision.ValidateRecords(records); err != nil {
			if clearErr := t.store.Signals.ClearPendingCommit(); clearErr != nil {
				return nil, fmt.Errorf("rewrite: kiểm tra chuỗi sự thật của chương thất bại (%v), đồng thời dọn lần nộp đóng băng cũng thất bại: %w: %w", err, errs.ErrStoreWrite, clearErr)
			}
			return nil, fmt.Errorf("rewrite: kiểm tra chuỗi sự thật của chương thất bại, đã gỡ đóng băng và chưa ghi kết quả làm lại: %w: %w", errs.ErrToolPrecondition, err)
		}

		// 4. Chỉ ghi đè bản ghi chuẩn và bản chính sau khi kiểm tra đã đạt; cùng
		// một tải trọng đã đóng băng phát lại được an toàn.
		if err := t.store.Drafts.SaveFinalChapter(chapter, content); err != nil {
			return nil, fmt.Errorf("rewrite: save final chapter: %w: %w", errs.ErrStoreWrite, err)
		}
		if err := t.store.ChapterRecords.Save(*candidate); err != nil {
			return nil, fmt.Errorf("rewrite: save chapter record: %w: %w", errs.ErrStoreWrite, err)
		}
		if len(recovered) > 0 {
			slog.Warn("Đã khôi phục sự kiện gieo mầm bị mất từ sổ phục bút của phiên bản cũ", "module", "commit", "chapter", chapter, "foreshadows", recovered)
		}
		if err := revision.NewProjector(t.store).Apply(records); err != nil {
			return nil, fmt.Errorf("rewrite: rebuild chapter projections: %w: %w", errs.ErrStoreWrite, err)
		}
		if err := t.store.Summaries.SaveSummary(domain.ChapterSummary{
			Chapter: chapter, Title: a.Title, Summary: a.Summary, Characters: a.Characters, KeyEvents: a.KeyEvents,
		}); err != nil {
			return nil, fmt.Errorf("rewrite: save summary: %w: %w", errs.ErrStoreWrite, err)
		}
		pending.Stage = domain.CommitStageStateApplied
		pending.UpdatedAt = time.Now().Format(time.RFC3339)
		if err := t.store.Signals.SavePendingCommit(pending); err != nil {
			return nil, fmt.Errorf("rewrite: update pending state stage: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	// 5. Cập nhật số từ (MarkChapterComplete là idempotent với chương đã hoàn
	// thành: thay số từ, slice.Contains ngăn vào hàng chờ hai lần)
	if progress.Phase != domain.PhaseComplete {
		if err := t.store.Progress.MarkChapterComplete(chapter, wordCount, a.HookType, a.DominantStrand); err != nil {
			return nil, fmt.Errorf("rewrite: update word count: %w: %w", errs.ErrStoreWrite, err)
		}

		// 6. Rút hàng chờ đang chờ; khi hàng chờ rỗng, CompleteRewrite tự chuyển
		// flow về writing
		if err := t.store.Progress.CompleteRewrite(chapter); err != nil {
			return nil, fmt.Errorf("rewrite: complete rewrite: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	// 7. Đọc ảnh chụp Progress sau khi rút hàng chờ, trả về như sự thật
	mode := pending.RewriteMode
	if mode == "" {
		mode = "rewrite"
	}
	latest, err := t.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("rewrite: load progress after drain: %w: %w", errs.ErrStoreRead, err)
	}
	remaining := []int{}
	nextChapter := chapter + 1
	flow := string(domain.FlowWriting)
	if latest != nil {
		remaining = append(remaining, latest.PendingRewrites...)
		nextChapter = latest.NextChapter()
		flow = string(latest.Flow)
	}
	drained := len(remaining) == 0

	// Chỉ quyết định hoàn thành sau khi hàng chờ đã rỗng: lần nộp làm lại không
	// đi qua nhánh chính applyCompletion nên chỉ có thể kích hoạt tại đây.
	//   - Phân tầng + viết tiến thuận: dùng tổng phán đoán layeredComplete
	//     (tập chốt hạt cấu trúc đã xong / chưa tuyên bố thì đi mức chất lượng).
	//   - Phân tầng + làm lại sau reopen (ReopenedFromComplete): làm lại chỉ sửa
	//     chương có sẵn, không tăng/giảm cấu trúc, nên đủ cấu trúc là hoàn thành
	//     lại — nếu vì làm lại mà một tuyến manh mối bị lung lay mà kẹt ở
	//     writing, cuối tập sẽ rơi vào vòng lặp viết tiến vượt phạm vi.
	//   - Không phân tầng: viết đủ TotalChapters là hoàn thành (làm lại không
	//     đổi số chương, vốn đã đủ).
	bookComplete := false
	if drained && latest != nil {
		reComplete := false
		switch {
		case latest.Layered && latest.ReopenedFromComplete:
			reComplete, err = layeredStructurallyComplete(t.store, latest)
		case latest.Layered:
			reComplete, err = layeredComplete(t.store, latest)
		default:
			reComplete = latest.TotalChapters > 0 && len(latest.CompletedChapters) >= latest.TotalChapters
		}
		if err != nil {
			return nil, fmt.Errorf("rewrite: evaluate completion: %w: %w", errs.ErrStoreRead, err)
		}
		if reComplete {
			if err := t.store.Progress.MarkComplete(); err != nil {
				return nil, fmt.Errorf("rewrite: mark complete: %w: %w", errs.ErrStoreWrite, err)
			}
			bookComplete = true
			p, err := t.store.Progress.Load()
			if err != nil {
				return nil, fmt.Errorf("rewrite: reload completed progress: %w: %w", errs.ErrStoreRead, err)
			}
			if p != nil {
				flow = string(p.Flow)
			}
		}
	}

	// Giống nhánh chính: rewrite/polish cũng chạy kiểm tra cơ học và lưu lại
	// (sau khi viết lại sẽ có bản ghi mới, các vi phạm cũ được coi là đã gỡ).
	violations := t.checkRules(content, a.Chapter)
	output, err := json.Marshal(map[string]any{
		"chapter": chapter, "rewritten": true, "mode": mode, "word_count": wordCount,
		"remaining_queue": remaining, "queue_drained": drained, "next_chapter": nextChapter,
		"flow": flow, "book_complete": bookComplete, "rule_violations": violations,
	})
	if err != nil {
		return nil, fmt.Errorf("rewrite: marshal output: %w", err)
	}
	pending.Stage = domain.CommitStageProgressMarked
	pending.Output = output
	pending.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := t.store.Signals.SavePendingCommit(pending); err != nil {
		return nil, fmt.Errorf("rewrite: update pending progress stage: %w: %w", errs.ErrStoreWrite, err)
	}

	// 8. Thêm checkpoint, rồi đánh dấu signal_saved, cuối cùng dọn PendingCommit.
	if err := t.appendCommitCheckpoint(chapter); err != nil {
		return nil, fmt.Errorf("rewrite: checkpoint commit: %w: %w", errs.ErrStoreWrite, err)
	}
	pending.Stage = domain.CommitStageSignalSaved
	pending.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := t.store.Signals.SavePendingCommit(pending); err != nil {
		return nil, fmt.Errorf("rewrite: update pending checkpoint stage: %w: %w", errs.ErrStoreWrite, err)
	}
	if err := t.store.Progress.ClearInProgress(); err != nil {
		return nil, fmt.Errorf("rewrite: clear in-progress: %w: %w", errs.ErrStoreWrite, err)
	}
	if err := t.store.Signals.ClearPendingCommit(); err != nil {
		return nil, fmt.Errorf("rewrite: clear pending commit: %w: %w", errs.ErrStoreWrite, err)
	}

	if err := t.store.World.SaveRuleViolations(chapter, violations); err != nil {
		slog.Warn("lưu vi phạm luật cơ học thất bại", "module", "tools", "chapter", chapter, "err", err)
	}
	t.refreshStyleStats(chapter, content)
	return output, nil
}

// restoreRewritePlants chỉ khắc phục đúng một dạng hỏng mà phiên bản cũ đã gây
// ra: sổ phục bút vẫn ghi lần gieo của chương này, nhưng bản ghi đã nhận của
// chương đã bị lần làm lại thất bại ghi đè. Sổ cho đủ id, mô tả và chương gieo
// nên có thể khôi phục một cách tất định; các bất khớp khác vẫn báo lỗi tường
// minh, không đoán bừa sự thật cốt truyện.
func (t *CommitChapterTool) restoreRewritePlants(chapter int, existing []domain.ForeshadowUpdate, facts *domain.ChapterFacts) ([]string, error) {
	planted := make(map[string]struct{}, len(existing)+len(facts.ForeshadowUpdates))
	for _, update := range existing {
		if update.Action == "plant" {
			planted[update.ID] = struct{}{}
		}
	}
	for _, update := range facts.ForeshadowUpdates {
		if update.Action == "plant" {
			planted[update.ID] = struct{}{}
		}
	}

	ledger, err := t.store.World.LoadForeshadowLedger()
	if err != nil {
		return nil, fmt.Errorf("rewrite: load foreshadow ledger for recovery: %w: %w", errs.ErrStoreRead, err)
	}
	var restored []domain.ForeshadowUpdate
	var ids []string
	for _, entry := range ledger {
		if entry.PlantedAt != chapter {
			continue
		}
		if _, ok := planted[entry.ID]; ok {
			continue
		}
		if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.Description) == "" {
			return nil, fmt.Errorf("rewrite: sổ phục bút chương %d thiếu id hoặc description có thể khôi phục: %w", chapter, errs.ErrToolConflict)
		}
		planted[entry.ID] = struct{}{}
		restored = append(restored, domain.ForeshadowUpdate{
			ID: entry.ID, Action: "plant", Description: entry.Description,
		})
		ids = append(ids, entry.ID)
	}
	if len(restored) > 0 {
		facts.ForeshadowUpdates = append(restored, facts.ForeshadowUpdates...)
	}
	return ids, nil
}

func (t *CommitChapterTool) refreshStyleStats(chapter int, content string) {
	if content == "" {
		var err error
		content, err = t.store.Drafts.LoadChapterText(chapter)
		if err != nil {
			slog.Error("cập nhật chỉ mục thống kê văn phong thất bại", "module", "tools", "chapter", chapter, "err", err)
			return
		}
		if content == "" {
			slog.Error("cập nhật chỉ mục thống kê văn phong thất bại", "module", "tools", "chapter", chapter, "err", errors.New("bản chính không tồn tại"))
			return
		}
	}
	t.styleStats.ChapterCommitted(chapter, content)
}

// buildSkipResult dựng giá trị trả về dạng sự thật cho trường hợp "nộp lại một
// chương đã hoàn thành", căn cứ với lần commit thường.
// Bộ điều phối dựa vào đó để ra quyết định tiếp theo (phân phối writer/editor/
// architect) mà không bị mê hoặc vì nhận được chỉ dẫn prose.
func (t *CommitChapterTool) buildSkipResult(chapter int, progress *domain.Progress) (json.RawMessage, error) {
	_, wordCount, err := t.store.Drafts.LoadChapterContent(chapter)
	if err != nil {
		return nil, fmt.Errorf("load completed chapter: %w: %w", errs.ErrStoreRead, err)
	}

	result := domain.CommitResult{
		Chapter:     chapter,
		Committed:   true,
		WordCount:   wordCount,
		NextChapter: chapter + 1,
	}

	if progress != nil && progress.Layered {
		boundary, err := t.store.Outline.CheckArcBoundary(chapter)
		if err != nil {
			return nil, fmt.Errorf("check completed chapter boundary: %w: %w", errs.ErrStoreRead, err)
		}
		if boundary != nil {
			result.ArcEnd = boundary.IsArcEnd
			result.VolumeEnd = boundary.IsVolumeEnd
			result.Volume = boundary.Volume
			result.Arc = boundary.Arc
			result.NeedsExpansion = boundary.NeedsExpansion
			result.NeedsNewVolume = boundary.NeedsNewVolume
			result.NextVolume = boundary.NextVolume
			result.NextArc = boundary.NextArc
		}
		result.ReviewRequired, result.ReviewReason = domain.ShouldArcReview(result.ArcEnd, result.VolumeEnd, result.Volume, result.Arc)
	} else if progress != nil {
		result.ReviewRequired, result.ReviewReason = domain.ShouldReview(len(progress.CompletedChapters))
	}

	if progress != nil {
		if progress.Phase == domain.PhaseComplete {
			result.BookComplete = true
		}
		result.Flow = string(progress.Flow)
	}

	return json.Marshal(result)
}

// loadCoreCharacterNameSet tải tập tên nhân vật đã có trong characters.json (gồm
// cả bí danh).
// Dùng làm bộ lọc "nhân vật lõi đã biết" cho cast_ledger — nhân vật lõi không
// đưa vào danh mục phụ.
// Khi tải lỗi trả về nil (lúc merge mọi characters đều vào ledger, vẫn chấp
// nhận được).
func loadCoreCharacterNameSet(s *store.Store) (map[string]bool, error) {
	chars, err := s.Characters.Load()
	if err != nil {
		return nil, err
	}
	if len(chars) == 0 {
		return nil, nil
	}
	set := make(map[string]bool, len(chars)*2)
	for _, c := range chars {
		if c.Name != "" {
			set[c.Name] = true
		}
		for _, alias := range c.Aliases {
			if alias != "" {
				set[alias] = true
			}
		}
	}
	return set, nil
}

// applyCompletion phán đoán lần commit này có làm cho toàn bộ sách hoàn thành
// hay không; nếu có thì MarkComplete và trả về true.
//   - Không phân tầng: viết đủ số chương đã thỏa thuận là hoàn thành.
//   - Phân tầng: đường chính là kiến trúc sư gọi tường minh save_foundation
//     type=complete_book; ở đây có thêm một lớp chốt tất định (xem
//     layeredComplete) — để chặn trường hợp model ở điểm kết thúc vừa không
//     append_volume vừa không complete_book, dẫn tới "người viết chạy thẳng
//     chương vượt phạm vi → bộ chặn vượt phạm vi chặn → thử lại lặp lại" rồi
//     treo vòng lặp (gốc rễ của ca chương 204..347 của 《凡骨》).
func (t *CommitChapterTool) applyCompletion(result *domain.CommitResult, progress *domain.Progress) (bool, error) {
	if progress == nil {
		return false, nil
	}
	if progress.Phase == domain.PhaseComplete {
		return true, nil
	}
	if progress.Layered {
		complete, err := layeredComplete(t.store, progress)
		if err != nil {
			return false, fmt.Errorf("evaluate layered completion: %w: %w", errs.ErrStoreRead, err)
		}
		if complete {
			if err := t.store.Progress.MarkComplete(); err != nil {
				return false, fmt.Errorf("mark book complete: %w: %w", errs.ErrStoreWrite, err)
			}
			return true, nil
		}
		return false, nil
	}
	if progress.TotalChapters > 0 && result.NextChapter > progress.TotalChapters {
		if err := t.store.Progress.MarkComplete(); err != nil {
			return false, fmt.Errorf("mark book complete: %w: %w", errs.ErrStoreWrite, err)
		}
		return true, nil
	}
	return false, nil
}

// ── Phán đoán hoàn thành cho sách phân tầng (cấp gói: commit_chapter và
// save_volume_summary dùng chung hai điểm kích hoạt) ──
//
// Kiểm tra hoàn thành luôn diễn ra trong công cụ "nơi mảnh sự thật cuối cùng
// được đặt xuống":
//   - Chưa tuyên bố tập chốt hạt: commit chương cuối (mức chất lượng
//     layeredBookComplete)
//   - Đã tuyên bố tập chốt hạt: mảnh ghép cuối cùng của nhánh chính là bộ ba
//     chốt cuối tập (thẩm duyệt → tóm tắt cung → tóm tắt tập), nên điểm kích
//     hoạt nằm ở save_volume_summary; khi bộ ba đã đủ sau khi rút hàng chờ làm
//     lại thì commit kích hoạt.

// layeredStructurallyComplete phán đoán sách dài phân tầng đã "viết xong về
// cấu trúc" chưa: hàng chờ làm lại rỗng + không còn cung khung nào chờ khai
// triển + mọi chương đã khai triển đều đã viết. Đây là sự thật trạng thái cuối
// mang tính tất định, không chứa phán đoán ngữ nghĩa về manh mối/tuyến dài —
// dùng làm lưới an toàn "chống vòng lặp trạng thái cuối" (sau khi rút hàng chờ
// làm lại thì hoàn thành lại theo tiêu chí này).
func layeredStructurallyComplete(st *store.Store, progress *domain.Progress) (bool, error) {
	// 1. Hàng chờ làm lại phải rỗng
	if len(progress.PendingRewrites) > 0 {
		return false, nil
	}
	volumes, err := st.Outline.LoadLayeredOutline()
	if err != nil {
		return false, fmt.Errorf("load layered outline: %w", err)
	}
	if len(volumes) == 0 {
		return false, nil
	}
	// 2. Không được còn cung khung nào chờ khai triển (trong kế hoạch vẫn có nội
	// dung cần viết)
	for i := range volumes {
		for j := range volumes[i].Arcs {
			if !volumes[i].Arcs[j].IsExpanded() {
				return false, nil
			}
		}
	}
	// 3. Mọi chương đã khai triển phải được viết xong
	expanded := len(domain.FlattenOutline(volumes))
	return expanded > 0 && len(progress.CompletedChapters) >= expanded, nil
}

// finaleWrapped kiểm tra bộ ba chốt cuối tập của tập chốt hạt (thẩm duyệt cung /
// tóm tắt cung / tóm tắt tập) đã đủ chưa.
// Hoàn thành của tập chốt hạt không đòi manh mối/tuyến dài phải về zero, nhưng
// phải chờ cung cuối qua được cổng chất lượng của biên tập viên — phần kết là
// quan trọng nhất của cả cuốn sách, không thể hoàn thành trước khi có thẩm
// duyệt (có thể đưa vào hàng chờ làm lại) và trước khi tóm tắt được ghi xuống.
func finaleWrapped(st *store.Store, progress *domain.Progress) (bool, error) {
	last := progress.LatestCompleted()
	if last <= 0 {
		return false, nil
	}
	b, err := st.Outline.CheckArcBoundary(last)
	if err != nil {
		return false, fmt.Errorf("check finale boundary: %w", err)
	}
	if b == nil || !b.IsArcEnd {
		return false, nil
	}
	hasReview, err := st.World.HasArcReview(last)
	if err != nil {
		return false, fmt.Errorf("load finale review: %w", err)
	}
	hasArcSummary, err := st.Summaries.HasArcSummary(b.Volume, b.Arc)
	if err != nil {
		return false, fmt.Errorf("load finale arc summary: %w", err)
	}
	hasVolumeSummary, err := st.Summaries.HasVolumeSummary(b.Volume)
	if err != nil {
		return false, fmt.Errorf("load finale volume summary: %w", err)
	}
	return hasReview && hasArcSummary && hasVolumeSummary, nil
}

// layeredComplete là phán đoán hoàn thành tổng thể cho việc viết tiến thuận
// của sách phân tầng:
//   - Đã tuyên bố tập chốt hạt (tập cuối của layered_outline mang cờ final) →
//     cấu trúc đã viết xong + bộ ba chốt cuối tập đủ là hoàn thành, không còn
//     đòi manh mối/tuyến dài về zero. Cả tập chốt hạt lấy việc gom mối làm mục
//     tiêu (kiến trúc sư đã phân bổ tuyến dài/manh mối vào từng cung khi quy
//     hoạch), vài điểm sót thuộc về chất lượng biên tập, không nên kẹt cả cuốn
//     sách ở ngoài trạng thái cuối — nếu không, những cuốn có estimated_scale
//     ước lượng quá cao sẽ không bao giờ hoàn thành hợp lệ (gốc rễ của ca
//     stop guard 140 chương).
//   - Chưa tuyên bố → dùng mức chất lượng layeredBookComplete, chặn trường hợp
//     model vừa không chốt hạt vừa không hoàn thành và kết thúc quá sớm ở chỗ
//     dàn ý đã cạn.
func layeredComplete(st *store.Store, progress *domain.Progress) (bool, error) {
	volumes, err := st.Outline.LoadLayeredOutline()
	if err != nil {
		return false, fmt.Errorf("load layered outline: %w", err)
	}
	if domain.FinaleVolume(volumes) > 0 {
		structurallyComplete, err := layeredStructurallyComplete(st, progress)
		if err != nil || !structurallyComplete {
			return structurallyComplete, err
		}
		return finaleWrapped(st, progress)
	}
	return layeredBookComplete(st, progress)
}

// ReconcileLayeredCompletion bổ sung trạng thái hoàn thành cho sách phân tầng
// dựa trên sự thật đã lưu hiện tại.
// Nhánh thường của save_volume_summary và cơ chế khôi phục sau crash của
// Engine dùng chung cổng này, tránh việc tóm tắt tập đã lưu xuống nhưng
// Progress chưa kịp MarkComplete làm mất vĩnh viễn điểm kích hoạt hoàn thành
// tự động.
func ReconcileLayeredCompletion(st *store.Store) (bool, error) {
	progress, err := st.Progress.Load()
	if err != nil {
		return false, fmt.Errorf("load progress: %w", err)
	}
	if progress == nil || !progress.Layered {
		return false, nil
	}
	if progress.Phase == domain.PhaseComplete {
		return true, nil
	}
	if progress.Phase != domain.PhaseWriting {
		return false, nil
	}
	complete, err := layeredComplete(st, progress)
	if err != nil || !complete {
		return complete, err
	}
	if err := st.Progress.MarkComplete(); err != nil {
		return false, fmt.Errorf("mark complete: %w", err)
	}
	return true, nil
}

// layeredBookComplete dùng sự thật khách quan để phán đoán sách dài phân tầng
// đã thực sự viết xong chưa, đối chiếu với vài mục định lượng được trong danh
// sách phán đoán hoàn thành của architect-long.md cộng với sự thật cấu trúc.
// Trên nền cấu trúc đầy đủ còn đòi manh mối về zero và tuyến dài phải khép
// lại — bất kỳ điều kiện nào chưa đạt thì nhường chỗ cho kiến trúc sư tiếp tục
// expand_arc / append_volume, tuyệt đối không giành chốt trước khi câu chuyện
// còn chưa viết xong. Khi không có compass thì thận trọng coi là chưa hoàn
// thành. Đây là phán đoán hoàn thành "mức chất lượng" dùng khi chưa tuyên bố
// tập chốt hạt, chặt hơn layeredStructurallyComplete.
func layeredBookComplete(st *store.Store, progress *domain.Progress) (bool, error) {
	structurallyComplete, err := layeredStructurallyComplete(st, progress)
	if err != nil || !structurallyComplete {
		return structurallyComplete, err
	}
	// 4. Manh mối đang hoạt động phải về zero (lời hứa đã được thực hiện)
	active, err := st.World.LoadActiveForeshadow()
	if err != nil {
		return false, fmt.Errorf("load active foreshadow: %w", err)
	}
	if len(active) > 0 {
		return false, nil
	}
	// 5. Tuyến dài đang hoạt động của la bàn phải khép lại (không có compass /
	// tuyến dài chưa gỡ thì đều trả về cho kiến trúc sư phân xử)
	compass, err := st.Outline.LoadCompass()
	if err != nil {
		return false, fmt.Errorf("load compass: %w", err)
	}
	if compass == nil || len(compass.OpenThreads) > 0 {
		return false, nil
	}
	return true, nil
}
