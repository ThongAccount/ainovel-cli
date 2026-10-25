package tools

import (
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// setLang đặt ngôn ngữ tiến trình trong suốt kiểm thử và khôi phục sau đó.
// i18n.Active() là biến toàn cục tiến trình (atomic.Value): không dùng
// t.Parallel() trong kiểm thử này, phải t.Cleanup để tránh ảnh hưởng các
// kiểm thử khác trong cùng gói.
func setLang(t *testing.T, lang string) {
	t.Helper()
	prev := string(i18n.Active())
	i18n.SetActive(lang)
	t.Cleanup(func() { i18n.SetActive(prev) })
}

func TestParsePremiseSections(t *testing.T) {
	setLang(t, string(i18n.LangZH))
	premise := `# Premise

## 题材和基调
东方玄幻，冷硬成长。

## 题材定位
东方玄幻升级流，面向追求爽点和关系推进的读者。

## 核心冲突
主角必须在宗门规则与个人良知之间做选择。

## 中期转向
旧有修炼路线失效，必须转向禁术体系。
`

	sections := parsePremiseSections(premise)
	if sections["题材和基调"] == "" {
		t.Fatalf("expected 题材和基调 section, got %+v", sections)
	}
	if sections["题材定位"] == "" {
		t.Fatalf("expected 题材定位 section, got %+v", sections)
	}
	if sections["核心冲突"] == "" {
		t.Fatalf("expected 核心冲突 section, got %+v", sections)
	}
	if sections["中段转折"] == "" {
		t.Fatalf("expected 中期转向 alias normalized to 中段转折, got %+v", sections)
	}
}

func TestParsePremiseSectionsTiengViet(t *testing.T) {
	setLang(t, string(i18n.LangVI))
	premise := `# Premise

## Thể loại và giọng điệu
Viễn tưởng hàn lâm, lạnh lùng.

## Định vị thể loại
Viễn tưởng hàn lâm theo dòng tuấn tuý, nhắm độc giả thích điểm mạnh và quan hệ tiến triển.

## Xung đột cốt lõi
Chính nhân vật phải chọn giữa luật tông môn và lương tâm cá nhân.

## Bước ngoặt giữa chừng
Đường tu luyện cũ thất bại, phải chuyển sang hệ thống cấm thuật.
`

	sections := parsePremiseSections(premise)
	if sections["Thể loại và giọng điệu"] == "" {
		t.Fatalf("expected Thể loại và giọng điệu section, got %+v", sections)
	}
	if sections["Định vị thể loại"] == "" {
		t.Fatalf("expected Định vị thể loại section, got %+v", sections)
	}
	if sections["Xung đột cốt lõi"] == "" {
		t.Fatalf("expected Xung đột cốt lõi section, got %+v", sections)
	}
	if sections["Bước ngoặt giữa chừng"] == "" {
		t.Fatalf("expected Bước ngoặt giữa chừng section, got %+v", sections)
	}
}

func TestPremiseStructure(t *testing.T) {
	setLang(t, string(i18n.LangZH))
	premise := `## 题材和基调
升级流，偏冷硬。

## 题材定位
升级流

## 核心冲突
冲突

## 主角目标
目标

## 终局方向
终局

## 写作禁区
禁区

## 差异化卖点
卖点

## 差异化钩子
钩子

## 核心兑现承诺
兑现

## 故事引擎
引擎

## 中段转折
转折
`

	structure := premiseStructure(premise, domain.PlanningTierMid)
	if ready, _ := structure["template_ready"].(bool); !ready {
		t.Fatalf("expected template_ready, got %+v", structure)
	}
	missing, _ := structure["missing"].([]string)
	if len(missing) != 0 {
		t.Fatalf("expected no missing headings, got %+v", missing)
	}
}

func TestPremiseStructureTiengViet(t *testing.T) {
	setLang(t, string(i18n.LangVI))
	premise := `## Thể loại và giọng điệu
Hành động, lạnh lùng.

## Định vị thể loại
Hành động

## Xung đột cốt lõi
Xung đột

## Mục tiêu chính
Mục tiêu

## Hướng kết cục
Kết cục

## Vùng cấm viết
Vùng cấm

## Điểm bán hấp dẫn
Điểm hấp dẫn

## Móc câu chuyện
Móc

## Lời hứa thực hiện
Hứa

## Động cơ câu chuyện
Động cơ

## Bước ngoặt giữa chừng
Bước ngoặt
`

	structure := premiseStructure(premise, domain.PlanningTierMid)
	if ready, _ := structure["template_ready"].(bool); !ready {
		t.Fatalf("expected template_ready, got %+v", structure)
	}
	missing, _ := structure["missing"].([]string)
	if len(missing) != 0 {
		t.Fatalf("expected no missing headings, got %+v", missing)
	}
}

func TestPremiseStructureShortAcceptsLegacyHeadingAlias(t *testing.T) {
	setLang(t, string(i18n.LangZH))
	premise := `## 题材和基调
单卷高压营救。

## 题材定位
短篇高密度冒险。

## 核心冲突
主角必须在一夜内救出人质。

## 主角目标
救出人质并活着离开。

## 结局方向
完成任务但付出代价。

## 写作禁区
不扩展成长期连载。

## 差异化卖点
时限压力与连续反转。

## 差异化钩子
每次选择都缩短救援时间。

## 核心兑现承诺
紧迫感、抉择与反转。

## 本作为什么适合短篇/单卷收束
核心矛盾和人物弧线都能在单次任务中完成。
`

	structure := premiseStructure(premise, domain.PlanningTierShort)
	if ready, _ := structure["template_ready"].(bool); !ready {
		t.Fatalf("expected short template_ready, got %+v", structure)
	}
}

func TestPremiseStructureShortTiengViet(t *testing.T) {
	setLang(t, string(i18n.LangVI))
	premise := `## Thể loại và giọng điệu
Cứu một lần áp lực cao.

## Định vị thể loại
Hành động ngắn, mật độ cao.

## Xung đột cốt lõi
Chính nhân vật phải cứu nhân vật trong một đêm.

## Mục tiêu chính
Cứu nhân vật và thoát hiểm còn sống.

## Hướng kết cục
Hoàn thành nhiệm vụ nhưng phải trả giá.

## Vùng cấm viết
Không mở rộng thành kết nối dài hạn.

## Điểm bán hấp dẫn
Áp lực thời hạn và phản chuyển liên tục.

## Móc câu chuyện
Mỗi lần lựa chọn đều rút ngắn thời gian cứu giúp.

## Lời hứa thực hiện
Khẩn cấp, quyết định và phản chuyển.

## Khả năng thích nghi truyện ngắn
Xung đột cốt lõi và nhân vật cung tuyến đều có thể hoàn thành trong một nhiệm vụ duy nhất.
`

	structure := premiseStructure(premise, domain.PlanningTierShort)
	if ready, _ := structure["template_ready"].(bool); !ready {
		t.Fatalf("expected short template_ready, got %+v", structure)
	}
}
