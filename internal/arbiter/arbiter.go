// Package arbiter là lớp phán quyết ngữ nghĩa: LLM-as-function được đánh thức
// khi cần.
//
// Hai mặt đối xứng (docs/engine-arbiter.md §2):
//
//	Mặt quyết định:  flow.LoadState   → flow.Route     → Instruction
//	Mặt ngữ nghĩa:    arbiter.Collect* → arbiter.Decide* → XxxDecision
//
// Kỷ luật: Collect tập trung mọi IO (đọc đủ sự thật từ store); Decide ngoài
// lời gọi mô hình do bộ thực thi thống nhất quản lý thì không có IO, nên có
// thể phát lại offline bằng các facts đã lưu; việc thi hành thuộc Engine.
// Mỗi kịch bản là một cặp hàm cộng với kiểu Decision riêng — hành động thuộc
// kịch bản khác không thể biểu đạt ở tầng kiểu; phần hợp lệ còn lại bị từ
// chối bởi Validate của từng kiểu —
// Đầu ra của Arbiter cũng không đáng tin như mọi đầu ra LLM khác, kiểm tra sự
// thật là cổng chốt cuối.
package arbiter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
)

// decideMaxTokens là trần đầu ra cho một lần phán quyết; JSON phán quyết rất
// nhỏ, phần lớn ngân sách dành cho suy nghĩ của mô hình suy luận
// (cùng lý do với userrules.normalizeMaxTokens).
const decideMaxTokens = 8192

// decide giao hợp đồng kịch bản và kiểm tra nghiệp vụ cho bộ thực thi có cấu
// trúc thống nhất. Ngoài lời gọi mô hình thì không có IO.
func decide[T any](ctx context.Context, model agentcore.ChatModel, contract llmcontract.Contract, systemPrompt, payload string, validate func(*T) error) (T, error) {
	out, err := llmcontract.Execute(ctx, model, llmcontract.Request[T]{
		Contract:     contract,
		SystemPrompt: systemPrompt,
		Payload:      payload,
		Options:      []agentcore.CallOption{agentcore.WithMaxTokens(decideMaxTokens)},
		Validate:     validate,
		Agent:        "arbiter",
		Hooks: llmcontract.Hooks{
			Resolved: func(res llmcontract.Resolution) {
				slog.Debug("chọn giao thức phán quyết", "module", "arbiter",
					"contract", contract.Name, "structured_mode", res.Mode,
					"capability_source", res.Source, "provider", res.Provider,
					"model", res.Model, "schema_fingerprint", contract.Fingerprint())
			},
			Correction: func(ev llmcontract.Correction) {
				slog.Warn("tự lành đầu ra phán quyết", "module", "arbiter", "attempt", ev.Attempt,
					"layer", ev.Layer, "structured_mode", ev.Mode, "err", ev.Err)
			},
		},
	})
	if err != nil {
		return out, fmt.Errorf("arbiter: %w", err)
	}
	return out, nil
}

// DispatchOp là hành động phân công dùng chung cho mọi kịch bản.
type DispatchOp struct {
	Agent string `json:"agent"`
	Task  string `json:"task"`
}

// workerNames là các đích phân công hợp lệ (khớp với các worker do
// agents.BuildWorkers đăng ký). Cắt slice có thứ tự: vừa làm enum của schema
// (thứ tự cố định giữ vân tay ổn định) vừa là danh sách trắng khi kiểm tra.
var workerNames = []string{"architect_long", "architect_short", "writer", "editor"}

// validate kiểm tra hành động phân công: đích phải nằm trong danh sách trắng
// và nhiệm vụ không được rỗng.
func (d *DispatchOp) validate(texts *arbiterTexts) error {
	if d == nil {
		return nil
	}
	if !slices.Contains(workerNames, d.Agent) {
		return fmt.Errorf(texts.dispatchAgentInvalid, d.Agent)
	}
	if strings.TrimSpace(d.Task) == "" {
		return fmt.Errorf("%s", texts.dispatchTaskRequired)
	}
	return nil
}

// dispatchSchema là vị trí schema có thể null của DispatchOp: chỉ hành động
// cần phân công mới đưa ra đối tượng, các trường hợp khác là null (chế độ
// strict bắt mọi trường required, ngữ nghĩa "tùy chọn" được diễn đạt bằng
// null).
func dispatchSchema(texts *arbiterTexts, desc string) map[string]any {
	return llmcontract.Nullable(schema.Object(
		schema.Property("agent", schema.Enum(texts.dispatchAgentLabel, workerNames...)).Required(),
		schema.Property("task", schema.String(texts.dispatchTaskLabel)).Required(),
	))
}

// marshalPayload serialize gói sự thật; hỏng thì đó là lỗi chương trình, bắt
// buộc phải phơi bày — tự bịa sự thật rỗng sẽ khiến model phán đoán trên đầu
// vào giả.
func marshalPayload(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("arbiter: không serialize được gói sự thật: %w", err)
	}
	return string(data), nil
}
