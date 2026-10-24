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

// PlanStartDecision là phán quyết khởi chạy: chọn kiến trúc sư và sinh văn bản
// nhiệm vụ (đã bổ sung nếu cần).
type PlanStartDecision struct {
	Planner string `json:"planner"` // architect_long | architect_short
	Task    string `json:"task"`    // nhiệm vụ đầy đủ giao cho kiến trúc sư (kèm yêu cầu đã bổ sung)
	Reason  string `json:"reason"`
}

// Validate kiểm tra tính hợp lệ của quyết định khởi chạy. Thông báo lỗi được
// đưa lại cho model nên theo ngôn ngữ tác phẩm ở cấp tiến trình.
func (d *PlanStartDecision) Validate() error {
	texts := activeTexts()
	if d.Planner != "architect_long" && d.Planner != "architect_short" {
		return fmt.Errorf(texts.plannerInvalid, d.Planner)
	}
	if strings.TrimSpace(d.Task) == "" {
		return fmt.Errorf("%s", texts.taskRequired)
	}
	if strings.TrimSpace(d.Reason) == "" {
		return fmt.Errorf("%s", texts.reasonRequired)
	}
	return nil
}

// planStartContractFor dựng hợp đồng khởi chạy theo ngôn ngữ tác phẩm: mọi
// trường required, planner là enum đóng.
func planStartContractFor(lang i18n.Lang) llmcontract.Contract {
	texts := arbiterTextsFor(lang)
	return llmcontract.Contract{
		Name:        "arbiter_plan_start",
		Description: texts.planStartDesc,
		Schema: schema.Object(
			schema.Property("planner", schema.Enum(texts.plannerLabel, "architect_long", "architect_short")).Required(),
			schema.Property("task", schema.String(texts.planStartTaskLabel)).Required(),
			schema.Property("reason", schema.String(texts.planStartReasonLabel)).Required(),
		),
		Lang: lang,
	}
}

// planStartPayload là tải người dùng của kịch bản plan_start (sự thật chính là
// đầu vào, không có trạng thái store — sách mới).
type planStartPayload struct {
	Requirement string `json:"requirement"`
	Style       string `json:"style,omitempty"`
}

// DecidePlanStart phán quyết khởi chạy: chọn kiến trúc sư theo yêu cầu người
// dùng; khi yêu cầu quá ngắn (<20 ký tự) thì tự bổ sung trong task hướng khác
// biệt hóa, độc giả mục tiêu và điểm tiêu thụ cốt lõi, ít nhất một móc câu
// chuyện ngoài quy chuẩn.
// Ngữ nghĩa thất bại: trả về error → nơi gọi báo lỗi và dừng khởi chạy (người
// dùng đang ở đó, báo lỗi tốt hơn đoán).
func DecidePlanStart(ctx context.Context, model agentcore.ChatModel, systemPrompt, requirement, style string) (PlanStartDecision, error) {
	payload, err := marshalPayload(planStartPayload{Requirement: requirement, Style: style})
	if err != nil {
		return PlanStartDecision{}, err
	}
	lang := i18n.Active()
	return decide(ctx, model, planStartContractFor(lang), systemPrompt, payload, (*PlanStartDecision).Validate)
}
