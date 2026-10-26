package imp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
)

// TestDiscardAnalysesAfter bảo vệ #4a: dọn dẹp các sản phẩm phân tích cũ vượt quá tiền tố mới,
// bảo đảm «phân tích lại một chương làm mất hiệu lực mọi phân tích phía sau», ngăn ledger cũ
// bị tái sử dụng khi các chương phía sau đến.
func TestDiscardAnalysesAfter(t *testing.T) {
	ws := OpenWorkspace(t.TempDir())
	for c := 1; c <= 5; c++ {
		if err := writeArtifact(ws, analysisPath(c), "d", ChapterAnalysisPayload{Facts: ImportedChapterFacts{Chapter: c}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := discardAnalysesAfter(ws, 2, 5); err != nil {
		t.Fatalf("dọn dẹp không nên thất bại: %v", err)
	}
	for c := 1; c <= 2; c++ {
		if !ws.has(analysisPath(c)) {
			t.Fatalf("chương %d thuộc tiền tố mới phải được giữ lại", c)
		}
	}
	for c := 3; c <= 5; c++ {
		if ws.has(analysisPath(c)) {
			t.Fatalf("chương %d vượt quá tiền tố mới phải bị dọn dẹp", c)
		}
	}
}

// analyzeFixture dựng một bản cắt gồm n chương, mỗi chương nội dung rất ngắn, dùng cho kiểm thử lô/phân tích.
func analyzeFixture(t *testing.T, n int) ([]byte, *Segmentation) {
	t.Helper()
	var b strings.Builder
	for c := 1; c <= n; c++ {
		b.WriteString("第")
		b.WriteString(strings.Repeat("一", 1))
		b.WriteString("章\n正文\n")
	}
	norm := []byte(b.String())
	units := buildSourceUnits(norm, 0)
	var ds []BoundaryDecision
	for i := 0; i < len(units); i += 2 { // mỗi 2 dòng là 1 chương (dòng tiêu đề + dòng nội dung)
		ds = append(ds, BoundaryDecision{UnitID: units[i].ID, Kind: kindChapter, Title: units[i].Text})
	}
	seg, err := resolveSegmentation(norm, units, ds)
	if err != nil {
		t.Fatalf("cắt fixture thất bại: %v", err)
	}
	if len(seg.Chapters) != n {
		t.Fatalf("số chương của fixture %d != %d", len(seg.Chapters), n)
	}
	return norm, seg
}

func TestPlanBatchOutputBudgetCaps(t *testing.T) {
	_, seg := analyzeFixture(t, 10)
	// Đầu vào thoáng, nhưng ngân sách đầu ra nhìn thấy chỉ đủ cho 2 chương (#83 canh giữ độ mịn lô, §20.4.2).
	b := AnalyzeBudget{ContextBytes: 1 << 20, MaxOutputTokens: 250, PerChapterOutput: 100, PromptOverhead: 0}
	end := planBatch(seg.Chapters, 0, 0, b)
	if end != 2 {
		t.Fatalf("ngân sách đầu ra phải giới hạn lô ở 2 chương, được end=%d", end)
	}
}

func TestPlanBatchInputBudgetCaps(t *testing.T) {
	_, seg := analyzeFixture(t, 10)
	// Đầu ra thoáng, nhưng ngân sách byte đầu vào chỉ đủ khoảng 1 chương.
	one := chapterBytes(seg.Chapters, 0)
	b := AnalyzeBudget{ContextBytes: one + 1, MaxOutputTokens: 1 << 20, PerChapterOutput: 1, PromptOverhead: 0}
	end := planBatch(seg.Chapters, 0, 0, b)
	if end != 1 {
		t.Fatalf("ngân sách đầu vào phải giới hạn lô ở 1 chương, được end=%d", end)
	}
}

func factsJSON(chapter int, title string) string {
	f := map[string]any{
		"chapter": chapter, "title": title, "summary": "摘要", "core_event": "核心事件",
		"key_events": []string{"事件"}, "hook": nil, "scenes": []string{}, "characters": []string{},
		"character_evidence": []any{}, "world_evidence": []any{}, "timeline_events": []any{},
		"foreshadow_updates": []any{}, "relationship_changes": []any{}, "state_changes": []any{},
		"hook_type": "mystery", "dominant_strand": "quest",
	}
	data, _ := json.Marshal(f)
	return string(data)
}

func TestValidateBatchRejections(t *testing.T) {
	_, seg := analyzeFixture(t, 2)
	// Số lượng không khớp
	bad := &AnalysisBatchResult{Chapters: []ImportedChapterFacts{{Chapter: 1}}}
	if err := validateBatch(bad, seg, 0, 2, importTextsVI); err == nil {
		t.Fatal("số lượng không khớp phải bị từ chối")
	}
	// hook_type không hợp lệ
	var f ImportedChapterFacts
	_ = json.Unmarshal([]byte(factsJSON(1, seg.Chapters[0].Title)), &f)
	f.HookType = "bogus"
	if err := validateBatch(&AnalysisBatchResult{Chapters: []ImportedChapterFacts{f}}, seg, 0, 1, importTextsVI); err == nil {
		t.Fatal("hook_type không hợp lệ phải bị từ chối")
	}
	// Biến thể khác hoa thường của enum: kiểm tra vẫn pass và được chuẩn hoá tại chỗ thành chữ thường — commit_chapter không kiểm tra lại enum,
	// biến thể đi thẳng vào trạng thái chính thức sẽ bị logic tiêu thụ chuỗi chính xác coi là loại lạ.
	_ = json.Unmarshal([]byte(factsJSON(1, seg.Chapters[0].Title)), &f)
	f.HookType, f.DominantStrand = "Crisis", "QUEST"
	got := &AnalysisBatchResult{Chapters: []ImportedChapterFacts{f}}
	if err := validateBatch(got, seg, 0, 1, importTextsVI); err != nil {
		t.Fatalf("biến thể khác hoa thường phải qua kiểm tra: %v", err)
	}
	if got.Chapters[0].HookType != "crisis" || got.Chapters[0].DominantStrand != "quest" {
		t.Fatalf("enum phải được chuẩn hoá thành chữ thường khi ghi xuống đĩa: %+v", got.Chapters[0])
	}
}

func TestAnalyzeNextPersistsWithRebatchOnTruncation(t *testing.T) {
	norm, seg := analyzeFixture(t, 2)
	book := t.TempDir()
	ws := &Workspace{dir: book}
	// Lô đầu 2 chương bị cắt ngắn: chương 1 đầy đủ, chương 2 cắt cụt → cứu được tiền tố liên tục là chương 1 (§9.5).
	truncated := `{"chapters":[` + factsJSON(1, seg.Chapters[0].Title) + `,{"chapter":2,"summary":"截断`
	m := &mockModel{
		responses: []string{truncated},
		stops:     []agentcore.StopReason{agentcore.StopReasonLength},
	}
	budget := AnalyzeBudget{ContextBytes: 1 << 20, MaxOutputTokens: 1000, PerChapterOutput: 10, PromptOverhead: 0}
	done, err := AnalyzeNext(context.Background(), m, "sys", ws, norm, seg, "segid", "v1", budget, callProfile{})
	if err != nil {
		t.Fatalf("AnalyzeNext: %v", err)
	}
	if done != 1 {
		t.Fatalf("khi bị cắt ngắn phải cứu được tiền tố liên tục là chương 1, được %d", done)
	}
	if !ws.has(analysisPath(1)) || ws.has(analysisPath(2)) {
		t.Fatal("chỉ nên ghi xuống đĩa chương 1")
	}
	if analyzedChapters(ws, seg, norm, "segid", "v1") != 1 {
		t.Fatal("số chương đã phân tích phải là 1")
	}
	// failures/ phải lưu phản hồi gốc và trạng thái cứu được (§14.2).
	if !ws.has("failures/last-response.txt") || !ws.has("failures/last.json") {
		t.Fatal("phải lưu phản hồi gốc lúc thất bại cùng siêu dữ liệu")
	}
}

func TestSalvagePrefixContiguous(t *testing.T) {
	_, seg := analyzeFixture(t, 3)
	// 2 chương đầu đầy đủ, chương 3 bị cắt ngắn.
	raw := `{"chapters":[` +
		factsJSON(1, seg.Chapters[0].Title) + `,` +
		factsJSON(2, seg.Chapters[1].Title) + `,` +
		`{"chapter":3,"summary":"截断`
	got := salvagePrefix(raw, seg, 0, importTextsVI)
	if len(got) != 2 {
		t.Fatalf("phải cứu được tiền tố liên tục của 2 chương đầu, được %d", len(got))
	}
	if got[0].Chapter != 1 || got[1].Chapter != 2 {
		t.Fatal("số chương của tiền tố không liên tục")
	}
}

func TestSalvagePrefixStopsAtGap(t *testing.T) {
	_, seg := analyzeFixture(t, 3)
	// Sau chương 1 nhảy thẳng sang chương 3 → việc cứu dừng ở chỗ bỏ số, chỉ trả về chương 1.
	raw := `{"chapters":[` + factsJSON(1, seg.Chapters[0].Title) + `,` + factsJSON(3, seg.Chapters[2].Title) + `]}`
	got := salvagePrefix(raw, seg, 0, importTextsVI)
	if len(got) != 1 {
		t.Fatalf("phải dừng ở chỗ bỏ số, được %d", len(got))
	}
}

// TestAnalyzedChaptersInvalidatesOnUpstreamChange xác minh việc đổi định danh cắt hoặc phiên bản prompt làm cho phân tích đã ghi xuống đĩa mất hiệu lực (bất biến 1).
// Đây là phần lõi để cơ chế InputDigest thực sự có tác dụng: đổi phía trên là mất hiệu lực phía dưới, chứ không chỉ dựa vào việc tệp có tồn tại hay không.
func TestAnalyzedChaptersInvalidatesOnUpstreamChange(t *testing.T) {
	norm, seg := analyzeFixture(t, 2)
	ws := &Workspace{dir: t.TempDir()}
	m := &mockModel{responses: []string{
		`{"chapters":[` + factsJSON(1, seg.Chapters[0].Title) + `,` + factsJSON(2, seg.Chapters[1].Title) + `]}`,
	}}
	budget := AnalyzeBudget{ContextBytes: 1 << 20, MaxOutputTokens: 1 << 20, PerChapterOutput: 10, PromptOverhead: 0}
	if _, err := AnalyzeNext(context.Background(), m, "sys", ws, norm, seg, "segid-A", "v1", budget, callProfile{}); err != nil {
		t.Fatalf("AnalyzeNext: %v", err)
	}
	if got := analyzedChapters(ws, seg, norm, "segid-A", "v1"); got != 2 {
		t.Fatalf("cùng định danh/phiên bản phải nhận 2 chương, được %d", got)
	}
	if got := analyzedChapters(ws, seg, norm, "segid-B", "v1"); got != 0 {
		t.Fatalf("thay đổi định danh cắt phải làm mất hiệu lực toàn bộ phân tích, được %d", got)
	}
	if got := analyzedChapters(ws, seg, norm, "segid-A", "v2"); got != 0 {
		t.Fatalf("thay đổi phiên bản prompt phải làm mất hiệu lực toàn bộ phân tích, được %d", got)
	}
}
