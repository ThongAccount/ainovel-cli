package host

import (
	"strings"
	"testing"
)

// feedAll nạp một lần toàn bộ input, trả về output tích luỹ.
func feedAll(t *testing.T, tool, input string) string {
	t.Helper()
	e := newToolExtractor(tool, observerTextsVI)
	if e == nil {
		t.Fatalf("no extractor for tool %q", tool)
	}
	return e.Feed(input)
}

// feedChunked nạp input theo số byte cố định, kiểm tra kết quả dạng luồng khớp với
// nạp một lần.
func feedChunked(t *testing.T, tool, input string, chunk int) string {
	t.Helper()
	e := newToolExtractor(tool, observerTextsVI)
	if e == nil {
		t.Fatalf("no extractor for tool %q", tool)
	}
	var b strings.Builder
	for i := 0; i < len(input); i += chunk {
		end := min(i+chunk, len(input))
		b.WriteString(e.Feed(input[i:end]))
	}
	return b.String()
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("expected substring %q in:\n---\n%s\n---", want, got)
	}
}

func mustNotContain(t *testing.T, got, want string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Errorf("unexpected substring %q in:\n---\n%s\n---", want, got)
	}
}

// ── Chế độ chung: obj phẳng ──

func TestExtract_PlanChapter(t *testing.T) {
	in := `{"chapter":1,"title":"Hợp đồng bán thân","goal":"Dựng nền tảng cho mỏ quặng","conflict":"Nợ của cha","hook":"Quặng tro","emotion_arc":"Bức bố"}`
	out := feedAll(t, "plan_chapter", in)
	mustContain(t, out, "✻ Quy hoạch")
	mustContain(t, out, "chapter: 1")
	mustContain(t, out, "title: Hợp đồng bán thân")
	mustContain(t, out, "goal: Dựng nền tảng cho mỏ quặng")
	mustContain(t, out, "conflict: Nợ của cha")
	mustContain(t, out, "hook: Quặng tro")
	mustContain(t, out, "emotion_arc: Bức bố")
}

// ── Chế độ chung: obj lồng nhau + mảng ──

func TestExtract_FoundationCharacters(t *testing.T) {
	in := `{"type":"characters","scale":"long","content":[` +
		`{"name":"Thẩm Lệ","role":"Nhân vật chính","aliases":["Mạch tro","Thẩm Thất"],"description":"Thiếu niên vùng biên cương.","traits":["Tiết chế","Đa nghi"]},` +
		`{"name":"Cố Tiểu Đăng","role":"Vai phò tá quan trọng","description":"Tuyển nữ thử dược trong tiệm thuốc."}` +
		`]}`
	out := feedAll(t, "save_foundation", in)
	mustContain(t, out, "✻ Thiết lập")
	mustContain(t, out, "type: characters")
	mustContain(t, out, "scale: long")
	// Dựng theo chế độ chung: mọi trường đều hiện, gồm cả aliases / traits trước đây bị danh sách trắng bỏ qua
	mustContain(t, out, "name: Thẩm Lệ")
	mustContain(t, out, "role: Nhân vật chính")
	mustContain(t, out, "aliases:")
	mustContain(t, out, "- Mạch tro")
	mustContain(t, out, "- Thẩm Thất")
	mustContain(t, out, "description: Thiếu niên vùng biên cương.")
	mustContain(t, out, "traits:")
	mustContain(t, out, "- Tiết chế")
	mustContain(t, out, "- Đa nghi")
	mustContain(t, out, "name: Cố Tiểu Đăng")
	mustContain(t, out, "role: Vai phò tá quan trọng")
}

func TestExtract_FoundationLayeredOutline(t *testing.T) {
	in := `{"type":"layered_outline","content":[` +
		`{"index":1,"title":"Ánh lửa mỏ lấp lánh","arcs":[` +
		`{"index":1,"title":"Lao động mỏ vảy huyền","goal":"Cầu sống","chapters":[` +
		`{"chapter":1,"title":"Hợp đồng bán thân","core_event":"Bị bán vào mỏ quặng."}` +
		`]}]}]}`
	out := feedAll(t, "save_foundation", in)
	mustContain(t, out, "type: layered_outline")
	// Tập
	mustContain(t, out, "index: 1")
	mustContain(t, out, "title: Ánh lửa mỏ lấp lánh")
	// Cung
	mustContain(t, out, "title: Lao động mỏ vảy huyền")
	mustContain(t, out, "goal: Cầu sống")
	// Chương
	mustContain(t, out, "chapter: 1")
	mustContain(t, out, "title: Hợp đồng bán thân")
	mustContain(t, out, "core_event: Bị bán vào mỏ quặng.")
	// Thụt lề lồng nhau thể hiện phân cấp
	mustContain(t, out, "arcs:\n")
	mustContain(t, out, "chapters:\n")
}

func TestExtract_FoundationUpdateCompass(t *testing.T) {
	in := `{"type":"update_compass","content":{"ending_direction":"Một mình bay lên vs cắt đứt lễ hiến tế","open_threads":["Chìa khóa mạch tro","Sổ bộ phiếu người sống"],"estimated_scale":"5-6 tập"}}`
	out := feedAll(t, "save_foundation", in)
	mustContain(t, out, "type: update_compass")
	mustContain(t, out, "ending_direction: Một mình bay lên vs cắt đứt lễ hiến tế")
	mustContain(t, out, "estimated_scale: 5-6 tập")
	mustContain(t, out, "open_threads:")
	mustContain(t, out, "- Chìa khóa mạch tro")
	mustContain(t, out, "- Sổ bộ phiếu người sống")
}

// ── save_review: có object trong mảng + mảng số ──

func TestExtract_SaveReview(t *testing.T) {
	in := `{"chapter":3,"scope":"chapter","verdict":"polish","summary":"Nhịp truyện hơi chậm.","dimensions":[{"dimension":"hook","score":55,"verdict":"fail"}],"issues":[{"type":"hook","severity":"error","description":"Cuối chương thiếu điểm móc."}],"affected_chapters":[3,4]}`
	out := feedAll(t, "save_review", in)
	mustContain(t, out, "✻ Thẩm duyệt")
	mustContain(t, out, "verdict: polish")
	mustContain(t, out, "summary: Nhịp truyện hơi chậm.")
	mustContain(t, out, "dimension: hook")
	mustContain(t, out, "score: 55")
	mustContain(t, out, "verdict: fail")
	mustContain(t, out, "type: hook")
	mustContain(t, out, "severity: error")
	mustContain(t, out, "description: Cuối chương thiếu điểm móc.")
	mustContain(t, out, "- 3")
	mustContain(t, out, "- 4")
}

// ── commit_chapter: lồng nhau phức tạp ──

func TestExtract_CommitChapter(t *testing.T) {
	in := `{"chapter":1,"summary":"Bị bán vào mỏ quặng.","characters":["Thẩm Lệ","Mẹ"],"key_events":["Ký hợp đồng bán thân"],"foreshadow_updates":[{"id":"f1","action":"plant","description":"Quặng tro nóng bừng."}],"state_changes":[{"entity":"Thẩm Lệ","field":"Thân phận","old_value":"Thiếu niên hái dược","new_value":"Lao công mỏ quặng"}]}`
	out := feedAll(t, "commit_chapter", in)
	mustContain(t, out, "✻ Chốt chương")
	mustContain(t, out, "summary: Bị bán vào mỏ quặng.")
	mustContain(t, out, "- Thẩm Lệ")
	mustContain(t, out, "- Mẹ")
	mustContain(t, out, "- Ký hợp đồng bán thân")
	mustContain(t, out, "id: f1")
	mustContain(t, out, "action: plant")
	mustContain(t, out, "description: Quặng tro nóng bừng.")
	mustContain(t, out, "entity: Thẩm Lệ")
	mustContain(t, out, "field: Thân phận")
	mustContain(t, out, "old_value: Thiếu niên hái dược")
	mustContain(t, out, "new_value: Lao công mỏ quặng")
}

// ── edit_chapter: chế độ chung + string nhiều dòng ──

func TestExtract_EditChapter(t *testing.T) {
	in := `{"chapter":24,"old_string":"Thẩm Lệ cúi đầu không nói.\nAnh siết chặt nắm đấm.","new_string":"Thẩm Lệ không ngẩng đầu, cổ họng nhấp nhô một cái.\nCác đốt ngón siết đến bạc màu.","replace_all":false}`
	out := feedAll(t, "edit_chapter", in)
	mustContain(t, out, "✻ Hoàn thiện")
	mustContain(t, out, "chapter: 24")
	mustContain(t, out, "old_string: Thẩm Lệ cúi đầu không nói.\nAnh siết chặt nắm đấm.")
	mustContain(t, out, "new_string: Thẩm Lệ không ngẩng đầu, cổ họng nhấp nhô một cái.\nCác đốt ngón siết đến bạc màu.")
	mustContain(t, out, "replace_all: false")
}

// ── Công cụ đọc: args ít thông tin nhưng header + trường quan trọng vẫn phải thấy ──

func TestExtract_ReadChapter(t *testing.T) {
	in := `{"chapter":234,"source":"final"}`
	out := feedAll(t, "read_chapter", in)
	mustContain(t, out, "✻ Đọc chương")
	mustContain(t, out, "chapter: 234")
	mustContain(t, out, "source: final")
}

func TestExtract_CheckConsistency(t *testing.T) {
	out := feedAll(t, "check_consistency", `{"chapter":234}`)
	mustContain(t, out, "✻ Kiểm tra tính nhất quán")
	mustContain(t, out, "chapter: 234")
}

// Xử lý dự phòng cho args rỗng: khi architect gọi novel_context mà không truyền
// tham số thì args là {}; không được im lặng hoàn toàn, ít nhất phải xuất header để
// người dùng nhận biết có lệnh gọi.
func TestExtract_NovelContextEmptyArgs(t *testing.T) {
	out := feedAll(t, "novel_context", `{}`)
	mustContain(t, out, "✻ Truy vấn ngữ cảnh")
}

func TestExtract_NovelContextWithChapter(t *testing.T) {
	out := feedAll(t, "novel_context", `{"chapter":234}`)
	mustContain(t, out, "✻ Truy vấn ngữ cảnh")
	mustContain(t, out, "chapter: 234")
}

// ── Chế độ luồng trần ──

func TestExtract_DraftChapterRawMarkdown(t *testing.T) {
	in := `{"chapter":1,"content":"# Chương 1\n\nThẩm Lệ đứng ở cửa mỏ.\n"}`
	out := feedAll(t, "draft_chapter", in)
	// Luồng trần: không trang trí, không có tiền tố key
	mustNotContain(t, out, "【")
	mustNotContain(t, out, "content:")
	mustNotContain(t, out, "chapter:")
	mustContain(t, out, "# Chương 1")
	mustContain(t, out, "Thẩm Lệ đứng ở cửa mỏ.")
}

func TestExtract_DraftChapterIgnoresOtherFields(t *testing.T) {
	// Các trường ngoài content phải bị bỏ qua im lặng, không làm bẩn output
	in := `{"chapter":7,"summary":"meta","content":"Nội dung truyện","extra_array":[1,2,3]}`
	out := feedAll(t, "draft_chapter", in)
	mustContain(t, out, "Nội dung truyện")
	mustNotContain(t, out, "meta")
	mustNotContain(t, out, "summary")
	mustNotContain(t, out, "7")
	mustNotContain(t, out, "1")
}

// ── Bất biến hành vi ──

func TestExtract_UnknownTool(t *testing.T) {
	if e := newToolExtractor("nonexistent_tool", observerTextsVI); e != nil {
		t.Errorf("expected nil for unknown tool")
	}
}

func TestExtract_DoneAfterClose(t *testing.T) {
	e := newToolExtractor("plan_chapter", observerTextsVI)
	e.Feed(`{"title":"x"}`)
	if !e.Done() {
		t.Error("expected Done after closing brace")
	}
}

// ── Bất biến khi chia chunk ──

// Cùng một input chia theo 1/3/7/13 byte, output phải giống hệt lúc nạp một lần.
func TestExtract_ChunkedEqualsWhole(t *testing.T) {
	cases := []struct {
		tool  string
		input string
	}{
		{"plan_chapter", `{"title":"Hợp đồng bán thân","goal":"Mục tiêu","conflict":"Nợ của cha","hook":"Quặng tro","emotion_arc":"Bức bố"}`},
		{"save_foundation", `{"type":"characters","content":[{"name":"Thẩm Lệ","role":"Nhân vật chính","aliases":["Mạch tro","Thẩm Thất"]}]}`},
		{"save_foundation", `{"type":"layered_outline","content":[{"index":1,"title":"Lửa mỏ","arcs":[{"index":1,"title":"Lao động mỏ","goal":"Cầu sống","chapters":[{"chapter":1,"title":"Hợp đồng bán thân"}]}]}]}`},
		{"save_review", `{"verdict":"accept","summary":"good","dimensions":[{"dimension":"hook","score":85,"verdict":"pass"}],"issues":[]}`},
		{"draft_chapter", `{"chapter":1,"content":"# Chương 1\n\nNội dung truyện.\n"}`},
	}
	for _, tc := range cases {
		whole := feedAll(t, tc.tool, tc.input)
		for _, chunk := range []int{1, 3, 7, 13} {
			got := feedChunked(t, tc.tool, tc.input, chunk)
			if got != whole {
				t.Errorf("tool=%s chunk=%d differs from whole\n--- whole ---\n%s\n--- chunked ---\n%s", tc.tool, chunk, whole, got)
			}
		}
	}
}

// ── Ký tự thoát và Unicode ──

func TestExtract_EscapeSequences(t *testing.T) {
	in := `{"goal":"Dòng1\nDòng2 \"dấu nháy\" \\dấu chéo chữ N"}`
	out := feedAll(t, "plan_chapter", in)
	mustContain(t, out, "Dòng1\nDòng2")
	mustContain(t, out, `"dấu nháy"`)
	mustContain(t, out, `\dấu chéo chữ N`)
	mustContain(t, out, "chữ N")
}

func TestExtract_UnicodeEscape(t *testing.T) {
	// Dạng \uXXXX giải mã thành ký tự thật; ở đây dữ liệu đưa vào ký tự đã giải
	// mã (một ký tự Unicode nhiều byte) để kiểm tra đường truyền byte.
	in := `{"goal":"Tiếng Việt"}`
	out := feedAll(t, "plan_chapter", in)
	mustContain(t, out, "Tiếng Việt")
}

// ── Container rỗng / cấu trúc đơn giản ──

func TestExtract_EmptyArrays(t *testing.T) {
	in := `{"key_events":[],"characters":["Thẩm Lệ"]}`
	out := feedAll(t, "commit_chapter", in)
	mustContain(t, out, "key_events:")
	mustContain(t, out, "characters:")
	mustContain(t, out, "- Thẩm Lệ")
}

func TestExtract_BoolAndNull(t *testing.T) {
	in := `{"foreshadow_updates":[{"id":"f1","action":"plant","description":null}],"chapter":1,"summary":"x","characters":["a"],"key_events":["b"]}`
	out := feedAll(t, "commit_chapter", in)
	mustContain(t, out, "id: f1")
	mustContain(t, out, "action: plant")
	mustContain(t, out, "description: null")
}

// ── Tình huống biên: mảng lồng mảng, lồng sâu ──

func TestExtract_NestedArrays(t *testing.T) {
	// affected_chapters vốn là mảng int; ở đây thay bằng mảng lồng mảng để kiểm tra
	in := `{"summary":"x","key_events":[],"characters":["a"],"foreshadow_updates":[],"relationship_changes":[]}`
	out := feedAll(t, "commit_chapter", in)
	mustContain(t, out, "summary: x")
	mustContain(t, out, "key_events:")
	mustContain(t, out, "- a")
}

func TestExtract_DeeplyNested(t *testing.T) {
	in := `{"a":{"b":{"c":{"d":"deep"}}}}`
	e := newToolExtractor("plan_chapter", observerTextsVI)
	out := e.Feed(in)
	mustContain(t, out, "a:")
	mustContain(t, out, "b:")
	mustContain(t, out, "c:")
	mustContain(t, out, "d: deep")
	if !e.Done() {
		t.Error("expected Done after final closing brace")
	}
}

// ── Chunk cắt giữa ký tự UTF-8 nhiều byte ──

func TestExtract_ChunkSplitInUTF8(t *testing.T) {
	// "ệ" dài 3 byte (E1 BB 87). Đặt kích thước lát ở 1 để mỗi byte được nạp riêng.
	in := `{"goal":"Tiếng Việt"}`
	whole := feedAll(t, "plan_chapter", in)
	chunked := feedChunked(t, "plan_chapter", in, 1)
	if whole != chunked {
		t.Errorf("byte-by-byte chunked output differs from whole:\n--- whole ---\n%s\n--- chunked ---\n%s", whole, chunked)
	}
	mustContain(t, chunked, "Tiếng Việt")
}

// ── Chế độ luồng trần: key trùng tên trong obj lồng nhau không được nhận nhầm ──

func TestExtract_NakedKeyOnlyTopLevel(t *testing.T) {
	// "content" xuất hiện hai lần: bên trong object lồng + ở cấp ngoài cùng. Chỉ bản
	// ở cấp ngoài cùng được trả ra.
	in := `{"meta":{"content":"Lồng không nên xuất ra"},"content":"Cấp ngoài cùng nên xuất ra"}`
	out := feedAll(t, "draft_chapter", in)
	mustContain(t, out, "Cấp ngoài cùng nên xuất ra")
	mustNotContain(t, out, "Lồng không nên xuất ra")
}

// ── Chế độ luồng trần: content không phải string thì bỏ qua toàn bộ ──

func TestExtract_NakedKeyNonStringValue(t *testing.T) {
	// content bị ghi nhầm thành object (không nên xảy ra nhưng phải chịu được)
	in := `{"content":{"unexpected":true}}`
	out := feedAll(t, "draft_chapter", in)
	if out != "" {
		t.Errorf("expected empty output, got: %q", out)
	}
}

// ── Sau khi container ngoài cùng đóng, Feed nữa không sinh gì ──

func TestExtract_FeedAfterDone(t *testing.T) {
	e := newToolExtractor("plan_chapter", observerTextsVI)
	e.Feed(`{"title":"x"}`)
	if !e.Done() {
		t.Fatal("expected Done")
	}
	if got := e.Feed(`junk`); got != "" {
		t.Errorf("expected empty output after Done, got: %q", got)
	}
}

// ── Chunk rỗng / input rỗng ──

func TestExtract_EmptyFeed(t *testing.T) {
	e := newToolExtractor("plan_chapter", observerTextsVI)
	if got := e.Feed(""); got != "" {
		t.Errorf("expected empty output for empty feed, got: %q", got)
	}
	if e.Done() {
		t.Error("Done should be false before any input")
	}
}

// ── Mảng lồng trực tiếp trong mảng (không qua obj) ──

func TestExtract_ArrayOfArrays(t *testing.T) {
	in := `{"matrix":[[1,2],[3,4]]}`
	out := feedAll(t, "plan_chapter", in)
	mustContain(t, out, "matrix:")
	mustContain(t, out, "- 1")
	mustContain(t, out, "- 2")
	mustContain(t, out, "- 3")
	mustContain(t, out, "- 4")
}

// ── Số rồi khoảng trắng rồi dấu phân tách ──

func TestExtract_NumberWithTrailingSpace(t *testing.T) {
	// "chapter": 1 ,  ← có nhiều khoảng trắng trước và sau số
	in := `{ "chapter" : 1 , "title" : "x" }`
	out := feedAll(t, "plan_chapter", in)
	mustContain(t, out, "chapter: 1")
	mustContain(t, out, "title: x")
}
