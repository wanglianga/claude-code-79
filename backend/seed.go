package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

// 种子数据：全部角色演示账号 + 跨状态餐单，日期相对今天生成，任何一天启动都可演示
func seed(db *sql.DB) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		log.Println("检测到已有数据，跳过种子初始化")
		return nil
	}
	log.Println("开始写入种子数据...")
	s := &Server{db: db}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// ---------- 用户 ----------
	type seedUser struct{ username, pw, name, phone, role string }
	users := []seedUser{
		{"admin", "admin123", "平台管理员", "13900000001", "admin"},
		{"finance01", "finance123", "王财政", "13900000002", "finance"},
		{"community01", "community123", "刘社区", "13900000003", "community"},
		{"kitchen01", "kitchen123", "何师傅", "13900000004", "kitchen"},
		{"rider01", "rider123", "张骑手", "13900000005", "rider"},
		{"volunteer01", "volunteer123", "陈志愿", "13900000006", "volunteer"},
		{"family01", "family123", "张强", "13800001111", "family"},
		{"family02", "family123", "李婷", "13800002222", "family"},
		{"elder01", "elder123", "王桂花", "13800003333", "elder"},
	}
	uid := map[string]int{}
	for _, u := range users {
		var id int
		if err := tx.QueryRow(`INSERT INTO users(username, password_hash, name, phone, role) VALUES($1,$2,$3,$4,$5) RETURNING id`,
			u.username, hashPassword(u.pw), u.name, u.phone, u.role).Scan(&id); err != nil {
			return fmt.Errorf("创建用户 %s 失败: %w", u.username, err)
		}
		uid[u.username] = id
	}

	// ---------- 老人档案 ----------
	type seedElder struct {
		name, idCard, gender, birth, phone, address, level, dietary, boxMethod string
		subsidy                                                                float64
		knock, cog, alone, mob                                                 bool
		emergName, emergPhone, note                                            string
		familyKey, userKey                                                     string
	}
	elders := []seedElder{
		{"张秀英", "110101194403020021", "女", "1944-03-02", "13800004444", "幸福里小区 3 栋 2 单元 501", "full",
			"低盐;软烂;忌辛辣", "next_delivery", 12, true, true, true, false,
			"张强", "13800001111", "独居且患轻度认知障碍，送餐必须敲门确认并拍照，属重点关怀对象", "family01", ""},
		{"李建国", "110101195007190033", "男", "1950-07-19", "13800005555", "康乐社区 7 号楼 1 单元 302", "partial",
			"低糖;软烂", "community_point", 8, true, false, false, true,
			"李婷", "13800002222", "腿脚不便，上下楼困难，餐盒统一回社区回收点", "family02", ""},
		{"王桂花", "110101195601250045", "女", "1956-01-25", "13800003333", "康乐社区 2 号楼 3 单元 101", "partial",
			"", "onsite", 6, false, false, false, false,
			"王桂花本人", "13800003333", "身体硬朗，每日到社区食堂自取，会用手机下单", "", "elder01"},
		{"陈福生", "110101194811080057", "男", "1948-11-08", "13800006666", "幸福里小区 5 栋 1 单元 201", "none",
			"低盐", "next_delivery", 0, false, false, false, false,
			"陈邻居", "13800007777", "自费就餐老人", "", ""},
	}
	eid := map[string]int{}
	for _, e := range elders {
		var fam, usr interface{}
		if e.familyKey != "" {
			fam = uid[e.familyKey]
		}
		if e.userKey != "" {
			usr = uid[e.userKey]
		}
		var id int
		err := tx.QueryRow(`INSERT INTO elders(user_id, name, id_card, gender, birth_date, phone, address, subsidy_level,
			subsidy_per_meal, dietary_restrictions, need_knock_confirm, box_return_method, emergency_contact_name,
			emergency_contact_phone, cognitive_impairment, living_alone, mobility_impaired, family_user_id, community_note)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) RETURNING id`,
			usr, e.name, e.idCard, e.gender, e.birth, e.phone, e.address, e.level, e.subsidy, e.dietary,
			e.knock, e.boxMethod, e.emergName, e.emergPhone, e.cog, e.alone, e.mob, fam, e.note).Scan(&id)
		if err != nil {
			return fmt.Errorf("创建老人 %s 失败: %w", e.name, err)
		}
		eid[e.name] = id
	}

	// ---------- 菜品 ----------
	type seedDish struct {
		name              string
		price             float64
		lowSalt, lowSugar bool
		softness          string
		nutrition         string
		holidayOnly       bool
	}
	dishes := []seedDish{
		{"软烂红烧肉", 12, false, false, "mushy", "高蛋白，炖至软烂易咀嚼", false},
		{"清蒸鲈鱼", 10, true, true, "normal", "低盐低脂，优质蛋白", false},
		{"番茄炒蛋", 6, false, false, "soft", "维生素与蛋白质均衡", false},
		{"低盐时蔬", 4, true, true, "soft", "膳食纤维，低盐炒制", false},
		{"无糖南瓜粥", 3, true, true, "mushy", "无糖配方，软糯易消化", false},
		{"软米饭", 2, false, false, "soft", "软糯米饭", false},
		{"低糖豆浆", 2, true, true, "normal", "植物蛋白，低糖", false},
		{"节日八宝饭", 8, false, false, "soft", "节日特供加餐", true},
	}
	dishID := map[string]int{}
	dishPrice := map[string]float64{}
	dishCost := map[string]float64{
		"软烂红烧肉": 7, "清蒸鲈鱼": 6, "番茄炒蛋": 3, "低盐时蔬": 2, "无糖南瓜粥": 1.5,
		"软米饭": 1, "低糖豆浆": 1, "节日八宝饭": 4,
	}
	for _, d := range dishes {
		var id int
		if err := tx.QueryRow(`INSERT INTO dishes(name, price, low_salt, low_sugar, softness, nutrition, holiday_only, unit_cost)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			d.name, d.price, d.lowSalt, d.lowSugar, d.softness, d.nutrition, d.holidayOnly, dishCost[d.name]).Scan(&id); err != nil {
			return fmt.Errorf("创建菜品 %s 失败: %w", d.name, err)
		}
		dishID[d.name] = id
		dishPrice[d.name] = d.price
	}

	// 每月补贴可享次数（0=不限次）
	quotaByName := map[string]int{"张秀英": 30, "李建国": 20, "王桂花": 25, "陈福生": 0}
	for name, q := range quotaByName {
		if _, err := tx.Exec(`UPDATE elders SET monthly_quota=$2 WHERE name=$1`, name, q); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// ---------- 餐单辅助 ----------
	type itemSpec struct {
		dish string
		qty  int
		note string
	}
	insertOrder := func(elderName, date, mealType, delType, status, source, createdBy string,
		items []itemSpec, holiday bool, holidayName string) (int, error) {
		e := elders[0]
		var el seedElder
		found := false
		for _, x := range elders {
			if x.name == elderName {
				el = x
				found = true
				break
			}
		}
		if !found {
			el = e
		}
		total := 0.0
		for _, it := range items {
			total += dishPrice[it.dish] * float64(it.qty)
		}
		subsidy := el.subsidy
		if subsidy > total {
			subsidy = total
		}
		hextra := 0.0
		if holiday {
			hextra = 5
			if subsidy+hextra > total {
				hextra = total - subsidy
			}
		}
		strict := el.cog || el.alone || el.mob
		var id int
		err := db.QueryRow(`INSERT INTO orders(order_no, elder_id, created_by, order_source, meal_date, meal_type, delivery_type,
			address, need_knock_confirm, strict_mode, box_return_method, boxes_issued, total_amount, subsidy_amount,
			holiday_extra, payable_amount, is_holiday_special, holiday_name, status)
			VALUES('TMP-'||gen_random_uuid(), $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,2,$11,$12,$13,$14,$15,$16,$17) RETURNING id`,
			eid[elderName], uid[createdBy], source, date, mealType, delType, el.address, el.knock, strict,
			el.boxMethod, total, subsidy+hextra, hextra, total-subsidy-hextra, holiday, holidayName, status).Scan(&id)
		if err != nil {
			return 0, err
		}
		if _, err := db.Exec(`UPDATE orders SET order_no=$1 WHERE id=$2`, orderNo(date, id), id); err != nil {
			return 0, err
		}
		for _, it := range items {
			if _, err := db.Exec(`INSERT INTO order_items(order_id, dish_id, dish_name, price, qty, custom_note)
				VALUES($1,$2,$3,$4,$5,$6)`, id, dishID[it.dish], it.dish, dishPrice[it.dish], it.qty, it.note); err != nil {
				return 0, err
			}
		}
		return id, nil
	}
	addDelivery := func(orderID int, delivererKey, dtype, status, pickup, delivered, signedBy string) error {
		var pu, de interface{}
		if pickup != "" {
			pu = pickup
		}
		if delivered != "" {
			de = delivered
		}
		_, err := db.Exec(`INSERT INTO deliveries(order_id, deliverer_id, deliverer_type, thermal_box_no, route_info,
			status, pickup_time, delivered_time, sign_type, signed_by_name, knock_confirmed)
			VALUES($1,$2,$3,'WBX-01','社区食堂→幸福里/康乐社区沿线',$4,$5,$6,'elder',$7,TRUE)`,
			orderID, uid[delivererKey], dtype, status, pu, de, signedBy)
		return err
	}
	addBox := func(orderID int, elderName string, issued, returned int, method, status string) error {
		var ra interface{}
		if status != "pending" {
			ra = time.Now()
		}
		_, err := db.Exec(`INSERT INTO box_records(order_id, elder_id, boxes_issued, boxes_returned, return_method, status, returned_at)
			VALUES($1,$2,$3,$4,$5,$6,$7)`, orderID, eid[elderName], issued, returned, method, status, ra)
		return err
	}
	event := func(orderID int, actorKey, action, detail string) {
		name := actorKey
		for _, u := range users {
			if u.username == actorKey {
				name = u.name
				break
			}
		}
		db.Exec(`INSERT INTO order_events(order_id, actor_id, actor_name, action, detail) VALUES($1,$2,$3,$4,$5)`,
			orderID, uid[actorKey], name, action, detail)
	}

	now := time.Now()
	day := func(offset int) string { return now.AddDate(0, 0, offset).Format("2006-01-02") }
	lastMonth := now.AddDate(0, -1, 0).Format("2006-01")
	lastMonthDay := func(d int) string { return fmt.Sprintf("%s-%02d", lastMonth, d) }

	// ---------- 上月餐单（将归入已归档财政档案） ----------
	lmOrders := []int{}
	for _, spec := range []struct {
		elder, date, status string
		items               []itemSpec
	}{
		{"张秀英", lastMonthDay(8), "completed", []itemSpec{{"软烂红烧肉", 1, ""}, {"低盐时蔬", 1, ""}, {"软米饭", 1, ""}}},
		{"张秀英", lastMonthDay(15), "completed", []itemSpec{{"清蒸鲈鱼", 1, ""}, {"低盐时蔬", 1, ""}, {"软米饭", 1, ""}}},
		{"李建国", lastMonthDay(10), "completed", []itemSpec{{"番茄炒蛋", 1, ""}, {"无糖南瓜粥", 1, ""}, {"软米饭", 1, ""}}},
		{"王桂花", lastMonthDay(12), "completed", []itemSpec{{"番茄炒蛋", 1, ""}, {"软米饭", 1, ""}}},
		{"陈福生", lastMonthDay(12), "completed", []itemSpec{{"软烂红烧肉", 1, ""}, {"低盐时蔬", 1, ""}, {"软米饭", 1, ""}}},
		{"李建国", lastMonthDay(20), "refunded", []itemSpec{{"清蒸鲈鱼", 1, ""}, {"软米饭", 1, ""}}},
	} {
		delType := "home"
		if spec.elder == "王桂花" {
			delType = "community_pickup"
		}
		source := "family"
		creator := "family01"
		if spec.elder == "李建国" {
			creator = "family02"
		}
		if spec.elder == "王桂花" {
			source, creator = "self", "elder01"
		}
		if spec.elder == "陈福生" {
			source, creator = "community", "community01"
		}
		oid, err := insertOrder(spec.elder, spec.date, "lunch", delType, spec.status, source, creator, spec.items, false, "")
		if err != nil {
			return err
		}
		if spec.status == "completed" {
			lmOrders = append(lmOrders, oid)
			if delType != "community_pickup" {
				addDelivery(oid, "rider01", "rider", "delivered", spec.date+" 11:10:00", spec.date+" 11:38:00", spec.elder)
			}
			addBox(oid, spec.elder, 2, 2, map[string]string{"王桂花": "onsite", "李建国": "community_point"}[spec.elder], "returned")
			if spec.elder == "张秀英" || spec.elder == "陈福生" {
				db.Exec(`UPDATE box_records SET return_method='next_delivery' WHERE order_id=$1`, oid)
			}
		}
		if spec.status == "refunded" {
			db.Exec(`UPDATE orders SET refund_amount=payable_amount, cancel_reason='老人生病住院，家属申请退餐' WHERE id=$1`, oid)
		}
		event(oid, creator, "提交订餐", "种子数据")
	}
	// 上月核销：归档财政档案
	agg, items, err := s.computeReconciliation(lastMonth)
	if err != nil {
		return err
	}
	var recID int
	err = db.QueryRow(`INSERT INTO reconciliations(month, status, total_orders, signed_orders, cancelled_orders, exception_orders,
		subsidy_total, refund_total, payable_total, kitchen_settlement, anomaly_count, followup_done,
		boxes_issued, boxes_returned, recycle_rate, holiday_extra_total, note, created_by, confirmed_at, archived_at)
		VALUES($1,'archived',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'上月例行核销归档',$16, now() - interval '15 days', now() - interval '14 days') RETURNING id`,
		lastMonth, agg["total_orders"], agg["signed_orders"], agg["cancelled_orders"], agg["exception_orders"],
		agg["subsidy_total"], agg["refund_total"], agg["payable_total"], agg["kitchen_settlement"],
		agg["anomaly_count"], agg["followup_done"], agg["boxes_issued"], agg["boxes_returned"],
		agg["recycle_rate"], agg["holiday_extra_total"], uid["finance01"]).Scan(&recID)
	if err != nil {
		return err
	}
	for _, it := range items {
		if _, err := db.Exec(`INSERT INTO reconciliation_items(reconciliation_id, order_id, order_no, elder_name, meal_date,
			order_status, total_amount, subsidy_amount, included, reason)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, recID, it["order_id"], it["order_no"], it["elder_name"],
			it["meal_date"], it["order_status"], it["total_amount"], it["subsidy_amount"], it["included"], it["reason"]); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`UPDATE orders SET status='settled', settled_in=$1 WHERE to_char(meal_date,'YYYY-MM')=$2 AND status IN ('signed','completed')`,
		recID, lastMonth); err != nil {
		return err
	}

	// ---------- 本月历史餐单 ----------
	// 09-10 张秀英已签收，但餐盒未回收 -> 未回收异常工单（open）
	oid1, err := insertOrder("张秀英", day(-5), "lunch", "home", "signed", "family", "family01",
		[]itemSpec{{"软烂红烧肉", 1, "炖烂一些"}, {"低盐时蔬", 1, ""}, {"软米饭", 1, ""}}, false, "")
	if err != nil {
		return err
	}
	addDelivery(oid1, "rider01", "rider", "delivered", day(-5)+" 11:05:00", day(-5)+" 11:32:00", "张秀英")
	addBox(oid1, "张秀英", 2, 0, "next_delivery", "pending")
	event(oid1, "family01", "提交订餐", "家属代订")
	event(oid1, "rider01", "送达签收", "签收人：张秀英")
	anID := 0
	err = db.QueryRow(`INSERT INTO anomalies(order_id, elder_id, type, priority, description, reported_by)
		VALUES($1,$2,'box_not_returned','high',$3,$4) RETURNING id`, oid1, eid["张秀英"],
		"餐盒 2 个未回收（约定下次送餐回收，已连续两次未带回），请社区跟进。", uid["community01"]).Scan(&anID)
	if err != nil {
		return err
	}

	// 09-12 张秀英又一单已签收，餐盒部分回收（累计未归还 3 个，超押金阈值）
	oid1b, err := insertOrder("张秀英", day(-3), "dinner", "home", "signed", "family", "family01",
		[]itemSpec{{"番茄炒蛋", 1, ""}, {"无糖南瓜粥", 1, ""}, {"软米饭", 1, ""}}, false, "")
	if err != nil {
		return err
	}
	addDelivery(oid1b, "rider01", "rider", "delivered", day(-3)+" 17:05:00", day(-3)+" 17:30:00", "张秀英")
	addBox(oid1b, "张秀英", 2, 1, "next_delivery", "partial")

	// 09-12 李建国已签收、餐盒已回社区点；其反馈饭菜不适合 -> meal_unsuitable(open)
	oid2, err := insertOrder("李建国", day(-3), "lunch", "home", "completed", "family", "family02",
		[]itemSpec{{"番茄炒蛋", 1, ""}, {"无糖南瓜粥", 1, ""}, {"软米饭", 1, ""}}, false, "")
	if err != nil {
		return err
	}
	addDelivery(oid2, "rider01", "rider", "delivered", day(-3)+" 11:08:00", day(-3)+" 11:41:00", "李婷")
	addBox(oid2, "李建国", 2, 2, "community_point", "returned")
	if _, err := db.Exec(`INSERT INTO feedbacks(order_id, elder_id, from_user, rating, suitable, content)
		VALUES($1,$2,$3,2,FALSE,'番茄炒蛋偏硬，老人牙口不好咬不动，建议更软烂')`, oid2, eid["李建国"], uid["family02"]); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO anomalies(order_id, elder_id, type, priority, description, reported_by)
		VALUES($1,$2,'meal_unsuitable','normal','老人「李建国」反馈饭菜不适合：番茄炒蛋偏硬，建议更软烂。',$3)`,
		oid2, eid["李建国"], uid["family02"]); err != nil {
		return err
	}

	// 09-13 张秀英未开门 -> 记录联系尝试 -> 异常 -> 社区回访 -> 已办结（改电话确认配送+次日重点关注）
	oid3, err := insertOrder("张秀英", day(-2), "lunch", "home", "refunded", "family", "family01",
		[]itemSpec{{"清蒸鲈鱼", 1, ""}, {"低盐时蔬", 1, ""}, {"软米饭", 1, ""}}, false, "")
	if err != nil {
		return err
	}
	db.Exec(`UPDATE orders SET refund_amount=payable_amount, cancel_reason='老人未开门且联系不上，社区确认老人在女儿家小住，退餐退款' WHERE id=$1`, oid3)
	addDelivery(oid3, "rider01", "rider", "failed", day(-2)+" 11:06:00", "", "")
	var dlv3 int
	db.QueryRow(`SELECT id FROM deliveries WHERE order_id=$1`, oid3).Scan(&dlv3)
	if _, err := db.Exec(`INSERT INTO contact_attempts(delivery_id, order_id, elder_id, knock_done, phone_done, neighbor_done, family_done, note, reported_by)
		VALUES($1,$2,$3,TRUE,TRUE,TRUE,FALSE,'敲门多次无人应答，电话未接，邻居称老人被女儿接走，家属电话未接通',$4)`,
		dlv3, oid3, eid["张秀英"], uid["rider01"]); err != nil {
		return err
	}
	var an3 int
	err = db.QueryRow(`INSERT INTO anomalies(order_id, elder_id, type, priority, description, reported_by, status, resolution, resolved_at)
		VALUES($1,$2,'no_answer','high','骑手送达后多次敲门无人应答，电话未接通。老人为认知障碍独居老人，需立即核实安全。',$3,'resolved',
		'电话回访其女张强，确认老人在女儿家小住，安全；本单退餐退款；老人记性差易忘送餐，后续配送改为电话确认后再上门，次日重点关注。', $4) RETURNING id`,
		oid3, eid["张秀英"], uid["rider01"], time.Now().Add(-24*time.Hour)).Scan(&an3)
	if err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO follow_ups(anomaly_id, elder_id, community_id, type, elder_status, result)
		VALUES($1,$2,$3,'phone','need_help','电话联系家属确认老人安全，老人暂住女儿家；老人记性变差，建议列为关注对象并改为电话确认配送')`,
		an3, eid["张秀英"], uid["community01"]); err != nil {
		return err
	}
	// 回访结果联动：张秀英 风险=关注、配送方式=电话确认后再上门、次日重点关注至今
	if _, err := db.Exec(`UPDATE elders SET risk_level='attention', delivery_confirm_mode='phone_first', focus_until=$1 WHERE id=$2`,
		day(0), eid["张秀英"]); err != nil {
		return err
	}

	// 09-14 王桂花社区食堂自取，现场签收+现场回收
	oid4, err := insertOrder("王桂花", day(-1), "lunch", "community_pickup", "completed", "self", "elder01",
		[]itemSpec{{"番茄炒蛋", 1, ""}, {"低盐时蔬", 1, ""}, {"软米饭", 1, ""}}, false, "")
	if err != nil {
		return err
	}
	addBox(oid4, "王桂花", 2, 2, "onsite", "returned")

	// 09-14 陈福生自费单已签收，餐盒部分回收
	oid5, err := insertOrder("陈福生", day(-1), "lunch", "home", "signed", "community", "community01",
		[]itemSpec{{"软烂红烧肉", 1, ""}, {"低盐时蔬", 1, ""}, {"软米饭", 1, ""}}, false, "")
	if err != nil {
		return err
	}
	addDelivery(oid5, "volunteer01", "volunteer", "delivered", day(-1)+" 11:15:00", day(-1)+" 11:50:00", "陈福生")
	addBox(oid5, "陈福生", 2, 1, "next_delivery", "partial")

	// 09-13 陈福生又一单已签收，餐盒未回收（累计未归还 3 个，超押金阈值）
	oid5b, err := insertOrder("陈福生", day(-2), "dinner", "home", "signed", "community", "community01",
		[]itemSpec{{"番茄炒蛋", 1, ""}, {"软米饭", 1, ""}}, false, "")
	if err != nil {
		return err
	}
	addDelivery(oid5b, "rider01", "rider", "delivered", day(-2)+" 17:10:00", day(-2)+" 17:40:00", "陈福生")
	addBox(oid5b, "陈福生", 2, 0, "next_delivery", "pending")

	// 中秋节加餐（未来日期，节日特供）
	if _, err := insertOrder("张秀英", day(10), "lunch", "home", "confirmed", "community", "community01",
		[]itemSpec{{"节日八宝饭", 1, ""}, {"清蒸鲈鱼", 1, ""}, {"低盐时蔬", 1, ""}}, true, "中秋节加餐"); err != nil {
		return err
	}

	// ---------- 今日待备餐餐单（供厨房/骑手现场演示） ----------
	todaySpecs := []struct {
		elder, delType, source, creator string
		items                           []itemSpec
	}{
		{"张秀英", "home", "family", "family01", []itemSpec{{"软烂红烧肉", 1, "务必软烂"}, {"低盐时蔬", 1, "少盐"}, {"无糖南瓜粥", 1, ""}, {"软米饭", 1, ""}}},
		{"李建国", "home", "family", "family02", []itemSpec{{"番茄炒蛋", 1, "炒软一些"}, {"低糖豆浆", 1, ""}, {"软米饭", 1, ""}}},
		{"王桂花", "community_pickup", "self", "elder01", []itemSpec{{"清蒸鲈鱼", 1, ""}, {"软米饭", 1, ""}}},
		{"陈福生", "volunteer", "community", "community01", []itemSpec{{"番茄炒蛋", 1, ""}, {"低盐时蔬", 1, ""}, {"软米饭", 1, ""}}},
	}
	for _, spec := range todaySpecs {
		oid, err := insertOrder(spec.elder, day(0), "lunch", spec.delType, "confirmed", spec.source, spec.creator, spec.items, false, "")
		if err != nil {
			return err
		}
		event(oid, spec.creator, "提交订餐", "种子数据")
	}

	// ---------- 补贴资格变更记录 ----------
	if _, err := db.Exec(`INSERT INTO subsidy_changes(elder_id, old_level, new_level, old_amount, new_amount, reason, changed_by, affected_orders)
		VALUES($1,'partial','partial',6,8,'街道复核提高补贴标准',$2,0)`, eid["李建国"], uid["community01"]); err != nil {
		return err
	}

	// ---------- 餐盒押金演示：张秀英为困难老人（可申请免押+志愿回收） ----------
	if _, err := db.Exec(`UPDATE elders SET elder_type='difficult' WHERE name='张秀英'`); err != nil {
		return err
	}
	// 餐盒库存：基准 50，扣除在途未归还（张秀英 3 个 + 陈福生 1 个）
	if _, err := db.Exec(`INSERT INTO box_inventory(location, stock) VALUES('社区食堂', 50) ON CONFLICT (location) DO NOTHING`); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE box_inventory SET stock = 50 - COALESCE((
		SELECT SUM(boxes_issued-boxes_returned) FROM box_records),0), updated_at=now() WHERE location='社区食堂'`); err != nil {
		return err
	}

	// ====================================================================
	// 状态变更（住院/搬离/去世）四段清算演示数据
	// ====================================================================
	insertDemoElder := func(name, idCard, gender, birth, addr, level string, subsidy float64, quota int, active bool, status string) (int, error) {
		var id int
		err := db.QueryRow(`INSERT INTO elders(name, id_card, gender, birth_date, phone, address, subsidy_level, subsidy_per_meal,
			dietary_restrictions, box_return_method, emergency_contact_name, emergency_contact_phone,
			monthly_quota, active, service_status)
			VALUES($1,$2,$3,$4::date,'13800000000',$5,$6,$7,'低盐','next_delivery','家属','13800009999',$8,$9,$10) RETURNING id`,
			name, idCard, gender, birth, addr, level, subsidy, quota, active, status).Scan(&id)
		return id, err
	}
	// 演示批次
	ensureBatch := func(date, mealType, bstatus string) int {
		var bid int
		db.QueryRow(`INSERT INTO kitchen_batches(batch_date, meal_type, batch_no, status, released_at, operator_id)
			VALUES($1::date,$2,$3,$4, CASE WHEN $4='released' THEN now() ELSE NULL END,$5)
			ON CONFLICT (batch_date, meal_type) DO UPDATE SET status=EXCLUDED.status RETURNING id`,
			date, mealType, "B"+replaceDash(date)+"-"+mealType, bstatus, uid["kitchen01"]).Scan(&bid)
		return bid
	}
	type demoItemSpec struct {
		dish string
		qty  int
	}
	insertDemoOrder := func(elderID int, date, mealType, status, delType string, batchID int, items []demoItemSpec) (int, error) {
		total, cost := 0.0, 0.0
		for _, it := range items {
			total += dishPrice[it.dish] * float64(it.qty)
			cost += dishCost[it.dish] * float64(it.qty)
		}
		subsidy := total
		if subsidy > 10 {
			subsidy = 10
		}
		payable := total - subsidy
		var bid interface{}
		if batchID > 0 {
			bid = batchID
		}
		var oid int
		err := db.QueryRow(`INSERT INTO orders(order_no, elder_id, created_by, order_source, meal_date, meal_type, delivery_type,
			address, strict_mode, box_return_method, boxes_issued, total_amount, subsidy_amount, payable_amount, status, batch_id)
			VALUES('TMP-'||gen_random_uuid(),$1,$2,'community',$3::date,$4,$5,'演示地址',FALSE,'next_delivery',2,$6,$7,$8,$9,$10) RETURNING id`,
			elderID, uid["community01"], date, mealType, delType, total, subsidy, payable, status, bid).Scan(&oid)
		if err != nil {
			return 0, err
		}
		db.Exec(`UPDATE orders SET order_no=$1 WHERE id=$2`, orderNo(date, oid), oid)
		for _, it := range items {
			db.Exec(`INSERT INTO order_items(order_id, dish_id, dish_name, price, qty) VALUES($1,$2,$3,$4,$5)`,
				oid, dishID[it.dish], it.dish, dishPrice[it.dish], it.qty)
		}
		_ = cost
		return oid, nil
	}
	addDeliveryRaw := func(orderID int, status, pickup, delivered string) {
		var pu, de interface{}
		if pickup != "" {
			pu = pickup
		}
		if delivered != "" {
			de = delivered
		}
		db.Exec(`INSERT INTO deliveries(order_id, deliverer_id, deliverer_type, thermal_box_no, status, pickup_time, delivered_time)
			VALUES($1,$2,'rider','WBX-DEMO',$3,$4,$5) ON CONFLICT (order_id) DO NOTHING`, orderID, uid["rider01"], status, pu, de)
	}
	addBoxReturned := func(orderID, elderID int) {
		db.Exec(`INSERT INTO box_records(order_id, elder_id, boxes_issued, boxes_returned, return_method, status, returned_at)
			VALUES($1,$2,2,2,'next_delivery','returned',now()) ON CONFLICT (order_id) DO NOTHING`, orderID, elderID)
	}
	// 写入状态变更主单 + 四段明细，再回写汇总
	seedChange := func(elderID int, ctype, effDate, hospital, newAddr string, inCov, retained bool,
		segOrders map[string][]int, handling map[string]string, kitchenSegs map[string]bool) int {
		var cid int
		db.QueryRow(`INSERT INTO elder_status_changes(elder_id, change_type, status, effective_date, hospital,
			family_contact_name, family_contact_phone, subsidy_retained, new_address, in_coverage,
			operator_id, family_confirmed_by, family_confirmed_at, note)
			VALUES($1,$2,'active',$3::date,$4,'家属张强','13800001111',$5,$6,$7,$8,$9,now(),'种子演示数据') RETURNING id`,
			elderID, ctype, effDate, hospital, retained, newAddr, inCov, uid["community01"],
			map[string]string{"hospitalization": "家属电话确认", "move_out": "家属现场确认", "death": "家属现场确认"}[ctype]).Scan(&cid)
		for seg, oids := range segOrders {
			for _, oid := range oids {
				var no, md, snap string
				var qty int
				var total, sub, pay, mcost float64
				var bID int
				db.QueryRow(`SELECT o.order_no, o.meal_date::text, o.status, COALESCE(o.batch_id,0),
					COALESCE((SELECT SUM(qi.qty) FROM order_items qi WHERE qi.order_id=o.id),1),
					o.total_amount, o.subsidy_amount, o.payable_amount,
					COALESCE((SELECT SUM(qi.qty*COALESCE(NULLIF(d.unit_cost,0),qi.price*0.5)) FROM order_items qi LEFT JOIN dishes d ON d.id=qi.dish_id WHERE qi.order_id=o.id),0)
					FROM orders o WHERE o.id=$1`, oid).
					Scan(&no, &md, &snap, &bID, &qty, &total, &sub, &pay, &mcost)
				var picked bool
				db.QueryRow(`SELECT EXISTS(SELECT 1 FROM deliveries WHERE order_id=$1 AND pickup_time IS NOT NULL)`, oid).Scan(&picked)
				bno := ""
				if bID > 0 {
					db.QueryRow(`SELECT batch_no FROM kitchen_batches WHERE id=$1`, bID).Scan(&bno)
				}
				db.Exec(`INSERT INTO status_change_items(change_id, order_id, segment, order_no, meal_date,
					order_status_snapshot, batch_id, batch_no, picked, qty, total_amount, subsidy_amount, payable_amount,
					material_cost, handling, transferred, kitchen_responsible)
					VALUES($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,FALSE,$16)`,
					cid, oid, seg, no, md[:10], snap, bID, bno, picked, qty, total, sub, pay, mcost,
					handling[seg], kitchenSegs[seg])
			}
		}
		// 回写四段汇总
		db.Exec(`UPDATE elder_status_changes SET
			seg_signed=(SELECT COUNT(*) FROM status_change_items WHERE change_id=$1 AND segment='signed'),
			seg_in_transit=(SELECT COUNT(*) FROM status_change_items WHERE change_id=$1 AND segment='in_transit'),
			seg_prepared=(SELECT COUNT(*) FROM status_change_items WHERE change_id=$1 AND segment='prepared_undelivered'),
			seg_unprepared=(SELECT COUNT(*) FROM status_change_items WHERE change_id=$1 AND segment='unprepared'),
			signed_subsidy_total=COALESCE((SELECT SUM(subsidy_amount) FROM status_change_items WHERE change_id=$1 AND segment='signed'),0),
			transit_subsidy_total=COALESCE((SELECT SUM(subsidy_amount) FROM status_change_items WHERE change_id=$1 AND segment='in_transit'),0),
			prepared_refund_total=COALESCE((SELECT SUM(payable_amount) FROM status_change_items WHERE change_id=$1 AND segment='prepared_undelivered'),0),
			prepared_cost_total=COALESCE((SELECT SUM(material_cost) FROM status_change_items WHERE change_id=$1 AND segment='prepared_undelivered'),0),
			unprepared_cancel_subsidy=COALESCE((SELECT SUM(subsidy_amount) FROM status_change_items WHERE change_id=$1 AND segment='unprepared'),0),
			kitchen_loss_total=COALESCE((SELECT SUM(material_cost) FROM status_change_items WHERE change_id=$1 AND kitchen_responsible),0)
			WHERE id=$1`, cid)
		return cid
	}

	// —— 演示一：住院暂停（在服暂停，等待出院恢复，四段齐全）——
	hElder, err := insertDemoElder("孙桂英", "110101194208080061", "女", "1942-08-08",
		"幸福里小区 8 栋 1 单元 302", "full", 12, 30, true, "paused_hospital")
	if err != nil {
		return err
	}
	bDinner := ensureBatch(day(0), "dinner", "released")
	bTodayLunch := ensureBatch(day(0), "lunch", "released")
	bTomorrowLunch := ensureBatch(day(1), "lunch", "preparing")
	hSigned, _ := insertDemoOrder(hElder, day(-4), "lunch", "signed", "home", 0,
		[]demoItemSpec{{"软烂红烧肉", 1}, {"软米饭", 1}})
	addDeliveryRaw(hSigned, "delivered", day(-4)+" 11:05", day(-4)+" 11:35")
	addBoxReturned(hSigned, hElder)
	hTransit, _ := insertDemoOrder(hElder, day(0), "lunch", "paused", "home", bTodayLunch,
		[]demoItemSpec{{"清蒸鲈鱼", 1}, {"低盐时蔬", 1}, {"软米饭", 1}})
	db.Exec(`UPDATE orders SET subsidy_frozen=TRUE WHERE id=$1`, hTransit)
	addDeliveryRaw(hTransit, "picked", day(0)+" 11:08", "")
	hPrepared, _ := insertDemoOrder(hElder, day(1), "lunch", "paused", "home", bTomorrowLunch,
		[]demoItemSpec{{"番茄炒蛋", 1}, {"无糖南瓜粥", 1}, {"软米饭", 1}})
	db.Exec(`UPDATE orders SET subsidy_frozen=TRUE WHERE id=$1`, hPrepared)
	hUnprep, _ := insertDemoOrder(hElder, day(3), "lunch", "cancelled", "home", 0,
		[]demoItemSpec{{"软烂红烧肉", 1}, {"软米饭", 1}})
	seedChange(hElder, "hospitalization", day(0), "市第一人民医院（心内科）", "", true, true,
		map[string][]int{"signed": {hSigned}, "in_transit": {hTransit}, "prepared_undelivered": {hPrepared}, "unprepared": {hUnprep}},
		map[string]string{
			"signed":               "已签收记录保留，补贴正常纳入财政结算",
			"in_transit":           "骑手已取餐，停止配送并携回；补贴冻结暂不核销，出院后按原异常餐单结清",
			"prepared_undelivered": "已备餐未出餐，暂停保留，登记批次/数量/成本，可转配其他老人",
			"unprepared":           "未备餐取消，释放补贴额度，不进财政结算",
		}, map[string]bool{})
	db.Exec(`UPDATE elder_status_changes SET expected_discharge_date=$2::date WHERE elder_id=$1 AND change_type='hospitalization'`, hElder, day(14))

	// —— 演示二：搬离服务区域（终止配送、退餐、补贴清算、回收餐盒）——
	mElder, err := insertDemoElder("吴桂兰", "110101193911200072", "女", "1939-11-20",
		"临江市滨湖花园 2 栋 601（已搬出原服务区）", "partial", 8, 20, false, "moved_out")
	if err != nil {
		return err
	}
	mSigned, _ := insertDemoOrder(mElder, day(-2), "lunch", "signed", "home", 0,
		[]demoItemSpec{{"清蒸鲈鱼", 1}, {"软米饭", 1}})
	addDeliveryRaw(mSigned, "delivered", day(-2)+" 11:06", day(-2)+" 11:40")
	addBoxReturned(mSigned, mElder)
	mPrepared, _ := insertDemoOrder(mElder, day(0), "dinner", "refunded", "home", bDinner,
		[]demoItemSpec{{"番茄炒蛋", 1}, {"软米饭", 1}})
	db.Exec(`UPDATE orders SET refund_amount=payable_amount, cancel_reason='搬离服务区，已备餐未送出按退餐处理' WHERE id=$1`, mPrepared)
	mUnprep, _ := insertDemoOrder(mElder, day(2), "lunch", "cancelled", "home", 0,
		[]demoItemSpec{{"软烂红烧肉", 1}, {"软米饭", 1}})
	mcid := seedChange(mElder, "move_out", day(0), "", "临江市滨湖花园 2 栋 601", false, true,
		map[string][]int{"signed": {mSigned}, "prepared_undelivered": {mPrepared}, "unprepared": {mUnprep}},
		map[string]string{
			"signed":               "已签收记录保留，按已用次数清算补贴",
			"prepared_undelivered": "已备餐未送出按退餐处理，自付退回；登记批次/数量/成本，可转配",
			"unprepared":           "未备餐取消，未用补贴额度停止核销",
		}, map[string]bool{})
	db.Exec(`UPDATE elder_status_changes SET boxes_to_recover=2, boxes_recovered=2 WHERE id=$1`, mcid)

	// —— 演示三：老人去世（停配停核销，保留签收，未出餐不结算，已出餐未送达记食材损耗/厨房责任）——
	dElder, err := insertDemoElder("刘德海", "110101193605030083", "男", "1936-05-03",
		"康乐社区 9 号楼 2 单元 403", "full", 12, 30, false, "deceased")
	if err != nil {
		return err
	}
	bBreak := ensureBatch(day(0), "breakfast", "released")
	dSigned, _ := insertDemoOrder(dElder, day(-3), "lunch", "signed", "home", 0,
		[]demoItemSpec{{"软烂红烧肉", 1}, {"低盐时蔬", 1}, {"软米饭", 1}})
	addDeliveryRaw(dSigned, "delivered", day(-3)+" 11:05", day(-3)+" 11:38")
	addBoxReturned(dSigned, dElder)
	dTransit, _ := insertDemoOrder(dElder, day(0), "breakfast", "exception", "home", bBreak,
		[]demoItemSpec{{"无糖南瓜粥", 1}, {"软米饭", 1}})
	db.Exec(`UPDATE orders SET subsidy_frozen=TRUE, cancel_reason='老人去世，已出餐未送达，记食材损耗与厨房责任' WHERE id=$1`, dTransit)
	addDeliveryRaw(dTransit, "failed", day(0)+" 07:20", "")
	dPrepared, _ := insertDemoOrder(dElder, day(0), "lunch", "refunded", "home", bTodayLunch,
		[]demoItemSpec{{"清蒸鲈鱼", 1}, {"软米饭", 1}})
	db.Exec(`UPDATE orders SET refund_amount=payable_amount, cancel_reason='老人去世，已出餐未送达，记食材损耗与厨房责任' WHERE id=$1`, dPrepared)
	dUnprep, _ := insertDemoOrder(dElder, day(2), "lunch", "cancelled", "home", 0,
		[]demoItemSpec{{"番茄炒蛋", 1}, {"软米饭", 1}})
	seedChange(dElder, "death", day(0), "", "", true, false,
		map[string][]int{"signed": {dSigned}, "in_transit": {dTransit}, "prepared_undelivered": {dPrepared}, "unprepared": {dUnprep}},
		map[string]string{
			"signed":               "已签收记录保留，补贴按实际签收正常结算",
			"in_transit":           "骑手已取餐未送达：停止配送，记食材损耗与厨房责任，餐食可转配，保温箱交回",
			"prepared_undelivered": "已出餐未送达：不进财政结算，记食材损耗与厨房责任，可转配其他老人",
			"unprepared":           "未备餐取消，停止配送与补贴核销",
		}, map[string]bool{"in_transit": true, "prepared_undelivered": true})

	// ====================================================================
	// 志愿者帮送与家属代订演示
	// ====================================================================
	// 志愿者资质：陈志愿已核验、已培训、正常；新增一名未培训志愿者用于拦截演示
	if _, err := db.Exec(`UPDATE users SET vol_org='康乐社区助老志愿队', vol_id_verified=TRUE,
		vol_trained=TRUE, vol_training_date=CURRENT_DATE-30, vol_eligible=TRUE WHERE username='volunteer01'`); err != nil {
		return err
	}
	db.Exec(`INSERT INTO volunteer_health(volunteer_id, check_date, health_status, temperature, note)
		VALUES($1,CURRENT_DATE,'healthy',36.4,'健康打卡') ON CONFLICT DO NOTHING`, uid["volunteer01"])
	var vol2 int
	db.QueryRow(`INSERT INTO users(username,password_hash,name,phone,role,vol_org,vol_id_verified,vol_trained,vol_eligible)
		VALUES('volunteer02',$1,'周志愿','13800005555','volunteer','外部公益组织',TRUE,FALSE,FALSE) RETURNING id`,
		hashPassword("volunteer123")).Scan(&vol2)

	// 授权陈福生可采用邻里见证/拍照留证（非严格对象）
	db.Exec(`UPDATE elders SET proxy_sign_authorized=TRUE WHERE name='陈福生'`)

	// 专用批次（昨日，已出餐）
	vBatchDate := day(-1)
	var vBatch int
	db.QueryRow(`INSERT INTO kitchen_batches(batch_date, meal_type, batch_no, status, released_at, operator_id)
		VALUES($1::date,'lunch',$2,'released',now(),$3) RETURNING id`,
		vBatchDate, "B"+replaceDash(vBatchDate)+"-lunch-vol", uid["kitchen01"]).Scan(&vBatch)

	insertVolOrder := func(date, status, elderName string, items []itemSpec) int {
		oid, err := insertOrder(elderName, date, "lunch", "volunteer", status, "community", "community01", items, false, "")
		if err != nil {
			return 0
		}
		db.Exec(`UPDATE orders SET batch_id=$2 WHERE id=$1`, oid, vBatch)
		return oid
	}
	dSelf, dFam, dNb, dPhoto, dSpill := day(-6), day(-5), day(-4), day(0), day(-3)
	// ① 老人本人签收（有效）
	oidSelf := insertVolOrder(dSelf, "signed", "陈福生", []itemSpec{{"番茄炒蛋", 1, ""}, {"软米饭", 1, ""}})
	db.Exec(`UPDATE orders SET sign_basis='elder', sign_effectiveness='valid' WHERE id=$1`, oidSelf)
	db.Exec(`INSERT INTO deliveries(order_id, deliverer_id, deliverer_type, thermal_box_no, route_info, status,
		pickup_time, delivered_time, sign_type, signed_by_name, knock_confirmed, vol_org, relation_to_elder,
		informed_consent, consent_by, dispatch_checked, eta, effectiveness, effectiveness_reason, community_verified_by, community_verified_at)
		VALUES($1,$2,'volunteer','WBX-V1','社区食堂→幸福里','delivered',$3::timestamptz,$4::timestamptz,'elder','陈福生',TRUE,
		'康乐社区助老志愿队','邻里',TRUE,'陈福生',TRUE,$3::timestamptz,'valid','老人本人签收，签收有效',$5,now())`,
		oidSelf, uid["volunteer01"], dSelf+" 11:05:00", dSelf+" 11:35:00", uid["community01"])
	db.Exec(`INSERT INTO elder_confirmations(order_id, elder_id, confirmer_role, confirmer_name, method, confirms_received, taste_feedback, community_id)
		VALUES($1,$2,'elder','陈福生','visit',TRUE,'味道合适，分量足',$3)`, oidSelf, eid["陈福生"], uid["community01"])

	// ② 家属代签（有效）
	oidFam := insertVolOrder(dFam, "signed", "陈福生", []itemSpec{{"清蒸鲈鱼", 1, ""}, {"软米饭", 1, ""}})
	db.Exec(`UPDATE orders SET sign_basis='family', sign_effectiveness='valid' WHERE id=$1`, oidFam)
	db.Exec(`INSERT INTO deliveries(order_id, deliverer_id, deliverer_type, thermal_box_no, route_info, status,
		pickup_time, delivered_time, sign_type, signed_by_name, knock_confirmed, vol_org, relation_to_elder,
		informed_consent, consent_by, dispatch_checked, eta, effectiveness, effectiveness_reason)
		VALUES($1,$2,'volunteer','WBX-V1','社区食堂→幸福里','delivered',$3::timestamptz,$4::timestamptz,'family','陈邻居(侄子)',TRUE,
		'康乐社区助老志愿队','邻里',TRUE,'陈邻居',TRUE,$3::timestamptz,'valid','家属代签，签收有效')`,
		oidFam, uid["volunteer01"], dFam+" 11:05:00", dFam+" 11:40:00")

	// ③ 邻里见证，已授权+知情，社区已回访核实（有效，依据 community）
	oidNb := insertVolOrder(dNb, "signed", "陈福生", []itemSpec{{"番茄炒蛋", 1, ""}, {"无糖南瓜粥", 1, ""}})
	db.Exec(`UPDATE orders SET sign_basis='community', sign_effectiveness='valid' WHERE id=$1`, oidNb)
	db.Exec(`INSERT INTO deliveries(order_id, deliverer_id, deliverer_type, thermal_box_no, route_info, status,
		pickup_time, delivered_time, sign_type, signed_by_name, knock_confirmed, witness_name, witness_phone,
		vol_org, relation_to_elder, informed_consent, consent_by, dispatch_checked, eta,
		effectiveness, effectiveness_reason, community_verified_by, community_verified_at)
		VALUES($1,$2,'volunteer','WBX-V1','社区食堂→幸福里','delivered',$3::timestamptz,$4::timestamptz,'neighbor','赵阿姨',TRUE,'赵阿姨','13800006677',
		'康乐社区助老志愿队','同楼栋住户',TRUE,'赵阿姨',TRUE,$3::timestamptz,
		'valid','社区授权且知情的邻里见证，回访老人确认收到',$5,now())`,
		oidNb, uid["volunteer01"], dNb+" 11:05:00", dNb+" 11:42:00", uid["community01"])
	db.Exec(`INSERT INTO elder_confirmations(order_id, elder_id, confirmer_role, confirmer_name, method, confirms_received, taste_feedback, community_id)
		VALUES($1,$2,'elder','陈福生','phone',TRUE,'收到了，粥很好',$3)`, oidNb, eid["陈福生"], uid["community01"])

	// ④ 拍照留证，待社区核实（verify_pending，不核销）+ 待核实异常
	oidPhoto := insertVolOrder(dPhoto, "verify_pending", "陈福生", []itemSpec{{"软烂红烧肉", 1, ""}, {"软米饭", 1, ""}})
	db.Exec(`UPDATE orders SET sign_basis='photo', sign_effectiveness='pending' WHERE id=$1`, oidPhoto)
	db.Exec(`INSERT INTO deliveries(order_id, deliverer_id, deliverer_type, thermal_box_no, route_info, status,
		pickup_time, delivered_time, sign_type, signed_by_name, sign_photo_url, knock_confirmed,
		vol_org, relation_to_elder, informed_consent, consent_by, dispatch_checked, eta, effectiveness, effectiveness_reason)
		VALUES($1,$2,'volunteer','WBX-V1','社区食堂→幸福里','delivered',$3::timestamptz,now(),'photo','志愿者拍照留证','/uploads/demo-volunteer.jpg',FALSE,
		'康乐社区助老志愿队','同楼栋住户',TRUE,'电话联系老人同意',TRUE,$3::timestamptz,
		'pending','志愿者拍照留证不能仅凭自述核销，须社区核实老人实际收到')`,
		oidPhoto, uid["volunteer01"], dPhoto+" 11:06:00")
	var oidPhotoNo string
	db.QueryRow(`SELECT order_no FROM orders WHERE id=$1`, oidPhoto).Scan(&oidPhotoNo)
	db.Exec(`INSERT INTO anomalies(order_id, elder_id, type, priority, description, reported_by)
		VALUES($1,$2,'volunteer_delivery','normal',$3,$4)`,
		oidPhoto, eid["陈福生"],
		"志愿者帮送餐单 "+oidPhotoNo+" 采用拍照留证，效力待社区核实：需回访老人本人确认实际收到，核实前不纳入补贴核销。",
		uid["community01"])

	// ⑤ 志愿者帮送异常（餐品洒漏，待社区核实，责任未划分）
	oidSpill := insertVolOrder(dSpill, "exception", "陈福生", []itemSpec{{"低盐时蔬", 1, ""}, {"软米饭", 1, ""}})
	var spillNo string
	db.QueryRow(`SELECT order_no FROM orders WHERE id=$1`, oidSpill).Scan(&spillNo)
	db.Exec(`INSERT INTO deliveries(order_id, deliverer_id, deliverer_type, thermal_box_no, route_info, status,
		pickup_time, anomaly_note, vol_org, relation_to_elder, informed_consent, consent_by, dispatch_checked, eta)
		VALUES($1,$2,'volunteer','WBX-V1','社区食堂→幸福里','failed',$3::timestamptz,'志愿者帮送异常·餐品洒漏：途中颠簸菜汤洒出','康乐社区助老志愿队','邻里',TRUE,'陈福生',TRUE,$3::timestamptz)`,
		oidSpill, uid["volunteer01"], dSpill+" 11:06:00")
	db.Exec(`INSERT INTO anomalies(order_id, elder_id, type, priority, description, reported_by, food_safety, responsible_user_id)
		VALUES($1,$2,'volunteer_delivery','normal',$3,$4,FALSE,$5)`,
		oidSpill, eid["陈福生"],
		"志愿者帮送异常【餐品洒漏】餐单 "+spillNo+"（老人「陈福生」）途中菜汤洒出，请社区核实路线/取餐时间/照片后决定补送或退餐并划分责任。",
		uid["volunteer01"], uid["volunteer01"])

	// 家属代订授权：把张秀英今日 confirmed 单补上代订授权三字段（其家属 family01 张强代订）
	db.Exec(`UPDATE orders SET proxy_relation='子女', proxy_auth_method='长期绑定授权', proxy_contact_phone='13800001111',
		proxy_name='张强' WHERE elder_id=$1 AND order_source='family' AND proxy_name=''`, eid["张秀英"])

	// ---------- 通知 ----------
	db.Exec(`INSERT INTO notifications(role, title, content) VALUES
		('kitchen', '今日待备餐', '今日有 4 份已确认餐单等待创建批次备餐'),
		('community', '餐盒未回收提醒', '张秀英 2 个餐盒未回收，已生成异常工单'),
		('community', '住院暂停四段清算', '孙桂英住院暂停，已按四段拆分餐单，请跟进已备餐转配'),
		('community', '帮送签收待核实', '陈福生今日拍照留证签收待回访老人本人，核实前不核销'),
		('volunteer', '当日健康打卡', '开始帮送前请完成发车前核验（身份/培训/健康/资格）'),
		('finance', '本月核销提醒', '本月已有多笔签收餐单，月底请生成核销单；每笔可见真实签收依据'),
		('finance', '去世清算', '刘德海去世已清算：保留已签收，未出餐不结算，已出餐未送达记食材损耗')`)

	log.Printf("种子数据完成：用户 %d，老人 %d，菜品 %d，上月归档核销 #%d", len(users)+1, len(elders), len(dishes), recID)
	return nil
}
