package imp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
	"github.com/voocel/litellm"
)

// flakyModel trả về lỗi có thể thử lại trong fails lần đầu, sau đó trả lời theo mockModel.
type flakyModel struct {
	mockModel
	fails int
}

func (f *flakyModel) Generate(ctx context.Context, msgs []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	if f.fails > 0 {
		f.fails--
		return nil, fastRetryErr{}
	}
	return f.mockModel.Generate(ctx, msgs, tools, opts...)
}

// fastRetryErr có thể thử lại và thời gian lùi cực ngắn (RetryAfter khớp RetryHinter), bảo đảm kiểm thử chạy nhanh.
type fastRetryErr struct{}

func (fastRetryErr) Error() string             { return "rate limited" }
func (fastRetryErr) Retryable() bool           { return true }
func (fastRetryErr) RetryAfter() time.Duration { return time.Millisecond }

// TestCallStructuredNotifiesRetries bảo vệ tính nhìn thấy của việc thử lại: cả yêu cầu lùi (backoff) lẫn hỏi lại
// do kiểm tra đều phải phản chiếu, nếu không thì lùi theo cấp số nhân có thể im lặng hàng vài phút,
// khiến người dùng tưởng nhập bị treo (ảnh chụp lỗi: 3 phút không có gì mới báo lỗi).
// Yêu cầu lùi còn phải mang mốc chặn retryAt khác 0 — bộ đếm ngược của UI phụ thuộc vào nó; hỏi lại do kiểm tra xảy ra tức thì, retryAt bằng 0.
func TestCallStructuredNotifiesRetries(t *testing.T) {
	m := &flakyModel{mockModel: mockModel{responses: []string{"不是 JSON", `{"boundaries":[]}`}}, fails: 2}
	var notes []string
	var retries, reasks int
	prof := callProfile{notify: func(s string, retryAt time.Time) {
		notes = append(notes, s)
		if !retryAt.IsZero() {
			retries++
		}
		if strings.Contains(s, "hỏi lại") {
			reasks++
		}
	}}
	if _, err := callStructured[boundaryBatch](context.Background(), m, segmentContract(importTextsVI), "sys", "p", 100, prof, nil); err != nil {
		t.Fatalf("cuối cùng phải thành công: %v", err)
	}
	if retries != 2 || reasks != 1 {
		t.Fatalf("phải phản chiếu 2 lần yêu cầu lùi có mốc chặn + 1 lần hỏi lại do kiểm tra, được %d/%d: %v", retries, reasks, notes)
	}
}

// TestBriefErrIncludesAdapterFacts bảo vệ khả năng chẩn đoán của phản chiếu lỗi: message của gateway có thể chỉ có một câu
// "Provider returned error", phản chiếu phải bổ sung các sự thật có cấu trúc mà litellm mang theo (phân loại/trạng thái HTTP/provider/mô hình),
// và sự thật phải đứng trước — khi bị cắt ngắn thì ưu tiên giữ chúng; lỗi không phải từ adapter giữ nguyên.
func TestBriefErrIncludesAdapterFacts(t *testing.T) {
	le := &litellm.LiteLLMError{
		Type: litellm.ErrorTypeProvider, StatusCode: 502,
		Provider: "openai", Model: "gpt-x", Message: "Provider returned error",
	}
	got := briefErr(fmt.Errorf("bọc ngoài: %w", le))
	for _, want := range []string{"lỗi dịch vụ phía trên", "HTTP 502", "openai", "gpt-x", "Provider returned error"} {
		if !strings.Contains(got, want) {
			t.Fatalf("phản chiếu phải chứa %q, được %q", want, got)
		}
	}
	if !strings.HasPrefix(got, "lỗi dịch vụ phía trên") {
		t.Fatalf("sự thật có cấu trúc phải đứng trước, được %q", got)
	}
	if got := briefErr(errors.New("lỗi thường")); got != "lỗi thường" {
		t.Fatalf("lỗi không phải từ adapter phải giữ nguyên, được %q", got)
	}
}

// TestCallStructuredCancelIsNotSemanticFailure bảo vệ ngữ nghĩa huỷ: người dùng huỷ (Esc) không phải thất bại ngữ nghĩa,
// không được bọc thành errSemantic kiểu «N lần thử» — sẽ làm sai hướng chẩn đoán và tạo thêm một sản phẩm failures/ gây hiểu lầm.
func TestCallStructuredCancelIsNotSemanticFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := &mockModel{responses: []string{"垃圾输出"}}
	_, err := callStructured[boundaryBatch](ctx, m, segmentContract(importTextsVI), "sys", "p", 100, callProfile{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("phải trả về context.Canceled, được %v", err)
	}
	var se *errSemantic
	if errors.As(err, &se) {
		t.Fatal("huỷ không nên được bọc thành thất bại ngữ nghĩa")
	}
}

// TestCallStructuredCarriesRawOnSemanticFailure ghi §14.2: khi đầu ra vi phạm hợp
// đồng, lỗi phải mang theo phản hồi gốc để runner gom về đúng thư mục failures/.
func TestCallStructuredCarriesRawOnSemanticFailure(t *testing.T) {
	const rawOut = "rác đầu ra không phải json"
	m := &nativeImportModel{mockModel: &mockModel{responses: []string{rawOut}}}
	_, err := callStructured[boundaryBatch](context.Background(), m, segmentContract(importTextsVI), "sys", "payload", 100, callProfile{}, nil)
	var se *errSemantic
	if !errors.As(err, &se) {
		t.Fatalf("phải trả errSemantic, được %T: %v", err, err)
	}
	if se.Raw != rawOut {
		t.Fatalf("Raw phải mang lại phản hồi gốc lần cuối, được %q", se.Raw)
	}
	// Loại lỗi là khoá ổn định (không phụ thuộc câu chữ hiển thị), nên khẳng định
	// theo kind thay vì theo thông báo đã dịch.
	var failure *llmcontract.Failure
	if !errors.As(err, &failure) || failure.Kind != llmcontract.FailureContract {
		t.Fatalf("lỗi phải là vi phạm hợp đồng, được %T: %v", err, err)
	}
	if !strings.Contains(se.Error(), "vi phạm hợp đồng schema gốc") {
		t.Fatalf("thông báo phải nêu rõ vi phạm hợp đồng, được %q", se.Error())
	}
}

func TestCallStructuredCarriesRawOnProtocolFailure(t *testing.T) {
	m := &nativeImportModel{mockModel: &mockModel{
		responses: []string{"upstream malformed output"},
		stops:     []agentcore.StopReason{agentcore.StopReasonError},
	}}
	_, err := callStructured[boundaryBatch](context.Background(), m, segmentContract(importTextsVI), "sys", "payload", 100, callProfile{}, nil)
	var se *errSemantic
	if !errors.As(err, &se) || se.Raw != "upstream malformed output" || !strings.Contains(se.Error(), "stop_reason=error") {
		t.Fatalf("lỗi giao thức phải mang theo phản hồi gốc, được %T: %v", err, err)
	}
}
