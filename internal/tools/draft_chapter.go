package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// DraftChapterTool ghi toàn bộ bản nháp của một chương, thay thế luồng cũ
// write_scene + polish_chapter. Agent tự quyết định viết một lạt hay viết tiếp
// theo từng đợt.
type DraftChapterTool struct {
	store *store.Store
}

func NewDraftChapterTool(store *store.Store) *DraftChapterTool {
	return &DraftChapterTool{store: store}
}

// texts lấy bảng văn bản theo ngôn ngữ tác phẩm, đọc lười mỗi lần gọi để
// không phụ thuộc thứ tự dựng tool và store.SetLanguage.
func (t *DraftChapterTool) texts() *writeTexts { return writeTextsFor(t.store.Language()) }

func (t *DraftChapterTool) Name() string { return "draft_chapter" }
func (t *DraftChapterTool) Description() string {
	return t.texts().draftChapterDesc
}
func (t *DraftChapterTool) Label() string { return t.texts().draftChapterLabel }

// Công cụ ghi, cấm chạy song song (tranh chấp đọc–sửa–ghi).
func (t *DraftChapterTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *DraftChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *DraftChapterTool) Schema() map[string]any {
	// Đánh dấu mode là required để tương thích với strict tool calling của
	// OpenAI — chế độ strict yêu cầu mọi property nằm trong danh sách required.
	// Hành vi cũ "bỏ qua mode thì mặc định write" giờ cần model truyền tường minh
	// mode="write"; nhánh default trong Execute không đổi.
	texts := t.texts()
	return schema.Object(
		schema.Property("chapter", schema.Int(texts.draftChapterArgChapter)).Required(),
		schema.Property("content", schema.String(texts.draftChapterArgContent)).Required(),
		schema.Property("mode", schema.Enum(texts.draftChapterArgMode, "write", "append")).Required(),
	)
}

// StrictSchema yêu cầu Provider bảo đảm tham số công cụ tuân theo schema.
func (t *DraftChapterTool) StrictSchema() bool { return true }

func (t *DraftChapterTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter int    `json:"chapter"`
		Content string `json:"content"`
		Mode    string `json:"mode"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if a.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}
	if a.Content == "" {
		return nil, fmt.Errorf("content must not be empty: %w", errs.ErrToolArgs)
	}
	if err := t.store.Progress.ValidateChapterWork(a.Chapter); err != nil {
		return nil, err
	}
	if err := EnsureChapterExpanded(t.store, a.Chapter); err != nil {
		return nil, err
	}
	completed, err := t.store.Progress.IsChapterCompleted(a.Chapter)
	if err != nil {
		return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	if completed {
		// Đường hoàn thiện/viết lại: chương đã hoàn thành nhưng vẫn nằm trong
		// pending_rewrites nên được phép ghi đè bản nháp.
		progress, err := t.store.Progress.Load()
		if err != nil {
			return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
		}
		inRewriteQueue := progress != nil && slices.Contains(progress.PendingRewrites, a.Chapter)
		if !inRewriteQueue {
			return json.Marshal(map[string]any{
				"chapter":   a.Chapter,
				"skipped":   true,
				"completed": true,
				"reason":    fmt.Sprintf(t.texts().draftChapterCompleted, a.Chapter),
			})
		}
	}
	if err := t.store.Progress.StartChapter(a.Chapter); err != nil {
		return nil, fmt.Errorf("mark chapter in progress: %w", err)
	}

	switch a.Mode {
	case "append":
		if err := t.store.Drafts.AppendDraft(a.Chapter, a.Content); err != nil {
			return nil, fmt.Errorf("append draft: %w", err)
		}
		full, err := t.store.Drafts.LoadDraft(a.Chapter)
		if err != nil {
			return nil, fmt.Errorf("load draft after append: %w", err)
		}
		if _, err := t.store.Checkpoints.AppendArtifact(
			domain.ChapterScope(a.Chapter), "draft",
			fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
		); err != nil {
			return nil, fmt.Errorf("checkpoint draft: %w", err)
		}
		return json.Marshal(map[string]any{
			"written":    true,
			"chapter":    a.Chapter,
			"mode":       "append",
			"word_count": utf8.RuneCountInString(full),
			"next_step":  t.texts().draftChapterNextStep,
		})
	default: // write
		if err := t.store.Drafts.SaveDraft(a.Chapter, a.Content); err != nil {
			return nil, fmt.Errorf("save draft: %w", err)
		}
		if _, err := t.store.Checkpoints.AppendArtifact(
			domain.ChapterScope(a.Chapter), "draft",
			fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
		); err != nil {
			return nil, fmt.Errorf("checkpoint draft: %w", err)
		}
		return json.Marshal(map[string]any{
			"written":    true,
			"chapter":    a.Chapter,
			"mode":       "write",
			"word_count": utf8.RuneCountInString(a.Content),
			"next_step":  t.texts().draftChapterNextStep,
		})
	}
}
