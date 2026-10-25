package host

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/voocel/agentcore"
)

// sessionRecord là dạng phân tích nhẹ của một bản ghi trong meta/sessions/*.jsonl —
// chỉ lấy các trường cần cho usage tích luỹ. Content và các trường lớn khác được
// bỏ qua khi phân tích, tiết kiệm IO lúc khởi động.
//
// Quy tắc gán model theo ba tầng giảm dần:
//  1. Usage.Provider/Model — model phản hồi thật do agentcore/litellm truyền qua (ưu tiên)
//  2. Meta(_meta)          — model "đang có hiệu lực" lúc đó do ModelLookup bổ sung khi upstream không truyền
//  3. Không có gì          — replay lùi về effectiveModel, dùng ModelSet hiện tại để suy ra (độ chính xác kém hơn)
type sessionRecord struct {
	Role  agentcore.Role     `json:"role"`
	Usage *agentcore.Usage   `json:"usage,omitempty"`
	Meta  *sessionRecordMeta `json:"_meta,omitempty"`
}

type sessionRecordMeta struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// ReplaySessions quét meta/sessions/agents/*.jsonl,
// cộng lại usage của từng tin nhắn assistant vào tracker. Trả về số bản ghi đã cộng.
//
// Ràng buộc gọi: chỉ gọi một lần để điền lại khi meta/usage.json thiếu.
// Lưu thường kỳ đi qua SaveNow / autoSaveLoop.
//
// Độ chính xác phụ thuộc vào ba tầng giảm dần mô tả trong chú thích sessionRecord —
// tầng 3 (thiếu cả Usage và _meta) chỉ xảy ra với nhật ký cũ hoặc khi upstream lỗi.
func (t *UsageTracker) ReplaySessions(rootDir string) (int, error) {
	if t == nil {
		return 0, nil
	}
	sessionsDir := filepath.Join(rootDir, "meta", "sessions")
	info, err := os.Stat(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return 0, nil
	}

	total := 0
	agentsDir := filepath.Join(sessionsDir, "agents")
	walkErr := filepath.WalkDir(agentsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		agentName := parseAgentNameFromFile(name)
		if agentName == "" {
			return nil
		}
		n, fileErr := t.replayFile(path, agentName)
		if fileErr != nil {
			slog.Warn("replay agent session failed", "module", "usage", "file", name, "err", fileErr)
			return nil
		}
		total += n
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return total, walkErr
	}
	return total, nil
}

// replayFile quét một tệp jsonl, đưa mọi tin nhắn assistant có Usage vào accumulate.
// agentName do bên gọi phân tích từ tên tệp phiên của Worker.
func (t *UsageTracker) replayFile(path, agentName string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	role := agentRoleName(agentName)
	count := 0
	scanner := bufio.NewScanner(f)
	// một dòng có thể rất dài (tin nhắn assistant + tool args đều được gộp phẳng), nới lên 4MB.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec sessionRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Role != agentcore.RoleAssistant || rec.Usage == nil {
			continue
		}
		provider, modelName := usageActualModel(rec.Usage)
		if rec.Meta != nil {
			if provider == "" {
				provider = rec.Meta.Provider
			}
			if modelName == "" {
				modelName = rec.Meta.Model
			}
		}
		t.accumulate(role, provider, modelName, *rec.Usage)
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("scan %s: %w", path, err)
	}
	return count, nil
}

// parseAgentNameFromFile lấy tên agent ("writer-ch01.jsonl" / "architect_short-001.jsonl")
// từ phần trước dấu "-". Quy ước đặt tên xem store/session.go::subAgentPath:
// agentName không chứa dấu dash, suffix là ch<n> hoặc số thứ tự tăng dần.
func parseAgentNameFromFile(name string) string {
	base := strings.TrimSuffix(name, ".jsonl")
	if i := strings.Index(base, "-"); i > 0 {
		return base[:i]
	}
	return ""
}
