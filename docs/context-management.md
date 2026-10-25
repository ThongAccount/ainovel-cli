# Giải thích quản lý context

Tài liệu này giải thích hệ thống quản lý context hiện hành của `ainovel-cli`, gồm:

- Vì sao cần quản lý context
- Context đến từ đâu
- Lúc chạy thì nén, khôi phục, bàn giao ra sao
- Giá trị, điều kiện kích hoạt và tình huống áp dụng của từng chiến lược
- Khi có sự cố thì nên xem chỗ nào trước

Mục tiêu không phải giới thiệu khái niệm trừu tượng, mà là để người bảo trì về sau mở tài liệu này là nắm được nhanh phần hiện thực hiện tại và các điểm vào để truy tìm sự cố.

## 1. Mục tiêu thiết kế

Quản lý context của dự án này không dành cho kịch bản trò chuyện tổng quát, mà hướng tới kịch bản sáng tác truyện. Nó phải giải quyết đồng thời mấy nhóm vấn đề:

1. Hội thoại dài sẽ vượt ra ngoài context window của model.
2. Sáng tác truyện cần giữ không phải "lịch sử trò chuyện" mà là trí nhớ tự sự có cấu trúc.
3. Sau khi nén, Writer không được đánh mất trạng thái nhân vật, phục bút, kế hoạch chương, ràng buộc văn phong, các mục thẩm duyệt còn cần sửa.
4. Khi khôi phục việc viết, không được giả định model vẫn "nhớ mình đã nói gì trước đó", phải ưu tiên dựa vào các tệp bền vững.

Vì vậy chúng ta dùng một phương án "bộ nhớ phân tầng":

- Bộ nhớ ngắn hạn: phần đuôi của các tin nhắn được giữ gần nhất
- Bộ nhớ trung hạn: `ContextSummary` sinh ra khi nén
- Bộ nhớ dài hạn: các tệp có cấu trúc trong project store
- Bộ nhớ khôi phục: handoff / restore pack / novel_context

## 2. Kiến trúc tổng thể

### 2.1 Các tầng chính

Hiện quản lý context chia thành bốn tầng:

1. `agentcore/context`
   Phụ trách ngân sách context tổng quát, pipeline chiến lược, khung nén/khôi phục.

2. `internal/tools/novel_context`
   Phụ trách lắp ráp dữ liệu có cấu trúc của dự án truyện thành context dùng được cho lượt hiện tại.

3. `internal/orchestrator/store_summary_*`
   Phụ trách nén nhanh dựa trên store, dành riêng cho Writer.

4. `internal/orchestrator/writer_restore.go`
   Phụ trách thêm một gói khôi phục đã nén sau `FullSummary`, đảm bảo Writer có thể viết tiếp.

### 2.2 Luồng dữ liệu

Lúc chạy có hai đường context chính:

1. Đường làm việc bình thường
   - Agent gọi `novel_context`
   - `novel_context` đọc tóm tắt chương, kế hoạch, nhân vật, dòng thời gian, v.v. từ store
   - Dữ liệu đó đi vào prompt của lượt hiện tại

2. Đường context quá dài
   - `ContextManager` phát hiện áp lực token
   - Nén theo thứ tự các chiến lược
   - Ưu tiên thử nén nhẹ và nén dựa trên store
   - Chỉ khi vẫn chưa đủ mới đi tới LLM `FullSummary`
   - Sau `FullSummary` thì chèn restore pack

## 3. Các tệp then chốt

### 3.1 Engine context tổng quát

- `../agentcore/context/strategy.go`
- `../agentcore/context/engine.go`
- `../agentcore/context/strategy_tool.go`
- `../agentcore/context/strategy_trim.go`
- `../agentcore/context/strategy_summary.go`
- `../agentcore/context/message.go`
- `../agentcore/context/summary_run.go`

Vai trò:

- Định nghĩa `Strategy` / `ForceCompactionStrategy`
- Chịu trách nhiệm chạy chuỗi chiến lược dựa trên ngân sách
- Chịu trách nhiệm biểu diễn `ContextSummary` và chuyển đổi bằng LLM
- Chịu trách nhiệm nén tóm tắt bằng LLM cho `FullSummary`

### 3.2 Nối phía dự án

- `internal/orchestrator/agents.go`

Vai trò:

- Lắp ráp `ContextManager` cho Writer (Coordinator đã nghỉ việc ngày 2026-07-12, xem docs/engine-arbiter.md)
- Chèn thêm `StoreSummaryCompact` cho Writer
- Cấu hình prompt `FullSummary` riêng cho việc viết truyện
- Cấu hình `writerRestorePack` cho Writer

### 3.3 Nén và khôi phục phía dự án

- `internal/orchestrator/store_summary_strategy.go`
- `internal/orchestrator/store_summary_builder.go`
- `internal/orchestrator/writer_restore.go`

Vai trò:

- Trước khi tóm tắt bằng LLM, ưu tiên dùng dữ liệu store để nén nhanh
- Xây dựng thống nhất context có cấu trúc mà Writer cần cho việc nén và khôi phục
- Thêm một restore message thuần trong bộ nhớ sau `FullSummary`

### 3.4 Lắp ráp context có cấu trúc

- `internal/tools/novel_context.go`
- `internal/tools/novel_context_builders.go`
- `internal/domain/runtime.go`

Vai trò:

- Định nghĩa `ContextProfile` / `MemoryPolicy`
- Quyết định tải bao nhiêu tóm tắt chương, bao nhiêu dòng thời gian, có bật tóm tắt phân tầng hay không
- Lắp ráp chương, nhân vật, phục bút, dòng thời gian, kinh nghiệm thẩm duyệt, v.v. trong store

### 3.5 Bàn giao và khôi phục

- `internal/orchestrator/handoff_policy.go`
- `internal/orchestrator/recovery_engine.go`

Vai trò:

- Ưu tiên dựa vào handoff ở các giai đoạn truyện dài / làm lại / thẩm duyệt
- Khi khôi phục, ghép gói bàn giao có cấu trúc vào prompt

### 3.6 Khả năng quan sát

- `internal/orchestrator/run.go`
- `internal/orchestrator/runtime.go`
- `internal/entry/tui/panels.go`

Vai trò:

- Ghi lại sự kiện viết lại context
- Xuất tên chiến lược, biến động token, số tin nhắn được giữ lại
- Để TUI thấy được context hiện tại là `projected` hay `compacted`

## 4. ContextManager được lắp ráp thế nào

Writer đi qua `newContextManager` (mỗi lần spawn, nhà máy dựng lại theo context window của model hiện tại). Trước khi Coordinator nghỉ việc, nó cũng dùng cùng nhà máy; cấu hình của nó được giữ trong bảng dưới đây làm đối chiếu lịch sử.

Các tham số chính của `contextManagerConfig` hiện tại:

- `ContextWindow`
  Context window tổng của model.

- `ReserveTokens`
  Số token dành riêng cho đầu ra của model.

- `KeepRecentTokens`
  Ngân sách token của phần đuôi tin nhắn gần nhất, cố giữ lại khi nén.

- `ToolMicrocompact`
  Cấu hình nén nhẹ kết quả tool.

- `ExtraStrategies`
  Các chiến lược nén bổ sung phía dự án. Writer hiện dùng nó để gắn `StoreSummaryCompact`.

- `Summary`
  Cấu hình của `FullSummary`, gồm prompt tuỳ chỉnh và post-summary hook.

Giá trị cấu hình thực tế hiện nay:

| Tham số | Writer | Coordinator (đã nghỉ việc, đối chiếu lịch sử) |
|------|--------|-------------|
| ReserveTokens | 16,384 | 32,000 |
| KeepRecentTokens | 20,000 | 30,000 |
| CommitOnProject | false | true |
| IdleThreshold | 5min | không có |
| ExtraStrategies | StoreSummaryCompact | không có |
| Summary Prompt tuỳ chỉnh | bản tự sự truyện | mặc định (bản trợ lý code) |

Ngưỡng kích hoạt nén = `ContextWindow - ReserveTokens`. Ví dụ với cửa sổ 128K, Writer kích hoạt ở khoảng ~112K.

Thứ tự pipeline chiến lược hiện tại của Writer là:

1. `ToolResultMicrocompact`
2. `LightTrim`
3. `StoreSummaryCompact`
4. `FullSummary`

Thứ tự này có ý nghĩa rõ ràng:

- Trước hết dọn nhiễu tool bằng cách rẻ nhất
- Sau đó cắt bớt các khối văn bản quá dài
- Nếu dữ liệu store đủ thì làm nén có cấu trúc hoàn toàn không cần LLM
- Chỉ cuối cùng mới lùi về tóm tắt bằng LLM

## 5. Vai trò của từng chiến lược

### 5.1 ToolResultMicrocompact

Vị trí hiện thực:

- `../agentcore/context/strategy_tool.go`

Vai trò:

- Dọn `tool_result` trong lịch sử
- Thay các kết quả tool cũ bằng văn bản giữ chỗ ngắn gọn

Giá trị:

- Nội dung tool trả về thường cồng kềnh, mật độ thông tin thấp
- Nhiều kết quả tool cũ chỉ là "tiếng ồn quá trình", không phải trí nhớ truyện

Đặc điểm cấu hình hiện tại của Writer:

- Đặt `IdleThreshold = 5m`

Điều này có nghĩa:

- Nếu tin nhắn assistant gần nhất đã nhàn rỗi quá ngưỡng
- Thì số kết quả tool cũ được giữ lại sẽ bị giảm mạnh hơn

Tình huống áp dụng:

- Nhiều lượt `novel_context`
- Sau nhiều lượt tool read / check / draft

### 5.2 LightTrim

Vị trí hiện thực:

- `../agentcore/context/strategy_trim.go`

Vai trò:

- Cắt bớt các khối văn bản rất dài
- Giữ phần đầu và phần cuối, thay phần giữa bằng placeholder

Giá trị:

- Giữ nguyên cấu trúc tin nhắn
- Chi phí thấp
- Rất hợp với nguyên văn chương quá dài hoặc các đoạn đầu ra lớn

Tình huống áp dụng:

- Một tin nhắn đơn quá dài, nhưng chưa cần tóm tắt toàn bộ lịch sử

### 5.3 StoreSummaryCompact

Vị trí hiện thực:

- `internal/orchestrator/store_summary_strategy.go`
- `internal/orchestrator/store_summary_builder.go`

Vai trò:

- Khi context của Writer quá dài
- Ưu tiên dùng trí nhớ có cấu trúc trong store bền vững để thay thế các tin nhắn cũ
- Không gọi LLM

Nó không phải tóm tắt hội thoại, mà là "thay thế bằng trí nhớ có cấu trúc".

Các dữ liệu cốt lõi được giữ lại hiện nay gồm:

- Tiến độ hiện tại
- Tóm tắt các chương gần nhất
- Kế hoạch chương hiện tại
- Dàn ý chương hiện tại
- Tóm tắt cung hiện tại
- Tóm tắt tập hiện tại
- Ảnh chụp nhân vật
- Manh mối đang hoạt động
- Các vấn đề thẩm duyệt còn cần sửa
- Dòng thời gian gần nhất
- Quy tắc văn phong

Điều kiện kích hoạt:

- Chương hiện tại lớn hơn 1
- Store đã có đủ tóm tắt lịch sử
- Và chương hiện tại có ít nhất dữ liệu ở trạng thái làm việc
  - `chapter_plan` hoặc `current_outline`

Giá trị:

- Giảm số lần nén bằng LLM
- Tránh cho thông tin then chốt của truyện trôi lệch trong lúc tóm tắt
- Khiến bộ nhớ dài hạn ưu tiên dựa vào sự thật đã ghi xuống đĩa thay vì lịch sử trò chuyện

Vì sao chỉ dành cho Writer:

- Đây là chiến lược nghiệp vụ truyện, không phải chiến lược của framework tổng quát
- Chế độ context của Editor / Architect khác (một nhiệm vụ đơn lẻ, áp lực cửa sổ nhỏ)
- Kiểm chứng trước trên Writer — nơi cần liên tục trí nhớ sáng tác nhất — là hợp lý nhất

### 5.4 FullSummary

Vị trí hiện thực:

- `../agentcore/context/strategy_summary.go`
- `../agentcore/context/summary_run.go`

Vai trò:

- Khi mấy tầng trên vẫn chưa đủ, dùng model sinh ra `ContextSummary`
- Giữ phần đuôi tin nhắn gần nhất
- Biến context cũ hơn thành một checkpoint có cấu trúc

Những điểm Writer khác với trợ lý code mặc định:

- Writer dùng summary prompt tuỳ chỉnh
- Nội dung tóm tắt yêu cầu rõ ràng phải giữ:
  - Tiến độ hiện tại
  - Trạng thái tức thời của nhân vật
  - Phục bút và tuyến phục bút đang hoạt động
  - Phản hồi thẩm duyệt và các mục còn cần sửa
  - Văn phong và nhịp điệu
  - Các quyết định then chốt
  - Bước tiếp theo
  - Context then chốt

Giá trị:

- Là chiến lược dự phòng cuối cùng
- Ngay cả khi dữ liệu store không đủ, vẫn có thể duy trì tính liên tục qua LLM

### 5.5 Circuit breaker

Vị trí hiện thực:

- `../agentcore/context/engine.go`

Vai trò:

- Khi nén liên tục thất bại tới ngưỡng (mặc định 3 lần), bỏ qua việc nén ở lượt hiện tại
- Khi bỏ qua vẫn phát ra `RewriteEvent` (`Reason = "circuit_breaker"`)
- TUI hiển thị scope là "bỏ qua do circuit breaker"
- Dùng chế độ nửa mở: bỏ qua một lượt thì lượt sau sẽ thử lại, thành công thì đặt lại, thất bại lại thì bỏ qua

Vì sao cần:

- Tóm tắt bằng LLM có thể liên tục thất bại vì mạng, model từ chối, v.v.
- Không có circuit breaker thì mỗi lượt Project đều thử và đều thất bại, lãng phí lượt gọi API
- Trong phiên viết truyện dài, lãng phí này sẽ tích luỹ

Truy tìm sự cố:

- Nếu TUI liên tục hiện "bỏ qua do circuit breaker", nghĩa là đường tóm tắt bằng LLM đang có vấn đề
- Kiểm tra trong slog các sự kiện viết lại context có `reason=circuit_breaker`
- Circuit breaker không ảnh hưởng `StoreSummaryCompact` (nó không gọi LLM)

### 5.6 Ước lượng token (nhận biết CJK)

Vị trí hiện thực:

- `../agentcore/context/usage.go`

Vai trò:

- Mọi kiểm soát ngân sách và thời điểm kích hoạt nén đều phụ thuộc vào ước lượng token
- `estimateTextTokens` tự phát hiện văn bản có chủ yếu là ký tự CJK hay không
- Văn bản CJK chiếm ưu thế: `runes × 1.5`
- Văn bản ASCII chiếm ưu thế: `bytes / 4`

Vì sao không dùng được `bytes/4` tiêu chuẩn:

- Một chữ Hán trong UTF-8 = 3 bytes
- `bytes/4` ước lượng một chữ Hán là 0.75 token, thực tế khoảng 1.5 token
- Đánh giá thấp gấp 2 lần khiến việc nén kích hoạt bị trễ hẳn

Phạm vi ảnh hưởng:

- `EstimateTokens` (một tin nhắn)
- `EstimateTotal` (danh sách tin nhắn)
- `EstimateContextTokens` (ước lượng hỗn hợp: Usage do LLM báo cáo + ước lượng các tin nhắn đuôi)
- Cắt ngân sách trong `store_summary_builder.go`

Lưu ý: args của ToolCall là JSON (ASCII chiếm ưu thế), vẫn dùng `bytes/4`, không bị điều chỉnh CJK ảnh hưởng.

## 6. Vì sao Writer có hai bộ "ký ức sau nén"

Writer hiện có hai chuỗi trông giống nhau nhưng khác trách nhiệm:

### 6.1 StoreSummaryCompact

Trách nhiệm:

- Thay thế trực tiếp các tin nhắn cũ trong quá trình nén

Đặc điểm:

- Xảy ra trước `FullSummary`
- Không dùng LLM
- Dùng store để thay lịch sử cũ hơn

### 6.2 writerRestorePack

Vị trí hiện thực:

- `internal/orchestrator/writer_restore.go`

Trách nhiệm:

- Thêm một restore message sau `FullSummary`

Đặc điểm:

- Xảy ra sau khi nén bằng LLM
- Được chèn qua `PostSummaryHook`
- Dùng để bổ sung thông tin có cấu trúc mà Writer bắt buộc phải thấy khi khôi phục và viết tiếp

Vì sao cần cả hai:

- `StoreSummaryCompact` không phải lúc nào cũng trúng
  - Ví dụ ở chương đầu tiên, hoặc khi dữ liệu store không đủ
- `FullSummary` dù làm tốt đến đâu vẫn có thể bỏ sót thông tin chính xác trong store
- Vì vậy restore pack là lớp bảo hiểm cuối cùng

Giờ hai phần này đã dùng chung `store_summary_builder.go`, tránh lệch cách diễn đạt.

## 7. Vai trò của novel_context

Vị trí hiện thực:

- `internal/tools/novel_context.go`
- `internal/tools/novel_context_builders.go`

`novel_context` không phải chiến lược nén, nó là "bộ lắp ráp context có cấu trúc" lúc chạy.

Nó chia dữ liệu trong store thành mấy nhóm:

- `working_memory`
  - Kế hoạch chương hiện tại
  - Dàn ý chương hiện tại
  - Tóm tắt các chương gần nhất
  - Dòng thời gian
  - checkpoint
  - previous tail

- `episodic_memory`
  - Trạng thái nhân vật
  - Trạng thái quan hệ
  - Thay đổi trạng thái gần đây
  - Manh mối

- `reference_pack`
  - Thiết lập và dữ liệu tham chiếu ổn định hơn

- `selected_memory`
  - Một ít ký ức quan trọng được chọn ra theo nhiệm vụ hiện tại

Giá trị:

- Nó quyết định context truyện có cấu trúc thực sự được "đưa cho model" mỗi lượt
- `StoreSummaryCompact` không gọi chính nó, nhưng dùng chung nguồn dữ liệu và cách lắp ráp tương tự

## 8. ContextProfile và MemoryPolicy

Vị trí hiện thực:

- `internal/domain/runtime.go`

### 8.1 ContextProfile

Vai trò:

- Quyết định kích thước cửa sổ tải theo tổng số chương

Quy tắc hiện tại:

- `<= 15` chương
  - Tóm tắt `10` chương gần nhất
  - Dòng thời gian `10` chương gần nhất

- `<= 50` chương
  - Tóm tắt `5` chương gần nhất
  - Dòng thời gian `8` chương gần nhất

- `> 50` chương
  - Tóm tắt `3` chương gần nhất
  - Dòng thời gian `5` chương gần nhất
  - Bật tóm tắt phân tầng

Giá trị:

- Kiểm soát quy mô context
- Tránh nhồi toàn bộ lịch sử vào prompt khi viết truyện dài

### 8.2 MemoryPolicy

Vai trò:

- Viết ra một cách tường minh chiến lược dùng context hiện tại
- Làm đầu ra cho `novel_context`
- Dùng cho logic handoff / reminder / chẩn đoán

Các trường then chốt:

- `SummaryWindow`
- `TimelineWindow`
- `LayeredSummaries`
- `SummaryStrategy`
- `HandoffPreferred`
- `ReadOnlyThreshold`

Giá trị:

- Biến "hệ thống hiện tại nên dùng ký ức ra sao" từ logic ẩn thành chiến lược runtime tường minh

## 9. Vai trò của handoff

Vị trí hiện thực:

- `internal/orchestrator/handoff_policy.go`

Khi tác phẩm bước vào giai đoạn dài hơn, phức tạp hơn và phụ thuộc nhiều hơn vào tệp có cấu trúc, hệ thống sẽ nghiêng về handoff.

Handoff pack ghi lại:

- Giai đoạn hiện tại và flow
- Vị trí chương kế tiếp
- Commit gần nhất
- Thẩm duyệt gần nhất
- Tóm tắt gần nhất
- memory policy hiện tại
- Câu hướng dẫn khôi phục

Giá trị:

- Khi khôi phục sau gián đoạn không phụ thuộc lịch sử trò chuyện
- Ưu tiên dựa vào tệp có cấu trúc trong các tình huống làm lại, thẩm duyệt, truyện dài

## 10. Khả năng quan sát và truy tìm sự cố

### 10.1 Sự kiện viết lại context

Vị trí hiện thực:

- `internal/orchestrator/run.go`

Mỗi lần viết lại context đều được xuất qua `contextRewriteCallback`:

- `reason`
- `strategy`
- `committed`
- `tokens_before`
- `tokens_after`
- `messages_before`
- `messages_after`
- `compacted_count`
- `kept_count`
- `split_turn`
- `incremental`
- `summary_runes`
- `duration_ms`

Những giá trị này đồng thời đi vào:

- `slog`
- Hàng đợi ranh giới runtime
- Sự kiện `COMPACT` của TUI

### 10.2 Nhìn thấy được gì trong TUI

TUI hiển thị:

- Token context hiện tại (kèm màu chuyển dần theo mức lành khoẻ)
- context window
- scope của context hiện tại (gồm "bỏ qua do circuit breaker")
- Tên chiến lược của lần gần nhất
- Số lượng summary

Ý nghĩa màu của tỉ lệ context (hiện thực ở `internal/entry/tui/layout.go`):

| Màu | Điều kiện | Ý nghĩa |
|------|------|------|
| Xanh lá | < 70% | dư dả, còn xa ngưỡng nén |
| Vàng | 70-85% | sát ngưỡng nén |
| Đỏ | > 85% | sắp nén hoặc đang nén |

Nhãn hiển thị của scope:

| Scope | Hiển thị | Ý nghĩa |
|-------|------|------|
| baseline | đường cơ sở | trạng thái bình thường |
| projected | chiếu | xem trước nén tạm thời |
| compacted | đã commit | nén đã có hiệu lực |
| recovered | đã khôi phục | khôi phục sau khi tràn |
| skipped | bỏ qua do circuit breaker | nén bị circuit breaker bỏ qua |

Giá trị:

- Nhanh chóng đánh giá được mức lành khoẻ của context hiện tại
- Khi vàng/đỏ thì có thể dự đoán việc nén sắp xảy ra
- Thấy "bỏ qua do circuit breaker" nghĩa là đường tóm tắt bằng LLM đang có vấn đề

### 10.3 Khi có sự cố thì xem chỗ nào trước

#### Kịch bản 1: Writer nén xong mất kế hoạch chương

Xem trước:

- `novel_context` có chèn ổn định `chapter_plan` không
- `store_summary_builder.go` có nhận được `chapterPlan` không
- `writerRestorePack` có được làm mới không

Tệp trọng yếu:

- `internal/tools/novel_context_builders.go`
- `internal/orchestrator/store_summary_builder.go`
- `internal/orchestrator/session.go`

#### Kịch bản 2: Nén xong mất trạng thái nhân vật / phục bút

Xem trước:

- `LoadLatestSnapshots`
- `LoadActiveForeshadow`
- `store_summary_builder.go`
- Summary prompt của Writer có bị ghi đè không

#### Kịch bản 3: Nén thường xuyên mà cứ không trúng store_summary

Xem trước:

- Chương hiện tại có phải `<= 1` không
- Đã có recent summaries / arc / volume summary chưa
- Có tồn tại `chapter_plan` hoặc `current_outline` không
- `writer.Context.Strategy` cuối cùng ghi lại có phải `full_summary` không

#### Kịch bản 4: Sau khi khôi phục thì context không đủ

Xem trước:

- Handoff có được sinh ra không
- Restore pack có được làm mới không
- Recovery prompt có chèn handoff không

#### Kịch bản 5: Quá nhiều kết quả tool làm phình context

Xem trước:

- `ToolResultMicrocompact` có trúng không
- `IdleThreshold` có có hiệu lực không

## 11. Các đánh đổi của phần hiện thực hiện tại

### Những hướng đã quyết định giữ

1. Không nhồi logic nghiệp vụ truyện vào `agentcore`
2. Ưu tiên dựa vào store có cấu trúc thay vì lịch sử trò chuyện
3. Writer dùng summary prompt riêng cho việc viết truyện
4. Nén và khôi phục cùng dùng builder, tránh lệch cách diễn đạt

### Những hạn chế vẫn cố ý giữ lại

1. `StoreSummaryCompact` chỉ dành cho Writer
2. Chương đầu tiên không trúng nén dựa trên store
3. Khi dữ liệu store không đủ vẫn lùi về `FullSummary`
4. `writerRestorePack` là bù đắp kiểu nối, không thay thế `FullSummary`

Những hạn chế này không phải là khiếm khuyết, mà là ranh giới được vẽ ở giai đoạn hiện tại để kiểm soát độ phức tạp.

## 12. Tóm tắt trong một câu

Quản lý context của dự án này không đơn giản chỉ là "rút ngắn hội thoại dài", mà là:

`Ưu tiên dùng trí nhớ truyện có cấu trúc để duy trì tính liên tục, chỉ khi cần mới để LLM tóm tắt hội thoại; đồng thời ở cả ba khâu nén, khôi phục và bàn giao đều cố gắng dựa vào cùng một bộ tệp bền vững.`

Nếu sau này bạn cần sửa hệ thống này, hãy giữ trước ba điều sau:

1. Đừng để ký ức then chốt của Writer lại chỉ phụ thuộc vào lịch sử trò chuyện.
2. Đừng để `store_summary` và `writer_restore` lệch cách diễn đạt.
3. Khi gặp vấn đề tính liên tục, hãy kiểm tra tệp có cấu trúc đã vào context chưa, rồi mới quyết định có sửa prompt hay không.
