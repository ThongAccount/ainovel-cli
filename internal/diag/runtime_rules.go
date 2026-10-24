package diag

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/store"
)

// Ngưỡng phát hiện runtime.
const (
	repeatCritical = 8 // số lần lặp gần đây đủ để nâng lên critical
	streamIdleWarn = 3 // ngưỡng cảnh báo tích lũy stream_idle
)

// RuntimeRuleFunc là chữ ký thống nhất của các quy tắc chẩn đoán runtime (tương ứng
// RuleFunc ở phía sáng tác).
// Đầu vào là RuntimeCapture đã gom nhóm và khử nhạy cảm, đầu ra là Finding dạng báo cáo —
// tất cả đều AutoNone, chỉ chẩn đoán, không sinh Action (kỷ luật quan sát, xem
// architecture.md §2.3).
type RuntimeRuleFunc func(rc *RuntimeCapture) []Finding

var runtimeRules = []RuntimeRuleFunc{
	repeatedErrors,
	stuckStep,
	streamIdleStorm,
}

// runtimeFindings chạy toàn bộ quy tắc runtime.
func runtimeFindings(rc *RuntimeCapture) []Finding {
	var out []Finding
	for _, rule := range runtimeRules {
		out = append(out, rule(rc)...)
	}
	return out
}

// Diagnose là lối vào chẩn đoán đầy đủ của /diag: chẩn đoán sáng tác + tín hiệu runtime +
// phát hiện runtime, trả về Report đã gộp cùng RuntimeCapture thô (để tái dùng khi xuất,
// tránh bắt lại dữ liệu).
// Finding runtime chỉ được gộp vào Findings để hiển thị, không sửa Actions — giữ đúng
// tinh thần quan sát thuần túy.
func Diagnose(s *store.Store) (Report, RuntimeCapture) {
	rep := Analyze(s)
	rc := CaptureRuntime(s)
	rep.Findings = append(rep.Findings, runtimeFindings(&rc)...)
	sortFindings(rep.Findings)
	return rep, rc
}

// repeatedErrors chỉ coi "lỗi / tham số không hợp lệ lặp lại gần đây" là Finding.
// Không đụng tới việc tool bình thường lặp — subagent/novel_context/read_chapter… vốn
// tần suất cao trong lần chạy dài, số lần tích lũy không phải tín hiệu vòng lặp; trường
// hợp "lặp mà không tiến" thật sự do stuckStep đảm nhiệm.
func repeatedErrors(rc *RuntimeCapture) []Finding {
	var out []Finding
	for _, r := range rc.Repeats {
		var rule, title, sugg string
		switch {
		case strings.Contains(r.Sig, " · err: "):
			rule = "RepeatedToolError"
			title = "tool lặp lại báo cùng một lỗi"
			sugg = "Gần đây cùng một tool liên tục trả về cùng một lỗi, thường do tham số của model không hợp lệ hoặc lệch hợp đồng tool; kiểm tra xác thực tool của agentcore / quy ước tham số trong prompt (xem #34)."
		case strings.Contains(r.Sig, "(args invalid)"):
			rule = "ArgsInvalidLoop"
			title = "tham số lặp lại không phân tích được"
			sugg = "Tham số model gửi lên không phân tích được nhưng vẫn thử lại liên tục; xem agentcore có ép kiểu lỏng cho kiểu đó không (xem #34)."
		default:
			continue // tool lặp bình thường không sinh Finding
		}
		sev := SevWarning
		if r.Count >= repeatCritical {
			sev = SevCritical
		}
		out = append(out, Finding{
			Rule:       rule,
			Category:   CatFlow,
			Severity:   sev,
			Confidence: ConfHigh,
			AutoLevel:  AutoNone,
			Target:     "runtime.flow",
			Title:      title,
			Evidence:   fmt.Sprintf("`%s` ×%d", r.Sig, r.Count),
			Suggestion: sugg,
		})
	}
	return out
}

// stuckStep phát hiện checkpoint liên tục dừng ở cùng một step.
func stuckStep(rc *RuntimeCapture) []Finding {
	if rc.StuckStep == "" {
		return nil
	}
	sev := SevWarning
	if rc.StuckCount >= repeatCritical {
		sev = SevCritical
	}
	return []Finding{{
		Rule:       "StuckStep",
		Category:   CatFlow,
		Severity:   sev,
		Confidence: ConfHigh,
		AutoLevel:  AutoNone,
		Target:     "runtime.flow",
		Title:      "checkpoint kẹt ở cùng một step",
		Evidence:   fmt.Sprintf("dừng liên tục tại `%s` ×%d", rc.StuckStep, rc.StuckCount),
		Suggestion: "Cùng một step được ghi lặp lại mà không tiến triển; kết hợp với chữ ký lặp ở trên để xác định tiểu tác nhân nào bị kẹt.",
	}}
}

// streamIdleStorm phát hiện tình trạng luồng bị ngắt thường xuyên (#32).
func streamIdleStorm(rc *RuntimeCapture) []Finding {
	n := rc.LogKinds["stream_idle"]
	if n < streamIdleWarn {
		return nil
	}
	return []Finding{{
		Rule:       "StreamIdleStorm",
		Category:   CatFlow,
		Severity:   SevWarning,
		Confidence: ConfHigh,
		AutoLevel:  AutoNone,
		Target:     "runtime.provider",
		Title:      "luồng bị ngắt thường xuyên (stream_idle)",
		Evidence:   fmt.Sprintf("stream_idle ×%d", n),
		Suggestion: "Phía upstream lâu không trả token nên bị watchdog giết nhầm; với model suy nghĩ chậm hãy tăng streamIdleTimeout, hoặc kiểm tra độ ổn định kết nối của provider (xem #32).",
	}}
}
