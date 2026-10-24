# Đề xuất tái cấu trúc: Hybrid Coordinator — Host routing × LLM phán quyết

> **Tài liệu lịch sử, đã bị loại bỏ.** Hybrid Coordinator đã bị kiến trúc Engine + Arbiter thay thế vào 2026-07-12; thiết kế hiện hành xem `docs/architecture.md`, `docs/engine-rfc.md`. Tài liệu này chỉ giữ lại bản ghi tiến hóa của các quyết định, không được dùng làm căn cứ để triển khai.
>
> Trạng thái ban đầu: **Đã được tiếp nhận và triển khai** (2026-04-20)
> Thời điểm khảo sát: 2026-04-20
> Tài liệu hiện hành tương ứng: `docs/architecture.md` §2 / §3 / §7 / §8 / §13 đã được cập nhật đồng bộ
>
> **Đây là bản thảo thứ hai của tài liệu.** Các vấn đề của phương án cực đoan trong bản thảo thứ nhất (xóa hoàn toàn Coordinator) được trình bày chi tiết trong Phụ lục A; giữ lại mục này để tránh phải đi lại con đường đã dẫn tới thất bại.
>
> Kết quả triển khai:
> - Tạo mới `internal/host/flow/` (router.go / state.go / dispatcher.go / router_test.go, toàn bộ 15 unit test cho các nhánh đều vượt qua)
> - Xóa `flow.go` / `queue_guard.go` / `book_complete.go` trong `internal/host/reminder/`; giữ lại StopGuard và Guard của sub-agent
> - Nén `assets/prompts/coordinator.md` từ 88 dòng xuống còn ~45 dòng (trách nhiệm thu hẹp còn thực thi chỉ thị Host + phán quyết + lựa chọn kiểu khởi chạy)
> - Đơn giản hóa mạnh `internal/host/resume.go`, chỉ tạo label và prompt ngắn; bước tiếp theo cụ thể do Router điều phối sau lần TurnEnd đầu tiên
> - Bổ sung các phương thức trợ giúp `HasArcReview` / `HasArcSummary` / `HasVolumeSummary` / `CheckConsistency` trong `internal/store/`
> - Sửa luôn bug khiến agent state trong `observer.go` dừng lại ở working

---

## 1. Bối cảnh

### 1.1 Định vị dự án

```
agentcore       — khung agent dùng chung
litellm         — gateway LLM dùng chung
ainovel-cli     — agent chuyên biệt về sáng tác tiểu thuyết (dự án này)
```

Không gian quyết định của agent chuyên biệt là **đóng**: lưu đồ quy trình cố định, số nhánh hữu hạn, điều khiển bởi sự thật. Triết lý thiết kế của agent dùng chung ("đặt cược vào năng lực mô hình") áp dụng vào bối cảnh chuyên biệt có xu hướng quá thuần túy.

### 1.2 Mục tiêu người dùng (theo thứ tự ưu tiên)

1. **Tính ổn định** — tiếp tục viết không ngừng, không bị gián đoạn vì lỗi routing
2. **Hưởng lợi từ các đợt nâng cấp LLM** — kiến trúc không chống lại năng lực mô hình
3. **Tận dụng tối đa năng lực multi-agent** — phân công chức năng rõ ràng

Đề xuất này mang lại **cải thiện Pareto** giữa cả ba (không hy sinh mục tiêu nào để đổi lấy mục tiêu khác).

---

## 2. Khảo sát hiện trạng

### 2.1 Phân loại các điểm quyết định của Coordinator

Trích từng điểm quyết định trong `coordinator.md`:

| # | Điểm quyết định | Tính chất | Tần suất |
|---|---|---|---|
| 1 | Chọn architect_long / short lúc khởi chạy | Phán quyết (hiểu ngữ nghĩa)| 1 lần mỗi cuốn sách |
| 2 | Mở rộng đầu vào (tự động bổ sung khi <20 ký tự)| Phán quyết (sáng tác)| 0-1 lần mỗi cuốn sách |
| 3 | Vòng lặp bổ sung quy hoạch | Routing (dựa trên sự thật)| 1-3 lần |
| 4 | Bước tiếp theo sau khi commit mỗi chương | **Routing** | **1-2 lần mỗi chương** |
| 5 | Thực hiện đánh giá cuối cung theo từng bước | Routing | 3-5 lần mỗi cung |
| 6 | Rẽ nhánh theo verdict của đánh giá | Routing (đã được mã hóa, xem §2.3)| 1 lần mỗi cung |
| 7 | Xử lý can thiệp của người dùng | Phán quyết (bắt buộc cần LLM)| Tùy ý |
| 8 | Giao lại sub-agent khi báo lỗi | Routing | Thỉnh thoảng |
| 9 | Xuất tóm tắt khi hoàn thành toàn bộ sách | Routing | 1 lần |

**Kết luận**: Trong 9 điểm quyết định, 6 điểm là routing thuần túy (tra bảng), 3 điểm là phán quyết thực sự cần LLM. **Tần suất routing cao hơn rất nhiều so với phán quyết** (1-2 lần mỗi chương so với vài lần mỗi cuốn sách).

### 2.2 Kênh Reminder vốn đã là bản nửa vời của việc mã hóa quy trình

Trong `internal/host/reminder/`, bộ sinh mỗi vòng tạo ra **chỉ thị cụ thể đến từng hành động** dựa trên sự thật:

- `flow.go` → `"当前 flow=writing，next_chapter=37。请直接调 subagent(writer, \"写第 37 章\")..."`
- `queue_guard.go` → `"当前 flow=rewriting，待处理队列：[3,5]。请立即调 writer 逐章重写..."`
- `book_complete.go` → `"全书已完成。请输出全书总结..."`

**Kiến trúc hiện tại có double dispatch**:
```
Lớp quy tắc: coordinator.md định nghĩa "nếu A thì B"
  ↓
Lớp Reminder: mỗi vòng cụ thể hóa quy tắc theo sự thật → sinh ra "bây giờ hãy làm B"
  ↓
Lớp LLM: đọc reminder rồi sinh tool_call (gần như chỉ lặp lại reminder)
  ↓
SubAgent thực thi
```

**Thực tế LLM chỉ "thực thi" những chỉ thị mà Reminder đưa cho nó**. Mắt xích trung gian này vừa tiêu tốn token vừa tạo ra bất định (LLM có thể không tuân thủ reminder một cách hoàn toàn, chẳng hạn như lỗi routing mid được quan sát thấy).

### 2.3 Lớp công cụ từng gánh quá nhiều phán đoán ngữ nghĩa

- Bản cài đặt cũ của `save_review` từng ghi đè verdict của Biên tập viên theo ngưỡng điểm cố định và trạng thái contract; nay đã bị xóa, phán quyết văn học thuộc về Biên tập viên, công cụ chỉ còn làm kiểm tra giao thức và ánh xạ trạng thái nguyên tử
- `commit_chapter.CheckArcBoundary()`: tính toán tức thời `arc_end / needs_expansion / needs_new_volume`
- `commit_chapter.applyCompletion()`: xác định tức thời `book_complete`
- `CommitResult` trả về 17 trường sự thật

**Kết luận**: Bất biến lưu trữ và bất biến giai đoạn mang tính tất định được giữ lại ở lớp công cụ, còn phán đoán văn học và ngữ nghĩa được giao cho mô hình.

### 2.4 Chi phí thực tế của hiện trạng

Mỗi chương, các vòng LLM của Coordinator:
- **1-2 turns mỗi chương** (đọc system prompt ~3000 token + reminder ~200 token + lịch sử + CommitResult ~500 token → sinh tool_call ~50 token)
- Với tiểu thuyết dài 200 chương, có khoảng **200-400 turns** gọi LLM cho Coordinator
- Trong đó **~90% là routing thuần túy** (LLM chỉ lặp lại reminder), **~10% là phán quyết**

**Mỗi chương tốn ~3500-7000 token cho quyết định của Coordinator, trong đó 95% là dư thừa** (Reminder đã tính sẵn đáp án).

---

## 3. Phương án thiết kế: Hybrid Coordinator

### 3.1 Ý tưởng cốt lõi

**Chuyển quyết định luồng từ LLM sang Host, nhưng vẫn giữ Điều phối viên làm nút phán quyết và kênh thực thi chỉ dẫn**.

```
┌──────────────────────────────────────────────────────────────────────┐
│                        Entry (TUI / Chạy nền)                        │
└────────────────────────────────┬─────────────────────────────────────┘
                                 │ Start / Resume / Steer
┌────────────────────────────────▼─────────────────────────────────────┐
│                                 Host                                 │
│                                                                      │
│   ┌────────────────────────────────────────────────────────────┐     │
│   │  Flow Router (lõi mới)                                     │     │
│   │  ───────────                                               │     │
│   │  Nghe Điều phối viên khi subagent tool trả về              │     │
│   │  Hàm thuần: route(Progress, Checkpoint, Boundary)          │     │
│   │      → NextInstruction                                     │     │
│   │  Có lệnh → coordinator.FollowUp(lệnh)                      │     │
│   │  Không lệnh (phán quyết) → không can thiệp, LLM tự quyết   │     │
│   └────────────────────────────────────────────────────────────┘     │
│                                                                      │
│   Giữ lại: vòng đời API / Observer / Usage Tracker                   │
│   Giữ lại: resume.go (đơn giản hóa, giữ nguyên logic cốt lõi)        │
└────────────────────────────────┬─────────────────────────────────────┘
                                  │
┌────────────────────────────────▼─────────────────────────────────────┐
│                      Điều phối viên Agent (LLM)                      │
│                                                                      │
│   Thu hẹp trách nhiệm còn hai loại:                                  │
│   1. Nhận lệnh Host FollowUp → tạo tool_call tương ứng               │
│   2. Khi người dùng Steer, tự phán quyết (truy vấn/đánh giá sửa đổi) │
│                                                                      │
│   coordinator.md: 88 dòng → ~25 dòng                                 │
│   MaxTurns: 1000 giữ lại (đáp lệnh steer + thực thi lệnh Host)       │
└────────────────────────────────┬─────────────────────────────────────┘
                                  │
                                  ▼
         ┌──────────────────────┼───────────────────────┐
         ▼                      ▼                       ▼
  ┌─────────────┐        ┌─────────────┐         ┌─────────────┐
  │Kiến trúc sư │        │Người viết   │         │Biên tập viên│
  └─────────────┘        └─────────────┘         └─────────────┘
```

### 3.2 Phân chia lại trách nhiệm

| Lớp | Việc cần làm | Việc không làm |
|---|---|---|
| **Host / Flow Router** | Đọc sự thật → định tuyến bằng hàm thuần → lệnh FollowUp | Tự gọi SubAgent (vẫn qua Điều phối viên) |
| **Điều phối viên** | Thực thi lệnh Host + phán quyết can thiệp của người dùng + chọn kiến trúc sư lúc khởi động | Tự quyết định "bước tiếp theo cần làm gì" |
| **SubAgent (A/W/E)** | Công việc chuyên môn tương ứng của mỗi vai trò | Không thay đổi |
| **Lớp công cụ** | Ghi xuống đĩa theo nguyên tử + trả về sự thật | Không thay đổi |

**Các tính bất biến then chốt**:
- ✅ Điều phối viên vẫn là một lượt chạy agent liên tục, giữ khả năng "cảm nhận liên tục" xuyên suốt tác phẩm
- ✅ Steer của người dùng vẫn đi qua `coordinator.Inject()`, giữ nguyên khả năng ngắt lời tức thì
- ✅ SubAgentTool vẫn do LLM gọi (theo đường native của agentcore); luồng sự kiện / ContextManager / chuyển đổi mô hình đều không đổi
- ✅ agentcore không bị sửa đổi

### 3.3 Logic cụ thể của Flow Router

```go
// internal/host/flow/router.go

type NextInstruction struct {
    Agent  string   // architect_long / architect_short / writer / editor
    Task   string   // mô tả tác vụ giao cho subagent
    Reason string   // lý do hiển thị cho Điều phối viên (tùy chọn, thuận tiện cho việc gỡ lỗi)
}

type RouterState struct {
    Progress        *domain.Progress
    LatestCheckpoint *domain.Checkpoint
    // Ranh giới cung của chế độ phân tầng (tính khi chương trước đã hoàn thành)
    LastCompleted   int
    ArcBoundary     *store.ArcBoundary
    HasArcReview    bool
    HasArcSummary   bool
    // Các mục thiết lập nền tảng còn thiếu
    FoundationMissing []string
}

// Route trả về chỉ dẫn bước tiếp theo. Trả về nil nghĩa là để Điều phối viên tự phán quyết (trường hợp phán quyết).
func Route(s RouterState) *NextInstruction {
    p := s.Progress

    // 0. Trạng thái cuối: để LLM xuất tóm tắt, không định tuyến
    if p.Phase == domain.PhaseComplete {
        return nil
    }

    // 1. Giai đoạn quy hoạch: LLM tự phán quyết (chọn kiến trúc sư), không định tuyến
    if p.Phase != domain.PhaseWriting {
        return nil
    }

    // 2. Giai đoạn sáng tác
    // 2a. Ưu tiên hàng đợi viết lại/hoàn thiện
    if len(p.PendingRewrites) > 0 {
        ch := p.PendingRewrites[0]
        verb := "重写"
        if p.Flow == domain.FlowPolishing {
            verb = "打磨"
        }
        return &NextInstruction{
            Agent:  "writer",
            Task:   fmt.Sprintf("%s第 %d 章", verb, ch),
            Reason: fmt.Sprintf("PendingRewrites 队列剩余 %d 章", len(p.PendingRewrites)),
        }
    }

    // 2b. Đang thẩm duyệt: không định tuyến, để Điều phối viên đi theo nhánh verdict dựa trên kết quả save_review
    if p.Flow == domain.FlowReviewing {
        return nil
    }

    // 2c. Xử lý sau cuối cung trong chế độ phân tầng
    if p.Layered && s.ArcBoundary != nil && s.ArcBoundary.IsArcEnd {
        b := s.ArcBoundary
        if !s.HasArcReview {
            return &NextInstruction{
                Agent:  "editor",
                Task:   fmt.Sprintf("对第 %d 卷第 %d 弧做弧级评审", b.Volume, b.Arc),
                Reason: "弧末评审未完成",
            }
        }
        if !s.HasArcSummary {
            return &NextInstruction{
                Agent:  "editor",
                Task:   fmt.Sprintf("生成第 %d 卷第 %d 弧摘要", b.Volume, b.Arc),
                Reason: "弧摘要未完成",
            }
        }
        if b.NeedsExpansion {
            return &NextInstruction{
                Agent:  "architect_long",
                Task:   fmt.Sprintf("展开第 %d 卷第 %d 弧（save_foundation type=expand_arc）", b.NextVolume, b.NextArc),
                Reason: "下一弧骨架待展开",
            }
        }
        if b.NeedsNewVolume {
            return &NextInstruction{
                Agent:  "architect_long",
                Task:   "评估并执行 save_foundation(type=append_volume) 或 mark_final",
                Reason: "卷结束需决定追加新卷",
            }
        }
    }

    // 2d. Viết tiếp bình thường
    next := p.NextChapter()
    return &NextInstruction{
        Agent:  "writer",
        Task:   fmt.Sprintf("写第 %d 章", next),
        Reason: "续写",
    }
}
```

**Đặc tính của hàm**:
- Hàm thuần (nhận RouterState, trả về NextInstruction)
- Có thể kiểm thử đơn vị (cho trạng thái, khẳng định kết quả định tuyến)
- **Trả về nil là hợp lệ** — nghĩa là "đây là trường hợp phán quyết, hãy để LLM tự quyết định"

### 3.4 Thời điểm kích hoạt

Host đăng ký sự kiện `agentcore.EventToolExecEnd`:

```go
coordinator.Subscribe(func(ev agentcore.Event) {
    if ev.Type == agentcore.EventToolExecEnd && ev.Tool == "subagent" && !ev.IsError {
        // SubAgent vừa trả về → đọc trạng thái mới nhất → định tuyến
        h.flowRouter.Dispatch()
    }
})
```

```go
func (r *FlowRouter) Dispatch() {
    state := r.loadState()
    instruction := Route(state)
    if instruction == nil {
        return // tình huống phán quyết, để LLM tự quyết
    }
    msg := formatInstruction(instruction)
    _ = r.coordinator.FollowUp(agentcore.UserMsg(msg))
}

func formatInstruction(i *NextInstruction) string {
    return fmt.Sprintf(
        "[Host 下达指令] 下一步：调用 subagent(%s, %q)\n"+
        "理由：%s\n"+
        "这是流程层的明确指令，请立即执行，不要先调 novel_context，不要先输出推理。",
        i.Agent, i.Task, i.Reason,
    )
}
```

### 3.5 Khả năng đáp ứng và tính đồng thời

**Luồng Steer của người dùng** (không thay đổi):
```
Steer → coordinator.Inject(UserMsg("[用户干预] xxx"))
```

- Đang chạy: chèn tin nhắn vào hàng đợi của run hiện tại
- Idle: resume run
- Paused: xếp hàng

**Đồng thời giữa lệnh định tuyến và Steer**:
- Cả hai đều đi vào hàng đợi tin nhắn của Điều phối viên và được xử lý theo thứ tự gốc của agentcore
- Nếu Host vừa gửi `FollowUp("[Host 指令] 写第 37 章")`, ngay sau đó người dùng Steer `"停一下，调整风格"`
  - Điều phối viên xử lý lệnh Host trước hay Steer trước?
  - **`Inject` có ngữ nghĩa chèn vào đầu hàng đợi hiện tại**, nên Steer được xử lý trước
  - Đây là hành vi mong muốn: can thiệp của người dùng có độ ưu tiên cao hơn lịch định tuyến thường lệ của Host

**Tránh xung đột giữa lệnh Host và Steer**:
- Flow Router **tạm dừng ngắn** trong vài turn sau khi nhận tín hiệu "Steer đã được chèn" (để Điều phối viên xử lý xong Steer rồi mới định tuyến)
- Nhận biết kết quả xử lý Steer bằng cách đăng ký `agentcore.EventMessageEnd` + kiểm tra thay đổi trạng thái Progress

### 3.6 Ví dụ đơn giản hóa coordinator.md

Giảm từ 88 dòng xuống khoảng 25 dòng:

```markdown
Bạn là điều phối viên sáng tác tiểu thuyết.

## Chế độ làm việc của bạn

**Luồng chính**: Host sẽ gửi tin nhắn `[Host 下达指令]` sau mỗi lần subagent trả về, cho bạn biết cần gọi subagent nào và làm gì tiếp theo. Nhận chỉ dẫn, hãy lập tức tạo tool_call tương ứng; không gọi novel_context để suy luận trước, không lặp lại.

**Phán quyết**: Khi gặp các tình huống sau, bạn cần tự phán quyết (Host sẽ không đưa chỉ dẫn, bạn phải chủ động hành động):

### Lúc khởi chạy: chọn kiến trúc sư

- Mặc định → `architect_long`
- Chỉ khi người dùng yêu cầu rõ ràng truyện ngắn/một tập/tác phẩm nhỏ và giới hạn độ dài trong 25 chương → `architect_short`

Nếu đầu vào của người dùng dưới 20 ký tự, trước tiên hãy bổ sung trong mô tả task một hướng đi khác biệt, đối tượng đọc và ít nhất một điểm móc truyện phi quy ước, rồi mới phân công.

### Steer của người dùng

Định dạng: `[用户干预] xxx`

- **Loại truy vấn** (hỏi trạng thái/bối cảnh): trực tiếp trả lời bằng văn bản, không cần gọi thêm tool; Host sẽ tiếp tục phân công.
- **Loại sửa đổi** (yêu cầu đổi bối cảnh/viết lại/điều chỉnh văn phong): đánh giá phạm vi ảnh hưởng:
  - Liên quan đến thay đổi bối cảnh → gọi architect_* để thực hiện `save_foundation(type=...)`
  - Liên quan đến chương đã viết → để tool tự động ghi chương mục tiêu vào `PendingRewrites` (có thể nói rõ ý định viết lại khi gọi lại writer)
  - Chỉ ảnh hưởng đến văn phong về sau → mô tả ngắn gọn yêu cầu, rồi khi nhận chỉ dẫn Host tiếp theo, đính kèm vào mô tả task của writer

## Tool

- `subagent(agent, task)`: gọi subagent
- `novel_context`: chỉ dùng khi yêu cầu truy vấn của người dùng cần đến nó, không gọi trước khi có chỉ dẫn Host

## Subagent

- `architect_long` / `architect_short` / `writer` / `editor`

## Cấm

- Gọi novel_context trước rồi mới hành động khi chỉ dẫn Host đã đến
- Tự quyết định bước tiếp theo khi không có Steer của người dùng và không có chỉ dẫn Host
```

### 3.7 Thu hẹp mạnh kênh Reminder

**Xóa**:
- `flow.go` (Host FollowUp đã đưa chỉ dẫn cụ thể, nhắc nhở định tuyến của Reminder không còn giá trị)
- `queue_guard.go` (hàng đợi được Host Router bảo đảm)
- `book_complete.go` (Host dùng FollowUp để đưa chỉ dẫn tổng kết khi Phase=Complete)

**Giữ lại**:
- `subagent_guards.go` (StopGuard của Người viết/Kiến trúc sư/Biên tập viên, bảo đảm subagent không kết thúc tay trắng)
- Thêm một `foundation_reminder.go` nhẹ: trong giai đoạn quy hoạch, thông báo cho Điều phối viên các mục còn thiếu (đây là **thông tin cần thiết cho phán quyết**, không phải lệnh định tuyến)

**Giữ lại StopGuard**:
- StopGuard của Điều phối viên được giữ lại (chặn end_turn khi `Phase != Complete` làm cơ chế dự phòng)
- Thêm nhắc nhở khi "đã nhận chỉ dẫn Host nhưng lượt này chưa gọi subagent tương ứng"

### 3.8 Đơn giản hóa nhẹ resume.go

Hiện tại `buildResumePrompt` sinh chỉ dẫn bằng ngôn ngữ tự nhiên chính xác đến từng bước theo checkpoint (121 dòng).

Kiến trúc mới:
- Khi khôi phục, trước tiên đọc Progress, Flow Router tính `NextInstruction`
- Điều phối viên nhận một prompt khôi phục **rất ngắn**, rồi chờ chỉ dẫn FollowUp từ Host

```
[Khôi phục] Cuốn sách「xxx」đã hoàn thành N chương, đang ở giai đoạn XX.
Vui lòng chờ chỉ dẫn bước tiếp theo từ Host, hoặc xử lý các can thiệp của người dùng có thể đã để lại trong thời gian dừng.
```

Hầu như toàn bộ logic phân nhánh được đẩy xuống Flow Router (Router vốn phải định tuyến theo trạng thái, Resume không cần một đường đi riêng).

---

## 4. Đánh giá mức độ đạt mục tiêu

### 4.1 Độ ổn định

| Rủi ro | Hiện tại | Kiến trúc mới |
|---|---|---|
| Điều phối viên chọn sai kiến trúc sư | Đã xảy ra (định tuyến sai ở mid) | Vẫn là phán quyết lúc khởi chạy, nhưng prompt đã đổi từ ba lựa chọn thành hai (đã thực hiện), phạm vi lỗi giảm mạnh |
| Điều phối viên không tuân thủ yêu cầu "chỉ nói viết chương thứ N" | Đã xảy ra | Host đưa chỉ dẫn theo định dạng cố định, không còn cần LLM tạo mô tả task |
| Điều phối viên bỏ sót việc kiểm tra queue_drained | Đã xảy ra | Host Router buộc đi theo thứ tự |
| Sau commit ở cuối cung, Điều phối viên quên gọi editor | Có thể | Host Router phát hiện IsArcEnd && !HasArcReview rồi phân công trực tiếp |
| Bỏ sót nhánh khôi phục sau sự cố | Lỗ hổng đã biết | Máy trạng thái của Flow Router tự nhiên bao phủ mọi nhánh |
| StopGuard chặn liên tục 5 lần rồi nâng lên fatal | Tồn tại | Sau khi lệnh Host đã rõ ràng, LLM rất khó chặn liên tục (trừ khi prompt hỏng nặng) |

### 4.2 Lợi ích khi nâng cấp LLM

| Chiều | Mức độ giữ lại |
|---|---|
| Nâng cấp mô hình Người viết → chất lượng sáng tác | 100% |
| Nâng cấp mô hình Biên tập viên → độ chính xác thẩm duyệt | 100% |
| Nâng cấp mô hình Kiến trúc sư → quy hoạch tinh tế | 100% |
| **Nâng cấp mô hình Điều phối viên → phán quyết chính xác hơn** | **100%** (vẫn giữ các tình huống phán quyết) |
| ~~Nâng cấp mô hình Điều phối viên → định tuyến chính xác hơn~~ | Từ bỏ (tỷ lệ lỗi định tuyến vốn đã phải bằng 0, không cần LLM thông minh hơn) |

**Giữ lại quan trọng**: Các tình huống phán quyết như đánh giá can thiệp của người dùng, lựa chọn kiến trúc sư, phán đoán ranh giới verdict vẫn do LLM xử lý, nên việc nâng cấp mô hình mang lại lợi ích trực tiếp.

### 4.3 Khả năng đa agent

- Số lượng, chức năng và cách lắp ráp SubAgent **không thay đổi hoàn toàn**
- Mô hình dị hợp (coordinator/architect/writer/editor được cấu hình độc lập) **không thay đổi hoàn toàn**
- Điều phối viên vẫn là run liên tục, giữ lại "góc nhìn toàn bộ cuốn sách"
- Phương tiện hợp tác (sản phẩm trong Store) không thay đổi

### 4.4 Khả năng đáp ứng

- Khả năng người dùng Steer ngắt qua `coordinator.Inject` được **giữ nguyên hoàn toàn**
- Host Router đưa chỉ dẫn khi SubAgent trả về, đi cùng kênh tin nhắn với Steer của người dùng
- Inject có độ ưu tiên cao hơn FollowUp (`Inject` mang ngữ nghĩa chèn đầu hàng), nên Steer không bị lệnh Host đẩy ra

### 4.5 Chi phí token

Hiện tại mỗi chương: Điều phối viên ~3500-7000 token × 1-2 turn = 3500-14000 token

Kiến trúc mới mỗi chương:
- Prompt của Điều phối viên được nén từ ~3000 token xuống ~800 token
- Mỗi chương vẫn cần 1 turn (Điều phối viên đọc chỉ dẫn FollowUp + tạo tool_call)
- Tổng cộng ~1000-1500 token

**Tiết kiệm 60-80%**. Với tiểu thuyết dài 200 chương, tiết kiệm khoảng 400k-1M token (không đạt 100% như phương án cực đoan, nhưng không hy sinh khả năng đáp ứng và góc nhìn toàn bộ cuốn sách).

---

## 5. Ảnh hưởng đến docs/architecture.md

### 5.1 Điều chỉnh §2 nguyên tắc cốt lõi

**Nguyên tắc một** (vòng lặp chính do LLM dẫn dắt) → điều chỉnh thành:
```
LLM điều khiển việc sáng tác và phán quyết, Host điều khiển việc định tuyến quy trình.

- Sáng tác và phán quyết (các quyết định cần hiểu ngữ nghĩa, đánh giá chất lượng và nhận diện ý định) vẫn để LLM thực hiện
- Định tuyến quy trình (đọc sự thật → tra bảng → phát lệnh) do mã Host đảm nhiệm
- Host không vượt qua Điều phối viên để gọi trực tiếp SubAgent, mà đưa chỉ dẫn rõ ràng qua FollowUp,
  giữ Điều phối viên làm kênh thực thi chỉ dẫn và nút phán quyết
```

**Nguyên tắc hai** (đặt cược vào năng lực mô hình, không đặt cược vào hardcode) → điều chỉnh thành:
```
Trên phương diện sáng tác và phán quyết, đặt cược vào mô hình (khả năng phán quyết của Người viết/Biên tập viên/Kiến trúc sư/Điều phối viên),
trên phương diện định tuyến quy trình thì dùng mã để biểu đạt (không gian quyết định của các agent chuyên ngành là đóng; tác vụ tra bảng không mang lại lợi ích gì cho LLM).
```

### 5.2 Điều chỉnh danh sách cấm của §13

- §13.13 "Không làm mặt phẳng điều khiển tất định đọc tệp tín hiệu của Host → chèn chỉ thị bước tiếp theo" →
  **Sửa cách diễn đạt**: "Không dùng tệp tín hiệu làm IPC (đọc trực tiếp Progress + Checkpoint là đủ); Host đọc sự thật rồi đưa ra chỉ thị gọi sub-agent cụ thể qua `coordinator.FollowUp` là một dạng định tuyến theo ngách hợp lý"
- §13.14 "Không hard-code chuyển Flow trong máy trạng thái" →
  **Sửa cách diễn đạt**: "Flow vẫn chỉ được cập nhật bởi tool (không viết máy trạng thái kiểu '如果 A 则 SetFlow(B)' trong Host), nhưng Flow Router có thể quyết định gọi ai tiếp theo dựa trên Flow và các sự thật khác"

### 5.3 Điều chỉnh lắp ráp Agent của §7

- Giữ lại lắp ráp Điều phối viên
- Rút `coordinator.md` từ 88 dòng xuống ~25 dòng
- Thu gọn các kênh Reminder (xóa flow/queue_guard/book_complete, giữ foundation/subagent_guards)
- Thêm package `internal/host/flow/`

---

## 6. Điểm yếu đã biết (liệt kê trung thực)

### 6.1 Sự tiến hóa dài hạn của Flow Router

- Khi có thêm tình huống mới (trạng thái flow mới, hậu xử lý cuối cung mới), khối switch-case của Router sẽ dài thêm
- Cần ràng buộc chặt: **chỉ xử lý định tuyến, không xử lý logic nghiệp vụ**; các quy tắc quyết định phải được viết unit test
- Lời cảnh báo tương tự `handleSubAgentDone` của v0.0.1 luôn còn hiệu lực; tuy nhiên phương án này tránh trôi về phía đối tượng thần nhờ "hàm thuần + unit test + chỉ gọi sự thật thuần"

### 6.2 Sự phức tạp của việc người dùng can thiệp

- Thiết kế hiện tại giao toàn bộ Steer cho LLM của Coordinator phán quyết
- Nhưng có Steer lại vượt qua nhiều loại (ví dụ "viết lại cho rõ ràng vài chương đầu của nhân vật A + thêm nhánh cốt truyện cho anh sau này")
- Cần dựa vào khả năng phân rã của LLM, prompt phải đưa ra hướng dẫn rõ ràng
- **Phần này được hưởng lợi trực tiếp khi nâng cấp mô hình** (so với việc hard-code phân loại enum của InterventionAgent, cách phán quyết linh hoạt của LLM hợp với tình huống thực tế hơn)

### 6.3 Phụ thuộc tiền đề về tính nhất quán của tầng sự thật

- Router dựa trên Progress + Checkpoint để ra quyết định, nên tầng sự thật phải đáng tin cậy
- Một tệp dùng `withWriteLock` + tmp/rename bảo đảm thay thế nguyên tử; các bước xuyên tệp của `commit_chapter` được khôi phục nhờ toàn bộ tải trọng PendingCommit, ảnh chụp nội dung truyện (văn bản) và phát lại idempotent theo giai đoạn; còn thao tác cấu trúc thì sửa các view dẫn xuất với cùng tham số; không cái nào tuyên bố có giao dịch nguyên tử kiểu cơ sở dữ liệu
- Nhưng nếu tầng sự thật xuất hiện sai lệch (ví dụ Progress nói chương 3 đã hoàn thành nhưng dưới chapters/ lại không có), Router sẽ ra quyết định sai
- Khuyến nghị: lúc khởi chạy thêm một lần **kiểm tra tính nhất quán của tầng sự thật** (ví dụ nếu phát hiện Progress.CompletedChapters không khớp với thư mục chapters/ thì báo cảnh báo)

### 6.4 Điều phối viên vẫn giữ khả năng định tuyến bằng LLM

- Ngay cả khi chỉ thị đã rõ ràng, LLM vẫn có thể "sáng tạo" ra cách không thực hiện (ví dụ sinh ra một đoạn suy nghĩ rồi mới gọi tool)
- StopGuard làm lưới an toàn: nếu đã nhận chỉ thị từ Host nhưng lượt này chưa gọi subagent thì chèn một lời nhắc
- Đây là lưới an toàn, không phải lệnh cấm — thỉnh thoảng "suy nghĩ thêm một bước" của mô hình mạnh cũng không phải là điều xấu

### 6.5 Yêu cầu độ phủ kiểm thử tăng lên

- Flow Router là hàm thuần, phải có unit test đầy đủ (bao phủ mọi tổ hợp Phase × Flow × Boundary)
- Kiểm thử tích hợp: mô phỏng toàn bộ chuỗi "commit → router → FollowUp → coordinator phản hồi → subagent"
- Kiểm thử khôi phục sau sập: kill tiến trình rồi resume, khẳng định Router suy luận đúng bước tiếp theo

---

## 7. Lộ trình triển khai

### Giai đoạn 1: Củng cố tầng sự thật (khoảng 0.5 ngày)

- Bổ sung kiểm tra tính nhất quán ở §6.3: quét một lần lúc khởi chạo/Resume, sinh cảnh báo
- Bảo đảm các API `store.HasArcReview(vol, arc)` và `HasArcSummary(vol, arc)` dùng được (nếu chưa có thì thêm)

### Giai đoạn 2: Đưa khung Flow Router vào (khoảng 1 ngày)

- Tạo package `internal/host/flow/`:
  - `route.go` — hàm thuần `Route(state) → *NextInstruction`
  - `dispatcher.go` — đăng ký sự kiện + đưa ra FollowUp
  - `route_test.go` — unit test bao phủ mọi nhánh
- Dùng công tắc cấu hình `flow_driven: true/false` để điều khiển việc kích hoạt
- Mặc định tắt (false), trước tiên chạy đối chứng

### Giai đoạn 3: Kích hoạt và xác minh (khoảng 1 ngày)

- Bật `flow_driven: true`
- Chạy một tiểu thuyết 30-50 chương, so sánh các chỉ số:
  - Số lần gọi LLM của Coordinator
  - Số lỗi định tuyến (phải bằng 0)
  - Tính phản hồi (steer ngắt ngang có hoạt động bình thường không)
- Sửa bug, điều chỉnh quy tắc Router

### Giai đoạn 4: Đơn giản hóa coordinator.md + thu gọn Reminder (khoảng 0.5 ngày)

- Sửa coordinator.md theo §3.6
- Xóa `reminder/flow.go / queue_guard.go / book_complete.go`
- Giữ lại các reminder foundation cần thiết
- Cập nhật StopGuard của subagent nếu cần (thường không cần)

### Giai đoạn 5: Đơn giản hóa resume.go (khoảng 0.5 ngày)

- Xóa phần lớn các nhánh của `buildResumePrompt`
- Thay bằng thông điệp ngắn gọn dùng chung "[恢复] 请等待 Host 指令"
- Sau Resume, Router tự nhiên suy luận hành động tiếp theo

### Giai đoạn 6: Cập nhật tài liệu kiến trúc (khoảng 0.5 ngày)

- Sửa §2 / §13 / §7 của `docs/architecture.md` theo §5
- Đổi trạng thái tài liệu phương án này thành "đã chấp nhận", lưu trữ vào `docs/history/`

### Giai đoạn 7: Thời gian quan sát (2-4 tuần)

- Chạy liên tục 2-3 tiểu thuyết dài (mỗi cuốn 100+ chương)
- Ghi lại mọi lỗi định tuyến (nếu có), các vấn đề về tính phản hồi, hành vi ngoài dự kiến của Coordinator
- Dựa trên quan sát, tinh chỉnh quy tắc Router và coordinator.md

**Tổng cộng khoảng 4 ngày triển khai + thời gian quan sát**.

---

## 8. Bảng so sánh

| Tiêu chí | Kiến trúc hiện tại | Hybrid (phương án này)| Phương án cực đoan (Phụ lục A)|
|---|---|---|---|
| Tính ổn định | Trung bình (LLM thỉnh thoảng định tuyến sai)| **Cao** | Cao |
| Tính phản hồi | Cao | **Cao** | **Thấp** (Host gọi trực tiếp SubAgent không thể bị ngắt)|
| Lợi ích từ LLM | 100% | **100%** | 85% (từ bỏ chiều định tuyến)|
| Tiết kiệm token | 0 | ~70% | ~95% |
| Góc nhìn toàn cuốn sách | Có | **Có** | Không (mỗi SubAgent độc lập)|
| Chi phí triển khai | - | Trung bình (khoảng 4 ngày)| Cao (khoảng 1 tuần + sửa agentcore)|
| Cập nhật tài liệu | - | Nhỏ (tinh chỉnh §2/§13)| Lớn (viết lại nguyên lý §2)|
| Cần sửa agentcore | - | Không | Có thể (gọi trực tiếp SubAgent)|
| Độ khó hoàn tác | - | Thấp (công tắc cấu hình)| Cao |

---

## 9. Các điểm quyết định

1. **Có chấp nhận phương án này (Hybrid Coordinator) không?** [ ] Chấp nhận · [ ] Chấp nhận sau khi sửa · [ ] Không chấp nhận
2. Giai đoạn 3 có nên được triển khai và xác minh trước dưới dạng một PR độc lập không? [ ]
3. Việc điều chỉnh §2 / §13 của `docs/architecture.md` có được xử lý luôn trong lần này không? [ ]
4. Thời gian quan sát: [ ] 2 tuần · [ ] 4 tuần · [ ] Lâu hơn

---

## Phụ lục A: Phương án cực đoan đã được đánh giá (xóa hoàn toàn Điều phối viên)

> Phương án bản nháp đầu tiên. Vì tính phản hồi giảm, tính khả thi kỹ thuật còn nghi vấn, mất góc nhìn toàn cuốn sách của Coordinator nên được hạ xuống làm tài liệu tham chiếu.

Cốt lõi của phương án cực đoan: Host gọi trực tiếp `SubAgentTool.Execute`, không đi qua Coordinator LLM.

**Các vấn đề đã nhận diện**:

1. **Suy giảm tính phản hồi**: `SubAgentTool.Execute` là lời gọi đồng bộ chặn, Steer của người dùng phải chờ SubAgent hiện tại trả về mới được xử lý. `Inject` của kiến trúc hiện tại có thể ngắt ngang lập tức.
2. **Tính khả thi kỹ thuật còn nghi vấn**:
   - Host gọi trực tiếp SubAgentTool vi phạm thông lệ sử dụng của agentcore
   - Luồng sự kiện (Event của `Subscribe`) có thể không được truyền lên đúng tới observer
   - Đường dẫn callback `ContextManagerFactory` / `OnMessage` của SubAgent chưa biết
   - Cần sửa agentcore hoặc thay đổi lớn observer
3. **Mất góc nhìn toàn cuốn sách của Coordinator**: mỗi SubAgent chạy độc lập, không có "người canh gác LLM liên tục". Trong các lần chạy dài, những vấn đề như trôi lệch văn phong hay nhân vật bị cắt đoạn sẽ mất một lớp bảo vệ vô hình.
4. **InterventionAgent bị đơn giản hóa quá mức**: phương án cực đoan dùng enum (query/modify_setting/rewrite_chapters/adjust_style/noop) để phân loại ý định người dùng, nhưng Steer thực tế có thể vượt qua nhiều loại, schema cứng sẽ gây phân loại sai.
5. **Khối lượng công việc viết lại tài liệu kiến trúc lớn**: nguyên lý cốt lõi của §2 bị lật đổ, 30% phần trình bày tài liệu bị ảnh hưởng.
6. **FlowDriver sẽ lớn thành đối tượng thần**: một vòng lặp nhồi toàn bộ logic định tuyến, mỗi khi thêm tình huống đều phải sửa, cùng cấu trúc với `handleSubAgentDone` của v0.0.1.

Phương án Hybrid tránh được 4 vấn đề đầu tiên, vấn đề thứ 5 được hạ xuống thành tinh chỉnh nhỏ, vấn đề thứ 6 được kiểm soát bằng "hàm thuần + unit test".

---

## Phụ lục B: Chi tiết vị trí đặt các điểm quyết định

| Điểm quyết định | Vị trí hiện tại | Vị trí trong kiến trúc mới | Loại |
|---|---|---|---|
| Chọn kiến trúc sư quy hoạch | coordinator.md L26-29 | Coordinator LLM phán quyết (lúc khởi chạy)| Phán quyết |
| Mở rộng đầu vào | coordinator.md L31 | Coordinator LLM phán quyết (lúc khởi chạy)| Phán quyết |
| Vòng lặp bổ sung quy hoạch | coordinator.md L36-38 | Host Router nhánh Phase=Premise/Outline (trả về nil để LLM tự chủ động hoặc FollowUp architect tường minh)| Hỗn hợp |
| Bước tiếp theo cho mỗi chương | coordinator.md L46-51 + reminder/flow | **Host Router nhánh 2d** (FollowUp writer)| Định tuyến |
| Đánh giá cuối cung | coordinator.md L78-82 | **Host Router nhánh 2c** (FollowUp editor/architect)| Định tuyến |
| Phân nhánh verdict | coordinator.md L59-61 + tool save_review | Đã được mã hóa ở tầng tool, Router chỉ đọc Flow | Định tuyến (đã hoàn thành)|
| Can thiệp của người dùng | coordinator.md L67-70 | Coordinator LLM phán quyết (khi nhận tin nhắn Inject)| Phán quyết |
| Giao lại khi kiến trúc sư quy hoạch báo lỗi | coordinator.md L40 | Host Router phát hiện FoundationMissing không thay đổi, đếm số lần thử lại | Định tuyến |
| Tổng kết khi hoàn thành toàn bộ cuốn sách | coordinator.md L63-65 + reminder/book_complete | Host Router phát hiện Phase=Complete → FollowUp "输出总结" | Định tuyến |

---

## Phụ lục C: Vị trí mã nguồn tham chiếu

- `assets/prompts/coordinator.md` — cần đơn giản hóa
- `internal/host/reminder/flow.go` / `queue_guard.go` / `book_complete.go` — cần xóa
- `internal/host/reminder/subagent_guards.go` — giữ lại
- `internal/host/reminder/stop_guard.go` — giữ lại + thêm kiểm tra "phải thực hiện chỉ thị nhận được từ Host"
- `internal/host/resume.go` — đơn giản hóa mạnh
- `internal/host/observer.go` — đăng ký EventToolExecEnd mới để kích hoạt Router
- `internal/host/flow/` — package mới
- `internal/tools/commit_chapter.go` L220-280 — CommitResult đã có đủ 17 trường
- `internal/tools/save_review.go` — ánh xạ nguyên tử từ verdict của Editor tới Flow/hàng đợi làm lại
- `internal/store/outline.go` `CheckArcBoundary` — API sự thật ranh giới cung
