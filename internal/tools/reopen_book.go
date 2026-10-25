package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// ReopenBookTool mở lại một cuốn sách đã hoàn thành để vào trạng thái làm lại,
// được Engine gọi tại ranh giới hành động can thiệp.
// Sau khi hoàn thành, completePhaseGate chặn cứng mọi lần phân phối subagent,
// nên người dùng không thể yêu cầu làm lại chương đã viết.
// Công cụ này không phải subagent nên vẫn gọi được trong giai đoạn complete: nó
// chuyển nguyên tử phase về writing, đưa chương mục tiêu vào PendingRewrites,
// đặt flow=rewriting, sau đó Flow Router tiếp tục phân phối Người viết viết lại
// từng chương theo hàng chờ sẵn có, và khi hàng chờ chạy hết thì commit_chapter
// tự động hoàn thành lại. Gate / Router / edit / commit không cần đổi logic.
type ReopenBookTool struct {
	store *store.Store
}

func NewReopenBookTool(s *store.Store) *ReopenBookTool {
	return &ReopenBookTool{store: s}
}

// texts lấy bảng văn bản theo ngôn ngữ tác phẩm, đọc lười mỗi lần gọi.
func (t *ReopenBookTool) texts() *writeTexts { return writeTextsFor(t.store.Language()) }

func (t *ReopenBookTool) Name() string  { return "reopen_book" }
func (t *ReopenBookTool) Label() string { return t.texts().reopenBookLabel }

func (t *ReopenBookTool) Description() string {
	return t.texts().reopenBookDesc
}

// Công cụ ghi, cấm chạy song song.
func (t *ReopenBookTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *ReopenBookTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

// ActivityDescription phục vụ UI/nhật ký hiển thị mô tả hoạt động hiện tại của
// công cụ.
func (t *ReopenBookTool) ActivityDescription(_ json.RawMessage) string {
	return "Mở lại toàn bộ sách để làm lại"
}

func (t *ReopenBookTool) Schema() map[string]any {
	texts := t.texts()
	return schema.Object(
		schema.Property("chapters", schema.Array(texts.reopenBookArgChapters, schema.Int(""))).Required(),
		schema.Property("reason", schema.String(texts.reopenBookArgReason)),
	)
}

func (t *ReopenBookTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapters []int  `json:"chapters"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if len(a.Chapters) == 0 {
		return nil, fmt.Errorf("chapters không được để trống, cần chỉ rõ các chương cần làm lại: %w", errs.ErrToolArgs)
	}

	progress, err := t.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	if progress == nil {
		return nil, fmt.Errorf("progress chưa khởi tạo: %w", errs.ErrToolPrecondition)
	}
	// Chỉ làm lại được chương đã viết; số chương không nằm trong tập đã hoàn
	// thành thuộc loại viết tiếp/vượt phạm vi, nên từ chối rõ ràng để dẫn người
	// dùng sang đường điều chỉnh độ dài.
	var invalid []int
	for _, ch := range a.Chapters {
		if !slices.Contains(progress.CompletedChapters, ch) {
			invalid = append(invalid, ch)
		}
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("các chương %v chưa viết xong, reopen chỉ làm lại được chương đã hoàn thành (thêm mở rộng cốt truyện thì hãy điều chỉnh độ dài): %w", invalid, errs.ErrToolPrecondition)
	}

	// Kiểm tra phase trước được store.Reopen chặn dự phòng (chỉ gọi được khi
	// phase=complete).
	if err := t.store.Progress.Reopen(a.Chapters, a.Reason); err != nil {
		return nil, fmt.Errorf("reopen: %w: %w", errs.ErrStoreWrite, err)
	}

	// checkpoint: đối xứng với complete_book (GlobalScope + meta/progress.json).
	if _, err := t.store.Checkpoints.AppendArtifact(domain.GlobalScope(), "reopen", "meta/progress.json"); err != nil {
		return nil, fmt.Errorf("checkpoint reopen: %w: %w", errs.ErrStoreWrite, err)
	}

	return json.Marshal(map[string]any{
		"reopened":         true,
		"phase":            string(domain.PhaseWriting),
		"pending_rewrites": a.Chapters,
		"next_step":        t.texts().reopenBookNextStep,
	})
}
