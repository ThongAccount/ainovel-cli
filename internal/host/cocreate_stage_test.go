package host

import (
	"context"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/host/imp"
	"github.com/voocel/ainovel-cli/internal/store"
)

// newFlagTestHost dựng một Host tối thiểu, đủ để dẫn máy trạng thái cờ
// cocreating và cơ chế bảo vệ chống chạy song song.
// emitEvent dùng kênh không chặn, chỉ cần đệm events, không cần observer.
// Nhánh trạng thái đang chạy của PauseForCoCreate sẽ gọi Engine Abort (tái dùng
// đường tạm dừng Esc đã kiểm chứng), không nằm trong unit test này; ở đây chỉ
// phủ trạng thái không chạy cùng logic cờ/bảo vệ.
func newFlagTestHost(lc lifecycle, cocreating bool) *Host {
	return &Host{
		lifecycle:  lc,
		cocreating: cocreating,
		engine:     &engine{}, // acquireExclusive hỏi engine.isRunning() (cổng chặn cửa sổ dừng)
		events:     make(chan Event, 16),
	}
}

func TestPauseForCoCreate_NonRunningSetsFlag(t *testing.T) {
	h := newFlagTestHost(lifecycleIdle, false)
	if !h.PauseForCoCreate() {
		t.Fatal("trạng thái idle phải cho phép vào đồng sáng tác theo giai đoạn")
	}
	if !h.cocreating {
		t.Error("sau khi vào, cocreating phải là true")
	}
	if h.lifecycle != lifecycleIdle {
		t.Errorf("vào ở trạng thái không chạy không được đổi lifecycle, got %s", h.lifecycle)
	}
}

func TestPauseForCoCreate_RejectsCompleted(t *testing.T) {
	h := newFlagTestHost(lifecycleCompleted, false)
	if h.PauseForCoCreate() {
		t.Error("sau khi hoàn thành toàn bộ sách không nên cho phép vào đồng sáng tác theo giai đoạn")
	}
	if h.cocreating {
		t.Error("sau khi từ chối không được bật cờ cocreating")
	}
}

func TestPauseForCoCreate_RejectsReentrant(t *testing.T) {
	h := newFlagTestHost(lifecyclePaused, true)
	if h.PauseForCoCreate() {
		t.Error("đang trong đồng sáng tác thì phải từ chối vào lại")
	}
}

func TestCancelCoCreate_ClearsFlag(t *testing.T) {
	h := newFlagTestHost(lifecyclePaused, true)
	h.CancelCoCreate()
	if h.cocreating {
		t.Error("sau khi huỷ, cocreating phải được xoá")
	}
	if h.lifecycle != lifecyclePaused {
		t.Errorf("huỷ không được đổi lifecycle, got %s", h.lifecycle)
	}
}

func TestCancelCoCreate_NoopWhenNotCocreating(t *testing.T) {
	h := newFlagTestHost(lifecycleRunning, false)
	h.CancelCoCreate() // không được panic, không được đổi trạng thái
	if h.cocreating || h.lifecycle != lifecycleRunning {
		t.Error("CancelCoCreate ở trạng thái không đồng sáng tác phải là no-op")
	}
}

func TestResumeFromCoCreate_RejectsEmptyDraft(t *testing.T) {
	h := newFlagTestHost(lifecyclePaused, true)
	if err := h.ResumeFromCoCreate("   "); err == nil {
		t.Fatal("draft rỗng phải báo lỗi")
	}
	if !h.cocreating {
		t.Error("draft rỗng trả về trước khi xoá cờ, cocreating phải giữ nguyên true")
	}
}

func TestResumeFromCoCreate_RejectsWhenNotCocreating(t *testing.T) {
	h := newFlagTestHost(lifecyclePaused, false)
	err := h.ResumeFromCoCreate("## Hướng đi tiếp theo\n- Bước vào tập hai")
	if err == nil || !strings.Contains(err.Error(), "not in co-create") {
		t.Fatalf("ở trạng thái không đồng sáng tác phải báo not in co-create, got %v", err)
	}
}

func TestAcquireExclusive(t *testing.T) {
	cases := []struct {
		name       string
		lc         lifecycle
		cocreating bool
		exclusive  string
		wantErr    string // rỗng = kỳ vọng được cho qua
	}{
		{"running", lifecycleRunning, false, "", "đang chạy hoặc đang dừng"},
		{"cocreating", lifecyclePaused, true, "", "đồng sáng tác theo giai đoạn"},
		{"busy", lifecycleIdle, false, "nhập", "Tác vụ nhập đang chạy"},
		{"idle free", lifecycleIdle, false, "", ""},
		{"paused free", lifecyclePaused, false, "", ""},
	}
	// Cửa sổ dừng sau Abort: lifecycle đã đặt paused nhưng goroutine của engine
	// chưa thoát hẳn, vẫn phải từ chối — nếu không việc nhập sẽ ghi đồng thời
	// vào cùng store với phần chốt của engine.
	drain := newFlagTestHost(lifecyclePaused, false)
	drain.engine.running = true
	if err := drain.acquireExclusive("nhập"); err == nil {
		t.Fatal("trong thời gian engine thoát nước phải từ chối tác vụ độc quyền")
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newFlagTestHost(c.lc, c.cocreating)
			h.exclusive = c.exclusive
			err := h.acquireExclusive("nhập")
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("phải được cho qua, got %v", err)
				}
				if h.exclusive != "nhập" {
					t.Fatalf("sau khi cho qua phải ghi nhận việc chiếm dụng, got %q", h.exclusive)
				}
				h.releaseExclusive()
				if h.exclusive != "" {
					t.Fatalf("sau khi giải phóng thì việc chiếm dụng phải được xóa, got %q", h.exclusive)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("phải chứa %q, got %v", c.wantErr, err)
			}
			if !strings.Contains(err.Error(), "nhập") {
				t.Errorf("thông báo lỗi phải kèm action %q, got %v", "nhập", err)
			}
		})
	}
}

// TestExclusiveBlocksCreationEntries bảo vệ #2: khi tác vụ nền độc quyền
// (nhập/viết mô phỏng) đang chạy, không chỉ tác vụ nền thứ hai bị chặn mà cả lối
// vào sáng tác (Continue/Resume) và tác vụ nền mới cũng phải bị chặn — nếu không
// Continue sẽ để Tài phán đổi trạng thái trước khi engine bị cổng chặn đúng, và
// trong lúc Resume/next engine có thể chạy vượt trước.
func TestExclusiveBlocksCreationEntries(t *testing.T) {
	h := newFlagTestHost(lifecycleIdle, false)
	h.exclusive = "nhập"
	if _, err := h.ImportFrom(context.Background(), imp.Options{}); err == nil {
		t.Error("trong lúc tác vụ độc quyền chạy, ImportFrom phải bị từ chối")
	}
	if err := h.Continue("Viết tiếp"); err == nil {
		t.Error("trong lúc tác vụ độc quyền chạy, Continue phải bị từ chối (phải chặn trước khi Tài phán phán quyết)")
	}
	if _, err := h.Resume(); err == nil {
		t.Error("trong lúc tác vụ độc quyền chạy, Resume phải bị từ chối")
	}
}

// TestStageCoCreate_OccupancyBlocksConcurrentEntries kiểm tra mọi lối vào độc
// quyền bị chặn trong cửa sổ đồng sáng tác: import/start/resume/continue đều phải
// bị từ chối khi đang cocreating, bịt lỗ hổng thời kỳ paused chỉ kiểm tra
// ==running.
func TestStageCoCreate_OccupancyBlocksConcurrentEntries(t *testing.T) {
	h := newFlagTestHost(lifecycleIdle, false)
	if !h.PauseForCoCreate() {
		t.Fatal("vào đồng sáng tác theo giai đoạn thất bại")
	}

	if _, err := h.ImportFrom(context.Background(), imp.Options{}); err == nil {
		t.Error("trong cửa sổ đồng sáng tác, ImportFrom phải bị từ chối")
	}
	if err := h.StartPrepared("Viết một câu chuyện mới"); err == nil {
		t.Error("trong cửa sổ đồng sáng tác, StartPrepared phải bị từ chối")
	}
	if _, err := h.Resume(); err == nil {
		t.Error("trong cửa sổ đồng sáng tác, Resume phải bị từ chối")
	}
	if err := h.Continue("Viết tiếp"); err == nil {
		t.Error("trong cửa sổ đồng sáng tác, Continue phải bị từ chối")
	}

	// Sau khi thoát đồng sáng tác, trạng thái chiếm được giải phóng (ở đây đi qua
	// Cancel; đường can thiệp Resume thuộc phạm vi kiểm thử tích hợp)
	h.CancelCoCreate()
	if h.cocreating {
		t.Fatal("sau khi thoát, cờ chiếm phải được giải phóng")
	}
}

func TestBuildStoryStateSummary_NilStore(t *testing.T) {
	if got := buildStoryStateSummary(nil, hostTextsVI); got != "" {
		t.Errorf("nil store phải trả về chuỗi rỗng, thực tế %q", got)
	}
}

func TestBuildStoryStateSummary_Populated(t *testing.T) {
	dir := t.TempDir()
	st := store.NewStore(dir)
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.Init(100); err != nil {
		t.Fatal(err)
	}
	if err := st.Book.Save(domain.BookMetadata{Title: "Bóng thơ", Synopsis: "Thiếu niên truy tìm bóng đã lạc mất."}); err != nil {
		t.Fatal(err)
	}
	p, _ := st.Progress.Load()
	p.CompletedChapters = []int{1, 2, 3}
	p.TotalWordCount = 12000
	if err := st.Progress.Save(p); err != nil {
		t.Fatal(err)
	}
	if err := st.Outline.SaveCompass(domain.StoryCompass{
		EndingDirection: "Người chính bước lên đỉnh cao",
		OpenThreads:     []string{"Thù hận thầy môn chưa báo"},
		EstimatedScale:  "Dự kiến 4-6 tập",
	}); err != nil {
		t.Fatal(err)
	}

	got := buildStoryStateSummary(st, hostTextsVI)
	for _, want := range []string{"Bóng thơ", "đã hoàn thành 3 chương", "chương kế tiếp là chương 4", "Người chính bước lên đỉnh cao", "Thù hận thầy môn chưa báo", "Dự kiến 4-6 tập"} {
		if !strings.Contains(got, want) {
			t.Errorf("tóm tắt phải chứa %q, thực tế:\n%s", want, got)
		}
	}
}

func TestBuildStoryStateSummaryUsesDynamicPlanningWording(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.Init(66); err != nil {
		t.Fatal(err)
	}
	if err := st.Outline.SaveLayeredOutline([]domain.VolumeOutline{{
		Index: 1, Title: "Tập một", Arcs: []domain.ArcOutline{
			{Index: 1, Chapters: []domain.OutlineEntry{{Title: "Một"}, {Title: "Hai"}}},
			{Index: 2, EstimatedChapters: 64},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	p, err := st.Progress.Load()
	if err != nil {
		t.Fatal(err)
	}
	p.Layered = true
	if err := st.Progress.Save(p); err != nil {
		t.Fatal(err)
	}

	got := buildStoryStateSummary(st, hostTextsVI)
	if !strings.Contains(got, "hiện đã chi tiết hóa 2 chương (phần sau lập kế hoạch động theo cung)") {
		t.Fatalf("cách diễn đạt tóm tắt theo quy hoạch động sai:\n%s", got)
	}
	if strings.Contains(got, "66") || strings.Contains(got, "dự kiến 2 chương") {
		t.Fatalf("tóm tắt theo quy hoạch động không được ám chỉ tổng số chương cố định:\n%s", got)
	}
}
