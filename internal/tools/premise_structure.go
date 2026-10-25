package tools

import (
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// premiseHeadingAliases là bảng alias heading premise tiếng Trung. Giữ nguyên
// byte-for-byte ([P]): mô hình có thể viết heading tiếng Trung, và
// internal/host/imp đọc lại cấu trúc này, nên bản alias zh không được dịch.
var premiseHeadingAliases = map[string]string{
	"题材定位":    "题材定位",
	"题材和基调":   "题材和基调",
	"核心冲突":    "核心冲突",
	"主角目标":    "主角目标",
	"结局方向":    "终局方向",
	"终局方向":    "终局方向",
	"写作禁区":    "写作禁区",
	"差异化卖点":   "差异化卖点",
	"差异化钩子":   "差异化钩子",
	"核心兑现承诺":  "核心兑现承诺",
	"故事引擎":    "故事引擎",
	"关系/成长主线": "关系/成长主线",
	"升级路径":    "升级路径",
	"中段转折":    "中段转折",
	"中期转向":    "中段转折",
	"终局命题":    "终局命题",
	"短篇适配性":   "短篇适配性",
	"本作为什么适合短篇/单卷收束": "短篇适配性",
}

// premiseHeadingAliasesVI là bản tiếng Việt của premiseHeadingAliases.
// Dùng khi kiến trúc sư viết premise bằng tiếng Việt. Cùng cấu trúc heading,
// cùng quy tắc alias (viết tách biệt, không dịch bảng zh).
var premiseHeadingAliasesVI = map[string]string{
	"Định vị thể loại":                "Định vị thể loại",
	"Thể loại và giọng điệu":          "Thể loại và giọng điệu",
	"Xung đột cốt lõi":                "Xung đột cốt lõi",
	"Mục tiêu chính":                  "Mục tiêu chính",
	"Hướng kết cục":                   "Hướng kết cục",
	"Vùng cấm viết":                   "Vùng cấm viết",
	"Điểm bán hấp dẫn":                "Điểm bán hấp dẫn",
	"Móc câu chuyện":                  "Móc câu chuyện",
	"Lời hứa thực hiện":               "Lời hứa thực hiện",
	"Động cơ câu chuyện":              "Động cơ câu chuyện",
	"Trục quan hệ / trưởng thành":     "Trục quan hệ / trưởng thành",
	"Lộ trình nâng cấp":               "Lộ trình nâng cấp",
	"Bước ngoặt giữa chừng":           "Bước ngoặt giữa chừng",
	"Câu hỏi kết cục":                 "Câu hỏi kết cục",
	"Khả năng thích nghi truyện ngắn": "Khả năng thích nghi truyện ngắn",
}

// premiseAliasTable chọn bảng alias theo ngôn ngữ tác phẩm.
func premiseAliasTable(lang string) map[string]string {
	if i18n.Normalize(lang) == i18n.LangZH {
		return premiseHeadingAliases
	}
	return premiseHeadingAliasesVI
}

// parsePremiseSections chia premise thành các section theo heading.
// Không đổi chữ ký export: đọc ngôn ngữ từ i18n.Active() (package này không
// có store, xem COMMON.md §3).
func parsePremiseSections(premise string) map[string]string {
	return parsePremiseSectionsFor(premise, string(i18n.Active()))
}

func parsePremiseSectionsFor(premise string, lang string) map[string]string {
	lines := strings.Split(premise, "\n")
	sections := make(map[string]string)
	var current string
	var body []string

	flush := func() {
		if current == "" {
			return
		}
		text := strings.TrimSpace(strings.Join(body, "\n"))
		if text != "" {
			sections[current] = text
		}
		body = body[:0]
	}

	table := premiseAliasTable(lang)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if heading, ok := canonicalPremiseHeading(trimmed, table); ok {
			flush()
			current = heading
			continue
		}
		if current != "" {
			body = append(body, line)
		}
	}
	flush()
	return sections
}

// canonicalPremiseHeading tra cứu heading trong bảng alias đã chọn.
func canonicalPremiseHeading(trimmed string, table map[string]string) (string, bool) {
	if !strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
	if title == "" {
		return "", false
	}
	canonical, ok := table[title]
	return canonical, ok
}

func premiseStructure(premise string, tier domain.PlanningTier) map[string]any {
	sections := parsePremiseSections(premise)
	required := requiredPremiseHeadings(tier)
	found := make([]string, 0, len(required))
	var missing []string
	for _, heading := range required {
		if _, ok := sections[heading]; ok {
			found = append(found, heading)
			continue
		}
		missing = append(missing, heading)
	}

	structure := map[string]any{
		"template_ready": len(missing) == 0,
		"found":          found,
		"missing":        missing,
	}
	if len(sections) > 0 {
		structure["section_count"] = len(sections)
	}
	return structure
}

// requiredPremiseHeadings trả về danh sách heading bắt buộc cho mỗi tier,
// không đổi chữ ký export: đọc ngôn ngữ từ i18n.Active().
func requiredPremiseHeadings(tier domain.PlanningTier) []string {
	return requiredPremiseHeadingsFor(tier, string(i18n.Active()))
}

func requiredPremiseHeadingsFor(tier domain.PlanningTier, lang string) []string {
	if i18n.Normalize(lang) == i18n.LangZH {
		return requiredPremiseHeadingsZH(tier)
	}
	return requiredPremiseHeadingsVI(tier)
}

func requiredPremiseHeadingsZH(tier domain.PlanningTier) []string {
	common := []string{
		"题材和基调",
		"题材定位",
		"核心冲突",
		"主角目标",
		"终局方向",
		"写作禁区",
		"差异化卖点",
		"差异化钩子",
		"核心兑现承诺",
	}

	switch tier {
	case domain.PlanningTierLong:
		return append(common,
			"故事引擎",
			"关系/成长主线",
			"升级路径",
			"中段转折",
			"终局命题",
		)
	case domain.PlanningTierMid:
		return append(common,
			"故事引擎",
			"中段转折",
		)
	case domain.PlanningTierShort:
		return append(common,
			"短篇适配性",
		)
	default:
		return common
	}
}

func requiredPremiseHeadingsVI(tier domain.PlanningTier) []string {
	common := []string{
		"Thể loại và giọng điệu",
		"Định vị thể loại",
		"Xung đột cốt lõi",
		"Mục tiêu chính",
		"Hướng kết cục",
		"Vùng cấm viết",
		"Điểm bán hấp dẫn",
		"Móc câu chuyện",
		"Lời hứa thực hiện",
	}

	switch tier {
	case domain.PlanningTierLong:
		return append(common,
			"Động cơ câu chuyện",
			"Trục quan hệ / trưởng thành",
			"Lộ trình nâng cấp",
			"Bước ngoặt giữa chừng",
			"Câu hỏi kết cục",
		)
	case domain.PlanningTierMid:
		return append(common,
			"Động cơ câu chuyện",
			"Bước ngoặt giữa chừng",
		)
	case domain.PlanningTierShort:
		return append(common,
			"Khả năng thích nghi truyện ngắn",
		)
	default:
		return common
	}
}
