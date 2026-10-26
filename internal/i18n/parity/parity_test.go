package parity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixture ghi một file Go vào thư mục tạm và trả về thư mục gốc.
func writeFixture(t *testing.T, name, source string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("tạo thư mục: %v", err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("ghi fixture: %v", err)
	}
	return root
}

// details gộp mọi mô tả sai lệch thành một chuỗi để assert.
func details(issues []Issue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, issue.Detail)
	}
	return strings.Join(parts, " | ")
}

const cleanPair = `package demo

type demoTexts struct {
	greeting string
	count    string
}

var demoTextsVI = &demoTexts{
	greeting: "Xin chào %s",
	count:    "Có %d chương",
}

var demoTextsZH = &demoTexts{
	greeting: "你好 %s",
	count:    "共 %d 章",
}
`

func TestCheckCleanPairHasNoIssue(t *testing.T) {
	root := writeFixture(t, "demo/texts.go", cleanPair)
	issues, err := Check(root)
	if err != nil {
		t.Fatalf("quét: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("bản khớp phải sạch, nhận: %v", issues)
	}
}

// Bản ZH thiếu một trường: Go vẫn biên dịch được, nhưng khi chạy với tác
// phẩm tiếng Trung thì trường đó rỗng.
func TestCheckCatchesFieldMissingOnZH(t *testing.T) {
	source := `package demo

type demoTexts struct {
	greeting string
	count    string
}

var demoTextsVI = &demoTexts{
	greeting: "Xin chào %s",
	count:    "Có %d chương",
}

var demoTextsZH = &demoTexts{
	greeting: "你好 %s",
}
`
	root := writeFixture(t, "demo/texts.go", source)
	issues, err := Check(root)
	if err != nil {
		t.Fatalf("quét: %v", err)
	}
	if len(issues) == 0 {
		t.Fatal("phải báo trường thiếu ở bản ZH")
	}
	if got := details(issues); !strings.Contains(got, "bản ZH thiếu trường: count") {
		t.Fatalf("phải nêu đúng trường thiếu, nhận: %s", got)
	}
}

// Cả hai bản đều thiếu một trường: hai bản "khớp" nhau nhưng trường rỗng lúc
// chạy. Đây là lỗi mà so khớp hai bản không bắt được.
func TestCheckCatchesFieldMissingOnBothSides(t *testing.T) {
	source := `package demo

type demoTexts struct {
	greeting string
	count    string
}

var demoTextsVI = &demoTexts{
	greeting: "Xin chào %s",
}

var demoTextsZH = &demoTexts{
	greeting: "你好 %s",
}
`
	root := writeFixture(t, "demo/texts.go", source)
	issues, err := Check(root)
	if err != nil {
		t.Fatalf("quét: %v", err)
	}
	if got := details(issues); !strings.Contains(got, "thiếu trường so với khai báo struct demoTexts: count") {
		t.Fatalf("phải báo trường thiếu so với struct, nhận: %s", got)
	}
}

// Định dạng lệch: %s ở bản VI nhưng %d ở bản ZH → lỗi hiển thị kiểu
// %!d(string=...) lúc chạy.
func TestCheckCatchesFormatMismatch(t *testing.T) {
	source := `package demo

type demoTexts struct {
	greeting string
}

var demoTextsVI = &demoTexts{
	greeting: "Xin chào %s",
}

var demoTextsZH = &demoTexts{
	greeting: "你好 %d",
}
`
	root := writeFixture(t, "demo/texts.go", source)
	issues, err := Check(root)
	if err != nil {
		t.Fatalf("quét: %v", err)
	}
	if got := details(issues); !strings.Contains(got, "định dạng khác bản ZH") {
		t.Fatalf("phải báo định dạng lệch, nhận: %s", got)
	}
}

// Trường khai báo rỗng: tương đương thiếu trường ở cả hai bản.
func TestCheckCatchesEmptyText(t *testing.T) {
	source := `package demo

type demoTexts struct {
	greeting string
}

var demoTextsVI = &demoTexts{
	greeting: "",
}

var demoTextsZH = &demoTexts{
	greeting: "你好",
}
`
	root := writeFixture(t, "demo/texts.go", source)
	issues, err := Check(root)
	if err != nil {
		t.Fatalf("quét: %v", err)
	}
	if got := details(issues); !strings.Contains(got, "trường greeting rỗng") {
		t.Fatalf("phải báo trường rỗng, nhận: %s", got)
	}
}

// Bảng lồng nhau: bản VI có nhóm con, bản ZH không → phải báo.
func TestCheckCatchesMissingNestedGroup(t *testing.T) {
	source := `package demo

type childTexts struct {
	label string
}

type demoTexts struct {
	title string
	child childTexts
}

var demoTextsVI = &demoTexts{
	title: "Tiêu đề",
	child: childTexts{
		label: "Nhãn",
	},
}

var demoTextsZH = &demoTexts{
	title: "标题",
}
`
	root := writeFixture(t, "demo/texts.go", source)
	issues, err := Check(root)
	if err != nil {
		t.Fatalf("quét: %v", err)
	}
	if got := details(issues); !strings.Contains(got, "thiếu ở bản ZH") {
		t.Fatalf("phải báo nhóm lồng nhau thiếu, nhận: %s", got)
	}
}

// Bảng chỉ có một bản không phải bảng song ngữ → không báo.
func TestCheckIgnoresOneSidedTable(t *testing.T) {
	source := `package demo

type demoTexts struct {
	greeting string
}

var demoTextsVI = &demoTexts{
	greeting: "Xin chào",
}
`
	root := writeFixture(t, "demo/texts.go", source)
	issues, err := Check(root)
	if err != nil {
		t.Fatalf("quét: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("bảng một bản không phải bảng song ngữ, nhận: %v", issues)
	}
}

// Bản có giá trị hằng số thì không so được định dạng → không báo sai.
func TestCheckSkipsConstValues(t *testing.T) {
	source := `package demo

const greetingVI = "Xin chào %s"
const greetingZH = "你好 %s"

type demoTexts struct {
	greeting string
}

var demoTextsVI = &demoTexts{
	greeting: greetingVI,
}

var demoTextsZH = &demoTexts{
	greeting: greetingZH,
}
`
	root := writeFixture(t, "demo/texts.go", source)
	issues, err := Check(root)
	if err != nil {
		t.Fatalf("quét: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("giá trị hằng số không được suy diễn thành lỗi, nhận: %v", issues)
	}
}
