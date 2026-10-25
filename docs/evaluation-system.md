# Hệ thống đánh giá ainovel-cli

> Đánh giá không phải viết lại một bộ script kiểm tra mới, mà lấy **trình chẩn đoán sự thật (`diag`), bộ thống kê văn thể toàn cuốn (`stylestat`), đánh giá bản địa bảy chiều (`ReviewEntry`)** hiện có của dự án làm trình đánh giá, bao thêm một harness xử lý batch ngoại tuyến. Một định nghĩa sự thật, hai nơi không còn trôi lệch.

---

## 0. Tại sao cần thiết kế lại

ổn định đã chạy được: viết xong một lần truyện dài 235 chương / 1,27 triệu chữ, vòng quy hoạch lặp lại đã khép lại (xem `architecture.md` §9.1). Điểm nghẽn đã chuyển —— **chất lượng đã có thể lặp lại**:

- Sau khi thay đổi một prompt, quy trình vẫn ổn định không? Chuỗi công cụ, đẩy trạng thái, sự thật đã lưu trữ vẫn chính xác không?
- Chất lượng nội dung, đề cương, đánh giá đã thực sự cải thiện, hay chỉ lần này ngẫu nhiên đạt kết quả tốt?
- Trong truyện dài, nhân vật, dòng thời gian, phục bút, ngữ cảnh có liên tục và đáng tin cậy không?
- **Văn thể cố định toàn cuốn** (mẫu câu tic trung bình mấy chục lần mỗi chương, hình thức đồng nhất cuối chương, đọc lại từng chữ xuyên chương) có trở tốt hay trở xấu không? Đây là thủ phạm thực sự của thực chứng 6.5/10 trên 196 chương, đánh giá một chương vốn mù lòa với nó.

Hiện tại các phán đoán này dựa vào "cảm giác + đọc mẫu thủ công". Hệ thống đánh giá cần biến việc thay đổi prompt từ dựa vào cảm giác thành quy trình kỹ thuật **có hồi quy, có bằng chứng, có đọc mẫu thủ công**.

Nhưng dự án này không cần, cũng không nên sao chép nền tảng đánh giá (eval) thông dụng của ngành (dataset / experiment / scorer / cơ sở dữ liệu / Web UI). Lý do đơn giản: **lõi của những khả năng này——kiểm tra xác định và tín hiệu chất lượng——đã có trong dự án, viết bằng Go và chia sẻ cùng một mô hình sự thật với thời gian chạy**.

---

## 1. Nhận định chính: trình đánh giá đã tồn tại

Hệ thống đánh giá có bốn loại trình đánh giá, ba loại trong số đó đã được triển khai trong kho mã, chỉ đơn thuần chưa từng được gọi là "trình đánh giá":

| Trình đánh giá | Khả năng hiện có của dự án | Điểm vào | Đầu ra |
|---|---|---|---|
| **Chẩn đoán sự thật xác định** | một tập hợp quy tắc của `internal/diag` + quy tắc thời gian chạy | `diag.Diagnose(store)` | `Report{Stats, Findings}`, Finding đi kèm Severity/Evidence |
| **Văn thể hồi quy toàn cuốn** | `internal/stylestat` | `stylestat.Compute(input)` | mẫu câu trung bình mỗi chương, câu lặp từng chữ xuyên chương, tỷ lệ câu ngắn cuối chương, trộn lẫn định dạng tiêu đề |
| **Phán quyết chất lượng (rubric)** | rubric có phiên bản (bản ban đầu phát sinh từ bảy chiều của `editor.md`) | LLM Judge (đo thang cố định làm A/B) | consistency/character/pacing/continuity/foreshadow/hook/aesthetic |
| **Xuất xương rang hành vi** | xuất của `internal/diag` | `diag.WriteExport(store, rep, rc)` | xương rang hành vi, dùng cho đọc mẫu thủ công và lưu trữ |

`diag.Analyze(s *store.Store)` nhận một Store và có thể sản xuất toàn bộ `Report`——**nó vốn có thể chạy ngoại tuyến trên bất kỳ thư mục sản xuất nào**. `stylestat.Compute` là hàm thuần. Điều này có nghĩa là hệ thống đánh giá không phải triển khai lại "chương đã được lưu xuống đĩa hay chưa, progress đã được đẩy tiếp hay chưa, checkpoint đã tồn tại hay không, có cặn pending sót hay không, quy trình có kẹt vòng vô hạn không"——các quy tắc này diag đều đã làm, và mỗi quy tắc đều tương ứng với một bẫy thực từng từng vấp (`PhaseFlowMismatch`, `OrphanedSteer`, `OutlineExhausted`, `repeatedErrors`/`stuckStep` tương ứng với `idleResume` / livelock do cạn đề cương / công cụ gọi in ra như văn bản, v.v. các lỗi lịch sử).

> **Nhiệm vụ của hệ thống đánh giá không phải tạo kiểm tra, mà là: điều khiển batch + chạy các trình đánh giá hiện có trên sản xuất + ánh xạ Finding/thống kê thành cổng kiểm soát + tổng hợp báo cáo**.

---

## 2. Nguyên tắc thiết kế

### 2.1 Trình đánh giá là trình chẩn đoán, tuyệt đối không triển khai lại kiểm tra xác định

Kiểm tra xác định chỉ gọi `diag.Diagnose`, không trong tầng đánh giá giải mã lại `progress.json` / `checkpoints.jsonl` / `sessions/*.jsonl`. Lý do là luật DRY sắt của dự án: **"trạng thái hợp lệ là gì" chỉ được có một định nghĩa.** Nếu đánh giá dùng Python giải mã lại checkpoint để kiểm tra commit có thiếu hay không, thì đã có hai định nghĩa "commit hoàn thành", khi thời gian chạy thay đổi quy tắc diag mà đánh giá không theo thì cổng kiểm soát lập tức sai lệch.

→ harness đánh giá dùng **Go**, gọi in-process `diag` và `stylestat`, chia sẻ `internal/domain` và `internal/store` với thời gian chạy. Đây là khác biệt căn bản nhất của thiết kế này so với phiên bản trước.

### 2.2 Văn thể hồi quy toàn cuốn là tín hiệu chất lượng hàng đầu

LLM Judge một chương xem mỗi chương đều "bình thường", nhưng điểm nghẽn chính là sự cố định xuyên chương. Do đó cột chắn xác định của chất lượng hồi quy là `stylestat`, không phải LLM Judge.

**Điều kiện tiên quyết: `stylestat.Compute` ít hơn 5 chương trả về nil trực tiếp** (`stylestat.go` `minChapters=5`, mẫu quá nhỏ tần số vô nghĩa). Do đó văn thể hồi quy **chỉ có hiệu lực ở tầng Quality / Longform từ 5 chương trở lên**, Smoke một chương không nhận được tín hiệu văn thể——điều này quyết định chi phí và chiến lược mặc định dưới đây. Chỉ số bao gồm：

- số lần mẫu câu của variant trung bình mỗi chương so với baseline (`patterns[].per_chapter`)
- tỷ lệ kết thúc bằng câu ngắn ở cuối chương (`ending.short_ratio` gần 1 là bệnh)
- số câu lặp từng chữ xuyên chương (`repeated_sentences`)
- trộn lẫn định dạng tiêu đề (`title_formats`)
- tỷ lệ từ chỉ thời gian ở mở đầu (`opening_time_rate`)

Đây là chỉ số không tốn chi phí LLM, xác định, và trực tiếp đánh vào điểm nghẽn chất lượng. **LLM Judge là bổ sung, stylestat delta là đường chủ đạo**.

### 2.3 LLM Judge căn chỉnh theo rubric bản địa bảy chiều, không thiết lập lò riêng

Judge không sáng tạo dimensi đánh giá mới——dimensi chặt chẽ bằng bảy mục của `domain.DimensionScore`, so sánh baseline/variant.

**Nhưng rubric phải được phiên bản hóa, có thể đóng băng**, lưu trữ dưới dạng ảnh chụp nhanh `evals/rubrics/*.json`, không đọc `editor.md` theo thời gian thực khi chạy. Lý do: khi đối tượng được kiểm tra chính là `editor.md` bản thân, nếu thẩm phán thay đổi cùng với `editor.md`, tiêu chuẩn đánh giá sẽ trôi lệch——thẩm phán và đối tượng được kiểm tra cùng nguồn sẽ không phán đoán được "sửa editor thì tốt hay xấu". Vì vậy rubric ban đầu **phát sinh** từ bảy chiều của editor (đảm bảo đường kính nhất quán), sau đó **độc lập tiến hóa, bump phiên bản rõ ràng**; report ghi nhận dùng phiên bản rubric nào.

### 2.4 Finding xác định quyết định cổng kiểm soát, LLM và con người chỉ thực hiện phán quyết chất lượng

Căn chỉnh luật sắt kiến trúc "thống kê quy cho mã, phán quyết quy cho LLM":

- **Chỉ bằng chứng xác định có thể chặn việc hợp nhất**: Finding `SevCritical` của `diag`, case tuyên bố thất bại khẳng định hợp đồng.
- **LLM Judge và đọc mẫu thủ công sinh ra warning và đường gợi ý xếp hạng**, không tự mình quyết định hợp nhất.
- Một câu: `Finding.Severity` ánh xạ trực tiếp cấp độ cổng kiểm soát, không giới thiệu hệ phân loại mức độ nghiêm trọng mới.

### 2.5 Đánh giá chỉ quan sát, không can thiệp luồng điều khiển

Đánh giá tái sử dụng `diag`, nhưng **loại bỏ `Action` và `Planner` của diag**——đó là những thứ của luồng điều khiển thời gian chạy. Trong ngữ cảnh đánh giá, `diag.Report` chỉ lấy `Stats` và `Findings`, Action đều bị bỏ qua. Đánh giá không tự động sửa prompt, không tự động hoàn tác, không chạy tiếp. Đây là mở rộng của kỷ luật người quan sát (`architecture.md` §2.3) trong ngữ cảnh đánh giá.

### 2.6 Thất bại hiển thị rõ ràng

Không mô phỏng thành công, không nuốt lỗi, không dùng mẫu để giả vờ qua. Nếu một trong số mô hình, công cụ, cấu hình, hệ thống tệp, phân tích, judge thất bại, báo cáo ghi rõ nguyên nhân. **Thất bại bản thân là kết quả đánh giá**——một case chạy hỏng, cổng kiểm soát là FAIL, không phải "bỏ qua".

### 2.7 Mỗi lần chỉ xác thực một biến

Ràng buộc cứng A/B: cùng yêu cầu, cùng cấu hình, cùng mô hình/provider, cùng phong cách, cách ly thư mục đầu ra. Baseline = prompt chính thức hiện tại, Variant = chỉ thay tệp prompt cần xác thực lần này. Một lần thí nghiệm không đồng thời sửa Writer/Architect/Editor/Arbiter.

---

## 3. Toàn cảnh kiến trúc

```text
[Cases]  evals/cases/*.json —— bộ khẳng định tầng sự thật, không phải dòng dataset chung
   │
[Runner]  internal/eval —— lắp ráp host điều khiển in-process (cắt dừng theo giới hạn số chương), ghi đè bundle.Prompts trong bộ nhớ để tạo variant
   │       baseline run ┐
   │       variant  run ┘  mỗi thư mục output được cách ly
   ▼
[Collectors]  thu thập trên mỗi thư mục sản xuất：
   ├── diag.Diagnose(store)      → Report{Stats, Findings}      (sự thật + thời gian chạy)
   ├── stylestat.Compute(input)  → thống kê văn thể toàn cuốn                 (cột chắn chất lượng hồi quy)
   ├── case khẳng định hợp đồng     → kỳ vọng checkpoint/phase/công cụ hợp đồng (diag không bao quát)
   ├── usage / cost / token      → đọc từ meta/usage.json
   └── tool_calls                → đọc từ meta/sessions/*.jsonl các cuộc gọi công cụ thực tế
   ▼
[Graders]
   ├── cổng kiểm soát xác định：Finding.Severity + khẳng định hợp đồng → hard_fail / regression
   ├── stylestat delta：chênh lệch chỉ số văn thể variant so với baseline
   ├── LLM Judge（tùy chọn）：so sánh A/B rubric bảy chiều
   └── Human：đọc thủ công sản phẩm baseline/variant
   ▼
[Report]  report.json (dành cho máy) + report.md (dành cho người) + xuất xương rang hành vi
   └── Cổng: PASS / WARN / FAIL
```

Hướng phụ thuộc: `eval → host → agents → tools → store → domain`, tái sử dụng ngang `diag` / `stylestat`. Tầng đánh giá **không phụ thuộc ngược** luồng điều khiển thời gian chạy, chỉ đọc Store và trình đánh giá chỉ đọc.

> **Triển khai hiện tại bao phủ đường chắn xác định**: khi không có `--variant` thì là `mode=single`; truyền `--variant` thì là `mode=ab`, cùng một case cách ly chạy baseline và variant, và sinh ra delta. Collectors đã kết nối `diag.Diagnose`, case khẳng định hợp đồng, `stylestat.Compute`, `meta/usage.json`, đếm cuộc gọi công cụ session; Graders đã kết nối cổng kiểm soát xác định, delta diag baseline/variant, delta cost/token/tool call, delta stylestat. Runner trực tiếp lắp ráp `host.New` và tự mang theo cắt dừng theo giới hạn số chương, **không tái sử dụng `headless.Run` không giới hạn số chương**.LLM Judge và Human vẫn là tầng tùy chọn sau này, không tham gia cổng kiểm soát xác định hiện tại.

---

## 4. Tại sao dùng Go in-process, không phải shell + Python

| Chiều | shell sao chép mã nguồn + Python phân tích (lối cũ) | Go in-process (thiết kế này) |
|---|---|---|
| Kiểm tra xác định | Python giải mã lại JSON, hai định nghĩa với quy tắc diag | gọi trực tiếp `diag.Diagnose(store)`, một định nghĩa |
| Thay variant | sao chép toàn bộ cây mã nguồn + `go build` lại hai binary | lắp ráp host sau khi ghi đè `bundle.OverridePrompt(...)` trong bộ nhớ, không sao chép không biên dịch lại |
| Văn thể hồi quy | cần viết lại logic chia câu tiếng Trung của stylestat bằng Python | gọi trực tiếp `stylestat.Compute` |
| Rubric của Judge | dimensi rải rác trong Python | tái sử dụng `domain.DimensionScore`, cùng nguồn với hệ thống trực tuyến |
| Rủi ro trôi lệch | cao: thời gian chạy thay đổi mô hình sự thật, đánh giá không theo | thấp: sự thay đổi trường bị lộ tại thời điểm biên dịch |

Lại `prompt_ab.sh` mà phải sao chép mã nguồn và biên dịch lại, là vì prompt được nhúng vào binary (`go:embed`). Nhưng `assets.Bundle.Prompts` là cấu trúc thông thường, **runner chỉ cần thay đổi một trường trong bộ nhớ để tạo variant**, chẳng cần sao chép mã nguồn. Đây là sự đơn giản hóa lớn nhất đi kèm khi viết harness bằng Go.

> **Hạn chế triển khai**: `assets.Load` thông qua `loadPrompts` đưa prompt Worker (architect/writer/editor) cùng nhau thêm hậu tố `WithSimulationGuidance`. Nếu variant chỉ điền văn bản thô vào `bundle.Prompts.Writer`, thì mất đi hậu tố ảnh mô phỏng văn phong của baseline, A/B không tương đương.
>
> Cách làm đúng là ghi đè qua `assets.OverridePrompt`, bên trong đi cùng loại gói bọc hoàn toàn như `Load`; eval không sao chép logic gói bọc.

> Phiên bản trước giữ `prompt_ab.sh` / `prompt_ab_report.py` và "trích xuất khả năng dần dần". Thiết kế này từ bỏ con đường này: vấn đề chúng giải quyết (chạy cách ly + tổng hợp chỉ số) là tập con trong harness Go in-process, ép dùng lại thì trở nặng với keo dán giao diện ba ngôn ngữ shell/Python/Go. **Harness Go là đường chính duy nhất**; harness Go hiện tại đã bao phủ chạy cách ly baseline/variant, tổng hợp lặp lại và delta xác định. Các script cũ (`scripts/prompt_ab.sh`, `scripts/prompt_ab_report.py`) cùng sách thao tác `docs/prompt-ab.md` đã bị xóa bỏ khi thiết kế này được triển khai, không còn giữ lại.

---

## 5. Biểu kê ca kiểm tra

Ca kiểm tra là đơn vị nhỏ nhất của đầu vào đánh giá, cũng là một nhóm **khẳng định thuộc tầng sự thật**. Dùng JSON để mô tả, tránh các quy tắc rải rác trong các tham số dòng lệnh.

```json
{
  "id": "writer_first_chapter_xianxia",
  "category": "smoke",
  "role": "writer",
  "description": "验证 Writer 第一章正文质量与工具链稳定性",
  "prompt": "写一本修仙长篇，主角从边城杂役起步，靠异常记忆破解宗门旧案卷入长生局。",
  "style": "fantasy",
  "max_chapters": 1,
  "target_prompts": ["writer.md"],
  "rubric": "writer_chapter",

  "expect": {
    "phase": "writing",
    "min_completed_chapters": 1,
    "required_checkpoints": ["chapter:1:plan", "chapter:1:draft", "chapter:1:commit"],
    "no_pending": ["pending_commit", "pending_steer"]
  },

  "gate": {
    "max_severity": "warning",
    "max_cost_delta_ratio": 0.3,
    "max_tool_call_delta_ratio": 0.3,
    "stylestat_regression": "warn"
  }
}
```

**Ngữ nghĩa trường**:

- `expect`: khẳng định hợp đồng cấp case, **chỉ khai báo những mong đợi mà quy tắc chung của diag chưa bao phủ và gắn liền với case này** (ví dụ "ca smoke này phải tạo ra đúng chapter:1:commit"). Những điều chung như "không còn pending dư thừa / luân phiên pha nhất quán / không có khoảng trống chương" giao cho diag, không khai báo lại trong case.
- `category`: tầng đánh giá ∈ `smoke` / `workflow` / `quality` / `longform` / `recovery` / `steering`. Quyết định bộ cổng nào được chạy và stylestat/Judge có được bật mặc định hay không.
- `role`: vai trò được kiểm tra ∈ `writer` / `architect` / `editor`. Hai chiều này độc lập——tầng quyết định "kiểm tra đến độ sâu nào", vai trò quyết định "kiểm tra Worker nào". Tầng Workflow chọn tập khẳng định theo `role`.
- `max_severity`: mức nghiêm trọng tối đa mà diag Finding cho phép. Vượt quá là hard fail.
- `gate.max_cost_delta_ratio` / `gate.max_tool_call_delta_ratio`: ngưỡng tăng chi phí và số lần gọi công cụ của variant so với baseline; mặc định là `0.3` khi bị bỏ qua, giá trị rõ ràng `0` nghĩa là không cho phép tăng, giá trị âm nghĩa là tắt delta gate này.
- `rubric`: bảng chấm điểm LLM Judge phiên bản nào được bật. Mặc định thì không chạy Judge.
- `gate.stylestat_regression`: `block` / `warn` / `off`, điều khiển việc suy lui văn phong có bị chặn hay không (chỉ có hiệu lực với case có ≥5 chương).

---

## 6. Các tầng đánh giá

Mỗi tầng xác định rõ **đánh giá viên sẵn có nào được dùng**, tránh "tầng đánh giá tự viết lại bản phán xét".

### 6.1 Smoke (bắt buộc chạy mỗi lần prompt thay đổi, tập nhỏ nhất)

Chỉ xem hệ thống có chạy ổn định hay không, không đánh giá văn phong. 1 chương / giai đoạn lập kế hoạch là đủ để phát hiện.

| case | Mục tiêu | Đánh giá viên chính |
|---|---|---|
| `writer_first_chapter` | Writer hoàn thành chương 1 và commit | `expect.required_checkpoints` + diag |
| `architect_short` | Lập kế hoạch ngắn lưu đầy đủ premise/outline/characters/world_rules | diag `MissingSummaries` (kiểm tra foundation cùng nguồn) + `expect` |
| `architect_long` | Lập kế hoạch dài lưu layered_outline/compass, khai triển cung đầu | diag `OutlineExhausted`/`CompassDrift` + `expect` |
| `editor_review` | Editor lưu review tại điểm đánh giá (đủ bảy chiều) | khẳng định trường `ReviewEntry` |

Chi phí: 1 chương × baseline+variant, từ giây đến phút, không bật Judge, không chạy stylestat (số chương < 5, `Compute` trả về nil). CI mặc định chỉ chạy tầng này.

### 6.2 Workflow (xác minh hành vi agent phù hợp hợp đồng kiến trúc)

**Quan trắc then chốt: khẳng định hợp đồng, không khẳng định chuỗi công cụ chính xác.** Kiến trúc đặt cược vào quy trình ra quyết định tự chủ của LLM (`architecture.md` §2.1). Cố định thứ tự công cụ đưa vào tầng đánh giá việc viết mã sẵn cho hành vi LLM —— thứ bị §10.13 bác bỏ. Vì vậy ở đây chỉ khẳng định **thật nhất định**:

- Writer: tồn tại checkpoint `chapter:N:commit`; sau commit, sub-agent kết thúc lượt này (không có văn bản chính đuôi dài quá độ); checkpoint draft đứng trước commit. **Không** khẳng định "bắt buộc phải đúng thứ tự novel_context→read_chapter→plan→draft→check→commit".
- Architect: outline giai đoạn sáng tác chỉ thêm mới, không ghi đè toàn bộ (checkpoint của `expand_arc`/`append_volume`, không có lần ghi đầy đủ `layered_outline` thứ hai); sau khi khai triển, outline phẳng và layered có số chương nhất quán.
- Editor: `ReviewEntry.Verdict` hợp lệ (accept/polish/rewrite); rewrite/polish phải tạo ra các chương bị ảnh hưởng; cuối cung có checkpoint `arc_summary`, cuối tập có checkpoint `volume_summary`.
- Engine phân phối: lệnh Route khớp với Worker thực sự chạy (đọc từ session trace, diag `repeatedErrors` ngăn chặn vòng lặp); phán quyết ngữ nghĩa đối chiếu `meta/decisions.jsonl`.

Hầu hết được quy tắc diag + khẳng định checkpoint bao phủ trực tiếp; phần ít (văn bản chính đuôi sau commit) cần bổ sung một lần kiểm tra trace nhẹ trong collector.

### 6.3 Quality (chỉ chạy sau khi quy trình đạt, đánh giá chất lượng nội dung)

Hai bước:

1. **stylestat delta (xác định, chủ đường)**: chênh lệch chỉ số văn phong của variant so với baseline. Đây là bằng chứng cứng về suy lui chất lượng. **Yêu cầu case chạy đủ ≥5 chương** (nếu không `Compute` trả về nil, mục này gắn nhãn `insufficient_sample`), nên case Quality thuần 1 chương không nhận được kết quả suy lui văn phong, cần đặt `max_chapters` trên 5.
2. **LLM Judge (phụ trợ)**: rubric A/B bảy chiều (xem §8).

Chỉ có case vượt qua §6.1/§6.2 mới vào Quality — quy trình chưa đúng thì nói về chất lượng cũng vô nghĩa.

### 6.4 Longform & Recovery (thay đổi lớn / nightly)

Không cần chạy mỗi lần. Nó bao phủ ổn định dài hạn và khả năng khôi phục — chính là sân chủ của quy tắc runtime diag và quy tắc ngữ cảnh:

- Viết liên tiếp 3 / 5 chương đầu → diag `GhostCharacter`/`TimelineGaps`/`RelationshipStagnation`/`ChapterGaps` + stylestat trùng lặp xuyên chương.
- Đánh giá cuối cung + khai triển cung sau → `OutlineExhausted`/`StaleForeshadow`/`CompassDrift`.
- Can thiệp của người dùng giữa chừng (case steering) → user_rules có được ghi vào `meta/user_rules.json` không, và có được các chương sau tuân thủ không.
- Khôi phục sau khi bị kill: chạy đến draft chương N rồi kill → Resume → diag xác nhận `checkpoints.jsonl` không có step trùng lặp, không ghi đè draft đã lưu xuống đĩa, `pending_commit` cuối cùng về 0.
- Sử dụng công cụ phình to / ngoại lệ chi phí → diag `repeatedErrors`/`stuckStep`/`streamIdleStorm` + chênh lệch mức sử dụng.

---

## 7. Cổng kiểm tra xác định

Cấp độ cổng kiểm tra được suy trực tiếp từ **Severity của diag Finding** + **khẳng định hợp đồng của case**, không thiết lập phân loại riêng.

### 7.1 Hard Fail (chặn hợp nhập)

- Tiến trình panic / headless trả về error.
- diag xuất ra Finding `SevCritical` (`InvalidPendingRewrites` / `PhaseFlowMismatch` v.v.).
- Khẳng định hợp đồng `expect` của case thất bại: thiếu checkpoint commit, phase chưa đạt kỳ vọng, pending đã khai báo chưa về 0.
- Số lỗi / số Finding critical của variant nhiều hơn so với baseline (suy lui xuống mức tồi tệ hơn).

### 7.2 Regression (mặc định warning, việc chặn do case gate quyết định)

- diag mới thêm Finding `SevWarning` (variant nhiều hơn baseline).
- Mức tăng của tool calls / cost / input token / output token vượt ngưỡng của case (mặc định 30%).
- **Suy lui stylestat**: tần suất mẫu câu trung bình mỗi chương tăng, tỉ lệ câu ngắn cuối chương tăng, câu trùng lặp xuyên chương nhiều hơn, tiêu đề xen kẽ xuất hiện — theo `gate.stylestat_regression` quyết định warn/block.
- Số từ chương dưới 60% hoặc trên 180% so với baseline (ngưỡng cùng nguồn của diag `WordCountAnomaly`).

### 7.3 Quality Gate (đệ cuối thủ công)

- LLM Judge chỉ hỗ trợ và xếp hạng.
- Judge đánh giá variant rõ yếu hơn → phải đọc mẫu thủ công để xác nhận.
- Đọc mẫu thủ công xác nhận suy thoái → chặn.
- Judge đánh giá variant tốt hơn nhưng xác định có hard fail → vẫn chặn.

### 7.4 Điều kiện đề xuất hợp nhập

Thay đổi prompt thường ngày: Smoke toàn phần đạt + Workflow của vai trò mục tiêu toàn phần đạt (Smoke 1 chương không chứa suy lui văn phong; nếu đã chạy case Quality ≥5 chương, thì stylestat không có suy lui rõ rệt).
Thay đổi lớn: thêm 2-3 case Quality + 1-2 case Longform + đọc mẫu thủ công.

---

## 8. LLM Judge

Judge là công cụ hỗ trợ chất lượng, bản chất là **so sánh baseline/variant ngoài tuyến bằng rubric có phiên bản (bản ban đầu phát sinh từ `editor.md` bảy chiều)**. rubric là thước đo cố định, độc lập với sự phát triển của `editor.md` trực tuyến (lý do xem §2.3), report ghi lại phiên bản rubric được sử dụng.

### 8.1 Đầu vào (kiểm tra kích thước, chớ nhét cả cuốn sách)

- Yêu cầu gốc của người dùng + dàn ý/hợp đồng chương hiện tại.
- **Văn bản chính của cùng một chương** trong baseline và variant.
- Tóm tắt 1-2 chương gần nhất + tóm tắt trạng thái nhân vật (đọc từ store).
- Các lát stylestat liên quan đến chương đó (để Judge thấy các sự thật như "câu này trong toàn sách lặp lại 7 lần").

### 8.2 Đầu ra (cấu trúc, theo hướng bảy chiều)

```json
{
  "scores": {
    "consistency": 8, "character": 7, "pacing": 8, "continuity": 8,
    "foreshadow": 7, "hook": 7, "aesthetic": 6
  },
  "winner": "variant",
  "confidence": "medium",
  "reasons": ["variant 行动推进更集中", "baseline 前情复述更重"],
  "risks": ["variant 配角动机铺垫略少"]
}
```

- Các chiều phải chính xác là bảy mục của `domain.DimensionScore`, mỗi mục từ 0-10.
- `winner` ∈ baseline/variant/tie; `confidence` ∈ low/medium/high.
- `reasons`/`risks` mỗi điều tối đa 80 ký tự, trích dẫn bản gốc ngắn gọn.

### 8.3 Ranh giới

Judge **không được**: quyết định quy trình có đạt hay không, sửa sản phẩm, tự động sửa prompt, làm cơ sở duy nhất để hợp nhập, tạo trích dẫn văn bản gốc dài.
Judge **có thể**: xếp hạng cho đánh giá thủ công, đánh dấu suy thoái rõ rệt, tóm tắt khác biệt A/B, lộ rõ phản ứng phụ của việc thay đổi prompt.

---

## 9. Báo cáo

Mỗi thí nghiệm tạo ra `report.json` (đọc máy, có thể tạo lại markdown) + `report.md` (đọc người) + `artifacts/{case_id}/{baseline,variant}/` (sản phẩm thô). Khi `--repeat N`, đường dẫn là `artifacts/{case_id}/rN/{baseline,variant}/`.

### 9.1 Chênh lệch chỉ số

Báo cáo hiển thị chênh lệch của variant so với baseline, giá trị tuyệt đối và tỷ lệ đặt cạnh nhau:

```text
completed: baseline=5 variant=5   ← chỉ số văn phong chỉ có nghĩa khi ≥5 chương
tool_calls: baseline=12 variant=16  +4 (+33.3%)
cost_usd: baseline=0.42 variant=0.55  +0.13 (+31.0%)
output_tokens: baseline=8200 variant=9100  +900 (+11.0%)
critical_findings: baseline=0 variant=0
warning_findings: baseline=1 variant=2  +1
stylestat.pattern_top_per_chapter: baseline=3.1 variant=5.4  +2.3   ← suy lui văn phong
stylestat.ending_short_ratio: baseline=0.42 variant=0.71  +0.29     ← nhấn mạnh tính đồng nhất cuối chương
```

### 9.2 Tổng hợp Repeat

Với `--repeat N`, không chỉ xem lần chạy cuối cùng; hiện tại triển khai hiển thị tỷ lệ đạt, số lần thất bại nghiêm trọng, số lần cảnh báo, và min/avg/max của cost/tool_calls. Sau khi tích hợp Judge, sẽ bổ sung phân bố winner để tránh trộn nhiễu từ model trọng tài vào báo cáo tất định mặc định.

```text
writer_first_chapter_xianxia repeat=3
- pass_rate: 3/3
- cost_usd: avg=0.41 min=0.38 max=0.44
- tool_calls: avg=13 min=12 max=15
- stylestat.pattern_top_per_chapter: avg delta=+0.4（无显著回归）
```

### 9.3 Báo cáo khả thi tối thiểu

```text
Gate: FAIL

Hard Fail:
- writer_first_chapter_xianxia: missing checkpoint chapter:1:commit

Warnings:
- writer_dialogue_density: tool_calls +35%
- writer_anti_ai_tone: ending_short_ratio +0.28 (文体回归)

Quality:
- writer_anti_ai_tone: judge prefers variant, confidence=medium

Artifacts:
- workspace/evals/20260629-120000/report.json
```

---

## 10. Cấu trúc thư mục và lệnh

```text
internal/eval/
  case.go        Cấu trúc manifest Case + nạp
  eval.go        Điều phối CLI: single / A/B / repeat
  runner.go      Lắp ráp host để điều khiển (dừng theo giới hạn số chương + drain đến Done), ghi đè bundle.OverridePrompt trong bộ nhớ
  collect.go     Chạy diag.Diagnose + stylestat.Compute + usage/tool_calls trên thư mục đầu ra + khẳng định hợp đồng
  grade.go       Ánh xạ Finding→cổng chặn + delta baseline/variant + quyết định cổng stylestat
  report.go      report.json + report.md

cmd/ainovel-cli  Điểm vào subcommand eval của cmd/ainovel-cli

evals/
  cases/         smoke/ workflow/ quality/ longform/ recovery/ steering/
  rubrics/       writer_chapter.json / architect_outline.json / editor_review.json
  variants/      writer-anti-ai-tone/writer.md v.v. (chỉ đặt prompt cần thay thế trong mỗi thư mục)
  reports/       Lưu trữ báo cáo lịch sử
```

Lệnh:

```bash
# Chạy hàng loạt nhiều case (CI mặc định chỉ chạy smoke, không bật judge)
ainovel-cli eval --cases evals/cases/smoke \
  --variant evals/variants/writer-anti-ai-tone \
  --out workspace/evals/writer-anti-ai-tone --ci
```

**Các tham số đã triển khai trong đợt này**: `--cases` (thư mục hoặc một manifest), `--variant` (thư mục prompt biến thể; khi được truyền sẽ tự chạy baseline+variant A/B), `--repeat N` (chạy lại mỗi case N lần), `--config`, `--out`, `--max-chapters N` (ghi đè mặc định của case), `--timeout` (giới hạn trên theo thời gian thực cho một case), `--ci` (tắt đầu ra theo từng sự kiện; mã thoát khác 0 nghĩa là thất bại nghiêm trọng, điều này cũng có hiệu lực khi không dùng cờ này).

**Đang lên kế hoạch (chưa triển khai, không dùng trên dòng lệnh vì sẽ báo flag chưa định nghĩa)**: `--judge`/`--no-judge` (Phase 3 LLM Judge). Hiện tại các thay đổi prompt lớn có thể dùng trước A/B tất định + repeat:

```bash
# Thay đổi prompt lớn: A/B + repeat để giảm ngẫu nhiên
ainovel-cli eval --cases evals/cases/quality \
  --variant evals/variants/writer-anti-ai-tone \
  --repeat 3 --ci
```

---

## 11. Những việc được loại trừ rõ ràng

Vi phạm điều này đồng nghĩa với việc đánh giá đi lệch so với định vị.

1. **Không sao chép logic chẩn đoán chung của diag ở tầng đánh giá** — mọi phán đoán chung (cặn sót pending, tính nhất quán phase/flow, thiếu chương, vòng lặp vô hạn) đều đi qua `diag`; định nghĩa sự thật chỉ có một. Khẳng định hợp đồng cấp case (`expect.required_checkpoints` v.v.) được phép đọc trực tiếp `store`/checkpoint API, nhưng chỉ làm **khẳng định mỏng** — xác minh các kỳ vọng cụ thể liên quan mạnh đến case này, tuyệt đối không viết lại các quy tắc chung mà diag đã có.
2. **Không triển khai lại các quy tắc tất định** — diag đã có một nhóm quy tắc artifact + quy tắc runtime. Nếu thiếu quy tắc thì thêm vào diag; tầng đánh giá chỉ sử dụng.
3. **Không viết lại logic văn phong Trung Quốc của stylestat trong Python** — gọi trực tiếp package Go.
4. **Không để LLM Judge quyết định luồng có đạt hay không** — cổng chặn chỉ công nhận bằng chứng tất định.
5. **Không để đánh giá can thiệp vào luồng điều khiển** — loại bỏ Action/Planner của diag, không tự động sửa prompt, không hoàn tác, không chạy tiếp, không phát hành.
6. **Không khẳng định chuỗi lệnh gọi công cụ chính xác** — chỉ khẳng định hợp đồng (commit xảy ra, điểm kiểm tra tồn tại), bảo vệ cái cược vào "quy trình do LLM dẫn dắt".
7. **Không đưa vào cơ sở dữ liệu / Web UI / nền tảng đánh giá trực tuyến** — giai đoạn hiện tại cần một công cụ hồi quy cục bộ có thể lặp lại, triển khai được và chi phí thấp.
8. **Không sao chép mã nguồn rồi biên dịch lại để tạo variant** — ghi đè `bundle.Prompts` trong bộ nhớ.
9. **Không mock thành công, không nuốt lỗi** — mọi bước thất bại đều được ghi lại tường minh; nếu case chạy lỗi thì FAIL.
10. **case không thay đổi thường xuyên theo prompt** — case là bộ test ổn định; thay đổi case để cho variant đạt là gian lận.

---

## 12. Triển khai theo giai đoạn

### Phase 1 · Runner + cổng chặn tất định (MVP, chứng minh giả thuyết trước)

- `internal/eval`: Cấu trúc Case + runner (in-process headless + bundle override) + collect (gọi `diag.Diagnose`) + grade (Finding→cổng chặn + hợp đồng `expect`).
- `evals/cases/smoke/` đặt 3-4 case.
- Báo cáo trước sẽ xuất `report.json` + markdown tối thiểu.

**Acceptance**: Một lệnh chạy hết smoke; Người viết bỏ qua commit, cặn sót pending, thiếu điểm kiểm tra, giai đoạn không khớp **đều bị cổng chặn chặn lại** (diag vốn đã có thể kiểm tra; điều đang xác minh là harness đã nối đúng).

### Phase 2 · A/B + repeat + hồi quy stylestat (đã triển khai)

- `--variant` tự động chạy baseline và variant, xuất đầu ra cách ly.
- `--repeat N` tổng hợp tỷ lệ đạt, số lần chạy thất bại nghiêm trọng, số lần chạy cảnh báo, min/avg/max của cost/tool_calls.
- collect thêm `stylestat.Compute`, grade thêm delta văn phong.
- Báo cáo hiển thị trung bình câu theo chương / tỷ lệ câu ngắn cuối chương / câu lặp giữa các chương / so sánh baseline-variant của việc trộn lẫn tiêu đề.

**Acceptance**: Dùng một case ≥5 chương + một variant có "句式 tic 加重", có thể bị hồi quy văn phong đánh dấu cảnh báo; case không đủ chương phải hiển thị rõ `insufficient_sample` thay vì bị đánh giá sai là đạt.

### Phase 3 · LLM Judge

- `evals/rubrics/` + `judge.go`, rubric A/B bảy chiều.
- Judge thất bại (JSON không hợp lệ) → báo cáo ghi nhận thất bại, không ảnh hưởng kết quả tất định.

**Acceptance**: Đầu ra Judge đưa vào json+md, không làm ô nhiễm cổng chặn tất định.

### Phase 4 · Longform & Recovery

- 3-5 chương liên tục / đánh giá cuối cung / can thiệp người dùng / replay pending_commit / case áp lực nén ngữ cảnh.
- Tái sử dụng quy tắc ngữ cảnh+runtime của diag.

**Acceptance**: Có thể phát hiện dòng thời gian lặp lại, cặn sót pending, thiếu tóm tắt cung, vòng lặp công cụ.

---

## 13. Quy chuẩn bảo trì Case

- **Giữ số lượng vừa phải**: Smoke 3-5, Workflow mỗi vai trò 3-5, Quality 2-4, Longform/Recovery mỗi loại 2-3. Quá nhiều thì không ai muốn chạy.
- **Case tốt**: đầu vào ngắn và rõ ràng, bao phủ rủi ro thực, lộ vấn đề trong ít chương, không phụ thuộc vào việc mô hình sinh câu cố định, không ghi sở thích phong cách quá chi tiết.
- **Case kém**: đầu vào quá dài, nhiều mục tiêu cùng lúc, phải chạy mấy chục chương mới đánh giá được, chỉ dựa vào cảm nhận chủ quan.
- **Đặt tên Variant**: `writer-anti-ai-tone` / `architect-rolling-outline` / `editor-strict-review`, mỗi thư mục chỉ đặt prompt cần thay thế.

---

## 14. Rủi ro và giới hạn

- **Ngẫu nhiên của mô hình**: chạy nhiều lần với cùng prompt vẫn thay đổi. Thay đổi quan trọng thì dùng `--repeat 3` để xem xu hướng.
- **Chi phí**: Judge và longform tốn tiền. Cục bộ mặc định chỉ chạy **smoke** (1 chương × baseline+variant, cổng chặn diag tất định, không bật Judge, không chạy stylestat); **stylestat chỉ bật với Quality/Longform từ ≥5 chương** (smoke không đủ chương, `Compute` trả về nil, báo cáo gắn `insufficient_sample`); để dành suite đầy đủ cho các thay đổi lớn.
- **Thiên lệch của Judge**: Judge cũng là mô hình, ưu tiên văn bản giải thích gọn gàng, chưa chắc tương đương với tiểu thuyết hay — vì vậy chỉ dùng làm hỗ trợ, stylestat là tuyến chính tất định.
- **Quá mức đo lường**: số từ/số lần gọi công cụ/chi phí/thống kê văn phong đều là tín hiệu, không phải mục tiêu. Số liệu stylestat có thành bệnh hay không do người phán quyết theo thể loại, **không cố định ngưỡng** (nhất quán với editor.md).
- **Không tự động hoàn tác trực tuyến**: công cụ hồi quy ngoại tuyến, không phụ trách tự động sửa prompt / phát hành trực tuyến.

---

## 15. Kết luận

Giá trị của hệ thống đánh giá này không nằm ở việc tự động phán đoán chất lượng văn học, mà biến việc thay đổi prompt từ "dựa vào cảm tính" thành "có hồi quy, có bằng chứng, có người đọc mẫu".

Khác biệt căn bản so với thiết kế trước chỉ nằm ở một câu: **bộ đánh giá đã nằm trong codebase.** `diag` là bộ chẩn đoán sự thật tất định, `stylestat` là bộ hồi quy văn phong toàn cuốn sách, `ReviewEntry` bảy chiều là rubric gốc. Hệ thống đánh giá cần là một tầng harness Go mỏng — chạy hàng loạt, thu thập, ánh xạ Finding và số liệu thành cổng chặn, tổng hợp báo cáo — thay vì viết lại các phán đoán sự thật đó bằng một ngôn ngữ khác.

Một định nghĩa sự thật, không bao giờ trôi lệch. Đây chính là kỷ luật xuyên suốt từ kiến trúc đến đánh giá của dự án: **harness tối thiểu, tái sử dụng mạnh nhất, phán quyết tất định thuộc về code, phán xử thuộc về LLM và con người.**

---

## 16. Tham khảo

Cấu trúc phổ biến của LLM eval trong ngành (dataset / experiment / scorer / trace / regression gate) là nguồn tư tưởng của thiết kế này, nhưng **cố ý không sao chép** — "scorer" của dự án là `diag`/`stylestat` đã có, "trace" là lớp sự thật checkpoint/session đã có, "dataset" là case gắn khẳng định lớp sự thật.

- OpenAI Evals · https://developers.openai.com/api/docs/guides/evals (lưu ý: nền tảng Evals được quản lý đã công bố lộ trình thời gian ngừng hoạt động; chỉ trích dẫn tư tưởng về kiểm thử có cấu trúc/chấm điểm tự động/hiệu chỉnh thủ công, không dùng làm phụ thuộc trong tương lai)
- Braintrust · https://www.braintrust.dev/foundations/what-is-an-eval
- LangSmith · https://docs.langchain.com/langsmith/evaluation-concepts
