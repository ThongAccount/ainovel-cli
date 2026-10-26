package host

import (
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// TestHostReopen bảo vệ lối thoát mở lại ở cấp người dùng của /reopen: hoàn thành
// tác phẩm là một quyết định lớn, mở lại chỉ được người dùng chủ động yêu cầu —
// chưa hoàn thành thì từ chối, đang chạy thì từ chối; mở lại thành công sẽ đưa
// phase về writing, hướng viết tiếp đi kèm được đăng ký thành can thiệp chờ xử
// lý (PendingSteer), lúc khôi phục sẽ được Tài phán phán quyết rồi chèn vào
// trước khi viết tiếp.
func TestHostReopen(t *testing.T) {
	st := storepkg.NewStore(t.TempDir())
	h := &Host{store: st, events: make(chan Event, 8)}

	if err := st.Progress.Init(2); err != nil {
		t.Fatal(err)
	}
	if err := h.Reopen(""); err == nil {
		t.Fatal("sách chưa hoàn thành phải bị từ chối mở lại")
	}

	_ = st.Progress.UpdatePhase(domain.PhaseWriting)
	if err := st.Progress.MarkComplete(); err != nil {
		t.Fatal(err)
	}
	if err := h.Reopen("Mở tập mới với giới hạn tám mươi năm"); err != nil {
		t.Fatalf("mở lại sách đã hoàn thành phải thành công: %v", err)
	}
	p, _ := st.Progress.Load()
	if p.Phase != domain.PhaseWriting {
		t.Fatalf("sau khi mở lại phase phải là writing, got %s", p.Phase)
	}
	if len(p.PendingRewrites) != 0 || p.ReopenedFromComplete {
		t.Fatalf("mở lại để viết tiếp không được mang ngữ nghĩa làm lại: %+v", p)
	}
	// Bộ đếm mở lại phải được ghi xuống đĩa: digest progress của lần hoàn thành
	// lại mới phải khác lần trước — checkpoint có cùng digest được khử trùng lặp
	// theo tính lũy đẳng, lần hoàn thành lại với byte giống hệt sẽ không sinh
	// checkpoint mới, và StopGuard sẽ nhầm tác phẩm hoàn thành thành dừng vì
	// quay vòng không tiến triển.
	if p.ReopenCount != 1 {
		t.Fatalf("bộ đếm mở lại phải là 1, got %d", p.ReopenCount)
	}
	meta, _ := st.RunMeta.Load()
	if meta == nil || !strings.Contains(meta.PendingSteer, "tám mươi năm") {
		t.Fatalf("hướng viết tiếp phải được đăng ký thành can thiệp chờ xử lý, got %+v", meta)
	}

	running := &Host{store: st, lifecycle: lifecycleRunning, events: make(chan Event, 1)}
	if err := running.Reopen(""); err == nil {
		t.Fatal("khi engine đang chạy phải từ chối mở lại")
	}
}
