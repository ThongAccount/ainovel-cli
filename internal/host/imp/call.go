package imp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
	"github.com/voocel/ainovel-cli/internal/llmretry"
	"github.com/voocel/litellm"
)

// callModel là phụ thuộc tối thiểu của lõi đối với mô hình, thuận tiện cho việc tiêm mock trong kiểm thử.
type callModel interface {
	Generate(ctx context.Context, messages []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error)
}

// errTruncated biểu thị mô hình dừng vì giới hạn độ dài (lỗi dung lượng). Nó mang theo văn bản gốc để bên gọi quyết định báo thất bại hay cứu tiền tố (§9.5).
type errTruncated struct {
	Raw string
}

func (e *errTruncated) Error() string {
	return "đầu ra của mô hình bị cắt do giới hạn độ dài (stop=length)"
}

// errSemantic biểu thị lỗi ở tầng đầu ra không thể khắc phục bằng cách hỏi lại, mang theo phản hồi gốc,
// để runner ghi tập trung vào sản phẩm thất bại trong failures/ (§14.2); mọi hàm ngữ nghĩa đều dùng chung.
type errSemantic struct {
	Raw string
	Err error
}

func (e *errSemantic) Error() string { return e.Err.Error() }
func (e *errSemantic) Unwrap() error { return e.Err }

// callProfile mang các tuỳ chọn thinking và khả năng quan sát, được suy ra từ ModelRuntime mà Host dò ra.
// Giao thức có cấu trúc do callStructured chọn độc lập, dựa trên sự thật của mô hình và Contract tĩnh.
type callProfile struct {
	thinking agentcore.ThinkingLevel
	// texts là bảng văn bản mô hình đọc (mô tả hợp đồng, lệnh trong payload) theo
	// ngôn ngữ tác phẩm; nil thì dùng bản tiếng Việt mặc định.
	texts *importTexts
	// notify tùy chọn: hiển thị lại cho giao diện việc thử lại theo backoff của yêu cầu / hỏi lại do kiểm tra hợp lệ; nil thì im lặng (§14.1).
	// retryAt khác 0 = thời điểm hết hạn của lần thử lại kế tiếp, UI dựa vào đó để vẽ đồng hồ đếm ngược từng giây (sự kiện chỉ mang mốc hết hạn, thời gian còn lại được tính lúc vẽ).
	notify func(msg string, retryAt time.Time)
	// progress tùy chọn: hiển thị lại tiến trình nội bộ của các giai đoạn dài (khối cắt thứ N/M, tóm tắt đoạn N/M); nil thì im lặng.
	// Quá trình cắt/tổng hợp gọi mô hình từng khối/từng đoạn bên trong hàm, một khối có thể mất vài phút; không có nó thì cả bảng điều khiển im lặng như bị treo (§14.1).
	progress func(current, total int, msg string)
	// log tùy chọn: nhật ký riêng của quá trình nhập (logs/import.log); nil thì lùi về logger mặc định.
	log *slog.Logger
}

func (p callProfile) logger() *slog.Logger {
	if p.log != nil {
		return p.log
	}
	return slog.Default()
}

// step hiển thị lại một dòng tiến độ thường (tiến trình nội bộ của các giai đoạn dài).
func (p callProfile) step(current, total int, format string, args ...any) {
	if p.progress != nil {
		p.progress(current, total, fmt.Sprintf(format, args...))
	}
}

// say hiển thị lại một trạng thái của lời gọi dài. Lần thử lại có thể im lặng vài phút (backoff luỹ tiến cộng dồn hơn 2 phút),
// không hiển thị lại thì người dùng tưởng bị treo.
func (p callProfile) say(format string, args ...any) {
	p.sayRetry(time.Time{}, format, args...)
}

// sayRetry hiển thị lại một trạng thái kèm thời điểm hết hạn của lần thử lại, để UI đếm ngược.
func (p callProfile) sayRetry(retryAt time.Time, format string, args ...any) {
	if p.notify != nil {
		p.notify(fmt.Sprintf(format, args...), retryAt)
	}
}

// snippet nén văn bản nhiều dòng thành tóm tắt ngắn một dòng để hiển thị lại trên giao diện: gộp khoảng trắng, cắt còn tối đa max rune.
func snippet(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// briefErr nén lỗi thành văn bản ngắn một dòng để hiển thị lại trên giao diện (chuỗi lỗi đầy đủ vẫn đi qua nhật ký và sản phẩm thất bại).
// Sự thật có cấu trúc của adapter đặt trước: khi bị cắt, ưu tiên giữ "哪类错、什么状态码", message của gateway có thể hy sinh.
func briefErr(err error) string {
	s := err.Error()
	if d := modelErrDetail(err); d != "" {
		s = d + ": " + s
	}
	return snippet(s, 100)
}

// errTypeLabels dịch phân loại lỗi của litellm thành nhãn ngắn dễ đọc bằng tiếng Việt.
var errTypeLabels = map[litellm.ErrorType]string{
	litellm.ErrorTypeAuth:            "lỗi xác thực",
	litellm.ErrorTypeRateLimit:       "bị giới hạn tần suất",
	litellm.ErrorTypeNetwork:         "lỗi mạng",
	litellm.ErrorTypeValidation:      "tham số yêu cầu không hợp lệ",
	litellm.ErrorTypeProvider:        "lỗi dịch vụ phía trên",
	litellm.ErrorTypeTimeout:         "hết thời gian chờ",
	litellm.ErrorTypeQuota:           "hạn mức không đủ",
	litellm.ErrorTypeModel:           "mô hình không khả dụng",
	litellm.ErrorTypeInternal:        "lỗi nội bộ",
	litellm.ErrorTypeContextOverflow: "ngữ cảnh vượt giới hạn",
	litellm.ErrorTypeOverloaded:      "phía trên quá tải",
	litellm.ErrorTypeContentFilter:   "bị bộ lọc nội dung chặn",
}

// modelErrDetail trích từ chuỗi lỗi các sự thật có cấu trúc của adapter (phân loại lỗi, trạng thái HTTP, provider, mô hình).
// Message của gateway thường chỉ có một câu chung chung "Provider returned error", chỉ riêng nó không thể phân biệt là sai cấu hình,
// lỗi phía trên hay bị giới hạn tần suất; litellm luôn mang sẵn những sự thật này, chỉ là chúng không nằm trong văn bản Error(). Unwrap của
// adapter agentcore cho phép rõ ràng bên gọi của litellm dùng errors.As để lấy lỗi gốc. Lỗi không phải lỗi gọi mô hình trả về chuỗi rỗng.
func modelErrDetail(err error) string {
	var le *litellm.LiteLLMError
	if !errors.As(err, &le) {
		return ""
	}
	parts := make([]string, 0, 4)
	if label := errTypeLabels[le.Type]; label != "" {
		parts = append(parts, label)
	}
	if le.StatusCode != 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", le.StatusCode))
	}
	if le.Provider != "" {
		parts = append(parts, le.Provider)
	}
	if le.Model != "" {
		parts = append(parts, le.Model)
	}
	return strings.Join(parts, ", ")
}

// callOptions lắp ráp các CallOption của lần gọi này: luôn kèm trần đầu ra; thinking tuỳ theo năng lực.
// thinking chỉ được gửi khi khác Auto — gửi bất kỳ mức nào (kể cả off) cho mô hình không hỗ trợ thinking là tham số không hợp lệ (cùng chiến lược với arbiter).
func (p callProfile) callOptions(maxTokens int) []agentcore.CallOption {
	opts := []agentcore.CallOption{agentcore.WithMaxTokens(maxTokens)}
	if p.thinking != agentcore.ThinkingAuto {
		opts = append(opts, agentcore.WithThinking(p.thinking))
	}
	return opts
}

// callStructured thích ứng bộ thực thi có cấu trúc thống nhất cho tầng nhập, và ánh xạ lỗi chung thành ngữ nghĩa sản phẩm của nhập.
func callStructured[T any](ctx context.Context, m callModel, contract llmcontract.Contract, systemPrompt, payload string, maxTokens int, prof callProfile, validate func(*T) error) (T, error) {
	out, err := llmcontract.Execute(ctx, m, llmcontract.Request[T]{
		Contract:     contract,
		SystemPrompt: systemPrompt,
		Payload:      payload,
		Options:      prof.callOptions(maxTokens),
		Validate:     validate,
		Agent:        "import",
		Hooks: llmcontract.Hooks{
			Resolved: func(res llmcontract.Resolution) {
				prof.logger().Debug("imp chọn giao thức có cấu trúc",
					"contract", contract.Name, "structured_mode", res.Mode,
					"capability_source", res.Source, "provider", res.Provider,
					"model", res.Model, "schema_fingerprint", contract.Fingerprint())
			},
			RequestRetry: func(ev llmretry.Event) {
				prof.sayRetry(time.Now().Add(ev.Delay), "yêu cầu mô hình thất bại (%s), thử lại lần %d", briefErr(ev.Err), ev.Attempt)
				prof.logger().Warn("imp thử lại yêu cầu mô hình", "attempt", ev.Attempt, "delay", ev.Delay, "err", ev.Err)
			},
			Correction: func(ev llmcontract.Correction) {
				prof.say("kiểm tra đầu ra không đạt (%s), hỏi lại lần %d kèm phản hồi lỗi", briefErr(ev.Err), ev.Attempt+1)
				prof.logger().Warn("imp tự sửa đầu ra có cấu trúc", "attempt", ev.Attempt,
					"layer", ev.Layer, "structured_mode", ev.Mode, "err", ev.Err)
			},
		},
	})
	if err == nil {
		return out, nil
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	var failure *llmcontract.Failure
	if !errors.As(err, &failure) {
		return out, fmt.Errorf("imp: %w", err)
	}
	switch failure.Kind {
	case llmcontract.FailureLength:
		return out, &errTruncated{Raw: failure.Raw}
	case llmcontract.FailureSafety, llmcontract.FailureContract, llmcontract.FailureProtocol:
		if failure.Raw != "" {
			return out, &errSemantic{Raw: failure.Raw, Err: fmt.Errorf("imp: %w", failure)}
		}
	case llmcontract.FailureRequest:
		if detail := modelErrDetail(failure); detail != "" {
			return out, fmt.Errorf("imp: lệnh gọi mô hình thất bại (%s): %w", detail, failure)
		}
	}
	return out, fmt.Errorf("imp: %w", failure)
}
