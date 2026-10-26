package imp

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

var cjkRe = regexp.MustCompile(`[一-鿿]`)

func TestImportTextsForSelectsLanguage(t *testing.T) {
	if got := importTextsFor("vi"); got != importTextsVI {
		t.Fatalf("importTextsFor(vi) chọn sai bảng")
	}
	if got := importTextsFor("zh"); got != importTextsZH {
		t.Fatalf("importTextsFor(zh) chọn sai bảng")
	}
	if got := importTextsFor(""); got != importTextsVI {
		t.Fatalf(`importTextsFor("") phải mặc định tiếng Việt`)
	}
	if got := importTextsFor("chinese"); got != importTextsZH {
		t.Fatalf("importTextsFor(chinese) chọn sai bảng")
	}
	if got := importTextsOrDefault(nil); got != importTextsVI {
		t.Fatalf("importTextsOrDefault(nil) phải mặc định tiếng Việt")
	}
}

func TestContractsAreBilingual(t *testing.T) {
	cases := []struct {
		name string
		vi   string
		zh   string
	}{
		{"segment", segmentContract(importTextsVI).Description, segmentContract(importTextsZH).Description},
		{"analysis", analysisContract(importTextsVI).Description, analysisContract(importTextsZH).Description},
		{"range", rangeContract(importTextsVI).Description, rangeContract(importTextsZH).Description},
		{"synthesis", synthesisContract(importTextsVI).Description, synthesisContract(importTextsZH).Description},
	}
	for _, c := range cases {
		if c.vi == c.zh {
			t.Fatalf("%s: mô tả vi và zh phải khác nhau", c.name)
		}
		if cjkRe.MatchString(c.vi) {
			t.Fatalf("%s: mô tả tiếng Việt còn ký tự Trung: %q", c.name, c.vi)
		}
		if !cjkRe.MatchString(c.zh) {
			t.Fatalf("%s: mô tả tiếng Trung phải giữ ký tự Trung: %q", c.name, c.zh)
		}
	}
}

func TestContractSchemaDescriptionsAreBilingual(t *testing.T) {
	vi, err := json.Marshal(segmentContract(importTextsVI).Schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	zh, err := json.Marshal(segmentContract(importTextsZH).Schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	if cjkRe.Match(vi) {
		t.Fatalf("schema tiếng Việt còn ký tự Trung: %s", vi)
	}
	if !cjkRe.Match(zh) {
		t.Fatalf("schema tiếng Trung phải giữ ký tự Trung: %s", zh)
	}
	if !strings.Contains(string(vi), "ranh giới") {
		t.Fatalf("schema tiếng Việt thiếu nhãn tiếng Việt: %s", vi)
	}
	if !strings.Contains(string(zh), "边界") {
		t.Fatalf("schema tiếng Trung thiếu nhãn tiếng Trung: %s", zh)
	}
}

func TestBuildLedgerIsBilingual(t *testing.T) {
	prior := []ImportedChapterFacts{{
		Characters:        []string{"Lam"},
		ForeshadowUpdates: []domain.ForeshadowUpdate{{ID: "f1", Action: "plant", Description: "tin cũ"}},
		StateChanges:      []domain.StateChange{{Entity: "Lam", Field: "status", NewValue: "bị thương"}},
	}}

	vi := buildLedger(prior, importTextsVI)
	if cjkRe.MatchString(vi) {
		t.Fatalf("ledger tiếng Việt còn ký tự Trung: %q", vi)
	}
	if !strings.Contains(vi, "Nhân vật đã biết") {
		t.Fatalf("ledger tiếng Việt thiếu nhãn: %q", vi)
	}
	if !strings.Contains(vi, "Phục bút đang hoạt động") {
		t.Fatalf("ledger tiếng Việt thiếu nhãn phục bút: %q", vi)
	}

	zh := buildLedger(prior, importTextsZH)
	if !strings.Contains(zh, "已知人物") {
		t.Fatalf("ledger tiếng Trung thiếu nhãn gốc: %q", zh)
	}
	if !strings.Contains(zh, "活跃伏笔") {
		t.Fatalf("ledger tiếng Trung thiếu nhãn phục bút gốc: %q", zh)
	}
}

func TestBuildAnalyzePayloadIsBilingual(t *testing.T) {
	seg := &Segmentation{Chapters: []ChapterSpan{{Number: 1, Title: "Mở đầu"}}}
	vi := buildAnalyzePayload(nil, seg, "", 0, 1, importTextsVI)
	if cjkRe.MatchString(vi) {
		t.Fatalf("payload tiếng Việt còn ký tự Trung: %q", vi)
	}
	if !strings.Contains(vi, "Hãy phân tích chương 1-1") {
		t.Fatalf("payload tiếng Việt thiếu lệnh phân tích: %q", vi)
	}
	if !strings.Contains(vi, "## Chương 1: Mở đầu") {
		t.Fatalf("payload tiếng Việt thiếu tiêu đề chương: %q", vi)
	}

	zh := buildAnalyzePayload(nil, seg, "", 0, 1, importTextsZH)
	if !strings.Contains(zh, "请分析第 1-1 章") {
		t.Fatalf("payload tiếng Trung thiếu lệnh gốc: %q", zh)
	}
	if !strings.Contains(zh, "## 第 1 章：Mở đầu") {
		t.Fatalf("payload tiếng Trung thiếu tiêu đề chương gốc: %q", zh)
	}
}

func TestBuildRangeAndBookPayloadsAreBilingual(t *testing.T) {
	facts := []ImportedChapterFacts{{Chapter: 1, Summary: "tóm tắt"}}
	viRange := buildRangePayload(facts, importTextsVI)
	if cjkRe.MatchString(viRange) {
		t.Fatalf("payload range tiếng Việt còn ký tự Trung: %q", viRange)
	}
	if !strings.Contains(viRange, "Hãy tạo một RangeDigest") {
		t.Fatalf("payload range tiếng Việt thiếu lệnh: %q", viRange)
	}
	zhRange := buildRangePayload(facts, importTextsZH)
	if !strings.Contains(zhRange, "请为第 1-1 章生成一个 RangeDigest") {
		t.Fatalf("payload range tiếng Trung thiếu lệnh gốc: %q", zhRange)
	}

	viBook := buildBookPayload("{}", 3, importTextsVI)
	if cjkRe.MatchString(viBook) {
		t.Fatalf("payload book tiếng Việt còn ký tự Trung: %q", viBook)
	}
	if !strings.Contains(viBook, "Hãy tạo BookSynthesis") {
		t.Fatalf("payload book tiếng Việt thiếu lệnh: %q", viBook)
	}
	zhBook := buildBookPayload("{}", 3, importTextsZH)
	if !strings.Contains(zhBook, "请生成 BookSynthesis") {
		t.Fatalf("payload book tiếng Trung thiếu lệnh gốc: %q", zhBook)
	}
}

// TestValidationErrorsAreBilingual bảo vệ nhánh [L]: các thông báo lỗi kiểm tra
// nghiệp vụ mà llmcontract gửi LẠI mô hình phải theo ngôn ngữ tác phẩm.
// Nếu chỉ có tiếng Việt thì mô hình viết truyện tiếng Trung sẽ nhận lỗi lệch
// giọng; nếu chỉ có tiếng Trung thì mô hình viết truyện tiếng Việt bị kéo lệch.
func TestValidationErrorsAreBilingual(t *testing.T) {
	check := func(name string, vi, zh error) {
		t.Helper()
		if vi == nil || zh == nil {
			t.Fatalf("%s: cả hai nhánh ngôn ngữ đều phải trả lỗi", name)
		}
		if cjkRe.MatchString(vi.Error()) {
			t.Fatalf("%s: lỗi tiếng Việt còn ký tự Trung: %q", name, vi.Error())
		}
		if !cjkRe.MatchString(zh.Error()) {
			t.Fatalf("%s: lỗi tiếng Trung phải giữ ký tự Trung: %q", name, zh.Error())
		}
	}

	seg := &Segmentation{Chapters: []ChapterSpan{{Number: 1, Title: "Mở đầu"}}}
	shortBatch := &AnalysisBatchResult{Chapters: []ImportedChapterFacts{{Chapter: 1}}}
	check("validateBatch",
		validateBatch(shortBatch, seg, 0, 2, importTextsVI),
		validateBatch(shortBatch, seg, 0, 2, importTextsZH))

	badKind := []ImportedVolumeRange{{Title: "Tập một", Arcs: []ImportedArcRange{{StartChapter: 1, EndChapter: 2}}}}
	check("validateStructure",
		validateStructure(badKind, 3, importTextsVI),
		validateStructure(badKind, 3, importTextsZH))

	digest := &RangeDigest{StartChapter: 1, EndChapter: 5, Plot: "truyện"}
	check("validateRangeDigest",
		validateRangeDigest(digest, 1, 2, "range digest", importTextsVI),
		validateRangeDigest(digest, 1, 2, "range digest", importTextsZH))

	unit := SourceUnit{ID: "L1", Line: 1, Text: "风起云涌"}
	units := map[string]SourceUnit{"L1": unit}
	check("resolveBoundaryByte",
		resolveBoundaryByteErr(units, "L9", "", importTextsVI),
		resolveBoundaryByteErr(units, "L9", "", importTextsZH))

	vVI := chunkValidator{projIDs: map[string]bool{"L1": true}, ownedIDs: map[string]bool{"L1": true},
		unitByID: units, texts: importTextsVI}
	vZH := chunkValidator{projIDs: map[string]bool{"L1": true}, ownedIDs: map[string]bool{"L1": true},
		unitByID: units, texts: importTextsZH}
	check("chunkValidator.validate",
		vVI.validate([]BoundaryDecision{{UnitID: "", Kind: kindChapter}}),
		vZH.validate([]BoundaryDecision{{UnitID: "", Kind: kindChapter}}))
}

// resolveBoundaryByteErr bọc lỗi của resolveBoundaryByte để test dùng chung
// kiểu error với các nhánh kiểm tra khác.
func resolveBoundaryByteErr(units map[string]SourceUnit, unitID, anchor string, t *importTexts) error {
	_, err := resolveBoundaryByte(units, unitID, anchor, t)
	return err
}
