package api

import (
	"database/sql"
	"net/http"

	"skirental/internal/rules"
)

// 教练的课程与待确认雪具
func (s *Server) coachCourses(w http.ResponseWriter, r *http.Request) {
	c := currentUser(r)
	s.listJSON(w, `SELECT COALESCE(json_agg(t ORDER BY t.start_at), '[]') FROM (
		SELECT co.id, co.name, co.session_type, co.shift_minutes,
			to_char(co.start_at,'YYYY-MM-DD HH24:MI') AS start_at,
			to_char(co.end_at,'HH24:MI') AS end_at, co.level, co.booked_count, co.capacity,
			(SELECT json_agg(json_build_object(
				'rental_id', rr.id, 'guest_name', u.name, 'code', rr.code, 'status', rr.status,
				'compensate_total', rr.compensate_total,
				'items', (SELECT json_agg(json_build_object(
					'ri_id', ri.id, 'type', ri.equipment_type,
					'recommended', ri.recommended_size, 'actual', m.size_label,
					'fit_status', ri.fit_status, 'fit_note', ri.fit_note,
					'qr_code', ei.qr_code, 'guest_foot_eu', gu.foot_eu,
					'guest_height_cm', gu.height_cm
				)) FROM rental_items ri
				  LEFT JOIN equipment_models m ON m.id=ri.model_id
				  LEFT JOIN equipment_items ei ON ei.id=ri.item_id
				  LEFT JOIN users gu ON gu.id=rr.guest_id
				  WHERE ri.rental_id=rr.id),
				'refits', (SELECT json_agg(json_build_object(
					'id', rf.id, 'status', rf.status, 'reason', rf.reason,
					'old_size', rf.old_size, 'new_size', rf.new_size,
					'compensate_amount', rf.compensate_amount,
					'shift_applied', rf.shift_applied, 'rights_impact', rf.rights_impact
				)) FROM refits rf WHERE rf.rental_id=rr.id AND rf.status IN ('requested','awaiting_confirm'))
			))
			FROM rentals rr JOIN users u ON u.id=rr.guest_id
			WHERE rr.course_id=co.id AND rr.status IN ('reserved','equipped','return_pending')
		) AS rentals
		FROM courses co WHERE co.coach_id=$1 AND co.start_at > now() - interval '6 hour'
	) t`, c.UserID)
}

type fitCheckReq struct {
	RentalItemID int64  `json:"rental_item_id"`
	FitStatus    string `json:"fit_status"` // ok|issue
	Note         string `json:"note"`
}

// 教练课前确认雪鞋是否合脚、雪板长度是否适合初学者
func (s *Server) fitCheck(w http.ResponseWriter, r *http.Request) {
	c := currentUser(r)
	var req fitCheckReq
	if err := decode(r, &req); err != nil || (req.FitStatus != "ok" && req.FitStatus != "issue") {
		writeError(w, http.StatusBadRequest, "fit_status 需为 ok 或 issue")
		return
	}
	tx, _ := s.db.Begin()
	defer tx.Rollback()

	var etype, recommended, actualSize sql.NullString
	var footEU sql.NullFloat64
	var height, guestID, courseCoach int64
	var level sql.NullString
	err := tx.QueryRow(`SELECT ri.equipment_type, ri.recommended_size, m.size_label,
		u.foot_eu, u.height_cm, u.id, u.level, COALESCE(co.coach_id,0)
		FROM rental_items ri
		JOIN rentals rr ON rr.id=ri.rental_id
		JOIN users u ON u.id=rr.guest_id
		LEFT JOIN equipment_models m ON m.id=ri.model_id
		LEFT JOIN courses co ON co.id=rr.course_id
		WHERE ri.id=$1 AND ri.status='out' FOR UPDATE OF ri`, req.RentalItemID).
		Scan(&etype, &recommended, &actualSize, &footEU, &height, &guestID, &level, &courseCoach)
	if err != nil {
		writeError(w, http.StatusNotFound, "未找到已出库的该件雪具")
		return
	}
	if courseCoach != 0 && courseCoach != c.UserID {
		writeError(w, http.StatusForbidden, "这不是你的课程学员")
		return
	}

	note := req.Note
	fitOK := req.FitStatus == "ok"
	// 系统辅助校验，给出客观依据
	advice := ""
	if etype.String == "boot" && footEU.Valid {
		var bootEU float64
		if e := parseEU(actualSize.String, &bootEU); e == nil {
			if !rules.BootFitOK(footEU.Float64, bootEU) {
				fitOK = false
				advice = "系统校验：雪鞋欧码与脚码偏差超过 0.5，建议换码"
			}
		}
	}
	if etype.String == "ski" {
		var recCM, actualCM float64
		if parseCM(recommended.String, &recCM) == nil && parseCM(actualSize.String, &actualCM) == nil {
			if level.String == "beginner" && actualCM-recCM > 5 {
				fitOK = false
				advice = "系统校验：雪板超出初学者建议长度 5cm 以上，不易控制，建议换短板"
			} else if !rules.SkiFitOK(recCM, actualCM) {
				advice = "系统校验：实际雪板长度偏离建议值，请注意滑行安全"
				if req.FitStatus == "issue" {
					fitOK = false
				}
			}
		}
	}
	if !fitOK && note == "" {
		note = advice
	} else if advice != "" {
		note = note + "；" + advice
	}
	status := "ok"
	if !fitOK {
		status = "issue"
	}
	tx.Exec(`UPDATE rental_items SET fit_status=$1, fit_note=$2, fit_checked_by=$3, fit_checked_at=now()
		WHERE id=$4`, status, note, c.UserID, req.RentalItemID)

	// 不合规 -> 给游客发安全提醒，并提示去换码队列
	if !fitOK {
		var rentalID int64
		tx.QueryRow(`SELECT rental_id FROM rental_items WHERE id=$1`, req.RentalItemID).Scan(&rentalID)
		msg := "教练课前检查发现雪具不合脚/不适配：" + note + "，建议立即到柜台换码后再上雪道"
		tx.Exec(`INSERT INTO safety_reminders(guest_id, rental_id, message) VALUES ($1,$2,$3)`,
			guestID, rentalID, msg)
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := map[string]interface{}{"fit_status": status, "note": note}
	if !fitOK {
		resp["message"] = "已记录不合规并向游客推送安全提醒，请到柜台换码"
	} else {
		resp["message"] = "课前确认通过"
	}
	writeJSON(w, http.StatusOK, resp)
}
