package store

import (
	"errors"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

func TestSaveAndLoadRunMeta(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	meta := domain.RunMeta{
		StartedAt: "2026-03-07T10:00:00+08:00",
		Provider:  "openrouter",
		Style:     "fantasy",
		Model:     "gpt-4o",
	}
	if err := store.RunMeta.Save(meta); err != nil {
		t.Fatalf("SaveRunMeta: %v", err)
	}

	loaded, err := store.RunMeta.Load()
	if err != nil {
		t.Fatalf("LoadRunMeta: %v", err)
	}
	if loaded.Style != "fantasy" {
		t.Errorf("style mismatch: %s", loaded.Style)
	}
	if loaded.Provider != "openrouter" {
		t.Errorf("provider mismatch: %s", loaded.Provider)
	}
	if loaded.Model != "gpt-4o" {
		t.Errorf("model mismatch: %s", loaded.Model)
	}
}

func TestLoadRunMeta_Empty(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	meta, err := store.RunMeta.Load()
	if err != nil {
		t.Fatalf("LoadRunMeta on empty: %v", err)
	}
	if meta != nil {
		t.Fatalf("expected nil, got %+v", meta)
	}
}

func TestInitRunMeta_PreservesHistory(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Trước hết tạo RunMeta có ý định lần chạy
	_ = store.RunMeta.Save(domain.RunMeta{
		StartedAt:    "old",
		Provider:     "openai",
		Style:        "fantasy",
		Model:        "old-model",
		PendingSteer: "đang chờ xử lý",
	})

	// Init phải giữ lại các sự thật ý định lần chạy như PendingSteer
	_ = store.RunMeta.Init("suspense", "openrouter", "new-model")

	meta, _ := store.RunMeta.Load()
	if meta.Style != "suspense" {
		t.Errorf("style should be updated, got %s", meta.Style)
	}
	if meta.Provider != "openrouter" {
		t.Errorf("provider should be updated, got %s", meta.Provider)
	}
	if meta.Model != "new-model" {
		t.Errorf("model should be updated, got %s", meta.Model)
	}
	if meta.PendingSteer != "đang chờ xử lý" {
		t.Errorf("pending steer should be preserved, got %s", meta.PendingSteer)
	}
	if meta.AdvanceMode != domain.ChapterAdvanceAuto {
		t.Errorf("missing advance mode should initialize to auto, got %q", meta.AdvanceMode)
	}
}

func TestSetAndClearPendingSteer(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Đặt PendingSteer
	if err := store.RunMeta.SetPendingSteer("đổi nhân vật chính thành nữ"); err != nil {
		t.Fatalf("SetPendingSteer: %v", err)
	}
	meta, _ := store.RunMeta.Load()
	if meta.PendingSteer != "đổi nhân vật chính thành nữ" {
		t.Errorf("expected pending steer, got %s", meta.PendingSteer)
	}

	// Xóa
	if err := store.RunMeta.ClearPendingSteer(); err != nil {
		t.Fatalf("ClearPendingSteer: %v", err)
	}
	meta, _ = store.RunMeta.Load()
	if meta.PendingSteer != "" {
		t.Errorf("expected empty pending steer, got %s", meta.PendingSteer)
	}
}

func TestSetPlanningTier(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	if err := store.RunMeta.SetPlanningTier(domain.PlanningTierLong); err != nil {
		t.Fatalf("SetPlanningTier: %v", err)
	}

	meta, err := store.RunMeta.Load()
	if err != nil {
		t.Fatalf("LoadRunMeta: %v", err)
	}
	if meta == nil {
		t.Fatal("expected run meta to exist")
	}
	if meta.PlanningTier != domain.PlanningTierLong {
		t.Fatalf("expected planning tier %q, got %q", domain.PlanningTierLong, meta.PlanningTier)
	}
}

func TestClearPendingSteer_Noop(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Gọi trên meta rỗng không được báo lỗi
	if err := store.RunMeta.ClearPendingSteer(); err != nil {
		t.Fatalf("ClearPendingSteer on empty: %v", err)
	}
}

func TestAdvanceControlRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.RunMeta.Init("fantasy", "openrouter", "m"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.RunMeta.SetAdvanceMode(domain.ChapterAdvanceReview); err != nil {
		t.Fatalf("SetAdvanceMode: %v", err)
	}
	if err := store.RunMeta.GrantAdvancePermit(3); err != nil {
		t.Fatalf("GrantAdvancePermit: %v", err)
	}
	hold := domain.AdvanceHold{After: domain.AdvanceHoldAfterRewritesDrained, Reason: "viết lại chương 3"}
	if err := store.RunMeta.SetAdvanceHold(hold); err != nil {
		t.Fatalf("SetAdvanceHold: %v", err)
	}

	meta, _ := store.RunMeta.Load()
	if meta.AdvanceMode != domain.ChapterAdvanceReview || meta.AdvancePermitChapter != 3 {
		t.Fatalf("advance mode/permit round trip: %+v", meta)
	}
	if meta.AdvanceHold == nil || *meta.AdvanceHold != hold {
		t.Fatalf("advance hold round trip: %+v", meta.AdvanceHold)
	}

	if err := store.RunMeta.ClearAdvancePermit(3); err != nil {
		t.Fatalf("ClearAdvancePermit: %v", err)
	}
	if err := store.RunMeta.ClearAdvanceHold(hold); err != nil {
		t.Fatalf("ClearAdvanceHold: %v", err)
	}
	meta, _ = store.RunMeta.Load()
	if meta.AdvancePermitChapter != 0 || meta.AdvanceHold != nil {
		t.Fatalf("advance intent should be consumed: %+v", meta)
	}
}

func TestInitRunMeta_PreservesAdvanceIntent(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	if err := store.RunMeta.Init("fantasy", "openrouter", "m"); err != nil {
		t.Fatal(err)
	}
	_ = store.RunMeta.SetAdvanceMode(domain.ChapterAdvanceReview)
	_ = store.RunMeta.GrantAdvancePermit(7)
	hold := domain.AdvanceHold{After: domain.AdvanceHoldAtBoundary, Reason: "duyệt"}
	_ = store.RunMeta.SetAdvanceHold(hold)
	// Đường khởi động lại tiến trình: Host.New gọi Init mỗi lần, ý định lần chạy của người
	// dùng phải còn sống.
	_ = store.RunMeta.Init("fantasy", "openrouter", "m")

	meta, _ := store.RunMeta.Load()
	if meta.AdvanceMode != domain.ChapterAdvanceReview || meta.AdvancePermitChapter != 7 {
		t.Fatalf("advance mode/permit should survive Init, got %+v", meta)
	}
	if meta.AdvanceHold == nil || *meta.AdvanceHold != hold {
		t.Fatalf("advance hold should survive Init, got %+v", meta.AdvanceHold)
	}
}

func TestAdvanceControlRejectsConflictingIntent(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.RunMeta.Init("fantasy", "openrouter", "m"); err != nil {
		t.Fatal(err)
	}
	if err := store.RunMeta.GrantAdvancePermit(1); err == nil {
		t.Fatal("auto mode must reject permit")
	}
	if err := store.RunMeta.SetAdvanceMode(domain.ChapterAdvanceReview); err != nil {
		t.Fatal(err)
	}
	if err := store.RunMeta.GrantAdvancePermit(2); err != nil {
		t.Fatal(err)
	}
	if err := store.RunMeta.GrantAdvancePermit(3); err == nil {
		t.Fatal("conflicting permit must fail")
	}
	hold := domain.AdvanceHold{After: domain.AdvanceHoldAtBoundary, Reason: "dừng"}
	if err := store.RunMeta.SetAdvanceHold(hold); err != nil {
		t.Fatal(err)
	}
	if err := store.RunMeta.SetAdvanceHold(domain.AdvanceHold{After: domain.AdvanceHoldAtBoundary, Reason: "một ý định khác"}); err == nil {
		t.Fatal("conflicting hold must fail")
	}
	if err := (domain.AdvanceHold{After: domain.AdvanceHoldAtChapter, Reason: "thiếu mục tiêu"}).Validate(); err == nil {
		t.Fatal("chapter hold without target must fail")
	}
	if err := (domain.AdvanceHold{After: domain.AdvanceHoldAtBoundary, TargetChapter: 3, Reason: "mục tiêu sai"}).Validate(); err == nil {
		t.Fatal("non-chapter hold with target must fail")
	}
	if err := store.RunMeta.ClearAdvanceHold(domain.AdvanceHold{After: domain.AdvanceHoldAtBoundary, Reason: "giá trị cũ"}); err == nil {
		t.Fatal("compare-and-clear must reject changed hold")
	}
	if err := store.RunMeta.SetAdvanceMode(domain.ChapterAdvanceAuto); err != nil {
		t.Fatal(err)
	}
	meta, _ := store.RunMeta.Load()
	if meta.AdvancePermitChapter != 0 || meta.AdvanceHold == nil {
		t.Fatalf("auto should clear permit but preserve hold: %+v", meta)
	}
}

func TestInitRunMeta_UnknownAdvanceModeDoesNotWrite(t *testing.T) {
	store := NewStore(t.TempDir())
	original := domain.RunMeta{Style: "old", AdvanceMode: "future"}
	if err := store.RunMeta.Save(original); err != nil {
		t.Fatal(err)
	}
	err := store.RunMeta.Init("new", "openrouter", "m")
	var unsupported *domain.UnsupportedAdvanceModeError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected UnsupportedAdvanceModeError, got %v", err)
	}
	meta, loadErr := store.RunMeta.Load()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if meta.Style != "old" || meta.AdvanceMode != "future" {
		t.Fatalf("failed Init must not rewrite RunMeta: %+v", meta)
	}
}

// TestRunMetaInit_PreservesPlanStart giai đoạn quy hoạch (phán quyết đã ghi xuống, artifact
// foundation đầu tiên chưa ghi xuống)
// khi khởi động lại sau sập, RunMeta.Init của Host.New không được xóa PlanStart — đó là
// căn cứ duy nhất để khôi phục danh tính người lập kế hoạch (engine.planStartFallback).
func TestRunMetaInit_PreservesPlanStart(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.RunMeta.SetStartPrompt("viết một truyện ngắn hồi trí"); err != nil {
		t.Fatalf("set start prompt: %v", err)
	}
	rec := domain.PlanStartRecord{RawPrompt: "viết một truyện ngắn hồi trí", Planner: "architect_short", PlannerTask: "toàn văn nhiệm vụ", DecisionID: "dec-x"}
	if err := store.RunMeta.SetPlanStart(rec); err != nil {
		t.Fatalf("set plan start: %v", err)
	}
	// Mô phỏng khởi động lại tiến trình: Host.New sẽ gọi Init lần nữa
	if err := store.RunMeta.Init("default", "openrouter", "m"); err != nil {
		t.Fatalf("init: %v", err)
	}
	meta, err := store.RunMeta.Load()
	if err != nil || meta == nil {
		t.Fatalf("load: %v", err)
	}
	if meta.PlanStart == nil || meta.PlanStart.Planner != "architect_short" {
		t.Fatalf("Init phải giữ PlanStart, got %+v", meta.PlanStart)
	}
	// StartPrompt cũng là sự thật xuyên lần khởi động: khi phán quyết thất bại, nó là căn cứ
	// duy nhất để engine phán quyết bù.
	if meta.StartPrompt != "viết một truyện ngắn hồi trí" {
		t.Fatalf("Init phải giữ StartPrompt, got %q", meta.StartPrompt)
	}
}
