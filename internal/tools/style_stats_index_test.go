package tools

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/stylestat"
)

func TestStyleStatsIndexAppendRewriteAndRemove(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}

	chapters := map[int]string{
		1: "# Gió nổi\nTrong đêm, anh không do dự mà sợ hãi.\nCả đời chưa đi xa, mong người thay tôi ngắm núi biển nơi xa.\nAnh bước đi.",
		2: "# Mây dâng\nBuổi sớm, cô im lặng mấy nhịp thở.\nCả đời chưa đi xa, mong người thay tôi ngắm núi biển nơi xa.\nTrời sáng.",
		3: "# Sấm vang\nÁnh lạnh lướt qua mắt Lưu Cửu Uyên.\nCả đời chưa đi xa, mong người thay tôi ngắm núi biển nơi xa.\nKhông ai đáp.",
		4: "# Thuận triều ngầm\nMọi người cảm thấy mưa gió sắp tới.\nTiếng chuông vọng từ cuối phố dài.\nCánh cửa mở.",
		5: "# Lối về\nNhư một giấc mơ cũ nằm nặng trên đỉnh núi.\nMột thứ lạnh lẽo không nói thành lời lan ra.\nNgọn đèn tắt.",
		6: "# Cửa núi\nTrái tim anh thắt lại, nhưng anh không quay đầu.\nCâu chuyện vẫn phải tiếp tục đi về phía trước.",
	}
	for chapter, text := range chapters {
		if err := st.Drafts.SaveFinalChapter(chapter, text); err != nil {
			t.Fatal(err)
		}
	}

	titles := []string{"第一章 风起", "Mây dâng", "第3章 雷动", "Thuận triều ngầm", "Lối về", "Cửa núi"}
	stopwords := []string{"Lưu Cửu Uyên"}
	index := NewStyleStatsIndex(st)
	completed := []int{1, 2, 3, 4, 5, 6}
	assertStyleStatsIndexMatchesCompute(t, index, chapters, completed, titles, stopwords)

	chapters[7] = "# Sương tan\nNhư tỉnh giấc khỏi giấc mơ cũ, anh không nói gì.\nGió ngừng."
	if err := st.Drafts.SaveFinalChapter(7, chapters[7]); err != nil {
		t.Fatal(err)
	}
	index.ChapterCommitted(7, chapters[7])
	completed = append(completed, 7)
	assertStyleStatsIndexMatchesCompute(t, index, chapters, completed, titles, stopwords)

	chapters[2] = "# Mây dâng\nKhi bình minh, cô thấy nặng ngực.\nCâu dài sau khi viết lại chỉ xuất hiện ở chương này, không được lặp lại qua các chương.\nGió ngừng."
	if err := st.Drafts.SaveFinalChapter(2, chapters[2]); err != nil {
		t.Fatal(err)
	}
	index.ChapterCommitted(2, chapters[2])
	assertStyleStatsIndexMatchesCompute(t, index, chapters, completed, titles, stopwords)

	delete(chapters, 4)
	completed = []int{1, 2, 3, 5, 6, 7}
	assertStyleStatsIndexMatchesCompute(t, index, chapters, completed, titles, stopwords)
}

func TestStyleStatsIndexSurfacesMissingCompletedChapter(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	_, err := NewStyleStatsIndex(st).Snapshot([]int{1}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "chương 1 đã đánh dấu hoàn thành nhưng không có bản chính") {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
}

func TestStyleStatsDependencyIsRequired(t *testing.T) {
	st := store.NewStore(t.TempDir())
	tests := []struct {
		name string
		new  func()
	}{
		{
			name: "context",
			new: func() {
				NewContextTool(st, References{}, "default", nil)
			},
		},
		{
			name: "commit",
			new: func() {
				NewCommitChapterTool(st, nil)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("StyleStatsIndex nil phải thất bại ngay lập tức")
				}
			}()
			tt.new()
		})
	}
}

func assertStyleStatsIndexMatchesCompute(
	t *testing.T,
	index *StyleStatsIndex,
	chapters map[int]string,
	completed []int,
	titles, stopwords []string,
) {
	t.Helper()
	ids := append([]int(nil), completed...)
	sort.Ints(ids)
	texts := make([]string, 0, len(ids))
	for _, chapter := range ids {
		texts = append(texts, chapters[chapter])
	}
	want := stylestat.Compute(stylestat.Input{
		Chapters:  texts,
		Titles:    titles,
		Stopwords: stopwords,
	})
	got, err := index.Snapshot(completed, titles, stopwords)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chỉ mục không khớp\n có: %+v\n cần: %+v", got, want)
	}
}
