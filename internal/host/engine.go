package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/subagent"

	"github.com/voocel/ainovel-cli/internal/arbiter"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/flow"
	"github.com/voocel/ainovel-cli/internal/notify"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/tools"
)

// engine là công cụ thực thi tất định: đọc sự thật → Route → kiểm tra tiền điều kiện → chạy Worker trực tiếp →
// kiểm tra tiến triển → vòng lặp; các tình huống ngữ nghĩa được hỏi Arbiter khi cần. Nó thi hành quyết định, không tham gia phán đoán văn học
// (docs/engine-rfc.md). Một goroutine duy nhất chạy tuần tự, trạng thái điều khiển chỉ đổi ở ranh giới vòng lặp.
type engine struct {
	store   *storepkg.Store
	workers *subagent.Runner

	arbiterModel    agentcore.ChatModel
	failurePrompt   string
	planStartPrompt string // system prompt cho phán quyết khởi động: khi phán quyết chưa hoàn tất, engine bổ sung phán quyết tại chỗ dựa trên StartPrompt
	style           string // tên phong cách, truyền cho DecidePlanStart khi bổ sung phán quyết
	// reconsult trả can thiệp đã lỗi thời về đường phán quyết đầy đủ của host (lưu trữ/kiểm toán/áp dụng toàn bộ hành động),
	// chạy bất đồng bộ — engine chỉ loại bỏ đơn phái đã lỗi thời, không tự phán quyết lại thiếu sót.
	reconsult func(text string)

	observer  *observer
	budget    *BudgetSentinel
	gate      *ChapterAdvanceGate
	refresh   func() // làm mới RestorePack trước mỗi lần phái writer
	emitEvent func(Event)
	notify    func(kind, level, title, body string)
	onPause   func(summary string) // engine tự tạm dừng (bế tắc / abort do phán quyết thất bại): đi theo ngữ nghĩa tạm dừng thống nhất của host (lifecycle=paused)
	onDone    func()               // run kết thúc (bất kể lý do); host xác định trạng thái cuối theo sự thật trong store

	mu      sync.Mutex
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	running bool
	pending []controlOp       // hành động điều khiển của can thiệp, commit tại ranh giới
	next    *flow.Instruction // chỉ dẫn ưu tiên cho vòng kế tiếp (plan_start / arbiter dispatch)
	// deferGateForNext chỉ đi cùng next: hold+dispatch phải chạy cặp
	// editor/writer trước để nó lập hàng đợi làm lại, sau đó Gate mới kiểm tra được rewrites_drained.
	deferGateForNext bool

	// Theo dõi bế tắc: sau mỗi lượt thực thi, nếu Route vẫn sinh cùng khoá chỉ dẫn thì bộ đếm tăng.
	// Chỉ dẫn của Router là hình chiếu của hậu điều kiện nhiệm vụ; khi nhiệm vụ thực sự xong thì chỉ dẫn kế tiếp sẽ khác.
	lastKey string
	repeats int
	// Thử lại khi thất bại: cùng một khoá chỉ dẫn chỉ thử lại một lần, thất bại lần nữa thì hỏi Arbiter.
	failedKey string
	// Giữ lỗi Worker gần nhất của cùng chỉ dẫn, để phán quyết bế tắc nhìn thấy nguyên nhân thất bại thật.
	lastWorkerErrorKey string
	lastWorkerError    error
}

// deadlockConsultAt / deadlockAbortAt: khi repeats chạm mốc trước thì hỏi Arbiter, chạm mốc sau thì ngắt cứng.
// Engine tất định phải đặt trần rõ ràng cho vòng lặp không tiến triển (RFC §5).
const (
	deadlockConsultAt = 3
	deadlockAbortAt   = 5
)

// controlOp là hành động thay đổi trạng thái điều khiển trong phán quyết can thiệp (commit tại ranh giới; RFC §3).
// text/facts giữ nguyên ngữ cảnh hỏi ban đầu: khi dispatch không khớp thì hỏi lại với sự thật mới.
type controlOp struct {
	hold     *arbiter.AdvanceHoldOp
	reopen   *arbiter.ReopenOp
	dispatch *arbiter.DispatchOp
	text     string
	facts    arbiter.InterventionFacts
}

// start khởi chạy vòng lặp của engine; nếu đang chạy thì không làm gì (trả về false).
func (e *engine) start(initial *flow.Instruction) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	ctx = agentcore.WithToolProgress(ctx, e.observer.workerProgress)
	e.cancel = cancel
	e.running = true
	// Khi initial rỗng thì không ghi đè e.next — trong lúc dừng, can thiệp có thể đã được applyControlOp xếp vào
	// đơn phái phán quyết (ví dụ editor làm lại); start(nil) xóa nó sẽ khiến Route phái writer viết tiếp,
	// trái với ý định của người dùng.
	if initial != nil {
		e.next = initial
		e.deferGateForNext = false
	}
	e.lastKey, e.repeats, e.failedKey = "", 0, ""
	e.lastWorkerErrorKey, e.lastWorkerError = "", nil
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.run(ctx)
	}()
	return true
}

// abort huỷ vòng lặp hiện tại (ngữ nghĩa tạm dừng; checkpoint bảo đảm không mất dữ liệu).
func (e *engine) abort() {
	e.mu.Lock()
	cancel := e.cancel
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// wait chờ goroutine Engine hiện tại thoát hẳn. Host.Close sẽ cancel trước rồi gọi nó,
// bảo đảm công cụ ghi và runEnded đã kết thúc trước khi đóng kênh sự kiện và thoát tiến trình.
func (e *engine) wait() {
	e.wg.Wait()
}

func (e *engine) isRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// enqueue xếp hành động điều khiển của can thiệp vào hàng đợi ranh giới (khi engine đang chạy); trả về false nghĩa là chưa chạy,
// phía gọi nên tự thực hiện ngay.
func (e *engine) enqueue(op controlOp) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		return false
	}
	e.pending = append(e.pending, op)
	return true
}

func (e *engine) run(ctx context.Context) {
	defer func() {
		e.mu.Lock()
		e.running = false
		e.cancel = nil
		leftover := e.pending
		e.pending = nil
		e.mu.Unlock()
		// Tranh chấp lúc thoát: hành động can thiệp còn sót khi enqueue chạy song song với lúc thoát không được âm thầm bị bỏ —
		// hold/reopen là ghi sự thật idempotent, dùng ctx riêng để thực thi bù; dispatch không có engine để phái,
		// khôi phục PendingSteer đã lưu (host có thể đã xóa theo “đã xếp hàng”), lần sau
		// Resume/Continue phát lại toàn bộ can thiệp.
		for _, op := range leftover {
			if op.dispatch != nil {
				if op.text != "" {
					if err := e.store.RunMeta.SetPendingSteer(op.text); err != nil {
						slog.Warn("lưu lại can thiệp còn sót thất bại", "module", "engine", "err", err)
					}
				}
				e.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Level: "warn",
					Summary: "engine đã dừng, đơn phái phán quyết chưa thực thi; can thiệp đã được giữ, sẽ tự phán quyết lại khi tiếp tục sáng tác"})
				op.dispatch = nil
			}
			if op.hold != nil || op.reopen != nil {
				if err := e.applyControlOp(context.Background(), op); err != nil {
					e.emitEvent(Event{Time: time.Now(), Category: "ERROR", Level: "error",
						Summary: "bổ sung can thiệp lúc engine thoát thất bại: " + err.Error()})
				}
			}
		}
		e.onDone()
	}()

	for {
		if ctx.Err() != nil {
			return
		}
		// hold+dispatch phải để cặp phái tạo sự thật làm lại trước; các trường hợp khác đều kiểm tra thống nhất trước khi phái
		// Gate, để boundary hold và review không có giấy phép không chạy thừa một Worker.
		deferGate := e.applyPendingOps(ctx) || e.nextDefersGate()
		if !deferGate {
			if e.gate.HandleBoundary() {
				return
			}
		}

		inst := e.takeNext()
		if inst == nil {
			state, err := flow.LoadState(e.store)
			if err != nil {
				e.pauseWithNotify(notify.KindWorkerFailure, "đọc sự thật định tuyến thất bại, đã tạm dừng: "+err.Error())
				return
			}
			// Tóm tắt tập có thể đã lưu mà tiến trình chưa kịp MarkComplete. Khi toàn bộ hiện vật tổng hợp đã đủ thì
			// trước hết bù phán đoán hoàn tất theo sự thật, rồi mới giao cho Router, tránh tập khép lại bị phái nhầm đi viết tập sau.
			if state.AggregateRefresh == nil && state.Progress != nil && state.Progress.Layered &&
				state.Progress.Phase == domain.PhaseWriting && state.ArcBoundary != nil &&
				state.ArcBoundary.IsVolumeEnd && state.HasArcReview && state.HasArcSummary && state.HasVolumeSummary {
				complete, reconcileErr := tools.ReconcileLayeredCompletion(e.store)
				if reconcileErr != nil {
					e.pauseWithNotify(notify.KindWorkerFailure, "khôi phục trạng thái hoàn tất thất bại, đã tạm dừng: "+reconcileErr.Error())
					return
				}
				if complete {
					continue
				}
			}
			inst = flow.Route(state)
		}
		if inst == nil {
			var err error
			inst, err = e.planStartFallback(ctx)
			if err != nil {
				e.pauseWithNotify(notify.KindPlanStart, "đọc sự thật khôi phục quy hoạch thất bại, đã tạm dừng: "+err.Error())
				return
			}
		}
		if inst == nil {
			// Tình huống ngữ nghĩa hoặc trạng thái cuối: đã xong → chốt tất định; các trường hợp còn lại (dư lượng Steering v.v.)
			// → dừng tự nhiên, chờ người dùng Continue / can thiệp.
			return
		}
		replaced, err := e.precheck(inst)
		if err != nil {
			e.pauseWithNotify(notify.KindWorkerFailure, "kiểm tra tiền điều kiện phái lệnh thất bại, đã tạm dừng: "+err.Error())
			return
		}
		if replaced != nil {
			inst = replaced
		}
		allowed, gateErr := e.gate.Allow(inst)
		if gateErr != nil {
			e.pauseWithNotify(notify.KindAdvanceGate, "lỗi điều khiển tiến triển chương, đã tạm dừng: "+gateErr.Error())
			return
		}
		if !allowed {
			return
		}
		if stop := e.trackDeadlock(ctx, &inst); stop {
			return
		}
		if inst == nil {
			continue // phán quyết bế tắc yêu cầu tính lại định tuyến
		}

		err = e.runWorker(ctx, inst)
		if ctx.Err() != nil {
			return
		}
		e.rememberWorkerError(inst, err)
		if err != nil {
			// trackDeadlock ghi nhận trước lần thử này khi phái lệnh. Lỗi chưa bước vào thực thi Worker hợp lệ
			// không được tính là “cùng một nhiệm vụ không tiến triển”.
			e.discardNonSemanticDeadlockAttempt(inst, err)
			if stop := e.handleWorkerError(ctx, inst, err); stop {
				return
			}
		}

		// Ranh giới chính sách: cắt lỗi theo ngân sách được ưu tiên trước tạm dừng do nghiệm thu/tiến triển.
		if e.budget.HandleBoundary() {
			return
		}
		if e.gate.HandleBoundary() {
			return
		}
	}
}

func (e *engine) takeNext() *flow.Instruction {
	e.mu.Lock()
	defer e.mu.Unlock()
	inst := e.next
	e.next = nil
	e.deferGateForNext = false
	return inst
}

func (e *engine) nextDefersGate() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.next != nil && e.deferGateForNext
}

// planStartFallback phủ hai khoảng trống: sự thật quy hoạch thiếu, và Route không suy ra được nhà quy hoạch:
//  1. Phán quyết đã lưu nhưng save_foundation đầu tiên chưa xảy ra → chạy tiếp theo PlanStartRecord đã đóng băng,
//     không phán quyết lại (RFC §6); sau khi foundation đầu tiên lưu thì tier đã đủ, nhánh bù sẽ tiếp quản.
//  2. Phán quyết chưa bao giờ hoàn tất (lỗi mô hình lúc khởi động) nhưng có sự thật đầu vào StartPrompt → bổ sung phán quyết tại chỗ.
//     Đây là thử lại phán quyết đầu tiên, không vi phạm “khôi phục không phụ thuộc phán quyết lại” — mệnh đề đó nói về phán quyết đã tồn tại.
//     Bổ sung phán quyết thất bại thì tạm dừng tường minh: khởi động thất bại không được dừng lặng lẽ.
func (e *engine) planStartFallback(ctx context.Context) (*flow.Instruction, error) {
	progress, err := e.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("load progress: %w", err)
	}
	if progress == nil {
		return nil, nil
	}
	if progress.Phase == domain.PhaseWriting || progress.Phase == domain.PhaseComplete {
		return nil, nil
	}
	meta, err := e.store.RunMeta.Load()
	if err != nil {
		return nil, fmt.Errorf("load run meta: %w", err)
	}
	if meta == nil || meta.PlanningTier != "" {
		return nil, nil
	}
	missing, err := e.store.FoundationMissing()
	if err != nil {
		return nil, fmt.Errorf("load foundation state: %w", err)
	}
	if len(missing) == 0 {
		return nil, nil
	}
	if meta.PlanStart != nil {
		return &flow.Instruction{
			Agent:  meta.PlanStart.Planner,
			Task:   meta.PlanStart.PlannerTask,
			Reason: "bắt đầu quy hoạch theo phán quyết khởi động đã đóng băng",
		}, nil
	}
	if meta.StartPrompt == "" {
		return nil, nil
	}
	return e.retryPlanStart(ctx, meta.StartPrompt), nil
}

// retryPlanStart bổ sung và đóng băng quyết định khởi động (phán quyết ghi sự thật trước rồi mới thực thi, đồng cấu trúc với StartPrepared).
func (e *engine) retryPlanStart(ctx context.Context, prompt string) *flow.Instruction {
	start := time.Now()
	decision, derr := runObservedDecision(e.observer, "Bổ sung phán quyết khởi động", func() (arbiter.PlanStartDecision, error) {
		return arbiter.DecidePlanStart(ctx, e.arbiterModel, e.planStartPrompt, prompt, e.style)
	})
	rec := storepkg.DecisionRecord{Kind: "plan_start", Decider: "arbiter", Input: prompt,
		Reason: decision.Reason, DurationMs: time.Since(start).Milliseconds()}
	if derr == nil {
		if data, err := json.Marshal(decision); err == nil {
			rec.Decision = data
		}
	} else {
		rec.Error = derr.Error()
	}
	rec, recErr := e.store.Decisions.Append(rec)
	if recErr != nil {
		slog.Warn("lưu kiểm toán bổ sung phán quyết khởi động thất bại", "module", "engine", "err", recErr)
	}
	if derr != nil {
		e.pauseWithNotify(notify.KindPlanStart, "phán quyết khởi động thất bại, đã tạm dừng (vui lòng kiểm tra cấu hình mô hình/mạng rồi tiếp tục): "+derr.Error())
		return nil
	}
	if err := e.store.RunMeta.SetPlanStart(domain.PlanStartRecord{
		RawPrompt: prompt, Planner: decision.Planner, PlannerTask: decision.Task, DecisionID: rec.ID,
	}); err != nil {
		e.pauseWithNotify(notify.KindPlanStart, "không lưu được phán quyết khởi động, đã tạm dừng: "+err.Error())
		return nil
	}
	e.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Level: "info",
		Summary: fmt.Sprintf("đã bổ sung phán quyết khởi động (nhà quy hoạch: %s——%s)", decision.Planner, decision.Reason)})
	return &flow.Instruction{Agent: decision.Planner, Task: decision.Task, Reason: decision.Reason}
}

// precheck là hình hài tất định của ToolGate cũ: đơn phái không hợp lệ bị ghi thẳng lại, không cần văn bản dạy dỗ.
func (e *engine) precheck(inst *flow.Instruction) (*flow.Instruction, error) {
	progress, err := e.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("load progress: %w", err)
	}
	if progress != nil && progress.Phase == domain.PhaseComplete {
		// Lối thoát hợp lệ duy nhất ở giai đoạn hoàn tất là reopen (hành động can thiệp); mọi đơn phái đều bị loại bỏ.
		slog.Warn("đơn phái ở giai đoạn hoàn tất bị loại bỏ", "module", "engine", "agent", inst.Agent)
		return &flow.Instruction{}, nil // đặt rỗng: vòng sau Route trả nil nên dừng tự nhiên
	}
	if inst.Agent == "writer" {
		t := hostTextsFor(e.store.Language())
		if progress == nil || progress.Phase != domain.PhaseWriting {
			phase := "<nil>"
			if progress != nil {
				phase = string(progress.Phase)
			}
			return nil, fmt.Errorf(t.writerPhaseError, phase, errInvalidWriteTarget)
		}
		ch, err := writerTargetChapter(e.store)
		if err != nil {
			return nil, err
		}
		if ch > 0 {
			if err := tools.EnsureChapterExpanded(e.store, ch); err != nil {
				if !errors.Is(err, errs.ErrToolPrecondition) {
					return nil, err
				}
				// Chương đích chưa khai triển → đổi phái architect_long để khai triển (văn bản dạy dỗ của gate cũ
				// là để nói với LLM; Engine tự làm đúng việc).
				return &flow.Instruction{
					Agent:  "architect_long",
					Task:   fmt.Sprintf(t.expandArcTask, err),
					Reason: "chương mục tiêu chưa khai triển, khai triển trước rồi viết tiếp",
				}, nil
			}
		}
		e.refresh()
	}
	return nil, nil
}

// writerTargetChapter suy ra chương mà lần phái writer kế tiếp thực sự sẽ viết (đầu hàng đợi làm lại, nếu không thì chương kế tiếp).
func writerTargetChapter(st *storepkg.Store) (int, error) {
	progress, err := st.Progress.Load()
	if err != nil {
		return 0, fmt.Errorf("load progress: %w", err)
	}
	if progress == nil {
		return 0, fmt.Errorf("progress chưa khởi tạo")
	}
	if len(progress.PendingRewrites) > 0 {
		return progress.PendingRewrites[0], nil
	}
	return progress.NextChapter(), nil
}

// trackDeadlock duy trì bộ đếm bế tắc: sự xuất hiện liên tiếp của cùng Agent+Task cho thấy lượt trước
// chưa thoả mãn hậu điều kiện định tuyến. Các checkpoint trung gian bên trong Worker như plan/draft/edit
// chỉ dùng cho khôi phục và quan sát, không được đặt lại bộ đếm cấp Engine (issue #84).
// Khi repeats đạt ngưỡng thì hỏi Arbiter, vượt trần cứng thì ngắt mạch.
// Trả về stop=true nghĩa là vòng lặp phải kết thúc; inst có thể bị Arbiter ghi lại (reroute) hoặc đặt rỗng (tính lại).
func (e *engine) trackDeadlock(ctx context.Context, inst **flow.Instruction) (stop bool) {
	in := *inst
	if in == nil || in.Agent == "" {
		*inst = nil
		return false
	}
	key := instructionKey(in)
	if key == e.lastKey {
		e.repeats++
	} else {
		e.lastKey, e.repeats = key, 1
	}
	if e.repeats < deadlockConsultAt {
		return false
	}
	if e.repeats >= deadlockAbortAt {
		e.pauseStuck(notify.KindDeadlock, in, fmt.Sprintf("ngắt mạch bế tắc: chỉ dẫn không tiến triển liên tục %d lần (%s), đã tạm dừng chờ can thiệp thủ công", e.repeats, in.Agent))
		return true
	}
	// Hỏi Arbiter về bế tắc (repeats ∈ [consultAt, abortAt)). Phán quyết retry không đặt lại bộ đếm.
	facts := e.failureFacts("deadlock", in, e.workerErrorFor(in))
	decision, err := runObservedDecision(e.observer, "Phán quyết bế tắc", func() (arbiter.FailureDecision, error) {
		return arbiter.DecideFailure(ctx, e.arbiterModel, e.failurePrompt, facts)
	})
	e.recordFailureDecision("deadlock", in, facts, decision, err)
	if err != nil {
		e.pauseWithNotify(notify.KindDeadlock, "phán quyết bế tắc thất bại, đã tạm dừng chờ can thiệp thủ công: "+err.Error())
		return true
	}
	switch decision.Action {
	case "retry":
		return false
	case "reroute":
		*inst = &flow.Instruction{Agent: decision.Dispatch.Agent, Task: decision.Dispatch.Task, Reason: decision.Reason}
		return false
	default: // abort
		e.pauseStuck(notify.KindDeadlock, in, "Phán quyết bế tắc: "+decision.Reason)
		return true
	}
}

// runWorker chạy trực tiếp một lượt sub-agent: sự kiện DISPATCH + trung chuyển tiến độ + phân tích kết quả.
func (e *engine) runWorker(ctx context.Context, inst *flow.Instruction) error {
	e.observer.dispatchStart(inst.Agent, inst.Task, inst.Reason)
	// Nhiệm vụ Writer được đánh dấu trước là đang thực hiện (giống Dispatcher cũ: dàn ý trên UI lập tức phản ánh “▸ đang thực hiện”).
	if inst.Agent == "writer" && inst.Chapter > 0 {
		if err := e.store.Progress.ValidateChapterWork(inst.Chapter); err != nil {
			runErr := fmt.Errorf("%w: %w", errInvalidWriteTarget, err)
			e.observer.dispatchFinish(inst.Agent, runErr)
			return runErr
		}
		if err := e.store.Progress.StartChapter(inst.Chapter); err != nil {
			runErr := fmt.Errorf(hostTextsFor(e.store.Language()).preMarkChapterFailed, errInvalidWriteTarget, inst.Chapter, err)
			e.observer.dispatchFinish(inst.Agent, runErr)
			return runErr
		}
	}

	// Tiến độ Worker được trung chuyển qua ctx ToolProgress tới observer.
	runCtx := agentcore.WithToolProgress(ctx, func(p agentcore.ProgressPayload) {
		e.observer.workerProgress(p)
	})
	_, err := e.workers.Run(runCtx, inst.Agent, inst.Task)
	if err == nil {
		// Thành công là xoá theo dõi thất bại: lần thất bại kế tiếp của cùng khoá lại được hưởng lượt “thử lại một lần”.
		e.failedKey = ""
	}
	e.observer.dispatchFinish(inst.Agent, err)
	return err
}

// handleWorkerError thử lại cùng chỉ dẫn một lần, rồi chuyển loại lỗi và sự thật hiện tại cho Arbiter.
// Engine không cứng hoá loại lỗi thực thi nào “chắc chắn không phục hồi được”; đổi phái theo ngữ nghĩa do mô hình quyết định, ranh giới Store vẫn
// tiếp tục chặn ghi dữ liệu không hợp lệ.
func (e *engine) handleWorkerError(ctx context.Context, inst *flow.Instruction, werr error) (stop bool) {
	msg := werr.Error()

	key := instructionKey(inst)
	if e.failedKey != key {
		// Thất bại lần đầu: thử lại chỉ dẫn gốc một lần (vòng sau Route tính lại, bản chất dữ liệu dẫn dắt là idempotent).
		e.failedKey = key
		return false
	}
	e.failedKey = ""
	facts := e.failureFacts("worker_failure", inst, werr)
	decision, err := runObservedDecision(e.observer, "Phán quyết thất bại", func() (arbiter.FailureDecision, error) {
		return arbiter.DecideFailure(ctx, e.arbiterModel, e.failurePrompt, facts)
	})
	e.recordFailureDecision("worker_failure", inst, facts, decision, err)
	if err != nil {
		e.pauseWithNotify(notify.KindWorkerFailure, "Phán quyết thất bại không khả dụng, đã tạm dừng chờ can thiệp thủ công: "+msg+contentFilterAdvice(werr))
		return true
	}
	switch decision.Action {
	case "retry":
		return false
	case "reroute":
		e.mu.Lock()
		e.next = &flow.Instruction{Agent: decision.Dispatch.Agent, Task: decision.Dispatch.Task, Reason: decision.Reason}
		e.deferGateForNext = false
		e.mu.Unlock()
		return false
	default: // abort
		e.pauseStuck(notify.KindWorkerFailure, inst, "Phán quyết thất bại: "+decision.Reason+contentFilterAdvice(werr))
		return true
	}
}

// pauseStuck tạm dừng khi engine từ bỏ một chỉ dẫn: chương làm lại phải ra khỏi hàng đợi trước rồi mới dừng. Chỉ dùng cho chỉ dẫn mà engine đã kết luận
// không đi nổi (ngắt mạch bế tắc, abort do phán quyết bế tắc / phán quyết thất bại); các lỗi hạ tầng như phán quyết không khả dụng vẫn đi
// qua pauseWithNotify — đó là vấn đề bên ngoài, không nên đánh đổi bằng một chương làm lại.
func (e *engine) pauseStuck(kind string, inst *flow.Instruction, body string) {
	if e.dropStuckRewrite(inst) {
		body += fmt.Sprintf("; chương %d đã bị chuyển khỏi hàng đợi làm lại (giữ lại bản chính trước đó), việc sáng tác tiếp sẽ tiến từ các chương sau", inst.Chapter)
	}
	e.pauseWithNotify(kind, body)
}

// dropStuckRewrite chuyển chương làm lại bị kẹt khỏi hàng đợi. PendingRewrites là sự thật đã lưu, engine từ bỏ
// chỉ dẫn này mà không ra khỏi hàng đợi thì lần khởi động sau sẽ phát lại ngay chỉ dẫn chết đó, khóa vĩnh viễn cả tác phẩm (issue #110).
// Trả về true nghĩa là đã thực sự ra khỏi hàng đợi.
func (e *engine) dropStuckRewrite(inst *flow.Instruction) bool {
	if inst == nil || inst.Agent != "writer" || inst.Chapter <= 0 {
		return false
	}
	progress, err := e.store.Progress.Load()
	if err != nil || progress == nil || !slices.Contains(progress.PendingRewrites, inst.Chapter) {
		return false
	}
	if err := e.store.Progress.CompleteRewrite(inst.Chapter); err != nil {
		slog.Warn("chuyển chương làm lại bị kẹt khỏi hàng đợi thất bại", "module", "engine", "chapter", inst.Chapter, "err", err)
		return false
	}
	return true
}

// discardNonSemanticDeadlockAttempt huỷ lần ghi nhận trước của trackDeadlock cho lần phái này.
// Chỉ loại các loại lỗi ổn định khi lệnh gọi mô hình chưa thực thi trọn vẹn; content_filter
// giữ nguyên trong đường tự lành cũ, còn max_turns, stop_guard, hủy v.v. — những trường hợp không tiến triển thật — vẫn được đếm.
func (e *engine) discardNonSemanticDeadlockAttempt(inst *flow.Instruction, werr error) {
	if inst == nil || !isNonSemanticWorkerFailure(werr) {
		return
	}
	key := instructionKey(inst)
	if e.lastKey != key || e.repeats <= 0 {
		return
	}
	e.repeats--
	if e.repeats == 0 {
		e.lastKey = ""
	}
}

// isNonSemanticWorkerFailure chỉ nhận diện lỗi “lần thực thi mô hình này không sinh ra ngữ nghĩa nào để phán đoán”.
// Ưu tiên dựa trên hợp đồng chuỗi lỗi của agentcore; khi nhà cung cấp làm phẳng chuỗi lỗi thì dùng lại phân loại nhật ký.
func isNonSemanticWorkerFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, agentcore.ErrContextOverflow) || errors.Is(err, agentcore.ErrStreamPartial) {
		return true
	}
	providerErr := agentcore.ClassifyProvider(err)
	classified := errors.Is(providerErr, agentcore.ErrProviderStreamIdle) ||
		errors.Is(providerErr, agentcore.ErrProviderQuota) ||
		errors.Is(providerErr, agentcore.ErrProviderRateLimit) ||
		errors.Is(providerErr, agentcore.ErrProviderTimeout) ||
		errors.Is(providerErr, agentcore.ErrProviderAuth) ||
		errors.Is(providerErr, agentcore.ErrProviderNetwork) ||
		errors.Is(providerErr, agentcore.ErrProviderOverloaded)
	return classified || errorKind(err, err.Error()) == "overloaded"
}

func instructionKey(inst *flow.Instruction) string {
	if inst == nil {
		return ""
	}
	return inst.Agent + "\x00" + inst.Task
}

func (e *engine) rememberWorkerError(inst *flow.Instruction, workerErr error) {
	if workerErr == nil || inst == nil {
		e.lastWorkerErrorKey, e.lastWorkerError = "", nil
		return
	}
	e.lastWorkerErrorKey, e.lastWorkerError = instructionKey(inst), workerErr
}

func (e *engine) workerErrorFor(inst *flow.Instruction) error {
	if e.lastWorkerErrorKey != instructionKey(inst) {
		return nil
	}
	return e.lastWorkerError
}

// contentFilterAdvice gắn lối thoát người dùng làm được cho lần tạm dừng do bị chặn kiểm duyệt nội dung.
// Kiểm duyệt là hộp đen của nhà cung cấp, không thể kiểm tra trước hay né tránh, chỉ có thể đưa quyết định lên tay người dùng;
// bản thân lần chặn không ngắt mạch sớm — đổi ngữ cảnh rồi phái lại có tỉ lệ tự lành thật (đo thực tế ở ch21-24),
// hãy đi hết chuỗi “thử lại miễn phí → phán xử” rồi mới tạm dừng.
func contentFilterAdvice(werr error) string {
	if !errors.Is(werr, agentcore.ErrProviderContentFilter) {
		return ""
	}
	return ". Đây là lần chặn kiểm duyệt nội dung của nhà cung cấp (không phải lỗi cục bộ), có thể chọn: /model chuyển sang nhà cung cấp không có lớp kiểm duyệt rồi nhập «tiếp tục»; hoặc sửa cách diễn đạt trong bản nháp chương (drafts/) rồi tiếp tục; thử lại nguyên bản gần như chắc chắn vẫn bị chặn"
}

// errInvalidWriteTarget đánh dấu mục tiêu ghi không hợp lệ bị tiền điều kiện của runWorker chặn lại, để chuỗi lỗi và
// gói sự thật của Arbiter giữ được ngữ nghĩa ổn định; có thử lại hay đổi phái vẫn do quy trình thất bại thống nhất quyết định.
// Văn bản cố định là tiếng Việt (sentinel dùng cho errors.Is); bản theo ngôn ngữ tác phẩm
// cho gói sự thật gửi Tài phán nằm ở hostTexts.invalidWriteTarget.
var errInvalidWriteTarget = errors.New("mục tiêu ghi không hợp lệ")

func (e *engine) failureFacts(kind string, inst *flow.Instruction, workerErr error) arbiter.FailureFacts {
	t := hostTextsFor(e.store.Language())
	f := arbiter.FailureFacts{Kind: kind, Agent: inst.Agent, Task: inst.Task, Repeats: e.repeats}
	if workerErr != nil {
		f.Error = workerErr.Error()
		// gói sự thật đi vào prompt của Tài phán nên câu chữ phải theo ngôn ngữ tác phẩm;
		// sentinel errInvalidWriteTarget dùng cho errors.Is nên giữ văn bản cố định.
		if errors.Is(workerErr, errInvalidWriteTarget) {
			f.Error = strings.Replace(f.Error, errInvalidWriteTarget.Error(), t.invalidWriteTarget, 1)
		}
		f.ErrorKind = errorKind(workerErr, f.Error)
		if f.ErrorKind == "" {
			f.ErrorKind = "unknown"
		}
	}
	missing, err := e.store.FoundationMissing()
	if err != nil {
		f.FactWarnings = append(f.FactWarnings, t.foundationReadFailed+err.Error())
	} else {
		f.FoundationGap = missing
	}
	p, err := e.store.Progress.Load()
	if err != nil {
		f.FactWarnings = append(f.FactWarnings, t.progressReadFailed+err.Error())
	}
	if p != nil {
		f.Phase = string(p.Phase)
		f.NextChapter = p.NextChapter()
		f.PendingQueue = p.PendingRewrites
	}
	return f
}

func (e *engine) recordFailureDecision(kind string, inst *flow.Instruction, facts arbiter.FailureFacts, d arbiter.FailureDecision, derr error) {
	rec := storepkg.DecisionRecord{Kind: kind, Decider: "arbiter", Input: inst.Agent + ": " + inst.Task, Reason: d.Reason}
	if data, err := json.Marshal(facts); err == nil {
		rec.Facts = data
	}
	if derr == nil {
		if data, err := json.Marshal(d); err == nil {
			rec.Decision = data
		}
	} else {
		rec.Error = derr.Error()
	}
	if _, err := e.store.Decisions.Append(rec); err != nil {
		slog.Warn("lưu kiểm toán phán quyết thất bại", "module", "engine", "kind", kind, "err", err)
	}
}

// applyPendingOps commit hành động điều khiển của can thiệp tại ranh giới vòng lặp; vòng lặp phải được rút sạch — hỏi lại đồng bộ
// (reconsult) sẽ thêm hành động mới trong lúc áp dụng, phải tiêu hết trong ranh giới này, nếu không sẽ có
// một worker thừa bị phái (can thiệp phải có hiệu lực trước phần sáng tác tiếp theo).
// Trả về có cặp hold+dispatch phải chạy trước hay không; trong trường hợp đó phía gọi hoãn kiểm tra Gate.
func (e *engine) applyPendingOps(ctx context.Context) (deferGate bool) {
	for {
		e.mu.Lock()
		ops := e.pending
		e.pending = nil
		e.mu.Unlock()
		if len(ops) == 0 {
			return deferGate
		}
		for _, op := range ops {
			pairedHoldDispatch := op.hold != nil && !op.hold.Cancel && op.dispatch != nil
			err := e.applyControlOp(ctx, op)
			if err != nil {
				// Lưu hành động thất bại: host đã xóa PendingSteer theo “đã xếp hàng”,
				// ở đây lưu lại toàn bộ can thiệp, phán quyết lại và thử lại khi khôi phục/tiếp tục (hành động idempotent + hỏi lại theo sự thật mới).
				if op.text != "" {
					if serr := e.store.RunMeta.SetPendingSteer(op.text); serr != nil {
						slog.Warn("lưu lại can thiệp thất bại", "module", "engine", "err", serr)
					}
				}
				e.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Level: "warn",
					Summary: "hành động can thiệp thất bại, đã được giữ; sẽ tự thử lại khi khôi phục/tiếp tục"})
			} else if pairedHoldDispatch && e.nextDefersGate() {
				// Chỉ khi cả hold và đơn phái cặp đều ghi thành công thì mới được bỏ qua Gate lần này.
				// Nếu ghi hold thất bại hoặc đơn phái bị loại vì sự thật đã lỗi thời mà vẫn bỏ qua, sẽ khiến
				// Worker không được bảo vệ tiến triển.
				deferGate = true
			}
		}
	}
}

// applyControlOp thực thi một hành động điều khiển (hold ghi thẳng RunMeta, reopen gọi lõi công cụ, dispatch đối soát trước).
// Khi engine không chạy thì host gọi trực tiếp trong đường can thiệp; trả về lỗi lưu trữ đầu tiên (phía gọi quyết định có nên
// giữ PendingSteer để phát lại khi khôi phục hay không).
func (e *engine) applyControlOp(ctx context.Context, op controlOp) error {
	var firstErr error
	fail := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	if op.dispatch != nil {
		// Expect phải được đối chiếu trước khi hold và các hành động cặp khác được ghi. Nếu không, sau khi đơn phái lỗi thời thì hold cũ
		// sẽ còn sót lại và xung đột với hold do phán quyết lại theo sự thật mới, cuối cùng chỉ tạm dừng mà bỏ sót việc sửa đổi.
		fresh, err := arbiter.CollectInterventionFacts(e.store)
		if err != nil {
			return fmt.Errorf("làm mới sự thật can thiệp: %w", err)
		}
		if fresh.Phase != op.facts.Phase || fresh.Flow != op.facts.Flow ||
			fresh.QueueHead() != op.facts.QueueHead() {
			e.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Level: "warn",
				Summary: "đơn phái phán quyết đã lỗi thời (sự thật đã tiến triển), phán quyết lại với sự thật mới nhất"})
			e.recordStale(op)
			if op.text != "" && e.reconsult != nil {
				// Hỏi lại đồng bộ: can thiệp phải có hiệu lực trước phần sáng tác tiếp theo — bất đồng bộ sẽ khiến engine phái thêm worker trước khi phán quyết mới
				// được ghi. Hành động mới do applyPendingOps rút sạch tại ranh giới này.
				e.reconsult(op.text)
			}
			return nil
		}
	}
	if op.hold != nil {
		if op.hold.Cancel {
			meta, err := e.store.RunMeta.Load()
			if err != nil {
				e.emitEvent(Event{Time: time.Now(), Category: "ERROR", Summary: "đọc lần tạm dừng một lần thất bại: " + err.Error(), Level: "error"})
				return err
			}
			if meta != nil && meta.AdvanceHold != nil {
				if err := e.store.RunMeta.ClearAdvanceHold(*meta.AdvanceHold); err != nil {
					e.emitEvent(Event{Time: time.Now(), Category: "ERROR", Summary: "huỷ lần tạm dừng một lần thất bại: " + err.Error(), Level: "error"})
					return err
				}
			}
			e.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "đã huỷ lần tạm dừng một lần", Level: "info"})
		} else {
			hold := domain.AdvanceHold{After: op.hold.After, TargetChapter: op.hold.TargetChapter, Reason: op.hold.Reason}
			if err := e.store.RunMeta.SetAdvanceHold(hold); err != nil {
				e.emitEvent(Event{Time: time.Now(), Category: "ERROR", Summary: "thiết lập lần tạm dừng một lần thất bại: " + err.Error(), Level: "error"})
				return err // khi hold chưa ghi xuống đĩa thì dispatch liên kết không được thực thi
			}
			e.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "đã thiết lập lần tạm dừng một lần: " + op.hold.Reason, Level: "info"})
		}
	}
	if op.reopen != nil {
		args, _ := json.Marshal(map[string]any{"chapters": op.reopen.Chapters, "reason": op.reopen.Reason})
		if _, err := tools.NewReopenBookTool(e.store).Execute(ctx, args); err != nil {
			e.emitEvent(Event{Time: time.Now(), Category: "ERROR", Summary: "mở lại làm lại thất bại: " + err.Error(), Level: "error"})
			fail(err)
		} else {
			e.emitEvent(Event{Time: time.Now(), Category: "SYSTEM",
				Summary: fmt.Sprintf("đã mở lại làm lại toàn tác phẩm: chương %v được xếp hàng", op.reopen.Chapters), Level: "info"})
		}
	}
	if op.dispatch != nil {
		// Expect đã được đối chiếu trước mọi ghi trạng thái cặp. CheckpointSeq chỉ để kiểm toán, không tham gia
		// đối soát: khi can thiệp đến thì worker thường đang chạy, seq chắc chắn đã tiến.
		e.mu.Lock()
		// Khoảng trống đã biết (ranh giới best-effort, xem làm rõ ③ trong engine-arbiter.md): từ đây đơn phái chỉ nằm trong bộ nhớ,
		// nếu bị giết cứng trước khi worker khởi động (kill -9, defer không chạy) thì ý định phái lệnh này mất —
		// thoát bình thường / Abort thì defer của run lưu lại PendingSteer làm chốt đỡ.
		e.next = &flow.Instruction{Agent: op.dispatch.Agent, Task: interventionDispatchTask(op.dispatch.Task, op.text, hostTextsFor(e.store.Language())), Reason: "Phán quyết can thiệp của người dùng"}
		e.deferGateForNext = op.hold != nil && !op.hold.Cancel
		e.mu.Unlock()
	}
	return firstErr
}

// interventionDispatchTask giữ lại can thiệp gốc của người dùng, tránh Arbiter vô tình mở rộng
// mục tiêu sửa đổi. Tầng dưới có thể đọc ngữ cảnh rộng hơn để phán đoán, nhưng chỉ được coi nguyên văn là nguồn cho phép hành động.
// Đoạn giải thích được nối thêm sẽ vào văn bản nhiệm vụ của Worker, nên nó theo ngôn ngữ tác phẩm (t.interventionOriginal).
func interventionDispatchTask(task, original string, t *hostTexts) string {
	task = strings.TrimSpace(task)
	if strings.TrimSpace(original) == "" {
		return task
	}
	return task + t.interventionOriginal + original
}

func (e *engine) recordStale(op controlOp) {
	rec := storepkg.DecisionRecord{Kind: "decision_stale", Decider: "engine", Input: op.text}
	if data, err := json.Marshal(op.facts); err == nil {
		rec.Facts = data
	}
	if _, err := e.store.Decisions.Append(rec); err != nil {
		slog.Warn("ghi bản ghi lỗi thời thất bại", "module", "engine", "err", err)
	}
}

// pauseWithNotify cho engine tự tạm dừng (ngắt mạch bế tắc / abort do phán quyết thất bại): thông báo ngoài màn hình + đi theo ngữ nghĩa
// tạm dừng thống nhất của host (onPause → abortWithEvent: lifecycle=paused + sự kiện trong màn hình + cancel ctx).
func (e *engine) pauseWithNotify(kind, body string) {
	e.notify(kind, "warn", "ainovel: engine tạm dừng", body)
	if e.onPause != nil {
		e.onPause(body)
		return
	}
	e.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: body, Level: "warn"})
	e.abort()
}

// completionSummary là báo cáo chốt tất định khi hoàn tất tác phẩm, không tốn lệnh gọi LLM.
func completionSummary(progress domain.Progress, book domain.BookMetadata) string {
	var b strings.Builder
	fmt.Fprintf(&b, "«%s» hoàn thành: tổng %d chương %d từ", book.Title, len(progress.CompletedChapters), progress.TotalWordCount)
	return b.String()
}
