package diag

import "fmt"

// PlanActions sinh hành động có thể thực thi từ các Finding độ tin cậy cao.
// Chỉ Finding có Confidence==high && AutoLevel==safe mới sinh ra Action.
func PlanActions(findings []Finding) []Action {
	var actions []Action
	seen := make(map[string]struct{})

	for _, f := range findings {
		if f.Confidence != ConfHigh || f.AutoLevel != AutoSafe {
			continue
		}
		if _, ok := seen[f.Rule]; ok {
			continue
		}
		seen[f.Rule] = struct{}{}

		actions = append(actions, planRule(f)...)
	}
	return actions
}

func planRule(f Finding) []Action {
	key := findingFingerprint(f)

	switch f.Rule {
	case "PhaseFlowMismatch":
		return []Action{
			{SourceRule: f.Rule, Kind: ActionEmitNotice, Severity: f.Severity, Summary: f.Title, Message: f.Title, Fingerprint: key},
			{SourceRule: f.Rule, Kind: ActionEnqueueFollowUp, Severity: f.Severity, Summary: "sửa bất thường máy trạng thái", Message: "Bất thường máy trạng thái: " + f.Evidence + ". Hãy kiểm tra và sửa trạng thái phase/flow của progress trước khi chạy tiếp.", Fingerprint: key},
		}
	case "OutlineExhausted":
		return []Action{
			{SourceRule: f.Rule, Kind: ActionEnqueueFollowUp, Severity: f.Severity, Summary: "xử lý đề cương cạn", Message: "Số chương đã hoàn thành đã chạm giới hạn đã lên kế hoạch. Hãy ưu tiên gọi Kiến trúc sư triển khai cung kế tiếp hoặc thêm tập mới rồi mới viết tiếp.", Fingerprint: key},
		}
	case "OrphanedSteer":
		return []Action{
			{SourceRule: f.Rule, Kind: ActionEnqueueFollowUp, Severity: f.Severity, Summary: "tiêu thụ can thiệp của người dùng chưa xử lý", Message: "Còn chỉ thị can thiệp của người dùng chưa được tiêu thụ, hãy xử lý pending steer trước rồi mới tiếp tục nhiệm vụ hiện tại.", Fingerprint: key},
		}
	default:
		return nil
	}
}

func findingFingerprint(f Finding) string {
	return fmt.Sprintf("%s|%s|%s|%s", f.Rule, f.Target, f.Title, f.Evidence)
}
