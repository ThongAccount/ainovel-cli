package store

import (
	"os"
	"slices"
	"sort"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// CastStore quản lý danh mục diễn viên phụ (meta/cast_ledger.json).
//
// Danh mục diễn viên phụ ghi lại "nhân vật phụ có tên đã từng xuất hiện", độc lập với
// characters.json (hồ sơ nhân vật chính):
//   - characters.json: vai chính + nhân vật phụ then chốt do Kiến trúc sư thiết kế
//     tường minh, không sửa trong lúc viết
//   - cast_ledger.json: công cụ commit_chapter tự cộng dồn, gồm mọi nhân vật phụ
//     có tên mà không thuộc hồ sơ chính
//
// MergeAppearances là idempotent: commit lặp lại trong cùng một chương không làm
// AppearanceCount tăng hai lần.
type CastStore struct{ io *IO }

func NewCastStore(io *IO) *CastStore { return &CastStore{io: io} }

const castLedgerPath = "meta/cast_ledger.json"

// Load đọc danh mục diễn viên phụ. Trả về lát cắt rỗng nếu tệp không tồn tại.
func (s *CastStore) Load() ([]domain.CastEntry, error) {
	var entries []domain.CastEntry
	if err := s.io.ReadJSON(castLedgerPath, &entries); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

// Save lưu toàn bộ danh mục diễn viên phụ (ghi nguyên tử).
func (s *CastStore) Save(entries []domain.CastEntry) error {
	return s.io.WriteJSON(castLedgerPath, entries)
}

// MergeAppearances gộp các lần xuất hiện trong chương này vào danh mục.
//
// Tham số:
//   - chapter: số chương
//   - characters: mảng tên nhân vật xuất hiện trong chương (từ commit_chapter.Characters)
//   - intros: mô tả nhân vật mới do Người viết khai báo tường minh (lần xuất hiện
//     đầu hoặc bổ sung BriefRole)
//   - knownCore: tập tên nhân vật chính đã có trong characters.json (những tên này
//     bỏ qua ghi vào ledger)
//
// Hành vi:
//   - tên nằm trong knownCore: bỏ qua (hồ sơ nhân vật chính là lối ghi duy nhất)
//   - tên đã có trong ledger và chapter đã nằm trong AppearanceChapters: bỏ qua
//     hoàn toàn (idempotent)
//   - tên đã có trong ledger nhưng chapter là mới: cập nhật LastSeenChapter,
//     thêm chapter và tăng count
//   - tên chưa có trong ledger: tạo mục mới
//   - BriefRole trong intros chỉ được dùng khi BriefRole của mục ledger còn rỗng,
//     để không đè lên mô tả sớm hơn
func (s *CastStore) MergeAppearances(
	chapter int,
	characters []string,
	intros []domain.CastIntro,
	knownCore map[string]bool,
) error {
	if chapter <= 0 || len(characters) == 0 {
		return nil
	}
	return s.io.WithWriteLock(func() error {
		var entries []domain.CastEntry
		if err := s.io.ReadJSONUnlocked(castLedgerPath, &entries); err != nil && !os.IsNotExist(err) {
			return err
		}

		introMap := make(map[string]string, len(intros))
		for _, in := range intros {
			if in.Name != "" {
				introMap[in.Name] = in.BriefRole
			}
		}

		index := make(map[string]int, len(entries))
		for i, e := range entries {
			index[e.Name] = i
			for _, alias := range e.Aliases {
				index[alias] = i
			}
		}

		seen := make(map[string]bool, len(characters))
		for _, name := range characters {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			if knownCore[name] {
				continue
			}
			if i, ok := index[name]; ok {
				entry := &entries[i]
				if !slices.Contains(entry.AppearanceChapters, chapter) {
					entry.AppearanceChapters = append(entry.AppearanceChapters, chapter)
					entry.AppearanceCount = len(entry.AppearanceChapters)
					if chapter > entry.LastSeenChapter {
						entry.LastSeenChapter = chapter
					}
					if chapter < entry.FirstSeenChapter || entry.FirstSeenChapter == 0 {
						entry.FirstSeenChapter = chapter
					}
				}
				if entry.BriefRole == "" {
					if br, ok := introMap[name]; ok && br != "" {
						entry.BriefRole = br
					}
				}
				continue
			}
			entries = append(entries, domain.CastEntry{
				Name:               name,
				BriefRole:          introMap[name],
				FirstSeenChapter:   chapter,
				LastSeenChapter:    chapter,
				AppearanceCount:    1,
				AppearanceChapters: []int{chapter},
			})
		}
		return s.io.WriteJSONUnlocked(castLedgerPath, entries)
	})
}

// RecentActive trả về N mục diễn viên phụ hoạt động gần nhất (sắp theo
// LastSeenChapter giảm dần). Dùng cho novel_context gọi lại "nhân vật phụ vừa
// xuất hiện" mà Người viết có thể cần khi viết chương kế tiếp.
//
// Các mục đã được nâng lên characters.json (Promoted=true) sẽ bị bỏ qua để tránh
// gọi lại trùng với hồ sơ chính.
func (s *CastStore) RecentActive(limit int) ([]domain.CastEntry, error) {
	if limit <= 0 {
		return nil, nil
	}
	entries, err := s.Load()
	if err != nil {
		return nil, err
	}
	active := entries[:0:0]
	for _, e := range entries {
		if e.Promoted {
			continue
		}
		active = append(active, e)
	}
	if len(active) == 0 {
		return nil, nil
	}
	sort.Slice(active, func(i, j int) bool {
		if active[i].LastSeenChapter != active[j].LastSeenChapter {
			return active[i].LastSeenChapter > active[j].LastSeenChapter
		}
		return active[i].AppearanceCount > active[j].AppearanceCount
	})
	if len(active) > limit {
		active = active[:limit]
	}
	return active, nil
}
