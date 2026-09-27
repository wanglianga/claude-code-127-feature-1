package rules

// 课程前尺码重配（教练发起的安全重配）业务规则

// 单次重配预计耗时（分钟）：收回旧雪具、按库存选码、换发试穿
const RefitBaseDelayMinutes = 15

// 同一门课程因重配累计顺延的上限（分钟），超出后不再顺延，避免拖垮教练后续排课
const RefitMaxCourseShiftMinutes = 45

// 每顺延 15 分钟给游客的课程权益补偿（租金减免，元）
const RefitCompensatePerQuarter = 20.0

// RefitDelayMinutes 重配耗时：固定 15 分钟（1 件雪具的收回+选码+换发+试穿）
func RefitDelayMinutes() int {
	return RefitBaseDelayMinutes
}

// RefitCompensation 游客补偿：每 15 分钟延误补偿 20 元租金减免，封顶不超过租金总额
func RefitCompensation(delayMinutes int, feeTotal float64) float64 {
	if delayMinutes <= 0 {
		return 0
	}
	c := float64((delayMinutes+14)/15) * RefitCompensatePerQuarter
	if feeTotal >= 0 && c > feeTotal {
		c = feeTotal
	}
	return round2(c)
}

// RefitCourseShift 计算本次重配实际计入课程顺延的分钟数。
// currentShift 为该课程已累计顺延分钟数；返回 (本次实际顺延, 顺延后的累计值)。
func RefitCourseShift(currentShift, delay int) (applied, total int) {
	if delay < 0 {
		delay = 0
	}
	remain := RefitMaxCourseShiftMinutes - currentShift
	if remain < 0 {
		remain = 0
	}
	applied = delay
	if applied > remain {
		applied = remain
	}
	return applied, currentShift + applied
}
