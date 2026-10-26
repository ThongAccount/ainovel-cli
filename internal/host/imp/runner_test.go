package imp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/tools"
)

// testDeps dựng Deps tối thiểu cho ba hàm ngữ nghĩa dùng chung một mức mock.
func testDeps(st *store.Store, m callModel) Deps {
	c := Caller{Model: m}
	return Deps{
		Store:         st,
		CommitChapter: tools.NewCommitChapterTool(st, tools.NewStyleStatsIndex(st)),
		Segment:       c,
		Analyze:       c,
		Synthesize:    c,
		Prompts:       Prompts{Segment: "seg", Analyze: "ana", Synthesize: "syn", Range: "range"},
	}
}

// TestRunEndToEnd dùng mô hình mock để chạy trọn vẹn đường ống ingest→segment→analyze→synthesize→publish,
// đi qua commit_chapter thật để ghi xuống đĩa, xác minh Foundation chính thức và toàn bộ chương đã sẵn sàng.
func TestRunEndToEnd(t *testing.T) {
	dir := t.TempDir()
	st := store.NewStore(dir)
	if err := st.Init(); err != nil {
		t.Fatalf("store init: %v", err)
	}
	src := filepath.Join(dir, "book.txt")
	if err := os.WriteFile(src, []byte("第一章\n正文一\n第二章\n正文二\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	seg := boundariesJSON(
		boundaryFixture("L1", "", kindChapter, "第一章"),
		boundaryFixture("L3", "", kindChapter, "第二章"),
	)
	ana := `{"chapters":[` + factsJSON(1, "第一章") + `,` + factsJSON(2, "第二章") + `]}`
	syn := synthesisFixtureJSON(2, storyClosed)
	m := &mockModel{responses: []string{seg, ana, syn}}

	ch, err := Run(context.Background(), testDeps(st, m), Options{SourcePath: src, AutoConfirm: true, ContinueAfter: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var runErr error
	var doneSeen bool
	for ev := range ch {
		if ev.Stage == StageError {
			runErr = ev.Err
		}
		if ev.Stage == StageDone {
			doneSeen = true
		}
	}
	if runErr != nil {
		t.Fatalf("đường ống thất bại: %v", runErr)
	}
	if !doneSeen {
		t.Fatal("không nhận được StageDone")
	}
	// Trạng thái chính thức đã sẵn sàng: thông tin tác phẩm, premise và dàn ý phẳng phủ toàn bộ chương đã được ghi xuống đĩa (world_rules hợp lệ khi rỗng nên không yêu cầu).
	if book, _ := st.Book.Load(); book == nil || book.Synopsis == "" {
		t.Fatalf("thông tin tác phẩm chưa được ghi xuống đĩa: %+v", book)
	}
	if p, _ := st.Outline.LoadPremise(); p == "" {
		t.Fatal("premise chưa được ghi xuống đĩa")
	}
	if o, _ := st.Outline.LoadOutline(); len(o) != 2 {
		t.Fatalf("dàn ý phẳng phải phủ 2 chương, được %d", len(o))
	}
	prog, _ := st.Progress.Load()
	if prog == nil || len(prog.CompletedChapters) != 2 {
		t.Fatalf("phải hoàn tất 2 chương: %+v", prog)
	}
	if active, done, err := ResumeStatus(st); err != nil || !active || !done {
		t.Fatalf("ResumeStatus phải là active&done, được active=%v done=%v", active, done)
	}
	// --continue: không đặt Hold hoàn tất nhập (để host tự tiếp tục).
	if meta, _ := st.RunMeta.Load(); meta != nil && meta.AdvanceHold != nil {
		t.Fatalf("--continue không nên để lại Hold hoàn tất nhập: %+v", meta.AdvanceHold)
	}
}

// TestRunSetsCompletionHold xác minh rằng sau khi nhập xong mà không có --continue thì đặt boundary Hold (RFC §12.4).
// Hold là biện pháp duy nhất bảo đảm «sau khi nhập không vô tình viết tiếp», phải được lưu bền trên đường phát hành.
func TestRunSetsCompletionHold(t *testing.T) {
	dir := t.TempDir()
	st := store.NewStore(dir)
	if err := st.Init(); err != nil {
		t.Fatalf("store init: %v", err)
	}
	src := filepath.Join(dir, "book.txt")
	if err := os.WriteFile(src, []byte("第一章\n正文一\n第二章\n正文二\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seg := boundariesJSON(
		boundaryFixture("L1", "", kindChapter, "第一章"),
		boundaryFixture("L3", "", kindChapter, "第二章"),
	)
	ana := `{"chapters":[` + factsJSON(1, "第一章") + `,` + factsJSON(2, "第二章") + `]}`
	syn := synthesisFixtureJSON(2, storyClosed)
	m := &mockModel{responses: []string{seg, ana, syn}}

	ch, err := Run(context.Background(), testDeps(st, m), Options{SourcePath: src, AutoConfirm: true}) // không có --continue
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for ev := range ch {
		if ev.Stage == StageError {
			t.Fatalf("đường ống thất bại: %v", ev.Err)
		}
	}
	meta, err := st.RunMeta.Load()
	if err != nil {
		t.Fatalf("load run meta: %v", err)
	}
	if meta == nil || meta.AdvanceHold == nil {
		t.Fatalf("hoàn tất nhập phải đặt boundary Hold, được %+v", meta)
	}
}

// TestRunRejectsDifferentSource bảo vệ chặn đổi nguồn (RFC §12.1/§18.2): khi không gian làm việc đang chạy mà truyền vào tệp nguồn
// có nội dung khác thì phải báo lỗi rõ ràng — ingest chỉ chạy khi chưa có không gian làm việc, không đối chiếu sẽ âm thầm tiếp tục từ điểm dừng của cuốn sách cũ,
// phát hành xong cuốn sách cũ mà chưa đọc được byte nào của tệp mới. Truyền lại cùng đường dẫn tệp là thói quen khôi phục thường gặp, nên so theo tóm tắt nội dung để cho qua.
func TestRunRejectsDifferentSource(t *testing.T) {
	dir := t.TempDir()
	st := store.NewStore(dir)
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(a, []byte("第一章\n正文一\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Ingest(dir, a, Options{}.intent()); err != nil {
		t.Fatalf("tạo không gian làm việc: %v", err)
	}
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(b, []byte("完全不同的另一本书\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch, err := Run(context.Background(), testDeps(st, &mockModel{responses: []string{"{}"}}), Options{SourcePath: b})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var runErr error
	for ev := range ch {
		if ev.Stage == StageError {
			runErr = ev.Err
		}
	}
	if runErr == nil || !strings.Contains(runErr.Error(), "khác nội dung") {
		t.Fatalf("tệp nguồn khác phải bị từ chối rõ ràng, được %v", runErr)
	}
}

// TestConfirmNotesGate bảo vệ ngưỡng dung sai của --yes: cấu trúc cắt từng có dung sai về ngữ nghĩa (Notes khác rỗng)
// đã bị viết lại một cách tất định, không nên để --yes chưa xem bản xem trước mù quáng cho qua; sau khi xem trước ở TUI và bấm y (AcceptSegmentation) thì cho qua,
// phương thức xác nhận ghi lại user_confirmed để truy vết.
func TestConfirmNotesGate(t *testing.T) {
	newRunner := func(opts Options, notes []string) *runner {
		ws := &Workspace{dir: t.TempDir()}
		if err := ws.writeJSON(fileIntent, Intent{}); err != nil {
			t.Fatal(err)
		}
		seg := Segmentation{Chapters: []ChapterSpan{{Number: 1, Title: "第一章", End: 10}}, Notes: notes}
		if err := writeArtifact(ws, fileSegmentation, "d", seg); err != nil {
			t.Fatal(err)
		}
		return &runner{opts: opts, events: make(chan Event, 8), ws: ws}
	}
	r := newRunner(Options{AutoConfirm: true}, []string{"空正文占位并入前段"})
	if r.confirm() {
		t.Fatal("--yes không nên cho qua kết quả cắt có ghi chú dung sai")
	}
	if ev := <-r.events; !strings.Contains(ev.Message, "không tự động duyệt") {
		t.Fatalf("bản xem trước phải nêu lý do không cho qua: %q", ev.Message)
	}
	if !newRunner(Options{AutoConfirm: true}, nil).confirm() {
		t.Fatal("--yes nên cho qua kết quả cắt không có ghi chú dung sai")
	}
	r = newRunner(Options{AcceptSegmentation: true}, []string{"空正文占位并入前段"})
	if !r.confirm() {
		t.Fatal("phím y thủ công sau bản xem trước nên cho qua kết quả cắt có ghi chú dung sai")
	}
	conf, err := readArtifact[Confirmation](r.ws, fileConfirmation)
	if err != nil {
		t.Fatal(err)
	}
	if conf.Payload.Method != confirmMethodUser {
		t.Fatalf("xác nhận thủ công phải ghi user_confirmed, được %q", conf.Payload.Method)
	}
}

// TestStoryChoiceIgnoresStaleResolution bảo vệ #5: sau khi tổng hợp lại thì phán quyết câu chuyện cũ mất hiệu lực,
// storyChoice không được âm thầm áp open/closed cũ lên synthesis mới (nếu không người dùng sẽ không được hỏi lại).
func TestStoryChoiceIgnoresStaleResolution(t *testing.T) {
	ws := OpenWorkspace(t.TempDir())
	if err := ws.writeJSON(fileIntent, Intent{}); err != nil {
		t.Fatal(err)
	}
	if err := writeArtifact(ws, fileSynthesis, "d", BookSynthesis{Premise: "p1", StoryStatus: storyUncertain}); err != nil {
		t.Fatal(err)
	}
	raw, _ := ws.readBytes(fileSynthesis)
	if err := writeArtifact(ws, fileStoryResolve, Digest(raw), StoryResolution{Choice: storyClosed}); err != nil {
		t.Fatal(err)
	}
	r := &runner{ws: ws}
	if got, err := r.storyChoice(); err != nil || got != storyClosed {
		t.Fatalf("phán quyết gắn với synthesis hiện tại phải trả về closed, được %q", got)
	}
	// Tổng hợp lại: viết lại synthesis → InputDigest của phán quyết cũ lệch khớp, phải bị bỏ qua, trở lại «cần hỏi lại» (trả về rỗng).
	if err := writeArtifact(ws, fileSynthesis, "d", BookSynthesis{Premise: "p2", StoryStatus: storyUncertain}); err != nil {
		t.Fatal(err)
	}
	if got, err := r.storyChoice(); err != nil || got != "" {
		t.Fatalf("sau khi tổng hợp lại thì phán quyết cũ phải mất hiệu lực và trả về rỗng, được %q", got)
	}
}

// TestBudgetsFromDepsPerTier bảo vệ nút xoay mức (RFC §13.1): ngân sách của từng hàm ngữ nghĩa được suy ra theo mức của chính nó,
// cửa sổ nhỏ của mức rẻ chỉ ràng buộc hàm của nó, không kéo theo các giai đoạn khác.
func TestBudgetsFromDepsPerTier(t *testing.T) {
	small := ModelRuntime{ContextTokens: 32000, MaxOutputTokens: 4000}
	big := ModelRuntime{ContextTokens: 200000, MaxOutputTokens: 16000}
	b := budgetsFromDeps(Deps{
		Segment:    Caller{Runtime: small},
		Analyze:    Caller{Runtime: big},
		Synthesize: Caller{Runtime: big},
	})
	if b.SegmentChunkBytes >= b.Analyze.ContextBytes {
		t.Fatalf("cửa sổ mức nhỏ của segment chỉ nên ràng buộc chính nó: seg=%d analyze=%d", b.SegmentChunkBytes, b.Analyze.ContextBytes)
	}
	if b.Analyze.MaxOutputTokens != 16000 || b.SegmentMaxTokens != 4000 {
		t.Fatalf("ngân sách đầu ra mỗi cái phải lấy trần của mức riêng: analyze=%d segment=%d", b.Analyze.MaxOutputTokens, b.SegmentMaxTokens)
	}
}

// TestRunSavesFailureOnContractViolation bảo vệ §14.2: vi phạm hợp đồng của Schema gốc
// phải lộ ra ngay, và phản hồi gốc cùng siêu dữ liệu phải được ghi vào failures/.
func TestRunSavesFailureOnContractViolation(t *testing.T) {
	dir := t.TempDir()
	st := store.NewStore(dir)
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "book.txt")
	if err := os.WriteFile(src, []byte("第一章\n正文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &nativeImportModel{mockModel: &mockModel{responses: []string{"这不是 JSON"}}}
	ch, err := Run(context.Background(), testDeps(st, m), Options{SourcePath: src, AutoConfirm: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var failed bool
	for ev := range ch {
		if ev.Stage == StageError {
			failed = true
		}
	}
	if !failed {
		t.Fatal("đầu ra không hợp lệ phải kết thúc bằng StageError")
	}
	ws := OpenWorkspace(dir)
	if !ws.has("failures/last-response.txt") {
		t.Fatal("phải lưu phản hồi gốc cuối cùng của mô hình")
	}
	var meta FailureMeta
	if err := ws.readJSON("failures/last.json", &meta); err != nil {
		t.Fatalf("đọc siêu dữ liệu lỗi: %v", err)
	}
	if meta.Stage != string(ActionSegment) {
		t.Fatalf("siêu dữ liệu lỗi phải ghi giai đoạn segment, được %q", meta.Stage)
	}
}

// TestRunGuidanceResegments bảo vệ §18.3: khi khôi phục mà mang theo --guide thì kết quả cắt cũ tự nhiên lệch khớp,
// nhận diện lại theo hướng dẫn mới và lại dừng ở bước xác nhận; InputDigest của kết quả cắt mới gắn với văn bản hướng dẫn.
func TestRunGuidanceResegments(t *testing.T) {
	dir := t.TempDir()
	st := store.NewStore(dir)
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "book.txt")
	if err := os.WriteFile(src, []byte("第一章\n正文一\n第二章\n正文二\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	drain := func(ch <-chan Event) (awaiting bool) {
		for ev := range ch {
			if ev.Stage == StageError {
				t.Fatalf("đường ống thất bại: %v", ev.Err)
			}
			if ev.Stage == StageAwaitingConfirmation {
				awaiting = true
			}
		}
		return awaiting
	}
	// Nhập tương tác lần đầu: mô hình cắt cả cuốn sách thành 1 chương, dừng ở bước xác nhận.
	one := boundariesJSON(boundaryFixture("L1", "", kindChapter, "第一章"))
	ch, err := Run(context.Background(), testDeps(st, &mockModel{responses: []string{one}}), Options{SourcePath: src})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !drain(ch) {
		t.Fatal("nhập lần đầu phải dừng ở bước xác nhận kết quả cắt")
	}
	// Khôi phục kèm hướng dẫn: kết quả cắt cũ lệch khớp → nhận diện lại thành 2 chương, lại dừng ở bước xác nhận.
	two := boundariesJSON(
		boundaryFixture("L1", "", kindChapter, "第一章"),
		boundaryFixture("L3", "", kindChapter, "第二章"),
	)
	guidance := "第二章也是独立章节"
	ch2, err := Run(context.Background(), testDeps(st, &mockModel{responses: []string{two}}), Options{Guidance: guidance})
	if err != nil {
		t.Fatalf("Run khôi phục: %v", err)
	}
	if !drain(ch2) {
		t.Fatal("sau khi nhận diện lại phải lại dừng ở bước xác nhận kết quả cắt")
	}
	ws := OpenWorkspace(dir)
	art, err := readArtifact[Segmentation](ws, fileSegmentation)
	if err != nil {
		t.Fatalf("đọc sản phẩm cắt: %v", err)
	}
	if len(art.Payload.Chapters) != 2 {
		t.Fatalf("phải cắt thành 2 chương theo hướng dẫn, được %d", len(art.Payload.Chapters))
	}
	norm, _ := ws.LoadSource()
	if art.InputDigest != segmentInputDigest(Digest(norm), guidance, segmentPromptVersion) {
		t.Fatal("InputDigest của kết quả cắt mới phải gắn với văn bản hướng dẫn")
	}
}

// TestBudgetsFromRuntime xác minh cả hai ngân sách lớn theo dung lượng thật của mô hình, khi không biết năng lực thì lùi về mặc định thận trọng (RFC §9.2/§21).
func TestBudgetsFromRuntime(t *testing.T) {
	if got := budgetsFromRuntime(ModelRuntime{}); got != DefaultRunBudgets() {
		t.Fatal("khi không biết năng lực phải lùi về mặc định thận trọng")
	}
	small := budgetsFromRuntime(ModelRuntime{ContextTokens: 32000, MaxOutputTokens: 4000})
	big := budgetsFromRuntime(ModelRuntime{ContextTokens: 200000, MaxOutputTokens: 16000})
	if big.Analyze.ContextBytes <= small.Analyze.ContextBytes {
		t.Fatalf("context lớn hơn phải phóng to ngân sách đầu vào của analyze: small=%d big=%d", small.Analyze.ContextBytes, big.Analyze.ContextBytes)
	}
	if big.Analyze.MaxOutputTokens != 16000 {
		t.Fatalf("ngân sách đầu ra phải lấy trần completion của mô hình, được %d", big.Analyze.MaxOutputTokens)
	}
}

// TestProfileForKeyPolicy bảo vệ phạm vi gộp sự kiện: yêu cầu lùi (có mốc chặn) cùng Key cập nhật tại chỗ;
// hỏi lại do kiểm tra là sự kiện ngữ nghĩa xuyên các lần gọi, không có Key nên mỗi cái một dòng — cắt gọi theo từng khối, dùng chung Key sẽ khiến khối sau ghi đè khối trước,
// bảng chỉ còn một dòng unit_id đổi liên tục, mất sạch manh mối để truy; step là sự kiện tiến trình thường (không có mức cảnh báo).
func TestProfileForKeyPolicy(t *testing.T) {
	r := &runner{events: make(chan Event, 3)}
	prof := r.profileFor(Caller{}, StageSegmenting)
	prof.notify("退避", time.Now().Add(time.Second))
	prof.notify("重问", time.Time{})
	prof.step(2, 12, "切分第 %d/%d 块...", 2, 12)
	backoff, reask, step := <-r.events, <-r.events, <-r.events
	if backoff.Key == "" || backoff.Level != "warn" || backoff.RetryAt.IsZero() {
		t.Fatalf("yêu cầu lùi phải là sự kiện warn có Key và mốc chặn: %+v", backoff)
	}
	if reask.Key != "" || reask.Level != "warn" {
		t.Fatalf("hỏi lại do kiểm tra phải là sự kiện warn không có Key (đứng riêng một dòng): %+v", reask)
	}
	if step.Level != "" || step.Current != 2 || step.Total != 12 {
		t.Fatalf("step phải là sự kiện tiến trình thường: %+v", step)
	}
}

// TestCallProfileOptions xác minh callProfile chỉ phụ trách ngân sách đầu ra và thinking; response_format
// do callStructured chọn dựa trên thực tế của mô hình và Contract, không được lắp ráp lại trong Profile.
func TestCallProfileOptions(t *testing.T) {
	if got := (callProfile{}).callOptions(100); len(got) != 1 {
		t.Fatalf("giá trị 0 chỉ nên mang maxTokens, được %d option", len(got))
	}
	if got := (callProfile{thinking: "high"}).callOptions(100); len(got) != 2 {
		t.Fatalf("thinking nên mang 2 option, được %d", len(got))
	}
}
