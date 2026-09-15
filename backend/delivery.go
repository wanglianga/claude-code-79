package main

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 配送任务池：骑手看 rider 任务，志愿者看 volunteer 任务；含未接单与本人已接
func (s *Server) deliveryTasks(c *gin.Context) {
	role := c.GetString("role")
	uid := c.GetInt("uid")
	dtype := "rider"
	if role == "volunteer" {
		dtype = "volunteer"
	}
	rows, err := s.db.Query(`SELECT d.id, d.order_id, o.order_no, e.name, o.address, e.phone,
		e.emergency_contact_name, e.emergency_contact_phone, o.need_knock_confirm, o.strict_mode,
		d.status, d.thermal_box_no, d.route_info, d.pickup_time::text, d.delivered_time::text,
		d.sign_photo_url, d.signed_by_name, d.knock_confirmed, d.is_timeout, d.deliverer_id, o.meal_date::text, o.meal_type,
		e.risk_level, e.delivery_confirm_mode, e.no_answer_count, (e.focus_until >= CURRENT_DATE)
		FROM deliveries d
		JOIN orders o ON o.id=d.order_id
		JOIN elders e ON e.id=o.elder_id
		WHERE d.deliverer_type=$1 AND (d.deliverer_id IS NULL OR d.deliverer_id=$2)
		  AND o.status IN ('ready','delivering','signed','completed','exception','settled')
		ORDER BY (e.focus_until >= CURRENT_DATE) DESC NULLS LAST, (e.risk_level='high') DESC,
		  CASE d.status WHEN 'assigned' THEN 0 WHEN 'picked' THEN 1 ELSE 2 END, d.id DESC LIMIT 100`,
		dtype, uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询配送任务失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var (
			did, oid                                               int
			no, elderName, addr, phone, emergName, emergPhone      string
			needKnock, strict, knock, timeout                      bool
			status, box, route, photo, signedBy                    string
			pickup, delivered                                      sql.NullString
			delivererID                                            *int
			mealDate, mealType                                     string
			riskLevel, confirmMode                                 string
			noAnswerCount                                          int
			focus                                                  bool
		)
		rows.Scan(&did, &oid, &no, &elderName, &addr, &phone, &emergName, &emergPhone,
			&needKnock, &strict, &status, &box, &route, &pickup, &delivered, &photo, &signedBy,
			&knock, &timeout, &delivererID, &mealDate, &mealType,
			&riskLevel, &confirmMode, &noAnswerCount, &focus)
		mine := delivererID != nil && *delivererID == uid
		list = append(list, gin.H{
			"id": did, "order_id": oid, "order_no": no, "elder_name": elderName, "address": addr,
			"phone": phone, "emergency_contact_name": emergName, "emergency_contact_phone": emergPhone,
			"need_knock_confirm": needKnock, "strict_mode": strict, "status": status,
			"thermal_box_no": box, "route_info": route, "pickup_time": pickup.String, "delivered_time": delivered.String,
			"sign_photo_url": photo, "signed_by_name": signedBy, "knock_confirmed": knock,
			"is_timeout": timeout, "mine": mine, "meal_date": mealDate[:10], "meal_type": mealType,
			"risk_level": riskLevel, "delivery_confirm_mode": confirmMode,
			"no_answer_count": noAnswerCount, "focus": focus,
		})
	}
	ok(c, list)
}

func (s *Server) claimDelivery(c *gin.Context) {
	id := c.Param("id")
	uid := c.GetInt("uid")
	expected := delivererTypeForRole(c.GetString("role"))
	var dtype, status string
	var delivererID *int
	err := s.db.QueryRow(`SELECT deliverer_type, status, deliverer_id FROM deliveries WHERE id=$1`, id).
		Scan(&dtype, &status, &delivererID)
	if err != nil {
		fail(c, http.StatusNotFound, "配送任务不存在")
		return
	}
	if dtype != expected {
		fail(c, http.StatusForbidden, "任务类型与当前角色不匹配，无法认领")
		return
	}
	if delivererID != nil && *delivererID != uid {
		fail(c, http.StatusForbidden, "任务已被其他配送员接单")
		return
	}
	if status != "assigned" {
		fail(c, http.StatusBadRequest, "任务已被接走或状态不允许接单")
		return
	}
	// 原子认领：仅当任务仍未被认领时绑定当前配送员
	res, err := s.db.Exec(`UPDATE deliveries SET deliverer_id=$1 WHERE id=$2 AND deliverer_id IS NULL AND status='assigned' AND deliverer_type=$3`,
		uid, id, expected)
	if err != nil {
		fail(c, http.StatusInternalServerError, "接单失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fail(c, http.StatusBadRequest, "任务已被接走或状态不允许接单")
		return
	}
	var orderID int
	var orderNo string
	s.db.QueryRow(`SELECT d.order_id, o.order_no FROM deliveries d JOIN orders o ON o.id=d.order_id WHERE d.id=$1`, id).
		Scan(&orderID, &orderNo)
	tx, _ := s.db.Begin()
	addOrderEvent(tx, orderID, uid, c.GetString("name"), "配送员接单", "")
	tx.Commit()
	ok(c, gin.H{"claimed": true, "order_no": orderNo})
}

// 取餐：登记保温箱编号与路线
func (s *Server) pickupDelivery(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		ThermalBoxNo string `json:"thermal_box_no" binding:"required"`
		RouteInfo    string `json:"route_info"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写保温箱编号")
		return
	}
	uid := c.GetInt("uid")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var orderID, elderID int
	var orderNo, status, dtype string
	var delivererID *int
	err = tx.QueryRow(`SELECT d.order_id, o.order_no, d.status, d.deliverer_id, o.elder_id, d.deliverer_type
		FROM deliveries d JOIN orders o ON o.id=d.order_id WHERE d.id=$1 FOR UPDATE`, id).
		Scan(&orderID, &orderNo, &status, &delivererID, &elderID, &dtype)
	if err != nil {
		fail(c, http.StatusNotFound, "配送任务不存在")
		return
	}
	if dtype != delivererTypeForRole(c.GetString("role")) {
		fail(c, http.StatusForbidden, "任务类型与当前角色不匹配")
		return
	}
	if status != "assigned" {
		fail(c, http.StatusBadRequest, "该任务已取餐或已结束")
		return
	}
	if delivererID != nil && *delivererID != uid {
		fail(c, http.StatusForbidden, "该任务已被其他配送员接单")
		return
	}
	// 取餐即绑定当前配送员
	if _, err := tx.Exec(`UPDATE deliveries SET deliverer_id=$1, thermal_box_no=$2, route_info=$3, status='picked', pickup_time=now() WHERE id=$4`,
		uid, req.ThermalBoxNo, req.RouteInfo, id); err != nil {
		fail(c, http.StatusInternalServerError, "取餐登记失败")
		return
	}
	if _, err := tx.Exec(`UPDATE orders SET status='delivering', updated_at=now() WHERE id=$1`, orderID); err != nil {
		fail(c, http.StatusInternalServerError, "更新餐单状态失败")
		return
	}
	addOrderEvent(tx, orderID, uid, c.GetString("name"), "骑手取餐", "保温箱 "+req.ThermalBoxNo+"，路线："+req.RouteInfo)
	notifyElderParties(tx, elderID, orderID, "餐单配送中", orderNo+" 骑手已取餐，正在配送")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"picked": true})
}

// 送达签收：严格模式必须照片+敲门确认+签收人；超时自动生成异常
func (s *Server) completeDelivery(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		SignPhotoURL  string `json:"sign_photo_url"`
		SignType      string `json:"sign_type" binding:"required"`
		SignedByName  string `json:"signed_by_name" binding:"required"`
		KnockConfirmed bool  `json:"knock_confirmed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写签收类型与签收人")
		return
	}
	uid := c.GetInt("uid")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var (
		orderID, elderID, boxesIssued, timeoutMin int
		orderNo, elderName, boxMethod, status     string
		dtype                                     string
		strict, needKnock                         bool
		delivererID                               *int
		pickupTime                                *string
	)
	err = tx.QueryRow(`SELECT d.order_id, o.order_no, d.status, d.deliverer_id, o.elder_id, e.name,
		o.strict_mode, o.need_knock_confirm, o.boxes_issued, o.box_return_method, d.timeout_minutes, d.pickup_time::text, d.deliverer_type
		FROM deliveries d JOIN orders o ON o.id=d.order_id JOIN elders e ON e.id=o.elder_id
		WHERE d.id=$1 FOR UPDATE`, id).
		Scan(&orderID, &orderNo, &status, &delivererID, &elderID, &elderName,
			&strict, &needKnock, &boxesIssued, &boxMethod, &timeoutMin, &pickupTime, &dtype)
	if err != nil {
		fail(c, http.StatusNotFound, "配送任务不存在")
		return
	}
	if dtype != delivererTypeForRole(c.GetString("role")) {
		fail(c, http.StatusForbidden, "任务类型与当前角色不匹配")
		return
	}
	if status != "picked" {
		fail(c, http.StatusBadRequest, "请先取餐再签收")
		return
	}
	if delivererID == nil || *delivererID != uid {
		fail(c, http.StatusForbidden, "该任务不属于当前配送员")
		return
	}
	// 严格模式：认知障碍/独居/行动不便老人必须照片 + 敲门确认 + 签收人
	if strict {
		if req.SignPhotoURL == "" {
			fail(c, http.StatusBadRequest, "该老人为严格签收对象（认知障碍/独居/行动不便），必须上传送达照片")
			return
		}
		if !req.KnockConfirmed {
			fail(c, http.StatusBadRequest, "该老人为严格签收对象，必须完成敲门确认")
			return
		}
	}
	if needKnock && !req.KnockConfirmed {
		fail(c, http.StatusBadRequest, "该老人要求敲门确认，请确认后再签收")
		return
	}
	// 超时判定
	isTimeout := false
	if pickupTime != nil && *pickupTime != "" {
		var mins float64
		tx.QueryRow(`SELECT EXTRACT(EPOCH FROM (now() - $1::timestamptz))/60`, *pickupTime).Scan(&mins)
		isTimeout = mins > float64(timeoutMin)
	}
	if _, err := tx.Exec(`UPDATE deliveries SET status='delivered', delivered_time=now(), sign_photo_url=$1,
		sign_type=$2, signed_by_name=$3, knock_confirmed=$4, is_timeout=$5, deliverer_id=$6 WHERE id=$7`,
		req.SignPhotoURL, req.SignType, req.SignedByName, req.KnockConfirmed, isTimeout, uid, id); err != nil {
		fail(c, http.StatusInternalServerError, "签收登记失败")
		return
	}
	if _, err := tx.Exec(`UPDATE orders SET status='signed', updated_at=now() WHERE id=$1`, orderID); err != nil {
		fail(c, http.StatusInternalServerError, "更新餐单状态失败")
		return
	}
	// 成功送达：连续未开门计数清零
	if _, err := tx.Exec(`UPDATE elders SET no_answer_count=0 WHERE id=$1`, elderID); err != nil {
		fail(c, http.StatusInternalServerError, "更新老人风险计数失败")
		return
	}
	// 餐盒台账：现场回收方式立即视为回收；发放出账、回收入账
	if boxesIssued > 0 {
		returned := 0
		boxStatus := "pending"
		if boxMethod == "onsite" {
			returned = boxesIssued
			boxStatus = "returned"
		}
		_, err = tx.Exec(`INSERT INTO box_records(order_id, elder_id, boxes_issued, boxes_returned, return_method, status, returned_at)
			VALUES($1,$2,$3,$4,$5,$6, CASE WHEN $6='pending' THEN NULL ELSE now() END)
			ON CONFLICT (order_id) DO UPDATE SET boxes_returned=EXCLUDED.boxes_returned, status=EXCLUDED.status`,
			orderID, elderID, boxesIssued, returned, boxMethod, boxStatus)
		if err != nil {
			fail(c, http.StatusInternalServerError, "登记餐盒台账失败")
			return
		}
		if err := adjustInventory(tx, returned-boxesIssued); err != nil {
			fail(c, http.StatusInternalServerError, "更新餐盒库存失败")
			return
		}
	}
	addOrderEvent(tx, orderID, uid, c.GetString("name"), "送达签收",
		"签收人："+req.SignedByName+"（"+req.SignType+"）；敲门确认："+map[bool]string{true: "是", false: "否"}[req.KnockConfirmed])
	if isTimeout {
		anID, _ := createAnomaly(tx, orderID, elderID, "rider_timeout",
			"餐单 "+orderNo+" 配送超时（超过 "+itoa(timeoutMin)+" 分钟），请关注老人用餐情况。", uid)
		notify(tx, 0, "community", orderID, "配送超时提醒", orderNo+" 配送超时，请关注老人「"+elderName+"」")
		_ = anID
	}
	notifyElderParties(tx, elderID, orderID, "餐单已签收", orderNo+" 已由 "+req.SignedByName+" 签收")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"delivered": true, "is_timeout": isTimeout})
}

// 配送失败（老人未开门等）：未开门须记录敲门/电话/邻里询问/家属联系，生成异常并触发社区回访，
// 连续未开门计数 +1（超阈值后社区可发起上门查看），事件同步家属与社区网格员
func (s *Server) failDelivery(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Type         string `json:"type" binding:"required"` // no_answer / other
		Note         string `json:"note"`
		KnockDone    bool   `json:"knock_done"`    // 敲门
		PhoneDone    bool   `json:"phone_done"`    // 电话联系
		NeighborDone bool   `json:"neighbor_done"` // 邻里询问
		FamilyDone   bool   `json:"family_done"`   // 家属联系
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请选择异常类型")
		return
	}
	if req.Type != "no_answer" && req.Type != "other" {
		req.Type = "other"
	}
	uid := c.GetInt("uid")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var (
		orderID, elderID int
		orderNo, elderName, status, dtype string
		strict           bool
		delivererID      *int
	)
	err = tx.QueryRow(`SELECT d.order_id, o.order_no, d.status, o.elder_id, e.name, o.strict_mode,
		d.deliverer_type, d.deliverer_id
		FROM deliveries d JOIN orders o ON o.id=d.order_id JOIN elders e ON e.id=o.elder_id
		WHERE d.id=$1 FOR UPDATE`, id).
		Scan(&orderID, &orderNo, &status, &elderID, &elderName, &strict, &dtype, &delivererID)
	if err != nil {
		fail(c, http.StatusNotFound, "配送任务不存在")
		return
	}
	if dtype != delivererTypeForRole(c.GetString("role")) {
		fail(c, http.StatusForbidden, "任务类型与当前角色不匹配")
		return
	}
	if delivererID != nil && *delivererID != uid {
		fail(c, http.StatusForbidden, "该任务已被其他配送员接单，无权终止")
		return
	}
	if status != "picked" && status != "assigned" {
		fail(c, http.StatusBadRequest, "当前状态无法上报异常")
		return
	}
	// 平台要求：未开门上报必须先完成敲门与电话联系（邻里/家属联系如实记录）
	if req.Type == "no_answer" && (!req.KnockDone || !req.PhoneDone) {
		fail(c, http.StatusBadRequest, "未开门上报必须先完成敲门并电话联系老人，请确认后提交")
		return
	}
	// 上报异常同时绑定当前配送员（未认领任务）
	if _, err := tx.Exec(`UPDATE deliveries SET status='failed', anomaly_note=$1, deliverer_id=$2 WHERE id=$3`, req.Note, uid, id); err != nil {
		fail(c, http.StatusInternalServerError, "上报失败")
		return
	}
	if _, err := tx.Exec(`UPDATE orders SET status='exception', updated_at=now() WHERE id=$1`, orderID); err != nil {
		fail(c, http.StatusInternalServerError, "更新餐单状态失败")
		return
	}
	// 未开门：记录联系尝试并累计连续未开门次数
	noAnswerCount := 0
	if req.Type == "no_answer" {
		if _, err := tx.Exec(`INSERT INTO contact_attempts(delivery_id, order_id, elder_id, knock_done, phone_done, neighbor_done, family_done, note, reported_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			id, orderID, elderID, req.KnockDone, req.PhoneDone, req.NeighborDone, req.FamilyDone, req.Note, uid); err != nil {
			fail(c, http.StatusInternalServerError, "记录联系尝试失败")
			return
		}
		if err := tx.QueryRow(`UPDATE elders SET no_answer_count=no_answer_count+1 WHERE id=$1 RETURNING no_answer_count`, elderID).
			Scan(&noAnswerCount); err != nil {
			fail(c, http.StatusInternalServerError, "更新未开门计数失败")
			return
		}
	}
	desc := "餐单 " + orderNo + " 配送异常：" + anomalyTypeName(req.Type) + "。" + req.Note
	if req.Type == "no_answer" {
		desc += "（已敲门✓、已电话✓"
		if req.NeighborDone {
			desc += "、已询问邻里✓"
		}
		if req.FamilyDone {
			desc += "、已联系家属✓"
		}
		desc += "；连续未开门 " + itoa(noAnswerCount) + " 次）"
	}
	if strict {
		desc += "（该老人为重点关注对象：认知障碍/独居/行动不便，请立即回访确认安全）"
	}
	anID, _ := createAnomaly(tx, orderID, elderID, req.Type, desc, uid)
	addOrderEvent(tx, orderID, uid, c.GetString("name"), "配送异常上报", anomalyTypeName(req.Type)+"。"+req.Note)
	// 同步家属与社区网格员
	notify(tx, 0, "community", orderID, "配送异常："+anomalyTypeName(req.Type),
		"老人「"+elderName+"」"+orderNo+" 配送异常，请尽快回访（工单 #"+itoa(anID)+"）")
	notifyElderParties(tx, elderID, orderID, "配送异常提醒", orderNo+" 配送异常，社区将跟进处理")
	if req.Type == "no_answer" && noAnswerCount >= 2 {
		notify(tx, 0, "community", orderID, "未开门超阈值预警",
			"老人「"+elderName+"」已连续 "+itoa(noAnswerCount)+" 次未开门，可发起上门查看")
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"reported": true, "anomaly_id": anID, "no_answer_count": noAnswerCount})
}
