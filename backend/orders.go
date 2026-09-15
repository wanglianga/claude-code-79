package main

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// 餐单可见性：家属/老人只看关联老人的；骑手/志愿者看自己配送的；其余角色全量
func (s *Server) orderFilter(c *gin.Context) (string, []interface{}) {
	role := c.GetString("role")
	uid := c.GetInt("uid")
	switch role {
	case "family":
		return `o.elder_id IN (SELECT id FROM elders WHERE family_user_id=$1)`, []interface{}{uid}
	case "elder":
		return `o.elder_id IN (SELECT id FROM elders WHERE user_id=$1)`, []interface{}{uid}
	case "rider", "volunteer":
		return `o.id IN (SELECT order_id FROM deliveries WHERE deliverer_id=$1)`, []interface{}{uid}
	default:
		return `TRUE`, nil
	}
}

func (s *Server) listOrders(c *gin.Context) {
	where, args := s.orderFilter(c)
	conds := []string{where}
	if v := c.Query("date"); v != "" {
		args = append(args, v)
		conds = append(conds, `o.meal_date=$`+itoa(len(args)))
	}
	if v := c.Query("month"); v != "" {
		args = append(args, v)
		conds = append(conds, `to_char(o.meal_date,'YYYY-MM')=$`+itoa(len(args)))
	}
	if v := c.Query("status"); v != "" {
		args = append(args, v)
		conds = append(conds, `o.status=$`+itoa(len(args)))
	}
	if v := c.Query("elder_id"); v != "" {
		args = append(args, v)
		conds = append(conds, `o.elder_id=$`+itoa(len(args)))
	}
	q := `SELECT o.id, o.order_no, o.elder_id, e.name, o.order_source, o.meal_date::text, o.meal_type, o.delivery_type,
		o.total_amount, o.subsidy_amount, o.payable_amount, o.status, o.strict_mode, o.is_holiday_special, o.holiday_name,
		o.created_at::text, COALESCE(u.name,'')
		FROM orders o JOIN elders e ON e.id=o.elder_id LEFT JOIN users u ON u.id=o.created_by
		WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY o.meal_date DESC, o.id DESC LIMIT 300`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询餐单失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var (
			id, elderID                                          int
			no, elderName, source, mealDate, mealType, delType   string
			total, sub, payable                                  float64
			status, createdAt, createdBy                         string
			strict, holiday                                      bool
			holidayName                                          string
		)
		rows.Scan(&id, &no, &elderID, &elderName, &source, &mealDate, &mealType, &delType,
			&total, &sub, &payable, &status, &strict, &holiday, &holidayName, &createdAt, &createdBy)
		list = append(list, gin.H{
			"id": id, "order_no": no, "elder_id": elderID, "elder_name": elderName, "order_source": source,
			"meal_date": mealDate[:10], "meal_type": mealType, "delivery_type": delType,
			"total_amount": total, "subsidy_amount": sub, "payable_amount": payable,
			"status": status, "strict_mode": strict, "is_holiday_special": holiday, "holiday_name": holidayName,
			"created_at": createdAt, "created_by": createdBy,
		})
	}
	ok(c, list)
}

// 创建餐单：老人自订 / 家属代订 / 社区代订；自动快照补贴资格与严格签收标记
func (s *Server) createOrder(c *gin.Context) {
	var req struct {
		ElderID          int    `json:"elder_id" binding:"required"`
		MealDate         string `json:"meal_date" binding:"required"`
		MealType         string `json:"meal_type"`
		DeliveryType     string `json:"delivery_type"`
		Address          string `json:"address"`
		BoxesIssued      int    `json:"boxes_issued"`
		IsHolidaySpecial bool   `json:"is_holiday_special"`
		HolidayName      string `json:"holiday_name"`
		Notes            string `json:"notes"`
		Items            []struct {
			DishID      int    `json:"dish_id" binding:"required"`
			Qty         int    `json:"qty" binding:"required"`
			CustomNote  string `json:"custom_note"`
		} `json:"items" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Items) == 0 {
		fail(c, http.StatusBadRequest, "请选择老人、用餐日期并至少选择一道菜")
		return
	}
	if req.MealType == "" {
		req.MealType = "lunch"
	}
	if req.DeliveryType == "" {
		req.DeliveryType = "home"
	}
	if req.BoxesIssued <= 0 {
		req.BoxesIssued = 2
	}
	role := c.GetString("role")
	uid := c.GetInt("uid")
	source := "community"
	if role == "family" {
		source = "family"
	} else if role == "elder" {
		source = "self"
	}

	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "下单失败")
		return
	}
	defer tx.Rollback()

	var (
		elderName, address, boxMethod, boxPolicy string
		subsidyPerMeal                           float64
		needKnock, cog, alone, mob               bool
		active                                   bool
		familyID                                 sql.NullInt64
		elderUserID                              sql.NullInt64
	)
	err = tx.QueryRow(`SELECT name, address, box_return_method, subsidy_per_meal, need_knock_confirm,
		cognitive_impairment, living_alone, mobility_impaired, active, family_user_id, user_id, box_policy
		FROM elders WHERE id=$1`, req.ElderID).
		Scan(&elderName, &address, &boxMethod, &subsidyPerMeal, &needKnock, &cog, &alone, &mob, &active, &familyID, &elderUserID, &boxPolicy)
	if err != nil || !active {
		fail(c, http.StatusBadRequest, "老人档案不存在或已停用")
		return
	}
	// 家属只能为绑定老人下单；老人只能为自己下单
	if role == "family" && (!familyID.Valid || int(familyID.Int64) != uid) {
		fail(c, http.StatusForbidden, "只能为绑定的老人订餐")
		return
	}
	if role == "elder" && (!elderUserID.Valid || int(elderUserID.Int64) != uid) {
		fail(c, http.StatusForbidden, "只能为本人订餐")
		return
	}
	if req.Address != "" {
		address = req.Address
	}
	// 餐盒策略：改用一次性餐盒或暂停新增发放时，本单不再发放可循环餐盒
	if boxPolicy == "disposable" || boxPolicy == "paused" {
		req.BoxesIssued = 0
	}
	// 同日同餐别防重复
	var dup int
	tx.QueryRow(`SELECT COUNT(*) FROM orders WHERE elder_id=$1 AND meal_date=$2 AND meal_type=$3 AND status NOT IN ('cancelled','refunded')`,
		req.ElderID, req.MealDate, req.MealType).Scan(&dup)
	if dup > 0 {
		fail(c, http.StatusBadRequest, "该老人当日此餐别已有餐单，请勿重复下单")
		return
	}

	// 计算金额并校验菜品
	total := 0.0
	type itemRow struct {
		dishID   int
		name     string
		price    float64
		qty      int
		note     string
		holOnly  bool
	}
	items := []itemRow{}
	for _, it := range req.Items {
		if it.Qty <= 0 {
			it.Qty = 1
		}
		var name string
		var price float64
		var avail, holOnly bool
		err := tx.QueryRow(`SELECT name, price, available, holiday_only FROM dishes WHERE id=$1`, it.DishID).
			Scan(&name, &price, &avail, &holOnly)
		if err != nil || !avail {
			fail(c, http.StatusBadRequest, "包含不可用菜品，请重新选择")
			return
		}
		if holOnly && !req.IsHolidaySpecial {
			fail(c, http.StatusBadRequest, "「"+name+"」为节日特供菜，请勾选节日加餐")
			return
		}
		total += price * float64(it.Qty)
		items = append(items, itemRow{it.DishID, name, price, it.Qty, it.CustomNote, holOnly})
	}
	// 补贴：资格补贴 + 节日加餐额外补贴（不超过餐费总额）
	subsidy := subsidyPerMeal
	if subsidy > total {
		subsidy = total
	}
	holidayExtra := 0.0
	if req.IsHolidaySpecial {
		holidayExtra = 5
		if subsidy+holidayExtra > total {
			holidayExtra = total - subsidy
		}
	}
	subsidyAmount := subsidy + holidayExtra
	payable := total - subsidyAmount
	strict := cog || alone || mob

	var orderID int
	err = tx.QueryRow(`INSERT INTO orders(order_no, elder_id, created_by, order_source, meal_date, meal_type, delivery_type,
		address, need_knock_confirm, strict_mode, box_return_method, boxes_issued, total_amount, subsidy_amount,
		holiday_extra, payable_amount, is_holiday_special, holiday_name, status, notes)
		VALUES('TMP-'||gen_random_uuid(), $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,'confirmed',$18)
		RETURNING id`,
		req.ElderID, uid, source, req.MealDate, req.MealType, req.DeliveryType,
		address, needKnock, strict, boxMethod, req.BoxesIssued, total, subsidyAmount,
		holidayExtra, payable, req.IsHolidaySpecial, req.HolidayName, req.Notes).Scan(&orderID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建餐单失败")
		return
	}
	no := orderNo(req.MealDate, orderID)
	if _, err := tx.Exec(`UPDATE orders SET order_no=$1 WHERE id=$2`, no, orderID); err != nil {
		fail(c, http.StatusInternalServerError, "生成单号失败")
		return
	}
	for _, it := range items {
		if _, err := tx.Exec(`INSERT INTO order_items(order_id, dish_id, dish_name, price, qty, custom_note)
			VALUES($1,$2,$3,$4,$5,$6)`, orderID, it.dishID, it.name, it.price, it.qty, it.note); err != nil {
			fail(c, http.StatusInternalServerError, "写入菜品明细失败")
			return
		}
	}
	eventDetail := "来源：" + sourceName(source) + "；金额 " + ftoa(total) + " 元，补贴 " + ftoa(subsidyAmount) + " 元"
	if boxPolicy == "disposable" {
		eventDetail += "；按餐盒策略改用一次性餐盒"
	} else if boxPolicy == "paused" {
		eventDetail += "；按餐盒策略暂停发放可循环餐盒"
	}
	addOrderEvent(tx, orderID, uid, c.GetString("name"), "提交订餐", eventDetail)
	notifyElderParties(tx, req.ElderID, orderID, "新餐单 "+no, "老人「"+elderName+"」"+req.MealDate+" 餐单已提交，等待厨房备餐")
	notify(tx, 0, "kitchen", orderID, "新餐单待备餐", no+"（"+elderName+"，"+req.MealDate+"）")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"id": orderID, "order_no": no, "total_amount": total, "subsidy_amount": subsidyAmount, "payable_amount": payable})
}

func sourceName(s string) string {
	switch s {
	case "self":
		return "老人自订"
	case "family":
		return "家属代订"
	default:
		return "社区代订"
	}
}

func ftoa(f float64) string {
	return strings.TrimRight(strings.TrimRight(formatFloat(f), "0"), ".")
}

func formatFloat(f float64) string {
	// 保留两位小数
	i := int(f*100 + 0.5)
	neg := i < 0
	if neg {
		i = -i
	}
	s := itoa(i)
	for len(s) < 3 {
		s = "0" + s
	}
	out := s[:len(s)-2] + "." + s[len(s)-2:]
	if neg {
		out = "-" + out
	}
	return out
}

// 餐单详情：同一餐单聚合 老人/家属/社区/厨房/骑手/财政 全部信息
func (s *Server) getOrder(c *gin.Context) {
	id := c.Param("id")
	var (
		o struct {
			ElderID, CreatedBy, BoxesIssued                                    int
			OrderNo, Source, MealDate, MealType, DelType, Address              string
			BoxMethod, Status, Notes, CancelReason, HolidayName                string
			Total, Subsidy, HExtra, Payable, Refund                            float64
			NeedKnock, Strict, Holiday                                         bool
			BatchID, SettledIn                                                 sql.NullInt64
			CreatedAt, UpdatedAt                                               string
		}
		elderName, elderPhone, elderAddr, dietary, emergName, emergPhone string
		cog, alone, mob                                                  bool
		elderBoxPolicy                                                 string
	)
	err := s.db.QueryRow(`SELECT o.id, o.order_no, o.elder_id, e.name, e.phone, e.address, e.dietary_restrictions,
		e.emergency_contact_name, e.emergency_contact_phone, e.cognitive_impairment, e.living_alone, e.mobility_impaired,
		e.box_policy,
		o.created_by, o.order_source, o.meal_date::text, o.meal_type, o.delivery_type, o.address, o.need_knock_confirm,
		o.strict_mode, o.box_return_method, o.boxes_issued, o.total_amount, o.subsidy_amount, o.holiday_extra,
		o.payable_amount, o.refund_amount, o.is_holiday_special, o.holiday_name, o.status, o.notes, o.cancel_reason,
		o.batch_id, o.settled_in, o.created_at::text, o.updated_at::text
		FROM orders o JOIN elders e ON e.id=o.elder_id WHERE o.id=$1`, id).
		Scan(&id, &o.OrderNo, &o.ElderID, &elderName, &elderPhone, &elderAddr, &dietary,
			&emergName, &emergPhone, &cog, &alone, &mob, &elderBoxPolicy,
			&o.CreatedBy, &o.Source, &o.MealDate, &o.MealType, &o.DelType, &o.Address, &o.NeedKnock,
			&o.Strict, &o.BoxMethod, &o.BoxesIssued, &o.Total, &o.Subsidy, &o.HExtra,
			&o.Payable, &o.Refund, &o.Holiday, &o.HolidayName, &o.Status, &o.Notes, &o.CancelReason,
			&o.BatchID, &o.SettledIn, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		fail(c, http.StatusNotFound, "餐单不存在")
		return
	}
	// 权限：家属/老人/骑手只能看自己的
	role := c.GetString("role")
	uid := c.GetInt("uid")
	if role == "family" || role == "elder" {
		var cnt int
		s.db.QueryRow(`SELECT COUNT(*) FROM elders WHERE id=$1 AND (family_user_id=$2 OR user_id=$2)`, o.ElderID, uid).Scan(&cnt)
		if cnt == 0 {
			fail(c, http.StatusForbidden, "无权查看该餐单")
			return
		}
	}
	if role == "rider" || role == "volunteer" {
		var cnt int
		s.db.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE order_id=$1 AND deliverer_id=$2`, id, uid).Scan(&cnt)
		if cnt == 0 {
			fail(c, http.StatusForbidden, "无权查看该餐单")
			return
		}
	}

	items := []gin.H{}
	irows, _ := s.db.Query(`SELECT dish_name, price, qty, custom_note FROM order_items WHERE order_id=$1`, id)
	if irows != nil {
		defer irows.Close()
		for irows.Next() {
			var n, cn string
			var p float64
			var q int
			irows.Scan(&n, &p, &q, &cn)
			items = append(items, gin.H{"dish_name": n, "price": p, "qty": q, "custom_note": cn})
		}
	}
	var delivery gin.H
	var d struct {
		ID, DelivererID                                     sql.NullInt64
		ThermalBox, Route, Status, SignPhoto, SignType      string
		SignedBy                                            string
		PickupTime, DeliveredTime                           sql.NullString
		Knock, Timeout                                      bool
		DelivererType, AnomalyNote                          string
	}
	derr := s.db.QueryRow(`SELECT id, deliverer_id, deliverer_type, thermal_box_no, route_info, status,
		pickup_time::text, delivered_time::text, sign_photo_url, sign_type, signed_by_name, knock_confirmed, is_timeout, anomaly_note
		FROM deliveries WHERE order_id=$1`, id).
		Scan(&d.ID, &d.DelivererID, &d.DelivererType, &d.ThermalBox, &d.Route, &d.Status,
			&d.PickupTime, &d.DeliveredTime, &d.SignPhoto, &d.SignType, &d.SignedBy, &d.Knock, &d.Timeout, &d.AnomalyNote)
	if derr == nil {
		delivererName := ""
		if d.DelivererID.Valid {
			s.db.QueryRow(`SELECT name FROM users WHERE id=$1`, d.DelivererID.Int64).Scan(&delivererName)
		}
		delivery = gin.H{
			"id": d.ID.Int64, "deliverer_type": d.DelivererType, "deliverer_name": delivererName,
			"thermal_box_no": d.ThermalBox, "route_info": d.Route, "status": d.Status,
			"pickup_time": d.PickupTime.String, "delivered_time": d.DeliveredTime.String,
			"sign_photo_url": d.SignPhoto, "sign_type": d.SignType, "signed_by_name": d.SignedBy,
			"knock_confirmed": d.Knock, "is_timeout": d.Timeout, "anomaly_note": d.AnomalyNote,
		}
	}
	events := []gin.H{}
	erows, _ := s.db.Query(`SELECT actor_name, action, detail, created_at::text FROM order_events WHERE order_id=$1 ORDER BY id`, id)
	if erows != nil {
		defer erows.Close()
		for erows.Next() {
			var an, ac, de, ca string
			erows.Scan(&an, &ac, &de, &ca)
			events = append(events, gin.H{"actor": an, "action": ac, "detail": de, "time": ca})
		}
	}
	anomalies := []gin.H{}
	arows, _ := s.db.Query(`SELECT id, type, priority, description, status, resolution, created_at::text FROM anomalies WHERE order_id=$1 ORDER BY id DESC`, id)
	if arows != nil {
		defer arows.Close()
		for arows.Next() {
			var aid int
			var t, p, de, st, res, ca string
			arows.Scan(&aid, &t, &p, &de, &st, &res, &ca)
			anomalies = append(anomalies, gin.H{"id": aid, "type": t, "type_name": anomalyTypeName(t), "priority": p,
				"description": de, "status": st, "resolution": res, "created_at": ca})
		}
	}
	var box gin.H
	var bID, bIssued, bReturned int
	var bMethod, bStatus string
	berr := s.db.QueryRow(`SELECT id, boxes_issued, boxes_returned, return_method, status FROM box_records WHERE order_id=$1`, id).
		Scan(&bID, &bIssued, &bReturned, &bMethod, &bStatus)
	if berr == nil {
		box = gin.H{"id": bID, "boxes_issued": bIssued, "boxes_returned": bReturned, "return_method": bMethod, "status": bStatus}
	}
	// 未开门联系尝试记录（敲门/电话/邻里/家属）
	contactAttempts := []gin.H{}
	crows, _ := s.db.Query(`SELECT knock_done, phone_done, neighbor_done, family_done, note, created_at::text
		FROM contact_attempts WHERE order_id=$1 ORDER BY id DESC`, id)
	if crows != nil {
		defer crows.Close()
		for crows.Next() {
			var k, p, n, f bool
			var note, ca string
			crows.Scan(&k, &p, &n, &f, &note, &ca)
			contactAttempts = append(contactAttempts, gin.H{"knock_done": k, "phone_done": p,
				"neighbor_done": n, "family_done": f, "note": note, "created_at": ca})
		}
	}
	feedbacks := []gin.H{}
	frows, _ := s.db.Query(`SELECT rating, suitable, content, created_at::text FROM feedbacks WHERE order_id=$1 ORDER BY id DESC`, id)
	if frows != nil {
		defer frows.Close()
		for frows.Next() {
			var r int
			var su bool
			var co, ca string
			frows.Scan(&r, &su, &co, &ca)
			feedbacks = append(feedbacks, gin.H{"rating": r, "suitable": su, "content": co, "created_at": ca})
		}
	}
	ok(c, gin.H{
		"id": atoi(id), "order_no": o.OrderNo, "status": o.Status,
		"elder": gin.H{"id": o.ElderID, "name": elderName, "phone": elderPhone, "address": elderAddr,
			"dietary_restrictions": dietary, "emergency_contact_name": emergName, "emergency_contact_phone": emergPhone,
			"cognitive_impairment": cog, "living_alone": alone, "mobility_impaired": mob, "box_policy": elderBoxPolicy},
		"order_source": o.Source, "meal_date": o.MealDate[:10], "meal_type": o.MealType, "delivery_type": o.DelType,
		"address": o.Address, "need_knock_confirm": o.NeedKnock, "strict_mode": o.Strict,
		"box_return_method": o.BoxMethod, "boxes_issued": o.BoxesIssued,
		"total_amount": o.Total, "subsidy_amount": o.Subsidy, "holiday_extra": o.HExtra,
		"payable_amount": o.Payable, "refund_amount": o.Refund,
		"is_holiday_special": o.Holiday, "holiday_name": o.HolidayName,
		"notes": o.Notes, "cancel_reason": o.CancelReason,
		"batch_id": o.BatchID.Int64, "settled_in": o.SettledIn.Int64,
		"created_at": o.CreatedAt, "updated_at": o.UpdatedAt,
		"items": items, "delivery": delivery, "events": events, "anomalies": anomalies,
		"box_record": box, "feedbacks": feedbacks, "contact_attempts": contactAttempts,
	})
}

// 家属临时改餐：仅备餐前可改；改餐生成异常事件并通知厨房/社区
func (s *Server) modifyOrder(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Reason string `json:"reason" binding:"required"`
		Items  []struct {
			DishID     int    `json:"dish_id" binding:"required"`
			Qty        int    `json:"qty" binding:"required"`
			CustomNote string `json:"custom_note"`
		} `json:"items" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Items) == 0 {
		fail(c, http.StatusBadRequest, "请填写改餐原因并选择菜品")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var status, orderNo, elderName string
	var elderID int
	err = tx.QueryRow(`SELECT o.status, o.order_no, o.elder_id, e.name FROM orders o JOIN elders e ON e.id=o.elder_id WHERE o.id=$1 FOR UPDATE`, id).
		Scan(&status, &orderNo, &elderID, &elderName)
	if err != nil {
		fail(c, http.StatusNotFound, "餐单不存在")
		return
	}
	// 归属校验：家属/老人仅能操作绑定老人的餐单，社区/管理员协同处置
	if !s.canOperateOrder(c, elderID) {
		fail(c, http.StatusForbidden, "只能操作绑定老人的餐单")
		return
	}
	if status != "pending" && status != "confirmed" {
		fail(c, http.StatusBadRequest, "厨房已开始备餐，无法改餐；如需退餐请联系社区")
		return
	}
	var subsidyPerMeal float64
	tx.QueryRow(`SELECT subsidy_per_meal FROM elders WHERE id=$1`, elderID).Scan(&subsidyPerMeal)
	total := 0.0
	type itemRow struct {
		dishID int
		name   string
		price  float64
		qty    int
		note   string
	}
	items := []itemRow{}
	for _, it := range req.Items {
		if it.Qty <= 0 {
			it.Qty = 1
		}
		var name string
		var price float64
		var avail bool
		if err := tx.QueryRow(`SELECT name, price, available FROM dishes WHERE id=$1`, it.DishID).Scan(&name, &price, &avail); err != nil || !avail {
			fail(c, http.StatusBadRequest, "包含不可用菜品")
			return
		}
		total += price * float64(it.Qty)
		items = append(items, itemRow{it.DishID, name, price, it.Qty, it.CustomNote})
	}
	if _, err := tx.Exec(`DELETE FROM order_items WHERE order_id=$1`, id); err != nil {
		fail(c, http.StatusInternalServerError, "改餐失败")
		return
	}
	for _, it := range items {
		tx.Exec(`INSERT INTO order_items(order_id, dish_id, dish_name, price, qty, custom_note) VALUES($1,$2,$3,$4,$5,$6)`,
			id, it.dishID, it.name, it.price, it.qty, it.note)
	}
	var hExtra float64
	tx.QueryRow(`SELECT holiday_extra FROM orders WHERE id=$1`, id).Scan(&hExtra)
	subsidy := subsidyPerMeal + hExtra
	if subsidy > total {
		subsidy = total
	}
	if _, err := tx.Exec(`UPDATE orders SET total_amount=$1, subsidy_amount=$2, payable_amount=$3, updated_at=now() WHERE id=$4`,
		total, subsidy, total-subsidy, id); err != nil {
		fail(c, http.StatusInternalServerError, "改餐失败")
		return
	}
	addOrderEvent(tx, atoi(id), c.GetInt("uid"), c.GetString("name"), "家属/用户临时改餐", "原因："+req.Reason+"；新金额 "+ftoa(total)+" 元")
	anID, _ := createAnomaly(tx, atoi(id), elderID, "family_change",
		"餐单 "+orderNo+" 发生临时改餐（原因："+req.Reason+"），金额已重算，请厨房按新菜品备餐。", c.GetInt("uid"))
	notify(tx, 0, "kitchen", atoi(id), "餐单改餐提醒", orderNo+" 已改餐，请按新明细备餐")
	notifyElderParties(tx, elderID, atoi(id), "餐单已改餐", orderNo+" 改餐成功，原因："+req.Reason)
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"updated": true, "anomaly_id": anID, "total_amount": total, "subsidy_amount": subsidy})
}

// 取消/退餐
func (s *Server) cancelOrder(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Reason string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写退餐原因")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var status, orderNo string
	var elderID int
	var payable float64
	err = tx.QueryRow(`SELECT status, order_no, elder_id, payable_amount FROM orders WHERE id=$1 FOR UPDATE`, id).
		Scan(&status, &orderNo, &elderID, &payable)
	if err != nil {
		fail(c, http.StatusNotFound, "餐单不存在")
		return
	}
	// 归属校验：家属/老人仅能操作绑定老人的餐单，社区/管理员协同处置
	if !s.canOperateOrder(c, elderID) {
		fail(c, http.StatusForbidden, "只能操作绑定老人的餐单")
		return
	}
	if status == "settled" || status == "cancelled" || status == "refunded" {
		fail(c, http.StatusBadRequest, "该餐单已核销或已退餐，无法操作")
		return
	}
	if status == "delivering" || status == "signed" || status == "completed" {
		fail(c, http.StatusBadRequest, "餐单已在配送或已签收，请通过异常工单处理退餐")
		return
	}
	newStatus := "cancelled"
	refund := 0.0
	if status == "preparing" || status == "ready" {
		// 厨房已投入，退餐需退款并记录
		newStatus = "refunded"
		refund = payable
	}
	if _, err := tx.Exec(`UPDATE orders SET status=$1, refund_amount=$2, cancel_reason=$3, updated_at=now() WHERE id=$4`,
		newStatus, refund, req.Reason, id); err != nil {
		fail(c, http.StatusInternalServerError, "退餐失败")
		return
	}
	addOrderEvent(tx, atoi(id), c.GetInt("uid"), c.GetString("name"), "退餐/取消", "原因："+req.Reason)
	notifyElderParties(tx, elderID, atoi(id), "餐单已退订", orderNo+" 已退订："+req.Reason)
	notify(tx, 0, "kitchen", atoi(id), "餐单退订", orderNo+" 已退订，请停止备餐")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"status": newStatus, "refund_amount": refund})
}

// 社区食堂现场取餐签收
func (s *Server) confirmPickup(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		SignedByName   string `json:"signed_by_name" binding:"required"`
		KnockConfirmed bool   `json:"knock_confirmed"`
		BoxesReturned  int    `json:"boxes_returned"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写签收人姓名")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var status, delType, orderNo string
	var elderID, boxesIssued int
	var strict bool
	err = tx.QueryRow(`SELECT status, delivery_type, elder_id, boxes_issued, strict_mode, order_no FROM orders WHERE id=$1 FOR UPDATE`, id).
		Scan(&status, &delType, &elderID, &boxesIssued, &strict, &orderNo)
	if err != nil {
		fail(c, http.StatusNotFound, "餐单不存在")
		return
	}
	if delType != "community_pickup" {
		fail(c, http.StatusBadRequest, "该餐单非社区食堂自取")
		return
	}
	if status != "ready" {
		fail(c, http.StatusBadRequest, "餐单尚未出餐或已签收")
		return
	}
	if _, err := tx.Exec(`UPDATE orders SET status='signed', updated_at=now() WHERE id=$1`, id); err != nil {
		fail(c, http.StatusInternalServerError, "签收失败")
		return
	}
	returned := req.BoxesReturned
	if returned < 0 {
		returned = 0
	}
	if returned > boxesIssued {
		returned = boxesIssued
	}
	if boxesIssued > 0 {
		boxStatus := "pending"
		if returned == boxesIssued {
			boxStatus = "returned"
		} else if returned > 0 {
			boxStatus = "partial"
		}
		_, err = tx.Exec(`INSERT INTO box_records(order_id, elder_id, boxes_issued, boxes_returned, return_method, status, returned_at)
			VALUES($1,$2,$3,$4,'onsite',$5, CASE WHEN $5='pending' THEN NULL ELSE now() END)
			ON CONFLICT (order_id) DO UPDATE SET boxes_returned=EXCLUDED.boxes_returned, status=EXCLUDED.status`,
			id, elderID, boxesIssued, returned, boxStatus)
		if err != nil {
			fail(c, http.StatusInternalServerError, "记录餐盒失败")
			return
		}
		if err := adjustInventory(tx, returned-boxesIssued); err != nil {
			fail(c, http.StatusInternalServerError, "更新餐盒库存失败")
			return
		}
	}
	addOrderEvent(tx, atoi(id), c.GetInt("uid"), c.GetString("name"), "社区食堂现场签收",
		"签收人："+req.SignedByName+"；现场回收餐盒 "+itoa(returned)+" 个")
	notifyElderParties(tx, elderID, atoi(id), "餐单已签收", orderNo+" 已在社区食堂完成签收")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"status": "signed", "boxes_returned": returned})
}

// 老人/家属反馈：不适合则自动生成异常并通知厨房与社区
func (s *Server) createFeedback(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Rating   int    `json:"rating" binding:"required"`
		Suitable bool   `json:"suitable"`
		Content  string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写评分")
		return
	}
	if req.Rating < 1 || req.Rating > 5 {
		req.Rating = 3
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	defer tx.Rollback()
	var elderID int
	var orderNo, elderName string
	err = tx.QueryRow(`SELECT o.elder_id, o.order_no, e.name FROM orders o JOIN elders e ON e.id=o.elder_id WHERE o.id=$1`, id).
		Scan(&elderID, &orderNo, &elderName)
	if err != nil {
		fail(c, http.StatusNotFound, "餐单不存在")
		return
	}
	// 归属校验：家属/老人仅能为绑定老人的餐单反馈，社区/管理员可代录
	if !s.canOperateOrder(c, elderID) {
		fail(c, http.StatusForbidden, "只能为绑定老人的餐单提交反馈")
		return
	}
	if _, err := tx.Exec(`INSERT INTO feedbacks(order_id, elder_id, from_user, rating, suitable, content) VALUES($1,$2,$3,$4,$5,$6)`,
		id, elderID, c.GetInt("uid"), req.Rating, req.Suitable, req.Content); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	addOrderEvent(tx, atoi(id), c.GetInt("uid"), c.GetString("name"), "提交用餐反馈",
		"评分 "+itoa(req.Rating)+" 星；"+map[bool]string{true: "饭菜适合", false: "饭菜不适合"}[req.Suitable])
	anomalyID := 0
	if !req.Suitable {
		anomalyID, _ = createAnomaly(tx, atoi(id), elderID, "meal_unsuitable",
			"老人「"+elderName+"」反馈饭菜不适合："+req.Content, c.GetInt("uid"))
		notify(tx, 0, "kitchen", atoi(id), "饭菜不适合反馈", orderNo+"："+req.Content)
		notify(tx, 0, "community", atoi(id), "饭菜不适合反馈", "老人「"+elderName+"」反馈饭菜不适合，请关注")
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"created": true, "anomaly_id": anomalyID})
}
