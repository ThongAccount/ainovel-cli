package imp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"maps"
	"os"
	"slices"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// analysisSchemaVersion là phiên bản schema sự thật theo từng chương, được đưa vào InputDigest.
const analysisSchemaVersion = 2

// ImportedCharacterFact / ImportedWorldFact là quan sát gọn dùng cho việc tổng hợp toàn tác phẩm, không ghi thẳng vào nhân vật hay quy tắc thế giới chính thức.
// Ít nhất phải mang số chương, để kết quả tổng hợp có nguồn gốc ổn định (RFC §9.1).
type ImportedCharacterFact struct {
	Chapter int    `json:"chapter"`
	Name    string `json:"name"`
	Note    string `json:"note,omitempty"`
}

type ImportedWorldFact struct {
	Chapter  int    `json:"chapter"`
	Category string `json:"category,omitempty"`
	Fact     string `json:"fact"`
}

// ImportedChapterFacts là sản phẩm có cấu trúc suy ra từ một chương (RFC §9.1).
type ImportedChapterFacts struct {
	Chapter             int                        `json:"chapter"`
	Title               string                     `json:"title"`
	Summary             string                     `json:"summary"`
	KeyEvents           []string                   `json:"key_events"`
	CoreEvent           string                     `json:"core_event"`
	Hook                string                     `json:"hook,omitempty"`
	Scenes              []string                   `json:"scenes,omitempty"`
	Characters          []string                   `json:"characters,omitempty"`
	CharacterEvidence   []ImportedCharacterFact    `json:"character_evidence,omitempty"`
	WorldEvidence       []ImportedWorldFact        `json:"world_evidence,omitempty"`
	TimelineEvents      []domain.TimelineEvent     `json:"timeline_events,omitempty"`
	ForeshadowUpdates   []domain.ForeshadowUpdate  `json:"foreshadow_updates,omitempty"`
	RelationshipChanges []domain.RelationshipEntry `json:"relationship_changes,omitempty"`
	StateChanges        []domain.StateChange       `json:"state_changes,omitempty"`
	HookType            string                     `json:"hook_type"`
	DominantStrand      string                     `json:"dominant_strand"`
}

// AnalysisBatchResult là kết quả có cấu trúc của một lần gọi theo lô, mỗi phần tử là sự thật của một chương.
type AnalysisBatchResult struct {
	Chapters []ImportedChapterFacts `json:"chapters"`
}

// ChapterAnalysisPayload là tải trọng của sản phẩm phân tích một chương; các chương cùng lô ghi cùng BatchStart/BatchEnd.
type ChapterAnalysisPayload struct {
	BatchStart int                  `json:"batch_start"`
	BatchEnd   int                  `json:"batch_end"`
	Facts      ImportedChapterFacts `json:"facts"`
}

// AnalyzeBudget là cặp ngân sách đầu vào/đầu ra cho việc phân tích theo từng chương (RFC §9.2).
// Đầu vào tính bằng byte để xấp xỉ context window; đầu ra tính bằng phần dự phòng sự thật thận trọng cho mỗi chương để xấp xỉ trần completion.
type AnalyzeBudget struct {
	ContextBytes     int // ngân sách đầu vào (nội dung truyện + ledger + overhead)
	MaxOutputTokens  int // ngân sách đầu ra nhìn thấy được (trần completion)
	PerChapterOutput int // phần dự phòng đầu ra thận trọng cho mỗi chương
	PromptOverhead   int // chi phí đầu vào cố định của system/ledger (byte)
}

func analysisPath(chapter int) string {
	return fmt.Sprintf("%s/%06d.json", dirAnalyses, chapter)
}

// analyzedChapters trả về số sản phẩm phân tích liên tục kể từ chương 1, mà InputDigest của chúng khớp với định danh/phiên bản/nội dung của lần cắt hiện tại (RFC §9.6).
// Thiếu, phân tích lỗi hoặc digest không khớp đều cắt chuỗi tại đây, để mọi thay đổi phía trên (cắt lại, đổi phiên bản prompt/schema) làm mất hiệu lực tự nhiên các phân tích phía dưới.
func analyzedChapters(w *Workspace, seg *Segmentation, normalized []byte, segIdentity, promptVersion string) int {
	n := 0
	for c := 1; c <= len(seg.Chapters); c++ {
		a, err := readArtifact[ChapterAnalysisPayload](w, analysisPath(c))
		if err != nil {
			break
		}
		if a.InputDigest != chapterInputDigest(segIdentity, promptVersion, seg, normalized, c-1) {
			break
		}
		n++
	}
	return n
}

// analyzedChaptersStrict giữ cùng ngữ nghĩa độ tươi mới với analyzedChapters, nhưng lộ ra các sản phẩm đã có bị hỏng hoặc không đọc được.
// Khôi phục trạng thái dùng bản nghiêm ngặt, để không nhầm lỗi đọc thật thành “chưa phân tích” rồi ghi đè làm lại.
func analyzedChaptersStrict(w *Workspace, seg *Segmentation, normalized []byte, segIdentity, promptVersion string) (int, error) {
	n := 0
	for c := 1; c <= len(seg.Chapters); c++ {
		a, err := readArtifact[ChapterAnalysisPayload](w, analysisPath(c))
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return n, fmt.Errorf("đọc sản phẩm phân tích chương %d: %w", c, err)
		}
		if a.InputDigest != chapterInputDigest(segIdentity, promptVersion, seg, normalized, c-1) {
			break
		}
		n++
	}
	return n, nil
}

// discardAnalysesAfter xoá các sản phẩm phân tích theo từng chương có số chương > keep, để "phân tích lại một chương thì mất hiệu lực mọi phân tích sau đó" trở thành điều đúng (#4a).
// Trong lượt phân tích tiến về bình thường, sau keep vốn đã không có sản phẩm nào, nên đây là thao tác không-op idempotent; chỉ khi phân tích lại giữa chừng (vượt qua tiền tố còn tươi) mới dọn phần đuôi cũ.
// Lỗi khi xoá phải được truyền ra: đây là điểm thực thi duy nhất của bất biến này, nuốt lỗi sẽ khiến phần đuôi cũ (digest từng chương luôn khớp)
// bị tái sử dụng như tiền tố còn tươi, và bước tổng hợp sẽ tiêu thụ sự thật trộn lẫn cũ mới mà không báo lỗi nào.
func discardAnalysesAfter(w *Workspace, keep, total int) error {
	for c := keep + 1; c <= total; c++ {
		if err := os.Remove(w.path(analysisPath(c))); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("dọn sản phẩm phân tích cũ %s: %w", analysisPath(c), err)
		}
	}
	return nil
}

// loadPriorFacts đọc sự thật của các chương 1..count đã ghi xuống đĩa, phục vụ việc dựng ledger.
func loadPriorFacts(w *Workspace, count int) []ImportedChapterFacts {
	var out []ImportedChapterFacts
	for c := 1; c <= count; c++ {
		a, err := readArtifact[ChapterAnalysisPayload](w, analysisPath(c))
		if err != nil {
			break
		}
		out = append(out, a.Payload.Facts)
	}
	return out
}

func loadPriorFactsStrict(w *Workspace, count int) ([]ImportedChapterFacts, error) {
	out := make([]ImportedChapterFacts, 0, count)
	for c := 1; c <= count; c++ {
		a, err := readArtifact[ChapterAnalysisPayload](w, analysisPath(c))
		if err != nil {
			return out, fmt.Errorf("đọc sự thật phân tích chương %d: %w", c, err)
		}
		out = append(out, a.Payload.Facts)
	}
	return out, nil
}

// buildLedger suy ra ngữ cảnh liên tục gọn từ các chương đã phân tích: bí danh nhân vật + ID phục bút đang hoạt động + trạng thái gần nhất.
// Đoạn ledger này đi vào đầu vào của lô, thuộc nội dung mô hình nhìn thấy, nên nhãn và dấu phân cách đi theo ngôn ngữ tác phẩm (t).
func buildLedger(prior []ImportedChapterFacts, t *importTexts) string {
	if len(prior) == 0 {
		return ""
	}
	names := map[string]bool{}
	active := map[string]string{} // foreshadow id -> desc
	var recent []string
	for _, f := range prior {
		for _, c := range f.Characters {
			names[c] = true
		}
		for _, fu := range f.ForeshadowUpdates {
			switch fu.Action {
			case "plant", "advance":
				if fu.Description != "" {
					active[fu.ID] = fu.Description
				} else if _, ok := active[fu.ID]; !ok {
					active[fu.ID] = ""
				}
			case "resolve":
				delete(active, fu.ID)
			}
		}
	}
	if len(prior) > 0 {
		last := prior[len(prior)-1]
		for _, sc := range last.StateChanges {
			recent = append(recent, fmt.Sprintf("%s.%s=%s", sc.Entity, sc.Field, sc.NewValue))
		}
	}
	var b strings.Builder
	if len(names) > 0 {
		b.WriteString(t.ledgerKnownCharsDesc)
		b.WriteString(strings.Join(slices.Sorted(maps.Keys(names)), t.ledgerListSep))
		b.WriteString("\n")
	}
	if len(active) > 0 {
		b.WriteString(t.ledgerActiveForeshadow)
		for _, id := range slices.Sorted(maps.Keys(active)) {
			fmt.Fprintf(&b, "- %s%s%s\n", id, t.ledgerForeshadowSep, active[id])
		}
	}
	if len(recent) > 0 {
		b.WriteString(t.ledgerRecentStateDesc)
		b.WriteString(strings.Join(recent, t.ledgerItemSep))
		b.WriteString("\n")
	}
	return b.String()
}

// planBatch tính từ chương start, theo cặp ngân sách đầu vào/đầu ra, trả về điểm cuối end của lô liên tục ([start,end), chỉ số chương bắt đầu từ 0).
// Ít nhất 1 chương; ngay cả khi một chương vượt ngân sách thì nó vẫn thành một lô riêng, và bên thực thi sẽ báo thiếu dung lượng khi bị cắt ngắn (RFC §9.2).
func planBatch(chapters []ChapterSpan, start, ledgerBytes int, b AnalyzeBudget) int {
	end := start + 1
	if b.ContextBytes <= 0 || b.MaxOutputTokens <= 0 || b.PerChapterOutput <= 0 {
		return end // chưa cấu hình ngân sách: từng chương một
	}
	inAcc := ledgerBytes + b.PromptOverhead + chapterBytes(chapters, start)
	outAcc := b.PerChapterOutput
	for end < len(chapters) {
		cb := chapterBytes(chapters, end)
		if inAcc+cb > b.ContextBytes {
			break
		}
		if outAcc+b.PerChapterOutput > b.MaxOutputTokens {
			break
		}
		inAcc += cb
		outAcc += b.PerChapterOutput
		end++
	}
	return end
}

func chapterBytes(chapters []ChapterSpan, i int) int {
	return chapters[i].End - chapters[i].Start
}

// chapterInputDigest gắn danh tính sản phẩm phân tích theo từng chương: định danh lần cắt + phiên bản prompt/schema + số chương + nội dung một chương.
// Gắn theo từng chương chứ không theo cả lô — cách chia lô là chi tiết thực thi đổi theo năng lực mô hình, không nên khiến việc đổi mô hình làm mất hiệu lực toàn bộ các chương đã phân tích;
// gắn segIdentity (InputDigest của sản phẩm segmentation) để bảo đảm sau khi cắt lại, mọi phân tích đều lệch khớp một cách tự nhiên (RFC §9.1/§6.3).
func chapterInputDigest(segIdentity, promptVersion string, seg *Segmentation, normalized []byte, i int) string {
	var b strings.Builder
	b.WriteString("analyze\x00")
	b.WriteString(promptVersion)
	fmt.Fprintf(&b, "\x00v%d\x00", analysisSchemaVersion)
	b.WriteString(segIdentity)
	fmt.Fprintf(&b, "\x00ch%d\x00", seg.Chapters[i].Number)
	b.WriteString(seg.Content(normalized, i))
	return Digest([]byte(b.String()))
}

// validateBatch kiểm tra hợp lệ theo hai tầng: cấp lô là liên tục, không thiếu không trùng; cấp chương là miền giá trị và tham chiếu (RFC §9.4).
// t là bảng văn bản theo ngôn ngữ tác phẩm: lỗi ở đây được llmcontract gửi lại
// mô hình để hỏi lại, nên phải theo ngôn ngữ đang viết.
func validateBatch(r *AnalysisBatchResult, seg *Segmentation, start, end int, t *importTexts) error {
	t = importTextsOrDefault(t)
	want := end - start
	if len(r.Chapters) != want {
		return fmt.Errorf(t.errBatchChapterCount, len(r.Chapters), want)
	}
	for i, f := range r.Chapters {
		want := seg.Chapters[start+i]
		if f.Chapter != want.Number {
			return fmt.Errorf(t.errBatchChapterNo, i, f.Chapter, want.Number)
		}
		if strings.TrimSpace(f.Summary) == "" || strings.TrimSpace(f.CoreEvent) == "" {
			return fmt.Errorf(t.errChapterCoreEmpty, f.Chapter)
		}
		if !domain.ValidHookType(strings.ToLower(f.HookType)) {
			return fmt.Errorf(t.errChapterHookType, f.Chapter, f.HookType)
		}
		if !domain.ValidDominantStrand(strings.ToLower(f.DominantStrand)) {
			return fmt.Errorf(t.errChapterDominantStrand, f.Chapter, f.DominantStrand)
		}
		for j, fu := range f.ForeshadowUpdates {
			if fu.Action == "plant" && strings.TrimSpace(fu.Description) == "" {
				return fmt.Errorf(t.errChapterForeshadowDesc, f.Chapter, j)
			}
		}
		// Kiểu liệt kê được kiểm tra theo chữ thường thì cũng ghi xuống đĩa theo chữ thường: commit_chapter không kiểm tra lại kiểu liệt kê,
		// nên biến thể khác hoa/thường sẽ lọt thẳng vào trạng thái chính thức (HookHistory và các thứ khác tiêu thụ theo chuỗi chính xác,
		// biến thể bị coi là loại không xác định); vậy nên khi kiểm tra hợp lệ là chuẩn hoá ngay.
		r.Chapters[i].HookType = strings.ToLower(f.HookType)
		r.Chapters[i].DominantStrand = strings.ToLower(f.DominantStrand)
	}
	return nil
}

// AnalyzeNext dựng một lô từ bản phân tích đầu tiên còn thiếu, ghi xuống đĩa theo nguyên tử, rồi trả về số chương đã gửi trong lượt này.
// Bị cắt ngắn nghĩa là «thất bại + thu nhỏ và gom lại lô» (mặc định, §9.5); nếu lô đã thu nhỏ còn một chương mà vẫn bị cắt ngắn thì phải báo tường minh là thiếu dung lượng.
func AnalyzeNext(ctx context.Context, m callModel, systemPrompt string, w *Workspace, normalized []byte, seg *Segmentation, segIdentity, promptVersion string, budget AnalyzeBudget, prof callProfile) (int, error) {
	total := len(seg.Chapters)
	start := analyzedChapters(w, seg, normalized, segIdentity, promptVersion)
	if start >= total {
		return 0, nil
	}
	ledger := buildLedger(loadPriorFacts(w, start), importTextsOrDefault(prof.texts))
	end := planBatch(seg.Chapters, start, len(ledger), budget)
	texts := importTextsOrDefault(prof.texts)

	for {
		payload := buildAnalyzePayload(normalized, seg, ledger, start, end, texts)
		res, err := callStructured[AnalysisBatchResult](ctx, m, analysisContract(texts), systemPrompt, payload, budget.MaxOutputTokens, prof, func(r *AnalysisBatchResult) error {
			return validateBatch(r, seg, start, end, texts)
		})
		if err != nil {
			var tr *errTruncated
			if errors.As(err, &tr) {
				// Khi bị cắt ngắn, ưu tiên cứu tiền tố hợp lệ liên tục dài nhất tính từ chương đầu lô; phần đã gửi không làm lại (§9.5).
				if salvaged := salvagePrefix(tr.Raw, seg, start, texts); len(salvaged) > 0 {
					for i, f := range salvaged {
						ch := start + i + 1
						digest := chapterInputDigest(segIdentity, promptVersion, seg, normalized, start+i)
						art := ChapterAnalysisPayload{BatchStart: start + 1, BatchEnd: end, Facts: f}
						if werr := writeArtifact(w, analysisPath(ch), digest, art); werr != nil {
							return i, fmt.Errorf("ghi xuống đĩa chương cứu được %d: %w", ch, werr)
						}
					}
					w.writeFailure(FailureMeta{Stage: "analyze", Detail: fmt.Sprintf("lô %d-%d bị cắt do giới hạn độ dài", start+1, end),
						StopReason: "length", PrefixSalvage: fmt.Sprintf("available:%d", len(salvaged))}, tr.Raw)
					prof.logger().Info("imp phân tích bị cắt, cứu được phần đầu liên tiếp", "batch_start", start+1, "salvaged", len(salvaged))
					echoChapterFacts(prof, salvaged)
					return len(salvaged), nil
				}
				// Không có tiền tố nào để cứu: ghi lại là không dùng được rồi «thất bại + thu nhỏ và gom lại lô»; nếu lô một chương vẫn bị cắt ngắn thì báo thiếu dung lượng.
				w.writeFailure(FailureMeta{Stage: "analyze", Detail: fmt.Sprintf("lô %d-%d bị cắt do giới hạn độ dài, không có phần đầu để cứu", start+1, end),
					StopReason: "length", PrefixSalvage: "unavailable"}, tr.Raw)
				if end-start > 1 {
					prof.logger().Warn("imp phân tích bị cắt, thu nhỏ và gom lại lô", "batch", fmt.Sprintf("%d-%d", start+1, end), "prefix_salvage", "unavailable")
					end = start + (end-start)/2
					// Dòng tiến độ không có Key: vừa để người dùng thấy hành động thu nhỏ lô, vừa ngăn các dòng backoff của hai lần gọi
					// độc lập liên tiếp bị gộp nhầm theo cùng một Key (hợp đồng Key chỉ phủ backoff tức thời trong cùng một lần gọi).
					prof.step(0, 0, "đầu ra bị cắt do giới hạn độ dài và không có phần đầu để cứu, thu nhỏ lô còn chương %d-%d để thử lại", start+1, end)
					continue
				}
				return 0, fmt.Errorf("lô một chương %d vẫn bị cắt do giới hạn độ dài, năng lực đầu ra của mô hình không đủ", start+1)
			}
			return 0, err
		}
		for i, f := range res.Chapters {
			ch := start + i + 1
			digest := chapterInputDigest(segIdentity, promptVersion, seg, normalized, start+i)
			payloadArt := ChapterAnalysisPayload{BatchStart: start + 1, BatchEnd: end, Facts: f}
			if err := writeArtifact(w, analysisPath(ch), digest, payloadArt); err != nil {
				return i, fmt.Errorf("ghi xuống đĩa phân tích chương %d: %w", ch, err)
			}
		}
		echoChapterFacts(prof, res.Chapters)
		return end - start, nil
	}
}

// echoChapterFacts hiển thị lại trên bảng điều khiển cách model hiểu cốt lõi từng chương — người dùng phải thấy được model đã đọc hiểu gì,
// chứ không chỉ thấy một con số đếm lô máy móc (§14.1).
func echoChapterFacts(prof callProfile, facts []ImportedChapterFacts) {
	for _, f := range facts {
		prof.step(0, 0, "Chương %d (%s): %s", f.Chapter, snippet(f.Title, 24), snippet(f.CoreEvent, 60))
	}
}

// buildAnalyzePayload lắp ráp đầu vào của lô: nội dung gốc của các chương liên tục + ledger trước lô.
// payload là nội dung mô hình nhìn thấy, nên chỉ dẫn và tiêu đề đi theo ngôn ngữ tác phẩm (t).
func buildAnalyzePayload(normalized []byte, seg *Segmentation, ledger string, start, end int, t *importTexts) string {
	var b strings.Builder
	fmt.Fprintf(&b, t.analyzeInstruction, start+1, end)
	if ledger != "" {
		b.WriteString(t.analyzeLedgerHeader)
		b.WriteString(ledger)
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		c := seg.Chapters[i]
		fmt.Fprintf(&b, t.analyzeChapterHeader, c.Number, c.Title)
		b.WriteString(seg.Content(normalized, i))
		b.WriteString("\n\n---\n\n")
	}
	return b.String()
}

// salvagePrefix phân tích tiền tố hợp lệ liên tục dài nhất trong phản hồi của lô bị cắt theo độ dài (RFC §9.5).
// Chỉ giữ lại các đối tượng liên tục kể từ chương đầu lô và đã vượt qua kiểm tra hợp lệ từng chương; gặp đối tượng đầu tiên không hoàn chỉnh/không hợp lệ/bỏ số chương là dừng, các byte sau đó không được diễn giải.
// Hàm thuần, AnalyzeNext gọi ưu tiên khi bị cắt do giới hạn dung lượng, để tránh vứt bỏ các chương ở tiền tố đã sinh xong.
func salvagePrefix(raw string, seg *Segmentation, start int, t *importTexts) []ImportedChapterFacts {
	arr := extractChaptersArray(raw)
	if arr == "" {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(arr))
	if _, err := dec.Token(); err != nil { // tiêu thụ '['
		return nil
	}
	var out []ImportedChapterFacts
	for dec.More() {
		var f ImportedChapterFacts
		if err := dec.Decode(&f); err != nil {
			break // đối tượng không hoàn chỉnh đầu tiên, dừng
		}
		idx := start + len(out)
		if idx >= len(seg.Chapters) || f.Chapter != seg.Chapters[idx].Number {
			break // bỏ số chương / vượt phạm vi
		}
		one := AnalysisBatchResult{Chapters: []ImportedChapterFacts{f}}
		if err := validateBatch(&one, seg, idx, idx+1, t); err != nil {
			break
		}
		out = append(out, one.Chapters[0]) // validateBatch đã chuẩn hoá kiểu liệt kê tại chỗ, lấy giá trị đã qua kiểm tra
	}
	return out
}

// extractChaptersArray cắt phần văn bản mảng JSON sau "chapters" (phần cuối có thể đã bị cắt).
func extractChaptersArray(raw string) string {
	i := strings.Index(raw, "\"chapters\"")
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(raw[i:], '[')
	if j < 0 {
		return ""
	}
	return raw[i+j:]
}
