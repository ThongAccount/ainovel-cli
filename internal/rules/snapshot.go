package rules

import (
	"fmt"
	"maps"
	"strings"
)

// Snapshot là ảnh chụp các quy tắc người dùng đã chuẩn hóa cho tác phẩm hiện tại
// (meta/user_rules.json).
//
// Đây là nguồn sự thật duy nhất khi chạy: được hợp nhất từ các nguồn đã chuẩn hóa khi
// tạo/import/làm mới tác phẩm; sau đó novel_context chèn và commit_chapter kiểm tra
// đều chỉ đọc bản này, không đọc lại tệp rules (tránh trôi lệch và hai bên đọc lệch nhau).
//
// Chỉ Structured + Preferences được chèn cho mô hình (xem Payload); Version / Status /
// Sources / Uncertain là siêu dữ liệu vận hành và chẩn đoán, không đi vào
// working_memory.user_rules.
type Snapshot struct {
	Version     int        `json:"version"`
	Status      Status     `json:"status"`
	Structured  Structured `json:"structured"`
	Preferences string     `json:"preferences"`
	Sources     []string   `json:"sources"`
	Uncertain   []string   `json:"uncertain"`
}

// Status đánh dấu việc chuẩn hóa ảnh chụp có hoàn tất thành công hay không.
type Status string

const (
	// StatusReady mọi nguồn đều chuẩn hóa thành công.
	StatusReady Status = "ready"
	// StatusDegraded ít nhất một nguồn chuẩn hóa thất bại và đã hạ cấp thành raw
	// preferences (xem thêm Uncertain / nhật ký).
	StatusDegraded Status = "degraded"
)

// SnapshotVersion là phiên bản schema hiện tại của ảnh chụp, thuận tiện cho việc
// di chuyển trong tương lai.
// v2: chapter_words rời khỏi structured (số chữ là ràng buộc mềm theo ngữ nghĩa,
// đi qua preferences).
// Ảnh chụp v1 vẫn tải tương thích trực tiếp: trường không xác định bị bỏ qua khi
// giải mã; lần lưu chồng tiếp theo sẽ tự hội tụ về v2. Cố ý không "dựng lại khi
// phiên bản không khớp" — làm vậy sẽ mất các quy tắc không thể tái tạo mà
// AddRuntimeRule thêm trong lúc chạy.
const SnapshotVersion = 2

// Candidate là kết quả ứng viên sau khi chuẩn hóa một nguồn.
//
// Các nguồn được sắp theo ưu tiên thấp→cao rồi giao cho BuildSnapshot hợp nhất
// một cách tất định. LLM chỉ chịu trách nhiệm biến ngôn ngữ tự nhiên của một nguồn
// thành Structured/Preferences ứng viên; thứ tự ưu tiên và việc ghi đè trường do
// BuildSnapshot (Go) phán quyết.
type Candidate struct {
	Source      string     // nhãn nguồn dễ đọc, đi vào Snapshot.Sources (ví dụ system_defaults / startup_prompt / global:my.md)
	Structured  Structured // các trường có cấu trúc ứng viên của nguồn này
	Preferences string     // nội dung tùy chọn bằng ngôn ngữ tự nhiên của nguồn này
	Uncertain   []string   // các mục cố ý không nâng lên structured + lý do (chẩn đoán)
	Degraded    bool       // nguồn này chuẩn hóa thất bại, đã hạ cấp thành raw preferences
}

// Payload trả về dạng dữ liệu được chèn vào working_memory.user_rules: chỉ lộ
// structured + preferences.
// Ngay cả khi cả hai đều rỗng, vẫn trả về cấu trúc ổn định để tránh LLM gặp
// user_rules=null và rơi vào nhánh ngoại lệ.
func (s Snapshot) Payload() map[string]any {
	return map[string]any{
		"structured":  s.Structured,
		"preferences": s.Preferences,
	}
}

// BuildSnapshot hợp nhất tất định các ứng viên đã sắp theo ưu tiên (thấp→cao)
// thành một ảnh chụp.
//
// Quy tắc hợp nhất (hoàn toàn tất định phía Go, không giao cho LLM):
//   - structured: ghi đè theo từng trường, nguồn ưu tiên cao ghi đè nguồn ưu tiên thấp;
//     fatigue_words cộng dồn theo từ
//   - preferences: không ghi đè, nối theo thứ tự nguồn (ưu tiên cao ở sau), kèm tiêu đề nguồn
//   - giá trị rỗng/0 được coi là thiếu trường, không ghi đè giá trị đã có
//     (sanitizeStructured)
//   - bất kỳ nguồn nào Degraded → status của ảnh chụp là degraded
func BuildSnapshot(cands []Candidate) Snapshot {
	snap := Snapshot{
		Version: SnapshotVersion,
		Status:  StatusReady,
		Sources: make([]string, 0, len(cands)),
	}
	var prefs []string
	for _, c := range cands {
		s := sanitizeStructured(c.Structured)
		if s.Genre != "" {
			snap.Structured.Genre = s.Genre
		}
		if len(s.ForbiddenChars) > 0 {
			snap.Structured.ForbiddenChars = s.ForbiddenChars
		}
		if len(s.ForbiddenPhrases) > 0 {
			snap.Structured.ForbiddenPhrases = s.ForbiddenPhrases
		}
		if len(s.FatigueWords) > 0 {
			snap.Structured.FatigueWords = mergeFatigueWords(snap.Structured.FatigueWords, s.FatigueWords)
		}

		if p := strings.TrimSpace(c.Preferences); p != "" {
			if src := strings.TrimSpace(c.Source); src != "" {
				prefs = append(prefs, fmt.Sprintf("## [%s]\n\n%s", src, p))
			} else {
				prefs = append(prefs, p)
			}
		}
		if src := strings.TrimSpace(c.Source); src != "" {
			snap.Sources = append(snap.Sources, src)
		}
		snap.Uncertain = append(snap.Uncertain, c.Uncertain...)
		if c.Degraded {
			snap.Status = StatusDegraded
		}
	}
	snap.Preferences = strings.Join(prefs, "\n\n")
	return snap
}

// OverlaySnapshot chồng một ứng viên ưu tiên cao lên ảnh chụp hiện có (ứng viên thắng).
//
// Dùng cho hành động rules của Arbiter trong lúc chạy: không chuẩn hóa lại mọi nguồn,
// chỉ ghi đè quy tắc mới vào ảnh chụp hiện tại — structured ghi đè theo trường,
// preferences thêm một đoạn, sources/uncertain tích lũy và trạng thái hạ cấp lan truyền.
func OverlaySnapshot(base Snapshot, cand Candidate) Snapshot {
	out := base
	out.Version = SnapshotVersion
	s := sanitizeStructured(cand.Structured)
	if s.Genre != "" {
		out.Structured.Genre = s.Genre
	}
	if len(s.ForbiddenChars) > 0 {
		out.Structured.ForbiddenChars = s.ForbiddenChars
	}
	if len(s.ForbiddenPhrases) > 0 {
		out.Structured.ForbiddenPhrases = s.ForbiddenPhrases
	}
	if len(s.FatigueWords) > 0 {
		out.Structured.FatigueWords = mergeFatigueWords(cloneFatigue(out.Structured.FatigueWords), s.FatigueWords)
	}
	if p := strings.TrimSpace(cand.Preferences); p != "" {
		section := p
		if src := strings.TrimSpace(cand.Source); src != "" {
			section = fmt.Sprintf("## [%s]\n\n%s", src, p)
		}
		if strings.TrimSpace(out.Preferences) == "" {
			out.Preferences = section
		} else {
			out.Preferences = out.Preferences + "\n\n" + section
		}
	}
	if src := strings.TrimSpace(cand.Source); src != "" {
		out.Sources = append(append([]string{}, out.Sources...), src)
	}
	if len(cand.Uncertain) > 0 {
		out.Uncertain = append(append([]string{}, out.Uncertain...), cand.Uncertain...)
	}
	if cand.Degraded {
		out.Status = StatusDegraded
	}
	return out
}

// mergeFatigueWords cộng dồn ngưỡng từ mệt mỏi theo từng từ; src ghi đè ngưỡng
// của cùng từ trong dst (ưu tiên nguồn gần hơn).
// Nhờ đó người dùng chỉ cần thêm ít từ mệt mỏi, không phải liệt kê lại toàn bộ
// cơ sở tích hợp.
func mergeFatigueWords(dst, src map[string]int) map[string]int {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]int, len(src))
	}
	maps.Copy(dst, src)
	return dst
}

func cloneFatigue(m map[string]int) map[string]int {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]int, len(m))
	maps.Copy(out, m)
	return out
}

// SystemDefaults là cơ sở cơ chế tích hợp trong mã (nguồn ưu tiên thấp nhất),
// không đi qua chuẩn hóa bằng LLM.
//
// Các ngưỡng được chuyển từ front matter của assets/rules/default.md cũ. Lý do đặt
// ngưỡng cũng được giữ lại: nhóm từ mệt mỏi phía sau (像一/沉默了/没有说话/X息) đến từ
// dữ liệu thực nghiệm của lần chạy dài 196 chương — sau khi các câu khuôn mẫu AI
// truyền thống ở nhóm trước bị loại bỏ, model chuyển sang dùng các "từ nhịp" này
// trung bình 5-7 lần mỗi chương, nên ngưỡng được nới rộng để dung sai mức dùng
// thông thường.
func SystemDefaults() Candidate {
	return Candidate{
		Source: "system_defaults",
		Structured: Structured{
			// Câu khuôn mẫu AI có độ dài cố định; checker so khớp chuỗi con theo ký tự,
			// còn mẫu có biến (不是X而是Y) thuộc tầng ngữ nghĩa.
			ForbiddenPhrases: []string{"某种程度上", "值得注意的是", "不知为何", "五味杂陈"},
			FatigueWords: map[string]int{
				"不禁": 1, "竟然": 1, "仿佛": 2, "此外": 1, "然而": 2,
				"一丝": 2, "一抹": 2, "一缕": 2, "宛如": 1, "不由得": 1,
				"像一": 3, "沉默了": 2, "没有说话": 2, "几息": 3, "一息": 3, "数息": 2,
			},
		},
	}
}

// sanitizeStructured thực hiện quy tắc "giá trị rỗng/0 = thiếu trường": bộ chuẩn hóa
// có thể trả về giá trị giữ chỗ như genre:"" (đo trong bản thử nghiệm), phải coi là
// chưa khai báo để tránh làm nhiễu việc hợp nhất và kiểm tra cơ chế.
func sanitizeStructured(s Structured) Structured {
	out := Structured{}
	if g := strings.TrimSpace(s.Genre); g != "" {
		out.Genre = g
	}
	out.ForbiddenChars = nonEmptyStrings(s.ForbiddenChars)
	out.ForbiddenPhrases = nonEmptyStrings(s.ForbiddenPhrases)
	out.FatigueWords = sanitizeFatigueWords(s.FatigueWords)
	return out
}

func nonEmptyStrings(in []string) []string {
	var out []string
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func sanitizeFatigueWords(m map[string]int) map[string]int {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]int, len(m))
	for w, n := range m {
		if w = strings.TrimSpace(w); w == "" || n <= 0 {
			continue
		}
		out[w] = n
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
