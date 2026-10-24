# Thiết kế lớp văn phong (Voice Layer)

> Trạng thái: thiết kế chốt v2 (2026-07-12, đã hấp thụ phản biện bên ngoài: bổ sung ngữ nghĩa ghi đè, ngữ nghĩa đường dẫn, thứ tự lắp ráp, điểm vào eval, giao thức thống kê đánh giá), **có thể triển khai**.
> Độ ưu tiên: đi trước sự tiến hoá của mặt phẳng điều khiển (docs/engine-arbiter.md) — "mùi AI" là nỗi đau đang nóng của người dùng.

## 1. Bối cảnh và định nghĩa vấn đề

Người dùng phản hồi rằng nội dung sinh ra "nặng mùi AI". Sau khi truy tìm, kết luận là: **vấn đề không nằm ở chỗ tri thức văn phong bị gắn quá chặt vào quy trình, mà ở chỗ vòng lặp cải tiến đứt ở hai điểm**:

1. **Sửa một lần là phải biên dịch lại** — các tài sản ngữ nghĩa văn phong (anti-ai-tone.md, phần tiêu chuẩn viết trong writer.md, styles/*.md) đều được `go:embed`; chỉ cần chỉnh một cách diễn đạt là phải dựng lại và phát hành;
2. **Không có vòng đo lường riêng cho văn phong** — sửa xong chỉ có thể dựa vào cảm nhận khi đọc, không có đối chiếu khách quan trước/sau, nên việc tối ưu hoá hoá thành thứ huyền học.

## 2. Đánh giá hiện trạng (năm lớp tài sản liên quan đến văn phong)

| Lớp | Vị trí | Hiện trạng | Người dùng chỉnh được? |
|----|------|------|---------|
| Tiêu chí ngữ nghĩa | `assets/references/anti-ai-tone.md` | writer tránh + editor dùng làm bằng chứng, gồm năm loại: cấu trúc / từ ngữ / miêu tả / đối thoại / nhịp độ | ❌ Nhúng sẵn |
| Tiêu chuẩn viết | `assets/prompts/writer.md` §Tiêu chuẩn viết | trộn chung với giao thức thực thi trong cùng một tệp nhúng | ❌ |
| Phong cách cài sẵn | `assets/styles/*.md` (4 cái) | cfg.Style chọn một điểm, được nối thêm vào writer prompt | ❌ và không thêm mới được |
| Quy tắc cơ học | `internal/rules` | từ mệt mỏi / cụm từ cấm / số từ, commit kiểm tra bắt buộc | ✅ đã có ba lớp phủ (lưu ý: ràng buộc "cấp dự án" của nó **đóng theo cwd**, xem 3.4) |
| Sở thích lúc chạy | hành động `rules` của Arbiter | ngôn ngữ tự nhiên → có cấu trúc, hiệu lực xuyên suốt các lần khởi động lại | ✅ |

Ngoài ra còn hai nền tảng then chốt: **stylestat** (thống kê tic ở mức câu của cả cuốn sách, đưa ngược lại writer làm "ảnh chụp đối chiếu lời lẽ", hoàn toàn bằng code nên không có ảo giác) và **`OverridePrompt`** của eval (hạ tầng A/B prompt đã có sẵn).

Kết luận: khả năng chỉnh của lớp cơ học và nguyên liệu đo lường đã sẵn sàng; khoảng trống nằm ở chỗ **lớp ngữ nghĩa không thể ghi đè** và **vòng đo lường chưa ngắm vào văn phong**.

## 3. Thiết kế

### 3.1 Nguyên tắc cốt lõi

**Tách "viết như thế nào" (văn phong) ra khỏi "phối hợp như thế nào" (giao thức): cái trước được dữ liệu hoá và có thể ghi đè; cái sau giữ nguyên dạng nhúng sẵn trong bản biên dịch.**

### 3.2 Tách writer.md: điền lại tại chỗ bằng placeholder

Phần tiêu chuẩn viết trong writer.md nằm ở **giữa** tệp (sau giao thức thực thi, trước phần tính liên tục của nhân vật phụ), nên không thể đơn giản nối vào cuối. Ta dùng phương án placeholder:

- `writer.md` (giao thức, nhúng sẵn): giữ giao thức thực thi / chạy tiếp từ điểm dừng / viết lại và gọt giũa / điều khoản chương / mô tả cơ chế tùy chọn người dùng / **toàn bộ phần số từ (kèm gợi ý cách viết)** / tính liên tục của nhân vật phụ / tham số commit; vị trí của phần tiêu chuẩn viết cũ được thay bằng **một** placeholder `{{VOICE}}`
- `voice.md` (văn phong, có thể ghi đè): toàn bộ phần tiêu chuẩn viết (khử mùi AI / đa dạng cấu trúc câu / không kể lại tình cảnh trước)

Gợi ý cách viết cho phần số từ vẫn nằm lại trong tệp giao thức (được phản biện 2026-07-12 chấp nhận): nó gắn chặt với việc thực thi hợp đồng số từ, tách ra sẽ cần placeholder thứ hai và biến Voice thành định dạng nhiều mảnh — không đáng để dành cho một đoạn kỹ thuật mà rất ít người muốn ghi đè; sở thích về số từ của người dùng đi qua user_rules. Tên tệp giữ nguyên `writer.md` (eval `OverridePrompt` dùng tên tệp làm khoá, đổi tên chỉ thêm động phối).

**Thứ tự lắp ráp phải tương thích từng byte với hiện trạng.** Hiện trạng là `writer.md → simulationGuidance → style` (assets/load.go:84 + agents/build.go:247), vì vậy chỉ có một hàm lắp ráp:

```go
// lối vào duy nhất cho sản xuất, eval và test; điền lại {{VOICE}} tại chỗ bảo đảm việc tách không mất mát
func BuildWriterPrompt(protocolTemplate, voice, simulationGuidance, style string) string
// = replace(protocolTemplate, "{{VOICE}}", voice) + simulationGuidance + style
```

Bài học từ tiền lệ: chú thích của `WithSimulationGuidance` đã ghi lại cái hố "baseline có bọc, variant thì không → A/B không tương đương"; đường lắp ráp phân nhánh là mầm mống của loại tai nạn tương tự, nên ta gộp về một hàm.

### 3.3 Mô hình ghi đè: ngữ nghĩa theo từng tài sản (không mơ hồ)

| Tài sản | Ngữ nghĩa ghi đè | Lý do |
|------|---------|------|
| `voice.md` | **Nối thêm**: giữ bản tích hợp, phần toàn cục / của cuốn sách được nối thêm như một khối có nhãn đánh dấu | Thay cả tệp sẽ khiến người dùng vĩnh viễn kẹt ở bản tích hợp cũ; nhu cầu phổ biến là tinh chỉnh chứ không phải viết lại |
| `anti-ai-tone.md` | **Nối thêm** (như trên) | Nhu cầu phổ biến là bổ sung tiêu chí; người dùng muốn lật đổ bản tích hợp rất hiếm, không thiết kế riêng cho họ |
| `styles/<name>.md` | **Thay cả tệp cùng tên**; tên tệp mới tức là thêm một phong cách mới | Phong cách là giọng tổng thể, trộn hai phong cách là vô nghĩa |
| `genres/<name>/style-references.md` | Thay cả tệp cùng tên; style tự định nghĩa không có reference thì **cho phép thiếu, không lùi về default** (tham chiếu sai còn tệ hơn là không có) | Như trên |
| user_rules | ưu tiên cao nhất lúc chạy (giữ nguyên hiện trạng) | — |

Phần lắp ráp theo ngữ nghĩa nối thêm có nhãn ranh giới tường minh:

```
## Văn phong mặc định của dự án
...
## Văn phong ghi đè toàn cục của người dùng (các yêu cầu dưới đây ưu tiên hơn mặc định của dự án)
...
## Văn phong ghi đè của cuốn sách này (các yêu cầu dưới đây ưu tiên hơn tất cả những gì ở trên)
...
```

**Ranh giới trung thực**: trong ngữ nghĩa nối thêm, "cái sau thắng" chỉ là chỉ dẫn ưu tiên dành cho LLM, không phải bảo đảm cơ học — văn phong vốn là nội dung mang tính gợi ý nên điều này chấp nhận được; ràng buộc nào cần bảo đảm cơ học thì đi qua tầng rules (nơi ghi đè thật sự). Ranh giới này được ghi vào tài liệu người dùng.

`arc-templates.md` thuộc mặt phẳng quy hoạch (định hình cấu trúc truyện chứ không phải giọng), **không nằm trong danh sách trắng v1**, ghi nhận để bàn.

### 3.4 Ngữ nghĩa đường dẫn: ràng buộc cấp cuốn sách theo outputDir, không theo cwd

```
Cấp cuốn sách   <outputDir>/style/     >   Toàn cục   ~/.ainovel/style/   >   Mặc định tích hợp (embed dự phòng)
```

- Ràng buộc theo outputDir khiến Voice **đi cùng cuốn sách**: khôi phục cùng một cuốn sách ở thư mục khác vẫn nạp cùng một bản văn phong; đường dẫn phân giải nhất quán giữa Docker/headless/TUI; nhiều cuốn sách dùng chung cwd không bị lẫn nội dung
- Chữ ký `assets.Load` nhận rõ ràng gốc phân giải (outputDir), **bên trong không đọc cwd**
- Lưu ý khác biệt với tầng rules: `./.ainovel/rules` của rules gắn theo cwd (quy ước sẵn có ở internal/rules/loader.go, thiết kế này không đụng tới); tài liệu người dùng làm rõ hai bên khác nhau về ngữ nghĩa — rules là "cấp dự án", voice là "cấp cuốn sách"

Cấu trúc đầy đủ trong thư mục người dùng:

```
<outputDir>/style/            (đồng cấu trúc với ~/.ainovel/style/)
  voice.md                    khối nối thêm
  anti-ai-tone.md             khối nối thêm
  styles/
    xianxia.md                thêm mới hoặc thay tệp cùng tên
  genres/
    xianxia/
      style-references.md     tùy chọn
```

Tên style chính là tên tệp, kiểm tra theo `[a-z0-9-]+`, từ chối ký tự đường dẫn.

### 3.5 Vì sao mở cho người dùng là an toàn

Mọi bất biến của giao thức đều nằm ở **tầng sự thật**: draft đi trước check, commit bắt buộc kiểm tra quy tắc cơ học, chặn khi vượt số từ, checkpoint idempotent — không nằm trong prompt. Dù người dùng sửa voice.md quá đà, các chốt chặn và điều kiện tiền quyết của tool vẫn có hiệu lực; kết quả tệ nhất chỉ là văn phong xấu đi, máy trạng thái thì không hỏng.

### 3.6 Thời điểm có hiệu lực và điểm vào eval

- v1 phân giải lúc khởi động, **có hiệu lực sau khi khởi động lại** (khôi phục từ điểm dừng chính xác tới từng bước, chi phí khởi động lại gần như bằng không; không làm tải lại nóng)
- eval thêm **điểm vào variant voice độc lập** (ví dụ `Bundle.OverrideVoice(raw)`), bên trong đi qua đúng đường `BuildWriterPrompt` — cấm làm A/B văn phong bằng cách ghi đè cả writer.md (sẽ kéo theo cả giao thức, và phần giao thức của baseline/variant có thể không tương đương)

## 4. Vòng đo lường: bộ đánh giá văn phong

```
Sửa voice/anti-ai-tone
  → bộ đánh giá văn phong (các ca cố định, eval voice-variant A/B)      ← thứ duy nhất được thêm
  → so sánh chỉ số stylestat (chỉ số cứng, tất định)
  + LLM judge chấm điểm từng hạng mục theo tiêu chí anti-ai-tone, kèm bằng chứng (ban đầu chỉ báo cáo, chưa làm hard gate)
```

Giao thức thống kê (đầu vào cố định chỉ bảo đảm **khả năng so sánh**, không bảo đảm tái lập được):

- baseline/variant dùng chung một model và tham số suy luận đã khoá
- mỗi ca lặp N≥3 lần, báo cáo trung bình, phương sai và các mẫu thô
- judge chấm mù (không lộ danh tính baseline/variant)
- các ca phủ thể loại × loại chương (mở đầu / tiến triển thường ngày / đỉnh cao / chốt)

## 5. Những việc dứt khoát không làm (chống thiết kế quá mức)

- không mở prompt giao thức cho người dùng cuối (`OverridePrompt` giữ làm năng lực nội bộ của eval)
- không làm tải lại nóng lúc đang chạy
- không mở các mẫu regex của stylestat thành cấu hình người dùng (đã có sẵn điểm mở rộng của lớp cơ học: fatigue_words/forbidden_phrases trong rules)
- không làm chợ phong cách / cơ chế chia sẻ (chỉ cần sao chép thư mục style là đã chia sẻ được)
- arc-templates không nằm trong danh sách trắng v1

## 6. Các bước triển khai và tiêu chí nghiệm thu

1. Tách writer.md (placeholder `{{VOICE}}`) + hàm lắp ráp duy nhất `BuildWriterPrompt`
2. Bộ phân giải ba tầng: `assets.Load(outputDir, style)` + ngữ nghĩa theo từng tài sản (bảng 3.3) + hợp nhất danh mục styles; đơn vị kiểm thử phủ thứ tự ưu tiên / dự phòng khi thiếu / nhãn ranh giới khi nối thêm
3. Điểm vào `OverrideVoice` cho eval
4. Tài liệu người dùng: cấu trúc thư mục, ngữ nghĩa theo từng tài sản, khác biệt ngữ nghĩa đường dẫn giữa rules và voice, ví dụ
5. Bộ đánh giá văn phong (có thể tách thành nhiệm vụ riêng, làm sau)

**Tiêu chí nghiệm thu**: ① khi không có tệp ghi đè nào, sản phẩm của `BuildWriterPrompt` **giống từng byte** với trước khi tách; ② thứ tự ưu tiên ba tầng và ngữ nghĩa nối thêm/thay thế có đơn vị kiểm thử dạng bảng; ③ sau khi thêm `styles/xianxia.md`, khai báo `style: xianxia` là dùng được ngay; ④ A/B voice của eval đi cùng đường lắp ráp với sản xuất (có kiểm thử chứng minh); ⑤ toàn bộ kiểm thử và hồi quy sim đều xanh.

## 7. Quan hệ với sự tiến hoá của mặt phẳng điều khiển

Hoàn toàn trực giao (mặt phẳng nội dung so với mặt phẳng điều khiển), không phụ thuộc triển khai. Thứ tự đã thống nhất: **lớp văn phong → bộ đánh giá văn phong → Engine/Arbiter (tiến hành theo phương án quyết định ở §8 của tài liệu đó)**.
