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
	files, err := goFiles(root)
	if err != nil {
		return nil, err
	}

	structs := map[string]structType{} // "pkg::Type" -> khai báo struct
	var tables []bilingualTable
	var pairs []pairLiteral

	for _, file := range files {
		pkgStructs, pkgTables, pkgPairs, err := parseFile(file)
		if err != nil {
			return nil, err
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
		if bl, ok := kv.Value.(*ast.BasicLit); ok && bl.Kind == token.STRING {
			if s, err := strconv.Unquote(bl.Value); err == nil {
				level.texts[key.Name] = s
			}
		}
		if nested, ok := unwrapLiteral(kv.Value); ok {
			if id, ok := nested.Type.(*ast.Ident); ok {
				collectLevels(nested, id.Name, path+"."+key.Name, out)
			}
		}
	}
	out[path] = level
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
