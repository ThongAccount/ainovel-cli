package llmcontract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/llmretry"
)

// FailureKind phân biệt các ranh giới lỗi mà cùng một lần phản hồi có cấu trúc
// không thể tự sửa được.
type FailureKind string

const (
	FailureRequest  FailureKind = "request"
	FailureProtocol FailureKind = "protocol"
	FailureLength   FailureKind = "length"
	FailureSafety   FailureKind = "safety"
	FailureContract FailureKind = "contract"
)

// Failure giữ lại loại lỗi và đầu ra gốc của model, để nơi gọi tự quyết định
// cách biểu diễn trong nhật ký, tệp sản phẩm và giao diện.
type Failure struct {
	Kind     FailureKind
	Contract string
	Raw      string
	Err      error
}

func (e *Failure) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Err == nil {
		return e.Contract
	}
	if e.Contract == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %v", e.Contract, e.Err)
}

func (e *Failure) Unwrap() error { return e.Err }

// Correction mô tả một lỗi đầu ra mà model có thể tự sửa. Attempt là số thứ tự
// của lần gọi vừa thất bại.
type Correction struct {
	Attempt int
	Layer   string
	Mode    Mode
	Raw     string
	Err     error
}

// Hooks chỉ phục vụ quan sát, không đổi ngữ nghĩa thực thi.
type Hooks struct {
	Resolved     func(Resolution)
	RequestRetry func(llmretry.Event)
	Correction   func(Correction)
}

// Request mô tả một lần trả về có cấu trúc trực tiếp. Contract là nguồn duy
// nhất của cấu trúc, Validate chỉ xử lý các ràng buộc nghiệp vụ mà JSON Schema
// không diễn đạt được.
type Request[T any] struct {
	Contract     Contract
	SystemPrompt string
	Payload      string
	Options      []agentcore.CallOption
	Validate     func(*T) error
	Agent        string
	Hooks        Hooks
}

// Execute gộp chọn giao thức, chuẩn bị prompt, thử lại yêu cầu, phân loại lý do
// dừng, giải mã Schema/DTO và tự lành theo phản hồi nghiệp vụ.
//
// Lỗi định dạng/Schema ở chế độ prompt và lỗi nghiệp vụ ở cả hai chế độ đều
// được phản hồi liên tục cho model cho tới khi thành công hoặc hết context;
// vi phạm hợp đồng gốc thì lộ ngay. Văn bản phản hồi theo ngôn ngữ của hợp
// đồng (Contract.Lang, rỗng thì theo ngôn ngữ tác phẩm ở cấp tiến trình).
func Execute[T any](ctx context.Context, model llmretry.Generator, req Request[T]) (T, error) {
	var zero T
	if model == nil {
		return zero, &Failure{Kind: FailureProtocol, Contract: req.Contract.Name, Err: errors.New("mô hình chưa được cấu hình")}
	}

	texts := contractTextsFor(req.Contract.lang())
	schemaOptions, resolution := Plan(model, req.Contract)
	systemPrompt, err := PreparePrompt(req.SystemPrompt, req.Contract, resolution)
	if err != nil {
		return zero, &Failure{Kind: FailureContract, Contract: req.Contract.Name, Err: fmt.Errorf("chuẩn bị hợp đồng đầu ra: %w", err)}
	}
	if req.Hooks.Resolved != nil {
		req.Hooks.Resolved(resolution)
	}

	messages := []agentcore.Message{
		agentcore.SystemMsg(systemPrompt),
		agentcore.UserMsg(req.Payload),
	}
	options := append(schemaOptions, req.Options...)
	native := resolution.Mode == ModeNativeJSONSchema

	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		resp, err := llmretry.Generate(ctx, model, llmretry.Config{
			Agent:   req.Agent,
			OnRetry: req.Hooks.RequestRetry,
		}, messages, options...)
		if err != nil {
			if ctx.Err() != nil {
				return zero, ctx.Err()
			}
			return zero, &Failure{Kind: FailureRequest, Contract: req.Contract.Name, Err: err}
		}
		if resp == nil {
			return zero, &Failure{Kind: FailureProtocol, Contract: req.Contract.Name, Err: errors.New("mô hình trả về phản hồi rỗng")}
		}

		raw := resp.Message.TextContent()
		switch resp.Message.StopReason {
		case agentcore.StopReasonLength:
			return zero, &Failure{Kind: FailureLength, Contract: req.Contract.Name, Raw: raw, Err: errors.New("đầu ra của mô hình bị cắt do giới hạn độ dài (stop_reason=length)")}
		case agentcore.StopReasonSafety:
			return zero, &Failure{Kind: FailureSafety, Contract: req.Contract.Name, Raw: raw, Err: errors.New("mô hình từ chối hoặc bị lọc nội dung (stop_reason=safety)")}
		case agentcore.StopReasonError:
			return zero, &Failure{Kind: FailureProtocol, Contract: req.Contract.Name, Raw: raw, Err: errors.New("mô hình kết thúc ở trạng thái lỗi (stop_reason=error)")}
		case agentcore.StopReasonToolUse:
			return zero, &Failure{Kind: FailureProtocol, Contract: req.Contract.Name, Raw: raw, Err: errors.New("lời gọi có cấu trúc trả về tool call ngoài dự kiến (stop_reason=tool_use)")}
		case agentcore.StopReasonAborted:
			return zero, &Failure{Kind: FailureProtocol, Contract: req.Contract.Name, Raw: raw, Err: errors.New("lời gọi mô hình bị huỷ (stop_reason=aborted)")}
		}

		body := strings.TrimSpace(raw)
		if native {
			if body == "" {
				return zero, &Failure{Kind: FailureContract, Contract: req.Contract.Name, Raw: raw, Err: errors.New("schema gốc trả về nội dung rỗng")}
			}
		} else {
			body = ExtractJSONObject(raw)
		}

		layer := "schema"
		var cause error
		if body == "" {
			layer, cause = "decode", errors.New(texts.noJSONObject)
		} else if err := validateJSONWith(req.Contract.Schema, []byte(body), texts); err != nil {
			cause = err
		} else {
			var out T
			if err := json.Unmarshal([]byte(body), &out); err != nil {
				// Schema đã qua mà DTO vẫn không giải mã được, nghĩa là hợp đồng
				// tĩnh lệch với kiểu Go: bắt model viết lại không sửa được lỗi mã.
				return zero, &Failure{Kind: FailureContract, Contract: req.Contract.Name, Raw: raw, Err: fmt.Errorf("schema không khớp với DTO: %w", err)}
			}
			if req.Validate == nil {
				return out, nil
			}
			if err := req.Validate(&out); err == nil {
				return out, nil
			} else {
				layer, cause = "semantic", err
			}
		}

		if native && layer != "semantic" {
			return zero, &Failure{Kind: FailureContract, Contract: req.Contract.Name, Raw: raw, Err: fmt.Errorf("vi phạm hợp đồng schema gốc: %w", cause)}
		}
		correction := Correction{Attempt: attempt, Layer: layer, Mode: resolution.Mode, Raw: raw, Err: cause}
		if req.Hooks.Correction != nil {
			req.Hooks.Correction(correction)
		}
		hint := texts.promptCorrection
		if layer == "semantic" {
			hint = texts.semanticCorrection
		}
		messages = append(messages,
			agentcore.Message{Role: agentcore.RoleAssistant, Content: []agentcore.ContentBlock{agentcore.TextBlock(raw)}},
			agentcore.UserMsg(hint+texts.errorPrefix+cause.Error()),
		)
	}
}

// ExtractJSONObject trả về đối tượng JSON cân bằng đầu tiên trong văn bản;
// dấu ngoặc nhọn nằm trong chuỗi không tính vào phân cấp.
func ExtractJSONObject(raw string) string {
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return ""
	}
	depth, inString, escaped := 0, false, false
	for i := start; i < len(raw); i++ {
		switch c := raw[i]; {
		case inString && escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && c == '{':
			depth++
		case !inString && c == '}':
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return ""
}
