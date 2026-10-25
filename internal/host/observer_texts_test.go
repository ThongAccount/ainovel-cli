package host

import (
	"encoding/json"
	"testing"

	"github.com/voocel/ainovel-cli/internal/i18n"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

func TestObserverTextsForLanguage(t *testing.T) {
	if got := observerTextsFor("vi"); got != observerTextsVI {
		t.Fatalf("observerTextsFor(vi) = %p, want %p", got, observerTextsVI)
	}
	if got := observerTextsFor("zh"); got != observerTextsZH {
		t.Fatalf("observerTextsFor(zh) = %p, want %p", got, observerTextsZH)
	}
	if got := observerTextsFor("chinese"); got != observerTextsZH {
		t.Fatalf("observerTextsFor(chinese) = %p, want %p", got, observerTextsZH)
	}
	if got := observerTextsFor(""); got != observerTextsVI {
		t.Fatalf("observerTextsFor(\"\") = %p, want %p", got, observerTextsVI)
	}
}

func TestObserverToolHeadersPerLanguage(t *testing.T) {
	cases := []struct {
		tool string
		vi   string
		zh   string
	}{
		{"plan_chapter", "✻ Quy hoạch", "✻ 规划"},
		{"edit_chapter", "✻ Hoàn thiện", "✻ 打磨"},
		{"commit_chapter", "✻ Chốt chương", "✻ 章节提交"},
		{"save_review", "✻ Thẩm duyệt", "✻ 审阅"},
		{"save_arc_summary", "✻ Tóm tắt cung", "✻ 弧摘要"},
		{"save_volume_summary", "✻ Tóm tắt tập", "✻ 卷摘要"},
		{"save_foundation", "✻ Thiết lập", "✻ 设定"},
		{"revise_outline", "✻ Sửa dàn ý", "✻ 修订大纲"},
		{"read_chapter", "✻ Đọc chương", "✻ 读章节"},
		{"check_consistency", "✻ Kiểm tra tính nhất quán", "✻ 一致性检查"},
		{"novel_context", "✻ Truy vấn ngữ cảnh", "✻ 查询上下文"},
	}
	for _, tc := range cases {
		if got := observerTextsVI.toolHeader(tc.tool); got != tc.vi {
			t.Errorf("toolHeader(%q) vi = %q, want %q", tc.tool, got, tc.vi)
		}
		if got := observerTextsZH.toolHeader(tc.tool); got != tc.zh {
			t.Errorf("toolHeader(%q) zh = %q, want %q", tc.tool, got, tc.zh)
		}
	}
	if got := observerTextsVI.toolHeader("draft_chapter"); got != "" {
		t.Errorf("toolHeader(draft_chapter) = %q, want empty (chế độ luồng trần)", got)
	}
}

func TestObserverExtractorHeaderFollowsLanguage(t *testing.T) {
	for _, tc := range []struct {
		lang   string
		header string
	}{
		{"vi", "✻ Quy hoạch"},
		{"zh", "✻ 规划"},
	} {
		e := newToolExtractor("plan_chapter", observerTextsFor(tc.lang))
		if e == nil {
			t.Fatalf("lang %q: no extractor for plan_chapter", tc.lang)
		}
		out := e.Feed(`{"chapter":1}`)
		mustContain(t, out, tc.header)
	}
}

func TestDisplayToolNamePerLanguage(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args string
		vi   string
		zh   string
	}{
		{
			name: "chapter",
			tool: "commit_chapter",
			args: `{"chapter":7}`,
			vi:   "commit_chapter(Chương 7)",
			zh:   "commit_chapter(第7章)",
		},
		{
			name: "review chapter",
			tool: "save_review",
			args: `{"chapter":7,"scope":"chapter","verdict":"polish"}`,
			vi:   "save_review(Chương 7·polish)",
			zh:   "save_review(第7章·polish)",
		},
		{
			name: "review arc",
			tool: "save_review",
			args: `{"scope":"arc"}`,
			vi:   "save_review(Cung này)",
			zh:   "save_review(本弧)",
		},
		{
			name: "review global",
			tool: "save_review",
			args: `{"scope":"global"}`,
			vi:   "save_review(Toàn cục)",
			zh:   "save_review(全局)",
		},
		{
			name: "read draft",
			tool: "read_chapter",
			args: `{"chapter":7,"source":"draft"}`,
			vi:   "read_chapter(Chương 7·Bản nháp)",
			zh:   "read_chapter(第7章·草稿)",
		},
		{
			name: "read character",
			tool: "read_chapter",
			args: `{"chapter":7,"character":"Lâm"}`,
			vi:   "read_chapter(Chương 7·Đối thoại với Lâm)",
			zh:   "read_chapter(第7章·Lâm对话)",
		},
		{
			name: "novel context",
			tool: "novel_context",
			args: `{"chapter":7}`,
			vi:   "novel_context(Chương 7)",
			zh:   "novel_context(第7章)",
		},
	}
	for _, tc := range cases {
		args := json.RawMessage(tc.args)
		if got := displayToolName(tc.tool, args, observerTextsVI); got != tc.vi {
			t.Errorf("%s: vi = %q, want %q", tc.name, got, tc.vi)
		}
		if got := displayToolName(tc.tool, args, observerTextsZH); got != tc.zh {
			t.Errorf("%s: zh = %q, want %q", tc.name, got, tc.zh)
		}
	}
}

// TestObserverTextsFollowStoreLanguage kiểm tra ngôn ngữ được đọc lười từ
// store.Language(): đổi ngôn ngữ giữa phiên thì bảng văn bản phải đổi theo.
//
// store.SetLanguage cũng đặt ngôn ngữ ở cấp tiến trình (i18n.Active), nên test này
// khôi phục lại giá trị cũ và không chạy song song với test khác.
func TestObserverTextsFollowStoreLanguage(t *testing.T) {
	prev := i18n.Active()
	t.Cleanup(func() { i18n.SetActive(string(prev)) })

	s := storepkg.NewStore(t.TempDir())
	o := newObserver(s, func(Event) {}, func(string) {}, func() {})

	s.SetLanguage("vi")
	if got := o.texts(); got != observerTextsVI {
		t.Fatalf("texts() sau SetLanguage(vi) = %p, want %p", got, observerTextsVI)
	}
	s.SetLanguage("zh")
	if got := o.texts(); got != observerTextsZH {
		t.Fatalf("texts() sau SetLanguage(zh) = %p, want %p", got, observerTextsZH)
	}
}
