package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/store"
)

// ReadChapterTool đọc nguyên văn chương, để Agent đọc lại chính chữ mình vừa
// viết và chữ của các chương trước.
type ReadChapterTool struct {
	store *store.Store
}

func NewReadChapterTool(store *store.Store) *ReadChapterTool {
	return &ReadChapterTool{store: store}
}

// texts lấy bảng văn bản theo ngôn ngữ tác phẩm, đọc lười mỗi lần gọi.
func (t *ReadChapterTool) texts() *writeTexts { return writeTextsFor(t.store.Language()) }

func (t *ReadChapterTool) Name() string { return "read_chapter" }
func (t *ReadChapterTool) Description() string {
	return t.texts().readChapterDesc
}
func (t *ReadChapterTool) Label() string { return t.texts().readChapterLabel }

// Công cụ chỉ đọc, có thể được lập lịch song song (Biên tập viên thường đọc
// nhiều chương cùng lúc khi thẩm duyệt).
func (t *ReadChapterTool) ReadOnly(_ json.RawMessage) bool        { return true }
func (t *ReadChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return true }

func (t *ReadChapterTool) Schema() map[string]any {
	texts := t.texts()
	return schema.Object(
		schema.Property("chapter", schema.Int(texts.readChapterArgChapter)),
		schema.Property("from", schema.Int(texts.readChapterArgFrom)),
		schema.Property("to", schema.Int(texts.readChapterArgTo)),
		schema.Property("source", schema.Enum(texts.readChapterArgSource, "final", "draft")).Required(),
		schema.Property("character", schema.String(texts.readChapterArgCharacter)),
		schema.Property("max_runes", schema.Int(texts.readChapterArgMaxRunes)),
	)
}

func (t *ReadChapterTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter   int    `json:"chapter"`
		From      int    `json:"from"`
		To        int    `json:"to"`
		Source    string `json:"source"`
		Character string `json:"character"`
		MaxRunes  int    `json:"max_runes"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	if a.Source != "final" && a.Source != "draft" {
		return nil, fmt.Errorf("source must be final or draft")
	}

	// Chế độ 1: trích xuất thoại của nhân vật
	if a.Character != "" {
		var warnings []string
		warn := func(scope string, err error) {
			if err != nil {
				warnings = append(warnings, fmt.Sprintf(t.texts().loadFailedFmt, scope, err))
			}
		}
		chars, err := t.store.Characters.Load()
		warn("characters", err)
		var aliases []string
		for _, c := range chars {
			if c.Name == a.Character {
				aliases = c.Aliases
				break
			}
		}
		var maxCompleted int
		p, err := t.store.Progress.Load()
		warn("progress", err)
		if p != nil {
			maxCompleted = maxCompletedChapter(p.CompletedChapters)
		}
		samples, err := t.store.Drafts.ExtractDialogue(a.Character, aliases, 8, maxCompleted)
		warn("dialogue_samples", err)
		result := map[string]any{
			"character": a.Character,
			"samples":   samples,
		}
		if len(samples) == 0 {
			result["hint"] = t.texts().readChapterNoSamples
		}
		if len(warnings) > 0 {
			result["status"] = "partial"
			result["_warnings"] = warnings
		}
		return json.Marshal(result)
	}

	// Chế độ 2: đọc một dải chương
	if a.From > 0 && a.To > 0 {
		maxRunes := a.MaxRunes
		if maxRunes <= 0 {
			maxRunes = 2000
		}
		var load func(int) (string, error)
		if a.Source == "draft" {
			load = t.store.Drafts.LoadDraft
		} else {
			load = t.store.Drafts.LoadChapterText
		}
		texts := make(map[int]string)
		for ch := a.From; ch <= a.To; ch++ {
			chapter, err := load(ch)
			if err != nil {
				return nil, fmt.Errorf("load %s chapter %d: %w", a.Source, ch, err)
			}
			if chapter == "" {
				continue
			}
			runes := []rune(chapter)
			if len(runes) > maxRunes {
				chapter = string(runes[:maxRunes]) + "..."
			}
			texts[ch] = chapter
		}
		return json.Marshal(map[string]any{
			"chapters": texts,
			"from":     a.From,
			"to":       a.To,
			"source":   a.Source,
		})
	}

	// Chế độ 3: đọc một chương
	if a.Chapter <= 0 {
		return nil, fmt.Errorf("chapter is required")
	}

	var content string
	var err error
	switch a.Source {
	case "draft":
		content, err = t.store.Drafts.LoadDraft(a.Chapter)
	default: // final
		content, err = t.store.Drafts.LoadChapterText(a.Chapter)
	}
	if err != nil {
		return nil, fmt.Errorf("read chapter %d: %w", a.Chapter, err)
	}
	if content == "" {
		return json.Marshal(map[string]any{
			"chapter": a.Chapter,
			"source":  a.Source,
			"exists":  false,
			"hint":    t.texts().readChapterMissingHint,
		})
	}

	return json.Marshal(map[string]any{
		"chapter":    a.Chapter,
		"source":     a.Source,
		"content":    content,
		"word_count": len([]rune(content)),
	})
}

// maxCompletedChapter trả về số chương lớn nhất trong danh sách chương đã hoàn
// thành.
func maxCompletedChapter(completed []int) int {
	m := 0
	for _, ch := range completed {
		if ch > m {
			m = ch
		}
	}
	return m
}
