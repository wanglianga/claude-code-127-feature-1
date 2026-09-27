package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"skirental/internal/rules"
)

// 课程前尺码重配：教练发现初学者雪鞋过松影响安全时发起，
// 柜台按库存推荐替换尺码并完成换发；重配耗时可能顺延课程开始时间，
// 游客获得补偿、教练排课同步调整；完成后教练重新确认安全动作与课程时间。

type refitReq struct {
	RentalItemID int64  `json:"rental_item_id"`
	Reason       string `json:"reason"`
}

// 教练发起课前雪鞋重配
func (s *Server) createRefit(w http.ResponseWriter, r *http.Request) {
	c := currentUser(r)
	var req refitReq
	if err := decode(r, &req); err != nil || req.RentalItemID == 0 {
		writeError(w, http.StatusBadRequest, "需要指定要重配的雪鞋")
		return
	}
	if req.Reason == "" {
		req.Reason = "雪鞋过松，脚跟打滑影响安全"
	}
	tx, _ := s.db.Begin()
	defer tx.Rollback()

	var etype, riStatus, oldSize, session string
	var footEU sql.NullFloat64
	var oldItem, rentalID, courseID, guestID, courseCoach sql.NullInt64
	var courseStart sql.NullTime
	err := tx.QueryRow(`SELECT ri.equipment_type, ri.status, m.size_label, ri.item_id,
		rr.id, rr.session_type, rr.course_id, u.id, u.foot_eu,
		COALESCE(co.coach_id,0), co.start_at
		FROM rental_items ri
		JOIN rentals rr ON rr.id=ri.rental_id
		JOIN users u ON u.id=rr.guest_id
		LEFT JOIN equipment_models m ON m.id=ri.model_id
		LEFT JOIN courses co ON co.id=rr.course_id
		WHERE ri.id=$1 FOR UPDATE OF ri`, req.RentalItemID).
		Scan(&etype, &riStatus, &oldSize, &oldItem, &rentalID, &session,
			&courseID, &guestID, &footEU, &courseCoach, &courseStart)
	if err != nil {
		writeError(w, http.StatusNotFound, "未找到该租赁明细")
		return
	}
	if etype != "boot" {
		writeError(w, http.StatusBadRequest, "课前安全重配目前仅支持雪鞋")
		return
	}
	if riStatus != "out" {
		writeError(w, http.StatusConflict, "仅已出库使用中的雪鞋可以发起重配")
		return
	}
	if courseCoach.Valid && courseCoach.Int64 != 0 && courseCoach.Int64 != c.UserID {
		writeError(w, http.StatusForbidden, "这不是你的课程学员")
		return
	}
	var openRefits int
	tx.QueryRow(`SELECT count(*) FROM refits WHERE rental_item_id=$1 AND status IN ('requested','done')`,
		req.RentalItemID).Scan(&openRefits)
	if openRefits > 0 {
		writeError(w, http.StatusConflict, "该雪鞋已有进行中的重配，请先完成或取消")
		return
	}

	// 目标尺码：向脚码收敛，已等于脚码再收半码
	var curEU float64
	if err := parseEU(oldSize, &curEU); err != nil || !footEU.Valid {
		writeError(w, http.StatusBadRequest, "游客脚码或当前雪鞋尺码缺失，无法推算目标尺码")
		return
	}
	targetEU := rules.RefitTargetEU(footEU.Float64, curEU)
	targetSize := fmt.Sprintf("EU%v", trimFloat(targetEU))

	// 预估耗时：按当前重配队列等待人数
	var queueLen int
	tx.QueryRow(`SELECT count(*) FROM queue_tickets
		WHERE queue_type='refit' AND status IN ('waiting','called','serving')`).Scan(&queueLen)
	est := rules.RefitEstimateMinutes(queueLen)

	var refitID int64
	err = tx.QueryRow(`INSERT INTO refits
		(rental_item_id, rental_id, course_id, guest_id, coach_id, reason,
		 old_item_id, old_size, target_size, target_eu, est_minutes, course_start_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`,
		req.RentalItemID, rentalID, courseID, guestID, c.UserID, req.Reason,
		oldItem, oldSize, targetSize, targetEU, est, courseStart).Scan(&refitID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ticketNo, _ := s.nextTicketNo(tx, "refit")
	tx.Exec(`INSERT INTO queue_tickets(rental_id, queue_type, ticket_no) VALUES ($1,'refit',$2)`,
		rentalID, ticketNo)
	// 该雪鞋课前确认状态重置：换发新鞋后教练需重新确认安全动作
	tx.Exec(`UPDATE rental_items SET fit_status='issue',
		fit_note=$1, fit_checked_by=$2, fit_checked_at=now() WHERE id=$3`,
		"教练发起课前重配："+req.Reason, c.UserID, req.RentalItemID)
	tx.Exec(`INSERT INTO safety_reminders(guest_id, rental_id, message) VALUES ($1,$2,$3)`,
		guestID.Int64, rentalID,
		fmt.Sprintf("教练发现%s，已发起课前雪鞋重配（%s → 建议 %s），预计耗时约 %d 分钟，请到柜台重配队列等候叫号；如影响课程开始时间将自动顺延并补偿",
			req.Reason, oldSize, targetSize, est))
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"refit_id": refitID, "ticket_no": ticketNo,
		"target_size": targetSize, "est_minutes": est,
		"message": fmt.Sprintf("已发起课前重配并入队（%s → 建议 %s，预计约 %d 分钟），游客已收到安全提醒",
			oldSize, targetSize, est),
	})
}

// 柜台：重配队列 + 按库存推荐替换尺码
func (s *Server) counterRefits(w http.ResponseWriter, r *http.Request) {
	s.listJSON(w, `SELECT COALESCE(json_agg(t ORDER BY t.created_at), '[]') FROM (
		SELECT rf.id AS refit_id, rf.rental_item_id, rf.rental_id, rf.reason,
			rf.old_size, rf.target_size, rf.target_eu, rf.est_minutes, rf.status,
			rf.created_at,
			to_char(rf.created_at,'HH24:MI') AS created_time,
			rr.code AS rental_code, u.name AS guest_name, u.foot_eu,
			co.name AS course_name, to_char(rf.course_start_at,'YYYY-MM-DD HH24:MI') AS course_start,
			cu.name AS coach_name,
			(SELECT q.ticket_no FROM queue_tickets q
			  WHERE q.rental_id=rf.rental_id AND q.queue_type='refit'
			    AND q.status IN ('waiting','called','serving')
			  ORDER BY q.id DESC LIMIT 1) AS ticket_no,
			(SELECT json_agg(json_build_object(
				'model_id', m.id, 'size_label', m.size_label, 'stock', m.stock,
				'qr_code', m.qr_code, 'distance', m.distance
			  ) ORDER BY m.distance, m.id) FROM (
				SELECT m2.id, m2.size_label,
					abs(COALESCE((m2.spec->>'size_value')::float,0) - rf.target_eu::float) AS distance,
					(SELECT count(*) FROM equipment_items i WHERE i.model_id=m2.id AND i.status='available') AS stock,
					(SELECT i.qr_code FROM equipment_items i
					  WHERE i.model_id=m2.id AND i.status='available' ORDER BY i.id LIMIT 1) AS qr_code
				FROM equipment_models m2
				WHERE m2.active AND m2.type='boot'
				  AND EXISTS(SELECT 1 FROM equipment_items i WHERE i.model_id=m2.id AND i.status='available')
				ORDER BY distance, m2.id LIMIT 3
			  ) m) AS recommendations
		FROM refits rf
		JOIN rentals rr ON rr.id=rf.rental_id
		JOIN users u ON u.id=rf.guest_id
		LEFT JOIN courses co ON co.id=rf.course_id
		LEFT JOIN users cu ON cu.id=rf.coach_id
		WHERE rf.status='requested'
	) t`)
}

type refitCompleteReq struct {
	ActualMinutes int    `json:"actual_minutes"` // 缺省按发起到现在的实际耗时
	NewQR         string `json:"new_qr_code"`    // 可扫码指定新雪具；缺省按目标尺码自动选
}

// 柜台完成重配：换发新雪鞋、登记耗时、顺延课程、计算补偿、同步教练排课
func (s *Server) completeRefit(w http.ResponseWriter, r *http.Request) {
	c := currentUser(r)
	refitID := urlVar(r, "id")
	var req refitCompleteReq
	decode(r, &req)

	tx, _ := s.db.Begin()
	defer tx.Rollback()

	var riID, rentalID, guestID int64
	var courseID, oldItem sql.NullInt64
	var status, oldSize, targetSize, session string
	var targetEU sql.NullFloat64
	var courseStart sql.NullTime
	var createdAt time.Time
	err := tx.QueryRow(`SELECT rf.status, rf.rental_item_id, rf.rental_id, rf.guest_id,
		rf.course_id, rf.old_item_id, rf.old_size, rf.target_size, rf.target_eu,
		rf.course_start_at, rf.created_at, rr.session_type
		FROM refits rf JOIN rentals rr ON rr.id=rf.rental_id
		WHERE rf.id=$1 FOR UPDATE OF rf`, refitID).
		Scan(&status, &riID, &rentalID, &guestID, &courseID, &oldItem,
			&oldSize, &targetSize, &targetEU, &courseStart, &createdAt, &session)
	if err != nil {
		writeError(w, http.StatusNotFound, "重配单不存在")
		return
	}
	if status != "requested" {
		writeError(w, http.StatusConflict, "该重配单已处理："+status)
		return
	}

	// 选新雪具：扫码指定，或按目标欧码自动选库存最接近的
	var newItem, newModel int64
	if req.NewQR != "" {
		var st, etype string
		err := tx.QueryRow(`SELECT i.id, i.status, i.model_id, m.type
			FROM equipment_items i JOIN equipment_models m ON m.id=i.model_id
			WHERE i.qr_code=$1`, req.NewQR).Scan(&newItem, &st, &newModel, &etype)
		if err != nil {
			writeError(w, http.StatusBadRequest, "新雪具二维码无效")
			return
		}
		if etype != "boot" {
			writeError(w, http.StatusBadRequest, "课前重配只能换发雪鞋")
			return
		}
		if st != "available" {
			writeError(w, http.StatusConflict, "新雪具当前不可用："+st)
			return
		}
	} else {
		err := tx.QueryRow(`SELECT i.id, i.model_id FROM equipment_items i
			JOIN equipment_models m ON m.id=i.model_id
			WHERE i.status='available' AND m.active AND m.type='boot'
			ORDER BY abs(COALESCE((m.spec->>'size_value')::float,0) - $1::float), i.id
			LIMIT 1 FOR UPDATE OF i SKIP LOCKED`, targetEU.Float64).Scan(&newItem, &newModel)
		if err != nil {
			writeError(w, http.StatusConflict, "暂无可用雪鞋库存，请稍后或联系管理员补货")
			return
		}
	}

	// 旧鞋回收检查（过松/内衬问题高发，先检查再上架）
	if oldItem.Valid {
		tx.Exec(`UPDATE equipment_items SET status='inspection', current_rental_item_id=NULL WHERE id=$1`,
			oldItem.Int64)
	}
	tx.Exec(`UPDATE equipment_items SET status='out', rental_count=rental_count+1,
		current_rental_item_id=$1 WHERE id=$2`, riID, newItem)

	// 更新明细：新型号/尺码/费用押金，课前确认状态重置为待确认（教练需重新确认安全动作）
	var newSizeLabel string
	var newFee, newDeposit float64
	tx.QueryRow(`SELECT size_label, rental_fee, deposit FROM equipment_models WHERE id=$1`,
		newModel).Scan(&newSizeLabel, &newFee, &newDeposit)
	if session == "night" {
		newFee = round2f(newFee * (1 + rules.NightPremiumRate))
	}
	tx.Exec(`UPDATE rental_items SET model_id=$1, item_id=$2, status='out',
		recommended_size=$3, fee=$4, deposit=$5, exchanged_from_item_id=$6,
		fit_status='pending', fit_note='', fit_checked_by=NULL, fit_checked_at=NULL
		WHERE id=$7`,
		newModel, newItem, newSizeLabel, newFee, newDeposit, oldItem.Int64, riID)
	tx.Exec(`UPDATE rentals r SET
		deposit_total=(SELECT COALESCE(sum(deposit),0) FROM rental_items WHERE rental_id=r.id)
		WHERE r.id=$1`, rentalID)

	// 重配耗时：缺省按发起到完成的实际耗时
	actual := req.ActualMinutes
	if actual <= 0 {
		actual = int(time.Since(createdAt).Minutes())
		if actual < 1 {
			actual = 1
		}
	}
	// 课程开始顺延与游客补偿：游客可上课时刻 = 发起时刻 + 实际耗时
	// （若晚于原课程开始时间，课程开始顺延，教练排课同步调整）
	readyAt := createdAt.Add(time.Duration(actual) * time.Minute)
	if readyAt.Before(time.Now()) {
		readyAt = time.Now()
	}
	delay := 0
	var newStart interface{}
	if courseStart.Valid {
		delay = rules.RefitDelayMinutes(courseStart.Time, readyAt)
	}
	compensate := rules.RefitCompensation(delay)
	charge := rules.RefitCharge(true) // 教练发起的安全重配免费
	rightsImpact := rules.RefitRightsImpact(delay, compensate)
	if delay > 0 && courseID.Valid {
		tx.Exec(`UPDATE courses SET start_at=start_at+($2||' minutes')::interval,
			end_at=end_at+($2||' minutes')::interval WHERE id=$1`,
			courseID.Int64, fmt.Sprintf("%d", delay))
		newStart = courseStart.Time.Add(time.Duration(delay) * time.Minute)
	}
	// 游客补偿：租金减免 + 押金流水留痕（财务台账可见）
	if compensate > 0 {
		tx.Exec(`UPDATE rentals SET fee_total=GREATEST(0,
			(SELECT COALESCE(sum(fee),0) FROM rental_items WHERE rental_id=$1) - $2) WHERE id=$1`,
			rentalID, compensate)
		tx.Exec(`INSERT INTO deposit_transactions(rental_id, type, amount, note, created_by)
			VALUES ($1,'compensation',$2,$3,$4)`,
			rentalID, compensate,
			fmt.Sprintf("课前雪鞋重配致课程开始顺延 %d 分钟，游客补偿（租金减免）", delay), c.UserID)
	} else {
		tx.Exec(`UPDATE rentals r SET
			fee_total=(SELECT COALESCE(sum(fee),0) FROM rental_items WHERE rental_id=r.id)
			WHERE r.id=$1`, rentalID)
	}

	tx.Exec(`UPDATE refits SET status='done', new_item_id=$1, new_size=$2,
		actual_minutes=$3, delay_minutes=$4, compensate_yuan=$5, charge_yuan=$6,
		rights_impact=$7, course_new_start_at=$8, processed_by=$9, processed_at=now()
		WHERE id=$10`,
		newItem, newSizeLabel, actual, delay, compensate, charge, rightsImpact,
		newStart, c.UserID, refitID)
	// 换码留痕：计入该课程的换码拥堵统计
	tx.Exec(`INSERT INTO exchanges(rental_item_id, old_item_id, new_item_id, new_size, reason, status, requested_by, processed_by, processed_at)
		VALUES ($1,$2,$3,$4,$5,'done',$6,$6,now())`,
		riID, oldItem, newItem, newSizeLabel, "课前安全重配："+oldSize+" → "+newSizeLabel, c.UserID)
	tx.Exec(`UPDATE queue_tickets SET status='done', finished_at=now()
		WHERE rental_id=$1 AND queue_type='refit' AND status IN ('waiting','called','serving')`, rentalID)

	// 游客同步：调换原因、是否收费、课程权益影响
	impactMsg := "课程时间与权益不受影响"
	if delay > 0 {
		impactMsg = fmt.Sprintf("课程开始顺延 %d 分钟（时长不变），补偿 ¥%.0f 已减免租金", delay, compensate)
	}
	tx.Exec(`INSERT INTO safety_reminders(guest_id, rental_id, message) VALUES ($1,$2,$3)`,
		guestID, rentalID,
		fmt.Sprintf("课前雪鞋重配完成：%s → %s（原因：安全重配，免费）；%s；请等教练重新确认安全动作后再上课",
			oldSize, newSizeLabel, impactMsg))

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":         "重配完成，新雪鞋已换发，等待教练重新确认安全动作与课程时间",
		"new_size":        newSizeLabel,
		"actual_minutes":  actual,
		"delay_minutes":   delay,
		"compensate_yuan": compensate,
		"charge_yuan":     charge,
		"rights_impact":   rightsImpact,
	})
}

type refitReconfirmReq struct {
	SafetyConfirmed   bool   `json:"safety_confirmed"`   // 已重新确认安全动作（新鞋合脚、安全演示达标）
	ScheduleConfirmed bool   `json:"schedule_confirmed"` // 已确认（顺延后的）课程时间
	Note              string `json:"note"`
}

// 教练重新确认：重配完成后确认新雪鞋安全动作与课程时间，流程闭环
func (s *Server) reconfirmRefit(w http.ResponseWriter, r *http.Request) {
	c := currentUser(r)
	refitID := urlVar(r, "id")
	var req refitReconfirmReq
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	tx, _ := s.db.Begin()
	defer tx.Rollback()

	var status, newSize, impact string
	var riID, rentalID, guestID, coachID, delay int64
	var footEU sql.NullFloat64
	err := tx.QueryRow(`SELECT rf.status, rf.rental_item_id, rf.rental_id, rf.guest_id, rf.coach_id,
		COALESCE(rf.new_size,''), rf.delay_minutes, COALESCE(rf.rights_impact,''), u.foot_eu
		FROM refits rf JOIN users u ON u.id=rf.guest_id
		WHERE rf.id=$1 FOR UPDATE`, refitID).
		Scan(&status, &riID, &rentalID, &guestID, &coachID, &newSize, &delay, &impact, &footEU)
	if err != nil {
		writeError(w, http.StatusNotFound, "重配单不存在")
		return
	}
	if coachID != c.UserID {
		writeError(w, http.StatusForbidden, "只能确认自己发起的重配")
		return
	}
	if status != "done" {
		writeError(w, http.StatusConflict, "柜台尚未完成重配，暂不能确认")
		return
	}
	if !req.SafetyConfirmed || !req.ScheduleConfirmed {
		writeError(w, http.StatusBadRequest, "需同时确认新雪鞋安全动作与课程时间")
		return
	}

	// 系统辅助校验：新鞋欧码与脚码偏差应 ≤0.5
	advice := ""
	var bootEU float64
	if footEU.Valid && parseEU(newSize, &bootEU) == nil && !rules.BootFitOK(footEU.Float64, bootEU) {
		advice = "系统校验：新雪鞋欧码与脚码偏差超过 0.5，建议再次重配"
	}
	note := req.Note
	if note == "" {
		note = "新雪鞋合脚，安全动作已重新演示确认"
	}
	if advice != "" {
		note = note + "；" + advice
	}
	tx.Exec(`UPDATE rental_items SET fit_status='ok', fit_note=$1, fit_checked_by=$2, fit_checked_at=now()
		WHERE id=$3`, note, c.UserID, riID)
	tx.Exec(`UPDATE refits SET status='confirmed', coach_reconfirm_note=$1, confirmed_at=now()
		WHERE id=$2`, note, refitID)
	courseMsg := "课程按原时间进行"
	if delay > 0 {
		courseMsg = fmt.Sprintf("课程已顺延 %d 分钟", delay)
	}
	tx.Exec(`INSERT INTO safety_reminders(guest_id, rental_id, message) VALUES ($1,$2,$3)`,
		guestID, rentalID,
		fmt.Sprintf("教练已重新确认新雪鞋（%s）安全动作，%s，可以正常上课；%s", newSize, courseMsg, impact))
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := map[string]interface{}{
		"message":    "已重新确认安全动作与课程时间，游客可以上课",
		"fit_status": "ok",
	}
	if advice != "" {
		resp["advice"] = advice
	}
	writeJSON(w, http.StatusOK, resp)
}

// 教练取消重配（如游客决定退课或现场已自行解决）
func (s *Server) cancelRefit(w http.ResponseWriter, r *http.Request) {
	c := currentUser(r)
	refitID := urlVar(r, "id")
	tx, _ := s.db.Begin()
	defer tx.Rollback()
	var status, reason string
	var riID, rentalID, guestID, coachID int64
	err := tx.QueryRow(`SELECT status, rental_item_id, rental_id, guest_id, coach_id, reason
		FROM refits WHERE id=$1 FOR UPDATE`, refitID).
		Scan(&status, &riID, &rentalID, &guestID, &coachID, &reason)
	if err != nil {
		writeError(w, http.StatusNotFound, "重配单不存在")
		return
	}
	if coachID != c.UserID {
		writeError(w, http.StatusForbidden, "只能取消自己发起的重配")
		return
	}
	if status != "requested" {
		writeError(w, http.StatusConflict, "仅待处理的重配可以取消")
		return
	}
	tx.Exec(`UPDATE refits SET status='cancelled' WHERE id=$1`, refitID)
	tx.Exec(`UPDATE rental_items SET fit_status='pending', fit_note='重配已取消，待教练重新课前确认'
		WHERE id=$1`, riID)
	tx.Exec(`UPDATE queue_tickets SET status='cancelled', finished_at=now()
		WHERE rental_id=$1 AND queue_type='refit' AND status IN ('waiting','called','serving')`, rentalID)
	tx.Exec(`INSERT INTO safety_reminders(guest_id, rental_id, message) VALUES ($1,$2,$3)`,
		guestID, rentalID, "教练已取消课前雪鞋重配（"+reason+"），请等待教练重新课前确认")
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "重配已取消"})
}

// trimFloat 42.0 -> "42"，41.5 -> "41.5"
func trimFloat(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.1f", v)
}
