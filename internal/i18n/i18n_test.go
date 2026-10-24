package i18n

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]Lang{
		"vi":      LangVI,
		"VI":      LangVI,
		" vi ":    LangVI,
		"zh":      LangZH,
		"ZH":      LangZH,
		"chinese": LangZH,
		"cn":      LangZH,
		"":        LangVI, // rỗng → tiếng Việt, không bao giờ rơi về tiếng Trung
		"en":      LangVI,
		"junk":    LangVI,
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q want %q", in, got, want)
		}
	}
}

func TestTable(t *testing.T) {
	vi := struct{ A, B string }{"vi-a", "vi-b"}
	zh := struct{ A, B string }{"zh-a", "zh-b"}
	if got := Table(vi, zh, LangVI); got.A != "vi-a" || got.B != "vi-b" {
		t.Errorf("Table(vi) = %+v", got)
	}
	if got := Table(vi, zh, LangZH); got.A != "zh-a" || got.B != "zh-b" {
		t.Errorf("Table(zh) = %+v", got)
	}
}

func TestTextResolve(t *testing.T) {
	tx := Text{VI: "Chương %d", ZH: "第 %d 章"}
	if got := tx.Resolve(LangVI); got != "Chương %d" {
		t.Errorf("Resolve(vi) = %q", got)
	}
	if got := tx.Resolve(LangZH); got != "第 %d 章" {
		t.Errorf("Resolve(zh) = %q", got)
	}
}

func TestLangResolve(t *testing.T) {
	if got := LangVI.Resolve("vi", "zh"); got != "vi" {
		t.Errorf("LangVI.Resolve = %q", got)
	}
	if got := LangZH.Resolve("vi", "zh"); got != "zh" {
		t.Errorf("LangZH.Resolve = %q", got)
	}
}
