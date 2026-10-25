package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/voocel/agentcore/schema"
	agentcoretools "github.com/voocel/agentcore/tools"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// EditChapterTool thay thế chính xác các đoạn chuỗi trong bản nháp của chương,
// dùng cho tình huống hoàn thiện. So với việc viết lại cả chương bằng
// draft_chapter, cách này tiết kiệm token hơn 10 lần.
//
// Hợp đồng ghi xuống đĩa: chỉ sửa drafts/{ch:02d}.draft.md, cấm sửa trực tiếp
// chapters/ (bản chính do commit_chapter nắm độc quyền).
// Ngữ nghĩa seed: drafts chưa có nhưng chapters đã có → tự động sao chép
// chapters sang drafts làm điểm xuất phát.
// Kiểm tra sở hữu: chỉ cho phép sửa chương đã hoàn thành và đang nằm trong hàng
// chờ PendingRewrites.
//
// Công cụ này là bọc mỏng của agentcore.EditTool; toàn bộ logic tìm–thay (khớp
// nhiều tầng có dung sai, xuất diff, giữ cuối dòng/BOM) tái sử dụng từ thư viện
// gốc.
type EditChapterTool struct {
	store *store.Store
	edit  *agentcoretools.EditTool
}

func NewEditChapterTool(s *store.Store) *EditChapterTool {
	return &EditChapterTool{
		store: s,
		edit:  agentcoretools.NewEdit(s.Dir(), nil),
	}
}

// texts lấy bảng văn bản theo ngôn ngữ tác phẩm, đọc lười mỗi lần gọi.
func (t *EditChapterTool) texts() *writeTexts { return writeTextsFor(t.store.Language()) }

func (t *EditChapterTool) Name() string  { return "edit_chapter" }
func (t *EditChapterTool) Label() string { return t.texts().editChapterLabel }

// ReadOnly khai báo rõ đây là công cụ ghi (đi kèm ConcurrencySafeTool để không
// bị lập lịch song song).
func (t *EditChapterTool) ReadOnly(_ json.RawMessage) bool { return false }

// ConcurrencySafe cấm song song rõ ràng: nhiều lệnh edit_chapter trên cùng một
// chương sẽ tranh chấp đọc–sửa–ghi, và dù các chương khác nhau chạy song song
// thì thứ tự checkpoint vẫn bị xen kẽ. Chạy tuần tự là an toàn nhất.
func (t *EditChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

// ActivityDescription phục vụ UI/nhật ký hiển thị mô tả hoạt động hiện tại của
// công cụ.
func (t *EditChapterTool) ActivityDescription(_ json.RawMessage) string {
	return "Chỉnh sửa bản nháp chương"
}

func (t *EditChapterTool) Description() string {
	return t.texts().editChapterDesc
}

func (t *EditChapterTool) Schema() map[string]any {
	texts := t.texts()
	return schema.Object(
		schema.Property("chapter", schema.Int(texts.editChapterArgChapter)).Required(),
		schema.Property("old_string", schema.String(texts.editChapterArgOldString)).Required(),
		schema.Property("new_string", schema.String(texts.editChapterArgNewString)).Required(),
		schema.Property("replace_all", schema.Bool(texts.editChapterArgReplaceAll)),
	)
}

func (t *EditChapterTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter    int    `json:"chapter"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if a.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}
	if a.OldString == "" {
		return nil, fmt.Errorf("old_string không được để trống: %w", errs.ErrToolArgs)
	}
	if a.OldString == a.NewString {
		return nil, fmt.Errorf("old_string giống new_string, không cần sửa: %w", errs.ErrToolArgs)
	}
	if err := t.store.Progress.ValidateChapterWork(a.Chapter); err != nil {
		return nil, err
	}

	// Kiểm tra sở hữu: hiện thực cơ học cho giao thức của Người viết. Bản nháp
	// đầu của chương mới chỉ được ghi đè toàn chương; không thể trông chờ vào
	// việc model tự tuân thủ prompt rồi vẫn để lộ đường chỉnh sửa chính xác
	// mong manh thành một đường thực thi hợp lệ.
	completed, err := t.store.Progress.IsChapterCompleted(a.Chapter)
	if err != nil {
		return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	if !completed {
		return nil, fmt.Errorf("chương %d chưa hoàn thành, bản nháp đầu không được dùng edit_chapter; nếu có lỗi nặng hãy gọi draft_chapter(mode=\"write\", chapter=%d) để ghi đè toàn bộ chương: %w", a.Chapter, a.Chapter, errs.ErrToolPrecondition)
	}
	progress, err := t.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	if progress == nil || !slices.Contains(progress.PendingRewrites, a.Chapter) {
		return nil, fmt.Errorf("chương %d đã hoàn thành và không nằm trong hàng chờ PendingRewrites nên không thể chỉnh sửa; cần sửa thì để biên tập viên thẩm duyệt kích hoạt viết lại/hoàn thiện trước: %w", a.Chapter, errs.ErrToolPrecondition)
	}
	if err := EnsureChapterExpanded(t.store, a.Chapter); err != nil {
		return nil, err
	}

	// Seed: khi chưa có drafts thì sao chép một bản từ chapters làm điểm xuất phát
	if err := t.ensureDraft(a.Chapter); err != nil {
		return nil, err
	}

	// Ủy thác agentcore.EditTool hoàn tất việc tìm–thay
	subArgs, _ := json.Marshal(map[string]any{
		"path":        fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
		"file_path":   fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
		"old_text":    a.OldString,
		"old_string":  a.OldString,
		"new_text":    a.NewString,
		"new_string":  a.NewString,
		"replace_all": a.ReplaceAll,
	})
	result, err := t.edit.Execute(ctx, subArgs)
	if err != nil {
		return nil, fmt.Errorf("apply edit: %w: %w", errs.ErrToolPrecondition, err)
	}

	if _, err := t.store.Checkpoints.AppendArtifact(
		domain.ChapterScope(a.Chapter), "edit",
		fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
	); err != nil {
		return nil, fmt.Errorf("checkpoint edit: %w: %w", errs.ErrStoreWrite, err)
	}

	// Chỉ dẫn bổ sung: cho Người viết biết các bước tiếp theo để không bỏ sót
	// check_consistency / commit_chapter.
	var passthrough map[string]any
	if err := json.Unmarshal(result, &passthrough); err != nil {
		return result, nil
	}
	passthrough["chapter"] = a.Chapter
	passthrough["next_step"] = t.texts().editChapterNextStep
	return json.Marshal(passthrough)
}

// ensureDraft bảo đảm drafts/{ch}.draft.md tồn tại:
//   - đã có bản nháp → trả về luôn
//   - không có bản nháp nhưng có bản chính → sao chép bản chính sang drafts làm
//     điểm xuất phát để sửa (phổ biến trong tình huống hoàn thiện)
//   - không có gì cả → báo lỗi, nhắc dùng draft_chapter để tạo bản nháp đầu
func (t *EditChapterTool) ensureDraft(chapter int) error {
	draft, err := t.store.Drafts.LoadDraft(chapter)
	if err != nil {
		return fmt.Errorf("load draft: %w: %w", errs.ErrStoreRead, err)
	}
	if draft != "" {
		return nil
	}
	text, err := t.store.Drafts.LoadChapterText(chapter)
	if err != nil {
		return fmt.Errorf("load chapter: %w: %w", errs.ErrStoreRead, err)
	}
	if text == "" {
		return fmt.Errorf("chương %d không có bản nháp cũng không có bản chính, vui lòng gọi trước draft_chapter(mode=write, chapter=%d) để tạo bản nháp đầu: %w", chapter, chapter, errs.ErrToolPrecondition)
	}
	if err := t.store.Drafts.SaveDraft(chapter, text); err != nil {
		return fmt.Errorf("seed draft from chapter: %w: %w", errs.ErrStoreWrite, err)
	}
	return nil
}
