// Package parity kiểm tra tính đầy đủ và song song của các bảng văn bản
// song ngữ (tiếng Việt / tiếng Trung) trong mã production.
//
// Hợp đồng của các bảng này: mỗi bảng có hai biến cùng kiểu, một bản tiếng
// Việt (hậu tố VI) và một bản tiếng Trung (hậu tố ZH), được chọn bằng
// i18n.Table. Hai bản phải khai báo ĐÚNG HẤT các trường của kiểu struct:
//
//   - Thiếu một trường ở một bản → trường đó rỗng lúc chạy. Chuỗi rỗng có
//     thể lọt vào prompt gửi mô hình, vào thông báo cho người dùng, hoặc làm
//     hỏng JSON đầu ra — mà không có bất kỳ lỗi biên dịch nào.
//   - Lệch tập trường giữa hai bản → một ngôn ngữ có văn bản, ngôn ngữ kia
//     không có.
//
// Cổng CJK (scripts/check-vi-port.sh) không bắt được hai lỗi trên: nó chỉ đếm
// chuỗi literal, còn chuỗi rỗng thì không có gì để đếm. Bước kiểm tra này dùng
// go/parser để đọc cấu trúc thật của từng bảng nên không nhầm với bản dịch.
package parity

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Issue là một sai lệch giữa bản tiếng Việt và bản tiếng Trung của cùng một
// bảng văn bản song ngữ.
type Issue struct {
	// File là đường dẫn tương đối so với repo root, dùng "/" làm dấu phân cách.
	File string
	// Line là dòng khai báo bản có vấn đề.
	Line int
	// Table là tên biến của bản, ví dụ "hostTextsVI".
	Table string
	// Detail mô tả sai lệch bằng tiếng Việt.
	Detail string
}

// String trả về mô tả một dòng, đủ để in ra báo cáo.
func (i Issue) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", i.File, i.Line, i.Table, i.Detail)
}

// Check quét toàn bộ file Go production dưới root và trả về các sai lệch tìm
// được. Danh sách trả về rỗng nghĩa là mọi bảng song ngữ đều khớp.
//
// File test (_test.go) và thư mục sinh ra tự động không được quét: bảng song
// ngữ trong test là dữ liệu đầu vào của test, không phải bảng dùng lúc chạy.
func Check(root string) ([]Issue, error) {
	structs, tables, pairs, err := scanRepo(root)
	if err != nil {
		return nil, err
	}

	issues := compareTables(tables, structs)
	issues = append(issues, comparePairs(pairs)...)
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].File != issues[j].File {
			return issues[i].File < issues[j].File
		}
		if issues[i].Line != issues[j].Line {
			return issues[i].Line < issues[j].Line
		}
		return issues[i].Table < issues[j].Table
	})
	return issues, nil
}

// scanRepo đọc toàn bộ file Go production dưới root, gom các kiểu struct khai
// báo, các bản bảng song ngữ và các cặp `i18n.Text` tìm được.
func scanRepo(root string) (map[string]structType, []bilingualTable, []pairLiteral, error) {
	files, err := goFiles(root)
	if err != nil {
		return nil, nil, nil, err
	}

	structs := map[string]structType{} // "pkg::Type" -> khai báo struct
	var tables []bilingualTable
	var pairs []pairLiteral

	for _, file := range files {
		pkgStructs, pkgTables, pkgPairs, err := parseFile(file)
		if err != nil {
			return nil, nil, nil, err
		}
		pkg := packageDir(root, file)
		for name, st := range pkgStructs {
			structs[pkg+"::"+name] = st
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			rel = file
		}
		for i := range pkgTables {
			pkgTables[i].pkg = pkg
			pkgTables[i].file = filepath.ToSlash(rel)
		}
		for i := range pkgPairs {
			pkgPairs[i].file = filepath.ToSlash(rel)
		}
		tables = append(tables, pkgTables...)
		pairs = append(pairs, pkgPairs...)
	}
	return structs, tables, pairs, nil
}

// CheckDir là bản tiện dụng của Check cho thư mục hiện tại.
func CheckDir(dir string) ([]Issue, error) {
	return Check(dir)
}

// --- mô hình dữ liệu nội bộ -----------------------------------------------

// structType là khai báo của một kiểu struct trong Go.
type structType struct {
	// fields là danh sách tên trường, theo thứ tự khai báo.
	fields []string
}

// tableLevel là một mức của bảng: các trường được khai báo tại mức đó.
type tableLevel struct {
	// typ là tên kiểu struct của mức này ("" nếu không xác định được).
	typ string
	// keys là các trường được khai báo tại mức này.
	keys []string
	// texts lưu chuỗi literal của từng trường, để so sánh định dạng.
	texts map[string]string
	// allTexts gom MỌI chuỗi literal tìm thấy ở mức này, kể cả phần tử của
	// mảng chuỗi (mảng không lưu được vào texts vì giá trị không phải một
	// chuỗi đơn). Dùng để hỏi "mức này còn chữ Trung hay không".
	allTexts []string
	// opaque đếm các trường có giá trị KHÔNG đọc được bằng phân tích cú
	// pháp (hằng số, biểu thức nối, lời gọi hàm): nội dung của chúng không
	// biết, nên không thể kết luận chắc chắn về chúng.
	opaque int
}

// bilingualTable là một bản của bảng song ngữ (tiếng Việt hoặc tiếng Trung).
type bilingualTable struct {
	// name là tên biến, ví dụ "hostTextsVI".
	name string
	// base là tên bỏ hậu tố ngôn ngữ, ví dụ "hostTexts".
	base string
	// side là "vi" hoặc "zh".
	side string
	// pkg là thư mục gói (đường dẫn tương đối so với repo root).
	pkg string
	// file là đường dẫn file tương đối so với repo root.
	file string
	// line là dòng khai báo.
	line int
	// levels là các mức của bảng, khóa là đường dẫn trường ("hostTexts.style").
	levels map[string]tableLevel
}

// pairLiteral là một cặp văn bản viết trong một literal duy nhất, dạng
// `i18n.Text{VI: ..., ZH: ...}`. Khác với bảng struct, cặp nằm ngay tại chỗ
// dùng nên có thể nằm trong thân hàm, không chỉ ở khai báo biến.
type pairLiteral struct {
	// file là đường dẫn file tương đối so với repo root.
	file string
	// line là dòng của literal.
	line int
	// viSet và zhSet cho biết nhánh nào có trong literal.
	viSet bool
	zhSet bool
	// viText và zhText là chuỗi định dạng của hai nhánh (nếu đọc được).
	viText string
	viOK   bool
	zhText string
	zhOK   bool
}

// --- quét file ------------------------------------------------------------

// goFiles trả về danh sách file Go production dưới root.
func goFiles(root string) ([]string, error) {
	var files []string
	skipDirs := map[string]bool{
		".git": true, "vendor": true, "node_modules": true,
		"testdata": true, ".idea": true, ".vscode": true,
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// packageDir trả về đường dẫn tương đối của gói chứa file, dùng "/" làm dấu
// phân cách để ổn định giữa Windows và Linux.
func packageDir(root, file string) string {
	rel, err := filepath.Rel(root, filepath.Dir(file))
	if err != nil {
		rel = filepath.Dir(file)
	}
	return filepath.ToSlash(rel)
}

// parseFile đọc một file Go, trả về các kiểu struct khai báo, các bản bảng
// song ngữ và các cặp văn bản `i18n.Text` tìm thấy trong đó.
func parseFile(path string) (map[string]structType, []bilingualTable, []pairLiteral, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("đọc %s: %w", path, err)
	}

	structs := map[string]structType{}
	var tables []bilingualTable
	var pairs []pairLiteral

	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		switch gd.Tok {
		case token.TYPE:
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				info := structType{}
				for _, field := range st.Fields.List {
					// Trường nhúng không có tên riêng: literal dùng tên kiểu làm
					// khóa, nên không tính vào danh sách trường bắt buộc.
					for _, name := range field.Names {
						info.fields = append(info.fields, name.Name)
					}
				}
				structs[ts.Name.Name] = info
			}
		case token.VAR:
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := unwrapLiteral(vs.Values[i])
					if !ok {
						continue
					}
					typeID, ok := lit.Type.(*ast.Ident)
					if !ok {
						continue
					}
					side, base := languageSide(name.Name)
					if side == "" {
						continue
					}
					tbl := bilingualTable{
						name:   name.Name,
						base:   base,
						side:   side,
						line:   fset.Position(lit.Pos()).Line,
						levels: map[string]tableLevel{},
					}
					collectLevels(lit, typeID.Name, typeID.Name, tbl.levels)
					tables = append(tables, tbl)
				}
			}
		}
	}

	// Cặp `i18n.Text{VI: ..., ZH: ...}` có thể nằm ở bất kỳ chỗ nào trong
	// thân hàm, nên phải quét toàn bộ cây cú pháp chứ không chỉ khai báo biến.
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		name, ok := pairTypeName(lit.Type)
		if !ok || name != "Text" {
			return true
		}
		pair := pairLiteral{line: fset.Position(lit.Pos()).Line}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "VI":
				pair.viSet = true
				pair.viText, pair.viOK = formatStringOf(kv.Value)
			case "ZH":
				pair.zhSet = true
				pair.zhText, pair.zhOK = formatStringOf(kv.Value)
			}
		}
		pairs = append(pairs, pair)
		return true
	})

	return structs, tables, pairs, nil
}

// pairTypeName trả về tên kiểu của một literal, kể cả khi kiểu là biểu thức
// chọn như `i18n.Text`.
func pairTypeName(expr ast.Expr) (string, bool) {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name, true
	case *ast.SelectorExpr:
		return t.Sel.Name, true
	}
	return "", false
}

// formatStringOf đọc chuỗi định dạng của một biểu thức: chuỗi literal, chuỗi
// nối bằng `+`, hoặc đối số đầu của `fmt.Sprintf`.
func formatStringOf(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, okLeft := formatStringOf(e.X)
		right, okRight := formatStringOf(e.Y)
		if okLeft && okRight {
			return left + right, true
		}
	case *ast.CallExpr:
		if len(e.Args) == 0 {
			return "", false
		}
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok {
			return "", false
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "fmt" || sel.Sel.Name != "Sprintf" {
			return "", false
		}
		return formatStringOf(e.Args[0])
	}
	return "", false
}

// unwrapLiteral bóc literal từ biểu thức, xử lý cả `&T{...}` và `(T{...})`.
func unwrapLiteral(expr ast.Expr) (*ast.CompositeLit, bool) {
	switch e := expr.(type) {
	case *ast.CompositeLit:
		return e, true
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return unwrapLiteral(e.X)
		}
	case *ast.ParenExpr:
		return unwrapLiteral(e.X)
	}
	return nil, false
}

// languageSide nhận ra hậu tố ngôn ngữ của tên biến.
func languageSide(name string) (side, base string) {
	switch {
	case strings.HasSuffix(name, "VI"):
		return "vi", strings.TrimSuffix(name, "VI")
	case strings.HasSuffix(name, "ZH"):
		return "zh", strings.TrimSuffix(name, "ZH")
	}
	return "", ""
}

// literalStringOf đọc một biểu thức chuỗi literal, gồm nối chuỗi bằng "+".
// Không đọc lời gọi hàm: fmt.Sprintf("%s", x) cho biết định dạng chứ không cho
// biết văn bản lúc chạy, nên vẫn phải để là không xác định.
func literalStringOf(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, okLeft := literalStringOf(e.X)
		right, okRight := literalStringOf(e.Y)
		if okLeft && okRight {
			return left + right, true
		}
	}
	return "", false
}

// collectLevels đi xuống từng mức của literal, ghi lại các trường khai báo tại
// mỗi mức và giá trị chuỗi literal (nếu có).
func collectLevels(lit *ast.CompositeLit, typeName, path string, out map[string]tableLevel) {
	level := tableLevel{typ: typeName, texts: map[string]string{}}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		level.keys = append(level.keys, key.Name)

		if s, ok := literalStringOf(kv.Value); ok {
			level.texts[key.Name] = s
			level.allTexts = append(level.allTexts, s)
			continue
		}

		if nested, ok := unwrapLiteral(kv.Value); ok {
			if id, ok := nested.Type.(*ast.Ident); ok {
				// Nhóm con: nội dung của nó nằm ở mức sau.
				collectLevels(nested, id.Name, path+"."+key.Name, out)
				continue
			}
			// Mảng chuỗi: gom từng phần tử để kiểm tra nội dung nhánh ZH.
			if arr, ok := nested.Type.(*ast.ArrayType); ok {
				if id, ok := arr.Elt.(*ast.Ident); ok && id.Name == "string" {
					readable := true
					for _, item := range nested.Elts {
						bl, ok := item.(*ast.BasicLit)
						if !ok || bl.Kind != token.STRING {
							readable = false
							break
						}
						s, err := strconv.Unquote(bl.Value)
						if err != nil {
							readable = false
							break
						}
						level.allTexts = append(level.allTexts, s)
					}
					if !readable {
						level.opaque++
					}
					continue
				}
			}
			level.opaque++
			continue
		}

		level.opaque++
	}
	out[path] = level
}

// --- kiểm tra nhánh ZH -----------------------------------------------------

// Loại kết quả kiểm tra nhánh ZH.
const (
	// ZHViolation nghĩa là nhánh ZH chắc chắn không còn ký tự CJK nào.
	ZHViolation = "vi phạm"
	// ZHUnknown nghĩa là nhánh ZH có giá trị không đọc được bằng phân tích
	// cú pháp nên không kết luận được; cần người đọc.
	ZHUnknown = "không xác định"
)

// ZHFinding là kết quả kiểm tra nhánh ZH của một bảng song ngữ hoặc một
// cặp `i18n.Text`.
type ZHFinding struct {
	// File là đường dẫn tương đối so với repo root.
	File string
	// Line là dòng khai báo.
	Line int
	// Table là tên bảng (hoặc "i18n.Text" với cặp viết tay trong thân hàm).
	Table string
	// Kind là ZHViolation hoặc ZHUnknown.
	Kind string
	// Detail mô tả sai lệch bằng tiếng Việt.
	Detail string
}

// String trả về một dòng báo cáo.
func (f ZHFinding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s: %s", f.File, f.Line, f.Table, f.Kind, f.Detail)
}

// ZHReport là kết quả quét nhánh ZH của cả repo.
type ZHReport struct {
	// Tables là số bản ZH song ngữ đã kiểm tra (có bản VI đi kèm).
	Tables int
	// Pairs là số cặp `i18n.Text` có nhánh ZH đã kiểm tra.
	Pairs int
	// Findings là các nhánh ZH không còn ký tự CJK, hoặc không xác định được.
	Findings []ZHFinding
}

// Violations trả về các lỗi chặn: nhánh ZH chắc chắn không còn ký tự CJK.
func (r ZHReport) Violations() []ZHFinding {
	var out []ZHFinding
	for _, f := range r.Findings {
		if f.Kind == ZHViolation {
			out = append(out, f)
		}
	}
	return out
}

// Unknown trả về các nhánh ZH không đọc được, nên không kết luận được.
func (r ZHReport) Unknown() []ZHFinding {
	var out []ZHFinding
	for _, f := range r.Findings {
		if f.Kind == ZHUnknown {
			out = append(out, f)
		}
	}
	return out
}

// CheckZH quét repo và trả về các nhánh ZH không còn ký tự CJK.
//
// Lỗi mà mọi cổng khác đều bỏ lọt: cổng CJK không thấy vì nhánh ZH đã hết
// chữ Trung, cổng song ngữ không thấy vì tập trường vẫn khớp. Đó chính là
// cách một bảng "tiếng Trung" âm thầm trở thành bản tiếng Việt — thứ mà
// cổng CJK không thể phát hiện vì bản dịch không còn ký tự CJK.
//
// Quy tắc: nhánh ZH của mỗi bảng "...ZH" và mỗi cặp `i18n.Text` phải còn
// ít nhất MỘT ký tự CJK. Kiểm tra theo cả bảng, không theo từng mục: một
// mục đơn lẻ có thể hợp lệ là chuỗi ASCII thuần (ví dụ "%d").
//
// Không đòi bản "...ZH" phải có bản "...VI" đi kèm: nếu đòi thì chỉ cần
// đổi tên bản VI là nhánh ZH rơi khỏi phạm vi kiểm tra — cổng tự tắt mình
// trong im lặng, đúng cái lỗi mà cổng này sinh ra để chặn.
func CheckZH(root string) (ZHReport, error) {
	_, tables, pairs, err := scanRepo(root)
	if err != nil {
		return ZHReport{}, err
	}

	tableFindings, checkedTables := checkTableZHs(tables)
	pairFindings, checkedPairs := checkPairZHs(pairs)

	findings := append(tableFindings, pairFindings...)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Table < findings[j].Table
	})
	return ZHReport{
		Tables:   checkedTables,
		Pairs:    checkedPairs,
		Findings: findings,
	}, nil
}

// checkTableZHs kiểm tra MỌI bản ZH, kể cả bản không có bản VI đi kèm.
// Trả về các kết quả và số bản ZH đã kiểm tra.
//
// Không đòi có bản VI: một bản tên "...ZH" mà không còn chữ Trung thì sai
// dù có hay không có bản VI. Nếu đòi bản VI thì chỉ cần đổi tên bản VI là
// nhánh ZH rơi ra khỏi phạm vi kiểm tra — cổng tự tắt mình trong im lặng,
// đúng cái lỗi mà cổng này sinh ra để chặn.
func checkTableZHs(tables []bilingualTable) ([]ZHFinding, int) {
	groups := map[string][]bilingualTable{}
	for _, tbl := range tables {
		key := tbl.pkg + "::" + tbl.base
		groups[key] = append(groups[key], tbl)
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var findings []ZHFinding
	checked := 0
	for _, key := range keys {
		for _, tbl := range groups[key] {
			if tbl.side != "zh" {
				continue
			}
			checked++
			found, unknown, preview := zhTableContent(tbl)
			switch {
			case found:
				continue
			case unknown:
				findings = append(findings, ZHFinding{
					File: tbl.file, Line: tbl.line, Table: tbl.name, Kind: ZHUnknown,
					Detail: "nhánh ZH có giá trị không đọc được bằng phân tích cú pháp, " +
						"không kết luận được có còn ký tự CJK hay không",
				})
			default:
				findings = append(findings, ZHFinding{
					File: tbl.file, Line: tbl.line, Table: tbl.name, Kind: ZHViolation,
					Detail: fmt.Sprintf("nhánh ZH không còn ký tự CJK nào%s", preview),
				})
			}
		}
	}
	return findings, checked
}

// zhTableContent kiểm tra nội dung của một bản ZH.
//
// Trả về: đã tìm thấy ký tự CJK hay không, có mục không đọc được hay không,
// và một đoạn trích nhỏ để người đọc biết bảng đang chứa gì.
func zhTableContent(tbl bilingualTable) (found bool, unknown bool, preview string) {
	paths := make([]string, 0, len(tbl.levels))
	for path := range tbl.levels {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var samples []string
	opaque := 0
	for _, path := range paths {
		level := tbl.levels[path]
		opaque += level.opaque
		for _, text := range level.allTexts {
			if hasCJK(text) {
				return true, false, ""
			}
		}
		for _, key := range level.keys {
			text, ok := level.texts[key]
			if !ok {
				// Giá trị là mảng chuỗi: nội dung đã được gom vào allTexts.
				continue
			}
			if len(samples) < 3 {
				samples = append(samples, fmt.Sprintf("%s.%s=%q", path, key, text))
			}
		}
	}
	if opaque > 0 {
		return false, true, ""
	}
	if len(samples) == 0 {
		return false, false, " (bảng không có chuỗi literal nào để đọc)"
	}
	return false, false, ": " + strings.Join(samples, ", ")
}

// checkPairZHs kiểm tra nhánh ZH của các cặp `i18n.Text` viết tay.
// Trả về các kết quả và số cặp đã kiểm tra.
func checkPairZHs(pairs []pairLiteral) ([]ZHFinding, int) {
	var findings []ZHFinding
	checked := 0
	for _, pair := range pairs {
		if !pair.zhSet {
			// Thiếu nhánh ZH: comparePairs đã báo sai lệch đó.
			continue
		}
		checked++
		if !pair.zhOK {
			findings = append(findings, ZHFinding{
				File: pair.file, Line: pair.line, Table: "i18n.Text", Kind: ZHUnknown,
				Detail: "nhánh ZH không đọc được bằng phân tích cú pháp, " +
					"không kết luận được có còn ký tự CJK hay không",
			})
			continue
		}
		if !hasCJK(pair.zhText) {
			findings = append(findings, ZHFinding{
				File: pair.file, Line: pair.line, Table: "i18n.Text", Kind: ZHViolation,
				Detail: fmt.Sprintf("nhánh ZH không còn ký tự CJK nào: %q", pair.zhText),
			})
		}
	}
	return findings, checked
}

// hasCJK báo chuỗi có chứa ít nhất một ký tự CJK hay không.
//
// Dùng đúng hai dải mà cổng CJK (scripts/vi_port_scan.py) dùng, để hai bộ
// kiểm tra cùng hiểu một chữ "Trung" là gì: CJK hợp nhất (U+4E00–U+9FFF) và
// CJK mở rộng A (U+3400–U+4DBF).
func hasCJK(text string) bool {
	for _, r := range text {
		if (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) {
			return true
		}
	}
	return false
}

// --- so sánh ---------------------------------------------------------------

// compareTables ghép các bản VI với bản ZH cùng gốc rồi báo sai lệch.
func compareTables(tables []bilingualTable, structs map[string]structType) []Issue {
	groups := map[string][]bilingualTable{}
	for _, tbl := range tables {
		key := tbl.pkg + "::" + tbl.base
		groups[key] = append(groups[key], tbl)
	}

	var issues []Issue
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		group := groups[key]
		var vis, zhs []bilingualTable
		for _, tbl := range group {
			switch tbl.side {
			case "vi":
				vis = append(vis, tbl)
			case "zh":
				zhs = append(zhs, tbl)
			}
		}
		if len(vis) == 0 || len(zhs) == 0 {
			// Chỉ có một bản: không phải bảng song ngữ, không kiểm tra.
			continue
		}
		for i := 0; i < min(len(vis), len(zhs)); i++ {
			issues = append(issues, comparePair(vis[i], zhs[i], structs)...)
		}
	}

	sort.Slice(issues, func(i, j int) bool {
		if issues[i].File != issues[j].File {
			return issues[i].File < issues[j].File
		}
		if issues[i].Line != issues[j].Line {
			return issues[i].Line < issues[j].Line
		}
		return issues[i].Table < issues[j].Table
	})
	return issues
}

// comparePairs so các cặp `i18n.Text{VI: ..., ZH: ...}`.
//
// Cặp viết tay trong thân hàm rất dễ lệch khi dịch: sửa `%d` thành `%s` ở
// một bên là lỗi hiển thị kiểu %!d(string=...) lúc chạy, và bỏ trống một bên
// là câu rỗng đi thẳng vào prompt.
func comparePairs(pairs []pairLiteral) []Issue {
	var issues []Issue
	for _, pair := range pairs {
		switch {
		case !pair.viSet && !pair.zhSet:
			continue
		case !pair.viSet:
			issues = append(issues, Issue{
				File: pair.file, Line: pair.line, Table: "i18n.Text",
				Detail: "cặp song ngữ thiếu nhánh VI",
			})
		case !pair.zhSet:
			issues = append(issues, Issue{
				File: pair.file, Line: pair.line, Table: "i18n.Text",
				Detail: "cặp song ngữ thiếu nhánh ZH",
			})
		}
		if pair.viOK && pair.viText == "" {
			issues = append(issues, Issue{
				File: pair.file, Line: pair.line, Table: "i18n.Text",
				Detail: "nhánh VI rỗng",
			})
		}
		if pair.zhOK && pair.zhText == "" {
			issues = append(issues, Issue{
				File: pair.file, Line: pair.line, Table: "i18n.Text",
				Detail: "nhánh ZH rỗng",
			})
		}
		if !pair.viOK || !pair.zhOK {
			continue
		}
		viVerbs, viHas := formatVerbs(pair.viText)
		zhVerbs, zhHas := formatVerbs(pair.zhText)
		if viHas != zhHas || (viHas && viVerbs != zhVerbs) {
			issues = append(issues, Issue{
				File: pair.file, Line: pair.line, Table: "i18n.Text",
				Detail: fmt.Sprintf("định dạng hai nhánh khác nhau: VI %s, ZH %s",
					describeVerbs(viVerbs, viHas), describeVerbs(zhVerbs, zhHas)),
			})
		}
	}
	return issues
}

// describeVerbs mô tả chuỗi đặt tham số cho báo cáo.
func describeVerbs(verbs string, has bool) string {
	if !has {
		return "(không có tham số)"
	}
	return fmt.Sprintf("%q", verbs)
}

// comparePair so một cặp bản VI/ZH của cùng một bảng văn bản.
func comparePair(vi, zh bilingualTable, structs map[string]structType) []Issue {
	var issues []Issue

	paths := map[string]bool{}
	for path := range vi.levels {
		paths[path] = true
	}
	for path := range zh.levels {
		paths[path] = true
	}
	sortedPaths := make([]string, 0, len(paths))
	for path := range paths {
		sortedPaths = append(sortedPaths, path)
	}
	sort.Strings(sortedPaths)

	for _, path := range sortedPaths {
		viLevel, viOK := vi.levels[path]
		zhLevel, zhOK := zh.levels[path]
		switch {
		case viOK && !zhOK:
			issues = append(issues, Issue{
				File: vi.file, Line: vi.line, Table: vi.name,
				Detail: fmt.Sprintf("nhóm %s có ở bản VI nhưng thiếu ở bản ZH", path),
			})
			continue
		case !viOK && zhOK:
			issues = append(issues, Issue{
				File: zh.file, Line: zh.line, Table: zh.name,
				Detail: fmt.Sprintf("nhóm %s có ở bản ZH nhưng thiếu ở bản VI", path),
			})
			continue
		}

		if missing := missingFrom(viLevel.keys, zhLevel.keys); len(missing) > 0 {
			issues = append(issues, Issue{
				File: zh.file, Line: zh.line, Table: zh.name,
				Detail: fmt.Sprintf("bản ZH thiếu trường: %s", strings.Join(missing, ", ")),
			})
		}
		if missing := missingFrom(zhLevel.keys, viLevel.keys); len(missing) > 0 {
			issues = append(issues, Issue{
				File: vi.file, Line: vi.line, Table: vi.name,
				Detail: fmt.Sprintf("bản VI thiếu trường: %s", strings.Join(missing, ", ")),
			})
		}

		issues = append(issues, compareFormats(vi, zh, path, viLevel, zhLevel)...)
	}

	// Đầy đủ so với khai báo struct: thiếu trường ở CẢ HAI bản thì trường đó
	// rỗng lúc chạy, dù hai bản vẫn "khớp" với nhau.
	for _, tbl := range []bilingualTable{vi, zh} {
		for _, path := range sortedPaths {
			level, ok := tbl.levels[path]
			if !ok {
				continue
			}
			st, ok := structs[tbl.pkg+"::"+level.typ]
			if !ok {
				continue
			}
			if missing := missingFrom(st.fields, level.keys); len(missing) > 0 {
				issues = append(issues, Issue{
					File: tbl.file, Line: tbl.line, Table: tbl.name,
					Detail: fmt.Sprintf("thiếu trường so với khai báo struct %s: %s",
						level.typ, strings.Join(missing, ", ")),
				})
			}
			for _, key := range level.keys {
				text, ok := level.texts[key]
				if ok && text == "" {
					issues = append(issues, Issue{
						File: tbl.file, Line: tbl.line, Table: tbl.name,
						Detail: fmt.Sprintf("trường %s rỗng", key),
					})
				}
			}
		}
	}
	return issues
}

// compareFormats so chuỗi định dạng của hai bản: cùng thứ tự đặt tham số và
// cùng kiểu định dạng. Lệch ở đây làm lỗi hiển thị kiểu %!d(string=...) hoặc
// hoán đổi nội dung hai tham số.
func compareFormats(vi, zh bilingualTable, path string, viLevel, zhLevel tableLevel) []Issue {
	var issues []Issue
	for _, key := range viLevel.keys {
		viText, viOK := viLevel.texts[key]
		zhText, zhOK := zhLevel.texts[key]
		if !viOK || !zhOK {
			// Giá trị là hằng số hoặc biểu thức: không so được ở mức này.
			continue
		}
		viVerbs, viHas := formatVerbs(viText)
		zhVerbs, zhHas := formatVerbs(zhText)
		if !viHas || !zhHas {
			continue
		}
		if viVerbs != zhVerbs {
			issues = append(issues, Issue{
				File: vi.file, Line: vi.line, Table: vi.name,
				Detail: fmt.Sprintf("định dạng khác bản ZH ở %s.%s: VI %q, ZH %q",
					path, key, viVerbs, zhVerbs),
			})
		}
	}
	return issues
}

// formatVerbs trả về chuỗi đặt tham số của một format string, ví dụ "%s:%d".
// Tham số thứ hai cho biết chuỗi có đặt tham số nào hay không.
func formatVerbs(text string) (string, bool) {
	var verbs []string
	for i := 0; i < len(text); i++ {
		if text[i] != '%' {
			continue
		}
		j := i + 1
		for j < len(text) && strings.ContainsRune("-+# 0123456789.", rune(text[j])) {
			j++
		}
		if j >= len(text) {
			break
		}
		verb := text[j]
		if verb != '%' {
			verbs = append(verbs, "%"+string(verb))
		}
		i = j
	}
	if len(verbs) == 0 {
		return "", false
	}
	return strings.Join(verbs, ""), true
}

// missingFrom trả về các phần tử của have không có trong want.
func missingFrom(have, want []string) []string {
	set := make(map[string]bool, len(want))
	for _, w := range want {
		set[w] = true
	}
	var missing []string
	for _, h := range have {
		if !set[h] {
			missing = append(missing, h)
		}
	}
	return missing
}
