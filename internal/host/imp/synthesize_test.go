package imp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/domain"
)

func factsN(n int) []ImportedChapterFacts {
	out := make([]ImportedChapterFacts, n)
	for i := 0; i < n; i++ {
		out[i] = ImportedChapterFacts{
			Chapter: i + 1, Title: "第" + itoa(i+1) + "章", CoreEvent: "事件", Summary: "摘要",
			HookType: "mystery", DominantStrand: "quest",
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestValidateStructure(t *testing.T) {
	ok := []ImportedVolumeRange{{Title: "卷一", Arcs: []ImportedArcRange{{StartChapter: 1, EndChapter: 3}}}}
	if err := validateStructure(ok, 3, importTextsVI); err != nil {
		t.Fatalf("cấu trúc hợp lệ phải qua: %v", err)
	}
	gap := []ImportedVolumeRange{{Arcs: []ImportedArcRange{{StartChapter: 1, EndChapter: 2}, {StartChapter: 4, EndChapter: 5}}}}
	if err := validateStructure(gap, 5, importTextsVI); err == nil {
		t.Fatal("lỗ hổng phải bị từ chối")
	}
	short := []ImportedVolumeRange{{Arcs: []ImportedArcRange{{StartChapter: 1, EndChapter: 2}}}}
	if err := validateStructure(short, 3, importTextsVI); err == nil {
		t.Fatal("chưa phủ N phải bị từ chối")
	}
}

func TestAssembleFoundationHappyClosed(t *testing.T) {
	facts := factsN(3)
	s := &BookSynthesis{
		Synopsis:     "无剧透简介",
		Premise:      "# 故事前提\n\n前提",
		Characters:   []domain.Character{{Name: "甲"}},
		PlanningTier: domain.PlanningTierShort,
		StoryStatus:  storyClosed,
		Compass:      domain.StoryCompass{EndingDirection: "收束"},
		Structure:    []ImportedVolumeRange{{Title: "卷一", Arcs: []ImportedArcRange{{Title: "弧一", StartChapter: 1, EndChapter: 3}}}},
	}
	f, err := AssembleFoundation(s, facts, true, "book.txt")
	if err != nil {
		t.Fatalf("lắp ráp phải thành công: %v", err)
	}
	if len(domain.FlattenOutline(f.Volumes)) != 3 {
		t.Fatal("số chương sau khi bung phải là 3")
	}
	if !f.Volumes[len(f.Volumes)-1].Final {
		t.Fatal("khi closed thì tập cuối phải là Final")
	}
	if f.Book.Title != "book" || f.Book.Synopsis != "无剧透简介" {
		t.Fatalf("lắp ráp thông tin tác phẩm sai: %+v", f.Book)
	}
}

func TestAssembleFoundationTitleMismatch(t *testing.T) {
	facts := factsN(2)
	facts[1].Title = "" // Phá vỡ tính nhất quán tiêu đề sẽ làm FlattenOutline kiểm tra thất bại? Tiêu đề rỗng nhưng cấu trúc lấy từ facts nên vẫn nhất quán.
	// Dùng chương mà cấu trúc không phủ tới để tạo ra bất nhất thật: số chương không khớp.
	s := &BookSynthesis{
		Synopsis: "无剧透简介", Premise: "# 故事前提", Characters: []domain.Character{{Name: "甲"}},
		PlanningTier: domain.PlanningTierShort, StoryStatus: storyOpen,
		Compass:   domain.StoryCompass{EndingDirection: "x"},
		Structure: []ImportedVolumeRange{{Arcs: []ImportedArcRange{{StartChapter: 1, EndChapter: 1}}}},
	}
	if _, err := AssembleFoundation(s, facts, false, "b.txt"); err == nil {
		t.Fatal("cấu trúc chỉ phủ 1 chương trong khi sự thật có 2 chương thì phải bị từ chối")
	}
}

func TestImportedBookTitle(t *testing.T) {
	if got := importedBookTitle("我的小说.txt"); got != "我的小说" {
		t.Fatalf("phải suy ra tên sách từ tên tệp: %q", got)
	}
}

func TestPlanFactRangesSplits(t *testing.T) {
	facts := factsN(20)
	one := len(compactFact(facts[0]))
	ranges := planFactRanges(facts, one*3) // mỗi đoạn khoảng 3 chương
	if len(ranges) < 2 {
		t.Fatalf("phải chia thành nhiều đoạn, được %d", len(ranges))
	}
	if ranges[0][0] != 0 || ranges[len(ranges)-1][1] != 20 {
		t.Fatal("các đoạn chưa phủ trọn vẹn")
	}
}

// TestToCompactCarriesEvidence bảo vệ #6: character/world evidence dựng ngược từ từng chương phải đi vào khung nhìn gọn cho bước tổng hợp,
// nếu không bộ tổng hợp chỉ có thể bịa nhân vật chính thức và quy tắc thế giới từ phần tóm tắt.
func TestToCompactCarriesEvidence(t *testing.T) {
	f := ImportedChapterFacts{
		Chapter: 1, Title: "第一章", CoreEvent: "e", Summary: "s",
		CharacterEvidence: []ImportedCharacterFact{{Chapter: 1, Name: "甲", Note: "沉稳"}},
		WorldEvidence:     []ImportedWorldFact{{Chapter: 1, Category: "magic", Fact: "灵气充盈"}},
	}
	cv := toCompact(f)
	if len(cv.CharacterEvidence) != 1 || cv.CharacterEvidence[0].Name != "甲" {
		t.Fatalf("character evidence chưa được mang vào khung nhìn gọn: %+v", cv.CharacterEvidence)
	}
	if len(cv.WorldEvidence) != 1 || cv.WorldEvidence[0].Fact != "灵气充盈" {
		t.Fatalf("world evidence chưa được mang vào khung nhìn gọn: %+v", cv.WorldEvidence)
	}
}

// TestSynthesizeRejectsRangeMismatch bảo vệ #4: trong giai đoạn Map của sách dài, chương bắt đầu/kết của tóm tắt đoạn phải khớp với yêu cầu,
// nếu không lúc gộp sẽ coi đoạn lệch chỗ là tóm tắt của đoạn này.
func TestSynthesizeRejectsRangeMismatch(t *testing.T) {
	err := validateRangeDigest(&RangeDigest{StartChapter: 1, EndChapter: 5, Plot: "错位区间"}, 1, 2, "range digest", importTextsVI)
	if err == nil {
		t.Fatal("chương bắt đầu/kết của đoạn không khớp yêu cầu thì phải bị từ chối")
	}
	if !strings.Contains(err.Error(), "phạm vi chương") {
		t.Fatalf("lỗi phải chỉ ra phạm vi đoạn không khớp, được: %v", err)
	}
}

// TestGroupDigestsByBudget bảo vệ #3 gom nhóm: các tóm tắt đoạn liên tục được chia thành nhóm liên tục theo ngân sách byte, tóm tắt đơn lẻ vượt ngân sách cũng tự thành một nhóm.
func TestGroupDigestsByBudget(t *testing.T) {
	ds := []RangeDigest{
		{StartChapter: 1, EndChapter: 5, Plot: strings.Repeat("x", 200)},
		{StartChapter: 6, EndChapter: 10, Plot: strings.Repeat("y", 200)},
		{StartChapter: 11, EndChapter: 15, Plot: strings.Repeat("z", 200)},
		{StartChapter: 16, EndChapter: 20, Plot: strings.Repeat("w", 200)},
	}
	per := len(mustJSON(t, ds[0]))
	groups := groupDigestsByBudget(ds, per*2+10) // mỗi nhóm chứa khoảng 2 cái
	if len(groups) != 2 || len(groups[0]) != 2 || len(groups[1]) != 2 {
		t.Fatalf("phải chia thành 2 nhóm mỗi nhóm 2 cái, được %v", groups)
	}
	if groups[0][0].StartChapter != 1 || groups[1][1].EndChapter != 20 {
		t.Fatal("các nhóm chưa giữ được phủ liên tục")
	}
}

// TestReduceToFitMergesUntilBudget bảo vệ #3: khi tổng khối tóm tắt đoạn vượt ngân sách thì gom từng tầng cho tới khi vừa chứa,
// thay vì đi không giới hạn vào lệnh tổng hợp cuối.
func TestReduceToFitMergesUntilBudget(t *testing.T) {
	ds := []RangeDigest{
		{StartChapter: 1, EndChapter: 5, Plot: strings.Repeat("x", 200)},
		{StartChapter: 6, EndChapter: 10, Plot: strings.Repeat("y", 200)},
		{StartChapter: 11, EndChapter: 15, Plot: strings.Repeat("z", 200)},
		{StartChapter: 16, EndChapter: 20, Plot: strings.Repeat("w", 200)},
	}
	budget := len(mustJSON(t, ds[0]))*2 + 10
	// Mỗi nhóm gom ra một tóm tắt nhỏ: chương 1-10, chương 11-20.
	m := &mockModel{responses: []string{
		rangeDigestJSON(1, 10, "合并一"),
		rangeDigestJSON(11, 20, "合并二"),
	}}
	out, err := reduceToFit(context.Background(), m, "range", ds, budget, 4096, callProfile{})
	if err != nil {
		t.Fatalf("reduceToFit: %v", err)
	}
	if len(out) != 2 || out[0].StartChapter != 1 || out[0].EndChapter != 10 || out[1].StartChapter != 11 || out[1].EndChapter != 20 {
		t.Fatalf("phải gom thành 2 tóm tắt đoạn liên tục, được %+v", out)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSynthesizeDirectWithMock(t *testing.T) {
	facts := factsN(3)
	resp := synthesisFixtureJSON(3, storyOpen)
	m := &mockModel{responses: []string{resp}}
	s, err := Synthesize(context.Background(), m, "sys", "range-sys", &Workspace{dir: t.TempDir()}, facts, 0, 4096, callProfile{})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if s.StoryStatus != storyOpen || len(s.Structure) != 1 {
		t.Fatalf("kết quả tổng hợp không khớp: %+v", s)
	}
	if _, err := AssembleFoundation(s, facts, false, "b.txt"); err != nil {
		t.Fatalf("lắp ráp phải thành công: %v", err)
	}
	_ = agentcore.StopReasonStop
}
