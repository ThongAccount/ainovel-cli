package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

func setupLayered(t *testing.T, volumes []domain.VolumeOutline) *Store {
	t.Helper()
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init(0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := s.Outline.SaveLayeredOutline(volumes); err != nil {
		t.Fatalf("SaveLayeredOutline: %v", err)
	}
	if err := s.Progress.SetLayered(true); err != nil {
		t.Fatalf("SetLayered: %v", err)
	}
	return s
}

func TestSaveLayeredOutlineRebuildsFlatProjection(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Outline.SaveOutline([]domain.OutlineEntry{{Chapter: 1, Title: "tiêu đề cũ"}}); err != nil {
		t.Fatalf("SaveOutline: %v", err)
	}

	volumes := []domain.VolumeOutline{{
		Index: 1, Title: "Tập một", Theme: "khởi hành",
		Arcs: []domain.ArcOutline{{
			Index: 1, Title: "Cung một", Goal: "bước vào thế giới mới",
			Chapters: []domain.OutlineEntry{
				{Chapter: 99, Title: "Mới một", CoreEvent: "khởi hành", Hook: "phát hiện"},
				{Chapter: 100, Title: "Mới hai", CoreEvent: "đào sâu", Hook: "khủng hoảng"},
			},
		}},
	}}
	if err := s.Outline.SaveLayeredOutline(volumes); err != nil {
		t.Fatalf("SaveLayeredOutline: %v", err)
	}

	flat, err := s.Outline.LoadOutline()
	if err != nil {
		t.Fatalf("LoadOutline: %v", err)
	}
	if len(flat) != 2 || flat[0].Chapter != 1 || flat[0].Title != "Mới một" || flat[1].Chapter != 2 || flat[1].Title != "Mới hai" {
		t.Fatalf("flat projection = %+v", flat)
	}
	markdown, err := os.ReadFile(filepath.Join(s.Dir(), "outline.md"))
	if err != nil {
		t.Fatalf("read outline.md: %v", err)
	}
	if !strings.Contains(string(markdown), "Mới một") || strings.Contains(string(markdown), "tiêu đề cũ") {
		t.Fatalf("outline.md chưa được dựng lại đồng bộ:\n%s", markdown)
	}
}

func TestCheckArcBoundaryNeedsNewVolume(t *testing.T) {
	// Chỉ 1 tập 1 cung 1 chương, và không phải Final → phải kích hoạt NeedsNewVolume
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "Tập một", Theme: "khởi đầu",
		Arcs: []domain.ArcOutline{{
			Index: 1, Title: "Cung đầu", Goal: "mục tiêu",
			Chapters: []domain.OutlineEntry{{Title: "Chương một", CoreEvent: "khai cuộc", Hook: "tiếp tục"}},
		}},
	}})

	b, err := s.Outline.CheckArcBoundary(1) // Chương 1 = chương cuối của cung/tập
	if err != nil {
		t.Fatalf("CheckArcBoundary: %v", err)
	}
	if b == nil {
		t.Fatal("expected boundary, got nil")
	}
	if !b.IsArcEnd || !b.IsVolumeEnd {
		t.Fatalf("expected arc+volume end, got arc=%v vol=%v", b.IsArcEnd, b.IsVolumeEnd)
	}
	if !b.NeedsNewVolume {
		t.Fatal("expected NeedsNewVolume=true")
	}
	if b.NextVolume != 0 || b.NextArc != 0 {
		t.Fatalf("expected no next, got vol=%d arc=%d", b.NextVolume, b.NextArc)
	}
}

func TestCheckArcBoundaryLastVolumeRequiresDecision(t *testing.T) {
	// Chương cuối của tập đơn → kích hoạt NeedsNewVolume, để bộ định tuyến bắt Kiến trúc sư
	// chọn một trong hai: append_volume để viết tiếp / complete_book để chốt.
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "Tập duy nhất", Theme: "chủ đề",
		Arcs: []domain.ArcOutline{{
			Index: 1, Title: "Cung duy nhất", Goal: "chốt lại",
			Chapters: []domain.OutlineEntry{{Title: "Chương cuối", CoreEvent: "kết cục", Hook: "không"}},
		}},
	}})

	b, err := s.Outline.CheckArcBoundary(1)
	if err != nil {
		t.Fatalf("CheckArcBoundary: %v", err)
	}
	if !b.NeedsNewVolume {
		t.Fatal("expected NeedsNewVolume=true at last expanded chapter")
	}
	if b.HasNextArc() {
		t.Fatal("expected no next arc")
	}
}

func TestCheckArcBoundaryNextArcInSameVolume(t *testing.T) {
	// 2 cung: khi cung 1 kết thúc phải trỏ tới cung 2, không kích hoạt NeedsNewVolume
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "Tập một", Theme: "khởi đầu",
		Arcs: []domain.ArcOutline{
			{Index: 1, Title: "Cung đầu", Goal: "mục tiêu", Chapters: []domain.OutlineEntry{{Title: "Chương một", CoreEvent: "sự kiện", Hook: "điểm móc"}}},
			{Index: 2, Title: "Cung thứ hai", Goal: "mục tiêu 2", EstimatedChapters: 10},
		},
	}})

	b, err := s.Outline.CheckArcBoundary(1)
	if err != nil {
		t.Fatalf("CheckArcBoundary: %v", err)
	}
	if !b.IsArcEnd {
		t.Fatal("expected arc end")
	}
	if b.IsVolumeEnd {
		t.Fatal("expected not volume end (second arc exists)")
	}
	if b.NeedsNewVolume {
		t.Fatal("expected NeedsNewVolume=false")
	}
	if b.NextVolume != 1 || b.NextArc != 2 {
		t.Fatalf("expected next vol=1 arc=2, got vol=%d arc=%d", b.NextVolume, b.NextArc)
	}
	if !b.NeedsExpansion {
		t.Fatal("expected NeedsExpansion=true for skeleton arc")
	}
}

func TestCheckArcBoundaryReportsExactArcSpan(t *testing.T) {
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1,
		Arcs: []domain.ArcOutline{
			{Index: 1, Chapters: []domain.OutlineEntry{{Title: "một"}, {Title: "hai"}}},
			{Index: 2, Chapters: []domain.OutlineEntry{{Title: "ba"}, {Title: "bốn"}, {Title: "năm"}}},
		},
	}})

	b, err := s.Outline.CheckArcBoundary(5)
	if err != nil {
		t.Fatalf("CheckArcBoundary: %v", err)
	}
	if b == nil || !b.IsArcEnd || b.StartChapter != 3 || b.EndChapter != 5 {
		t.Fatalf("unexpected arc span: %+v", b)
	}
}

func TestExpandArcCalibratesUnwrittenPlan(t *testing.T) {
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "Tập một", Theme: "khởi đầu",
		Arcs: []domain.ArcOutline{
			{Index: 1, Title: "Cung cũ", Goal: "gây ra sự chia rẽ ngoài dự kiến", Chapters: []domain.OutlineEntry{{Title: "Chia rẽ", CoreEvent: "đồng đội rời đi", Hook: "không rõ tung tích"}}},
			{Index: 2, Title: "Khung gốc", Goal: "đi cùng theo kế hoạch cũ", EstimatedChapters: 8},
		},
	}})

	expansion := domain.ArcExpansion{
		Title: "Đuổi theo hai lối",
		Goal:  "thừa nhận việc chia rẽ đã xảy ra, để hai tuyến hành động tiến sát cùng một sự thật",
		Chapters: []domain.OutlineEntry{
			{Title: "Hai tấm bản đồ", CoreEvent: "hai đội xuất phát từ hai manh mối khác nhau", Hook: "manh mối cùng chỉ về một địa điểm"},
			{Title: "Tiếng vọng qua tường", CoreEvent: "hai bên ảnh hưởng lựa chọn của nhau từ xa", Hook: "cái giá của cuộc gặp lại hiện ra"},
		},
	}
	if err := s.ExpandArc(1, 2, expansion); err != nil {
		t.Fatalf("ExpandArc: %v", err)
	}

	volumes, err := s.Outline.LoadLayeredOutline()
	if err != nil {
		t.Fatalf("LoadLayeredOutline: %v", err)
	}
	got := volumes[0].Arcs[1]
	if got.Title != expansion.Title || got.Goal != expansion.Goal {
		t.Fatalf("expected calibrated title/goal, got title=%q goal=%q", got.Title, got.Goal)
	}
	if got.EstimatedChapters != 0 || len(got.Chapters) != 2 {
		t.Fatalf("expected expanded arc, got estimated=%d chapters=%d", got.EstimatedChapters, len(got.Chapters))
	}
	flat, err := s.Outline.LoadOutline()
	if err != nil {
		t.Fatalf("LoadOutline: %v", err)
	}
	if len(flat) != 3 || flat[1].Chapter != 2 || flat[2].Chapter != 3 {
		t.Fatalf("expected continuous flattened outline, got %+v", flat)
	}
	progress, err := s.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if progress.TotalChapters != 3 {
		t.Fatalf("expected total chapters 3, got %d", progress.TotalChapters)
	}

	if err := s.ExpandArc(1, 2, expansion); err != nil {
		t.Fatalf("same expansion must be idempotent: %v", err)
	}
	// Mô phỏng lần trước mới chỉ ghi xong layered JSON, còn flat outline dẫn xuất và Progress
	// chưa được bổ sung.
	if err := os.Remove(filepath.Join(s.Dir(), "outline.json")); err != nil {
		t.Fatalf("remove flat outline: %v", err)
	}
	if err := s.Progress.SetTotalChapters(1); err != nil {
		t.Fatalf("set stale total: %v", err)
	}
	if err := s.ExpandArc(1, 2, expansion); err != nil {
		t.Fatalf("idempotent retry should repair derived state: %v", err)
	}
	flat, err = s.Outline.LoadOutline()
	if err != nil || len(flat) != 3 {
		t.Fatalf("flat outline was not repaired: len=%d err=%v", len(flat), err)
	}
	progress, err = s.Progress.Load()
	if err != nil || progress.TotalChapters != 3 {
		t.Fatalf("progress total was not repaired: progress=%+v err=%v", progress, err)
	}
	changed := expansion
	changed.Goal = "viết lại cung đã triển khai sau sự kiện"
	if err := s.ExpandArc(1, 2, changed); err == nil {
		t.Fatal("expected a different expansion to reject overwriting the expanded arc")
	}
}

func TestAppendVolumeValidation(t *testing.T) {
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "Tập một", Theme: "khởi đầu",
		Arcs: []domain.ArcOutline{{
			Index: 1, Title: "Cung đầu", Goal: "mục tiêu",
			Chapters: []domain.OutlineEntry{{Title: "chương", CoreEvent: "sự kiện", Hook: "điểm móc"}},
		}},
	}})

	validVol := domain.VolumeOutline{
		Index: 2, Title: "Tập hai", Theme: "nâng cấp",
		Arcs: []domain.ArcOutline{{
			Index: 1, Title: "Cung một", Goal: "mục tiêu",
			Chapters: []domain.OutlineEntry{{Title: "Chương mới", CoreEvent: "tiến triển", Hook: "điểm móc"}},
		}},
	}

	// Lần thêm bình thường phải thành công
	if err := s.AppendVolume(validVol); err != nil {
		t.Fatalf("AppendVolume valid: %v", err)
	}

	// Index không tăng → thất bại
	if err := s.AppendVolume(domain.VolumeOutline{
		Index: 1, Title: "trùng lặp", Theme: "x",
		Arcs: []domain.ArcOutline{{Index: 1, Title: "cung", Goal: "g", Chapters: []domain.OutlineEntry{{Title: "ch", CoreEvent: "e", Hook: "h"}}}},
	}); err == nil {
		t.Fatal("expected error for non-increasing index")
	}

	// Không có cung → thất bại
	if err := s.AppendVolume(domain.VolumeOutline{Index: 3, Title: "rỗng", Theme: "x"}); err == nil {
		t.Fatal("expected error for volume with no arcs")
	}

	// Cung đầu không có chương → thất bại
	if err := s.AppendVolume(domain.VolumeOutline{
		Index: 3, Title: "khung", Theme: "x",
		Arcs: []domain.ArcOutline{{Index: 1, Title: "cung", Goal: "g", EstimatedChapters: 10}},
	}); err == nil {
		t.Fatal("expected error for first arc without chapters")
	}
}

// Lưu ý: ngữ nghĩa từ chối append dựa trên tập Final trước đây đã được đẩy xuống tầng
// save_foundation (từ chối khi Phase=Complete),
// xem save_foundation_test.go::TestSaveFoundationAppendVolumeRejectsAfterComplete.
// tầng store chỉ giữ kiểm tra cấu trúc (Index tăng dần / cung đầu có chương...).

func TestSaveAndLoadCompass(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// direction rỗng phải thất bại
	if err := s.Outline.SaveCompass(domain.StoryCompass{EstimatedScale: "3 tập"}); err == nil {
		t.Fatal("expected error for empty ending_direction")
	}

	// Lưu bình thường
	compass := domain.StoryCompass{
		EndingDirection: "nhân vật chính đối mặt với lựa chọn cuối cùng",
		OpenThreads:     []string{"manh mối A", "quan hệ B"},
		EstimatedScale:  "dự kiến 4-6 tập",
		LastUpdated:     12,
	}
	if err := s.Outline.SaveCompass(compass); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}

	loaded, err := s.Outline.LoadCompass()
	if err != nil {
		t.Fatalf("LoadCompass: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected compass, got nil")
	}
	if loaded.EndingDirection != "nhân vật chính đối mặt với lựa chọn cuối cùng" {
		t.Fatalf("expected direction %q, got %q", "nhân vật chính đối mặt với lựa chọn cuối cùng", loaded.EndingDirection)
	}
	if len(loaded.OpenThreads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(loaded.OpenThreads))
	}
}

// TestOutlineFeedbackPool vòng khép bể phản hồi: commit ghi xuống → đọc được sau khi
// khởi động lại → thao tác cấu trúc tiêu thụ và xóa.
func TestOutlineFeedbackPool(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Outline.AppendOutlineFeedback(ChapterFeedback{Chapter: 3, Deviation: "nhánh phụ phình to", Suggestion: "cung sau gom lại"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := s.Outline.AppendOutlineFeedback(ChapterFeedback{Chapter: 3, Deviation: "nhánh phụ phình to", Suggestion: "cung sau gom lại"}); err != nil {
		t.Fatalf("append duplicate: %v", err)
	}
	if err := s.Outline.AppendOutlineFeedback(ChapterFeedback{Chapter: 4, Suggestion: "phản diện xuất hiện sớm"}); err != nil {
		t.Fatalf("append2: %v", err)
	}

	// Đọc được sau khi khởi động lại (thể hiện Store mới) — không phải trạng thái trong bộ nhớ
	s2 := NewStore(dir)
	fbs, err := s2.Outline.LoadPendingOutlineFeedback()
	if err != nil {
		t.Fatalf("load feedback: %v", err)
	}
	if len(fbs) != 2 || fbs[0].Chapter != 3 || fbs[1].Suggestion != "phản diện xuất hiện sớm" {
		t.Fatalf("đọc sau khi khởi động lại thất bại: %+v", fbs)
	}
	for _, fb := range fbs {
		if fb.At == "" {
			t.Fatal("At phải được bổ sung tự động")
		}
	}

	if err := s2.Outline.ClearOutlineFeedback(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if left, err := s2.Outline.LoadPendingOutlineFeedback(); err != nil || len(left) != 0 {
		t.Fatalf("sau khi tiêu thụ phải rỗng: %+v", left)
	}
	// Xóa idempotent
	if err := s2.Outline.ClearOutlineFeedback(); err != nil {
		t.Fatalf("clear idempotent: %v", err)
	}
}

func TestOutlineFeedbackCorruptionIsNotSilentlyConsumed(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	path := filepath.Join(dir, outlineFeedbackFile)
	if err := os.WriteFile(path, []byte("{\n"), 0o644); err != nil {
		t.Fatalf("write corrupt feedback: %v", err)
	}
	if _, err := s.Outline.LoadPendingOutlineFeedback(); err == nil {
		t.Fatal("corrupt feedback must return a read error")
	}
	if err := s.Outline.ClearOutlineFeedback(); err == nil {
		t.Fatal("corrupt feedback must not be cleared as if it had been consumed")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("corrupt feedback should remain for diagnosis: %v", err)
	}
}
