package revision

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/llm"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
	"github.com/voocel/ainovel-cli/internal/store"
)

func TestAnalysisContractIsStrictReady(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.LangVI, i18n.LangZH} {
		if err := llmcontract.ValidateStrictReady(analysisContractFor(lang).Schema); err != nil {
			t.Fatalf("hợp đồng ngôn ngữ %s không hợp lệ: %v", lang, err)
		}
	}
}

func TestScanUsesAcceptedContentInsteadOfFileMetadata(t *testing.T) {
	st := newRevisionTestStore(t, 1)
	acceptTestChapter(t, st, 1, "Đoạn một\nĐoạn hai", domain.ChapterFacts{Title: "Chương 1", Summary: "Tóm tắt", KeyEvents: []string{"Sự kiện"}})

	path := filepath.Join(st.Dir(), "chapters", "01.md")
	if err := os.WriteFile(path, []byte("Đoạn một\r\nĐoạn hai"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := Scan(st)
	if err != nil || len(changes) != 0 {
		t.Fatalf("Chỉ đổi ký tự cuối dòng không được tạo bản sửa đổi: changes=%v err=%v", changes, err)
	}
	if err := os.WriteFile(path, []byte("Đoạn một\r\nNgười dùng viết lại"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err = Scan(st)
	if err != nil || len(changes) != 1 || changes[0].Before == changes[0].After {
		t.Fatalf("Thay đổi nội dung không được nhận diện: changes=%+v err=%v", changes, err)
	}
}

func TestScanRejectsEmptyCompletedChapter(t *testing.T) {
	st := newRevisionTestStore(t, 1)
	facts := domain.ChapterFacts{Title: "Chương 1", Summary: "Tóm tắt", KeyEvents: []string{"Sự kiện"}}
	acceptTestChapter(t, st, 1, "Nội dung hệ thống", facts)
	if err := os.WriteFile(filepath.Join(st.Dir(), "chapters", "01.md"), []byte(" \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(st); err == nil {
		t.Fatal("Bản chính rỗng phải bị từ chối tường minh")
	}
}

func TestMigrateLegacyBaselineKeepsExternalChangeDirty(t *testing.T) {
	st := newRevisionTestStore(t, 1)
	facts := domain.ChapterFacts{
		Title: "Chương 1", Summary: "Lâm Mặc rời làng", Characters: []string{"Lâm Mặc"}, KeyEvents: []string{"Rời làng"},
		TimelineEvents: []domain.TimelineEvent{{Time: "Buổi sớm", Event: "Lâm Mặc rời làng", Characters: []string{"Lâm Mặc"}}},
		HookType:       "mystery", DominantStrand: "quest",
	}
	if err := st.Drafts.SaveDraft(1, "Nội dung hệ thống đã commit"); err != nil {
		t.Fatal(err)
	}
	if err := st.Drafts.SaveFinalChapter(1, "Nội dung người dùng sửa sau đó"); err != nil {
		t.Fatal(err)
	}
	if err := st.Summaries.SaveSummary(domain.ChapterSummary{
		Chapter: 1, Title: facts.Title, Summary: facts.Summary, Characters: facts.Characters, KeyEvents: facts.KeyEvents,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.StartChapter(1); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.MarkChapterComplete(1, 8, facts.HookType, facts.DominantStrand); err != nil {
		t.Fatal(err)
	}
	writeLegacyCommitSession(t, st.Dir(), 1, facts)

	if err := MigrateLegacyBaseline(st); err != nil {
		t.Fatal(err)
	}
	record, err := st.ChapterRecords.Load(1)
	if err != nil || record == nil {
		t.Fatalf("Thiếu bản ghi tiếp nhận sau khi di chuyển: record=%+v err=%v", record, err)
	}
	if record.Content != "Nội dung hệ thống đã commit" {
		t.Fatalf("Di chuyển tiếp nhận nhầm nội dung vùng làm việc hiện tại: %q", record.Content)
	}
	changes, err := Scan(st)
	if err != nil || len(changes) != 1 || changes[0].Chapter != 1 {
		t.Fatalf("Sửa đổi bên ngoài trước khi di chuyển phải được giữ ở trạng thái chờ đồng bộ: changes=%+v err=%v", changes, err)
	}
	if err := MigrateLegacyBaseline(st); err != nil {
		t.Fatalf("Di chuyển lặp lại phải là idempotent: %v", err)
	}
}

func TestMigrateLegacyBaselineFromImportArtifact(t *testing.T) {
	st := newRevisionTestStore(t, 1)
	facts := domain.ChapterFacts{
		Title: "Chương 1", Summary: "Nhập sách cũ", Characters: []string{"Lâm Mặc"}, KeyEvents: []string{"Vào thành cũ"},
		HookType: "mystery", DominantStrand: "quest",
	}
	if err := st.Drafts.SaveDraft(1, "Nội dung nhập"); err != nil {
		t.Fatal(err)
	}
	if err := st.Drafts.SaveFinalChapter(1, "Nội dung nhập"); err != nil {
		t.Fatal(err)
	}
	if err := st.Summaries.SaveSummary(domain.ChapterSummary{
		Chapter: 1, Title: facts.Title, Summary: facts.Summary, Characters: facts.Characters, KeyEvents: facts.KeyEvents,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.StartChapter(1); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.MarkChapterComplete(1, 4, facts.HookType, facts.DominantStrand); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.ChapterScope(1), "commit", "chapters/01.md"); err != nil {
		t.Fatal(err)
	}
	writeLegacyImportArtifact(t, st.Dir(), 1, facts)

	if err := MigrateLegacyBaseline(st); err != nil {
		t.Fatal(err)
	}
	record, err := st.ChapterRecords.Load(1)
	if err != nil || record == nil || record.Facts.Summary != facts.Summary || record.Content != "Nội dung nhập" {
		t.Fatalf("Kết quả di chuyển sách nhập sai: record=%+v err=%v", record, err)
	}
}

func TestChangedExcerptOmitsUnchangedPrefixAndSuffix(t *testing.T) {
	got := changedExcerpt("Mở đầu giống nhau\nNội dung cũ\nKết giống nhau", "Mở đầu giống nhau\nNội dung mới\nKết giống nhau")
	if got.Before != "Nội dung cũ" || got.After != "Nội dung mới" || got.BeforeStart != 2 || got.AfterStart != 2 {
		t.Fatalf("changed excerpt = %+v", got)
	}
}

func TestProjectorRebuildsWorldStateFromChapterRecords(t *testing.T) {
	st := newRevisionTestStore(t, 2)
	if err := st.World.SaveTimeline([]domain.TimelineEvent{{Chapter: 1, Time: "Cũ", Event: "Cần bị xoá"}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	records := []domain.ChapterRecord{
		testRecord(1, "Nội dung một", domain.ChapterFacts{
			Title: "Chương 1", Summary: "Tóm tắt mới", Characters: []string{"Lâm Mặc", "Chủ quán"}, KeyEvents: []string{"Rời thành"},
			TimelineEvents:      []domain.TimelineEvent{{Time: "Trong đêm", Event: "Lâm Mặc rời thành", Characters: []string{"Lâm Mặc"}}},
			ForeshadowUpdates:   []domain.ForeshadowUpdate{{ID: "Lá thư", Action: "plant", Description: "Lá thư chưa mở"}},
			RelationshipChanges: []domain.RelationshipEntry{{CharacterA: "Lâm Mặc", CharacterB: "Chủ quán", Relation: "Hai bên tin tưởng nhau"}},
			StateChanges:        []domain.StateChange{{Entity: "Lâm Mặc", Field: "location", NewValue: "Ngoài thành"}},
			CastIntros:          []domain.CastIntro{{Name: "Chủ quán", BriefRole: "Chủ quán nhà trọ"}}, HookType: "mystery", DominantStrand: "quest",
		}, domain.StyleDelta{Prose: []string{"Giảm miêu tả tâm lý mang tính giải thích"}}, now),
		testRecord(2, "Nội dung hai", domain.ChapterFacts{
			Title: "Chương 2", Summary: "Tiếp theo", Characters: []string{"Lâm Mặc", "Chủ quán"}, KeyEvents: []string{"Mở lá thư"},
			ForeshadowUpdates:   []domain.ForeshadowUpdate{{ID: "Lá thư", Action: "resolve"}},
			RelationshipChanges: []domain.RelationshipEntry{{CharacterA: "Chủ quán", CharacterB: "Lâm Mặc", Relation: "Đoạn tuyệt"}},
		}, domain.StyleDelta{}, now.Add(time.Minute)),
	}
	if err := NewProjector(st).Apply(records); err != nil {
		t.Fatal(err)
	}
	timeline, _ := st.World.LoadTimeline()
	if len(timeline) != 1 || timeline[0].Event != "Lâm Mặc rời thành" || timeline[0].Chapter != 1 {
		t.Fatalf("Dòng thời gian không được dựng lại theo bản ghi: %+v", timeline)
	}
	ledger, _ := st.World.LoadForeshadowLedger()
	if len(ledger) != 1 || ledger[0].Status != "resolved" || ledger[0].ResolvedAt != 2 {
		t.Fatalf("Phóng chiếu manh mối sai: %+v", ledger)
	}
	relationships, _ := st.World.LoadRelationships()
	if len(relationships) != 1 || relationships[0].Relation != "Đoạn tuyệt" || relationships[0].Chapter != 2 {
		t.Fatalf("Phóng chiếu quan hệ sai: %+v", relationships)
	}
	style, _ := st.World.LoadAuthorRevisionStyle()
	if style == nil || len(style.Prose) != 1 || style.Prose[0] != "Giảm miêu tả tâm lý mang tính giải thích" {
		t.Fatalf("Phong cách sửa đổi của người dùng không được phóng chiếu: %+v", style)
	}
}

func TestServiceAcceptsRevisionAndRefreshesFacts(t *testing.T) {
	st := newRevisionTestStore(t, 1)
	acceptTestChapter(t, st, 1, "Lâm Mặc ở lại trong thành.", domain.ChapterFacts{
		Title: "Chương 1", Summary: "Lâm Mặc ở lại trong thành", Characters: []string{"Lâm Mặc"}, KeyEvents: []string{"Ở lại thành"},
	})
	if err := os.WriteFile(filepath.Join(st.Dir(), "chapters", "01.md"), []byte("Lâm Mặc rời thành trong đêm."), 0o644); err != nil {
		t.Fatal(err)
	}
	model := &revisionModel{response: `{
  "change_summary":"Lâm Mặc đổi từ ở lại thành sang rời thành trong đêm",
  "story_changed":true,
  "facts":{
    "title":"Chương 1","summary":"Lâm Mặc rời thành trong đêm","characters":["Lâm Mặc"],"key_events":["Lâm Mặc rời thành"],
    "timeline_events":[{"time":"Trong đêm","event":"Lâm Mặc rời thành","characters":["Lâm Mặc"]}],
    "foreshadow_updates":[],"relationship_changes":[],
    "state_changes":[{"entity":"Lâm Mặc","field":"location","old_value":"Trong thành","new_value":"Ngoài thành","reason":"Tự nguyện rời đi"}],
    "cast_intros":[],"hook_type":null,"dominant_strand":null
  },
  "style_delta":{"prose":["Diễn đạt hành động trực tiếp, không bổ sung phần giải thích"],"dialogue":[],"taboos":[]},
  "outline_impact":{"deviation":"Nhân vật chính đã rời thành sớm","suggestion":"Phần sau tiếp nối từ ngoài thành"},
  "downstream_issues":[]
}`}
	index := &recordingStyleIndex{}
	result, err := NewService(st, model, "Phân tích bản sửa đổi của người dùng", index).Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Applied) != 1 || result.Applied[0] != 1 {
		t.Fatalf("Kết quả đồng bộ sai: %+v", result)
	}
	record, err := st.ChapterRecords.Load(1)
	if err != nil || record == nil || record.Origin != domain.ChapterOriginUser || record.Revision != 2 {
		t.Fatalf("Bản ghi tiếp nhận sai: record=%+v err=%v", record, err)
	}
	summary, _ := st.Summaries.LoadSummary(1)
	if summary == nil || summary.Summary != "Lâm Mặc rời thành trong đêm" {
		t.Fatalf("Tóm tắt chưa được làm mới: %+v", summary)
	}
	changes, err := Scan(st)
	if err != nil || len(changes) != 0 {
		t.Fatalf("Sau khi tiếp nhận, vùng làm việc vẫn còn dirty: changes=%v err=%v", changes, err)
	}
	if index.chapter != 1 || index.text != "Lâm Mặc rời thành trong đêm." {
		t.Fatalf("Chỉ mục thống kê phong cách chưa được làm mới: %+v", index)
	}
	if cp := st.Checkpoints.LatestByStep(domain.ChapterScope(1), "revision_sync"); cp == nil {
		t.Fatal("Thiếu checkpoint revision_sync")
	}
}

func TestServiceResumesProjectionWithoutCallingModel(t *testing.T) {
	st := newRevisionTestStore(t, 1)
	oldFacts := domain.ChapterFacts{Title: "Chương 1", Summary: "Tóm tắt cũ", KeyEvents: []string{"Sự kiện cũ"}}
	acceptTestChapter(t, st, 1, "Nội dung cũ", oldFacts)
	newFacts := domain.ChapterFacts{Title: "Chương 1", Summary: "Tóm tắt mới", KeyEvents: []string{"Sự kiện mới"}}
	record := testRecord(1, "Nội dung người dùng", newFacts, domain.StyleDelta{}, time.Now())
	record.Revision = 2
	if err := st.Drafts.SaveFinalChapter(1, record.Content); err != nil {
		t.Fatal(err)
	}
	if err := st.ChapterRecords.Save(record); err != nil {
		t.Fatal(err)
	}
	pending := domain.PendingRevision{
		Stage:     domain.RevisionStageRecordsApplied,
		Items:     []domain.PendingRevisionItem{{Chapter: 1, Record: record, Analysis: domain.RevisionAnalysis{Facts: newFacts}}},
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := st.Revisions.SavePending(pending); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(st, nil, "", nil).Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	summary, _ := st.Summaries.LoadSummary(1)
	if summary == nil || summary.Summary != "Tóm tắt mới" {
		t.Fatalf("Sau khi khôi phục, tóm tắt chưa được phóng chiếu: %+v", summary)
	}
	if pending, _ := st.Revisions.LoadPending(); pending != nil {
		t.Fatalf("Bản ghi khôi phục chưa được dọn: %+v", pending)
	}
}

func TestServiceResumesPreparedAfterRecordWasAlreadyWritten(t *testing.T) {
	st := newRevisionTestStore(t, 1)
	oldFacts := domain.ChapterFacts{Title: "Chương 1", Summary: "Tóm tắt cũ", KeyEvents: []string{"Sự kiện cũ"}}
	acceptTestChapter(t, st, 1, "Nội dung cũ", oldFacts)
	base, err := st.ChapterRecords.Load(1)
	if err != nil {
		t.Fatal(err)
	}
	newFacts := domain.ChapterFacts{Title: "Chương 1", Summary: "Tóm tắt mới", KeyEvents: []string{"Sự kiện mới"}}
	record := testRecord(1, "Nội dung người dùng", newFacts, domain.StyleDelta{}, time.Now())
	record.Revision = base.Revision + 1
	if err := st.Drafts.SaveFinalChapter(1, record.Content); err != nil {
		t.Fatal(err)
	}
	if err := st.ChapterRecords.Save(record); err != nil {
		t.Fatal(err)
	}
	pending := domain.PendingRevision{
		Stage: domain.RevisionStagePrepared,
		Items: []domain.PendingRevisionItem{{
			Chapter: 1, BaseSHA256: base.ContentSHA256, CurrentSHA256: record.ContentSHA256,
			Record: record, Analysis: domain.RevisionAnalysis{Facts: newFacts},
		}},
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := st.Revisions.SavePending(pending); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(st, nil, "", nil).Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	summary, _ := st.Summaries.LoadSummary(1)
	if summary == nil || summary.Summary != "Tóm tắt mới" {
		t.Fatalf("Khôi phục prepared không dựng lại phóng chiếu: %+v", summary)
	}
	if pending, _ := st.Revisions.LoadPending(); pending != nil {
		t.Fatalf("Bản ghi khôi phục prepared chưa được dọn: %+v", pending)
	}
}

func TestServiceResumesPartiallyWrittenPreparedBatch(t *testing.T) {
	st := newRevisionTestStore(t, 2)
	oldFacts := func(chapter int) domain.ChapterFacts {
		return domain.ChapterFacts{Title: "Tiêu đề cũ", Summary: "Tóm tắt cũ", KeyEvents: []string{"Sự kiện cũ"}}
	}
	acceptTestChapter(t, st, 1, "Nội dung cũ một", oldFacts(1))
	acceptTestChapter(t, st, 2, "Nội dung cũ hai", oldFacts(2))
	items := make([]domain.PendingRevisionItem, 0, 2)
	for chapter := 1; chapter <= 2; chapter++ {
		base, err := st.ChapterRecords.Load(chapter)
		if err != nil {
			t.Fatal(err)
		}
		facts := domain.ChapterFacts{Title: "Tiêu đề mới", Summary: fmt.Sprintf("Tóm tắt mới %d", chapter), KeyEvents: []string{"Sự kiện mới"}}
		content := fmt.Sprintf("Nội dung người dùng %d", chapter)
		record := testRecord(chapter, content, facts, domain.StyleDelta{}, time.Now())
		record.Revision = base.Revision + 1
		if err := st.Drafts.SaveFinalChapter(chapter, content); err != nil {
			t.Fatal(err)
		}
		items = append(items, domain.PendingRevisionItem{
			Chapter: chapter, BaseSHA256: base.ContentSHA256, CurrentSHA256: record.ContentSHA256,
			Record: record, Analysis: domain.RevisionAnalysis{Facts: facts},
		})
	}
	if err := st.ChapterRecords.Save(items[0].Record); err != nil {
		t.Fatal(err)
	}
	pending := domain.PendingRevision{Stage: domain.RevisionStagePrepared, Items: items, StartedAt: time.Now(), UpdatedAt: time.Now()}
	if err := st.Revisions.SavePending(pending); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(st, nil, "", nil).Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	for chapter := 1; chapter <= 2; chapter++ {
		record, _ := st.ChapterRecords.Load(chapter)
		summary, _ := st.Summaries.LoadSummary(chapter)
		if record == nil || record.Revision != 2 || summary == nil || summary.Summary != fmt.Sprintf("Tóm tắt mới %d", chapter) {
			t.Fatalf("chapter %d not recovered: record=%+v summary=%+v", chapter, record, summary)
		}
	}
}

func TestProjectorFillsCastRoleFromLaterChapter(t *testing.T) {
	st := newRevisionTestStore(t, 2)
	now := time.Now()
	records := []domain.ChapterRecord{
		testRecord(1, "Nội dung một", domain.ChapterFacts{
			Title: "Chương 1", Summary: "Lần đầu gặp chủ quán", Characters: []string{"Chủ quán"}, KeyEvents: []string{"Gặp lần đầu"},
		}, domain.StyleDelta{}, now),
		testRecord(2, "Nội dung hai", domain.ChapterFacts{
			Title: "Chương 2", Summary: "Xác nhận thân phận", Characters: []string{"Chủ quán"}, KeyEvents: []string{"Xác nhận thân phận"},
			CastIntros: []domain.CastIntro{{Name: "Chủ quán", BriefRole: "Chủ quán nhà trọ"}},
		}, domain.StyleDelta{}, now.Add(time.Minute)),
	}
	if err := NewProjector(st).Apply(records); err != nil {
		t.Fatal(err)
	}
	cast, err := st.Cast.Load()
	if err != nil || len(cast) != 1 || cast[0].BriefRole != "Chủ quán nhà trọ" {
		t.Fatalf("Mô tả nhân vật phụ về sau chưa được bổ sung: cast=%+v err=%v", cast, err)
	}
}

func TestServiceRejectsAndClearsStalePreparedAnalysis(t *testing.T) {
	st := newRevisionTestStore(t, 1)
	facts := domain.ChapterFacts{Title: "Chương 1", Summary: "Tóm tắt", KeyEvents: []string{"Sự kiện"}}
	acceptTestChapter(t, st, 1, "Nội dung hệ thống", facts)
	path := filepath.Join(st.Dir(), "chapters", "01.md")
	if err := os.WriteFile(path, []byte("Sửa lần một"), 0o644); err != nil {
		t.Fatal(err)
	}
	base, _ := st.ChapterRecords.Load(1)
	record := testRecord(1, "Sửa lần một", facts, domain.StyleDelta{}, time.Now())
	record.Revision = 2
	pending := domain.PendingRevision{
		Stage: domain.RevisionStagePrepared,
		Items: []domain.PendingRevisionItem{{
			Chapter: 1, BaseSHA256: base.ContentSHA256,
			CurrentSHA256: domain.ChapterContentSHA256("Sửa lần một"), Record: record,
		}},
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := st.Revisions.SavePending(pending); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Sửa lần hai"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(st, nil, "", nil).Sync(context.Background()); err == nil {
		t.Fatal("Sửa nội dung lần nữa sau khi phân tích phải bị từ chối áp dụng")
	}
	if pending, _ := st.Revisions.LoadPending(); pending != nil {
		t.Fatalf("Bản ghi prepared hết hạn phải được dọn: %+v", pending)
	}
}

type revisionModel struct{ response string }

func (m *revisionModel) Capabilities() llm.Capabilities {
	return llm.Capabilities{Structured: llm.StructuredCapabilities{JSONSchema: llm.SupportYes, Strict: llm.SupportYes}}
}

func (m *revisionModel) Generate(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	return &agentcore.LLMResponse{Message: agentcore.Message{
		Role: agentcore.RoleAssistant, Content: []agentcore.ContentBlock{agentcore.TextBlock(m.response)}, StopReason: agentcore.StopReasonStop,
	}}, nil
}

func (m *revisionModel) GenerateStream(context.Context, []agentcore.Message, []agentcore.ToolSpec, ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	return nil, nil
}

func (m *revisionModel) SupportsTools() bool { return true }

type recordingStyleIndex struct {
	chapter int
	text    string
}

func (i *recordingStyleIndex) ChapterCommitted(chapter int, text string) {
	i.chapter, i.text = chapter, text
}

func newRevisionTestStore(t *testing.T, total int) *store.Store {
	t.Helper()
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.Init(total); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.UpdatePhase(domain.PhaseWriting); err != nil {
		t.Fatal(err)
	}
	return st
}

func acceptTestChapter(t *testing.T, st *store.Store, chapter int, content string, facts domain.ChapterFacts) {
	t.Helper()
	if err := st.Drafts.SaveFinalChapter(chapter, content); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ChapterRecords.Accept(chapter, domain.ChapterOriginGenerated, content, facts, domain.StyleDelta{}); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.StartChapter(chapter); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.MarkChapterComplete(chapter, len([]rune(content)), facts.HookType, facts.DominantStrand); err != nil {
		t.Fatal(err)
	}
	if err := st.Summaries.SaveSummary(domain.ChapterSummary{
		Chapter: chapter, Title: facts.Title, Summary: facts.Summary, Characters: facts.Characters, KeyEvents: facts.KeyEvents,
	}); err != nil {
		t.Fatal(err)
	}
}

func testRecord(chapter int, content string, facts domain.ChapterFacts, style domain.StyleDelta, acceptedAt time.Time) domain.ChapterRecord {
	return domain.ChapterRecord{
		Version: domain.ChapterRecordVersion, Chapter: chapter, Revision: 1, Origin: domain.ChapterOriginUser,
		Content: content, ContentSHA256: domain.ChapterContentSHA256(content), Facts: facts, StyleDelta: style, AcceptedAt: acceptedAt,
	}
}

func writeLegacyCommitSession(t *testing.T, dir string, chapter int, facts domain.ChapterFacts) {
	t.Helper()
	args, err := json.Marshal(legacyCommitArgs{Chapter: chapter, ChapterFacts: facts})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	messages := []agentcore.Message{
		{
			Role: agentcore.RoleAssistant, Timestamp: now.Add(-time.Second),
			Content: []agentcore.ContentBlock{agentcore.ToolCallBlock(agentcore.ToolCall{
				ID: "commit-1", Name: "commit_chapter", Args: args,
			})},
		},
		{
			Role: agentcore.RoleTool, Timestamp: now,
			Content:  []agentcore.ContentBlock{agentcore.TextBlock(`{"committed":true}`)},
			Metadata: map[string]any{"tool_call_id": "commit-1", "tool_name": "commit_chapter", "is_error": false},
		},
	}
	var data []byte
	for _, message := range messages {
		line, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	path := filepath.Join(dir, "meta", "sessions", "agents", fmt.Sprintf("writer-ch%02d.jsonl", chapter))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeLegacyImportArtifact(t *testing.T, dir string, chapter int, facts domain.ChapterFacts) {
	t.Helper()
	artifact := struct {
		Payload struct {
			Facts legacyCommitArgs `json:"facts"`
		} `json:"payload"`
	}{}
	artifact.Payload.Facts = legacyCommitArgs{Chapter: chapter, ChapterFacts: facts}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "meta", "import", "analyses", fmt.Sprintf("%06d.json", chapter))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
