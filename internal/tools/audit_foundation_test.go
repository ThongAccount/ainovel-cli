package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
	"github.com/voocel/ainovel-cli/internal/store"
)

func completeShortFoundation(t *testing.T) *store.Store {
	t.Helper()
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.Init(1); err != nil {
		t.Fatal(err)
	}
	if err := s.Book.Save(domain.BookMetadata{Title: "Sách kiểm thử thẩm duyệt", Synopsis: "Lâm Châu tìm lối thoát trong thành phố cấm đêm."}); err != nil {
		t.Fatal(err)
	}
	if err := s.Outline.SavePremise("# Sách kiểm thử thẩm duyệt\n\n## Mục tiêu nhân vật chính\nLâm Châu tìm cách sống sót"); err != nil {
		t.Fatal(err)
	}
	if err := s.Outline.SaveOutline([]domain.OutlineEntry{{Chapter: 1, Title: "Tìm cách sống sót", CoreEvent: "Lâm Châu thoát nạn"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Characters.Save([]domain.Character{{Name: "Lâm Châu", Role: "Nhân vật chính", Description: "Người tìm đường sống sót"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.World.SaveWorldRules([]domain.WorldRule{{Category: "society", Rule: "Cổng thành cấm đêm", Boundary: "Đóng lúc hoàng hôn"}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAuditFoundationControlsWritingTransition(t *testing.T) {
	s := completeShortFoundation(t)
	tool := NewAuditFoundationTool(s)
	if !tool.StrictSchema() {
		t.Fatal("audit_foundation phải dùng strict schema")
	}
	if err := llmcontract.ValidateStrictReady(tool.Schema()); err != nil {
		t.Fatalf("schema của audit_foundation chưa strict-ready: %v", err)
	}
	missing, err := s.FoundationMissing()
	if err != nil || len(missing) != 1 || missing[0] != "foundation_audit" {
		t.Fatalf("cần chỉ foundation_audit, thực tế: %v, err=%v", missing, err)
	}
	fingerprint, err := s.FoundationFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	failed, _ := json.Marshal(map[string]any{
		"fingerprint": fingerprint,
		"ready":       false,
		"summary":     "Tên nhân vật không nhất quán",
		"issues": []map[string]any{{
			"artifact": "characters", "description": "Nhân vật không nhất quán", "evidence": "Tiền đề nói Lâm Châu, bảng nhân vật lại là người khác", "suggestion": "Thống nhất nhân vật",
		}},
	})
	if _, err := tool.Execute(context.Background(), failed); err != nil {
		t.Fatalf("thẩm duyệt thất bại phải lưu lại hướng dẫn: %v", err)
	}
	if p, _ := s.Progress.Load(); p.Phase == domain.PhaseWriting {
		t.Fatal("thẩm duyệt thất bại không được bước vào writing")
	}

	passed, _ := json.Marshal(map[string]any{
		"fingerprint": fingerprint,
		"ready":       true,
		"summary":     "Bối cảnh nền tảng nhất quán",
		"issues":      []any{},
	})
	if _, err := tool.Execute(context.Background(), passed); err != nil {
		t.Fatalf("thẩm duyệt đạt: %v", err)
	}
	if p, _ := s.Progress.Load(); p.Phase != domain.PhaseWriting {
		t.Fatalf("thẩm duyệt đạt phải bước vào writing, thực tế: %s", p.Phase)
	}
}

func TestSaveFoundationWaitsForSemanticAudit(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress.Init(1); err != nil {
		t.Fatal(err)
	}
	if err := s.Book.Save(domain.BookMetadata{Title: "Sách thử nghiệm", Synopsis: "Lâm Châu mạo hiểm bước vào thành phố cấm đêm."}); err != nil {
		t.Fatal(err)
	}
	if err := s.Outline.SavePremise("# test"); err != nil {
		t.Fatal(err)
	}
	if err := s.Characters.Save([]domain.Character{{Name: "Lâm Châu", Role: "Nhân vật chính"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.World.SaveWorldRules([]domain.WorldRule{{Category: "society", Rule: "Cấm đêm", Boundary: "Buổi tối"}}); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{
		"type": "outline", "scale": "short",
		"content": []map[string]any{{"chapter": 1, "title": "Khởi đầu", "core_event": "Lâm Châu vào thành", "hook": "Cấm đêm", "scenes": []string{"Vào thành"}}},
	})
	result, err := NewSaveFoundationTool(s).Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Ready     bool     `json:"foundation_ready"`
		Remaining []string `json:"remaining"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Ready || len(payload.Remaining) != 1 || payload.Remaining[0] != "foundation_audit" {
		t.Fatalf("save_foundation phải chờ thẩm duyệt: %+v", payload)
	}
	if p, _ := s.Progress.Load(); p.Phase == domain.PhaseWriting {
		t.Fatal("save_foundation không được bước vào writing trước khi thẩm duyệt")
	}
}

func TestAuditFoundationRejectsStaleFingerprint(t *testing.T) {
	s := completeShortFoundation(t)
	fingerprint, err := s.FoundationFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Outline.SavePremise("# Phiên bản đã sửa"); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{
		"fingerprint": fingerprint, "ready": true, "summary": "Đạt", "issues": []any{},
	})
	if _, err := NewAuditFoundationTool(s).Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "gọi lại novel_context") {
		t.Fatalf("cần từ chối fingerprint cũ, thực tế: %v", err)
	}
}
