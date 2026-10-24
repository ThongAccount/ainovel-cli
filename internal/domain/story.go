package domain

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/i18n"
)

// BookMetadata là thông tin tác phẩm dành cho người đọc và ấn phẩm.
// Thiết lập sáng tác thuộc Foundation, tiến độ chạy thuộc Progress; cả hai đều
// không mang dữ liệu này.
type BookMetadata struct {
	Title    string `json:"title"`
	Synopsis string `json:"synopsis"`
}

// Normalized trả về giá trị chuẩn có thể lưu bền và so sánh.
func (b BookMetadata) Normalized() BookMetadata {
	b.Title = strings.TrimSpace(b.Title)
	b.Synopsis = strings.TrimSpace(b.Synopsis)
	return b
}

// Validate kiểm tra các trường bắt buộc của thông tin tác phẩm.
func (b BookMetadata) Validate() error {
	b = b.Normalized()
	if b.Title == "" {
		return fmt.Errorf("book title is required")
	}
	if b.Synopsis == "" {
		return fmt.Errorf("book synopsis is required")
	}
	return nil
}

// OutlineEntry là một mục đề cương, tương ứng với một chương.
type OutlineEntry struct {
	Chapter   int      `json:"chapter"`
	Title     string   `json:"title"`
	CoreEvent string   `json:"core_event"`
	Hook      string   `json:"hook"`
	Scenes    []string `json:"scenes"`
}

// Character là hồ sơ nhân vật.
type Character struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"` // bí danh/biệt danh/biệt hiệu (ví dụ "Thiếu niên phế", "Anh Việt")
	Role        string   `json:"role"`
	Description string   `json:"description"`
	Arc         string   `json:"arc"`
	Traits      []string `json:"traits"`
	Tier        string   `json:"tier,omitempty"` // core / important / secondary / decorative (mặc định important)
}

// VolumeOutline là đề cương cấp tập (chế độ truyện dài phân tầng).
type VolumeOutline struct {
	Index int          `json:"index"`
	Title string       `json:"title"`
	Theme string       `json:"theme"`           // xung đột/chủ đề cốt lõi của tập
	Final bool         `json:"final,omitempty"` // tập kết: toàn bộ tác phẩm khép lại ở tập này (Kiến trúc sư tuyên bố khi append_volume)
	Arcs  []ArcOutline `json:"arcs"`
}

// IsExpanded kiểm tra tập đã được triển khai (có cấu trúc cấp cung) hay chưa.
func (v *VolumeOutline) IsExpanded() bool { return len(v.Arcs) > 0 }

// FinaleVolume trả về số tập kết đã được tuyên bố, hoặc 0 nếu chưa tuyên bố.
// Sự thật tập kết = "tập cuối có cờ Final": sau khi tuyên bố, toàn bộ tác phẩm
// bước vào trạng thái khép lại (quy hoạch chốt, viết xong tập cuối là hoàn thành);
// nếu sau đó thêm một tập mới không có cờ, tập mới trở thành tập cuối và trạng thái
// khép lại tự nhiên được gỡ bỏ — vì vậy không cần công cụ thu hồi, trạng thái luôn
// có thể suy ra từ dữ liệu đề cương.
func FinaleVolume(volumes []VolumeOutline) int {
	if n := len(volumes); n > 0 && volumes[n-1].Final {
		return volumes[n-1].Index
	}
	return 0
}

// StoryCompass là la bàn hướng kết cục, thay thế danh sách khung tập cố định.
// Architect có thể cập nhật ở mỗi ranh giới tập, cho phép hướng truyện tiến hóa
// cùng quá trình sáng tác.
type StoryCompass struct {
	EndingDirection string   `json:"ending_direction"`          // hướng kết cục (mô tả mang tính chủ đề)
	OpenThreads     []string `json:"open_threads,omitempty"`    // tuyến dài còn mở (phải khép lại mới kết thúc)
	EstimatedScale  string   `json:"estimated_scale,omitempty"` // quy mô mơ hồ (ví dụ "dự kiến 4-6 tập")
	LastUpdated     int      `json:"last_updated,omitempty"`    // số chương đã hoàn thành tại lúc cập nhật
}

// ArcOutline là đề cương cấp cung.
type ArcOutline struct {
	Index             int            `json:"index"` // số thứ tự cung trong tập
	Title             string         `json:"title"`
	Goal              string         `json:"goal"`                         // mục tiêu cung (mở đầu – phát triển – cao trào – kết)
	EstimatedChapters int            `json:"estimated_chapters,omitempty"` // số chương ước tính của cung khung (bị xóa sau khi triển khai)
	Chapters          []OutlineEntry `json:"chapters"`
}

// IsExpanded kiểm tra cung đã được triển khai (có các chương chi tiết) hay chưa.
func (a *ArcOutline) IsExpanded() bool { return len(a.Chapters) > 0 }

// ArcExpansion là kế hoạch đầy đủ cho một cung chưa viết, do Architect lập tại ranh
// giới cấu trúc.
// Title/Goal không phải bản sao máy móc của khung: model có thể sửa kế hoạch về
// những gì chưa xảy ra dựa trên nội dung đã hoàn thành.
type ArcExpansion struct {
	Title    string         `json:"title"`
	Goal     string         `json:"goal"`
	Chapters []OutlineEntry `json:"chapters"`
}

// EstimatedChapterCapacity tính ước lượng sức chứa nội bộ của đề cương phân tầng:
// cung đã triển khai dùng số chương thật, cung khung dùng EstimatedChapters. Giá
// trị này chỉ dùng cho chiến lược ngữ cảnh, không phải tổng số chương của tác
// phẩm; các chương đã chi tiết hóa và có thể viết luôn đến từ FlattenOutline,
// tuyệt đối không lộ giá trị này cho người dùng hoặc model.
func EstimatedChapterCapacity(volumes []VolumeOutline) int {
	n := 0
	for _, v := range volumes {
		for _, a := range v.Arcs {
			if a.IsExpanded() {
				n += len(a.Chapters)
			} else {
				n += a.EstimatedChapters
			}
		}
	}
	return n
}

// FlattenOutline mở rộng đề cương phân tầng thành danh sách chương phẳng, giữ số
// chương toàn cục liên tục.
func FlattenOutline(volumes []VolumeOutline) []OutlineEntry {
	var result []OutlineEntry
	ch := 1
	for _, v := range volumes {
		for _, a := range v.Arcs {
			for _, e := range a.Chapters {
				e.Chapter = ch
				result = append(result, e)
				ch++
			}
		}
	}
	return result
}

// WorldRule là một mục luật thế giới.
type WorldRule struct {
	Category string `json:"category"` // magic / technology / geography / society / other
	Rule     string `json:"rule"`     // mô tả luật
	Boundary string `json:"boundary"` // ranh giới không được vi phạm
}

// RenumberVolumes đánh số lại tập và cung theo vị trí, bắt đầu từ 1.
//
// Model quy hoạch không viết index đáng tin cậy: thường đếm từ 0, thậm chí ghi 0
// cho mọi cung trong cùng một tập. Mà ExpandArc / ArcScope tìm theo giá trị index,
// nên giá trị lặp hoặc 0 khiến cung không bao giờ truy cập được — triệu chứng chỉ
// xuất hiện vài bước sau ở expand_arc báo "tham số không hợp lệ", hoàn toàn chỉ sai
// hướng.
//
// Thứ tự mảng mới là sự thật, index chỉ là tên của nó, nên trước khi ghi xuống
// luôn đánh số lại theo vị trí.
func RenumberVolumes(volumes []VolumeOutline) {
	for vi := range volumes {
		volumes[vi].Index = vi + 1
		for ai := range volumes[vi].Arcs {
			volumes[vi].Arcs[ai].Index = ai + 1
		}
	}
}

// Cùng một điểm móc/sự kiện chính lặp tới số lần này bị coi là đề cương đi lòng
// vòng. Hai lần có thể là chủ ý theo hai phần; từ ba lần trở đi không có cách viết
// chính đáng: người đọc bị treo bởi cùng một câu hỏi suốt ba chương mà không ai giải
// đáp.
const (
	maxHookRepeat      = 3
	maxCoreEventRepeat = 2
)

// StalledOutline phát hiện đề cương "đi lòng vòng": tiêu đề chương khác nhau nhưng
// điểm móc hoặc sự kiện chính là cùng một câu sao chép nhiều lần. Khi đó Writer
// sẽ trung thành thực hiện — mỗi chương lặp lại chương trước rồi thêm chút, đọc
// giống viết lại hơn là viết tiếp.
//
// Cả Writer lẫn Editor đều không nhận ra loại khiếm khuyết này: cả hai chỉ
// nhìn một chương và chương đó tự nhất quán. Phải so sánh toàn bộ tác phẩm tại nơi
// ghi đề cương mới chặn được.
//
// Trả về chuỗi rỗng nếu đạt; nếu không, trả về chẩn đoán song ngữ có thể gửi thẳng
// cho người lập kế hoạch.
func StalledOutline(entries []OutlineEntry) string {
	hooks := map[string]int{}
	events := map[string]int{}
	for _, e := range entries {
		if h := strings.TrimSpace(e.Hook); h != "" {
			hooks[h]++
		}
		if c := strings.TrimSpace(e.CoreEvent); c != "" {
			events[c]++
		}
	}
	if worst, n := mostRepeated(hooks); n >= maxHookRepeat {
		return i18n.Text{
			VI: fmt.Sprintf("Đề cương đi lòng vòng: cùng hook lặp trong %d/%d chương — %q. "+
				"Hook của mỗi chương phải là hệ quả mới sinh ra trong chính chương đó và được chương sau giải đáp; vui lòng viết lại từng chương", n, len(entries), truncateRunes(worst, 40)),
			ZH: fmt.Sprintf("大纲空转：同一 hook 在 %d/%d 章重复——%q。"+
				"每章 hook 必须是本章新产生的后果，且由下一章兑现；请逐章改写", n, len(entries), truncateRunes(worst, 40)),
		}.Resolve(i18n.Active())
	}
	if worst, n := mostRepeated(events); n >= maxCoreEventRepeat {
		return i18n.Text{
			VI: fmt.Sprintf("Đề cương đi lòng vòng: cùng core_event lặp trong %d chương — %q. "+
				"Mỗi chương phải xảy ra việc khác nhau và làm thay đổi tình thế; vui lòng viết lại từng chương", n, truncateRunes(worst, 40)),
			ZH: fmt.Sprintf("大纲空转：同一 core_event 在 %d 章重复——%q。"+
				"每章须发生不同的事并改变处境；请逐章改写", n, truncateRunes(worst, 40)),
		}.Resolve(i18n.Active())
	}
	return ""
}

func mostRepeated(m map[string]int) (string, int) {
	var key string
	best := 0
	for k, n := range m {
		if n > best {
			key, best = k, n
		}
	}
	return key, best
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// SkeletonArcs đếm các cung khung chưa triển khai, dùng cho kiểm tra trước khi
// hoàn thành tác phẩm.
//
// Kiểm tra hoàn thành chỉ so sánh đề cương phẳng, còn đề cương phẳng do
// FlattenOutline suy ra từ "cung đã triển khai" — cung khung đóng góp 0 chương và
// hoàn toàn vô hình với kiểm tra này. Sự cố thực nghiệm: hai cung khung ở tập 1
// cộng lại 38 chương chưa từng triển khai, Kiến trúc sư nhảy thẳng sang tập 2, viết
// xong 15 chương rồi tuyên bố hoàn thành; kiểm tra cho qua vì next(16) > len(flat)(15).
//
// Muốn khép lại sớm vẫn có lối thoát chính đáng: append_volume với "final": true để
// tuyên bố tập kết.
func SkeletonArcs(volumes []VolumeOutline) []string {
	var out []string
	for vi := range volumes {
		for ai := range volumes[vi].Arcs {
			if a := &volumes[vi].Arcs[ai]; !a.IsExpanded() {
				out = append(out, i18n.Text{
					VI: fmt.Sprintf("Tập %d, cung %d「%s」", volumes[vi].Index, a.Index, a.Title),
					ZH: fmt.Sprintf("第 %d 卷第 %d 弧「%s」", volumes[vi].Index, a.Index, a.Title),
				}.Resolve(i18n.Active()))
			}
		}
	}
	return out
}

// maxArcChapters là giới hạn số chương chi tiết trong một cung.
//
// Ràng buộc đến từ thẩm duyệt cuối cung, không phải khẩu vị văn bản: Editor tại
// ranh giới cung phải đọc toàn bộ cung mới có thể đưa ra nhận xét. Thực nghiệm một
// cung 20 chương = 113792 chữ ≈ 37k token nội dung, cộng đề cương/ảnh chụp/prompt
// thì không model dùng được nào chịu nổi — cửa sổ 32k cục bộ không chứa nổi, gói
// miễn phí trên đám mây ở 8 tok/s liên tục đứt luồng 14 lần và cuối cùng toàn bộ
// pipeline kẹt tại ranh giới cung. Tám chương ≈ 45k chữ ≈ 15k token, hai bên đều
// còn dư địa.
//
// Về mặt cấu trúc, đây cũng là điều tốt: nhồi 20 chương vào một cung vốn đã chỉ ra
// mục tiêu cung chưa hội tụ.
const maxArcChapters = 8

// OversizedArc kiểm tra quy mô của một cung có vượt giới hạn hay không; nếu vượt,
// trả về chẩn đoán song ngữ có thể gửi thẳng cho người lập kế hoạch.
// Trả về chuỗi rỗng nếu đạt.
//
// chapters lấy giá trị lớn hơn giữa "số chương chi tiết" và "số chương ước tính
// của khung": một cung đã ghi estimated=20 ngay ở giai đoạn khung sẽ chắc chắn
// đụng cùng giới hạn khi expand_arc, nhưng lúc đó đã qua hai mươi chương — vấn đề
// cấu trúc phải được báo ngay khi cấu trúc được ghi xuống.
func OversizedArc(label string, chapters int) string {
	if chapters <= maxArcChapters {
		return ""
	}
	return i18n.Text{
		VI: fmt.Sprintf("%s có %d chương chi tiết, vượt giới hạn %d chương cho một cung. "+
			"Thẩm duyệt cuối cung phải đọc toàn bộ cung một lần, nên không model nào có thể thẩm duyệt cung quá dài (thực nghiệm 20 chương đã làm tắc pipeline). "+
			"Vui lòng chia thành nhiều cung, mỗi cung có mục tiêu độc lập: lần gọi này chỉ triển khai cung đầu tiên trong %d chương đầu; "+
			"phần còn lại để thành cung khung, triển khai tiếp khi viết tới ranh giới",
			label, chapters, maxArcChapters, maxArcChapters),
		ZH: fmt.Sprintf("%s 详细章节 %d 章，超过单弧上限 %d 章。"+
			"弧末评审需一次读完整弧，过长的弧任何模型都无法审阅（实测 20 章即卡死流水线）。"+
			"请把它拆成多个各自有独立目标的弧：先用本次调用只展开前 %d 章内的第一个弧，"+
			"其余留作骨架弧，写到边界时再展开",
			label, chapters, maxArcChapters, maxArcChapters),
	}.Resolve(i18n.Active())
}
