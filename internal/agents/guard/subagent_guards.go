package guard

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/store"
)

// subagentMaxConsecutiveBlocks: chặn liên tiếp N lần thì nâng cấp thành dừng,
// tránh mô hình yếu rơi vào vòng lặp vô hạn.
const subagentMaxConsecutiveBlocks = 3

// BlockHook là callback kiểm toán của StopGuard: được gọi đồng bộ mỗi lần
// chặn/nâng cấp. Host dùng nó để đưa sự thật chặn ra luồng sự kiện TUI và thông
// báo ngoài màn hình — nếu không, chặn chỉ nằm trong log, người dùng trên giao
// diện chỉ thấy "lag + token chạy nhanh" mà không biết hệ thống đang tự lành
// hay đang quay tròn (issue #75).
// Callback không tham gia quyết định của guard. Giá trị reason:
//   - "blocked"    đã chèn thông điệp thúc, model sẽ tiếp tục
//   - "escalated"  quay tròn liên tục vượt giới hạn, run hiện tại dừng và trả lên trên
//   - "hard_stop"  provider từ chối (safety/content_filter), dừng ngay
type BlockHook func(agent, reason string, consecutive int32)

// hardStopReasons là các lý do provider từ chối mà thông điệp thúc không cứu
// được. Chèn "phải commit" với chúng vô ích, ngược lại mỗi lần còn tốn token
// của một lượt gọi LLM đầy đủ, và cuối cùng khi nâng cấp escalate, Engine chạy
// lại cả nhiệm vụ Worker, cộng dồn nhiều lần lãng phí
// (thực đo ch02 gặp safety: một lần viết chương sinh 3 lần phân công lại, 17
// lượt gọi LLM, tỉ lệ trúng rơi từ 50% xuống 2.8%).
//
// Lưu ý StopReasonError / StopReasonAborted không cần liệt kê: agentcore khi
// nhận hai stop reason này ở loop.go sẽ dừng run trực tiếp, không bao giờ gọi
// StopGuard. Ở đây chỉ liệt kê ngữ nghĩa từ chối của provider thật sự đi
// tới StopGuard.
var hardStopReasons = map[agentcore.StopReason]struct{}{
	"safety":         {},
	"content_filter": {},
}

// newCheckpointDeltaGuard dựng một StopGuard:
// nếu sau baseline chưa xuất hiện checkpoint của step bắt buộc thì từ chối
// end_turn.
// baseline do nơi gọi chụp lại ở thời điểm dựng, bảo đảm ngữ nghĩa đúng cho
// từng run.
//
// blockMsg nhận tập step checkpoint đã quan sát được sau baseline, để dựng
// thông điệp thúc theo tiến độ thực tế — thông điệp tĩnh gây hiểu lầm trong
// kịch bản "công cụ bắt buộc cứ báo lỗi" (thúc model gọi một công cụ đang
// hỏng, xem #75).
//
// Ngữ nghĩa đếm là "có tiến triển thì đặt lại": giữa hai lần chặn mà xuất hiện
// checkpoint mới (draft lại / check…) nghĩa là model đang tiến, consecutive
// về 0; chỉ quay tròn liên tục không ra sản phẩm mới bị cộng dồn và nâng cấp
// thành dừng.
func newCheckpointDeltaGuard(st *store.Store, agentName string, requiredSteps []string, blockMsg func(seen map[string]struct{}) string, onBlock BlockHook) agentcore.StopGuard {
	var baseline int64
	if cp := st.Checkpoints.LatestGlobal(); cp != nil {
		baseline = cp.Seq
	}
	need := make(map[string]struct{}, len(requiredSteps))
	for _, s := range requiredSteps {
		need[s] = struct{}{}
	}
	var consecutive atomic.Int32
	var lastBlockSeq atomic.Int64 // Seq checkpoint mới nhất quan sát được ở lần chặn trước; -1 nghĩa là chưa chặn lần nào
	lastBlockSeq.Store(-1)
	return func(_ context.Context, info agentcore.StopInfo) agentcore.StopDecision {
		// Lỗi không phục hồi được: nâng cấp ngay, không lãng phí một lượt thúc.
		if _, hard := hardStopReasons[info.Message.StopReason]; hard {
			slog.Error("subagent stop_guard phát hiện dừng không phục hồi được, nâng cấp ngay",
				"module", "agent.guard", "agent", agentName,
				"turn", info.TurnIndex, "stop_reason", info.Message.StopReason)
			if onBlock != nil {
				onBlock(agentName, "hard_stop", consecutive.Load())
			}
			return agentcore.StopDecision{Allow: false, Escalate: true}
		}
		// Quét ngược các checkpoint sau baseline để gom các step đã xuất hiện
		// (dùng chung cho quyết định cho đi qua và thông điệp tiến độ).
		// Checkpoint mới nằm ở đuôi, gặp seq <= baseline là break.
		all := st.Checkpoints.All()
		latestSeq := baseline
		seen := make(map[string]struct{})
		for i := len(all) - 1; i >= 0; i-- {
			cp := all[i]
			if cp.Seq <= baseline {
				break
			}
			if cp.Seq > latestSeq {
				latestSeq = cp.Seq
			}
			seen[cp.Step] = struct{}{}
		}
		for s := range need {
			if _, ok := seen[s]; ok {
				consecutive.Store(0)
				return agentcore.StopDecision{Allow: true}
			}
		}
		// Có sản phẩm mới ghi xuống đĩa kể từ lần chặn trước = model đang tiến
		// (ví dụ bị thúc rồi draft lại để thử chốt), nên đặt lại đếm; nâng cấp chỉ
		// nên phạt quay tròn không tiến triển, chứ không gộp mọi lần chặn của cả
		// run để huỷ.
		if prev := lastBlockSeq.Load(); prev >= 0 && latestSeq > prev {
			consecutive.Store(0)
		}
		lastBlockSeq.Store(latestSeq)
		n := consecutive.Add(1)
		if n > subagentMaxConsecutiveBlocks {
			slog.Error("subagent stop_guard chặn liên tục quá giới hạn, nâng cấp thành dừng",
				"module", "agent.guard", "agent", agentName, "turn", info.TurnIndex, "consecutive", n)
			if onBlock != nil {
				onBlock(agentName, "escalated", n)
			}
			return agentcore.StopDecision{Allow: false, Escalate: true}
		}
		slog.Warn("subagent stop_guard chặn end_turn",
			"module", "agent.guard", "agent", agentName, "turn", info.TurnIndex, "consecutive", n)
		if onBlock != nil {
			onBlock(agentName, "blocked", n)
		}
		return agentcore.StopDecision{Allow: false, InjectMessage: blockMsg(seen)}
	}
}

// staticBlockMsg điều chỉnh văn bản tĩnh cho đúng chữ ký blockMsg (sản phẩm của
// kiến trúc sư/biên tập viên ghi xuống đĩa bằng một công cụ, không có tiến độ
// nhiều bước nên thúc tĩnh là đủ).
func staticBlockMsg(msg string) func(map[string]struct{}) string {
	return func(map[string]struct{}) string { return msg }
}

// NewWriterStopGuard yêu cầu writer trong lượt này phải có ít nhất một lần
// commit_chapter thành công.
// Thông điệp thúc được dựng theo tiến độ các step đã ghi xuống đĩa: writer là
// subagent duy nhất có chuỗi công cụ nhiều bước, nên câu tĩnh "phải gọi
// commit_chapter" gây hiểu lầm khi bước trước còn thiếu hoặc chính commit
// đang báo lỗi.
func NewWriterStopGuard(st *store.Store, onBlock BlockHook) agentcore.StopGuard {
	texts := guardTextsFor(storeLang(st))
	return newCheckpointDeltaGuard(st, "writer", []string{"commit"},
		func(seen map[string]struct{}) string { return writerBlockMsg(texts, seen) }, onBlock)
}

// writerBlockMsg dựa trên các checkpoint step đã xuất hiện trong lượt để biết
// writer đang kẹt ở bước nào.
// Tên step tương ứng giá trị ghi xuống đĩa của từng công cụ: plan / draft / edit
// / consistency_check / commit.
func writerBlockMsg(texts *guardTexts, seen map[string]struct{}) string {
	_, hasDraft := seen["draft"]
	_, hasEdit := seen["edit"]
	_, hasCheck := seen["consistency_check"]
	switch {
	case !hasDraft && !hasEdit:
		return texts.writerNoDraft
	case !hasCheck:
		return texts.writerNoCheck
	default:
		return texts.writerNeedFinal
	}
}

// NewArchitectStopGuard yêu cầu architect trong lượt này phải ghi xuống đĩa
// ít nhất một sản phẩm kế hoạch.
func NewArchitectStopGuard(st *store.Store, onBlock BlockHook) agentcore.StopGuard {
	texts := guardTextsFor(storeLang(st))
	return newCheckpointDeltaGuard(st, "architect",
		[]string{
			"book", "premise", "outline", "layered_outline", "characters", "world_rules",
			"foundation_audit", "expand_arc", "append_volume", "update_compass", "complete_book", "revise_outline", "resolve_outline_feedback", "plan",
		},
		staticBlockMsg(texts.architectNeedArtifact),
		onBlock,
	)
}

// NewEditorStopGuard yêu cầu editor ghi xuống đĩa sản phẩm khớp với "nhiệm vụ"
// của lượt này rồi mới được kết thúc.
//
// Nhận biết nhiệm vụ: khi được phân công sinh tóm tắt, chỉ save_review (soát
// duyệt) không tính là hoàn tất — phải sinh đúng tóm tắt tương ứng. Nếu không,
// editor "được phân công sinh tóm tắt cung nhưng lại soát duyệt trước" sẽ thoả
// mãn tiêu chí lỏng cũ và kết thúc sớm, khiến tóm tắt cung không bao giờ được
// ghi xuống đĩa (kèm việc dispatcher khử trùng bị câm, đã từng gây vòng lặp vô
// hạn ở cung khung trong tập, xem outline-exhaustion-livelock).
// Lần thoát bằng công cụ trạng thái cuối cũng vẫn hỏi ý kiến StopGuard (hợp
// đồng kiểm thử TestContract_TerminalToolExitConsultsStopGuard), nên save_review
// dừng cứng ở build.go là an toàn: trong nhiệm vụ sinh tóm tắt, khi editor soát
// duyệt trước, guard này sẽ bác lần thoát đó và thúc tiếp cho tới khi đúng
// tóm tắt được ghi xuống đĩa.
func NewEditorStopGuard(st *store.Store, task string, onBlock BlockHook) agentcore.StopGuard {
	texts := guardTextsFor(storeLang(st))
	switch {
	case strings.Contains(task, "save_volume_summary") ||
		strings.Contains(task, "卷摘要") ||
		strings.Contains(task, "tóm tắt tập"):
		return newCheckpointDeltaGuard(st, "editor", []string{"volume_summary"},
			staticBlockMsg(texts.editorVolumeSummary), onBlock)
	case strings.Contains(task, "save_arc_summary") ||
		strings.Contains(task, "弧摘要") ||
		strings.Contains(task, "tóm tắt cung"):
		return newCheckpointDeltaGuard(st, "editor", []string{"arc_summary"},
			staticBlockMsg(texts.editorArcSummary), onBlock)
	default:
		// Nhiệm vụ biên tập hoặc tạm thời: bất kỳ bản biên tập/tóm tắt nào ghi
		// xuống đĩa cũng được (giữ hành vi lỏng sẵn có).
		return newCheckpointDeltaGuard(st, "editor",
			[]string{"review", "arc_summary", "volume_summary"},
			staticBlockMsg(texts.editorAnyArtifact), onBlock)
	}
}
