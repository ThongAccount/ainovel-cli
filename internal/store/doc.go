// Package store cung cấp lớp lưu trữ bền dựa trên hệ thống tệp.
//
// Kiến trúc: 1 nền IO + nhiều kho con + 1 gốc tổ hợp.
// Mỗi kho con giữ một thể hiện IO riêng và một sync.RWMutex riêng.
// Các miền chính (Progress, Outline, Drafts, Summaries…) đọc ghi không chặn nhau;
// WorldStore gộp nhiều miền nhỏ, ít ghi vào dùng chung một khóa.
//
// Gốc tổ hợp Store giữ tham chiếu tới mọi kho con và tuần tự hóa các thao tác
// liên miền (ExpandArc, AppendVolume, ClearHandledSteer); nhiều tệp không tạo thành
// một giao dịch nguyên tử, nên lời gọi dựa vào thứ tự ghi an toàn, lỗi tường minh và
// khả năng phát lại idempotent với cùng tham số để khôi phục.
//
// Chia nhỏ các kho con:
//   - ProgressStore: trạng thái tiến độ chính (meta/progress.json)
//   - OutlineStore: tiền đề, đề cương (phẳng/phân tầng), la bàn
//   - DraftStore: dàn ý chương, bản nháp, bản chính thức
//   - SummaryStore: tóm tắt chương/cung/tập
//   - RunMetaStore: siêu dữ liệu chạy (mô hình, lịch sử can thiệp)
//   - SignalStore: tệp tín hiệu một lần (khôi phục PendingCommit)
//   - CheckpointStore: checkpoint cấp bước (meta/checkpoints.jsonl)
//   - RuntimeStore: hàng đợi sự kiện runtime (meta/runtime/*.jsonl)
//   - CharacterStore: hồ sơ nhân vật, ảnh chụp trạng thái
//   - WorldStore: dòng thời gian, phục bút, quan hệ, thay đổi trạng thái, luật thế
//     giới, luật văn phong, thẩm duyệt
package store
