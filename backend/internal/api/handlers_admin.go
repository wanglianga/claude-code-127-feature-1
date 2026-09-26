package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func (s *Server) analytics(w http.ResponseWriter, r *http.Request) {
	sections := map[string]string{
		// 哪些型号损耗高
		"model_wear": s.qList(`SELECT COALESCE(json_agg(t ORDER BY t.damage_count DESC), '[]') FROM (
			SELECT m.id, m.brand, m.name, m.size_label, m.type AS equipment_type,
			       (SELECT count(*) FROM equipment_items i WHERE i.model_id=m.id) AS stock,
			       (SELECT count(*) FROM equipment_items i WHERE i.model_id=m.id
			         AND i.status IN ('repair','retired','inspection')) AS unavailable,
			       COALESCE((SELECT count(*) FROM damage_reports d
			         JOIN rental_items ri ON ri.id=d.rental_item_id
			         WHERE ri.model_id=m.id),0) AS damage_count,
			       COALESCE((SELECT count(*) FROM damage_reports d
			         JOIN rental_items ri ON ri.id=d.rental_item_id
			         WHERE ri.model_id=m.id AND d.damage_type='lost'),0) AS lost_count,
			       COALESCE((SELECT sum(ro.repair_cost) FROM repair_orders ro
			         JOIN equipment_items i ON i.id=ro.item_id WHERE i.model_id=m.id),0) AS repair_cost,
			       COALESCE((SELECT count(*) FROM rental_items ri WHERE ri.model_id=m.id),0) AS rental_count
			FROM equipment_models m WHERE m.active
		) t`),
		// 哪些课程时段换码拥堵
		"course_exchange_congestion": s.qList(`SELECT COALESCE(json_agg(t ORDER BY t.exchange_count DESC), '[]') FROM (
			SELECT co.id, co.name, co.session_type,
			       to_char(co.start_at,'YYYY-MM-DD HH24:MI') AS start_at,
			       u.name AS coach_name,
			       (SELECT count(*) FROM rentals rr
			         JOIN rental_items ri ON ri.rental_id=rr.id
			         JOIN exchanges e ON e.rental_item_id=ri.id
			         WHERE rr.course_id=co.id) AS exchange_count,
			       (SELECT count(*) FROM rentals rr
			         JOIN rental_items ri ON ri.rental_id=rr.id
			         WHERE rr.course_id=co.id AND ri.fit_status='issue') AS fit_issue_count,
			       co.booked_count, co.capacity
			FROM courses co LEFT JOIN users u ON u.id=co.coach_id
		) t`),
		// 分时取具槽占用（旺季排队压力）
		"slot_pressure": s.qList(`SELECT COALESCE(json_agg(t ORDER BY t.slot_date, t.start_time), '[]') FROM (
			SELECT ps.id, ps.slot_date::text AS slot_date, ps.start_time, ps.session_type,
			       ps.booked_count, ps.capacity,
			       round(100.0*ps.booked_count/NULLIF(ps.capacity,0),1) AS occupancy_pct
			FROM pickup_slots ps WHERE ps.slot_date >= CURRENT_DATE - 3
		) t`),
		// 哪些游客需要额外安全提醒
		"guest_safety": s.qList(`SELECT COALESCE(json_agg(t ORDER BY t.reminder_count DESC), '[]') FROM (
			SELECT u.id, u.name, u.phone, u.level,
			       (SELECT count(*) FROM safety_reminders sr WHERE sr.guest_id=u.id) AS reminder_count,
			       (SELECT count(*) FROM rentals rr
			         JOIN rental_items ri ON ri.rental_id=rr.id
			         WHERE rr.guest_id=u.id AND ri.fit_status='issue') AS fit_issue_count,
			       (SELECT count(*) FROM exchanges e
			         JOIN rental_items ri ON ri.id=e.rental_item_id
			         JOIN rentals rr ON rr.id=ri.rental_id
			         WHERE rr.guest_id=u.id) AS exchange_count,
			       (SELECT string_agg(msg, ' | ') FROM (
			         SELECT message AS msg FROM safety_reminders
			         WHERE guest_id=u.id ORDER BY id DESC LIMIT 3
			       ) q) AS latest_messages
			FROM users u WHERE u.role='guest'
			GROUP BY u.id, u.name, u.phone, u.level
			HAVING (SELECT count(*) FROM safety_reminders sr WHERE sr.guest_id=u.id) > 0
			    OR (SELECT count(*) FROM rentals rr JOIN rental_items ri ON ri.rental_id=rr.id
			        WHERE rr.guest_id=u.id AND ri.fit_status='issue') > 0
		) t`),
		// 损坏类型分布
		"damage_types": s.qList(`SELECT COALESCE(json_agg(t), '[]') FROM (
			SELECT damage_type, count(*) AS n,
			       CASE damage_type
			         WHEN 'edge_damage' THEN '板刃损伤' WHEN 'base_scratch' THEN '板底划伤'
			         WHEN 'binding_issue' THEN '固定器故障' WHEN 'liner_wet' THEN '内衬潮湿'
			         WHEN 'shell_crack' THEN '壳体裂纹' WHEN 'strap_broken' THEN '绑带断裂'
			         WHEN 'lost' THEN '雪具丢失' ELSE '其他异常' END AS label
			FROM damage_reports GROUP BY damage_type ORDER BY n DESC
		) t`),
	}

	result := map[string]json.RawMessage{}
	for name, q := range sections {
		var data json.RawMessage
		if err := s.db.QueryRow(q).Scan(&data); err != nil {
			writeError(w, http.StatusInternalServerError, "分析查询 "+name+" 失败: "+err.Error())
			return
		}
		if string(data) == "null" {
			data = json.RawMessage("[]")
		}
		result[name] = data
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) qList(q string) string { return q }

// 每套雪具：在谁手里、是否适配课程、是否可继续出租
func (s *Server) equipmentTrace(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	args := []interface{}{}
	q := `SELECT COALESCE(json_agg(t ORDER BY t.id), '[]') FROM (
		SELECT i.id, i.qr_code, i.serial_no, i.status, i.condition_note, i.rental_count,
		       m.brand, m.name, m.size_label, m.type AS equipment_type,
		       rr.id AS rental_id, rr.code AS rental_code,
		       u.name AS holder_name, u.phone AS holder_phone,
		       co.name AS course_name, co.session_type,
		       ri.fit_status, ri.fit_note,
		       CASE i.status
		         WHEN 'available' THEN '可出租'
		         WHEN 'reserved' THEN '已预约待取'
		         WHEN 'out' THEN '在游客手中'
		         WHEN 'inspection' THEN '回收检查中'
		         WHEN 'repair' THEN '维修中，暂不可租'
		         WHEN 'retired' THEN '已报废' END AS status_label,
		       (i.status='available') AS rentable
		FROM equipment_items i
		JOIN equipment_models m ON m.id=i.model_id
		LEFT JOIN rental_items ri ON ri.id=i.current_rental_item_id
		LEFT JOIN rentals rr ON rr.id=ri.rental_id
		LEFT JOIN users u ON u.id=rr.guest_id
		LEFT JOIN courses co ON co.id=rr.course_id
		WHERE 1=1
	`
	if status != "" {
		q += " AND i.status=$1"
		args = append(args, status)
	}
	q += ") t"
	s.listJSON(w, q, args...)
}

type modelReq struct {
	Code      string             `json:"code"`
	Brand     string             `json:"brand"`
	Name      string             `json:"name"`
	Type      string             `json:"type"`
	SizeLabel string             `json:"size_label"`
	SizeValue float64            `json:"size_value"`
	RentalFee float64            `json:"rental_fee"`
	Deposit   float64            `json:"deposit"`
	Spec      map[string]float64 `json:"spec"`
}

func (s *Server) createModel(w http.ResponseWriter, r *http.Request) {
	var req modelReq
	if err := decode(r, &req); err != nil || req.Code == "" || req.Type == "" {
		writeError(w, http.StatusBadRequest, "型号编码、类型必填")
		return
	}
	valid := map[string]bool{"ski": true, "boot": true, "helmet": true, "pole": true, "protector": true}
	if !valid[req.Type] {
		writeError(w, http.StatusBadRequest, "雪具类型不正确")
		return
	}
	if req.Spec == nil {
		req.Spec = map[string]float64{}
	}
	req.Spec["size_value"] = req.SizeValue
	spec, _ := json.Marshal(req.Spec)
	var id int64
	err := s.db.QueryRow(`INSERT INTO equipment_models
		(code, brand, name, type, size_label, spec, rental_fee, deposit)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8) RETURNING id`,
		req.Code, req.Brand, req.Name, req.Type, req.SizeLabel, string(spec),
		req.RentalFee, req.Deposit).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "型号创建失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"model_id": id})
}

type itemReq struct {
	ModelID  int64  `json:"model_id"`
	SerialNo string `json:"serial_no"`
	Qty      int    `json:"qty"` // 批量入库
}

func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	var req itemReq
	if err := decode(r, &req); err != nil || req.ModelID == 0 {
		writeError(w, http.StatusBadRequest, "型号必填")
		return
	}
	if req.Qty <= 0 {
		req.Qty = 1
	}
	tx, _ := s.db.Begin()
	defer tx.Rollback()
	var code string
	if err := tx.QueryRow(`SELECT code FROM equipment_models WHERE id=$1`, req.ModelID).Scan(&code); err != nil {
		writeError(w, http.StatusBadRequest, "型号不存在")
		return
	}
	var ids []int64
	for i := 0; i < req.Qty; i++ {
		var n int
		tx.QueryRow(`SELECT count(*)+1 FROM equipment_items WHERE model_id=$1`, req.ModelID).Scan(&n)
		qr := fmt.Sprintf("QR-%s-%03d", code, n)
		serial := req.SerialNo
		if req.Qty > 1 {
			serial = fmt.Sprintf("%s-%03d", req.SerialNo, n)
		}
		var id int64
		if err := tx.QueryRow(`INSERT INTO equipment_items(model_id, qr_code, serial_no)
			VALUES ($1,$2,$3) RETURNING id`, req.ModelID, qr, serial).Scan(&id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"item_ids": ids, "message": fmt.Sprintf("已入库 %d 件雪具并生成二维码", len(ids)),
	})
}

// ---------- 照片上传 ----------

func (s *Server) uploadPhoto(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "上传表单解析失败（限 8MB）")
		return
	}
	file, header, err := r.FormFile("photo")
	if err != nil {
		writeError(w, http.StatusBadRequest, "缺少 photo 文件")
		return
	}
	defer file.Close()
	ext := ".jpg"
	name := strings.ToLower(header.Filename)
	switch {
	case strings.HasSuffix(name, ".png"):
		ext = ".png"
	case strings.HasSuffix(name, ".webp"):
		ext = ".webp"
	}
	fname := fmt.Sprintf("d%d%s", time.Now().UnixNano(), ext)
	dst := s.cfg.UploadDir + "/" + fname
	out, err := os.Create(dst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, file); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"url": "/uploads/" + fname, "message": "照片已上传",
	})
}
