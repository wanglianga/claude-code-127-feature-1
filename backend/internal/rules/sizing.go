package rules

import "math"

// 游客体测与课程画像
type Profile struct {
	HeightCM    int     `json:"height_cm"`
	WeightKG    int     `json:"weight_kg"`
	FootEU      float64 `json:"foot_eu"`
	HeadCM      int     `json:"head_cm"`
	Level       string  `json:"level"`        // beginner|intermediate|advanced
	SessionType string  `json:"session_type"` // day|night
}

type Suggestion struct {
	EquipmentType string  `json:"equipment_type"`
	Size          string  `json:"size"`
	SizeValue     float64 `json:"size_value"`
	MinCM         float64 `json:"min_cm"`
	MaxCM         float64 `json:"max_cm"`
	Reason        string  `json:"reason"`
	Optional      bool    `json:"optional"`
	SafetyNote    string  `json:"safety_note,omitempty"`
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// SuggestSki 雪板长度：初学者用更短更软的板（身高 0.82~0.87），高级可到 0.95；体重偏大 +5cm
func SuggestSki(p Profile) Suggestion {
	factor := map[string]float64{
		"beginner": 0.85, "intermediate": 0.90, "advanced": 0.95,
	}[p.Level]
	if factor == 0 {
		factor = 0.85
	}
	base := float64(p.HeightCM) * factor
	if p.WeightKG > 0 {
		bmi := float64(p.WeightKG) / math.Pow(float64(p.HeightCM)/100, 2)
		if bmi >= 26 {
			base += 5
		} else if bmi < 18 {
			base -= 3
		}
	}
	length := math.Round(base/5) * 5
	length = clamp(length, 100, 185)
	s := Suggestion{
		EquipmentType: "ski",
		Size:          itoa(int(length)) + "cm",
		SizeValue:     length,
		MinCM:         length - 2.5,
		MaxCM:         length + 2.5,
	}
	switch p.Level {
	case "beginner":
		s.Reason = "初学者建议身高 0.85 倍的短板，转弯和刹车更容易控制"
	case "intermediate":
		s.Reason = "进阶水平建议身高 0.90 倍，兼顾稳定与灵活"
	default:
		s.Reason = "高级水平建议身高 0.95 倍，高速更稳定"
	}
	return s
}

// SuggestBoot 雪鞋：按脚码（EU 欧码），初学者建议合脚不挤脚
func SuggestBoot(p Profile) Suggestion {
	eu := clamp(p.FootEU, 34, 48)
	s := Suggestion{
		EquipmentType: "boot",
		Size:          "EU" + floatTrim1(eu),
		SizeValue:     eu,
		MinCM:         eu - 0.5,
		MaxCM:         eu + 0.5,
		Reason:        "按脚码匹配欧码，雪鞋应贴脚无明显晃动",
	}
	if p.Level == "beginner" {
		s.SafetyNote = "初学者课后如脚趾发麻或脚跟打滑，可到柜台换半码"
	}
	return s
}

// SuggestHelmet 头盔：按头围
func SuggestHelmet(p Profile) Suggestion {
	label, note := helmetSize(p.HeadCM)
	return Suggestion{
		EquipmentType: "helmet",
		Size:          label,
		SizeValue:     float64(p.HeadCM),
		MinCM:         0,
		MaxCM:         0,
		Reason:        note,
		SafetyNote:    "佩戴后前后晃动头盔不应松动，卡扣保留两指余量",
	}
}

func helmetSize(head int) (string, string) {
	switch {
	case head <= 0:
		return "M", "未提供头围，默认 M 码，柜台可调换"
	case head <= 54:
		return "S", "头围 " + itoa(head) + "cm 对应 S 码（52-54cm）"
	case head <= 57:
		return "M", "头围 " + itoa(head) + "cm 对应 M 码（55-57cm）"
	case head <= 60:
		return "L", "头围 " + itoa(head) + "cm 对应 L 码（58-60cm）"
	default:
		return "XL", "头围 " + itoa(head) + "cm 对应 XL 码（61cm 以上）"
	}
}

// SuggestPole 雪杖：高山雪杖约身高 *0.7，取 5cm 整数；初学者首课可选不用
func SuggestPole(p Profile) Suggestion {
	length := math.Round(float64(p.HeightCM)*0.7/5) * 5
	length = clamp(length, 95, 130)
	s := Suggestion{
		EquipmentType: "pole",
		Size:          itoa(int(length)) + "cm",
		SizeValue:     length,
		MinCM:         length - 2.5,
		MaxCM:         length + 2.5,
		Reason:        "雪杖长度约为身高 0.7 倍，持杖时大臂小臂约 90 度",
	}
	if p.Level == "beginner" {
		s.Optional = true
		s.SafetyNote = "初学者首课建议先不持杖，专注犁式刹车，需要时再领用"
	}
	return s
}

// SuggestProtector 护具（护臀/护膝套装）：按身高体重分档
func SuggestProtector(p Profile) Suggestion {
	label := "M"
	reason := "默认 M 码"
	switch {
	case p.HeightCM > 0 && p.HeightCM < 150 || p.WeightKG > 0 && p.WeightKG < 45:
		label, reason = "S", "身高<150cm 或体重<45kg 建议 S 码"
	case p.HeightCM >= 175 || p.WeightKG >= 80:
		label, reason = "XL", "身高≥175cm 或体重≥80kg 建议 XL 码"
	case p.HeightCM >= 165 || p.WeightKG >= 65:
		label, reason = "L", "身高≥165cm 或体重≥65kg 建议 L 码"
	default:
		reason = "身高 150-165cm、体重 45-65kg 建议 M 码"
	}
	s := Suggestion{
		EquipmentType: "protector",
		Size:          label,
		SizeValue:     map[string]float64{"S": 1, "M": 2, "L": 3, "XL": 4}[label],
		Reason:        reason,
	}
	if p.Level == "beginner" {
		s.Optional = false
		s.SafetyNote = "初学者摔倒频次高，建议全程佩戴护臀与护膝"
	} else {
		s.Optional = true
	}
	return s
}

// AllSuggestions 生成五类雪具建议
func AllSuggestions(p Profile) []Suggestion {
	return []Suggestion{SuggestSki(p), SuggestBoot(p), SuggestHelmet(p), SuggestPole(p), SuggestProtector(p)}
}

// SkiFitOK 教练课前确认雪板长度是否合适（实际分配尺码需落在建议容差内）
func SkiFitOK(recommended, actualCM float64) bool {
	d := actualCM - recommended
	return d >= -5 && d <= 5
}

// BootFitOK 雪鞋欧码偏差不超过 0.5
func BootFitOK(footEU, bootEU float64) bool {
	return math.Abs(footEU-bootEU) <= 0.5
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [16]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func floatTrim1(v float64) string {
	if math.Abs(v-math.Round(v)) < 0.01 {
		return itoa(int(math.Round(v)))
	}
	whole := math.Floor(v)
	frac := int(math.Round((v - whole) * 10))
	return itoa(int(whole)) + "." + itoa(frac)
}
