package stylestat

import "github.com/voocel/ainovel-cli/internal/i18n"

// styleTexts chứa các văn bản model đọc trong báo cáo thống kê văn phong: tên
// hiển thị của từng lớp mẫu câu.
//
// Biểu thức phát hiện thì giữ nguyên (xem patternDefs trong stylestat.go):
// chúng là đường cơ sở đo trên văn bản Trung, không thuộc phạm vi dịch.
//
// Lớp này không có store nên ngôn ngữ lấy từ i18n.Active(), tức ngôn ngữ tác
// phẩm do store thiết lập và đồng bộ vào biến toàn cục.
type styleTexts struct {
	// patterns là tên của các lớp mẫu câu, theo đúng thứ tự patternDefs.
	patterns []string
}

// Bản tiếng Việt giữ nguyên các mảnh tiếng Trung trong ngoặc 『』, vì đó chính
// là các dấu hiệu cụ thể mà biểu thức phát hiện dò ra: model phải nhận ra
// đúng cấu trúc đang được nói tới, nên phần tiếng Việt chỉ giải thích cấu trúc
// đó chứ không thay tên mẫu.
var styleTextsVI = &styleTexts{
	patterns: []string{
		"Câu chữ đính chính『不是…(而)是…』",
		"Lượng từ đo thời gian『X息/X瞬』",
		"So sánh tương đương『像一/仿佛/如同/宛如』",
		"Nhịp im lặng『沉默了/没有说话/没有回头』",
		"Mẫu biểu cảm『眼中闪过/嘴角勾起/咬了咬唇』",
		"Phản ứng thân thể『心头一紧/身子一颤/倒吸凉气』",
		"Dấu hiệu tư duy『心想/意识到/感到/觉得』",
		"Cụm từ trừu tượng『一种说不出的/的意义在于』",
	},
}

var styleTextsZH = &styleTexts{
	patterns: []string{
		"矫正句『不是…(而)是…』",
		"计时量词『X息/X瞬』",
		"明喻『像一/仿佛/如同/宛如』",
		"沉默节拍『沉默了/没有说话/没有回头』",
		"神态模板『眼中闪过/嘴角勾起/咬了咬唇』",
		"躯体反应『心头一紧/身子一颤/倒吸凉气』",
		"思维标记『心想/意识到/感到/觉得』",
		"抽象套话『一种说不出的/的意义在于』",
	},
}

// styleTextsFor chọn bảng văn bản theo ngôn ngữ tác phẩm.
func styleTextsFor(lang i18n.Lang) *styleTexts {
	return i18n.Table(styleTextsVI, styleTextsZH, lang)
}

// activeTexts trả bảng văn bản theo ngôn ngữ tác phẩm hiện hành.
func activeTexts() *styleTexts {
	return styleTextsFor(i18n.Active())
}
