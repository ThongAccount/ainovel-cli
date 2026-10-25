package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
	"github.com/voocel/ainovel-cli/internal/store"
)

// AuditFoundationTool nhận kết luận thẩm duyệt ngữ nghĩa của Kiến trúc sư về
// bối cảnh nền tảng đã ghi xuống đĩa.
// Phần văn phong và ngữ nghĩa xuyên tệp do model phán xét; công cụ chỉ bảo đảm
// phiên bản thẩm duyệt, kết luận và việc chuyển trạng thái nhất quán.
type AuditFoundationTool struct {
	store *store.Store
}

func NewAuditFoundationTool(store *store.Store) *AuditFoundationTool {
	return &AuditFoundationTool{store: store}
}

// texts lấy bảng văn bản theo ngôn ngữ tác phẩm, đọc lười mỗi lần gọi.
func (t *AuditFoundationTool) texts() *writeTexts { return writeTextsFor(t.store.Language()) }

func (t *AuditFoundationTool) Name() string { return "audit_foundation" }
func (t *AuditFoundationTool) Description() string {
	return t.texts().auditFoundationDesc
}
func (t *AuditFoundationTool) Label() string                          { return t.texts().auditFoundationLabel }
func (t *AuditFoundationTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *AuditFoundationTool) ConcurrencySafe(_ json.RawMessage) bool { return false }
func (t *AuditFoundationTool) StrictSchema() bool                     { return true }

func (t *AuditFoundationTool) Schema() map[string]any {
	texts := t.texts()
	issue := schema.Object(
		schema.Property("artifact", schema.String(texts.auditFoundationArgArtifact)).Required(),
		schema.Property("description", schema.String(texts.auditFoundationArgDescription)).Required(),
		schema.Property("evidence", schema.String(texts.auditFoundationArgEvidence)).Required(),
		schema.Property("suggestion", llmcontract.Nullable(schema.String(texts.auditFoundationArgSuggestion))).Required(),
	)
	return schema.Object(
		schema.Property("fingerprint", schema.String(texts.auditFoundationArgFingerprint)).Required(),
		schema.Property("ready", schema.Bool(texts.auditFoundationArgReady)).Required(),
		schema.Property("summary", schema.String(texts.auditFoundationArgSummary)).Required(),
		schema.Property("issues", schema.Array(texts.auditFoundationArgIssues, issue)).Required(),
	)
}

func (t *AuditFoundationTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var audit domain.FoundationAudit
	if err := json.Unmarshal(args, &audit); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if strings.TrimSpace(audit.Fingerprint) == "" {
		return nil, fmt.Errorf("fingerprint is required: %w", errs.ErrToolArgs)
	}
	if strings.TrimSpace(audit.Summary) == "" {
		return nil, fmt.Errorf("summary is required: %w", errs.ErrToolArgs)
	}

	missing, err := t.store.FoundationMissing()
	if err != nil {
		return nil, fmt.Errorf("load foundation state: %w: %w", errs.ErrStoreRead, err)
	}
	for _, item := range missing {
		if item != "foundation_audit" {
			return nil, fmt.Errorf("bối cảnh nền tảng còn thiếu %s, chưa thể thẩm duyệt: %w", item, errs.ErrToolPrecondition)
		}
	}
	current, err := t.store.FoundationFingerprint()
	if err != nil {
		return nil, fmt.Errorf("fingerprint foundation: %w: %w", errs.ErrStoreRead, err)
	}
	if audit.Fingerprint != current {
		return nil, fmt.Errorf("bối cảnh nền tảng đã thay đổi; vui lòng gọi lại novel_context để lấy fingerprint mới nhất rồi thẩm duyệt lại: %w", errs.ErrToolConflict)
	}
	if audit.Ready && len(audit.Issues) > 0 {
		return nil, fmt.Errorf("khi ready=true thì issues phải rỗng: %w", errs.ErrToolArgs)
	}
	if !audit.Ready && len(audit.Issues) == 0 {
		return nil, fmt.Errorf("khi ready=false thì phải nêu cụ thể các issues: %w", errs.ErrToolArgs)
	}
	for i, issue := range audit.Issues {
		if strings.TrimSpace(issue.Artifact) == "" || strings.TrimSpace(issue.Description) == "" || strings.TrimSpace(issue.Evidence) == "" {
			return nil, fmt.Errorf("issues[%d] phải có artifact, description và evidence: %w", i, errs.ErrToolArgs)
		}
	}

	if err := t.store.Outline.SaveFoundationAudit(audit); err != nil {
		return nil, fmt.Errorf("save foundation audit: %w: %w", errs.ErrStoreWrite, err)
	}
	result := map[string]any{
		"foundation_ready": audit.Ready,
		"issues":           audit.Issues,
	}
	if !audit.Ready {
		result["next_action"] = t.texts().auditFoundationNextAction
		return json.Marshal(result)
	}

	if _, err := t.store.Checkpoints.AppendArtifact(domain.GlobalScope(), "foundation_audit", "meta/foundation_audit.json"); err != nil {
		return nil, fmt.Errorf("checkpoint foundation audit: %w: %w", errs.ErrStoreWrite, err)
	}
	if err := t.store.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		return nil, fmt.Errorf("enter writing phase: %w: %w", errs.ErrStoreWrite, err)
	}
	result["phase"] = string(domain.PhaseWriting)
	return json.Marshal(result)
}
