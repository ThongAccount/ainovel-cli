package parity

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRepoGlossaryParity là cổng chặn thuật ngữ của cả repo.
//
// Bảng thuật ngữ (docs/i18n-vi-glossary.md) là nguồn chuẩn của lớp song ngữ:
// một bản VI dịch lệch khỏi bảng thuật ngữ vẫn qua được cổng hình thức và
// cổng CJK, vì hai bên đều đủ ký tự và đủ trường. Chỉ có so với bảng thuật
// ngữ mới thấy được từ đã bị thay bằng từ khác.
//
// Cổng siết những dòng repo đang tuân thủ; những dòng còn lại được miễn
// theo danh sách ngoại lệ ghim trong glossary_gate.go, mỗi mục kèm lý do.
// Danh sách ngoại lệ phải còn hiệu lực: mục không khớp dòng nào, hoặc mục
// khớp một dòng đã không còn sai lệch, đều bị báo đỏ — đó là chống rỗng.
func TestRepoGlossaryParity(t *testing.T) {
	root := repoRoot(t)
	report, err := CheckGlossary(root)
	if err != nil {
		t.Fatalf("kiểm tra thuật ngữ: %v", err)
	}

	// Chặn "xanh giả": nếu cổng không còn gì để kiểm tra thì nó không bảo vệ
	// được gì, và phải báo ngay thay vì im lặng.
	if report.Terms < 100 {
		t.Fatalf("chỉ đọc được %d dòng bảng thuật ngữ, có vẻ bộ đọc đã hỏng", report.Terms)
	}
	if report.Enforced < 50 {
		t.Fatalf("chỉ siết được %d dòng thuật ngữ, có vẻ danh sách ngoại lệ đã nuốt hết bảng thuật ngữ",
			report.Enforced)
	}
	if report.EnforcedOccurrences < 200 {
		t.Fatalf("chỉ kiểm tra được %d chỗ thuật ngữ, có vẻ bộ đọc đã hỏng",
			report.EnforcedOccurrences)
	}
	if report.Exempted == 0 {
		t.Fatalf("không ngoại lệ nào được dùng, có vẻ danh sách ngoại lệ không khớp bảng thuật ngữ")
	}

	if len(report.Violations) > 0 {
		var sb strings.Builder
		for _, issue := range report.Violations {
			sb.WriteString("\n  " + issue.String())
		}
		t.Errorf("thuật ngữ không khớp bảng thuật ngữ (%d sai lệch):%s",
			len(report.Violations), sb.String())
	}

	if len(report.StaleExceptions) > 0 {
		t.Errorf("ngoại lệ không còn khớp dòng nào trong bảng thuật ngữ (%d): %s — xóa hoặc sửa mục đó",
			len(report.StaleExceptions), exceptionSummary(report.StaleExceptions))
	}

	if len(report.RedundantExceptions) > 0 {
		t.Errorf("ngoại lệ không còn cần thiết (%d): %s — mã đã đúng bảng thuật ngữ, hãy xóa mục đó",
			len(report.RedundantExceptions), exceptionSummary(report.RedundantExceptions))
	}

	t.Logf("đã kiểm tra %d dòng bảng thuật ngữ: %d dòng siết (%d chỗ), %d dòng miễn theo ngoại lệ, %d sai lệch",
		report.Terms, report.Enforced, report.EnforcedOccurrences, report.Exempted, len(report.Violations))
}

// TestGlossaryExceptionsAreWellFormed bảo đảm danh sách ngoại lệ không rỗng
// và không mơ hồ: mỗi mục phải có lý do, không trùng nhau, và khớp đúng một
// dòng của bảng thuật ngữ.
func TestGlossaryExceptionsAreWellFormed(t *testing.T) {
	root := repoRoot(t)
	terms, err := ParseGlossary(filepath.Join(root, "docs", "i18n-vi-glossary.md"))
	if err != nil {
		t.Fatalf("đọc bảng thuật ngữ: %v", err)
	}

	seen := map[string]bool{}
	for _, exc := range glossaryExceptions {
		if strings.TrimSpace(exc.Reason) == "" {
			t.Errorf("ngoại lệ %q không có lý do", exc.ZH[0])
		}
		if len(exc.ZH) == 0 {
			t.Errorf("ngoại lệ %q không có thuật ngữ tiếng Trung", exc.VI)
		}
		for _, zh := range exc.ZH {
			if seen[zh] {
				t.Errorf("thuật ngữ %q bị ghim ngoại lệ hai lần", zh)
			}
			seen[zh] = true
		}
	}

	for _, term := range terms {
		matches := 0
		for _, exc := range glossaryExceptions {
			hit := false
			for _, zh := range exc.ZH {
				for _, termZH := range term.ZH {
					if zh == termZH {
						hit = true
					}
				}
			}
			if hit {
				matches++
			}
		}
		if matches > 1 {
			t.Errorf("dòng %q khớp %d ngoại lệ, phải khớp đúng một", term.ZH[0], matches)
		}
	}
}

// TestCheckGlossaryUsagesFlagsEnforcedTerm kiểm tra phần logic của cổng:
// một thuật ngữ không nằm trong danh sách ngoại lệ thì sai lệch phải được báo.
func TestCheckGlossaryUsagesFlagsEnforcedTerm(t *testing.T) {
	usages := []GlossaryUsage{{
		Term:        GlossaryTerm{ZH: []string{"章节"}, VI: []string{"chương"}},
		Occurrences: 3,
		Violations: []Issue{{
			File: "internal/demo/texts.go", Line: 42, Table: "demoTextsVI",
			Detail: "thuật ngữ \"章节\" phải dùng \"chương\" theo bảng thuật ngữ, nhánh VI không có",
		}},
	}}

	report := checkGlossaryUsages(usages)
	if len(report.Violations) != 1 {
		t.Fatalf("thuật ngữ không có ngoại lệ phải bị báo, thực tế báo %d sai lệch", len(report.Violations))
	}
	if report.Enforced != 1 || report.Exempted != 0 {
		t.Fatalf("phải siết 1 dòng và miễn 0, thực tế siết %d miễn %d",
			report.Enforced, report.Exempted)
	}
	if len(report.StaleExceptions) != len(glossaryExceptions) {
		t.Fatalf("các ngoại lệ không dùng phải bị báo là cũ, thực tế báo %d/%d",
			len(report.StaleExceptions), len(glossaryExceptions))
	}
}

// TestCheckGlossaryUsagesExemptsPinnedTerm kiểm tra rằng ngoại lệ ghim thật
// sự miễn trừ: thuật ngữ có ngoại lệ thì sai lệch của nó không bị báo.
func TestCheckGlossaryUsagesExemptsPinnedTerm(t *testing.T) {
	exc := glossaryExceptions[0]
	usages := []GlossaryUsage{{
		Term:        GlossaryTerm{ZH: exc.ZH, VI: []string{exc.VI}},
		Occurrences: 2,
		Violations: []Issue{{
			File: "internal/demo/texts.go", Line: 7, Table: "demoTextsVI",
			Detail: "sai lệch đã được ghim ngoại lệ",
		}},
	}}

	report := checkGlossaryUsages(usages)
	if len(report.Violations) != 0 {
		t.Fatalf("thuật ngữ đã ghim ngoại lệ không được báo sai lệch, thực tế báo %d", len(report.Violations))
	}
	if report.Exempted != 1 || report.Enforced != 0 {
		t.Fatalf("phải miễn 1 dòng và siết 0, thực tế miễn %d siết %d",
			report.Exempted, report.Enforced)
	}
	if len(report.RedundantExceptions) != 0 {
		t.Fatalf("ngoại lệ còn hiệu lực không được báo là thừa, thực tế báo %v", report.RedundantExceptions)
	}
}

// TestCheckGlossaryUsagesReportsRedundantException kiểm tra chống rỗng:
// nếu mã đã được sửa đúng bảng thuật ngữ thì mục ngoại lệ phải bị báo là
// thừa, để người ta xóa nó đi chứ không để nó nằm mãi che một quy tắc.
func TestCheckGlossaryUsagesReportsRedundantException(t *testing.T) {
	exc := glossaryExceptions[0]
	usages := []GlossaryUsage{{
		Term:        GlossaryTerm{ZH: exc.ZH, VI: []string{exc.VI}},
		Occurrences: 2,
	}}

	report := checkGlossaryUsages(usages)
	if len(report.RedundantExceptions) != 1 {
		t.Fatalf("ngoại lệ không còn cần thiết phải bị báo, thực tế báo %v", report.RedundantExceptions)
	}
	if report.RedundantExceptions[0] != exc.ZH[0] {
		t.Fatalf("phải báo ngoại lệ %q, thực tế báo %q", exc.ZH[0], report.RedundantExceptions[0])
	}
}

// TestCheckGlossaryUsagesReportsStaleException kiểm tra rằng ngoại lệ không
// còn khớp dòng nào trong bảng thuật ngữ cũng phải bị báo — nếu không, một
// ngoại lệ cũ có thể nằm im cho tới khi bảng thuật ngữ đổi tên.
func TestCheckGlossaryUsagesReportsStaleException(t *testing.T) {
	report := checkGlossaryUsages(nil)
	if len(report.StaleExceptions) != len(glossaryExceptions) {
		t.Fatalf("mọi ngoại lệ không khớp phải bị báo là cũ, thực tế báo %d/%d",
			len(report.StaleExceptions), len(glossaryExceptions))
	}
}

// TestCheckGlossaryUsagesMatchesExceptionByZHName bảo đảm ngoại lệ khớp theo
// tên thuật ngữ, không theo vị trí: bảng thuật ngữ có thể thêm bớt dòng mà
// danh sách ngoại lệ không bị lệch.
func TestCheckGlossaryUsagesMatchesExceptionByZHName(t *testing.T) {
	exc := glossaryExceptions[0]
	// Thuật ngữ có thêm cách viết tiếng Trung thứ hai, thứ tự cũng khác.
	term := GlossaryTerm{ZH: []string{exc.ZH[0] + "-khác", exc.ZH[0]}, VI: []string{exc.VI}}
	if got := exceptionFor(term); got < 0 {
		t.Fatalf("ngoại lệ %q phải khớp theo tên thuật ngữ", exc.ZH[0])
	}
}
