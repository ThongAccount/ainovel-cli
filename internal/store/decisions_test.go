package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecisionStore_AppendAndRecent(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	first, err := s.Decisions.Append(DecisionRecord{
		Kind: "intervention", Decider: "arbiter",
		Input: "viết lại chương 3", Facts: json.RawMessage(`{"phase":"writing"}`),
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if first.ID == "" || first.At == "" || first.SchemaVersion != decisionSchemaVersion {
		t.Fatalf("Append phải bổ sung ID/At/SchemaVersion: %+v", first)
	}

	if _, err := s.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: "tiếp tục viết"}); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	// Phán quyết thất bại: error là sự thật kiểm toán, phải ghi xuống nguyên trạng và đọc lại được.
	if _, err := s.Decisions.Append(DecisionRecord{Kind: "plan_start", Decider: "arbiter", Input: "phàm tu luyện sinh", Error: "USER_INACTIVE"}); err != nil {
		t.Fatalf("append 3: %v", err)
	}

	recent, err := s.Decisions.Recent(10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("phải có 3 bản ghi, got %d", len(recent))
	}
	if recent[2].Error != "USER_INACTIVE" || len(recent[2].Decision) != 0 {
		t.Fatalf("phán quyết thất bại phải có error và không có decision: %+v", recent[2])
	}
	if recent[0].Input != "viết lại chương 3" || recent[1].Input != "tiếp tục viết" {
		t.Fatalf("thứ tự bản ghi phải là cũ→mới: %+v", recent)
	}

	// Cắt theo n: chỉ lấy 1 bản ghi gần nhất
	last, err := s.Decisions.Recent(1)
	if err != nil || len(last) != 1 || last[0].Input != "phàm tu luyện sinh" {
		t.Fatalf("Recent(1) phải lấy bản ghi mới nhất, got %+v err=%v", last, err)
	}
}

func TestDecisionStore_InputTruncation(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	huge := strings.Repeat("á", maxDecisionInputBytes) // 2 byte/ký tự, vượt xa ngưỡng
	rec, err := s.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: huge})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if !rec.InputTruncated || len(rec.Input) > maxDecisionInputBytes {
		t.Fatalf("input vượt ngưỡng phải bị cắt và đánh dấu: truncated=%v len=%d", rec.InputTruncated, len(rec.Input))
	}
	// Bản ghi sau khi cắt vẫn phải đọc lại được
	recent, err := s.Decisions.Recent(1)
	if err != nil || len(recent) != 1 {
		t.Fatalf("đọc lại thất bại: %v", err)
	}
}

// Dòng hỏng đã ghi ký ở giữa tệp (sau đó vẫn có dòng đã ghi ký hoàn chỉnh) phải hỏng cứng
// — không được phán quyết trên lịch sử bị khuyết.
func TestDecisionStore_RecentRejectsCommittedCorruptLine(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := s.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: "được"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	// Dòng hỏng đã kết thúc bằng '\n' (đã ghi ký nhưng hỏng), sau đó ghi nối một bản ghi hoàn chỉnh.
	if err := s.Decisions.io.AppendLine(decisionsFile, []byte("{\"schema_version\":1,\"kind\":\"interv\n")); err != nil {
		t.Fatalf("append corrupt: %v", err)
	}
	if _, err := s.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: "sau đó"}); err != nil {
		t.Fatalf("append trailing: %v", err)
	}
	if _, err := s.Decisions.Recent(10); err == nil {
		t.Fatal("dòng hỏng đã ghi ký ở giữa tệp phải báo lỗi tường minh")
	}
}

// Dòng cuối sót lại do sập (byte cuối khác '\n', phần ghi nối chưa ghi ký) được dung thứ
// như not-exist: bỏ dòng sót, trả về các bản ghi hoàn chỉnh trước đó, không hỏng cứng —
// nếu không, một lần sập sẽ làm hỏng vĩnh viễn nhật ký audit append-only.
func TestDecisionStore_RecentToleratesUncommittedTail(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := s.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: "được"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	// Mô phỏng dòng cuối sót lại do sập: không có dấu xuống dòng.
	if err := s.Decisions.io.AppendLine(decisionsFile, []byte(`{"schema_version":1,"kind":"interv`)); err != nil {
		t.Fatalf("append partial: %v", err)
	}
	recent, err := s.Decisions.Recent(10)
	if err != nil {
		t.Fatalf("dòng cuối sót lại phải được dung thứ, không được báo lỗi: %v", err)
	}
	if len(recent) != 1 || recent[0].Input != "được" {
		t.Fatalf("phải bỏ dòng sót và giữ bản ghi đã ghi ký, got: %+v", recent)
	}
	// Việc khôi phục phải thực sự cắt phần cuối trên đĩa, chứ không chỉ bỏ qua trong lần đọc
	// này; nếu không, lần ghi nối kế tiếp sẽ nối hai đoạn JSON thành dữ liệu hỏng vĩnh viễn.
	// Sau khi ghi nối, đọc lại phải giữ được vòng khép hoàn chỉnh.
	if _, err := s.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: "sau khi khôi phục"}); err != nil {
		t.Fatalf("append after recovery: %v", err)
	}
	recent, err = s.Decisions.Recent(10)
	if err != nil {
		t.Fatalf("recent after append: %v", err)
	}
	if len(recent) != 2 || recent[0].Input != "được" || recent[1].Input != "sau khi khôi phục" {
		t.Fatalf("sau khi khôi phục phần cuối phải ghi nối tiếp được, got: %+v", recent)
	}
	raw, err := os.ReadFile(filepath.Join(dir, decisionsFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatalf("tệp audit sau khi khôi phục phải kết thúc bằng dấu xuống dòng của bản ghi: %q", raw)
	}
}

// Dù phần cuối có đúng là JSON hoàn chỉnh, chừng nào thiếu dấu xuống dòng mà giao thức
// yêu cầu thì vẫn là bản ghi chưa ghi ký; việc khôi phục phải bỏ nó và bảo đảm các lần
// ghi nối sau không tạo ra chuỗi `}{`.
func TestDecisionStore_RecoveryDropsValidJSONWithoutCommitNewline(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := s.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: "đã ghi ký"}); err != nil {
		t.Fatal(err)
	}
	partial, err := json.Marshal(DecisionRecord{SchemaVersion: decisionSchemaVersion, Kind: "intervention", Input: "chưa ghi ký"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Decisions.io.AppendLine(decisionsFile, partial); err != nil {
		t.Fatal(err)
	}

	// Mô phỏng khởi động lại: lần đọc hoặc ghi nối kế tiếp chính là ranh giới khôi phục audit.
	reopened := NewStore(dir)
	if err := reopened.Init(); err != nil {
		t.Fatalf("restart init: %v", err)
	}
	if _, err := reopened.Decisions.Append(DecisionRecord{Kind: "intervention", Decider: "arbiter", Input: "sau khi khởi động lại"}); err != nil {
		t.Fatal(err)
	}
	recent, err := reopened.Decisions.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].Input != "đã ghi ký" || recent[1].Input != "sau khi khởi động lại" {
		t.Fatalf("JSON chưa ghi ký không có dấu xuống dòng không được chấp nhận, got: %+v", recent)
	}
}
