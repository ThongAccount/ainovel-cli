package diag

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/store"
)

// sentinel là một đoạn "văn bản tiểu thuyết" tuyệt đối không được xuất hiện trong bản xuất.
//
// Nội dung cố ý để bằng tiếng Trung: đây là dữ liệu thử nghiệm để chứng minh văn bản chính
// tiếng Trung cũng bị làm mờ.
const sentinel = "雪夜里主角揭穿了反派的惊天阴谋这是机密正文"

// writeSession ghi một số thông điệp theo định dạng sessions/*.jsonl vào thư mục output tạm.
func writeSession(t *testing.T, rel string, msgs []agentcore.Message) string {
	t.Helper()
	dir := t.TempDir()
	writeSessionAt(t, dir, rel, msgs)
	return dir
}

func writeSessionAt(t *testing.T, dir, rel string, msgs []agentcore.Message) {
	t.Helper()
	path := filepath.Join(dir, "meta", "sessions", rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var b strings.Builder
	for _, m := range msgs {
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func commitCall(chapterRaw string) agentcore.Message {
	args := json.RawMessage(`{"chapter":` + chapterRaw + `,"content":"` + sentinel + sentinel + `"}`)
	return agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.ToolCallBlock(agentcore.ToolCall{Name: "commit_chapter", Args: args})},
	}
}

func errResult(msg string) agentcore.Message {
	return agentcore.Message{
		Role:     agentcore.RoleTool,
		Content:  []agentcore.ContentBlock{agentcore.TextBlock(msg)},
		Metadata: map[string]any{"is_error": true},
	}
}

// TestExport_DeathLoopShape tái hiện #34 từ đầu đến cuối: model chuyển tham số chapter
// của commit_chapter thành chuỗi nên vướng vòng lặp kiểm tra. Khẳng định bản xuất định vị
// được vấn đề và văn bản tiểu thuyết không lọt ra ngoài.
func TestExport_DeathLoopShape(t *testing.T) {
	var msgs []agentcore.Message
	// Một đoạn văn bản thô do Worker xuất ra (<4KB, lọt qua session_compact), phải bị làm mờ.
	msgs = append(msgs, agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(sentinel)},
	})
	// 14 vòng commit_chapter(chapter:"7") + InputValidationError.
	for range 14 {
		msgs = append(msgs, commitCall(`"7"`))
		msgs = append(msgs, errResult("InputValidationError: chapter must be int"))
	}

	dir := writeSession(t, filepath.Join("agents", "writer-ch07.jsonl"), msgs)
	s := store.NewStore(dir)
	rep, rc := Diagnose(s)
	out := string(RenderExport(rep, rc))

	if strings.Contains(out, sentinel) {
		t.Fatalf("văn bản tiểu thuyết đã lọt ra! bản xuất chứa sentinel:\n%s", out)
	}
	if !strings.Contains(out, `chapter: "7"`) {
		t.Errorf("thiếu tín hiệu bất thường về kiểu chapter: \"7\" (nguyên nhân gốc #34)\n%s", out)
	}
	if !strings.Contains(out, "InputValidationError") {
		t.Errorf("chuỗi lỗi không được giữ lại\n%s", out)
	}
	if !strings.Contains(out, "×14") {
		t.Errorf("nhóm lặp không liệt kê ×14\n%s", out)
	}
	// Phase 2: bộ phát hiện runtime phải phán đoán vòng lặp này là RepeatedToolError mức critical.
	if !strings.Contains(out, "tool lặp lại báo cùng một lỗi") {
		t.Errorf("bộ phát hiện runtime không sinh RepeatedToolError\n%s", out)
	}
	if !strings.Contains(out, "[critical]") {
		t.Errorf("14 lần lặp phải được nâng lên critical\n%s", out)
	}
}

// TestExport_NumberVsStringArg chứng minh phép chiếu phân biệt được giá trị vô hướng
// với chuỗi: chapter:7 (số) giữ thành 7, chapter:"7" (chuỗi) giữ thành "7".
func TestExport_NumberVsStringArg(t *testing.T) {
	intDir := writeSession(t, filepath.Join("agents", "writer-ch07.jsonl"), []agentcore.Message{commitCall(`7`)})
	si := store.NewStore(intDir)
	repInt, rcInt := Diagnose(si)
	outInt := string(RenderExport(repInt, rcInt))
	if !strings.Contains(outInt, "chapter: 7") || strings.Contains(outInt, `chapter: "7"`) {
		t.Errorf("tham số dạng số phải được render thành chapter: 7 (không có dấu nháy)\n%s", outInt)
	}
}

// TestProjectValue_ProseArgRedacted bảo vệ ranh giới khử nhạy cảm: giá trị ngắn kiểu
// định danh được giữ, còn giá trị tiếng Trung / có khoảng trắng (ví dụ dispatch task,
// chapter title) đều bị làm mờ.
func TestProjectValue_ProseArgRedacted(t *testing.T) {
	keep := map[string]string{
		`"7"`:       `"7"`,       // số bị chuyển thành chuỗi (tín hiệu #34)
		`"premise"`: `"premise"`, // enum
		`"writer"`:  `"writer"`,  // tên vai trò
		`7`:         `7`,         // giá trị vô hướng dạng số
		`true`:      `true`,      // giá trị vô hướng bool
	}
	for in, want := range keep {
		if got := projectValue([]byte(in)); got != want {
			t.Errorf("phải giữ %s: got %q want %q", in, got, want)
		}
	}
	// Chứa tiếng Trung / khoảng trắng → phải bị làm mờ và không chứa nguyên văn.
	// Các giá trị thử nghiệm cố ý để bằng tiếng Trung để kiểm tra đúng ranh giới này.
	prose := []string{`"第7章 雪夜的真相"`, `"雪夜杀机"`, `"主角揭穿阴谋"`}
	for _, in := range prose {
		got := projectValue([]byte(in))
		if !strings.HasPrefix(got, "<redacted") {
			t.Errorf("giá trị ngắn tiếng Trung / có khoảng trắng phải bị làm mờ: %s → %q", in, got)
		}
		if strings.Contains(got, "雪夜") || strings.Contains(got, "主角") {
			t.Errorf("sau khi làm mờ vẫn còn văn bản chính: %s → %q", in, got)
		}
	}
}

// TestWriteExport_WritesFile chứng minh đường thuần hàm: không phụ thuộc TUI, ghi ra
// đường dẫn tương đối cố định.
func TestWriteExport_WritesFile(t *testing.T) {
	dir := writeSession(t, filepath.Join("agents", "writer-ch07.jsonl"), []agentcore.Message{commitCall(`"7"`), errResult("boom")})
	s := store.NewStore(dir)

	rep, rc := Diagnose(s)
	path, err := WriteExport(s, rep, rc)
	if err != nil {
		t.Fatalf("WriteExport: %v", err)
	}
	if want := filepath.Join(dir, filepath.FromSlash(ExportRelPath)); path != want {
		t.Errorf("đường dẫn không đúng: got %s want %s", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(data), "diag-export") {
		t.Errorf("nội dung tệp bất thường\n%s", data)
	}
	if strings.Contains(string(data), sentinel) {
		t.Errorf("tệp đã ghi chứa văn bản chính")
	}
}

// TestRedactMessage_DupSha chứng minh cùng một đoạn văn bản xuất hiện lặp sẽ cho cùng sha
// (tín hiệu vòng lặp).
func TestRedactMessage_DupSha(t *testing.T) {
	a := redactMessage("writer-ch07", agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(sentinel)},
	})
	b := redactMessage("writer-ch07", agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(sentinel)},
	})
	if a.TextSha == "" || a.TextSha != b.TextSha {
		t.Errorf("cùng văn bản chính phải cho cùng sha: %q vs %q", a.TextSha, b.TextSha)
	}
	if a.Redacted != 1 {
		t.Errorf("phải làm mờ 1 khối văn bản, got %d", a.Redacted)
	}
}
