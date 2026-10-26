package host

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/agents"
	"github.com/voocel/ainovel-cli/internal/agents/ctxpack"
	"github.com/voocel/ainovel-cli/internal/arbiter"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/flow"
	"github.com/voocel/ainovel-cli/internal/host/exp"
	"github.com/voocel/ainovel-cli/internal/host/imp"
	"github.com/voocel/ainovel-cli/internal/host/sim"
	runtimelog "github.com/voocel/ainovel-cli/internal/logger"
	modelreg "github.com/voocel/ainovel-cli/internal/models"
	"github.com/voocel/ainovel-cli/internal/notify"
	"github.com/voocel/ainovel-cli/internal/revision"
	"github.com/voocel/ainovel-cli/internal/rules"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/tools"
	"github.com/voocel/ainovel-cli/internal/userrules"
)

// Host là vỏ ngoài runtime: vòng đời / điểm vào can thiệp / phóng chiếu sự kiện / quản lý mô hình.
// Việc điều phối và thực thi nằm ở engine (vòng lặp tất định); phán quyết ngữ nghĩa nằm ở arbiter (LLM-as-function).
type Host struct {
	cfg             bootstrap.Config
	bundle          assets.Bundle
	store           *storepkg.Store
	bookLease       *bookLease
	styleStats      *tools.StyleStatsIndex
	models          *bootstrap.ModelSet
	engine          *engine
	thinkingApplier agents.ApplyThinking // /model đổi mức suy luận thì liên kết từng Worker
	writerRestore   *ctxpack.WriterRestorePack
	userRules       *userrules.Service
	observer        *observer
	usage           *UsageTracker
	usageCancel     context.CancelFunc  // dừng autoSaveLoop và kích hoạt lần flush cuối
	budget          *BudgetSentinel     // chính sách ngân sách; chưa bật thì nil (các phương thức an toàn với nil)
	gate            *ChapterAdvanceGate // thành phần chính sách thống nhất cho giấy phép chương và lần tạm dừng một lần
	notifier        *notify.Notifier    // cảnh báo không người trực; chưa bật thì nil (Send an toàn với nil)
	configPath      string              // đích ghi cấu hình xuống đĩa: /config, /model ghi vào bản đang có hiệu lực gần nhất (có bản cấp dự án thì ghi vào đó, không thì ghi bản toàn cục)
	logCleanup      func()
	fileLogErr      error

	events   chan Event
	streamCh chan string
	done     chan struct{}

	mu         sync.Mutex
	lifecycle  lifecycle
	cocreating bool   // cờ chiếm đồng sáng tác theo giai đoạn: trong cửa sổ paused chặn can thiệp đồng thời từ import/simulate/continue
	exclusive  string // cờ chiếm của tác vụ nền độc quyền (nhập/mô phỏng/sửa): rỗng nghĩa là không tác vụ nào đang chạy, chặn các cửa vào độc quyền đồng thời
	// exclusiveCancel là hàm huỷ của tác vụ độc quyền hiện tại: ngân sách dừng cứng / tạm dừng thủ công
	// phải dừng được cả tác vụ nhập đang đốt tiền, không chỉ Engine — abortWithEvent huỷ nó khi Engine
	// chưa chạy (callback abort của người canh ngân sách và lệnh Abort thủ công dùng chung một cơ chế
	// dừng). releaseExclusive xóa luôn nó.
	exclusiveCancel context.CancelFunc
	closeOnce       sync.Once
	asyncWG         sync.WaitGroup
	closing         bool

	interMu sync.Mutex // phán quyết can thiệp nối tiếp FIFO (cùng lúc có nhiều nhất một lần tư vấn đang trên đường)

	outputMu     sync.RWMutex
	outputClosed bool

	// runCtx ràng buộc các lệnh gọi phán quyết LLM ở phía host (phán quyết khởi động / phân loại can thiệp);
	// Close huỷ, tránh lúc thoát vẫn còn phán quyết đang trên đường mà không cắt được.
	runCtx    context.Context
	runCancel context.CancelFunc
}

type lifecycle string

const (
	lifecycleIdle      lifecycle = "idle"
	lifecycleRunning   lifecycle = "running"
	lifecyclePaused    lifecycle = "paused"
	lifecycleCompleted lifecycle = "completed"
)

// New tạo Host.
func New(cfg bootstrap.Config, bundle assets.Bundle, options ...NewOption) (*Host, error) {
	cfg.FillDefaults()
	if err := cfg.ValidateBase(); err != nil {
		return nil, err
	}
	var opts newOptions
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}

	bookLease, err := acquireBookLease(cfg.OutputDir)
	if err != nil {
		return nil, err
	}
	keepBookLease := false
	var logCleanup func()
	defer func() {
		if keepBookLease {
			return
		}
		if err := bookLease.Close(); err != nil {
			slog.Error("giải phóng thư mục truyện thất bại", "module", "host", "dir", cfg.OutputDir, "err", err)
		}
		if logCleanup != nil {
			logCleanup()
		}
	}()

	var fileLogErr error
	if opts.logFile != "" {
		logCleanup, fileLogErr = runtimelog.SetupFile(cfg.OutputDir, opts.logFile, opts.logAlsoStderr, opts.logAttrs...)
		if fileLogErr != nil {
			logCleanup = nil
			slog.Warn("nhật ký tệp không dùng được, tiếp tục dùng nhật ký của tiến trình hiện tại", "module", "host", "file", opts.logFile, "err", fileLogErr)
		}
	}

	slog.Info("khởi chạy", "module", "boot", "provider", cfg.Provider, "model", cfg.ModelName, "output", cfg.OutputDir)

	// Dựng goroutine nền lấy lại metadata mô hình (cửa sổ context / giá) từ OpenRouter, bộ nhớ đệm trên đĩa 24h.
	modelreg.StartPricingRefresh(modelreg.DefaultRegistry(), bootstrap.DefaultConfigDir())

	store := storepkg.NewStore(cfg.OutputDir)
	// Nhãn của Markdown dẫn xuất đi theo ngôn ngữ tác phẩm: các lượt xem này sẽ được novel_context đọc
	// ngược lại thành ngữ cảnh; nhãn khác ngôn ngữ với nội dung sẽ kéo mô hình về ngôn ngữ khác.
	store.SetLanguage(cfg.NormalizedLanguage())
	if err := store.Init(); err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}
	// RunMeta là nguồn sự thật cho toàn bộ ngữ nghĩa điều khiển, phải kiểm tra hợp lệ trước khi dựng mô hình / tác vụ nền.
	// advance mode lạ trả thẳng lỗi có cấu trúc; cấm đoán hạ cấp rồi tiếp tục ghi xuống đĩa.
	if err := store.RunMeta.Init(cfg.Style, cfg.Provider, cfg.ModelName); err != nil {
		return nil, fmt.Errorf("init run meta: %w", err)
	}

	models, err := bootstrap.NewModelSet(cfg)
	if err != nil {
		return nil, fmt.Errorf("create models: %w", err)
	}
	slog.Info("mô hình đã sẵn sàng", "module", "boot", "summary", models.Summary())

	usage := NewUsageTracker(models, store)
	// Ưu tiên đọc meta/usage.json; các trường hợp sau đều quay về hồi lấp một lần từ sessions/*.jsonl:
	//   - tệp không tồn tại (trước lần lưu đầu tiên)
	//   - phiên bản schema không khớp (sau nâng cấp thì bỏ định dạng cũ)
	//   - tệp tồn tại nhưng hỏng / lỗi IO (không để dữ liệu hỏng làm số liệu tích luỹ vĩnh viễn về 0)
	// Hồi lấp xong gọi SaveNow ngay để cố định kết quả, lần khởi động sau Load sẽ trúng thẳng.
	loaded, loadErr := usage.LoadFromStore()
	if loadErr != nil {
		slog.Warn("nạp usage thất bại, sẽ thử lấp lại từ sessions", "module", "usage", "err", loadErr)
	}
	if !loaded {
		if n, err := usage.ReplaySessions(cfg.OutputDir); err != nil {
			slog.Warn("usage replay thất bại", "module", "usage", "err", err)
		} else if n > 0 {
			slog.Info("đã lấp lại usage từ session", "module", "usage", "messages", n)
			if err := usage.SaveNow(); err != nil {
				slog.Warn("lưu sau khi lấp lại usage thất bại", "module", "usage", "err", err)
			}
		}
	}
	usageCtx, usageCancel := context.WithCancel(context.Background())
	usage.StartAutoSave(usageCtx)

	// Khai báo trước onGuardBlock: chỉ gắn được closure phát sự kiện sau khi h đã dựng xong.
	var onGuardBlock func(agent, reason string, consecutive int32)
	styleStats := tools.NewStyleStatsIndex(store)
	workers, restore, applyThinking := agents.BuildWorkers(cfg, store, styleStats, models, bundle, usage.Record,
		func(agent, reason string, consecutive int32) {
			if onGuardBlock != nil {
				onGuardBlock(agent, reason, consecutive)
			}
		})
	store.Signals.ClearStaleSignals()

	h := &Host{
		cfg:             cfg,
		bundle:          bundle,
		store:           store,
		bookLease:       bookLease,
		styleStats:      styleStats,
		models:          models,
		thinkingApplier: applyThinking,
		writerRestore:   restore,
		userRules:       userrules.NewService(store, models.Default, rules.DefaultOptions()),
		usage:           usage,
		usageCancel:     usageCancel,
		configPath:      bootstrap.EffectiveConfigPath(),
		logCleanup:      logCleanup,
		fileLogErr:      fileLogErr,
		events:          make(chan Event, 100),
		streamCh:        make(chan string, 256),
		done:            make(chan struct{}, 4),
		lifecycle:       lifecycleIdle,
	}
	h.runCtx, h.runCancel = context.WithCancel(context.Background())
	h.observer = newObserver(store, h.emitEvent, h.emitDelta, h.emitClear)
	// Arbiter ở phía host dùng chung một chuỗi ToolProgress → observer → bàn làm việc với Worker.
	h.runCtx = agentcore.WithToolProgress(h.runCtx, h.observer.workerProgress)
	if cfg.Notify.IsEnabled() {
		h.notifier = notify.New(cfg.Notify.Command, cfg.Notify.Events)
	}
	// Người canh ngân sách: Engine gọi thẳng HandleBoundary ở biên mỗi vòng lặp (không còn qua đăng ký sự kiện).
	if sentinel := NewBudgetSentinel(cfg.Budget,
		func() float64 { c, _, _, _, _ := usage.Totals(); return c },
		func(reason string) { h.abortWithEvent(reason, "error") },
		func(level, summary string) {
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: level})
			h.notifier.Send(notify.Notification{Kind: notify.KindBudget, Level: level, Title: "ainovel: ngân sách", Body: summary})
		},
	); sentinel != nil {
		h.budget = sentinel
		usage.SetOnCost(sentinel.OnCost)
		// Cảnh báo vùng mù tính phí: khi mô hình không báo usage thì chi phí luôn bằng 0, ngân sách không bao giờ kích hoạt — cầu dao chưa nối thì phải gọi người.
		usage.SetOnMissingUsage(func() {
			const blind = "vùng mù của ngân sách: mô hình không trả về dữ liệu usage, thống kê chi phí bằng 0 nên giới hạn ngân sách sẽ không kích hoạt (với mô hình tự định nghĩa, hãy kiểm tra giá trong registry hoặc include_usage ở phía trên)"
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: blind, Level: "warn"})
			h.notifier.Send(notify.Notification{Kind: notify.KindBudget, Level: "warn", Title: "ainovel: ngân sách", Body: blind})
		})
	}
	// Cổng chặn tiến triển thống nhất: thực thi lần tạm dừng một lần, và chặn chương mới không có giấy phép ở chế độ review.
	h.gate = NewChapterAdvanceGate(store,
		func(reason string) {
			h.abortWithEvent(reason, "info")
			h.notifier.Send(notify.Notification{Kind: notify.KindAdvanceGate, Level: "info", Title: "ainovel: chờ nghiệm thu", Body: reason})
		},
		func(level, summary string) {
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: level})
			h.notifier.Send(notify.Notification{Kind: notify.KindAdvanceGate, Level: level, Title: "ainovel: tiến chương", Body: summary})
		},
	)
	// Chặn của StopGuard nổi lên: blocked là hành động tự lành tần suất cao, chỉ vào luồng sự kiện trong màn hình (đẩy thông báo sẽ làm nhiễu);
	// escalated / hard_stop nghĩa là tác vụ con của lượt này hỏng, gửi cặp sự kiện + notify (kiến trúc §2.3).
	onGuardBlock = func(agent, reason string, n int32) {
		switch reason {
		case "escalated":
			body := fmt.Sprintf("%s đã %d lần liên tiếp không tạo ra sản phẩm bắt buộc, dừng lượt này và trả về Engine xử lý", agent, n)
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Agent: agent, Summary: "StopGuard nâng cấp: " + body, Level: "warn"})
			h.notifier.Send(notify.Notification{Kind: notify.KindStopGuard, Level: "warn", Title: "ainovel: StopGuard", Body: body})
		case "hard_stop":
			body := fmt.Sprintf("%s bị provider từ chối trả lời (safety/content_filter), dừng lượt này ngay", agent)
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Agent: agent, Summary: "StopGuard nâng cấp: " + body, Level: "warn"})
			h.notifier.Send(notify.Notification{Kind: notify.KindStopGuard, Level: "warn", Title: "ainovel: StopGuard", Body: body})
		default: // blocked
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Agent: agent,
				Summary: fmt.Sprintf("StopGuard: %s cố kết thúc khi chưa tạo đủ sản phẩm bắt buộc, đã chặn lời thúc giục (lần liên tiếp thứ %d)", agent, n), Level: "info"})
		}
	}
	// Engine: công cụ thực thi tất định (docs/engine-rfc.md). arbiter dùng mô hình Default (giới hạn tạm thời,
	// xem engine-arbiter.md §4.2).
	h.engine = &engine{
		store:           store,
		workers:         workers,
		arbiterModel:    newUsageTrackedModel(models.Default, "arbiter", usage.Record),
		failurePrompt:   bundle.Prompts.ArbiterFailure,
		planStartPrompt: bundle.Prompts.ArbiterPlanStart,
		style:           cfg.Style,
		// Hỏi lại đồng bộ: chặn vòng lặp engine một lần phán quyết (vài giây), đổi lại "can thiệp có hiệu lực trước khi sáng tác tiếp".
		reconsult: h.handleIntervention,
		observer:  h.observer,
		budget:    h.budget,
		gate:      h.gate,
		refresh:   h.refreshWriterRestore,
		emitEvent: h.emitEvent,
		notify: func(kind, level, title, body string) {
			h.notifier.Send(notify.Notification{Kind: kind, Level: level, Title: title, Body: body})
		},
		onPause: func(summary string) { h.abortWithEvent(summary, "warn") },
		onDone:  h.runEnded,
	}

	keepBookLease = true
	return h, nil
}

// ── Vòng đời ──

// PrepareUserRules sinh ảnh chụp quy tắc người dùng của cuốn sách này ở chế độ tạo mới (phía khởi động thì tất định, không đi vào Run sáng tác chính).
//
// Đầu vào là **yêu cầu sáng tác gốc** của người dùng (chưa qua bọc BuildStartPrompt) — chuẩn hoá cần chính bộ quy tắc
// của người dùng, không phải bộ khung khởi động. Cửa vào phải được gọi một lần trước StartPrepared (cả hai đường
// tạo mới quick/cocreate đều đi qua đây).
//
// Chuẩn hoá thất bại chỉ hạ cấp chứ không báo lỗi (đường tăng cường); chỉ khi ảnh chụp không ghi xuống đĩa được
// mới trả error để dừng mở sách — các lượt chạy sau sẽ không có nguồn sự thật ổn định (xem thiết kế §Thất bại và hạ cấp).
func (h *Host) PrepareUserRules(rawPrompt string) error {
	if err := h.refuseNewBookOverExisting(); err != nil {
		return err
	}
	svc := userrules.NewService(h.store, h.models.Default, rules.DefaultOptions())
	snap, err := svc.Build(context.Background(), rawPrompt)
	if err != nil {
		return fmt.Errorf("ghi ảnh chụp quy tắc người dùng xuống đĩa thất bại, không thể tiếp tục: %w", err)
	}
	logUserRulesSnapshot(snap)
	return nil
}

// ensureUserRules bảo đảm ảnh chụp tồn tại trên đường khôi phục; khi thiếu thì sinh theo
// system_defaults + tệp rules.
func (h *Host) ensureUserRules() {
	svc := userrules.NewService(h.store, h.models.Default, rules.DefaultOptions())
	snap, err := svc.GetOrBuild(context.Background())
	if err != nil {
		slog.Warn("đọc/tạo ảnh chụp quy tắc người dùng thất bại, lúc chạy sẽ dùng giá trị mặc định tích hợp", "module", "rules", "err", err)
		return
	}
	logUserRulesSnapshot(snap)
}

// logUserRulesSnapshot hiện lại lúc khởi động: để người dùng thấy hệ thống đã hiểu các quy tắc thành gì (tái sử dụng nhật ký, không thêm cơ chế mới).
func logUserRulesSnapshot(snap *rules.Snapshot) {
	if snap == nil {
		return
	}
	slog.Info("ảnh chụp quy tắc người dùng",
		"module", "rules",
		"status", string(snap.Status),
		"nguồn", snap.Sources,
		"cụm bị cấm", len(snap.Structured.ForbiddenPhrases),
		"từ mệt", len(snap.Structured.FatigueWords),
	)
	if snap.Status == rules.StatusDegraded {
		slog.Warn("một phần quy tắc không phân tích được, đã chạy theo raw preferences (có thể tạo lại ảnh chụp)",
			"module", "rules", "uncertain", snap.Uncertain)
	}
}

// StartPrepared bắt đầu sáng tác bằng **yêu cầu sáng tác gốc** của người dùng: phán quyết plan_start chọn kiến trúc sư
// và mở rộng yêu cầu, kết quả phán quyết được cố định thành
// sự thật (PlanStartRecord) trước rồi mới khởi chạy Engine — việc khôi phục luôn dựa trên sự thật đã ghi xuống đĩa, không phán quyết lại những gì đã có.
// Sự thật đầu vào (StartPrompt) được ghi xuống đĩa trước lúc phán quyết: khi phán quyết hỏng thì nó là căn cứ để engine bổ sung phán quyết,
// nên khởi động hỏng có thể tự lành từ mọi cửa vào khôi phục (Resume/tiếp tục), không phải ngõ cụt.
func (h *Host) StartPrepared(rawRequirement string) error {
	h.mu.Lock()
	if h.lifecycle == lifecycleRunning {
		h.mu.Unlock()
		return fmt.Errorf("already running")
	}
	if h.cocreating {
		h.mu.Unlock()
		return fmt.Errorf("đồng sáng tác theo giai đoạn đang chạy, hãy kết thúc đồng sáng tác trước")
	}
	h.mu.Unlock()

	rawRequirement = strings.TrimSpace(rawRequirement)
	if rawRequirement == "" {
		return fmt.Errorf("prompt is required")
	}
	if err := h.refuseNewBookOverExisting(); err != nil {
		return err
	}
	if err := upgradeProject(h.store); err != nil {
		return err
	}
	if err := h.budget.Refuse(); err != nil {
		return err
	}
	if err := h.store.Checkpoints.Reset(); err != nil {
		return fmt.Errorf("reset checkpoints: %w", err)
	}
	if err := h.store.Progress.Init(0); err != nil {
		return fmt.Errorf("init progress: %w", err)
	}
	// Sự thật đầu vào ghi xuống đĩa trước lúc phán quyết: sau khi phán quyết hỏng (lỗi mô hình...) StartPrompt vẫn còn,
	// engine bổ sung phán quyết dựa vào đó khi khôi phục/tiếp tục (planStartFallback), nên khởi động hỏng không còn là ngõ cụt.
	if err := h.store.RunMeta.SetStartPrompt(rawRequirement); err != nil {
		return fmt.Errorf("ghi lại yêu cầu sáng tác: %w", err)
	}

	// Phán quyết khởi động: hỏng thì báo lỗi tường minh để dừng (lúc khởi động người dùng còn ở bên, báo lỗi tốt hơn đoán).
	start := time.Now()
	decision, derr := runObservedDecision(h.observer, "phán quyết khởi động", func() (arbiter.PlanStartDecision, error) {
		return arbiter.DecidePlanStart(h.runCtx, h.arbiterModel(),
			h.bundle.Prompts.ArbiterPlanStart, rawRequirement, h.cfg.Style)
	})
	rec := storepkg.DecisionRecord{Kind: "plan_start", Decider: "arbiter", Input: rawRequirement,
		Reason: decision.Reason, DurationMs: time.Since(start).Milliseconds()}
	if derr == nil {
		if data, err := json.Marshal(decision); err == nil {
			rec.Decision = data
		}
	} else {
		rec.Error = derr.Error()
	}
	var recErr error
	if rec, recErr = h.store.Decisions.Append(rec); recErr != nil {
		slog.Warn("ghi audit phán quyết khởi động xuống đĩa thất bại", "module", "host", "err", recErr)
	}
	if derr != nil {
		return fmt.Errorf("phán quyết khởi động thất bại: %w", derr)
	}
	if err := h.store.RunMeta.SetPlanStart(domain.PlanStartRecord{
		RawPrompt: rawRequirement, Planner: decision.Planner, PlannerTask: decision.Task, DecisionID: rec.ID,
	}); err != nil {
		return fmt.Errorf("ghi lại phán quyết khởi động: %w", err)
	}

	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM",
		Summary: fmt.Sprintf("Bắt đầu sáng tác (kiến trúc sư: %s——%s)", decision.Planner, decision.Reason), Level: "info"})
	if !h.startEngine(&flow.Instruction{Agent: decision.Planner, Task: decision.Task, Reason: decision.Reason}) {
		return fmt.Errorf("Engine đã chạy hoặc đang dừng, không thể khởi chạy cuốn sách mới")
	}
	return nil
}

// refuseNewBookOverExisting từ chối mở cuốn sách mới trong thư mục đã có chương: StartPrepared sẽ đặt lại
// checkpoints và progress, gõ nhầm là âm thầm xóa sạch chuỗi tiến độ của cả cuốn sách (sau khi nhập xong dừng ở trang
// chào, bấm nhầm Enter là tình huống điển hình nhất). Chỉ nhìn số chương đã hoàn thành — phần dư của giai đoạn quy hoạch /
// khởi động hỏng chưa có chương nào, nên vẫn cho qua để giữ đường tự lành: thử lại trong cùng phiên bằng Ctrl+S ở đồng sáng tác
// và bổ sung phán quyết khi khôi phục.
func (h *Host) refuseNewBookOverExisting() error {
	progress, err := h.store.Progress.Load()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if progress == nil || len(progress.CompletedChapters) == 0 {
		return nil
	}
	book, err := h.store.Book.Load()
	if err != nil {
		return err
	}
	if book == nil {
		return fmt.Errorf("thư mục đầu ra đã có chương nhưng thông tin tác phẩm không tồn tại")
	}
	name := book.Title
	return fmt.Errorf("thư mục đầu ra đã có «%s» với %d chương đã sáng tác, tạo mới sẽ xoá tiến độ và điểm kiểm tra: muốn viết tiếp hãy dùng lối vào khôi phục (khởi động lại ứng dụng sẽ tự khôi phục), muốn viết sách mới hãy đổi thư mục đầu ra",
		name, len(progress.CompletedChapters))
}

// startEngine là cửa vào khởi chạy engine duy nhất (Start/Resume/Continue/khởi động lại sau can thiệp dùng chung).
// lifecycle phải được đặt thành running trước khi bắt đầu goroutine: engine có thể kết thúc ngay (hoàn thành truyện / không có tuyến),
// runEnded sẽ đưa lifecycle về trạng thái cuối; nếu đảo thứ tự, runEnded chạy trước, ở đây lại ghi running,
// giao diện sẽ mãi hiện "đang chạy" trong khi engine thực tế đã dừng.
func (h *Host) startEngine(initial *flow.Instruction) bool {
	// Cổng chặn xuyên lần khởi động: khi còn workspace nhập chưa hoàn tất, cấm Engine thường tiêu thụ trạng thái nửa đã phát hành (RFC §12.5).
	active, done, importErr := imp.ResumeStatus(h.store)
	if importErr != nil {
		h.emitEvent(Event{Time: time.Now(), Category: "ERROR", Level: "error",
			Summary: "đọc trạng thái nhập thất bại, đã chặn việc sáng tác thường ghi đè lên sản phẩm hiện có: " + importErr.Error()})
		return false
	}
	if active && !done {
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Level: "warn",
			Summary: "còn một lần nhập tiểu thuyết ngoài chưa hoàn tất, hãy chạy /import để khôi phục xong rồi mới tiếp tục sáng tác"})
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return false
	}
	// Khi tác vụ nền độc quyền (nhập/mô phỏng) đang chạy, engine không được chạy trước để tránh tranh chấp ghi với nó. Đây là
	// chốt chặn cuối cùng chung cho mọi đường khởi động engine (Resume/Continue khởi động lại/tự nối tiếp/next) — canh cửa ở đầu vào là lớp thứ nhất, ở đây là lớp cuối.
	if h.exclusive != "" {
		return false
	}
	// lifecycle có thể đã là paused, nhưng goroutine Engine cũ vẫn đang chạy phần defer khi thoát.
	// Phải kiểm tra luôn trạng thái thật của Engine; nếu không sẽ đưa lifecycle về running, trong khi start
	// thực tế là no-op, rồi runEnded cũ lại đặt nó xuống idle.
	if h.engine.isRunning() {
		return false
	}
	h.observer.setAborting(false)
	previous := h.lifecycle
	h.lifecycle = lifecycleRunning
	if !h.engine.start(initial) {
		h.lifecycle = previous
		return false
	}
	return true
}

// Reopen cưỡng ép mở lại cuốn sách đã hoàn tất về trạng thái sáng tác. Hoàn thành truyện và mở lại đều là quyết định lớn: hoàn thành truyện có thể do kiến trúc sư phán quyết,
// còn mở lại chỉ người dùng mới chủ động yêu cầu (/reopen), không qua phán quyết của mô hình. Khi direction khác rỗng thì đăng ký làm can thiệp đang chờ,
// khi khôi phục sẽ được Arbiter phán quyết rồi chèn vào (cùng đường với can thiệp trong lúc dừng), sau đó mới chạy tiếp engine (tuyến cuối tập phát lệnh tập kế tiếp).
func (h *Host) Reopen(direction string) error {
	h.mu.Lock()
	switch {
	case h.lifecycle == lifecycleRunning:
		h.mu.Unlock()
		return fmt.Errorf("Engine sáng tác đang chạy, không cần mở lại")
	case h.cocreating:
		h.mu.Unlock()
		return fmt.Errorf("đồng sáng tác theo giai đoạn đang chạy, hãy kết thúc đồng sáng tác trước")
	case h.exclusive != "":
		ex := h.exclusive
		h.mu.Unlock()
		return fmt.Errorf("Tác vụ %s đang chạy, hãy hoàn tất trước khi mở lại", ex)
	}
	h.mu.Unlock()
	if err := h.requireCleanChapters(); err != nil {
		return err
	}

	if err := h.store.Progress.ReopenContinue(); err != nil {
		return err
	}
	reopenEvent := Event{Time: time.Now(), Category: "SYSTEM", Summary: "đã mở lại cuốn sách ở trạng thái sáng tác (người dùng đã hủy phán quyết hoàn tất)", Level: "info"}
	if d := strings.TrimSpace(direction); d != "" {
		reopenEvent.Detail = reopenEvent.Summary + "\nHướng viết tiếp: " + d
	}
	h.emitEvent(reopenEvent)
	if d := strings.TrimSpace(direction); d != "" {
		if err := h.store.RunMeta.SetPendingSteer(d); err != nil {
			return fmt.Errorf("đã mở lại, nhưng ghi lại hướng viết tiếp thất bại: %v, hãy nhập lại hướng trực tiếp ở ô nhập", err)
		}
	}
	return nil
}

// Resume chế độ khôi phục: sinh resume prompt từ checkpoint + progress rồi khởi chạy.
func (h *Host) Resume() (string, error) {
	h.mu.Lock()
	if h.lifecycle == lifecycleRunning {
		h.mu.Unlock()
		return "", fmt.Errorf("already running")
	}
	if h.cocreating {
		h.mu.Unlock()
		return "", fmt.Errorf("đồng sáng tác theo giai đoạn đang chạy, hãy kết thúc đồng sáng tác trước")
	}
	if h.exclusive != "" {
		ex := h.exclusive
		h.mu.Unlock()
		return "", fmt.Errorf("Tác vụ %s đang chạy, hãy hoàn tất trước khi khôi phục sáng tác", ex)
	}
	h.mu.Unlock()
	if err := upgradeProject(h.store); err != nil {
		return "", err
	}

	label, err := resumeLabel(h.store)
	if err != nil {
		return "", err
	}
	if label == "" {
		return "", nil // chế độ tạo mới, không có gì để khôi phục
	}
	if err := h.requireCleanChapters(); err != nil {
		return label, err
	}
	if err := h.budget.Refuse(); err != nil {
		return "", err
	}

	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "Khôi phục sáng tác: " + label, Level: "info"})
	for _, w := range h.store.CheckConsistency() {
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "Cảnh báo tính nhất quán: " + w, Level: "warn"})
	}
	// Bảo đảm ảnh chụp quy tắc người dùng tồn tại; đã có thì đọc rẻ.
	h.ensureUserRules()
	h.refreshWriterRestore()
	// Can thiệp đang chờ (để lại trong lúc dừng / sót lại do sập giữa lúc phán quyết) phải được phán quyết trước khi engine chạy tiếp —
	// nếu không engine có thể viết tiếp những chương trái với can thiệp. Chạy đồng bộ (chặn vài giây là chấp nhận được,
	// giao diện đã hiện "khôi phục sáng tác"); doIntervention tự xóa PendingSteer sau khi thành công và
	// dựng engine với restart=true. Không có can thiệp đang chờ → chạy tiếp luôn.
	meta, err := h.store.RunMeta.Load()
	if err != nil {
		return label, fmt.Errorf("đọc can thiệp đang chờ xử lý: %w", err)
	}
	if meta != nil && meta.PendingSteer != "" {
		if err := h.doIntervention(meta.PendingSteer, true); err != nil {
			return label, err
		}
	} else {
		// Chỉ khôi phục sự thật, không khôi phục phiên (RFC §6): Engine tính lại tuyến từ store để chạy tiếp.
		if !h.startEngine(nil) {
			return label, fmt.Errorf("Engine đang hoàn tất lần dừng trước, hãy thử khôi phục lại sau")
		}
	}
	// lifecycle do startEngine / runEnded quản lý, ở đây không ghi đè —
	// ghi đè khi engine kết thúc ngay (hoàn thành truyện...) sẽ biến trạng thái cuối thành running.
	return label, nil
}

// handleIntervention thích ứng với callback hỏi lại không trả về của Engine; lỗi đã được doIntervention phát sự kiện.
func (h *Host) handleIntervention(text string) {
	_ = h.doIntervention(text, false)
}

// doIntervention là đường phán quyết thống nhất cho can thiệp của người dùng: Collect → Decide → thực thi.
// Nối tiếp FIFO (cùng lúc có nhiều nhất một lần tư vấn đang trên đường); answer/rules thực thi ngay, hành động
// trạng thái điều khiển (hold/reopen/dispatch) khi engine đang chạy thì xếp hàng ở biên, khi đã dừng thì thực thi ngay.
// Khi restart=true (ngữ nghĩa Continue) thì xử lý xong can thiệp sẽ bảo đảm engine đang chạy.
func (h *Host) doIntervention(text string, restart bool) error {
	h.interMu.Lock()
	defer h.interMu.Unlock()

	// Chống sập: trước lúc phán quyết thì lưu trước (PendingSteer), xóa theo kiểu nguyên tử sau khi áp dụng thành công
	// hoặc sau khi đã báo lỗi trước mặt người dùng (ClearHandledSteer đồng thời đặt lại FlowSteering). Sập trong lúc phán quyết → lần Resume sau phát lại.
	if err := h.store.RunMeta.SetPendingSteer(text); err != nil {
		wrapped := fmt.Errorf("lưu can thiệp thất bại, đã dừng phán quyết: %w", err)
		h.emitEvent(Event{Time: time.Now(), Category: "ERROR", Agent: "arbiter",
			Summary: wrapped.Error(), Detail: wrapped.Error(), Level: "error"})
		return wrapped
	}
	clearPending := func() error {
		if err := h.store.ClearHandledSteer(); err != nil {
			return fmt.Errorf("xoá can thiệp đã xử lý thất bại: %w", err)
		}
		return nil
	}

	facts, err := arbiter.CollectInterventionFacts(h.store)
	if err != nil {
		wrapped := fmt.Errorf("thu thập sự thật của can thiệp thất bại, chưa gọi Tài phán: %w", err)
		h.emitEvent(Event{Time: time.Now(), Category: "ERROR", Agent: "arbiter",
			Summary: wrapped.Error(), Detail: wrapped.Error(), Level: "error"})
		return wrapped
	}
	facts.Running = h.engine.isRunning()

	start := time.Now()
	decision, derr := runObservedDecision(h.observer, "phán quyết can thiệp của người dùng", func() (arbiter.InterventionDecision, error) {
		return arbiter.DecideIntervention(h.runCtx, h.arbiterModel(),
			h.bundle.Prompts.ArbiterIntervention, facts, text)
	})

	rec := storepkg.DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: text,
		Reason: decision.Reason, DurationMs: time.Since(start).Milliseconds()}
	if cp := h.store.Checkpoints.LatestGlobal(); cp != nil {
		rec.CheckpointSeq = cp.Seq
	}
	if data, err := json.Marshal(facts); err == nil {
		rec.Facts = data
	}
	if derr == nil {
		if data, err := json.Marshal(decision); err == nil {
			rec.Decision = data
		}
	} else {
		rec.Error = derr.Error()
	}
	if _, err := h.store.Decisions.Append(rec); err != nil {
		wrapped := fmt.Errorf("ghi audit phán quyết can thiệp xuống đĩa thất bại, từ chối thực thi hành động: %w", err)
		h.emitEvent(Event{Time: time.Now(), Category: "ERROR", Agent: "arbiter",
			Summary: wrapped.Error(), Detail: wrapped.Error(), Level: "error"})
		return wrapped
	}

	if derr != nil {
		// Thà không động, còn không được động sai: không ghi bất cứ thứ gì. Lỗi gọi hàm và
		// lỗi kiểm tra đầu ra dùng chung một đường error, phải hiện nguyên văn, không được gộp thành "không hiểu".
		// Đã báo trước mặt người dùng → xóa pending (nếu không lần Resume sau sẽ tự phát lại đúng can thiệp hỏng đó).
		h.emitEvent(newInterventionFailureEvent(derr))
		if err := clearPending(); err != nil {
			return fmt.Errorf("%v；%w", derr, err)
		}
		return derr
	}

	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "Phán quyết: " + decision.Reason, Level: "info"})
	if decision.Answer != "" {
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: decision.Answer, Level: "info"})
	}
	// Một hành động bất kỳ không lưu được → giữ PendingSteer (khi khôi phục sẽ phát lại trọn vẹn để phán quyết lại;
	// hold/reopen là idempotent, dispatch hỏi lại với sự thật mới, nên phát lại vẫn an toàn).
	var actionErr error
	if decision.Rules != "" {
		if snap, _, err := h.userRules.AddRuntimeRule(h.runCtx, decision.Rules); err != nil {
			h.emitEvent(Event{Time: time.Now(), Category: "ERROR", Summary: "ghi quy tắc sáng tác xuống đĩa thất bại: " + err.Error(), Level: "error"})
			actionErr = err
		} else if snap != nil {
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "quy tắc sáng tác đã cập nhật và được lưu lại", Level: "info"})
		}
	}

	if decision.Hold != nil || decision.Reopen != nil || decision.Dispatch != nil {
		op := controlOp{hold: decision.Hold, reopen: decision.Reopen, dispatch: decision.Dispatch, text: text, facts: facts}
		if !h.engine.enqueue(op) {
			// Engine không chạy: thực thi ngay; lưu không được → giữ PendingSteer, khi khôi phục sẽ phát lại trọn can thiệp.
			if err := h.engine.applyControlOp(context.Background(), op); err != nil {
				h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Level: "warn",
					Summary: "thực thi hành động can thiệp thất bại, đã giữ lại; sẽ tự thử lại khi khôi phục/tiếp tục"})
				return err
			}
			// reopen/dispatch thể hiện ý định tiếp tục sáng tác, nên dựng engine lên.
			if decision.Reopen != nil || decision.Dispatch != nil {
				restart = true
			}
		}
	}
	if actionErr != nil {
		// Giữ PendingSteer: khi khôi phục/tiếp tục sẽ phát lại trọn vẹn để phán quyết lại.
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Level: "warn",
			Summary: "một phần hành động can thiệp không thành công, đã giữ lại can thiệp; sẽ tự thử lại khi khôi phục/tiếp tục"})
		return actionErr
	}
	// Hành động đã áp dụng/xếp hàng thành công, xóa lớp chống sập (sau khi xếp hàng, lỗi phía engine hoặc tranh chấp lúc thoát
	// do engine ghi lại PendingSteer làm lưới an toàn).
	if err := clearPending(); err != nil {
		h.emitEvent(Event{Time: time.Now(), Category: "ERROR", Level: "error", Summary: err.Error()})
		return err
	}

	if restart && !h.engine.isRunning() {
		if err := h.budget.Refuse(); err != nil {
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: err.Error(), Level: "warn"})
			return err
		}
		h.refreshWriterRestore()
		if !h.startEngine(nil) {
			// Lúc này hành động can thiệp đã có hiệu lực và PendingSteer đã xóa, chỉ là engine chưa dựng lên được ngay — không được nói dối là "đã lưu".
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Level: "warn",
				Summary: "can thiệp đã có hiệu lực, nhưng Engine không chạy tiếp được ngay; hãy nhập lệnh tiếp tục ở ô nhập sau hoặc khởi động lại ứng dụng để khôi phục"})
			return fmt.Errorf("can thiệp đã có hiệu lực, nhưng Engine không chạy tiếp được ngay")
		}
	}
	return nil
}

func newInterventionFailureEvent(err error) Event {
	detail := err.Error()
	return Event{
		Time:     time.Now(),
		Category: "ERROR",
		Agent:    "arbiter",
		Summary:  "phán quyết can thiệp thất bại: " + detail + " (không sửa gì cả)",
		Detail:   detail,
		Kind:     errorKind(err, detail),
		Level:    "error",
	}
}

// arbiterModel trả về mô hình phán quyết có theo dõi mức dùng (token/chi phí chảy vào hệ thống ngân sách và usage).
func (h *Host) arbiterModel() agentcore.ChatModel {
	return newUsageTrackedModel(h.models.Default, "arbiter", h.usage.Record)
}

// Continue được gọi khi người dùng nhập vào ô nhập sau khi engine đã dừng: phán quyết can thiệp + bảo đảm engine chạy lại.
func (h *Host) Continue(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("text is required")
	}
	h.mu.Lock()
	if h.cocreating {
		h.mu.Unlock()
		return fmt.Errorf("đồng sáng tác theo giai đoạn đang chạy, hãy kết thúc đồng sáng tác trước")
	}
	if h.exclusive != "" {
		ex := h.exclusive
		h.mu.Unlock()
		// Trong lúc tác vụ độc quyền chạy phải chặn trước lúc phán quyết: nếu không Arbiter đã đổi PendingSteer/quy tắc/trạng thái điều khiển, rồi engine mới bị cổng chặn chặn lại.
		return fmt.Errorf("Tác vụ %s đang chạy, hãy hoàn tất trước khi tiếp tục sáng tác", ex)
	}
	h.mu.Unlock()
	if err := h.requireCleanChapters(); err != nil {
		return err
	}
	if err := h.budget.Refuse(); err != nil {
		return err
	}

	err, launched := h.runAsync(func() error {
		h.emitEvent(Event{Time: time.Now(), Category: "USER", Summary: "[Tiếp tục] " + text, Level: "info"})
		return h.doIntervention(text, true)
	})
	if !launched {
		return fmt.Errorf("Host đang đóng, không thể tiếp tục sáng tác")
	}
	return err
}

// SetAdvanceMode đổi chế độ tiến triển chương theo cách tất định. Nó chỉ ghi ý định chạy của người dùng,
// không gọi Arbiter, cũng không tự khởi chạy engine đã tạm dừng.
func (h *Host) SetAdvanceMode(mode domain.ChapterAdvanceMode) error {
	h.interMu.Lock()
	defer h.interMu.Unlock()
	if err := h.store.RunMeta.SetAdvanceMode(mode); err != nil {
		return err
	}
	label := "tự động tiến"
	if mode == domain.ChapterAdvanceReview {
		label = "nghiệm thu từng chương"
	}
	summary := "Đã đổi chế độ tiến chương sang " + label
	h.mu.Lock()
	state := h.lifecycle
	h.mu.Unlock()
	if mode == domain.ChapterAdvanceAuto && state != lifecycleRunning && state != lifecycleCompleted {
		summary += "; hiện vẫn đang tạm dừng, nhập lệnh tiếp tục để chạy lại"
	}
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: "info"})
	return nil
}

// AdvanceOneChapter cấp quyền cho đúng một chương ở chế độ nghiệm thu từng chương và khởi chạy Engine.
func (h *Host) AdvanceOneChapter() error {
	h.interMu.Lock()
	defer h.interMu.Unlock()

	h.mu.Lock()
	running, cocreating, ex := h.lifecycle == lifecycleRunning, h.cocreating, h.exclusive
	h.mu.Unlock()
	if running || h.engine.isRunning() {
		return fmt.Errorf("sáng tác vẫn đang chạy hoặc đang hoàn tất lần tạm dừng, hãy thử lại /next sau")
	}
	if cocreating {
		return fmt.Errorf("đồng sáng tác theo giai đoạn đang chạy, hãy kết thúc đồng sáng tác trước")
	}
	if ex != "" {
		return fmt.Errorf("Tác vụ %s đang chạy, hãy hoàn tất trước khi chạy /next", ex)
	}
	if err := h.requireCleanChapters(); err != nil {
		return err
	}
	meta, err := h.store.RunMeta.Load()
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("RunMeta chưa khởi tạo")
	}
	if meta.AdvanceMode != domain.ChapterAdvanceReview {
		return fmt.Errorf("/next chỉ dùng cho chế độ nghiệm thu từng chương, hãy chạy /review on trước")
	}
	if meta.AdvanceHold != nil {
		return fmt.Errorf("vẫn còn ý định tạm dừng một lần đang chờ xử lý (%s), hãy khôi phục hoặc hoàn tất can thiệp hiện tại trước", meta.AdvanceHold.Reason)
	}
	if err := h.budget.Refuse(); err != nil {
		return err
	}
	progress, err := h.store.Progress.Load()
	if err != nil {
		return err
	}
	if progress == nil || progress.Phase != domain.PhaseWriting {
		phase := "<nil>"
		if progress != nil {
			phase = string(progress.Phase)
		}
		return fmt.Errorf("giai đoạn hiện tại không cho phép cấp phép chương mới (phase=%s)", phase)
	}
	target := progress.NextChapter()
	if target <= 0 {
		return fmt.Errorf("không suy ra được chương tiếp theo từ tiến độ hiện tại")
	}
	if err := h.store.RunMeta.GrantAdvancePermit(target); err != nil {
		return err
	}
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM",
		Summary: fmt.Sprintf("đã cấp phép chương %d; sau khi chương đó được ghi, hệ thống sẽ hoàn tất phần thẩm duyệt và bảo trì cấu trúc cung/tập cần thiết, rồi lại chờ cấp phép", target), Level: "info"})
	h.refreshWriterRestore()
	if !h.startEngine(nil) {
		// Giấy phép được lưu theo số chương và là idempotent với cùng mục tiêu, nên lần gọi sau thử lại sẽ không cấp phép trùng.
		return fmt.Errorf("giấy phép chương đã lưu, nhưng Engine vẫn đang hoàn tất lần dừng trước; hãy thử lại /next sau")
	}
	return nil
}

// Steer gửi can thiệp của người dùng (dùng được mọi lúc khi đang chạy; khi đã dừng thì phán quyết xong, tuỳ hành động mà quyết định có dựng engine lên không).
// TUI chờ kết quả qua tea.Cmd nên nhận được lỗi phán quyết/lưu trữ thật mà không chặn giao diện.
func (h *Host) Steer(text string) error {
	err, launched := h.runAsync(func() error {
		h.emitEvent(Event{Time: time.Now(), Category: "USER", Summary: "[Can thiệp người dùng] " + text, Level: "info"})
		return h.doIntervention(text, false)
	})
	if !launched {
		return fmt.Errorf("Host đang đóng, không thể gửi can thiệp")
	}
	return err
}

// Abort tạm dừng vòng lặp engine hiện tại.
func (h *Host) Abort() bool {
	return h.abortWithEvent("người dùng tạm dừng sáng tác hiện tại", "warn")
}

// abortWithEvent thực hiện việc tạm dừng với sự kiện mang lý do chỉ định. Dừng vì ngân sách và tạm dừng thủ công dùng chung một cơ chế dừng,
// chỉ khác ở văn bản sự kiện (dừng vì ngân sách = lệnh Abort mà người dùng đã ký trước, ngữ nghĩa tương đương tạm dừng thủ công).
func (h *Host) abortWithEvent(summary, level string) bool {
	h.mu.Lock()
	running := h.lifecycle == lifecycleRunning
	if running {
		h.lifecycle = lifecyclePaused
	}
	cancelExclusive := h.exclusiveCancel
	h.mu.Unlock()
	if running {
		// Phải đặt cờ trước engine.abort: việc cancel lan xuống sẽ lập tức sinh sự kiện lỗi khởi tạo stream / worker,
		// observer dựa vào cờ này để nhận ra nhiễu phái sinh từ abort và dập đi.
		h.observer.setAborting(true)
		h.engine.abort()
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: level})
		return true
	}
	// Engine không chạy nhưng tác vụ độc quyền (nhập...) vẫn đang chạy: nó cũng đang đốt tiền, dừng cứng vì ngân sách / tạm dừng thủ công
	// phải dừng được nó — nếu không chính sách ngân sách với tác vụ nhập là vô nghĩa (docs/import-pipeline.md §13.1).
	if cancelExclusive != nil {
		cancelExclusive()
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: level})
		return true
	}
	return false
}

// Close dừng engine và đóng kênh sự kiện.
//
// Ngữ nghĩa lưu Usage: trước hết huỷ autoSaveLoop (nó tự flush trạng thái dirty lần cuối),
// rồi bổ sung một lần SaveNow đồng bộ để chốt. Vài trăm token cuối cùng của các lệnh gọi LLM đang
// trên đường bị mất sau khi dừng sẽ được bù lại tự động nhờ phát lại session jsonl ở lần khởi động sau.
func (h *Host) Close() {
	h.closeOnce.Do(func() {
		h.mu.Lock()
		h.closing = true
		cancelExclusive := h.exclusiveCancel
		h.mu.Unlock()

		h.observer.setAborting(true)
		if h.runCancel != nil {
			h.runCancel() // cắt các lệnh gọi phán quyết phía host đang trên đường và việc chuyển tiếp của supervisor
		}
		if cancelExclusive != nil {
			cancelExclusive()
		}
		h.engine.abort()
		h.engine.wait()
		h.asyncWG.Wait()

		if h.usageCancel != nil {
			h.usageCancel()
			h.usageCancel = nil
		}
		h.usage.WaitAutoSave()
		if err := h.usage.SaveNow(); err != nil {
			slog.Warn("ghi usage xuống đĩa trước khi thoát thất bại", "module", "usage", "err", err)
		}
		h.closeOutputChannels()
		if err := h.bookLease.Close(); err != nil {
			slog.Error("giải phóng thư mục truyện thất bại", "module", "host", "dir", h.cfg.OutputDir, "err", err)
		}
		if h.logCleanup != nil {
			h.logCleanup()
			h.logCleanup = nil
		}
	})
}

// FileLogError trả về lỗi khởi tạo nhật ký tệp ở giai đoạn dựng; không đổi trong suốt vòng đời Host.
func (h *Host) FileLogError() error {
	return h.fileLogErr
}

// runEnded được engine.onDone gọi khi vòng lặp engine kết thúc (bất kể lý do): xác định trạng thái cuối theo sự thật trong store.
//   - Phase=Complete  → đánh dấu completed, phát sự kiện "sáng tác hoàn tất"
//   - còn lại          → đánh dấu idle/paused, phát sự kiện "sáng tác đã dừng"
func (h *Host) runEnded() {
	h.observer.finalize()

	h.mu.Lock()
	progress, err := h.store.Progress.Load()
	if err != nil {
		if h.lifecycle == lifecycleRunning {
			h.lifecycle = lifecycleIdle
		}
		h.mu.Unlock()
		h.emitEvent(Event{Time: time.Now(), Category: "ERROR", Level: "error",
			Summary: "đọc tiến độ khi Engine kết thúc thất bại: " + err.Error()})
		select {
		case h.done <- struct{}{}:
		default:
		}
		return
	}
	book, err := h.store.Book.Load()
	if err != nil {
		h.lifecycle = lifecycleIdle
		h.mu.Unlock()
		h.emitEvent(Event{Time: time.Now(), Category: "ERROR", Level: "error",
			Summary: "đọc thông tin tác phẩm khi Engine kết thúc thất bại: " + err.Error()})
		select {
		case h.done <- struct{}{}:
		default:
		}
		return
	}
	if progress != nil && progress.Phase == domain.PhaseComplete {
		if book == nil {
			h.lifecycle = lifecycleIdle
			h.mu.Unlock()
			h.emitEvent(Event{Time: time.Now(), Category: "ERROR", Level: "error",
				Summary: "thông tin tác phẩm không tồn tại khi Engine kết thúc"})
			select {
			case h.done <- struct{}{}:
			default:
			}
			return
		}
		h.lifecycle = lifecycleCompleted
		// Chốt sau khi hoàn thành truyện: sinh theo cách tất định (store đã có đủ sự thật, không tốn lệnh gọi LLM; mục cuối RFC).
		summary := completionSummary(*progress, *book)
		h.mu.Unlock()
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: "success"})
		h.notifier.Send(notify.Notification{
			Kind: notify.KindRunEnd, Level: "info", Title: "ainovel: sáng tác hoàn tất",
			Body: h.runEndBody("", summary),
		})
	} else {
		wasRunning := h.lifecycle == lifecycleRunning
		if wasRunning {
			h.lifecycle = lifecycleIdle
		}
		completed := 0
		title := ""
		if progress != nil {
			completed = len(progress.CompletedChapters)
		}
		if book != nil {
			title = book.Title
		}
		h.mu.Unlock()
		if wasRunning {
			summary := fmt.Sprintf("Engine dừng (đã hoàn thành %d chương)", completed)
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: "warn"})
			h.notifier.Send(notify.Notification{
				Kind: notify.KindRunEnd, Level: "warn", Title: "ainovel: sáng tác dừng",
				Body: h.runEndBody(title, summary),
			})
		}
	}

	select {
	case h.done <- struct{}{}:
	default:
	}
}

// runEndBody ghép nội dung thông báo run_end: tên sách + tóm tắt tiến độ + tổng chi phí đã trả.
func (h *Host) runEndBody(title, summary string) string {
	if name := strings.TrimSpace(title); name != "" {
		summary = "《" + name + "》" + summary
	}
	cost, _, _, _, _ := h.usage.Totals()
	if cost > 0 {
		summary += fmt.Sprintf(" · chi phí $%.2f", cost)
	}
	return summary
}

// ── Kênh ──

// StreamClearSentinel gửi qua streamCh bằng một tin duy nhất để báo hiệu "xoá luồng round hiện tại".
// Không dùng clearCh riêng nữa — hai kênh không có thứ tự khiến header ✻ thường rơi vào cuối round trước.
const StreamClearSentinel = "\x00\x00CLEAR\x00\x00"

func (h *Host) Events() <-chan Event  { return h.events }
func (h *Host) Stream() <-chan string { return h.streamCh }
func (h *Host) Done() <-chan struct{} { return h.done }
func (h *Host) Dir() string           { return h.store.Dir() }

// ── Phát sự kiện ──

func (h *Host) emitEvent(ev Event) {
	h.outputMu.RLock()
	defer h.outputMu.RUnlock()
	if h.outputClosed {
		return
	}
	// Đọc khoá bảo đảm các sự kiện trước lúc đóng được ghi trọn vẹn; sự kiện sau khi đóng bị từ chối thẳng.
	LogEvent(ev)
	select {
	case h.events <- ev:
	default:
		select {
		case <-h.events:
		default:
		}
		select {
		case h.events <- ev:
		default:
		}
	}
}

func (h *Host) emitDelta(delta string) {
	h.outputMu.RLock()
	defer h.outputMu.RUnlock()
	if h.outputClosed {
		return
	}
	select {
	case h.streamCh <- delta:
	default:
		select {
		case <-h.streamCh:
		default:
		}
		select {
		case h.streamCh <- delta:
		default:
		}
	}
}

func (h *Host) closeOutputChannels() {
	h.outputMu.Lock()
	defer h.outputMu.Unlock()
	if h.outputClosed {
		return
	}
	h.outputClosed = true
	close(h.done)
	close(h.events)
	close(h.streamCh)
}

func (h *Host) emitClear() {
	// Đi qua streamCh bằng "sentinel", bảo đảm được giao tới TUI theo đúng thứ tự cùng emitDelta.
	h.emitDelta(StreamClearSentinel)
}

// ── Snapshot (tổng hợp trạng thái TUI) ──

func (h *Host) Snapshot() UISnapshot {
	h.mu.Lock()
	state := h.lifecycle
	provider, model, _ := h.models.CurrentSelection("default")
	modelWindow, _ := h.cfg.ResolveContextWindow(provider, model)
	thinkingLevel := h.cfg.ResolveReasoningEffort("default")
	style := h.cfg.Style
	h.mu.Unlock()

	// Phân giải động cửa sổ ngữ cảnh của mô hình hiện tại, sau khi /model hoặc /config đổi thì Snapshot kế tiếp tự phản ánh.
	cost, tokIn, tokOut, cacheRead, cacheWrite := h.usage.Totals()
	saved := h.usage.SavedUSD()
	overallCapable := h.usage.OverallCacheCapable()
	recentRead, recentInput, recentSamples := h.usage.OverallRecent()
	perAgent := h.usage.PerAgent()
	cacheStats := make([]AgentCacheStat, 0, len(perAgent))
	for _, a := range perAgent {
		cacheStats = append(cacheStats, AgentCacheStat{
			Role:            a.Role,
			Input:           a.Input,
			Output:          a.Output,
			CacheRead:       a.CacheRead,
			CacheWrite:      a.CacheWrite,
			Cost:            a.Cost,
			Saved:           a.Saved,
			CacheCapable:    a.CacheCapable,
			RecentCacheRead: a.RecentCacheRead,
			RecentInput:     a.RecentInput,
			RecentSamples:   a.RecentSamples,
		})
	}
	perModel := h.usage.PerModel()
	modelStats := make([]AgentCacheStat, 0, len(perModel))
	for _, a := range perModel {
		modelStats = append(modelStats, AgentCacheStat{
			Model:        a.Model,
			Input:        a.Input,
			Output:       a.Output,
			CacheRead:    a.CacheRead,
			CacheWrite:   a.CacheWrite,
			Cost:         a.Cost,
			Saved:        a.Saved,
			CacheCapable: a.CacheCapable,
		})
	}

	snap := UISnapshot{
		Provider:               provider,
		ModelName:              model,
		ModelContextWindow:     modelWindow,
		ThinkingLevel:          thinkingLevel,
		Style:                  style,
		RuntimeState:           string(state),
		IsRunning:              state == lifecycleRunning,
		TotalInputTokens:       tokIn,
		TotalOutputTokens:      tokOut,
		TotalCacheReadTokens:   cacheRead,
		TotalCacheWriteTokens:  cacheWrite,
		TotalCostUSD:           cost,
		TotalSavedUSD:          saved,
		BudgetLimitUSD:         h.budget.Limit(),
		OverallCacheCapable:    overallCapable,
		OverallRecentCacheRead: recentRead,
		OverallRecentInput:     recentInput,
		OverallRecentSamples:   recentSamples,
		TotalCacheBreaks:       h.usage.OverallCacheBreaks(),
		CachePerAgent:          cacheStats,
		CachePerModel:          modelStats,
		MissingAssistantUsage:  h.usage.MissingAssistantUsage(),
	}

	if book, _ := h.store.Book.Load(); book != nil {
		snap.BookTitle = book.Title
		snap.Synopsis = truncate(book.Synopsis, 200)
	}
	progress, _ := h.store.Progress.Load()
	if progress != nil {
		snap.Phase = string(progress.Phase)
		snap.Flow = string(progress.Flow)
		snap.CurrentChapter = progress.CurrentChapter
		snap.TotalChapters = progress.TotalChapters
		snap.CompletedCount = len(progress.CompletedChapters)
		snap.TotalWordCount = progress.TotalWordCount
		snap.InProgressChapter = progress.InProgressChapter
		snap.PendingRewrites = progress.PendingRewrites
		snap.RewriteReason = progress.RewriteReason
		snap.Layered = progress.Layered
		if progress.CurrentVolume > 0 {
			snap.CurrentVolumeArc = fmt.Sprintf("Tập %d · Cung %d", progress.CurrentVolume, progress.CurrentArc)
		}
	}
	if meta, _ := h.store.RunMeta.Load(); meta != nil {
		snap.PendingSteer = meta.PendingSteer
		snap.AdvanceMode = string(meta.AdvanceMode)
		snap.AdvancePermitChapter = meta.AdvancePermitChapter
		if meta.AdvanceHold != nil {
			snap.HasAdvanceHold = true
			snap.AdvanceHoldReason = meta.AdvanceHold.Reason
		}
	}

	snap.Agents = h.observer.agentSnapshots()
	snap.StatusLabel = deriveStatusLabel(snap)

	// Nhãn khôi phục
	if label, err := resumeLabel(h.store); err == nil && label != "" {
		snap.RecoveryLabel = label
	}

	h.fillDetails(&snap, progress)

	return snap
}

// fillDetails điền khu vực chi tiết: thiết lập, nhân vật, commit/review/tóm tắt gần nhất.
func (h *Host) fillDetails(snap *UISnapshot, progress *domain.Progress) {
	if premise, _ := h.store.Outline.LoadPremise(); premise != "" {
		snap.Premise = truncate(premise, 80)
	}
	if outline, _ := h.store.Outline.LoadOutline(); len(outline) > 0 {
		completed := make(map[int]struct{})
		if progress != nil {
			completed = make(map[int]struct{}, len(progress.CompletedChapters))
			for _, chapter := range progress.CompletedChapters {
				completed[chapter] = struct{}{}
			}
		}
		for _, e := range outline {
			title := e.Title
			if _, ok := completed[e.Chapter]; ok {
				committedTitle, err := h.store.Summaries.LoadSummaryTitle(e.Chapter)
				if err != nil {
					slog.Warn("chiếu tiêu đề chương thất bại", "module", "host.snapshot", "chapter", e.Chapter, "err", err)
				} else if strings.TrimSpace(committedTitle) != "" {
					title = committedTitle
				}
			}
			snap.Outline = append(snap.Outline, OutlineSnapshot{
				Chapter: e.Chapter, Title: title, CoreEvent: e.CoreEvent,
			})
		}
	}
	if progress != nil && progress.Layered {
		if compass, _ := h.store.Outline.LoadCompass(); compass != nil {
			snap.CompassDirection = compass.EndingDirection
			snap.CompassScale = compass.EstimatedScale
		}
		if volumes, _ := h.store.Outline.LoadLayeredOutline(); len(volumes) > 0 {
			for _, v := range volumes {
				if v.Index > progress.CurrentVolume {
					snap.NextVolumeTitle = v.Title
					break
				}
			}
		}
	}
	if chars, _ := h.store.Characters.Load(); len(chars) > 0 {
		for _, c := range chars {
			label := c.Name
			if c.Role != "" {
				label += "（" + c.Role + "）"
			}
			snap.Characters = append(snap.Characters, label)
		}
	}
	if ledger, _ := h.store.Cast.Load(); len(ledger) > 0 {
		snap.SupportingCount = len(ledger)
		recent, _ := h.store.Cast.RecentActive(5)
		for _, e := range recent {
			label := e.Name
			if e.BriefRole != "" {
				label += "（" + e.BriefRole + "）"
			}
			snap.RecentSupporting = append(snap.RecentSupporting, label)
		}
	}
	if progress != nil && len(progress.CompletedChapters) > 0 {
		lastCh := progress.CompletedChapters[len(progress.CompletedChapters)-1]
		wc := progress.ChapterWordCounts[lastCh]
		snap.LastCommitSummary = fmt.Sprintf("Chương %d · %d từ", lastCh, wc)
	}
	currentCh := 1
	if progress != nil && len(progress.CompletedChapters) > 0 {
		currentCh = progress.CompletedChapters[len(progress.CompletedChapters)-1]
	}
	if review, err := h.store.World.LoadLastReview(currentCh); err == nil && review != nil {
		snap.LastReviewSummary = fmt.Sprintf("verdict=%s · %d vấn đề", review.Verdict, len(review.Issues))
		if len(review.AffectedChapters) > 0 {
			snap.LastReviewSummary += fmt.Sprintf(" · ảnh hưởng %v", review.AffectedChapters)
		}
	}
	if cp := h.store.Checkpoints.LatestGlobal(); cp != nil {
		snap.LastCheckpointName = fmt.Sprintf("%s.%s", cp.Scope, cp.Step)
	}
	if progress != nil {
		for i := len(progress.CompletedChapters) - 1; i >= 0 && len(snap.RecentSummaries) < 2; i-- {
			ch := progress.CompletedChapters[i]
			if summary, err := h.store.Summaries.LoadSummary(ch); err == nil && summary != nil {
				snap.RecentSummaries = append(snap.RecentSummaries,
					fmt.Sprintf("Chương %d: %s", ch, truncate(summary.Summary, 50)))
			}
		}
	}
}

func deriveStatusLabel(s UISnapshot) string {
	switch {
	case s.Phase == string(domain.PhaseComplete):
		return "COMPLETE"
	case s.Flow == string(domain.FlowReviewing):
		return "REVIEW"
	case s.Flow == string(domain.FlowRewriting) || s.Flow == string(domain.FlowPolishing):
		return "REWRITE"
	case s.RuntimeState == "running":
		return "RUNNING"
	default:
		return "READY"
	}
}

// ── Quản lý mô hình ──

func (h *Host) ConfiguredProviders() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	providers := make([]string, 0, len(h.cfg.Providers))
	for name := range h.cfg.Providers {
		providers = append(providers, name)
	}
	sort.Strings(providers)
	return providers
}

func (h *Host) ConfiguredModels(provider string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.CandidateModels(provider)
}

func (h *Host) CurrentModelSelection(role string) (string, string, bool) {
	return h.models.CurrentSelection(role)
}

func (h *Host) SwitchModel(role, provider, model string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if provider == "" || model == "" {
		return fmt.Errorf("provider and model are required")
	}
	if err := h.models.Swap(role, provider, model); err != nil {
		return err
	}
	if role == "" || role == "default" {
		h.cfg.Provider = provider
		h.cfg.ModelName = model
	} else {
		if h.cfg.Roles == nil {
			h.cfg.Roles = make(map[string]bootstrap.RoleConfig)
		}
		rc := h.cfg.Roles[role]
		rc.Provider = provider
		rc.Model = model
		h.cfg.Roles[role] = rc
	}
	// Đổi mô hình không sửa ý định mức suy luận đã lưu: chỉ bị chặn trần theo năng lực mô hình mới lúc phát lệnh.
	if h.configPath != "" {
		if err := bootstrap.SaveConfig(h.configPath, h.cfg); err != nil {
			slog.Warn("lưu cấu hình thất bại", "module", "host", "err", err)
		}
	}
	h.applyThinkingLocked(role)
	// Khi chuyển sang mô hình chưa đăng ký thì in một dòng warn, báo cho người dùng biết đã rơi vào phương án dự phòng 128k — truyện dài dễ bị nén sớm.
	logRole := role
	if logRole == "" {
		logRole = "default"
	}
	window, source := h.cfg.ResolveContextWindow(provider, model)
	bootstrap.LogContextWindowChoice(logRole, model, window, source)

	// Không có ngữ cảnh thường trú nào cần liên kết: ContextManager của writer/architect/editor đi qua
	// ContextManagerFactory, lần spawn sau sẽ tự dựng lại theo cửa sổ của mô hình mới.

	h.emitEvent(Event{
		Time:     time.Now(),
		Category: "SYSTEM",
		Summary:  fmt.Sprintf("Đã đổi mô hình: %s → %s/%s", role, provider, model),
		Level:    "info",
	})
	return nil
}

// concreteThinkingRoles là các vai trò cụ thể có thể áp dụng mức suy luận (khớp với định tuyến của agents.ApplyThinking).
// Khi gọi với default thì áp lại từng vai trò theo ResolveReasoningEffort của vai trò đó.
var concreteThinkingRoles = []string{"architect", "writer", "editor"}

// CurrentThinking trả về chuỗi mức suy luận gốc đang có hiệu lực của một vai trò (để bảng /model đồng bộ giá trị hiện tại).
func (h *Host) CurrentThinking(role string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.ResolveReasoningEffort(strings.ToLower(strings.TrimSpace(role)))
}

func (h *Host) AvailableThinking(role string) []agentcore.ThinkingLevel {
	h.mu.Lock()
	model := h.models.ForRole(strings.ToLower(strings.TrimSpace(role)))
	h.mu.Unlock()
	return agents.AvailableThinkingForModel(model)
}

// resolveThinkingForRoleLocked tính mức suy luận thực sự có hiệu lực của một vai trò: lấy ý định gốc
// (ResolveReasoningEffort: cấp vai trò → mặc định cấp cao nhất), rồi bị chặn trần theo năng lực
// của mô hình hiện tại của vai trò đó. Việc chặn trần chỉ xảy ra trên đường "có hiệu lực" này, không ghi
// ngược cấu hình — nơi lưu luôn giữ ý định gốc của người dùng.
func (h *Host) resolveThinkingForRoleLocked(role string) agentcore.ThinkingLevel {
	parsed, _ := agents.ParseThinkingLevel(h.cfg.ResolveReasoningEffort(role))
	resolved, _ := agents.ResolveThinkingForModel(h.models.ForRole(role), parsed)
	return resolved
}

// applyThinkingLocked phát mức suy luận có hiệu lực xuống agent đang chạy; mỗi vai trò bị chặn trần theo mô hình riêng.
func (h *Host) applyThinkingLocked(role string) {
	if h.thinkingApplier == nil {
		return
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" || role == "default" {
		for _, r := range concreteThinkingRoles {
			h.thinkingApplier(r, h.resolveThinkingForRoleLocked(r))
		}
		return
	}
	h.thinkingApplier(role, h.resolveThinkingForRoleLocked(role))
}

// SetRoleThinking đặt mức suy luận cho một vai trò (hoặc default): kiểm tra → lưu → liên kết agent đang chạy → sự kiện.
// Lặp lại cấu trúc của SwitchModel; trục độc lập với việc chọn mô hình, có thể chỉnh riêng. level rỗng = không ghi đè (kế thừa).
func (h *Host) SetRoleThinking(role, level string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	parsed, err := agents.ParseThinkingLevel(level)
	if err != nil {
		return err
	}
	role = strings.ToLower(strings.TrimSpace(role))
	// Nơi lưu giữ ý định gốc: lưu thẳng mức người dùng chọn, việc chặn trần chỉ xảy ra lúc phát lệnh (applyThinkingLocked) theo năng lực mô hình.
	if role == "" || role == "default" {
		h.cfg.ReasoningEffort = string(parsed)
	} else {
		if h.cfg.Roles == nil {
			h.cfg.Roles = make(map[string]bootstrap.RoleConfig)
		}
		rc := h.cfg.Roles[role]
		rc.ReasoningEffort = string(parsed)
		h.cfg.Roles[role] = rc
	}
	if h.configPath != "" {
		if err := bootstrap.SaveConfig(h.configPath, h.cfg); err != nil {
			slog.Warn("lưu cấu hình thất bại", "module", "host", "err", err)
		}
	}

	// Liên kết agent đang chạy: vai trò cụ thể thì áp thẳng; default thì duyệt từng vai trò cụ thể và áp lại theo
	// ResolveReasoningEffort (vai trò đã bị cấp vai trò ghi đè thì giữ của riêng nó, vai trò chưa ghi đè thì nhận mặc định mới).
	h.applyThinkingLocked(role)

	logRole := role
	if logRole == "" {
		logRole = "default"
	}
	shown := string(parsed)
	if shown == "" {
		shown = "mặc định (kế thừa)"
	}
	h.emitEvent(Event{
		Time:     time.Now(),
		Category: "SYSTEM",
		Summary:  fmt.Sprintf("Đã đổi mức suy luận: %s → %s", logRole, shown),
		Level:    "info",
	})
	return nil
}

// ── Phát lại sự kiện ──

func (h *Host) ReplayQueue(afterSeq int64) ([]domain.RuntimeQueueItem, error) {
	if h.store == nil || h.store.Runtime == nil {
		return nil, nil
	}
	return h.store.Runtime.LoadQueueAfter(afterSeq)
}

// ── Đồng sáng tác ──

// CoCreateStream đồng sáng tác lạnh: làm rõ nhu cầu từ đầu, sinh chỉ dẫn sáng tác cho cả tác phẩm.
func (h *Host) CoCreateStream(ctx context.Context, history []CoCreateMessage, onProgress func(kind, text string)) (CoCreateReply, error) {
	t := hostTextsFor(h.store.Language())
	return coCreateStream(ctx, h.models, h.store.Sessions, t.coCreateSystemPrompt, history, onProgress)
}

// StageCoCreateStream đồng sáng tác theo giai đoạn: quy hoạch hướng đi tiếp theo dựa trên nội dung đã viết.
// System prompt = prompt giai đoạn + tóm tắt trạng thái truyện hiện tại, để trợ lý biết "đã viết được những gì".
func (h *Host) StageCoCreateStream(ctx context.Context, history []CoCreateMessage, onProgress func(kind, text string)) (CoCreateReply, error) {
	t := hostTextsFor(h.store.Language())
	return coCreateStream(ctx, h.models, h.store.Sessions, stageSystemPrompt(h.store, t), history, onProgress)
}

// stagePlanPrefix bọc "brief hướng đi tiếp theo" do đồng sáng tác sinh ra thành một can thiệp quy hoạch theo giai đoạn, đưa Tài phán phán quyết.
// Chỉ dán nhãn sự thật [Lập kế hoạch theo giai đoạn] + phát biểu trung tính, không viết cứng "làm thế nào" — định tuyến cụ thể (compass / architect /
// user_rules) giao cho tiêu chí «Lập kế hoạch theo giai đoạn» trong arbiter-intervention.md, tránh tạo nguồn sự thật thứ hai cạnh prompt,
// và không bịt yêu cầu mang tính phong cách đi qua user_rules (giữ "phân loại can thiệp thuộc về LLM"). Continue sẽ chồng thêm tiều tố [Can thiệp người dùng].
//
// Văn bản tiều tố đi theo ngôn ngữ tác phẩm, xem hostTexts.stagePlanPrefix.

// PauseForCoCreate bước vào đồng sáng tác theo giai đoạn: đặt cờ chiếm đồng sáng tác, nếu đang chạy thì tạm dừng Engine luôn.
// Trả về false nghĩa là không vào được (cả cuốn sách đã hoàn tất hoặc đang đồng sáng tác), bên gọi cứ bỏ qua.
// Cờ chiếm chặn can thiệp đồng thời từ import/simulate/start/resume/continue trong cửa sổ đồng sáng tác —
// sau khi tạm dừng khi đang chạy thì lifecycle=paused, cơ chế loại trừ ==running hiện có mất hiệu lực, nên cờ này bù lại;
// đã dừng (idle/paused) vẫn cho vào, quy hoạch xong thì qua Continue để chạy tiếp.
func (h *Host) PauseForCoCreate() bool {
	h.mu.Lock()
	if h.cocreating || h.lifecycle == lifecycleCompleted {
		h.mu.Unlock()
		return false
	}
	h.cocreating = true
	running := h.lifecycle == lifecycleRunning
	h.mu.Unlock()

	// Khi đang chạy thì tái dùng abortWithEvent để dừng (running→paused + setAborting + Abort + sự kiện), cùng thứ tự với tạm dừng
	// thủ công, không chép lại thêm một bản; đã dừng (idle/paused) thì chỉ đặt cờ, quy hoạch xong qua Continue để chạy tiếp.
	if running {
		h.abortWithEvent("đã vào đồng sáng tác theo giai đoạn, sáng tác đã tạm dừng", "info")
	} else {
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "đã vào đồng sáng tác theo giai đoạn", Level: "info"})
	}
	return true
}

// ResumeFromCoCreate kết thúc đồng sáng tác theo giai đoạn: chèn hướng đi tiếp theo do đồng sáng tác sinh ra như một can thiệp rồi khôi phục sáng tác.
// Sau khi xóa cờ chiếm, tái dùng đường chèn can thiệp của Continue (chịu ràng buộc kiểm tra ngân sách trước).
// Lưu ý: khi draft rỗng thì trả về sớm mà không xóa cờ là cố ý (đồng sáng tác chưa kết thúc); canh canStart() phía TUI
// và chỗ này dùng chung tiêu chí "không rỗng", bảo đảm đường này không tới được, nên cocreating không bị rò.
func (h *Host) ResumeFromCoCreate(draft string) error {
	draft = strings.TrimSpace(draft)
	if draft == "" {
		return fmt.Errorf("draft is required")
	}
	h.mu.Lock()
	if !h.cocreating {
		h.mu.Unlock()
		return fmt.Errorf("not in co-create")
	}
	h.cocreating = false
	h.mu.Unlock()

	// Lệnh abort của PauseForCoCreate là bất đồng bộ: chờ vòng lặp engine hội tụ thật rồi mới tiếp tục, để trở lại
	// tiền đề "đã dừng thật" giống Continue sau khi tạm dừng thủ công. Cửa sổ đồng sáng tác ở thang thời gian tương tác
	// người-máy, thăm dò ngắn không tạo cảm giác giật.
	for h.engine.isRunning() {
		time.Sleep(20 * time.Millisecond)
	}

	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "đồng sáng tác theo giai đoạn đã xong, đã chèn hướng tiếp theo và khôi phục sáng tác", Level: "info"})
	return h.Continue(hostTextsFor(h.store.Language()).stagePlanPrefix + draft)
}

// CancelCoCreate bỏ dở đồng sáng tác theo giai đoạn: xóa cờ chiếm, giữ trạng thái tạm dừng (người dùng có thể tiếp tục trong ô nhập hoặc khởi động lại Resume).
func (h *Host) CancelCoCreate() {
	h.mu.Lock()
	if !h.cocreating {
		h.mu.Unlock()
		return
	}
	h.cocreating = false
	h.mu.Unlock()
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "đã thoát đồng sáng tác theo giai đoạn, sáng tác vẫn tạm dừng (có thể nhập lệnh tiếp tục ở ô nhập)", Level: "info"})
}

// ── Công cụ ──

func (h *Host) refreshWriterRestore() {
	if h.writerRestore != nil {
		h.writerRestore.Refresh(h.store)
	}
}

func (h *Host) CheckChapterRevisions() ([]int, error) {
	pending, err := h.store.Revisions.LoadPending()
	if err != nil {
		return nil, fmt.Errorf("đọc bản ghi khôi phục bản sửa: %w", err)
	}
	if pending != nil {
		chapters := make([]int, 0, len(pending.Items))
		for _, item := range pending.Items {
			chapters = append(chapters, item.Chapter)
		}
		return chapters, nil
	}
	changes, err := revision.Scan(h.store)
	if err != nil {
		return nil, err
	}
	return revision.ChangedChapters(changes), nil
}

func (h *Host) SyncChapterRevisions(ctx context.Context) (*revision.Result, error) {
	if err := h.acquireExclusive("đồng bộ bản sửa chương"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	h.mu.Lock()
	h.exclusiveCancel = cancel
	h.mu.Unlock()
	defer h.releaseExclusive()

	pending, err := h.store.Revisions.LoadPending()
	if err != nil {
		return nil, err
	}
	if pending == nil {
		changes, err := revision.Scan(h.store)
		if err != nil {
			return nil, err
		}
		if len(changes) == 0 {
			return &revision.Result{}, nil
		}
		if err := h.budget.Refuse(); err != nil {
			return nil, err
		}
	}
	model := h.models.ForRoleWithFailover("editor", func(ev bootstrap.FailoverEvent) {
		slog.Warn("chuyển provider sửa chương", "module", "revision", "role", ev.Role,
			"reason", ev.Reason, "from", fmt.Sprintf("%s/%s", ev.FromProvider, ev.FromModel),
			"to", fmt.Sprintf("%s/%s", ev.ToProvider, ev.ToModel), "err", ev.Err)
	})
	model = newUsageTrackedModel(model, "editor", h.usage.Record)
	service := revision.NewService(h.store, model, h.bundle.Prompts.RevisionAnalyze, h.styleStats)
	return service.Sync(ctx)
}

func (h *Host) requireCleanChapters() error {
	chapters, err := h.CheckChapterRevisions()
	if err != nil {
		return fmt.Errorf("kiểm tra bản sửa bên ngoài của chương: %w", err)
	}
	if len(chapters) > 0 {
		return fmt.Errorf("phát hiện nội dung chương đã bị sửa từ bên ngoài: %v; hãy chạy /sync trước", chapters)
	}
	return nil
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// ImportFrom chạy một lần nhập có biên dịch ngữ nghĩa từ tiểu thuyết bên ngoài: ingest → segment → analyze → synthesize → publish.
// Mô hình chỉ phán quyết phần ngữ nghĩa mở (biên/sự thật/tổng hợp), Go phụ trách tọa độ/ghi đè/tính lũy thừa; loại trừ với lúc Engine chạy,
// sau khi nhập xong thì AdvanceHold quyết định có viết tiếp hay không.
// Kênh sự kiện trả về do imp.Run đóng, bên gọi chịu trách nhiệm tiêu thụ (đầy thì bỏ để tránh chặn goroutine của pipeline).
func (h *Host) ImportFrom(ctx context.Context, opts imp.Options) (<-chan imp.Event, error) {
	// Kiểm tra tiền đề ngân sách trước khi khởi chạy cùng kỷ luật với Start/Resume/Continue: nhập là chuỗi lệnh gọi mô hình
	// xuyên suốt, đã quá hạn ngân sách thì không được khởi chạy (§13.1 «đưa vào ngân sách sentinel hiện có»).
	if err := h.budget.Refuse(); err != nil {
		return nil, err
	}
	if err := h.acquireExclusive("nhập"); err != nil {
		return nil, err
	}
	// Đăng ký hàm huỷ: dừng cứng vì ngân sách / tạm dừng thủ công sẽ huỷ context riêng của tác vụ nhập qua abortWithEvent
	// (nếu không thì người canh chỉ đi tạm dừng Engine chưa chạy, còn tác vụ nhập vẫn đốt tiền).
	ctx, cancel := context.WithCancel(ctx)
	h.mu.Lock()
	h.exclusiveCancel = cancel
	h.mu.Unlock()

	deps := imp.Deps{
		Store:         h.store,
		CommitChapter: tools.NewCommitChapterTool(h.store, h.styleStats),
		Segment:       h.importCaller("segment"),
		Analyze:       h.importCaller("analyze"),
		Synthesize:    h.importCaller("synthesize"),
		Prompts: imp.Prompts{
			Segment:    h.bundle.Prompts.ImportSegment,
			Analyze:    h.bundle.Prompts.ImportAnalyze,
			Synthesize: h.bundle.Prompts.ImportSynthesize,
			Range:      h.bundle.Prompts.ImportRange,
		},
	}
	ch, err := imp.Run(ctx, deps, opts)
	if err != nil {
		h.releaseExclusive()
		return nil, err
	}
	return h.superviseImport(ch, opts), nil
}

// ImportResumeHint trả về một dòng nhắc về lần nhập chưa hoàn tất (rỗng nếu không có), để TUI chủ động báo khi khởi động (RFC §18.2).
// Chỉ gọi một lần lúc khởi động: bên trong sẽ tính lại InputDigest của từng artefact trong workspace, không hợp để đưa vào vòng thăm dò snapshot.
func (h *Host) ImportResumeHint() string {
	return imp.ResumeSummary(h.store)
}

// importCaller phân giải tầng mô hình cho một hàm ngữ nghĩa của tác vụ nhập (RFC §13.1): nếu cấu hình roles có import_<fn>
// thì dùng tầng đó (mức dùng cũng ghi vào sổ của vai trò đó), nếu không thì rơi về architect. Đây là cấu hình lệnh gọi, không đổi bất kỳ hợp đồng ngữ nghĩa nào.
func (h *Host) importCaller(fn string) imp.Caller {
	role := "import_" + fn
	if _, _, explicit := h.models.CurrentSelection(role); !explicit {
		role = "architect"
	}
	model := h.models.ForRoleWithFailover(role, func(ev bootstrap.FailoverEvent) {
		slog.Warn("chuyển provider nhập", "module", "import", "role", ev.Role,
			"reason", ev.Reason,
			"from", fmt.Sprintf("%s/%s", ev.FromProvider, ev.FromModel),
			"to", fmt.Sprintf("%s/%s", ev.ToProvider, ev.ToModel),
			"err", ev.Err)
	})
	model = newUsageTrackedModel(model, role, h.usage.Record)
	return imp.Caller{Model: model, Runtime: h.importModelRuntime(role, model)}
}

// importModelRuntime dò năng lực gọi của mô hình ở tầng vai trò đã chọn, phục vụ ngân sách kép / thinking thích ứng của imp (RFC §13/§21).
// Trường dò lỗi để giá trị 0, phía imp lùi về mặc định thận trọng, bảo đảm không có thông tin năng lực vẫn chạy đúng.
// Cấu trúc đầu ra do llmcontract của imp đọc sự thật mô hình tại chỗ trước mỗi lệnh gọi, không lưu đệm lại trong Runtime.
func (h *Host) importModelRuntime(role string, model agentcore.ChatModel) imp.ModelRuntime {
	var rt imp.ModelRuntime
	provider, name, _ := h.models.CurrentSelection(role)
	if name == "" {
		name = bootstrap.ModelName(model)
		provider = bootstrap.ModelProvider(model)
	}
	// Trần context / completion: registry là nguồn tin cậy duy nhất (Info() của mô hình đã bọc không chứa cửa sổ).
	rt.ContextTokens, _ = h.cfg.ResolveContextWindow(provider, name)
	if entry, ok := modelreg.DefaultRegistry().Resolve(name); ok {
		rt.MaxOutputTokens = entry.MaxTokens
	}
	// thinking: phân giải theo reasoning effort của vai trò và năng lực mô hình; không hỗ trợ thì không gửi (cùng chiến lược với arbiter).
	if level, err := agents.ParseThinkingLevel(h.cfg.ResolveReasoningEffort(role)); err == nil {
		if resolved, ok := agents.ResolveThinkingForModel(model, level); ok {
			rt.Thinking = resolved
		}
	}
	return rt
}

// Simulate đọc thư mục simulate và tạo hoặc cập nhật dần hồ sơ mô phỏng.
func (h *Host) Simulate(ctx context.Context) (<-chan sim.Event, error) {
	if err := h.acquireExclusive("tạo hồ sơ mô phỏng"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	h.mu.Lock()
	h.exclusiveCancel = cancel
	h.mu.Unlock()

	wd, err := os.Getwd()
	if err != nil {
		h.releaseExclusive()
		return nil, fmt.Errorf("get working dir: %w", err)
	}
	deps := sim.Deps{
		Store: h.store,
		LLM:   h.models.ForRole("architect"),
		Prompts: sim.Prompts{
			Source: h.bundle.Prompts.SimulationSource,
			Merge:  h.bundle.Prompts.SimulationMerge,
		},
	}
	ch, err := sim.Run(ctx, deps, sim.Options{SourceDir: filepath.Join(wd, "simulate")})
	if err != nil {
		h.releaseExclusive()
		return nil, err
	}
	return superviseExclusive(h, ch), nil
}

// ImportSimulationProfile nhập hồ sơ mô phỏng đã tạo trước đó.
func (h *Host) ImportSimulationProfile(ctx context.Context, path string) (<-chan sim.Event, error) {
	if err := h.acquireExclusive("nhập hồ sơ mô phỏng"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	h.mu.Lock()
	h.exclusiveCancel = cancel
	h.mu.Unlock()
	ch, err := sim.RunImport(ctx, h.store, path)
	if err != nil {
		h.releaseExclusive()
		return nil, err
	}
	return superviseExclusive(h, ch), nil
}

// acquireExclusive chiếm nguyên tử ô độc quyền cho tác vụ nền (import/simulate/revision): từ chối khi Engine đang chạy, đang trong cửa sổ đồng sáng tác theo giai đoạn,
// hoặc đã có tác vụ độc quyền khác đang chạy. Thành công là đăng ký chiếm ô, khi tác vụ kết thúc phải gọi releaseExclusive để trả lại — nếu không hai lần nhập
// hoặc nhập + mô phỏng sẽ tranh nhau sửa cùng một trạng thái. Vá lỗ hổng trước đây chỉ kiểm tra ==running/cocreating mà không đăng ký chính tác vụ.
func (h *Host) acquireExclusive(action string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.closing:
		return fmt.Errorf("Host đang đóng, không thể %s", action)
	// engine.isRunning() phải kiểm tra: Abort đặt lifecycle=paused trước rồi mới chờ goroutine thoát bất đồng bộ,
	// trong khoảng đó lifecycle đã khác running nhưng engine vẫn có thể đang ghi store (cùng kỷ luật với cổng chặn lúc khởi chạy).
	case h.lifecycle == lifecycleRunning || h.engine.isRunning():
		return fmt.Errorf("Engine sáng tác đang chạy hoặc đang dừng, hãy thử lại %s sau", action)
	case h.cocreating:
		return fmt.Errorf("đồng sáng tác theo giai đoạn đang chạy, hãy kết thúc đồng sáng tác trước khi %s", action)
	case h.exclusive != "":
		return fmt.Errorf("Tác vụ %s đang chạy, hãy hoàn tất trước khi %s", h.exclusive, action)
	}
	h.exclusive = action
	return nil
}

// releaseExclusive trả lại ô độc quyền cho tác vụ nền (kèm hàm huỷ đã đăng ký).
func (h *Host) releaseExclusive() {
	h.mu.Lock()
	cancel := h.exclusiveCancel
	h.exclusive = ""
	h.exclusiveCancel = nil
	h.mu.Unlock()
	if cancel != nil {
		cancel() // tác vụ đã kết thúc: trả context phái sinh; không có tác dụng phụ với runner đã thoát
	}
}

// superviseExclusive chuyển tiếp sự kiện của tác vụ độc quyền, khi kênh đóng (tác vụ kết thúc) thì trả lại ô chiếm.
func superviseExclusive[T any](h *Host, src <-chan T) <-chan T {
	out := make(chan T, 32)
	if !h.launchAsync(func() {
		defer close(out)
		defer h.releaseExclusive()
		for ev := range src {
			select {
			case out <- ev:
			case <-h.runCtx.Done():
				// Trong lúc đóng vẫn tiếp tục rút cạn kênh nguồn, tránh producer bị chặn ở sự kiện cuối cùng mà không thoát được.
				for range src {
				}
				return
			}
		}
	}) {
		close(out)
		h.releaseExclusive()
	}
	return out
}

// superviseImport là chủ sở hữu duy nhất của câu hỏi "sau khi nhập xong có nối tiếp không": chuyển tiếp sự kiện nhập, khi hoàn tất thì trả lại ô độc quyền trước,
// rồi mới quyết định và thực hiện việc nối tiếp, cuối cùng ghi kết quả nối tiếp thật vào trường Continued của sự kiện StageDone. TUI chỉ dựa vào đó để hiển thị,
// không còn tự suy đoán trạng thái chạy từ cờ --continue cục bộ (loại bỏ tranh chấp thứ tự do ba bên Runner/Host/TUI hiểu khác nhau).
func (h *Host) superviseImport(src <-chan imp.Event, opts imp.Options) <-chan imp.Event {
	out := make(chan imp.Event, 32)
	if !h.launchAsync(func() {
		defer close(out)
		released := false
		release := func() {
			if !released {
				released = true
				h.releaseExclusive()
			}
		}
		defer release()
		for ev := range src {
			if ev.Stage == imp.StageDone {
				release() // trả lại ô độc quyền trước, để startEngine của lượt nối tiếp vượt qua được cổng chặn độc quyền
				ev.Continued = h.continueAfterImport(opts)
			}
			select {
			case out <- ev:
			case <-h.runCtx.Done():
				for range src {
				}
				return
			}
		}
	}) {
		close(out)
		h.releaseExclusive()
	}
	return out
}

// launchAsync đăng ký một tác vụ nền trong vòng đời Host. closing và WaitGroup.Add cùng nằm dưới
// một khoá, bảo đảm sau khi Close bắt đầu Wait sẽ không còn Add mới.
func (h *Host) launchAsync(fn func()) bool {
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		return false
	}
	h.asyncWG.Add(1)
	h.mu.Unlock()
	go func() {
		defer h.asyncWG.Done()
		fn()
	}()
	return true
}

// runAsync tái sử dụng cơ chế đăng ký tác vụ nền sẵn có của Host, đồng thời trả lỗi nghiệp vụ lại cho bên gọi.
func (h *Host) runAsync(fn func() error) (error, bool) {
	result := make(chan error, 1)
	if !h.launchAsync(func() { result <- fn() }) {
		return nil, false
	}
	return <-result, true
}

// continueAfterImport quyết định và thực hiện việc nối tiếp tự động thật của --continue, trả về Engine đã khởi chạy hay chưa.
// Ý định nối tiếp hợp lệ = opts lần này hoặc intent đã lưu trong workspace (phủ kịch bản khôi phục bằng /import không có tham số sau khi sập);
// chỉ nối tiếp ở chế độ tiến triển auto, để quy hoạch mở rộng cung thích ứng tiếp nhận câu chuyện còn mở, hoặc để truyện đã hoàn tất khép lại; ở chế độ review thì giao cho người dùng /next.
func (h *Host) continueAfterImport(opts imp.Options) bool {
	want := opts.ContinueAfter
	if !want {
		in, err := imp.OpenWorkspace(h.store.Dir()).LoadIntent()
		if err != nil {
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Level: "warn",
				Summary: "đã nhập xong, nhưng đọc ý định tiếp tục tự động thất bại: " + err.Error()})
		} else if in != nil {
			want = in.ContinueAfterImport
		}
	}
	if !want {
		return false
	}
	meta, err := h.store.RunMeta.Load()
	if err != nil || meta == nil {
		slog.Warn("đọc RunMeta cho bước tiếp tục tự động sau nhập thất bại", "module", "host", "err", err)
		return false
	}
	if meta.AdvanceMode != domain.ChapterAdvanceAuto {
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Level: "info",
			Summary: "đã nhập xong; hiện đang ở chế độ nghiệm thu từng chương, nhập lệnh tiếp tục hoặc /next để viết tiếp"})
		return false
	}
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Level: "info", Summary: "đã nhập xong, tự động viết tiếp"})
	if !h.startEngine(nil) {
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Level: "warn",
			Summary: "khởi chạy bước tiếp tục tự động thất bại, hãy nhập lệnh tiếp tục để khôi phục thủ công"})
		return false
	}
	return true
}

// Export xuất các chương đã hoàn thành ra tệp bên ngoài (hiện chỉ hỗ trợ TXT).
//
// Khác với ImportFrom: xuất là thao tác chỉ đọc (không đụng Progress / Checkpoint),
// nên **không yêu cầu Engine dừng** — giữa chừng lúc viết vẫn xuất được "sản phẩm ở giai đoạn hiện tại".
// Chỉ đọc một ảnh chụp nhất quán gồm Progress.CompletedChapters + bản chính của chương + đề cương + premise.
func (h *Host) Export(ctx context.Context, opts exp.Options) (*exp.Result, error) {
	return exp.Run(ctx, exp.Deps{Store: h.store}, opts)
}
