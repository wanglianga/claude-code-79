package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 厨房汇总：某日某餐别已确认餐单按菜品聚合（含低盐低糖/软烂等营养要求）
func (s *Server) kitchenSummary(c *gin.Context) {
	date := c.DefaultQuery("date", today())
	mealType := c.DefaultQuery("meal_type", "lunch")
	dishRows, err := s.db.Query(`SELECT oi.dish_name, d.low_salt, d.low_sugar, d.softness, SUM(oi.qty),
		string_agg(DISTINCT NULLIF(oi.custom_note,''), '；')
		FROM order_items oi
		JOIN orders o ON o.id=oi.order_id
		LEFT JOIN dishes d ON d.id=oi.dish_id
		WHERE o.meal_date=$1 AND o.meal_type=$2 AND o.status IN ('confirmed','preparing','ready')
		GROUP BY oi.dish_name, d.low_salt, d.low_sugar, d.softness ORDER BY oi.dish_name`, date, mealType)
	if err != nil {
		fail(c, http.StatusInternalServerError, "汇总失败")
		return
	}
	defer dishRows.Close()
	dishes := []gin.H{}
	for dishRows.Next() {
		var name, softness string
		var ls, lg bool
		var qty int
		var notes *string
		dishRows.Scan(&name, &ls, &lg, &softness, &qty, &notes)
		n := ""
		if notes != nil {
			n = *notes
		}
		dishes = append(dishes, gin.H{"dish_name": name, "low_salt": ls, "low_sugar": lg,
			"softness": softness, "qty": qty, "custom_notes": n})
	}
	orderRows, err := s.db.Query(`SELECT o.id, o.order_no, e.name, e.dietary_restrictions, o.status, o.delivery_type,
		o.strict_mode, o.notes, o.is_holiday_special
		FROM orders o JOIN elders e ON e.id=o.elder_id
		WHERE o.meal_date=$1 AND o.meal_type=$2 AND o.status IN ('confirmed','preparing','ready','delivering')
		ORDER BY o.id`, date, mealType)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询餐单失败")
		return
	}
	defer orderRows.Close()
	orders := []gin.H{}
	for orderRows.Next() {
		var id int
		var no, elderName, dietary, status, delType, notes string
		var strict, holiday bool
		orderRows.Scan(&id, &no, &elderName, &dietary, &status, &delType, &strict, &notes, &holiday)
		items := []gin.H{}
		irows, err := s.db.Query(`SELECT dish_name, qty, custom_note FROM order_items WHERE order_id=$1`, id)
		if err == nil {
			for irows.Next() {
				var dn, cn string
				var q int
				irows.Scan(&dn, &q, &cn)
				items = append(items, gin.H{"dish_name": dn, "qty": q, "custom_note": cn})
			}
			irows.Close()
		}
		orders = append(orders, gin.H{"id": id, "order_no": no, "elder_name": elderName,
			"dietary_restrictions": dietary, "status": status, "delivery_type": delType,
			"strict_mode": strict, "notes": notes, "is_holiday_special": holiday, "items": items})
	}
	var batchID int
	var batchStatus string
	_ = s.db.QueryRow(`SELECT id, status FROM kitchen_batches WHERE batch_date=$1 AND meal_type=$2`, date, mealType).
		Scan(&batchID, &batchStatus)
	ok(c, gin.H{"date": date, "meal_type": mealType, "dishes": dishes, "orders": orders,
		"batch_id": batchID, "batch_status": batchStatus})
}

func (s *Server) listBatches(c *gin.Context) {
	date := c.DefaultQuery("date", today())
	rows, err := s.db.Query(`SELECT b.id, b.batch_no, b.batch_date::text, b.meal_type, b.status, b.released_at::text,
		COALESCE(u.name,''), (SELECT COUNT(*) FROM orders WHERE batch_id=b.id)
		FROM kitchen_batches b LEFT JOIN users u ON u.id=b.operator_id
		WHERE b.batch_date >= $1::date - 7 ORDER BY b.batch_date DESC, b.id DESC`, date)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询批次失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var id, orderCount int
		var no, dt, mt, st, op string
		var released *string
		rows.Scan(&id, &no, &dt, &mt, &st, &released, &op, &orderCount)
		rel := ""
		if released != nil {
			rel = *released
		}
		list = append(list, gin.H{"id": id, "batch_no": no, "batch_date": dt[:10], "meal_type": mt,
			"status": st, "released_at": rel, "operator": op, "order_count": orderCount})
	}
	ok(c, list)
}

// 创建出餐批次：把当日已确认餐单纳入批次，按菜品汇总计划数量
func (s *Server) createBatch(c *gin.Context) {
	var req struct {
		Date     string `json:"date" binding:"required"`
		MealType string `json:"meal_type"`
		Notes    string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请选择出餐日期")
		return
	}
	if req.MealType == "" {
		req.MealType = "lunch"
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var exists int
	tx.QueryRow(`SELECT COUNT(*) FROM kitchen_batches WHERE batch_date=$1 AND meal_type=$2`, req.Date, req.MealType).Scan(&exists)
	if exists > 0 {
		fail(c, http.StatusBadRequest, "该日期餐别的批次已存在，请直接查看")
		return
	}
	orderRows, err := tx.Query(`SELECT id FROM orders WHERE meal_date=$1 AND meal_type=$2 AND status='confirmed' ORDER BY id`,
		req.Date, req.MealType)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询餐单失败")
		return
	}
	orderIDs := []int{}
	for orderRows.Next() {
		var id int
		orderRows.Scan(&id)
		orderIDs = append(orderIDs, id)
	}
	orderRows.Close()
	if len(orderIDs) == 0 {
		fail(c, http.StatusBadRequest, "该日期没有待备餐的餐单")
		return
	}
	var batchID int
	batchNo := "B" + replaceDash(req.Date) + "-" + req.MealType
	err = tx.QueryRow(`INSERT INTO kitchen_batches(batch_date, meal_type, batch_no, operator_id, notes)
		VALUES($1,$2,$3,$4,$5) RETURNING id`,
		req.Date, req.MealType, batchNo, c.GetInt("uid"), req.Notes).Scan(&batchID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建批次失败")
		return
	}
	for _, oid := range orderIDs {
		if _, err := tx.Exec(`UPDATE orders SET status='preparing', batch_id=$1, updated_at=now() WHERE id=$2`, batchID, oid); err != nil {
			fail(c, http.StatusInternalServerError, "更新餐单状态失败")
			return
		}
		addOrderEvent(tx, oid, c.GetInt("uid"), c.GetString("name"), "厨房开始备餐", "纳入批次")
	}
	// 按菜品汇总计划数量
	if _, err := tx.Exec(`INSERT INTO batch_items(batch_id, dish_id, dish_name, planned_qty)
		SELECT $1, oi.dish_id, oi.dish_name, SUM(oi.qty)
		FROM order_items oi JOIN orders o ON o.id=oi.order_id
		WHERE o.batch_id=$1 GROUP BY oi.dish_id, oi.dish_name`, batchID); err != nil {
		fail(c, http.StatusInternalServerError, "生成备餐计划失败")
		return
	}
	notify(tx, 0, "community", 0, "厨房批次已创建", req.Date+" "+req.MealType+" 批次开始备餐")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"id": batchID, "order_count": len(orderIDs)})
}

func (s *Server) getBatch(c *gin.Context) {
	id := c.Param("id")
	var b struct {
		BatchNo, Date, MealType, Status string
		Released                        *string
		Notes                           string
	}
	err := s.db.QueryRow(`SELECT batch_no, batch_date::text, meal_type, status, released_at::text, notes FROM kitchen_batches WHERE id=$1`, id).
		Scan(&b.BatchNo, &b.Date, &b.MealType, &b.Status, &b.Released, &b.Notes)
	if err != nil {
		fail(c, http.StatusNotFound, "批次不存在")
		return
	}
	items := []gin.H{}
	rows, err := s.db.Query(`SELECT dish_name, planned_qty, actual_qty FROM batch_items WHERE batch_id=$1 ORDER BY id`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var n string
			var p, a int
			rows.Scan(&n, &p, &a)
			items = append(items, gin.H{"dish_name": n, "planned_qty": p, "actual_qty": a})
		}
	}
	orders := []gin.H{}
	orows, err := s.db.Query(`SELECT o.id, o.order_no, e.name, o.status, o.delivery_type FROM orders o
		JOIN elders e ON e.id=o.elder_id WHERE o.batch_id=$1 ORDER BY o.id`, id)
	if err == nil {
		defer orows.Close()
		for orows.Next() {
			var oid int
			var no, en, st, dt string
			orows.Scan(&oid, &no, &en, &st, &dt)
			orders = append(orders, gin.H{"id": oid, "order_no": no, "elder_name": en, "status": st, "delivery_type": dt})
		}
	}
	released := ""
	if b.Released != nil {
		released = *b.Released
	}
	ok(c, gin.H{"id": atoi(id), "batch_no": b.BatchNo, "batch_date": b.Date[:10], "meal_type": b.MealType,
		"status": b.Status, "released_at": released, "notes": b.Notes, "items": items, "orders": orders})
}

// 出餐：录入实际出餐数量；少做则自动生成异常并把受影响餐单置为异常
func (s *Server) releaseBatch(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Items []struct {
			DishName  string `json:"dish_name" binding:"required"`
			ActualQty int    `json:"actual_qty"`
		} `json:"items" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请录入实际出餐数量")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var status, batchDate, mealType string
	err = tx.QueryRow(`SELECT status, batch_date, meal_type FROM kitchen_batches WHERE id=$1 FOR UPDATE`, id).
		Scan(&status, &batchDate, &mealType)
	if err != nil {
		fail(c, http.StatusNotFound, "批次不存在")
		return
	}
	if status == "released" {
		fail(c, http.StatusBadRequest, "该批次已出餐")
		return
	}
	shortages := map[string]int{}
	for _, it := range req.Items {
		var planned int
		err := tx.QueryRow(`SELECT planned_qty FROM batch_items WHERE batch_id=$1 AND dish_name=$2`, id, it.DishName).Scan(&planned)
		if err != nil {
			continue
		}
		if _, err := tx.Exec(`UPDATE batch_items SET actual_qty=$1 WHERE batch_id=$2 AND dish_name=$3`, it.ActualQty, id, it.DishName); err != nil {
			fail(c, http.StatusInternalServerError, "更新出餐数量失败")
			return
		}
		if it.ActualQty < planned {
			shortages[it.DishName] = planned - it.ActualQty
		}
	}
	if _, err := tx.Exec(`UPDATE kitchen_batches SET status='released', released_at=now() WHERE id=$1`, id); err != nil {
		fail(c, http.StatusInternalServerError, "出餐失败")
		return
	}
	// 正常餐单 → ready；为配送到家/志愿者帮送的餐单生成配送任务
	orderRows, err := tx.Query(`SELECT id, elder_id, order_no, delivery_type FROM orders WHERE batch_id=$1 AND status='preparing' ORDER BY id`, id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询批次餐单失败")
		return
	}
	type ord struct {
		id, elderID int
		no, delType string
	}
	ords := []ord{}
	for orderRows.Next() {
		var o ord
		orderRows.Scan(&o.id, &o.elderID, &o.no, &o.delType)
		ords = append(ords, o)
	}
	orderRows.Close()
	// 计算每道菜被少做的餐单（取下单最晚的 N 单承担缺货）
	shortOrderIDs := map[int]bool{}
	for dishName, short := range shortages {
		rows, err := tx.Query(`SELECT o.id FROM orders o JOIN order_items oi ON oi.order_id=o.id
			WHERE o.batch_id=$1 AND oi.dish_name=$2 AND o.status='preparing' ORDER BY o.id DESC LIMIT $3`, id, dishName, short)
		if err != nil {
			continue
		}
		for rows.Next() {
			var oid int
			rows.Scan(&oid)
			shortOrderIDs[oid] = true
		}
		rows.Close()
	}
	for _, o := range ords {
		if shortOrderIDs[o.id] {
			if _, err := tx.Exec(`UPDATE orders SET status='exception', updated_at=now() WHERE id=$1`, o.id); err != nil {
				continue
			}
			anID, _ := createAnomaly(tx, o.id, o.elderID, "kitchen_shortage",
				"厨房少做：餐单 "+o.no+" 所需菜品出餐数量不足，请社区联系老人/家属协调改餐或退餐。", c.GetInt("uid"))
			addOrderEvent(tx, o.id, c.GetInt("uid"), c.GetString("name"), "厨房少做", "出餐数量不足，转入异常处理")
			notifyElderParties(tx, o.elderID, o.id, "餐单异常：厨房少做", o.no+" 出餐数量不足，社区将联系您处理")
			_ = anID
			continue
		}
		if _, err := tx.Exec(`UPDATE orders SET status='ready', updated_at=now() WHERE id=$1`, o.id); err != nil {
			continue
		}
		addOrderEvent(tx, o.id, c.GetInt("uid"), c.GetString("name"), "厨房出餐完成", "批次已出餐")
		if o.delType == "home" || o.delType == "volunteer" {
			dtype := "rider"
			if o.delType == "volunteer" {
				dtype = "volunteer"
			}
			if _, err := tx.Exec(`INSERT INTO deliveries(order_id, deliverer_type) VALUES($1,$2) ON CONFLICT (order_id) DO NOTHING`, o.id, dtype); err == nil {
				if dtype == "volunteer" {
					notify(tx, 0, "volunteer", o.id, "新的帮送任务", o.no+" 待志愿者接单帮送")
				} else {
					notify(tx, 0, "rider", o.id, "新的配送任务", o.no+" 待取餐配送")
				}
			}
		} else {
			notify(tx, 0, "community", o.id, "待现场取餐", o.no+" 已出餐，等待老人到社区食堂取餐")
		}
		notifyElderParties(tx, o.elderID, o.id, "餐单已出餐", o.no+" 厨房已出餐")
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"released": true, "shortage_orders": len(shortOrderIDs)})
}
