package imp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// Nhãn mã hoá nguồn được hỗ trợ, ghi vào Manifest và sự kiện tiến trình, không có đường lùi im lặng (RFC §7.1).
const (
	encodingUTF8    = "utf-8"
	encodingUTF8BOM = "utf-8-bom"
	encodingGB18030 = "gb18030"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// decoded là kết quả giải mã một lần: văn bản + mã hoá thực sự được chọn.
type decoded struct {
	text     string
	encoding string
}

// decodeSource giải mã theo thứ tự UTF-8 / UTF-8 BOM / GB18030 và trả về mã hoá đã chọn.
// Khi không giải mã chắc chắn được hoặc xuất hiện ký tự thay thế thì thất bại ngay, lỗi kèm kết
// quả dò; không giấu việc "thử GB18030" thành đường lùi im lặng.
func decodeSource(raw []byte) (decoded, error) {
	if bytes.HasPrefix(raw, utf8BOM) {
		body := raw[len(utf8BOM):]
		if !utf8.Valid(body) {
			return decoded{}, fmt.Errorf("khai báo UTF-8 BOM nhưng nội dung không phải UTF-8 hợp lệ")
		}
		return decoded{text: string(body), encoding: encodingUTF8BOM}, nil
	}
	if utf8.Valid(raw) {
		return decoded{text: string(raw), encoding: encodingUTF8}, nil
	}
	out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(raw)
	if err != nil {
		return decoded{}, fmt.Errorf("không phải UTF-8 hợp lệ, giải mã GB18030 cũng thất bại: %w", err)
	}
	if !utf8.Valid(out) {
		return decoded{}, fmt.Errorf("kết quả giải mã GB18030 vẫn không phải UTF-8 hợp lệ, không giải mã được chắc chắn")
	}
	if i := bytes.IndexRune(out, utf8.RuneError); i >= 0 {
		return decoded{}, fmt.Errorf("giải mã GB18030 xuất hiện ký tự thay thế (U+FFFD @ byte %d), không giải mã được chắc chắn; hãy xác nhận mã hóa tệp", i)
	}
	return decoded{text: string(out), encoding: encodingGB18030}, nil
}

// normalize chỉ làm các chuyển đổi không làm thay đổi nội dung văn chương: CRLF/CR thống nhất về LF.
// Giữ dòng trống, thụt lề, dòng tiêu đề và ký tự trong nội dung truyện; không xoá văn bản đầu,
// chương rỗng, quảng cáo hay cái gọi là nhiễu ở cuối (RFC §7.2).
// BOM đã được tách ở giai đoạn decodeSource.
func normalize(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

// Ingest đọc tệp nguồn, giải mã, chuẩn hoá, rồi tạo ảnh chụp workspace meta/import/ một cách nguyên tử
// bằng cách rename thư mục.
// Trả về handle của workspace cùng Manifest; bên gọi dựa vào đó phát sự kiện tiến trình.
func Ingest(bookDir, sourcePath string, in Intent) (*Workspace, *Manifest, error) {
	raw, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc tệp nguồn: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil, fmt.Errorf("tệp nguồn rỗng: %s", sourcePath)
	}
	dec, err := decodeSource(raw)
	if err != nil {
		return nil, nil, err
	}
	normBytes := []byte(normalize(dec.text))

	m := Manifest{
		Version:          workspaceSchemaVersion,
		SourceName:       filepath.Base(sourcePath),
		RawSHA256:        Digest(raw),
		NormalizedSHA256: Digest(normBytes),
		Encoding:         dec.encoding,
		SizeBytes:        int64(len(raw)),
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	if in.Version == 0 {
		in.Version = workspaceSchemaVersion
	}

	ws, err := createWorkspace(bookDir, m, in, normBytes)
	if err != nil {
		return nil, nil, err
	}
	return ws, &m, nil
}

// SourceUnit là toạ độ ổn định mà mô hình có thể trích dẫn (RFC §7.3).
// ID chỉ dùng để hiển thị và cho mô hình trích dẫn; mọi phán đoán thứ tự / bao hàm / tăng dần đều
// theo thứ tự số của (Line, Part), không được so sánh chuỗi ID theo thứ tự từ điển.
type SourceUnit struct {
	ID        string `json:"id"`   // L1257; dòng vượt ngân sách tách thành L1257.1, L1257.2
	Line      int    `json:"line"` // bắt đầu từ 1
	Part      int    `json:"part"` // 0=cả dòng; lát ảo 1..N
	StartByte int    `json:"start_byte"`
	EndByte   int    `json:"end_byte"`
	Text      string `json:"text"`
}

// unitLess định nghĩa thứ tự toàn phần của SourceUnit: Line trước, Part sau, cả hai đều so sánh theo số (bản sửa A1).
func unitLess(a, b SourceUnit) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Part < b.Part
}

// buildSourceUnits dựng bảng toạ độ ổn định từ văn bản đã chuẩn hoá.
// Dòng bình thường là một unit; khi số byte của một dòng vượt maxUnitBytes thì chỉ sinh nhiều
// unit ảo tại các ranh giới ký tự UTF-8, không ghi lại vào source.txt, không chèn ngắt dòng mềm,
// không đổi bất kỳ ký tự nguồn nào (RFC §7.3). maxUnitBytes<=0 nghĩa là không tách lát.
func buildSourceUnits(normalized []byte, maxUnitBytes int) []SourceUnit {
	var units []SourceUnit
	n := len(normalized)
	line := 0
	offset := 0
	for offset < n {
		nl := bytes.IndexByte(normalized[offset:], '\n')
		lineEnd := n
		if nl >= 0 {
			lineEnd = offset + nl
		}
		line++
		if maxUnitBytes > 0 && lineEnd-offset > maxUnitBytes {
			part := 0
			s := offset
			for s < lineEnd {
				e := s + maxUnitBytes
				if e >= lineEnd {
					e = lineEnd
				} else {
					for e > s && !utf8.RuneStart(normalized[e]) {
						e--
					}
					if e == s { // trường hợp cực đoan: một rune đơn lẻ quá dài
						e = s + maxUnitBytes
					}
				}
				part++
				units = append(units, SourceUnit{
					ID: fmt.Sprintf("L%d.%d", line, part), Line: line, Part: part,
					StartByte: s, EndByte: e, Text: string(normalized[s:e]),
				})
				s = e
			}
		} else {
			units = append(units, SourceUnit{
				ID: fmt.Sprintf("L%d", line), Line: line, Part: 0,
				StartByte: offset, EndByte: lineEnd, Text: string(normalized[offset:lineEnd]),
			})
		}
		if nl < 0 {
			break
		}
		offset = lineEnd + 1
	}
	return units
}

// resolveBoundaryByte ánh xạ một quyết định ranh giới thành vị trí byte chính xác:
// không có anchor thì lấy đầu unit; có anchor thì đòi khớp duy nhất theo từng ký tự bên trong
// unit đó, rồi mới ánh xạ thành độ lệch byte (RFC §8.3).
// t là bảng văn bản theo ngôn ngữ tác phẩm: gọi từ validate của mô hình thì lỗi
// đi vào ngữ cảnh LLM nên phải theo ngôn ngữ đang viết; gọi từ đường vận hành
// thì truyền bản tiếng Việt cố định.
func resolveBoundaryByte(unitByID map[string]SourceUnit, unitID, anchor string, t *importTexts) (int, error) {
	t = importTextsOrDefault(t)
	u, ok := unitByID[unitID]
	if !ok {
		return 0, fmt.Errorf(t.errBoundaryNoUnit, unitID)
	}
	if anchor == "" {
		return u.StartByte, nil
	}
	switch strings.Count(u.Text, anchor) {
	case 0:
		return 0, fmt.Errorf(t.errAnchorNotFound, anchor, unitID)
	case 1:
		return u.StartByte + strings.Index(u.Text, anchor), nil
	default:
		return 0, fmt.Errorf(t.errAnchorNotUnique, anchor, unitID)
	}
}
