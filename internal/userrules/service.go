package userrules

import (
	"context"
	"log/slog"
	"strings"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/store"
)

// Service điều phối việc sinh và cập nhật ảnh chụp quy tắc người dùng: chuẩn
// hóa từng nguồn → hợp nhất tất định → ghi xuống đĩa.
//
// Hai nơi gọi dùng chung một bộ logic:
//   - mở sách/làm mới: Build / GetOrBuild, do Host gọi một cách tất định.
//   - cập nhật lúc chạy: sau khi Arbiter trích rules, Host gọi AddRuntimeRule.
type Service struct {
	store     *store.Store
	norm      *Normalizer
	rulesOpts rules.LoadOptions
}

// NewService dựng dịch vụ. model dùng cho chuẩn hóa (nên là mô hình mạnh); khi
// model là nil thì mọi nguồn hạ cấp thành raw preferences (vẫn sinh được ảnh
// chụp, kiểm tra máy do system_defaults đảm bảo).
//
// Ngôn ngữ tác phẩm lấy từ st.Language() — ngôn ngữ của sách là thứ quyết
// định, không phụ thuộc ngôn ngữ chung của tiến trình.
func NewService(st *store.Store, model agentcore.ChatModel, opts rules.LoadOptions) *Service {
	return &Service{
		store: st,
		norm: newNormalizer(model, func() i18n.Lang {
			if st == nil {
				return i18n.Active()
			}
			return i18n.Normalize(st.Language())
		}),
		rulesOpts: opts,
	}
}

// normalizeOrDegrade chuẩn hóa một nguồn; khi thất bại thì ghi lại nguyên nhân
// thật và hạ cấp thành raw preferences
// (ảnh chụp Status=degraded, giữ nguyên văn) — hạ cấp là sự thật nhìn thấy
// được, nguyên nhân lỗi chỉ vào nhật ký.
func (s *Service) normalizeOrDegrade(ctx context.Context, source, text string) rules.Candidate {
	cand, err := s.norm.Normalize(ctx, source, text)
	if err != nil {
		slog.Warn("chuẩn hóa quy tắc thất bại, hạ cấp thành sở thích nguyên văn", "module", "rules", "source", source, "err", err)
		return degraded(source, text)
	}
	return cand
}

// Build chuẩn hóa các nguồn tĩnh (system_defaults + tệp rules + prompt khởi
// chạy) thành ảnh chụp rồi ghi xuống đĩa.
// Dùng khi mở sách/làm mới. startupPrompt có thể rỗng.
func (s *Service) Build(ctx context.Context, startupPrompt string) (*rules.Snapshot, error) {
	cands := []rules.Candidate{rules.SystemDefaults()}
	for _, rs := range rules.RawFileSources(s.rulesOpts) {
		cands = append(cands, s.normalizeOrDegrade(ctx, rs.Label, rs.Text))
	}
	if strings.TrimSpace(startupPrompt) != "" {
		cands = append(cands, s.normalizeOrDegrade(ctx, "startup_prompt", startupPrompt))
	}
	snap := rules.BuildSnapshot(cands)
	if err := s.store.UserRules.Save(&snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// GetOrBuild trả về ảnh chụp hiện tại; khi thiếu thì khởi tạo theo
// system_defaults + tệp rules.
// Mọi đường đọc lúc chạy đều đi qua đây.
func (s *Service) GetOrBuild(ctx context.Context) (*rules.Snapshot, error) {
	cur, err := s.store.UserRules.Load()
	if err != nil {
		return nil, err
	}
	if cur != nil {
		return cur, nil
	}
	return s.Build(ctx, "")
}

// AddRuntimeRule chuẩn hóa một quy tắc dài hạn phát sinh lúc chạy, chồng lên
// ảnh chụp hiện tại với ưu tiên cao nhất rồi ghi xuống đĩa.
// Không bao giờ báo lỗi vì chuẩn hóa hỏng — mục đó hạ cấp thành raw
// preferences.
// Trả về ảnh chụp sau khi chồng và ứng viên chuẩn hóa của lần này.
func (s *Service) AddRuntimeRule(ctx context.Context, text string) (*rules.Snapshot, rules.Candidate, error) {
	cur, err := s.GetOrBuild(ctx)
	if err != nil {
		return nil, rules.Candidate{}, err
	}
	cand := s.normalizeOrDegrade(ctx, "runtime_update", text)
	merged := rules.OverlaySnapshot(*cur, cand)
	if err := s.store.UserRules.Save(&merged); err != nil {
		return nil, cand, err
	}
	return &merged, cand, nil
}
