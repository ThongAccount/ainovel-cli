package imp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/logger"
	"github.com/voocel/ainovel-cli/internal/store"
)

// Phiên bản prompt/schema được đưa vào InputDigest của từng giai đoạn; tăng phiên bản khi nâng cấp hợp đồng prompt để làm các sản phẩm phía sau tự nhiên mất hiệu lực.
const (
	segmentPromptVersion = "seg-v2" // v2: ranh giới chỉ đặt ở chỗ phân tách thật, tiêu đề sao chép nguyên văn (phối hợp với kiểm tra hồi tiếng tiêu đề)
	analyzePromptVersion = "analyze-v1"
	confirmMethodAuto    = "auto_authorized"
	confirmMethodUser    = "user_confirmed" // Xác nhận thủ công tường minh bằng cách nhấn y sau khi xem trước ở TUI
)

// Prompts là các system prompt của từng hàm ngữ nghĩa. Giai đoạn tổng hợp chia làm hai phần: Synthesize tạo BookSynthesis cho toàn bộ sách,
// Range tạo RangeDigest cho các đoạn chương liên tục của sách dài; hai bên có cấu trúc đầu ra khác nhau nên phải dùng prompt tương ứng riêng.
type Prompts struct {
	Segment    string
	Analyze    string
	Synthesize string
	Range      string
}

// RunBudgets là ngân sách đầu vào/đầu ra của các hàm ngữ nghĩa. Bản đầu dùng hằng số thận trọng;
// sau này nên suy ra từ context window / giới hạn completion của mô hình architect hiện tại, để lô tự nhiên lớn lên theo năng lực (RFC §9.2/§21).
type RunBudgets struct {
	MaxUnitBytes         int
	SegmentChunkBytes    int
	SegmentContextMargin int
	SegmentMaxTokens     int
	Analyze              AnalyzeBudget
	SynthesizeRangeBytes int
	SynthesizeMaxTokens  int
}

// DefaultRunBudgets trả về ngân sách mặc định thận trọng, làm phương án dự phòng khi năng lực mô hình chưa biết (dò dò thất bại).
func DefaultRunBudgets() RunBudgets {
	return RunBudgets{
		MaxUnitBytes:         8000,
		SegmentChunkBytes:    24000,
		SegmentContextMargin: 20,
		SegmentMaxTokens:     8192,
		Analyze:              AnalyzeBudget{ContextBytes: 24000, MaxOutputTokens: 8000, PerChapterOutput: 900, PromptOverhead: 2000},
		SynthesizeRangeBytes: 16000,
		SynthesizeMaxTokens:  8192,
	}
}

// ModelRuntime chứa các sự thật về năng lực mô hình cần cho các lời gọi ngữ nghĩa của imp, do Host nạp sau khi dò ở biên (RFC §13/§17).
// Nhờ đó cả hai ngân sách (đầu vào và đầu ra) tự nhiên lớn lên theo context/completion, còn thinking được gửi theo năng lực; khi toàn bộ giá trị bằng 0 thì lùi về mặc định thận trọng,
// hành vi giống hệt trước khi tích hợp năng lực. Đầu ra có cấu trúc không gửi response_format theo năng lực provider (xem chú thích của callProfile).
type ModelRuntime struct {
	ContextTokens   int                     // Giới hạn trên của ngữ cảnh đầu vào (token)
	MaxOutputTokens int                     // Giới hạn trên của đầu ra nhìn thấy được trong một lần (token)
	Thinking        agentcore.ThinkingLevel // Đã resolve theo năng lực; ThinkingAuto("") nghĩa là không gửi tường minh
}

// profile suy ra các tuỳ chọn năng lực gọi (thinking) của runtime này.
func (rt ModelRuntime) profile() callProfile {
	return callProfile{thinking: rt.Thinking}
}

// Caller là mức mô hình của một hàm ngữ nghĩa: mô hình + các sự thật về năng lực của mô hình đó (RFC §13.1/§17).
// segment/analyze/synthesize mỗi cái giữ một mức riêng, ngân sách và tuỳ chọn gọi đều suy ra theo mức của nó,
// cửa sổ nhỏ của mức rẻ chỉ ràng buộc hàm của chính nó, không làm nặng thêm các giai đoạn khác.
type Caller struct {
	Model   callModel
	Runtime ModelRuntime
}

// budgetsFromRuntime suy ra ngân sách của các hàm ngữ nghĩa từ giới hạn context/completion thật của mô hình (RFC §9.2/§21).
// Chỉ nhờ đó việc "đổi sang mô hình mạnh hơn thì tự động mở rộng lô, giảm số lần gọi" mới thành hiện thực; khi năng lực chưa biết thì lùi về mặc định thận trọng.
func budgetsFromRuntime(rt ModelRuntime) RunBudgets {
	if rt.ContextTokens <= 0 || rt.MaxOutputTokens <= 0 {
		return DefaultRunBudgets()
	}
	const bytesPerToken = 3 // Quy đổi thận trọng cho UTF-8 tiếng Trung: token→byte (đánh giá thấp dung lượng thì an toàn hơn)
	out := rt.MaxOutputTokens
	// Ngân sách đầu vào: trừ đầu ra nhìn thấy được và khoảng dự phòng ~10% cho suy luận/hệ thống khỏi ngữ cảnh rồi quy đổi sang byte.
	reserve := rt.ContextTokens / 10
	inTokens := rt.ContextTokens - out - reserve
	if inTokens < 2000 {
		inTokens = 2000
	}
	inBytes := inTokens * bytesPerToken
	return RunBudgets{
		MaxUnitBytes:         min(inBytes/2, 32000),
		SegmentChunkBytes:    inBytes,
		SegmentContextMargin: 20,
		SegmentMaxTokens:     out,
		Analyze: AnalyzeBudget{
			ContextBytes:     inBytes,
			MaxOutputTokens:  out,
			PerChapterOutput: 900,
			PromptOverhead:   2000,
		},
		SynthesizeRangeBytes: inBytes,
		SynthesizeMaxTokens:  out,
	}
}

// Confirmation là sản phẩm xác nhận kết quả cắt, gắn với segmentation hiện tại (RFC §8.4).
type Confirmation struct {
	Method   string `json:"method"`
	Chapters int    `json:"chapters"`
}

// StoryResolution là phán quyết của người dùng về trạng thái câu chuyện uncertain, gắn với synthesis hiện tại (RFC §10.4).
type StoryResolution struct {
	Choice string `json:"choice"` // open / closed
}

// Deps là các phụ thuộc hẹp của runner (RFC §17). Ba hàm ngữ nghĩa tự khai báo mức mô hình của mình;
// Host mặc định đặt tất cả vào architect, tầng cấu hình có thể trỏ các hàm mang tính cơ học sang mức rẻ hơn (RFC §13.1).
type Deps struct {
	Store         *store.Store
	CommitChapter ChapterCommitter
	Segment       Caller
	Analyze       Caller
	Synthesize    Caller // range digest và book synthesis cùng mức (cùng một giai đoạn tổng hợp)
	Prompts       Prompts
	Budgets       RunBudgets
}

// budgetsFromDeps suy ra ngân sách theo năng lực mức riêng của từng hàm ngữ nghĩa (RFC §9.2/§13.1).
func budgetsFromDeps(d Deps) RunBudgets {
	seg := budgetsFromRuntime(d.Segment.Runtime)
	ana := budgetsFromRuntime(d.Analyze.Runtime)
	syn := budgetsFromRuntime(d.Synthesize.Runtime)
	return RunBudgets{
		MaxUnitBytes:         seg.MaxUnitBytes,
		SegmentChunkBytes:    seg.SegmentChunkBytes,
		SegmentContextMargin: seg.SegmentContextMargin,
		SegmentMaxTokens:     seg.SegmentMaxTokens,
		Analyze:              ana.Analyze,
		SynthesizeRangeBytes: syn.SynthesizeRangeBytes,
		SynthesizeMaxTokens:  syn.SynthesizeMaxTokens,
	}
}

// Run chạy trọn vẹn đường ống nhập: LoadState → NextAction → thực hiện một hành động → đọc lại sự thật.
// Chạy trong goroutine riêng của nó; kênh sự kiện trả về do chính hàm này đóng.
func Run(ctx context.Context, deps Deps, opts Options) (<-chan Event, error) {
	if deps.Store == nil || deps.CommitChapter == nil ||
		deps.Segment.Model == nil || deps.Analyze.Model == nil || deps.Synthesize.Model == nil {
		return nil, fmt.Errorf("deps không đầy đủ")
	}
	if deps.Budgets == (RunBudgets{}) {
		deps.Budgets = budgetsFromDeps(deps)
	}
	// Nhật ký của luồng nhập tách thành tệp riêng: bản ghi đầy đủ của một lần nhập (sự kiện, lần thử lại, chuỗi lỗi đầy đủ) không trộn lẫn với
	// nhật ký của engine/TUI, khi truy vết chỉ cần xem đúng một tệp này. Hỏng khi tạo tệp phải hiển thị lại — bảng điều khiển sẽ hướng dẫn người dùng
	// xem logs/import.log, lùi im lặng tức là chỉ tới một tệp không tồn tại (Debug-First).
	log, closeLog, logErr := logger.FileLogger(deps.Store.Dir(), "import.log")
	log.Info("imp: runtime mô hình nhập",
		"segment_ctx", deps.Segment.Runtime.ContextTokens,
		"analyze_ctx", deps.Analyze.Runtime.ContextTokens,
		"synthesize_ctx", deps.Synthesize.Runtime.ContextTokens,
		"analyze_max_output", deps.Analyze.Runtime.MaxOutputTokens,
		"analyze_context_bytes", deps.Budgets.Analyze.ContextBytes)
	events := make(chan Event, 32)
	go func() {
		defer close(events)
		defer closeLog()
		r := &runner{deps: deps, opts: opts, events: events, ws: OpenWorkspace(deps.Store.Dir()), log: log}
		if logErr != nil {
			r.emit(StageIngesting, 0, 0, fmt.Sprintf("tạo tệp nhật ký nhập thất bại (%v), lần ghi lại này dùng nhật ký mặc định", logErr), nil)
		}
		r.run(ctx)
	}()
	return events, nil
}

type runner struct {
	deps   Deps
	opts   Options
	events chan Event
	ws     *Workspace
	act    Action       // Hành động đang thực thi, dùng để ghi chú giai đoạn cho sản phẩm thất bại
	log    *slog.Logger // Nhật ký riêng của luồng nhập (logs/import.log); nil thì lùi về logger mặc định
}

func (r *runner) emit(stage Stage, current, total int, msg string, err error) {
	r.send(Event{Time: time.Now(), Stage: stage, Current: current, Total: total, Message: msg, Err: err})
}

func (r *runner) send(ev Event) {
	r.logEvent(ev)
	// Sự kiện trạng thái cuối và điểm dừng mang tín hiệu thành bại/cần hành động duy nhất (mất bản xem trước xác nhận hoặc nhắc --story thì người dùng không biết phải làm gì),
	// phải được chuyển đến đáng tin cậy; chỉ sự kiện tiến trình ở giữa mới được bỏ qua khi tích tụ.
	if ev.Stage == StageError || ev.Stage == StageDone ||
		ev.Stage == StageAwaitingConfirmation || ev.Stage == StageAwaitingStoryStatus {
		r.events <- ev
		return
	}
	select {
	case r.events <- ev:
	default: // Khi kênh đầy thì bỏ tiến trình, tuyệt đối không chặn đường ống
	}
}

// logEvent ghi lại từng sự kiện tiến trình vào nhật ký riêng của luồng nhập (<thư mục gốc sách>/logs/import.log): dòng thử lại của bảng điều khiển bị ghi đè tại chỗ,
// bảng điều khiển biến mất khi nhấn Esc, nên nhật ký là bản ghi đầy đủ duy nhất có thể truy vết sau này (§14.1).
func (r *runner) logEvent(ev Event) {
	log := r.log
	if log == nil {
		log = slog.Default()
	}
	args := []any{"stage", string(ev.Stage)}
	if ev.Total > 0 {
		args = append(args, "progress", fmt.Sprintf("%d/%d", ev.Current, ev.Total))
	}
	if ev.Err != nil {
		args = append(args, "err", ev.Err)
	}
	level := slog.LevelInfo
	switch {
	case ev.Stage == StageError:
		level = slog.LevelError // Trạng thái cuối thất bại là dòng đáng lọc ra nhất trong nhật ký, không được rơi về INFO
	case ev.Level == "warn":
		level = slog.LevelWarn
	}
	log.Log(context.Background(), level, ev.Message, args...)
}

func (r *runner) fail(msg string, err error) {
	r.saveFailure(err)
	r.emit(StageError, 0, 0, msg, err)
}

// saveFailure gom mọi lỗi có mang theo phản hồi gốc vào failures/ (RFC §14.2 điểm đổ thứ ba),
// segment/synthesize và mọi hàm ngữ nghĩa khác đều dùng chung phương án dự phòng này; đường cứu vãn khi phân tích bị cắt ngắn đã ghi sẵn metadata tinh vi hơn tại chỗ.
// Lỗi không có phản hồi gốc (IO, huỷ, kiểm tra tiền điều kiện) không có đầu ra mô hình nào để lưu nên không ghi.
func (r *runner) saveFailure(err error) {
	var se *errSemantic
	var tr *errTruncated
	switch {
	case errors.As(err, &se):
		r.ws.writeFailure(FailureMeta{Stage: string(r.act), Detail: err.Error()}, se.Raw)
	case errors.As(err, &tr):
		r.ws.writeFailure(FailureMeta{Stage: string(r.act), Detail: err.Error(), StopReason: "length"}, tr.Raw)
	}
}

// facts tổng hợp sự thật của workspace với đối chiếu bản phát hành chính thức.
func (r *runner) facts() (Facts, error) {
	return CollectFacts(r.deps.Store, r.ws)
}

// profileFor suy ra các tuỳ chọn gọi của một mức và phản chiếu việc lùi yêu cầu / hỏi lại khi kiểm tra về luồng sự kiện của giai đoạn tương ứng——
// lùi thử lại có thể cộng dồn im lặng hơn 2 phút, không phản chiếu thì người dùng tưởng bị treo (§14.1).
// Key chỉ dành cho lùi yêu cầu (kèm mốc thời hạn): đó là trạng thái tức thời trong cùng một lời gọi, UI cập nhật tại chỗ một dòng ("lần N" nhảy số).
// Hỏi lại khi kiểm tra là sự kiện ngữ nghĩa xuyên suốt các lời gọi — cắt gọi theo từng khối, mỗi khối hỏi lại riêng, dùng chung Key sẽ khiến khối sau ghi đè khối trước,
// nuốt mất dấu vết truy vết (thực đo cho thấy bảng điều khiển chỉ còn một dòng unit_id liên tục đổi), nên mỗi cái giữ một dòng riêng để lưu lịch sử.
func (r *runner) profileFor(c Caller, stage Stage) callProfile {
	prof := c.Runtime.profile()
	prof.log = r.log
	prof.texts = r.modelTexts()
	prof.notify = func(msg string, retryAt time.Time) {
		ev := Event{Time: time.Now(), Stage: stage, Message: msg, Level: "warn", RetryAt: retryAt}
		if !retryAt.IsZero() {
			ev.Key = "retry:" + string(stage)
		}
		r.send(ev)
	}
	prof.progress = func(current, total int, msg string) {
		r.send(Event{Time: time.Now(), Stage: stage, Current: current, Total: total, Message: msg})
	}
	return prof
}

// modelTexts chọn bảng văn bản mô hình đọc theo ngôn ngữ tác phẩm.
// Đọc lazily từ store mỗi lần gọi để đổi ngôn ngữ giữa phiên có hiệu lực ngay.
func (r *runner) modelTexts() *importTexts {
	if r.deps.Store == nil {
		return importTextsVI
	}
	return importTextsFor(r.deps.Store.Language())
}

// applyGuidance lưu bền hướng dẫn tường minh của lần --guide này thành đầu vào ngữ nghĩa cho workspace (RFC §18.3).
// Hướng dẫn là một trong các đầu vào của InputDigest segmentation: nội dung đổi thì kết quả cắt cũ cùng toàn bộ phần sau của nó tự nhiên lệch khớp và được làm lại,
// không cần quy tắc vô hiệu hoá thủ công. Khi workspace chưa được tạo thì bỏ qua trước, vòng lặp kế sau ingest sẽ ghi.
func (r *runner) applyGuidance() error {
	g := strings.TrimSpace(r.opts.Guidance)
	if g == "" || !r.ws.Active() {
		return nil
	}
	existing, err := r.ws.LoadGuidance()
	if err != nil {
		return fmt.Errorf("đọc hướng dẫn cắt hiện có: %w", err)
	}
	if existing == g {
		return nil
	}
	// Sau khi phát hành bắt đầu, sản phẩm chính thức không thể bị ghi đè (§12.2): lúc này cắt lại chắc chắn sẽ đụng bức tường "từ chối ghi đè" ở bước publish,
	// hơn nữa trước khi đụng tường còn phải trả lại toàn bộ chuỗi lời gọi mô hình cho cắt/phân tích/tổng hợp — hãy đưa thất bại về sớm, nơi chi phí bằng 0.
	// book là lần ghi đầu tiên của phát hành, có nó nghĩa là phát hành đã bắt đầu (kiểm tra tiền điều kiện nhập bảo đảm sách vốn trống).
	book, err := r.deps.Store.Book.Load()
	if err != nil {
		return fmt.Errorf("đọc book chính thức: %w", err)
	}
	if book != nil {
		return fmt.Errorf("Foundation chính thức đã bắt đầu phát hành, --guide cắt lại sẽ xung đột với nội dung đã phát hành nên bị từ chối ghi đè, không còn nhận hướng dẫn cắt")
	}
	return r.ws.writeAtomic(fileGuidance, []byte(g))
}

// checkSourceIdentity chặn trường hợp "workspace đang chạy mà lại truyền tệp nguồn khác": ingest chỉ chạy khi chưa có workspace,
// nếu không đối chiếu, /import B.txt sẽ âm thầm tiếp tục từ điểm dừng của A, phát hành xong A mà B chưa đọc byte nào (RFC §12.1/§18.2).
// Truyền lại đường dẫn của cùng một tệp là thói quen phổ biến (/import cùng đường dẫn để khôi phục), nên so sánh theo tóm tắt nội dung thay vì từ chối mọi đường dẫn.
func (r *runner) checkSourceIdentity() error {
	if r.opts.SourcePath == "" || !r.ws.Active() {
		return nil
	}
	m, err := r.ws.LoadManifest()
	if err != nil {
		return nil // Bộ ba danh tính không đọc được thì đi theo chẩn đoán dữ liệu hỏng của ingest, không báo lỗi lần hai ở đây
	}
	raw, err := os.ReadFile(r.opts.SourcePath)
	if err != nil {
		return fmt.Errorf("đọc tệp nguồn %s: %w", r.opts.SourcePath, err)
	}
	if Digest(raw) != m.RawSHA256 {
		return fmt.Errorf("đang có bản nhập của %q, tệp nguồn lần này khác nội dung: hãy hoàn tất hoặc huỷ bản nhập cũ (xoá meta/import/) rồi nhập cuốn sách mới", m.SourceName)
	}
	return nil
}

func (r *runner) run(ctx context.Context) {
	if err := r.checkSourceIdentity(); err != nil {
		r.fail("kiểm tra danh tính tệp nguồn", err)
		return
	}
	var previous *Facts
	for {
		if ctx.Err() != nil {
			r.fail("người dùng đã huỷ", ctx.Err())
			return
		}
		if err := r.applyGuidance(); err != nil {
			r.fail("ghi hướng dẫn cắt", err)
			return
		}
		facts, err := r.facts()
		if err != nil {
			r.fail("đọc trạng thái nhập", err)
			return
		}
		if previous != nil && facts == *previous {
			r.fail("nhập bị đứng yên", fmt.Errorf("sau khi thực hiện hành động, sự thật không thay đổi, hành động kế tiếp vẫn là %q", NextAction(facts)))
			return
		}
		snapshot := facts
		previous = &snapshot
		act := NextAction(facts)
		r.act = act
		err = nil
		switch act {
		case ActionIngest:
			err = r.ingest(ctx)
		case ActionSegment:
			err = r.segment(ctx)
		case ActionAwaitConfirmation:
			if !r.confirm() {
				return // Chế độ tương tác: chờ người dùng xác nhận, dừng tại đây
			}
		case ActionAnalyze:
			err = r.analyze(ctx)
		case ActionSynthesize:
			err = r.synthesize(ctx)
		case ActionAwaitStoryResolution:
			if !r.resolveStoryStatus() {
				return // Không có phán quyết tường minh: dừng tại đây, chờ --story=open|closed
			}
		case ActionPublish:
			err = r.publish(ctx)
		case ActionDone:
			r.emit(StageDone, 0, 0, "nhập hoàn tất, chờ nghiệm thu để viết tiếp", nil)
			return
		default:
			err = fmt.Errorf("hành động không xác định %q", act)
		}
		if err != nil {
			r.fail("nhập thất bại", err)
			return
		}
	}
}

func (r *runner) ingest(ctx context.Context) error {
	// Đã đi tới ingest mà thư mục vẫn tồn tại = bộ ba danh tính (manifest/source/intent) thiếu hoặc hỏng:
	// createWorkspace sẽ từ chối bằng thông điệp "đã tồn tại (/import không tham số có thể khôi phục)", còn lần chạy lại không tham số lại quay về đây vì
	// WorkspaceReady=false và đòi đường dẫn nguồn — hai thông điệp cấn nhau, người dùng không có lối nào đi.
	if r.ws.Active() {
		return fmt.Errorf("meta/import/ đã tồn tại nhưng danh tính không gian làm việc không dùng được (manifest/source/intent thiếu hoặc hỏng), hãy kiểm tra thủ công rồi xoá thư mục đó và nhập lại")
	}
	if err := checkImportPreconditions(r.deps.Store); err != nil {
		return err
	}
	if r.opts.SourcePath == "" {
		return fmt.Errorf("nhập mới cần đường dẫn tệp nguồn")
	}
	r.emit(StageIngesting, 0, 0, "đọc, giải mã, chuẩn hoá và chụp ảnh tệp nguồn...", nil)
	_, m, err := Ingest(r.deps.Store.Dir(), r.opts.SourcePath, r.opts.intent())
	if err != nil {
		return err
	}
	r.emit(StageIngesting, 0, 0, fmt.Sprintf("ảnh chụp tệp nguồn sẵn sàng: %s (mã hóa %s, %d byte)", m.SourceName, m.Encoding, m.SizeBytes), nil)
	return nil
}

func (r *runner) segment(ctx context.Context) error {
	src, err := r.ws.LoadSource()
	if err != nil {
		return err
	}
	units := buildSourceUnits(src, r.deps.Budgets.MaxUnitBytes)
	guidance, err := r.ws.LoadGuidance()
	if err != nil {
		return fmt.Errorf("đọc hướng dẫn cắt: %w", err)
	}
	r.emit(StageSegmenting, 0, 0, fmt.Sprintf("nhận diện ranh giới chương theo ngữ nghĩa (%d đơn vị tọa độ)...", len(units)), nil)
	digest := segmentInputDigest(Digest(src), guidance, segmentPromptVersion)
	// Danh tính bộ nhớ đệm khối còn gắn thêm MaxUnitBytes: bảng unit được xác định duy nhất bởi (nguồn đã chuẩn hoá, MaxUnitBytes), đổi mức
	// mô hình làm MaxUnitBytes đổi theo và sẽ dựng lại các lát ảo của dòng quá dài — chuỗi ID (L1.1…) và các đầu cuối khối vẫn tái lập được nhưng phạm vi byte
	// đã đổi, chỉ khớp theo ID đầu cuối sẽ tái sử dụng ranh giới cũ lệch chỗ (anchor lệch sẽ thất bại tất định, hoặc cắt sai trong im lặng).
	chunkIdentity := fmt.Sprintf("%s\x00units:%d", digest, r.deps.Budgets.MaxUnitBytes)
	seg, err := Segment(ctx, r.deps.Segment.Model, r.deps.Prompts.Segment, src, units, guidance,
		r.deps.Budgets.SegmentChunkBytes, r.deps.Budgets.SegmentContextMargin, r.deps.Budgets.SegmentMaxTokens,
		r.profileFor(r.deps.Segment, StageSegmenting), r.ws, chunkIdentity)
	if err != nil {
		return err
	}
	if err := writeArtifact(r.ws, fileSegmentation, digest, *seg); err != nil {
		return err
	}
	// Kết quả cắt cuối cùng đã ghi xuống đĩa, bộ nhớ đệm cấp khối đã hoàn thành sứ mệnh; dọn dẹp thất bại không ảnh hưởng tính đúng đắn (digest vẫn khớp), nhưng phải để lại dấu vết.
	if cerr := r.ws.clearDir(dirSegmentChunks); cerr != nil {
		r.emit(StageSegmenting, 0, 0, fmt.Sprintf("dọn bộ nhớ đệm cấp khối thất bại (không ảnh hưởng kết quả cắt): %v", cerr), nil)
	}
	r.emit(StageSegmenting, len(seg.Chapters), len(seg.Chapters),
		fmt.Sprintf("cắt xong: %d chương, %d vùng phụ", len(seg.Chapters), len(seg.Matter)), nil)
	return nil
}

// confirm xử lý việc xác nhận kết quả cắt. --yes tự động chấp nhận và ghi sản phẩm confirmation; nếu không thì hiển thị bản xem trước và dừng.
func (r *runner) confirm() bool {
	seg, err := readArtifact[Segmentation](r.ws, fileSegmentation)
	if err != nil {
		r.fail("đọc kết quả cắt", err)
		return false
	}
	in, err := r.ws.LoadIntent()
	if err != nil {
		r.fail("đọc ý định nhập", err)
		return false
	}
	accept := r.opts.AcceptSegmentation
	auto := r.opts.AutoConfirm || (in != nil && in.AutoConfirm)
	// Kết quả cắt đã trải qua bước dung hòa ngữ nghĩa (Notes khác rỗng: hấp thụ chương rỗng / dự phòng đầu văn bản / khử trùng lặp chồng lấn) không được --yes bỏ qua mù quáng:
	// cấu trúc đã bị viết lại một cách tất định, bắt buộc phải có người kiểm tra — nếu không thì ghi chú dung hòa dưới --yes chẳng ai đọc, tức là viết lại trong im lặng.
	// Đã xem trước ở TUI rồi nhấn y thì đi theo AcceptSegmentation (phán quyết tường minh sau khi đã xem trước), không bị giới hạn này.
	blockedByNotes := auto && !accept && len(seg.Payload.Notes) > 0
	if blockedByNotes {
		auto = false
	}
	if !auto && !accept {
		msg := buildConfirmPreview(&seg.Payload)
		if blockedByNotes {
			msg += "  ! có ghi chú dung sai khi cắt, --yes không tự động duyệt, hãy kiểm tra thủ công\n"
		}
		r.emit(StageAwaitingConfirmation, len(seg.Payload.Chapters), len(seg.Payload.Chapters), msg, nil)
		return false
	}
	raw, err := r.ws.readBytes(fileSegmentation)
	if err != nil {
		r.fail("đọc sản phẩm cắt", err)
		return false
	}
	method, doneMsg := confirmMethodAuto, "đã tự động chấp nhận kết quả cắt (--yes)"
	if accept {
		method, doneMsg = confirmMethodUser, "đã xác nhận kết quả cắt (kiểm tra thủ công)"
	}
	conf := Confirmation{Method: method, Chapters: len(seg.Payload.Chapters)}
	if err := writeArtifact(r.ws, fileConfirmation, Digest(raw), conf); err != nil {
		r.fail("ghi sản phẩm xác nhận", err)
		return false
	}
	r.emit(StageAwaitingConfirmation, len(seg.Payload.Chapters), len(seg.Payload.Chapters), doneMsg, nil)
	return true
}

// buildConfirmPreview dựng bản xem trước để xác nhận kết quả cắt: số chương, vùng phụ, toàn bộ tiêu đề chương và dấu uncertain (RFC §8.4).
// Liệt kê đầy đủ, viewport của bảng điều khiển có thể cuộn xem; không đặt giới hạn cắt ngắn.
func buildConfirmPreview(seg *Segmentation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "đã cắt %d chương", len(seg.Chapters))
	if len(seg.Matter) > 0 {
		fmt.Fprintf(&b, ", %d vùng phụ", len(seg.Matter))
	}
	if len(seg.Uncertain) > 0 {
		fmt.Fprintf(&b, " (%d chương đáng ngờ)", len(seg.Uncertain))
	}
	b.WriteString(", hãy kiểm tra:\n")
	uncertain := make(map[int]bool, len(seg.Uncertain))
	for _, n := range seg.Uncertain {
		uncertain[n] = true
	}
	for _, c := range seg.Chapters {
		fmt.Fprintf(&b, "  Chương %d: %s", c.Number, c.Title)
		if uncertain[c.Number] {
			b.WriteString("  [đáng ngờ]")
		}
		b.WriteByte('\n')
	}
	for _, mt := range seg.Matter {
		fmt.Fprintf(&b, "  [%s] %s\n", mt.Kind, mt.Title)
	}
	// Ghi chú dung hòa của giai đoạn cắt (ví dụ tiêu đề giữ chỗ nội dung rỗng được gộp vào đoạn trước) nhất định phải hiện ra ở điểm dừng chờ người, nếu không hành vi hấp thụ sẽ thành viết lại trong im lặng.
	for _, n := range seg.Notes {
		fmt.Fprintf(&b, "  ! %s\n", n)
	}
	// Gợi ý thao tác (y để xác nhận / --guide để cắt lại / Esc) do khối tạm dừng của TUI dựng thống nhất, ở đây chỉ giữ sự thật, tránh hai bản văn trôi lệch nhau.
	return b.String()
}

func (r *runner) analyze(ctx context.Context) error {
	src, err := r.ws.LoadSource()
	if err != nil {
		return err
	}
	segArt, err := readArtifact[Segmentation](r.ws, fileSegmentation)
	if err != nil {
		return err
	}
	seg := &segArt.Payload
	total := len(seg.Chapters)
	// Digest từng chương chỉ gắn với nội dung của chính chương đó, không chứa ngữ cảnh lô hay ledger của các chương trước. Nếu chương thứ K bị thiếu/lệch khớp nên phải phân tích lại,
	// các sản phẩm cũ còn nằm sau đó với digest vừa khớp sẽ bị tái sử dụng kèm ledger đã mất hiệu lực. Trước khi bắt đầu phân tích, dọn phần đuôi vượt quá tiền tố còn tươi,
	// ép "phân tích lại một chương thì mất hiệu lực mọi phân tích sau đó", để các lượt phân tích tiếp theo không tạo ra đuôi dữ liệu cũ nữa (RFC §9.6 / #4a).
	if err := discardAnalysesAfter(r.ws, analyzedChapters(r.ws, seg, src, segArt.InputDigest, analyzePromptVersion), total); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		start := analyzedChapters(r.ws, seg, src, segArt.InputDigest, analyzePromptVersion)
		if start >= total {
			break
		}
		r.emit(StageAnalyzing, start, total, fmt.Sprintf("phân tích lô liên tục từ chương %d...", start+1), nil)
		done, err := AnalyzeNext(ctx, r.deps.Analyze.Model, r.deps.Prompts.Analyze, r.ws, src, seg, segArt.InputDigest, analyzePromptVersion, r.deps.Budgets.Analyze, r.profileFor(r.deps.Analyze, StageAnalyzing))
		if err != nil {
			return err
		}
		if done == 0 {
			break
		}
	}
	r.emit(StageAnalyzing, total, total, "đã trích xuất sự thật theo từng chương", nil)
	return nil
}

func (r *runner) synthesize(ctx context.Context) error {
	segArt, err := readArtifact[Segmentation](r.ws, fileSegmentation)
	if err != nil {
		return err
	}
	total := len(segArt.Payload.Chapters)
	facts := loadPriorFacts(r.ws, total)
	if len(facts) != total {
		return fmt.Errorf("phân tích theo từng chương chưa đầy đủ: %d/%d", len(facts), total)
	}
	r.emit(StageSynthesizing, 0, total, "tổng hợp ngữ nghĩa toàn tác phẩm theo tầng...", nil)
	syn, err := Synthesize(ctx, r.deps.Synthesize.Model, r.deps.Prompts.Synthesize, r.deps.Prompts.Range, r.ws, facts,
		r.deps.Budgets.SynthesizeRangeBytes, r.deps.Budgets.SynthesizeMaxTokens, r.profileFor(r.deps.Synthesize, StageSynthesizing))
	if err != nil {
		return err
	}
	if err := writeArtifact(r.ws, fileSynthesis, synthesisInputDigest(facts), *syn); err != nil {
		return err
	}
	r.emit(StageSynthesizing, total, total, fmt.Sprintf("tổng hợp xong: %d tập, trạng thái câu chuyện %s", len(syn.Structure), syn.StoryStatus), nil)
	return nil
}

func (r *runner) publish(ctx context.Context) error {
	synArt, err := readArtifact[BookSynthesis](r.ws, fileSynthesis)
	if err != nil {
		return err
	}
	segArt, err := readArtifact[Segmentation](r.ws, fileSegmentation)
	if err != nil {
		return err
	}
	seg := &segArt.Payload
	src, err := r.ws.LoadSource()
	if err != nil {
		return err
	}
	total := len(seg.Chapters)
	facts := loadPriorFacts(r.ws, total)
	if len(facts) != total {
		return fmt.Errorf("phân tích trước khi phát hành chưa đầy đủ: %d/%d", len(facts), total)
	}
	closed, err := r.resolveStory(&synArt.Payload)
	if err != nil {
		return err
	}
	manifest, err := r.ws.LoadManifest()
	if err != nil {
		return err
	}
	f, err := AssembleFoundation(&synArt.Payload, facts, closed, manifest.SourceName)
	if err != nil {
		return err
	}
	r.emit(StageValidating, 0, total, "lắp ráp Foundation đã qua kiểm tra", nil)

	r.emit(StagePublishing, 0, total, "phát hành Foundation chính thức...", nil)
	if err := publishFoundation(r.deps.Store, f); err != nil {
		return err
	}
	// Hold hoàn tất nhập phải được ghi bền sớm hơn mọi lần ghi chương: nếu sập giữa "ghi chương cuối" và "đặt Hold",
	// sau khi khởi động lại isPublished=true → nhập bị coi là đã hoàn tất nhưng lại quên đặt Hold, Engine sẽ tưởng là cuốn sách nhập rồi viết tiếp như một lần dừng bình thường.
	// Đặt nó sau publishFoundation (đã khởi tạo RunMeta) và trước khi ghi chương, đóng hẳn khoảng cửa này; khi chạy lại phát hành thì đặt lại một cách idempotent
	// (--continue không đặt Hold, để cơ chế tiếp sức tự động lo, RFC §12.4).
	if err := r.setCompletionHold(); err != nil {
		return fmt.Errorf("thiết lập Hold hoàn tất nhập: %w", err)
	}
	for i, c := range seg.Chapters {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.emit(StagePublishing, c.Number, total, fmt.Sprintf("phát hành chương %d/%d: %s", c.Number, total, c.Title), nil)
		if err := publishChapter(ctx, r.deps.Store, r.deps.CommitChapter, c.Number, seg.Content(src, i), facts[i]); err != nil {
			return err
		}
	}
	return nil
}

// storyChoice trả về phán quyết hợp lệ cho trạng thái uncertain: ưu tiên phán quyết đã ghi xuống đĩa và gắn với synthesis hiện tại, tiếp theo là opts của lần này, sau nữa là intent gốc.
// Phán quyết đã ghi xuống đĩa phải được kiểm tra rằng InputDigest khớp với synthesis hiện tại — sau khi tổng hợp lại, phán quyết cũ mất hiệu lực, không được lặng lẽ
// gắn open/closed cũ lên kết quả mới, nếu không người dùng sẽ không được hỏi lại (RFC §10.4). --story tường minh (intent) là chỉ dẫn thường trực của người dùng xuyên suốt các lần tổng hợp nên vẫn giữ được.
func (r *runner) storyChoice() (string, error) {
	if raw, err := r.ws.readBytes(fileSynthesis); err == nil {
		if art, aerr := readArtifact[StoryResolution](r.ws, fileStoryResolve); aerr == nil && art.InputDigest == Digest(raw) {
			return art.Payload.Choice, nil
		} else if aerr != nil && !os.IsNotExist(aerr) {
			return "", fmt.Errorf("đọc phán quyết trạng thái câu chuyện: %w", aerr)
		}
	} else {
		return "", fmt.Errorf("đọc sản phẩm tổng hợp: %w", err)
	}
	if r.opts.StoryResolution != "" {
		return r.opts.StoryResolution, nil
	}
	in, err := r.ws.LoadIntent()
	if err != nil {
		return "", fmt.Errorf("đọc ý định nhập: %w", err)
	}
	return in.StoryResolution, nil
}

// resolveStoryStatus ghi story-resolution.json xuống đĩa khi trạng thái uncertain và đã có phán quyết tường minh (gắn với synthesis hiện tại),
// để NextAction phía sau tự nhiên cho đi; không có phán quyết thì hiển thị trạng thái chờ và dừng.
func (r *runner) resolveStoryStatus() bool {
	choice, err := r.storyChoice()
	if err != nil {
		r.fail("đọc phán quyết trạng thái câu chuyện", err)
		return false
	}
	if choice != storyOpen && choice != storyClosed {
		r.emit(StageAwaitingStoryStatus, 0, 0, "tổng hợp xác định trạng thái câu chuyện là uncertain, hãy dùng --story=open|closed để làm rõ rồi thử lại", nil)
		return false
	}
	raw, err := r.ws.readBytes(fileSynthesis)
	if err != nil {
		r.fail("đọc kết quả tổng hợp", err)
		return false
	}
	if err := writeArtifact(r.ws, fileStoryResolve, Digest(raw), StoryResolution{Choice: choice}); err != nil {
		r.fail("ghi phán quyết trạng thái câu chuyện xuống đĩa", err)
		return false
	}
	return true
}

// resolveStory cho ra trạng thái khép lại câu chuyện dựa trên kết quả tổng hợp và phán quyết tường minh của người dùng (RFC §10.4).
func (r *runner) resolveStory(syn *BookSynthesis) (bool, error) {
	switch syn.StoryStatus {
	case storyClosed:
		return true, nil
	case storyOpen:
		return false, nil
	case storyUncertain:
		choice, err := r.storyChoice()
		if err != nil {
			return false, err
		}
		switch choice {
		case storyClosed:
			return true, nil
		case storyOpen:
			return false, nil
		default:
			return false, fmt.Errorf("trạng thái câu chuyện uncertain, cần --story=open|closed")
		}
	default:
		return false, fmt.Errorf("story_status không xác định: %q", syn.StoryStatus)
	}
}

// setCompletionHold đặt Hold cho một lần nhập đã hoàn tất; chỉ --continue mới bỏ qua (RFC §12.4).
// Lỗi phải được truyền ra — Hold là biện pháp bảo vệ duy nhất cho "sau khi nhập không được viết tiếp nhầm", thất bại trong im lặng đồng nghĩa với lớp bảo vệ mất hiệu lực.
func (r *runner) setCompletionHold() error {
	in, err := r.ws.LoadIntent()
	if err != nil {
		return fmt.Errorf("đọc ý định nhập: %w", err)
	}
	if r.opts.ContinueAfter || (in != nil && in.ContinueAfterImport) {
		return nil
	}
	return r.deps.Store.RunMeta.SetAdvanceHold(domain.AdvanceHold{
		After:  domain.AdvanceHoldAtBoundary,
		Reason: "nhập tiểu thuyết bên ngoài hoàn tất, chờ nghiệm thu để viết tiếp",
	})
}
