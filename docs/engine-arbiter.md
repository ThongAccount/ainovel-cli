# Tiến hoá mặt phẳng điều khiển: Engine + Arbiter (bỏ vòng lặp dài của Coordinator)

> Trạng thái (2026-07-14 v6): **đã hoàn tất hiện thực mã nguồn** — Engine/Arbiter đã được triển khai, Coordinator cùng toàn bộ phần phụ đã bị xoá (danh sách ở §10); xác minh đầu-cuối: viết trọn một cuốn sách, phán quyết khi thất bại, phán quyết khi bế tắc, nghiệm thu làm lại (thứ tự hold+editor), hold biên dừng ngay, bảo toàn can thiệp trong tranh chấp lúc thoát và cơ chế một giấy phép cho một chương. Toàn bộ các mục chặn của ba vòng phản biện bên ngoài đều đã được xử lý (gồm vòng lặp sự thật của feedback, bảo vệ PendingSteer khi sập, tranh chấp vòng đời).
> **Đã chuyển tài liệu (2026-07-12)**: phần thân của architecture.md đã được viết lại toàn bộ theo kiến trúc Engine+Arbiter hiện hành (kèm chiến lược xác minh / mục lục / kỷ luật mới); các tự sự kiến trúc cũ trong README, context-management, evaluation-system, observability, user-rules-runtime đã được dọn sạch (chỉ giữ lại phần đối chiếu lịch sử được ghi nhãn). Cấu hình Coordinator và đường tương thích phiên làm việc đều đã bị xoá; Arbiter hiện cố ý dùng chung model Default, không mở cấu hình vai trò riêng.
> **Làm rõ ngữ nghĩa thiết kế (vòng phản biện thứ tư/năm)**: ① điểm tiêu thụ feedback của writer **chính là** thao tác cấu trúc kế tiếp (expand_arc/append_volume/update_compass, đọc qua novel_context rồi xoá) — đó là "đề xuất cho đại cương phía sau" (nguyên văn commit schema), không phải tín hiệu điều phối tức thời; lệch nghiêm trọng giữa cung đi qua thẩm duyệt của editor và kênh can thiệp của người dùng. Sách không phân tầng không có thao tác cấu trúc, **commit không ghi feedback của nó xuống đĩa** (tránh tạo sự thật rác vĩnh viễn không ai tiêu thụ; giá trị trả về được phản chiếu lại để chẩn đoán). ② rule_violations đã khép kín: commit ghi xuống đĩa theo hai đường (**siêu dữ liệu chất lượng theo nguyên tắc best-effort**, không cùng mức nhất quán mạnh với việc commit chương — nếu sập đúng sau khi pending_commit bị xoá thì thiếu một bản ghi, điều này chấp nhận được) → novel_context(chapter=N) chèn vào → editor tiêu thụ theo ánh xạ §kiểm tra cơ học. ③ Bảo vệ PendingSteer khi sập là **lưu trữ best-effort một lượt trong luồng**: lần lưu trữ đầu tiên thất bại sẽ dừng phán quyết một cách tường minh; giai đoạn phán quyết, áp dụng hành động thất bại, thoát bình thường/Abort đều được bảo vệ; có hai cửa sổ được nói rõ là không bảo đảm — (a) sau khi lệnh phân công chuyển vào hàng đợi thực thi trong bộ nhớ (e.next) và trước khi worker khởi chạy, bị kill cứng tiến trình (cửa sổ mức mili giây, defer không chạy); (b) can thiệp đồng thời đang chờ interMu (chưa ghi vào ô trạng thái). Người dùng ở hiện trường có thể nhận ra, chi phí gửi lại chỉ tính bằng giây, nên không dựng cơ chế intent/FIFO bền vững cho việc này. ④ Phán quyết lúc khởi động thất bại không phải bế tắc (bài học từ sự cố thật 2026-07-12: tài khoản provider hết hạn khiến plan_start thất bại, mọi đường khôi phục đều không đi được): StartPrompt (sự thật đầu vào) được đổi thành ghi xuống đĩa **trước** khi phán quyết; khi plan_start chưa từng hoàn thành, engine dùng planStartFallback để phán quyết bù tại chỗ dựa trên đó — việc thử lại phán quyết đầu tiên không vi phạm nguyên tắc "khôi phục không làm lại phán quyết đã có"; nếu phán quyết bù thất bại thì dừng tường minh và phản chiếu lại, bản ghi kiểm toán của phán quyết thất bại mang trường error (DecisionRecord.Error).
> Tài liệu này được giữ lại làm bản ghi quyết định thiết kế; kiến trúc hiện hành xem ở mục kiến trúc của README và docs/engine-rfc.md. Liên quan: docs/voice-layer.md (đã triển khai).

## 1. Động cơ: giả định lỗi thời bị vây quanh bởi đống vá

Giả định khai sinh của dự án — "một Prompt, một vòng LLM thường trực dài điều khiển cả cuốn sách" — đã lỗi thời. Sau đợt refactor Hybrid tháng 4, quyền quyết định thực tế nằm trong tay `flow.Route`, và 90% các lệnh gọi của Coordinator trong vòng lặp chính chỉ làm **chuyển tiếp nguyên vẹn**. Hệ sinh thái vá lỗi phải trả giá để "duy trì một phiên LLM không nên dừng":

1. Kỹ thuật văn bản động cho StopGuard + blockMessage
2. Giao thức lệnh lặp của Dispatcher ("lần thứ N")
3. Huấn luyện hành vi trong coordinator.md (trường hợp riêng khi khôi phục / lệnh loại truy vấn phải được phát trong cùng vòng / không được dùng việc dừng máy để bày tỏ thái độ)
4. completePhaseGate / writerExpandedChapterGate
5. MaxTurns=100_000
6. FlowBoundaryHook
7. Trường hợp riêng cho bất đồng về việc kết thúc

**Các hệ thống con mới bên trong dự án (import/simulation/cocreate/userrules) đều không đi qua Coordinator, mà đã dùng mô hình "Host trực tiếp điều phối + LLM đóng vai hàm"** — phương án này đưa luồng chính về thống nhất với chính mô hình đã được kiểm chứng đó.

## 2. Hình thức mục tiêu

```
Entry
  ↓
Host(giữ nguyên tên package; thêm EngineLoop bên trong, không chỉ đổi tên máy móc)
  ├─ đọc Store → flow.Route → chạy trực tiếp Worker
  ├─ ngữ cảnh ngữ nghĩa rõ ràng → gọi hàm Arbiter
  └─ chiếu sự kiện / ngân sách / điểm dừng / thông báo (giữ nguyên trách nhiệm hiện hành)
  ↓
Workers(architect / writer / editor, tự chủ bên trong, giữ chốt chặn checkpoint-delta)
  ↓
Tools → Store(nguồn sự thật duy nhất)
```

Trách nhiệm: **Route quản lý mọi bước kế tiếp có thể tra bảng; Arbiter quản lý các phán đoán ngữ nghĩa có ranh giới rõ; Worker quản lý sáng tác mở; Engine thực thi quyết định, không tham gia phán đoán văn chương; Observer/Diag chỉ quan sát.**

Tóm gọn trạng thái cuối: **một Engine tất định chạy tuần tự, ba Worker tự chủ, vài hàm Arbiter gọi khi cần, một tầng sự thật trên hệ thống tệp.**

### Đối xứng hai mặt phẳng (dự định ghi vào architecture.md như một quy tắc mới)

```
Mặt phẳng tất định:  flow.LoadState   → flow.Route     → Instruction   (kiểm thử spec liệt kê đầy đủ)
Mặt phẳng ngữ nghĩa:  arbiter.Collect* → arbiter.Decide* → XxxDecision   (ghi quyết định xuống đĩa + hồi quy eval)
              └── thu thập sự thật(IO) ──┘└── lõi quyết định (phát lại offline được) ──┘└── Engine thực thi ──┘
```

## 3. Các kịch bản của Arbiter (bộ cuối cùng, giữ ở mức tối thiểu)

| Kịch bản | Kích hoạt | Ghi chú |
|------|------|------|
| `plan_start` | khởi động cuốn sách mới | chọn nhà quy hoạch short/long + bổ sung nhu cầu quá ngắn |
| `intervention` | người dùng can thiệp | truy vấn / quy tắc dài hạn / điều chỉnh cấu trúc cốt truyện / làm lại phần đã viết / làm lại hoặc từ chối sau khi hoàn thành |
| `worker_failure` | Worker báo lỗi **và phân loại tất định không còn đường ra** | các lỗi mạng/tham số/thiếu tiền điều kiện do code tất định phân loại trước, không đưa sang Arbiter |
| `deadlock` | vòng trước vẫn sinh ra cùng lệnh định tuyến | xem §8, câu hỏi bắt buộc 5, về cách đếm và ngữ nghĩa kết thúc |
| `completion_dispute` | **dự phòng, chỉ thêm khi có bằng chứng** | phán đoán kết thúc ở cuối tập đã do Route giao architect (nhánh 10) đảm nhiệm; chỉ cần cho bất đồng giữa chừng kiểu "cấu trúc chưa tới biên nhưng câu chuyện nên khép lại", tỉ lệ xảy ra thật chưa biết, không dựng trước |

Tổng kết sau khi hoàn thành tác phẩm không phải phán quyết mà là tác vụ sinh nội dung — Engine trực tiếp giao editor hoặc một lượt gọi LLM thông thường để hoàn thành, không chiếm một kịch bản Arbiter.

## 4. Thiết kế Arbiter

### 4.1 Kiểu Decision theo từng kịch bản (bản sửa v3: từ bỏ cấu trúc đa năng)

```go
// kiểu con dùng chung, chống các kịch bản trôi lệch
type DispatchDecision struct {
    Instruction flow.Instruction
    Expect      DispatchExpect // xem §5
}

type PlanStartDecision struct {
    Planner string // architect_long | architect_short
    Task    string // gồm nhu cầu đã bổ sung
    Reason  string
}

type InterventionDecision struct {
    Answer   string
    Rules    string
    Hold     *AdvanceHoldOp
    Reopen   *ReopenOp
    Dispatch *DispatchDecision
    Reason   string
}

type FailureDecision struct {
    Action   string // retry | reroute | abort
    Dispatch *DispatchDecision
    Reason   string
}
```

Lịch sử tiến hoá: danh sách hành động (kiểm tra thứ tự thừa, mảng đa hình dễ sai) → cấu trúc phẳng đa năng (thứ tự phi hợp lệ không biểu đạt được, nhưng tổ hợp phi hợp lệ vẫn phải kiểm bằng ma trận kịch bản × hành động) → **kiểu theo từng kịch bản (hành động không khớp kịch bản không biểu đạt được, kiểm tra ma trận biến mất, schema của một kịch bản nhỏ hơn, LLM ổn định hơn, eval chấm được theo từng kịch bản)**. Validate thu hẹp thành kiểm tra sự thật theo từng kiểu (ràng buộc phase, v.v.).

### 4.2 API: mỗi kịch bản là một cặp hàm tường minh

```go
func CollectInterventionFacts(st *store.Store) InterventionFacts        // ranh giới IO, cùng kỷ luật với flow.LoadState
func DecideIntervention(ctx, model, facts, text) (InterventionDecision, error) // ngoài yêu cầu model do bộ thực thi thống nhất quản lý thì không có IO, phát lại offline được
// các kịch bản còn lại cùng dạng, mỗi kịch bản một cặp; Collect/Decide có hình dạng thống nhất, không dựng khung Question/Decision tổng quát
```

- **Đường thất bại**: bộ thực thi có cấu trúc thống nhất chọn JSON Schema gốc hoặc hợp đồng dạng prompt tuỳ năng lực của model; lỗi định dạng/Schema ở chế độ prompt và lỗi kiểm tra nghiệp vụ ở cả hai chế độ đều mang nguyên nhân chính xác để model tự sửa, vòng đời chỉ do `context` điều khiển. Vi phạm hợp đồng gốc, từ chối trả lời, cắt ngắn, kết thúc sai và lỗi yêu cầu không thể thử lại đều được trả về tường minh ngay; can thiệp không sinh ghi, khởi động báo lỗi tường minh, failure/deadlock thì tạm dừng thận trọng
- **Ký ức can thiệp**: decisions.jsonl kiêm nhiệm lịch sử can thiệp, `CollectInterventionFacts` đưa vào tóm tắt N phán quyết gần nhất
- **Model**: Arbiter dùng chung Default, không lộ role riêng; chỉ mở rộng hợp đồng cấu hình khi xuất hiện nhu cầu rõ ràng về năng lực hoặc chi phí

### 4.3 Kiểm toán (nhỏ mà ổn định; kiểm toán ≠ nguồn khôi phục)

```json
{"schema_version":1,"id":"...","kind":"intervention","checkpoint_seq":123,
 "input":"...","facts":{...},"decision":{...},"reason":"...","duration_ms":1200}
```

(token/chi phí không nằm trong bản ghi — model phán quyết được bọc qua usageTrackedModel, mức dùng đi chung vào UsageTracker/ngân sách, cùng một sổ sách với các Worker khác.)

- facts chỉ lưu sự thật có cấu trúc + tóm tắt + tham chiếu artifact/checkpoint, **không sao chép chính văn, không lưu trọn gói ngữ cảnh**; có giới hạn kích thước cho từng bản ghi, vượt giới hạn thì cắt ngắn và đánh dấu
- **input được giữ trong bản ghi** (cần thiết cho việc phát lại offline `Decide*(facts, input)` — bản kiểm toán không có input thì không thể hồi quy); khử nhạy cảm diễn ra ở **ranh giới xuất diag**, không diễn ra lúc ghi xuống đĩa
- Nhật ký kiểm toán không phải event sourcing, cũng không phải nguồn dữ liệu khôi phục

## 5. Giao thức commit trạng thái (vòng lặp Engine tuần tự)

```
đọc sự thật → Route / Arbiter sinh quyết định → đối chiếu tiền điều kiện → thực thi hành động
       → Worker chạy → tính lại hậu điều kiện của Route → vòng tiếp theo
```

- **Bất biến: trạng thái điều khiển chỉ đổi tuần tự tại ranh giới Engine.** Can thiệp có thể được tham vấn song song trong lúc Worker chạy (an toàn vì chỉ đọc, người dùng thấy phản chiếu Answer/Reason trong tích tắc), nhưng **hành động đổi trạng thái điều khiển (hold/reopen/dispatch) phải vào hàng đợi của Engine, đối chiếu tại ranh giới rồi mới commit**; answer (không trạng thái) và rules (mặt phẳng nội dung, quy tắc cũ của chương này có hiệu lực ở chương kế tiếp) thì thực thi tức thì
- Mỗi Dispatch mang theo ảnh chụp tại thời điểm Collect, đối soát tại ranh giới, không khớp → bỏ, ghi `decision_stale`, hỏi lại bằng sự thật mới:

```go
type DispatchExpect struct {
    CheckpointSeq int64
    Phase         domain.Phase
    Flow          domain.FlowState
    QueueHead     int
}
```

- Tiền điều kiện tường minh tốt hơn hash toàn cục của Store (đọc được, chẩn đoán được); không làm digest toàn cục

## 6. Mô hình khôi phục (chỉ khôi phục sự thật, không khôi phục phiên)

```
khởi động → đọc Progress → đọc Checkpoint mới nhất → tra PendingSteer/AdvanceHold/giấy phép chương → Gate đối soát → Route → tiếp tục chạy Worker
```

Việc khôi phục của plan_start phụ thuộc một sự thật bền vững duy nhất (trong RunMeta), **phán quyết ghi sự thật xuống đĩa trước, rồi mới bắt đầu thực thi**:

```go
type PlanStartRecord struct {
    RawPrompt   string
    Planner     string
    PlannerTask string
    DecisionID  string // liên kết bản ghi kiểm toán
    Status      string // decided | dispatched | done —— hiện thực tường minh trạng thái trung gian của giao dịch khởi động
}
```

Sập ở bất kỳ điểm nào: nếu Record tồn tại thì tiếp tục theo Status, không tham vấn lại; nếu Record thiếu thì coi như hỏi lại cho cuốn sách mới (hỏi lại là chấp nhận được, kiểm toán giữ lại hai bản ghi).

## 7. Lộ trình di chuyển (sắp xếp lại v3: Engine đi trước, Arbiter nối dây sau)

Cơ sở của việc điều chỉnh thứ tự: phương án "Arbiter đi trước" cần một đường ống chuyển tiếp (phán quyết được ngụy trang thành lệnh Host qua steering để đưa cho Coordinator); **nếu Engine đi trước thì cả đường ống đó không cần dựng**, Arbiter nối thẳng vào bộ thực thi của Engine. Mỗi bước đều là xoá bớt thứ, không dựng cầu nối tạm; mối lo ngại "hai bộ não cạnh tranh" bị triệt tiêu một cách có cấu trúc nhờ chính thứ tự đó.

| # | Bước | Trạng thái |
|---|------|------|
| 0 | Mục không điều kiện: bổ sung quy hoạch đưa vào Router (spec liệt kê đầy đủ đi trước); kiểm toán decisions.jsonl. Cải tiến hiện thực: danh tính nhà quy hoạch suy ra từ `RunMeta.PlanningTier` sẵn có, không cần cơ chế ghi mới | ✅ 2026-07-12 |
| 1 | Bàn giao lớp văn phong (docs/voice-layer.md) | ✅ 2026-07-12 |
| 2 | Chốt Step 2 RFC (docs/engine-rfc.md, bảy câu hỏi bắt buộc) | ✅ 2026-07-12 |
| 3 | WorkerRunner: gọi trực tiếp subagent.Runner theo lập trình, sự kiện đi qua relay ToolProgress trong ctx | ✅ 2026-07-22 |
| 4-5 | Engine tiếp quản toàn bộ việc phân công + nối dây bốn kịch bản Arbiter (plan_start/intervention/failure/deadlock), nối thẳng vào bộ thực thi của Engine (khi triển khai phát hiện việc Engine đi trước khiến đường ống chuyển tiếp steering không cần dựng, gộp 4/5 lại làm một) | ✅ 2026-07-12 |
| 6 | Xoá Coordinator cùng toàn bộ phần phụ (thực hiện đủ danh sách ở §10); kiểm thử tích hợp đầu-cuối (tool thật viết trọn cuốn sách / phán quyết thất bại / phán quyết bế tắc) | ✅ 2026-07-12 |

## 8. Các câu hỏi bắt buộc của Step 2 RFC (chưa chốt thì không bước sang bước 3)

1. **Mặt phẳng tách Worker**: API của WorkerRunner; toàn bộ quyền sở hữu và vòng đời của các phần lắp ráp trong build.go — mô hình theo vai trò/failover, prompt cache key, ThinkingLevel, UsageRecorder, SessionLogger, Writer ContextManagerFactory, RestorePack, StopGuardFactory, StopAfterTools, chiếu sự kiện lồng của Observer
2. **Vòng đời Engine**: khởi chạy/tạm dừng/huỷ/khôi phục; bảo đảm một Worker chạy tuần tự; đổi /model và thinking lúc chạy
3. **Hoàn thiện giao thức commit trạng thái**: đối soát Expect của §5 mở rộng cho mọi kịch bản; danh sách tiền điều kiện của Engine sau khi tháo Gate
4. **Phân loại lỗi**: phân loại tất định (retry/reroute/terminal) đi trước, chỉ trường hợp không còn đường ra mới gửi `worker_failure`; phân tầng với cơ chế thử lại ở tầng agentcore
5. **Giao thức bế tắc**: cùng một `Agent+Task` xuất hiện liên tiếp nghĩa là hậu điều kiện của định tuyến chưa được thoả mãn; các checkpoint trung gian bên trong Worker không đặt lại bộ đếm; quyết định retry của Arbiter cũng không đặt lại bộ đếm; 3 lần tham vấn, 5 lần ngắt mạch cứng.
6. **Ngữ nghĩa khi sập**: làm sao phán đoán Worker trước đã tạo ra sự thật hợp lệ hay chưa
7. **Nghiệm thu prototype**: đối chiếu từng điểm năm mục Observer/Usage/Context/chuyển model/khôi phục với hiện trạng

## 9. Bảng cân đối giá trị

| Chiều | Hiện trạng | Trạng thái cuối |
|------|------|------|
| Chi phí LLM mỗi chương | mỗi ranh giới một lượt gọi chuyển tiếp | bỏ đi; lớp sự cố "chuyển tiếp thất bại" biến mất |
| Khả năng kiểm thử phán quyết | ~không có (trộn trong phiên dài) | phát lại offline theo từng kịch bản + hồi quy eval |
| Độ hồi đáp của can thiệp | chờ ranh giới chương (mức phút) | tham vấn tức thì, trạng thái điều khiển commit tại ranh giới |
| Độ phức tạp | hệ sinh thái bảy món vá | giảm ròng 1500+ dòng, ba lớp sự cố được giải tán |
| Khôi phục sau sập | phát lại phiên + giao thức khôi phục | đọc store rồi chạy tiếp |
| Rủi ro giai đoạn chuyển tiếp | — | tập trung ở bước 3/4 (tách Worker), do RFC + chốt prototype kiểm soát; bước 0/1 không điều kiện nào |

## 10. Danh sách xoá ở trạng thái cuối

Coordinator cùng logic khôi phục phiên của nó, Coordinator StopGuard, giao thức steering của Dispatcher và giao thức văn bản `[Host 下达指令]`, FlowBoundaryHook, completePhaseGate / writerExpandedChapterGate (kiểm tra được chuyển thành tiền điều kiện của Engine), MaxTurns=100_000, toàn bộ huấn luyện hành vi trong coordinator.md.

## 11. Phản đối và biên bản phản biện

1. *"Độ đúng của phán quyết sẽ không tăng"* — đúng; khác biệt thật là tập trung / biên tập / kiểm tra tiền điều kiện so với trí nhớ phiên, nhìn chung nhỉnh hơn một chút và lần đầu có thể đo được
2. *"Hiện trạng đã chạy được, đụng vào mặt phẳng điều khiển là rủi ro"* — thừa nhận; nền móng được xây cho việc đó, chia bước thì dừng được và lùi được
3. *"Kiến trúc không phải nút cổ chai, chất lượng nội dung mới là"* — đúng một phần, lớp văn phong đi trước
4. **Phản biện một (2026-07-12)**: thiếu giao thức commit trạng thái → §5; Step 2 quá mỏng → câu hỏi bắt buộc ở §8 + chốt prototype; trình tự khởi động → §6; tuyên bố "trạng thái phi hợp lệ không biểu đạt được" quá mạnh → lịch sử tiến hoá ở 4.1; sai sự thật về danh sách trắng vai trò arbiter → 4.2; vệ sinh kiểm toán → 4.3
5. **Phản biện hai (2026-07-12)**: kiểu Decision theo từng kịch bản (chấp nhận, 4.1); sắp lại thứ tự di chuyển, Engine đi trước (chấp nhận, §7); thống nhất commit tại ranh giới (chấp nhận, §5); PlanStartRecord (chấp nhận, §6); không đổi tên host (chấp nhận); giữ gợi ý số từ trong tệp giao thức (chấp nhận, xem voice-layer). **Ý kiến còn lại**: kiểm toán phải giữ input nếu không sẽ không phát lại được (4.3); hạ `completion_dispute` xuống kịch bản dự phòng (§3)

## 12. Kỷ luật và những việc không làm

**Kỷ luật**: ① mỗi điểm quyết định mới phải đi qua cách chia ba ở §2, cấm mặc định "thêm luật vào prompt"; ② mỗi điểm quyết định của LLM phải có danh sách sự thật / đầu ra có cấu trúc / đường hạ cấp / kiểm toán ghi xuống đĩa; ③ chỉ viết chốt chặn dựa trên sự thật, không viết chốt chặn dựa trên hành vi; ④ bất biến khai báo tốt hơn script thủ tục; ⑤ khi đổi mặt phẳng điều khiển, sửa spec liệt kê đầy đủ trước rồi mới sửa phần hiện thực.

**Không làm**: viết lại theo event sourcing; trừu tượng hoá Store cho nhiều người thuê giả định; DSL quy trình tổng quát; State Digest toàn cục; đổi tên package host; dựng trước `completion_dispute`.
