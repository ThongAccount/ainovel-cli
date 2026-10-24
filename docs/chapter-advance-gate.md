# Chapter Advance Gate

> Trạng thái: đã hiện thực  
> Ngày: 2026-07-14  
> Giải quyết: duyệt từng chương, tạm dừng an toàn sau can thiệp, giấy phép chương chính xác trong khôi phục sau sập

## 1. Tại sao cần có nó

Rủi ro cốt lõi của việc tự động sáng tác truyện dài không phải là tốn thêm một lượt gọi, mà là hệ thống vẫn ghi chương mới trong lúc người dùng đang thẩm duyệt, đồng thời gấp các tóm tắt, trạng thái nhân vật và phản hồi đại cương dựa trên cốt truyện cũ vào nguồn sự thật về sau. Xoá chương bị ghi thừa không tự động huỷ các trạng thái dẫn xuất đó, và người dùng sẽ mất niềm tin vào quá trình sáng tác.

Dự án vẫn lấy "đưa ra mục tiêu rồi tự hoàn thành liên tục" làm định vị mặc định, nên không biến việc xác nhận từng chương thành mặc định toàn cục. Hệ thống cung cấp hai chính sách rõ ràng:

- `auto`: chế độ mặc định, tiến triển tự chủ liên tục;
- `review`: chế độ duyệt từng chương do người dùng chủ động chọn, mỗi chương mới tiến về phía trước đều cần một giấy phép chính xác.

Đây không phải là trao lại quy trình cho Coordinator LLM. Khi nào cần người dùng xác nhận là chính sách của người dùng; bước tiếp theo của quy trình tất định vẫn do Route suy ra; còn việc có cần dừng lại một lần để người dùng duyệt kết quả của một lần can thiệp hay không thì mới là phán đoán ngữ nghĩa của Arbiter.

## 2. Phân chia ranh giới

| Vấn đề | Thuộc về | Lý do |
|---|---|---|
| Hiện tại có phải chế độ duyệt từng chương không | RunMeta / Host | Ý định chạy lâu dài của người dùng |
| Chương nào đã được cấp giấy phép | RunMeta / Gate | Sự thật cơ học có thể kiểm chứng, có thể khôi phục |
| Chạy Worker nào tiếp theo | `flow.Route` | Suy ra bằng hàm thuần từ sự thật sáng tác |
| Lệnh có bắt đầu một chương mới tiến về phía trước không | `flow.StartsForwardChapter` | Phán đoán cơ học có kiểu |
| "Sửa xong cho tôi xem" có cần tạm dừng không | Arbiter | Phán đoán ngữ nghĩa ngôn ngữ tự nhiên |
| Tạm dừng kích hoạt khi nào | `ChapterAdvanceGate` | Thực thi tất định cho ý định một lần |
| Ngân sách có cho phép tiếp tục không | `BudgetSentinel` | Chính sách độc lập của Host |

`AdvanceMode`, giấy phép chương và hold một lần không đi vào bảng quyết định của Route, cũng không cho phép model sửa đổi. Máy trạng thái sáng tác của Route và chính sách duyệt từng chương giữ quan hệ trực giao.

## 3. Mô hình trạng thái tối giản

Trong `meta/run.json` chỉ thêm ba mục ý định chạy:

```go
type RunMeta struct {
	AdvanceMode          ChapterAdvanceMode `json:"advance_mode"`
	AdvancePermitChapter int                `json:"advance_permit_chapter,omitempty"`
	AdvanceHold          *AdvanceHold       `json:"advance_hold,omitempty"`
}

const (
	ChapterAdvanceAuto   ChapterAdvanceMode = "auto"
	ChapterAdvanceReview ChapterAdvanceMode = "review"
)

const (
	AdvanceHoldAtBoundary           AdvanceHoldAfter = "boundary"
	AdvanceHoldAfterRewritesDrained AdvanceHoldAfter = "rewrites_drained"
	AdvanceHoldAtChapter            AdvanceHoldAfter = "chapter"
)

type AdvanceHold struct {
	After         AdvanceHoldAfter `json:"after"`
	TargetChapter int              `json:"target_chapter,omitempty"`
	Reason        string           `json:"reason"`
}
```

Không có PolicyEngine tổng quát, mảng điều kiện, hàng đợi giấy phép, thời hạn hết hạn hay phiên bản chính sách. Việc tiến chương chỉ giữ một chế độ bền vững, một giấy phép chính xác và một hold một lần có kiểu.

### 3.1 Bất biến

1. `AdvanceMode` chỉ có thể là `auto` hoặc `review`; giá trị lạ trả về `UnsupportedAdvanceModeError`.
2. Chế độ lạ không được khởi chạy Host, cũng không được ghi đè RunMeta.
3. Dưới `auto`, giấy phép phải bằng `0`.
4. Dưới `review`, giấy phép chỉ có thể là `0` hoặc một số chương nguyên dương.
5. Cấp quyền lặp lại cho cùng mục tiêu là idempotent; mục tiêu khác không được ghi đè giấy phép đang trong luồng.
6. Giấy phép chỉ ràng buộc việc "bắt đầu một chương mới tiến về phía trước còn chưa hoàn thành"; quy hoạch, thẩm duyệt, làm lại, gọt giũa và khôi phục commit không bị chặn.
7. Giấy phép gắn với số chương, không gắn với một lần chạy tiến trình hay một lần gọi Worker cụ thể.
8. Chỉ khi chương mục tiêu đã vào `CompletedChapters`, `PendingCommit` tương ứng đã được dọn sạch và tồn tại checkpoint `commit` của chương đó, giấy phép mới được coi là đã tiêu thụ ổn định.
9. Chương mục tiêu đã hoàn thành nhưng thiếu checkpoint commit là hư hỏng trạng thái: báo lỗi tường minh và tạm dừng, không đoán bừa cách sửa.
10. Giấy phép chưa tiêu thụ phải bằng `Progress.NextChapter()`. `PendingRewrites` không làm thay đổi `NextChapter()`, nên việc làm lại và giấy phép tiến về phía trước đang trong luồng có thể cùng tồn tại một cách cơ học.
11. `AdvanceHold` chỉ dùng được `boundary`, `rewrites_drained` hoặc `chapter`, và phải mang lý do không rỗng; `chapter` phải mang chương mục tiêu là số dương, các điều kiện khác không được mang theo.
12. hold và giấy phép dùng compare-and-clear; khi trạng thái bị hành động mới thay thế thì không được xoá nhầm.
13. Hold theo chương mục tiêu, ở chế độ `review`, tạo thành một giấy uỷ quyền khoảng một lần; sau khi tạm dừng, chính sách duyệt từng chương vốn có vẫn giữ nguyên.

## 4. Store API

RunMetaStore cung cấp các thao tác nguyên tử hẹp và có kiểu:

```go
SetAdvanceMode(mode domain.ChapterAdvanceMode) error
GrantAdvancePermit(chapter int) error
ClearAdvancePermit(chapter int) error
SetAdvanceHold(hold domain.AdvanceHold) error
ClearAdvanceHold(expected domain.AdvanceHold) error
```

- Khi chuyển lại `auto`, giấy phép chương được xoá trong cùng khoá ghi, nhưng hold sinh ra từ một lần can thiệp khác của người dùng thì không bị xoá;
- Cấp quyền chỉ hợp lệ dưới `review`;
- Thao tác xoá chỉ tiêu thụ đúng mục tiêu mà bên gọi vừa đọc;
- Khi khởi tạo RunMeta, chế độ mặc định là `auto`, đồng thời giữ lại chế độ, giấy phép và hold đã ghi xuống đĩa.

Dự án hiện không có dữ liệu lịch sử cần di chuyển, nên phần hiện thực không gồm đọc trường cũ, ghi kép hay nhánh hạ cấp.

## 5. Ngữ nghĩa hàm thuần

### 5.1 Nhận diện chương mới tiến về phía trước

```go
func StartsForwardChapter(
	inst *Instruction,
	progress *domain.Progress,
	pending *domain.PendingCommit,
) bool
```

Chỉ trả về true khi tất cả các điều kiện sau cùng đúng:

- Worker là `writer`;
- phase là `writing`;
- không có `PendingCommit`;
- không có hàng đợi làm lại;
- không có `InProgressChapter`;
- chương mục tiêu bằng `NextChapter()`.

Phán đoán chỉ đọc các trường có kiểu, không phân tích văn bản Task hay Reason.

### 5.2 Hold một lần

`ResolveAdvanceHold` trả về dựa trên hold và Progress:

- `keep`: điều kiện chưa được thoả mãn;
- `consume`: ở trạng thái hoàn thành tác phẩm chỉ cần dọn ý định;
- `consume-and-stop`: dọn ý định và tạm dừng.

`boundary` kích hoạt tại ranh giới Worker hiện tại; `rewrites_drained` kích hoạt sau khi hàng đợi làm lại cạn; `chapter` kích hoạt sau khi chương mục tiêu vào danh sách hoàn thành, `PendingCommit` được dọn sạch và có checkpoint commit. Điều kiện lạ và sự thật thiếu đều báo lỗi ngay.

## 6. ChapterAdvanceGate

Gate là thành phần chính sách tiến triển sáng tác duy nhất ngoài ngân sách, với đúng hai trách nhiệm:

1. Phân giải và tiêu thụ hold một lần tại ranh giới vòng lặp;
2. Kiểm tra giấy phép duyệt từng chương trước khi phát lệnh cho writer, và đối soát tại ranh giới xem giấy phép đã được tiêu thụ ổn định hay chưa.

Thứ tự của Engine là:

```text
Gửi các can thiệp đang chờ xử lý
→ Kiểm tra ranh giới Gate
→ Route / lấy lệnh phân công từ Arbiter
→ precheck
→ Kiểm tra giấy phép phát lệnh của Gate
→ Worker
→ Kiểm tra ranh giới Budget
→ Kiểm tra ranh giới Gate
→ Vòng tiếp theo
```

Khi `auto && hold == nil`, kiểm tra ranh giới đọc RunMeta rồi trả về ngay, không đọc Progress, PendingCommit hay checkpoint.

### 6.1 hold + dispatch

Arbiter có thể phán quyết "viết lại chương 3, sửa xong cho tôi xem" thành:

```json
{
  "hold": {
    "after": "rewrites_drained",
    "reason": "重写完成后等待用户验收"
  },
  "dispatch": {
    "agent": "editor",
    "task": "复核第 3 章并按结果建立返工队列"
  }
}
```

Nhóm hành động này phải thực hiện lệnh phân công đi kèm trước, để Editor dựng nên sự thật làm lại, rồi Gate mới phán đoát hàng đợi đã cạn hay chưa. Engine gắn cờ "lần phân công này hoãn Gate" vào chính lệnh trong bộ nhớ, và xoá cờ đó khi lấy lệnh đi; lệnh phân công thường của Arbiter không được vượt qua Gate.

### 6.2 permit và làm lại

`reopen` sau khi hoàn thành tác phẩm chỉ xảy ra ở `complete`, còn `/next` chỉ xảy ra ở `writing`, hai cái loại trừ nhau một cách cơ học. `PendingRewrites` đã có sẵn trong giai đoạn viết không làm thay đổi chương đã hoàn thành lớn nhất, nên giấy phép vẫn khớp với cùng một `NextChapter()`; Worker làm lại vẫn có thể chạy, nhưng không tiêu thụ giấy phép tiến về phía trước.

## 7. Khôi phục sau sập

Việc commit chương là một saga nhiều bước, nên giấy phép không thể được biểu diễn bằng giá trị boolean kiểu "lần run sau được viết một chương". Khi khôi phục, Gate đối soát theo ba loại sự thật:

| Cửa sổ sự thật | Hành vi của Gate |
|---|---|
| Chương mục tiêu chưa hoàn thành, không có PendingCommit | Giữ giấy phép, cho phép bắt đầu/khôi phục chương đó |
| PendingCommit thuộc về chương mục tiêu | Giữ giấy phép, để quá trình khôi phục commit hoàn tất |
| Chương mục tiêu đã hoàn thành, PendingCommit đã dọn sạch, có checkpoint commit | Tiêu thụ giấy phép |
| Chương mục tiêu đã hoàn thành nhưng thiếu checkpoint | Báo lỗi và tạm dừng |
| Giấy phép trỏ tới chương chưa hoàn thành khác NextChapter | Báo lỗi và tạm dừng |

Vì vậy, dù tiến trình sập ở bất kỳ cửa sổ nào trong số các cửa sổ ghi bản nháp, ghi trạng thái, đánh dấu tiến độ hay ghi tín hiệu, cùng một giấy phép sẽ không bị dùng nhầm cho chương kế tiếp.

## 8. Arbiter

Schema của can thiệp dùng `AdvanceHoldOp`:

```go
type AdvanceHoldOp struct {
	Cancel        bool                    `json:"cancel,omitempty"`
	After         domain.AdvanceHoldAfter `json:"after,omitempty"`
	TargetChapter int                     `json:"target_chapter,omitempty"`
	Reason        string                  `json:"reason,omitempty"`
}
```

Quy tắc:

- Cụm rõ ràng "dừng lại một chút" dùng `boundary`;
- Dưới `auto`, "sửa chương đã viết, sửa xong để tôi duyệt" dùng `rewrites_drained`;
- "viết tới chương N" dùng `chapter`, phân biệt rõ với việc điều chỉnh độ dài "cả cuốn sách có N chương";
- `review` đã dừng ở từng chương, không tạo lại một hold cùng nghĩa;
- Hold theo chương mục tiêu dưới `review` là giấy uỷ quyền hàng loạt một lần mà người dùng đã ký rõ ràng;
- "tiếp tục" có thể huỷ hold hiện có, nhưng không thể cấp giấy phép chương;
- Đổi chế độ chỉ dùng `/review on|off`, còn cho phép đi tiếp chỉ dùng `/next`.

Engine gọi trực tiếp RunMetaStore để áp dụng hành động có cấu trúc, không ngụy trang nó thành LLM Tool.

## 9. Giao diện người dùng

### 9.1 `/review on|off`

- `/review on`: lưu ngay chính sách duyệt từng chương; nếu Worker đang chạy, sẽ dừng trước chương mới tiến về phía trước kế tiếp sau khi công việc hiện tại hoàn tất;
- `/review off`: chuyển lại tiến triển tự động và xoá giấy phép theo cách nguyên tử; không tự khởi chạy lại Engine đang tạm dừng, sự kiện sẽ nhắc rõ ràng người dùng nhập lệnh tiếp tục.

### 9.2 `/next`

Chỉ dùng được khi tất cả các điều kiện sau cùng đúng:

- Engine chưa chạy;
- không phải giai đoạn đồng sáng tác;
- chế độ là `review`;
- không có hold đang chờ xử lý;
- ngân sách cho phép;
- phase là `writing`.

Lệnh này cấp giấy phép chính xác cho `NextChapter()` và khởi chạy Engine. Thông báo sẽ nói rõ: sau khi chương đó được commit, các bước thẩm duyệt cần thiết và bảo trì cấu trúc cung/tập vẫn sẽ hoàn tất, rồi lại chờ phép đi tiếp.

### 9.3 Hiển thị trạng thái

`UISnapshot` là nguồn sự thật duy nhất của TUI, gồm:

- `AdvanceMode`;
- `AdvancePermitChapter`;
- `HasAdvanceHold`;
- `AdvanceHoldReason`.

Thanh bên hiển thị trạng thái tự động / duyệt từng chương và chương đã được cho phép đi tiếp; khi đang chờ, ô nhập gợi ý "nhập ý kiến sửa đổi, hoặc `/next` để cho phép chương kế tiếp". Kind của thông báo là `advance_gate`.

## 10. Kiểm chứng

Kiểm thử phủ:

- chuyển trạng thái nguyên tử và compare-and-clear của chế độ, giấy phép, hold trong RunMeta;
- chế độ lạ thất bại tường minh mà không ghi đè RunMeta;
- nhận diện bằng hàm thuần chương mới tiến về phía trước so với làm lại/khôi phục;
- ngữ nghĩa boundary, làm lại chưa cạn, làm lại đã cạn và hoàn thành tác phẩm của hold;
- chặn khi không có giấy phép, cho phép đi tiếp với giấy phép chính xác, báo lỗi khi giấy phép sai chương;
- giữ giấy phép trong thời gian PendingCommit, tiêu thụ sau khi commit ổn định;
- tạm dừng khi dấu hoàn thành xung đột với checkpoint;
- giấy phép xen kẽ với PendingRewrites không báo nhầm;
- kiểm thử đầu-cuối ở phía Engine chứng minh một giấy phép chỉ ổn định đúng một chương mới;
- khi Gate đã đánh dấu tạm dừng nhưng goroutine Engine cũ còn đang thoát, `/next` từ chối rõ ràng việc vào lại, thử lại sau sẽ khôi phục idempotent theo giấy phép của cùng chương đó;
- hồi quy cho hold-only, hold+dispatch và tranh chấp khi thoát.

## 11. Những việc dứt khoát không làm

- không để model quyết định chế độ chạy hay cấp giấy phép;
- không sửa Route để chiều theo chính sách xác nhận của người dùng;
- không biến làm lại, quy hoạch, thẩm duyệt và bảo trì cấu trúc thành xác nhận từng bước;
- không thêm PolicyEngine tổng quát, danh sách StopCondition hay DSL chính sách;
- không cung cấp cấp quyền trước nhiều chương hay hàng đợi giấy phép;
- không giữ mô hình tạm dừng cũ, trường tương thích, DTO di chuyển hay chuỗi ghi kép;
- không hạ cấp âm thầm cho một chế độ tương lai chưa biết.

Nếu sau này xuất hiện nhu cầu mới về ranh giới tự trị đã được kiểm chứng lặp lại, hãy mở rộng chế độ dựa trên bằng chứng; chi phí đổi ý thấp hiện tại chính là khả năng tương thích cho tương lai.
