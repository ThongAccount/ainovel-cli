// Package userrules là lớp dịch vụ chuẩn hóa quy tắc người dùng: các quy tắc
// viết bằng ngôn ngữ tự nhiên từ mọi nguồn được chuẩn hóa qua một lời gọi LLM
// có cấu trúc thành các trường ứng viên, rồi rules.BuildSnapshot hợp nhất
// một cách tất định thành ảnh chụp quy tắc của sách.
//
// Phân tầng trách nhiệm:
//   - package rules: dữ liệu thuần + hợp nhất tất định (Snapshot / Candidate /
//     BuildSnapshot / SystemDefaults)
//   - package này: chuẩn hóa LLM + điều phối + ghi xuống đĩa (phụ thuộc
//     agentcore + store + rules)
//
// Chuẩn hóa là đường nâng cao, không phải điều kiện tiên quyết cho sáng tác:
// mọi nguồn thất bại đều hạ cấp thành raw preferences, sáng tác chính phải tiếp
// tục.
package userrules

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
	"github.com/voocel/ainovel-cli/internal/rules"
)

// normalizeMaxTokens là trần đầu ra cho một lần chuẩn hóa (token suy nghĩ và
// JSON đầu ra dùng chung ngân sách này).
// JSON chuẩn hóa rất nhỏ (thường <1k), phần lớn ngân sách dành cho suy nghĩ của
// các mô hình không tắt được thinking —
// để hẹp quá thì suy nghĩ chiếm mất chỗ của JSON, gây cắt ngắn và phân tích
// thất bại. max_tokens là trần chứ không phải lượng tính phí, tăng lên không
// tốn thêm chi phí.
const normalizeMaxTokens = 8192

// normalizeContractFor dựng hợp đồng chuẩn hóa theo ngôn ngữ tác phẩm. Hợp
// đồng đặt sát DTO biên giới: mọi trường required, fatigue_words dùng mảng
// đối tượng (chế độ strict cấm map có khoá động), cả hai chế độ dùng chung
// một quy ước DTO.
func normalizeContractFor(lang i18n.Lang) llmcontract.Contract {
	texts := ruleTextsFor(lang)
	return llmcontract.Contract{
		Name:        "userrules_normalize",
		Description: texts.contractDesc,
		Schema: schema.Object(
			schema.Property("structured", schema.Object(
				schema.Property("genre", schema.String(texts.genreLabel)).Required(),
				schema.Property("forbidden_chars", schema.Array(texts.forbiddenCharsLbl, schema.String(texts.charLabel))).Required(),
				schema.Property("forbidden_phrases", schema.Array(texts.forbiddenPhrasesLb, schema.String(texts.phraseLabel))).Required(),
				schema.Property("fatigue_words", schema.Array(texts.fatigueWordsLabel, schema.Object(
					schema.Property("word", schema.String(texts.fatigueWordLabel)).Required(),
					schema.Property("max_per_chapter", schema.Int(texts.maxPerChapterLabel)).Required(),
				))).Required(),
			)).Required(),
			schema.Property("preferences", schema.String(texts.preferencesLabel)).Required(),
			schema.Property("uncertain", schema.Array(texts.uncertainLabel, schema.String(texts.uncertainItemLabel))).Required(),
		),
		Lang: lang,
	}
}

// Normalizer chuẩn hóa các quy tắc bằng ngôn ngữ tự nhiên của một nguồn thành
// rules.Candidate.
type Normalizer struct {
	model agentcore.ChatModel
	// lang trả về ngôn ngữ tác phẩm hiệu lực. Service gắn nguồn ngôn ngữ từ
	// store (st.Language()) để tất định và kiểm thử được; bộ chuẩn hóa dựng
	// độc lập dùng i18n.Active().
	lang func() i18n.Lang
}

// NewNormalizer dựng bộ chuẩn hóa bằng một ChatModel. Chuẩn hóa là công cụ khởi
// chạy một lần, nên nên truyền mô hình mạnh (ví dụ mô hình mặc định của
// ModelSet), không cần theo mô hình yếu dùng để sáng tác.
//
// Bộ chuẩn hóa không ghi đè thinking: tham số suy luận "off" tường minh chỉ
// được một số mô hình hỗ trợ, mô hình chat thường sẽ từ chối. Giữ mặc định
// của provider/model, và normalizeMaxTokens dành sẵn ngân sách đầu ra cho
// các mô hình không tắt được thinking.
func NewNormalizer(model agentcore.ChatModel) *Normalizer {
	return newNormalizer(model, func() i18n.Lang { return i18n.Active() })
}

// newNormalizer dựng bộ chuẩn hóa với nguồn ngôn ngữ tường minh.
func newNormalizer(model agentcore.ChatModel, lang func() i18n.Lang) *Normalizer {
	if lang == nil {
		lang = func() i18n.Lang { return i18n.Active() }
	}
	return &Normalizer{model: model, lang: lang}
}

// Normalize chuẩn hóa một nguồn. Thất bại trả về error (kèm nguyên nhân thật)
// để nơi gọi tự quyết định hạ cấp
// (Service.normalizeOrDegrade ghi ứng viên degraded) — lỗi kỹ thuật không
// được ngụy trang thành kết quả bình thường, lỗi dừng (xác thực/quyền…)
// không thử lại.
func (n *Normalizer) Normalize(ctx context.Context, source, text string) (rules.Candidate, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return rules.Candidate{Source: source}, nil
	}
	if n == nil || n.model == nil {
		return rules.Candidate{}, fmt.Errorf("mô hình chuẩn hóa chưa được cấu hình")
	}

	lang := n.lang()
	texts := ruleTextsFor(lang)
	contract := normalizeContractFor(lang)
	out, err := llmcontract.Execute(ctx, n.model, llmcontract.Request[normalizerOutput]{
		Contract:     contract,
		SystemPrompt: texts.systemPrompt,
		Payload:      text,
		Options:      []agentcore.CallOption{agentcore.WithMaxTokens(normalizeMaxTokens)},
		Validate: func(out *normalizerOutput) error {
			_, err := out.toCandidate(source, texts)
			return err
		},
		Agent: "rules",
		Hooks: llmcontract.Hooks{
			Resolved: func(res llmcontract.Resolution) {
				slog.Debug("chọn giao thức chuẩn hóa quy tắc", "module", "rules", "source", source,
					"contract", contract.Name, "structured_mode", res.Mode,
					"capability_source", res.Source, "provider", res.Provider, "model", res.Model,
					"schema_fingerprint", contract.Fingerprint())
			},
			Correction: func(ev llmcontract.Correction) {
				slog.Warn("tự lành đầu ra chuẩn hóa quy tắc", "module", "rules", "source", source,
					"attempt", ev.Attempt, "layer", ev.Layer, "structured_mode", ev.Mode, "err", ev.Err)
			},
		},
	})
	if err != nil {
		return rules.Candidate{}, fmt.Errorf("chuẩn hóa thất bại: %w", err)
	}
	return out.toCandidate(source, texts)
}

// degraded dựng một ứng viên hạ cấp: khi chuẩn hóa thất bại thì lấy nguyên văn
// làm sở thích văn phong, không trích xuất quy tắc máy nào.
// uncertain đánh dấu nguồn (để hiển thị lại "những nguồn nào chưa phân tích
// được") nhưng không chứa chi tiết lỗi kỹ thuật — lỗi kỹ thuật chỉ vào nhật ký.
// uncertain là siêu dữ liệu vận hành và chẩn đoán, không chèn vào ngữ cảnh của
// model.
func degraded(source, text string) rules.Candidate {
	return rules.Candidate{
		Source:      source,
		Preferences: text,
		Uncertain:   []string{source + ": chuẩn hóa thất bại, đã coi nguyên văn là sở thích văn phong (không trích xuất quy tắc máy kiểm tra được)"},
		Degraded:    true,
	}
}

// normalizerOutput là DTO biên giới mà bộ chuẩn hóa thống nhất (cả hai chế độ
// dùng chung): uncertain là mảng chuỗi cố định, fatigue_words là mảng đối
// tượng cố định — hình dạng do hợp đồng đóng băng, không còn đoán nhiều hình.
type normalizerOutput struct {
	Structured  normalizerStructured `json:"structured"`
	Preferences string               `json:"preferences"`
	Uncertain   []string             `json:"uncertain"`
}

type normalizerStructured struct {
	Genre            string             `json:"genre"`
	ForbiddenChars   []string           `json:"forbidden_chars"`
	ForbiddenPhrases []string           `json:"forbidden_phrases"`
	FatigueWords     []fatigueWordEntry `json:"fatigue_words"`
}

type fatigueWordEntry struct {
	Word          string `json:"word"`
	MaxPerChapter int    `json:"max_per_chapter"`
}

// toCandidate kiểm tra DTO biên giới rồi chuyển thành ứng viên miền: mục
// fatigue phải có từ khác rỗng và trần là số nguyên dương
// (lỗi kiểm tra được đưa lại cho model để sửa), phía miền vẫn là
// map[string]int.
func (o normalizerOutput) toCandidate(source string, texts *ruleTexts) (rules.Candidate, error) {
	var fatigue map[string]int
	for _, e := range o.Structured.FatigueWords {
		word := strings.TrimSpace(e.Word)
		if word == "" {
			return rules.Candidate{}, fmt.Errorf("%s", texts.emptyFatigueWord)
		}
		if e.MaxPerChapter < 1 {
			return rules.Candidate{}, fmt.Errorf(texts.invalidMaxPerChapter, word, e.MaxPerChapter)
		}
		if fatigue == nil {
			fatigue = make(map[string]int, len(o.Structured.FatigueWords))
		}
		fatigue[word] = e.MaxPerChapter
	}
	return rules.Candidate{
		Source: source,
		Structured: rules.Structured{
			Genre:            strings.TrimSpace(o.Structured.Genre),
			ForbiddenChars:   nonEmpty(o.Structured.ForbiddenChars),
			ForbiddenPhrases: nonEmpty(o.Structured.ForbiddenPhrases),
			FatigueWords:     fatigue,
		},
		Preferences: strings.TrimSpace(o.Preferences),
		Uncertain:   nonEmpty(o.Uncertain),
	}, nil
}

func nonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}
