package main

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// 老人状态变更（住院/转院/搬离/去世）：餐单按生效日期拆成
// 已签收 / 在途 / 已备餐未出餐 / 未备餐 四段，分别清算并同步家属、社区、厨房、财政、骑手。

func statusChangeName(t string) string {
	switch t {
	case "hospitalization":
		return "住院暂停"
	case "transfer":
		return "转院"
	case "move_out":
		return "搬离"
	case "death":
		return "去世"
	case "resume":
		return "出院恢复"
	}
	return t
}

func segmentName(seg string) string {
	switch seg {
	case "signed":
		return "已签收"
	case "in_transit":
		return "在途（骑手已取餐）"
	case "prepared_undelivered":
		return "已备餐未出餐/已出餐未送达"
	case "unprepared":
		return "未备餐"
	}
	return seg
}

// 覆盖范围判定：覆盖片区/小区关键词（演示环境以地址关键词为准）
var coveredKeywords = []string{"幸福里", "康乐", "朝阳社区", "社区食堂", "辖区"}

func isInCoverage(addr string) bool {
	for _, k := range coveredKeywords {
		if strings.Contains(addr, k) {
			return true
		}
	}
	return false
}

type segOrder struct {
	id              int
	orderNo         string
	mealDate        string
	status          string
	batchID         int
	batchNo         string
	picked          bool
	qty             int
	total, sub, pay float64
	cost            float64
	segment         string
}

// 按生效日期把老人在服餐单拆成四段（不含已取消/已退餐/已核销暂停单）
func loadSegmentOrders(tx *sql.Tx, elderID int, effDate string) ([]segOrder, error) {
	rows, err := tx.Query(`SELECT o.id, o.order_no, o.meal_date::text, o.status, COALESCE(o.batch_id,0),
		COALESCE(kb.batch_no,''),
		EXISTS(SELECT 1 FROM deliveries d WHERE d.order_id=o.id AND d.status='picked'),
		COALESCE((SELECT SUM(oi.qty) FROM order_items oi WHERE oi.order_id=o.id),0),
		o.total_amount, o.subsidy_amount, o.payable_amount
		FROM orders o LEFT JOIN kitchen_batches kb ON kb.id=o.batch_id
		WHERE o.elder_id=$1 AND o.status NOT IN ('cancelled','refunded','paused')
		ORDER BY o.meal_date, o.id`, elderID)
	if err != nil {
		return nil, err
	}
	out := []segOrder{}
	for rows.Next() {
		var o segOrder
		if err := rows.Scan(&o.id, &o.orderNo, &o.mealDate, &o.status, &o.batchID, &o.batchNo,
			&o.picked, &o.qty, &o.total, &o.sub, &o.pay); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// 主结果集关闭后再逐单查成本（同一事务连接不可嵌套结果集）
	for i := range out {
		if err := tx.QueryRow(`SELECT COALESCE(SUM(oi.qty * COALESCE(NULLIF(d.unit_cost,0), oi.price*0.5)),0)
			FROM order_items oi LEFT JOIN dishes d ON d.id=oi.dish_id WHERE oi.order_id=$1`, out[i].id).Scan(&out[i].cost); err != nil {
			return nil, err
		}
		switch {
		case out[i].status == "signed" || out[i].status == "completed" || out[i].status == "settled":
			out[i].segment = "signed"
		case out[i].picked || out[i].status == "delivering":
			out[i].segment = "in_transit"
		case out[i].status == "preparing" || out[i].status == "ready":
			out[i].segment = "prepared_undelivered"
		default:
			out[i].segment = "unprepared" // confirmed / pending / exception
		}
	}
	return out, nil
}

func countOpenAnomalies(tx *sql.Tx, elderID int) (int, error) {
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM anomalies WHERE elder_id=$1 AND status<>'resolved'`, elderID).Scan(&n)
	return n, err
}

func segMap(orders []segOrder) gin.H {
	groups := map[string][]segOrder{"signed": {}, "in_transit": {}, "prepared_undelivered": {}, "unprepared": {}}
	for _, o := range orders {
		groups[o.segment] = append(groups[o.segment], o)
	}
	toList := func(os []segOrder) []gin.H {
		out := []gin.H{}
		for _, o := range os {
			out = append(out, gin.H{
				"order_id": o.id, "order_no": o.orderNo, "meal_date": o.mealDate[:10], "status": o.status,
				"batch_id": o.batchID, "batch_no": o.batchNo, "picked": o.picked, "qty": o.qty,
				"total_amount": o.total, "subsidy_amount": o.sub, "payable_amount": o.pay, "material_cost": o.cost,
			})
		}
		return out
	}
	return gin.H{
		"signed":               toList(groups["signed"]),
		"in_transit":           toList(groups["in_transit"]),
		"prepared_undelivered": toList(groups["prepared_undelivered"]),
		"unprepared":           toList(groups["unprepared"]),
		"counts": gin.H{
			"signed": len(groups["signed"]), "in_transit": len(groups["in_transit"]),
			"prepared_undelivered": len(groups["prepared_undelivered"]), "unprepared": len(groups["unprepared"]),
		},
	}
}

// 预检查：未办结异常（补送/改约/退餐/回访）须先按原异常餐单结清；并给出四段预览
func (s *Server) previewStatusChange(c *gin.Context) {
	elderID := c.Param("id")
	changeType := c.DefaultQuery("type", "hospitalization")
	effDate := c.Query("effective_date")
	if effDate == "" {
		effDate = today()
	}
	newAddr := c.Query("new_address")

	var elderName string
	var active bool
	err := s.db.QueryRow(`SELECT name, active FROM elders WHERE id=$1`, elderID).Scan(&elderName, &active)
	if err != nil {
		fail(c, http.StatusNotFound, "老人档案不存在")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()

	openN, err := countOpenAnomalies(tx, atoi(elderID))
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询异常工单失败")
		return
	}
	openList := []gin.H{}
	arows, _ := tx.Query(`SELECT a.id, COALESCE(o.order_no,''), a.type, a.description, a.status
		FROM anomalies a LEFT JOIN orders o ON o.id=a.order_id
		WHERE a.elder_id=$1 AND a.status<>'resolved' ORDER BY a.id`, elderID)
	if arows != nil {
		for arows.Next() {
			var id int
			var no, typ, desc, st string
			arows.Scan(&id, &no, &typ, &desc, &st)
			openList = append(openList, gin.H{"id": id, "order_no": no, "type": typ,
				"type_name": anomalyTypeName(typ), "description": desc, "status": st})
		}
		arows.Close()
	}
	orders, err := loadSegmentOrders(tx, atoi(elderID), effDate)
	if err != nil {
		fail(c, http.StatusInternalServerError, "拆分餐单失败")
		return
	}
	segments := segMap(orders)

	// 未回收餐盒/保温箱（搬离/去世需回收）
	var outstandingBoxes int
	tx.QueryRow(`SELECT COALESCE(SUM(boxes_issued-boxes_returned),0) FROM box_records WHERE elder_id=$1 AND status<>'returned'`,
		elderID).Scan(&outstandingBoxes)
	thermalBoxes := 0
	for _, o := range orders {
		if o.picked {
			thermalBoxes++
		}
	}

	resp := gin.H{
		"elder_id": atoi(elderID), "elder_name": elderName, "active": active,
		"change_type": changeType, "effective_date": effDate,
		"blocked": openN > 0, "open_anomaly_count": openN, "open_anomalies": openList,
		"segments": segments,
		"boxes_to_recover": outstandingBoxes, "thermal_boxes_to_recover": thermalBoxes,
	}
	if changeType == "move_out" {
		resp["new_address"] = newAddr
		resp["in_coverage"] = isInCoverage(newAddr)
	}
	ok(c, resp)
}

// 校验家属确认（状态变更必须家属确认）
func requireFamilyConfirmed(who string) bool {
	return strings.TrimSpace(who) != ""
}

func (s *Server) createStatusChange(c *gin.Context) {
	var req struct {
		ChangeType          string `json:"change_type"`
		EffectiveDate       string `json:"effective_date" binding:"required"`
		Hospital            string `json:"hospital"`
		ExpectedDischarge   string `json:"expected_discharge_date"`
		TransferHospital    string `json:"transfer_hospital"`
		FamilyContactName   string `json:"family_contact_name"`
		FamilyContactPhone  string `json:"family_contact_phone"`
		SubsidyRetained     *bool  `json:"subsidy_retained"`
		NewAddress          string `json:"new_address"`
		FamilyConfirmedBy   string `json:"family_confirmed_by"`
		Note                string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.EffectiveDate == "" {
		fail(c, http.StatusBadRequest, "请选择生效日期")
		return
	}
	if req.ChangeType == "" {
		req.ChangeType = "hospitalization"
	}
	if req.ChangeType != "hospitalization" && req.ChangeType != "transfer" &&
		req.ChangeType != "move_out" && req.ChangeType != "death" {
		fail(c, http.StatusBadRequest, "变更类型不合法")
		return
	}
	if !requireFamilyConfirmed(req.FamilyConfirmedBy) {
		fail(c, http.StatusBadRequest, "状态变更须登记家属确认人")
		return
	}
	subsidyRetained := true
	if req.SubsidyRetained != nil {
		subsidyRetained = *req.SubsidyRetained
	}
	if (req.ChangeType == "hospitalization" || req.ChangeType == "transfer") &&
		(strings.TrimSpace(req.Hospital) == "" || strings.TrimSpace(req.FamilyContactName) == "") {
		fail(c, http.StatusBadRequest, "住院/转院须登记医院与家属联系人")
		return
	}
	if req.ChangeType == "move_out" && strings.TrimSpace(req.NewAddress) == "" {
		fail(c, http.StatusBadRequest, "搬离须填写新地址")
		return
	}

	elderID := atoi(c.Param("id"))
	uid := c.GetInt("uid")
	operator := c.GetString("name")

	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()

	var elderName, curStatus, address, familyPhone string
	var familyID sql.NullInt64
	err = tx.QueryRow(`SELECT name, service_status, address, COALESCE(family_user_id,0), emergency_contact_phone
		FROM elders WHERE id=$1 FOR UPDATE`, elderID).
		Scan(&elderName, &curStatus, &address, &familyID, &familyPhone)
	if err != nil {
		fail(c, http.StatusNotFound, "老人档案不存在")
		return
	}
	if curStatus != "active" {
		fail(c, http.StatusBadRequest, "老人当前不处于在服状态，请先结清上一次状态变更（出院恢复/清算）后再办理")
		return
	}
	// 未办结异常：先按原异常餐单结清，再进入清算
	openN, err := countOpenAnomalies(tx, elderID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "校验异常工单失败")
		return
	}
	if openN > 0 {
		fail(c, http.StatusBadRequest, "还有补送/改约/退餐/回访未办结，请先按原异常餐单结清后再办理状态变更")
		return
	}

	orders, err := loadSegmentOrders(tx, elderID, req.EffectiveDate)
	if err != nil {
		fail(c, http.StatusInternalServerError, "拆分餐单失败")
		return
	}

	inCoverage := true
	if req.ChangeType == "move_out" {
		inCoverage = isInCoverage(req.NewAddress)
	}

	// 汇总指标
	var nSigned, nTransit, nPrepared, nUnprepared int
	var signedSub, transitSub, preparedRefund, preparedCost, unpreparedSub, kitchenLoss float64
	for _, o := range orders {
		switch o.segment {
		case "signed":
			nSigned++
			signedSub += o.sub
		case "in_transit":
			nTransit++
			transitSub += o.sub
		case "prepared_undelivered":
			nPrepared++
			preparedCost += o.cost
			if (req.ChangeType == "move_out" && !inCoverage) || req.ChangeType == "death" {
				preparedRefund += o.pay
			}
		case "unprepared":
			nUnprepared++
			unpreparedSub += o.sub
		}
	}

	hospital := req.Hospital
	if req.ChangeType == "transfer" {
		if req.TransferHospital != "" {
			hospital = req.TransferHospital
		}
	}
	var changeID int
	err = tx.QueryRow(`INSERT INTO elder_status_changes(elder_id, change_type, effective_date, hospital,
		expected_discharge_date, family_contact_name, family_contact_phone, subsidy_retained, transfer_hospital,
		new_address, in_coverage, death_date, seg_signed, seg_in_transit, seg_prepared, seg_unprepared,
		signed_subsidy_total, transit_subsidy_total, prepared_refund_total, prepared_cost_total,
		unprepared_cancel_subsidy, kitchen_loss_total, operator_id, family_confirmed_by, family_confirmed_at, note)
		VALUES($1,$2,$3::date,$4,NULLIF($5,'')::date,$6,$7,$8,$9,$10,$11,
		CASE WHEN $12='death' THEN $3::date ELSE NULL END,
		$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,now(),$25) RETURNING id`,
		elderID, req.ChangeType, req.EffectiveDate, hospital, req.ExpectedDischarge,
		req.FamilyContactName, req.FamilyContactPhone, subsidyRetained, req.TransferHospital,
		req.NewAddress, inCoverage, req.ChangeType,
		nSigned, nTransit, nPrepared, nUnprepared,
		signedSub, transitSub, preparedRefund, preparedCost, unpreparedSub, kitchenLoss,
		uid, req.FamilyConfirmedBy, req.Note).Scan(&changeID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "登记状态变更失败: "+err.Error())
		return
	}

	// 逐单写入四段明细并按事件类型处置
	terminate := req.ChangeType == "move_out" && !inCoverage || req.ChangeType == "death"
	continueService := req.ChangeType == "move_out" && inCoverage // 新地址仍在覆盖范围：继续服务，仅更新送餐地址
	pause := req.ChangeType == "hospitalization" || req.ChangeType == "transfer"
	for _, o := range orders {
		var handling string
		transferred := false
		kitchenResp := false
		batchID := interface{}(nil)
		if o.batchID > 0 {
			batchID = o.batchID
		}
		// 仍在覆盖范围内搬离：四段仅作拆分留痕，餐单继续履约，不暂停/不退餐/不取消
		if continueService {
			tx.Exec(`UPDATE orders SET address=$2, updated_at=now() WHERE id=$1 AND status IN ('confirmed','pending','preparing','ready','delivering')`, o.id, req.NewAddress)
			handling = map[string]string{
				"signed":               "搬离前已签收，记录保留，正常财政结算",
				"in_transit":           "老人仍在覆盖范围，在途餐单按新地址继续配送签收，不冻结补贴",
				"prepared_undelivered": "继续服务，送餐地址已更新为新地址，厨房正常出餐",
				"unprepared":           "继续服务，送餐地址已更新，厨房正常备餐",
			}[o.segment]
			addOrderEvent(tx, o.id, uid, operator, "搬离（仍在覆盖区）·四段留痕", "归入【"+segmentName(o.segment)+"】段："+handling)
			if _, err := tx.Exec(`INSERT INTO status_change_items(change_id, order_id, segment, order_no, meal_date,
				order_status_snapshot, batch_id, batch_no, picked, qty, total_amount, subsidy_amount, payable_amount,
				material_cost, handling, transferred, kitchen_responsible)
				VALUES($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
				changeID, o.id, o.segment, o.orderNo, o.mealDate[:10], o.status, batchID, o.batchNo, o.picked, o.qty,
				o.total, o.sub, o.pay, o.cost, handling, transferred, kitchenResp); err != nil {
				fail(c, http.StatusInternalServerError, "写入清算明细失败: "+err.Error())
				return
			}
			continue
		}
		switch o.segment {
		case "signed":
			handling = "已签收记录保留，补贴按实际签收正常纳入财政结算"
			addOrderEvent(tx, o.id, uid, operator, statusChangeName(req.ChangeType)+"·四段清算",
				"归入【已签收】段：记录保留，正常财政结算")
		case "in_transit":
			// 骑手已取餐：绝不能按未服务直接核销；停止配送并携回
			tx.Exec(`UPDATE deliveries SET status='failed', anomaly_note=$2 WHERE order_id=$1 AND status='picked'`,
				o.id, statusChangeName(req.ChangeType)+"，停止配送，骑手携回餐食与保温箱")
			handling = "骑手已取餐，停止配送并携回；餐食可转配，补贴冻结暂不核销"
			switch {
			case req.ChangeType == "death":
				// 已出餐未送达：不进财政结算，餐单置为异常（不核销），记食材损耗与厨房责任
				tx.Exec(`UPDATE orders SET status='exception', subsidy_frozen=TRUE, updated_at=now() WHERE id=$1`, o.id)
				handling = "已出餐未送达：不进财政结算，记食材损耗与厨房责任，保温箱由骑手交回；可转配其他老人"
				kitchenResp = true
				kitchenLoss += o.cost
			case terminate && req.ChangeType == "move_out":
				tx.Exec(`UPDATE orders SET status='refunded', refund_amount=$2, cancel_reason=$3, subsidy_frozen=TRUE WHERE id=$1`,
					o.id, o.pay, "老人搬离服务区域，在途餐单携回退餐")
				handling = "终止配送，骑手携回；自付部分按退餐处理，补贴不核销，保温箱交回；可转配"
			default: // 住院/转院暂停
				tx.Exec(`UPDATE orders SET status='paused', subsidy_frozen=TRUE, updated_at=now() WHERE id=$1`, o.id)
			}
			addOrderEvent(tx, o.id, uid, operator, statusChangeName(req.ChangeType)+"·四段清算",
				"归入【在途】段："+handling)
			anID, _ := createAnomaly(tx, o.id, elderID, "other",
				"老人「"+elderName+"」"+statusChangeName(req.ChangeType)+"，在途餐单 "+o.orderNo+
					"（批次 "+o.batchNo+"，"+itoa(o.qty)+" 份）骑手已取餐，需结清：停止配送/携回/转配，补贴冻结。", uid)
			notify(tx, 0, "rider", o.id, "停止配送", o.orderNo+" 老人"+statusChangeName(req.ChangeType)+"，请停止配送并交回餐食与保温箱（工单 #"+itoa(anID)+"）")
		case "prepared_undelivered":
			tx.Exec(`UPDATE orders SET subsidy_frozen=TRUE, updated_at=now() WHERE id=$1`)
			if pause {
				tx.Exec(`UPDATE orders SET status='paused' WHERE id=$1`, o.id)
				handling = "已备餐未出餐：暂停保留，登记批次/数量/食材成本，可转配其他老人；出院后按原异常餐单结清"
				anID, _ := createAnomaly(tx, o.id, elderID, "kitchen_shortage",
					"老人「"+elderName+"」"+statusChangeName(req.ChangeType)+"，餐单 "+o.orderNo+" 所在批次 "+o.batchNo+
						" 已备餐 "+itoa(o.qty)+" 份（食材成本 "+ftoa(o.cost)+" 元），请安排转配或暂停，出院后结清。", uid)
				_ = anID
			} else if req.ChangeType == "move_out" {
				tx.Exec(`UPDATE orders SET status='refunded', refund_amount=$2, cancel_reason=$3 WHERE id=$1`,
					o.id, o.pay, "老人搬离服务区域，已备餐未送出按退餐处理")
				preparedRefund += 0
				handling = "已备餐未送出，按退餐处理；登记批次/数量/成本，可转配其他老人"
			} else { // death
				tx.Exec(`UPDATE orders SET status='refunded', refund_amount=$2, cancel_reason=$3 WHERE id=$1`,
					o.id, o.pay, "老人去世，已出餐未送达，记食材损耗与厨房责任")
				kitchenResp = true
				kitchenLoss += o.cost
				handling = "已出餐未送达：不进财政结算，记食材损耗与厨房责任，可转配其他老人"
			}
			addOrderEvent(tx, o.id, uid, operator, statusChangeName(req.ChangeType)+"·四段清算",
				"归入【已备餐未出餐】段：批次 "+o.batchNo+"，"+itoa(o.qty)+" 份，成本 "+ftoa(o.cost)+" 元；"+handling)
			notify(tx, 0, "kitchen", o.id, "停止备餐/安排转配",
				o.orderNo+" 老人"+statusChangeName(req.ChangeType)+"，批次 "+o.batchNo+" 已备餐 "+itoa(o.qty)+" 份，请停止后续出餐并登记转配")
		case "unprepared":
			tx.Exec(`UPDATE orders SET status='cancelled', cancel_reason=$2, updated_at=now() WHERE id=$1`,
				o.id, statusChangeName(req.ChangeType)+"，未备餐取消，释放补贴额度，不产生费用")
			handling = "未备餐：停止后续配送并取消，释放补贴额度，不进财政结算"
			addOrderEvent(tx, o.id, uid, operator, statusChangeName(req.ChangeType)+"·四段清算",
				"归入【未备餐】段：取消并释放补贴额度")
			notify(tx, 0, "kitchen", o.id, "取消备餐", o.orderNo+" 老人"+statusChangeName(req.ChangeType)+"，该单未备餐，请勿制作")
		}
		if _, err := tx.Exec(`INSERT INTO status_change_items(change_id, order_id, segment, order_no, meal_date,
			order_status_snapshot, batch_id, batch_no, picked, qty, total_amount, subsidy_amount, payable_amount,
			material_cost, handling, transferred, kitchen_responsible)
			VALUES($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
			changeID, o.id, o.segment, o.orderNo, o.mealDate[:10], o.status, batchID, o.batchNo, o.picked, o.qty,
			o.total, o.sub, o.pay, o.cost, handling, transferred, kitchenResp); err != nil {
			fail(c, http.StatusInternalServerError, "写入清算明细失败: "+err.Error())
			return
		}
	}
	if kitchenLoss > 0 {
		tx.Exec(`UPDATE elder_status_changes SET kitchen_loss_total=$2 WHERE id=$1`, changeID, kitchenLoss)
	}

	// 更新老人服务状态
	switch req.ChangeType {
	case "hospitalization", "transfer":
		tx.Exec(`UPDATE elders SET service_status='paused_hospital' WHERE id=$1`, elderID)
	case "move_out":
		if inCoverage {
			// 仍在覆盖范围：更新地址，继续服务；未来餐单地址快照同步
			tx.Exec(`UPDATE elders SET address=$2 WHERE id=$1`, elderID, req.NewAddress)
			tx.Exec(`UPDATE orders SET address=$2 WHERE elder_id=$1 AND status IN ('confirmed','pending')`, elderID, req.NewAddress)
			tx.Exec(`UPDATE elder_status_changes SET status='closed' WHERE id=$1`, changeID)
		} else {
			tx.Exec(`UPDATE elders SET service_status='moved_out', active=FALSE, address=$2 WHERE id=$1`, elderID, req.NewAddress)
		}
	case "death":
		tx.Exec(`UPDATE elders SET service_status='deceased', active=FALSE WHERE id=$1`, elderID)
	}

	// 餐盒/保温箱回收（搬离出范围 / 去世）
	var outstandingBoxes int
	tx.QueryRow(`SELECT COALESCE(SUM(boxes_issued-boxes_returned),0) FROM box_records WHERE elder_id=$1 AND status<>'returned'`,
		elderID).Scan(&outstandingBoxes)
	if terminate {
		nThermal := 0
		for _, o := range orders {
			if o.picked {
				nThermal++
			}
		}
		tx.Exec(`UPDATE elder_status_changes SET boxes_to_recover=$2 WHERE id=$1`, changeID, outstandingBoxes+nThermal)
	}

	// 同步家属端、社区端、厨房端、财政核销端
	title := "老人" + statusChangeName(req.ChangeType) + "：" + elderName
	summary := "生效日期 " + req.EffectiveDate + "；四段：已签收 " + itoa(nSigned) + "、在途 " + itoa(nTransit) +
		"、已备餐未出餐 " + itoa(nPrepared) + "、未备餐 " + itoa(nUnprepared)
	if familyID.Valid {
		notify(tx, int(familyID.Int64), "", 0, title, summary+"；家属确认人："+req.FamilyConfirmedBy)
	}
	notify(tx, 0, "community", 0, title, summary+"；经办人："+operator)
	if continueService {
		notify(tx, 0, "kitchen", 0, title, "新地址仍在覆盖范围，已更新送餐地址，请按新地址正常备餐出餐："+req.NewAddress)
		notify(tx, 0, "rider", 0, title, "老人在覆盖范围内迁居，在途/后续餐单按新地址配送："+req.NewAddress)
	} else {
		notify(tx, 0, "kitchen", 0, title, summary+"；请停止后续备餐/出餐并处理已备餐转配")
		if nTransit > 0 {
			notify(tx, 0, "rider", 0, title, "有 "+itoa(nTransit)+" 单在途，请停止配送并交回餐食与保温箱")
		}
	}
	if req.ChangeType == "death" || terminate {
		financeMsg := summary + "；已签收补贴 " + ftoa(signedSub) + " 元正常结算；未出餐/未送达部分不进财政结算"
		if kitchenLoss > 0 {
			financeMsg += "；食材损耗（厨房责任）" + ftoa(kitchenLoss) + " 元"
		}
		notify(tx, 0, "finance", 0, title+"（清算）", financeMsg)
	} else if !subsidyRetained {
		notify(tx, 0, "finance", 0, title, "老人补贴资格暂停期间不保留，出院恢复时需重新核验")
	}

	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{
		"id": changeID, "change_type": req.ChangeType, "in_coverage": inCoverage,
		"segments": gin.H{"signed": nSigned, "in_transit": nTransit, "prepared": nPrepared, "unprepared": nUnprepared},
		"kitchen_loss_total": kitchenLoss,
	})
}

// 出院恢复：重新核验送餐地址/饮食禁忌/补贴资格/紧急联系人，重算当月剩余可享次数，
// 暂停期间冻结的在途/已备餐餐单按原异常餐单结清（退餐、不核销补贴），避免重复核销
func (s *Server) resumeStatusChange(c *gin.Context) {
	changeID := atoi(c.Param("id"))
	uid := c.GetInt("uid")
	operator := c.GetString("name")
	var req struct {
		EffectiveDate     string  `json:"effective_date" binding:"required"`
		Address           string  `json:"address" binding:"required"`
		Dietary           string  `json:"dietary"`
		SubsidyLevel      string  `json:"subsidy_level"`
		SubsidyAmount     float64 `json:"subsidy_amount"`
		EmergencyName     string  `json:"emergency_contact_name"`
		EmergencyPhone    string  `json:"emergency_contact_phone"`
		FamilyConfirmedBy string  `json:"family_confirmed_by" binding:"required"`
		Note              string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写恢复日期、重新核验的送餐地址与家属确认人")
		return
	}
	if req.SubsidyLevel == "" {
		req.SubsidyLevel = "none"
	}
	if req.SubsidyLevel != "none" && req.SubsidyLevel != "partial" && req.SubsidyLevel != "full" {
		fail(c, http.StatusBadRequest, "补贴等级须为 none/partial/full")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()

	var elderID int
	var ctype, cstatus, effDate, elderName, oldLevel string
	var oldAmount float64
	err = tx.QueryRow(`SELECT sc.elder_id, sc.change_type, sc.status, sc.effective_date::text, e.name,
		e.subsidy_level, e.subsidy_per_meal
		FROM elder_status_changes sc JOIN elders e ON e.id=sc.elder_id
		WHERE sc.id=$1 FOR UPDATE OF sc`, changeID).
		Scan(&elderID, &ctype, &cstatus, &effDate, &elderName, &oldLevel, &oldAmount)
	if err != nil {
		fail(c, http.StatusNotFound, "状态变更记录不存在")
		return
	}
	if ctype != "hospitalization" && ctype != "transfer" {
		fail(c, http.StatusBadRequest, "仅住院/转院暂停记录可办理出院恢复")
		return
	}
	if cstatus != "active" {
		fail(c, http.StatusBadRequest, "该暂停记录已恢复或已关闭")
		return
	}

	// 结清因暂停挂起的餐单：在途/已备餐一律退餐、补贴不核销，并办结对应异常工单
	pausedOrders, err := tx.Query(`SELECT id, order_no FROM orders WHERE elder_id=$1 AND status='paused' FOR UPDATE`, elderID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询暂停餐单失败")
		return
	}
	type po struct{ id int; no string }
	pos := []po{}
	for pausedOrders.Next() {
		var p po
		pausedOrders.Scan(&p.id, &p.no)
		pos = append(pos, p)
	}
	pausedOrders.Close()
	for _, p := range pos {
		var payable float64
		tx.QueryRow(`SELECT payable_amount FROM orders WHERE id=$1`, p.id).Scan(&payable)
		tx.Exec(`UPDATE orders SET status='refunded', refund_amount=$2, subsidy_frozen=TRUE,
			cancel_reason='出院恢复结清：住院暂停期间未签收，按退餐处理，补贴不核销', updated_at=now() WHERE id=$1`, p.id, payable)
		tx.Exec(`UPDATE anomalies SET status='resolved', resolution=COALESCE(resolution,'')||' 出院恢复时按原异常餐单结清：退餐、补贴不核销。', resolved_at=now()
			WHERE order_id=$1 AND status<>'resolved'`, p.id)
		tx.Exec(`UPDATE status_change_items SET handling=handling||'；出院恢复时结清：退餐，补贴不核销'
			WHERE change_id=$1 AND order_id=$2`, changeID, p.id)
		addOrderEvent(tx, p.id, uid, operator, "出院恢复·结清原异常餐单",
			"住院暂停期间未签收，按退餐处理，补贴不核销，避免重复核销")
	}

	// 重新核验四要素并写回档案
	if _, err := tx.Exec(`UPDATE elders SET service_status='active', active=TRUE, address=$2,
		dietary_restrictions=$3, subsidy_level=$4, subsidy_per_meal=$5,
		emergency_contact_name=$6, emergency_contact_phone=$7 WHERE id=$1`,
		elderID, req.Address, req.Dietary, req.SubsidyLevel, req.SubsidyAmount,
		req.EmergencyName, req.EmergencyPhone); err != nil {
		fail(c, http.StatusInternalServerError, "更新档案失败")
		return
	}
	// 补贴资格变化留痕
	if oldLevel != req.SubsidyLevel || oldAmount != req.SubsidyAmount {
		tx.Exec(`INSERT INTO subsidy_changes(elder_id, old_level, new_level, old_amount, new_amount, reason, changed_by, affected_orders)
			VALUES($1,$2,$3,$4,$5,'出院恢复重新核验补贴资格',$6,0)`,
			elderID, oldLevel, req.SubsidyLevel, oldAmount, req.SubsidyAmount, uid)
	}

	// 重算当月剩余可享次数：仅实际签收且含补贴的餐单计次，暂停退餐单不计
	month := currentMonth()
	var quota, used int
	tx.QueryRow(`SELECT monthly_quota FROM elders WHERE id=$1`, elderID).Scan(&quota)
	tx.QueryRow(`SELECT COUNT(*) FROM orders WHERE elder_id=$1 AND to_char(meal_date,'YYYY-MM')=$2
		AND status IN ('signed','completed','settled') AND subsidy_amount>0`, elderID, month).Scan(&used)
	remaining := 0
	if quota > 0 {
		remaining = quota - used
		if remaining < 0 {
			remaining = 0
		}
	}

	if _, err := tx.Exec(`UPDATE elder_status_changes SET status='resumed', resumed_at=now(),
		reverify_address=$2, reverify_dietary=$3, reverify_subsidy_level=$4, reverify_subsidy_amount=$5,
		reverify_emergency_name=$6, reverify_emergency_phone=$7, remaining_quota=$8,
		family_confirmed_by=COALESCE(NULLIF(family_confirmed_by,''),$9), note=CASE WHEN $10='' THEN note ELSE note||' | 恢复：'||$10 END
		WHERE id=$1`,
		changeID, req.Address, req.Dietary, req.SubsidyLevel, req.SubsidyAmount,
		req.EmergencyName, req.EmergencyPhone, remaining, req.FamilyConfirmedBy, req.Note); err != nil {
		fail(c, http.StatusInternalServerError, "更新状态变更记录失败")
		return
	}

	var familyID sql.NullInt64
	tx.QueryRow(`SELECT family_user_id FROM elders WHERE id=$1`, elderID).Scan(&familyID)
	if familyID.Valid {
		notify(tx, int(familyID.Int64), "", 0, "出院恢复："+elderName,
			"送餐地址/饮食禁忌/补贴资格/紧急联系人已重新核验；当月剩余可享补贴次数 "+quotaText(quota, remaining))
	}
	notify(tx, 0, "community", 0, "出院恢复："+elderName, "已恢复在服，四要素已核验，暂停餐单已结清（退餐不核销）")
	notify(tx, 0, "kitchen", 0, "恢复备餐："+elderName, "老人已出院恢复，可正常接收订餐")
	notify(tx, 0, "finance", 0, "出院恢复："+elderName,
		"暂停期间餐单未核销；当月已用 "+itoa(used)+" 次，剩余 "+quotaText(quota, remaining))
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"resumed": true, "remaining_quota": remaining, "quota": quota, "settled_paused": len(pos)})
}

func quotaText(quota, remaining int) string {
	if quota <= 0 {
		return "不限次"
	}
	return itoa(remaining) + "/" + itoa(quota) + " 次"
}

// 已备餐/在途餐食转配给其他老人
func (s *Server) transferMeal(c *gin.Context) {
	changeID := atoi(c.Param("id"))
	var req struct {
		ItemIDs    []int `json:"item_ids"`
		ToElderID  int   `json:"to_elder_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.ItemIDs) == 0 || req.ToElderID == 0 {
		fail(c, http.StatusBadRequest, "请选择转配餐食与接收老人")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var toName string
	if err := tx.QueryRow(`SELECT name FROM elders WHERE id=$1 AND active`, req.ToElderID).Scan(&toName); err != nil {
		fail(c, http.StatusBadRequest, "接收老人不存在或已不在服")
		return
	}
	qty := 0
	for _, iid := range req.ItemIDs {
		var orderID, q int
		var seg, no string
		if err := tx.QueryRow(`SELECT order_id, segment, qty, order_no FROM status_change_items
			WHERE id=$1 AND change_id=$2 AND transferred=FALSE AND segment IN ('in_transit','prepared_undelivered')`,
			iid, changeID).Scan(&orderID, &seg, &q, &no); err != nil {
			continue
		}
		if _, err := tx.Exec(`UPDATE status_change_items SET transferred=TRUE, handling=handling||$2 WHERE id=$1`,
			iid, "；已转配给老人「"+toName+"」"); err != nil {
			fail(c, http.StatusInternalServerError, "登记转配失败: "+err.Error())
			return
		}
		addOrderEvent(tx, orderID, c.GetInt("uid"), c.GetString("name"), "已备餐食转配",
			no+" 的 "+itoa(q)+" 份餐食转配给老人「"+toName+"」，避免食材损耗")
		qty += q
	}
	if qty > 0 {
		tx.Exec(`UPDATE elder_status_changes SET transferred_qty=transferred_qty+$2 WHERE id=$1`, changeID, qty)
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"transferred": qty > 0, "transferred_qty": qty})
}

// 登记餐盒/保温箱回收（搬离/去世清算）
func (s *Server) recoverBoxes(c *gin.Context) {
	changeID := atoi(c.Param("id"))
	var req struct {
		Count int `json:"count"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Count <= 0 {
		fail(c, http.StatusBadRequest, "请填写本次回收数量")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var elderID, toRecover, recovered int
	if err := tx.QueryRow(`SELECT elder_id, boxes_to_recover, boxes_recovered FROM elder_status_changes
		WHERE id=$1 FOR UPDATE`, changeID).Scan(&elderID, &toRecover, &recovered); err != nil {
		fail(c, http.StatusNotFound, "状态变更记录不存在")
		return
	}
	newRec := recovered + req.Count
	if newRec > toRecover {
		newRec = toRecover
	}
	tx.Exec(`UPDATE elder_status_changes SET boxes_recovered=$2 WHERE id=$1`, changeID, newRec)
	if err := adjustInventory(tx, newRec-recovered); err != nil {
		fail(c, http.StatusInternalServerError, "更新餐盒库存失败")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"boxes_recovered": newRec, "boxes_to_recover": toRecover})
}

func (s *Server) listStatusChanges(c *gin.Context) {
	conds := []string{"TRUE"}
	args := []interface{}{}
	if v := c.Query("type"); v != "" {
		args = append(args, v)
		conds = append(conds, `sc.change_type=$`+itoa(len(args)))
	}
	if v := c.Query("status"); v != "" {
		args = append(args, v)
		conds = append(conds, `sc.status=$`+itoa(len(args)))
	}
	role := c.GetString("role")
	uid := c.GetInt("uid")
	if role == "family" || role == "elder" {
		args = append(args, uid)
		conds = append(conds, `sc.elder_id IN (SELECT id FROM elders WHERE family_user_id=$`+itoa(len(args))+` OR user_id=$`+itoa(len(args))+`)`)
	}
	rows, err := s.db.Query(`SELECT sc.id, sc.elder_id, e.name, sc.change_type, sc.status, sc.effective_date::text,
		COALESCE(sc.hospital,''), COALESCE(sc.new_address,''), sc.in_coverage,
		sc.seg_signed, sc.seg_in_transit, sc.seg_prepared, sc.seg_unprepared,
		sc.signed_subsidy_total, sc.kitchen_loss_total, sc.boxes_to_recover, sc.boxes_recovered,
		sc.transferred_qty, sc.remaining_quota, COALESCE(u.name,''), sc.family_confirmed_by, sc.created_at::text
		FROM elder_status_changes sc JOIN elders e ON e.id=sc.elder_id
		LEFT JOIN users u ON u.id=sc.operator_id
		WHERE `+strings.Join(conds, " AND ")+` ORDER BY sc.id DESC LIMIT 100`, args...)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询状态变更失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var (
			id, elderID, nSig, nTr, nPre, nUnpre, boxTo, boxRec, transQ, remain int
			name, ctype, status, effDate, hospital, newAddr, op, confirmed, at  string
			inCov                                                                 bool
			signedSub, loss                                                       float64
		)
		rows.Scan(&id, &elderID, &name, &ctype, &status, &effDate, &hospital, &newAddr, &inCov,
			&nSig, &nTr, &nPre, &nUnpre, &signedSub, &loss, &boxTo, &boxRec, &transQ, &remain,
			&op, &confirmed, &at)
		list = append(list, gin.H{
			"id": id, "elder_id": elderID, "elder_name": name, "change_type": ctype,
			"change_type_name": statusChangeName(ctype), "status": status, "effective_date": effDate[:10],
			"hospital": hospital, "new_address": newAddr, "in_coverage": inCov,
			"seg_signed": nSig, "seg_in_transit": nTr, "seg_prepared": nPre, "seg_unprepared": nUnpre,
			"signed_subsidy_total": signedSub, "kitchen_loss_total": loss,
			"boxes_to_recover": boxTo, "boxes_recovered": boxRec, "transferred_qty": transQ,
			"remaining_quota": remain, "operator": op, "family_confirmed_by": confirmed, "created_at": at,
		})
	}
	ok(c, list)
}

func (s *Server) getStatusChange(c *gin.Context) {
	id := c.Param("id")
	var (
		elderID                                                                int
		ctype, status, effDate, hospital, famName, famPhone, transferHosp      string
		newAddr, confirmedBy, note, op, createdAt, resumedAt, deathDate        string
		rvAddr, rvDiet, rvLevel, rvEmName, rvEmPhone                          string
		inCov, subsidyRetained                                                 bool
		expectedDischarge                                                      sql.NullString
		nSig, nTr, nPre, nUnpre, boxTo, boxRec, transQ, remain                 int
		signedSub, transitSub, prepRefund, prepCost, unprepSub, loss, rvAmount float64
	)
	err := s.db.QueryRow(`SELECT sc.elder_id, sc.change_type, sc.status, sc.effective_date::text,
		COALESCE(sc.hospital,''), COALESCE(sc.expected_discharge_date::text,''), sc.family_contact_name, sc.family_contact_phone,
		sc.subsidy_retained, COALESCE(sc.transfer_hospital,''), COALESCE(sc.new_address,''), sc.in_coverage,
		COALESCE(sc.death_date::text,''),
		sc.seg_signed, sc.seg_in_transit, sc.seg_prepared, sc.seg_unprepared,
		sc.signed_subsidy_total, sc.transit_subsidy_total, sc.prepared_refund_total, sc.prepared_cost_total,
		sc.unprepared_cancel_subsidy, sc.kitchen_loss_total, sc.boxes_to_recover, sc.boxes_recovered,
		sc.transferred_qty, sc.remaining_quota,
		COALESCE(sc.reverify_address,''), COALESCE(sc.reverify_dietary,''), COALESCE(sc.reverify_subsidy_level,''),
		sc.reverify_subsidy_amount, COALESCE(sc.reverify_emergency_name,''), COALESCE(sc.reverify_emergency_phone,''),
		COALESCE(u.name,''), sc.family_confirmed_by, sc.created_at::text, COALESCE(sc.resumed_at::text,'')
		FROM elder_status_changes sc LEFT JOIN users u ON u.id=sc.operator_id WHERE sc.id=$1`, id).
		Scan(&elderID, &ctype, &status, &effDate, &hospital, &expectedDischarge, &famName, &famPhone,
			&subsidyRetained, &transferHosp, &newAddr, &inCov, &deathDate,
			&nSig, &nTr, &nPre, &nUnpre, &signedSub, &transitSub, &prepRefund, &prepCost, &unprepSub, &loss,
			&boxTo, &boxRec, &transQ, &remain,
			&rvAddr, &rvDiet, &rvLevel, &rvAmount, &rvEmName, &rvEmPhone,
			&op, &confirmedBy, &createdAt, &resumedAt)
	if err != nil {
		fail(c, http.StatusNotFound, "状态变更记录不存在")
		return
	}
	var elderName, serviceStatus, address, dietary, emergName, emergPhone string
	var quota int
	s.db.QueryRow(`SELECT name, service_status, address, dietary_restrictions, emergency_contact_name,
		emergency_contact_phone, monthly_quota FROM elders WHERE id=$1`, elderID).
		Scan(&elderName, &serviceStatus, &address, &dietary, &emergName, &emergPhone, &quota)

	items := []gin.H{}
	rows, err := s.db.Query(`SELECT id, order_id, segment, order_no, meal_date::text, order_status_snapshot,
		COALESCE(batch_no,''), picked, qty, total_amount, subsidy_amount, payable_amount, material_cost,
		handling, transferred, kitchen_responsible FROM status_change_items
		WHERE change_id=$1 ORDER BY CASE segment WHEN 'signed' THEN 1 WHEN 'in_transit' THEN 2
		WHEN 'prepared_undelivered' THEN 3 ELSE 4 END, meal_date, id`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var iid, oid, qty int
			var seg, no, md, snap, bn, handling string
			var picked, transferred, kitchen bool
			var total, sub, pay, cost float64
			rows.Scan(&iid, &oid, &seg, &no, &md, &snap, &bn, &picked, &qty, &total, &sub, &pay, &cost,
				&handling, &transferred, &kitchen)
			items = append(items, gin.H{
				"id": iid, "order_id": oid, "segment": seg, "segment_name": segmentName(seg),
				"order_no": no, "meal_date": md[:10], "status_snapshot": snap, "batch_no": bn,
				"picked": picked, "qty": qty, "total_amount": total, "subsidy_amount": sub,
				"payable_amount": pay, "material_cost": cost, "handling": handling,
				"transferred": transferred, "kitchen_responsible": kitchen,
			})
		}
	}
	ok(c, gin.H{
		"id": atoi(id), "elder_id": elderID, "elder_name": elderName, "change_type": ctype,
		"change_type_name": statusChangeName(ctype), "status": status, "effective_date": effDate[:10],
		"hospital": hospital, "expected_discharge_date": expectedDischarge.String,
		"family_contact_name": famName, "family_contact_phone": famPhone, "subsidy_retained": subsidyRetained,
		"transfer_hospital": transferHosp, "new_address": newAddr, "in_coverage": inCov, "death_date": deathDate,
		"segments": gin.H{"signed": nSig, "in_transit": nTr, "prepared": nPre, "unprepared": nUnpre},
		"signed_subsidy_total": signedSub, "transit_subsidy_total": transitSub,
		"prepared_refund_total": prepRefund, "prepared_cost_total": prepCost,
		"unprepared_cancel_subsidy": unprepSub, "kitchen_loss_total": loss,
		"boxes_to_recover": boxTo, "boxes_recovered": boxRec, "transferred_qty": transQ,
		"remaining_quota": remain, "monthly_quota": quota,
		"reverify": gin.H{"address": rvAddr, "dietary": rvDiet, "subsidy_level": rvLevel,
			"subsidy_amount": rvAmount, "emergency_name": rvEmName, "emergency_phone": rvEmPhone},
		"elder_current": gin.H{"service_status": serviceStatus, "address": address, "dietary": dietary,
			"emergency_name": emergName, "emergency_phone": emergPhone},
		"operator": op, "family_confirmed_by": confirmedBy, "note": note,
		"created_at": createdAt, "resumed_at": resumedAt, "items": items,
	})
}
