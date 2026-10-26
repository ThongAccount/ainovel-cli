package imp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
)

func TestUnitLessNumericNotLexical(t *testing.T) {
	// Thứ tự từ điển sẽ cho L900 > L1000, L1257.2 > L1800; thứ tự số phải ngược lại.
	if !unitLess(SourceUnit{Line: 900}, SourceUnit{Line: 1000}) {
		t.Fatal("L900 phải < L1000 (thứ tự số)")
	}
	if !unitLess(SourceUnit{Line: 1257, Part: 2}, SourceUnit{Line: 1800}) {
		t.Fatal("L1257.2 phải < L1800")
	}
	if !unitLess(SourceUnit{Line: 1257, Part: 1}, SourceUnit{Line: 1257, Part: 2}) {
		t.Fatal("part cùng dòng phải theo thứ tự số")
	}
	if unitLess(SourceUnit{Line: 5}, SourceUnit{Line: 5}) {
		t.Fatal("bằng nhau không nên less")
	}
}

func TestBuildSourceUnitsRoundtrip(t *testing.T) {
	norm := []byte("第一章\n正文一\n\n第二章\n正文二")
	units := buildSourceUnits(norm, 0)
	// Ghép lại: văn bản của mỗi unit + '\n' giữa các dòng phải khôi phục văn bản đã chuẩn hoá.
	var b strings.Builder
	for i, u := range units {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(u.Text)
		if u.Text != string(norm[u.StartByte:u.EndByte]) {
			t.Fatalf("phạm vi byte của unit %s không khớp với văn bản", u.ID)
		}
	}
	if b.String() != string(norm) {
		t.Fatalf("ghép lại không khớp: %q", b.String())
	}
	if units[0].ID != "L1" || units[3].ID != "L4" {
		t.Fatalf("ID không khớp: %s %s", units[0].ID, units[3].ID)
	}
}

func TestBuildSourceUnitsVirtualShard(t *testing.T) {
	// Một dòng dài vượt xa ngân sách → tách thành nhiều unit ảo, ranh giới nằm trên ranh giới ký tự UTF-8.
	long := strings.Repeat("字", 100) // mỗi chữ 3 byte = 300 byte
	units := buildSourceUnits([]byte(long), 30)
	if len(units) < 2 {
		t.Fatalf("dòng vượt ngân sách phải được tách, được %d", len(units))
	}
	var b strings.Builder
	for _, u := range units {
		if u.Line != 1 || u.Part == 0 {
			t.Fatalf("mảnh ảo phải cùng Line, Part>=1: %+v", u)
		}
		b.WriteString(u.Text) // các mảnh cùng một dòng, không có phân tách bằng xuống dòng
	}
	if b.String() != long {
		t.Fatal("ghép lại các mảnh ảo bị mất chữ")
	}
}

func TestResolveBoundaryByteAnchor(t *testing.T) {
	units := []SourceUnit{{ID: "L1", Line: 1, StartByte: 0, EndByte: 10, Text: "楔子风起楔"}}
	m := map[string]SourceUnit{"L1": units[0]}
	if _, err := resolveBoundaryByte(m, "L1", "风起", importTextsVI); err != nil {
		t.Fatalf("điểm neo duy nhất phải thành công: %v", err)
	}
	if _, err := resolveBoundaryByte(m, "L1", "楔", importTextsVI); err == nil {
		t.Fatal("điểm neo trùng lặp phải thất bại")
	}
	if _, err := resolveBoundaryByte(m, "L1", "缺失", importTextsVI); err == nil {
		t.Fatal("điểm neo không tồn tại phải thất bại")
	}
	if _, err := resolveBoundaryByte(m, "L9", "", importTextsVI); err == nil {
		t.Fatal("unit không tồn tại phải thất bại")
	}
}

func TestPlanChunksCoversWithoutGap(t *testing.T) {
	units := buildSourceUnits([]byte(strings.Repeat("行内容\n", 50)), 0)
	chunks := planChunks(units, 40)
	if len(chunks) < 2 {
		t.Fatalf("phải chia thành nhiều khối, được %d", len(chunks))
	}
	// Không kẽ hở, không chồng lấn và phủ trọn vẹn.
	if chunks[0][0] != 0 || chunks[len(chunks)-1][1] != len(units) {
		t.Fatal("chưa phủ trọn vẹn")
	}
	for i := 1; i < len(chunks); i++ {
		if chunks[i][0] != chunks[i-1][1] {
			t.Fatalf("khối %d không tiếp nối khối trước: %v", i, chunks)
		}
	}
}

func segFixture() ([]byte, []SourceUnit) {
	norm := []byte("前言\n感谢阅读\n第一章 风起\n正文一\n卷二\n第二章 云涌\n正文二")
	return norm, buildSourceUnits(norm, 0)
}

func TestResolveSegmentationHappy(t *testing.T) {
	norm, units := segFixture()
	// L1 前言(front) / L3 第一章 / L5 卷二(group) / L6 第二章
	decisions := []BoundaryDecision{
		{UnitID: "L1", Kind: kindFrontMatter, Title: "前言"},
		{UnitID: "L3", Kind: kindChapter, Title: "第一章 风起"},
		{UnitID: "L5", Kind: kindGroup, Title: "卷二"},
		{UnitID: "L6", Kind: kindChapter, Title: "第二章 云涌"},
	}
	seg, err := resolveSegmentation(norm, units, decisions)
	if err != nil {
		t.Fatalf("kiểm tra phủ phải pass: %v", err)
	}
	if len(seg.Chapters) != 2 {
		t.Fatalf("số chương phải là 2 (group không tính), được %d", len(seg.Chapters))
	}
	if seg.Chapters[0].Number != 1 || seg.Chapters[1].Number != 2 {
		t.Fatal("số chương phải liên tục")
	}
	if !strings.Contains(seg.Content(norm, 0), "正文一") {
		t.Fatalf("nội dung chương một không khớp: %q", seg.Content(norm, 0))
	}
	// Phủ: đoạn đầu (front_matter) bắt đầu từ 0, chương cuối phủ tới cuối văn bản.
	if len(seg.Matter) == 0 || seg.Matter[0].Kind != kindFrontMatter || seg.Matter[0].Start != 0 {
		t.Fatalf("đoạn đầu phải là front_matter bắt đầu từ 0: %+v", seg.Matter)
	}
	if seg.Chapters[len(seg.Chapters)-1].End != len(norm) {
		t.Fatal("chương cuối phải phủ tới cuối văn bản")
	}
}

func TestResolveSegmentationRejections(t *testing.T) {
	norm, units := segFixture()
	cases := []struct {
		name string
		ds   []BoundaryDecision
	}{
		{"无章节", []BoundaryDecision{
			{UnitID: "L1", Kind: kindFrontMatter},
		}},
		{"非法kind", []BoundaryDecision{
			{UnitID: "L1", Kind: "verse"},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := resolveSegmentation(norm, units, c.ds); err == nil {
				t.Fatalf("phải bị từ chối: %s", c.name)
			}
		})
	}
}

// TestResolveSegmentationReordersAndDedups bảo vệ kỷ luật toạ độ của phần dự phòng cuối vòng: thứ tự lộn xộn thỉnh thoảng xảy ra trong khối của mô hình được khôi phục
// một cách tất định bằng cách sắp theo byte (thực đo: 319 ranh giới từng thất bại vì 1 chỗ đảo thứ tự, và bộ nhớ đệm khối khiến lỗi tái hiện tất định);
// trùng ở cùng byte thì giữ lại cái xuất hiện trước và ghi vào Notes để bản xem trước xác nhận trình bày.
func TestResolveSegmentationReordersAndDedups(t *testing.T) {
	norm, units := segFixture()
	seg, err := resolveSegmentation(norm, units, []BoundaryDecision{
		{UnitID: "L3", Kind: kindChapter, Title: "第一章 风起"},
		{UnitID: "L1", Kind: kindChapter, Title: "开篇"}, // lộn xộn: vị trí trước L3
		{UnitID: "L6", Kind: kindChapter, Title: "第二章 云涌"},
		{UnitID: "L6", Kind: kindChapter, Title: "第二章 重复"}, // trùng ở cùng byte
	})
	if err != nil {
		t.Fatalf("lộn xộn/trùng lặp phải được sửa một cách tất định chứ không bị từ chối: %v", err)
	}
	if len(seg.Chapters) != 3 {
		t.Fatalf("phải có 3 chương, được %d: %+v", len(seg.Chapters), seg.Chapters)
	}
	if seg.Chapters[0].Title != "开篇" || seg.Chapters[0].Start != 0 {
		t.Fatalf("sau khi sắp, chương đầu phải là ranh giới có vị trí sớm nhất: %+v", seg.Chapters[0])
	}
	if seg.Chapters[2].Title != "第二章 云涌" {
		t.Fatalf("trùng ở cùng byte phải giữ lại cái xuất hiện trước: %+v", seg.Chapters[2])
	}
	if len(seg.Notes) != 1 || !strings.Contains(seg.Notes[0], "trùng nhau") {
		t.Fatalf("ranh giới trùng lặp phải được ghi vào Notes: %v", seg.Notes)
	}
}

// TestResolveSegmentationAbsorbsLeadingText bảo vệ việc sửa tất định cho phần đầu bị báo thiếu: văn bản đầu sách không rỗng như giới thiệu/quảng cáo
// nếu bị mô hình báo thiếu ranh giới thì không được bị cuối vòng phủ quyết — báo thiếu đã nằm trong bộ nhớ đệm khối, phủ quyết sẽ khiến lần chạy lại
// với 0 lần gọi tái hiện lỗi một cách tất định. Go bổ sung một front_matter để bao [0, first) và ghi Notes để bản xem trước xác nhận trình bày.
func TestResolveSegmentationAbsorbsLeadingText(t *testing.T) {
	norm, units := segFixture()
	// Chỉ báo chương từ L3: văn bản L1/L2 không rỗng không có ranh giới nào thuộc về.
	seg, err := resolveSegmentation(norm, units, []BoundaryDecision{
		{UnitID: "L3", Kind: kindChapter, Title: "第一章 风起"},
		{UnitID: "L6", Kind: kindChapter, Title: "第二章 云涌"},
	})
	if err != nil {
		t.Fatalf("văn bản đầu chưa có ranh giới thuộc về phải được thu thành front_matter chứ không bị từ chối: %v", err)
	}
	if len(seg.Matter) != 1 || seg.Matter[0].Kind != kindFrontMatter || seg.Matter[0].Start != 0 {
		t.Fatalf("phải bổ sung front_matter bắt đầu từ 0: %+v", seg.Matter)
	}
	if len(seg.Chapters) != 2 || seg.Chapters[0].Start == 0 {
		t.Fatalf("các chương không nên nuốt văn bản đầu: %+v", seg.Chapters)
	}
	if len(seg.Notes) != 1 || !strings.Contains(seg.Notes[0], "không được mô hình gán") {
		t.Fatalf("phải ghi lại ghi chú để đối chiếu thủ công: %v", seg.Notes)
	}
}

// TestResolveSegmentationNotesDuplicateTitles bảo vệ tính nhìn thấy của chương trùng tên: trong nguồn có quy ước tiêu đề thì tên chương
// không nên trùng, trùng là dấu hiệu tất định của «một chương bị cắt nhầm» — chỉ ghi Notes (chặn --yes, bản xem trước trình bày) để nhờ người
// kiểm tra, việc có gộp hay không không do Go phán quyết.
func TestResolveSegmentationNotesDuplicateTitles(t *testing.T) {
	norm, units := segFixture()
	seg, err := resolveSegmentation(norm, units, []BoundaryDecision{
		{UnitID: "L1", Kind: kindFrontMatter, Title: "前言"},
		{UnitID: "L3", Kind: kindChapter, Title: "第一章 风起"},
		{UnitID: "L6", Kind: kindChapter, Title: "第一章风起"}, // trùng tên (bỏ qua khác biệt khoảng trắng)
	})
	if err != nil {
		t.Fatalf("chương trùng tên nên cho qua và ghi Notes: %v", err)
	}
	if len(seg.Chapters) != 2 {
		t.Fatalf("phải có 2 chương, được %d", len(seg.Chapters))
	}
	if len(seg.Notes) != 1 || !strings.Contains(seg.Notes[0], "cùng tiêu đề") {
		t.Fatalf("phải ghi một ghi chú đối chiếu trùng tên: %v", seg.Notes)
	}
}

// TestChunkValidatorOwnedDiscipline bảo vệ độ phủ của kiểm tra lúc gọi: kind không hợp lệ trong vùng owned,
// anchor hỏng, xung đột ngữ nghĩa tại cùng vị trí, phần đầu khối đầu chưa có ranh giới thuộc về đều phải bị hỏi lại kèm phản hồi ngay lúc gọi — cho qua sẽ đi kèm khối vào bộ nhớ đệm,
// đến lúc resolve ở cuối vòng mới phát hiện thì lần chạy lại với 0 lần gọi chỉ đọc lại đúng dữ liệu hỏng đó; ranh giới ở vùng ngữ cảnh chắc chắn bị cắt bỏ nên không hỏi lại vì chúng;
// trùng hoàn toàn tại cùng vị trí là dư thừa cơ học, sau khi cho qua sẽ do resolve lặng lẽ khử trùng lặp.
func TestChunkValidatorOwnedDiscipline(t *testing.T) {
	norm, units := segFixture()
	unitByID := map[string]SourceUnit{}
	proj, owned := map[string]bool{}, map[string]bool{}
	for _, u := range units {
		unitByID[u.ID] = u
		proj[u.ID] = true
	}
	owned["L1"], owned["L2"], owned["L3"] = true, true, true
	v := chunkValidator{projIDs: proj, ownedIDs: owned, unitByID: unitByID, normalized: norm}

	cases := []struct {
		name    string
		bs      []BoundaryDecision
		wantErr bool
	}{
		{"owned 非法 kind", []BoundaryDecision{{UnitID: "L1", Kind: "volume"}}, true},
		{"owned 坏 anchor", []BoundaryDecision{{UnitID: "L3", Kind: kindChapter, Anchor: "不存在的锚"}}, true},
		{"owned 合法 anchor", []BoundaryDecision{{UnitID: "L3", Kind: kindChapter, Anchor: "第一章"}}, false},
		{"上下文区非法 kind 不重问", []BoundaryDecision{{UnitID: "L6", Kind: "volume"}}, false},
		{"投影外幻觉 ID", []BoundaryDecision{{UnitID: "L99", Kind: kindChapter}}, true},
		{"同位语义冲突重问", []BoundaryDecision{
			{UnitID: "L1", Kind: kindChapter, Title: "前言"},
			{UnitID: "L1", Kind: kindFrontMatter, Title: "前言"},
		}, true},
		{"同位完全重复放行", []BoundaryDecision{
			{UnitID: "L1", Kind: kindChapter, Title: "前言"},
			{UnitID: "L1", Kind: kindChapter, Title: "前言"},
		}, false},
		// Phản chiếu tiêu đề: tên chương/tên tập phải thực sự tồn tại trong nguyên văn của unit chứa ranh giới — tiêu đề bịa của ranh giới ảo bị chặn ở đây.
		{"编造章节标题重问", []BoundaryDecision{{UnitID: "L2", Kind: kindChapter, Title: "第某章 我编的"}}, true},
		{"归纳标题须 uncertain 放行", []BoundaryDecision{{UnitID: "L2", Kind: kindChapter, Title: "第某章 我编的", Uncertain: true}}, false},
		{"回显容忍空白差异", []BoundaryDecision{{UnitID: "L3", Kind: kindChapter, Title: "第一章风起"}}, false},
		{"编造卷名重问", []BoundaryDecision{{UnitID: "L2", Kind: kindGroup, Title: "卷九"}}, true},
		{"附属描述性标题不核对", []BoundaryDecision{{UnitID: "L2", Kind: kindFrontMatter, Title: "引言"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := v.validate(c.bs); (err != nil) != c.wantErr {
				t.Fatalf("wantErr=%v, được %v", c.wantErr, err)
			}
		})
	}

	// Phủ đầu khối đầu: L1/L2 không rỗng mà không có ranh giới thuộc về → hỏi lại; sau khi bổ sung ranh giới tại điểm đầu thì pass.
	vs := v
	vs.coverStart = true
	if err := vs.validate([]BoundaryDecision{{UnitID: "L3", Kind: kindChapter}}); err == nil {
		t.Fatal("đầu khối đầu chưa có ranh giới thuộc về phải bị hỏi lại")
	}
	if err := vs.validate([]BoundaryDecision{
		{UnitID: "L1", Kind: kindFrontMatter}, {UnitID: "L3", Kind: kindChapter},
	}); err != nil {
		t.Fatalf("điểm đầu đã được phủ thì phải pass: %v", err)
	}
	if err := vs.validate(nil); err == nil {
		t.Fatal("khối đầu không có ranh giới nào phải bị hỏi lại (toàn bộ văn bản đầu chưa có ranh giới thuộc về)")
	}
}

// TestSegmentClearsChunksOnResolveFailure bảo vệ chốt chặn của «tái hiện tất định nhờ bộ nhớ đệm»: khi tích hợp cuối vòng thất bại thì
// bộ nhớ đệm khối đã không còn giá trị (digest luôn khớp, lần chạy lại với 0 lần gọi đọc lại đúng lô ranh giới đó rồi chết lần nữa), phải xoá để đổi lấy
// cơ hội cho mô hình cắt lại ở lần sau; ảnh chụp quyết định được ghi thống nhất vào failures/ qua errSemantic.
func TestSegmentClearsChunksOnResolveFailure(t *testing.T) {
	norm, units := segFixture()
	// Mô hình đánh dấu cả cuốn sách là front_matter: không có chương nào, Go không thể sửa một cách tất định, cuối vòng thất bại.
	m := &mockModel{responses: []string{boundariesJSON(boundaryFixture("L1", "", kindFrontMatter, "前言"))}}
	w := &Workspace{dir: t.TempDir()}
	_, err := Segment(context.Background(), m, "sys", norm, units, "", 0, 0, 4096, callProfile{}, w, "id-1")
	if err == nil {
		t.Fatal("không có chương thì phải thất bại ở cuối vòng")
	}
	var se *errSemantic
	if !errors.As(err, &se) {
		t.Fatalf("thất bại cuối vòng phải là errSemantic (ghi thống nhất vào failures/), được %T", err)
	}
	if _, statErr := os.Stat(filepath.Join(w.dir, dirSegmentChunks)); !os.IsNotExist(statErr) {
		t.Fatalf("sau khi thất bại cuối vòng thì bộ nhớ đệm khối phải được xoá: %v", statErr)
	}
}

// mockModel trả lời theo thứ tự các phản hồi định sẵn, phục vụ kiểm thử hợp đồng typed-call.
// stops có thể chỉ định stop reason cho từng lần gọi; mặc định dùng stop hoặc StopReasonStop.
type mockModel struct {
	responses []string
	stops     []agentcore.StopReason
	i         int
	stop      agentcore.StopReason
}

func (m *mockModel) Generate(_ context.Context, _ []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	idx := m.i
	r := m.responses[idx%len(m.responses)]
	sr := m.stop
	if idx < len(m.stops) {
		sr = m.stops[idx]
	}
	if sr == "" {
		sr = agentcore.StopReasonStop
	}
	m.i++
	return &agentcore.LLMResponse{Message: agentcore.Message{
		Role:       agentcore.RoleAssistant,
		Content:    []agentcore.ContentBlock{agentcore.TextBlock(r)},
		StopReason: sr,
	}}, nil
}

// TestResolveSegmentationSingleLineChapters bảo vệ #9: đoạn một dòng không có xuống dòng (kịch bản cắt bằng điểm neo) cả đoạn chính là nội dung truyện,
// tiểu thuyết một dòng hay nhiều chương trong một dòng không nên bị từ chối với lý do «nội dung truyện rỗng».
func TestResolveSegmentationSingleLineChapters(t *testing.T) {
	normalized := []byte("第一章甲的故事第二章乙的故事") // cả tác phẩm nằm trên một dòng, không xuống dòng
	units := buildSourceUnits(normalized, 0)
	decisions := []BoundaryDecision{
		{UnitID: "L1", Kind: kindChapter, Title: "第一章"},                // không điểm neo → byte 0
		{UnitID: "L1", Anchor: "第二章", Kind: kindChapter, Title: "第二章"}, // điểm neo trong dòng cắt ra chương hai
	}
	seg, err := resolveSegmentation(normalized, units, decisions)
	if err != nil {
		t.Fatalf("nhiều chương trong một dòng phải được chấp nhận: %v", err)
	}
	if len(seg.Chapters) != 2 {
		t.Fatalf("phải cắt ra 2 chương, được %d", len(seg.Chapters))
	}
	if got := seg.Content(normalized, 0); got != "第一章甲的故事" {
		t.Fatalf("phạm vi nội dung chương đầu không đúng: %q", got)
	}
}

func TestSegmentWithMockModel(t *testing.T) {
	norm, units := segFixture()
	resp := boundariesJSON(
		boundaryFixture("L1", "", kindFrontMatter, "前言"),
		boundaryFixture("L3", "", kindChapter, "第一章 风起"),
		boundaryFixture("L5", "", kindGroup, "卷二"),
		boundaryFixture("L6", "", kindChapter, "第二章 云涌"),
	)
	m := &mockModel{responses: []string{resp}}
	seg, err := Segment(context.Background(), m, "sys", norm, units, "", 0, 0, 4096, callProfile{}, nil, "")
	if err != nil {
		t.Fatalf("Segment: %v", err)
	}
	if len(seg.Chapters) != 2 {
		t.Fatalf("phải có 2 chương, được %d", len(seg.Chapters))
	}
}

// TestResolveSegmentationAbsorbsEmptyChapter bảo vệ dung sai với nguồn bẩn: nguồn tiểu thuyết mạng thực tế thường có tiêu đề
// placeholder kiểu «已锁定/付费章节» (có tiêu đề, thiếu nội dung truyện). Loại ranh giới này không được làm hỏng toàn bộ —
// phủ quyết ở cuối vòng sẽ lãng phí toàn bộ lượt gọi mô hình của giai đoạn cắt;
// đoạn placeholder được gộp vào đoạn trước (không mất một chữ nào của văn bản), ghi vào Notes để bản xem trước xác nhận trình bày cho người kiểm tra.
func TestResolveSegmentationAbsorbsEmptyChapter(t *testing.T) {
	norm, units := segFixture()
	// Dòng L5 "卷二" bị mô hình đánh dấu là tiêu đề chương: span [L5,L6) của nó không có nội dung truyện → gộp vào chương một.
	decisions := []BoundaryDecision{
		{UnitID: "L1", Kind: kindFrontMatter, Title: "前言"},
		{UnitID: "L3", Kind: kindChapter, Title: "第一章 风起"},
		{UnitID: "L5", Kind: kindChapter, Title: "第五章 [本章节已锁定]"},
		{UnitID: "L6", Kind: kindChapter, Title: "第二章 云涌"},
	}
	seg, err := resolveSegmentation(norm, units, decisions)
	if err != nil {
		t.Fatalf("chương placeholder nội dung rỗng phải được hấp thụ chứ không thất bại toàn cục: %v", err)
	}
	if len(seg.Chapters) != 2 {
		t.Fatalf("phải có 2 chương (placeholder gộp vào đoạn trước), được %d", len(seg.Chapters))
	}
	if got := seg.Content(norm, 0); !strings.Contains(got, "卷二") {
		t.Fatalf("đoạn placeholder phải được gộp vào chương một (không mất văn bản): %q", got)
	}
	if len(seg.Notes) != 1 || !strings.Contains(seg.Notes[0], "已锁定") {
		t.Fatalf("phải ghi lại ghi chú để đối chiếu thủ công: %v", seg.Notes)
	}
	// Điểm đầu vốn đã là chương nội dung rỗng: không có đoạn trước để gộp → rơi thành front_matter, cũng không thất bại.
	seg, err = resolveSegmentation(norm, units, []BoundaryDecision{
		{UnitID: "L1", Kind: kindChapter, Title: "占位"}, // [L1,L2) dòng tiêu đề đơn không có nội dung truyện
		{UnitID: "L2", Kind: kindChapter, Title: "第一章"},
	})
	if err != nil {
		t.Fatalf("điểm đầu có nội dung rỗng phải rơi thành front_matter: %v", err)
	}
	if len(seg.Matter) != 1 || seg.Matter[0].Kind != kindFrontMatter {
		t.Fatalf("điểm đầu có nội dung rỗng phải là front_matter: %+v", seg.Matter)
	}
}

// TestSegmentClipsContextBoundaries bảo vệ phần thực thi phía Go của kỷ luật toạ độ: ranh giới mô hình trả về ở vùng ngữ cảnh
// không kích hoạt hỏi lại về ngữ nghĩa (mô hình yếu thường hết 3 lượt và làm chết cả khối), mã cắt thẳng — ranh giới đó thuộc về khối kề,
// khối kề sẽ báo nó trong vùng owned của chính nó, giữ lại sẽ gây trùng/lộn xộn giữa các khối.
func TestSegmentClipsContextBoundaries(t *testing.T) {
	norm, units := segFixture()
	chunks := planChunks(units, planningBudget(40, "sys", "")) // khớp với cách Segment lập kế hoạch bên trong
	if len(chunks) < 2 {
		t.Fatalf("fixture phải chia ra ít nhất 2 khối, được %d", len(chunks))
	}
	// Phản hồi mỗi khối: một ranh giới chương cho unit đầu của owned (không có tiêu đề thì dùng firstLine làm dự phòng, né kiểm tra phản chiếu tiêu đề —
	// ở đây đang kiểm thử kỷ luật toạ độ); khối đầu còn kèm thêm một ranh giới của unit đầu khối kế (vùng ngữ cảnh).
	responses := make([]string, len(chunks))
	for ci, owned := range chunks {
		boundaries := []map[string]any{boundaryFixture(units[owned[0]].ID, "", kindChapter, "")}
		if ci == 0 {
			boundaries = append(boundaries, boundaryFixture(units[chunks[1][0]].ID, "", kindChapter, ""))
		}
		responses[ci] = boundariesJSON(boundaries...)
	}
	// Ghi chú cắt bỏ đi qua phản chiếu tiến trình thường (kỷ luật toạ độ định kỳ, không phải cảnh báo — màu warn sẽ khiến người dùng tưởng đã lỗi).
	var clipNotes int
	prof := callProfile{progress: func(_, _ int, s string) {
		if strings.Contains(s, "cắt bỏ") {
			clipNotes++
		}
	}}
	seg, err := Segment(context.Background(), &mockModel{responses: responses}, "sys", norm, units, "", 40, 2, 4096, prof, nil, "")
	if err != nil {
		t.Fatalf("ranh giới ở vùng ngữ cảnh phải bị cắt bỏ chứ không thất bại: %v", err)
	}
	if len(seg.Chapters) != len(chunks) {
		t.Fatalf("phải có %d chương (ranh giới vượt phạm vi không được tính lặp), được %d", len(chunks), len(seg.Chapters))
	}
	if clipNotes != 1 {
		t.Fatalf("phải phản chiếu 1 ghi chú cắt bỏ, được %d", clipNotes)
	}
}

// TestSegmentReusesChunkArtifacts bảo vệ điểm dừng cấp khối: cắt ghi bộ nhớ đệm ranh giới theo từng khối, khi chạy lại các khối có digest khớp
// được tái sử dụng với 0 lần gọi mô hình — cắt là giai đoạn đắt nhất, một khối hỏng không nên phải trả lại các khối đã xong (cùng triết lý với analyze/synthesize).
func TestSegmentReusesChunkArtifacts(t *testing.T) {
	norm, units := segFixture()
	chunks := planChunks(units, planningBudget(40, "sys", "")) // khớp với cách Segment lập kế hoạch bên trong
	responses := make([]string, len(chunks))
	for ci, owned := range chunks {
		responses[ci] = boundariesJSON(boundaryFixture(units[owned[0]].ID, "", kindChapter, ""))
	}
	w := &Workspace{dir: t.TempDir()}
	m1 := &mockModel{responses: responses}
	seg1, err := Segment(context.Background(), m1, "sys", norm, units, "", 40, 2, 4096, callProfile{}, w, "id-1")
	if err != nil {
		t.Fatalf("chạy lần đầu: %v", err)
	}
	if m1.i != len(chunks) {
		t.Fatalf("chạy lần đầu phải gọi %d lần, được %d", len(chunks), m1.i)
	}
	m2 := &mockModel{responses: responses}
	seg2, err := Segment(context.Background(), m2, "sys", norm, units, "", 40, 2, 4096, callProfile{}, w, "id-1")
	if err != nil {
		t.Fatalf("chạy lại: %v", err)
	}
	if m2.i != 0 {
		t.Fatalf("khối có digest khớp phải được tái sử dụng với 0 lần gọi, thực tế gọi %d lần", m2.i)
	}
	if len(seg2.Chapters) != len(seg1.Chapters) {
		t.Fatalf("kết quả tái sử dụng phải nhất quán: %d != %d", len(seg2.Chapters), len(seg1.Chapters))
	}
	// Đổi định danh (đổi phiên bản prompt/hướng dẫn/nguồn) → bộ nhớ đệm tự nhiên lệch khớp, làm lại tất cả.
	m3 := &mockModel{responses: responses}
	if _, err := Segment(context.Background(), m3, "sys", norm, units, "", 40, 2, 4096, callProfile{}, w, "id-2"); err != nil {
		t.Fatalf("chạy lại khi đổi định danh: %v", err)
	}
	if m3.i != len(chunks) {
		t.Fatalf("đổi định danh phải làm lại tất cả (%d lần gọi), được %d", len(chunks), m3.i)
	}
}

// TestSegmentShrinksChunkOnTruncation bảo vệ vòng lặp ngân sách đầu ra: nhiều chương ngắn làm JSON ranh giới của một khối
// vượt quá đầu ra nhìn thấy (stop=length), phải thử lại với khối thu nhỏ một nửa thay vì thất bại toàn cục — cùng triết lý với việc thu nhỏ lô của analyze.
func TestSegmentShrinksChunkOnTruncation(t *testing.T) {
	norm, units := segFixture() // 7 unit, một khối [0,7), mid=3
	left := boundariesJSON(boundaryFixture("L1", "", kindChapter, ""))
	right := boundariesJSON(boundaryFixture("L6", "", kindChapter, "第二章 云涌"))
	m := &mockModel{
		responses: []string{`{"boundaries":[]}`, left, right},
		stops:     []agentcore.StopReason{agentcore.StopReasonLength}, // lần gọi đầu bị cắt ngắn, hai nửa khối bình thường
	}
	seg, err := Segment(context.Background(), m, "sys", norm, units, "", 0, 0, 4096, callProfile{}, nil, "")
	if err != nil {
		t.Fatalf("khi bị cắt ngắn phải thử lại với khối thu nhỏ chứ không thất bại: %v", err)
	}
	if m.i != 3 {
		t.Fatalf("phải có 1 lần cắt ngắn + 2 lần gọi nửa khối, được %d", m.i)
	}
	if len(seg.Chapters) != 2 {
		t.Fatalf("kết quả thu nhỏ khối phải phủ trọn vẹn (2 chương), được %d", len(seg.Chapters))
	}
}

// TestPlanningBudget bảo vệ phần trừ chi phí cấu trúc trong ngân sách lập kế hoạch cắt: nội dung owned chỉ là một phần của yêu cầu.
func TestPlanningBudget(t *testing.T) {
	if got := planningBudget(0, "sys", "g"); got != 0 {
		t.Fatalf("không có ngân sách thì phải truyền nguyên, được %d", got)
	}
	if got := planningBudget(1000, strings.Repeat("s", 100), strings.Repeat("g", 100)); got != 600 {
		t.Fatalf("(1000-200)*3/4 phải là 600, được %d", got)
	}
	if got := planningBudget(1000, strings.Repeat("s", 2000), ""); got != 250 {
		t.Fatalf("prompt quá dài phải kích hoạt mức dưới chunkBytes/4=250, được %d", got)
	}
}

// TestBuildProjectionContextByteCap bảo vệ trần byte của vùng ngữ cảnh: dòng quá dài được tách thành unit ảo (một mảnh có thể tới
// MaxUnitBytes) sẽ nuốt mất ngân sách đầu vào, ngữ cảnh chỉ là thông tin tham chiếu nên co theo trần byte thay vì nhận trọn.
func TestBuildProjectionContextByteCap(t *testing.T) {
	_, units := segFixture()
	if _, ids := buildProjection(units, [2]int{2, 3}, 2, 1, ""); len(ids) != 1 || !ids["L3"] {
		t.Fatalf("trần byte phải cắt các unit ngữ cảnh, chỉ còn owned: %v", ids)
	}
	if _, ids := buildProjection(units, [2]int{2, 3}, 2, 0, ""); len(ids) != 5 {
		t.Fatalf("không có trần byte thì phải có mỗi phía trước/sau 2 unit ngữ cảnh (tổng 5), được %v", ids)
	}
}

func TestCallStructuredTruncation(t *testing.T) {
	m := &mockModel{responses: []string{`{"boundaries":[]}`}, stop: agentcore.StopReasonLength}
	_, err := callStructured[boundaryBatch](context.Background(), m, segmentContract(importTextsVI), "s", "p", 16, callProfile{}, nil)
	var trunc *errTruncated
	if err == nil || !asTruncated(err, &trunc) {
		t.Fatalf("cắt ngắn theo độ dài phải trả về *errTruncated, được %v", err)
	}
}

func asTruncated(err error, target **errTruncated) bool {
	t, ok := err.(*errTruncated)
	if ok {
		*target = t
	}
	return ok
}
