package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/voocel/agentcore"
	corecontext "github.com/voocel/agentcore/context"
	"github.com/voocel/agentcore/llm"
	"github.com/voocel/agentcore/subagent"
	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/agents/ctxpack"
	"github.com/voocel/ainovel-cli/internal/agents/guard"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/tools"
)

// agentToRole chuẩn hóa tên subagent thành tên role mà ModelSet nhận.
// architect_short / architect_long cùng dùng chung một cấu hình role architect.
// Cùng nghĩa với host.agentRoleName, vì build và host không phụ thuộc lẫn nhau
// nên mỗi bên giữ một bản.
func agentToRole(name string) string {
	if strings.HasPrefix(name, "architect_") {
		return "architect"
	}
	return name
}

// promptCacheBase sinh mã băm ngắn ổn định từ thư mục sách, dùng làm tiền tố
// định danh cho bộ nhớ đệm prompt: cùng một cuốn sách dùng chung bucket định
// tuyến qua các lần khởi động tiến trình, mà không làm lộ đường dẫn cục bộ cho
// provider. Hậu tố vai trò do nơi gọi ghép, subagent mỗi lần spawn lại thêm
// "#seq" (mỗi phiên một khoá).
func promptCacheBase(bookDir string) string {
	sum := sha256.Sum256([]byte(bookDir))
	return "nvl-" + hex.EncodeToString(sum[:6])
}

// subagentMaxRetries là trần thử lại LLM cho mọi Worker.
// Chiến lược lùi bước: lùi theo cấp số nhân (bị chặn trên bởi maxDelay), ưu tiên
// tuân theo Retry-After của server.
// Công cụ chỉ khởi chạy sau khi tin nhắn Assistant đã gửi trọn vẹn, nên
// stream-idle / 503 / nhiễu mạng ngắn đều thử lại an toàn trong Worker mà không
// phát lại tác dụng phụ của công cụ.
const subagentMaxRetries = 7

// UsageRecorder là callback ghi nhận mức dùng, tuỳ chọn cho BuildWorkers; chữ ký
// giống OnMessage, mỗi tin nhắn agent gọi một lần, lớp Host lo gom lại. task là
// văn bản nhiệm vụ của lần spawn này, dùng làm định danh phiên để cơ chế phát
// hiện đứt chuỗi cache đặt lại đường cơ sở theo từng phiên.
// nil nghĩa là không theo dõi.
type UsageRecorder func(agentName, task string, msg agentcore.AgentMessage)

// ApplyThinking áp mức suy luận của một vai trò cụ thể lên Worker (dùng khi
// đổi /model lúc chạy).
// architect → cả hai subagent architect_*; writer/editor → subagent tương ứng.
// level rỗng = giữ mặc định của model/provider. Tên role khác bị bỏ qua.
type ApplyThinking func(role string, level agentcore.ThinkingLevel)

// ParseThinkingLevel chuyển chuỗi cấu hình thành agentcore.ThinkingLevel.
// "" hợp lệ (= không ghi đè/kế thừa); còn lại phải là off/low/medium/high/xhigh/max,
// nếu không sẽ trả về error (lúc khởi động hạ cấp thành rỗng kèm warn, lúc chạy
// trả lại error cho người dùng).
func ParseThinkingLevel(s string) (agentcore.ThinkingLevel, error) {
	lv := agentcore.NormalizeThinkingLevel(agentcore.ThinkingLevel(s))
	switch lv {
	case "", agentcore.ThinkingOff, agentcore.ThinkingLow, agentcore.ThinkingMedium,
		agentcore.ThinkingHigh, agentcore.ThinkingXHigh, agentcore.ThinkingMax:
		return lv, nil
	default:
		return "", fmt.Errorf("mức suy luận không hợp lệ %q (chọn: off/low/medium/high/xhigh/max)", s)
	}
}

func ResolveThinkingForModel(model agentcore.ChatModel, level agentcore.ThinkingLevel) (agentcore.ThinkingLevel, bool) {
	level = agentcore.NormalizeThinkingLevel(level)
	// Với mô hình chat thường không hỗ trợ thinking, "off" tường minh không phải
	// no-op mà là tham số không hợp lệ.
	if cp, ok := model.(llm.CapabilityProvider); ok && cp.Capabilities().Thinking.Supported == llm.SupportNo {
		return agentcore.ThinkingAuto, level == agentcore.ThinkingAuto
	}
	return llm.ThinkingPolicyFor(model).Resolve(level)
}

func AvailableThinkingForModel(model agentcore.ChatModel) []agentcore.ThinkingLevel {
	if cp, ok := model.(llm.CapabilityProvider); ok && cp.Capabilities().Thinking.Supported == llm.SupportNo {
		return []agentcore.ThinkingLevel{agentcore.ThinkingAuto}
	}
	return llm.ThinkingPolicyFor(model).Available
}

// roleThinking phân giải mức suy luận có hiệu lực của một vai trò; giá trị không
// hợp lệ bị hạ cấp thành rỗng (không ghi đè) kèm warn.
func roleThinking(cfg bootstrap.Config, role string) agentcore.ThinkingLevel {
	lv, err := ParseThinkingLevel(cfg.ResolveReasoningEffort(role))
	if err != nil {
		slog.Warn("bỏ qua cấu hình mức suy luận không hợp lệ", "module", "agent", "role", role, "err", err)
		return ""
	}
	return lv
}

func resolvedRoleThinking(model agentcore.ChatModel, cfg bootstrap.Config, role string) agentcore.ThinkingLevel {
	resolved, _ := ResolveThinkingForModel(model, roleThinking(cfg, role))
	return resolved
}

// BuildWorkers lắp ráp ba Worker (architect_short/long, writer, editor) thành
// subagent.Runner gọi được bằng chương trình. Engine gọi trực tiếp các cửa vào
// có kiểu của nó, không qua lớp công cụ LLM (docs/engine-rfc.md §1).
// Trả về Runner, WriterRestorePack và ApplyThinking (liên kết mức suy luận các
// vai trò với /model lúc chạy; ContextManager của writer/architect/editor được
// dựng lại tự động qua nhà máy).
// onGuardBlock tuỳ chọn (an toàn khi nil): callback kiểm toán việc chặn/nâng cấp
// của StopGuard từng Worker.
func BuildWorkers(
	cfg bootstrap.Config,
	store *store.Store,
	styleStats *tools.StyleStatsIndex,
	models *bootstrap.ModelSet,
	bundle assets.Bundle,
	recordUsage UsageRecorder,
	onGuardBlock guard.BlockHook,
) (*subagent.Runner, *ctxpack.WriterRestorePack, ApplyThinking) {
	// Công cụ dùng chung
	contextTool := tools.NewContextTool(store, bundle.References, cfg.Style, styleStats)
	readChapter := tools.NewReadChapterTool(store)

	architectTools := []agentcore.Tool{
		contextTool,
		tools.NewSaveBookTool(store),
		tools.NewSaveFoundationTool(store),
		tools.NewReviseOutlineTool(store),
		// Chương trong hàng đợi làm lại chỉ có thể nhận yêu cầu "viết lại thành
		// gì" qua chapter_contract: revise_outline không được đụng chương đã viết,
		// save_foundation(outline) bị cấm ghi đè toàn bộ trong giai đoạn sáng tác.
		tools.NewPlanChapterTool(store),
		tools.NewResolveOutlineFeedbackTool(store),
		tools.NewAuditFoundationTool(store),
	}
	writerTools := []agentcore.Tool{
		contextTool,
		readChapter,
		tools.NewPlanChapterTool(store),
		tools.NewDraftChapterTool(store),
		tools.NewEditChapterTool(store),
		tools.NewCheckConsistencyTool(store),
		tools.NewCommitChapterTool(store, styleStats),
	}
	editorTools := []agentcore.Tool{
		contextTool,
		readChapter,
		tools.NewSaveReviewTool(store),
		tools.NewSaveArcSummaryTool(store),
		tools.NewSaveVolumeSummaryTool(store),
	}

	// Chuyển provider dự phòng chỉ ghi nhật ký, không báo cho lớp host
	reportFailover := func(ev bootstrap.FailoverEvent) {
		slog.Warn("chuyển provider",
			"module", "agent",
			"role", ev.Role,
			"reason", ev.Reason,
			"from", fmt.Sprintf("%s/%s", ev.FromProvider, ev.FromModel),
			"to", fmt.Sprintf("%s/%s", ev.ToProvider, ev.ToModel),
			"err", ev.Err,
		)
	}

	architectModel := models.ForRoleWithFailover("architect", reportFailover)
	writerModel := models.ForRoleWithFailover("writer", reportFailover)
	editorModel := models.ForRoleWithFailover("editor", reportFailover)

	// ContextManager của Writer được dựng lại mỗi lần gọi qua nhà máy, cửa sổ
	// ngữ cảnh bám theo model khi hoán đổi (xem nhà máy bên dưới).
	writerProvider, writerModelName, _ := models.CurrentSelection("writer")
	writerContextWindow, writerSource := cfg.ResolveContextWindow(writerProvider, writerModelName)
	bootstrap.LogContextWindowChoice("writer", writerModelName, writerContextWindow, writerSource)

	// modelLookup gắn _meta:{provider,model} cho mỗi tin nhắn assistant khi ghi
	// vào session, để replay không còn phải dựa vào "ModelSet hiện tại" để suy ra
	// chi phí lịch sử, và đổi model lúc chạy vẫn tính chính xác.
	modelLookup := func(agentName string) (string, string) {
		role := agentToRole(agentName)
		provider, name, _ := models.CurrentSelection(role)
		return provider, name
	}
	baseOnMsg := store.Sessions.SubAgentLogger(modelLookup)
	onMsg := func(agentName, task string, msg agentcore.AgentMessage) {
		baseOnMsg(agentName, task, msg)
		if recordUsage != nil {
			recordUsage(agentName, task, msg)
		}
	}

	// Bộ nhớ đệm prompt: một sách một nền, một vai trò một tên, một phiên một
	// khoá (subagent spawn thêm #seq).
	// Hệ OpenAI dùng prompt_cache_key để giữ affinity định tuyến; hệ Claude dùng
	// cache_control cuộn điểm gãy (nền system + đầu nhọn tin nhắn cuối). Khi
	// provider không hỗ trợ, agentcore âm thầm bỏ qua theo năng lực; với phiên
	// nhiều lượt, lợi ích đọc cache luôn dương nên không cần công tắc.
	cacheBase := promptCacheBase(store.Dir())

	architectStopGuardFactory := func(_, _ string) agentcore.StopGuard {
		return guard.NewArchitectStopGuard(store, onGuardBlock)
	}
	// Mô tả subagent được ghép thành mô tả công cụ mà model đọc, nên theo ngôn
	// ngữ tác phẩm lấy từ bundle.
	lang := i18n.Normalize(bundle.Language)
	texts := workerTextsFor(lang)
	promptTexts := ctxpack.PromptTextsFor(lang)

	architectThinking, _ := ResolveThinkingForModel(architectModel, roleThinking(cfg, "architect"))
	architectShort := subagent.Config{
		Name:             "architect_short",
		Description:      texts.architectShortDesc,
		Model:            architectModel,
		SystemPrompt:     bundle.Prompts.ArchitectShort,
		Tools:            architectTools,
		MaxTurns:         15,
		MaxRetries:       subagentMaxRetries,
		ThinkingLevel:    architectThinking,
		OnMessage:        onMsg,
		CacheLastMessage: "ephemeral",
		PromptCacheKey:   cacheBase + "-architect_short",
		StopAfterToolResult: func(toolName string, result json.RawMessage) bool {
			return foundationReadyResult(toolName, result)
		},
		StopGuardFactory: architectStopGuardFactory,
	}
	architectLong := subagent.Config{
		Name:                "architect_long",
		Description:         texts.architectLongDesc,
		Model:               architectModel,
		SystemPrompt:        bundle.Prompts.ArchitectLong,
		Tools:               architectTools,
		MaxTurns:            20,
		MaxRetries:          subagentMaxRetries,
		ThinkingLevel:       architectThinking,
		OnMessage:           onMsg,
		CacheLastMessage:    "ephemeral",
		PromptCacheKey:      cacheBase + "-architect_long",
		StopAfterToolResult: architectLongShouldStopAfterToolResult,
		StopGuardFactory:    architectStopGuardFactory,
	}

	// Đường lắp ráp duy nhất: mẫu giao thức {{VOICE}} được điền tại chỗ đoạn văn
	// phong, rồi nối thêm bộ phong cách.
	// A/B voice của eval đi qua cùng hàm, bảo đảm hai nhánh tương đương
	// (docs/voice-layer.md §3.2).
	writerPrompt := assets.BuildWriterPrompt(bundle.Prompts.Writer, bundle.Voice, bundle.Styles[cfg.Style])

	restore := &ctxpack.WriterRestorePack{}
	restore.Refresh(store)

	writer := subagent.Config{
		Name:             "writer",
		Description:      texts.writerDesc,
		Model:            writerModel,
		SystemPrompt:     writerPrompt,
		Tools:            writerTools,
		MaxTurns:         30,
		MaxRetries:       subagentMaxRetries,
		ThinkingLevel:    resolvedRoleThinking(writerModel, cfg, "writer"),
		StopAfterTools:   []string{"commit_chapter"},
		OnMessage:        onMsg,
		CacheLastMessage: "ephemeral",
		PromptCacheKey:   cacheBase + "-writer",
		StopGuardFactory: func(_, _ string) agentcore.StopGuard {
			return guard.NewWriterStopGuard(store, onGuardBlock)
		},
		ContextManagerFactory: func(model agentcore.ChatModel) agentcore.ContextManager {
			// Dựng lại bộ quản lý ngữ cảnh theo model writer hiện tại cho mỗi chương.
			window, _ := models.ResolveContextWindow(bootstrap.ModelProvider(model), bootstrap.ModelName(model))
			return newContextManager(contextManagerConfig{
				Model:         model,
				ContextWindow: window,
				ReserveTokens: bootstrap.CompactReserveTokens(window),
				Agent:         "writer",
				// Chiếu sau khi gửi, tránh các lượt sau ghi lại tiền tố yêu cầu.
				CommitProjected: true,
				ToolMicrocompact: &corecontext.ToolResultMicrocompactConfig{
					MinResultTokens: 200,
				},
				ExtraStrategies: []corecontext.Strategy{
					ctxpack.NewStoreSummaryCompact(ctxpack.StoreSummaryCompactConfig{
						Store:            store,
						KeepRecentTokens: 20000,
					}),
				},
				Summary: &corecontext.FullSummaryConfig{
					PostSummaryHooks:    []corecontext.PostSummaryHook{restore.Hook()},
					SystemPrompt:        promptTexts.SummarySystem,
					SummaryPrompt:       promptTexts.Summary,
					UpdateSummaryPrompt: promptTexts.UpdateSummary,
					TurnPrefixPrompt:    promptTexts.TurnPrefix,
				},
			})
		},
	}

	editor := subagent.Config{
		Name:             "editor",
		Description:      texts.editorDesc,
		Model:            editorModel,
		SystemPrompt:     bundle.Prompts.Editor,
		Tools:            editorTools,
		MaxTurns:         20,
		MaxRetries:       subagentMaxRetries,
		ThinkingLevel:    resolvedRoleThinking(editorModel, cfg, "editor"),
		OnMessage:        onMsg,
		CacheLastMessage: "ephemeral",
		PromptCacheKey:   cacheBase + "-editor",
		// Trúng sản phẩm cuối cùng là dừng. Lần thoát cuối vẫn hỏi ý kiến StopGuard
		// (hợp đồng kiểm thử TestContract_TerminalToolExitConsultsStopGuard),
		// NewEditorStopGuard có nhận biết nhiệm vụ nên bác lần thoát sớm kiểu
		// "được phân công sinh tóm tắt nhưng chỉ làm soát duyệt", nên save_review
		// có thể dừng cứng an toàn.
		StopAfterToolResult: func(toolName string, _ json.RawMessage) bool {
			return toolName == "save_review" || toolName == "save_arc_summary" || toolName == "save_volume_summary"
		},
		StopGuardFactory: func(_, task string) agentcore.StopGuard {
			return guard.NewEditorStopGuard(store, task, onGuardBlock)
		},
	}

	runner := subagent.NewRunner(architectShort, architectLong, writer, editor)

	// Liên kết mức suy luận các vai trò lúc chạy (dùng khi đổi /model).
	applyThinking := func(role string, level agentcore.ThinkingLevel) {
		switch role {
		case "architect":
			level, _ = ResolveThinkingForModel(models.ForRole("architect"), level)
			runner.SetThinkingLevel("architect_short", level)
			runner.SetThinkingLevel("architect_long", level)
		case "writer", "editor":
			level, _ = ResolveThinkingForModel(models.ForRole(role), level)
			runner.SetThinkingLevel(role, level)
		}
	}

	return runner, restore, applyThinking
}

type saveFoundationResult struct {
	Type            string `json:"type"`
	FoundationReady bool   `json:"foundation_ready"`
}

func decodeSaveFoundationResult(toolName string, result json.RawMessage) saveFoundationResult {
	if toolName != "save_foundation" {
		return saveFoundationResult{}
	}
	var r saveFoundationResult
	_ = json.Unmarshal(result, &r)
	return r
}

func architectLongShouldStopAfterToolResult(toolName string, result json.RawMessage) bool {
	if foundationReadyResult(toolName, result) {
		return true
	}
	r := decodeSaveFoundationResult(toolName, result)
	switch r.Type {
	case "expand_arc", "complete_book":
		return true
	default:
		return false
	}
}

func foundationReadyResult(toolName string, result json.RawMessage) bool {
	if toolName != "audit_foundation" {
		return false
	}
	var r struct {
		FoundationReady bool `json:"foundation_ready"`
	}
	return json.Unmarshal(result, &r) == nil && r.FoundationReady
}
