package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// Store là gốc tổ hợp của quản lý trạng thái, giữ toàn bộ kho con.
type Store struct {
	dir string
	ios []*IO

	Progress       *ProgressStore
	Book           *BookStore
	Outline        *OutlineStore
	Drafts         *DraftStore
	Summaries      *SummaryStore
	RunMeta        *RunMetaStore
	UserRules      *UserRulesStore
	Signals        *SignalStore
	Runtime        *RuntimeStore
	Characters     *CharacterStore
	Cast           *CastStore
	World          *WorldStore
	Checkpoints    *CheckpointStore
	Sessions       *SessionStore
	Usage          *UsageStore
	Simulation     *SimulationStore
	Decisions      *DecisionStore
	ChapterRecords *ChapterRecordStore
	Revisions      *RevisionStore

	crossMu sync.Mutex // tuần tự hóa điều phối xuyên miền; không có nghĩa nhiều tệp có tính nguyên tử giao dịch
}

const (
	LegacyProjectFormatVersion  = 1
	CurrentProjectFormatVersion = 2
	projectFormatPath           = "meta/format.json"
)

type projectFormat struct {
	Version int `json:"version"`
}

// NewStore tạo bộ quản lý trạng thái, dir là thư mục gốc kết quả của tiểu thuyết.
func NewStore(dir string) *Store {
	var ios []*IO
	// mk ghi lại IO của từng kho con: chúng giữ khóa riêng (không chặn nhau), nhưng ngôn
	// ngữ thì thống nhất cho cả tác phẩm, nên phải đăng ký tập trung thì mới đặt được
	// một lần cho tất cả mà không bỏ sót kho thêm về sau.
	mk := func() *IO { x := newIO(dir); ios = append(ios, x); return x }
	io := mk()
	outline := NewOutlineStore(io)
	s := &Store{
		dir:            dir,
		Progress:       NewProgressStore(mk()),
		Book:           NewBookStore(mk()),
		Outline:        outline,
		Drafts:         NewDraftStore(mk()),
		Summaries:      NewSummaryStore(mk(), outline),
		RunMeta:        NewRunMetaStore(mk()),
		UserRules:      NewUserRulesStore(mk()),
		Signals:        NewSignalStore(mk()),
		Runtime:        NewRuntimeStore(mk()),
		Characters:     NewCharacterStore(mk(), outline),
		Cast:           NewCastStore(mk()),
		World:          NewWorldStore(mk()),
		Checkpoints:    NewCheckpointStore(io),
		Sessions:       NewSessionStore(mk()),
		Usage:          NewUsageStore(mk()),
		Simulation:     NewSimulationStore(mk()),
		Decisions:      NewDecisionStore(mk()),
		ChapterRecords: NewChapterRecordStore(mk()),
		Revisions:      NewRevisionStore(mk()),
	}
	s.ios = ios
	return s
}

// SetLanguage đặt ngôn ngữ tác phẩm ("vi" / "zh"), ảnh hưởng nhãn của mọi bản Markdown
// dẫn xuất.
// Gọi một lần lúc khởi động là đủ; nếu không gọi thì dùng mặc định tiếng Việt của
// fork này.
//
// Đồng thời đồng bộ ngôn ngữ ở cấp tiến trình của i18n: các package không có handle
// store (internal/arbiter, internal/llmcontract…) cũng cần sinh văn bản hướng model
// theo ngôn ngữ của tác phẩm.
func (s *Store) SetLanguage(lang string) {
	for _, x := range s.ios {
		x.SetLanguage(lang)
	}
	i18n.SetActive(lang)
}

// Language trả về dạng chuẩn của ngôn ngữ tác phẩm hiện tại ("vi" / "zh").
// Đây là nguồn sự thật duy nhất cho mọi văn bản hướng model khi chọn ngôn ngữ:
// internal/tools và internal/host/exp đều đọc từ đây, để không ai phải tự duy trì một
// bộ phán đoán ngôn ngữ riêng.
func (s *Store) Language() string {
	if len(s.ios) == 0 {
		return string(i18n.LangVI)
	}
	return s.ios[0].Language()
}

// Dir trả về thư mục gốc kết quả.
func (s *Store) Dir() string { return s.dir }

// LoadProjectFormatVersion trả về phiên bản định dạng dữ liệu của thư mục tác phẩm.
// Tác phẩm cũ không có tệp phiên bản nên được coi là v1 và được nâng cấp thống nhất lúc
// khởi động; mã nghiệp vụ không cần giữ nhánh cho định dạng cũ.
func (s *Store) LoadProjectFormatVersion() (int, error) {
	var format projectFormat
	if err := s.Progress.io.ReadJSON(projectFormatPath, &format); err != nil {
		if os.IsNotExist(err) {
			return LegacyProjectFormatVersion, nil
		}
		return 0, err
	}
	if format.Version <= 0 {
		return 0, fmt.Errorf("phiên bản định dạng dự án không hợp lệ: %d", format.Version)
	}
	return format.Version, nil
}

// SaveProjectFormatVersion cập nhật nguyên tử phiên bản định dạng dự án sau khi một lần di chuyển hoàn tất.
func (s *Store) SaveProjectFormatVersion(version int) error {
	if version <= 0 {
		return fmt.Errorf("phiên bản định dạng dự án phải lớn hơn 0: %d", version)
	}
	return s.Progress.io.WriteJSON(projectFormatPath, projectFormat{Version: version})
}

// CheckConsistency kiểm tra nông lớp sự thật một lần, dùng để sinh cảnh báo lúc khởi
// động / khôi phục.
// Hoàn toàn chỉ-đọc: không sửa dữ liệu, chỉ trả về mô tả vấn đề dễ đọc; bên gọi quyết
// định hiển thị thế nào (log / UI).
// Để tránh chi phí IO quét cả thư mục, chỉ kiểm tra các điểm then chốt của Progress:
//   - chương hoàn thành gần nhất phải có bản chính thức trong chapters/
//   - ở chế độ Layered, Volume/Arc hiện tại phải tìm được trong layered_outline
func (s *Store) CheckConsistency() []string {
	var warnings []string
	progress, err := s.Progress.Load()
	if err != nil {
		return append(warnings, fmt.Sprintf("đọc progress thất bại: %v", err))
	}
	if progress == nil {
		return warnings
	}
	if n := len(progress.CompletedChapters); n > 0 {
		lastCh := progress.CompletedChapters[n-1]
		if text, err := s.Drafts.LoadChapterText(lastCh); err != nil {
			warnings = append(warnings, fmt.Sprintf("đọc bản chính thức chương %d thất bại: %v", lastCh, err))
		} else if text == "" {
			warnings = append(warnings, fmt.Sprintf("progress đánh dấu chương %d đã hoàn thành, nhưng chapters/%02d.md không tồn tại hoặc rỗng", lastCh, lastCh))
		}
	}
	if progress.Layered && progress.CurrentVolume > 0 && progress.CurrentArc > 0 {
		volumes, err := s.Outline.LoadLayeredOutline()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("đọc đề cương phân tầng thất bại: %v", err))
		} else if len(volumes) > 0 {
			found := false
			for _, v := range volumes {
				if v.Index != progress.CurrentVolume {
					continue
				}
				for _, a := range v.Arcs {
					if a.Index == progress.CurrentArc {
						found = true
						break
					}
				}
				break
			}
			if !found {
				warnings = append(warnings, fmt.Sprintf("progress hiện ở V%d A%d không tìm được mục tương ứng trong đề cương phân tầng", progress.CurrentVolume, progress.CurrentArc))
			}
		}
	}
	return warnings
}

// FoundationMissing trả về danh sách thông tin tác phẩm và thiết lập nền còn thiếu ở
// giai đoạn quy hoạch ban đầu, theo thứ tự ổn định.
// Chế độ truyện dài (đã có layered_outline) còn đòi hỏi compass. Lỗi đọc phải được
// trả về nguyên trạng, không được coi artifact hỏng hoặc không đọc được là “chưa tạo”,
// nếu không bên gọi có thể đè lên dữ liệu thật.
func (s *Store) FoundationMissing() ([]string, error) {
	var missing []string
	book, err := s.Book.Load()
	if err != nil {
		return nil, fmt.Errorf("load book metadata: %w", err)
	}
	if book == nil {
		missing = append(missing, "book")
	}
	premise, err := s.Outline.LoadPremise()
	if err != nil {
		return nil, fmt.Errorf("load premise: %w", err)
	}
	if premise == "" {
		missing = append(missing, "premise")
	}
	outline, err := s.Outline.LoadOutline()
	if err != nil {
		return nil, fmt.Errorf("load outline: %w", err)
	}
	if len(outline) == 0 {
		missing = append(missing, "outline")
	}
	characters, err := s.Characters.Load()
	if err != nil {
		return nil, fmt.Errorf("load characters: %w", err)
	}
	if len(characters) == 0 {
		missing = append(missing, "characters")
	}
	rules, err := s.World.LoadWorldRules()
	if err != nil {
		return nil, fmt.Errorf("load world rules: %w", err)
	}
	if len(rules) == 0 {
		missing = append(missing, "world_rules")
	}
	layered, err := s.Outline.LoadLayeredOutline()
	if err != nil {
		return nil, fmt.Errorf("load layered outline: %w", err)
	}
	if len(layered) > 0 {
		compass, err := s.Outline.LoadCompass()
		if err != nil {
			return nil, fmt.Errorf("load compass: %w", err)
		}
		if compass == nil {
			missing = append(missing, "compass")
		}
	}
	// Tác phẩm mới chỉ được đi từ quy hoạch sang viết sau khi model đã thẩm duyệt
	// ngữ nghĩa tường minh các artifact đã ghi xuống.
	// PhaseWriting/Complete đại diện cho tác phẩm cũ hoặc tác phẩm mới đã thẩm duyệt,
	// để giữ tương thích với dự án lịch sử; bản thân việc thẩm duyệt là một hành động
	// chứ không phải một tệp thiếu, nên chỉ được thêm vào khi các artifact khác đã đủ.
	if len(missing) == 0 {
		progress, err := s.Progress.Load()
		if err != nil {
			return nil, fmt.Errorf("load progress: %w", err)
		}
		if progress == nil || (progress.Phase != domain.PhaseWriting && progress.Phase != domain.PhaseComplete) {
			missing = append(missing, "foundation_audit")
		}
	}
	return missing, nil
}

// FoundationFingerprint trả về dấu vân tay nội dung của các artifact thiết lập nền hiện
// tại. Kiến trúc sư phải trả lại nguyên văn giá trị đọc được từ novel_context cho công
// cụ thẩm duyệt, để kết luận chắc chắn nói về phiên bản đã thực sự ghi xuống chứ không
// phải nội dung trong phiên làm việc chưa lưu hoặc đã cũ.
func (s *Store) FoundationFingerprint() (string, error) {
	files := []string{"meta/book.json", "premise.md", "outline.json", "characters.json", "world_rules.json"}
	layered, err := s.Outline.LoadLayeredOutline()
	if err != nil {
		return "", fmt.Errorf("load layered outline: %w", err)
	}
	if len(layered) > 0 {
		files = append(files, "layered_outline.json", "meta/compass.json")
	}

	h := sha256.New()
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(s.dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", fmt.Errorf("đọc %s: %w", rel, err)
		}
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Init tạo cấu trúc thư mục con cần thiết.
func (s *Store) Init() error {
	if err := s.Checkpoints.InitError(); err != nil {
		return fmt.Errorf("tải checkpoint: %w", err)
	}
	return s.Progress.io.EnsureDirs([]string{
		"chapters", "summaries", "drafts", "reviews", "meta", "meta/chapter_records", "meta/runtime", "meta/runtime/tasks", "meta/sessions", "meta/sessions/agents",
	})
}

// ── Các phương thức điều phối xuyên miền ──

// ExpandArc hiệu chỉnh cung khung rồi triển khai thành các chương chi tiết (Outline + Progress liên kết).
func (s *Store) ExpandArc(volumeIdx, arcIdx int, expansion domain.ArcExpansion) error {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	s.Outline.io.mu.Lock()
	defer s.Outline.io.mu.Unlock()

	volumes, err := s.Outline.expandArcUnlocked(volumeIdx, arcIdx, expansion)
	if err != nil {
		return err
	}

	s.Progress.io.mu.Lock()
	defer s.Progress.io.mu.Unlock()

	p, err := s.Progress.loadUnlocked()
	if err != nil {
		return err
	}
	if p == nil {
		p = &domain.Progress{}
	}
	p.TotalChapters = domain.EstimatedChapterCapacity(volumes)
	return s.Progress.saveUnlocked(p)
}

// AppendVolume thêm tập mới vào cuối đề cương phân tầng (Outline + Progress liên kết).
func (s *Store) AppendVolume(vol domain.VolumeOutline) error {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	s.Outline.io.mu.Lock()
	defer s.Outline.io.mu.Unlock()

	volumes, err := s.Outline.appendVolumeUnlocked(vol)
	if err != nil {
		return err
	}

	s.Progress.io.mu.Lock()
	defer s.Progress.io.mu.Unlock()

	p, err := s.Progress.loadUnlocked()
	if err != nil {
		return err
	}
	if p == nil {
		p = &domain.Progress{}
	}
	p.TotalChapters = domain.EstimatedChapterCapacity(volumes)
	return s.Progress.saveUnlocked(p)
}

// ReviseOutline thay phần đuôi của kế hoạch chưa xảy ra, tính từ fromChapter.
// Với đề cương phẳng là thay đuôi của cả tác phẩm; với đề cương phân tầng chỉ thay đuôi
// của cung chứa chương đích. Định nghĩa này giúp cùng một tải trọng phát lại vẫn cho
// cùng kết quả, đồng thời tránh phải liệt kê các thao tác kiểu JSON Patch hay
// insert/delete.
func (s *Store) ReviseOutline(fromChapter int, replacement []domain.OutlineEntry) (int, error) {
	if fromChapter <= 0 {
		return 0, fmt.Errorf("from_chapter phải lớn hơn 0: %w", errs.ErrToolArgs)
	}

	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	s.Outline.io.mu.Lock()
	defer s.Outline.io.mu.Unlock()
	s.Progress.io.mu.Lock()
	defer s.Progress.io.mu.Unlock()

	p, err := s.Progress.loadUnlocked()
	if err != nil {
		return 0, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	if p == nil {
		return 0, fmt.Errorf("progress chưa được khởi tạo: %w", errs.ErrToolPrecondition)
	}
	if p.Phase == domain.PhaseComplete {
		return 0, fmt.Errorf("tác phẩm đã hoàn thành, không được sửa đề cương: %w", errs.ErrToolPrecondition)
	}
	protected := p.InProgressChapter
	if latest := p.LatestCompleted(); latest > protected {
		protected = latest
	}
	if fromChapter <= protected {
		// Chỉ báo “không được sửa” sẽ dồn bên gọi vào ngõ cụt: thực nghiệm cho thấy
		// Kiến trúc sư thử liên tiếp 4 lần (from=21/22/13, rồi lùi về
		// save_foundation(outline)) đều bị từ chối và quay vòng tới mức cắt mạch. Lỗi
		// phải nói rõ ai phụ trách làm lại, nếu không Kiến trúc sư sẽ tiếp tục tìm một
		// lối thoát không tồn tại trong bộ công cụ của mình.
		return 0, fmt.Errorf(
			"Chương %d đã hoàn thành hoặc đang được viết; revise_outline chỉ sửa được các chương chưa xảy ra, phải bắt đầu sau chương %d. "+
				"Các chương đã viết nằm trong pending_rewrites không thuộc phạm vi sửa đề cương: việc làm lại do Người viết thực hiện theo hàng đợi; "+
				"Nếu Kiến trúc sư không có chương tương lai nào cần sửa, hãy gọi resolve_outline_feedback để xác nhận kế hoạch hiện tại vẫn còn hiệu lực rồi kết thúc: %w",
			fromChapter, protected, errs.ErrToolPrecondition)
	}

	if p.Layered {
		volumes, err := s.Outline.reviseLayeredTailUnlocked(fromChapter, replacement)
		if err != nil {
			return 0, err
		}
		p.TotalChapters = domain.EstimatedChapterCapacity(volumes)
		if err := s.Progress.saveUnlocked(p); err != nil {
			return 0, fmt.Errorf("save progress: %w: %w", errs.ErrStoreWrite, err)
		}
		return p.TotalChapters, nil
	}

	outline, err := s.Outline.reviseFlatTailUnlocked(fromChapter, replacement)
	if err != nil {
		return 0, err
	}
	p.TotalChapters = len(outline)
	if err := s.Progress.saveUnlocked(p); err != nil {
		return 0, fmt.Errorf("save progress: %w: %w", errs.ErrStoreWrite, err)
	}
	return p.TotalChapters, nil
}

// ClearHandledSteer xóa PendingSteer và đặt lại trạng thái FlowSteering của bản cũ.
// Hai tệp không thể tạo thành một giao dịch hệ thống tệp, nên phải ghi Progress (có
// thể lặp lại) trước rồi mới xóa ý định khôi phục; nếu bước nào thất bại thì PendingSteer
// vẫn còn, và lần Resume sau có thể phát lại an toàn.
func (s *Store) ClearHandledSteer() error {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	s.RunMeta.io.mu.Lock()
	defer s.RunMeta.io.mu.Unlock()
	s.Progress.io.mu.Lock()
	defer s.Progress.io.mu.Unlock()

	meta, err := s.RunMeta.loadUnlocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	p, err := s.Progress.loadUnlocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if p != nil && p.Flow == domain.FlowSteering {
		if err := domain.ValidateFlowTransition(p.Flow, domain.FlowWriting); err != nil {
			return err
		}
		p.Flow = domain.FlowWriting
		if err := s.Progress.saveUnlocked(p); err != nil {
			return err
		}
	}
	if meta != nil && meta.PendingSteer != "" {
		meta.PendingSteer = ""
		if err := s.RunMeta.saveUnlocked(*meta); err != nil {
			return err
		}
	}
	return nil
}
