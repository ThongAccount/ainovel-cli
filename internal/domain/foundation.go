package domain

// FoundationAuditIssue là vấn đề nhất quán xuyên tệp do Architect chỉ ra cho phần
// nền tảng đã ghi xuống.
type FoundationAuditIssue struct {
	Artifact    string `json:"artifact"`
	Description string `json:"description"`
	Evidence    string `json:"evidence"`
	Suggestion  string `json:"suggestion,omitempty"`
}

// FoundationAudit ghi lại một lần model thẩm duyệt phần nền tảng của một phiên bản
// xác định.
type FoundationAudit struct {
	Fingerprint string                 `json:"fingerprint"`
	Ready       bool                   `json:"ready"`
	Summary     string                 `json:"summary"`
	Issues      []FoundationAuditIssue `json:"issues"`
}
