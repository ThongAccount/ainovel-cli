# Thiết kế thống nhất cho quy tắc người dùng

## 1. Trong một câu

Mọi quy tắc viết dài hạn đều được chuẩn hoá vào cùng một bản chụp quy tắc của cuốn sách; lúc chạy, snapshot đó chỉ được phân phối qua `novel_context`, không còn nhồi văn bản quy tắc gốc vào prompt nhiều lần.

```text
prompt khởi động / tệp rules của người dùng / yêu cầu dài hạn trong lúc chạy
        ↓
Chuẩn hoá ngữ nghĩa bằng LLM (theo từng nguồn)
        ↓
Gộp tất định bằng Go (theo thứ tự ưu tiên)  ←  Quy tắc mặc định của hệ thống (nhúng trong code, đi thẳng vào bước gộp, không qua LLM)
        ↓
output/novel/meta/user_rules.json
        ↓
novel_context chèn vào
        ↓
Architect / Writer / Editor / kiểm tra commit dùng chung
```

## 2. Trạng thái hiện thực (2026-07-19, đã triển khai + đã sửa thiếu sót sau review)

Thiết kế này đã được hiện thực, 24 package `go build` / `go vet` / `go test` đều xanh. Sau một vòng code review đã sửa 4 khoảng trống (đều đã sửa): ① quy tắc trong prompt khởi động chỉ được nối vào phương thức chết `Host.Start`, còn đường vào thật đi qua `StartPrepared` nên không tạo snapshot — đã truyền thẳng prompt gốc từ hai đường vào quick/cocreate và gọi thống nhất `Host.PrepareUserRules`; ② lỗi ghi snapshot xuống đĩa bị nuốt — `PrepareUserRules` đổi thành ghi lỗi thì trả về error và huỷ việc mở sách (đường resume giữ best-effort, tránh mang thêm chế độ lỗi mới cho sách cũ); ③ lỗi đọc tệp rules bị bỏ qua im lặng — `raw.go` ghi log cho lỗi khác "không tồn tại" (quyền truy cập, v.v.); ④ README vẫn dạy YAML/front matter cũ và trỏ tới tệp đã bị xoá — đã viết lại.

Kết quả triển khai nhất quán với tài liệu này; các lựa chọn hiện thực sau khi nâng cấp đầu ra có cấu trúc như sau:

1. **Chuẩn hoá chỉ có một `Contract.Schema`, không duy trì hai bộ prompt.**
   Khi model khai báo hỗ trợ thì phát JSON Schema gốc; khi không hỗ trợ hoặc không rõ năng lực thì tầng hợp đồng thống nhất chèn cùng Schema đó vào prompt.
   Cả hai chế độ đều được Go kiểm tra lại Schema, sau đó thực hiện kiểm tra giá trị và nghiệp vụ xuyên trường.
2. **Khi một trường đơn lẻ không hợp lệ thì hạ cấp thành "thiếu trường đó", chứ không hạ cấp cả nguồn.**
   Ví dụ một trường là chỗ trống hoặc sai kiểu, sanitize sẽ bỏ trường đó (coi như chưa khai báo) và giữ lại các trường hợp lệ còn lại của nguồn đó;
   chỉ khi "cả lần chuẩn hoá thất bại" (mạng / model / JSON không hợp lệ / lỗi phân tích) mới hạ cấp toàn bộ nguồn thành raw preferences và
   đặt `status=degraded`. Như vậy một trường hỏng không liên luễn các quy tắc hợp lệ khác của cùng nguồn. Lỗi đầu ra mà model có thể tự sửa sẽ mang nguyên nhân chính xác để tiếp tục tự lành, vòng đời do `context` điều khiển; lỗi kết thúc dứt khoát đi vào log và hạ cấp theo nguồn.

Điểm đặt mã: `internal/rules` (dữ liệu thuần + gộp tất định: snapshot.go / raw.go / types.go), `internal/userrules`
(chuẩn hoá bằng LLM + điều phối + ghi xuống đĩa: normalize.go / service.go), `internal/store/user_rules.go` (lưu trữ snapshot),
`internal/userrules/service.go` (ghi quy tắc lúc chạy), `assets/prompts/arbiter-intervention.md` (phân ba nhánh).
Baseline máy học mặc định của hệ thống đã được chuyển từ `assets/rules/default.md` vào code dưới dạng `rules.SystemDefaults()`, đường phân tích YAML và phụ thuộc yaml.v3 đã bị xoá. **Chưa kiểm chứng**: chuỗi đầy đủ "LLM thật mở sách / hành động rules của Arbiter lúc chạy" (prototype offline của normalizer đã kiểm chứng 10/10).

## 3. Vì sao

Writer ở mỗi chương không nhận được ổn định toàn bộ prompt ban đầu của người dùng. Nó chủ yếu dựa vào nhiệm vụ của chương và `novel_context(chapter=N)`.

Vì vậy quy tắc dài hạn không thể dựa vào trí nhớ lịch sử hội thoại, cũng không nên dựa vào regex để lén lút đoán từ ngôn ngữ tự nhiên. Cách đúng là: chuẩn hoá tường minh quy tắc dài hạn thành trạng thái, rồi để `novel_context` phân phối thống nhất.

"Chuẩn hoá" ở đây bắt buộc phải dùng khả năng hiểu ngôn ngữ tự nhiên của mô hình lớn, chứ không phải liệt kê các cách diễn đạt trong Go. Chương trình chỉ định nghĩa một ít trường có thể kiểm tra bằng máy, và đảm nhiệm schema, gộp tất định, kiểm tra hợp lệ, ghi xuống đĩa và kiểm tra lúc commit; các cách diễn đạt kiểu "mỗi chương khoảng một nghìn năm trăm chữ", "một chương đừng quá hai nghìn", "đừng viết kiểu bánh răng cơ địa mệnh nữa" được LLM hiểu theo ngữ nghĩa.

## 4. Trạng thái thống nhất

Lúc chạy, cuốn sách chỉ duy trì một nguồn sự thật về quy tắc người dùng:

```text
output/novel/meta/user_rules.json
```

Hình dạng được giữ đơn giản:

```json
{
  "version": 1,
  "status": "ready",
  "structured": {
    "genre": "修仙",
    "forbidden_chars": [],
    "forbidden_phrases": ["某种程度上"],
    "fatigue_words": {}
  },
  "preferences": "主角冷静克制；少解释，多用行动和对话。",
  "sources": [
    "startup_prompt",
    ".ainovel/rules/style.md"
  ],
  "uncertain": [
    "少用比喻：没有明确阈值，按风格偏好处理"
  ]
}
```

Ranh giới các trường:

- `version`: phiên bản schema của snapshot, thuận tiện cho việc di chuyển trong tương lai.
- `status`: `ready` / `degraded`, đánh dấu việc chuẩn hoá có hoàn tất hay không; chỉ dùng để phản chiếu và chẩn đoán, không đi vào phán đoán sáng tác.
- `structured`: các quy tắc mà code kiểm tra bằng máy hoặc tiêu thụ một cách ổn định.
- `preferences`: các sở thích bằng ngôn ngữ tự nhiên không kiểm tra bằng máy được nhưng có hiệu lực dài hạn với việc sáng tác.
- `sources`: kiểm toán nguồn, không đi vào phán đoán sáng tác.
- `uncertain`: chẩn đoán của việc chuẩn hoá, chỉ dùng để phản chiếu và truy tìm, không đi vào phán đoán sáng tác.

Chỉ `structured` và `preferences` được chèn cho model; `version` / `status` / `sources` / `uncertain` là siêu dữ liệu vận hành và chẩn đoán, không vào `working_memory.user_rules`. Lỗi kỹ thuật không vào snapshot, chỉ vào log (xem §8).

## 5. Các nguồn đầu vào

Quy tắc dài hạn có bốn nguồn đầu vào:

1. **Prompt khởi động**: các yêu cầu dài hạn người dùng viết khi mở sách.
2. **Tệp rules của người dùng**: sở thích dài hạn ở cấp toàn cục hoặc cấp dự án, đọc như ngôn ngữ tự nhiên thông thường.
3. **Quy tắc mặc định của hệ thống**: baseline máy học nhúng sẵn trong code.
4. **Yêu cầu dài hạn trong lúc chạy**: khi người dùng nói giữa chừng kiểu "từ giờ cứ thế", Arbiter trích xuất hành động `rules`, Host gọi `AddRuntimeRule`.

Các nguồn đầu vào này không đi thẳng vào prompt của Writer, cũng không bị đọc lại nhiều lần lúc chạy. Chúng chỉ tham gia chuẩn hoá khi sinh hoặc cập nhật snapshot, kết quả được gộp vào `meta/user_rules.json`.

## 6. Tệp rules

Tệp rules là prompt dài hạn thông thường, không phải prompt lúc chạy, cũng không phải tệp cấu hình. Nó chỉ là đầu vào để chuẩn hoá, không hỗ trợ YAML:

```md
# 写作偏好

每章 1200-1600 字。
主角冷静克制，不要圣母。
少解释，多用行动和对话推进。
不要出现“某种程度上”。
```

Sau khi hệ thống đọc, nội dung được chuẩn hoá thành:

```json
{
  "structured": {
    "forbidden_phrases": ["某种程度上"]
  },
  "preferences": "每章 1200-1600 字；主角冷静克制，不要圣母；少解释，多用行动和对话推进。"
}
```

Nếu trong tệp xuất hiện YAML front matter thì vẫn được xử lý như văn bản thông thường, không coi là khai báo có cấu trúc. Kết quả có cấu trúc chỉ đến từ quy trình chuẩn hoá thống nhất.

Sau khi khởi động, nếu người dùng sửa tệp rules thì cuốn sách hiện tại không tự đổi; cần sinh lại snapshot. Nhờ vậy sách cũ không bị trôi lệch hành vi chỉ vì tệp rules toàn cục thay đổi.

## 7. Chuẩn hoá ngữ nghĩa

Chuẩn hoá là một lượt gọi LLM độc lập, có ràng buộc schema — mỗi nguồn được chuẩn hoá một lần, không trộn vào quá trình sinh nội dung, cũng không dựa vào biểu thức chính quy hoặc bảng từ khoá để phân tích cứng.

Đầu vào:

- Nguyên văn của một nguồn duy nhất (prompt khởi động / một tệp rules / một yêu cầu lúc chạy)
- Mô tả các trường `structured` hiện hệ thống hỗ trợ

Quy tắc mặc định của hệ thống không nằm trong danh sách này — chúng là các quy tắc có cấu trúc đã biên dịch sẵn nhúng trong code, đi thẳng vào §10, không qua normalizer.

Đầu ra:

- `structured` ứng viên của nguồn đó
- `preferences` ứng viên của nguồn đó
- `sources`
- `uncertain`

Trách nhiệm phía Go:

- Cung cấp schema.
- Kiểm tra kiểu trường và giá trị.
- Theo thứ tự ưu tiên ở §10, gộp tất định các nguồn (LLM không phân xử thứ tự ưu tiên của nguồn).
- Lưu snapshot.
- Chèn snapshot trong `novel_context`.
- Dùng chính snapshot đó để kiểm tra máy trong `commit_chapter`.

Trách nhiệm phía LLM:

- Hiểu quy tắc bằng ngôn ngữ tự nhiên của một nguồn duy nhất.
- Nâng các quy tắc rõ ràng, kiểm tra được bằng máy lên `structured`.
- Giữ các sở thích về thẩm mỹ, phong cách, nhân vật ở dạng `preferences`.
- Giữ lập trường bảo thủ với nội dung không chắc chắn, không tự bịa ngưỡng.

### 7.1 Nâng một cách bảo thủ

`structured` là quy tắc cứng hoặc tham số ổn định, không phải "khu vực model tự đoán". Việc nâng quy tắc phải bảo thủ:

- Chỉ ghi vào `structured` khi người dùng diễn đạt rõ ràng, không mơ hồ.
- `forbidden_chars` / `forbidden_phrases` là các trường mức error, phải bảo thủ đặc biệt; chỉ nâng khi có lệnh cấm rõ ràng kiểu "đừng xuất hiện X", "cấm X", "đừng viết X".
- `fatigue_words` chỉ được nâng khi người dùng đưa ra cả từ và ngưỡng rõ ràng; yêu cầu không có ngưỡng kiểu "bớt dùng ẩn dụ", "đừng quá văn", "giảm lời lẽ quen" đi vào `preferences`.
- Mọi ý định về số chữ/độ dài ("mỗi chương 3000 chữ", "ngắn hơn một chút") đều đi vào `preferences`: độ dài chương là phán đoán ngữ nghĩa theo nhịp kể, không kiểm tra bằng máy — đóng thành ranh giới cứng sẽ khiến model phải điền nước cho tới khi vượt ranh giới.
- Yêu cầu không thể máy hoá, không có ngưỡng rõ ràng, phụ thuộc phán đoán theo ngữ cảnh đều đi vào `preferences`.

Nguyên tắc:

```text
Thà bỏ sót mà lọt vào structured, để hạ xuống sở thích mềm;
Tuyệt đối không lọt nhầm vào structured, tạo ra sự thật quy tắc sai cho từng chương.
```

Chi phí của việc bỏ sót là sở thích phong cách bị nhạt đi; chi phí của việc trích sai là mỗi chương sinh ra một sự thật quy tắc sai.

## 8. Thất bại và hạ cấp

Chuẩn hoá là đường tăng cường, không phải điều kiện tiên quyết của việc viết sách. Model hiểu hỏng tuyệt đối không được chặn việc viết sách.

- **Hạ cấp theo nguồn**: nếu một nguồn chuẩn hoá thất bại (mạng / model / JSON không hợp lệ / lỗi kiểm tra schema), nguồn đó hạ cấp thành raw preferences, không sinh `structured`; các nguồn thành công khác vẫn đóng góp `structured` như thường.
- **Tự lành do context điều khiển**: lỗi yêu cầu có thể thử lại, lỗi định dạng/Schema ở chế độ prompt và lỗi kiểm tra nghiệp vụ tiếp tục tự lành cho tới khi thành công hoặc `context` kết thúc; không đặt số lần cố định. Vi phạm hợp đồng gốc, từ chối trả lời, cắt ngắn, kết thúc sai và lỗi yêu cầu không thể thử lại được lộ ra ngay lập tức và hạ cấp theo nguồn.
- **Lỗi kỹ thuật vào log**: các lỗi kỹ thuật về JSON / schema / mạng được ghi vào log, không vào `working_memory.user_rules`, không làm đầu vào sáng tác.
- **Đánh dấu snapshot**: khi bất kỳ nguồn nào bị hạ cấp, snapshot đặt `status=degraded`.
- **Cứ ghi được xuống đĩa là tiếp tục**: miễn là `meta/user_rules.json` ghi được, việc sáng tác chính phải tiếp tục.
- **Chỉ hủy khi ghi xuống đĩa thất bại**: chỉ dừng khi snapshot không ghi được xuống đĩa, vì khi đó các lần chạy sau không có nguồn sự thật ổn định.

Hợp đồng của `AddRuntimeRule` (lúc chạy): khi normalizer thất bại thì lưu snapshot degraded,
không chèn các lỗi chuẩn hoá kiểu JSON/schema/mạng vào quy trình sáng tác; chỉ lỗi ghi xuống đĩa mới trả về error.

## 9. Quy tắc mặc định của hệ thống

`System defaults` là baseline máy học nhúng sẵn trong code, không phải tệp rules của người dùng, cũng không dùng YAML.

Nó không đi qua chuẩn hoá bằng LLM — đã ở dạng có cấu trúc, đi thẳng vào phép gộp tất định bằng Go ở §10 với vai trò nguồn có ưu tiên thấp nhất. Nhờ vậy quy tắc mặc định không có vấn đề LLM thất bại, trôi lệch hay chi phí.

Các quy tắc máy học mặc định của hệ thống trước đây tạm nằm ở `assets/rules/default.md` (chi tiết hiện thực cũ, không phải YAML người dùng cần tương thích); khi triển khai thiết kế này đã được chuyển vào code dưới dạng `rules.SystemDefaults()`, đường phân tích YAML đã bị xoá (xem §2).

Khi di chuyển, giữ lại chú thích cần thiết giải thích nguồn gốc của ngưỡng, ví dụ một số ngưỡng từ mệt mỏi đến từ thực nghiệm sản phẩm của các lần chạy dài. Đây không phải để tương thích YAML cũ, mà để người bảo trì tương lai biết ngưỡng mặc định tồn tại vì lý do gì và khi nào nên điều chỉnh.

## 10. Quy tắc gộp

Thứ tự gộp theo nguyên tắc "càng cụ thể càng được ưu tiên":

```text
System defaults
→ Global rules 编译结果
→ Project rules 编译结果
→ Startup prompt 编译结果
→ Runtime user update
```

Nguồn ưu tiên cao ghi đè nguồn ưu tiên thấp.

Việc gộp do Go thực hiện một cách tất định: LLM chỉ chuẩn hoá ngôn ngữ tự nhiên của một nguồn thành `structured`/`preferences` ứng viên, Go làm ghi đè theo trường và nối văn bản theo thứ tự nêu trên, không giao việc phán xử thứ tự ưu tiên cho LLM.

- `structured`: ghi đè theo từng trường, trường cùng tên của nguồn sau ghi đè trường của nguồn trước.
- `preferences`: không ghi đè lẫn nhau, được nối thành văn bản dễ đọc theo thứ tự ưu tiên (nguồn ưu tiên cao đứng sau), để LLM thấy được thứ tự nguồn.

Giới hạn đã biết: `preferences` được sắp theo ưu tiên, nhưng Go không hoà giải xung đột. Trong lúc chạy dài, nếu người dùng lần lượt đưa ra các sở thích mềm mâu thuẫn (trước "bình tĩnh tiết chế" sau "nói nhiều"), cả hai đều ở lại trong văn bản và LLM tự cân bằng theo thứ tự cùng ngữ cảnh; nếu cần ghi đè cứng xác định thì hãy biểu đạt thành trường `structured` có thể máy hoá.

## 11. Điểm vào ghi xuống đĩa

Chuẩn hoá, gộp và ghi xuống đĩa là cùng một bộ logic, nhưng có hai bên gọi, phải phân biệt rõ, nếu không sẽ trộn phần chuẩn bị khởi động vào ngữ cảnh sáng tác chính:

- **Mở sách / làm mới (phía khởi động, tất định)**: Host / quy trình khởi động gọi trực tiếp bộ logic này để sinh snapshot ban đầu, không đi vào vòng lặp sáng tác chính. Đây là tác vụ chuẩn bị khởi động có tính tất định.
- **Cập nhật lúc chạy (hành động phán quyết can thiệp)**: hành động `rules` mà Arbiter phân loại được Host gọi trực tiếp `userrules.Service.AddRuntimeRule`, tái sử dụng cùng bộ logic kiểm tra / gộp / ghi xuống đĩa, đưa quy tắc mới không có điểm xuất phát về tiến độ vào snapshot dưới dạng `Runtime user update`.

(Về mặt hiện thực, nên thu bộ logic này về một dịch vụ nội bộ để hai bên gọi cùng dùng; cách đặt tên cụ thể để lúc triển khai.)

Dù bên gọi nào, kết quả cuối cùng cũng ghi vào cùng một `meta/user_rules.json`. Logic ghi xuống đĩa chỉ làm ba việc:

1. Kiểm tra các trường có cấu trúc.
2. Gộp theo thứ tự ưu tiên ở §10 vào snapshot hiện tại của cuốn sách.
3. Trả về toàn bộ sự thật quy tắc sau khi lưu.

Không làm:

- Không phái subagent.
- Không sửa dàn ý.
- Không lặng lẽ nuốt trường không hợp lệ (ghi log và hạ cấp, xem §8).
- Không coi văn bản gốc là prompt cuối cùng để chèn thẳng.

Ví dụ cập nhật lúc chạy: người dùng nói "từ giờ cứ thế" (không có điểm xuất phát về tiến độ) → Arbiter phán quyết thành hành động `rules` → Host chuẩn hoá mục đó qua `AddRuntimeRule` → gộp vào snapshot với ưu tiên cao nhất dưới dạng `Runtime user update` → phản chiếu qua luồng sự kiện.

## 12. Phản chiếu

Mỗi lần sinh hoặc cập nhật snapshot `user_rules`, kết quả chuẩn hoá đều phải được phản chiếu lại cho người dùng:

```text
已生成本书规则快照：
- 机械规则：每章 1200-1600 字；禁用短语“某种程度上”
- 风格偏好：主角冷静克制；少解释，多用行动和对话推进
- 未提升为机械规则：少用比喻（无明确阈值，按风格偏好处理）
```

- Khởi động / làm mới: dùng lại năng lực log quy tắc khởi động sẵn có để in snapshot, không thêm cơ chế mới; trong kịch bản đồng sáng tác có thể gộp phần phản chiếu vào bước xác nhận đồng sáng tác.
- Lúc chạy: sau khi `AddRuntimeRule` thành công thì phản chiếu qua luồng sự kiện ("quy tắc viết đã được cập nhật và lưu bền vững").
- Khi hạ cấp: với `status=degraded`, phần phản chiếu nói rõ nguồn nào không phân giải được, hiện đang chạy bằng raw preferences, và có thể sinh lại snapshot.

Phản chiếu không phải cổng phê duyệt lần hai; tác dụng của nó là để người dùng biết hệ thống đã hiểu thành gì, và sau khi phát hiện sai thì có thể sinh lại snapshot.

## 13. Cách agent tiêu thụ

Mọi agent chỉ nhìn:

```json
working_memory.user_rules
```

Phân chia trách nhiệm:

- Architect: điều chỉnh mật độ cốt truyện mỗi chương và số chương tách ra theo ý định số chữ trong `preferences`.
- Writer: viết theo quy tắc cứng trong `structured`, điều chỉnh phong cách theo `preferences`.
- Editor: thẩm duyệt theo cùng bộ quy tắc đó.
- `commit_chapter`: dùng `structured` để kiểm tra bằng máy và trả về violations.

Writer không hiểu lại prompt khởi động gốc, cũng không đọc tệp rules gốc.

## 14. Phân loại can thiệp: ba hướng đi

Can thiệp lúc chạy được chia thành ba loại theo "cần đổi cái gì":

- **Cách viết** (bút pháp / phong cách / chất lượng: số chữ, từ ngữ, cụm cấm, cấu trúc câu, tỉ lệ đối thoại, định dạng tiêu đề, v.v.) → hành động `rules` của Arbiter, được chuẩn hoá và gộp vào `meta/user_rules.json`. Ví dụ: "mỗi chương 1500 chữ", "tiêu đề chỉ dùng tiếng Việt", "nhân vật chính nhìn chung bình tĩnh tiết chế", "tỉ lệ đối thoại cao hơn một chút".
- **Viết cái gì** (cốt truyện / cấu trúc / hướng đi của nhân vật / độ dài) → architect, rơi vào compass / outline / hồ sơ nhân vật. Ví dụ: "tập này viết nhiều tuyến chiến đấu hơn", "từ chương 30 giọng văn nhân vật chính chuyển sang lạnh", "tăng lên 40 chương".
- **Sửa phần đã viết** (viết lại / hiệu đính chương đã chỉ định) → editor, vào hàng đợi PendingRewrites.

Tiêu chí phán đoán: **"Cách viết" → rules; "Viết cái gì" → architect; "Sửa phần đã viết" → editor**.

## 15. Các bước triển khai

1. Thêm store `meta/user_rules.json`.
2. Thêm một pass chuẩn hoá LLM độc lập (theo từng nguồn), dùng ràng buộc schema để sinh `structured/preferences/sources/uncertain` ứng viên.
3. Thêm phép gộp tất định phía Go: ghi đè theo trường và nối văn bản cho từng nguồn theo thứ tự ưu tiên, sinh ra snapshot.
4. Thu gộp chuẩn hoá / gộp / ghi xuống đĩa thành một bộ logic, hai bên gọi dùng chung: phía khởi động gọi trực tiếp để sinh snapshot ban đầu; lúc chạy thì hành động `rules` từ phán quyết can thiệp tái sử dụng qua `AddRuntimeRule`. Khi thất bại thì xử lý theo §8: nguồn hạ cấp thành raw preferences, snapshot `status=degraded`, việc sáng tác chính vẫn tiếp tục.
5. Chuyển các quy tắc máy học mặc định của hệ thống trong `assets/rules/default.md` sang cấu trúc nhúng trong code hoặc JSON asset, giữ lại chú thích nguồn gốc ngưỡng; xoá đường phân tích YAML cho rules của người dùng, không làm lớp tương thích.
6. Sau khi đọc tệp rules thì không còn chèn thẳng nội dung vào prompt như trước, mà chuẩn hoá rồi gộp vào snapshot `user_rules`.
7. `novel_context` chỉ chèn `working_memory.user_rules` từ `meta/user_rules.json`.
8. `commit_chapter` dùng chính `user_rules.structured` đó để kiểm tra.
10. Phân loại can thiệp (do Arbiter đảm nhiệm, arbiter-intervention.md) rõ ràng chia ba hướng theo "cần đổi cái gì": yêu cầu dài hạn về phong cách / chất lượng viết đi qua hành động `rules` và rơi vào snapshot; cốt truyện / cấu trúc / nhân vật / độ dài đi qua architect; làm lại chương đã viết đi qua editor (chi tiết ở §14).

## 16. Tiêu chí nghiệm thu

- Người dùng viết trong prompt khởi động "mỗi chương 1200-1600 chữ", `novel_context` của chương đầu tiên do Writer nhận phải thấy được nguyên văn ý định đó trong `preferences`.
- Tệp rules chỉ viết bằng ngôn ngữ tự nhiên vẫn được chuẩn hoá vào cùng một `user_rules` khi sinh snapshot.
- Tệp rules không cần và không hỗ trợ YAML; toàn bộ được chuẩn hoá theo quy tắc ngôn ngữ tự nhiên.
- Lúc chạy không còn đọc tệp rules; chỉ đọc `meta/user_rules.json`.
- Quy tắc máy học mặc định không còn đến từ tệp rules YAML, và rules của người dùng cũng không có lớp tương thích YAML.
- Chuẩn hoá không dùng regex / từ khoá hardcode; việc hiểu ngôn ngữ tự nhiên do LLM thực hiện.
- Quy tắc mơ hồ không bị nâng thành trường `structured` mức error.
- Quy tắc mặc định của hệ thống không đi qua LLM, đi thẳng vào phép gộp của Go.
- Thứ tự ưu tiên nguồn và ghi đè trường do Go thực hiện tất định, cùng đầu vào cho cùng một snapshot.
- Lúc chạy, người dùng nói "từ giờ cứ thế", sau khi hành động rules của Arbiter gộp vào snapshot thì `novel_context` của các chương sau phải thấy được bản cập nhật.
- Chuẩn hoá thất bại không chặn việc viết sách: nguồn thất bại hạ cấp thành raw preferences, snapshot `status=degraded`, việc sáng tác chính vẫn tiếp tục; chỉ khi snapshot không ghi được xuống đĩa mới dừng.
- Chuẩn hoá thất bại trả về `status=degraded`, không đẩy lỗi kỹ thuật lên làm bẩn luồng chính.
- Sau khi sinh hoặc cập nhật snapshot sẽ phản chiếu `structured` / `preferences` / các mục chưa nâng; khi hạ cấp thì phần phản chiếu nói rõ nguồn bị hạ cấp.
- Mở cuốn sách mới không thừa kế `user_rules` của cuốn sách trước.
- Trường có cấu trúc không hợp lệ không bị bỏ qua im lặng: ghi log và hạ cấp nguồn đó, không chặn luồng chính.

## 17. Những việc dứt khoát không làm (đã đánh giá là không cần, không phải cắt theo giai đoạn)

Các năng lực dưới đây không sinh lợi ích gì với nhu cầu hiện tại, không đưa vào thiết kế, để tránh thiết kế quá mức:

- Ngữ nghĩa xoá / huỷ ở mức trường như `clear_fields`.
- Tự động làm mới khi phát hiện tệp rules thay đổi (sửa tệp thì chủ động sinh lại snapshot là được).
- Mốc thời gian / hoà giải ghi đè cho `preferences` (cần ghi đè cứng thì dùng `structured`).
- Lưu mảng `diagnostics` trong snapshot (lỗi kỹ thuật vào log là đủ, snapshot chỉ giữ `status`).
- Sinh mô tả trường schema tự động từ kiểu Go (chỉ cần duy trì thủ công một bản mô tả ngắn).

Nguyên tắc thiết kế không đổi: LLM chịu trách nhiệm hiểu ngôn ngữ tự nhiên, Go chịu trách nhiệm gộp tất định, kiểm tra hợp lệ, ghi xuống đĩa và kiểm tra.
