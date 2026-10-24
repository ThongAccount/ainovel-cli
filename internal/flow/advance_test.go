package flow

import (
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

func TestStartsForwardChapter(t *testing.T) {
	base := &domain.Progress{Phase: domain.PhaseWriting, CompletedChapters: []int{1}}
	tests := []struct {
		name    string
		inst    *Instruction
		p       *domain.Progress
		pending *domain.PendingCommit
		want    bool
	}{
		{"Chương kế tiếp bình thường", &Instruction{Agent: "writer", Chapter: 2}, base, nil, true},
		{"Số chương bằng 0 thì suy từ sự thật", &Instruction{Agent: "writer"}, base, nil, true},
		{"Văn bản không tham gia", &Instruction{Agent: "writer", Chapter: 2, Task: "tuỳ ý", Reason: "tuỳ ý"}, base, nil, true},
		{"Writer đang làm lại", &Instruction{Agent: "writer", Chapter: 1}, &domain.Progress{Phase: domain.PhaseWriting, CompletedChapters: []int{1}, PendingRewrites: []int{1}}, nil, false},
		{"Khôi phục chương", &Instruction{Agent: "writer", Chapter: 2}, &domain.Progress{Phase: domain.PhaseWriting, CompletedChapters: []int{1}, InProgressChapter: 2}, nil, false},
		{"Khôi phục commit", &Instruction{Agent: "writer", Chapter: 2}, base, &domain.PendingCommit{Chapter: 2}, false},
		{"Không phải chương kế tiếp", &Instruction{Agent: "writer", Chapter: 3}, base, nil, false},
		{"Editor", &Instruction{Agent: "editor"}, base, nil, false},
		{"Chỉ dẫn rỗng", nil, base, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := StartsForwardChapter(tc.inst, tc.p, tc.pending); got != tc.want {
				t.Fatalf("StartsForwardChapter() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveAdvanceHold(t *testing.T) {
	tests := []struct {
		name    string
		hold    *domain.AdvanceHold
		p       *domain.Progress
		want    AdvanceHoldResolution
		wantErr bool
	}{
		{"Không có hold", nil, &domain.Progress{Phase: domain.PhaseWriting}, AdvanceHoldKeep, false},
		{"Tạm dừng tại ranh giới", &domain.AdvanceHold{After: domain.AdvanceHoldAtBoundary, Reason: "Dừng"}, &domain.Progress{Phase: domain.PhaseWriting, PendingRewrites: []int{1}}, AdvanceHoldConsumeAndStop, false},
		{"Làm lại chưa ráo", &domain.AdvanceHold{After: domain.AdvanceHoldAfterRewritesDrained, Reason: "Nghiệm thu"}, &domain.Progress{Phase: domain.PhaseWriting, PendingRewrites: []int{1}}, AdvanceHoldKeep, false},
		{"Làm lại đã ráo", &domain.AdvanceHold{After: domain.AdvanceHoldAfterRewritesDrained, Reason: "Nghiệm thu"}, &domain.Progress{Phase: domain.PhaseWriting}, AdvanceHoldConsumeAndStop, false},
		{"Chương đích chưa xong", &domain.AdvanceHold{After: domain.AdvanceHoldAtChapter, TargetChapter: 3, Reason: "Viết tới chương 3"}, &domain.Progress{Phase: domain.PhaseWriting, CompletedChapters: []int{1, 2}}, AdvanceHoldKeep, false},
		{"Chương đích đã xong", &domain.AdvanceHold{After: domain.AdvanceHoldAtChapter, TargetChapter: 3, Reason: "Viết tới chương 3"}, &domain.Progress{Phase: domain.PhaseWriting, CompletedChapters: []int{1, 2, 3}}, AdvanceHoldConsumeAndStop, false},
		{"Thiếu chương đích", &domain.AdvanceHold{After: domain.AdvanceHoldAtChapter, Reason: "Viết tới chương đích"}, &domain.Progress{Phase: domain.PhaseWriting}, AdvanceHoldKeep, true},
		{"Hoàn thành chỉ tiêu thụ", &domain.AdvanceHold{After: domain.AdvanceHoldAtBoundary, Reason: "Dừng"}, &domain.Progress{Phase: domain.PhaseComplete}, AdvanceHoldConsume, false},
		{"Điều kiện lạ", &domain.AdvanceHold{After: "unknown", Reason: "Dừng"}, &domain.Progress{Phase: domain.PhaseWriting}, AdvanceHoldKeep, true},
		{"Thiếu tiến độ", &domain.AdvanceHold{After: domain.AdvanceHoldAtBoundary, Reason: "Dừng"}, nil, AdvanceHoldKeep, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveAdvanceHold(tc.hold, tc.p)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("resolution = %v, want %v", got, tc.want)
			}
		})
	}
}
