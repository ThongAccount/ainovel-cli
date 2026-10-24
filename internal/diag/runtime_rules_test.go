package diag

import "testing"

// TestRuntimeFindings_Classify chứng minh chữ ký lặp được phân loại theo hình thức,
// ngưỡng nâng/hạ cấp đúng, và mọi Finding runtime đều AutoNone (kỷ luật quan sát: chỉ
// chẩn đoán, không sinh Action).
func TestRuntimeFindings_Classify(t *testing.T) {
	rc := RuntimeCapture{
		Repeats: []RepeatStat{
			{Sig: "writer-ch07 · err: InputValidationError", Count: 14}, // vòng lặp lỗi → critical
			{Sig: "writer-ch07 · novel_context", Count: 45},             // tool bình thường tần suất cao → không sinh Finding
			{Sig: "writer · save_plan (args invalid)", Count: 4},        // tham số không hợp lệ → warning
		},
		StuckStep:  "writing.commit_ch07",
		StuckCount: 9, // kẹt → critical
		LogKinds:   map[string]int{"stream_idle": 4},
		LogErrors:  270, // tích lũy trong lần chạy dài, không được tự sinh Finding
	}

	fs := runtimeFindings(&rc)
	sev := map[string]Severity{}
	for _, f := range fs {
		sev[f.Rule] = f.Severity
		if f.AutoLevel != AutoNone {
			t.Errorf("%s phải là AutoNone (kỷ luật quan sát), got %s", f.Rule, f.AutoLevel)
		}
	}

	want := map[string]Severity{
		"RepeatedToolError": SevCritical,
		"ArgsInvalidLoop":   SevWarning,
		"StuckStep":         SevCritical,
		"StreamIdleStorm":   SevWarning,
	}
	for rule, w := range want {
		if sev[rule] != w {
			t.Errorf("%s: got %q want %q", rule, sev[rule], w)
		}
	}
	// Tool bình thường tần suất cao / error tích lũy trong log không được sinh Finding
	// (tránh báo động giả khi chạy dài).
	if _, ok := sev["RepeatedToolCall"]; ok {
		t.Error("tool bình thường lặp không được sinh Finding")
	}
	if _, ok := sev["LogErrorBurst"]; ok {
		t.Error("error tích lũy trong log không được tự sinh Finding")
	}
}

// TestRuntimeFindings_Quiet chứng minh khi không có tín hiệu bất thường thì không sinh
// Finding runtime nào (không báo động giả).
func TestRuntimeFindings_Quiet(t *testing.T) {
	rc := RuntimeCapture{
		LogKinds:  map[string]int{"stream_idle": 1}, // dưới ngưỡng
		LogErrors: 2,
	}
	if fs := runtimeFindings(&rc); len(fs) != 0 {
		t.Errorf("trạng thái yên tĩnh không được sinh Finding, got %d: %+v", len(fs), fs)
	}
}
