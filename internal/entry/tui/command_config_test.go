package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/host"
)

func hubFieldIDs(fields []hubField) []string {
	ids := make([]string, len(fields))
	for i, f := range fields {
		ids[i] = f.id
	}
	return ids
}

func hubFieldIndex(fields []hubField, id string) int {
	for i, field := range fields {
		if field.id == id {
			return i
		}
	}
	return -1
}

// Chọn Provider đã có phải mở hub chi tiết (xem thông tin trước, rồi chỉnh từng mục), chứ không nhảy thẳng sang "đổi protocol".
func TestSelectingProviderOpensHub(t *testing.T) {
	st := &modelConfigState{editModelIdx: -1}
	st.applyProviderChoice(configProviderChoice{existing: &host.ProviderSnapshot{
		Name: "openrouter", BaseURL: "u", HasAPIKey: true,
		Models: []bootstrap.ModelConfig{{Name: "m"}},
	}})
	if st.step != configStepHub {
		t.Fatalf("chọn Provider đã có phải vào hub, được step=%d", st.step)
	}
	ids := hubFieldIDs(st.hubFields())
	// Provider tích hợp sẵn (type rỗng) không trải thêm mục protocol/Endpoint gây nhiễu, nhưng vẫn giữ key/models/save.
	if slices.Contains(ids, "protocol") || slices.Contains(ids, "api") {
		t.Fatalf("hub của provider tích hợp sẵn không nên có protocol/Endpoint, được %v", ids)
	}
	for _, want := range []string{"key", "baseurl", "models", "save"} {
		if !slices.Contains(ids, want) {
			t.Fatalf("hub thiếu %q, được %v", want, ids)
		}
	}
}

// Chỉ hub của Provider tuỳ chỉnh (ghi rõ protocol openai) mới hiển thị protocol và Endpoint.
func TestCustomProviderHubShowsProtocolAndEndpoint(t *testing.T) {
	st := &modelConfigState{editModelIdx: -1}
	st.applyProviderChoice(configProviderChoice{existing: &host.ProviderSnapshot{
		Name: "proxy", Type: "openai", API: "responses", HasAPIKey: true,
		Models: []bootstrap.ModelConfig{{Name: "m"}},
	}})
	ids := hubFieldIDs(st.hubFields())
	if !slices.Contains(ids, "protocol") || !slices.Contains(ids, "api") {
		t.Fatalf("hub provider openai tuỳ chỉnh phải có protocol/Endpoint, được %v", ids)
	}
}

// Esc lùi từng bậc: chỉnh sửa trong dòng ở hub → hub → danh sách Provider → đóng.
func TestEscapeBackHierarchy(t *testing.T) {
	st := &modelConfigState{step: configStepHub, provider: "proxy"}
	st.beginInlineEdit("baseurl")
	m := Model{modelConfig: st}
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEsc})
	if st.step != configStepHub || st.editingField != "" {
		t.Fatalf("Esc ở chỉnh sửa trong dòng phải ở lại hub và huỷ nhập, được step=%d field=%q", st.step, st.editingField)
	}
	if got, ok := st.escapeBack(); !ok || got != configStepProvider {
		t.Fatalf("Esc ở hub phải quay lại danh sách, được %d,%v", got, ok)
	}
	st.step = configStepProvider
	if _, ok := st.escapeBack(); ok {
		t.Fatal("Esc ở danh sách phải đóng cả panel")
	}
}

func TestModelListAddsAndEditsInPlace(t *testing.T) {
	st := &modelConfigState{step: configStepModels, editModelIdx: -1,
		models: []bootstrap.ModelConfig{{Name: "m1"}}, modelOrigins: []string{"m1"}}
	st.cursor = len(st.models)
	m := Model{modelConfig: st}
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEnter})
	if st.step != configStepModels || st.editingField != configModelNameField || !st.addingModel {
		t.Fatalf("khi thêm phải ở lại chỉnh sửa trong dòng của danh sách, step=%d field=%q adding=%v", st.step, st.editingField, st.addingModel)
	}
	st.input.SetValue("  m2  ")
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEnter})
	if st.editingField != configModelWindowField || st.models[1].Name != "m2" {
		t.Fatalf("sau khi gửi tên phải chuyển sang cột cửa sổ ngay trong cùng dòng, field=%q models=%#v", st.editingField, st.models)
	}
	st.input.SetValue("128K")
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEnter})
	if st.editingField != "" || st.addingModel || st.models[1].ContextWindow != 128000 {
		t.Fatalf("model mới chưa hoàn tất trong cùng trang: %#v", st)
	}
}

func TestModelListEditsSelectedCellAndCancels(t *testing.T) {
	st := &modelConfigState{step: configStepModels, editModelIdx: -1,
		models: []bootstrap.ModelConfig{{Name: "m1", ContextWindow: 1000}}, modelOrigins: []string{"m1"}}
	m := Model{modelConfig: st}
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEnter})
	if st.editingField != configModelWindowField || st.step != configStepModels {
		t.Fatalf("Enter ở cột phải phải chỉnh sửa cửa sổ trong dòng, step=%d field=%q", st.step, st.editingField)
	}
	st.input.SetValue("200K")
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEsc})
	if st.editingField != "" || st.models[0].ContextWindow != 1000 {
		t.Fatalf("Esc phải huỷ ô đang chọn và không đổi giá trị: %#v", st.models[0])
	}
}

func TestModelRenameProducesExplicitDraftAndReferenceNotice(t *testing.T) {
	st := &modelConfigState{
		step: configStepModels, provider: "proxy", models: []bootstrap.ModelConfig{{Name: "old"}},
		modelOrigins: []string{"old"}, snapshot: host.ModelConfigurationSnapshot{
			References: map[string][]string{"proxy\x00old": {"default", "writer fallback[0]"}},
		},
	}
	m := Model{modelConfig: st}
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEnter})
	st.input.SetValue("renamed")
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEnter})
	draft := st.draft()
	if len(draft.Renames) != 1 || draft.Renames[0] != (host.ModelRename{From: "old", To: "renamed"}) {
		t.Fatalf("đổi tên model phải giữ quan hệ định danh tường minh, renames=%#v", draft.Renames)
	}
	if !strings.Contains(st.message, "đồng bộ") || !strings.Contains(st.message, "default") {
		t.Fatalf("đổi tên model đang được tham chiếu phải nói rõ hành vi khi lưu, message=%q", st.message)
	}
}

func TestModelListRendersEditableColumnsAndReferences(t *testing.T) {
	st := &modelConfigState{
		step: configStepModels, provider: "proxy", models: []bootstrap.ModelConfig{{Name: "deepseek-chat", ContextWindow: 128000}},
		modelOrigins: []string{"deepseek-chat"}, snapshot: host.ModelConfigurationSnapshot{
			References: map[string][]string{"proxy\x00deepseek-chat": {"default"}},
		},
	}
	plain := ansi.Strip(renderModelConfigModal(120, st))
	for _, want := range []string{"Tên/ID Model", "Cửa sổ ngữ cảnh", "Vai trò gán", "deepseek-chat", "128K", "default", "+ Thêm Model mới"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("bảng model một trang thiếu %q:\n%s", want, plain)
		}
	}
}

func TestModelNameInlineEditorKeepsModalRowsIntact(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(oldProfile) })
	st := &modelConfigState{
		step: configStepModels, provider: "deepseek",
		models:       []bootstrap.ModelConfig{{Name: "deepseek-v4-pro"}, {Name: "deepseek-v4-flash"}},
		modelOrigins: []string{"deepseek-v4-pro", "deepseek-v4-flash"},
	}
	st.beginModelEdit(0, 0)
	view := renderModelConfigModal(120, st)
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if width := lipgloss.Width(line); width != 72 {
			t.Fatalf("chỉnh sửa tên model trong dòng phá vỡ bề rộng dòng %d: width=%d line=%q\n%s", i, width, ansi.Strip(line), ansi.Strip(view))
		}
	}
	if len(lines) != 7 {
		t.Fatalf("chỉnh sửa trong dòng không được tạo xuống dòng vật lý, được %d dòng:\n%s", len(lines), ansi.Strip(view))
	}
}

func TestRenamedReferencedModelStillCannotBeDeleted(t *testing.T) {
	st := &modelConfigState{
		provider: "proxy", currentModel: "old", models: []bootstrap.ModelConfig{{Name: "renamed"}},
		modelOrigins: []string{"old"}, snapshot: host.ModelConfigurationSnapshot{
			References: map[string][]string{"proxy\x00old": {"default"}},
		},
	}
	if st.deleteModel(0) || len(st.models) != 1 || !strings.Contains(st.message, "đang được sử dụng") {
		t.Fatalf("khi tên mới chưa lưu vẫn phải bảo vệ việc xoá theo định danh gốc, models=%#v message=%q", st.models, st.message)
	}
}

func TestCancellingNewModelNameRemovesTemporaryRow(t *testing.T) {
	st := &modelConfigState{step: configStepModels, models: []bootstrap.ModelConfig{{Name: "m1"}}, modelOrigins: []string{"m1"}}
	st.cursor = 1
	m := Model{modelConfig: st}
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEsc})
	if len(st.models) != 1 || len(st.modelOrigins) != 1 || st.cursor != 1 {
		t.Fatalf("huỷ thêm phải dọn dòng tạm, models=%#v origins=%#v cursor=%d", st.models, st.modelOrigins, st.cursor)
	}
}

func TestParseContextWindowInput(t *testing.T) {
	cases := map[string]int{
		"": 0, "0": 0, "auto": 0, "128K": 128000, "1M": 1000000,
		"1.5m": 1500000, "200000": 200000,
	}
	for input, want := range cases {
		got, err := parseContextWindowInput(input)
		if err != nil || got != want {
			t.Errorf("parseContextWindowInput(%q) = %d, %v; want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"-1", "abc", "0.5"} {
		if _, err := parseContextWindowInput(input); err == nil {
			t.Errorf("parseContextWindowInput(%q) should fail", input)
		}
	}
}

func TestModelConfigModalDoesNotRenderAPIKey(t *testing.T) {
	state := &modelConfigState{step: configStepHub, provider: "proxy", apiKeyOptional: true}
	state.beginInlineEdit("key")
	state.input.SetValue("sk-super-secret")
	view := renderModelConfigModal(120, state)
	if strings.Contains(view, "sk-super-secret") {
		t.Fatal("API key leaked into rendered modal")
	}
}

func TestProviderHubEditsAPIKeyInlineAndTrims(t *testing.T) {
	state := &modelConfigState{step: configStepHub, provider: "proxy", existing: true,
		hasAPIKey: true, apiKeyHint: "sk-o******7890", apiKeyOptional: true, apiKeyAction: host.APIKeyKeep}
	state.cursor = hubFieldIndex(state.hubFields(), "key")
	m := Model{modelConfig: state}
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEnter})
	if state.step != configStepHub || state.editingField != "key" {
		t.Fatalf("API Key phải được chỉnh sửa ngay tại dòng cũ trong hub, được step=%d field=%q", state.step, state.editingField)
	}
	state.input.SetValue("  sk-new-secret-1234567890  ")
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEnter})
	if state.editingField != "" || state.apiKey != "sk-new-secret-1234567890" || state.apiKeyAction != host.APIKeyReplace {
		t.Fatalf("kết quả gửi API Key trong dòng bị sai: field=%q key=%q action=%q", state.editingField, state.apiKey, state.apiKeyAction)
	}
	if got := state.keyStatus(); got != "sk-n******7890" {
		t.Fatalf("API Key mới phải hiện gợi ý đã che mờ, được %q", got)
	}
}

func TestProviderHubEditsBaseURLInlineAndKeepsLongTailVisible(t *testing.T) {
	state := &modelConfigState{step: configStepHub, provider: "proxy", existing: true,
		apiKeyOptional: true, baseURL: "https://old.example/v1"}
	state.cursor = hubFieldIndex(state.hubFields(), "baseurl")
	m := Model{modelConfig: state}
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEnter})
	if state.editingField != "baseurl" || state.input.Value() != "https://old.example/v1" {
		t.Fatalf("Base URL phải được điền sẵn để chỉnh sửa ngay tại dòng cũ, field=%q value=%q", state.editingField, state.input.Value())
	}
	state.input.SetValue("  https://example.com/a/very/long/provider/path/UNIQUE-END  ")
	state.input.CursorEnd()
	view := renderModelConfigModal(76, state)
	if !strings.Contains(view, "UNIQUE-END") {
		t.Fatalf("khi sửa Base URL dài phải thấy phần đuôi gần con trỏ:\n%s", view)
	}
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEnter})
	if state.baseURL != "https://example.com/a/very/long/provider/path/UNIQUE-END" {
		t.Fatalf("Base URL chưa được TrimSpace, được %q", state.baseURL)
	}
}

func TestSaveConfigHighlightsOnlyWhenDirty(t *testing.T) {
	state := &modelConfigState{editModelIdx: -1}
	state.applyProviderChoice(configProviderChoice{existing: &host.ProviderSnapshot{
		Name: "proxy", Type: "openai", BaseURL: "https://old.example/v1", HasAPIKey: true,
		APIKeyHint: "sk-o******7890", Models: []bootstrap.ModelConfig{{Name: "m1"}},
	}})
	if state.isDirty() {
		t.Fatal("vừa vào Provider đã có không nên bị đánh dấu đã sửa")
	}
	state.baseURL = "https://new.example/v1"
	if !state.isDirty() {
		t.Fatal("sau khi Base URL đổi phải đánh dấu đã sửa")
	}
	state.baseURL = "https://old.example/v1"
	if state.isDirty() {
		t.Fatal("đổi về lại giá trị gốc thì phải tự trở về trạng thái chưa sửa")
	}
	state.beginInlineEdit("baseurl")
	state.input.SetValue("https://editing.example/v1")
	if !state.isDirty() {
		t.Fatal("khi đang nhập giá trị mới cho Base URL phải đánh dấu đã sửa theo thời gian thực")
	}
	state.input.SetValue(" https://old.example/v1 ")
	if state.isDirty() {
		t.Fatal("nhập trong dòng tương đương giá trị gốc không nên báo nhầm đã sửa")
	}
	state.editingField = ""
	state.apiKeyAction = host.APIKeyReplace
	state.apiKey = "sk-new-secret"
	if !state.isDirty() {
		t.Fatal("sau khi thay API Key phải đánh dấu đã sửa")
	}

	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(oldProfile) })
	lines := renderProviderHubFields(state, 68)
	want := lipgloss.NewStyle().Foreground(colorSuccess).Render("Lưu cấu hình")
	found := false
	for _, line := range lines {
		if strings.Contains(line, want) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("khi có thay đổi, mục lưu phải dùng màu thành công, lines=%q", lines)
	}

	newProvider := &modelConfigState{}
	if !newProvider.isDirty() {
		t.Fatal("Provider mới luôn được coi là thay đổi chưa lưu")
	}
}

func TestStyledBaseURLLineKeepsANSIAndFillsModalWidth(t *testing.T) {
	plain := "› Base URL  https://api.deepseek.com"
	styled := "\x1b[38;2;255;200;0m› \x1b[0m" +
		"\x1b[1;38;2;255;200;0mBase URL\x1b[0m  " +
		"\x1b[4;38;2;220;220;220mhttps://api.deepseek.com\x1b[0m"
	if got := ansi.Strip(truncateStyledWidth(styled, 56)); got != plain {
		t.Fatalf("cắt có nhận biết ANSI đã phá vỡ dòng nhập: %q", got)
	}

	modal := renderPaddedModalFrame(60, 3, "/config", "", []string{styled})
	lines := strings.Split(modal, "\n")
	if len(lines) != 3 || lipgloss.Width(lines[1]) != 60 {
		t.Fatalf("dòng nhập trong lớp phủ không lấp đầy bề rộng cố định: width=%d\n%s", lipgloss.Width(lines[1]), modal)
	}
	if !strings.Contains(ansi.Strip(lines[1]), "https://api.deepseek.com") {
		t.Fatalf("lớp phủ mất Base URL:\n%s", modal)
	}
}

func TestProviderHubDeleteClearsOnlyOptionalAPIKey(t *testing.T) {
	optional := &modelConfigState{step: configStepHub, provider: "proxy", providerType: "openai",
		hasAPIKey: true, apiKeyHint: "sk-o******7890", apiKeyOptional: true, apiKeyAction: host.APIKeyKeep}
	optional.cursor = hubFieldIndex(optional.hubFields(), "key")
	m := Model{modelConfig: optional}
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyDelete})
	if optional.apiKeyAction != host.APIKeyClear || optional.keyStatus() != "Đã xóa" {
		t.Fatalf("Delete trên Key tuỳ chọn phải đánh dấu xoá, action=%q status=%q", optional.apiKeyAction, optional.keyStatus())
	}

	required := &modelConfigState{step: configStepHub, provider: "openrouter",
		hasAPIKey: true, apiKeyHint: "sk-o******7890", apiKeyOptional: false, apiKeyAction: host.APIKeyKeep}
	required.cursor = hubFieldIndex(required.hubFields(), "key")
	m = Model{modelConfig: required}
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyDelete})
	if required.apiKeyAction != host.APIKeyKeep || !strings.Contains(required.message, "không thể xóa") {
		t.Fatalf("Key bắt buộc không nên bị xoá, action=%q message=%q", required.apiKeyAction, required.message)
	}
}

func TestConfigTextInputSupportsCursorEditing(t *testing.T) {
	state := &modelConfigState{step: configStepCustomName}
	state.startTextInput("ac", "Tên Provider", false)
	state.input.SetCursor(1)
	m := Model{modelConfig: state}
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if got := state.input.Value(); got != "abc" {
		t.Fatalf("ô nhập thống nhất phải hỗ trợ chèn tại con trỏ, được %q", got)
	}
}

func TestProviderHubShowsConfigPathAndConnectionAction(t *testing.T) {
	state := &modelConfigState{
		step: configStepHub, provider: "proxy", apiKeyOptional: true, currentModel: "m2",
		models:   []bootstrap.ModelConfig{{Name: "m1"}, {Name: "m2"}},
		snapshot: host.ModelConfigurationSnapshot{ConfigPath: `C:\work\.ainovel\config.json`},
	}
	fields := state.hubFields()
	idx := hubFieldIndex(fields, "test")
	if idx < 0 || fields[idx].value != "m2" {
		t.Fatalf("kiểm tra kết nối phải ưu tiên model hiện tại, fields=%#v", fields)
	}
	view := renderModelConfigModal(120, state)
	for _, want := range []string{"Cấu hình nâng cao", "extra_body"} {
		if !strings.Contains(view, want) {
			t.Fatalf("hub cấu hình thiếu %q:\n%s", want, view)
		}
	}
	compact := strings.NewReplacer("\r", "", "\n", "", " ", "", "│", "").Replace(view)
	if !strings.Contains(compact, `C:\work\.ainovel\config.json`) {
		t.Fatalf("hub cấu hình chưa hiện đầy đủ đường dẫn cấu hình:\n%s", view)
	}
}

func TestModelConfigMessageWrapKeepsErrorTail(t *testing.T) {
	state := &modelConfigState{step: configStepHub, provider: "proxy", apiKeyOptional: true,
		message: "Kết nối thất bại: " + strings.Repeat("máy chủ trả về thông báo lỗi rất dài", 8) + " UNIQUE-ERROR-TAIL"}
	view := renderModelConfigModal(64, state)
	compact := strings.NewReplacer("\r", "", "\n", "", " ", "", "│", "").Replace(view)
	if !strings.Contains(compact, "UNIQUE-ERROR-TAIL") {
		t.Fatalf("lỗi dài không nên cắt mất phần đuôi:\n%s", view)
	}
}

func TestConnectionActionStartsAsyncTestWithoutLeavingHub(t *testing.T) {
	state := &modelConfigState{step: configStepHub, provider: "proxy", providerType: "openai",
		apiKeyOptional: true, models: []bootstrap.ModelConfig{{Name: "m1"}}}
	state.cursor = hubFieldIndex(state.hubFields(), "test")
	m := Model{modelConfig: state}
	_, cmd := m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !state.testing || state.step != configStepHub {
		t.Fatalf("kiểm tra kết nối phải chạy bất đồng bộ và ở lại hub, cmd=%v testing=%v step=%d", cmd != nil, state.testing, state.step)
	}
}

func TestConnectionTestCanBeCancelled(t *testing.T) {
	cancelled := false
	state := &modelConfigState{step: configStepHub, provider: "proxy", testing: true,
		testCancel: func() { cancelled = true }}
	m := Model{modelConfig: state}
	m.handleModelConfigKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled || !state.testing || state.message != "Đang hủy kiểm tra kết nối..." {
		t.Fatalf("Esc phải huỷ kiểm tra đang chạy và chờ kết quả, cancelled=%v testing=%v message=%q", cancelled, state.testing, state.message)
	}

	updated, _, handled := m.handleRuntimeMsg(modelConfigConnectionMsg{err: context.Canceled})
	m = updated.(Model)
	if !handled || m.modelConfig.testing || m.modelConfig.message != "Kiểm tra kết nối đã bị hủy" {
		t.Fatalf("kết quả huỷ chưa hội tụ đúng: handled=%v testing=%v message=%q", handled, m.modelConfig.testing, m.modelConfig.message)
	}
}

func TestConfigCommandIsRegistered(t *testing.T) {
	spec, ok := commandRegistryInstance().Find("config")
	if !ok {
		t.Fatal("/config is not registered")
	}
	if spec.Usage != "/config" || !spec.AutoExecute {
		t.Fatalf("config spec = %#v", spec)
	}
}

func TestModelSwitchLabelIncludesContextWindow(t *testing.T) {
	state := modelSwitchState{models: []host.ConfiguredModel{{Name: "gpt-test", ContextWindow: 400000}}}
	if got := state.modelLabel(); got != "gpt-test · 400K" {
		t.Fatalf("modelLabel = %q", got)
	}
}

// Đồng nhất với /model: /config được render thành lớp phủ có khung theo chiều cao nội dung (không còn giãn thành lớp che căn giữa chiếm 3/4 màn hình).
func TestModelConfigModalIsCompactOverlay(t *testing.T) {
	state := &modelConfigState{step: configStepProvider, providerChoices: []configProviderChoice{
		{label: "Sửa openrouter", existing: &host.ProviderSnapshot{Name: "openrouter"}},
		{label: "+ Thêm Provider mới…", add: true},
	}}
	lines := strings.Split(renderModelConfigModal(120, state), "\n")

	// 1 dòng tiêu đề + 2 tuỳ chọn + viền trên dưới = 5 dòng; chiều cao bám theo nội dung, không phồng lên vì chiều cao màn hình.
	if len(lines) != 5 {
		t.Fatalf("lớp phủ gọn phải có 5 dòng (theo chiều cao nội dung), được %d dòng:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[0], "┌") || !strings.Contains(lines[0], "/config") {
		t.Fatalf("dòng đầu phải là viền trên có tiêu đề /config, được %q", lines[0])
	}
	if !strings.Contains(lines[len(lines)-1], "└") {
		t.Fatalf("dòng cuối phải là viền dưới, được %q", lines[len(lines)-1])
	}
}

// Menu cấp 1 chỉ liệt kê "sửa mục có sẵn + lối vào thêm mới", không trải toàn bộ danh mục Provider tích hợp; danh mục chỉ xuất hiện ở menu cấp 2.
func TestProviderMenuIsTwoLevel(t *testing.T) {
	state := &modelConfigState{snapshot: host.ModelConfigurationSnapshot{
		Providers:       []host.ProviderSnapshot{{Name: "openrouter"}, {Name: "anthropic"}},
		DefaultProvider: "openrouter",
	}}
	state.buildProviderMenus()

	// Cấp 1 = 2 mục sửa + 1 lối vào thêm mới; mục cuối là "thêm mới", không lẫn mục add/preset nào khác.
	if len(state.providerChoices) != 3 {
		t.Fatalf("menu cấp 1 phải có 2 mục sửa + 1 mục thêm, được %d mục", len(state.providerChoices))
	}
	if !state.providerChoices[len(state.providerChoices)-1].add {
		t.Fatal("mục cuối menu cấp 1 phải là lối vào \"Thêm Provider mới…\"")
	}
	for i, c := range state.providerChoices[:2] {
		if c.existing == nil || c.add {
			t.Fatalf("mục %d của menu cấp 1 phải là sửa Provider có sẵn, được %#v", i, c)
		}
	}

	// Cấp 2 = danh mục có thể thêm: không rỗng, và các mục tích hợp đã cấu hình (openrouter/anthropic) không lặp lại.
	if len(state.presetChoices) == 0 {
		t.Fatal("menu cấp 2 phải liệt kê danh mục Provider có thể thêm")
	}
	if len(state.presetChoices) >= len(bootstrap.ProviderPresets()) {
		t.Fatalf("Provider tích hợp đã cấu hình phải bị loại khỏi danh mục thêm mới, presets=%d tổng=%d",
			len(state.presetChoices), len(bootstrap.ProviderPresets()))
	}
	for _, c := range state.presetChoices {
		if c.preset != nil && (c.preset.Name == "openrouter" || c.preset.Name == "anthropic") {
			t.Fatalf("danh mục thêm mới không nên chứa %q đã cấu hình", c.preset.Name)
		}
	}
}
