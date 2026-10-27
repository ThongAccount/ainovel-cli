// Package llmretry là lõi thử lại ở tầng yêu cầu dùng chung cho các lời gọi
// trực tiếp tới mô hình: chỉ thử lại những lỗi mà adapter đánh dấu rõ là
// retryable, tuân theo Retry-After và backoff luỹ tiến, đồng thời đưa tiến độ
// vào chuỗi quan sát sẵn có của bàn làm việc qua ToolProgress. Lỗi kết thúc
// như tài khoản, xác thực hay quyền sẽ được trả về ngay; lỗi retryable tiếp
// tục được thử lại, vòng đời chỉ do context kiểm soát.
package llmretry

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/voocel/agentcore"
)

const maxRetryDelay = 60 * time.Second

// Generator là giao diện mô hình tối thiểu cần cho việc thử lại yêu cầu.
type Generator interface {
	Generate(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (*agentcore.LLMResponse, error)
}

// Event mô tả một lần thử lại yêu cầu sắp xảy ra.
type Event struct {
	Attempt int
	Delay   time.Duration
	Err     error
}

// Config cấu hình thông tin quan sát được của việc thử lại, không đổi ngữ nghĩa thử lại.
type Config struct {
	Agent   string
	OnRetry func(Event)
}

// Generate gọi model.Generate. Lỗi retryable được thử lại liên tục sau khi
// lùi, cho tới khi thành công hoặc context kết thúc; lỗi không phải
// retryable được trả về ngay.
func Generate(ctx context.Context, model Generator, cfg Config, messages []agentcore.Message, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	for retry := 1; ; retry++ {
		resp, err := model.Generate(ctx, messages, nil, opts...)
		if err == nil {
			return resp, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !isRetryable(err) {
			return nil, err
		}

		delay := retryDelay(err, retry-1)
		event := Event{Attempt: retry, Delay: delay, Err: err}
		if cfg.OnRetry != nil {
			cfg.OnRetry(event)
		}
		meta, _ := json.Marshal(struct {
			DelayMS int64 `json:"retry_delay_ms"`
		}{DelayMS: delay.Milliseconds()})
		agentcore.ReportToolProgress(ctx, agentcore.ProgressPayload{
			Kind:    agentcore.ProgressRetry,
			Agent:   cfg.Agent,
			Attempt: retry,
			Message: err.Error(),
			Meta:    meta,
		})
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func isRetryable(err error) bool {
	var retryable agentcore.RetryableError
	return errors.As(err, &retryable) && retryable.Retryable()
}

func retryDelay(err error, attempt int) time.Duration {
	var hinter agentcore.RetryHinter
	if errors.As(err, &hinter) {
		if delay := hinter.RetryAfter(); delay > 0 {
			if delay > maxRetryDelay {
				return maxRetryDelay
			}
			return delay
		}
	}
	delay := time.Second
	for i := 0; i < attempt && delay < maxRetryDelay; i++ {
		delay *= 2
	}
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}
