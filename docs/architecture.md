# Kiến trúc runtime ainovel-cli

> Lớp sự thật mang tính tất định, lớp ngữ nghĩa mang tính tự chủ: một Engine nối tiếp mang tính tất định, ba Worker tự chủ, vài hàm Tài phán được gọi theo nhu cầu, và một lớp sự thật trên hệ thống tệp.
>
> Ngày 2026-07-12, việc thay thế mặt phẳng điều khiển đã hoàn tất: vòng lặp dài của LLM Điều phối viên bị loại bỏ, thay vào đó là Engine (vòng lặp tất định) + Tài phán (hàm phán quyết ngữ nghĩa) tiếp quản. Các quyết định thiết kế và biên bản đánh giá xem `docs/engine-arbiter.md`, RFC xem `docs/engine-rfc.md`.

---

## 1. Mục tiêu (theo thứ tự ưu tiên)

1. **Tính ổn định**: với một câu lệnh đầu vào, viết trọn vẹn một cuốn tiểu thuyết (200~500 chương) một cách ổn định. Không tự ngắt giữa chừng vì vấn đề kiến trúc.
2. **Chất lượng có thể lặp lại cải tiến**: prompt / tài liệu tham chiếu / tiêu chí đánh giá / chiến lược ngữ cảnh có thể điều chỉnh độc lập mà không kéo theo kiến trúc.
3. **Khả năng khôi phục**: sau khi sập, mất mạng hoặc tạm dừng, có thể tiếp tục từ checkpoint gần nhất.
4. **Khả năng quan sát**: tiến độ, sản phẩm và thời gian của từng chương, từng bước đều tra cứu được.

"Ổn định" là tiền đề, "chất lượng" là tầng trên. Mọi quyết định kiến trúc đều ưu tiên phục vụ tính ổn định.

---

## 2. Nguyên tắc cốt lõi

### 2.1 Nguyên tắc ba phần: đưa quyết định về đúng bản chất

- **Chuyển trạng thái có thể liệt kê → mã nguồn**. "Sau khi viết xong một chương thì cử ai" là đọc sự thật rồi tra bảng: hàm thuần `flow.Route` + kiểm thử đặc tả liệt kê hàng chục nghìn tổ hợp, tỷ lệ lỗi tiệm cận 0, không tốn chi phí LLM.
- **Phán đoán ngữ nghĩa có ranh giới rõ ràng → hàm LLM (Tài phán)**. Chọn kiến trúc sư quy hoạch, phân loại can thiệp của người dùng, lối thoát khi thất bại/đình trệ: sự thật đi vào, quyết định có cấu trúc đi ra, kiểm tra hợp lệ cơ học làm lưới an toàn, mỗi lần phán quyết được ghi xuống đĩa để có thể phát lại.
- **Sáng tác mở → vòng lặp LLM (Worker)**. Trong phạm vi một chương, một lần đánh giá, một lần quy hoạch, architect/writer/editor hoàn toàn tự chủ.

Tính đối xứng của hai mặt phẳng là kỷ luật xuyên suốt — mọi điểm quyết định mới trong tương lai đều tuân theo hình dạng này, không phát minh mô hình mới:

```
Mặt phẳng tất định:  flow.LoadState   → flow.Route     → Instruction   (kiểm thử đặc tả liệt kê đầy đủ)
Mặt phẳng ngữ nghĩa:    arbiter.Collect* → arbiter.Decide* → XxxDecision   (decisions.jsonl + hồi quy eval)
              └── Thu thập sự thật(IO) ──┘└── Lõi(hồi phát ngoại tuyến) ──┘└── Engine thực thi ──┘
```

### 2.2 Công cụ là giao diện duy nhất của lớp sự thật

Mọi tương tác với hệ thống tệp, Progress, Checkpoint đều do công cụ thực hiện. Đối với một tệp đơn lẻ, dùng thay thế nguyên tử `temp + fsync + rename`; ghi tuần tự xuyên nhiều tệp không giả vờ là giao dịch cơ sở dữ liệu: việc commit chương dùng Saga `PendingCommit` bền vững, còn ghi cấu trúc dùng cơ chế phát lại idempotent mang tính tất định và để lộ thất bại một cách tường minh. Mỗi bước đều phải kiểm tra lỗi; chỉ những quy trình đã ghi xuống đĩa ý định khôi phục mới có thể hứa khôi phục sau khi khởi động lại với đúng tải trọng ban đầu.

### 2.3 Lớp quan sát chỉ quan sát

UI, chẩn đoán, nhật ký sự kiện đều là những bên tiêu thụ thụ động được chiếu ra từ luồng sự kiện / artefact chỉ đọc. Chỉ đọc sự thật, không sinh ra sự thật, không ảnh hưởng luồng điều khiển.

Dữ liệu quan sát được chia nghiêm ngặt thành ba tầng: `agentcore.ProgressPayload` là tầng truyền tải, văn bản lỗi phải đầy đủ và không được chứa chiến lược cắt ngắn của UI; `host.Event.Summary` mang ngữ nghĩa hiển thị ngắn gọn, còn `Detail` là chẩn đoán đầy đủ; nhật ký tệp ưu tiên ghi đầy đủ `Detail`, TUI chỉ đọc `Summary` và cắt ngắn theo bề rộng terminal khi render cuối cùng. File logger do `Host` nắm giữ: trước tiên giành được lease của thư mục tiểu thuyết, sau đó thiết lập phiên nhật ký, rồi mới lắp ráp Store, mô hình và Engine; như vậy vừa không vượt qua cơ chế chiếm dụng một cuốn, vừa bao phủ toàn bộ quá trình lắp ráp và đóng nhật ký. "Nhật ký đầy đủ" ở đây nghĩa là chuỗi lỗi, tham số không hợp lệ gốc và siêu dữ liệu vòng đời không bị mất, chứ không phải chép lại toàn bộ nội dung tiểu thuyết đã sinh thành công vào `tui.log`; nội dung lớn vẫn do artefact của Store và `meta/sessions` mang.

**`internal/diag` là hệ thống con quan sát duy nhất của engine** — hạ tầng hỗ trợ hạng nhất, nhưng không phải lõi sản phẩm. Nó đọc chéo hầu hết các artefact + session + log + checkpoint, kiêm hai nhiệm vụ: ① **chẩn đoán chất lượng sáng tác** (quy tắc → Finding, báo cáo trên màn hình `/diag`); ② **gỡ lỗi runtime + xuất đã khử nhạy cảm** (bóc lược xương hành vi khỏi văn bản + tổng hợp theo vòng lặp → ghi đè `meta/diag-export.md`).

**Kỷ luật của người quan sát (không được nới lỏng)**: diag có thể chẩn đoán, có thể khuyến nghị, nhưng**không bao giờ tự hành động** — không tự sửa, không tự chạy tiếp, không đổi quy trình (bài học lịch sử xem §10 mục 5).

### 2.4 Lớp sự thật phẳng

Chỉ có ba loại sự thật:

- **Progress** — chỉ mục tiến độ (đã viết đến chương nào, danh sách chương cần viết lại)
- **Checkpoint** — nhật ký tiến triển ở mức bước (plan / draft / commit / review / arc_summary)
- **Artifact** — sản phẩm như nội dung truyện của chương, dàn ý, nhân vật, tóm tắt

Không đưa vào các abstraction như WorkflowInstance / TaskInstance / Command. Các sự thật phụ (bể phản hồi dàn ý, nhật ký vi phạm cơ học, kiểm toán phán quyết) cũng là jsonl phẳng, mỗi loại có đúng một bên sản xuất và một bên tiêu thụ.

### 2.5 Bốn nguyên tắc bất di bất dịch

**Nguyên tắc một: công cụ chỉ trả về sự thật, không trả về chỉ thị điều phối xuyên lớp**. `commit_chapter` trả về các trường có cấu trúc như `arc_end` / `needs_expansion`; không kèm chuỗi chỉ thị kiểu `[系统]`. Trường `next_step` trong subagent là chỉ dẫn nội tuyến của một phát biểu sự thật ("Tôi vừa lưu plan, bước tiếp theo là draft"), không tính là vi phạm — xem §6.3.

**Nguyên tắc hai: định tuyến quy trình do Flow Router đảm nhiệm, thực thi do Engine đảm nhiệm**. `Route(state) → *Instruction` trong `internal/flow/router.go` là hàm thuần (được đóng đinh bằng kiểm thử đặc tả liệt kê hàng chục nghìn tổ hợp); mỗi vòng, Engine đọc sự thật từ store, Route suy ra chỉ thị, rồi **chạy trực tiếp Worker theo lập trình** (`subagent.Runner.Run`, đầu vào/kết quả/chuỗi lỗi có kiểu), không có lớp chuyển tiếp công cụ qua LLM. Trả về nil nghĩa là kịch bản ngữ nghĩa (hoàn tất cuốn/chờ can thiệp) hoặc dừng tự nhiên. **Đình trệ có giới hạn tường minh** (RFC §5): sau vòng trước, Route vẫn sinh ra cùng `Agent+Task`, tức là điều kiện hậu kiện của định tuyến chưa được thỏa mãn; 3 lần hỏi Tài phán, 5 lần thì ngắt mạch cứng và tạm dừng. Checkpoint trung gian bên trong Worker không đặt lại bộ đếm, Engine tất định không cho phép quay vòng vô hạn.

**Nguyên tắc ba: phán quyết ngữ nghĩa đi qua Tài phán, mỗi lần phán quyết đều ghi xuống đĩa**. Lúc khởi chạy chọn kiến trúc sư quy hoạch, phân loại can thiệp của người dùng, định tuyến lối thoát khi thất bại/đình trệ — các hàm Decide theo từng kịch bản trong `internal/arbiter` phán quyết: sự thật đi vào, quyết định có cấu trúc đi ra, kiểm tra hợp lệ cơ học làm lưới an toàn, kiểm toán decisions.jsonl (hồi quy phát lại ngoại tuyến). Ba Worker giữ lại `CheckpointDeltaGuard` riêng (rào chắn sự thật: chưa ghi xuống đĩa sản phẩm thì không được kết thúc).

**Nguyên tắc bốn: mã hóa cứng ranh giới, không mã hóa cứng phán đoán ngữ nghĩa không thể liệt kê**. Mã nguồn chỉ cố định các bất biến có thể chứng minh (quyền, giai đoạn, thứ tự, tính idempotent, tính toàn vẹn cấu trúc) và cung cấp cho mô hình đầy đủ sự thật cùng không gian thao tác đủ rộng; các câu hỏi mở như lựa chọn trong sáng tác, đánh giá chất lượng, cách kế hoạch thích nghi với nội dung truyện phải dành cho Worker / Tài phán. Cấm thay thế khả năng hiểu của mô hình bằng từ khóa, ngưỡng chấm điểm, danh mục lệch chuẩn hay bảng quy tắc; cũng cấm thu hẹp không gian quyết định hợp lệ của mô hình chỉ vì lo mô hình sẽ sai. Trước khi thêm một quy tắc vào mã nguồn, phải chứng minh trước rằng không gian quyết định là đóng và kết quả có thể kiểm chứng bằng máy; nếu không, nên cải thiện khả năng diễn đạt của ngữ cảnh và công cụ, để lợi ích của việc nâng cấp mô hình được thu hoạch mà không cần thay vỏ bọc.

---

## 3. Toàn cảnh kiến trúc

```
[Entry: TUI / Chạy nền]
        │ prompt / steer
[Host vỏ bọc]
   ├── observer            Trung chuyển tiến độ Worker + sự kiện do Engine phát → chiếu sang UI/nhật ký
   ├── engine              Vòng lặp tất định: LoadState → Route → kiểm tra tiền điều kiện → chạy Worker → ranh giới sentinel
   ├── Đường can thiệp     Steer/Continue → Tài phán phán quyết → thực thi hành động(tức thời/commit tại ranh giới)
   └── usage / ngân sách / điểm dừng / quản lý mô hình
        │ Gọi theo lập trình subagent.Runner.Run (tiến độ được trung chuyển qua ctx ToolProgress)
[architect_short/long · writer · editor](mỗi vai có run + context + mô hình riêng)
        │ Lời gọi công cụ
[Tools]  novel_context · read_chapter · plan_chapter · draft_chapter · edit_chapter
         check_consistency · commit_chapter · save_review · save_arc_summary
         save_volume_summary · save_foundation
        │ Nguyên tử một tệp + phát lại idempotent (commit dùng Saga bền vững)
[Store: hệ thống tệp (tmp + rename)]
   Progress · Checkpoints · Outline · Drafts · Summaries · Characters · World
   · Signals · Decisions(kiểm toán phán quyết) · bể phản hồi · nhật ký vi phạm
```

| Lớp | Làm gì | Không làm gì |
|---|---|---|
| Entry | Hiển thị, nhận đầu vào | Quyết định nghiệp vụ |
| Host/Engine | Vòng đời, thực thi Route, chạy Worker, ranh giới sentinel, điều phối can thiệp | Đánh giá văn học; ghi sự thật sáng tác (hành động trạng thái điều khiển đi qua lõi công cụ) |
| Tài phán | Phán quyết ngữ nghĩa (quyết định có cấu trúc) | Tự sáng tác; thực thi hành động |
| Workers | Suy nghĩ, sáng tác, thẩm duyệt | Đọc/ghi Store trực tiếp (bắt buộc qua công cụ) |
| Tools | IO nguyên tử một tệp + lỗi tường minh + idempotent; commit dùng Saga | Chỉ thị điều phối xuyên subagent |
| Store | Ghi xuống đĩa qua hệ thống tệp | Logic nghiệp vụ |

Phụ thuộc là một chiều: `entry → host → agents/arbiter → tools → store → domain`; `flow` là gói chiến lược thuần ở tầng trên (trên store, dưới host). Độc lập theo chiều ngang: `errs/` có thể được mọi lớp tham chiếu, `diag/` đăng ký theo luồng sự kiện của host + chỉ đọc `store/`.

---

## 4. Mô hình dữ liệu

### 4.1 BookMetadata và Progress

`BookMetadata` là nguồn sự thật duy nhất cho tên sách và phần giới thiệu dành cho người đọc, được lưu bền vững vào `meta/book.json`; `book.md` chỉ là bản chiếu có thể đọc. Premise không lưu trùng tên sách, Progress cũng không mang thông tin tác phẩm.

```go
type BookMetadata struct {
    Title    string
    Synopsis string
}
```

Progress (`internal/domain/runtime.go`) chỉ ghi lại trạng thái vận hành:

```go
type Progress struct {
    Phase             Phase           // init / premise / outline / writing / complete
    CurrentChapter    int
    TotalChapters     int
    CompletedChapters []int
    TotalWordCount    int
    ChapterWordCounts map[int]int
    InProgressChapter int             // chương đang được viết
    Flow              FlowState       // writing / reviewing / rewriting / polishing / steering
    PendingRewrites   []int
    StrandHistory     []string        // chuỗi dominant_strand
    HookHistory       []string        // chuỗi hook_type
    CurrentVolume, CurrentArc int     // phân cấp cho tiểu thuyết dài
    Layered           bool
}
```

Logic điều khiển chỉ đọc các trường sự thật nêu trên, không phụ thuộc vào bất kỳ "dấu thời gian cập nhật" nào — thông tin thời gian do `OccurredAt` của checkpoint mang.

RunMeta (`meta/run.json`) mang **ý định vận hành của người dùng** (không phải sự thật sáng tác): PlanningTier, PlanStart (kết quả phán quyết lúc khởi chạy được cố định, cơ sở duy nhất để khôi phục sau khi sập trong giai đoạn quy hoạch), PendingSteer (bảo vệ khi sập trong lúc can thiệp, một ô đang trong hành trình), AdvanceMode / AdvancePermitChapter (chính sách nghiệm thu theo từng chương và giấy phép chương chính xác), AdvanceHold (một lần tạm dừng do người dùng ký trong lúc can thiệp). `RunMeta.Init` giữ lại toàn bộ các trường ý định qua các lần khởi động lại.

### 4.2 Checkpoint (`internal/domain/checkpoint.go`)

```go
type Scope      struct { Kind ScopeKind; Chapter, Volume, Arc int }
type Checkpoint struct {
    Seq        int64       // tăng đơn điệu
    Scope      Scope       // chapter / arc / volume / global
    Step       string      // plan / draft / commit / review / arc_summary / ...
    Artifact   string
    Digest     string
    OccurredAt time.Time
}
```

Lưu trữ: `meta/checkpoints.jsonl`, chỉ ghi nối. Ghi lặp lại với `Scope+Step+Digest` giống hệt được coi là idempotent và không tạo dòng mới.

### 4.3 Artifact và các sự thật phụ

Artifact nằm ở `store/outline.go` `drafts.go` `summaries.go` `characters.go` `world.go`.

- **Signals**: `PendingCommit` (khôi phục khi commit bị gián đoạn). Được đọc lúc khởi chạy/khôi phục, không đọc trong lúc vận hành.
- **Decisions** (`meta/decisions.jsonl`): bản ghi kiểm toán mỗi lần Tài phán phán quyết (facts+input+decision), có thể phát lại ngoại tuyến; **không phải nguồn dữ liệu khôi phục** (việc khôi phục chỉ phụ thuộc Progress/Checkpoint/RunMeta).
- **Sự thật thế giới tăng dần**: thay đổi dòng thời gian và trạng thái nhân vật được ghi nối tương ứng vào `timeline.jsonl`, `meta/state_changes.jsonl`; trong tiến trình duy trì chỉ mục khử trùng lặp, lần commit bình thường chỉ ghi phần tăng của chương hiện tại. Mảng JSON của phiên bản cũ được chuyển sang theo giao thức idempotent "ghi atom tệp nhật ký mới trước, xóa tệp cũ sau" vào lần ghi nối kế tiếp, `timeline.md` là bản chiếu dành cho con người và có thể dựng lại.
- **Bể phản hồi dàn ý** (`meta/outline_feedback.jsonl`): phản hồi thường của Người viết được tiêu thụ trong lần thao tác cấu trúc kế tiếp; nếu bản sửa nội dung truyện từ bên ngoài ảnh hưởng đến cốt truyện thì phải ưu tiên chuyển cho Kiến trúc sư trước khi viết tiếp, xử lý xong thì làm rỗng.
- **Nhật ký vi phạm cơ học** (`meta/rule_violations.jsonl`): kết quả kiểm tra theo user_rules khi commit, được Biên tập viên tiêu thụ qua `novel_context(chapter=N)`; là siêu dữ liệu chất lượng theo nguyên tắc best-effort, không có tính nhất quán mạnh cùng cấp với lần commit.

### 4.4 Dàn ý phân cấp và hội tụ hoàn thành cuốn (tập chốt hạ)

Quy hoạch cuộn tròn (điểm neo compass + bộ khung tập + cung mở rộng theo nhu cầu) giải quyết "mở đầu và lăn bóng", nhưng lại biến "khi nào kết thúc" từ một con số thành một phán quyết ngỏ mở ở cuối mỗi tập — việc hội tụ để hoàn thành cuốn phải được thiết kế tường minh, nếu không sẽ xuất hiện hai loại đình trệ: số liệu đã viết xong nhưng không khép lại được (vòng lặp viết tiếp vượt phạm vi, đã được cơ chế cấu trúc chặn bổ sung sửa) và truyện đã viết xong nhưng số liệu không cho phép dừng (estimated_scale bị ước lượng quá cao + ngưỡng hoàn thành bị bác cứng → thêm nội dung hoặc ngắt mạch).

**Tập chốt hạ là một khái niệm hạng nhất của sự hội tụ**, hoàn thành cuốn = một lần phán quyết hướng đi + một đoạn trượt tất định:

- **Tuyên bố (phán quyết ngữ nghĩa bằng LLM)**: Kiến trúc sư chọn một trong ba vào cuối tập — append_volume (tiếp tục) / append_volume kèm `"final": true` (tập chốt hạ) / complete_book (khi mọi điều kiện hiện tại đều thỏa mãn). estimated_scale trong phán đoán hoàn thành là **bằng chứng chứ không phải quyền phủ quyết**.
- **Thực thi (tra bảng sự thật bằng mã nguồn)**: sự thật chốt hạ = `domain.FinaleVolume`. Khi cấu trúc tập cuối đã viết xong (`layeredStructurallyComplete`) **và phần khép cuối tập đủ bộ ba (đánh giá cung/tóm tắt cung/tóm tắt tập)** thì tự động MarkComplete — việc hoàn thành không được vượt trước cổng chất lượng của Biên tập viên. Cuốn chưa tuyên bố vẫn đi theo đường cấp chất lượng `layeredBookComplete` (manh mối + tuyến dài về 0).
- **Gỡ bỏ (suy ra từ dữ liệu, không có công cụ hoàn tác)**: sau khi tuyên bố lại thêm một tập mới chưa đánh dấu → trạng thái hội tụ tự nhiên được gỡ bỏ. Trạng thái luôn có thể suy ra từ layered_outline.
- **Điều phối phán đoán hoàn thành**: cuối tập, nhánh 10 của Route cử architect_long đi qua danh sách kiểm tra phán đoán hoàn thành — quyền phán quyết hoàn thành thuộc về Kiến trúc sư (một Worker), không thuộc mặt phẳng điều khiển.

---

## 5. Đặc tả công cụ

Công cụ là điểm tương tác duy nhất giữa lớp sự thật và Agent.

### 5.1 Công cụ đọc

`novel_context(scope)` / `read_chapter(n)` — có thể gọi bất cứ lúc nào, không phụ thuộc trạng thái trước đó, và trả về đủ dữ liệu để LLM tự quyết định độc lập. `novel_context(chapter=N)` còn chèn thêm các vi phạm theo kiểm tra tự động của chương đó (nếu có); nhánh architect chèn tóm tắt các tập đã hoàn thành/tóm tắt tập và cung hiện tại, ảnh chụp nhân vật, nhóm phản hồi dàn ý và trạng thái foundation. Khi mở rộng cung, nội dung đã xảy ra là sự thật, còn khung xương chỉ là kế hoạch; Kiến trúc sư có thể đồng bộ sửa đổi title/goal của cung mục tiêu và mở rộng thành các chương trong `expand_arc`.

### 5.2 Công cụ ghi (nguyên tử theo tệp đơn + ngữ nghĩa khôi phục phân cấp)

Ghi một tệp là nguyên tử; các bước xuyên nhiều tệp không cam kết tính nguyên tử kiểu cơ sở dữ liệu. Bản ghi thường và bản ghi làm lại của `commit_chapter` cùng dùng `PendingCommit`, tiến triển theo “toàn bộ ý định → artifact/trạng thái → Progress → checkpoint → xóa ý định”; khi khôi phục chỉ dùng payload đã chuẩn hóa được ghi xuống đĩa lần đầu cùng ảnh chụp nội dung truyện, tuyệt đối không dùng các tham số do mô hình sinh lại sau khi khởi động lại hoặc bản nháp đã bị ghi đè. Các thao tác cấu trúc như `expand_arc`/`append_volume` không có ý định được lưu bền, chỉ cam kết khả năng phát lại idempotent với cùng tham số, sửa chữa view dẫn xuất và trả về lỗi tường minh.

| Công cụ | Artifact | Bước |
|---|---|---|
| `save_book` | meta/book.json + book.md | book |
| `plan_chapter` | drafts/chXX.plan.json | plan |
| `draft_chapter` | drafts/chXX.draft.md | draft |
| `edit_chapter` | drafts/chXX.draft.md | edit |
| `check_consistency` | không có (chỉ đọc, trả về inline) | consistency_check |
| `commit_chapter` | chapters/chXX.md + Progress (+ nhóm phản hồi/bản ghi vi phạm best-effort) | commit |
| `save_review` | reviews/chXX.json (global là chXX-global.json) | review |
| `save_arc_summary` | summaries/arc-vNNaNN.json | arc_summary |
| `save_volume_summary` | summaries/vol-vNN.json | volume_summary |
| `save_foundation` | foundation/*.json (expand_arc/append_volume/update_compass thành công là tiêu thụ nhóm phản hồi) | premise / outline / layered_outline / characters / world_rules / expand_arc / append_volume / update_compass / complete_book |

`commit_chapter` đảm nhiệm phát hiện trạng thái hoàn thành của cung/tập/cả cuốn sách, trả về sự thật có cấu trúc; `save_review` không phán quyết theo ngưỡng văn học, chỉ kiểm tra hợp lệ sự thật thẩm duyệt và ánh xạ atomically verdict do Biên tập viên đưa ra thành Flow và hàng đợi làm lại.

`edit_chapter` là lớp bọc mỏng của `agentcore.EditTool`; nó chỉ cho phép chỉnh sửa các chương đã hoàn thành và nằm trong `PendingRewrites`. Bản nháp đầu của một chương mới phải đi qua `draft_chapter(mode="write")` để ghi đè toàn bộ chương.

### 5.3 Phân tầng lỗi

| Loại lỗi | Lớp xử lý | Hành động |
|---|---|---|
| Hết thời gian mạng / EOF luồng | Tools | Thử lại 3 lần |
| provider 429/503 | litellm | failover sang provider dự phòng |
| Xác thực / mô hình không tồn tại | Tools | ném terminal lên trên |
| Thiếu artifact trước đó | Tools | ném conflict lên trên, LLM gọi `novel_context` rồi thử lại |
| Tham số công cụ không hợp lệ | Tools | ném validation lên trên, LLM sửa tham số |
| retryable (stream-idle v.v.) | lớp subagent | thử lại tại chỗ với MaxRetries=7, không ra khỏi Worker |
| Worker thất bại (guard nâng cấp/hard_stop v.v.) | Engine | lỗi tất định thì dừng trực tiếp; các lỗi còn lại được thử lại một lần với cùng chỉ dẫn → Tài phán phán quyết retry/reroute/abort |
| Bế tắc (cùng chỉ dẫn định tuyến liên tục lặp lại) | Engine | tham vấn Tài phán 3 lần; đến lần thứ 5 thì tạm dừng do ngắt mạch cứng |
| Phản hồi luồng rỗng / suy nghĩ lâu | litellm (`StreamIdleTimeout=5min`) | watchdog kích hoạt thử lại |

### 5.4 Tính idempotent

Mỗi công cụ ghi đều kiểm tra checkpoint trước khi thực thi: nếu `Step+Digest` của checkpoint mới nhất trong scope hiện tại trùng với lần này, công cụ trả về trực tiếp artifact đã tồn tại. Cả việc thử lại lẫn việc phân phối lặp lại sau khi khôi phục từ sự cố đều an toàn—đây cũng là nền tảng để cơ chế khôi phục mô hình của Engine (đọc store để tiếp tục) hoạt động.

---

## 6. Lắp ráp Worker

> Về lý thuyết, một Prompt siêu lớn + một Agent có thể viết trọn một cuốn sách, nhưng ba điều sẽ cản trở độ ổn định: **bùng nổ ngữ cảnh** (dù nén mạnh, 200 chương vẫn suy thoái), **xung đột vai trò** (quy hoạch nghiêm túc / sáng tác giàu hình ảnh / thẩm duyệt phê phán cùng làm loãng nhau trong cùng một prompt), **mất lợi ích từ mô hình dị loại** (chọn mô hình riêng cho quy hoạch/sáng tác/thẩm duyệt là không gian tối ưu đáng kể về chi phí/chất lượng). Vì vậy, topology nhiều Worker là cần thiết.

### 6.1 Lắp ráp và vận hành

`agents.BuildWorkers` (`internal/agents/build.go`) lắp ráp ba loại Worker thành một `subagent.Runner`: Engine gọi trực tiếp `Run(agent, task)`, mỗi lần gọi là một `agentcore.AgentLoop` hoàn chỉnh (ngữ cảnh độc lập, mô hình độc lập, cơ chế thử lại độc lập). Toàn bộ cấu hình lắp ráp có hiệu lực ngay một lần: mô hình theo vai trò + failover, prompt cache key (mỗi lần spawn tự tăng #seq), ThinkingLevel, UsageRecorder/SessionLogger (OnMessage), ContextManagerFactory cho Người viết (cửa sổ tự động được dựng lại khi chuyển đổi qua /model), RestorePack, StopGuardFactory, StopAfterTools.

Việc truyền tiến độ của Worker đi qua **callback ToolProgress của ctx**: Engine gọi `Runner.Run` bằng `agentcore.WithToolProgress(ctx, relay)`; các sự kiện gọi công cụ/nội dung truyền theo luồng/thinking/thử lại/ngữ cảnh của subagent đi qua relay để vào observer—cùng hình dạng ProgressPayload như thời Điều phối viên, nên lớp quan sát được tái sử dụng.

```
Engine ── Runner.Run(agent, task) ──▶ architect_short/long · writer · editor
                                          │ gọi công cụ
                                        Store（phương tiện phối hợp, Worker không giao tiếp trực tiếp với nhau）
```

`bootstrap.ModelSet` hỗ trợ mô hình cấp vai trò: architect/writer/editor được cấu hình độc lập + provider failover. Người viết chạy Sonnet thay vì Opus có thể tiết kiệm chi phí đến một bậc độ lớn trên tiểu thuyết 200 chương. Tài phán dùng thống nhất mô hình Default (được tính phí qua usageTrackedModel); hiện chưa mở cấu hình vai trò độc lập.

### 6.2 Ba chế độ phối hợp

Worker không giao tiếp trực tiếp với nhau; mọi thông tin đều chuyển qua các artifact có cấu trúc trong Store:

**Chế độ A · Chuyển giao nối tiếp (nhánh chính)**: Route phái Kiến trúc sư quy hoạch → Người viết viết chương 1..N → Biên tập viên thẩm duyệt cuối cung → Người viết viết lại. Mỗi bước "tiếp theo phái ai" do Route suy luận từ sự thật.

**Chế độ B · Vòng lặp phản hồi**: Người viết báo cáo trong commit rằng nội dung lệch dàn ý → nhóm phản hồi được ghi xuống đĩa (chỉ với sách phân tầng) → lần thao tác cấu trúc kế tiếp, Kiến trúc sư tham khảo qua novel_context → khi thao tác thành công, nhóm phản hồi được tiêu thụ và xóa. Người viết không gọi trực tiếp Kiến trúc sư; phản hồi luân chuyển qua lớp sự thật.

**Chế độ C · Mở rộng khung xương (quy hoạch cuộn)**: Sau commit, sự thật cho thấy cung kế tiếp vẫn chỉ là khung xương → Route (hoặc precheck của Engine) phái architect_long mở rộng → Người viết tiếp tục. Khả năng "quy hoạch cuộn" cho tiểu thuyết dài chính là vòng lặp này.

### 6.3 Ràng buộc mã nguồn cho quy trình Worker (không dựa vào prompt kiểu phao cứu sinh)

> Quy trình writer ban đầu dựa vào ràng buộc "tiến triển đúng theo thứ tự sau" trong `writer.md`. LLM thường vi phạm—bỏ qua plan rồi draft trực tiếp, hoặc chỉ viết nội dung truyện trong cuộc trò chuyện mà không ghi xuống đĩa. **Ràng buộc quy trình bằng prompt không ổn định**; nâng cấp mô hình thậm chí còn có thể khiến nó "sáng tạo ra cách không tuân thủ".

Bốn lớp ràng buộc mã nguồn (cùng có hiệu lực):

| Lớp | Điểm đặt | Tác dụng |
|---|---|---|
| `StopAfterTools` / `StopAfterToolResult` | `agents/build.go` SubAgentConfig | thoát ngay khỏi lần chạy Worker khi công cụ then chốt thành công (lần thoát ở trạng thái cuối vẫn tham vấn StopGuard, xem kiểm thử hợp đồng). Khi Người viết gặp `commit_chapter` thì dừng ngay; các lệnh `save_review`/`save_arc_summary`/`save_volume_summary` của Biên tập viên và bước kết cung/kết tập của Kiến trúc sư dùng `StopAfterToolResult` |
| `CheckpointDeltaGuard` | `agents/guard/subagent_guards.go` | lấy baseline checkpoint làm ranh giới; trước khi lượt hiện tại kết thúc, phải thấy checkpoint mới tương ứng với step, nếu không sẽ từ chối `end_turn`; chặn liên tiếp 3 lần thì nâng cấp thành terminate (dự phòng cho vòng lặp vô hạn của mô hình yếu). Guard của Biên tập viên nhận biết nhiệm vụ: khi được phái tạo tóm tắt, việc chỉ kiểm tra lại không được coi là hoàn thành |
| `next_step` nội tuyến trong công cụ | trường trong giá trị trả về của từng công cụ | mỗi sự thật kèm sẵn "khuyến nghị bước tiếp theo", nên khi thấy sự thật, LLM biết bước tiếp theo |
| Kiểm tra quyền sở hữu/điều kiện trước đó bên trong công cụ | `edit_chapter` `commit_chapter` v.v. | lớp dữ liệu chặn trực tiếp: sửa một đoạn trong bản nháp đầu, sửa chương đã hoàn thành chưa được đưa vào hàng đợi, và commit rỗng đều bị từ chối; `ConcurrencySafe=false` ngăn tranh chấp khi chạy đồng thời |

`writer.md` chỉ đảm nhiệm: giao thức thực thi, mô hình nhận thức về việc tiếp tục từ điểm dừng, và cách diễn giải hợp đồng chương; tiêu chuẩn sáng tác nằm ở lớp văn phong (`{{VOICE}}` là placeholder được điền lại, người dùng có thể ghi đè, xem `docs/voice-layer.md`). **Đây chính là tiền đề để lớp văn phong dám mở cho người dùng: các bất biến nằm ở lớp công cụ, nên prompt có sửa hỏng tuỳ ý cũng không làm hỏng máy trạng thái.**

### 6.4 Phụ thuộc agentcore

`../agentcore` là thư viện Agent đa dụng do chính dự án này phát triển (liên kết bằng go.work). Các primitive mà Engine sử dụng: `subagent.Runner.Run` (gọi trực tiếp bằng chương trình, kết quả có kiểu và chuỗi lỗi—việc phân loại như `errors.Is(err, subagent.ErrUnknownAgent)` không phụ thuộc vào nội dung thông báo lỗi), ToolProgress của ctx (truyền tiếp sự kiện), `subagent.Config`, `StopGuard`/`StopAfterTools`. `subagent.Tool` chỉ dành cho các host cần phơi bày Runner cho mô hình thông qua `Runner.AsTool()`; AINovel không đi qua lớp này.

**Ranh giới chỉnh sửa**: có thể đưa vào agentcore—chiến lược ContextManager mới, bộ chuyển đổi provider mới, kiểu sự kiện mới; không đưa vào agentcore—mô hình nghiệp vụ và công cụ nghiệp vụ. Tiêu chí quyết định: giả sử agentcore sau này được coding agent / agent dịch vụ khách hàng đưa vào sử dụng, năng lực mới chỉ được đưa vào nếu vẫn có ý nghĩa trong kịch bản đó. **Cấm viết bản vá dự phòng ở tầng ứng dụng**—thiếu năng lực thì sửa trực tiếp phía thượng nguồn.

**Kiểm thử hợp đồng** (`internal/agents/agentcore_contract_test.go`, 6 mục, tất cả đều được điều khiển qua `Runner.Run`): ghim các hành vi của framework mà dự án phụ thuộc thành những khẳng định có thể thực thi (thoát ở trạng thái cuối vẫn tham vấn StopGuard, Error/Aborted không chạm guard, chuỗi lỗi Escalate có thể khớp bằng `errors.Is`, `ErrUnknownAgent` có kiểu trong `Run`, tiến độ lỗi của công cụ đầy đủ và thuần văn bản). **Phải vượt toàn bộ trước khi bump agentcore**—chú thích sẽ lỗi thời, còn kiểm thử thì không (kỷ luật này đã bắt được một giả định hỏng và tiết kiệm một workaround).

### 6.5 Bộ nhớ đệm prompt

Đòn bẩy thứ hai cho chi phí chạy dài hạn (đòn bẩy thứ nhất là chọn mô hình). Bản giải thích đầy đủ xem `docs/prompt-cache-design.md`. Phân công ba tầng: **litellm chỉ dịch giao thức**, **agentcore quyết định vị trí đặt bộ nhớ đệm và danh tính của nó**, **ainovel tích hợp bằng một dòng cấu hình**.

Điều kiện tiên quyết để bộ nhớ đệm mang lại lợi ích là **byte tiền tố của yêu cầu phải ổn định**, được bảo đảm bởi ba kỷ luật (tất cả nằm trong agentcore):

1. **tools có byte xác định** — Description/Schema được dựng lại mỗi lần; mọi lần lặp qua map đều sắp xếp trước
2. **lịch sử append-only** — tin nhắn chỉ được thêm, không viết lại; nén ngữ cảnh là một giao dịch tường minh "trả giá một lần miss toàn bộ để đổi lấy cửa sổ", và phép chiếu phải `CommitOnProject`
3. **nội dung động nằm ở phần đuôi** — toàn bộ envelope/chỉ dẫn được thêm vào đuôi, không bao giờ viết lại các tin nhắn trước đó

Cấu hình theo nguyên tắc "một cuốn sách một base, một vai trò một tên, một phiên một khóa": Hệ OpenAI dùng `PromptCacheKey = nvl-<书哈希>-<角色>#<spawn序号>` để tạo affinity định tuyến (mặc định chỉ gửi tới endpoint chính thức, bên trung gian có thể bật tường minh); hệ Claude dùng `CacheLastMessage: "ephemeral"` cho điểm gãy cuộn + điểm gãy sàn hệ thống. **Đường đỏ về khóa**: mọi đại lượng được đưa vào cache key được tính lần đầu trong phiên rồi đóng băng, cứ để giá trị cũ còn hơn làm vỡ cache. Phát hiện đứt cache (`host/usage.go noteCacheBreak`) chỉ quan sát, không sửa; bộ đếm được ghi vào `usage.json cache_breaks` và bảng cache của TUI.

---

## 7. Engine và Arbiter

### 7.1 Vòng lặp Engine (`internal/host/engine.go`)

```
for {
    Áp dụng hành động trạng thái điều khiển từ can thiệp (xả; hold+dispatch trước tiên thiết lập sự thật làm lại)
    advanceGate.HandleBoundary() // tiêu thụ hold + đối soát permit review
    inst := giao việc can thiệp ?? Route(LoadState) ?? planStartFallback
    inst == nil → return          // hoàn thành / dừng ngữ nghĩa, chờ Continue
    precheck(inst)                // hình thức tất định của ToolGate cũ: loại bỏ giao việc ở giai đoạn hoàn thành;
                                  // writer: chương đích chưa được triển khai → giao lại cho architect để triển khai
    advanceGate.Allow(inst)       // chỉ chặn chương mới tiến triển chưa được cho phép
    trackDeadlock(inst)           // cùng Agent+Task lặp lại liên tiếp: hỏi Arbiter 3 lần, ngắt mạch ở lần 5
    runWorker(inst)               // subagent.Runner.Run + truyền tiến độ + sự kiện DISPATCH
    Phân loại lỗi: lỗi tất định→tạm dừng; thất bại lần đầu thử lại một lần; thất bại lần nữa→Arbiter(retry/reroute/abort)
    Ranh giới chính sách: budget → advanceGate
}
```

Một goroutine chạy tuần tự; hủy `ctx` = tạm dừng (checkpoint bảo đảm không mất dữ liệu). **Trạng thái điều khiển chỉ thay đổi tại ranh giới vòng lặp**: các hành động hold/reopen/dispatch từ can thiệp được xếp hàng đến lúc ghi xuống đĩa tại ranh giới (tổ hợp hold+dispatch thực hiện giao việc để dựng hàng đợi trước, rồi mới cho Gate tiêu thụ hold); answer/rules được thực hiện ngay. Chế độ `review` chỉ ràng buộc các chương mới tiến triển, không chặn làm lại, thẩm duyệt, duy trì cấu trúc và khôi phục sau khi ghi. Trước khi thực thi giao việc, Arbiter đối soát Expect (các trường ngữ nghĩa Phase/Flow/QueueHead; CheckpointSeq chỉ dùng để kiểm toán, không đối soát — khi can thiệp, worker thường đang chạy nên seq chắc chắn thay đổi); nếu không khớp thì hủy và đồng bộ gửi lại can thiệp gốc vào đầy đủ đường phán quyết để hỏi lại.

### 7.2 Arbiter (`internal/arbiter/`)

Bốn kịch bản; mỗi kịch bản có một cặp `Collect*Facts` (ranh giới IO) / `Decide*` (không có IO, ngoại trừ yêu cầu mô hình do bộ thực thi thống nhất quản lý, nên có thể phát lại ngoại tuyến) + loại Decision riêng (hành động không khớp kịch bản không thể biểu diễn ở mức kiểu):

| Kịch bản | Kích hoạt | Loại quyết định |
|---|---|---|
| `plan_start` | Khởi chạy sách mới | Chọn kiến trúc sư short/long + mở rộng yêu cầu quá ngắn |
| `intervention` | Can thiệp của người dùng | Tổ hợp answer/rules/hold/reopen/dispatch (thứ tự thực thi do Engine cố định) |
| `worker_failure` | Worker báo lỗi và phân loại tất định không tìm ra lối ra | retry / reroute / abort |
| `deadlock` | Cùng chỉ thị lặp lại không có tiến triển | retry / reroute / abort |

Đường thất bại: bộ thực thi có cấu trúc thống nhất chọn JSON Schema gốc hoặc hợp đồng prompt tùy theo năng lực; lỗi định dạng/Schema trong chế độ prompt và lỗi kiểm tra nghiệp vụ trong cả hai chế độ sẽ truyền nguyên nhân chính xác về cho mô hình để tiếp tục sửa cho đến khi thành công hoặc hết `context`, không đặt giới hạn số lần. Vi phạm hợp đồng gốc, từ chối trả lời, cắt ngắn, kết thúc do lỗi và lỗi yêu cầu không thể thử lại đều được trả về ngay và rõ ràng; can thiệp không tạo ra thao tác ghi, khởi chạy báo lỗi rõ ràng, failure/deadlock tạm dừng thận trọng. **Đầu ra của Arbiter không đáng tin cũng như mọi đầu ra của LLM** — sau khi kiểm tra JSON Schema, `Validate` tiếp tục thực hiện kiểm tra cơ học dựa trên sự thật (ràng buộc phase, reopen chỉ áp dụng khi đã hoàn thành, chương vượt phạm vi). Mức dùng đi qua `usageTrackedModel` để đưa vào hệ thống ngân sách và usage.

### 7.3 Vỏ Host (`internal/host/host.go`)

Vòng đời (`StartPrepared`/`Resume`/`Continue`/`Steer`/`Abort`/`Close`), điều phối can thiệp (FIFO tuần tự + bảo vệ sự cố PendingSteer), chiếu sự kiện, quản lý mô hình. Các kênh quan sát `Events`/`Stream`/`Done`, UI tổng hợp `Snapshot()`, các điểm mở rộng (nhập/xuất/đồng sáng tác/viết theo mẫu/chuyển mô hình).

`runEnded` (callback engine.onDone) xác định trạng thái cuối theo sự thật trong store: Phase=Complete → completed + bản tóm tắt hoàn thành có tính tất định (không tốn lượt gọi LLM); còn lại → idle/paused. **Không được để bất kỳ logic "tự động chạy tiếp" nào xuất hiện ở đây** (bài học lịch sử §10 mục 5).

---

## 8. Khởi chạy, khôi phục và can thiệp

### 8.1 Tạo mới

```
User: "一句话需求"
  → StartPrepared(raw)
    → Progress.Init / Checkpoints.Reset
    → StartPrompt cố định trong RunMeta (ghi sự thật đầu vào xuống đĩa trước khi phán quyết)
    → Arbiter plan_start phán quyết (chọn kiến trúc sư + mở rộng yêu cầu) → lỗi hiển thị rõ ràng (kiểm toán kèm error)
    → PlanStartRecord cố định trong RunMeta (ghi sự thật phán quyết xuống đĩa trước, rồi mới bắt đầu thực thi)
    → engine.start (chỉ thị giao việc đầu tiên)
```

Phán quyết thất bại không phải ngõ cụt: StartPrompt đã có, nên mọi lần khôi phục/tiếp tục sau đó đều do engine phán quyết bù (xem §8.2).

### 8.2 Khôi phục (khởi động lại sau sự cố)

```
Tiến trình khởi động → resumeLabel (nhãn thuần UI) → cảnh báo tính nhất quán → AdvanceGate đối soát
  → Nếu có PendingSteer → đồng bộ đi theo đường phán quyết can thiệp (can thiệp có hiệu lực trước khi chạy tiếp) rồi khởi động engine
  → nếu không thì engine.start(nil): chỉ khôi phục sự thật, Route tính lại từ store để chạy tiếp
```

Không có phiên nào cần khôi phục. Sự cố trong giai đoạn quy hoạch (phán quyết đã ghi xuống đĩa, foundation đầu tiên chưa ghi xuống đĩa) được `planStartFallback` giao tiếp dựa trên PlanStartRecord, không thực hiện lại phán quyết đã có. Nếu phán quyết khởi chạy **chưa từng hoàn tất** (sự cố mô hình lúc khởi chạy), `planStartFallback` sẽ phán quyết bù tại chỗ dựa trên StartPrompt — đây là lần thử lại của phán quyết đầu tiên, không vi phạm nguyên tắc "khôi phục không phán quyết lại"; nếu phán quyết bù thất bại thì tạm dừng và thông báo rõ ràng, không được dừng im lặng. An toàn khi giao lặp được bảo đảm bởi tính lũy đẳng của công cụ (§5.4).

### 8.3 Can thiệp của người dùng

`Steer`/`Continue` đi thống nhất qua đường phán quyết Arbiter (`doIntervention`):

```
Ghi PendingSteer xuống đĩa (bảo vệ sự cố) → Collect facts → Decide (mức giây)
  → Ghi decisions.jsonl xuống đĩa → answer hiển thị lại / rules ghi xuống đĩa ngay lập tức
  → hold/reopen/dispatch xếp vào hàng đợi và ghi xuống đĩa tại ranh giới (khi engine đã dừng thì thực hiện ngay và khởi động lại engine tùy theo ý định)
  → Tất cả hành động thành công → xóa PendingSteer theo nguyên tử (ClearHandledSteer)
```

Bảo vệ sự cố là **cơ chế lưu trữ best-effort cho duy nhất một yêu cầu đang xử lý**: lần `SetPendingSteer` đầu tiên thất bại sẽ báo lỗi rõ ràng và dừng phán quyết, tuyệt đối không tiếp tục khi không có bản ghi khôi phục; giai đoạn phán quyết, hành động thất bại (giữ lại để phát lại), thoát bình thường/Abort (defer ghi lại các giao việc còn sót) đều được bảo vệ. Vẫn có hai cửa sổ mà không thể bảo đảm rõ ràng — giao việc đã chuyển vào hàng đợi thực thi trong bộ nhớ rồi bị hủy cứng (mức mili giây), và đầu vào đồng thời đang chờ interMu. Người dùng đang ở đây có thể nhận biết, chi phí gửi lại chỉ ở mức giây.

**Tầng lưu trữ của can thiệp dài hạn**: quy tắc về văn phong/chất lượng sáng tác từ hành động `rules` của phán quyết được `userrules.Service` chuẩn hóa vào snapshot quy tắc của cuốn sách, `novel_context` chèn `working_memory.user_rules` — có hiệu lực xuyên suốt quá trình nén và khởi động lại (xem chi tiết tại [Snapshot quy tắc người dùng](user-rules-runtime.md)). Các lối ra còn lại vốn đã được ghi vào store (độ dài/cốt truyện → giao việc architect, sửa chương cũ → editor xếp vào hàng đợi PendingRewrites, làm lại sau khi hoàn thành → reopen).

### 8.4 Điều khiển tiến triển chương

`ChapterAdvanceGate` thống nhất thực thi hai loại ý định người dùng ở hai thang thời gian khác nhau:

| Ý định | Nguồn | Ngữ nghĩa |
|---|---|---|
| `AdvanceMode=review` + permit chính xác | `/review on`, `/next` | Chính sách bền vững: mỗi chương mới tiến triển phải được cho phép riêng lẻ |
| `AdvanceHold` | Arbiter intervention | Ý định dùng một lần: tạm dừng sau ranh giới hiện tại, sau khi làm lại đã xả hết, hoặc sau khi chương mục tiêu được ghi ổn định |

Permit được gắn với số chương. Chỉ khi chương đích đã vào CompletedChapters, PendingCommit đã rỗng và tồn tại checkpoint commit thì permit mới được tiêu thụ; vì vậy, sự cố ở bất kỳ cửa sổ nào của saga commit cũng không thể dùng cùng permit đó cho chương kế tiếp. Các bất biến chi tiết xem tại [Chapter Advance Gate](chapter-advance-gate.md).

---

## 9. Cấu trúc thư mục

```
internal/
  domain/         Dữ liệu thuần: Phase / FlowState / Progress / Checkpoint / Scope / Story / Plan /
                  Review / StateChange / quy tắc chuyển Phase-Flow
  store/          Lưu trữ trên hệ thống tệp (tmp+rename + điều phối lũy đẳng; commit có sự thật giai đoạn Saga): progress / checkpoints / outline /
                  drafts / summaries / characters / world / signals / run_meta / runtime /
                  session / decisions(kiểm toán phán quyết)
  tools/          11 công cụ Agent, thao tác ghi dạng nguyên tử trên một tệp + lỗi rõ ràng + lũy đẳng; commit dùng thêm Saga bền vững
  flow/           Chiến lược định tuyến (hàm thuần + ranh giới IO): router.go (bảng quyết định Route) + state.go (LoadState)
                  + pause.go (phán quyết điểm dừng)
  arbiter/        Tầng phán quyết ngữ nghĩa (LLM-as-function): plan_start / intervention / failure(deadlock)
                  Cặp hàm Collect/Decide riêng cho từng kịch bản + loại Decision riêng cho từng kịch bản + kiểm tra cơ học
  agents/         build.go lắp ráp ba Worker (subagent.Runner, Engine gọi trực tiếp theo chương trình); ctxpack/ chiến lược nén ngữ cảnh của Writer
    guard/        subagent_guards.go (CheckpointDeltaGuard ×3, rào bảo vệ sự thật cho Worker)
  host/           host.go (vòng đời/điều phối can thiệp) + engine.go (vòng lặp thực thi tất định) + observer*.go
                  + events.go + usage*.go + budget.go + advance_gate.go + resume.go + cocreate.go
    imp/          Nhập bằng biên dịch ngữ nghĩa tiểu thuyết bên ngoài: ingest → segment → analyze → synthesize → publish (suy luận trạng thái thuần + LLM-as-function)
    exp/          Xuất các chương đã hoàn thành: TXT / EPUB 3; thuần chỉ đọc
  entry/          tui (Bubble Tea) / headless / startup
  bootstrap/      config + ModelSet + provider failover + trình hướng dẫn setup
  eval/           Đánh giá ngoại tuyến (A/B prompt/voice, hồi quy)
  diag/ errs/ models/ notify/ rules/ userrules/ stylestat/ ...

assets/
  prompts/        arbiter-plan-start / arbiter-intervention / arbiter-failure / architect-short|long
                  / writer(mẫu giao thức,placeholder {{VOICE}}) / editor / import-* / simulation-*
  voice.md        Tiêu chuẩn sáng tác (mặc định tích hợp của tầng văn phong; xem lớp phủ ba tầng tại docs/voice-layer.md)
  references/     Kỹ thuật viết + anti-ai-tone + mẫu thể loại, v.v.
  styles/         Mặc định/giả tưởng/ngôn tình/trinh thám (người dùng có thể ghi đè/thêm mới)

../agentcore     Khung Agent dùng chung (thư mục anh em trong go.work, có thể thêm năng lực dùng chung, không thêm nghiệp vụ)
../litellm       Cổng LLM
```

### 9.1 Các cột mốc phát triển

| Thời gian | Tái cấu trúc | Hiệu quả ròng |
|---|---|---|
| 2026-04-10 | `internal/orchestrator/` (6342 dòng) → `host/` + `agents/` | Lõi runtime -74% |
| 2026-04-20 | Hybrid Coordinator: tạo `host/flow/`, đưa định tuyến về hàm thuần | Tỷ lệ lỗi định tuyến tiệm cận 0 |
| 2026-05-02 | agentcore sửa chế độ suy nghĩ chậm/truyền phát; xóa bản vá chạy tiếp `idleResumeCount` | mimo / suy nghĩ chậm + truyền phát chạy thông |
| 2026-06-05 | Vòng đóng quy hoạch lăn + viết tiếp bằng suy luận ngược từ `/import` | 200+ chương chạy thông lần đầu |
| 2026-07-12 | **Thay thế mặt phẳng điều khiển bằng Engine + Arbiter**: vòng lặp dài của Coordinator và hệ sinh thái bảy bản vá bị loại bỏ; lớp phủ ba tầng cho tầng văn phong; củng cố qua năm vòng đánh giá đối kháng | Mỗi ranh giới tiết kiệm một lần chuyển tiếp LLM; mặt phẳng điều khiển 100% có thể kiểm thử ngoại tuyến; phán quyết ngữ nghĩa có thể phát lại |
| 2026-07-15 | **Pipeline biên dịch ngữ nghĩa `/import`**: các quy tắc cắt khối hard-coded bị loại bỏ, chuyển sang biên dịch theo từng giai đoạn ingest→segment→analyze→synthesize→publish; suy luận trạng thái thuần (`NextAction(Facts)`) + artifact được ràng buộc bằng vân tay đầu vào, có thể khôi phục và lũy đẳng xuyên suốt | Năng lực cắt khối tự nhiên mạnh lên theo năng lực mô hình; không trôi lệch phép liệt kê giai đoạn; có thể tiếp tục sau gián đoạn, mặt phẳng điều khiển kiểm thử ngoại tuyến |

Thực đo: hy3-preview free 12 chương / 73 phút, mimo-v2.5-pro 10 chương / 8.4 vạn chữ, đều chạy xong trong một lần; tiểu thuyết dài gpt-5.4《凡骨》235 chương / 127 vạn chữ chạy thông vòng đóng quy hoạch lăn (dữ liệu thời Coordinator, lần chạy đầu tiên thời Engine còn chờ bổ sung).

---

## 10. Những điều không làm

Vi phạm những điều này tức là kiến trúc đã lệch.

1. **Không đưa vào khái niệm Task / Job / WorkItem**. "Tác vụ hiện tại" mà UI hiển thị là phép chiếu của luồng sự kiện, không phải sự thật.
2. **Không phát minh bộ lập lịch thứ hai ngoài Route**. Mọi quyết định "bước tiếp theo giao cho ai" đều phải đi qua bảng quyết định Route (được đóng đinh bằng spec liệt kê đầy đủ) hoặc phán quyết của Arbiter (ghi xuống đĩa để kiểm toán); không được rải rác bằng các if-else phân phối.
3. **Không làm cơ chế "tiếp tụng chạy khi rảnh"**. Vòng lặp Engine kết thúc = Host chuyển sang trạng thái cuối; cách duy nhất để nó chạy lại là người dùng `Continue` hoặc khởi động lại `Resume`.
4. **Không thêm kỷ luật hành vi vào prompt**. Nếu cần hàng rào hành vi thì điều đó cho thấy đã phân tầng sai — bất biến thuộc về điều kiện tiên quyết của tool, phán đoán thuộc về Arbiter, quy trình thuộc về Route.
5. **Không thêm bản vá tự động tiếp tụng chạy cho Host khi dừng bất thường**. `idleResumeCount` từng có trong lần chạy dài duy nhất thực sự được kích hoạt đã 100% không cứu được tình huống, mà còn che lấp nguyên nhân thật ở tầng agentcore (xem chi tiết `feedback_no_host_resilience.md`).
6. **Không suy luận tác vụ đã hoàn thành dựa trên "tool exec end"**. Bằng chứng duy nhất của việc hoàn thành là checkpoint được ghi.
7. **Không làm mô hình bốn tầng như WorkflowInstance / Command + Apply**. Tầng sự thật chỉ có Progress + Checkpoint + Artifact.
8. **Không hỗ trợ Worker song song**. Một vòng lặp Engine đang hoạt động, một cuốn sách tiến triển tuần tự. Với nhiều tiểu thuyết, hãy dùng nhiều tiến trình.
9. **Không thực hiện lệnh gọi LLM ở tầng tool** (trừ chính tool Agent). Chỉ IO thuần + kiểm tra hợp lệ + tính lũy đẳng.
10. **Không để UI đọc trực tiếp Store**. Chỉ được đăng ký sự kiện hoặc đọc `Snapshot()` của Host.
11. **Không viết máy trạng thái Flow ở phía Host**. Nhãn Flow chỉ được cập nhật bởi tool; Route chỉ đọc, không ghi.
12. **Không viết mã cứng dự phòng cho "ảo giác LLM"**. Hãy tối ưu prompt, cải thiện giá trị trả về của tool, và làm cho novel_context trình bày sự thật rõ ràng hơn.
13. **Không để diag / tầng quan sát can thiệp vào luồng điều khiển**. Chẩn đoán chỉ đọc; tuyệt đối không tự động sửa / tiếp tụng chạy / đổi quy trình.
14. **Chính sách ngân sách và tiến trình chương không đi vào tầng Route/tool**. `BudgetSentinel` / `ChapterAdvanceGate` là các thành phần chính sách ở ranh giới Engine (thực thi chỉ dẫn người dùng đã ký trước, không đánh giá hành vi văn chương); `notify` thuần túy quan sát.
15. **Mọi thay đổi ở mặt phẳng điều khiển phải sửa spec liệt kê đầy đủ trước rồi mới sửa hiện thực**; **trước khi bump agentcore phải vượt qua kiểm thử hợp đồng**.
16. **Không làm DSL workflow tổng quát, event sourcing, State Digest toàn cục**. Route là một bảng cho một miền; khái quát hoá chính là thiết kế thừa.

---

## 11. Chiến lược xác minh

### 11.1 Danh mục tài sản kiểm thử

| Tầng | Tài sản | Độ phủ |
|---|---|---|
| Spec mặt phẳng điều khiển | `flow/router_exhaustive_test.go` | Kiểm kê đầy đủ 12 vạn tổ hợp của bảng quyết định Route + tính chất hàm thuần/tính tất định/tính bảo toàn |
| Hợp đồng framework | `agents/agentcore_contract_test.go` | 5 giả định hành vi của agentcore, được kích hoạt qua `Runner.Run` (bắt buộc chạy trước khi nâng cấp) |
| Engine end-to-end | `host/engine_test.go` | fake model + tool thật: viết trọn một cuốn sách / phán quyết thất bại / phán quyết bế tắc / thứ tự nghiệm thu làm lại / boundary hold dừng ngay / bảo toàn khi tranh chấp lúc thoát / một giấy phép một chương |
| Phán quyết | `arbiter/arbiter_test.go` | Phân tích / thử lại dựa trên phản hồi / ma trận kiểm tra theo từng kịch bản / thu thập sự thật |
| Hợp đồng pipeline sự thật | kiểm thử store/tools | Bể phản hồi vượt qua các lần khởi động lại, bản ghi vi phạm latest-wins / viết lại xóa sạch / chèn novel_context, PlanStart được giữ lại qua Init |
| Tầng văn phong | `assets/load_test.go` | Tách ra giống hệt từng byte / ngữ nghĩa ghi đè ba tầng / eval dùng chung đường dẫn lắp ráp |
| Chất lượng ngữ nghĩa | `internal/eval` + decisions.jsonl | A/B prompt/voice, phát lại phán quyết ngoại tuyến (tập hồi quy đang được xây dựng) |

### 11.2 Kịch bản ổn định

- **A Chạy dài**: 80~200 chương chạy trọn một lần, Phase=complete. Cho phép provider failover, thử lại; cấm mọi hành vi tự động tiếp tụng chạy.
- **B Khôi phục sau sự cố**: kill tiến trình sau bất kỳ step nào → Resume → Route tiếp tụng chạy từ sự thật, không viết lại sản phẩm đã ghi xuống đĩa, các điểm kiểm tra không có step trùng lặp. Sự cố trong giai đoạn quy hoạch đi qua PlanStartRecord.
- **C Provider dao động**: 503 xen kẽ → litellm failover, Worker không nhận biết.
- **D Can thiệp của người dùng**: Steer khi đang chạy → phán quyết hiển thị lại trong vài giây, hành động được commit tại ranh giới; Steer khi đã dừng → sau khi phán quyết thì khởi chạy lại theo ý định; sự cố → phát lại PendingSteer.

### 11.3 Tuân thủ (có thể viết thành linter / test)

- `flow.Route` phải là hàm thuần: cấm đọc Store / bất kỳ IO nào
- Trong thân hàm `runEnded` không được xuất hiện bất kỳ lệnh gọi khởi chạy engine nào
- Mỗi kịch bản phán quyết mới phải thêm theo cặp Collect/Decide + kiểu Decision + ghi xuống đĩa
- Code liên quan đến recovery chỉ được xuất hiện trong `host/resume.go` và `engine.planStartFallback`

### 11.4 Lặp lại cải thiện chất lượng

Đổi văn phong → sửa `<thư mục sách>/style/` (cấp người dùng) hoặc assets/voice.md (tích hợp sẵn), kiểm chứng bằng tập đánh giá văn phong theo A/B; thêm tiêu chí thẩm định → sửa editor.md (save_review tiếp nhận có cấu trúc); thêm tài liệu tham chiếu → nối dây tường minh ở ba chỗ (`tools.References` + `loadReferences` + ánh xạ chèn novel_context).

**Thống kê văn phong cấp sách (`internal/stylestat`)**: Host tạo một `StyleStatsIndex` duy nhất cho mỗi cuốn sách và chèn tường minh vào `novel_context` cùng `commit_chapter`. Lần khởi chạy đầu tiên khôi phục chỉ mục từ toàn bộ các chương đã hoàn thành, sau đó cập nhật tăng dần cho các chương mới thêm/viết lại (mẫu câu / cụm từ tần suất cao / câu lặp qua các chương / hình thức cuối chương), tái sử dụng snapshot ở trạng thái sách không đổi và chèn vào `episodic_memory.style_stats`: editor phán quyết theo số liệu, writer tự tránh dựa trên đó. Eval ngoại tuyến vẫn có thể gọi trực tiếp hàm thuần `Compute`. **Thống kê thuộc về code, phán quyết thuộc về LLM**.

---

## 12. Tổng kết

> **Tầng sự thật xác định, tầng ngữ nghĩa tự chủ.** Model tự do ở những nơi không thể xác minh (viết cái gì, viết thế nào, phán đoán thế nào), và bị ràng buộc ở những nơi có thể xác minh (thứ tự, tính lũy đẳng, giai đoạn).

Không có task queue, không có policy engine, không có phiên thường trú. Chỉ có:

- Một vòng lặp Engine tuần tự và tất định (~500 dòng, sáu đường dẫn end-to-end được đóng đinh)
- Một bảng quyết định Route (hàm thuần, spec liệt kê đầy đủ 12 vạn tổ hợp)
- Bốn hàm phán quyết của Arbiter (sự thật đi vào, quyết định có cấu trúc đi ra, ghi xuống đĩa để phát lại)
- Ba loại Worker theo chức năng (context độc lập với model, hàng rào sự thật không gây gián đoạn)
- 11 tool nguyên tử trong một tệp, phục hồi tường minh/lũy đẳng xuyên tệp; trong đó commit dùng Saga bền vững + một tệp checkpoint jsonl

Lợi ích của việc nâng cấp model chảy đi đâu là thấy ngay: sáng tác tốt hơn (toàn bộ đầu ra của Người viết/Kiến trúc sư/Biên tập viên), phán quyết chính xác hơn (bốn kịch bản của Arbiter), tóm tắt tốt hơn (ctxpack) — chỉ cần đổi model là có được, không sửa một dòng nào của lớp vỏ. Mặt phẳng điều khiển không hưởng lợi ích từ model, bởi vì **tra bảng không cần trí tuệ**; nó cần được chứng minh là đúng, mà điều đó đã được chứng minh rồi.

Tính cứng nhắc của quy trình là có chủ đích, đã được định giá và để mở cửa: muốn nới lỏng thứ tự tool của writer → nới một đoạn prompt giao thức (bất biến được hỗ trợ ở tầng tool); muốn phân phối theo cung → thêm một nhánh vào Route; muốn mở rộng năng lực phán quyết → thêm một cặp Collect/Decide. Mỗi lần nới lỏng đều có trọng tài (spec liệt kê đầy đủ, đánh giá văn phong, phát lại decisions) — **dùng bằng chứng để quyết định độ dài dây cho model, thay vì dùng niềm tin**.

Kỷ luật duy nhất: **khi ai đó muốn thêm một điểm quyết định, trước hết hãy đi qua phép chia ba — có thể liệt kê thì đưa vào Route, ranh giới rõ ràng thì đưa vào Arbiter, mở rộng thì đưa vào Worker**. Quyết định nào không thuộc cả ba, hãy nghĩ lại xem nó có thật sự tồn tại không.
