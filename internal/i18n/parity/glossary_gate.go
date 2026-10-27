package parity

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// GlossaryException là một ngoại lệ đã ghim cho cổng thuật ngữ.
//
// Bảng thuật ngữ là nguồn chuẩn, nhưng không phải dòng nào trong đó cũng có
// thể siết thành cổng đỏ: tiếng Việt có nhiều đồng nghĩa hợp lệ ("dàn ý" và
// "đề cương", "dọn" và "dọn dẹp"), và tiếng Trung có từ đa nghĩa — cùng một
// từ, hai nghĩa khác nhau ("mục lục" và "thư mục", "vai trò" và "nhân vật").
// Cổng "thô" báo đỏ hơn một trăm chỗ, hầu hết là báo
// động giả — cổng đỏ 117 lần thì không ai đọc, và cổng bị tắt trong im
// lặng thì cũng không bảo vệ được gì.
//
// Vì vậy cổng siết những dòng repo đang tuân thủ, còn những dòng còn lại
// được ghim ở đây kèm lý do cụ thể. Danh sách này là một hợp đồng, không
// phải danh sách "cho qua": mỗi mục phải trả lời được "vì sao repo không
// theo bảng", và mỗi mục phải còn hiệu lực — nếu mã đã sửa xong thì mục đó
// phải bị xóa, và cổng sẽ báo đỏ nếu quên.
//
// Cổng chỉ phủ lớp song ngữ (bảng VI/ZH và cặp i18n.Text). Chuỗi chỉ có
// tiếng Việt trong TUI/diag không thuộc phạm vi này.
type GlossaryException struct {
	// ZH là các thuật ngữ tiếng Trung mà ngoại lệ này áp dụng.
	ZH []string
	// VI là cách viết tiếng Việt trong bảng thuật ngữ.
	VI string
	// Reason là lý do bằng tiếng Việt vì sao nhánh VI của repo không theo bảng.
	Reason string
}

// glossaryExceptions là danh sách ngoại lệ đã ghim của cổng thuật ngữ.
//
// Mỗi mục là một đồng nghĩa hợp lệ hoặc một từ đa nghĩa, KHÔNG phải một
// sai lệch đã biết. Hai sai lệch thật đã được sửa trong mã thay vì ghim ở
// đây: thuật ngữ "phục bút" (repo dùng "manh mối", vốn là từ đã dành cho
// thuật ngữ manh mối) và thuật ngữ "mô hình" (repo dùng "model" trong văn xuôi).
var glossaryExceptions = []GlossaryException{
	{
		ZH:     []string{"正文"},
		VI:     "nội dung truyện",
		Reason: "đồng nghĩa: repo dùng \"nội dung\", vẫn là cùng một khái niệm thân bản chương",
	},
	{
		ZH:     []string{"大纲", "提纲"},
		VI:     "Đề cương",
		Reason: "đồng nghĩa: repo dùng \"dàn ý\" — cách viết tự nhiên hơn cho văn bản mô tả",
	},
	{
		ZH:     []string{"钩子"},
		VI:     "điểm móc",
		Reason: "đồng nghĩa: repo dùng \"móc câu\", \"móc cuối chương\"",
	},
	{
		ZH:     []string{"人物", "角色"},
		VI:     "nhân vật",
		Reason: "đa nghĩa: 角色 ở chỗ này là \"vai trò\" (vai trò tường thuật), không phải nhân vật",
	},
	{
		ZH:     []string{"记忆", "情节记忆"},
		VI:     "ký ức",
		Reason: "đồng nghĩa: repo dùng \"trí nhớ\"",
	},
	{
		ZH:     []string{"规划", "规划期"},
		VI:     "quy hoạch",
		Reason: "đồng nghĩa: repo dùng \"lập kế hoạch\" và gọi vai trò là \"kiến trúc sư\"",
	},
	{
		ZH:     []string{"写作", "写作期"},
		VI:     "sáng tác",
		Reason: "đồng nghĩa: repo dùng \"viết\", \"quy tắc viết\", \"Người viết\"",
	},
	{
		ZH:     []string{"返工"},
		VI:     "làm lại",
		Reason: "đồng nghĩa: repo dùng \"khắc phục\"",
	},
	{
		ZH:     []string{"截断"},
		VI:     "cắt ngắn",
		Reason: "đồng nghĩa: repo dùng \"đã cắt\"",
	},
	{
		ZH:     []string{"失败"},
		VI:     "thất bại",
		Reason: "đồng nghĩa: repo dùng \"không thành công\", hoặc rút gọn thành nhãn phạm vi kèm lỗi cụ thể phía sau",
	},
	{
		ZH:     []string{"校验"},
		VI:     "kiểm tra hợp lệ",
		Reason: "đồng nghĩa: repo dùng \"kiểm tra\" kèm danh từ (\"kiểm tra nghiệp vụ\", \"kiểm tra sự thật\")",
	},
	{
		ZH:     []string{"可选"},
		VI:     "tùy chọn",
		Reason: "đồng nghĩa: repo dùng \"có thể chọn\"",
	},
	{
		ZH:     []string{"必填"},
		VI:     "bắt buộc",
		Reason: "đồng nghĩa: repo dùng \"phải điền\"",
	},
	{
		ZH:     []string{"落盘"},
		VI:     "ghi xuống đĩa",
		Reason: "đồng nghĩa: repo dùng \"ghi ... xuống đĩa\", \"bền vào đĩa\"",
	},
	{
		ZH:     []string{"覆盖"},
		VI:     "ghi đè",
		Reason: "đa nghĩa: 覆盖 ở chỗ này là \"phủ\" (bao phủ phạm vi), không phải ghi đè",
	},
	{
		ZH:     []string{"复用"},
		VI:     "tái sử dụng",
		Reason: "đồng nghĩa: repo dùng \"tái dùng\", \"dùng lại\"",
	},
	{
		ZH:     []string{"清理"},
		VI:     "dọn dẹp",
		Reason: "đồng nghĩa: repo dùng \"dọn\"",
	},
	{
		ZH:     []string{"加载"},
		VI:     "nạp",
		Reason: "đồng nghĩa: repo dùng \"Tải\"",
	},
	{
		ZH:     []string{"限流", "重试"},
		VI:     "giới hạn tần suất / thử lại",
		Reason: "đồng nghĩa: nhánh 重试 dịch là \"thử gửi lại\"",
	},
	{
		ZH:     []string{"目录"},
		VI:     "thư mục",
		Reason: "đa nghĩa: 目录 ở chỗ này là \"mục lục\" (mục lục truyện), không phải thư mục",
	},
	{
		ZH:     []string{"输出"},
		VI:     "đầu ra",
		Reason: "đồng nghĩa: repo dùng \"xuất ra\"",
	},
	{
		ZH:     []string{"规则"},
		VI:     "quy tắc",
		Reason: "đồng nghĩa: repo dùng \"luật\" (\"luật thế giới\")",
	},
	{
		ZH:     []string{"证据"},
		VI:     "bằng chứng",
		Reason: "đồng nghĩa: repo dùng \"chứng cứ\"",
	},
	{
		ZH:     []string{"建议"},
		VI:     "khuyến nghị",
		Reason: "đồng nghĩa: repo dùng \"đề xuất\", \"gợi ý\"",
	},
	{
		ZH:     []string{"重复"},
		VI:     "trùng lặp",
		Reason: "đồng nghĩa: repo dùng \"lặp\", \"lặp lại\"",
	},
	{
		ZH:     []string{"继续"},
		VI:     "tiếp tục",
		Reason: "đồng nghĩa: repo dùng \"còn tiếp\", \"cần tiếp\"",
	},
	{
		ZH:     []string{"完成"},
		VI:     "hoàn thành",
		Reason: "đồng nghĩa: repo dùng \"hoàn tất\"",
	},
	{
		ZH:     []string{"确认"},
		VI:     "xác nhận",
		Reason: "đa nghĩa: 确认 ở chỗ này là \"xác định\" (\"không xác định được\"), không phải xác nhận",
	},
	{
		ZH:     []string{"请"},
		VI:     "vui lòng",
		Reason: "đồng nghĩa: repo dùng \"hãy\"",
	},
	{
		ZH:     []string{"无法"},
		VI:     "không thể",
		Reason: "đồng nghĩa: repo dùng cấu trúc \"không ... được\" (\"không phân loại được\")",
	},
	{
		ZH:     []string{"实测"},
		VI:     "thực đo",
		Reason: "đồng nghĩa: repo dùng \"thực nghiệm\"",
	},
	{
		ZH:     []string{"用于"},
		VI:     "dùng cho",
		Reason: "đồng nghĩa: repo dùng \"dùng để\"",
	},
}

// GlossaryReport là kết quả kiểm tra thuật ngữ trên toàn repo.
type GlossaryReport struct {
	// Terms là số dòng bảng thuật ngữ đọc được.
	Terms int
	// Enforced là số dòng được siết thành cổng đỏ.
	Enforced int
	// Exempted là số dòng được miễn theo danh sách ngoại lệ.
	Exempted int
	// Occurrences là tổng số chỗ song ngữ có chứa thuật ngữ.
	Occurrences int
	// EnforcedOccurrences là số chỗ thuộc các dòng được siết.
	EnforcedOccurrences int
	// Violations là sai lệch ở các dòng không được miễn.
	Violations []Issue
	// StaleExceptions là ngoại lệ không còn khớp dòng nào trong bảng thuật ngữ.
	StaleExceptions []string
	// RedundantExceptions là ngoại lệ còn khớp nhưng dòng tương ứng không còn
	// sai lệch — tức là mã đã sửa xong và ngoại lệ phải bị xóa.
	RedundantExceptions []string
}

// CheckGlossary kiểm tra thuật ngữ trên toàn repo và trả về báo cáo.
//
// Dòng nào của bảng thuật ngữ không nằm trong glossaryExceptions thì được
// siết: bất kỳ chỗ nào nhánh VI dùng khác bảng đều là sai lệch. Dòng nào
// nằm trong danh sách ngoại lệ thì được miễn, nhưng danh sách phải còn
// hiệu lực: ngoại lệ không khớp dòng nào, hoặc khớp một dòng đã không còn
// sai lệch, đều bị báo đỏ — đó là chống rỗng.
func CheckGlossary(root string) (GlossaryReport, error) {
	terms, err := ParseGlossary(filepath.Join(root, "docs", "i18n-vi-glossary.md"))
	if err != nil {
		return GlossaryReport{}, fmt.Errorf("đọc bảng thuật ngữ: %w", err)
	}
	_, tables, pairs, err := scanRepo(root)
	if err != nil {
		return GlossaryReport{}, fmt.Errorf("quét repo: %w", err)
	}
	usages := AnalyzeGlossary(tables, pairs, terms)
	return checkGlossaryUsages(usages), nil
}

// checkGlossaryUsages đối chiếu các thuật ngữ đo được với danh sách ngoại lệ.
func checkGlossaryUsages(usages []GlossaryUsage) GlossaryReport {
	report := GlossaryReport{Terms: len(usages)}
	matched := make([]bool, len(glossaryExceptions))

	for _, usage := range usages {
		report.Occurrences += usage.Occurrences
		idx := exceptionFor(usage.Term)
		if idx < 0 {
			report.Enforced++
			report.EnforcedOccurrences += usage.Occurrences
			report.Violations = append(report.Violations, usage.Violations...)
			continue
		}
		matched[idx] = true
		report.Exempted++
		if len(usage.Violations) == 0 {
			report.RedundantExceptions = append(report.RedundantExceptions,
				glossaryExceptions[idx].ZH[0])
		}
	}

	for i, exc := range glossaryExceptions {
		if !matched[i] {
			report.StaleExceptions = append(report.StaleExceptions, exc.ZH[0])
		}
	}

	sort.Strings(report.StaleExceptions)
	sort.Strings(report.RedundantExceptions)
	sort.Slice(report.Violations, func(i, j int) bool {
		if report.Violations[i].File != report.Violations[j].File {
			return report.Violations[i].File < report.Violations[j].File
		}
		return report.Violations[i].Line < report.Violations[j].Line
	})
	return report
}

// exceptionFor trả về chỉ số của ngoại lệ áp dụng cho một thuật ngữ, hoặc -1.
//
// Ngoại lệ khớp dòng khi hai bên có ít nhất một thuật ngữ tiếng Trung giống
// hệt nhau. Khớp theo tên thay vì theo vị trí để bảng thuật ngữ có thể thêm
// bớt dòng mà danh sách ngoại lệ không bị lệch.
func exceptionFor(term GlossaryTerm) int {
	for i, exc := range glossaryExceptions {
		for _, zh := range exc.ZH {
			for _, termZH := range term.ZH {
				if zh == termZH {
					return i
				}
			}
		}
	}
	return -1
}

// exceptionSummary mô tả một danh sách ngoại lệ để in ra báo cáo.
func exceptionSummary(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, ", ")
}
