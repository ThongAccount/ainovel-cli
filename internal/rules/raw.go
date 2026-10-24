package rules

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RawSource là một nguồn thô chờ chuẩn hóa (toàn bộ nội dung tệp rules).
//
// Sau khi bỏ YAML, tệp rules chỉ là prompt bằng ngôn ngữ tự nhiên; bước chuẩn hóa
// chỉ cần nguyên bản, không phân tích front matter nữa.
type RawSource struct {
	Label string     // nhãn nguồn, đi vào Snapshot.Sources (ví dụ global:my-style.md)
	Kind  SourceKind // cấp ưu tiên
	Text  string     // nội dung gốc của tệp
}

// RawFileSources liệt kê tệp .md trong thư mục rules theo thứ tự Global → Project
// và trả về nội dung thô.
//
// Dùng cùng quy ước quét với readDirFromDisk (tệp .md ở cấp cao nhất, theo thứ tự
// tên tệp, bỏ tệp ẩn), nhưng không phân tích YAML; toàn bộ văn bản được chuyển nguyên
// vẹn cho bộ chuẩn hóa. System defaults / prompt khởi động / yêu cầu trong lúc chạy
// do service cung cấp riêng.
func RawFileSources(opts LoadOptions) []RawSource {
	var out []RawSource
	out = append(out, rawDir(opts.HomeRulesDir, SourceGlobal)...)
	out = append(out, rawDir(opts.ProjectRulesDir, SourceProject)...)
	return out
}

func rawDir(dir string, kind SourceKind) []RawSource {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Không có thư mục là trạng thái bình thường nên bỏ qua âm thầm; nhưng lỗi quyền
		// hoặc đường dẫn thực sự là tệp phải được ghi lại — nếu không, người dùng đã viết
		// quy tắc nhưng quy tắc hoàn toàn không có tác dụng mà không nhận được phản hồi nào,
		// khiến việc chẩn đoán tốn kém (xem known_rules_path_stale_readme).
		if !os.IsNotExist(err) {
			slog.Warn("Không đọc được thư mục quy tắc, đã bỏ qua", "module", "rules", "dir", dir, "err", err)
		}
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var out []RawSource
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("Không đọc được tệp quy tắc, đã bỏ qua", "module", "rules", "file", path, "err", err)
			continue
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			continue
		}
		out = append(out, RawSource{
			Label: kind.String() + ":" + name,
			Kind:  kind,
			Text:  text,
		})
	}
	return out
}
