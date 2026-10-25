# Pipeline nhập ngữ nghĩa tiểu thuyết bên ngoài

> Trạng thái: đã hiện thực (v1, `internal/host/imp`; cứu vãn tiền tố bị cắt ngắn bao gồm giai đoạn ba·bổ sung)
> Ngày: 2026-07-15
> Mục tiêu: để việc nhập tiểu thuyết bên ngoài vừa liên tục hưởng lợi ích từ các nâng cấp năng lực mô hình, vừa có các bảo đảm kỹ thuật về không mất toàn bộ nội dung, có thể chẩn đoán thất bại, có thể khôi phục sau sự cố và có thể xác minh trước khi phát hành.
> Sửa đổi: thứ tự SourceUnit theo thứ tự số của `(Line, Part)` (§7.3/§8.3); cứu vãn tiền tố bị cắt ngắn được hạ cấp thành tối ưu hiệu quả có thể hoàn thành sau và phải có khả năng quan sát được (§9.5/§13.3/§19); bậc mô hình cho hàm ngữ nghĩa được mở thành công tắc (§13.1/§17).
> Sửa đổi 2026-07-16: công tắc bậc mô hình được hiện thực dưới dạng cấu hình roles `import_segment/import_analyze/import_synthesize` (§13.1); việc chẻ lại theo ngôn ngữ tự nhiên được hiện thực dưới dạng đầu vào ngữ nghĩa qua `--guide` và tệp `guidance.txt` trong workspace (§18.3); thất bại ngữ nghĩa thống nhất lưu phản hồi gốc vào `failures/` (§14.2); xác nhận chẻ lại hỗ trợ phím `y` trong bảng điều khiển để cho phép một lần (§8.4); việc nhập chưa hoàn tất được nhắc nhở chủ động khi khởi động (§18.2). chế độ JSON Schema (§13.2 cấp 1) tạm thời chưa hiện thực, được đánh dấu TODO để chờ cải tạo thống nhất cùng các điểm gọi mô hình khác trong toàn repo.

## 1. Trong một câu

Nhập không phải là “dùng regex để cắt văn bản, rồi bắt mô hình phun ra toàn bộ JSON của một cuốn sách trong một lần”, cũng không phải là một Import Agent tự do chạy; đó là một **pipeline biên dịch ngữ nghĩa nhiều giai đoạn**:

> Mô hình chịu trách nhiệm hiểu ngữ nghĩa mở, mã nguồn chịu trách nhiệm tọa độ, độ bao phủ, kiểu dữ liệu, hash, thứ tự và tính lũy thừa; mọi sản phẩm ngữ nghĩa chỉ được phát hành sang trạng thái sách chính thức sau khi đã hoàn tất kiểm tra hợp lệ trong workspace độc lập.

```text
Văn bản bên ngoài
  → Đọc và chuẩn hóa tất định
  → LLM nhận diện ranh giới chương/tập/văn bản phụ
  → Mã nguồn kiểm tra hợp lệ độ bao phủ toàn văn
  → Người dùng xác nhận kết quả chẻ lại（có thể tường minh ủy quyền tự động chấp nhận）
  → LLM trích xuất sự thật theo từng chương theo lô chương liên tiếp
  → LLM tổng hợp ngữ nghĩa toàn bộ sách theo tầng
  → Mã nguồn lắp ráp và kiểm tra hợp lệ Foundation
  → Phát hành Foundation và các chương một cách idempotent
  → Mặc định tạm dừng một lần; chỉ khi tường minh --continue mới tiếp tục theo các cổng chặn thông thường
```

## 2. Vì sao bắt buộc phải tái cấu trúc

Hiện trạng triển khai là:

```text
Chẻ chương bằng regex
  → Đưa toàn bộ nội dung các chương vào ReverseFoundation trong một lần
  → Mô hình xuất một lần premise / characters / world_rules / dàn ý đầy đủ các chương / compass
  → Ghi ngay vào Foundation chính thức
  → Sau đó đọc lại cùng nội dung, phân tích và commit từng chương
```

Nó có bốn vấn đề về cấu trúc.

### 2.1 Việc cắt chương đang liệt kê ngữ nghĩa mở

Tiêu đề chương không có cú pháp đóng. Chỉ thêm các regex như “Chương N”, “Tập N”, “Chapter N” chỉ có thể bao phủ những định dạng đã từng thấy, không thể bao phủ tiêu đề tự do của tác giả, bố cục trộn lẫn, cấu trúc phân cấp tập–chương và các định dạng trong tương lai.

Nghiêm trọng hơn, việc chẻ lại hiện tại khiến các ranh giới không khớp biến mất trực tiếp khỏi kết quả, đồng thời có thể âm thầm loại bỏ văn bản trước tiêu đề đầu tiên, các chương rỗng và nội dung bị đánh giá là nhiễu ở phần cuối. Mã nguồn không thể chứng minh rằng những nội dung này nên bị loại bỏ.

### 2.2 Đầu vào và đầu ra của lệnh gọi Foundation đều tăng tuyến tính theo số chương

`ReverseFoundation` đồng thời gánh phần hiểu toàn bộ cuốn sách và tạo dàn ý đầy đủ cho tất cả các chương: đầu vào chứa toàn bộ nội dung truyện, còn đầu ra chứa cấu trúc chi tiết của từng chương. Với 54 chương, JSON đã có thể bị cắt ngắn; tăng `max_tokens` chỉ đẩy điểm thất bại sang những cuốn sách dài hơn.

### 2.3 Trạng thái chính thức đã bị thay đổi trước khi xảy ra thất bại

Foundation và các chương được phát hành trong khi vẫn đang được phân tích. Khi một bước sau thất bại, người dùng nhận được trạng thái sách chính thức với một nửa đã nhập xong và một nửa chưa được phân tích. `from=N` hiện tại chỉ giả định rằng người dùng biết phải khôi phục từ đâu, không thể chứng minh rằng tệp nguồn, kết quả chẻ lại và các chương đã tồn tại vẫn nhất quán.

### 2.4 Nhiều kết luận ngữ nghĩa bị hard-code

Phương án hiện tại còn cố định:

- Nội dung truyện được nhập chỉ được có một tập;
- Chỉ có thể chia thành 1～3 cung;
- Chọn short/mid/long theo ngưỡng 25/80 dựa trên số chương đã nhập;
- Có xu hướng cố tình tạo `open_threads` để cho phép viết tiếp;
- Mỗi chương bắt buộc phải có nhân vật, số sự kiện cố định và loại hook cố định.

Không điều nào trong số đó là sự thật có thể chứng minh một cách máy móc từ định dạng tệp; chúng phải được mô hình đánh giá dựa trên nội dung truyện, hoặc do người dùng nêu rõ ý định.

## 3. Mục tiêu và ngoài phạm vi

### 3.1 Mục tiêu

1. **Hiểu được định dạng mở**: không yêu cầu người dùng chuyển tiểu thuyết sang định dạng tiêu đề tích hợp, cũng không yêu cầu người dùng viết regex.
2. **Có thể giao lại toàn văn**: mọi đoạn văn bản nguồn không rỗng đều phải thuộc về một chương hoặc khu vực phụ rõ ràng; cấm thất lạc âm thầm.
3. **Quy mô có thể kiểm soát**: không còn trường hợp một lệnh gọi đọc toàn bộ nội dung truyện của cả cuốn sách và xuất ra toàn bộ đối tượng chương; phân đoạn, lô chương với hai ngân sách và tổng hợp theo khoảng đều có ranh giới đầu vào/đầu ra cục bộ, còn đầu ra toàn cục chỉ tăng theo độ phức tạp ngữ nghĩa thực sự như số nhân vật, cung tập.
4. **Thất bại không làm ô nhiễm**: không ghi trạng thái sáng tác chính thức trước khi hoàn tất phân tích ngữ nghĩa và kiểm tra hợp lệ Foundation.
5. **Khôi phục chính xác**: việc khôi phục dựa trên ảnh chụp nguồn và `InputDigest` của artifact, không phụ thuộc `from=N` hay trí nhớ của người dùng.
6. **Lợi ích từ mô hình đến trực tiếp**: mô hình mạnh hơn trực tiếp cải thiện nhận diện ranh giới, trích xuất sự thật, phân chia cung tập và phán đoán khả năng viết tiếp, không cần thêm quy tắc Go.
7. **Tái sử dụng ngữ nghĩa commit chính thức**: việc phát hành chương tiếp tục dùng PendingCommit, điểm kiểm tra và khả năng lũy thừa theo digest của `commit_chapter`.
8. **Quan sát được đầy đủ**: tiến độ, danh tính mô hình, mức sử dụng, phản hồi thất bại gốc và lỗi cuối cùng đều có điểm ghi rõ ràng.
9. **Cùng tồn tại tương tác và tự động hóa**: mặc định để người dùng xác nhận các ranh giới ngữ nghĩa rủi ro cao, đồng thời cung cấp cơ chế cấp quyền không người trực bị rõ ràng; luồng tự động không phụ thuộc vào việc đoán âm thầm.

### 3.2 Ngoài phạm vi

- Không xây dựng Điều phối viên hoặc vòng lặp Agent dài hạn dùng chung.
- Không xây dựng framework Workflow/PolicyEngine/đồ thị nhiệm vụ dùng chung.
- Không tự động sửa hoặc viết lại văn bản gốc của người dùng.
- Không hiện thực cơ sở dữ liệu, truy xuất vector hay song song phân tán cho việc nhập.
- Không hỗ trợ việc gộp mơ hồ một tiểu thuyết khác vào một cuốn sách đã có.
- Không hiện thực việc di chuyển trạng thái `from=N` cũ hay hỗ trợ song song hai kiến trúc.
- Không mở rộng EPUB/PDF trong RFC này; phiên bản đầu vẫn chỉ nhận txt/md, lớp đọc giữ ở phạm vi cục bộ để về sau có thể thay thế mà không thay đổi các hợp đồng tiếp theo.

## 4. Ranh giới trách nhiệm

| Vấn đề | Thuộc về | Lý do |
|---|---|---|
| Giải mã byte, chuẩn hóa ký tự xuống dòng | Go | Định dạng tệp và chuyển đổi tất định |
| Vị trí nguồn nào là tiêu đề chương, tiêu đề tập hoặc văn bản phụ | LLM | Ngữ nghĩa mở, không thể liệt kê đầy đủ |
| Tiêu đề tương ứng với vị trí nguồn ổn định nào | Go | SourceUnit, điểm neo văn bản gốc và phạm vi byte có thể kiểm tra bằng máy |
| Chuyện gì xảy ra trong một chương | LLM | Hiểu ngữ nghĩa văn học |
| Cách tổng hợp nhân vật, quy tắc thế giới, phục bút và quan hệ | LLM | Tổng hợp ngữ nghĩa xuyên chương |
| Ranh giới cung tập, câu chuyện đã khép lại hay chưa, cấp độ quy hoạch | LLM | Phụ thuộc vào hình dạng tự sự, không phụ thuộc vào ngưỡng cố định |
| Phạm vi các chương có tăng dần, không chồng lấn, phủ toàn bộ hay không | Go | Bất biến có thể chứng minh |
| Kiểu JSON, enum tập đóng, số chương được tham chiếu có hợp lệ hay không | Go | Hợp đồng có kiểu dữ liệu |
| Có thể tái sử dụng phân tích đã có hay không | Host/Workspace | Chỉ có thể tái sử dụng nếu đầu vào ngữ nghĩa thực tế dựng lại được cùng `InputDigest` |
| Khi nào ghi trạng thái sách chính thức | Host/Store | Giao thức phát hành và khôi phục sau sự cố |
| Có được phép tiếp tục theo phép chẻ lại hiện tại hay không | Người dùng/Ý định | Xác nhận tương tác hoặc `--yes` rõ ràng, mã nguồn không tự ý trả lời thay |

Các lệnh gọi LLM ở đây không phải là mặt phẳng điều khiển của Tài phán, cũng không phải vòng lặp sáng tác của Worker. Chúng là các **hàm ngữ nghĩa** có ranh giới rõ ràng: nhận sự thật có kiểu dữ liệu, trả kết quả ngữ nghĩa có kiểu dữ liệu, rồi Host kiểm tra hợp lệ trước khi thực thi.

## 5. Kiến trúc tổng thể

```text
[TUI / Headless]
       │ /import <path> / tự động ủy quyền / xác nhận / hủy
[Host]
       │ chiếm quyền vòng đời nhập, sự kiện, thời gian chạy mô hình
[imp.Runner]
       ├── LoadState → NextAction（chỉ suy ra từ sự thật của không gian làm việc）
       ├── Source     đọc, giải mã, chuẩn hóa, ảnh chụp
       ├── Segment    phép chiếu cấu trúc → nhận diện ranh giới bằng LLM → kiểm tra hợp lệ độ bao phủ
       ├── Analyze    lô liên tiếp với ngân sách kép → tạm trữ sự thật theo từng chương
       ├── Synthesize tổng hợp phân tầng → BookSynthesis
       ├── Validate   lắp ráp và kiểm tra hợp lệ toàn bộ Foundation
       └── Publish    Foundation chính thức → commit_chapter
               │
[meta/import không gian làm việc]          [Store chính thức]
ảnh chụp nguồn/chẻ lại/phân tích/tổng hợp  Progress/Checkpoint/Artifact/PendingCommit
```

Runner là trình điều phối các giai đoạn tất định thông thường, không có khả năng ra quyết định tự do. Mỗi lần nó chỉ thực hiện một hành động do `NextAction` suy ra, rồi đọc lại sự thật sau khi hành động hoàn tất.

## 6. Workspace và suy luận trạng thái

Sự thật trong quá trình nhập được lưu trong thư mục sách:

```text
meta/import/
├── manifest.json
├── intent.json
├── source.txt
├── guidance.txt          # Khi có: hướng dẫn chẻ lại bằng ngôn ngữ tự nhiên của người dùng（--guide）, là đầu vào ngữ nghĩa của segmentation
├── segmentation.json
├── confirmation.json
├── analyses/
│   ├── 000001.json
│   ├── 000002.json
│   └── ...
├── range-digests/
│   ├── 000001-000050.json
│   └── ...
├── synthesis.json
├── story-resolution.json
└── failures/
    ├── last.json
    └── last-response.txt
```

Phiên bản đầu giữ lại workspace. Nó vừa là căn cứ khôi phục, vừa là bản ghi kiểm toán của quá trình nhập; không bổ sung cơ chế tự động dọn dẹp và lưu trữ lịch sử.

`intent.json` lưu các cấp quyền rõ ràng của người dùng khi khởi chạy việc nhập (tự động xác nhận, trạng thái câu chuyện uncertain được chọn trước, có bỏ qua Hold khi hoàn tất hay không). Đây là ý định của người dùng vẫn phải được tuân thủ sau khi khôi phục, không phải trạng thái giai đoạn có thể suy đoán từ artifact; sau khi được tạo, Runner không âm thầm ghi đè.

### 6.1 Manifest

```go
type ImportManifest struct {
	Version          int    `json:"version"`
	SourceName       string `json:"source_name"`
	RawSHA256        string `json:"raw_sha256"`
	NormalizedSHA256 string `json:"normalized_sha256"`
	Encoding         string `json:"encoding"`
	SizeBytes        int64  `json:"size_bytes"`
	CreatedAt        string `json:"created_at"`
}

type ImportIntent struct {
	Version             int    `json:"version"`
	AutoConfirm         bool   `json:"auto_confirm,omitempty"`
	StoryResolution     string `json:"story_resolution,omitempty"` // open / closed
	ContinueAfterImport bool   `json:"continue_after_import,omitempty"`
}
```

- `source.txt` là ảnh chụp cục bộ sau khi chuẩn hóa, việc khôi phục không còn phụ thuộc vào việc đường dẫn gốc vẫn tồn tại;
- Manifest không lưu đường dẫn nguồn tuyệt đối, tránh làm lộ thư mục của máy và loại bỏ vấn đề khôi phục do di chuyển tệp;
- Intent chỉ chấp nhận giá trị thuộc tập đóng, lưu chính xác cấp quyền của người dùng trong lệnh khởi chạy; khi khôi phục không suy ngược ý định cũ từ advance mode hiện tại;
- Khi phiên bản schema không khớp, yêu cầu rõ ràng dùng phiên bản tương ứng để tiếp tục hoặc nhập lại, không tự đoán cách di chuyển.

Khi tạo lần đầu, trước tiên ghi đầy đủ và kiểm tra hợp lệ manifest, intent, source trong thư mục tạm cùng cấp, rồi phát hành thành `meta/import/` bằng cách đổi tên thư mục; nếu `meta/import/` không tồn tại thì không coi là workspace đang hoạt động. Nhờ vậy, bộ ba khởi tạo ban đầu không bao giờ đi vào `NextAction` ở trạng thái khởi tạo dở dang, cũng không cần thêm `stage=initializing` cho quá trình tạo. Khi phát hiện thư mục khởi tạo còn sót lại lúc khởi động, phải nhắc nhở rõ ràng và giữ lại thông tin chẩn đoán, không tự coi là workspace thành công và cũng không âm thầm xóa.

### 6.2 Không lưu enum giai đoạn có thể trôi lệch

Trạng thái bền vững không ghi các trường điều khiển như `stage=analyzing`, `current=37`. Hành động tiếp theo được suy ra từ artifact:

```text
Không có manifest/intent/source                                  → ingest
Không có segmentation                                            → segment
Không có confirmation khớp với tóm tắt đầu vào của segmentation  → await_confirmation / auto_confirm
Có phân tích chương bị thiếu hoặc tóm tắt đầu vào không khớp     → analyze_first_missing
Thiếu RangeDigest khớp đầu vào hoặc synthesis                    → synthesize_first_missing
story_status=uncertain và không có lựa chọn người dùng khớp      → await_story_resolution
Artifact chính thức không khớp với synthesis                     → publish
Tất cả artifact chính thức đều khớp                              → done
```

`Stage` trong sự kiện chỉ dùng để hiển thị trên UI, không phải nguồn sự thật để khôi phục.

### 6.3 Thống nhất danh tính artifact

Không hiện thực đồ thị phụ thuộc. Mọi artifact ngữ nghĩa trong workspace dùng chung một quy tắc định danh:

```go
type Artifact[T any] struct {
	SchemaVersion int    `json:"schema_version"`
	InputDigest   string `json:"input_digest"`
	Payload       T      `json:"payload"`
}
```

`InputDigest` bao phủ toàn bộ **đầu vào ngữ nghĩa** thực sự mà hành động đó tiêu thụ, được mã hóa theo thứ tự cố định rồi tính toán:

- segmentation: nội dung nguồn đã chuẩn hóa, phép chiếu SourceUnit, hướng dẫn của người dùng và phiên bản prompt/schema phân đoạn;
- confirmation: nội dung segmentation và phương thức xác nhận;
- phân tích chương: phạm vi chương của lô cùng nội dung truyện, ledger tính liên tục trước khi vào lô, phiên bản prompt/schema và hướng dẫn của người dùng;
- RangeDigest/BookSynthesis: nội dung phân tích có thứ tự hoặc nội dung digest cấp dưới mà chúng tiêu thụ, cùng phiên bản prompt/schema tổng hợp;
- story resolution: nội dung synthesis và lựa chọn của người dùng;
- phát hành: nội dung chuẩn hóa của đối tượng miền đang chờ phát hành.

provider/model, usage, thinking và các sự thật thực thi khác được ghi vào provenance/session, không tự động làm mất hiệu lực phân tích đã thành công khi cấu hình mô hình thay đổi; khi người dùng yêu cầu phân tích lại, phải xóa rõ ràng artifact tương ứng. Quyết định tái sử dụng bộ nhớ đệm chỉ dựa vào việc hành động hiện tại có dựng lại được cùng `InputDigest` hay không.

`NextAction` đi theo pipeline tuyến tính cố định để tìm artifact đầu tiên bị thiếu, không phân tích được hoặc có `InputDigest` không khớp. Khi chẻ lại lại, sửa hướng dẫn của người dùng hoặc thay đổi sự thật cấp trên, các artifact cấp dưới sẽ tự nhiên không khớp; không viết quy tắc vô hiệu hóa kiểu “khi phép chẻ lại thay đổi thì phải xóa thủ công những tệp nào”.

Khi phát hành, artifact chính thức được so sánh từng mục với kết quả tổng hợp; nếu giống nhau thì bỏ qua theo cách lũy thừa, nếu khác thì báo cáo xung đột, không ghi đè bằng phỏng đoán. Vì vậy, `ResumeFrom` được loại bỏ. Để khôi phục chỉ cần thực thi lại `/import`; Runner sẽ tiếp tục từ sự thật bị thiếu đầu tiên.

## 7. Đọc tệp nguồn

### 7.1 Giải mã

Phiên bản đầu hỗ trợ:

- UTF-8 / UTF-8 BOM;
- GB18030 (bao phủ văn bản tiểu thuyết GBK phổ biến).

Kết quả giải mã phải trả về encoding đã chọn, đồng thời ghi vào Manifest và các sự kiện tiến trình. Không được giấu "thử GB18030" thành phương án dự phòng âm thầm. Nếu không thể giải mã một cách đáng tin cậy hoặc xuất hiện ký tự thay thế không thể chấp nhận, phải thất bại ngay, lỗi phải chứa kết quả dò.

### 7.2 Chuẩn hóa

Chỉ thực hiện các chuyển đổi không làm thay đổi nội dung văn chương:

- Loại bỏ BOM;
- Chuẩn hóa CRLF/CR thành LF;
- Giữ lại dòng trống, thụt lề, dòng tiêu đề và ký tự nội dung truyện (văn bản);
- Không xoá văn bản đầu, chương rỗng, quảng cáo, thông tin bản quyền hoặc cái gọi là nhiễu ở cuối.

Mọi quyết định loại trừ đều để dành cho kết quả ngữ nghĩa của phân đoạn và được hiển thị trong bản xem trước.

### 7.3 Tọa độ ổn định

Văn bản đã chuẩn hóa lập bảng `SourceUnit` thống nhất:

```go
type SourceUnit struct {
	ID        string // L1257; dòng vượt ngân sách được tách thành L1257.1, L1257.2
	Line      int
	Part      int
	StartByte int
	EndByte   int
	Text      string
}
```

- `ID` chỉ dùng để hiển thị và để model tham chiếu; mọi phán đoán về thứ tự, bao hàm và tăng dần đều so sánh theo bộ `(Line, Part)` dạng số, cấm so sánh chuỗi ID theo thứ tự từ điển (`"L900"` theo thứ tự từ điển sẽ lớn hơn `"L1000"`); JSON projection giữ id dạng chuỗi, phía Go phân tích thành `(Line, Part)` rồi mới so sánh;
- Dòng bình thường tương ứng với một unit, đường đi phổ biến vẫn là tọa độ theo số dòng trực quan;
- Khi một dòng vượt ngân sách của projection cấu trúc, Go chỉ sinh nhiều **unit ảo** tại các ranh giới ký tự UTF-8;
- Các lát ảo không được ghi lại vào `source.txt`, không chèn ngắt dòng mềm, không thay đổi bất kỳ ký tự nguồn nào;
- Khi có ranh giới bên trong cùng một unit, model trả về unit ID cùng một đoạn anchor nguyên văn được sao chép từng ký tự; Go yêu cầu anchor là duy nhất trong unit đó, rồi ánh xạ sang vị trí byte chính xác;
- Khi anchor không tồn tại hoặc không duy nhất, phải trả lỗi cụ thể cho model, cấm đoán offset, cắt ngắn văn bản hoặc yêu cầu người dùng sửa bản gốc trước.

Vì vậy, văn bản chia chương thông thường vẫn giữ mô hình số dòng, còn văn bản không có ngắt dòng, một dòng chứa nhiều chương hoặc dòng dài bất thường đều được xử lý bằng cùng một loại tọa độ.

## 8. Phân đoạn ngữ nghĩa

### 8.1 Projection cấu trúc

Model nhìn thấy projection cấu trúc được chia khối theo ngân sách ngữ cảnh:

```json
{
  "owned_units": {"start": "L1200", "end": "L1800"},
  "context_units": {"start": "L1180", "end": "L1820"},
  "units": [
    {"id": "L1200", "line": 1200, "text": "风从城门外吹来。", "blank_before": true},
    {"id": "L1257", "line": 1257, "text": "卷二·北境", "blank_before": true, "blank_after": true}
  ],
  "user_guidance": ""
}
```

Vùng ngữ cảnh có thể chồng lấn, nhưng mỗi lần gọi chỉ được trả về kết quả cho `owned_units`, vì vậy không có bỏ phiếu giữa các khối chồng lấn hay hợp nhất xung đột. Kỷ luật tọa độ do Go thực thi (sửa đổi ngày 2026-07-16): ranh giới mà model trả về trong vùng ngữ cảnh không kích hoạt câu hỏi lại về mặt ngữ nghĩa—ranh giới đó thuộc thẩm quyền của khối lân cận (nó sẽ báo lại một lần nữa trong khoảng owned của chính nó), mã cắt thẳng và hiển thị lại lời giải thích; chỉ thử lại về mặt ngữ nghĩa dành cho những thất bại ngữ nghĩa thực sự (ID ảo giác ngoài projection, kind không hợp lệ, v.v.). Hành vi cũ hỏi lại kèm phản hồi khi vượt phạm vi, khiến model yếu thường tiêu hao hết cả 3 lần thử và làm sập cả khối.

Kích thước khối được tính từ context window của model architect hiện tại và ngân sách dự trữ, không chia theo số dòng hay số chương cố định. Khi ngữ cảnh của model được mở rộng, số lần gọi tự nhiên giảm xuống. Ngân sách quy hoạch không được dùng đầy đủ (sửa đổi ngày 2026-07-16): nội dung owned chỉ là một phần của yêu cầu, khi quy hoạch phải trừ đi độ dài thực tế của system prompt và hướng dẫn, rồi tính phần bọc JSON projection bị phình to theo tỉ lệ 3/4; vùng ngữ cảnh có thêm giới hạn byte (chunkBytes/8, mức tối thiểu 4096), chặn các lát ảo của dòng quá dài nuốt mất ngân sách đầu vào. Phía đầu ra có lớp dự phòng đối xứng: khi JSON ranh giới của một khối bị cắt ngắn theo độ dài (nhiều chương ngắn), thu nhỏ khối một nửa và thử lại đệ quy—nửa khối có đường dẫn bộ nhớ đệm riêng, kết quả thử lại không phải trả phí lần nữa; chỉ khi mức unit vẫn bị cắt ngắn thì mới thực sự thiếu dung lượng.

Quyết định ranh giới của từng khối được ghi xuống đĩa dưới dạng artifact (`segment-chunks/chunk-*.json`, danh tính = danh tính phân đoạn + MaxUnitBytes + phạm vi owned của khối—bảng unit được xác định duy nhất bởi "nguồn đã chuẩn hóa + MaxUnitBytes", khi đổi mức và tái tạo các lát của dòng quá dài thì bộ nhớ đệm tự nhiên không khớp, nên không có nguy cơ tái sử dụng ranh giới cũ bị lệch chỗ), nếu bất kỳ khối nào thất bại hoặc bị gián đoạn thì khi chạy lại, các khối đã hoàn thành được tái sử dụng mà không cần gọi nào—cùng triết lý với analyze theo chương và synthesize theo khoảng; sau khi segmentation cuối cùng được ghi xuống đĩa thì bộ nhớ đệm cấp khối bị xoá. Khi tích hợp cuối cùng (resolve) thất bại, bộ nhớ đệm khối cũng bị xoá và ảnh chụp quyết định được ghi vào `failures/`: lúc này digest của bộ nhớ đệm luôn khớp, giữ lại sẽ khiến lần chạy lại đọc lại y hệt những ranh giới đó mà không cần gọi nào và tái hiện xác định cùng một thất bại. Ranh giới chương có nội dung rỗng (tiêu đề giữ chỗ kiểu "已锁定/付费章节" rất phổ biến trong nguồn tiểu thuyết mạng thực tế) không làm thất bại toàn cục: chúng được gộp vào đoạn trước (không mất một chữ nào của văn bản), ghi vào `Segmentation.Notes` để bản xem trước xác nhận trình bày, nếu con người không đồng ý thì có thể dùng `--guide` để phán quyết.

### 8.2 Kết quả model

```go
type BoundaryDecision struct {
	UnitID    string   `json:"unit_id"`
	Anchor    string   `json:"anchor,omitempty"`
	Kind      string   `json:"kind"` // chapter / group / front_matter / back_matter
	Title     string   `json:"title,omitempty"`
	Uncertain bool     `json:"uncertain,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}
```

- `chapter` là unit nội dung có thể được gửi đi, bao gồm cả phán đoán ngữ nghĩa về việc prologue, đoạn dẫn dắp, ngoại truyện... có được tính là chương hay không;
- `group` là bằng chứng về cấu trúc cấp cao như tập, phần, mục, không được coi trực tiếp là chương;
- `front_matter` / `back_matter` đánh dấu các vùng phụ thuộc rõ ràng không đưa vào nội dung chương;
- `anchor` phải đến từng ký tự từ unit tương ứng; nếu ranh giới nằm ở đầu unit thì có thể bỏ qua;
- `uncertain` chỉ dùng cho gợi ý trong bản xem trước, mã không đặt ngưỡng độ tin cậy.

Không để model sinh biểu thức chính quy. Biểu thức chính quy vẫn ép ngữ nghĩa mở về một cú pháp hữu hạn, đồng thời đưa vào các giả định về escape, khớp cục bộ và định dạng thống nhất.

### 8.3 Kiểm tra hợp lệ bằng mã

Go chỉ kiểm tra hợp lệ:

1. Mọi unit ID đều tồn tại và nằm trong projection của lần gọi (owned + vùng ngữ cảnh; ngoài projection là ảo giác, có phản hồi để hỏi lại);
2. kind của ranh giới trong vùng owned thuộc tập kín, anchor không rỗng là duy nhất trong unit tương ứng và ánh xạ được tới ranh giới byte UTF-8, không có xung đột ngữ nghĩa tại cùng vị trí (khi kind/tiêu đề khác nhau thì việc giữ bên nào không do Go phán quyết, hỏi lại model; các bản trùng hoàn toàn là dư thừa cơ học, sau khi cho qua sẽ dọn trùng lặp âm thầm), khối đầu tiên phải có ranh giới bao trọn điểm bắt đầu văn bản (đoạn mở đầu có phải front matter hay không do model phán đoán, Go không trả lời thay) — tất cả đều kiểm tra hợp lệ ngay trong lúc gọi (sửa đổi ngày 2026-07-16): nếu để giá trị hỏng vào bộ nhớ đệm khối thì đến bước cuối mới phát hiện, digest luôn khớp khiến thất bại tái hiện xác định; ranh giới trong vùng ngữ cảnh đều bị cắt bỏ, không hỏi lại vì chúng;
2a. **Đối chiếu tiêu đề** (sửa đổi ngày 2026-07-16, seg-v2): title của ranh giới chapter/group, sau khi chuẩn hóa (bỏ khoảng trắng), phải thực sự tồn tại trong nguyên văn của unit ranh giới, nếu không thì hỏi lại ngay trong lúc gọi—thực đo trên một nguồn thu thập theo trang, trong 157 chương có 67 chương là ranh giới và tiêu đề do model bịa ra trên phần văn bản tiếp nối ở giữa chương (mơ hồ kỷ luật bao phủ buộc mỗi khối phải đặt ranh giới ở đầu khối), và tất cả đều bị kiểm tra đối chiếu sự thật này chặn lại. Phán đoán ngữ nghĩa vẫn thuộc về model: với nguồn thực sự không có quy ước tiêu đề, đặt `uncertain=true` thì có thể giữ lại tiêu đề do model tổng hợp (bản xem trước hiển thị dấu hiệu nghi ngờ); tiêu đề mô tả của front/back matter có rủi ro thấp nên không đối chiếu. prompt đồng thời được siết chặt: ranh giới chỉ được đặt tại những điểm phân cách cấu trúc thực sự, khi đầu khối là phần nội dung tiếp nối của chương trước thì trả về boundaries rỗng là kết quả đúng (trừ trường hợp đầu khối đầu tiên phải bao phủ);
3. Thứ tự và sự trùng lặp của ranh giới do Go sửa một cách tất định thay vì bác bỏ (sửa đổi ngày 2026-07-16): khôi phục thứ tự thực bằng cách sắp xếp ổn định theo byte đã phân tích—thứ tự giữa các khối được đảm bảo bởi việc các khoảng owned không chồng lấn, nên hỗn loạn thứ tự chỉ có thể xảy ra bên trong khối, việc sắp xếp không làm mất thông tin nào; với bản trùng cùng byte thì giữ lại bản xuất hiện trước và ghi vào `Notes`. Hành vi cũ yêu cầu thứ tự tăng nghiêm ngặt, nếu không thì toàn bộ thất bại; thực đo cho thấy 319 ranh giới thất bại vì chỉ một chỗ bị đảo thứ tự trong khối, và bộ nhớ đệm khối sẽ khiến thất bại đó tái hiện xác định. Mọi phán đoán thứ tự đều theo thứ tự số của `(Line, Part)`, không so sánh ID theo thứ tự từ điển;
4. Phạm vi nội dung của mỗi chương được tạo ra đều không rỗng (tiêu đề giữ chỗ có nội dung rỗng được gộp vào đoạn trước, xem §8.1);
5. Mọi văn bản nguồn không rỗng đều thuộc chính xác một chương, một group title hoặc một vùng front/back matter xác định rõ (văn bản không rỗng ở đầu chưa được gán thuộc tính—phần giới thiệu/quảng cáo đầu sách bị bỏ sót báo cáo ranh giới—do Go một cách tất định gom thành front_matter và ghi vào `Notes` để đưa sang bản xem trước xác nhận, không bác bỏ ở bước cuối);
5a. Các chương trùng tên (sau khi bỏ khoảng trắng, tiêu đề giống nhau) được ghi vào `Notes` để con người đối chiếu (sửa đổi ngày 2026-07-16)—trong nguồn có quy ước tiêu đề thì tên chương không nên trùng nhau, sự trùng lặp là tín hiệu tất định cho thấy "một chương bị cắt nhầm"; có gộp hay không không do Go phán quyết, Notes khác rỗng là chặn `--yes`;
6. Không có phạm vi chồng lấn, vượt phạm vi hoặc chưa được gán thuộc tính;
7. group không bị tính nhầm vào tổng số chương.

Việc "L1257 về mặt ngữ nghĩa có phải tiêu đề chương hay không" không do Go phán xét lại.

### 8.4 Xác nhận của người dùng

Ở chế độ tương tác, trước khi xác nhận không gọi phân tích chương và không ghi vào Store chính thức. Bản xem trước phải hiển thị tối thiểu:

- Số tập/group và số chương;
- Toàn bộ tiêu đề chương, có thể cuộn để xem;
- Phạm vi và tóm tắt của văn bản phụ thuộc ở đầu và ở cuối;
- Chương rỗng, chương dài bất thường và các ranh giới được model đánh dấu uncertain;
- Dòng bắt đầu và dòng kết thúc của từng chương, để người dùng đối chiếu với bản gốc.

Người dùng có thể:

- Xác nhận (trong bảng xem trước TUI nhấn `y`, bên trong chạy lại bằng AcceptSegmentation; cho phép một lần đối với phân đoạn hiện tại, không ghi vào intent, confirmation ghi `method=user_confirmed`);
- Nhập mô tả bằng ngôn ngữ tự nhiên rồi nhận diện lại, ví dụ `/import --guide=幕间·X 也是独立章节`;
- Huỷ và giữ lại workspace (Esc).

`/import <path> --yes` là ủy quyền không người canh giám một cách tường minh: Runner, sau khi kiểm tra hợp lệ ghi đè đạt yêu cầu, ghi cùng loại artifact confirmation, ghi `method=auto_authorized`, rồi tiếp tục phân tích. `--yes` dù có ranh giới uncertain vẫn nghĩa là người dùng chọn tin tưởng phân đoạn lần này, nhưng uncertain vẫn được giữ lại trong artifact và trong nhật ký. **Ngoại lệ (sửa đổi ngày 2026-07-16)**: khi phân đoạn có kèm giải thích dung sai (Notes khác rỗng—đã xảy ra việc hấp thụ chương rỗng, gom phần đầu và dọn trùng lặp do chồng lấn) thì `--yes` không tự động cho qua, vẫn dừng ở bản xem trước xác nhận—cấu trúc đã bị viết lại một cách tất định, sự ủy quyền mù quáng mà chưa xem bản xem trước không nên nuốt mất điều đó; thao tác `y` sau khi đã xem bản xem trước (AcceptSegmentation) không bị giới hạn này.

`--yes` chỉ bỏ qua bước xác nhận phân đoạn, không thay người dùng quyết định `story_status=uncertain`, cũng không bỏ qua bước Hold khi nhập hoàn tất. Người dùng không cần viết biểu thức chính quy hay điền thủ công `from=N`.

## 9. Trích xuất sự thật theo chương cho các lô liên tiếp

Sau khi xác nhận, bắt đầu từ bản phân tích thiếu đầu tiên, gom các chương liên tiếp thành lô theo **ngân sách kép cho đầu vào và đầu ra** của mô hình hiện tại. Phiên bản đầu tiên xử lý lô theo thứ tự, không thực hiện đồng thời giữa các cửa sổ: ID phục bút, bí danh nhân vật và thay đổi trạng thái đều có trình tự thời gian, ledger cô đọng do lô trước tạo ra chính là đầu vào của lô sau.

Việc xử lý tuần tự chỉ ràng buộc chiến lược thực thi của phiên bản đầu tiên, không phải giới hạn kiến trúc vĩnh viễn; artifact phân tích vẫn được ghi xuống đĩa độc lập theo từng chương, và khi có bằng chứng cho thấy việc gộp song song vẫn giữ được chất lượng ngữ nghĩa thì chỉ cần thay thế bộ lập lịch lô.

### 9.1 Đầu ra lô, artifact theo từng chương

Bỏ hẳn envelope hỗn hợp `=== TAG ===`. Mỗi lần gọi trả về một đối tượng lô có cấu trúc, mỗi phần tử trong mảng vẫn là sự thật của một chương:

```go
type ImportedChapterFacts struct {
	Chapter             int                        `json:"chapter"`
	Title               string                     `json:"title"`
	Summary             string                     `json:"summary"`
	KeyEvents           []string                   `json:"key_events"`
	CoreEvent           string                     `json:"core_event"`
	Hook                string                     `json:"hook"`
	Scenes              []string                   `json:"scenes"`
	Characters          []string                   `json:"characters"`
	CharacterEvidence   []ImportedCharacterFact    `json:"character_evidence,omitempty"`
	WorldEvidence       []ImportedWorldFact        `json:"world_evidence,omitempty"`
	TimelineEvents      []domain.TimelineEvent      `json:"timeline_events,omitempty"`
	ForeshadowUpdates   []domain.ForeshadowUpdate  `json:"foreshadow_updates,omitempty"`
	RelationshipChanges []domain.RelationshipEntry `json:"relationship_changes,omitempty"`
	StateChanges        []domain.StateChange       `json:"state_changes,omitempty"`
	HookType            string                     `json:"hook_type"`
	DominantStrand      string                     `json:"dominant_strand"`
}

type AnalysisBatchResult struct {
	Chapters []ImportedChapterFacts `json:"chapters"`
}

type ChapterAnalysisPayload struct {
	BatchStart int                  `json:"batch_start"`
	BatchEnd   int                  `json:"batch_end"`
	Facts      ImportedChapterFacts `json:"facts"`
}
```

Mỗi `analyses/NNNNNN.json` đều là `Artifact[ChapterAnalysisPayload]`. Các chương cùng một lô được ghi xuống đĩa đều mang cùng `BatchStart/BatchEnd`; `InputDigest` của chúng áp dụng **ràng buộc theo từng chương**: danh tính phân đoạn (`InputDigest` của artifact segmentation) + phiên bản prompt/schema + số chương + nội dung truyện (văn bản) của riêng chương đó. Việc ràng buộc theo từng chương thay vì theo lô là bởi ranh giới lô thay đổi theo năng lực đầu vào/đầu ra của mô hình (dùng mô hình mạnh hơn thì lô tự nhiên lớn lên); nếu ghi phân chia lô vào danh tính thì khi đổi mô hình, toàn bộ các phân tích đã thành công sẽ mất đối chiếu, buộc phải tính lại và bị tính phí trùng lặp. Ràng buộc danh tính phân đoạn thì đảm bảo khi “chia lại, đổi phiên bản prompt/schema, đổi nguồn” thì các phân tích hạ nguồn tự nhiên mất đối chiếu, còn việc chỉ đổi mô hình thì không bị ảnh hưởng oan — đó mới đúng là ngữ nghĩa vô hiệu hóa mà việc khôi phục thực sự cần.

`ImportedCharacterFact` và `ImportedWorldFact` là các quan sát cô đọng dùng cho việc tổng hợp toàn sách, không ghi trực tiếp vào nhân vật chính thức hay quy tắc thế giới. Chúng mang ít nhất số chương, nhờ đó kết quả tổng hợp có nguồn gốc ổn định.

### 9.2 Gom lô theo ngân sách kép

Kế hoạch lô phải đồng thời thỏa mãn:

```text
Đầu vào ước tính + system/prompt/ledger + phần dành cho suy luận + đầu ra nhìn thấy dự kiến ≤ context window
Đầu ra nhìn thấy dự kiến ≤ giới hạn completion khả dụng của provider/model
```

- Ước tính đầu vào bao gồm tiêu đề, nội dung truyện (văn bản) của từng chương và ledger trước lô;
- Ước tính đầu ra gồm chi phí cấu trúc cố định của schema analyzer và phần dự trữ sự thật thận trọng cho mỗi chương; nó chỉ quyết định lô này nạp bao nhiêu chương, không cắt ngắn trường nào;
- Với model có reasoning token dùng chung ngân sách completion với JSON nhìn thấy được, phải trừ phần dành cho suy luận trước;
- Càng mạnh về năng lực đầu ra, provider/model càng làm cho lô lớn lên tự nhiên; không được viết cứng quy tắc “mỗi lô 10/20 chương”;
- Khi bản thân đầu vào của một chương không lọt vào context, hoặc bản thân đầu ra cấu trúc tối thiểu của một chương không lọt vào completion, phải báo cáo tường minh chương đó cùng năng lực của mô hình, không cắt ngắn nội dung truyện (văn bản) hay giả vờ tạo ra thành công dạng rút gọn.

Vì vậy, khi tổng số chương tăng lên chỉ làm tăng số lô, không còn để bất kỳ lần trả lời nào phình vô hạn theo quy mô toàn bộ sách; đồng thời không chuyển #83 từ tầm độ phân giải toàn sách sang tầm độ phân giải theo lô vốn không bị ràng buộc bởi đầu ra.

### 9.3 Ngữ cảnh của lô

Một lần gọi lô chỉ chứa:

- nguyên văn và tiêu đề của dải chương liên tiếp hiện tại;
- bảng bí danh nhân vật cô đọng được suy ra từ các chương trước;
- ID phục bút đang hoạt động cùng trạng thái một câu;
- tóm tắt trạng thái gần đây cần thiết.

Mô hình xử lý các chương trong lô theo thứ tự mảng, có thể duy trì bí danh, phục bút và trạng thái xuyên suốt bên trong lô; sau khi lô kết thúc, Go cập nhật ledger cô đọng theo thứ tự các sự thật đã được xác minh. Nó không phụ thuộc vào Premise toàn sách chưa được sinh ra, cũng không phải đọc lại toàn bộ phần trước. Sự thật theo chương là đầu vào của Foundation, chứ không phải ngược lại tạo ra phụ thuộc vòng.

### 9.4 Kiểm tra hợp lệ toàn bộ phản hồi

Code kiểm tra hợp lệ theo hai lớp về cấu trúc, miền giá trị và tham chiếu, không hard-code chất lượng văn học:

- Cấp lô: mảng `chapters` liên tục theo số chương dự kiến, không trùng lặp, không thiếu; phạm vi lô, `InputDigest` và schema version khớp;
- Cấp chương: chapter/title khớp với phân đoạn nguồn, summary/core_event không rỗng, các trường closed-set chính thức của domain như hook type, strand hợp lệ, các trường dòng thời gian, phục bút và thay đổi trạng thái có kiểu hợp lệ.

Code không yêu cầu “phải có 3～6 sự kiện”“phải có nhân vật xuất hiện”“phải có ba cảnh”. Chương yên tĩnh, chương thư từ, chương miêu tả bối cảnh hay chương có nhân vật không tên đều là những hình dạng văn học hợp lệ.

Khi toàn bộ phản hồi có lỗi kiểm tra hợp lệ JSON hoặc ngữ nghĩa, không commit bất kỳ chương mới nào trong đó; đưa lỗi cụ thể phản hồi lại cho cùng mô hình, đi theo vòng thử lại ở tầng đầu ra của §13.3. Mô hình có thể viết lại các đối tượng trước đó sau khi sửa, vì vậy khi kiểm tra hợp lệ thông thường thất bại thì không được tùy tiện lưu một phần mảng.

### 9.5 Tiền tố liên tục khi bị cắt ngắn do độ dài

> Định vị triển khai: mục này là **tối ưu token trên đường lỗi**, không phải điều kiện mà tính đúng đắn của việc khôi phục phụ thuộc vào. Ở v1 (giai đoạn ba), bị cắt ngắn nghĩa là “thất bại + thu nhỏ và gom lại lô”, bản thân cách đó đúng và có thể khôi phục; việc cứu vãi tiền tố liên tục được triển khai ở một tiểu giai đoạn độc lập (giai đoạn ba · bổ sung), có thể bật/tắt riêng và nghiệm thu riêng.

Chỉ khi phản hồi ghi rõ `StopReasonLength` và trả về phần văn bản có thể phân tích được, mới cho phép lưu **tiền tố hợp lệ liên tục dài nhất** từ phản hồi thất bại:

1. Dùng JSON decoder dạng streaming để đi vào mảng `chapters` cấp cao nhất;
2. Bắt đầu từ chương đầu lô, đọc tuần tự từng đối tượng JSON đã đóng hoàn chỉnh;
3. Mỗi đối tượng phải tự động vượt qua kiểm tra hợp lệ theo chương của §9.4, và khi cùng với các đối tượng trước đó tạo thành một chuỗi liên tục bắt đầu từ chương đầu lô thì lập tức ghi atomically vào artifact phân tích của chương tương ứng;
4. Gặp đối tượng không hoàn chỉnh, không hợp lệ, bị bỏ số hoặc trùng lặp đầu tiên thì dừng ngay, mọi byte sau đó tuyệt đối không được diễn giải;
5. Cấm tự bổ sung dấu ngoặc, viết nối nửa JSON, đoán trường bị thiếu hoặc cứu các đối tượng không liên tục từ vị trí phía sau;
6. Toàn bộ phản hồi gốc, StopReason, phạm vi tiền tố đã lưu và chương thất bại đầu tiên đều được ghi vào failure artifact, sự kiện và nhật ký;
7. `NextAction` gom lô lại từ bản phân tích thiếu đầu tiên, không làm lại tiền tố hợp lệ đã commit.

typed-call phải ghi lại lần này có nhận được phần văn bản dùng được hay không: các chế độ có cấu trúc không streaming như JSON Schema có thể không cho ra tiền tố có thể phân tích được khi dừng do độ dài. Nếu provider không trả về phần văn bản, không thể chứng minh rõ ràng rằng đó là cắt ngắn do độ dài, hoặc không có đối tượng hợp lệ nào hoàn thành, thì không lưu bất kỳ kết quả nào, phát sự kiện/nhật ký `prefix_salvage=unavailable` và quay về “thất bại + thu nhỏ và gom lại lô” thay vì quay vòng trống trong im lặng. Khi lô một chương vẫn bị cắt ngắn thì báo cáo trực tiếp rằng năng lực đầu ra của mô hình không đủ, không thu nhỏ lặp vòng hay tạo ra sự thật rỗng.

Cắt ngắn do độ dài là lỗi dung lượng, không đi vào vòng lặp tự sửa ngữ nghĩa “đưa lỗi kiểm tra hợp lệ phản hồi lại cho cùng mô hình”, cũng không thử lại nguyên xi cùng một lô.

### 9.6 Khôi phục

Mỗi khi phân tích một chương thành công, `analyses/NNNNNN.json` được ghi atomically. Sau khi sập:

- Phân tích có `InputDigest` khớp được tái sử dụng trực tiếp, không tính phí trùng lặp;
- Phân tích thiếu hoặc mất đối chiếu đầu tiên trở thành điểm khởi đầu của lô tiếp theo;
- Khi đầu vào ngữ nghĩa phía thượng nguồn thay đổi, các phân tích không thể dựng lại cùng `InputDigest` sẽ tự nhiên mất hiệu lực;
- Tiền tố hợp lệ liên tục đã commit do cắt ngắn độ dài và các artifact hoàn thành bình thường dùng chung quy tắc khôi phục hoàn toàn giống nhau;
- Không cho phép người dùng vượt qua một chương thất bại để tiếp tục sinh ra các sự thật ngữ nghĩa về sau không liên tục.

## 10. Tổng hợp phân tầng

### 10.1 Vì sao không thể thực hiện đầu ra một lần cho toàn bộ sách

Tổng hợp toàn sách cần sự hiểu biết xuyên chương, nhưng không cần đọc lại toàn bộ nội dung truyện (văn bản), cũng không nên xuất đối tượng chi tiết cho từng chương. Sự thật theo chương đã chứa ngữ nghĩa ở tầng chương; việc tổng hợp chỉ xử lý những sự thật cô đọng này.

### 10.2 Hình dạng Map/Reduce

```text
ImportedChapterFacts × N
        ↓ Chia thành các khoảng liên tục theo context window hiện tại
RangeDigest × M
        ↓ Nếu cần thì tiếp tục gộp
BookSynthesis
```

Với sách ngắn, nếu một lần có thể chứa toàn bộ sự thật theo chương thì sinh thẳng `BookSynthesis`; chỉ sách dài mới sinh ra `RangeDigest`. Có phân tầng hay không do ngân sách token quyết định một cách máy móc, không phải do ngưỡng số chương quyết định.

`RangeDigest` chứa diễn biến tình tiết, biến đổi nhân vật, sự thật thế giới, các phục bút đã mở/đã thu và các ranh giới cấu trúc ứng viên của dải liên tục đó. Kích thước đầu ra của nó bị ràng buộc bởi một khoảng duy nhất; tổng hợp cuối cùng không còn xuất lại N bản đối tượng chi tiết theo chương, mà chỉ xuất sự thật toàn cục và phạm vi tập/cung.

### 10.3 Kết quả tổng hợp cuối cùng

```go
type BookSynthesis struct {
	Title         *string                `json:"title"`    // Khi không xác nhận được từ nội dung truyện (văn bản) thì là null, suy ra từ tên tệp
	Synopsis      string                 `json:"synopsis"` // Giới thiệu không tiết lộ nội dung dành cho người đọc
	Premise       string                 `json:"premise"`
	Characters    []domain.Character     `json:"characters"`
	WorldRules    []domain.WorldRule     `json:"world_rules"`
	Structure     []ImportedVolumeRange  `json:"structure"`
	Compass       domain.StoryCompass    `json:"compass"`
	PlanningTier  domain.PlanningTier    `json:"planning_tier"`
	StoryStatus   string                 `json:"story_status"` // open / closed / uncertain
	StatusReason  string                 `json:"status_reason"`
}
```

Cấu trúc chỉ trả về các phạm vi, không xuất lại toàn bộ các chương:

```go
type ImportedVolumeRange struct {
	Title string             `json:"title"`
	Theme string             `json:"theme"`
	Arcs  []ImportedArcRange `json:"arcs"`
}

type ImportedArcRange struct {
	Title        string `json:"title"`
	Goal         string `json:"goal"`
	StartChapter int    `json:"start_chapter"`
	EndChapter   int    `json:"end_chapter"`
}
```

Mô hình tự quyết định số tập và số cung, có thể tham khảo các tiêu đề group trong tệp nguồn, nhưng không bị giới hạn bởi “một tập”“1～3 cung”. Go dùng title/core_event/hook/scenes của `ImportedChapterFacts` để lắp ráp `OutlineEntry` chính thức.

### 10.4 Trạng thái câu chuyện

Quá trình nhập chỉ dựng lại sự thật trong nội dung truyện (văn bản), không giả vờ các tuyến dài chưa khép lại chỉ để cho Engine tiếp tục:

- `open`: nội dung truyện (văn bản) có mục tiêu hoặc căng thẳng thực sự chưa khép lại, sinh Compass theo cách thông thường;
- `closed`: phát hành như một tác phẩm đã hoàn thành, tập cuối được đánh dấu Final; khi cần viết phần tiếp thì người dùng phải reopen một cách tường minh và nêu hướng mới;
- `uncertain`: trước khi phát hành, yêu cầu người dùng chọn cách xử lý là chưa hoàn thành hay đã hoàn thành; nếu Intent đã lưu lựa chọn qua `--story=open|closed` thì dùng trực tiếp, nếu không thì vào trạng thái chờ tương tác. Lựa chọn được lưu dưới dạng artifact `story-resolution.json` với đầu vào là synthesis hiện tại.

Code không lén đoán ý định của người dùng qua việc kiểm tra `open_threads` có rỗng hay không.

## 11. Lắp ráp và xác minh Foundation

Mô hình tổng hợp ngữ nghĩa, còn Go phụ trách lắp ráp các đối tượng miền chính thức. Trước khi phát hành phải đáp ứng:

1. Premise có tiêu đề tên sách hợp lệ; khi không thể xác nhận tên sách từ nội dung truyện (văn bản), dùng basename của tệp nguồn và đánh dấu nguồn là filename, không để mô hình tuyên bố rằng đó là “tên sách thật”;
2. Mọi phạm vi tập và cung liên tục theo thứ tự;
3. Phạm vi đầu tiên bắt đầu từ chương 1, phạm vi cuối cùng kết thúc ở chương N;
4. Mỗi chương thuộc chính xác một cung;
5. Sau `FlattenOutline`, số chương là N, tiêu đề và sự thật theo từng chương nhất quán;
6. Tên nhân vật, quy tắc thế giới và Compass tuân thủ các ràng buộc kiểu domain hiện có;
7. PlanningTier là giá trị thuộc tập hợp đóng hợp lệ, nhưng lý do lựa chọn đến từ mô hình chứ không phải từ ngưỡng số chương;
8. Trạng thái closed/open khớp với hình dạng đã phát hành của Final, Compass;
9. `InputDigest` của artifact Synthesis có thể được dựng lại từ tập hợp phân tích có thứ tự hiện tại.

Khi vi phạm ràng buộc cấu trúc, đưa lỗi cụ thể về cho mô hình để tạo lại, liên tục cho đến khi thành công hoặc context bị hủy; không ghi xuống đĩa bản dở dang.

## 12. Phát hành chính thức

### 12.1. Điều kiện tiên quyết để phát hành

Bản nhập mới chỉ được đưa vào:

- không có chương đã hoàn thành;
- không có chương đang xử lý hoặc PendingCommit;
- không có không gian làm việc nhập khác thuộc nguồn khác;
- Foundation chính thức đang trống, hoặc hoàn toàn khớp với digest đã phát hành của không gian làm việc hiện tại.

Vì ngữ nghĩa hợp nhất giữa tiểu thuyết hiện có và văn bản bên ngoài mới chưa rõ ràng, phiên bản đầu tiên từ chối rõ ràng, không đoán xem sẽ ghi đè hay ghi thêm.

### 12.2. Phát hành Foundation

Phát hành theo thứ tự phụ thuộc chính thức:

```text
planning tier
→ premise
→ characters
→ world rules
→ layered outline + flat outline
→ compass
→ progress đối soát
```

Mỗi bước:

1. Tính digest của nội dung chờ phát hành;
2. Nếu artifact chính thức chưa tồn tại thì ghi bằng thao tác nguyên tử và thêm checkpoint;
3. Nếu đã tồn tại và digest giống nhau thì bỏ qua một cách idempotent;
4. Nếu đã tồn tại nhưng khác thì trả về lỗi xung đột, không ghi đè.

Sau khi gặp sự cố giữa chừng, chỉ cần đối chiếu lại từ phần tử đầu tiên; không cần giao dịch xuyên tệp hay máy trạng thái Foundation Pending.

### 12.3. Phát hành chương

Tái sử dụng quy trình hiện có theo thứ tự chương:

```text
Lưu draft
→ Progress.StartChapter
→ commit_chapter(sự thật theo từng chương)
```

`commit_chapter` đã có saga PendingCommit, checkpoint và kiểm tra idempotent cho chương đã hoàn thành. Bản nhập không sao chép bộ logic commit thứ hai.

Cửa sổ sự cố:

| Cửa sổ sự cố | Hành vi khôi phục |
|---|---|
| trước draft | lưu lại cùng nội dung truyện (văn bản) |
| sau draft, trước StartChapter | đối chiếu digest rồi tiếp tục |
| sau StartChapter, trước PendingCommit | thực hiện lại commit cho cùng chương |
| trong PendingCommit | do saga commit hiện có khôi phục |
| sau chapter complete | nếu digest/checkpoint nhất quán thì bỏ qua |
| nội dung chính thức xung đột với digest nguồn | dừng rõ ràng, báo cáo chương bị xung đột |

### 12.4. Ranh giới hoàn tất nhập

Sau khi tất cả các chương đã được commit ổn định, đặt `AdvanceHoldAtBoundary` một lần với lý do rõ ràng là “外部小说导入完成，等待验收后续写”. Cơ chế này chỉ bảo vệ lần nhập xuyên hệ thống này, không thay đổi chế độ `auto/review` dài hạn của người dùng.

`--yes` chỉ cho phép tự động chấp nhận việc phân đoạn, không được ngầm bỏ qua Hold này. Chỉ khi người dùng truyền đồng thời cờ `--continue` riêng biệt, Runner mới không tạo Hold dành riêng cho bản nhập; sau đó vẫn tuân thủ chế độ advance bình thường: `auto` có thể tiếp tục viết tiếp, còn `review` vẫn chờ `/next`.

TUI mặc định không còn tự động tiếp tục nối mạch mà không có thông báo. Sau khi người dùng kiểm tra trạng thái Foundation và các chương, họ dùng điểm vào tiếp tục hiện có để khôi phục quá trình sáng tác.

**Điểm đóng bảng**: Sau khi một lần nhập khởi động từ trang chào đón kết thúc thành công, nhấn Esc để đóng bảng sẽ chạy bổ sung một lần `Resume()` (cổng khôi phục ở bootstrap chỉ chạy một lần lúc khởi chạy). Người dùng sẽ đi thẳng đến bàn làm việc, bị Hold hoàn tất nhập chặn ở ranh giới chương kế tiếp để nghiệm thu, thay vì bị giữ lại trên trang chào đón nơi chỉ cần nhấn Enter nhầm là đã "开新书". Ở trạng thái lỗi cuối cùng và trong tình huống trên bàn làm việc, Esc chỉ đóng bảng.

**Lớp phòng vệ khi tạo mới**: `PrepareUserRules` / `StartPrepared` từ chối tạo mới khi thư mục sách đã có chương (`CompletedChapters` khác rỗng) — StartPrepared đặt lại checkpoints và progress ngay từ đầu, nên một lần chạm nhầm sẽ âm thầm xóa toàn bộ cuốn sách (bao gồm tất cả các chương vừa nhập). Phần dàn ý còn sót lại khi chưa có chương vẫn được cho qua, nhằm giữ lại đường tự chữa của Đồng sáng tác khi nhấn Ctrl+S để thử lại trong cùng phiên và khôi phục để cắt bổ sung.

### 12.5. Cổng chặn phát hành qua các lần khởi chạy lại

Khi không gian làm việc tồn tại và `NextAction != done`, `Host.New/Resume` phải nhận diện cuốn sách là một bản nhập chưa hoàn tất:

- cho phép xem, chẩn đoán và thực hiện quy trình khôi phục `/import`;
- cấm khởi chạy Engine thông thường, Continue hoặc phân phối Người viết;
- hiển thị rõ hành động khôi phục hiện tại, không coi Foundation/chương đã phát hành một phần như một cuốn sách có ngữ nghĩa đầy đủ để viết tiếp.

Cổng chặn đọc trực tiếp không gian làm việc và Store chính thức để suy ra, không thêm `published bool`. Nhờ vậy, sau khi sự cố xảy ra ở bất kỳ cửa sổ phát hành nào, quy trình sáng tác thông thường cũng không thể tiêu thụ trạng thái phát hành một phần trước khi Runner đã khôi phục.

## 13. Lõi gọi mô hình

`imp` bên trong giữ một helper typed-call nhỏ và chuyên biệt, không xây dựng framework workflow LLM tổng quát.

### 13.1. Chọn mô hình

- mặc định dùng mô hình của vai trò Kiến trúc sư;
- mức mô hình của các hàm ngữ nghĩa là một nút điều chỉnh mở: segment/analyze/synthesize có thể tự khai báo mức riêng, mặc định rơi về Kiến trúc sư, và tầng cấu hình có thể chỉ định segment, vốn mang tính cơ học nhiều hơn, tới một mức rẻ hơn. Đây là cấu hình lời gọi, không thay đổi bất kỳ hợp đồng ngữ nghĩa nào, cũng không biến 「một vai trò」 thành tiền đề kiến trúc — mục đích là để phần lợi ích chi phí từ việc 「mô hình mức rẻ trở nên mạnh hơn」 cũng có thể được hưởng;
  - Điểm hiện thực: cấu hình roles hỗ trợ ba role key `import_segment` / `import_analyze` / `import_synthesize`; khi chưa cấu hình thì rơi về Kiến trúc sư. Ngân sách kép của từng hàm cùng các tùy chọn thinking và cấu trúc được suy ra độc lập theo năng lực thật của từng mức (cửa sổ nhỏ của mức nhỏ chỉ ràng buộc chính hàm đó), và mức dùng được ghi nhận theo vai trò của mức thực tế;
- tích hợp failover đã cấu hình của vai trò được chọn;
- dùng reasoning effort của vai trò được chọn, và thông qua capability probe để quyết định có gửi tham số thinking hay không;
- ghi metadata session và usage theo provider/model thật;
- đưa vào ngân sách sentinel hiện có (được triển khai ngày 2026-07-16): trước khi khởi chạy phải vượt qua `Refuse()` với cùng kỷ luật của Start/Resume/Continue; ngân sách hết trong lúc chạy sẽ dừng cứng qua `abortWithEvent`, hủy context riêng của bản nhập (Host đăng ký cancel của công việc độc quyền, thay vì chỉ tạm dừng Engine chưa chạy).

### 13.2. Khả năng xuất kết quả có cấu trúc

Bốn loại artifact nhập dùng chung `llmcontract.Execute`: khi mô hình hoặc cấu hình người dùng xác nhận rõ ràng có hỗ trợ, gửi JSON Schema native; khi năng lực chưa biết hoặc xác nhận không hỗ trợ, tự động sinh Prompt Contract từ cùng một Schema. Chế độ native kiểm tra hợp lệ toàn bộ phản hồi, còn chế độ tương thích chỉ trích xuất đối tượng JSON cân bằng; cả hai đường đều kiểm tra hợp lệ theo cùng một Schema trước, rồi mới giải mã DTO và thực hiện kiểm tra nghiệp vụ. Sau khi yêu cầu báo lỗi, không được lặng lẽ xóa schema để thử lại; sai lệch trong đánh giá năng lực hoặc Provider từ chối phải được phơi bày.

### 13.3. Tách lỗi ở tầng yêu cầu, ngữ nghĩa và dung lượng

- Lỗi tầng yêu cầu: chỉ thử lại các lỗi timeout, giới hạn tần suất và lỗi mạng mà adapter đánh dấu rõ ràng là retryable, giữ nguyên ngữ nghĩa backoff hiện có và tiếp tục cho đến khi thành công hoặc context bị hủy;
- Lỗi tầng đầu ra: đưa lỗi cụ thể từ phân tích JSON hoặc Validate về lại cho cùng mô hình, liên tục tự sửa cho đến khi thành công hoặc context bị hủy bởi người dùng/hệ thống ngân sách; vi phạm hợp đồng Schema native, từ chối trả lời và cắt ngắn sẽ không bị hỏi lại một cách mù quáng.
- Không được thử lại trong im lặng: mỗi lần backoff ở tầng yêu cầu ("进行第 N 次重试 · Xs 后重试") và mỗi lần hỏi lại ở tầng đầu ra đều được phản ánh qua sự kiện tiến độ trên bảng nhập — nếu không phản ánh, người dùng sẽ tưởng ứng dụng bị treo. Sự kiện backoff chỉ mang thời điểm chặn (`RetryAt`), số giây còn lại do tầng render tính từng tick để tạo đếm ngược thời gian thực (dùng chung cơ chế với bảng sự kiện của bàn sáng tác); trong suốt thời gian chạy, bảng luôn có spinner ở đầu trên cùng với thời gian đã trôi qua, và cuối nhật ký có thêm con trỏ sao chạy dòng giống hệt bản streaming.
- Phản ánh lỗi không được chung chung: message của gateway thường chỉ có một câu "Provider returned error"; phần phản ánh và văn bản lỗi thống nhất phải kèm theo sự thật có cấu trúc của adapter (phân loại lỗi/trạng thái HTTP/provider/mô hình, `modelErrDetail` được trích xuất từ chuỗi lỗi litellm qua errors.As), đặt sự thật trước và ưu tiên giữ lại khi bị cắt ngắn.
- Giai đoạn dài không được im lặng: việc phân đoạn từng khối và tổng hợp từng khoảng gọi mô hình bên trong hàm (một khối có thể mất vài phút), truyền tiến độ từng khối/từng khoảng qua `callProfile.step` ("切分第 N/M 块，已识别 K 个边界"). Key của sự kiện chỉ dành cho backoff yêu cầu (sự kiện tức thời trong cùng một lời gọi, nhảy tại chỗ); việc hỏi lại do kiểm tra hợp lệ là sự kiện ngữ nghĩa xuyên các lời gọi, từng dòng riêng để giữ lại lịch sử — dùng chung Key sẽ khiến khối sau ghi đè khối trước và làm mất toàn bộ manh mối gỡ lỗi.
- Chuyển toàn bộ nhật ký: mỗi sự kiện tiến độ (kể cả các dòng thử lại bị bảng ghi đè tại chỗ) được ghi vào **nhật ký riêng cho bản nhập** `<书根>/logs/import.log` (không trộn với tui.log; mỗi lần nhập có một tệp để xem toàn bộ bản chép); các lần backoff yêu cầu và hỏi lại ngữ nghĩa cũng được ghi vào cùng nhật ký với chuỗi lỗi đầy đủ.
- Phản ánh ngữ nghĩa của mô hình chứ không chỉ đếm cơ học: phân đoạn từng khối phản ánh các tiêu đề mô hình nhận diện ("模型识别出：第十二章 风雪夜 / …（共 N 处）"), phân tích từng chương phản ánh sự kiện cốt lõi ("第 12 章〈风雪夜〉：……"), hoàn tất tổng hợp thì phản ánh bản tóm tắt toàn bộ sách (tóm tắt premise) — người dùng phải nhìn thấy mô hình đã hiểu nội dung gì.
- Lỗi dung lượng: `StopReasonLength` không được thử lại nguyên trạng, cũng không đi vào vòng lặp tự sửa ngữ nghĩa; analysis batch khi một phần văn bản có thể phân tích thì lưu tiền tố hợp lệ liên tục theo §9.5, nếu không thì ghi `prefix_salvage=unavailable` và thu nhỏ lại batch để hợp nhất lại; các hàm ngữ nghĩa còn lại sẽ thất bại rõ ràng và giữ nguyên phản hồi gốc.

Xác thực, phân quyền, mô hình không hỗ trợ và xung đột trạng thái đều thất bại ngay lập tức. Không mô phỏng thành công, không dùng đối tượng rỗng làm phương án dự phòng và không bỏ qua chương thất bại.

### 13.4. Ngân sách đầu vào và đầu ra

Mỗi hàm ngữ nghĩa có schema, ngân sách đầu vào, dự trữ cho suy luận và ngân sách đầu ra quan sát được riêng:

- Đầu ra phân đoạn chỉ chứa các ranh giới của owned range hiện tại;
- analysis batch đồng thời chịu ràng buộc của context window và giới hạn completion, đầu ra là sự thật theo từng chương trong một dải liên tục hữu hạn;
- RangeDigest chỉ chứa một khoảng liên tục;
- BookSynthesis chỉ chứa sự thật toàn cục và phạm vi tập và cung, không lặp lại đối tượng chương.

Mỗi yêu cầu đều ghi lại ước tính đầu vào, dự trữ suy luận, max tokens được yêu cầu và đầu ra quan sát được dự kiến trước khi gửi. Ước tính chỉ quyết định cách chia khối/gom batch, không xóa nội dung truyện (văn bản) hay trường sự thật. Vì vậy, không tồn tại cấu trúc khiến “tổng số chương càng nhiều thì một phản hồi nhất định càng dài”, và không thể bỏ qua rủi ro cắt ngắn đầu ra chỉ vì đầu vào vừa đủ chứa.

## 14. Sự kiện, nhật ký và chẩn đoán

### 14.1. Giai đoạn sự kiện

```go
const (
	StageIngesting            Stage = "ingesting"
	StageSegmenting           Stage = "segmenting"
	StageAwaitingConfirmation Stage = "awaiting_confirmation"
	StageAnalyzing            Stage = "analyzing"
	StageSynthesizing         Stage = "synthesizing"
	StageAwaitingStoryStatus  Stage = "awaiting_story_status"
	StageValidating           Stage = "validating"
	StagePublishing           Stage = "publishing"
	StageDone                 Stage = "done"
	StageError                Stage = "error"
)
```

Mỗi sự kiện chứa action, chương/khoảng hiện tại, tổng số, thời gian đã trôi qua và lỗi tùy chọn. Sự kiện analysis batch có thêm phạm vi batch, ước tính ngân sách, StopReason và phạm vi tiền tố đã commit. Event là một projection, không tham gia vào khôi phục.

### 14.2. Lỗi phải xuất hiện đồng thời ở ba nơi

1. Bảng nhập của TUI: tự động ngắt dòng, giữ lại toàn bộ chuỗi lỗi;
2. `tui.log`: ghi có cấu trúc stage, chapter/range, model, attempt và error;
3. `meta/import/failures/`: lưu metadata của lần thất bại cuối cùng và phản hồi mô hình chưa bị cắt bỏ.

Nội dung truyện (văn bản) gốc không được ghi vào nhật ký thông thường, cũng không đưa vào bản xuất chẩn đoán có che giấu danh tính theo mặc định. Phản hồi lỗi nằm trong thư mục sách của chính người dùng, và thông báo lỗi nêu rõ đường dẫn.

### 14.3. Session và Usage

Mỗi lời gọi ngữ nghĩa ghi lại:

- Tên task ổn định, như `import/segment/0003`, `import/analyze/0054-0061`;
- Phản hồi gốc của assistant;
- provider/model và usage;
- structured mode, thinking level và kết quả kiểm tra hợp lệ đầu ra.

Mức dùng được gom vào vai trò Kiến trúc sư, để ngân sách nhìn thấy được chi phí nhập.

## 15. Vòng đời và đồng thời

- Việc nhập loại trừ với các thao tác ghi của Engine, đồng sáng tác ở giai đoạn và simulation;
- Trong thời gian nhập, mỗi cuốn sách chỉ cho phép một Runner;
- Khi người dùng huỷ, các lệnh gọi mô hình đang thực hiện sẽ bị huỷ; các sự thật trong không gian làm việc đã được ghi xuống đĩa theo cách nguyên tử vẫn được giữ lại;
- Huỷ trước khi xác nhận sẽ không sửa đổi Store chính thức;
- Huỷ sau khi bắt đầu phát hành không thực hiện hoàn tác mang tính phỏng đoán; lần sau chỉ có thể khôi phục chính xác việc phát hành;
- Trong phiên bản đầu, các analysis batch diễn ra tuần tự; bên trong mỗi batch, một lệnh gọi mô hình trả về sự thật theo thứ tự chương; việc phát hành chính thức vẫn diễn ra tuần tự theo từng chương;
- `Host.New/Resume` thực thi cổng chặn §12.5 khi import chưa hoàn tất; ngữ nghĩa loại trừ vẫn giữ nguyên qua các lần khởi động lại tiến trình;
- Việc xuất có cho phép đồng thời hay không giữ nguyên ngữ nghĩa chỉ-đọc hiện có, nhưng nó chỉ có thể thấy những chương đã được phát hành chính thức.

## 16. Bất biến cốt lõi

1. Mỗi artifact trong không gian làm việc được định danh bởi `SchemaVersion + InputDigest + Payload`; chỉ có thể tái sử dụng khi dựng lại được cùng `InputDigest` từ đầu vào ngữ nghĩa thực tế hiện tại.
2. Manifest tương ứng với duy nhất một ảnh chụp nguồn đã chuẩn hóa; mỗi đoạn văn bản nguồn không rỗng phải thuộc về đúng một nơi.
3. Mô hình chỉ có thể tham chiếu SourceUnit, điểm neo văn bản gốc và số chương do Host cung cấp; Go chỉ chấp nhận các tọa độ có thể ánh xạ duy nhất về byte nguồn.
4. analysis batch chỉ có thể gửi một phản hồi đầy đủ, hoặc với `StopReasonLength`, tiền tố hợp lệ liên tục dài nhất bắt đầu từ chương đầu tiên; việc thiếu bất kỳ chương nào sẽ chặn mọi phân tích và tổng hợp về sau.
5. Các phạm vi tập–cung phải liên tục, không chồng lấn và phủ kín hoàn toàn `1..N`; Foundation chính thức chỉ có thể được phát hành từ Synthesis đã vượt qua toàn bộ kiểm tra xác minh.
6. Các chương chính thức chỉ có thể được phát hành theo đúng thứ tự qua `commit_chapter`; artifact chính thức đã tồn tại chỉ có thể được tái sử dụng theo cách idempotent khi digest nội dung giống nhau, nếu khác thì thất bại do xung đột.
7. Không được cách diễn giải bất kỳ lỗi mô hình nào thành “không có nội dung” hoặc “tiếp tục chương sau”, cũng không được vá nửa JSON hay bỏ qua chương bị lỗi.
8. `done` phải được chứng minh đồng thời bởi artifact không gian làm việc, artifact chính thức, Progress, PendingCommit và điểm kiểm tra; Engine thông thường không được khởi chạy trước khi `done`.

## 17. Cấu trúc package và giao diện hẹp

Giữ nguyên `internal/host/imp`, chia theo trách nhiệm:

```text
imp/
├── types.go       công bố Options/Event và DTO ngữ nghĩa
├── source.go      đọc, giải mã, chuẩn hóa, SourceUnit/anchor
├── workspace.go   artifact nguyên tử meta/import và InputDigest
├── call.go        lệnh gọi typed LLM riêng cho import
├── segment.go     phép chiếu cấu trúc, hàm ngữ nghĩa ranh giới, kiểm tra hợp lệ độ bao phủ
├── analyze.go     lô liên tiếp với ngân sách kép, sự thật theo từng chương và tiền tố bị cắt ngắn
├── synthesize.go  RangeDigest và BookSynthesis
├── publish.go     đối soát Foundation và phát hành qua commit_chapter
└── runner.go      LoadState → NextAction → thực thi
```

Không thêm `ImportEngine`, `Task`, `WorkflowInstance`, Repository dùng chung hay sổ đăng ký plugin.

Các dependency do Host tiêm vào vẫn giữ ở dạng hẹp:

```go
type Deps struct {
	Store         *store.Store
	CommitChapter ChapterCommitter
	Model         agentcore.ChatModel
	Runtime       ModelRuntime
	Prompts       Prompts
	Emit          func(Event)
}
```

`ModelRuntime` chỉ mang theo các thực tế của lệnh gọi như context window, giới hạn completion, thinking, callback session/usage, đồng thời dành sẵn một vị trí chọn mức model cho mỗi hàm ngữ nghĩa (mặc định là architect); không để `imp` phụ thuộc ngược lại vào toàn bộ Host, cũng không hàn một vai trò đơn lẻ thành tiền đề kiến trúc.

## 18. Giao diện người dùng

### 18.1 Nhập mới

```text
/import <path> [--yes] [--story=open|closed] [--continue] [--guide=<切分指导>]
```

Hành vi mặc định: tạo ảnh chụp nguồn, cắt theo ngữ nghĩa và mở bản xem trước để xác nhận; sau khi hoàn tất phát hành, đặt một Hold dành riêng cho import. Xóa `from=N`.

Ba tùy chọn đầu là các quyền cho phép tường minh, độc lập với nhau, và được ghi vào `intent.json`:

- `--yes`: tự động chấp nhận kết quả cắt sau khi kiểm tra xác minh ghi đè đạt; không quyết định trạng thái câu chuyện uncertain, không bỏ qua Hold khi hoàn tất;
- `--story=open|closed`: chỉ cung cấp trước lựa chọn của người dùng khi synthesis trả về uncertain; không ghi đè sự thật của mô hình khi mô hình đã phán quyết rõ ràng open/closed;
- `--continue`: không tạo Hold dành riêng cho import; không bỏ qua advance mode thông thường; trong `review` vẫn cần `/next`.

`--guide` khác với ba tùy chọn trước: đây không phải quyền cho phép khởi chạy mà là đầu vào ngữ nghĩa cho việc cắt, được ghi xuống đĩa thành `guidance.txt` trong không gian làm việc (có thể chứa khoảng trắng, phải đặt cuối lệnh). Xem §18.3.

Vì vậy `/import book.txt --yes` vẫn dừng lại sau khi import hoàn tất; chỉ khi truyền thêm `--continue` thì quy trình sáng tác mới được phép tiếp tục nối tiếp khi các cổng chặn thông thường cho phép.

### 18.2 Khôi phục

Khi một cuốn sách đã có không gian làm việc đang hoạt động, chạy `/import` không tham số để suy ra bước tiếp theo trực tiếp từ sự thật và intent đã lưu; đường dẫn tệp nguồn và tham số khởi chạy đều không phải là yêu cầu bắt buộc để khôi phục. `/import <path>` kèm đường dẫn mới không được ghi đè không gian làm việc đang hoạt động.

Import chưa hoàn tất phải hiện diện chủ động, không được đợi đến khi yêu cầu sáng tác của người dùng bị cổng chặn từ chối mới bị lộ ra. Thực hiện bằng ba lớp nhắc nhở tăng dần:

1. Khi khởi chạy, TUI kiểm tra một lần (`imp.ResumeSummary`, sinh mô tả theo giai đoạn dựa trên `NextAction`), màn hình chào hiển thị nổi bật "phát hiện import chưa hoàn tất (đã phân tích N/M chương), nhập /import để tiếp tục từ điểm dừng";
2. Khi người dùng bỏ qua lời nhắc và thử sáng tác, cổng chặn xuyên suốt các lần khởi động lại (§12.5) từ chối khởi chạy engine và phát sự kiện cảnh báo;
3. Trong lúc khôi phục đang chạy, bảng nhập hiển thị theo thời gian thực giai đoạn hiện tại và tiến độ.

### 18.3 Cắt lại

Sau khi người dùng kiểm tra bản xem trước, dùng `/import --guide=<自然语言说明>` để nhận diện lại, ví dụ `--guide=幕间·X 也是独立章节`. Hướng dẫn được ghi vào `guidance.txt` trong không gian làm việc và được tính vào `InputDigest` của segmentation: khi hướng dẫn thay đổi, kết quả cắt cũ cùng confirmation, phân tích và synthesis cũ không thể dựng lại cùng `InputDigest`, nên tất nhiên toàn bộ được làm lại; không cung cấp trình biên tập biểu thức chính quy.

### 18.4 Huỷ

Huỷ trước khi xác nhận chỉ giữ lại không gian làm việc; trước khi phát hành, có thể chủ động loại bỏ toàn bộ không gian làm việc. Sau khi phát hành bắt đầu, không cung cấp thao tác loại bỏ kiểu “giả vờ chưa có gì xảy ra”; chỉ cho phép khôi phục và hoàn tất, hoặc người dùng tự xử lý cuốn sách chính thức.

## 19. Thứ tự triển khai

### Giai đoạn 1: Không gian làm việc và suy luận trạng thái thuần tuý

- Manifest, Intent, ảnh chụp nguồn, `Artifact/InputDigest`, đọc/ghi nguyên tử;
- `LoadState/NextAction`;
- Kiểm tra xác minh tiên quyết cho sách rỗng và khôi phục cùng nguồn;
- Xóa phụ thuộc thiết kế `ResumeFrom`.

Giai đoạn này không gọi mô hình; trước tiên chứng minh các sự thật khôi phục không mơ hồ.

### Giai đoạn 2: Cắt theo ngữ nghĩa và xác nhận

- SourceUnit, chia nhỏ ảo cho dòng quá dài, điểm neo văn bản gốc và chia khối theo ngân sách ngữ cảnh;
- Lệnh gọi typed cho BoundaryDecision;
- Kiểm tra xác minh bao phủ toàn văn;
- Bản xem trước TUI, nhận diện lại bằng ngôn ngữ tự nhiên, `--yes` và artifact confirmation.

Trước tiên dùng các tiêu đề không theo chuẩn, tiêu đề tập, lời nói đầu và lời ghi chú cuối để kiểm tra “không sót một chữ nào”.

### Giai đoạn 3: Sự thật theo từng chương trong các batch liên tục

- `ImportedChapterFacts`;
- Lập kế hoạch batch với ngân sách kép context/completion;
- Phân tích tuần tự giữa các batch và ledger tính liên tục gọn nhẹ;
- Khôi phục artifact `InputDigest` cho từng chương;
- Cắt ngắn nghĩa là 「thất bại + thu nhỏ và tổ chức lại batch」, đồng thời ghi lại văn bản một phần có dùng được hay không;
- Nối session, usage, failover, thinking, lỗi hạn mức và thử lại theo phản hồi cấu trúc.

### Giai đoạn 3·Bổ sung: Cứu tiền tố bị cắt (tối ưu hiệu quả, có thể làm sau)

- Phân tích tiền tố hợp lệ liên tục cho `StopReasonLength` (§9.5);
- Chỉ bật khi văn bản một phần có thể phân tích, không thay đổi tính đúng đắn của việc khôi phục; có công tắc riêng và tiêu chí nghiệm thu riêng.

### Giai đoạn 4: Tổng hợp phân cấp và Foundation

- RangeDigest nhận biết ngữ cảnh;
- BookSynthesis;
- Cấu trúc tập–cung theo phạm vi;
- StoryStatus;
- Lắp ráp và kiểm tra xác minh đầy đủ Foundation.

### Giai đoạn 5: Phát hành và bàn giao

- Đối soát digest từng artifact của Foundation;
- Phát hành bằng cách tái sử dụng `commit_chapter`;
- Khôi phục sau huỷ/sập;
- Cổng chặn Engine xuyên suốt các lần khởi động lại;
- AdvanceHold mặc định khi import hoàn tất và tùy chọn `--continue` tường minh;
- TUI/log/failure artifact đầy đủ.

### Giai đoạn 6: Xóa triển khai cũ

- Xóa phán quyết định dạng chương trong `splitter.go`;
- Xóa tagged envelope;
- Xóa lệnh gọi toàn bộ cuốn sách `ReverseFoundation`;
- Xóa ngưỡng số chương của `pickScale`;
- Xóa `ResumeFrom/from=N`;
- Xóa ràng buộc prompt “một tập cố định, 1～3 cung, cưỡng chế open threads”;
- Chỉ cập nhật mô tả quy trình cũ trong README và architecture sau khi triển khai hoàn tất.

## 20. Kiểm thử và nghiệm thu

### 20.1 Hàm thuần và kiểm thử thuộc tính

- Mọi segmentation hợp lệ đều thỏa mãn phạm vi toàn văn không chồng lấn, không thiếu;
- SourceUnit không hợp lệ, điểm neo văn bản gốc không duy nhất, ranh giới đảo thứ tự hoặc trùng lặp đều phải bị từ chối;
- Thứ tự ranh giới được phán quyết theo thứ tự số của `(Line, Part)`; tạo một tập unit mà «thứ tự từ điển và thứ tự số cho kết luận đối lập», khẳng định rằng nó vượt qua theo thứ tự số;
- Cả dòng thường lẫn lát ảo đều có thể ánh xạ không tổn thất về cùng một tập byte nguồn đã chuẩn hóa;
- Mọi ranges tập–cung hợp lệ đều phủ đúng `1..N`;
- Cùng đầu vào ngữ nghĩa luôn sinh ra cùng `InputDigest`; bất kỳ thay đổi nào trong đầu vào thực tế cũng làm cho artifact tương ứng không khớp;
- Việc gom batch với ngân sách kép không vượt quá ràng buộc context/completion đã cho;
- NextAction luôn không đổi đối với cùng một ảnh chụp sự thật.

Thực hiện fuzz/property test cho ánh xạ tọa độ, lắp ráp phạm vi, ngân sách batch và `InputDigest`; không khẳng định mô hình sẽ xuất ra một tiêu đề cố định nào.

### 20.2 Kiểm thử hợp đồng mô hình

- Tên chương không theo chuẩn và cấu trúc tập/chương pha trộn;
- Chương mở đầu/phần chuyển/ngoại truyện được mô hình phán quyết theo ngữ nghĩa là chương;
- front/back matter được hiển thị rõ ràng thay vì bị loại bỏ;
- Toàn bộ sách trong một dòng, một dòng chứa nhiều chương và dòng vượt ngân sách đều được cắt chính xác qua SourceUnit + anchor;
- Chương yên tĩnh được phép có characters rỗng;
- JSON không hợp lệ, thiếu trường, phạm vi vượt ranh giới đều đi vào luồng thử lại theo phản hồi;
- analysis batch trả về các đối tượng theo từng chương liên tục, không được bỏ số hoặc lặp lại;
- `StopReasonLength` chỉ lưu tiền tố hợp lệ liên tục dài nhất; nửa đối tượng và các đối tượng không liên tục phía sau không được lưu;
- Khi chế độ có cấu trúc không tạo ra văn bản một phần có thể phân tích, khẳng định rằng luồng đi theo 「thất bại + thu nhỏ và tổ chức lại batch」 và nhật ký ghi `prefix_salvage=unavailable`;
- JSON hỏng dưới `StopReasonStop` thông thường không đi vào luồng tiền tố bị cắt;
- Khi một chương đơn lẻ vẫn bị cắt, phải thất bại tường minh, không sinh sự thật rỗng;
- Lỗi phân tích/lỗi nghiệp vụ của Prompt Contract liên tục được phản hồi để tự sửa cho đến khi thành công hoặc ngữ cảnh bị huỷ; vi phạm hợp đồng Schema gốc, từ chối trả lời và cắt ngắn phải giữ nguyên phản hồi gốc và kết thúc ngay;
- Mô hình không hỗ trợ thinking/JSON Schema không nhận tham số không hợp lệ.

Kiểm thử mô hình khẳng định hợp đồng và bất biến, không khẳng định các phán quyết văn học chính xác.

### 20.3 Ma trận sập

Ít nhất bao gồm:

- Sau khi tạo ảnh chụp nguồn;
- Sau segmentation, trước khi xác nhận;
- Trước và sau khi đối tượng thứ N của analysis batch được ghi xuống đĩa;
- Trước và sau khi chương cuối của tiền tố bị cắt theo độ dài được ghi xuống đĩa;
- Giữa chừng RangeDigest;
- Sau Synthesis, trước Foundation;
- Trước và sau mỗi artifact Foundation;
- Các cửa sổ draft/StartChapter/PendingCommit/progress/checkpoint;
- Sau khi chương cuối được gửi, trước và sau AdvanceHold;
- Khởi động lại sau khi đã phát hành một phần Foundation/chương và thử dùng Host.Resume thông thường.

Sau mỗi lần khởi động lại từ một cửa sổ, chỉ có thể tiếp tục hành động hiện tại; không được tiêu thụ lại một lệnh gọi mô hình đã thành công, cũng không được vượt qua artifact bị lỗi. Khi `NextAction != done`, Engine thông thường phải bị cổng chặn chặn lại cho đến khi việc khôi phục import hoàn tất.

### 20.4 Hình dạng hồi quy #83

Tạo đầu vào dài từ 54 chương trở lên, xác minh:

1. Không có lệnh gọi đơn lẻ nào yêu cầu xuất dàn ý chi tiết cho 54 chương;
2. analysis batch gom batch đồng thời theo ngân sách context đầu vào và completion đầu ra nhìn thấy được, không nhồi quá nhiều chương chỉ vì đầu vào còn chỗ;
3. Giai đoạn 3·Bổ sung: mô phỏng `StopReasonLength` với “13 chương đầu hoàn chỉnh, chương 14 bị cắt”, chỉ gửi 13 chương đầu và hành động tiếp theo bắt đầu từ chương 14; khi chưa triển khai cứu tiền tố, toàn bộ batch thất bại và phải tổ chức lại batch bắt đầu từ chương đầu của batch;
4. Mô phỏng phản hồi bị cắt không có đối tượng hoàn chỉnh: lỗi hiển thị đầy đủ, ghi log, lưu phản hồi gốc và không ghi artifact phân tích;
5. Mô phỏng JSON hỏng thông thường, đi theo luồng thử lại bằng phản hồi cấu trúc thay vì cứu tiền tố;
6. Sau khi sửa, chỉ chạy lại hành động thiếu đầu tiên, không làm lại các chương đã hoàn tất;
7. Tiêu đề không theo chuẩn đi qua cắt theo ngữ nghĩa để vào bản xem trước, không được sửa bằng cách thêm biểu thức chính quy.

### 20.5 Tiêu chuẩn nghiệm thu cuối cùng

1. Chế độ tương tác mặc định cho phép người dùng xem và xác nhận toàn bộ ranh giới chương trước khi ghi chính thức; `--yes` có thể tự động chấp nhận một cách tường minh và để lại các artifact kiểm toán tương đương.
2. Mọi đoạn văn bản nguồn không rỗng đều tìm được một mối thuộc về duy nhất trong segmentation.
3. 200～500 chương không tạo ra một lệnh gọi mô hình đọc toàn bộ nội dung truyện và xuất đối tượng của tất cả các chương; mỗi batch phân tích đều chịu ràng buộc của cả ngân sách đầu vào lẫn đầu ra, còn đầu ra toàn cục chỉ thể hiện sự thật toàn cục và phạm vi tập–cung.
4. Sau khi bất kỳ giai đoạn nào sập, có thể khôi phục chính xác mà không cần `from=N`.
5. Trạng thái chính thức giữ nguyên cho đến khi hoàn tất xác minh ngữ nghĩa.
6. Sau khi việc phát hành bị gián đoạn, commit saga hiện có có thể khôi phục và không gửi lại chương đã gửi.
7. Import chưa hoàn tất không thể khởi chạy Engine thông thường sau khi khởi động lại; chỉ có thể xem, chẩn đoán hoặc khôi phục import.
8. `--yes` không bỏ qua Hold khi hoàn tất; chỉ `--continue` độc lập mới bỏ qua, và không vượt qua cổng chặn review.
9. Năng lực, mức sử dụng, StopReason, ước tính ngân sách và lỗi của mô hình cùng provider đều có thể quan sát được.
10. Chỉ cần thay bằng mô hình mạnh hơn là có thể cải thiện chất lượng cắt, phân tích và tổng hợp, đồng thời tự nhiên mở rộng batch an toàn và giảm số lần gọi, mà không phải sửa các quy tắc văn học trong Go.

## 21. Khả năng mở rộng hướng tới tương lai

Khả năng mở rộng của phương án này đến từ các ranh giới ổn định, chứ không phải từ việc dựng sẵn các lớp trừu tượng:

- Tăng cường khả năng hiểu của mô hình: ba loại hàm ngữ nghĩa Boundary/Chapter/Synthesis trở nên chính xác hơn trực tiếp;
- Mở rộng cửa sổ ngữ cảnh hoặc đầu ra: bộ lập ngân sách kép tự động mở rộng analysis batch an toàn và giảm số lần chia khối cùng số tầng Reduce;
- Tăng cường đầu ra có cấu trúc: typed-call tự động chọn ràng buộc provider mạnh hơn;
- Mô hình ở mức giá rẻ mạnh lên: các segment mang tính cơ học nhiều hơn có thể chuyển sang mức giá rẻ hơn, lợi ích chi phí đi kèm mà không thay đổi hợp đồng ngữ nghĩa;
- Định dạng đầu vào mới: chỉ cần chuyển EPUB và các định dạng tương tự sang cùng một văn bản chuẩn hóa và tọa độ SourceUnit;
- Ngữ nghĩa toàn sách mới: thêm các trường có người tiêu thụ rõ ràng vào `ImportedChapterFacts` hoặc `BookSynthesis`, không thay đổi giao thức khôi phục và phát hành;
- Tăng cường đồng sáng tác với người dùng: bổ sung bản sửa bằng ngôn ngữ tự nhiên tại bước xác nhận ranh giới, không viết kiến thức về định dạng vào mã nguồn.

Phần không đổi gồm bao phủ toàn văn, danh tính `InputDigest`, kiểm tra phạm vi và phát hành idempotent. Đây là những phần sổ sách mà ngay cả model mạnh hơn cũng không đáng giao cho model xử lý; toàn bộ ngữ nghĩa có thể thay đổi nằm lại trong các hàm mô hình, nên lợi ích của việc nâng cấp mô hình có thể thấm vào kết quả sản phẩm.

## 22. Quyết định cuối cùng

Chọn **pipeline nhập theo ngữ nghĩa có từng giai đoạn**, từ chối hai hướng:

1. Tiếp tục mở rộng biểu thức chính quy cho chương và các ngưỡng số chương/số cung;
2. Để một Agent với vòng lặp dài tự do tiếp quản toàn bộ việc nhập.

Ranh giới cuối cùng là:

> **Mô hình quyết định văn bản mang ý nghĩa gì; mã nguồn bảo đảm mỗi chữ đi về đâu, mỗi kết quả tương ứng với đầu vào nào, đầu vào và đầu ra của mỗi lệnh gọi đều vừa sức chứa, sau khi thất bại phải tiếp tục từ đâu, và khi nào một kết quả mới đủ tư cách trở thành sự thật chính thức.**

Cách này vừa giữ được khả năng tự chủ của mô hình cùng lợi ích trong tương lai, vừa duy trì kiến trúc gọn gàng hiện tại của ainovel-cli: Engine + các hàm ngữ nghĩa có kiểu + lớp sự thật tệp.
