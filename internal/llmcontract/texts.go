package llmcontract

import "github.com/voocel/ainovel-cli/internal/i18n"

// contractTexts chứa toàn bộ văn bản mô hình đọc trong llmcontract.
//
// Nội dung ở đây đi thẳng vào prompt: hậu tố hợp đồng đầu ra, câu nhắc tự
// sửa khi model vi phạm schema, và thông báo lỗi kiểm tra hợp lệ được đưa
// lại cho model. Văn bản phải theo ngôn ngữ tác phẩm — model đang viết
// tiếng Việt mà nhận chỉ dẫn tiếng Trung sẽ bị kéo về tiếng Trung (xem
// internal/i18n và internal/store/labels.go).
type contractTexts struct {
	// Hậu tố hợp đồng gắn vào prompt ở chế độ prompt contract.
	outputContract string
	// Câu nhắc tự sửa khi đầu ra vi phạm schema hoặc vi phạm nghiệp vụ.
	promptCorrection   string
	semanticCorrection string
	errorPrefix        string

	// Thông báo lỗi kiểm tra hợp lệ — đưa lại cho model để tự sửa.
	jsonParseFailed     string
	jsonExtraValue      string
	jsonTrailingInvalid string
	contractInvalid     string
	mustBeType          string
	mustBeOneOf         string
	missingProperties   string
	missingItems        string
	requiredInvalid     string
	requiredField       string
	undeclaredField     string
	notAnObject         string
	unionOnlyStrings    string
	typeMustBe          string
	mustBeStringArray   string
	onlyStringOrNull    string
	mustBeArray         string
	validJSONValue      string
	noJSONObject        string
	strictNotRequired   string
}

var contractTextsVI = &contractTexts{
	outputContract: "## Hợp đồng đầu ra\n\n" +
		"Chỉ xuất một đối tượng JSON khớp với JSON Schema dưới đây; không xuất phần giải thích, hàng rào Markdown hay chính thẻ này.",

	promptCorrection:   "Đầu ra ở trên không khớp với JSON Schema. Hãy sửa theo lỗi và chỉ xuất đối tượng JSON hoàn chỉnh, không giải thích, không hàng rào Markdown.",
	semanticCorrection: "JSON ở trên có cấu trúc hợp lệ nhưng giá trị trường chưa vượt qua kiểm tra nghiệp vụ. Hãy sửa theo lỗi và xuất lại đối tượng JSON hoàn chỉnh.",
	errorPrefix:        "\nLỗi: ",

	jsonParseFailed:     "JSON phân tích không thành công: %w",
	jsonExtraValue:      "JSON có giá trị thừa phía sau",
	jsonTrailingInvalid: "Phần cuối JSON không hợp lệ: %w",
	contractInvalid:     "%s hợp đồng không hợp lệ: %w",
	mustBeType:          "%s phải là %s, thực tế là %s",
	mustBeOneOf:         "%s phải là một trong %v, thực tế là %v",
	missingProperties:   "%s hợp đồng thiếu properties",
	missingItems:        "%s hợp đồng thiếu items",
	requiredInvalid:     "%s.required hợp đồng không hợp lệ: %w",
	requiredField:       "%s.%s là trường bắt buộc",
	undeclaredField:     "%s.%s không được khai báo trong hợp đồng",
	notAnObject:         "%s.%s hợp đồng không phải object",
	unionOnlyStrings:    "type dạng hợp nhất chỉ được chứa chuỗi",
	typeMustBe:          "type phải là chuỗi hoặc mảng chuỗi",
	mustBeStringArray:   "phải là mảng chuỗi",
	onlyStringOrNull:    "chỉ hỗ trợ chuỗi và null",
	mustBeArray:         "phải là mảng",
	validJSONValue:      "giá trị JSON hợp lệ",
	noJSONObject:        "không tìm thấy đối tượng JSON trong đầu ra",
	strictNotRequired:   "%s.%s chưa được liệt kê trong required (strict yêu cầu mọi thuộc tính đều required)",
}

var contractTextsZH = &contractTexts{
	outputContract: "## 输出契约\n\n" +
		"只输出一个符合下列 JSON Schema 的 JSON 对象，不要输出解释、Markdown 围栏或标签本身。",

	promptCorrection:   "上面的输出不符合 JSON Schema。请根据错误修正，并只输出完整 JSON 对象，不要解释或 Markdown 围栏。",
	semanticCorrection: "上面的 JSON 结构合法但字段取值未通过业务校验。请根据错误修正，并重新输出完整 JSON 对象。",
	errorPrefix:        "\n错误：",

	jsonParseFailed:     "JSON 解析失败: %w",
	jsonExtraValue:      "JSON 后存在额外值",
	jsonTrailingInvalid: "JSON 尾部非法: %w",
	contractInvalid:     "%s 契约非法: %w",
	mustBeType:          "%s 必须是 %s，实际为 %s",
	mustBeOneOf:         "%s 必须是 %v 之一，实际为 %v",
	missingProperties:   "%s 契约缺少 properties",
	missingItems:        "%s 契约缺少 items",
	requiredInvalid:     "%s.required 契约非法: %w",
	requiredField:       "%s.%s 是必填字段",
	undeclaredField:     "%s.%s 未在契约中声明",
	notAnObject:         "%s.%s 契约不是对象",
	unionOnlyStrings:    "type 联合必须只包含字符串",
	typeMustBe:          "type 必须是字符串或字符串数组",
	mustBeStringArray:   "必须是字符串数组",
	onlyStringOrNull:    "只支持字符串和 null",
	mustBeArray:         "必须是数组",
	validJSONValue:      "有效 JSON 值",
	noJSONObject:        "输出中未找到 JSON 对象",
	strictNotRequired:   "%s.%s 未列入 required(strict 要求全属性 required)",
}

// contractTextsFor chọn bảng văn bản theo ngôn ngữ tác phẩm.
func contractTextsFor(lang i18n.Lang) *contractTexts {
	return i18n.Table(contractTextsVI, contractTextsZH, lang)
}
