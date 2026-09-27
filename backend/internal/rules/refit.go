package rules

import (
	"fmt"
	"time"
)

// 课前尺码重配（雪鞋过松安全重配）规则

// RefitTargetEU 过松重配的目标欧码：向游客脚码收敛；
// 当前尺码已等于脚码时，再收半码（过松就往小调）。夹在 34~48。
func RefitTargetEU(footEU, currentEU float64) float64 {
	t := footEU
	if t >= currentEU {
		t = currentEU - 0.5
	}
	return clamp(t, 34, 48)
}

// RefitEstimateMinutes 预估重配耗时：基础换装 10 分钟，每多一位排队 +5 分钟
func RefitEstimateMinutes(queueLen int) int {
	if queueLen < 0 {
		queueLen = 0
	}
	return 10 + queueLen*5
}

// RefitDelayMinutes 课程开始顺延：重配完成时刻晚于课程开始时间时，
// 课程开始顺延到完成时刻，向上按 5 分钟取整；未晚于则不顺延。
func RefitDelayMinutes(courseStart, readyAt time.Time) int {
	if courseStart.IsZero() || !readyAt.After(courseStart) {
		return 0
	}
	d := int(readyAt.Sub(courseStart).Minutes())
	if d <= 0 {
		return 0
	}
	return (d + 4) / 5 * 5
}

// RefitCompensation 游客补偿：安全重配导致课程延误，每 15 分钟补偿 20 元
// （不足 15 分钟按 15 分钟计），封顶 100 元，以租金减免形式返还。
func RefitCompensation(delayMinutes int) float64 {
	if delayMinutes <= 0 {
		return 0
	}
	units := (delayMinutes + 14) / 15
	c := float64(units) * 20
	if c > 100 {
		c = 100
	}
	return round2(c)
}

// RefitCharge 安全原因重配免费；非安全原因（游客自行要求）收服务费 20 元。
func RefitCharge(safety bool) float64 {
	if safety {
		return 0
	}
	return 20
}

// RefitRightsImpact 课程权益影响说明（游客端与教练端同步展示）
func RefitRightsImpact(delayMinutes int, compensation float64) string {
	if delayMinutes <= 0 {
		return "重配在课程开始前完成，课程时间与权益不受影响"
	}
	s := fmt.Sprintf("课程开始顺延 %d 分钟，课程时长与内容不变", delayMinutes)
	if compensation > 0 {
		s += fmt.Sprintf("；补偿 ¥%.0f 已减免租金", compensation)
	}
	return s
}
