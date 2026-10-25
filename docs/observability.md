# Sổ tay quan sát

Khi chạy một tiểu thuyết dài, làm sao biết các cơ chế có thực sự hoạt động hay không?

Tài liệu này không chép lại toàn bộ quy tắc diag, mà hướng tới **vận hành thực tế**: khi bạn đã chạy tới chương N thì nên mở tệp nào, xem trường nào, để kết luận lành khoẻ hay bất thường.

---

## 1. Quy trình điều tra dùng chung

```
1. /diag                       # chẩn đoán tự động, xem khu vực Findings
2. cd output/{novel}/meta/     # cat trực tiếp các tệp trọng yếu
3. tail decisions.jsonl                # xem các phán quyết Arbiter gần nhất
4. ls -lt sessions/agents/             # định vị session Worker gần nhất rồi tail
```

Những sự thật mà `/diag` không bao phủ (bao gồm các mục "chờ bổ sung chẩn đoán" được liệt kê trong tài liệu này) thì phải kiểm tra thủ công bằng các bước 2-4.

### Báo issue: xuất chẩn đoán đã khử nhạy cảm

Mỗi lần `/diag` sẽ ghi thêm `output/{novel}/meta/diag-export.md` — một bản chẩn đoán **đã khử nhạy cảm** (nội dung truyện / prompt / suy nghĩ đã bị loại bỏ, chỉ giữ lại bộ xương hành vi: tên tool, chuỗi lỗi, số lần lặp, phase/flow, step bị kẹt, phân loại lỗi trong log). Khi gặp vòng lặp vô hạn / sự cố gián đoạn, chỉ cần dán tệp này vào issue GitHub là người bảo trì định vị được, không cần dữ liệu `output/` của người dùng.

---

## 2. Bảng tra nhanh các tệp trọng yếu

Sắp theo "đường điều tra thường gặp nhất khi có sự cố":

| Tệp trọng yếu | Đường dẫn | Xem gì | Lành khoẻ | Không lành khoẻ |
|---|---|---|---|---|
| Tiến độ | `meta/progress.json` | `phase` / `flow` / `completed_chapters` | phase tiến triển đơn điệu, flow nằm trong tập hợp hợp lệ | phase lùi / flow kẹt ở một trạng thái |
| La bàn | `meta/compass.json` | khoảng cách giữa `last_updated` và chương mới nhất | gap < 15 chương | gap > 15 chương (bị CompassDrift bắt) |
| Danh bạ nhân vật phụ | `meta/cast_ledger.json` | số mục / tỉ lệ điền brief_role / tính nhất quán tên | xem §4 | xem §4 |
| Sổ cái phục bút | `meta/foreshadow.json` | số chương đình trệ dài nhất của `status="planted"` | < số chương/3 | > số chương/3 (bị StaleForeshadow bắt) |
| Dàn ý | `meta/layered_outline.json` | số chương còn lại chưa viết trong tập hiện tại | đã mở rộng trước 1-2 chương | đã viết tới chương hiện tại nhưng chương kế tiếp chưa có outline (OutlineExhausted) |
| Hồ sơ nhân vật | `meta/characters.json` | có tìm được nhân vật core/important trong tóm tắt N chương gần nhất không | đều tìm thấy | vắng mặt (bị GhostCharacter bắt) |
| Điểm kiểm tra | `meta/checkpoints.jsonl` | `step` của dòng cuối có khớp progress không | khớp | không khớp (khôi phục sau sập chưa tự lành) |
| Nhật ký phán quyết | `meta/decisions.jsonl` | facts/decision của vài phán quyết gần nhất | phân loại chính xác, hành động hợp lý | cùng một loại can thiệp lại lặp lại phán quyết thất bại |

---

## 3. Quan sát la bàn (compass)

**Thời điểm sửa**: 2026-05-08 (commit `fix: update_compass 工具自动填 last_updated`)

### Xem gì

```bash
cat output/{novel}/meta/compass.json
```

Ngữ nghĩa các trường:
- `ending_direction`: hướng kết thúc (nên khớp với phần hướng kết thúc trong `premise.md`)
- `open_threads`: các tuyến dài đang mở (architect thêm/bớt ở mỗi ranh giới tập)
- `estimated_scale`: quy mô dự kiến (ví dụ "4-6 tập", cập nhật ở mỗi ranh giới tập)
- `last_updated`: **tool tự điền** bằng số chương đã hoàn thành lớn nhất tại thời điểm cập nhật (không còn phụ thuộc LLM tự điền)

### Đánh giá mức lành khoẻ

| Tín hiệu | Kết luận |
|---|---|
| `last_updated` nằm trong khoảng `[latest-15, latest]` | Lành khoẻ |
| `last_updated` lệch sau latest quá 15 chương | architect không cập nhật ở ranh giới cung/tập — kiểm tra prompt architect-long.md |
| `last_updated == 0` | **dữ liệu bẩn từ trước lần sửa này**, lần update_compass sau sẽ tự lành |
| `ending_direction` không khớp với phần hướng kết thúc trong premise.md | architect lén đổi ý định của người dùng — ghi lại và quyết định có đóng băng trường này hay không (chủ đề thiết kế, xem todo.md) |

### Cách xác minh lần sửa có hiệu lực

So sánh trước và sau khi chạy truyện dài:
- **Trước lần sửa**: sau khi chạy 30+ chương, `compass.last_updated` nhiều khả năng là `0` hoặc một số chương đầu
- **Sau lần sửa**: mỗi lần architect gọi `update_compass`, `last_updated` đều bị tầng tool ghi đè thành latest hiện tại

---

## 4. Quan sát danh bạ nhân vật phụ (cast_ledger)

**Tính năng đã triển khai**: 2026-05-08 (commit `feat: 新增配角名册自动追踪次要角色`)

### Xem gì

```bash
cat output/{novel}/meta/cast_ledger.json | jq 'length'                     # tổng số mục
cat output/{novel}/meta/cast_ledger.json | jq '[.[] | select(.brief_role == "" or .brief_role == null)] | length'  # số mục thiếu brief_role
cat output/{novel}/meta/cast_ledger.json | jq '[.[] | select(.appearance_count >= 3)] | length'   # số nhân vật xuất hiện thường xuyên (≥3 lần)
cat output/{novel}/meta/cast_ledger.json | jq 'sort_by(-.appearance_count) | .[:10]'  # 10 nhân vật xuất hiện nhiều nhất
```

### Đánh giá mức lành khoẻ

| Chiều | Lành khoẻ | Bất thường | Xử lý |
|---|---|---|---|
| **Số mục so với số chương đã hoàn thành** | số mục trong ledger ≈ số chương đã hoàn thành × 0.3-0.6 | > số chương × 0.8 (nhân vật chuyển cảnh bị ghi nhầm vào sổ) | kiểm tra phần `cast_intros` trong writer.md đã đủ rõ chưa |
| **Tỉ lệ điền brief_role** | thiếu < 30% | thiếu > 50% | Writer bỏ sót nhiều — prompt chưa dẫn dắt đủ |
| **Trùng tên / tên gần giống** | không có dấu hiệu một người nhiều tên | đồng thời xuất hiện "李X" / "老李" / "X掌柜" | LLM trôi tên — thêm ràng buộc "dùng tên nhất quán" vào prompt hoặc thêm công cụ hợp nhất do người dùng steer |
| **Nhân vật xuất hiện thường xuyên** | ít mục có `appearance_count >= 5` | nhiều mục xuất hiện cao độ qua nhiều cung | nên cân nhắc nâng cấp lên hồ sơ lõi (kênh nâng cấp ở giai đoạn 3) |
| **Kết quả truy xuất có được dùng không** | khi Writer viết tới nhân vật cũ, trường characters của commit_chapter có chứa tên đã có trong ledger | Writer phát minh lại cùng một tên (xuất hiện "老周A" và "老周B") | truy xuất recent_cast không được tiêu thụ — kiểm tra phần "Tính nhất quán của nhân vật phụ" trong writer.md |

### Kiểm chứng luồng dữ liệu (đầu-cuối)

Sau khi chạy 5 chương:
1. `cat meta/cast_ledger.json` phải không rỗng (trừ khi mỗi chương chỉ dùng nhân vật lõi)
2. Nếu Writer giới thiệu "老周" ở chương 1:
   - trong `cast_ledger` phải có mục `老周` với `appearance_count=1`
3. Nếu chương 5 lại viết về 老周:
   - `老周.appearance_count=2`, `last_seen_chapter=5`
4. Giá trị trả về của novel_context ở chương 5 trong `meta/sessions/agents/writer-*.jsonl` phải cho thấy 老周 trong `episodic_memory.recent_cast`
5. Nếu bước trên đã thấy mà Writer không tiêu thụ (老周 được viết ra không khớp với chương 1) — đây là vấn đề prompt

### Hiện chưa có chẩn đoán tự động (nhưng snapshot đã nạp)

`diag.Snapshot.CastLedger` đã được đọc trong `Load()`, có thể cho quy tắc tiêu thụ trực tiếp — nhưng hiện chưa viết quy tắc nào. Việc kiểm chứng vẫn dựa vào các lệnh `jq` ở trên, thực hiện thủ công.

Nếu sau này bổ sung quy tắc chẩn đoán (các ứng viên):
- `CastBriefRoleMissing`: cảnh báo khi tỉ lệ thiếu > 50%
- `CastBloat`: cảnh báo khi số mục > số chương × 0.8
- `CastPromotionCandidate`: appearance_count ≥ 5 và xuyên cung → gợi ý nâng cấp

Chưa nên chốt ngưỡng ngay bây giờ — hãy đợi có dữ liệu truyện dài rồi xem phân bố thực tế mới quyết định. Bản thân mã quy tắc chỉ cần 30-50 dòng.

---

## 5. Writer có đang làm đúng như kỳ vọng không

Khi chạy truyện dài, điều quan tâm nhất là **Writer có thực sự làm theo prompt không**. Cách quan sát trực tiếp nhất là session log:

```bash
ls output/{novel}/meta/sessions/agents/    # mỗi subagent một file jsonl
tail -50 output/{novel}/meta/sessions/agents/writer-*.jsonl
```

Theo dõi vài hành vi cụ thể:

| Hành vi mong đợi | Thể hiện trong jsonl |
|---|---|
| Writer đã xem recent_cast | trường `episodic_memory.recent_cast` trong giá trị trả về của tool novel_context khác rỗng |
| Writer đã điền cast_intros khi commit_chapter | mảng `cast_intros` trong tham số tool_call khác rỗng (chỉ ở chương giới thiệu nhân vật mới) |
| Writer đã dùng gợi ý chương liên quan | số lần gọi `read_chapter` > 1 (mặc định là 1, vượt quá nghĩa là đã tra lại) |
| Writer không vi phạm thứ tự tool | chuỗi tool_call đúng theo `novel_context → read_chapter → plan_chapter → draft_chapter → check_consistency → commit_chapter` |

Nếu trong jsonl thấy Writer gọi novel_context nhiều lần không đọc gì, hoặc sau commit_chapter lại gọi tool khác — đó là prompt chưa giữ được kỷ luật.

---

## 6. Đường đỏ cho kịch bản chạy dài

Khi chạy truyện dài 100+ chương, bất kỳ mục nào sau đây bị trúng thì nên dừng lại điều tra:

- [ ] Bị CompassDrift và vẫn chưa hết sau 2 cung
- [ ] Số mục trong cast_ledger > số chương đã hoàn thành × 0.8
- [ ] Tỉ lệ điền brief_role trong cast_ledger < 30%
- [ ] Cùng một nhân vật có dấu hiệu nhiều tên ("老李" / "李掌柜" cùng tồn tại)
- [ ] Khi viết chương mới, Writer không đọc nhân vật cũ đã có trong recent_cast (phát minh lại tên)
- [ ] Trong session của Worker xuất hiện ≥ 5 lần liên tiếp gọi novel_context không đọc gì
- [ ] Sau khi commit bất kỳ chương nào, `meta/checkpoints.jsonl` không có step `commit_chapter` tương ứng

4 mục đầu là độ lành khoẻ của các cơ chế mới trong lần này; 3 mục sau là độ ổn định của các cơ chế đã có.

---

## 7. Quy tắc bảo trì tài liệu

**Khi thêm một tệp trọng yếu ở tầng sự thật (tạo mới một `meta/*.json` / `meta/*.jsonl`), cập nhật đồng bộ:**

1. Thêm một dòng tra nhanh ở §2 của tài liệu này
2. Nếu tệp cần quan sát riêng (không chỉ là phán đoán đơn giản "có/không"), thêm một mục chuyên đề ở §X
3. Nếu muốn có chẩn đoán tự động, nạp nó trong `internal/diag/snapshot.go::Load` và thêm quy tắc trong `internal/diag/rules_*.go`

**Không nên:**
- Không chép toàn bộ quy tắc trong `internal/diag/` vào tài liệu này (đó là tài liệu tham chiếu quy tắc, không phải sổ tay quan sát)
- Không viết quy tắc chẩn đoán cho từng cơ chế — ngưỡng bịa ra thì sai, hãy quan sát trước rồi bổ sung sau
