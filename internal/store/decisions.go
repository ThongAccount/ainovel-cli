package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// DecisionStore lưu vết kiểm toán các phán quyết ngữ nghĩa của LLM lúc chạy
// (meta/decisions.jsonl, chỉ ghi nối).
//
// Định vị (docs/engine-arbiter.md §4.3): nguồn dữ liệu cho kiểm toán và phát lại
// ngoại tuyến — ghi lại “lúc đó thấy sự thật nào, đã phán quyết ra sao”, phục vụ hồi
// quy eval và so sánh A/B cho Tài phán trong tương lai. Nó **không phải** nguồn
// truy vết sự kiện, cũng **không phải** nguồn khôi phục (khôi phục chỉ dựa trên
// Progress/Checkpoint/RunMeta và các lớp sự thật khác).
type DecisionStore struct{ io *IO }

func NewDecisionStore(io *IO) *DecisionStore { return &DecisionStore{io: io} }

const (
	decisionSchemaVersion = 1
	decisionsFile         = "meta/decisions.jsonl"
	// maxDecisionInputBytes giới hạn trên của một input; vượt ngưỡng thì cắt bớt và
	// đánh dấu, để nội dung dán quá dài không làm phình kích thước tệp kiểm toán.
	maxDecisionInputBytes = 8 << 10
)

// DecisionRecord là bản ghi kiểm toán của một phán quyết ngữ nghĩa. facts chỉ lưu sự
// thật có cấu trúc và tham chiếu, không sao chép văn bản.
// input được giữ trong bản ghi (cần cho phát lại ngoại tuyến); việc khử nhạy cảm
// diễn ra ở ranh giới export của diag, không phải lúc ghi xuống.
type DecisionRecord struct {
	SchemaVersion  int             `json:"schema_version"`
	ID             string          `json:"id"`
	At             string          `json:"at"`
	Kind           string          `json:"kind"`    // intervention | plan_start | volume_end | ...
	Decider        string          `json:"decider"` // arbiter | architect (thẩm duyệt cuối tập)
	CheckpointSeq  int64           `json:"checkpoint_seq,omitempty"`
	Input          string          `json:"input,omitempty"`
	InputTruncated bool            `json:"input_truncated,omitempty"`
	Facts          json.RawMessage `json:"facts,omitempty"`
	Decision       json.RawMessage `json:"decision,omitempty"`
	Reason         string          `json:"reason,omitempty"`
	Error          string          `json:"error,omitempty"` // văn bản lỗi khi phán quyết thất bại — thất bại cũng là sự thật kiểm toán, thiếu nó thì chẩn đoán chỉ còn dựa vào suy đoán
	Model          string          `json:"model,omitempty"`
	DurationMs     int64           `json:"duration_ms,omitempty"`
}

// Append ghi một bản ghi phán quyết xuống; SchemaVersion/At/ID được bổ sung bởi
// phương thức này, input vượt ngưỡng sẽ bị cắt bớt.
// Trả về bản ghi đã bổ sung (ID để bên gọi liên kết, ví dụ PlanStartRecord.DecisionID).
func (s *DecisionStore) Append(rec DecisionRecord) (DecisionRecord, error) {
	rec.SchemaVersion = decisionSchemaVersion
	if rec.At == "" {
		rec.At = time.Now().Format(time.RFC3339)
	}
	if rec.ID == "" {
		rec.ID = newDecisionID()
	}
	if len(rec.Input) > maxDecisionInputBytes {
		rec.Input = rec.Input[:maxDecisionInputBytes]
		rec.InputTruncated = true
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return rec, fmt.Errorf("mã hóa bản ghi phán quyết: %w", err)
	}
	s.io.mu.Lock()
	defer s.io.mu.Unlock()
	// Lần ghi nối trước có thể hỏng trước khi ghi dấu xuống dòng. Xóa trước phần cuối
	// mà giao thức chứng minh được là chưa ghi ký, để không nối JSON mới vào sau một
	// dòng cụt; bản ghi hoàn chỉnh có dấu xuống dòng tuyệt đối không tự ý sửa.
	if _, err := s.committedDataUnlocked(); err != nil {
		return rec, fmt.Errorf("repair decision tail: %w", err)
	}
	if err := s.io.AppendLineUnlocked(decisionsFile, append(data, '\n')); err != nil {
		return rec, err
	}
	return rec, nil
}

// Recent trả về n bản ghi gần nhất (cũ→mới); trả về rỗng nếu thiếu tệp.
//
// Dòng đã ghi ký mà bị hỏng phải trả lỗi tường minh — Tài phán không thể tiếp tục
// phán quyết trên một gói sự thật thiếu một phần lịch sử. Phần cuối cụt do sập giữa
// chừng (byte cuối khác '\n') bị committedDataUnlocked cắt bớt và cảnh báo tường minh;
// đây không phải sửa theo phỏng đoán, vì giao thức của tệp này quy định chỉ bản ghi
// kết thúc bằng dấu xuống dòng mới được coi là đã ghi ký.
func (s *DecisionStore) Recent(n int) ([]DecisionRecord, error) {
	s.io.mu.Lock()
	defer s.io.mu.Unlock()
	data, err := s.committedDataUnlocked()
	if err != nil {
		return nil, err
	}
	all, err := parseDecisionRecords(data)
	if err != nil {
		return nil, err
	}
	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return all, nil
}

// committedDataUnlocked trả về các bản ghi hoàn chỉnh có dấu xuống dòng và cắt các
// byte tàn dư sau dấu xuống dòng khỏi đĩa. Bên gọi phải giữ khóa ghi io.mu. Việc cắt
// là idempotent; nếu thất bại thì tệp gốc được giữ nguyên và lỗi được ném lên rõ ràng.
func (s *DecisionStore) committedDataUnlocked() ([]byte, error) {
	data, err := s.io.ReadFileUnlocked(decisionsFile)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || data[len(data)-1] == '\n' {
		return data, nil
	}
	keep := bytes.LastIndexByte(data, '\n') + 1
	if err := os.Truncate(s.io.path(decisionsFile), int64(keep)); err != nil {
		return nil, err
	}
	slog.Warn("Đã sửa phần cuối chưa ghi ký của nhật ký phán quyết",
		"module", "store", "file", decisionsFile, "discarded_bytes", len(data)-keep)
	return data[:keep], nil
}

func parseDecisionRecords(data []byte) ([]DecisionRecord, error) {
	var all []DecisionRecord
	lines := bytes.Split(data, []byte{'\n'})
	for i, raw := range lines {
		if i == len(lines)-1 && len(raw) == 0 {
			break
		}
		var rec DecisionRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return nil, fmt.Errorf("phân tích %s dòng %d: %w", decisionsFile, i+1, err)
		}
		all = append(all, rec)
	}
	return all, nil
}

func newDecisionID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("dec-%d", time.Now().UnixNano())
	}
	return "dec-" + hex.EncodeToString(b[:])
}
