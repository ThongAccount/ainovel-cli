package imp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

func mustLoadState(t *testing.T, w *Workspace) Facts {
	t.Helper()
	f, err := LoadState(w)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	return f
}

func TestNextActionChain(t *testing.T) {
	cases := []struct {
		name string
		f    Facts
		want Action
	}{
		{"空", Facts{}, ActionIngest},
		{"已建区待切分", Facts{WorkspaceReady: true}, ActionSegment},
		{"已切分待确认", Facts{WorkspaceReady: true, Segmented: true}, ActionAwaitConfirmation},
		{"已确认待分析", Facts{WorkspaceReady: true, Segmented: true, Confirmed: true, ExpectedChapters: 3}, ActionAnalyze},
		{"分析未满", Facts{WorkspaceReady: true, Segmented: true, Confirmed: true, ExpectedChapters: 3, AnalyzedChapters: 2}, ActionAnalyze},
		{"分析齐待综合", Facts{WorkspaceReady: true, Segmented: true, Confirmed: true, ExpectedChapters: 3, AnalyzedChapters: 3}, ActionSynthesize},
		{"综合后 uncertain 待裁定", Facts{WorkspaceReady: true, Segmented: true, Confirmed: true, ExpectedChapters: 3, AnalyzedChapters: 3, Synthesized: true, StoryUncertain: true}, ActionAwaitStoryResolution},
		{"uncertain 已裁定待发布", Facts{WorkspaceReady: true, Segmented: true, Confirmed: true, ExpectedChapters: 3, AnalyzedChapters: 3, Synthesized: true, StoryUncertain: true, StoryResolved: true}, ActionPublish},
		{"明确状态待发布", Facts{WorkspaceReady: true, Segmented: true, Confirmed: true, ExpectedChapters: 3, AnalyzedChapters: 3, Synthesized: true}, ActionPublish},
		{"全部一致", Facts{WorkspaceReady: true, Segmented: true, Confirmed: true, ExpectedChapters: 3, AnalyzedChapters: 3, Synthesized: true, Published: true}, ActionDone},
		{"发布终态短路上游失鲜", Facts{Published: true}, ActionDone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NextAction(c.f)
			if got != c.want {
				t.Fatalf("NextAction=%s want=%s", got, c.want)
			}
			// Phải không đổi với cùng một ảnh chụp sự thật.
			if NextAction(c.f) != got {
				t.Fatal("NextAction phải không đổi với cùng một Facts")
			}
		})
	}
}

func TestLoadStateReflectsWorkspace(t *testing.T) {
	book := t.TempDir()
	// Chưa tạo vùng: không hoạt động → ingest.
	w := OpenWorkspace(book)
	if NextAction(mustLoadState(t, w)) != ActionIngest {
		t.Fatal("cuốn sách trống phải ingest trước")
	}
	// Sau khi tạo vùng: workspace ready, chưa cắt → segment.
	src := filepath.Join(book, "book.txt")
	if err := os.WriteFile(src, []byte("第一章\n正文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, _, err := Ingest(book, src, Intent{})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	f := mustLoadState(t, ws)
	if !f.WorkspaceReady || f.Segmented {
		t.Fatalf("sự thật sau khi tạo vùng không khớp: %+v", f)
	}
	if NextAction(f) != ActionSegment {
		t.Fatal("sau khi tạo vùng phải segment")
	}
}

func TestLoadStateReportsCorruptArtifact(t *testing.T) {
	book := t.TempDir()
	src := filepath.Join(book, "book.txt")
	if err := os.WriteFile(src, []byte("第一章\n正文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, _, err := Ingest(book, src, Intent{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.writeAtomic(fileSegmentation, []byte("{")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(ws); err == nil || !strings.Contains(err.Error(), "sản phẩm cắt") {
		t.Fatalf("sản phẩm hỏng không được giả làm chưa cắt: %v", err)
	}
}

func TestIngestSnapshotConsistent(t *testing.T) {
	book := t.TempDir()
	src := filepath.Join(book, "book.txt")
	content := "第一章\r\n正文一\r\n\r\n第二章\r\n正文二"
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, m, err := Ingest(book, src, Intent{})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if m.Encoding != encodingUTF8 || m.SourceName != "book.txt" {
		t.Fatalf("manifest không khớp: %+v", m)
	}
	snap, err := ws.LoadSource()
	if err != nil {
		t.Fatal(err)
	}
	// Ảnh chụp nguồn phải đã được chuẩn hoá, và tóm tắt phải khớp với manifest.
	if string(snap) != "第一章\n正文一\n\n第二章\n正文二" {
		t.Fatalf("ảnh chụp nguồn chưa được chuẩn hoá: %q", snap)
	}
	if Digest(snap) != m.NormalizedSHA256 {
		t.Fatal("tóm tắt ảnh chụp nguồn không khớp với manifest")
	}
}

// TestGuidanceChangeInvalidatesSegmentation bảo vệ §18.3: hướng dẫn cắt là đầu vào ngữ nghĩa của segmentation,
// khi hướng dẫn đổi thì kết quả cắt cũ (và mọi thứ phía dưới) tự nhiên lệch khớp để làm lại, không cần quy tắc huỷ hiệu lực thủ công.
func TestGuidanceChangeInvalidatesSegmentation(t *testing.T) {
	book := t.TempDir()
	src := filepath.Join(book, "book.txt")
	if err := os.WriteFile(src, []byte("第一章\n正文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, _, err := Ingest(book, src, Intent{})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	norm, err := ws.LoadSource()
	if err != nil {
		t.Fatal(err)
	}
	seg := Segmentation{Chapters: []ChapterSpan{{Number: 1, Title: "第一章", Start: 0, End: len(norm)}}}
	if err := writeArtifact(ws, fileSegmentation, segmentInputDigest(Digest(norm), "", segmentPromptVersion), seg); err != nil {
		t.Fatal(err)
	}
	if !mustLoadState(t, ws).Segmented {
		t.Fatal("khi không có hướng dẫn thì kết quả cắt phải còn hiệu lực")
	}
	if err := ws.writeAtomic(fileGuidance, []byte("幕间也是独立章节")); err != nil {
		t.Fatal(err)
	}
	if mustLoadState(t, ws).Segmented {
		t.Fatal("sau khi hướng dẫn đổi thì kết quả cắt cũ phải mất hiệu lực (cần nhận diện lại)")
	}
}

// TestResumeSummary bảo vệ thông báo khởi động §18.2: không có không gian làm việc thì trả về chuỗi rỗng; khi dừng dở giữa chừng thì đưa ra mô tả theo giai đoạn,
// để người dùng không phải chờ đến lúc việc sáng tác bị chặn mới biết cuốn sách này đang dừng giữa chừng quá trình nhập.
func TestResumeSummary(t *testing.T) {
	dir := t.TempDir()
	st := store.NewStore(dir)
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if got := ResumeSummary(st); got != "" {
		t.Fatalf("không có không gian làm việc nhập thì phải trả về chuỗi rỗng, được %q", got)
	}
	src := filepath.Join(dir, "book.txt")
	if err := os.WriteFile(src, []byte("第一章\n正文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, _, err := Ingest(dir, src, Intent{})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if got := ResumeSummary(st); !strings.Contains(got, "chưa hoàn tất phần cắt") {
		t.Fatalf("vừa tạo vùng phải nhắc chưa xong phần cắt, được %q", got)
	}
	// Cắt + xác nhận đã sẵn sàng, phân tích 0/1 → nhắc tiến độ phân tích.
	norm, _ := ws.LoadSource()
	seg := Segmentation{Chapters: []ChapterSpan{{Number: 1, Title: "第一章", Start: 0, End: len(norm)}}}
	if err := writeArtifact(ws, fileSegmentation, segmentInputDigest(Digest(norm), "", segmentPromptVersion), seg); err != nil {
		t.Fatal(err)
	}
	raw, _ := ws.readBytes(fileSegmentation)
	if err := writeArtifact(ws, fileConfirmation, Digest(raw), Confirmation{Method: confirmMethodAuto, Chapters: 1}); err != nil {
		t.Fatal(err)
	}
	if got := ResumeSummary(st); !strings.Contains(got, "đã phân tích 0/1 chương") {
		t.Fatalf("phải nhắc tiến độ phân tích, được %q", got)
	}
}

// TestResumeStatusPublishedIsTerminal bảo vệ trạng thái cuối sau khi phát hành (sự cố đã đo thực tế): khi cuốn sách đã phát hành toàn bộ,
// việc segmentPromptVersion nâng cấp làm sản phẩm cắt trong không gian làm việc bị cũ, ResumeStatus không được vì thế mà xếp lại cuốn sách
// về «dở dang giữa chừng quá trình nhập» — nếu không thì cổng chặn startEngine qua các lần khởi động lại sẽ vĩnh viễn từ chối cho viết tiếp cuốn sách đã phát hành.
func TestResumeStatusPublishedIsTerminal(t *testing.T) {
	dir := t.TempDir()
	st := store.NewStore(dir)
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "book.txt")
	if err := os.WriteFile(src, []byte("第一章\n正文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, _, err := Ingest(dir, src, Intent{})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	norm, _ := ws.LoadSource()
	// Ghi kết quả cắt bằng số phiên bản cũ: mô phỏng digest lệch khớp do nâng cấp prompt sau khi phát hành.
	seg := Segmentation{Chapters: []ChapterSpan{{Number: 1, Title: "第一章", Start: 0, End: len(norm)}}}
	if err := writeArtifact(ws, fileSegmentation, segmentInputDigest(Digest(norm), "", "seg-v0"), seg); err != nil {
		t.Fatal(err)
	}
	// Chưa phát hành + kết quả cắt đã cũ: vẫn là nhập dở dang, cổng chặn phải chặn lại.
	if active, done, err := ResumeStatus(st); err != nil || !active || done {
		t.Fatalf("không gian làm việc đã cũ mà chưa phát hành phải bị xếp là chưa hoàn tất (active=%v done=%v)", active, done)
	}
	// Thư viện chính thức đã ghi đủ theo kết quả cắt đó → đối soát phát hành pass, trạng thái cuối không bị ảnh hưởng bởi phía trên đã cũ.
	if err := st.Book.Save(domain.BookMetadata{Title: "测试书", Synopsis: "测试简介"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Outline.SavePremise("前提"); err != nil {
		t.Fatal(err)
	}
	if err := st.Outline.SaveOutline([]domain.OutlineEntry{{Chapter: 1, Title: "第一章"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.Save(&domain.Progress{CompletedChapters: []int{1}}); err != nil {
		t.Fatal(err)
	}
	if active, done, err := ResumeStatus(st); err != nil || !active || !done {
		t.Fatalf("cuốn sách đã phát hành phải được xếp là nhập đã hoàn tất (active=%v done=%v)", active, done)
	}
	if got := ResumeSummary(st); got != "" {
		t.Fatalf("cuốn sách đã phát hành không nên nhắc nhập chưa hoàn tất, được %q", got)
	}
}

func TestImportPreconditions(t *testing.T) {
	// Cuốn sách trống thì pass.
	empty := store.NewStore(t.TempDir())
	if err := checkImportPreconditions(empty); err != nil {
		t.Fatalf("cuốn sách trống phải qua kiểm tra tiền điều kiện: %v", err)
	}
	// Có chương đã hoàn tất thì bị từ chối.
	nonEmpty := store.NewStore(t.TempDir())
	if err := nonEmpty.Progress.Save(&domain.Progress{CompletedChapters: []int{1, 2}}); err != nil {
		t.Fatal(err)
	}
	if err := checkImportPreconditions(nonEmpty); err == nil {
		t.Fatal("cuốn sách không rỗng phải bị từ chối nhập")
	}
	withBook := store.NewStore(t.TempDir())
	if err := withBook.Book.Save(domain.BookMetadata{Title: "已有作品", Synopsis: "已有简介"}); err != nil {
		t.Fatal(err)
	}
	if err := checkImportPreconditions(withBook); err == nil {
		t.Fatal("khi đã có thông tin tác phẩm thì phải bị từ chối nhập")
	}
}
