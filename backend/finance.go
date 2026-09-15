package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 月度核销汇总：实际签收才发放补贴，退餐/异常/餐盒回收率/厨房结算一并进入财政档案
func (s *Server) computeReconciliation(month string) (gin.H, []gin.H, error) {
	agg := gin.H{}
	row := s.db.QueryRow(`SELECT
		COUNT(*),
		COUNT(*) FILTER (WHERE status IN ('signed','completed','settled')),
		COUNT(*) FILTER (WHERE status IN ('cancelled','refunded')),
		COUNT(*) FILTER (WHERE status='exception'),
		COALESCE(SUM(subsidy_amount) FILTER (WHERE status IN ('signed','completed','settled')),0),
		COALESCE(SUM(refund_amount),0),
		COALESCE(SUM(payable_amount) FILTER (WHERE status IN ('signed','completed','settled')),0),
		COALESCE(SUM(total_amount) FILTER (WHERE status IN ('signed','completed','settled')),0),
		COALESCE(SUM(holiday_extra) FILTER (WHERE status IN ('signed','completed','settled')),0)
		FROM orders WHERE to_char(meal_date,'YYYY-MM')=$1`, month)
	var total, signed, cancelled, exception int
	var subsidy, refund, payable, kitchen, hextra float64
	if err := row.Scan(&total, &signed, &cancelled, &exception, &subsidy, &refund, &payable, &kitchen, &hextra); err != nil {
		return nil, nil, err
	}
	var anomalyCount, followupDone int
	s.db.QueryRow(`SELECT COUNT(*) FROM anomalies WHERE to_char(created_at,'YYYY-MM')=$1`, month).Scan(&anomalyCount)
	s.db.QueryRow(`SELECT COUNT(*) FROM follow_ups WHERE to_char(created_at,'YYYY-MM')=$1`, month).Scan(&followupDone)
	var boxesIssued, boxesReturned int
	s.db.QueryRow(`SELECT COALESCE(SUM(b.boxes_issued),0), COALESCE(SUM(b.boxes_returned),0)
		FROM box_records b JOIN orders o ON o.id=b.order_id WHERE to_char(o.meal_date,'YYYY-MM')=$1`, month).
		Scan(&boxesIssued, &boxesReturned)
	recycleRate := 100.0
	if boxesIssued > 0 {
		recycleRate = float64(boxesReturned) * 100.0 / float64(boxesIssued)
	}
	agg["total_orders"] = total
	agg["signed_orders"] = signed
	agg["cancelled_orders"] = cancelled
	agg["exception_orders"] = exception
	agg["subsidy_total"] = subsidy
	agg["refund_total"] = refund
	agg["payable_total"] = payable
	agg["kitchen_settlement"] = kitchen
	agg["holiday_extra_total"] = hextra
	agg["anomaly_count"] = anomalyCount
	agg["followup_done"] = followupDone
	agg["boxes_issued"] = boxesIssued
	agg["boxes_returned"] = boxesReturned
	agg["recycle_rate"] = recycleRate

	// 明细：逐单判定是否纳入补贴发放
	items := []gin.H{}
	rows, err := s.db.Query(`SELECT o.id, o.order_no, e.name, o.meal_date::text, o.status, o.total_amount, o.subsidy_amount
		FROM orders o JOIN elders e ON e.id=o.elder_id
		WHERE to_char(o.meal_date,'YYYY-MM')=$1 ORDER BY o.meal_date, o.id`, month)
	if err != nil {
		return agg, items, nil
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var no, elderName, mealDate, status string
		var totalAmt, sub float64
		rows.Scan(&id, &no, &elderName, &mealDate, &status, &totalAmt, &sub)
		included := false
		reason := ""
		switch status {
		case "signed", "completed", "settled":
			included = true
			reason = "实际签收，纳入补贴发放"
		case "cancelled":
			reason = "已取消，不发放补贴"
		case "refunded":
			reason = "已退餐退款，不发放补贴"
		case "exception":
			reason = "异常未办结，暂不发放补贴"
		default:
			reason = "未完成签收，不发放补贴"
		}
		items = append(items, gin.H{"order_id": id, "order_no": no, "elder_name": elderName,
			"meal_date": mealDate[:10], "order_status": status, "total_amount": totalAmt,
			"subsidy_amount": sub, "included": included, "reason": reason})
	}
	return agg, items, nil
}

func (s *Server) listReconciliations(c *gin.Context) {
	rows, err := s.db.Query(`SELECT r.id, r.month, r.status, r.total_orders, r.signed_orders, r.subsidy_total,
		r.refund_total, r.kitchen_settlement, r.recycle_rate, r.anomaly_count, r.followup_done,
		COALESCE(u.name,''), r.created_at::text, r.confirmed_at::text, r.archived_at::text
		FROM reconciliations r LEFT JOIN users u ON u.id=r.created_by ORDER BY r.month DESC`)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询核销记录失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var (
			id, total, signed, anomaly, followup              int
			month, status, createdBy, createdAt               string
			subsidy, refund, kitchen, rate                    float64
			confirmedAt, archivedAt                           *string
		)
		rows.Scan(&id, &month, &status, &total, &signed, &subsidy, &refund, &kitchen, &rate,
			&anomaly, &followup, &createdBy, &createdAt, &confirmedAt, &archivedAt)
		ca, aa := "", ""
		if confirmedAt != nil {
			ca = *confirmedAt
		}
		if archivedAt != nil {
			aa = *archivedAt
		}
		list = append(list, gin.H{"id": id, "month": month, "status": status, "total_orders": total,
			"signed_orders": signed, "subsidy_total": subsidy, "refund_total": refund,
			"kitchen_settlement": kitchen, "recycle_rate": rate, "anomaly_count": anomaly,
			"followup_done": followup, "created_by": createdBy, "created_at": createdAt,
			"confirmed_at": ca, "archived_at": aa})
	}
	ok(c, list)
}

// 生成/重新生成某月核销单（draft）
func (s *Server) createReconciliation(c *gin.Context) {
	var req struct {
		Month string `json:"month" binding:"required"`
		Note  string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Month) != 7 {
		fail(c, http.StatusBadRequest, "请选择核销月份（YYYY-MM）")
		return
	}
	var existingID int
	var existingStatus string
	lookupErr := s.db.QueryRow(`SELECT id, status FROM reconciliations WHERE month=$1`, req.Month).Scan(&existingID, &existingStatus)
	if lookupErr == nil && existingStatus == "archived" {
		fail(c, http.StatusBadRequest, "该月财政档案已归档锁定，不可重新生成")
		return
	}
	agg, items, err := s.computeReconciliation(req.Month)
	if err != nil {
		fail(c, http.StatusInternalServerError, "汇总核销数据失败")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var recID int
	if lookupErr == nil {
		// 已存在 draft/confirmed：刷新
		recID = existingID
		if _, err := tx.Exec(`UPDATE reconciliations SET status='draft', total_orders=$1, signed_orders=$2, cancelled_orders=$3,
			exception_orders=$4, subsidy_total=$5, refund_total=$6, payable_total=$7, kitchen_settlement=$8,
			anomaly_count=$9, followup_done=$10, boxes_issued=$11, boxes_returned=$12, recycle_rate=$13,
			holiday_extra_total=$14, note=$15, confirmed_at=NULL WHERE id=$16`,
			agg["total_orders"], agg["signed_orders"], agg["cancelled_orders"], agg["exception_orders"],
			agg["subsidy_total"], agg["refund_total"], agg["payable_total"], agg["kitchen_settlement"],
			agg["anomaly_count"], agg["followup_done"], agg["boxes_issued"], agg["boxes_returned"], agg["recycle_rate"],
			agg["holiday_extra_total"], req.Note, recID); err != nil {
			fail(c, http.StatusInternalServerError, "更新核销单失败")
			return
		}
		tx.Exec(`DELETE FROM reconciliation_items WHERE reconciliation_id=$1`, recID)
	} else {
		if err := tx.QueryRow(`INSERT INTO reconciliations(month, total_orders, signed_orders, cancelled_orders, exception_orders,
			subsidy_total, refund_total, payable_total, kitchen_settlement, anomaly_count, followup_done,
			boxes_issued, boxes_returned, recycle_rate, holiday_extra_total, note, created_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING id`,
			req.Month, agg["total_orders"], agg["signed_orders"], agg["cancelled_orders"], agg["exception_orders"],
			agg["subsidy_total"], agg["refund_total"], agg["payable_total"], agg["kitchen_settlement"],
			agg["anomaly_count"], agg["followup_done"], agg["boxes_issued"], agg["boxes_returned"],
			agg["recycle_rate"], agg["holiday_extra_total"], req.Note, c.GetInt("uid")).Scan(&recID); err != nil {
			fail(c, http.StatusInternalServerError, "创建核销单失败")
			return
		}
	}
	for _, it := range items {
		if _, err := tx.Exec(`INSERT INTO reconciliation_items(reconciliation_id, order_id, order_no, elder_name, meal_date,
			order_status, total_amount, subsidy_amount, included, reason)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			recID, it["order_id"], it["order_no"], it["elder_name"], it["meal_date"],
			it["order_status"], it["total_amount"], it["subsidy_amount"], it["included"], it["reason"]); err != nil {
			fail(c, http.StatusInternalServerError, "写入核销明细失败: "+err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"id": recID, "summary": agg})
}

func (s *Server) getReconciliation(c *gin.Context) {
	id := c.Param("id")
	var (
		month, status, note, createdBy                              string
		total, signed, cancelled, exception, anomaly, followup      int
		subsidy, refund, payable, kitchen, rate, hextra             float64
		boxesIssued, boxesReturned                                  int
		createdAt                                                   string
		confirmedAt, archivedAt                                     *string
	)
	err := s.db.QueryRow(`SELECT r.month, r.status, r.total_orders, r.signed_orders, r.cancelled_orders, r.exception_orders,
		r.subsidy_total, r.refund_total, r.payable_total, r.kitchen_settlement, r.anomaly_count, r.followup_done,
		r.boxes_issued, r.boxes_returned, r.recycle_rate, r.holiday_extra_total, r.note, COALESCE(u.name,''),
		r.created_at::text, r.confirmed_at::text, r.archived_at::text
		FROM reconciliations r LEFT JOIN users u ON u.id=r.created_by WHERE r.id=$1`, id).
		Scan(&month, &status, &total, &signed, &cancelled, &exception, &subsidy, &refund, &payable, &kitchen,
			&anomaly, &followup, &boxesIssued, &boxesReturned, &rate, &hextra, &note, &createdBy,
			&createdAt, &confirmedAt, &archivedAt)
	if err != nil {
		fail(c, http.StatusNotFound, "核销单不存在")
		return
	}
	items := []gin.H{}
	rows, err := s.db.Query(`SELECT order_id, order_no, elder_name, meal_date::text, order_status, total_amount, subsidy_amount, included, reason
		FROM reconciliation_items WHERE reconciliation_id=$1 ORDER BY meal_date, order_id`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var oid int
			var no, elder, mealDate, st, reason string
			var totalAmt, sub float64
			var included bool
			rows.Scan(&oid, &no, &elder, &mealDate, &st, &totalAmt, &sub, &included, &reason)
			items = append(items, gin.H{"order_id": oid, "order_no": no, "elder_name": elder,
				"meal_date": mealDate[:10], "order_status": st, "total_amount": totalAmt,
				"subsidy_amount": sub, "included": included, "reason": reason})
		}
	}
	ca, aa := "", ""
	if confirmedAt != nil {
		ca = *confirmedAt
	}
	if archivedAt != nil {
		aa = *archivedAt
	}
	ok(c, gin.H{
		"id": atoi(id), "month": month, "status": status, "total_orders": total, "signed_orders": signed,
		"cancelled_orders": cancelled, "exception_orders": exception, "subsidy_total": subsidy,
		"refund_total": refund, "payable_total": payable, "kitchen_settlement": kitchen,
		"anomaly_count": anomaly, "followup_done": followup, "boxes_issued": boxesIssued,
		"boxes_returned": boxesReturned, "recycle_rate": rate, "holiday_extra_total": hextra,
		"note": note, "created_by": createdBy, "created_at": createdAt,
		"confirmed_at": ca, "archived_at": aa, "items": items,
	})
}

func (s *Server) confirmReconciliation(c *gin.Context) {
	id := c.Param("id")
	res, err := s.db.Exec(`UPDATE reconciliations SET status='confirmed', confirmed_at=now() WHERE id=$1 AND status='draft'`, id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "确认失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fail(c, http.StatusBadRequest, "仅草稿状态可确认")
		return
	}
	ok(c, gin.H{"confirmed": true})
}

// 归档：财政档案锁定，当月已签收餐单置为已核销
func (s *Server) archiveReconciliation(c *gin.Context) {
	id := c.Param("id")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var month, status string
	err = tx.QueryRow(`SELECT month, status FROM reconciliations WHERE id=$1 FOR UPDATE`, id).Scan(&month, &status)
	if err != nil {
		fail(c, http.StatusNotFound, "核销单不存在")
		return
	}
	if status != "confirmed" {
		fail(c, http.StatusBadRequest, "请先确认核销单再归档")
		return
	}
	if _, err := tx.Exec(`UPDATE reconciliations SET status='archived', archived_at=now() WHERE id=$1`, id); err != nil {
		fail(c, http.StatusInternalServerError, "归档失败")
		return
	}
	if _, err := tx.Exec(`UPDATE orders SET status='settled', settled_in=$1, updated_at=now()
		WHERE to_char(meal_date,'YYYY-MM')=$2 AND status IN ('signed','completed')`, id, month); err != nil {
		fail(c, http.StatusInternalServerError, "更新餐单核销状态失败")
		return
	}
	notify(tx, 0, "community", 0, "月度核销已归档", month+" 财政档案已归档")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"archived": true})
}

func (s *Server) listSubsidyChanges(c *gin.Context) {
	rows, err := s.db.Query(`SELECT sc.id, e.name, sc.old_level, sc.new_level, sc.old_amount, sc.new_amount,
		sc.reason, COALESCE(u.name,''), sc.affected_orders, sc.created_at::text
		FROM subsidy_changes sc JOIN elders e ON e.id=sc.elder_id LEFT JOIN users u ON u.id=sc.changed_by
		ORDER BY sc.id DESC LIMIT 100`)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var id, affected int
		var elderName, ol, nl, reason, by, at string
		var oa, na float64
		rows.Scan(&id, &elderName, &ol, &nl, &oa, &na, &reason, &by, &affected, &at)
		list = append(list, gin.H{"id": id, "elder_name": elderName, "old_level": ol, "new_level": nl,
			"old_amount": oa, "new_amount": na, "reason": reason, "changed_by": by,
			"affected_orders": affected, "created_at": at})
	}
	ok(c, list)
}
