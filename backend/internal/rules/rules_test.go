package rules

import (
	"strings"
	"testing"
	"time"
)

func TestSuggestSkiBeginner(t *testing.T) {
	s := SuggestSki(Profile{HeightCM: 170, WeightKG: 65, Level: "beginner"})
	// 170*0.85 = 144.5 -> 145
	if s.Size != "145cm" {
		t.Fatalf("beginner ski = %s, want 145cm", s.Size)
	}
	if !SkiFitOK(145, 150) || !SkiFitOK(145, 140) {
		t.Fatal("±5cm should fit")
	}
	if SkiFitOK(145, 151) {
		t.Fatal("+6cm should not fit")
	}
}

func TestSuggestSkiAdvancedHeavy(t *testing.T) {
	// 身高180 体重90 -> BMI 27.8 >=26, 高级 0.95: 171+5=176 -> 175
	s := SuggestSki(Profile{HeightCM: 180, WeightKG: 90, Level: "advanced"})
	if s.Size != "175cm" {
		t.Fatalf("advanced heavy ski = %s, want 175cm", s.Size)
	}
}

func TestBootFit(t *testing.T) {
	s := SuggestBoot(Profile{FootEU: 42})
	if s.Size != "EU42" {
		t.Fatalf("boot = %s", s.Size)
	}
	if !BootFitOK(42, 42.5) || BootFitOK(42, 43) {
		t.Fatal("boot tolerance ±0.5")
	}
	s2 := SuggestBoot(Profile{FootEU: 42.5})
	if s2.Size != "EU42.5" {
		t.Fatalf("boot half size = %s", s2.Size)
	}
}

func TestHelmetSizes(t *testing.T) {
	cases := map[int]string{50: "S", 56: "M", 59: "L", 62: "XL", 0: "M"}
	for head, want := range cases {
		if got, _ := helmetSize(head); got != want {
			t.Fatalf("head %d = %s want %s", head, got, want)
		}
	}
}

func TestPole(t *testing.T) {
	s := SuggestPole(Profile{HeightCM: 170})
	// 170*0.7=119 -> 120
	if s.Size != "120cm" {
		t.Fatalf("pole = %s want 120cm", s.Size)
	}
	b := SuggestPole(Profile{HeightCM: 170, Level: "beginner"})
	if !b.Optional {
		t.Fatal("beginner pole should be optional")
	}
}

func TestProtector(t *testing.T) {
	if SuggestProtector(Profile{HeightCM: 180, WeightKG: 70}).Size != "XL" {
		t.Fatal("180cm -> XL")
	}
	if SuggestProtector(Profile{HeightCM: 140, WeightKG: 50}).Size != "S" {
		t.Fatal("140cm -> S")
	}
	if SuggestProtector(Profile{HeightCM: 160, WeightKG: 55}).Size != "M" {
		t.Fatal("160cm/55kg -> M")
	}
	if SuggestProtector(Profile{HeightCM: 170, WeightKG: 70}).Size != "L" {
		t.Fatal("170cm/70kg -> L")
	}
}

func TestAllSuggestionsCount(t *testing.T) {
	if n := len(AllSuggestions(Profile{HeightCM: 170, WeightKG: 65, FootEU: 42, HeadCM: 57, Level: "beginner"})); n != 5 {
		t.Fatalf("got %d suggestions, want 5", n)
	}
}

func TestDamageDeduction(t *testing.T) {
	d, r := DamageDeduction("edge_damage", "medium", 1000)
	if d != 350 || r.RetireItem {
		t.Fatalf("edge medium = %v retire=%v", d, r.RetireItem)
	}
	d, _ = DamageDeduction("edge_damage", "light", 1000)
	if d != 175 {
		t.Fatalf("edge light = %v want 175", d)
	}
	d, _ = DamageDeduction("edge_damage", "heavy", 1000)
	if d != 525 {
		t.Fatalf("edge heavy = %v want 525", d)
	}
	d, r = DamageDeduction("lost", "lost", 800)
	if d != 800 || !r.RetireItem {
		t.Fatalf("lost = %v", d)
	}
	d, _ = DamageDeduction("shell_crack", "heavy", 1000)
	if d != 750 { // 0.5*1.5 capped below deposit
		t.Fatalf("crack heavy = %v want 750", d)
	}
}

func TestClaim(t *testing.T) {
	// 覆盖：扣 350，免赔 100，上限 1000 -> 赔 250
	c := CalcClaim(350, 100, 1000, true)
	if c.Payout != 250 {
		t.Fatalf("payout = %v want 250", c.Payout)
	}
	// 低于免赔
	c = CalcClaim(50, 100, 1000, true)
	if c.Payout != 0 {
		t.Fatalf("low claim payout = %v want 0", c.Payout)
	}
	// 超保额
	c = CalcClaim(1200, 100, 500, true)
	if c.Payout != 500 {
		t.Fatalf("capped payout = %v want 500", c.Payout)
	}
	// 不覆盖
	c = CalcClaim(350, 0, 1000, false)
	if c.Payout != 0 || c.Covered {
		t.Fatal("uncovered claim must not pay")
	}
}

func TestLateFeeAndNight(t *testing.T) {
	if LateFee(2, 400) != 200 {
		t.Fatalf("late fee = %v want 200", LateFee(2, 400))
	}
	if LateFee(0, 400) != 0 {
		t.Fatal("zero hours = zero fee")
	}
}

func TestRefitTargetEU(t *testing.T) {
	// 当前 EU42 过松、脚码 42：目标收半码 EU41.5
	if got := RefitTargetEU(42, 42); got != 41.5 {
		t.Fatalf("target = %v want 41.5", got)
	}
	// 当前 EU43 明显偏大、脚码 42：目标收敛到脚码 EU42
	if got := RefitTargetEU(42, 43); got != 42 {
		t.Fatalf("target = %v want 42", got)
	}
	// 脚码带半码：当前 EU44 过松、脚码 43.5 -> EU43.5
	if got := RefitTargetEU(43.5, 44); got != 43.5 {
		t.Fatalf("target = %v want 43.5", got)
	}
	// 下限保护：脚码已到最小档时仍按最小档推荐（库存无更小码，柜台可换同码新件）
	if got := RefitTargetEU(34, 34); got != 34 {
		t.Fatalf("target = %v want 34（夹在 34~48）", got)
	}
}

func TestRefitEstimateMinutes(t *testing.T) {
	if RefitEstimateMinutes(0) != 10 {
		t.Fatal("空队列基础 10 分钟")
	}
	if RefitEstimateMinutes(3) != 25 {
		t.Fatal("3 人排队 = 10+15 = 25 分钟")
	}
	if RefitEstimateMinutes(-1) != 10 {
		t.Fatal("负数按空队列处理")
	}
}

func TestRefitDelayMinutes(t *testing.T) {
	start := time.Date(2026, 9, 26, 10, 0, 0, 0, time.Local)
	// 完成时刻早于课程开始：不顺延
	if d := RefitDelayMinutes(start, start.Add(-5*time.Minute)); d != 0 {
		t.Fatalf("提前完成不应顺延，got %d", d)
	}
	// 晚 7 分钟 -> 向上 5 分钟取整 = 10
	if d := RefitDelayMinutes(start, start.Add(7*time.Minute)); d != 10 {
		t.Fatalf("晚 7 分钟应顺延 10，got %d", d)
	}
	// 恰好 20 分钟 -> 20
	if d := RefitDelayMinutes(start, start.Add(20*time.Minute)); d != 20 {
		t.Fatalf("晚 20 分钟应顺延 20，got %d", d)
	}
	// 无课程（零值）不顺延
	if d := RefitDelayMinutes(time.Time{}, start.Add(time.Hour)); d != 0 {
		t.Fatalf("无课程不应顺延，got %d", d)
	}
}

func TestRefitCompensation(t *testing.T) {
	if RefitCompensation(0) != 0 {
		t.Fatal("未延误不补偿")
	}
	if RefitCompensation(10) != 20 { // 不足 15 分钟按 15 分钟计
		t.Fatalf("10 分钟应补 20，got %v", RefitCompensation(10))
	}
	if RefitCompensation(30) != 40 {
		t.Fatalf("30 分钟应补 40，got %v", RefitCompensation(30))
	}
	if RefitCompensation(500) != 100 {
		t.Fatalf("补偿封顶 100，got %v", RefitCompensation(500))
	}
}

func TestRefitChargeAndRights(t *testing.T) {
	if RefitCharge(true) != 0 {
		t.Fatal("安全重配应免费")
	}
	if RefitCharge(false) != 20 {
		t.Fatal("非安全重配应收服务费 20")
	}
	if RefitRightsImpact(0, 0) == "" {
		t.Fatal("无影响也应有说明")
	}
	s := RefitRightsImpact(20, 40)
	if !strings.Contains(s, "顺延 20 分钟") || !strings.Contains(s, "40") {
		t.Fatalf("权益说明应含顺延与补偿：%s", s)
	}
}
