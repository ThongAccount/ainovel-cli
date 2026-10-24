package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/voocel/ainovel-cli/internal/domain"
)

const checkpointsFile = "meta/checkpoints.jsonl"

// CheckpointStore quản lý việc ghi nối và truy vấn checkpoint cấp bước.
// Định dạng đĩa: meta/checkpoints.jsonl, chỉ ghi nối; truy vấn đi qua ảnh trong bộ nhớ.
// Bất biến: cache là ảnh của checkpoints.jsonl, được Append/Reset bảo trì tại một chỗ.
// Đồng thời: cache được bảo vệ bởi io.mu, ghi dùng Lock, đọc dùng RLock.
type CheckpointStore struct {
	io      *IO
	seqGen  atomic.Int64
	cache   []domain.Checkpoint
	loadErr error
}

// NewCheckpointStore tạo kho checkpoint, tải một lần các checkpoint sẵn có từ đĩa vào cache.
func NewCheckpointStore(io *IO) *CheckpointStore {
	cs := &CheckpointStore{io: io}
	cs.loadFromDisk()
	return cs
}

// loadFromDisk đọc jsonl trên đĩa vào cache một lần và khôi phục seqGen.
func (cs *CheckpointStore) loadFromDisk() {
	cs.io.mu.Lock()
	defer cs.io.mu.Unlock()

	cs.cache, cs.loadErr = readCheckpointsFile(cs.io.path(checkpointsFile))
	var maxSeq int64
	for _, cp := range cs.cache {
		if cp.Seq > maxSeq {
			maxSeq = cp.Seq
		}
	}
	cs.seqGen.Store(maxSeq)
}

// Append ghi nối một checkpoint.
// Idempotent: nếu Scope + Step + Digest giống nhau đã tồn tại thì bỏ qua ghi và
// trả về luôn bản ghi đã có.
func (cs *CheckpointStore) Append(scope domain.Scope, step, artifact, digest string) (*domain.Checkpoint, error) {
	cs.io.mu.Lock()
	defer cs.io.mu.Unlock()
	if cs.loadErr != nil {
		return nil, fmt.Errorf("khởi tạo kho checkpoint thất bại: %w", cs.loadErr)
	}

	if digest != "" {
		for i := len(cs.cache) - 1; i >= 0; i-- {
			cp := cs.cache[i]
			if cp.Scope.Matches(scope) && cp.Step == step && cp.Digest == digest {
				return &cp, nil
			}
		}
	}

	// Chỉ tăng seq sau khi ghi thành công, để tránh lỗi ghi để lại lỗ hổng số vĩnh viễn.
	// Đã giữ khóa ghi io.mu nên không có đọc-ghi chen nhau giữa Load và Store.
	seq := cs.seqGen.Load() + 1
	cp := domain.Checkpoint{
		Seq:        seq,
		Scope:      scope,
		Step:       step,
		Artifact:   artifact,
		Digest:     digest,
		OccurredAt: time.Now(),
	}

	data, err := json.Marshal(cp)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := cs.io.AppendLineUnlocked(checkpointsFile, data); err != nil {
		return nil, err
	}
	cs.seqGen.Store(seq)
	cs.cache = append(cs.cache, cp)
	return &cp, nil
}

// AppendArtifact tính dấu vân tay nội dung của artifact rồi ghi nối checkpoint.
func (cs *CheckpointStore) AppendArtifact(scope domain.Scope, step, artifact string) (*domain.Checkpoint, error) {
	if artifact == "" {
		return cs.Append(scope, step, "", "")
	}
	data, err := cs.io.ReadFile(artifact)
	if err != nil {
		return nil, fmt.Errorf("tính dấu vân tay artifact %s: %w", artifact, err)
	}
	sum := sha256.Sum256(data)
	return cs.Append(scope, step, artifact, "sha256:"+hex.EncodeToString(sum[:]))
}

// AppendArtifacts tạo dấu vân tay tổ hợp cho nhiều artifact chính thức của cùng một bước.
// Artifact giữ đường dẫn của artifact chính đầu tiên; bất kỳ artifact liên kết nào
// thay đổi cũng sinh checkpoint mới.
func (cs *CheckpointStore) AppendArtifacts(scope domain.Scope, step string, artifacts ...string) (*domain.Checkpoint, error) {
	if len(artifacts) == 0 {
		return cs.Append(scope, step, "", "")
	}
	h := sha256.New()
	for _, artifact := range artifacts {
		data, err := cs.io.ReadFile(artifact)
		if err != nil {
			return nil, fmt.Errorf("tính dấu vân tay artifact %s: %w", artifact, err)
		}
		_, _ = h.Write([]byte(artifact))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return cs.Append(scope, step, artifacts[0], "sha256:"+hex.EncodeToString(h.Sum(nil)))
}

// Latest trả về checkpoint mới nhất của scope chỉ định.
func (cs *CheckpointStore) Latest(scope domain.Scope) *domain.Checkpoint {
	cs.io.mu.RLock()
	defer cs.io.mu.RUnlock()
	for i := len(cs.cache) - 1; i >= 0; i-- {
		if cs.cache[i].Scope.Matches(scope) {
			cp := cs.cache[i]
			return &cp
		}
	}
	return nil
}

// LatestByStep trả về checkpoint mới nhất của scope + step chỉ định.
func (cs *CheckpointStore) LatestByStep(scope domain.Scope, step string) *domain.Checkpoint {
	cs.io.mu.RLock()
	defer cs.io.mu.RUnlock()
	for i := len(cs.cache) - 1; i >= 0; i-- {
		cp := cs.cache[i]
		if cp.Scope.Matches(scope) && cp.Step == step {
			return &cp
		}
	}
	return nil
}

// LatestGlobal trả về checkpoint mới nhất toàn cục (không phân biệt scope).
func (cs *CheckpointStore) LatestGlobal() *domain.Checkpoint {
	cs.io.mu.RLock()
	defer cs.io.mu.RUnlock()
	if len(cs.cache) == 0 {
		return nil
	}
	cp := cs.cache[len(cs.cache)-1]
	return &cp
}

// All trả về bản sao danh sách toàn bộ checkpoint (tăng dần theo seq).
func (cs *CheckpointStore) All() []domain.Checkpoint {
	cs.io.mu.RLock()
	defer cs.io.mu.RUnlock()
	if len(cs.cache) == 0 {
		return nil
	}
	out := make([]domain.Checkpoint, len(cs.cache))
	copy(out, cs.cache)
	return out
}

// Reset xóa sạch tệp checkpoint và cache. Chỉ dùng khi tạo tiểu thuyết mới.
// Xóa tệp trước rồi mới xóa bộ nhớ: nếu xóa tệp thất bại thì giữ lại cache và
// seqGen, tránh lệch trạng thái giữa bộ nhớ và đĩa.
func (cs *CheckpointStore) Reset() error {
	cs.io.mu.Lock()
	defer cs.io.mu.Unlock()
	if err := cs.io.RemoveFileUnlocked(checkpointsFile); err != nil {
		return err
	}
	cs.seqGen.Store(0)
	cs.cache = nil
	cs.loadErr = nil
	return nil
}

// InitError trả về lỗi khi tải ảnh checkpoint lúc khởi tạo. Store.Init phải kiểm tra
// nó trước, để jsonl hỏng không bị hiểu thành “không có checkpoint”.
func (cs *CheckpointStore) InitError() error {
	cs.io.mu.RLock()
	defer cs.io.mu.RUnlock()
	return cs.loadErr
}

// readCheckpointsFile phân tích jsonl nghiêm ngặt; phần cuối bị cắt cũng là lỗi lưu
// trữ mà người dùng cần nhìn thấy.
func readCheckpointsFile(path string) ([]domain.Checkpoint, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var result []domain.Checkpoint
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var cp domain.Checkpoint
		if err := json.Unmarshal(raw, &cp); err != nil {
			return nil, fmt.Errorf("phân tích %s dòng %d: %w", checkpointsFile, lineNo, err)
		}
		result = append(result, cp)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("quét %s: %w", checkpointsFile, err)
	}
	return result, nil
}
