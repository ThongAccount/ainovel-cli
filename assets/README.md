# assets: Bản đồ nội dung của thư mục assets

Trước khi thêm "một đoạn văn / một tài liệu / một quy tắc" vào hệ thống, hãy tra bảng dưới đây để xác định vị trí, sau đó xem cách kết nối.

| Thư mục | Lưu gì | Ai tiêu dùng | Cách kết nối |
|---|---|---|---|
| `prompts/` | Worker system prompt (writer / editor / architect×2), prompt phán quyết của Tài phán và prompt tác vụ một lần (import / simulation / revision) | `agents/build.go`, `internal/arbiter`, imp / sim / revision runner | `load.go` trường Prompts. Lưu ý: simulation_guidance được `load.go` chèn khi nạp, file md không thấy |
| `references/` | Tài liệu kiến thức sáng tác không liên quan thể loại. Không vào system prompt, do `novel_context` cắt giảm theo vai trò / chương trước khi chèn vào `reference_pack` | writer / editor / architect | **Ba nơi kết nối**: `tools.References` thêm trường + `load.go` loadReferences đọc + `novel_context.go` writerReferences / architectReferences chèn. Đưa vào thư mục sẽ không được tự động nạp |
| `references/genres/<style>/` | Kiến thức chuyên thuộc thể loại (style-references / arc-templates) | Tương tự, nạp khi `style != default` | `load.go` loadReferences |
| `rules/` | Thư mục quy tắc nội bộ cũ đã lỗi thời; cơ sở chuẩn đã được chuyển sang mã, quy tắc người dùng đến từ ảnh chụp ngôn ngữ tự nhiên của `~/.ainovel/rules/*.md` / `./.ainovel/rules/*.md` | `userrules.Service` chuẩn hoá thành `meta/user_rules.json`; `novel_context` chèn; `commit_chapter` kiểm tra | Xem cơ sở chuẩn nội bộ tại `internal/rules/snapshot.go` của `SystemDefaults()`; file `.md` của người dùng không định dạng, không YAML, chuẩn hoá theo ngôn ngữ tự nhiên |
| `styles/<style>.md` | Chỉ thị phong cách sáng tác theo thể loại | Nối vào system prompt của **writer** (`agents/build.go`) | Tên file là giá trị của `config.style`. Cùng `references/genres/<style>/` là hai bộ chứa của cùng một khái niệm thể loại: cái trước là chỉ thị phong cách, cái sau là tài liệu kiến thức |

## Xác định vị trí cho nội dung mới (5 câu hỏi)

1. Quy trình này phải được**bảo đảm**? → Không viết prompt, viết ràng buộc code (StopAfterTools / guard công cụ / Flow Router)
2. Đây là tiêu chí phán quyết? → Quy trình loại tra bảng viết `internal/flow/router.go`; phán đoán ngữ nghĩa viết `prompts/arbiter-*.md`
3. Đây là tiêu chuẩn thẩm mỹ / thực thi của một vai trò? → `prompts/<role>.md`
4. Đây là quy tắc mặc định có thể liệt kê cơ học (từ cấm / ngưỡng)? → `internal/rules/snapshot.go` của `SystemDefaults()`; quy tắc do người dùng tự định nghĩa ghi vào `.ainovel/rules/*.md`, được ảnh chụp chuẩn hoá sử dụng (số lượng ký tự / độ dài là ràng buộc mềm ngữ nghĩa, xử lý qua preferences, không làm quy tắc cơ học)
5. Đây là tài liệu kiến thức sáng tác? → `references/` (nhớ ba nơi kết nối)

## Bảo đảm tính nhất quán

Các đường dẫn envelope mà prompt tham chiếu (`working_memory.*` v.v.) phải nhất quán với `novel_context`. Hình dạng tham số công cụ chỉ được định nghĩa trong Schema của công cụ; prompt chỉ bổ sung ngữ nghĩa nghiệp vụ Schema không thể biểu thị, không sao chép danh sách tham số JSON và ví dụ hình dạng.

prompt có thể mô tả phương thức thực thi của một Worker đơn, nhưng routing toàn cục, chuyển đổi trạng thái và logic khôi phục chỉ lấy code làm chuẩn. Các bước xác định được từ sự thật trong Store đưa vào Router/Tool; các phán đoán cần hiểu nội dung tiểu thuyết hoặc ý định người dùng thì để lại cho mô hình.
