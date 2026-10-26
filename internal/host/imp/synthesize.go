package imp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// Tập đóng các trạng thái câu chuyện (RFC §10.4).
const (
	storyOpen      = "open"
	storyClosed    = "closed"
	storyUncertain = "uncertain"
)

// synthesisSchemaVersion được đưa vào InputDigest của RangeDigest / synthesis; tăng lên khi nâng
// hợp đồng tổng hợp để vô hiệu hoá các sản phẩm đã ghi xuống đĩa.
// synthesizePromptVersion được đưa vào InputDigest của synthesis; tăng lên khi đổi prompt tổng
// hợp, nếu không synthesis cũ vẫn bị nhận nhầm là còn hiệu lực.
const (
	synthesisSchemaVersion  = 3
	synthesizePromptVersion = "synthesize-v3"
	rangePromptVersion      = "range-v2" // được đưa vào rangeInputDigest; tăng lên khi đổi prompt Range, nếu không tóm tắt đoạn cũ vẫn bị nhận nhầm là còn hiệu lực
)

// ImportedArcRange / ImportedVolumeRange: tổng hợp chỉ trả về phạm vi tập–cung, không lặp lại toàn bộ các chương (RFC §10.3).
type ImportedArcRange struct {
	Title        string `json:"title"`
	Goal         string `json:"goal"`
	StartChapter int    `json:"start_chapter"`
	EndChapter   int    `json:"end_chapter"`
}

type ImportedVolumeRange struct {
	Title string             `json:"title"`
	Theme string             `json:"theme"`
	Arcs  []ImportedArcRange `json:"arcs"`
}

// BookSynthesis là kết quả tổng hợp cuối cùng: sự thật toàn cục + phạm vi tập–cung (RFC §10.3).
type BookSynthesis struct {
	Title        *string               `json:"title"`
	Synopsis     string                `json:"synopsis"`
	Premise      string                `json:"premise"`
	Characters   []domain.Character    `json:"characters"`
	WorldRules   []domain.WorldRule    `json:"world_rules"`
	Structure    []ImportedVolumeRange `json:"structure"`
	Compass      domain.StoryCompass   `json:"compass"`
	PlanningTier domain.PlanningTier   `json:"planning_tier"`
	StoryStatus  string                `json:"story_status"`
	StatusReason string                `json:"status_reason,omitempty"`
}

// RangeDigest là tóm tắt đoạn liên tục ở giai đoạn Map của sách dài; đầu ra bị ràng buộc trong một đoạn duy nhất (RFC §10.2).
type RangeDigest struct {
	StartChapter    int      `json:"start_chapter"`
	EndChapter      int      `json:"end_chapter"`
	Plot            string   `json:"plot"`
	Characters      []string `json:"characters,omitempty"`
	WorldFacts      []string `json:"world_facts,omitempty"`
	OpenedThreads   []string `json:"opened_threads,omitempty"`
	ResolvedThreads []string `json:"resolved_threads,omitempty"`
}

var validPlanningTiers = map[domain.PlanningTier]bool{
	domain.PlanningTierShort: true,
	domain.PlanningTierMid:   true,
	domain.PlanningTierLong:  true,
}

// planFactRanges chia sự thật từng chương thành các đoạn liên tục theo ngân sách byte; sách ngắn mà chứa được trong một lần thì tổng hợp thẳng trong một đoạn (RFC §10.2).
func planFactRanges(facts []ImportedChapterFacts, budgetBytes int) [][2]int {
	if len(facts) == 0 {
		return nil
	}
	if budgetBytes <= 0 {
		return [][2]int{{0, len(facts)}}
	}
	var ranges [][2]int
	start, acc := 0, 0
	for i, f := range facts {
		size := len(compactFact(f))
		if i > start && acc+size > budgetBytes {
			ranges = append(ranges, [2]int{start, i})
			start, acc = i, 0
		}
		acc += size
	}
	ranges = append(ranges, [2]int{start, len(facts)})
	return ranges
}

// compactView là bản nhìn gọn đưa vào bước tổng hợp: giữ các trường cần cho việc khái quát xuyên
// chương, không chứa toàn văn.
// character/world evidence là những quan sát được trích riêng cho tổng hợp toàn tác phẩm trong
// lúc suy ngược từng chương, bắt buộc phải mang theo—nếu không, bộ tổng hợp chỉ có thể bịa nhân
// vật chính thức và quy tắc thế giới từ phần tóm tắt, lãng phí vô ích những bằng chứng đã trích
// xuất (RFC §9.1/§10).
type compactView struct {
	Chapter           int                     `json:"chapter"`
	Title             string                  `json:"title"`
	CoreEvent         string                  `json:"core_event"`
	Summary           string                  `json:"summary"`
	Characters        []string                `json:"characters,omitempty"`
	CharacterEvidence []ImportedCharacterFact `json:"character_evidence,omitempty"`
	WorldEvidence     []ImportedWorldFact     `json:"world_evidence,omitempty"`
}

func toCompact(f ImportedChapterFacts) compactView {
	return compactView{
		Chapter:           f.Chapter,
		Title:             f.Title,
		CoreEvent:         f.CoreEvent,
		Summary:           f.Summary,
		Characters:        f.Characters,
		CharacterEvidence: f.CharacterEvidence,
		WorldEvidence:     f.WorldEvidence,
	}
}

func compactFact(f ImportedChapterFacts) string {
	data, _ := json.Marshal(toCompact(f))
	return string(data)
}

func compactFacts(facts []ImportedChapterFacts) string {
	views := make([]compactView, len(facts))
	for i, f := range facts {
		views[i] = toCompact(f)
	}
	data, _ := json.Marshal(views)
	return string(data)
}

// Synthesize tổng hợp theo tầng: sách ngắn cho ra BookSynthesis trực tiếp; sách dài cho ra
// RangeDigest trước rồi mới gộp (RFC §10).
// bookPrompt mô tả hợp đồng BookSynthesis, rangePrompt mô tả hợp đồng RangeDigest—hai giai đoạn
// có cấu trúc đầu ra khác nhau, phải dùng đúng system prompt tương ứng, nếu không mô hình nhận
// chỉ dẫn BookSynthesis lại bị yêu cầu tạo RangeDigest, chỉ dẫn tự mâu thuẫn.
func Synthesize(ctx context.Context, m callModel, bookPrompt, rangePrompt string, w *Workspace, facts []ImportedChapterFacts, budgetBytes, maxTokens int, prof callProfile) (*BookSynthesis, error) {
	t := importTextsOrDefault(prof.texts)
	ranges := planFactRanges(facts, budgetBytes)
	if len(ranges) <= 1 {
		return synthesizeBook(ctx, m, bookPrompt, compactFacts(facts), len(facts), maxTokens, prof)
	}
	digests := make([]RangeDigest, 0, len(ranges))
	for ri, r := range ranges {
		rangeFacts := facts[r[0]:r[1]]
		startCh, endCh := rangeFacts[0].Chapter, rangeFacts[len(rangeFacts)-1].Chapter
		want := rangeInputDigest(rangeFacts)
		rel := rangeDigestPath(startCh, endCh)
		// Tóm tắt đoạn đã ghi xuống đĩa mà khớp InputDigest được tái sử dụng thẳng, để khi một đoạn của sách dài làm hỏng thì không phải trả tiền lại (RFC §6/§10.2).
		if art, err := readArtifact[RangeDigest](w, rel); err == nil && art.InputDigest == want {
			digests = append(digests, art.Payload)
			continue
		}
		prof.step(ri+1, len(ranges), "tóm tắt đoạn %d/%d (chương %d-%d)...", ri+1, len(ranges), startCh, endCh)
		rd, err := callStructured[RangeDigest](ctx, m, rangeContract(t), rangePrompt, buildRangePayload(rangeFacts, t), maxTokens, prof, func(d *RangeDigest) error {
			return validateRangeDigest(d, startCh, endCh, "range digest", t)
		})
		if err != nil {
			return nil, fmt.Errorf("tổng hợp range %d-%d: %w", startCh, endCh, err)
		}
		if err := writeArtifact(w, rel, want, rd); err != nil {
			return nil, fmt.Errorf("ghi range digest xuống đĩa: %w", err)
		}
		digests = append(digests, rd)
	}
	// Reduce đệ quy: tổng khối lượng tóm tắt đoạn vẫn có thể vượt ngân sách đầu vào của bước tổng
	// hợp cuối cùng (đẩy #83 từ "toàn bộ chương" sang "toàn bộ tóm tắt đoạn").
	// Gộp từng tầng cho tới khi vừa chứa thì mới thực sự mở rộng không giới hạn (RFC §10.2).
	digests, err := reduceToFit(ctx, m, rangePrompt, digests, budgetBytes, maxTokens, prof)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(digests)
	return synthesizeBook(ctx, m, bookPrompt, string(data), len(facts), maxTokens, prof)
}

// reduceToFit liên tục gộp các tóm tắt đoạn liên tục theo nhóm ứng với ngân sách, cho tới khi chuỗi
// đã tuần tự hoá vừa chứa trong ngân sách đầu vào của BookSynthesis cuối cùng.
// Mỗi vòng giảm chặt số lượng tóm tắt nên chắc chắn hội tụ; một tóm tắt đơn lẻ dù vượt ngân sách
// cũng không tách thêm (tầng dưới đã là đơn vị ngữ nghĩa nhỏ nhất), giao cho lượt gọi cuối; nếu vì
// thế bị cắt ngắn thì callStructured báo lỗi tường minh thay vì tràn ngầm.
func reduceToFit(ctx context.Context, m callModel, rangePrompt string, digests []RangeDigest, budgetBytes, maxTokens int, prof callProfile) ([]RangeDigest, error) {
	t := importTextsOrDefault(prof.texts)
	round := 0
	for len(digests) > 1 {
		if budgetBytes <= 0 {
			return digests, nil
		}
		data, _ := json.Marshal(digests)
		if len(data) <= budgetBytes {
			return digests, nil
		}
		groups := groupDigestsByBudget(digests, budgetBytes)
		if len(groups) >= len(digests) {
			return digests, nil // không thể gộp thêm nữa (mỗi nhóm chỉ có một tóm tắt)
		}
		round++
		merged := make([]RangeDigest, 0, len(groups))
		for gi, g := range groups {
			startCh, endCh := g[0].StartChapter, g[len(g)-1].EndChapter
			prof.step(gi+1, len(groups), "gộp tóm tắt đoạn (vòng %d, %d/%d, chương %d-%d)...",
				round, gi+1, len(groups), startCh, endCh)
			rd, err := callStructured[RangeDigest](ctx, m, rangeContract(t), rangePrompt, buildDigestReducePayload(g, t), maxTokens, prof, func(d *RangeDigest) error {
				return validateRangeDigest(d, startCh, endCh, t.errRangeLabelMerged, t)
			})
			if err != nil {
				return nil, fmt.Errorf("gộp đoạn %d-%d: %w", startCh, endCh, err)
			}
			merged = append(merged, rd)
		}
		digests = merged
	}
	return digests, nil
}

// validateRangeDigest là hàm kiểm tra nghiệp vụ mà llmcontract gửi lại mô hình để
// hỏi lại, nên thông báo lỗi phải theo ngôn ngữ tác phẩm (t).
func validateRangeDigest(d *RangeDigest, startChapter, endChapter int, label string, t *importTexts) error {
	t = importTextsOrDefault(t)
	if strings.TrimSpace(d.Plot) == "" {
		return fmt.Errorf(t.errRangePlotEmpty, label)
	}
	if d.StartChapter != startChapter || d.EndChapter != endChapter {
		return fmt.Errorf(t.errRangeMismatch, label, d.StartChapter, d.EndChapter, startChapter, endChapter)
	}
	return nil
}

// groupDigestsByBudget chia các tóm tắt đoạn liên tục thành nhóm liên tục theo ngân sách byte; một tóm tắt đơn lẻ dù vượt ngân sách vẫn thành một nhóm riêng.
func groupDigestsByBudget(digests []RangeDigest, budgetBytes int) [][]RangeDigest {
	var groups [][]RangeDigest
	var cur []RangeDigest
	acc := 0
	for _, d := range digests {
		b, _ := json.Marshal(d)
		if len(cur) > 0 && acc+len(b) > budgetBytes {
			groups = append(groups, cur)
			cur, acc = nil, 0
		}
		cur = append(cur, d)
		acc += len(b)
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups
}

// buildDigestReducePayload lắp ráp đầu vào cho "gộp nhiều tóm tắt đoạn cấp dưới thành một RangeDigest".
// payload là nội dung mô hình nhìn thấy, câu lệnh đi theo ngôn ngữ tác phẩm (t).
func buildDigestReducePayload(digests []RangeDigest, t *importTexts) string {
	data, _ := json.Marshal(digests)
	return fmt.Sprintf(t.reduceInstruction,
		digests[0].StartChapter, digests[len(digests)-1].EndChapter, string(data))
}

// rangeDigestPath trả về đường dẫn tương đối của sản phẩm tóm tắt đoạn liên tục.
func rangeDigestPath(startChapter, endChapter int) string {
	return fmt.Sprintf("%s/%06d-%06d.json", dirRangeDigests, startChapter, endChapter)
}

// rangeInputDigest gắn tóm tắt gọn của đoạn liên tục đó với phiên bản prompt/schema của Range (RFC §6.3).
func rangeInputDigest(facts []ImportedChapterFacts) string {
	return Digest([]byte(fmt.Sprintf("range\x00%s\x00v%d\x00%s", rangePromptVersion, synthesisSchemaVersion, compactFacts(facts))))
}

func synthesizeBook(ctx context.Context, m callModel, systemPrompt, payload string, n, maxTokens int, prof callProfile) (*BookSynthesis, error) {
	t := importTextsOrDefault(prof.texts)
	prof.step(0, 0, "tạo tổng hợp toàn tác phẩm (thông tin tác phẩm/premise/characters/cấu trúc đề cương)...")
	s, err := callStructured[BookSynthesis](ctx, m, synthesisContract(t), systemPrompt, buildBookPayload(payload, n, t), maxTokens, prof, func(s *BookSynthesis) error {
		return validateSynthesis(s, n, t)
	})
	if err != nil {
		return nil, err
	}
	// Hiện lại cách mô hình hiểu toàn tác phẩm: đây là sản phẩm ngữ nghĩa cốt lõi nhất của việc nhập, xứng đáng để người dùng nhìn thấy ngay lập tức.
	prof.step(0, 0, "mô hình tóm tắt toàn tác phẩm: %s", snippet(s.Premise, 80))
	return &s, nil
}

// buildRangePayload lắp ráp đầu vào cho "tạo một RangeDigest cho một dải chương liên tục".
// payload là nội dung mô hình nhìn thấy, câu lệnh đi theo ngôn ngữ tác phẩm (t).
func buildRangePayload(facts []ImportedChapterFacts, t *importTexts) string {
	return fmt.Sprintf(t.rangeInstruction,
		facts[0].Chapter, facts[len(facts)-1].Chapter, compactFacts(facts))
}

// buildBookPayload lắp ráp đầu vào cho bước tổng hợp toàn tác phẩm; payload là nội dung mô hình nhìn thấy, câu lệnh đi theo ngôn ngữ tác phẩm (t).
func buildBookPayload(inner string, n int, t *importTexts) string {
	return fmt.Sprintf(t.bookInstruction, n, inner)
}

// validateSynthesis kiểm tra các ràng buộc cấu trúc của kết quả tổng hợp (miền giá trị / tập
// đóng / phạm vi), không phán xét lại chất lượng văn chương.
// t là bảng văn bản theo ngôn ngữ tác phẩm: lỗi ở đây được llmcontract gửi lại
// mô hình để hỏi lại, nên phải theo ngôn ngữ đang viết.
func validateSynthesis(s *BookSynthesis, n int, t *importTexts) error {
	t = importTextsOrDefault(t)
	if strings.TrimSpace(s.Synopsis) == "" {
		return fmt.Errorf("%s", t.errSynopsisEmpty)
	}
	if strings.TrimSpace(s.Premise) == "" {
		return fmt.Errorf("%s", t.errPremiseEmpty)
	}
	if len(s.Characters) == 0 {
		return fmt.Errorf("%s", t.errCharactersEmpty)
	}
	if !validPlanningTiers[s.PlanningTier] {
		return fmt.Errorf(t.errPlanningTierInvalid, s.PlanningTier)
	}
	switch s.StoryStatus {
	case storyOpen, storyClosed, storyUncertain:
	default:
		return fmt.Errorf(t.errStoryStatusInvalid, s.StoryStatus)
	}
	if strings.TrimSpace(s.Compass.EndingDirection) == "" {
		return fmt.Errorf("%s", t.errEndingDirectionEmpty)
	}
	return validateStructure(s.Structure, n, t)
}

// validateStructure kiểm tra phạm vi tập–cung liên tục, không chồng lấn, phủ đầy đủ 1..N (RFC §11 / bất biến 5).
func validateStructure(structure []ImportedVolumeRange, n int, t *importTexts) error {
	t = importTextsOrDefault(t)
	if len(structure) == 0 {
		return fmt.Errorf("%s", t.errStructureEmpty)
	}
	next := 1
	for vi, v := range structure {
		if len(v.Arcs) == 0 {
			return fmt.Errorf(t.errVolumeNoArc, vi, v.Title)
		}
		for ai, a := range v.Arcs {
			if a.StartChapter != next {
				return fmt.Errorf(t.errArcStartMismatch, vi, ai, a.StartChapter, next)
			}
			if a.EndChapter < a.StartChapter {
				return fmt.Errorf(t.errArcRangeInverted, vi, ai, a.StartChapter, a.EndChapter)
			}
			next = a.EndChapter + 1
		}
	}
	if next-1 != n {
		return fmt.Errorf(t.errArcCoverageMismatch, next-1, n)
	}
	return nil
}

// synthesisInputDigest gắn tóm tắt gọn của tập phân tích từng chương có thứ tự + phiên bản
// prompt/schema của bước tổng hợp (RFC §6.3 / bất biến 6).
// Đưa phiên bản vào, nên khi hợp đồng tổng hợp thay đổi thì synthesis cũ tự nhiên mất hiệu lực
// và phải làm lại.
func synthesisInputDigest(facts []ImportedChapterFacts) string {
	var b strings.Builder
	b.WriteString("synthesize\x00")
	b.WriteString(synthesizePromptVersion)
	fmt.Fprintf(&b, "\x00v%d", synthesisSchemaVersion)
	for _, f := range facts {
		b.WriteByte(0)
		b.WriteString(compactFact(f))
	}
	return Digest([]byte(b.String()))
}

// Foundation là tập đối tượng miền chính thức lắp ráp từ BookSynthesis + sự thật từng chương (kiểm tra đầy đủ trước khi phát hành, RFC §11).
type Foundation struct {
	Book         domain.BookMetadata
	PlanningTier domain.PlanningTier
	Premise      string
	Characters   []domain.Character
	WorldRules   []domain.WorldRule
	Volumes      []domain.VolumeOutline
	Compass      domain.StoryCompass
	Closed       bool
}

// AssembleFoundation dùng ngữ nghĩa tổng hợp + sự thật từng chương để lắp ráp Foundation chính thức
// và kiểm tra đầy đủ.
// closed là sự thật về việc khép lại sau khi story_status được phán quyết; fallbackName là tiêu đề
// suy đoán dùng khi nội dung truyện không xác nhận được tên sách.
func AssembleFoundation(s *BookSynthesis, facts []ImportedChapterFacts, closed bool, fallbackName string) (*Foundation, error) {
	n := len(facts)
	// Ở đường vận hành (sau khi mô hình đã vượt qua validate) lỗi này đi ra cho
	// người vận hành nên dùng tiếng Việt cố định, không theo ngôn ngữ tác phẩm.
	if err := validateSynthesis(s, n, importTextsVI); err != nil {
		return nil, err
	}
	byChapter := make(map[int]ImportedChapterFacts, n)
	for _, f := range facts {
		byChapter[f.Chapter] = f
	}

	volumes := make([]domain.VolumeOutline, 0, len(s.Structure))
	for vi, v := range s.Structure {
		vol := domain.VolumeOutline{Index: vi + 1, Title: v.Title, Theme: v.Theme}
		for ai, a := range v.Arcs {
			arc := domain.ArcOutline{Index: ai + 1, Title: a.Title, Goal: a.Goal}
			for ch := a.StartChapter; ch <= a.EndChapter; ch++ {
				f, ok := byChapter[ch]
				if !ok {
					return nil, fmt.Errorf("phạm vi cung tham chiếu chương không tồn tại: %d", ch)
				}
				arc.Chapters = append(arc.Chapters, domain.OutlineEntry{
					Chapter: ch, Title: f.Title, CoreEvent: f.CoreEvent, Hook: f.Hook, Scenes: f.Scenes,
				})
			}
			vol.Arcs = append(vol.Arcs, arc)
		}
		volumes = append(volumes, vol)
	}
	if closed && len(volumes) > 0 {
		volumes[len(volumes)-1].Final = true
	}

	// Sau FlattenOutline số chương là N và tiêu đề khớp với sự thật từng chương (RFC §11.5).
	flat := domain.FlattenOutline(volumes)
	if len(flat) != n {
		return nil, fmt.Errorf("số chương FlattenOutline %d != %d", len(flat), n)
	}
	for _, e := range flat {
		if e.Title != byChapter[e.Chapter].Title {
			return nil, fmt.Errorf("tiêu đề chương %d không khớp với sự thật theo từng chương", e.Chapter)
		}
	}

	title := ""
	if s.Title != nil {
		title = strings.TrimSpace(*s.Title)
	}
	if title == "" {
		title = importedBookTitle(fallbackName)
	}
	return &Foundation{
		Book:         (domain.BookMetadata{Title: title, Synopsis: s.Synopsis}).Normalized(),
		PlanningTier: s.PlanningTier,
		Premise:      s.Premise,
		Characters:   s.Characters,
		WorldRules:   s.WorldRules,
		Volumes:      volumes,
		Compass:      s.Compass,
		Closed:       closed,
	}, nil
}

// importedBookTitle dùng tên tệp nguồn khi nội dung truyện không xác nhận được tên sách, bảo đảm thông tin tác phẩm vẫn có một tiêu đề rõ ràng.
func importedBookTitle(fallbackName string) string {
	name := strings.TrimSuffix(fallbackName, ".txt")
	name = strings.TrimSuffix(name, ".md")
	if name == "" {
		name = "Bản nhập chưa đặt tên"
	}
	return name
}
