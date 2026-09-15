package main

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ---------------- 异常工单与社区回访 ----------------

func (s *Server) listAnomalies(c *gin.Context) {
	conds := []string{"TRUE"}
	args := []interface{}{}
	if v := c.Query("status"); v != "" {
		args = append(args, v)
		conds = append(conds, `a.status=$`+itoa(len(args)))
	}
	if v := c.Query("type"); v != "" {
		args = append(args, v)
		conds = append(conds, `a.type=$`+itoa(len(args)))
	}
	role := c.GetString("role")
	uid := c.GetInt("uid")
	if role == "family" || role == "elder" {
		args = append(args, uid)
		conds = append(conds, `a.elder_id IN (SELECT id FROM elders WHERE family_user_id=$`+itoa(len(args))+` OR user_id=$`+itoa(len(args))+`)`)
	}
	rows, err := s.db.Query(`SELECT a.id, a.order_id, COALESCE(o.order_no,''), a.elder_id, COALESCE(e.name,''),
		a.type, a.priority, a.description, a.status, a.resolution, COALESCE(u.name,''), a.created_at::text, a.resolved_at::text,
		(SELECT COUNT(*) FROM follow_ups f WHERE f.anomaly_id=a.id)
		FROM anomalies a
		LEFT JOIN orders o ON o.id=a.order_id
		LEFT JOIN elders e ON e.id=a.elder_id
		LEFT JOIN users u ON u.id=a.reported_by
		WHERE `+strings.Join(conds, " AND ")+` ORDER BY CASE a.status WHEN 'open' THEN 0 WHEN 'processing' THEN 1 ELSE 2 END, a.priority='normal', a.id DESC LIMIT 200`, args...)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询异常工单失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var (
			orderID, elderID                                sql.NullInt64
			id, followCount                                 int
			orderNo, elderName, typ, priority, desc         string
			status, resolution, reporter, createdAt         string
			resolvedAt                                      *string
		)
		rows.Scan(&id, &orderID, &orderNo, &elderID, &elderName, &typ, &priority, &desc,
			&status, &resolution, &reporter, &createdAt, &resolvedAt, &followCount)
		ra := ""
		if resolvedAt != nil {
			ra = *resolvedAt
		}
		list = append(list, gin.H{
			"id": id, "order_id": orderID.Int64, "order_no": orderNo, "elder_id": elderID.Int64, "elder_name": elderName,
			"type": typ, "type_name": anomalyTypeName(typ), "priority": priority, "description": desc,
			"status": status, "resolution": resolution, "reported_by": reporter,
			"created_at": createdAt, "resolved_at": ra, "follow_up_count": followCount,
		})
	}
	ok(c, list)
}

// 社区回访：记录回访结果；老人状态紧急时升级通知
func (s *Server) createFollowUp(c *gin.Context) {
	anomalyID := c.Param("id")
	var req struct {
		Type        string `json:"type" binding:"required"` // phone / visit
		ElderStatus string `json:"elder_status" binding:"required"`
		Result      string `json:"result" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写回访方式、老人状态与回访结果")
		return
	}
	if req.Type != "phone" && req.Type != "visit" {
		req.Type = "phone"
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var elderID, orderID int
	var elderName, aStatus string
	err = tx.QueryRow(`SELECT COALESCE(a.elder_id,0), COALESCE(a.order_id,0), COALESCE(e.name,''), a.status
		FROM anomalies a LEFT JOIN elders e ON e.id=a.elder_id WHERE a.id=$1 FOR UPDATE OF a`, anomalyID).
		Scan(&elderID, &orderID, &elderName, &aStatus)
	if err != nil {
		fail(c, http.StatusNotFound, "异常工单不存在")
		return
	}
	if aStatus == "resolved" {
		fail(c, http.StatusBadRequest, "工单已办结")
		return
	}
	if _, err := tx.Exec(`INSERT INTO follow_ups(anomaly_id, elder_id, community_id, type, elder_status, result)
		VALUES($1,$2,$3,$4,$5,$6)`, anomalyID, elderID, c.GetInt("uid"), req.Type, req.ElderStatus, req.Result); err != nil {
		fail(c, http.StatusInternalServerError, "记录回访失败")
		return
	}
	if _, err := tx.Exec(`UPDATE anomalies SET status='processing' WHERE id=$1 AND status='open'`, anomalyID); err != nil {
		fail(c, http.StatusInternalServerError, "更新工单失败")
		return
	}
	if orderID > 0 {
		addOrderEvent(tx, orderID, c.GetInt("uid"), c.GetString("name"), "社区回访",
			map[string]string{"phone": "电话回访", "visit": "上门回访"}[req.Type]+"：老人状态 "+
				map[string]string{"fine": "安好", "need_help": "需要协助", "urgent": "紧急"}[req.ElderStatus]+"。"+req.Result)
	}
	if req.ElderStatus == "urgent" {
		notifyElderParties(tx, elderID, orderID, "老人状态紧急", "老人「"+elderName+"」回访状态紧急："+req.Result)
		notify(tx, 0, "community", orderID, "紧急回访预警", "老人「"+elderName+"」状态紧急，请立即上门核实")
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"created": true})
}

// 办结异常：支持 关闭 / 重新配送 / 退餐退款 三种处理，联动餐单与补贴
func (s *Server) resolveAnomaly(c *gin.Context) {
	anomalyID := c.Param("id")
	var req struct {
		Resolution string `json:"resolution" binding:"required"`
		Action     string `json:"action"` // close / redeliver / refund
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写处理结果")
		return
	}
	if req.Action == "" {
		req.Action = "close"
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var orderID, elderID int
	var aStatus, orderNo string
	var payable float64
	err = tx.QueryRow(`SELECT a.status, COALESCE(a.order_id,0), COALESCE(a.elder_id,0),
		COALESCE(o.order_no,''), COALESCE(o.payable_amount,0)
		FROM anomalies a LEFT JOIN orders o ON o.id=a.order_id WHERE a.id=$1 FOR UPDATE OF a`, anomalyID).
		Scan(&aStatus, &orderID, &elderID, &orderNo, &payable)
	if err != nil {
		fail(c, http.StatusNotFound, "异常工单不存在")
		return
	}
	if aStatus == "resolved" {
		fail(c, http.StatusBadRequest, "工单已办结")
		return
	}
	if _, err := tx.Exec(`UPDATE anomalies SET status='resolved', resolution=$1, resolved_at=now() WHERE id=$2`,
		req.Resolution+"（处理方式："+map[string]string{"close": "关闭工单", "redeliver": "重新配送", "refund": "退餐退款"}[req.Action]+"）", anomalyID); err != nil {
		fail(c, http.StatusInternalServerError, "办结失败")
		return
	}
	if orderID > 0 {
		switch req.Action {
		case "redeliver":
			// 回到待配送：重置配送任务
			tx.Exec(`UPDATE orders SET status='ready', updated_at=now() WHERE id=$1 AND status='exception'`, orderID)
			tx.Exec(`UPDATE deliveries SET status='assigned', deliverer_id=NULL, pickup_time=NULL, delivered_time=NULL,
				sign_photo_url='', signed_by_name='', knock_confirmed=FALSE, anomaly_note='' WHERE order_id=$1`, orderID)
			addOrderEvent(tx, orderID, c.GetInt("uid"), c.GetString("name"), "异常办结：重新配送", req.Resolution)
			notify(tx, 0, "rider", orderID, "重新配送任务", orderNo+" 已恢复配送，请重新接单")
		case "refund":
			tx.Exec(`UPDATE orders SET status='refunded', refund_amount=payable_amount, updated_at=now() WHERE id=$1 AND status IN ('exception','ready','preparing')`, orderID)
			addOrderEvent(tx, orderID, c.GetInt("uid"), c.GetString("name"), "异常办结：退餐退款", req.Resolution)
			notify(tx, 0, "finance", orderID, "退餐退款", orderNo+" 已退餐，退款 "+ftoa(payable)+" 元，核销时请注意")
		default:
			addOrderEvent(tx, orderID, c.GetInt("uid"), c.GetString("name"), "异常办结", req.Resolution)
		}
		notifyElderParties(tx, elderID, orderID, "异常工单已办结", orderNo+"："+req.Resolution)
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"resolved": true})
}

// ---------------- 餐盒回收 ----------------

func (s *Server) listBoxRecords(c *gin.Context) {
	conds := []string{"TRUE"}
	args := []interface{}{}
	if v := c.Query("status"); v != "" {
		args = append(args, v)
		conds = append(conds, `b.status=$`+itoa(len(args)))
	}
	rows, err := s.db.Query(`SELECT b.id, b.order_id, o.order_no, e.name, b.boxes_issued, b.boxes_returned,
		b.return_method, b.status, b.returned_at::text, o.meal_date::text
		FROM box_records b JOIN orders o ON o.id=b.order_id JOIN elders e ON e.id=b.elder_id
		WHERE `+strings.Join(conds, " AND ")+` ORDER BY CASE b.status WHEN 'pending' THEN 0 WHEN 'partial' THEN 1 ELSE 2 END, b.id DESC LIMIT 200`, args...)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询餐盒台账失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var (
			id, oid, issued, returned   int
			no, elderName, method, st   string
			returnedAt                  *string
			mealDate                    string
		)
		rows.Scan(&id, &oid, &no, &elderName, &issued, &returned, &method, &st, &returnedAt, &mealDate)
		ra := ""
		if returnedAt != nil {
			ra = *returnedAt
		}
		list = append(list, gin.H{"id": id, "order_id": oid, "order_no": no, "elder_name": elderName,
			"boxes_issued": issued, "boxes_returned": returned, "return_method": method,
			"status": st, "returned_at": ra, "meal_date": mealDate[:10]})
	}
	ok(c, list)
}

// 回收餐盒（社区点回收 / 下次送餐带回）
func (s *Server) returnBoxes(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Count int `json:"count" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Count <= 0 {
		fail(c, http.StatusBadRequest, "请填写回收数量")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var issued, returned, orderID, elderID int
	var status, orderNo string
	err = tx.QueryRow(`SELECT b.boxes_issued, b.boxes_returned, b.status, b.order_id, b.elder_id, o.order_no
		FROM box_records b JOIN orders o ON o.id=b.order_id WHERE b.id=$1 FOR UPDATE`, id).
		Scan(&issued, &returned, &status, &orderID, &elderID, &orderNo)
	if err != nil {
		fail(c, http.StatusNotFound, "餐盒记录不存在")
		return
	}
	if status == "returned" {
		fail(c, http.StatusBadRequest, "该单餐盒已全部回收")
		return
	}
	newReturned := returned + req.Count
	if newReturned > issued {
		newReturned = issued
	}
	newStatus := "partial"
	if newReturned == issued {
		newStatus = "returned"
	}
	if _, err := tx.Exec(`UPDATE box_records SET boxes_returned=$1, status=$2, returned_at=now() WHERE id=$3`,
		newReturned, newStatus, id); err != nil {
		fail(c, http.StatusInternalServerError, "回收登记失败")
		return
	}
	if newStatus == "returned" {
		// 餐盒全部回收 => 餐单完成闭环
		tx.Exec(`UPDATE orders SET status='completed', updated_at=now() WHERE id=$1 AND status='signed'`, orderID)
	}
	addOrderEvent(tx, orderID, c.GetInt("uid"), c.GetString("name"), "餐盒回收",
		"本次回收 "+itoa(req.Count)+" 个，累计 "+itoa(newReturned)+"/"+itoa(issued))
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"boxes_returned": newReturned, "status": newStatus})
}

// 催回：生成餐盒未回收异常工单
func (s *Server) urgeBoxReturn(c *gin.Context) {
	id := c.Param("id")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var orderID, elderID, issued, returned int
	var orderNo, elderName string
	err = tx.QueryRow(`SELECT b.order_id, b.elder_id, b.boxes_issued, b.boxes_returned, o.order_no, e.name
		FROM box_records b JOIN orders o ON o.id=b.order_id JOIN elders e ON e.id=b.elder_id
		WHERE b.id=$1 AND b.status<>'returned'`, id).
		Scan(&orderID, &elderID, &issued, &returned, &orderNo, &elderName)
	if err != nil {
		fail(c, http.StatusBadRequest, "记录不存在或已回收完成")
		return
	}
	var dup int
	tx.QueryRow(`SELECT COUNT(*) FROM anomalies WHERE order_id=$1 AND type='box_not_returned' AND status<>'resolved'`, orderID).Scan(&dup)
	if dup > 0 {
		fail(c, http.StatusBadRequest, "该单已有未办结的餐盒催回工单")
		return
	}
	anID, _ := createAnomaly(tx, orderID, elderID, "box_not_returned",
		"餐单 "+orderNo+"（老人「"+elderName+"」）餐盒 "+itoa(issued-returned)+" 个未回收，请社区跟进。", c.GetInt("uid"))
	notify(tx, 0, "community", orderID, "餐盒未回收", orderNo+" 餐盒未回收，已生成工单 #"+itoa(anID))
	addOrderEvent(tx, orderID, c.GetInt("uid"), c.GetString("name"), "餐盒催回", "生成催回工单")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"anomaly_id": anID})
}

// ---------------- 通知 ----------------

func (s *Server) listNotifications(c *gin.Context) {
	uid := c.GetInt("uid")
	role := c.GetString("role")
	rows, err := s.db.Query(`SELECT id, order_id, title, content, read, created_at::text FROM notifications
		WHERE user_id=$1 OR (user_id IS NULL AND role=$2)
		ORDER BY id DESC LIMIT 50`, uid, role)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询通知失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var id int
		var orderID *int
		var title, content, createdAt string
		var read bool
		rows.Scan(&id, &orderID, &title, &content, &read, &createdAt)
		oid := 0
		if orderID != nil {
			oid = *orderID
		}
		list = append(list, gin.H{"id": id, "order_id": oid, "title": title, "content": content,
			"read": read, "created_at": createdAt})
	}
	ok(c, list)
}

func (s *Server) unreadCount(c *gin.Context) {
	uid := c.GetInt("uid")
	role := c.GetString("role")
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE NOT read AND (user_id=$1 OR (user_id IS NULL AND role=$2))`, uid, role).Scan(&n)
	ok(c, gin.H{"count": n})
}

func (s *Server) readAllNotifications(c *gin.Context) {
	uid := c.GetInt("uid")
	role := c.GetString("role")
	s.db.Exec(`UPDATE notifications SET read=TRUE WHERE user_id=$1 OR (user_id IS NULL AND role=$2)`, uid, role)
	ok(c, gin.H{"read": true})
}

// ---------------- 驾驶舱 ----------------

func (s *Server) dashboard(c *gin.Context) {
	role := c.GetString("role")
	uid := c.GetInt("uid")
	stats := gin.H{"role": role}
	q := func(sql string, args ...interface{}) int {
		var n int
		s.db.QueryRow(sql, args...).Scan(&n)
		return n
	}
	qf := func(sql string, args ...interface{}) float64 {
		var n float64
		s.db.QueryRow(sql, args...).Scan(&n)
		return n
	}
	switch role {
	case "family", "elder":
		stats["month_orders"] = q(`SELECT COUNT(*) FROM orders o JOIN elders e ON e.id=o.elder_id
			WHERE (e.family_user_id=$1 OR e.user_id=$1) AND to_char(o.meal_date,'YYYY-MM')=$2 AND o.status NOT IN ('cancelled','refunded')`, uid, currentMonth())
		stats["month_subsidy"] = qf(`SELECT COALESCE(SUM(o.subsidy_amount),0) FROM orders o JOIN elders e ON e.id=o.elder_id
			WHERE (e.family_user_id=$1 OR e.user_id=$1) AND to_char(o.meal_date,'YYYY-MM')=$2 AND o.status IN ('signed','completed','settled')`, uid, currentMonth())
		stats["open_anomalies"] = q(`SELECT COUNT(*) FROM anomalies a JOIN elders e ON e.id=a.elder_id
			WHERE (e.family_user_id=$1 OR e.user_id=$1) AND a.status<>'resolved'`, uid)
		stats["pending_boxes"] = q(`SELECT COALESCE(SUM(b.boxes_issued-b.boxes_returned),0) FROM box_records b
			JOIN elders e ON e.id=b.elder_id WHERE (e.family_user_id=$1 OR e.user_id=$1) AND b.status<>'returned'`, uid)
	case "kitchen":
		stats["today_confirmed"] = q(`SELECT COUNT(*) FROM orders WHERE meal_date=CURRENT_DATE AND status='confirmed'`)
		stats["today_preparing"] = q(`SELECT COUNT(*) FROM orders WHERE meal_date=CURRENT_DATE AND status='preparing'`)
		stats["today_ready"] = q(`SELECT COUNT(*) FROM orders WHERE meal_date=CURRENT_DATE AND status='ready'`)
		stats["unsuitable_feedback"] = q(`SELECT COUNT(*) FROM anomalies WHERE type='meal_unsuitable' AND status<>'resolved'`)
	case "rider", "volunteer":
		dtype := "rider"
		if role == "volunteer" {
			dtype = "volunteer"
		}
		stats["pool_tasks"] = q(`SELECT COUNT(*) FROM deliveries d JOIN orders o ON o.id=d.order_id
			WHERE d.deliverer_type=$1 AND d.deliverer_id IS NULL AND d.status='assigned' AND o.status='ready'`, dtype)
		stats["my_delivering"] = q(`SELECT COUNT(*) FROM deliveries WHERE deliverer_id=$1 AND status='picked'`, uid)
		stats["today_delivered"] = q(`SELECT COUNT(*) FROM deliveries WHERE deliverer_id=$1 AND status='delivered' AND delivered_time::date=CURRENT_DATE`, uid)
		stats["today_timeout"] = q(`SELECT COUNT(*) FROM deliveries WHERE deliverer_id=$1 AND is_timeout AND delivered_time::date=CURRENT_DATE`, uid)
	case "community":
		stats["open_anomalies"] = q(`SELECT COUNT(*) FROM anomalies WHERE status='open'`)
		stats["processing_anomalies"] = q(`SELECT COUNT(*) FROM anomalies WHERE status='processing'`)
		stats["high_priority"] = q(`SELECT COUNT(*) FROM anomalies WHERE status<>'resolved' AND priority='high'`)
		stats["pending_boxes"] = q(`SELECT COALESCE(SUM(boxes_issued-boxes_returned),0) FROM box_records WHERE status<>'returned'`)
		stats["today_pickup"] = q(`SELECT COUNT(*) FROM orders WHERE meal_date=CURRENT_DATE AND delivery_type='community_pickup' AND status='ready'`)
		stats["elders_total"] = q(`SELECT COUNT(*) FROM elders WHERE active`)
	case "finance", "admin":
		stats["month_signed"] = q(`SELECT COUNT(*) FROM orders WHERE to_char(meal_date,'YYYY-MM')=$1 AND status IN ('signed','completed','settled')`, currentMonth())
		stats["month_subsidy"] = qf(`SELECT COALESCE(SUM(subsidy_amount),0) FROM orders WHERE to_char(meal_date,'YYYY-MM')=$1 AND status IN ('signed','completed','settled')`, currentMonth())
		stats["month_refund"] = qf(`SELECT COALESCE(SUM(refund_amount),0) FROM orders WHERE to_char(meal_date,'YYYY-MM')=$1`, currentMonth())
		stats["open_anomalies"] = q(`SELECT COUNT(*) FROM anomalies WHERE status<>'resolved'`)
		stats["recycle_rate"] = qf(`SELECT CASE WHEN SUM(boxes_issued)>0
			THEN ROUND(SUM(boxes_returned)*100.0/SUM(boxes_issued),2) ELSE 100 END
			FROM box_records b JOIN orders o ON o.id=b.order_id WHERE to_char(o.meal_date,'YYYY-MM')=$1`, currentMonth())
		stats["archived_months"] = q(`SELECT COUNT(*) FROM reconciliations WHERE status='archived'`)
	}
	// 通用：今日餐单状态分布
	stats["today_by_status"] = func() []gin.H {
		rows, err := s.db.Query(`SELECT status, COUNT(*) FROM orders WHERE meal_date=CURRENT_DATE GROUP BY status`)
		if err != nil {
			return []gin.H{}
		}
		defer rows.Close()
		out := []gin.H{}
		for rows.Next() {
			var st string
			var n int
			rows.Scan(&st, &n)
			out = append(out, gin.H{"status": st, "count": n})
		}
		return out
	}()
	ok(c, stats)
}

// ---------------- 用户管理（平台管理员） ----------------

func (s *Server) listUsers(c *gin.Context) {
	rows, err := s.db.Query(`SELECT id, username, name, phone, role, active, created_at::text FROM users ORDER BY id`)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询用户失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var id int
		var username, name, phone, role, createdAt string
		var active bool
		rows.Scan(&id, &username, &name, &phone, &role, &active, &createdAt)
		list = append(list, gin.H{"id": id, "username": username, "name": name, "phone": phone,
			"role": role, "active": active, "created_at": createdAt})
	}
	ok(c, list)
}

func (s *Server) createUser(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Name     string `json:"name" binding:"required"`
		Phone    string `json:"phone"`
		Role     string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写完整用户信息")
		return
	}
	validRoles := map[string]bool{"admin": true, "finance": true, "community": true, "kitchen": true,
		"rider": true, "volunteer": true, "family": true, "elder": true}
	if !validRoles[req.Role] {
		fail(c, http.StatusBadRequest, "角色不合法")
		return
	}
	if len(req.Password) < 6 {
		fail(c, http.StatusBadRequest, "密码至少 6 位")
		return
	}
	var id int
	err := s.db.QueryRow(`INSERT INTO users(username, password_hash, name, phone, role) VALUES($1,$2,$3,$4,$5) RETURNING id`,
		req.Username, hashPassword(req.Password), req.Name, req.Phone, req.Role).Scan(&id)
	if err != nil {
		fail(c, http.StatusBadRequest, "创建失败：用户名已存在")
		return
	}
	ok(c, gin.H{"id": id})
}

func (s *Server) updateUser(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Active   *bool  `json:"active"`
		Password string `json:"password"`
		Phone    string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	if req.Active != nil {
		s.db.Exec(`UPDATE users SET active=$1 WHERE id=$2`, *req.Active, id)
	}
	if req.Password != "" {
		if len(req.Password) < 6 {
			fail(c, http.StatusBadRequest, "密码至少 6 位")
			return
		}
		s.db.Exec(`UPDATE users SET password_hash=$1 WHERE id=$2`, hashPassword(req.Password), id)
	}
	if req.Phone != "" {
		s.db.Exec(`UPDATE users SET phone=$1 WHERE id=$2`, req.Phone, id)
	}
	ok(c, gin.H{"updated": true})
}

// ---------------- 文件上传（送达照片等） ----------------

func (s *Server) upload(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		fail(c, http.StatusBadRequest, "请选择文件")
		return
	}
	if fh.Size > 8<<20 {
		fail(c, http.StatusBadRequest, "文件不能超过 8MB")
		return
	}
	ext := ".jpg"
	name := fh.Filename
	if i := strings.LastIndex(name, "."); i >= 0 {
		ext = strings.ToLower(name[i:])
	}
	allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".svg": true}
	if !allowed[ext] {
		fail(c, http.StatusBadRequest, "仅支持图片文件")
		return
	}
	fname := "u" + itoa(int(timeNowUnix())) + randHex(4) + ext
	dst := s.cfg.UploadDir + "/" + fname
	if err := c.SaveUploadedFile(fh, dst); err != nil {
		fail(c, http.StatusInternalServerError, "保存文件失败")
		return
	}
	ok(c, gin.H{"url": "/uploads/" + fname})
}
