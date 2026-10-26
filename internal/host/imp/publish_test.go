package imp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// spyCommitter ghi nhận số lần gọi Execute, phục vụ kiểm thử đường phát hành bất biến/khôi phục.
type spyCommitter struct{ calls int }

func (s *spyCommitter) Execute(context.Context, json.RawMessage) (json.RawMessage, error) {
	s.calls++
	return json.RawMessage(`{}`), nil
}

func TestCheckFoundationConflictsNormalizesBookMetadata(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Book.Save(domain.BookMetadata{Title: "测试书", Synopsis: "测试简介"}); err != nil {
		t.Fatal(err)
	}
	f := &Foundation{Book: domain.BookMetadata{Title: " 测试书 ", Synopsis: " 测试简介 "}}
	if err := checkFoundationConflicts(st, f); err != nil {
		t.Fatalf("thông tin tác phẩm giống nhau sau khi chuẩn hoá không nên xung đột: %v", err)
	}
}

// TestPublishChapterHandlesStalePendingCommit bảo vệ khôi phục cửa sổ sập khi phát hành: sập xảy ra giữa
// MarkChapterComplete và ClearPendingCommit sẽ để lại pending_commit trỏ tới chương này.
// Chương đã hoàn tất nếu bị bỏ qua thẳng sẽ né mất nhánh dọn dẹp của công cụ commit, Execute của chương kế tiếp bị ErrToolConflict
// từ chối, mỗi lần nhập lại chết ở đúng chỗ đó — khi trúng cặn sót vẫn phải đi qua một lần đường bất biến của công cụ.
func TestPublishChapterHandlesStalePendingCommit(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.Init(1); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.StartChapter(1); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.MarkChapterComplete(1, 100, "mystery", "quest"); err != nil {
		t.Fatal(err)
	}
	f := ImportedChapterFacts{Chapter: 1, Summary: "s", CoreEvent: "c", HookType: "mystery", DominantStrand: "quest"}

	// Không có cặn sót: chương đã hoàn tất bị bỏ qua với chi phí bằng 0, không kích hoạt commit.
	spy := &spyCommitter{}
	if err := publishChapter(context.Background(), st, spy, 1, "正文", f); err != nil {
		t.Fatalf("chương đã hoàn tất phải được bỏ qua theo tính bất biến: %v", err)
	}
	if spy.calls != 0 {
		t.Fatalf("không có cặn sót thì không nên gọi commit, được %d lần", spy.calls)
	}

	// Cặn sót trỏ tới chương này: phải đi qua đúng một lần đường bất biến của commit để hoàn tất dọn dẹp.
	if err := st.Signals.SavePendingCommit(domain.PendingCommit{Chapter: 1}); err != nil {
		t.Fatal(err)
	}
	if err := publishChapter(context.Background(), st, spy, 1, "正文", f); err != nil {
		t.Fatalf("đường dọn dẹp cặn sót không nên thất bại: %v", err)
	}
	if spy.calls != 1 {
		t.Fatalf("trúng cặn sót phải gọi commit đúng một lần, được %d lần", spy.calls)
	}
}
