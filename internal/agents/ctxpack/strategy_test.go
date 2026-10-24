package ctxpack

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
	corecontext "github.com/voocel/agentcore/context"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// setStoreLang đặt ngôn ngữ tác phẩm của store rồi tự khôi phục.
// store.SetLanguage đồng bộ cả biến ngôn ngữ toàn cục của i18n, nên phải
// khôi phục cả hai; không dùng t.Parallel() trong các test đổi ngôn ngữ.
func setStoreLang(t *testing.T, s *storepkg.Store, lang i18n.Lang) {
	t.Helper()
	previousStore := s.Language()
	previousActive := i18n.Active()
	s.SetLanguage(string(lang))
	t.Cleanup(func() {
		s.SetLanguage(previousStore)
		i18n.SetActive(string(previousActive))
	})
}

func TestStoreSummaryCompactApplyUsesPersistentStoreData(t *testing.T) {
	s := seededWriterStore(t)

	for _, lang := range []i18n.Lang{i18n.LangVI, i18n.LangZH} {
		t.Run(string(lang), func(t *testing.T) {
			setStoreLang(t, s, lang)
			texts := PromptTextsFor(lang)

			strategy := NewStoreSummaryCompact(StoreSummaryCompactConfig{
				Store:              s,
				KeepRecentTokens:   80,
				SummaryTokenBudget: 2000,
			})

			msgs := []agentcore.AgentMessage{
				agentcore.UserMsg(strings.Repeat("ngữ cảnh cũ", 400)),
				agentcore.Message{
					Role:    agentcore.RoleAssistant,
					Content: []agentcore.ContentBlock{agentcore.TextBlock(strings.Repeat("trả lời cũ", 80))},
				},
				agentcore.UserMsg("Tiếp tục viết chương 3, chú ý nối tiếp phần cuối chương 2."),
				agentcore.Message{
					Role:    agentcore.RoleAssistant,
					Content: []agentcore.ContentBlock{agentcore.TextBlock("Nhận rồi, trước hết tôi rà lại bối cảnh hiện tại.")},
				},
			}

			out, result, err := strategy.Apply(context.Background(), msgs, msgs, corecontext.Budget{
				Tokens:    corecontext.EstimateTotal(msgs),
				Window:    128,
				Threshold: 32,
			})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if !result.Applied {
				t.Fatal("phải áp dụng chiến lược tóm tắt từ store")
			}
			if result.Name != storeSummaryStrategyName {
				t.Fatalf("tên chiến lược lạ: %q", result.Name)
			}
			if len(out) < 2 {
				t.Fatalf("phải có bản tóm tắt + các tin nhắn giữ lại, got %d", len(out))
			}
			summary, ok := out[0].(corecontext.ContextSummary)
			if !ok {
				t.Fatalf("phải là ContextSummary, got %T", out[0])
			}
			if !strings.Contains(summary.Summary, texts.recentSummaries) {
				t.Fatalf("bản tóm tắt phải có mục tóm tắt chương, got %q", summary.Summary)
			}
			if !strings.Contains(summary.Summary, texts.currentChapterPlan) {
				t.Fatalf("bản tóm tắt phải có mục kế hoạch chương, got %q", summary.Summary)
			}
			if !strings.Contains(summary.Summary, texts.activeForeshadow) {
				t.Fatalf("bản tóm tắt phải có mục manh mối, got %q", summary.Summary)
			}
			if !strings.Contains(summary.Summary, texts.pendingReviews) {
				t.Fatalf("bản tóm tắt phải có mục vấn đề biên tập chờ sửa, got %q", summary.Summary)
			}
			if !strings.Contains(summary.Summary, "manh mối kho cần tích áp thêm một nhịp") {
				t.Fatalf("bản tóm tắt phải có chi tiết vấn đề biên tập, got %q", summary.Summary)
			}
			if result.Info == nil || result.Info.CompactedCount <= 0 {
				t.Fatalf("phải có thông tin nén, got %+v", result.Info)
			}
		})
	}
}

func TestWriterRestoreIncludesOptionalDataWarnings(t *testing.T) {
	s := seededWriterStore(t)
	if err := os.WriteFile(filepath.Join(s.Dir(), "meta", "style_rules.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, lang := range []i18n.Lang{i18n.LangVI, i18n.LangZH} {
		t.Run(string(lang), func(t *testing.T) {
			setStoreLang(t, s, lang)
			texts := PromptTextsFor(lang)

			text, ok, err := buildWriterRestoreText(s, restoreBudgetTokens)
			if err != nil {
				t.Fatalf("dữ liệu phụ hỏng không được ngăn việc khôi phục ngữ cảnh: %v", err)
			}
			if !ok || !strings.Contains(text, texts.dataWarning) || !strings.Contains(text, "style_rules") {
				t.Fatalf("ngữ cảnh khôi phục phải phơi bày cảnh báo đọc dữ liệu: %q", text)
			}
		})
	}
}

func TestStoreSummaryCompactApplyFallsBackWhenStoreDataInsufficient(t *testing.T) {
	dir := t.TempDir()
	s := storepkg.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Save(&domain.Progress{
		Phase:             domain.PhaseWriting,
		CurrentChapter:    1,
		TotalChapters:     3,
		CompletedChapters: nil,
	}); err != nil {
		t.Fatalf("Save progress: %v", err)
	}

	strategy := NewStoreSummaryCompact(StoreSummaryCompactConfig{Store: s, KeepRecentTokens: 20})
	msgs := []agentcore.AgentMessage{
		agentcore.UserMsg(strings.Repeat("ngữ cảnh cũ", 40)),
		agentcore.Message{
			Role:    agentcore.RoleAssistant,
			Content: []agentcore.ContentBlock{agentcore.TextBlock(strings.Repeat("trả lời cũ", 40))},
		},
	}

	out, result, err := strategy.Apply(context.Background(), msgs, msgs, corecontext.Budget{
		Tokens:    corecontext.EstimateTotal(msgs),
		Window:    64,
		Threshold: 16,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Applied {
		t.Fatal("khi dữ liệu lưu trữ không đủ thì phải không làm gì")
	}
	if len(out) != len(msgs) {
		t.Fatalf("các tin nhắn phải giữ nguyên, got %d", len(out))
	}
}

func TestWriterRestorePackRefreshReusesStoreBuilder(t *testing.T) {
	s := seededWriterStore(t)
	pack := &WriterRestorePack{}
	pack.Refresh(s)

	msg, ok, err := pack.buildMessage(restoreBudgetTokens)
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	if !ok {
		t.Fatal("phải có tin nhắn khôi phục")
	}
	text := msg.TextContent()
	if !strings.Contains(text, "<post-compact-context>") {
		t.Fatalf("phải bọc ngữ cảnh khôi phục, got %q", text)
	}
	texts := PromptTextsFor(i18n.Normalize(s.Language()))
	if !strings.Contains(text, texts.pendingReviews) {
		t.Fatalf("phải có mục vấn đề biên tập chờ sửa, got %q", text)
	}
	if !strings.Contains(text, texts.currentChapterPlan) {
		t.Fatalf("phải có mục kế hoạch chương, got %q", text)
	}

	if _, _, err := pack.buildMessage(0); err == nil {
		t.Fatal("phải trả lỗi tường minh khi gói khôi phục không vừa ngân sách")
	}
}

// TestPromptTextsFollowWorkLanguage kiểm tra prompt tóm tắt của Writer đổi
// theo ngôn ngữ tác phẩm.
func TestPromptTextsFollowWorkLanguage(t *testing.T) {
	vi := PromptTextsFor(i18n.LangVI)
	zh := PromptTextsFor(i18n.LangZH)

	if !strings.Contains(vi.SummarySystem, "trợ lý tóm tắt ngữ cảnh") {
		t.Fatalf("prompt hệ thống tiếng Việt sai: %q", vi.SummarySystem)
	}
	if !strings.Contains(zh.SummarySystem, "小说创作上下文摘要助手") {
		t.Fatalf("prompt hệ thống tiếng Trung sai: %q", zh.SummarySystem)
	}
	if !strings.Contains(vi.Summary, "Tiến độ hiện tại") {
		t.Fatalf("prompt tóm tắt tiếng Việt phải dùng nhãn tiếng Việt: %q", vi.Summary)
	}
	if !strings.Contains(zh.Summary, "当前进度") {
		t.Fatalf("prompt tóm tắt tiếng Trung phải dùng nhãn tiếng Trung: %q", zh.Summary)
	}
}

func seededWriterStore(t *testing.T) *storepkg.Store {
	t.Helper()

	s := storepkg.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Save(&domain.Progress{
		Phase:             domain.PhaseWriting,
		CurrentChapter:    3,
		TotalChapters:     6,
		CompletedChapters: []int{1, 2},
		Flow:              domain.FlowWriting,
	}); err != nil {
		t.Fatalf("Save progress: %v", err)
	}
	if err := s.Outline.SaveOutline([]domain.OutlineEntry{
		{Chapter: 1, Title: "Chương 1", CoreEvent: "Mở đầu"},
		{Chapter: 2, Title: "Chương 2", CoreEvent: "Xung đột leo thang"},
		{Chapter: 3, Title: "Chương 3", CoreEvent: "Truy tìm manh mối", Scenes: []string{"Người dẫn chuyện truy tìm vụ mất tích", "Phát hiện manh mối ở kho cũ"}},
	}); err != nil {
		t.Fatalf("SaveOutline: %v", err)
	}
	if err := s.Drafts.SaveChapterPlan(domain.ChapterPlan{
		Chapter:    3,
		Title:      "Chương 3",
		Goal:       "Đẩy tiến điều tra vụ mất tích",
		Conflict:   "Người dẫn chuyện và đồng hành bất đồng về hướng điều tra",
		Hook:       "Trong kho phát hiện băng thu đáng ngờ",
		EmotionArc: "Từ nghi ngờ đến căng thẳng",
	}); err != nil {
		t.Fatalf("SaveChapterPlan: %v", err)
	}
	if err := s.Summaries.SaveSummary(domain.ChapterSummary{
		Chapter:    1,
		Summary:    "Người dẫn chuyện nhận việc, phát hiện vụ mất tích không đơn giản.",
		Characters: []string{"Lâm Lam", "Châu Trạc"},
		KeyEvents:  []string{"Việc được giao thành lập"},
	}); err != nil {
		t.Fatalf("SaveSummary 1: %v", err)
	}
	if err := s.Summaries.SaveSummary(domain.ChapterSummary{
		Chapter:    2,
		Summary:    "Hai người truy tìm bến cũ, manh mối chỉ về kho bỏ hoang.",
		Characters: []string{"Lâm Lam", "Châu Trạc", "Thúc bá"},
		KeyEvents:  []string{"Xung đột ở bến cũ", "Manh mối kho xuất hiện"},
	}); err != nil {
		t.Fatalf("SaveSummary 2: %v", err)
	}
	if err := s.World.SaveForeshadowLedger([]domain.ForeshadowEntry{
		{ID: "tape", Description: "Băng thu người mất tích để lại", PlantedAt: 2, Status: "planted"},
	}); err != nil {
		t.Fatalf("SaveForeshadowLedger: %v", err)
	}
	if err := s.World.SaveTimeline([]domain.TimelineEvent{
		{Chapter: 2, Time: "Ban đêm", Event: "Chạm trán ở bến cũ", Characters: []string{"Lâm Lam", "Châu Trạc"}},
	}); err != nil {
		t.Fatalf("SaveTimeline: %v", err)
	}
	if err := s.World.SaveStyleRules(domain.WritingStyleRules{
		Prose:  []string{"Câu ngắn, giữ chút nghẹt thở"},
		Taboos: []string{"Tránh giải thích bí ẩn quá thẳng"},
	}); err != nil {
		t.Fatalf("SaveStyleRules: %v", err)
	}
	if err := s.World.SaveReview(domain.ReviewEntry{
		Chapter: 2,
		Scope:   "chapter",
		Verdict: "polish",
		Summary: "Phần dựng trước kết chương 2 hơi vội, cần bồi thêm nhịp nghẹt trước kho.",
		Issues: []domain.ConsistencyIssue{
			{
				Type:        "pacing",
				Severity:    "warning",
				Description: "Manh mối kho xuất hiện quá sớm, chưa đủ tích áp suspense.",
				Suggestion:  "Thêm một đoạn do dự và miêu tả khí áp môi trường trước khi vào kho.",
			},
		},
		ContractMisses: []string{"Câu móc cuối chương chưa đủ mạnh"},
	}); err != nil {
		t.Fatalf("Save chapter review: %v", err)
	}
	if err := s.World.SaveReview(domain.ReviewEntry{
		Chapter: 2,
		Scope:   "global",
		Verdict: "polish",
		Summary: "Nhịp điệu phần cuối chương 2 hơi nhanh, manh mối kho cần tích áp thêm một nhịp.",
	}); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}
	return s
}
