package arbiter

import (
	"context"
	"fmt"
	"strings"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
)

// FailureFacts là gói sự thật dùng chung cho hai kịch bản worker_failure và
// deadlock: Engine đã tự phân loại ở tầng quyết định (lỗi tham số, lỗi tạm
// thời… không đi tới đây), nên thứ đến với Arbiter đều là phần dư mà mã quyết
// định không tìm ra lối thoát.
type FailureFacts struct {
	Kind          string   `json:"kind"` // worker_failure | deadlock
	Agent         string   `json:"agent,omitempty"`
	Task          string   `json:"task,omitempty"`
	Error         string   `json:"error,omitempty"` // worker_failure: văn bản lỗi
	ErrorKind     string   `json:"error_kind,omitempty"`
	Repeats       int      `json:"repeats,omitempty"` // deadlock: số lần đã phát cùng chỉ thị
	Phase         string   `json:"phase,omitempty"`
	NextChapter   int      `json:"next_chapter,omitempty"`
	PendingQueue  []int    `json:"pending_rewrites,omitempty"`
	FoundationGap []string `json:"foundation_missing,omitempty"`
	FactWarnings  []string `json:"fact_warnings,omitempty"`
}

// FailureDecision là phán quyết thất bại/bế tắc.
type FailureDecision struct {
	Action   string      `json:"action"` // retry | reroute | abort
	Dispatch *DispatchOp `json:"dispatch,omitempty"`
	Reason   string      `json:"reason"`
}

// ValidateAgainst kiểm tra tính hợp lệ của phán quyết theo sự thật. Thông báo
// lỗi được đưa lại cho model nên theo ngôn ngữ tác phẩm ở cấp tiến trình.
func (d *FailureDecision) ValidateAgainst(f FailureFacts) error {
	texts := activeTexts()
	if strings.TrimSpace(d.Reason) == "" {
		return fmt.Errorf("%s", texts.reasonRequired)
	}
	switch d.Action {
	case "retry", "abort":
		return nil
	case "reroute":
		if d.Dispatch == nil {
			return fmt.Errorf("%s", texts.rerouteNeedsDispatch)
		}
		if err := d.Dispatch.validate(texts); err != nil {
			return err
		}
		return validateDispatchAgainst(d.Dispatch, f.Phase, texts)
	default:
		return fmt.Errorf(texts.actionInvalid, d.Action)
	}
}

// failureContractFor dựng hợp đồng thất bại theo ngôn ngữ tác phẩm: action là
// enum đóng, dispatch là đối tượng có thể null (chỉ khác null ở reroute); tổ
// hợp giữa các trường vẫn do ValidateAgainst kiểm tra theo sự thật.
func failureContractFor(lang i18n.Lang) llmcontract.Contract {
	texts := arbiterTextsFor(lang)
	return llmcontract.Contract{
		Name:        "arbiter_failure",
		Description: texts.failureDesc,
		Schema: schema.Object(
			schema.Property("action", schema.Enum(texts.failureActionLabel, "retry", "reroute", "abort")).Required(),
			schema.Property("dispatch", dispatchSchema(texts, texts.failureDispatchLabel)).Required(),
			schema.Property("reason", schema.String(texts.failureReasonLabel)).Required(),
		),
		Lang: lang,
	}
}

// DecideFailure hỏi ý kiến khi thất bại/bế tắc. Ngữ nghĩa thất bại: trả về
// error → Engine xử lý theo đường thận trọng nhất (tạm dừng + notify), tuyệt
// đối không hỏi vô hạn.
func DecideFailure(ctx context.Context, model agentcore.ChatModel, systemPrompt string, facts FailureFacts) (FailureDecision, error) {
	payload, err := marshalPayload(facts)
	if err != nil {
		return FailureDecision{}, err
	}
	lang := i18n.Active()
	return decide(ctx, model, failureContractFor(lang), systemPrompt, payload, func(d *FailureDecision) error {
		return d.ValidateAgainst(facts)
	})
}
