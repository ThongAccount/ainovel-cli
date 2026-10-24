package agents

import (
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/agents/ctxpack"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// TestWorkerDescriptionsFollowWorkLanguage kiểm tra mô tả subagent đổi theo
// ngôn ngữ tác phẩm lấy từ assets.Bundle.Language.
//
// Mô tả này được ghép thành mô tả công cụ mà model đọc khi spawn agent, nên
// phải đúng ngôn ngữ của cuốn sách đang viết.
func TestWorkerDescriptionsFollowWorkLanguage(t *testing.T) {
	for _, tc := range []struct {
		lang           string
		architectShort string
		architectLong  string
		writer         string
		editor         string
	}{
		{
			lang:           "vi",
			architectShort: "Kiến trúc sư truyện ngắn",
			architectLong:  "Kiến trúc sư truyện dài",
			writer:         "Người viết",
			editor:         "Biên tập viên",
		},
		{
			lang:           "zh",
			architectShort: "短篇规划师",
			architectLong:  "长篇规划师",
			writer:         "创作者",
			editor:         "审阅者",
		},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			bundle := assets.Bundle{Language: tc.lang}
			texts := workerTextsFor(i18n.Normalize(bundle.Language))

			if !strings.Contains(texts.architectShortDesc, tc.architectShort) {
				t.Fatalf("mô tả kiến trúc sư truyện ngắn sai ngôn ngữ: %q", texts.architectShortDesc)
			}
			if !strings.Contains(texts.architectLongDesc, tc.architectLong) {
				t.Fatalf("mô tả kiến trúc sư truyện dài sai ngôn ngữ: %q", texts.architectLongDesc)
			}
			if !strings.Contains(texts.writerDesc, tc.writer) {
				t.Fatalf("mô tả người viết sai ngôn ngữ: %q", texts.writerDesc)
			}
			if !strings.Contains(texts.editorDesc, tc.editor) {
				t.Fatalf("mô tả biên tập viên sai ngôn ngữ: %q", texts.editorDesc)
			}

			// Prompt tóm tắt của Writer phải đi cùng ngôn ngữ với bundle.
			promptTexts := ctxpack.PromptTextsFor(i18n.Normalize(bundle.Language))
			if tc.lang == "vi" {
				if !strings.Contains(promptTexts.SummarySystem, "trợ lý tóm tắt ngữ cảnh") {
					t.Fatalf("prompt tóm tắt sai ngôn ngữ: %q", promptTexts.SummarySystem)
				}
			} else if !strings.Contains(promptTexts.SummarySystem, "小说创作上下文摘要助手") {
				t.Fatalf("prompt tóm tắt sai ngôn ngữ: %q", promptTexts.SummarySystem)
			}
		})
	}
}
