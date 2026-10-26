package sim

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

type scriptedLLM struct {
	responses []string
	calls     atomic.Int32
}

func (s *scriptedLLM) Generate(_ context.Context, _ []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	idx := int(s.calls.Add(1)) - 1
	if idx >= len(s.responses) {
		return nil, fmt.Errorf("scriptedLLM đã hết phản hồi ở lượt gọi %d", idx+1)
	}
	return &agentcore.LLMResponse{
		Message: agentcore.Message{
			Role:      agentcore.RoleAssistant,
			Content:   []agentcore.ContentBlock{agentcore.TextBlock(s.responses[idx])},
			Timestamp: time.Now(),
		},
	}, nil
}

func TestRunnerGeneratesProfileThenSkipsUnchanged(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "simulate")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "a.txt"), []byte("Một móc câu mở đầu căng thẳng.\nMột cú lộ diễn nhanh.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sourceDir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "nested", "b.md"), []byte("# B\n\nMẫu thứ hai có tình huống treo."), 0o644); err != nil {
		t.Fatal(err)
	}

	st := store.NewStore(filepath.Join(dir, "output", "novel"))
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	llm := &scriptedLLM{responses: []string{
		validSourceReportJSON("a tone"),
		validSourceReportJSON("b tone"),
		validSynthesisJSON("tight pacing"),
	}}

	events, err := Run(context.Background(), Deps{
		Store:   st,
		LLM:     llm,
		Prompts: Prompts{Source: "source prompt", Merge: "merge prompt"},
	}, Options{SourceDir: sourceDir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var last Event
	for ev := range events {
		if ev.Err != nil {
			t.Fatalf("mô phỏng bị lỗi: %v", ev.Err)
		}
		last = ev
	}
	if last.Stage != StageDone {
		t.Fatalf("giai đoạn cuối = %s, cần %s", last.Stage, StageDone)
	}
	if got := llm.calls.Load(); got != 3 {
		t.Fatalf("số lượt gọi LLM của lần chạy đầu = %d, cần 3", got)
	}
	profile, err := st.Simulation.Load()
	if err != nil {
		t.Fatalf("nạp profile: %v", err)
	}
	if profile == nil || len(profile.Corpus.Sources) != 2 || len(profile.SourceReports) != 2 {
		t.Fatalf("profile chưa được lưu với hai nguồn: %+v", profile)
	}

	llm2 := &scriptedLLM{}
	events, err = Run(context.Background(), Deps{
		Store:   st,
		LLM:     llm2,
		Prompts: Prompts{Source: "source prompt", Merge: "merge prompt"},
	}, Options{SourceDir: sourceDir})
	if err != nil {
		t.Fatalf("chạy lại Run: %v", err)
	}
	var upToDate bool
	for ev := range events {
		if ev.Err != nil {
			t.Fatalf("chạy lại bị lỗi: %v", ev.Err)
		}
		if strings.Contains(ev.Message, "Hồ sơ mô phỏng đã là bản mới nhất") {
			upToDate = true
		}
	}
	if !upToDate {
		t.Fatal("chờ thông điệp bản đã là mới nhất")
	}
	if got := llm2.calls.Load(); got != 0 {
		t.Fatalf("số lượt gọi LLM khi chạy lại không đổi = %d, cần 0", got)
	}
}

func TestRunnerIncrementallyAnalyzesNewAndChangedSources(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "simulate")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	aPath := filepath.Join(sourceDir, "a.txt")
	if err := os.WriteFile(aPath, []byte("phiên bản đầu"), 0o644); err != nil {
		t.Fatal(err)
	}

	st := store.NewStore(filepath.Join(dir, "output", "novel"))
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	firstLLM := &scriptedLLM{responses: []string{
		validSourceReportJSON("first tone"),
		validSynthesisJSON("first synthesis"),
	}}
	drainRun(t, st, firstLLM, sourceDir)
	if got := firstLLM.calls.Load(); got != 2 {
		t.Fatalf("số lượt gọi của lần chạy đầu = %d, cần 2", got)
	}

	if err := os.WriteFile(filepath.Join(sourceDir, "c.markdown"), []byte("tài liệu mới"), 0o644); err != nil {
		t.Fatal(err)
	}
	addLLM := &scriptedLLM{responses: []string{
		validSourceReportJSON("new tone"),
		validSynthesisJSON("expanded synthesis"),
	}}
	drainRun(t, st, addLLM, sourceDir)
	if got := addLLM.calls.Load(); got != 2 {
		t.Fatalf("số lượt gọi khi thêm nguồn mới = %d, cần 2", got)
	}
	profile, _ := st.Simulation.Load()
	if len(profile.Corpus.Sources) != 2 {
		t.Fatalf("sau khi thêm nguồn, số nguồn = %d, cần 2", len(profile.Corpus.Sources))
	}

	oldHash := sourceHashForPath(t, profile, "a.txt")
	if err := os.WriteFile(aPath, []byte("phiên bản đã thay đổi"), 0o644); err != nil {
		t.Fatal(err)
	}
	changeLLM := &scriptedLLM{responses: []string{
		validSourceReportJSON("changed tone"),
		validSynthesisJSON("changed synthesis"),
	}}
	drainRun(t, st, changeLLM, sourceDir)
	if got := changeLLM.calls.Load(); got != 2 {
		t.Fatalf("số lượt gọi khi nguồn đã đổi = %d, cần 2", got)
	}
	profile, _ = st.Simulation.Load()
	if len(profile.Corpus.Sources) != 2 {
		t.Fatalf("nguồn đã đổi phải thay thế cùng đường dẫn, nhận %d nguồn", len(profile.Corpus.Sources))
	}
	if newHash := sourceHashForPath(t, profile, "a.txt"); newHash == oldHash {
		t.Fatal("chờ vân tay của nguồn đã đổi được cập nhật")
	}
}

func drainRun(t *testing.T, st *store.Store, llm *scriptedLLM, sourceDir string) {
	t.Helper()
	events, err := Run(context.Background(), Deps{
		Store:   st,
		LLM:     llm,
		Prompts: Prompts{Source: "source prompt", Merge: "merge prompt"},
	}, Options{SourceDir: sourceDir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for ev := range events {
		if ev.Err != nil {
			t.Fatalf("mô phỏng bị lỗi: %v", ev.Err)
		}
	}
}

func sourceHashForPath(t *testing.T, profile *domain.SimulationProfile, rel string) string {
	t.Helper()
	for _, source := range profile.Corpus.Sources {
		if source.RelativePath == rel {
			return source.SHA256
		}
	}
	t.Fatalf("không tìm thấy nguồn %q", rel)
	return ""
}

func validSourceReportJSON(summary string) string {
	return fmt.Sprintf(`{
  "title": null,
  "summary": %q,
  "style_observations": ["góc nhìn gần", "động từ giác quan"],
  "common_words": ["cánh cửa", "bóng tối"],
  "plot_patterns": ["mục tiêu cảnh biến thành tình thế khó hơn"],
  "hook_patterns": ["kết thúc bằng một lựa chọn chưa có đáp án"],
  "pacing_notes": ["mở đầu ngắn, biến cố tiến triển nhanh"],
  "reader_appeal": ["khoảng trống tò mò", "rủi ro rõ ràng"],
  "reusable_techniques": ["đặt sẵn một vật cụ thể trước khi tiết lộ"],
  "warnings": []
}`, summary)
}

func validSynthesisJSON(note string) string {
	return fmt.Sprintf(`{
  "style": {
    "narrative_voice": ["ngôi kể gần giới hạn"],
    "sentence_rhythm": ["pha câu ngắn giàu sức đập với câu hành động vừa phải"],
    "prose_texture": [%q],
    "perspective": ["giữ gần nhân vật chính"],
    "mood": ["căng thẳng, khẩn cấp"],
    "do_not_copy": ["không tái sử dụng tên hoặc câu gốc"]
  },
  "lexicon": {
    "common_words": ["cánh cửa", "bóng tối"],
    "emotion_words": ["do dự"],
    "scene_words": ["hành lang"],
    "transition_words": ["đồng thời"],
    "signature_phrases": ["chưa"]
  },
  "plot_design": {
    "opening_patterns": ["bắt đầu trong áp lực chưa được giải quyết"],
    "escalation_patterns": ["làm rõ cái giá sau mỗi câu trả lời"],
    "turning_point_patterns": ["đặt lại khung của phục bút"],
    "payoff_patterns": ["thu hồi vật đã đặt trước khi thêm câu hỏi tiếp theo"]
  },
  "hook_design": {
    "hook_types": ["bí ẩn", "lựa chọn"],
    "placement": ["mở và đóng cảnh bằng thay đổi rủi ro"],
    "cliffhanger_patterns": ["lựa chọn trước hậu quả"],
    "payoff_rules": ["trả lời một câu hỏi trong khi mở câu hỏi khác"]
  },
  "pacing_density": {
    "scene_density": ["một cảnh nên mang mục tiêu, trở ngại và bước ngoặt"],
    "information_release": ["trì hoãn phần giải thích đến sau hành động"],
    "dialogue_action_ratio": ["đối thoại phải làm thay đổi thế cân bằng quyền lực"],
    "compression_rules": ["tóm tắt đoạn chuyển, diễn lại quyết định"]
  },
  "reader_engagement": {
    "methods": ["khoảng trống tò mò", "rủi ro rõ ràng"],
    "emotional_drivers": ["sợ mất mát"],
    "progression_rewards": ["thu được phục bút rõ ràng"],
    "anti_patterns": ["phần giải thích phẳng"]
  },
  "role_guidance": {
    "architect": ["thiết kế cung với chi phí tăng dần lặp lại"],
    "writer": ["học hỏi kỹ thuật, không bao giờ sao chép văn bản"],
    "editor": ["kiểm tra sự mô phỏng chỉ ở cấp cấu trúc, không sao chép đạo văn"]
  }
}`, note)
}
