package api

import (
	"database/sql"
	"fmt"
	"net/http"

	"skirental/internal/rules"
)

// 课程前尺码重配：教练发现（初学者）雪鞋过松等安全隐患时发起，
// 柜台按库存推荐替换尺码并换发，教练复核安全动作与课程时间后完成。
// 重配耗时顺延课程开始时间，游客获得补偿（租金减免），全程游客端可见。

// refitRightsImpact 生成游客可见的课程权益影响说明
func refitRightsImpact(shiftApplied, shiftTotal int, compensation float64) string {
	timePart := fmt.Sprintf("本课程累计顺延已达上限 %d 分钟，课程时间不再调整", rules.RefitMaxCourseShiftMinutes)
	if shiftApplied > 0 {
		timePart = fmt.Sprintf("课程开始时间顺延 %d 分钟（本课程累计顺延 %d 分钟），课程时长与内容不缩水",
			shiftApplied, shiftTotal)
	}
	return fmt.Sprintf("%s；平台补偿租金减免 ¥%.2f，本次安全重配免费", timePart, compensation)
}

// ---------- 教练发起课前重配 ----------

type refitCreateReq struct {
	RentalItemID int64  `json:"rental_item_id"`
	Reason       string `json:"reason"`
}

func (s *Server) createRefit(w http.ResponseWriter, r *http.Request) {
	c := currentUser(r)
	var req refitCreateReq
	if err := decode(r, &req); err != nil || req.RentalItemID == 0 {
		writeError(w, http.StatusBadRequest, "需要指定要重配的雪具")
		return
	}
	tx, _ := s.db.Begin()
	defer tx.Rollback()

	var etype, oldSize string
	var rentalID, courseID, courseCoach, guestID int64
	var feeTotal float64
	var itemID sql.NullInt64
	err := tx.QueryRow(`SELECT ri.equipment_type, ri.rental_id, ri.item_id,
		COALESCE(m.size_label,''), u.id,
		COALESCE(rr.course_id,0), COALESCE(co.coach_id,0), rr.fee_total
		FROM rental_items ri
		JOIN rentals rr ON rr.id=ri.rental_id
		JOIN users u ON u.id=rr.guest_id
		LEFT JOIN equipment_models m ON m.id=ri.model_id
		LEFT JOIN courses co ON co.id=rr.course_id
		WHERE ri.id=$1 AND ri.status='out' FOR UPDATE OF ri`, req.RentalItemID).
		Scan(&etype, &rentalID, &itemID, &oldSize, &guestID,
			&courseID, &courseCoach, &feeTotal)
	if err != nil {
		writeError(w, http.StatusNotFound, "未找到已出库使用中的该件雪具（课前重配仅面向已出库雪具）")
		return
	}
	if courseID == 0 {
		writeError(w, http.StatusBadRequest, "该租赁单未关联课程，无需课前重配（可走普通换码）")
		return
	}
	if courseCoach != 0 && courseCoach != c.UserID {
		writeError(w, http.StatusForbidden, "这不是你的课程学员")
		return
	}
	var open int
	tx.QueryRow(`SELECT count(*) FROM refits WHERE rental_item_id=$1
		AND status IN ('requested','awaiting_confirm')`, req.RentalItemID).Scan(&open)
	if open > 0 {
		writeError(w, http.StatusConflict, "该雪具已有进行中的重配，请等待处理完成")
		return
	}

	reason := req.Reason
	if reason == "" {
		reason = "雪具尺码不适配影响安全"
		if etype == "boot" {
			reason = "雪鞋过松影响安全"
		}
	}
	delay := rules.RefitDelayMinutes()

	// 重配耗时影响课程开始时间：顺延课程（累计上限 45 分钟），教练排课同步调整
	var curShift int
	tx.QueryRow(`SELECT shift_minutes FROM courses WHERE id=$1 FOR UPDATE`, courseID).Scan(&curShift)
	shiftApplied, shiftTotal := rules.RefitCourseShift(curShift, delay)
	if shiftApplied > 0 {
		tx.Exec(`UPDATE courses SET shift_minutes=$2,
			start_at=start_at + ($3::text || ' minutes')::interval,
			end_at=end_at + ($3::text || ' minutes')::interval WHERE id=$1`,
			courseID, shiftTotal, shiftApplied)
	} else {
		tx.Exec(`UPDATE courses SET shift_minutes=$2 WHERE id=$1`, courseID, shiftTotal)
	}

	// 游客补偿：按延误时长租金减免，累计在租赁单上
	compensation := rules.RefitCompensation(delay, feeTotal)
	tx.Exec(`UPDATE rentals SET compensate_total=compensate_total+$1 WHERE id=$2`,
		compensation, rentalID)

	rights := refitRightsImpact(shiftApplied, shiftTotal, compensation)
	var refitID int64
	err = tx.QueryRow(`INSERT INTO refits
		(rental_id, rental_item_id, course_id, reason, old_size, old_item_id,
		 charge_fee, compensate_amount, delay_minutes, shift_applied, rights_impact, requested_by)
		VALUES ($1,$2,$3,$4,$5,$6,0,$7,$8,$9,$10,$11) RETURNING id`,
		rentalID, req.RentalItemID, courseID, reason, oldSize, itemID,
		compensation, delay, shiftApplied, rights, c.UserID).Scan(&refitID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 标记该件雪具课前确认不合规，等待柜台换发
	tx.Exec(`UPDATE rental_items SET fit_status='issue',
		fit_note=$2, fit_checked_by=$3, fit_checked_at=now() WHERE id=$1`,
		req.RentalItemID, "教练发起课前重配："+reason, c.UserID)
	// 游客端同步可见
	tx.Exec(`INSERT INTO safety_reminders(guest_id, rental_id, message) VALUES ($1,$2,$3)`,
		guestID, rentalID, "教练发起课前尺码重配："+reason+"。"+rights+"，请到柜台优先换发")
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"refit_id":           refitID,
		"delay_minutes":      delay,
		"shift_applied":      shiftApplied,
		"course_shift_total": shiftTotal,
		"compensate_amount":  compensation,
		"rights_impact":      rights,
		"message":            fmt.Sprintf("已发起课前重配并通知游客与柜台：课程顺延 %d 分钟，游客补偿 ¥%.2f", shiftApplied, compensation),
	})
}

// ---------- 教练查看本课程的重配（待复核/进行中） ----------

func (s *Server) coachRefits(w http.ResponseWriter, r *http.Request) {
	c := currentUser(r)
	s.listJSON(w, `SELECT COALESCE(json_agg(t ORDER BY t.id DESC), '[]') FROM (
		SELECT rf.id, rf.status, rf.reason, rf.old_size, rf.new_size,
			rf.compensate_amount, rf.delay_minutes, rf.shift_applied, rf.rights_impact,
			rf.rental_item_id, ri.equipment_type, ri.fit_status,
			rf.rental_id, rr.code AS rental_code, u.name AS guest_name,
			co.id AS course_id, co.name AS course_name, co.shift_minutes,
			to_char(co.start_at,'YYYY-MM-DD HH24:MI') AS course_start,
			to_char(rf.created_at,'MM-DD HH24:MI') AS created_at,
			to_char(rf.processed_at,'MM-DD HH24:MI') AS processed_at
		FROM refits rf
		JOIN rentals rr ON rr.id=rf.rental_id
		JOIN users u ON u.id=rr.guest_id
		JOIN rental_items ri ON ri.id=rf.rental_item_id
		LEFT JOIN courses co ON co.id=rf.course_id
		WHERE rf.status IN ('requested','awaiting_confirm')
		  AND (co.coach_id=$1 OR $2='admin')
	) t`, c.UserID, c.Role)
}

// ---------- 教练复核：重新确认安全动作与课程时间 ----------

type refitConfirmReq struct {
	Note string `json:"note"`
}

func (s *Server) confirmRefit(w http.ResponseWriter, r *http.Request) {
	c := currentUser(r)
	id := urlVar(r, "id")
	var req refitConfirmReq
	decode(r, &req)

	tx, _ := s.db.Begin()
	defer tx.Rollback()
	var status, courseStart, courseName string
	var riID, rentalID, guestID, courseCoach int64
	var processedAt sql.NullTime
	err := tx.QueryRow(`SELECT rf.status, rf.rental_item_id, rf.rental_id,
		rr.guest_id, COALESCE(co.coach_id,0), COALESCE(co.name,''),
		COALESCE(to_char(co.start_at,'YYYY-MM-DD HH24:MI'),''), rf.processed_at
		FROM refits rf
		JOIN rentals rr ON rr.id=rf.rental_id
		LEFT JOIN courses co ON co.id=rf.course_id
		WHERE rf.id=$1 FOR UPDATE OF rf`, id).
		Scan(&status, &riID, &rentalID, &guestID, &courseCoach, &courseName, &courseStart, &processedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "重配记录不存在")
		return
	}
	if courseCoach != 0 && courseCoach != c.UserID {
		writeError(w, http.StatusForbidden, "这不是你的课程学员")
		return
	}
	if status != "awaiting_confirm" {
		writeError(w, http.StatusConflict, "柜台尚未完成换发或该重配已复核")
		return
	}
	// 复核前置：教练须在换发后对该件雪具重新做过课前安全确认（fit-check）
	var fitStatus string
	var fitCheckedAt sql.NullTime
	tx.QueryRow(`SELECT fit_status, fit_checked_at FROM rental_items WHERE id=$1`, riID).
		Scan(&fitStatus, &fitCheckedAt)
	reChecked := fitStatus == "ok" && fitCheckedAt.Valid && processedAt.Valid &&
		fitCheckedAt.Time.After(processedAt.Time)
	if !reChecked {
		writeError(w, http.StatusConflict, "请先在课前确认中对换发后的雪具重新做「合适」确认，再复核课程时间")
		return
	}
	note := req.Note
	if note == "" {
		note = "安全动作复核通过，按调整后时间上课"
	}
	tx.Exec(`UPDATE refits SET status='done', reconfirmed_by=$1, reconfirmed_at=now(),
		reconfirm_note=$2 WHERE id=$3`, c.UserID, note, id)
	tx.Exec(`INSERT INTO safety_reminders(guest_id, rental_id, message) VALUES ($1,$2,$3)`,
		guestID, rentalID, fmt.Sprintf("教练已复核换发后的雪具安全合脚，课程时间确认为 %s（%s）", courseStart, note))
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":      "复核完成：安全动作与课程时间已重新确认，游客端已同步",
		"course_start": courseStart,
		"course_name":  courseName,
	})
}

// ---------- 柜台：待处理重配列表 ----------

func (s *Server) counterRefits(w http.ResponseWriter, r *http.Request) {
	s.listJSON(w, `SELECT COALESCE(json_agg(t ORDER BY t.id), '[]') FROM (
		SELECT rf.id, rf.status, rf.reason, rf.old_size, rf.new_size,
			rf.compensate_amount, rf.delay_minutes, rf.shift_applied, rf.rights_impact,
			rf.rental_item_id, ri.equipment_type,
			rf.rental_id, rr.code AS rental_code, u.name AS guest_name, u.phone AS guest_phone,
			co.name AS course_name, co.shift_minutes,
			to_char(co.start_at,'YYYY-MM-DD HH24:MI') AS course_start,
			cu.name AS coach_name,
			to_char(rf.created_at,'MM-DD HH24:MI') AS created_at
		FROM refits rf
		JOIN rentals rr ON rr.id=rf.rental_id
		JOIN users u ON u.id=rr.guest_id
		JOIN rental_items ri ON ri.id=rf.rental_item_id
		LEFT JOIN courses co ON co.id=rf.course_id
		LEFT JOIN users cu ON cu.id=rf.requested_by
		WHERE rf.status='requested'
	) t`)
}

// ---------- 柜台：按库存推荐替换尺码 ----------

// 按游客体测画像计算该类型雪具的目标尺码值
func refitTargetSize(etype string, p rules.Profile) (float64, string, string) {
	switch etype {
	case "ski":
		s := rules.SuggestSki(p)
		return s.SizeValue, s.Size, s.Reason
	case "boot":
		s := rules.SuggestBoot(p)
		return s.SizeValue, s.Size, s.Reason
	case "helmet":
		s := rules.SuggestHelmet(p)
		return s.SizeValue, s.Size, s.Reason
	case "pole":
		s := rules.SuggestPole(p)
		return s.SizeValue, s.Size, s.Reason
	default:
		s := rules.SuggestProtector(p)
		return s.SizeValue, s.Size, s.Reason
	}
}

type refitCandidate struct {
	ModelID   int64   `json:"model_id"`
	SizeLabel string  `json:"size_label"`
	Brand     string  `json:"brand"`
	Name      string  `json:"name"`
	Available int     `json:"available"`
	Fee       float64 `json:"fee"`
	Deposit   float64 `json:"deposit"`
}

// loadRefitProfile 读取重配对应的游客画像与当前型号
func (s *Server) loadRefitProfile(tx *sql.Tx, refitID int64) (rules.Profile, string, int64, error) {
	var p rules.Profile
	var etype string
	var curModel int64
	var h, w, head sql.NullInt64
	var foot sql.NullFloat64
	var level, session sql.NullString
	err := tx.QueryRow(`SELECT ri.equipment_type, COALESCE(ri.model_id,0),
		u.height_cm, u.weight_kg, u.foot_eu, u.head_cm, u.level, rr.session_type
		FROM refits rf
		JOIN rental_items ri ON ri.id=rf.rental_item_id
		JOIN rentals rr ON rr.id=rf.rental_id
		JOIN users u ON u.id=rr.guest_id
		WHERE rf.id=$1`, refitID).
		Scan(&etype, &curModel, &h, &w, &foot, &head, &level, &session)
	if err != nil {
		return p, "", 0, err
	}
	if h.Valid {
		p.HeightCM = int(h.Int64)
	}
	if w.Valid {
		p.WeightKG = int(w.Int64)
	}
	if foot.Valid {
		p.FootEU = foot.Float64
	}
	if head.Valid {
		p.HeadCM = int(head.Int64)
	}
	p.Level = level.String
	p.SessionType = session.String
	return p, etype, curModel, nil
}

// refitCandidates 按目标尺码从近到远列出有库存的同类型号
func refitCandidates(tx *sql.Tx, etype string, target float64, excludeModel int64, limit int) ([]refitCandidate, error) {
	rows, err := tx.Query(`SELECT m.id, m.size_label, m.brand, m.name, m.rental_fee, m.deposit,
		(SELECT count(*) FROM equipment_items i WHERE i.model_id=m.id AND i.status='available') AS available
		FROM equipment_models m
		WHERE m.active AND m.type=$1 AND m.id<>$2
		  AND EXISTS(SELECT 1 FROM equipment_items i WHERE i.model_id=m.id AND i.status='available')
		ORDER BY abs(COALESCE((m.spec->>'size_value')::float,0) - $3), m.id
		LIMIT $4`, etype, excludeModel, target, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []refitCandidate
	for rows.Next() {
		var c refitCandidate
		if err := rows.Scan(&c.ModelID, &c.SizeLabel, &c.Brand, &c.Name, &c.Fee, &c.Deposit, &c.Available); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Server) recommendRefit(w http.ResponseWriter, r *http.Request) {
	id := urlVar(r, "id")
	tx, _ := s.db.Begin()
	defer tx.Rollback()
	p, etype, curModel, err := s.loadRefitProfile(tx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "重配记录不存在")
		return
	}
	target, targetLabel, reason := refitTargetSize(etype, p)
	cands, err := refitCandidates(tx, etype, target, curModel, 5)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(cands) == 0 {
		// 没有其他尺码库存时退回当前型号换一件
		cands, _ = refitCandidates(tx, etype, target, 0, 1)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"equipment_type": etype,
		"target_size":    targetLabel,
		"target_reason":  reason,
		"candidates":     cands,
	})
}

// ---------- 柜台处理重配：换发新尺码 ----------

type processRefitReq struct {
	Approve bool   `json:"approve"`
	NewQR   string `json:"new_qr_code"`
	Note    string `json:"note"`
}

func (s *Server) processRefit(w http.ResponseWriter, r *http.Request) {
	c := currentUser(r)
	id := urlVar(r, "id")
	var req processRefitReq
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	tx, _ := s.db.Begin()
	defer tx.Rollback()

	var status, etype, oldSize, rights string
	var riID, rentalID, courseID, guestID, shiftApplied int64
	var oldItem sql.NullInt64
	var oldModel int64
	var compensate float64
	err := tx.QueryRow(`SELECT rf.status, rf.rental_item_id, rf.rental_id,
		COALESCE(rf.course_id,0), rf.shift_applied, rf.compensate_amount, rf.rights_impact,
		ri.equipment_type, COALESCE(ri.model_id,0), ri.item_id, COALESCE(rf.old_size,''),
		rr.guest_id
		FROM refits rf
		JOIN rental_items ri ON ri.id=rf.rental_item_id
		JOIN rentals rr ON rr.id=rf.rental_id
		WHERE rf.id=$1 FOR UPDATE OF rf`, id).
		Scan(&status, &riID, &rentalID, &courseID, &shiftApplied, &compensate, &rights,
			&etype, &oldModel, &oldItem, &oldSize, &guestID)
	if err != nil {
		writeError(w, http.StatusNotFound, "重配记录不存在")
		return
	}
	if status != "requested" {
		writeError(w, http.StatusConflict, "该重配已处理")
		return
	}

	// 取消重配：回滚课程顺延与游客补偿
	if !req.Approve {
		tx.Exec(`UPDATE refits SET status='cancelled', processed_by=$1, processed_at=now() WHERE id=$2`,
			c.UserID, id)
		if courseID != 0 && shiftApplied > 0 {
			tx.Exec(`UPDATE courses SET shift_minutes=GREATEST(shift_minutes-$2,0),
				start_at=start_at - ($2::text || ' minutes')::interval,
				end_at=end_at - ($2::text || ' minutes')::interval WHERE id=$1`,
				courseID, shiftApplied)
		}
		tx.Exec(`UPDATE rentals SET compensate_total=GREATEST(compensate_total-$1,0) WHERE id=$2`,
			compensate, rentalID)
		tx.Exec(`INSERT INTO safety_reminders(guest_id, rental_id, message) VALUES ($1,$2,$3)`,
			guestID, rentalID, "课前重配已取消："+req.Note+"；课程时间与费用已恢复")
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "已取消重配，课程顺延与游客补偿已回滚：" + req.Note})
		return
	}

	// 确定新雪具：扫码指定，或按游客目标尺码自动选库存
	var newItem, newModel int64
	if req.NewQR != "" {
		var st, mtype string
		err := tx.QueryRow(`SELECT i.id, i.status, i.model_id, m.type FROM equipment_items i
			JOIN equipment_models m ON m.id=i.model_id WHERE i.qr_code=$1 FOR UPDATE OF i`,
			req.NewQR).Scan(&newItem, &st, &newModel, &mtype)
		if err != nil {
			writeError(w, http.StatusBadRequest, "新雪具二维码无效")
			return
		}
		if st != "available" {
			writeError(w, http.StatusConflict, "新雪具当前不可用："+st)
			return
		}
		if mtype != etype {
			writeError(w, http.StatusBadRequest, "新雪具类型与原雪具不一致，无法换发")
			return
		}
	} else {
		p, _, _, err := s.loadRefitProfile(tx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		target, _, _ := refitTargetSize(etype, p)
		cands, _ := refitCandidates(tx, etype, target, oldModel, 1)
		if len(cands) == 0 { // 无其他尺码库存，退回同型号换一件
			cands, _ = refitCandidates(tx, etype, target, 0, 1)
		}
		if len(cands) == 0 {
			writeError(w, http.StatusConflict, "暂无可换库存，请稍后或留下联系方式")
			return
		}
		newModel = cands[0].ModelID
		err = tx.QueryRow(`SELECT id FROM equipment_items
			WHERE model_id=$1 AND status='available'
			ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`, newModel).Scan(&newItem)
		if err != nil {
			writeError(w, http.StatusConflict, "暂无可换库存，请稍后或留下联系方式")
			return
		}
	}

	// 旧件转检查（过松/合脚问题高发，先检查再上架），新件出库
	if oldItem.Valid {
		tx.Exec(`UPDATE equipment_items SET status='inspection', current_rental_item_id=NULL WHERE id=$1`,
			oldItem.Int64)
	}
	tx.Exec(`UPDATE equipment_items SET status='out', rental_count=rental_count+1,
		current_rental_item_id=$1 WHERE id=$2`, riID, newItem)

	// 跨型号换发：同步明细型号/尺码/租金押金，并调整整单合计（补偿单列不受影响）
	var newSizeLabel string
	var newFee, newDeposit float64
	var session string
	tx.QueryRow(`SELECT size_label, rental_fee, deposit FROM equipment_models WHERE id=$1`,
		newModel).Scan(&newSizeLabel, &newFee, &newDeposit)
	tx.QueryRow(`SELECT session_type FROM rentals WHERE id=$1`, rentalID).Scan(&session)
	if session == "night" {
		newFee = round2f(newFee * (1 + rules.NightPremiumRate))
	}
	if newModel != oldModel {
		tx.Exec(`UPDATE rental_items SET model_id=$1, item_id=$2, status='out',
			recommended_size=$3, fee=$4, deposit=$5, exchanged_from_item_id=$6,
			fit_status='pending', fit_note='课前重配已换发新尺码，待教练重新确认' WHERE id=$7`,
			newModel, newItem, newSizeLabel, newFee, newDeposit, oldItem.Int64, riID)
		tx.Exec(`UPDATE rentals r SET
			fee_total=(SELECT COALESCE(sum(fee),0) FROM rental_items WHERE rental_id=r.id),
			deposit_total=(SELECT COALESCE(sum(deposit),0) FROM rental_items WHERE rental_id=r.id)
			WHERE r.id=$1`, rentalID)
	} else {
		tx.Exec(`UPDATE rental_items SET item_id=$1, status='out', exchanged_from_item_id=$2,
			fit_status='pending', fit_note='课前重配已换发新尺码，待教练重新确认' WHERE id=$3`,
			newItem, oldItem.Int64, riID)
	}
	tx.Exec(`UPDATE refits SET status='awaiting_confirm', new_size=$1, new_item_id=$2,
		processed_by=$3, processed_at=now() WHERE id=$4`, newSizeLabel, newItem, c.UserID, id)
	// 游客端同步：换发完成、免费、等待教练复核
	tx.Exec(`INSERT INTO safety_reminders(guest_id, rental_id, message) VALUES ($1,$2,$3)`,
		guestID, rentalID, fmt.Sprintf("柜台已完成课前重配换发：%s → %s（免费）。请等待教练重新确认安全动作与课程时间后上课",
			oldSize, newSizeLabel))
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":     "重配换发完成，旧雪具已收回检查，新尺码：" + newSizeLabel + "；待教练复核",
		"new_item_id": newItem,
		"new_size":    newSizeLabel,
	})
}
