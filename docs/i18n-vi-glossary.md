# Thuật ngữ Việt hóa — ainovel-cli

Bảng tra cứu chuẩn cho mọi đợt Việt hóa. Khi dịch một chuỗi Trung Quốc còn sót trong
mã nguồn hoặc tài liệu, **tra bảng này trước** để các nhánh dịch song song dùng cùng
một từ vựng. Nếu một khái niệm không có trong bảng, dịch theo cách đã dùng trong
`internal/entry/tui/` (bản TUI đã Việt hóa 100% và là nguồn chuẩn về giọng văn).

## Quy tắc bất di bất dịch

1. **Giao thức với LLM** (mô tả tool, schema, prompt gửi cho model) phải dùng tiếng Việt
   khi chế độ `vi` — nếu không, model tiếng Việt nhận mô tả tiếng Trung và lệch giọng.
2. **Chuỗi trả về người dùng** (lỗi, thông báo, tiêu đề chẩn đoán) phải là tiếng Việt.
3. **Chú thích mã nguồn** cũng dịch sang tiếng Việt — quy ước đã được thiết lập ở
   `assets/load.go` và `internal/entry/tui/`.
4. **Không dịch** các định danh kỹ thuật: tên rule (`PhaseFlowMismatch`), tên sự kiện,
   tên trường JSON, tên hàm, khoá cấu hình, đường dẫn, tên provider/model, giá trị
   `vi` / `zh`.
5. **Số và ký hiệu giữ nguyên**: `%d`, `%s`, `{{VOICE}}`, emoji, khoảng trắng thụt đầu dòng.

## Vai trò Agent

| Trung Quốc | Tiếng Việt | Ghi chú |
|---|---|---|
| Coordinator | Điều phối viên | |
| Architect | Kiến trúc sư | vai trò quy hoạch |
| Writer | Người viết | vai trò sáng tác |
| Editor | Biên tập viên | vai trò thẩm duyệt |
| Arbiter | Tài phán | vai trò phán quyết |
| Headless | Chạy nền | |
| Co-create | Đồng sáng tác | dùng đúng nhãn đã có trong TUI |

## Thuật ngữ nghiệp vụ

| Trung Quốc | Tiếng Việt |
|---|---|
| 章节 / 章 | chương |
| 卷 | tập |
| 弧 / cung | cung |
| 章号 | số chương |
| 标题 | tiêu đề |
| 正文 | nội dung truyện (văn bản) |
| 草稿 | bản nháp |
| 终稿 | bản chính |
| 大纲 / 提纲 | Đề cương (dàn ý trong văn mô tả) |
| 题材和基调 | Thể loại và giọng điệu |
| 题材定位 | Định vị thể loại |
| 弧摘要 | tóm tắt cung |
| 卷摘要 | tóm tắt tập |
| 摘要 | tóm tắt |
| 伏笔 | phục bút |
| 线索 | manh mối |
| 钩子 | điểm móc |
| 时间线 | dòng thời gian |
| 人物 / 角色 | nhân vật |
| 设定 | thiết lập / bối cảnh |
| 事实 / 事实包 | sự thật / gói sự thật |
| 连续性 | tính liên tục |
| 一致性 | tính nhất quán |
| 上下文 | ngữ cảnh |
| 记忆 / 情节记忆 | ký ức / ký ức tình tiết |
| 工作记忆 | bộ nhớ làm việc |
| 参考包 | gói tham chiếu |
| 注入 | chèn |
| 抽取 / 提取 | trích xuất |
| 解析 | phân tích |
| 合成 | tổng hợp |
| 裁定 | phán quyết |
| 仲裁 | phán xử |
| 规划 / 规划期 | quy hoạch / giai đoạn quy hoạch |
| 写作 / 写作期 | sáng tác / giai đoạn sáng tác |
| 润色 / 打磨 | hoàn thiện |
| 返工 | làm lại |
| 重写 | viết lại |
| 改写 | viết lại |
| 截断 | cắt ngắn |
| 续写 | viết tiếp |
| 导入 | nhập |
| 导出 | xuất |
| 同步 | đồng bộ |

## Kỹ thuật và trạng thái

| Trung Quốc | Tiếng Việt |
|---|---|
| 状态 | trạng thái |
| 状态机 | máy trạng thái |
| 阶段 | giai đoạn |
| 步 / 步骤 | bước |
| 事件 | sự kiện |
| 边界 | ranh giới |
| 检查点 / checkpoint | điểm kiểm tra |
| 恢复 | khôi phục |
| 回滚 | hoàn tác |
| 失败 | thất bại |
| 成功 | thành công |
| 错误 | lỗi |
| 警告 | cảnh báo |
| 非法 | không hợp lệ |
| 缺少 | thiếu |
| 不能为空 / 为空 | không được để trống / rỗng |
| 未初始化 | chưa khởi tạo |
| 校验 | kiểm tra hợp lệ |
| 验证 | xác minh |
| 默认 | mặc định |
| 可选 | tùy chọn |
| 必填 | bắt buộc |
| 读取 | đọc |
| 写入 | ghi |
| 保存 | lưu |
| 落盘 | ghi xuống đĩa |
| 覆盖 | ghi đè |
| 拒绝覆盖 | từ chối ghi đè |
| 复用 | tái sử dụng |
| 跳过 | bỏ qua |
| 保留 | giữ lại |
| 清理 | dọn dẹp |
| 重建 | dựng lại |
| 加载 | nạp |
| 版本 | phiên bản |
| 模型 | mô hình |
| 提示词 | prompt |
| 采样 / 温度 | lấy mẫu / nhiệt độ |
| 令牌 / token | token |
| 缓存 | bộ nhớ đệm |
| 限流 / 重试 | giới hạn tần suất / thử lại |
| 降级 | hạ cấp |
| 开关 | công tắc |
| 配置 | cấu hình |
| 参数 | tham số |
| 字段 | trường |
| 路径 | đường dẫn |
| 文件 | tệp |
| 目录 | thư mục |
| 表格 / 目录树 | bảng / cây thư mục |
| 调用 | gọi |
| 返回 | trả về |
| 输入 | đầu vào |
| 输出 | đầu ra |
| 日志 | nhật ký |
| 调试 | gỡ lỗi |
| 告警 | cảnh báo |

## Chẩn đoán và chất lượng

| Trung Quốc | Tiếng Việt |
|---|---|
| 诊断 | chẩn đoán |
| 规则 | quy tắc |
| 严重程度 | mức độ nghiêm trọng |
| 置信度 | độ tin cậy |
| 证据 | bằng chứng |
| 建议 | khuyến nghị |
| 指纹 | vân tay |
| 硬失败 | thất bại nghiêm trọng (Hard Fail) |
| 警告项 | cảnh báo |
| 备注 | ghi chú |
| 重复 | trùng lặp |
| 冗余 | dư thừa |
| 英文泄漏 | rò rỉ tiếng Anh |
| 脚本混杂 | trộn lẫn hệ chữ |
| 残留 | cặn sót |
| 陈旧 | cũ |
| 漂移 | trôi lệch |
| 停滞 | đình trệ |
| 空洞 | rỗng tuếch |
| 幽灵角色 | nhân vật ma (không xuất hiện) |
| 时间线缺口 | lỗ hổng dòng thời gian |
| 关系停滞 | quan hệ đình trệ |
| 越界 | vượt phạm vi |
| 截断超长 | cắt ngắn quá độ |

## Hành động và thông báo

| Trung Quốc | Tiếng Việt |
|---|---|
| 开始 / 启动 | bắt đầu / khởi chạy |
| 停止 | dừng |
| 暂停 | tạm dừng |
| 继续 | tiếp tục |
| 完成 | hoàn thành |
| 取消 | huỷ |
| 确认 | xác nhận |
| 请 | vui lòng |
| 正在 | đang |
| 无法 | không thể |
| 跳过 | bỏ qua |
| 暂停重试 | tạm dừng thử lại |
| 实测 | thực đo |
| 避免 | tránh |
| 保证 | bảo đảm |
| 否则 | nếu không |
| 用于 | dùng cho |
