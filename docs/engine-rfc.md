# Step 2 RFC:Engine chạy trực tiếp Worker (đáp án cho bảy câu hỏi bắt buộc)

> Trạng thái: chốt (2026-07-12). Dựa trên việc trinh sát mã nguồn host/observer/subagent/usage/cocreate.
> Kết luận: cả bảy câu đều có đáp án rủi ro thấp, chuyển sang triển khai. Liên quan: docs/engine-arbiter.md.

## 1. Mặt phẳng thực thi của Worker: gọi subagent.Runner theo lập trình

> Ghi chú bổ sung (2026-07-22): agentcore đã tách phần thực thi có kiểu khỏi giao thức công cụ của model. `Runner.Run` là điểm vào phía host;
> `Runner.AsTool()` chỉ dành cho những host cần để LLM tự điều phối subagent. AINovel chỉ phụ thuộc Runner.

`Runner.Run(agent, task)` mỗi lần khởi chạy một `agentcore.AgentLoop` trọn vẹn. Engine gọi trực tiếp, nên **toàn bộ phần lắp ráp trong build.go vẫn hiệu lực nguyên vẹn**: mô hình theo vai trò + failover, prompt cache key (#seq tự tăng mỗi lần), ThinkingLevel,
UsageRecorder/SessionLogger(OnMessage), Writer ContextManagerFactory, RestorePack, StopGuardFactory,
StopAfterTools. Kết quả có kiểu cùng chuỗi lỗi được trả về thẳng, không đi qua mã hoá/giải mã JSON hay dò tìm kết quả từ tool.

**Chiếu sự kiện**: bộ chuyển tiếp tiến độ của subagent đọc **callback ToolProgress nằm trong ctx** (`agentcore.ReportToolProgress(ctx,...)`).
Engine gọi `Runner.Run` với `agentcore.WithToolProgress(ctx, relay)`, relay vẫn hoạt động như cũ; relay tổng hợp ProgressPayload thành
`EventToolExecUpdate` rồi đẩy vào `observer.handleToolUpdate` sẵn có — phía worker của observer (dòng TOOL / nội dung truyện streaming /
thinking / retry / context) **được tái sử dụng khoảng ~95%**. Dòng DISPATCH nay do Engine trực tiếp mở và kết thúc (thêm hai điểm vào trong observer).
Luồng tự sự ở cột trái của Coordinator biến mất, được thay bằng sự kiện tự sự của Engine.

**/model và cường độ suy luận**: đổi model thông qua ModelSet swap (configs giữ failover wrapper, cơ chế cũ); cường độ suy luận thông qua
`runner.SetThinkingLevel` (giữ applyThinking, bỏ nhánh coordinator).

## 2. Vòng đời của Engine

Một goroutine duy nhất chạy vòng lặp tuần tự; huỷ `ctx` = tạm dừng / huỷ (lan truyền vào worker loop, checkpoint bảo đảm không mất mát);
Resume/Continue = khởi chạy vòng lặp mới. Việc một Worker chạy tuần tự được bảo đảm tự nhiên bởi cấu trúc vòng lặp. Sentinel ngân sách / điểm dừng
được Engine gọi trực tiếp ở ranh giới mỗi vòng (thay cho việc đăng ký sự kiện và FlowBoundaryHook).

## 3. Giao thức commit trạng thái → cơ chế tuần tự khiến nó gần như biến mất

Chỉ ngay trước khi spawn mỗi vòng, Engine mới `LoadState+Route`, nên lệnh luôn dựa trên sự thật mới nhất — lệnh do Route sinh ra không có TOCTOU, không cần đối soát Expect.
Ảnh chụp Expect chỉ dùng cho **dispatch quyết định của Arbiter** (giữa lúc tham vấn và lúc thực thi có một lần chạy worker): trước khi thực thi tại ranh giới, so sánh
{Phase, QueueHead}; không khớp → bỏ + hỏi lại bằng sự thật mới. Kiểm tra tiền điều kiện (trách nhiệm cũ của Gate) trở thành code thường của Engine:
phase=complete thì không phát lệnh; chương mục tiêu của writer chưa được mở rộng → đổi sang phái architect_long expand (tất định, không cần văn bản hướng dẫn).
Các hành động trạng thái điều khiển khi can thiệp (hold/reopen/dispatch) được commit tại ranh giới hàng đợi của Engine; answer/rules thì tức thì.

## 4. Phân loại lỗi (tính tất định đi trước)

- retryable (mạng / giới hạn tần suất / stream-idle): MaxRetries=7 bên trong subagent đã xử lý tại chỗ, không thoát ra vòng lặp
- worker trả về error (escalate/hard_stop / lỗi cứng của tool): Engine thử lại 1 lần với cùng lệnh → vẫn thất bại → tham vấn Arbiter `worker_failure` (retry/reroute/abort) → abort, hoặc bản thân Arbiter thất bại → tạm dừng + notify
- lỗi tham số / agent lạ và các lỗi tất định khác: tạm dừng + notify ngay (lỗi code, thử lại vô nghĩa)

## 5. Giao thức bế tắc

Mỗi vòng ghi lại khóa lệnh `Agent+Task`. Nếu sau khi vòng trước thực thi, Route vẫn sinh ra cùng khóa, nghĩa là điều kiện hậu quả của nhiệm vụ chưa được thoả mãn, `repeat++`; lệnh thay đổi thì đặt lại về 0. Các checkpoint trung gian bên trong worker như `plan/draft/edit` không tính là tiến triển ở cấp Engine.
repeat==3 → tham vấn Arbiter `deadlock`; khuyến nghị retry của Arbiter **không** đặt lại bộ đếm; repeat==5 → ngắt mạch cứng: tạm dừng + notify.
(Thời đại Coordinator, việc "không đặt ngưỡng" dựa vào tính tự chủ của nó; Engine tất định buộc phải có giới hạn.)

## 6. Ngữ nghĩa khi sập → miễn phí

Không cần phán đoán "Worker trước có tạo ra sự thật hợp lệ hay không": ở tầng tool, checkpoint+digest là idempotent, Route tính lại từ store,
nên phát lệnh lặp lại vẫn an toàn. Việc thử lại luồng model của agentcore không vượt qua ranh giới thực thi tool. Khôi phục = vào thẳng vòng lặp. PendingSteer, trước khi vòng lặp khởi chạy, được xử lý như một can thiệp và đưa qua Arbiter.

## 7. Tiêu chí nghiệm thu của prototype

Kiểm thử tích hợp đầu-cuối (fake ChatModel): quy hoạch → bổ sung → viết chương → thẩm duyệt / tóm tắt cuối cung → mở rộng → hoàn thành toàn tuyến; can thiệp được phân loại và ghi vào store;
tạm dừng/khôi phục; ngắt mạch bế tắc; ghi nhận usage; hình dạng sự kiện của observer (dòng DISPATCH/TOOL, delta streaming). Cộng thêm lưới hồi quy gồm
spec Route 60k sẵn có, hợp đồng agentcore và kiểm thử luồng editor.

## Tổng kết giai đoạn hoàn thành (quyết định thiết kế)

Tổng kết hoàn thành tác phẩm đổi thành **sinh tất định**: store đã có toàn bộ sự thật (tóm tắt chương / nhân vật / sổ cái phục bút / số từ), Engine trực tiếp kết xuất báo cáo,
không tốn thêm một lượt gọi LLM để tạo ra văn bản nghi lễ. Tổng kết LLM của coordinator cũ bị huỷ (engine-arbiter.md §3: tổng kết không phải phán quyết).
