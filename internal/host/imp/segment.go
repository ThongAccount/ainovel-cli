package imp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// BoundaryDecision là phán quyết ranh giới của mô hình cho một owned range đơn lẻ (RFC §8.2).
type BoundaryDecision struct {
	UnitID    string `json:"unit_id"`
	Anchor    string `json:"anchor,omitempty"`
	Kind      string `json:"kind"` // chapter / group / front_matter / back_matter
	Title     string `json:"title,omitempty"`
	Uncertain bool   `json:"uncertain,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

const (
	kindChapter     = "chapter"
	kindGroup       = "group"
	kindFrontMatter = "front_matter"
	kindBackMatter  = "back_matter"
)

// boundaryBatch là kết quả có cấu trúc của một lời gọi phân đoạn.
type boundaryBatch struct {
	Boundaries []BoundaryDecision `json:"boundaries"`
}

// ChapterSpan là một chương có thể ghi sau khi xác nhận kết quả cắt: tiêu đề + phạm vi byte của văn bản đã chuẩn hoá (gồm cả dòng tiêu đề).
type ChapterSpan struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Start  int    `json:"start_byte"`
	End    int    `json:"end_byte"`
}

// MatterSpan là tiêu đề tập/phần hoặc một vùng phụ được đánh dấu rõ ràng.
type MatterSpan struct {
	Kind  string `json:"kind"`
	Title string `json:"title,omitempty"`
	Start int    `json:"start_byte"`
	End   int    `json:"end_byte"`
}

// Segmentation là kết quả cắt đã vượt qua kiểm tra bao phủ toàn văn (đầu vào của confirmation và phân tích từng chương).
type Segmentation struct {
	Chapters  []ChapterSpan `json:"chapters"`
	Matter    []MatterSpan  `json:"matter,omitempty"`    // group / front / back
	Uncertain []int         `json:"uncertain,omitempty"` // Số chương được đánh dấu uncertain, để bản xem trước gợi ý
	Notes     []string      `json:"notes,omitempty"`     // Ghi chú trong giai đoạn cắt cần người kiểm tra (ví dụ tiêu đề giữ chỗ nội dung rỗng được gộp vào đoạn trước)
}

// Content trả về nội dung đã chuẩn hoá của chương thứ i (gồm cả dòng tiêu đề).
func (s *Segmentation) Content(normalized []byte, i int) string {
	c := s.Chapters[i]
	return string(normalized[c.Start:c.End])
}

// resolveSegmentation ánh xạ các phán quyết ranh giới đã sắp xếp thành Segmentation đã qua kiểm tra bao phủ toàn văn (RFC §8.3).
// Hàm thuần: tách đầu ra của mô hình khỏi kiểm tra của mã, "dòng nào là tiêu đề chương" không do Go phán xử lại, nhưng bất biến bao phủ vẫn phải đúng.
func resolveSegmentation(normalized []byte, units []SourceUnit, decisions []BoundaryDecision) (*Segmentation, error) {
	if len(decisions) == 0 {
		return nil, fmt.Errorf("không nhận diện được ranh giới nào")
	}
	// Hợp đồng tiền điều kiện: units phải được sắp theo thứ tự số (Line,Part) (cấm sắp theo thứ tự từ điển của ID).
	for i := 1; i < len(units); i++ {
		if !unitLess(units[i-1], units[i]) {
			return nil, fmt.Errorf("SourceUnit không sắp theo thứ tự số (Line,Part): %s sau %s", units[i-1].ID, units[i].ID)
		}
	}
	unitByID := make(map[string]SourceUnit, len(units))
	for _, u := range units {
		unitByID[u.ID] = u
	}

	type point struct {
		byte int
		d    BoundaryDecision
	}
	points := make([]point, 0, len(decisions))
	for i, d := range decisions {
		switch d.Kind {
		case kindChapter, kindGroup, kindFrontMatter, kindBackMatter:
		default:
			return nil, fmt.Errorf("ranh giới[%d] kind không hợp lệ: %q", i, d.Kind)
		}
		b, err := resolveBoundaryByte(unitByID, d.UnitID, d.Anchor, importTextsVI)
		if err != nil {
			return nil, err
		}
		points = append(points, point{byte: b, d: d})
	}
	// Việc mô hình thỉnh thoảng trả về ranh giới sai thứ tự hoặc trùng lặp là vấn đề kỷ luật toạ độ, Go sửa một cách tất định thay vì phủ quyết cuối cùng — sau khi mọi khối đã thành công mà vì hai ranh giới bị đảo thứ tự mà bỏ cả giai đoạn cắt thì cái giá không thể chấp nhận (thực đo: 319 ranh giới hỏng vì đúng một chỗ đảo thứ tự trong khối,
	// hơn nữa bộ nhớ đệm khối khiến lỗi tái hiện một cách tất định). Thứ tự giữa các khối được bảo đảm bởi các khoảng owned không chồng lấn, nên chỉ có thể sai thứ tự
	// bên trong khối: sắp xếp ổn định theo byte là khôi phục lại thứ tự thật, không mất thông tin; trùng lặp cùng byte thì giữ lại cái xuất hiện trước và ghi Notes
	// để bản xem trước xác nhận cho người kiểm tra.
	sort.SliceStable(points, func(i, j int) bool { return points[i].byte < points[j].byte })
	var notes []string
	uniq := points[:0]
	for _, p := range points {
		if n := len(uniq); n > 0 && uniq[n-1].byte == p.byte {
			// Trùng lặp hoàn toàn giống nhau là dư thừa cơ học, khử trùng lặp trong im lặng; xung đột ngữ nghĩa cùng vị trí (kind/tiêu đề khác nhau) đã được hỏi lại trong lúc gọi,
			// đi tới đây thì chỉ có thể đến từ bộ nhớ đệm cũ trước khi sửa — giữ lại cái xuất hiện trước và ghi Notes để người kiểm tra.
			if prev := uniq[n-1].d; prev.Kind != p.d.Kind || boundaryLabel(prev) != boundaryLabel(p.d) {
				notes = append(notes, fmt.Sprintf("ranh giới %q và %q trùng nhau (byte %d), đã giữ lại cái trước",
					boundaryLabel(prev), boundaryLabel(p.d), p.byte))
			}
			continue
		}
		uniq = append(uniq, p)
	}
	points = uniq
	// Văn bản không rỗng trước ranh giới đầu tiên (phần giới thiệu đầu sách/quảng cáo v.v., mô hình bỏ sót ranh giới khởi đầu) không bị phủ quyết cuối cùng: Go bổ sung
	// một front_matter một cách tất định để ôm lấy [0, first), ghi Notes để bản xem trước xác nhận cho người kiểm tra — báo thiếu đã nằm trong bộ nhớ đệm khối,
	// phủ quyết cuối cùng sẽ khiến lần chạy lại không tốn lời gọi nào mà tái hiện đúng lỗi cũ (cùng triết lý với việc hấp thụ chương nội dung rỗng, RFC §8.3.5).
	// Bản thân phán đoán ngữ nghĩa đã được trả về mô hình ngay lúc gọi (chunkValidator.coverStart hỏi lại), bước dự phòng này chỉ chữa bộ nhớ đệm cũ.
	if head := points[0].byte; head != 0 && strings.TrimSpace(string(normalized[:head])) != "" {
		notes = append(notes, fmt.Sprintf("văn bản đầu %d byte không được mô hình gán (%s…), đã gom vào front_matter, hãy kiểm tra xem có chương nào bị bỏ sót không",
			head, snippet(string(normalized[:min(head, 48)]), 24)))
		points = append([]point{{byte: 0, d: BoundaryDecision{UnitID: units[0].ID, Kind: kindFrontMatter}}}, points...)
	}

	seg := &Segmentation{Notes: notes}
	chapterNo := 0
	// absorb gộp một đoạn vào span vừa tạo gần nhất (chương hay vùng phụ đều được), trả về false khi không có gì để gộp vào.
	absorb := func(end int) bool {
		ci, mi := len(seg.Chapters)-1, len(seg.Matter)-1
		switch {
		case ci >= 0 && (mi < 0 || seg.Chapters[ci].Start > seg.Matter[mi].Start):
			seg.Chapters[ci].End = end
		case mi >= 0:
			seg.Matter[mi].End = end
		default:
			return false
		}
		return true
	}
	for i, p := range points {
		start := p.byte
		if i == 0 {
			start = 0 // Đoạn đầu hấp thụ khoảng trắng ở đầu văn bản
		}
		end := len(normalized)
		if i+1 < len(points) {
			end = points[i+1].byte
		}
		title := strings.TrimSpace(p.d.Title)
		if title == "" {
			title = firstLine(normalized, p.byte, end)
		}
		switch p.d.Kind {
		case kindChapter:
			if strings.TrimSpace(bodyAfterTitle(normalized, p.byte, end)) == "" {
				// Nguồn tiểu thuyết mạng thật hay gặp chỗ giữ chỗ "已锁定/付费章节": có tiêu đề nhưng thiếu nội dung. Không làm hỏng toàn bộ —
				// phủ quyết một lần ở cuối sẽ lãng phí toàn bộ lời gọi mô hình của giai đoạn cắt; dòng tiêu đề được gộp vào đoạn trước (không mất một chữ nào của văn bản),
				// ghi vào Notes để bản xem trước xác nhận hiển thị, nếu người không đồng ý thì dùng --guide để phán quyết (điểm dừng của RFC §8.4 tồn tại chính là vì việc này).
				seg.Notes = append(seg.Notes,
					fmt.Sprintf("tiêu đề chương %q không có nội dung (byte %d..%d), đã gộp vào đoạn trước (thường gặp ở chương đã khóa hoặc trả phí)", title, start, end))
				if !absorb(end) {
					seg.Matter = append(seg.Matter, MatterSpan{Kind: kindFrontMatter, Title: title, Start: start, End: end})
				}
				continue
			}
			chapterNo++
			seg.Chapters = append(seg.Chapters, ChapterSpan{Number: chapterNo, Title: title, Start: start, End: end})
			if p.d.Uncertain {
				seg.Uncertain = append(seg.Uncertain, chapterNo)
			}
		default:
			seg.Matter = append(seg.Matter, MatterSpan{Kind: p.d.Kind, Title: title, Start: start, End: end})
		}
	}
	if chapterNo == 0 {
		return nil, fmt.Errorf("cắt không tạo ra chương nào (group không tính là chương)")
	}
	// Hai chương trùng tên là dấu hiệu tất định của việc "một chương bị cắt nhầm" (trong nguồn có quy ước tiêu đề thì tên chương không nên lặp lại), chỉ ghi Notes
	// để bản xem trước xác nhận cho người kiểm tra (Notes khác rỗng là chặn --yes) — có gộp hay không không do Go phán quyết.
	titleAt := make(map[string]int, len(seg.Chapters))
	for _, c := range seg.Chapters {
		key := squashSpace(c.Title)
		if first, ok := titleAt[key]; ok && key != "" {
			seg.Notes = append(seg.Notes, fmt.Sprintf("chương %d và chương %d có cùng tiêu đề (%q), nghi là cùng một chương bị cắt nhầm, hãy kiểm tra",
				c.Number, first, snippet(c.Title, 24)))
		} else {
			titleAt[key] = c.Number
		}
	}
	return seg, nil
}

// squashSpace bỏ hết khoảng trắng, dùng cho kiểm tra hồi tiếng tiêu đề và so sánh trùng tên — khác biệt về khoảng trắng/trang trí không phải khác biệt ngữ nghĩa.
func squashSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// firstLine trả về dòng đầu tiên trong [start,end) sau khi cắt khoảng trắng.
func firstLine(normalized []byte, start, end int) string {
	s := string(normalized[start:end])
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// bodyAfterTitle trả về phần nội dung của [start,end) sau khi bỏ dòng đầu (tiêu đề).
// Tiêu đề chương nhiều dòng chiếm riêng dòng đầu, nội dung nằm sau đó; đoạn một dòng không có ngắt dòng (kịch bản cắt theo điểm neo) thì cả đoạn là nội dung,
// lúc này trả về toàn bộ đoạn chứ không phải chuỗi rỗng — nếu không, một tiểu thuyết hợp lệ dạng một dòng/một dòng nhiều chương sẽ bị từ chối nhầm với lý do "nội dung truyện rỗng" (RFC §8.3).
func bodyAfterTitle(normalized []byte, start, end int) string {
	s := string(normalized[start:end])
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// planChunks chia units theo ngân sách byte thành các khoảng chỉ mục owned không chồng lấn và phủ kín toàn bộ [start,end).
// Kích thước khối tính từ ngân sách ngữ cảnh, không theo số dòng hay số chương cố định (RFC §8.1).
func planChunks(units []SourceUnit, budgetBytes int) [][2]int {
	if len(units) == 0 {
		return nil
	}
	if budgetBytes <= 0 {
		return [][2]int{{0, len(units)}}
	}
	var chunks [][2]int
	start := 0
	acc := 0
	for i, u := range units {
		size := u.EndByte - u.StartByte
		if acc > 0 && acc+size > budgetBytes {
			chunks = append(chunks, [2]int{start, i})
			start = i
			acc = 0
		}
		acc += size
	}
	chunks = append(chunks, [2]int{start, len(units)})
	return chunks
}

// buildProjection dựng payload phóng chiếu có cấu trúc cho một khoảng owned (kèm ít ngữ cảnh), mô hình chỉ trả về ranh giới cho phần owned.
// Đồng thời trả về tập hợp mọi unit_id trong phóng chiếu (owned + vùng ngữ cảnh), để kiểm tra đầu ra phân biệt ảo giác với vượt phạm vi.
func buildProjection(units []SourceUnit, owned [2]int, contextMargin, ctxBudget int, guidance string) (string, map[string]bool) {
	// Vùng ngữ cảnh co lại theo cả giới hạn số đơn vị lẫn số byte (khi ctxBudget<=0 thì chỉ theo số đơn vị): các đơn vị margin thường là dòng thường,
	// nhưng lát ảo của dòng quá dài có thể dài tới MaxUnitBytes, chỉ vài lát đã nuốt hết ngân sách đầu vào — ngữ cảnh chỉ là tham chiếu, không đáng cái giá đó.
	lo, budget := owned[0], ctxBudget
	for lo > 0 && owned[0]-lo < contextMargin {
		if n := len(units[lo-1].Text); ctxBudget > 0 {
			if n > budget {
				break
			}
			budget -= n
		}
		lo--
	}
	hi, budget := owned[1], ctxBudget
	for hi < len(units) && hi-owned[1] < contextMargin {
		if n := len(units[hi].Text); ctxBudget > 0 {
			if n > budget {
				break
			}
			budget -= n
		}
		hi++
	}
	type projUnit struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	proj := struct {
		OwnedStart   string     `json:"owned_start"`
		OwnedEnd     string     `json:"owned_end"`
		Units        []projUnit `json:"units"`
		UserGuidance string     `json:"user_guidance,omitempty"`
	}{
		OwnedStart:   units[owned[0]].ID,
		OwnedEnd:     units[owned[1]-1].ID,
		UserGuidance: guidance,
	}
	ids := make(map[string]bool, hi-lo)
	for i := lo; i < hi; i++ {
		proj.Units = append(proj.Units, projUnit{ID: units[i].ID, Text: units[i].Text})
		ids[units[i].ID] = true
	}
	data, _ := json.MarshalIndent(proj, "", "  ")
	return string(data), ids
}

// segmentInputDigest bao phủ các đầu vào ngữ nghĩa mà hành động phân đoạn thực sự tiêu thụ: nguồn đã chuẩn hoá, hướng dẫn người dùng, phiên bản prompt (RFC §6.3).
func segmentInputDigest(normalizedDigest, guidance, promptVersion string) string {
	return Digest([]byte(strings.Join([]string{"segment", promptVersion, normalizedDigest, guidance}, "\x00")))
}

// segmentChunkPath / segmentChunkDigest: đường dẫn và danh tính của sản phẩm cho bộ nhớ đệm ranh giới cấp khối.
// Danh tính gắn với danh tính của lần cắt (nguồn + hướng dẫn + phiên bản prompt) và vùng đơn vị owned của khối — bất kỳ thay đổi nào ở phía trên đều làm bộ nhớ đệm tự nhiên lệch khớp.
func segmentChunkPath(owned [2]int) string {
	return fmt.Sprintf("%s/chunk-%06d-%06d.json", dirSegmentChunks, owned[0], owned[1])
}

func segmentChunkDigest(identity, loID, hiID string) string {
	return Digest([]byte(strings.Join([]string{"segment-chunk", identity, loID, hiID}, "\x00")))
}

// Segment thực hiện cắt ngữ nghĩa trên toàn bộ văn bản đã chuẩn hoá: gọi mô hình theo từng khoảng owned để nhận diện ranh giới, rồi kiểm tra bao phủ toàn văn.
// contextMargin là số đơn vị ngữ cảnh, chunkBytes là ngân sách byte của khoảng owned, maxTokens là ngân sách đầu ra cho một lần.
// Khi w khác nil, ghi bộ nhớ đệm ranh giới xuống đĩa theo từng khối (identity = segmentInputDigest): một khối có thể mất vài phút, bất kỳ khối nào thất bại
// cũng không nên phải trả lại các lời gọi của những khối đã xong — cùng triết lý với analyze theo từng chương và synthesize theo từng đoạn, trước đây cắt là
// giai đoạn đắt giá duy nhất không có ghi bền trong giai đoạn, hỏng một chỗ là phải làm lại tất cả.
func Segment(ctx context.Context, m callModel, systemPrompt string, normalized []byte, units []SourceUnit, guidance string, chunkBytes, contextMargin, maxTokens int, prof callProfile, w *Workspace, identity string) (*Segmentation, error) {
	chunks := planChunks(units, planningBudget(chunkBytes, systemPrompt, guidance))
	unitByID := make(map[string]SourceUnit, len(units))
	for _, u := range units {
		unitByID[u.ID] = u
	}
	var decisions []BoundaryDecision
	// chunk xử lý một khoảng owned: trúng bộ nhớ đệm thì không tốn lời gọi nào; khi đầu ra bị cắt ngắn theo độ dài và khoảng còn chia được thì thu nhỏ khối một nửa
	// rồi thử lại đệ quy (JSON ranh giới của nhiều chương ngắn sẽ vượt quá đầu ra nhìn thấy được, cùng triết lý với analyze khi thu nhỏ lô) — nửa khối có
	// đường dẫn bộ nhớ đệm riêng nên thành quả thử lại không phải trả lại; chỉ khi thu nhỏ tới mức đơn vị mà vẫn bị cắt ngắn thì mới thật sự thiếu dung lượng.
	var chunk func(owned [2]int, cur, total int) ([]BoundaryDecision, error)
	chunk = func(owned [2]int, cur, total int) ([]BoundaryDecision, error) {
		lo, hi := units[owned[0]], units[owned[1]-1]
		rel, want := segmentChunkPath(owned), segmentChunkDigest(identity, lo.ID, hi.ID)
		if w != nil {
			if art, err := readArtifact[boundaryBatch](w, rel); err == nil && art.InputDigest == want {
				return art.Payload.Boundaries, nil
			}
		}
		// Một lời gọi mô hình cho một khối có thể mất vài phút, phản chiếu tiến trình theo từng khối kèm số ranh giới tích luỹ thì bảng điều khiển mới không im lặng trông như bị treo.
		prof.step(cur, total, "cắt khối %d/%d (%s..%s), đã nhận diện %d ranh giới...",
			cur, total, lo.ID, hi.ID, len(decisions))
		// Giới hạn byte của vùng ngữ cảnh lấy chunkBytes/8 nhưng không dưới 4096: cần chặn là các lát ảo của dòng quá dài
		// (một lát có thể dài tới MaxUnitBytes) nuốt hết ngân sách đầu vào, còn chi phí margin của các dòng thường thì vốn vô hại.
		payload, projIDs := buildProjection(units, owned, contextMargin, max(chunkBytes/8, 4096), guidance)
		ownedIDs := make(map[string]bool, owned[1]-owned[0])
		for i := owned[0]; i < owned[1]; i++ {
			ownedIDs[units[i].ID] = true
		}
		v := chunkValidator{projIDs: projIDs, ownedIDs: ownedIDs, unitByID: unitByID,
			normalized: normalized, coverStart: owned[0] == 0, texts: importTextsOrDefault(prof.texts)}
		batch, err := callStructured[boundaryBatch](ctx, m, segmentContract(importTextsOrDefault(prof.texts)), systemPrompt, payload, maxTokens, prof, func(b *boundaryBatch) error {
			return v.validate(b.Boundaries)
		})
		if err != nil {
			var tr *errTruncated
			if errors.As(err, &tr) && owned[1]-owned[0] > 1 {
				mid := (owned[0] + owned[1]) / 2
				prof.step(0, 0, "khối %s..%s bị cắt khi xuất ranh giới (chương quá dày), thử lại với khối chia đôi", lo.ID, hi.ID)
				prof.logger().Warn("imp: kết quả cắt bị cắt ngắn, chia đôi khối", "chunk", lo.ID+".."+hi.ID)
				left, lerr := chunk([2]int{owned[0], mid}, cur, total)
				if lerr != nil {
					return nil, lerr
				}
				right, rerr := chunk([2]int{mid, owned[1]}, cur, total)
				if rerr != nil {
					return nil, rerr
				}
				return append(left, right...), nil
			}
			return nil, fmt.Errorf("đoạn cắt %s..%s: %w", lo.ID, hi.ID, err)
		}
		// Ranh giới nằm trong vùng ngữ cảnh thuộc quyền của khối lân cận (nó sẽ báo lại trong khoảng owned của chính nó), Go cắt thẳng:
		// kỷ luật toạ độ do mã thực thi, còn lần thử lại ngữ nghĩa chỉ dành cho lỗi ngữ nghĩa thật sự — hành vi cũ hỏi lại khi có phản hồi vượt phạm vi,
		// mô hình yếu thường dùng hết cả 3 lần thử rồi làm sập luôn cả khối (RFC §8.1「mô hình lo ngữ nghĩa, Go lo toạ độ」).
		kept := make([]BoundaryDecision, 0, len(batch.Boundaries))
		for _, bd := range batch.Boundaries {
			if ownedIDs[bd.UnitID] {
				kept = append(kept, bd)
			}
		}
		if n := len(batch.Boundaries) - len(kept); n > 0 {
			// Đây là kỷ luật toạ độ thường lệ chứ không phải sự cố, dùng tiến trình bình thường để phản chiếu — màu cảnh báo sẽ khiến người dùng tưởng đã lỗi.
			prof.step(0, 0, "đã cắt bỏ %d ranh giới báo thừa trong vùng ngữ cảnh (khối lân cận sẽ tự báo, không phải lỗi)", n)
		}
		// Phản chiếu phán đoán ngữ nghĩa của mô hình (các tiêu đề đã nhận diện), để người dùng thấy được mô hình đã hiểu gì, chứ không chỉ có đếm số máy móc.
		if len(kept) > 0 {
			prof.step(0, 0, "mô hình nhận diện: %s", previewBoundaries(kept))
		}
		if w != nil {
			if err := writeArtifact(w, rel, want, boundaryBatch{Boundaries: kept}); err != nil {
				return nil, fmt.Errorf("ghi khối cắt %s..%s xuống đĩa: %w", lo.ID, hi.ID, err)
			}
		}
		return kept, nil
	}
	for ci, owned := range chunks {
		kept, err := chunk(owned, ci+1, len(chunks))
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, kept...)
	}
	seg, err := resolveSegmentation(normalized, units, decisions)
	if err != nil {
		// Khi tích hợp cuối cùng thất bại thì bộ nhớ đệm khối đã mất giá trị: digest luôn khớp sẽ khiến lần chạy lại không tốn lời gọi nào mà đọc lại đúng lô ranh giới cũ,
		// tái hiện một cách tất định cùng lỗi đó. Xoá bộ nhớ đệm để đổi lấy cơ hội gọi mô hình cắt lại ở lần sau; ảnh chụp quyết định đi qua errSemantic
		// và được ghi thống nhất vào failures/ để truy vết sau này. Thất bại khi xoá phải được báo cáo trung thực — nói dối là đã xoá sẽ khiến người dùng chạy lại
		// và lại đọc bộ nhớ đệm hỏng (Debug-First).
		hint := "đã xoá bộ nhớ đệm khối, chạy lại sẽ cắt lại"
		if w != nil {
			if cerr := w.clearDir(dirSegmentChunks); cerr != nil {
				hint = fmt.Sprintf("xoá bộ nhớ đệm khối thất bại: %v, hãy xoá thủ công meta/import/segment-chunks/ trước khi chạy lại", cerr)
			}
		}
		raw, _ := json.MarshalIndent(decisions, "", "  ")
		return nil, &errSemantic{Raw: string(raw), Err: fmt.Errorf("tích hợp toàn bộ kết quả cắt thất bại (%s): %w", hint, err)}
	}
	return seg, nil
}

// planningBudget trừ các chi phí cấu trúc của yêu cầu khỏi ngân sách đầu vào: system prompt và hướng dẫn bị trừ theo độ dài thực tế, phần còn lại quy đổi
// theo 3/4 để bù phần bùng nổ của vỏ bọc JSON phóng chiếu (id/dấu nháy/ký tự thoát ≈ 1/3 phần nội dung) — nội dung owned chỉ là một phần của yêu cầu,
// tính đúng trần sẽ vượt ngân sách đầu vào thật khi prompt dài hoặc vùng ngữ cảnh lớn. Sàn chunkBytes/4 chống prompt quá dài làm ngân sách
// bị ép xuống số âm; chunkBytes<=0 nghĩa là không có ngân sách (một khối), truyền nguyên vẹn.
func planningBudget(chunkBytes int, systemPrompt, guidance string) int {
	if chunkBytes <= 0 {
		return chunkBytes
	}
	b := (chunkBytes - len(systemPrompt) - len(guidance)) * 3 / 4
	return max(b, chunkBytes/4)
}

// boundaryLabel tạo cho mỗi phán quyết ranh giới một nhãn đọc được: ưu tiên tiêu đề, không có tiêu đề thì lùi về kind@unit_id.
func boundaryLabel(d BoundaryDecision) string {
	if t := strings.TrimSpace(d.Title); t != "" {
		return t
	}
	return d.Kind + "@" + d.UnitID
}

// previewBoundaries nén một lô phán quyết ranh giới thành một dòng xem trước tiêu đề (tối đa 3 + số đếm), để bảng điều khiển phản chiếu.
func previewBoundaries(bs []BoundaryDecision) string {
	titles := make([]string, 0, 3)
	for _, b := range bs {
		titles = append(titles, snippet(boundaryLabel(b), 24))
		if len(titles) == 3 {
			break
		}
	}
	s := strings.Join(titles, " / ")
	if len(bs) > len(titles) {
		s += fmt.Sprintf(" (tổng %d)", len(bs))
	}
	return s
}

// chunkValidator mang bối cảnh kiểm tra lúc gọi cho một lời gọi cắt: unit_id nằm ngoài phóng chiếu là ảo giác; ranh giới trong vùng owned
// còn phải có kind hợp lệ, anchor phân giải được, không xung đột ngữ nghĩa cùng vị trí; khối đầu phải có ranh giới ôm lấy điểm khởi đầu của văn bản.
// Nếu không chặn lúc gọi, những giá trị hỏng này sẽ đi kèm khối và nằm trong bộ nhớ đệm — digest luôn khớp, lần chạy lại không tốn lời gọi nào
// mà đọc lại đúng dữ liệu hỏng, lỗi tái hiện một cách tất định (RFC §8.3). Phán đoán ngữ nghĩa (giữ cái nào, mở đầu là gì) được trả về mô hình qua lần hỏi lại,
// Go không thay mô hình trả lời; ranh giới trong vùng ngữ cảnh đã định bị cắt bởi kỷ luật toạ độ nên không hỏi lại vì chúng.
type chunkValidator struct {
	projIDs, ownedIDs map[string]bool
	unitByID          map[string]SourceUnit
	normalized        []byte
	coverStart        bool // Khối đầu: văn bản không rỗng trước điểm khởi đầu phải có ranh giới nào đó ôm lấy
	// texts là bảng văn bản theo ngôn ngữ tác phẩm: lỗi validate ở đây được
	// llmcontract gửi lại mô hình để hỏi lại, nên phải theo ngôn ngữ đang viết.
	texts *importTexts
}

func (v chunkValidator) validate(bs []BoundaryDecision) error {
	t := importTextsOrDefault(v.texts)
	seen := make(map[int]BoundaryDecision)
	first := -1
	for _, b := range bs {
		if b.UnitID == "" {
			return fmt.Errorf("%s", t.errBoundaryMissingUnit)
		}
		if !v.projIDs[b.UnitID] {
			return fmt.Errorf(t.errBoundaryUnknownUnit, b.UnitID)
		}
		if !v.ownedIDs[b.UnitID] {
			continue
		}
		switch b.Kind {
		case kindChapter, kindGroup, kindFrontMatter, kindBackMatter:
		default:
			return fmt.Errorf(t.errBoundaryBadKind, b.UnitID, b.Kind)
		}
		at, err := resolveBoundaryByte(v.unitByID, b.UnitID, b.Anchor, t)
		if err != nil {
			return err
		}
		// Hồi tiếng tiêu đề: tiêu đề của chapter/group phải thực sự tồn tại trong văn bản gốc của đơn vị ranh giới (bỏ qua khác biệt khoảng trắng) —
		// tiêu đề bịa ra bị chặn ngay tại đây bằng sự thật (thực đo: trong một nguồn có 157 chương thì 67 chương là mô hình tự chế ranh giới trên phần tiếp nội dung giữa chương
		// và bịa tiêu đề). Quyền cân nhắc ngữ nghĩa vẫn thuộc về mô hình: nguồn thật sự không có quy ước tiêu đề có thể đặt uncertain để giữ tiêu đề đã tổng hợp;
		// tiêu đề mô tả của front/back matter rủi ro thấp nên không đối chiếu.
		if (b.Kind == kindChapter || b.Kind == kindGroup) && !b.Uncertain {
			if title := squashSpace(b.Title); title != "" && !strings.Contains(squashSpace(v.unitByID[b.UnitID].Text), title) {
				return fmt.Errorf(t.errBoundaryTitleMissing,
					b.UnitID, snippet(b.Title, 24))
			}
		}
		// Xung đột cùng vị trí (kind/tiêu đề khác nhau) là vấn đề ngữ nghĩa, giữ lại cái nào không do Go phán quyết; trùng lặp hoàn toàn giống nhau
		// là dư thừa cơ học, sau khi cho qua sẽ bị resolve khử trùng lặp trong im lặng.
		if prev, ok := seen[at]; ok {
			if prev.Kind != b.Kind || boundaryLabel(prev) != boundaryLabel(b) {
				return fmt.Errorf(t.errBoundaryConflict,
					boundaryLabel(prev), boundaryLabel(b), b.UnitID)
			}
		} else {
			seen[at] = b
		}
		if first < 0 || at < first {
			first = at
		}
	}
	if v.coverStart {
		head := first
		if head < 0 {
			head = len(v.normalized) // Khối đầu không báo ranh giới owned nào: toàn bộ văn bản đầu chưa được quy về
		}
		if head > 0 && strings.TrimSpace(string(v.normalized[:head])) != "" {
			return fmt.Errorf(t.errBoundaryStartUnowned,
				head, snippet(string(v.normalized[:min(head, 48)]), 24))
		}
	}
	return nil
}
