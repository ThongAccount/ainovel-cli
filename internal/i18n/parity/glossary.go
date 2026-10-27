package parity

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// GlossaryTerm là một dòng của bảng thuật ngữ Việt hóa: các thuật ngữ
// tiếng Trung và các thuật ngữ tiếng Việt tương ứng. Mỗi bên có thể có
// nhiều cách viết, tách bằng dấu "/".
type GlossaryTerm struct {
	// ZH là các thuật ngữ tiếng Trung, ít nhất một.
	ZH []string
	// VI là các thuật ngữ tiếng Việt tương ứng, ít nhất một.
	VI []string
}

// glossaryParen khớp phần chú thích ngoặc trong ô tiếng Việt, ví dụ
// "Đề cương (dàn ý trong văn mô tả)".
var glossaryParen = regexp.MustCompile(`\([^)]*\)`)

// ParseGlossary đọc bảng thuật ngữ Markdown và trả về các dòng đọc được.
//
// Bảng thuật ngữ là nguồn chuẩn: docs/i18n-vi-glossary.md. Bỏ qua dòng tiêu đề,
// dòng kẻ, và các dòng mà cột tiếng Trung không chứa ký tự CJK — bảng vai trò
// Agent dùng tên tiếng Anh (Coordinator → Điều phối viên), không phải cặp
// Trung Quốc/tiếng Việt.
func ParseGlossary(path string) ([]GlossaryTerm, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var terms []GlossaryTerm
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := splitTableCells(line)
		if len(cells) < 2 {
			continue
		}
		zh := splitAlternatives(cells[0])
		vi := splitAlternatives(cells[1])
		if len(zh) == 0 || len(vi) == 0 {
			continue
		}
		hasCJKTerm := false
		for _, term := range zh {
			if hasCJK(term) {
				hasCJKTerm = true
				break
			}
		}
		if !hasCJKTerm {
			continue
		}
		terms = append(terms, GlossaryTerm{ZH: zh, VI: vi})
	}
	return terms, nil
}

// splitTableCells tách một dòng bảng Markdown thành các ô.
func splitTableCells(line string) []string {
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

// splitAlternatives tách một ô thành các cách viết.
//
// Ô có thể chứa ngoặc giải thích, ví dụ "Đề cương (dàn ý trong văn mô tả)":
// cả "Đề cương" lẫn "dàn ý" đều là cách viết hợp lệ, nên phần trong ngoặc
// được giữ lại như một cách viết thay vì bị bỏ.
func splitAlternatives(cell string) []string {
	var out []string
	for _, part := range strings.Split(cell, "/") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		base := strings.TrimSpace(glossaryParen.ReplaceAllString(part, ""))
		if base != "" {
			out = append(out, base)
		}
		if loc := glossaryParen.FindString(part); loc != "" {
			inner := strings.TrimSpace(glossaryParen.ReplaceAllString(loc, ""))
			if inner != "" {
				out = append(out, inner)
			}
		}
	}
	return out
}

// GlossaryUsage là thống kê dùng một thuật ngữ trong các chuỗi song ngữ.
type GlossaryUsage struct {
	// Term là thuật ngữ được đo.
	Term GlossaryTerm
	// Occurrences là số chuỗi song ngữ có chứa thuật ngữ tiếng Trung.
	Occurrences int
	// Violations là các chỗ nhánh VI không dùng thuật ngữ tiếng Việt tương ứng.
	Violations []Issue
}

// AnalyzeGlossary đo mức dùng và mức tuân thủ từng thuật ngữ trong toàn repo.
//
// Kết quả dùng để quyết định thuật ngữ nào có thể kiểm tra tự động: thuật ngữ
// mà toàn repo đang tuân thủ thì siết được thành cổng đỏ; thuật ngữ mà repo
// chưa tuân thủ thì cần người quyết định trước, không nên báo đỏ hàng loạt.
func AnalyzeGlossary(tables []bilingualTable, pairs []pairLiteral, terms []GlossaryTerm) []GlossaryUsage {
	usages := make([]GlossaryUsage, len(terms))
	for i, term := range terms {
		usages[i] = GlossaryUsage{Term: term}
	}

	for _, pair := range pairs {
		if !pair.viOK || !pair.zhOK {
			continue
		}
		for i := range usages {
			if !containsAny(pair.zhText, usages[i].Term.ZH) {
				continue
			}
			usages[i].Occurrences++
			if !containsAnyFold(pair.viText, usages[i].Term.VI) {
				usages[i].Violations = append(usages[i].Violations, Issue{
					File: pair.file, Line: pair.line, Table: "i18n.Text",
					Detail: glossaryDetail(usages[i].Term),
				})
			}
		}
	}

	for i := range usages {
		usages[i].Violations = append(usages[i].Violations,
			checkTableGlossary(tables, usages[i].Term)...)
		usages[i].Occurrences += countTableOccurrences(tables, usages[i].Term)
	}
	return usages
}

// glossaryDetail mô tả một sai lệch thuật ngữ.
func glossaryDetail(term GlossaryTerm) string {
	return fmt.Sprintf("thuật ngữ %q phải dùng %q theo bảng thuật ngữ, nhánh VI không có",
		term.ZH[0], strings.Join(term.VI, " / "))
}

// checkTableGlossary so từng trường của bảng VI với trường tương ứng của
// bản ZH, theo một thuật ngữ.
func checkTableGlossary(tables []bilingualTable, term GlossaryTerm) []Issue {
	groups := map[string][]bilingualTable{}
	for _, tbl := range tables {
		key := tbl.pkg + "::" + tbl.base
		groups[key] = append(groups[key], tbl)
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var issues []Issue
	for _, key := range keys {
		group := groups[key]
		var vis, zhs []bilingualTable
		for _, tbl := range group {
			switch tbl.side {
			case "vi":
				vis = append(vis, tbl)
			case "zh":
				zhs = append(zhs, tbl)
			}
		}
		for i := 0; i < min(len(vis), len(zhs)); i++ {
			issues = append(issues, compareGlossaryLevels(vis[i], zhs[i], term)...)
		}
	}
	return issues
}

// compareGlossaryLevels so các trường có chuỗi literal ở cả hai bản.
func compareGlossaryLevels(vi, zh bilingualTable, term GlossaryTerm) []Issue {
	var issues []Issue

	paths := map[string]bool{}
	for path := range vi.levels {
		paths[path] = true
	}
	for path := range zh.levels {
		paths[path] = true
	}
	sortedPaths := make([]string, 0, len(paths))
	for path := range paths {
		sortedPaths = append(sortedPaths, path)
	}
	sort.Strings(sortedPaths)

	for _, path := range sortedPaths {
		viLevel, viOK := vi.levels[path]
		zhLevel, zhOK := zh.levels[path]
		if !viOK || !zhOK {
			continue
		}
		for _, key := range viLevel.keys {
			viText, ok := viLevel.texts[key]
			if !ok {
				continue
			}
			zhText, ok := zhLevel.texts[key]
			if !ok {
				continue
			}
			if !containsAny(zhText, term.ZH) {
				continue
			}
			if containsAnyFold(viText, term.VI) {
				continue
			}
			issues = append(issues, Issue{
				File: vi.file, Line: vi.line, Table: vi.name, Detail: glossaryDetail(term),
			})
		}
	}
	return issues
}

// countTableOccurrences đếm số trường của bảng song ngữ có chứa thuật ngữ.
func countTableOccurrences(tables []bilingualTable, term GlossaryTerm) int {
	count := 0
	for _, tbl := range tables {
		if tbl.side != "zh" {
			continue
		}
		for _, level := range tbl.levels {
			for _, text := range level.allTexts {
				if containsAny(text, term.ZH) {
					count++
				}
			}
		}
	}
	return count
}

// containsAny báo chuỗi có chứa ít nhất một trong các mẫu cho trước.
func containsAny(text string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

// containsAnyFold như containsAny nhưng không phân biệt hoa thường: bảng thuật
// ngữ viết "Đề cương" ở đầu ô, còn trong mã thường là "đề cương" giữa câu.
func containsAnyFold(text string, patterns []string) bool {
	folded := strings.ToLower(text)
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(folded, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}
