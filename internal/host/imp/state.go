package imp

import (
	"fmt"
	"os"

	"github.com/voocel/ainovel-cli/internal/store"
)

// Action là hành động tất định bước kế tiếp mà NextAction suy ra từ các sự thật của workspace.
// Trạng thái bền vững không ghi bộ enum giai đoạn có thể trôi lệch; hành động kế tiếp chỉ được
// suy ra từ sản phẩm (RFC §6.2).
type Action string

const (
	ActionIngest               Action = "ingest"
	ActionSegment              Action = "segment"
	ActionAwaitConfirmation    Action = "await_confirmation"
	ActionAnalyze              Action = "analyze"
	ActionSynthesize           Action = "synthesize"
	ActionAwaitStoryResolution Action = "await_story_resolution"
	ActionPublish              Action = "publish"
	ActionDone                 Action = "done"
)

// Facts là ảnh chụp sự thật tối thiểu đọc từ workspace, đủ để quyết định hành động kế tiếp.
// Tách quyết định thuần (NextAction) khỏi IO (LoadState): NextAction là hằng với cùng một
// Facts (RFC §20.1).
type Facts struct {
	WorkspaceReady   bool // đủ bộ ba manifest + intent + source
	Segmented        bool
	Confirmed        bool
	ExpectedChapters int // tổng số chương đã xác nhận cắt (điền từ giai đoạn hai)
	AnalyzedChapters int // số chương đã phân tích liên tục từ chương 1, khớp InputDigest (điền từ giai đoạn ba)
	Synthesized      bool
	StoryUncertain   bool
	StoryResolved    bool
	Published        bool // sản phẩm chính thức khớp hoàn toàn với synthesis (điền từ giai đoạn năm)
}

// NextAction đi theo pipeline tuyến tính cố định và trả về hành động đầu tiên còn thiếu hoặc
// chưa thoả mãn. Hàm thuần, không có IO.
func NextAction(f Facts) Action {
	switch {
	case f.Published:
		// Phát hành là trạng thái cuối: kho chính thức đã đối chiếu khớp toàn bộ, workspace chỉ
		// còn là bản lưu trữ kiểm toán. Sản phẩm thượng nguồn đã cũ vì nâng phiên bản prompt /
		// hướng dẫn không còn bắt phải làm lại—nếu không, việc nâng phiên bản sẽ bị truy ngược
		// đánh giá lại cuốn sách đã phát hành thành đang dở dang, và cổng chặn Engine qua các
		// lần khởi động sẽ khoá vĩnh viễn.
		return ActionDone
	case !f.WorkspaceReady:
		return ActionIngest
	case !f.Segmented:
		return ActionSegment
	case !f.Confirmed:
		return ActionAwaitConfirmation
	case f.AnalyzedChapters < f.ExpectedChapters:
		return ActionAnalyze
	case !f.Synthesized:
		return ActionSynthesize
	case f.StoryUncertain && !f.StoryResolved:
		return ActionAwaitStoryResolution
	default:
		return ActionPublish
	}
}

// artifactFresh xác định sản phẩm có tồn tại và InputDigest của nó có bằng giá trị want cần
// dựng lại ở thời điểm hiện tại hay không;
// thiếu, phân tích lỗi, lệch schema hoặc lệch digest đều bị coi là không còn mới (cần làm lại).
func artifactFresh[T any](w *Workspace, rel, want string) (bool, error) {
	a, err := readArtifact[T](w, rel)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return a.InputDigest == want, nil
}

// LoadState đọc ảnh chụp sự thật hiện tại từ workspace (chỉ workspace, không gồm Store chính thức).
// Ngắt mạch tuyến tính: mỗi bước đều kiểm tra InputDigest của sản phẩm có khớp với tóm tắt dựng
// lại được từ thượng nguồn hiện tại hay không; bước nào lệch thì coi như bước đó chưa hoàn thành,
// các sự thật phía dưới giữ nguyên false, để NextAction làm lại từ đó—chính điều này khiến
// việc đổi cắt / phiên bản prompt / nguồn làm hỏng phía dưới một cách tự nhiên
// (RFC §6.2/§6.3 / bất biến 1).
// Published do bên gọi bổ sung theo đối chiếu phát hành chính thức (đi qua CollectFacts thống nhất).
func LoadState(w *Workspace) (Facts, error) {
	var f Facts
	if !w.Active() {
		return f, nil
	}
	if !(w.has(fileManifest) && w.has(fileIntent) && w.has(fileSource)) {
		return f, nil
	}
	src, err := w.LoadSource()
	if err != nil {
		return f, fmt.Errorf("đọc ảnh chụp tệp nguồn nhập: %w", err)
	}
	f.WorkspaceReady = true
	guidance, err := w.LoadGuidance()
	if err != nil {
		return f, fmt.Errorf("đọc hướng dẫn cắt: %w", err)
	}

	// segmentation: gắn với nguồn đã chuẩn hoá + hướng dẫn của người dùng + phiên bản prompt cắt.
	// Đổi hướng dẫn (nhận diện lại bằng --guide) làm kết quả cắt cũ mất hiệu lực một cách tự nhiên.
	segArt, err := readArtifact[Segmentation](w, fileSegmentation)
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return f, fmt.Errorf("đọc sản phẩm cắt: %w", err)
	}
	if segArt.InputDigest != segmentInputDigest(Digest(src), guidance, segmentPromptVersion) {
		return f, nil
	}
	f.Segmented = true
	seg := &segArt.Payload
	f.ExpectedChapters = len(seg.Chapters)

	// confirmation: gắn với byte gốc của sản phẩm segmentation.
	segRaw, err := w.readBytes(fileSegmentation)
	if err != nil {
		return f, fmt.Errorf("đọc nguyên bản sản phẩm cắt: %w", err)
	}
	confirmed, err := artifactFresh[Confirmation](w, fileConfirmation, Digest(segRaw))
	if err != nil {
		return f, fmt.Errorf("đọc xác nhận cắt: %w", err)
	}
	if !confirmed {
		return f, nil
	}
	f.Confirmed = true

	// Phân tích từng chương: số chương đã phân tích liên tục mà InputDigest từng chương khớp
	// với danh tính / phiên bản / nội dung của kết quả cắt.
	f.AnalyzedChapters, err = analyzedChaptersStrict(w, seg, src, segArt.InputDigest, analyzePromptVersion)
	if err != nil {
		return f, err
	}
	if f.AnalyzedChapters < f.ExpectedChapters {
		return f, nil
	}

	// synthesis: gắn với tập sự thật từng chương có thứ tự.
	facts, err := loadPriorFactsStrict(w, f.ExpectedChapters)
	if err != nil {
		return f, err
	}
	synArt, err := readArtifact[BookSynthesis](w, fileSynthesis)
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return f, fmt.Errorf("đọc sản phẩm tổng hợp toàn tác phẩm: %w", err)
	}
	if synArt.InputDigest != synthesisInputDigest(facts) {
		return f, nil
	}
	f.Synthesized = true
	f.StoryUncertain = synArt.Payload.StoryStatus == storyUncertain

	// story resolution: khi uncertain thì gắn với byte gốc của sản phẩm synthesis, hoặc do intent
	// chọn trước.
	synRaw, err := w.readBytes(fileSynthesis)
	if err != nil {
		return f, fmt.Errorf("đọc nguyên bản sản phẩm tổng hợp toàn tác phẩm: %w", err)
	}
	resolved, err := artifactFresh[StoryResolution](w, fileStoryResolve, Digest(synRaw))
	if err != nil {
		return f, fmt.Errorf("đọc phán quyết trạng thái câu chuyện: %w", err)
	}
	if resolved {
		f.StoryResolved = true
	} else if in, iErr := w.LoadIntent(); iErr != nil {
		return f, fmt.Errorf("đọc ý định nhập: %w", iErr)
	} else if in.StoryResolution != "" {
		f.StoryResolved = true
	}
	return f, nil
}

// CollectFacts kết hợp sự thật của workspace với đối chiếu phát hành chính thức, là cửa vào
// sự thật thống nhất cho ResumeStatus/ResumeSummary/runner.
// Số chương kỳ vọng khi đối chiếu phát hành lấy từ kết quả cắt còn mới trước; khi kết quả cắt
// lệch vì nâng phiên bản prompt / hướng dẫn thì lùi về số chương đã xác nhận trong sản phẩm
// lúc đó—các chương chính thức của cuốn sách đã phát hành đúng là được ghi theo bản cắt ấy,
// nên tính lại digest bằng phiên bản hiện tại để đối chiếu sẽ không khớp với bất cứ thứ gì.
func CollectFacts(st *store.Store, w *Workspace) (Facts, error) {
	f, err := LoadState(w)
	if err != nil {
		return f, err
	}
	expected := f.ExpectedChapters
	if expected == 0 {
		if segArt, err := readArtifact[Segmentation](w, fileSegmentation); err == nil {
			expected = len(segArt.Payload.Chapters)
		}
	}
	f.Published, err = isPublished(st, expected)
	return f, err
}

// ResumeStatus báo cáo có tồn tại workspace nhập đang hoạt động hay không, và nó đã hoàn tất
// triệt để chưa (kèm đối chiếu phát hành chính thức).
// Dùng cho cổng chặn Engine qua các lần khởi động (RFC §12.5): khi active && !done thì cấm quy
// trình sáng tác thường tiêu thụ trạng thái phát hành dở dang.
func ResumeStatus(st *store.Store) (active, done bool, err error) {
	w := OpenWorkspace(st.Dir())
	if !w.Active() {
		return false, false, nil
	}
	f, err := CollectFacts(st, w)
	if err != nil {
		return true, false, err
	}
	return true, NextAction(f) == ActionDone, nil
}

// ResumeSummary sinh một dòng nhắc cho import chưa hoàn tất (RFC §18.2); không có import dở dang
// thì trả về chuỗi rỗng.
// Dùng để host chủ động thông báo ngay ở màn khởi động / chào mừng, tránh để người dùng chỉ phát
// hiện cuốn sách đang dở dang ở giữa import khi bị cổng chặn từ chối sáng tác.
func ResumeSummary(st *store.Store) string {
	w := OpenWorkspace(st.Dir())
	if !w.Active() {
		return ""
	}
	f, err := CollectFacts(st, w)
	if err != nil {
		return "phát hiện lỗi khi đọc trạng thái nhập: " + err.Error() + "; hãy chạy /import để xem và khắc phục"
	}
	var state string
	switch NextAction(f) {
	case ActionDone:
		return ""
	case ActionIngest, ActionSegment:
		state = "chưa hoàn tất phần cắt"
	case ActionAwaitConfirmation:
		state = fmt.Sprintf("đã cắt %d chương, chờ kiểm tra và xác nhận", f.ExpectedChapters)
	case ActionAnalyze:
		state = fmt.Sprintf("đã phân tích %d/%d chương", f.AnalyzedChapters, f.ExpectedChapters)
	case ActionSynthesize:
		state = "đã phân tích xong từng chương, chờ tổng hợp toàn tác phẩm"
	case ActionAwaitStoryResolution:
		state = "đang chờ làm rõ trạng thái câu chuyện (--story=open|closed)"
	case ActionPublish:
		state = "đã tổng hợp xong, chờ phát hành trạng thái chính thức"
	}
	return "phát hiện bản nhập chưa hoàn tất (" + state + "), nhập /import để tiếp tục từ điểm dừng"
}

// checkImportPreconditions kiểm tra các điều kiện tiên quyết của import mới (RFC §12.1):
// không có thông tin tác phẩm sẵn có, chương đã hoàn thành hay PendingCommit đang dở.
// Ngữ nghĩa gộp tiểu thuyết đã có với văn bản bên ngoài mới chưa rõ ràng, phiên bản đầu tiên
// quyết định từ chối rõ ràng.
func checkImportPreconditions(st *store.Store) error {
	book, err := st.Book.Load()
	if err != nil {
		return fmt.Errorf("đọc thông tin tác phẩm: %w", err)
	}
	if book != nil {
		return fmt.Errorf("đã có tác phẩm «%s», từ chối gộp tiểu thuyết bên ngoài vào sách đã có nội dung", book.Title)
	}
	prog, err := st.Progress.Load()
	if err != nil {
		return fmt.Errorf("đọc tiến trình: %w", err)
	}
	if prog != nil && len(prog.CompletedChapters) > 0 {
		return fmt.Errorf("đã có %d chương hoàn thành, từ chối gộp tiểu thuyết bên ngoài vào sách đã có nội dung", len(prog.CompletedChapters))
	}
	pending, err := st.Signals.LoadPendingCommit()
	if err != nil {
		return fmt.Errorf("đọc bản ghi đang dở: %w", err)
	}
	if pending != nil {
		return fmt.Errorf("còn bản ghi chương đang dở, hãy hoàn tất hoặc dọn dẹp trước khi nhập")
	}
	return nil
}
