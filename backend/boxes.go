package main

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 餐盒押金阈值：连续未归还达到该数量可收取押金
const boxDepositThreshold = 3

// 押金规则（按老人类型区分）：普通老人 20 元，困难老人 10 元且可申请免押
func depositAmountFor(elderType string) float64 {
	if elderType == "difficult" {
		return 10
	}
	return 20
}

// 老人未回收餐盒数
func elderUnreturned(tx *sql.Tx, elderID int) int {
	var n int
	tx.QueryRow(`SELECT COALESCE(SUM(boxes_issued-boxes_returned),0) FROM box_records WHERE elder_id=$1 AND status<>'returned'`, elderID).Scan(&n)
	return n
}

// 餐盒库存出账/入账（库存下限 0）
func adjustInventory(tx *sql.Tx, delta int) error {
	_, err := tx.Exec(`UPDATE box_inventory SET stock=GREATEST(0, stock+$1), updated_at=now() WHERE location='社区食堂'`, delta)
	return err
}

func strOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---------------- 老人维度餐盒汇总（未回收数量/提醒次数/家属反馈/押金状态） ----------------

func (s *Server) listElderBoxStatus(c *gin.Context) {
	rows, err := s.db.Query(`SELECT e.id, e.name, e.elder_type, e.box_policy, e.deposit_status,
		COALESCE(SUM(b.boxes_issued-b.boxes_returned) FILTER (WHERE b.status<>'returned'),0),
		COALESCE(SUM(b.remind_count),0),
		COALESCE(MAX(NULLIF(b.family_feedback,'')), ''),
		COALESCE(SUM(b.boxes_issued),0), COALESCE(SUM(b.boxes_returned),0)
		FROM elders e LEFT JOIN box_records b ON b.elder_id=e.id
		WHERE e.active
		GROUP BY e.id, e.name, e.elder_type, e.box_policy, e.deposit_status
		ORDER BY SUM(b.boxes_issued-b.boxes_returned) FILTER (WHERE b.status<>'returned') DESC NULLS LAST, e.id`)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询餐盒汇总失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var (
			id, unreturned, reminds, issued, returned int
			name, etype, policy, deposit, feedback    string
		)
		rows.Scan(&id, &name, &etype, &policy, &deposit, &unreturned, &reminds, &feedback, &issued, &returned)
		list = append(list, gin.H{
			"elder_id": id, "elder_name": name, "elder_type": etype, "box_policy": policy,
			"deposit_status": deposit, "unreturned": unreturned, "remind_count": reminds,
			"family_feedback": feedback, "boxes_issued_total": issued, "boxes_returned_total": returned,
			"over_threshold": unreturned >= boxDepositThreshold,
		})
	}
	ok(c, list)
}

// ---------------- 押金台账 ----------------

func (s *Server) listDeposits(c *gin.Context) {
	role := c.GetString("role")
	uid := c.GetInt("uid")
	where := "TRUE"
	args := []interface{}{}
	if role == "family" || role == "elder" {
		args = append(args, uid)
		where = `d.elder_id IN (SELECT id FROM elders WHERE family_user_id=$1 OR user_id=$1)`
	}
	rows, err := s.db.Query(`SELECT d.id, d.elder_id, e.name, e.elder_type, d.amount, d.status, d.unreturned_snapshot,
		d.family_feedback, COALESCE(cb.name,''), d.created_at::text, d.paid_at::text,
		COALESCE(wa.name,''), d.waive_reason, COALESCE(ap.name,''), d.waived_at::text,
		d.volunteer_id, d.volunteer_name, d.refund_at::text
		FROM box_deposits d
		JOIN elders e ON e.id=d.elder_id
		LEFT JOIN users cb ON cb.id=d.created_by
		LEFT JOIN users wa ON wa.id=d.waive_applicant_id
		LEFT JOIN users ap ON ap.id=d.waive_approver_id
		WHERE `+where+` ORDER BY d.id DESC LIMIT 100`, args...)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询押金台账失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var (
			id, elderID, unreturned, volunteerID             int
			elderName, elderType, status, feedback           string
			createdBy, createdAt, waiveReason, volunteerName string
			applicant, approver                              string
			amount                                           float64
			paidAt, waivedAt, refundAt                       *string
		)
		rows.Scan(&id, &elderID, &elderName, &elderType, &amount, &status, &unreturned,
			&feedback, &createdBy, &createdAt, &paidAt, &applicant, &waiveReason, &approver, &waivedAt,
			&volunteerID, &volunteerName, &refundAt)
		list = append(list, gin.H{
			"id": id, "elder_id": elderID, "elder_name": elderName, "elder_type": elderType,
			"amount": amount, "status": status, "unreturned_snapshot": unreturned,
			"family_feedback": feedback, "created_by": createdBy, "created_at": createdAt,
			"paid_at": strOr(paidAt), "waive_applicant": applicant, "waive_reason": waiveReason,
			"waive_approver": approver, "waived_at": strOr(waivedAt),
			"volunteer_id": volunteerID, "volunteer_name": volunteerName, "refund_at": strOr(refundAt),
		})
	}
	ok(c, list)
}

// 收取押金：连续未归还超阈值，金额按老人类型。
// 同一事务内锁定老人与其未终结押金单：待缴纳/已缴纳/免押审批中/已免押待回收
// 均属唯一进行中的押金责任链，存在任一即禁止重复立单
func (s *Server) chargeDeposit(c *gin.Context) {
	elderID := c.Param("id")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var elderName, elderType string
	err = tx.QueryRow(`SELECT name, elder_type FROM elders WHERE id=$1 FOR UPDATE`, elderID).
		Scan(&elderName, &elderType)
	if err != nil {
		fail(c, http.StatusNotFound, "老人档案不存在")
		return
	}
	// 锁定该老人所有未终结押金单（refund_at 为空且处于责任状态），保证唯一进行中责任链
	openRows, err := tx.Query(`SELECT id, status FROM box_deposits
		WHERE elder_id=$1 AND refund_at IS NULL AND status IN ('pending','paid','waive_pending','waived')
		ORDER BY id FOR UPDATE`, elderID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "校验押金链失败")
		return
	}
	openID := 0
	openStatus := ""
	openCount := 0
	for openRows.Next() {
		var id int
		var st string
		openRows.Scan(&id, &st)
		if openCount == 0 {
			openID, openStatus = id, st
		}
		openCount++
	}
	openRows.Close()
	if openCount > 0 {
		fail(c, http.StatusBadRequest, "该老人存在进行中的押金责任链（单号 #"+itoa(openID)+"，状态 "+openStatus+"），请先闭环再立单")
		return
	}
	unreturned := elderUnreturned(tx, atoi(elderID))
	if unreturned < boxDepositThreshold {
		fail(c, http.StatusBadRequest, "未归还餐盒未达阈值（≥3 个），暂无需收取押金")
		return
	}
	amount := depositAmountFor(elderType)
	var depositID int
	err = tx.QueryRow(`INSERT INTO box_deposits(elder_id, amount, unreturned_snapshot, created_by)
		VALUES($1,$2,$3,$4) RETURNING id`, elderID, amount, unreturned, c.GetInt("uid")).Scan(&depositID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建押金单失败")
		return
	}
	if _, err := tx.Exec(`UPDATE elders SET deposit_status='pending' WHERE id=$1`, elderID); err != nil {
		fail(c, http.StatusInternalServerError, "更新押金状态失败")
		return
	}
	notifyElderParties(tx, atoi(elderID), 0, "餐盒押金待缴纳",
		"老人「"+elderName+"」连续未归还餐盒 "+itoa(unreturned)+" 个，按规则需缴纳押金 "+ftoa(amount)+" 元，归还餐盒后可申请退还")
	notify(tx, 0, "community", 0, "押金已发起", "老人「"+elderName+"」押金单 #"+itoa(depositID)+" 已创建")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"deposit_id": depositID, "amount": amount, "unreturned": unreturned})
}

// 缴纳押金（家属）
func (s *Server) payDeposit(c *gin.Context) {
	depositID := c.Param("id")
	uid := c.GetInt("uid")
	role := c.GetString("role")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var elderID int
	var status, elderName string
	var amount float64
	err = tx.QueryRow(`SELECT d.elder_id, d.status, d.amount, e.name FROM box_deposits d
		JOIN elders e ON e.id=d.elder_id WHERE d.id=$1 FOR UPDATE`, depositID).
		Scan(&elderID, &status, &amount, &elderName)
	if err != nil {
		fail(c, http.StatusNotFound, "押金单不存在")
		return
	}
	if role == "family" || role == "elder" {
		var n int
		tx.QueryRow(`SELECT COUNT(*) FROM elders WHERE id=$1 AND (family_user_id=$2 OR user_id=$2)`, elderID, uid).Scan(&n)
		if n == 0 {
			fail(c, http.StatusForbidden, "只能为绑定老人缴纳押金")
			return
		}
	}
	if status != "pending" {
		fail(c, http.StatusBadRequest, "该押金单不在待缴纳状态")
		return
	}
	if _, err := tx.Exec(`UPDATE box_deposits SET status='paid', paid_at=now() WHERE id=$1`, depositID); err != nil {
		fail(c, http.StatusInternalServerError, "缴纳失败")
		return
	}
	tx.Exec(`UPDATE elders SET deposit_status='paid' WHERE id=$1`, elderID)
	notify(tx, 0, "community", 0, "押金已缴纳", "老人「"+elderName+"」押金 "+ftoa(amount)+" 元已缴纳")
	notify(tx, 0, "finance", 0, "押金已缴纳", "老人「"+elderName+"」餐盒押金 "+ftoa(amount)+" 元已缴纳")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"paid": true})
}

// 申请免押（困难老人，社区负责人发起并指定回收志愿者）
func (s *Server) waiveApply(c *gin.Context) {
	depositID := c.Param("id")
	var req struct {
		VolunteerID int    `json:"volunteer_id" binding:"required"`
		Reason      string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请选择回收志愿者并填写免押原因")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var elderID int
	var status, elderName, elderType string
	err = tx.QueryRow(`SELECT d.elder_id, d.status, e.name, e.elder_type FROM box_deposits d
		JOIN elders e ON e.id=d.elder_id WHERE d.id=$1 FOR UPDATE`, depositID).
		Scan(&elderID, &status, &elderName, &elderType)
	if err != nil {
		fail(c, http.StatusNotFound, "押金单不存在")
		return
	}
	if elderType != "difficult" {
		fail(c, http.StatusBadRequest, "仅困难老人可申请免押")
		return
	}
	if status != "pending" {
		fail(c, http.StatusBadRequest, "仅待缴纳状态可申请免押")
		return
	}
	var volunteerName, vrole string
	err = tx.QueryRow(`SELECT name, role FROM users WHERE id=$1 AND active`, req.VolunteerID).Scan(&volunteerName, &vrole)
	if err != nil || vrole != "volunteer" {
		fail(c, http.StatusBadRequest, "回收志愿者不存在或已停用")
		return
	}
	if _, err := tx.Exec(`UPDATE box_deposits SET status='waive_pending', waive_applicant_id=$1, waive_reason=$2,
		volunteer_id=$3, volunteer_name=$4 WHERE id=$5`,
		c.GetInt("uid"), req.Reason, req.VolunteerID, volunteerName, depositID); err != nil {
		fail(c, http.StatusInternalServerError, "申请失败")
		return
	}
	tx.Exec(`UPDATE elders SET deposit_status='waive_pending' WHERE id=$1`, elderID)
	notify(tx, 0, "admin", 0, "免押审批待办", "社区负责人为老人「"+elderName+"」申请餐盒押金免押，回收志愿者："+volunteerName+"，请审批")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"applied": true})
}

// 免押审批（平台管理员）：留存审批人，审批后志愿者可上门回收
func (s *Server) waiveApprove(c *gin.Context) {
	depositID := c.Param("id")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var elderID, volunteerID int
	var status, elderName, volunteerName string
	err = tx.QueryRow(`SELECT d.elder_id, d.status, e.name, COALESCE(d.volunteer_id,0), COALESCE(d.volunteer_name,'')
		FROM box_deposits d JOIN elders e ON e.id=d.elder_id WHERE d.id=$1 FOR UPDATE`, depositID).
		Scan(&elderID, &status, &elderName, &volunteerID, &volunteerName)
	if err != nil {
		fail(c, http.StatusNotFound, "押金单不存在")
		return
	}
	if status != "waive_pending" {
		fail(c, http.StatusBadRequest, "该押金单不在免押审批中")
		return
	}
	if _, err := tx.Exec(`UPDATE box_deposits SET status='waived', waive_approver_id=$1, waived_at=now() WHERE id=$2`,
		c.GetInt("uid"), depositID); err != nil {
		fail(c, http.StatusInternalServerError, "审批失败")
		return
	}
	tx.Exec(`UPDATE elders SET deposit_status='waived' WHERE id=$1`, elderID)
	notifyElderParties(tx, elderID, 0, "押金免押已批准", "老人「"+elderName+"」餐盒押金已免押，志愿者「"+volunteerName+"」将上门回收餐盒")
	if volunteerID > 0 {
		notify(tx, volunteerID, "", 0, "志愿回收任务", "请上门回收老人「"+elderName+"」的餐盒，回收后请在平台登记")
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"waived": true})
}

// 退还押金（餐盒全部归还后）
func (s *Server) refundDeposit(c *gin.Context) {
	depositID := c.Param("id")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var elderID int
	var status, elderName string
	err = tx.QueryRow(`SELECT d.elder_id, d.status, e.name FROM box_deposits d
		JOIN elders e ON e.id=d.elder_id WHERE d.id=$1 FOR UPDATE`, depositID).
		Scan(&elderID, &status, &elderName)
	if err != nil {
		fail(c, http.StatusNotFound, "押金单不存在")
		return
	}
	if status != "paid" {
		fail(c, http.StatusBadRequest, "仅已缴纳状态可退还")
		return
	}
	if n := elderUnreturned(tx, elderID); n > 0 {
		fail(c, http.StatusBadRequest, "尚有 "+itoa(n)+" 个餐盒未归还，不能退还押金")
		return
	}
	if _, err := tx.Exec(`UPDATE box_deposits SET status='refunded', refund_at=now() WHERE id=$1`, depositID); err != nil {
		fail(c, http.StatusInternalServerError, "退还失败")
		return
	}
	tx.Exec(`UPDATE elders SET deposit_status='none' WHERE id=$1`, elderID)
	notifyElderParties(tx, elderID, 0, "押金已退还", "老人「"+elderName+"」餐盒已全部归还，押金已退还")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"refunded": true})
}

// ---------------- 社区处置：上门回收 / 改一次性餐盒 / 暂停发放 ----------------

// 上门回收：社区上门收回餐盒，更新台账与库存
func (s *Server) doorCollect(c *gin.Context) {
	var req struct {
		ElderID int `json:"elder_id" binding:"required"`
		Count   int `json:"count" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Count <= 0 {
		fail(c, http.StatusBadRequest, "请选择老人并填写回收数量")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var elderName string
	if err := tx.QueryRow(`SELECT name FROM elders WHERE id=$1`, req.ElderID).Scan(&elderName); err != nil {
		fail(c, http.StatusNotFound, "老人档案不存在")
		return
	}
	collected, err := collectBoxes(tx, req.ElderID, req.Count)
	if err != nil {
		fail(c, http.StatusBadRequest, "回收失败：未归还餐盒不足")
		return
	}
	if err := adjustInventory(tx, collected); err != nil {
		fail(c, http.StatusInternalServerError, "更新库存失败")
		return
	}
	// 全部归还后：闭合该老人未终结的免押责任链（refund_at 标记终结），避免孤儿押金单；
	// 待缴纳/已缴纳押金单涉及资金，保留给退押流程显式处理
	if elderUnreturned(tx, req.ElderID) == 0 {
		if _, err := tx.Exec(`UPDATE box_deposits SET refund_at=now()
			WHERE elder_id=$1 AND status='waived' AND refund_at IS NULL`, req.ElderID); err != nil {
			fail(c, http.StatusInternalServerError, "闭合免押责任链失败")
			return
		}
		if _, err := tx.Exec(`UPDATE elders SET deposit_status='none'
			WHERE id=$1 AND deposit_status='waived'`, req.ElderID); err != nil {
			fail(c, http.StatusInternalServerError, "同步老人押金状态失败")
			return
		}
	}
	notify(tx, 0, "community", 0, "上门回收完成", "老人「"+elderName+"」上门回收餐盒 "+itoa(collected)+" 个，已更新库存")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"collected": collected})
}

// 志愿回收：免押审批指定的志愿者上门回收，结果更新餐盒库存
func (s *Server) volunteerCollect(c *gin.Context) {
	var req struct {
		ElderID int    `json:"elder_id" binding:"required"`
		Count   int    `json:"count" binding:"required"`
		Note    string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Count <= 0 {
		fail(c, http.StatusBadRequest, "请选择老人并填写回收数量")
		return
	}
	uid := c.GetInt("uid")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	// 必须是免押审批指定、且尚未闭合的责任链对应的回收志愿者（防止餐盒责任空转与闭环错配）
	var elderName string
	var depositID int
	err = tx.QueryRow(`SELECT e.name, d.id FROM box_deposits d JOIN elders e ON e.id=d.elder_id
		WHERE d.elder_id=$1 AND d.status='waived' AND d.volunteer_id=$2 AND d.refund_at IS NULL
		ORDER BY d.id DESC LIMIT 1 FOR UPDATE OF d`, req.ElderID, uid).Scan(&elderName, &depositID)
	if err != nil {
		fail(c, http.StatusForbidden, "仅免押审批指定的回收志愿者可登记志愿回收")
		return
	}
	collected, err := collectBoxes(tx, req.ElderID, req.Count)
	if err != nil {
		fail(c, http.StatusBadRequest, "回收失败：未归还餐盒不足")
		return
	}
	if err := adjustInventory(tx, collected); err != nil {
		fail(c, http.StatusInternalServerError, "更新库存失败")
		return
	}
	// 全部归还后仅闭合该授权责任链（refund_at 标记终结），老人汇总状态同步复位
	if elderUnreturned(tx, req.ElderID) == 0 {
		if _, err := tx.Exec(`UPDATE box_deposits SET refund_at=now() WHERE id=$1 AND refund_at IS NULL`, depositID); err != nil {
			fail(c, http.StatusInternalServerError, "闭合押金链失败")
			return
		}
		if _, err := tx.Exec(`UPDATE elders SET deposit_status='none' WHERE id=$1`, req.ElderID); err != nil {
			fail(c, http.StatusInternalServerError, "同步老人押金状态失败")
			return
		}
	}
	notify(tx, 0, "community", 0, "志愿回收完成", "志愿者回收老人「"+elderName+"」餐盒 "+itoa(collected)+" 个，库存已更新。"+req.Note)
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"collected": collected})
}

// 从最旧未归还记录开始回收，返回实际回收数量
func collectBoxes(tx *sql.Tx, elderID, count int) (int, error) {
	rows, err := tx.Query(`SELECT id, order_id, boxes_issued, boxes_returned FROM box_records
		WHERE elder_id=$1 AND status<>'returned' ORDER BY id FOR UPDATE`, elderID)
	if err != nil {
		return 0, err
	}
	type rec struct{ id, orderID, issued, returned int }
	recs := []rec{}
	for rows.Next() {
		var r rec
		rows.Scan(&r.id, &r.orderID, &r.issued, &r.returned)
		recs = append(recs, r)
	}
	rows.Close()
	remaining := count
	collected := 0
	for _, r := range recs {
		if remaining <= 0 {
			break
		}
		out := r.issued - r.returned
		take := out
		if take > remaining {
			take = remaining
		}
		newReturned := r.returned + take
		newStatus := "partial"
		if newReturned == r.issued {
			newStatus = "returned"
		}
		if _, err := tx.Exec(`UPDATE box_records SET boxes_returned=$1, status=$2, returned_at=now() WHERE id=$3`,
			newReturned, newStatus, r.id); err != nil {
			return collected, err
		}
		if newStatus == "returned" {
			tx.Exec(`UPDATE orders SET status='completed', updated_at=now() WHERE id=$1 AND status='signed'`, r.orderID)
		}
		addOrderEvent(tx, r.orderID, 0, "社区/志愿者", "餐盒回收", "回收 "+itoa(take)+" 个（累计 "+itoa(newReturned)+"/"+itoa(r.issued)+"）")
		remaining -= take
		collected += take
	}
	if collected == 0 {
		return 0, sql.ErrNoRows
	}
	return collected, nil
}

// 调整餐盒策略：normal 正常 / disposable 改用一次性餐盒 / paused 暂停新增发放
func (s *Server) setBoxPolicy(c *gin.Context) {
	elderID := c.Param("id")
	var req struct {
		Policy string `json:"policy" binding:"required"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请选择餐盒策略")
		return
	}
	if req.Policy != "normal" && req.Policy != "disposable" && req.Policy != "paused" {
		fail(c, http.StatusBadRequest, "策略须为 normal/disposable/paused")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var elderName string
	if err := tx.QueryRow(`SELECT name FROM elders WHERE id=$1 FOR UPDATE`, elderID).Scan(&elderName); err != nil {
		fail(c, http.StatusNotFound, "老人档案不存在")
		return
	}
	if _, err := tx.Exec(`UPDATE elders SET box_policy=$1 WHERE id=$2`, req.Policy, elderID); err != nil {
		fail(c, http.StatusInternalServerError, "设置失败")
		return
	}
	policyName := map[string]string{"normal": "正常发放", "disposable": "改用一次性餐盒", "paused": "暂停新增餐盒发放"}[req.Policy]
	notifyElderParties(tx, atoi(elderID), 0, "餐盒策略调整", "老人「"+elderName+"」餐盒策略调整为："+policyName+"。"+req.Reason)
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"policy": req.Policy})
}

// 家属反馈（针对餐盒提醒）
func (s *Server) familyBoxFeedback(c *gin.Context) {
	boxID := c.Param("id")
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写反馈内容")
		return
	}
	uid := c.GetInt("uid")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var elderID, orderID int
	var elderName, orderNo string
	err = tx.QueryRow(`SELECT b.elder_id, e.name, o.order_no, b.order_id FROM box_records b
		JOIN elders e ON e.id=b.elder_id JOIN orders o ON o.id=b.order_id WHERE b.id=$1 FOR UPDATE`, boxID).
		Scan(&elderID, &elderName, &orderNo, &orderID)
	if err != nil {
		fail(c, http.StatusNotFound, "餐盒记录不存在")
		return
	}
	var n int
	tx.QueryRow(`SELECT COUNT(*) FROM elders WHERE id=$1 AND (family_user_id=$2 OR user_id=$2)`, elderID, uid).Scan(&n)
	if n == 0 {
		fail(c, http.StatusForbidden, "只能反馈绑定老人的餐盒记录")
		return
	}
	if _, err := tx.Exec(`UPDATE box_records SET family_feedback=$1 WHERE id=$2`, req.Content, boxID); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	addOrderEvent(tx, orderID, uid, c.GetString("name"), "家属反馈餐盒", orderNo+"："+req.Content)
	notify(tx, 0, "community", 0, "家属反馈餐盒", "老人「"+elderName+"」家属："+req.Content)
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"created": true})
}

// 餐盒库存
func (s *Server) boxInventory(c *gin.Context) {
	var stock int
	var updatedAt string
	err := s.db.QueryRow(`SELECT stock, updated_at::text FROM box_inventory WHERE location='社区食堂'`).Scan(&stock, &updatedAt)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询库存失败")
		return
	}
	ok(c, gin.H{"stock": stock, "updated_at": updatedAt})
}
