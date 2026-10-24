package guard

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("khởi tạo store: %v", err)
	}
	return s
}

// setStoreLang đặt ngôn ngữ tác phẩm của store rồi tự khôi phục.
// store.SetLanguage đồng bộ cả biến ngôn ngữ toàn cục của i18n, nên phải
// khôi phục cả hai; không dùng t.Parallel() trong các test đổi ngôn ngữ.
func setStoreLang(t *testing.T, s *store.Store, lang i18n.Lang) {
	t.Helper()
	previousStore := s.Language()
	previousActive := i18n.Active()
	s.SetLanguage(string(lang))
	t.Cleanup(func() {
		s.SetLanguage(previousStore)
		i18n.SetActive(string(previousActive))
	})
}

// TestSubAgentGuard_HardStopReasonEscalatesImmediately kiểm tra: khi model trả
// về safety / content_filter — dạng provider từ chối không phục hồi được —
// StopGuard của subagent phải Escalate ngay thay vì chèn thông điệp thúc.
//
// Bối cảnh: đo thực tế hy3-preview:free khi viết chương 2 liên tục 8 lần
// stop_reason='safety'; logic cũ lặp lại việc chèn "phải commit", model vẫn
// safety, phải đủ 3 lần block mới escalate, sau đó Engine lại chạy lại writer
// tổng cộng 3 lần. Mỗi lần đều là SubAgent mới → tiền tố cache lạnh hoàn toàn.
// Sau khi sửa, safety đầu tiên escalate ngay, Engine có thể dừng theo lỗi
// không phục hồi được.
//
// Chỉ test safety / content_filter: StopReasonError / StopReasonAborted đi nhánh
// dừng run trực tiếp ở agentcore loop.go, không bao giờ gọi StopGuard, nên đưa
// vào chỉ tạo code chết.
func TestSubAgentGuard_HardStopReasonEscalatesImmediately(t *testing.T) {
	cases := []agentcore.StopReason{
		agentcore.StopReason("safety"),
		agentcore.StopReason("content_filter"),
	}
	for _, sr := range cases {
		t.Run(string(sr), func(t *testing.T) {
			s := newTestStore(t)
			guard := NewWriterStopGuard(s, nil)
			info := agentcore.StopInfo{
				TurnIndex: 1,
				Message:   agentcore.Message{StopReason: sr},
			}
			d := guard(context.Background(), info)
			if !d.Escalate {
				t.Fatalf("stop_reason=%q phải escalate ngay, got %#v", sr, d)
			}
			if d.InjectMessage != "" {
				t.Fatalf("stop_reason=%q không được chèn thông điệp, got %q", sr, d.InjectMessage)
			}
		})
	}
}

// TestSubAgentGuard_NormalStopStillBlocks bảo đảm hành vi chặn với stop_reason
// bình thường không bị ảnh hưởng bởi nhánh lỗi cứng — LLM tự dừng mà chưa
// commit thì vẫn phải được thúc.
func TestSubAgentGuard_NormalStopStillBlocks(t *testing.T) {
	s := newTestStore(t)
	guard := NewWriterStopGuard(s, nil)
	info := agentcore.StopInfo{
		TurnIndex: 1,
		Message:   agentcore.Message{StopReason: agentcore.StopReasonStop},
	}
	d := guard(context.Background(), info)
	if d.Escalate {
		t.Fatal("dừng bình thường không được escalate ở lần chặn đầu")
	}
	if d.Allow {
		t.Fatal("dừng bình thường phải bị chặn khi chưa có checkpoint commit")
	}
	if d.InjectMessage == "" {
		t.Fatal("dừng bình thường phải chèn thông điệp thúc tiếp")
	}
}

// TestSubAgentGuard_ProgressBetweenBlocksResetsCounter kiểm tra: khi giữa hai
// lần chặn xuất hiện checkpoint mới (model bị thúc rồi draft lại…) thì
// consecutive được đặt lại — nâng cấp chỉ phạt quay tròn không ra sản phẩm,
// theo ngữ nghĩa "có tiến triển thì đặt lại" (issue #75).
func TestSubAgentGuard_ProgressBetweenBlocksResetsCounter(t *testing.T) {
	s := newTestStore(t)
	guard := NewWriterStopGuard(s, nil)
	normalStop := agentcore.StopInfo{TurnIndex: 1, Message: agentcore.Message{StopReason: agentcore.StopReasonStop}}

	// Chặn → ghi xuống đĩa bản nháp mới (có tiến triển) → chặn lại: lặp vượt
	// ngưỡng cũng không được nâng cấp.
	for i := 0; i < subagentMaxConsecutiveBlocks+2; i++ {
		if d := guard(context.Background(), normalStop); d.Escalate {
			t.Fatalf("escalate ở lần chặn %d dù có tiến triển giữa các lần", i)
		}
		if _, err := s.Checkpoints.Append(domain.ChapterScope(1), "draft", "drafts/01.draft.md", fmt.Sprintf("d%d", i)); err != nil {
			t.Fatalf("ghi checkpoint draft: %v", err)
		}
	}
	// Ngừng tiến triển: phải đủ số lần chặn liên tiếp rồi mới nâng cấp.
	for i := 0; i < subagentMaxConsecutiveBlocks; i++ {
		if d := guard(context.Background(), normalStop); d.Escalate {
			t.Fatalf("escalate quá sớm ở lần chặn rảnh %d", i)
		}
	}
	if d := guard(context.Background(), normalStop); !d.Escalate {
		t.Fatal("phải escalate sau khi chặn liên tiếp không có tiến triển")
	}
}

// TestWriterStopGuard_StageAwareBlockMessage kiểm tra thông điệp thúc được dựng
// theo các step đã ghi xuống đĩa: câu tĩnh "phải gọi commit_chapter" gây hiểu
// lầm khi bước trước còn thiếu hoặc chính commit đang báo lỗi (issue #75).
func TestWriterStopGuard_StageAwareBlockMessage(t *testing.T) {
	s := newTestStore(t)
	guard := NewWriterStopGuard(s, nil)
	normalStop := agentcore.StopInfo{TurnIndex: 1, Message: agentcore.Message{StopReason: agentcore.StopReasonStop}}

	// Chưa có sản phẩm nào: phải dẫn qua đủ trình tự giao thức.
	d := guard(context.Background(), normalStop)
	if !strings.Contains(d.InjectMessage, "draft_chapter") || !strings.Contains(d.InjectMessage, "plan_chapter") {
		t.Fatalf("thông điệp chưa có bản nháp phải nhắc đủ trình tự, got %q", d.InjectMessage)
	}

	// Đã có bản nháp: phải thúc check_consistency để chốt.
	if _, err := s.Checkpoints.Append(domain.ChapterScope(1), "draft", "drafts/01.draft.md", "d1"); err != nil {
		t.Fatalf("ghi checkpoint draft: %v", err)
	}
	d = guard(context.Background(), normalStop)
	if !strings.Contains(d.InjectMessage, "check_consistency") {
		t.Fatalf("thông điệp chỉ có bản nháp phải nhắc check_consistency, got %q", d.InjectMessage)
	}

	// Đã có bản nháp + kiểm tra nhất quán: chỉ còn thiếu bước gửi, và phải để
	// lối xử lý khi commit báo lỗi.
	if _, err := s.Checkpoints.Append(domain.ChapterScope(1), "consistency_check", "meta/checks/01.json", "c1"); err != nil {
		t.Fatalf("ghi checkpoint consistency_check: %v", err)
	}
	d = guard(context.Background(), normalStop)
	if !strings.Contains(d.InjectMessage, "commit_chapter") {
		t.Fatalf("thông điệp sẵn sàng gửi phải nhắc commit, got %q", d.InjectMessage)
	}
	// Phần "xử lý lỗi" phải có ở cả hai ngôn ngữ.
	if !strings.Contains(d.InjectMessage, "lỗi") && !strings.Contains(d.InjectMessage, "错误") {
		t.Fatalf("thông điệp sẵn sàng gửi phải nhắc xử lý lỗi, got %q", d.InjectMessage)
	}
}

// TestWriterStopGuard_BlockMessageFollowsWorkLanguage kiểm tra thông điệp thúc
// của writer đổi theo ngôn ngữ tác phẩm.
func TestWriterStopGuard_BlockMessageFollowsWorkLanguage(t *testing.T) {
	normalStop := agentcore.StopInfo{TurnIndex: 1, Message: agentcore.Message{StopReason: agentcore.StopReasonStop}}

	for _, tc := range []struct {
		lang   i18n.Lang
		expect string
	}{
		{i18n.LangVI, "Cấm kết thúc"},
		{i18n.LangZH, "禁止结束"},
	} {
		t.Run(string(tc.lang), func(t *testing.T) {
			s := newTestStore(t)
			setStoreLang(t, s, tc.lang)
			guard := NewWriterStopGuard(s, nil)
			d := guard(context.Background(), normalStop)
			if !strings.Contains(d.InjectMessage, tc.expect) {
				t.Fatalf("thông điệp thúc phải theo ngôn ngữ %s: got %q", tc.lang, d.InjectMessage)
			}
		})
	}
}

// TestSubAgentGuard_BlockHookReceivesAgentAndReason kiểm tra callback kiểm toán
// nhận đúng tên agent và chuỗi reason — Host dựa vào đó để đưa sự kiện chặn ra
// TUI.
func TestSubAgentGuard_BlockHookReceivesAgentAndReason(t *testing.T) {
	s := newTestStore(t)
	var agents, reasons []string
	guard := NewWriterStopGuard(s, func(agent, reason string, _ int32) {
		agents = append(agents, agent)
		reasons = append(reasons, reason)
	})
	normalStop := agentcore.StopInfo{TurnIndex: 1, Message: agentcore.Message{StopReason: agentcore.StopReasonStop}}

	for i := 0; i < subagentMaxConsecutiveBlocks+1; i++ {
		guard(context.Background(), normalStop)
	}
	if len(reasons) != subagentMaxConsecutiveBlocks+1 {
		t.Fatalf("hook được gọi %d lần, want %d", len(reasons), subagentMaxConsecutiveBlocks+1)
	}
	for i, agent := range agents {
		if agent != "writer" {
			t.Fatalf("lần gọi hook %d: agent = %q, want writer", i, agent)
		}
	}
	for i := 0; i < subagentMaxConsecutiveBlocks; i++ {
		if reasons[i] != "blocked" {
			t.Fatalf("reason[%d] = %q, want blocked", i, reasons[i])
		}
	}
	if last := reasons[len(reasons)-1]; last != "escalated" {
		t.Fatalf("reason cuối = %q, want escalated", last)
	}

	// hard_stop cũng phải được báo.
	var hardReasons []string
	hardGuard := NewWriterStopGuard(s, func(_, reason string, _ int32) {
		hardReasons = append(hardReasons, reason)
	})
	hardGuard(context.Background(), agentcore.StopInfo{
		TurnIndex: 1,
		Message:   agentcore.Message{StopReason: agentcore.StopReason("safety")},
	})
	if len(hardReasons) != 1 || hardReasons[0] != "hard_stop" {
		t.Fatalf("reason của hard stop = %v, want [hard_stop]", hardReasons)
	}
}

// TestEditorStopGuard_TaskAware kiểm tra nhận biết nhiệm vụ: khi được phân công
// sinh tóm tắt cung, chỉ save_review (soát duyệt) không tính là hoàn tất, phải
// có arc_summary mới được cho qua — chặn điểm khởi đầu của vòng lặp vô hạn cung
// khung trong tập (Defect C).
func TestEditorStopGuard_TaskAware(t *testing.T) {
	normalStop := agentcore.StopInfo{TurnIndex: 1, Message: agentcore.Message{StopReason: agentcore.StopReasonStop}}

	// Nhiệm vụ sinh tóm tắt + chỉ có review → phải chặn.
	t.Run("nhiệm vụ tóm tắt bị chặn khi chỉ có review", func(t *testing.T) {
		s := newTestStore(t)
		guard := NewEditorStopGuard(s, "生成第 5 卷第 1 弧摘要（save_arc_summary）", nil)
		if _, err := s.Checkpoints.Append(domain.ArcScope(5, 1), "review", "reviews/v05a01.json", "d1"); err != nil {
			t.Fatalf("ghi checkpoint review: %v", err)
		}
		if d := guard(context.Background(), normalStop); d.Allow {
			t.Fatal("nhiệm vụ sinh tóm tắt không được thoả bằng checkpoint review")
		}
	})

	// Nhiệm vụ sinh tóm tắt + đã có arc_summary → cho qua.
	t.Run("nhiệm vụ tóm tắt được cho qua khi có arc_summary", func(t *testing.T) {
		s := newTestStore(t)
		guard := NewEditorStopGuard(s, "生成第 5 卷第 1 弧摘要（save_arc_summary）", nil)
		if _, err := s.Checkpoints.Append(domain.ArcScope(5, 1), "arc_summary", "summaries/arc-v05a01.json", "d1"); err != nil {
			t.Fatalf("ghi checkpoint arc_summary: %v", err)
		}
		if d := guard(context.Background(), normalStop); !d.Allow {
			t.Fatal("nhiệm vụ sinh tóm tắt phải được thoả bằng checkpoint arc_summary")
		}
	})

	// Nhiệm vụ biên tập + có review → cho qua (giữ hành vi lỏng mặc định).
	t.Run("nhiệm vụ biên tập được cho qua khi có review", func(t *testing.T) {
		s := newTestStore(t)
		guard := NewEditorStopGuard(s, "对第 5 卷第 1 弧做弧级评审（scope=arc）", nil)
		if _, err := s.Checkpoints.Append(domain.ArcScope(5, 1), "review", "reviews/v05a01.json", "d1"); err != nil {
			t.Fatalf("ghi checkpoint review: %v", err)
		}
		if d := guard(context.Background(), normalStop); !d.Allow {
			t.Fatal("nhiệm vụ biên tập phải được thoả bằng checkpoint review")
		}
	})
}

// TestEditorStopGuard_VietnameseTaskNames kiểm tra nhiệm vụ tiếng Việt vẫn được
// nhận biết: sau bản port, dispatcher giao nhiệm vụ bằng tiếng Việt nên guard
// phải khớp "tóm tắt tập"/"tóm tắt cung", không chỉ hai từ khoá tiếng Trung.
func TestEditorStopGuard_VietnameseTaskNames(t *testing.T) {
	normalStop := agentcore.StopInfo{TurnIndex: 1, Message: agentcore.Message{StopReason: agentcore.StopReasonStop}}

	t.Run("tóm tắt cung", func(t *testing.T) {
		s := newTestStore(t)
		guard := NewEditorStopGuard(s, "Sinh tóm tắt cung 1 của tập 1 (save_arc_summary)", nil)
		if _, err := s.Checkpoints.Append(domain.ArcScope(1, 1), "review", "reviews/v01a01.json", "d1"); err != nil {
			t.Fatalf("ghi checkpoint review: %v", err)
		}
		if d := guard(context.Background(), normalStop); d.Allow {
			t.Fatal("nhiệm vụ sinh tóm tắt cung không được thoả bằng checkpoint review")
		}
		if _, err := s.Checkpoints.Append(domain.ArcScope(1, 1), "arc_summary", "summaries/arc-v01a01.json", "d2"); err != nil {
			t.Fatalf("ghi checkpoint arc_summary: %v", err)
		}
		if d := guard(context.Background(), normalStop); !d.Allow {
			t.Fatal("nhiệm vụ sinh tóm tắt cung phải được thoả bằng checkpoint arc_summary")
		}
	})

	t.Run("tóm tắt tập", func(t *testing.T) {
		s := newTestStore(t)
		guard := NewEditorStopGuard(s, "Sinh tóm tắt tập 1 (save_volume_summary)", nil)
		if _, err := s.Checkpoints.Append(domain.ArcScope(1, 1), "review", "reviews/v01a01.json", "d1"); err != nil {
			t.Fatalf("ghi checkpoint review: %v", err)
		}
		if d := guard(context.Background(), normalStop); d.Allow {
			t.Fatal("nhiệm vụ sinh tóm tắt tập không được thoả bằng checkpoint review")
		}
		if _, err := s.Checkpoints.Append(domain.ArcScope(1, 1), "volume_summary", "summaries/vol-v01.json", "d2"); err != nil {
			t.Fatalf("ghi checkpoint volume_summary: %v", err)
		}
		if d := guard(context.Background(), normalStop); !d.Allow {
			t.Fatal("nhiệm vụ sinh tóm tắt tập phải được thoả bằng checkpoint volume_summary")
		}
	})
}

// TestEditorStopGuard_BlockMessageFollowsWorkLanguage kiểm tra thông điệp thúc
// của editor đổi theo ngôn ngữ tác phẩm.
func TestEditorStopGuard_BlockMessageFollowsWorkLanguage(t *testing.T) {
	normalStop := agentcore.StopInfo{TurnIndex: 1, Message: agentcore.Message{StopReason: agentcore.StopReasonStop}}

	for _, tc := range []struct {
		lang   i18n.Lang
		expect string
	}{
		{i18n.LangVI, "Nhiệm vụ lượt này là sinh tóm tắt cung"},
		{i18n.LangZH, "本次任务是生成弧摘要"},
	} {
		t.Run(string(tc.lang), func(t *testing.T) {
			s := newTestStore(t)
			setStoreLang(t, s, tc.lang)
			guard := NewEditorStopGuard(s, "Sinh tóm tắt cung 1 của tập 1 (save_arc_summary)", nil)
			d := guard(context.Background(), normalStop)
			if !strings.Contains(d.InjectMessage, tc.expect) {
				t.Fatalf("thông điệp thúc phải theo ngôn ngữ %s: got %q", tc.lang, d.InjectMessage)
			}
		})
	}
}
