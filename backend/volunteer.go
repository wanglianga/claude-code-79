package main

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 志愿者帮送与家属代订：身份/培训/健康核验、区别于骑手的签收效力、
// 帮送异常社区核实与三方责任、老人本人回访、真实签收依据核销。

func signBasisName(b string) string {
	switch b {
	case "self", "elder":
		return "老人本人签收"
	case "family":
		return "家属代签"
	case "neighbor":
		return "邻里见证"
	case "photo":
		return "志愿者拍照留证"
	case "community":
		return "社区核实结论"
	}
	return "—"
}

func effectivenessName(e string) string {
	switch e {
	case "valid":
		return "有效"
	case "pending":
		return "待社区核实"
	case "invalid":
		return "核实无效"
	}
	return "—"
}

func etaMinutes(m int) int {
	if m <= 0 {
		return 30
	}
	return m
}

// 志愿者发车前核验：身份核验 / 助餐培训 / 当日健康 / 帮送资格
type rowQuerier interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

func volunteerDispatchCheck(q rowQuerier, uid int) (gin.H, error) {
	var (
		name, org, suspendReason string
		idVerified, trained, eligible bool
		trainingDate                  sql.NullString
	)
	err := q.QueryRow(`SELECT name, COALESCE(vol_org,''), COALESCE(vol_id_verified,FALSE),
		COALESCE(vol_trained,FALSE), COALESCE(vol_training_date::text,''), COALESCE(vol_eligible,TRUE),
		COALESCE(vol_suspend_reason,'') FROM users WHERE id=$1 AND role='volunteer'`, uid).
		Scan(&name, &org, &idVerified, &trained, &trainingDate, &eligible, &suspendReason)
	if err != nil {
		return nil, err
	}
	var healthStatus string
	var temp float64
	var healthNote string
	var checkedAt sql.NullString
	_ = q.QueryRow(`SELECT health_status, temperature, COALESCE(note,''), created_at::text
		FROM volunteer_health WHERE volunteer_id=$1 AND check_date=CURRENT_DATE`, uid).
		Scan(&healthStatus, &temp, &healthNote, &checkedAt)
	healthOK := healthStatus == "healthy"
	pass := idVerified && trained && eligible && healthOK
	reasons := []string{}
	if !idVerified {
		reasons = append(reasons, "身份尚未核验")
	}
	if !trained {
		reasons = append(reasons, "未完成助餐培训")
	}
	if !eligible {
		reasons = append(reasons, "帮送资格已暂停："+suspendReason)
	}
	if !healthOK {
		reasons = append(reasons, "当日健康未打卡或身体不适")
	}
	return gin.H{
		"volunteer_id": uid, "name": name, "org": org,
		"id_verified": idVerified, "trained": trained, "training_date": trainingDate.String,
		"eligible": eligible, "suspend_reason": suspendReason,
		"health_status": healthStatus, "temperature": temp, "health_note": healthNote,
		"health_checked": checkedAt.Valid, "dispatch_passed": pass, "block_reasons": reasons,
	}, nil
}

// 志愿者本人查看发车前核验结果
func (s *Server) volunteerDispatchStatus(c *gin.Context) {
	info, err := volunteerDispatchCheck(s.db, c.GetInt("uid"))
	if err != nil {
		fail(c, http.StatusNotFound, "志愿者档案不存在")
		return
	}
	ok(c, info)
}

// 志愿者当日健康打卡
func (s *Server) volunteerHealthCheckin(c *gin.Context) {
	uid := c.GetInt("uid")
	var req struct {
		Status      string  `json:"health_status"`
		Temperature float64 `json:"temperature"`
		Note        string  `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Status == "" {
		req.Status = "healthy"
	}
	if req.Status != "healthy" && req.Status != "unwell" {
		fail(c, http.StatusBadRequest, "健康状态须为 healthy/unwell")
		return
	}
	if req.Temperature <= 0 {
		req.Temperature = 36.5
	}
	_, err := s.db.Exec(`INSERT INTO volunteer_health(volunteer_id, check_date, health_status, temperature, note)
		VALUES($1,CURRENT_DATE,$2,$3,$4)
		ON CONFLICT (volunteer_id, check_date)
		DO UPDATE SET health_status=EXCLUDED.health_status, temperature=EXCLUDED.temperature, note=EXCLUDED.note, created_at=now()`,
		uid, req.Status, req.Temperature, req.Note)
	if err != nil {
		fail(c, http.StatusInternalServerError, "健康打卡失败")
		return
	}
	ok(c, gin.H{"checked": true, "health_status": req.Status})
}

// 志愿者管理列表（含帮送次数、异常率、老人评价、资质）
func (s *Server) listVolunteerManage(c *gin.Context) {
	rows, err := s.db.Query(`SELECT u.id, u.name, u.phone, u.active,
		COALESCE(u.vol_org,''), COALESCE(u.vol_id_verified,FALSE), COALESCE(u.vol_trained,FALSE),
		COALESCE(u.vol_training_date::text,''), COALESCE(u.vol_eligible,TRUE), COALESCE(u.vol_suspend_reason,''),
		COALESCE(vh.health_status,''),
		(SELECT COUNT(*) FROM deliveries d WHERE d.deliverer_id=u.id AND d.deliverer_type='volunteer' AND d.status='delivered'),
		(SELECT COUNT(*) FROM deliveries d WHERE d.deliverer_id=u.id AND d.deliverer_type='volunteer' AND d.effectiveness='pending'),
		(SELECT COUNT(*) FROM anomalies a WHERE a.responsible_user_id=u.id AND a.type='volunteer_delivery'),
		(SELECT COUNT(*) FROM anomalies a WHERE a.responsible_user_id=u.id AND a.type='volunteer_delivery' AND a.responsible_party='volunteer'),
		COALESCE((SELECT ROUND(AVG(f.rating),1) FROM feedbacks f JOIN deliveries dd ON dd.order_id=f.order_id
			WHERE dd.deliverer_id=u.id AND dd.deliverer_type='volunteer'),0)
		FROM users u
		LEFT JOIN volunteer_health vh ON vh.volunteer_id=u.id AND vh.check_date=CURRENT_DATE
		WHERE u.role='volunteer' ORDER BY u.id`)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询志愿者失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var id int
		var name, phone, org, training, suspendReason, health string
		var active, idv, trained, eligible bool
		var delivered, pending, anomalies, responsible int
		var rating float64
		rows.Scan(&id, &name, &phone, &active, &org, &idv, &trained, &training, &eligible, &suspendReason,
			&health, &delivered, &pending, &anomalies, &responsible, &rating)
		rate := 0.0
		if delivered > 0 {
			rate = float64(anomalies) * 100 / float64(delivered)
		}
		list = append(list, gin.H{
			"id": id, "name": name, "phone": phone, "active": active, "org": org,
			"id_verified": idv, "trained": trained, "training_date": training,
			"eligible": eligible, "suspend_reason": suspendReason, "today_health": health,
			"delivered_count": delivered, "pending_verify": pending,
			"anomaly_count": anomalies, "responsible_count": responsible,
			"anomaly_rate": rate, "avg_rating": rating,
		})
	}
	ok(c, list)
}

// 社区维护志愿者资质 / 培训 / 资格
func (s *Server) updateVolunteer(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Org           string `json:"org"`
		IDVerified    *bool  `json:"id_verified"`
		Trained       *bool  `json:"trained"`
		TrainingDate  string `json:"training_date"`
		Eligible      *bool  `json:"eligible"`
		SuspendReason string `json:"suspend_reason"`
		Active        *bool  `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	var role string
	if err := s.db.QueryRow(`SELECT role FROM users WHERE id=$1`, id).Scan(&role); err != nil || role != "volunteer" {
		fail(c, http.StatusNotFound, "志愿者不存在")
		return
	}
	if _, err := s.db.Exec(`UPDATE users SET vol_org=$2,
		vol_id_verified=COALESCE($3,vol_id_verified), vol_trained=COALESCE($4,vol_trained),
		vol_training_date=NULLIF($5,'')::date, vol_eligible=COALESCE($6,vol_eligible),
		vol_suspend_reason=$7, active=COALESCE($8,active) WHERE id=$1`,
		id, req.Org, req.IDVerified, req.Trained, req.TrainingDate, req.Eligible, req.SuspendReason, req.Active); err != nil {
		fail(c, http.StatusInternalServerError, "更新志愿者资质失败")
		return
	}
	ok(c, gin.H{"updated": true})
}

// 志愿者帮送取餐：先核验身份/培训/健康/资格与路线，再登记保温箱、批次、预计送达、与老人关系、知情同意
func (s *Server) volunteerPickup(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		ThermalBoxNo     string `json:"thermal_box_no" binding:"required"`
		RouteInfo        string `json:"route_info"`
		RelationToElder  string `json:"relation_to_elder" binding:"required"`
		InformedConsent  bool   `json:"informed_consent"`
		ConsentBy        string `json:"consent_by"`
		ETAMinutes        int    `json:"eta_minutes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写保温箱编号、与老人关系，并完成发车核验")
		return
	}
	if !req.InformedConsent || strings.TrimSpace(req.ConsentBy) == "" {
		fail(c, http.StatusBadRequest, "帮送前须取得老人本人或家属知情同意，并登记同意人")
		return
	}
	uid := c.GetInt("uid")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()

	info, err := volunteerDispatchCheck(tx, uid)
	if err != nil {
		fail(c, http.StatusNotFound, "志愿者档案不存在")
		return
	}
	if !info["dispatch_passed"].(bool) {
		fail(c, http.StatusBadRequest, "发车核验未通过："+strings.Join(info["block_reasons"].([]string), "；"))
		return
	}
	var orderID, elderID int
	var orderNo, status, dtype string
	var delivererID *int
	err = tx.QueryRow(`SELECT d.order_id, o.order_no, d.status, d.deliverer_id, o.elder_id, d.deliverer_type
		FROM deliveries d JOIN orders o ON o.id=d.order_id WHERE d.id=$1 FOR UPDATE OF d`, id).
		Scan(&orderID, &orderNo, &status, &delivererID, &elderID, &dtype)
	if err != nil {
		fail(c, http.StatusNotFound, "帮送任务不存在")
		return
	}
	if dtype != "volunteer" {
		fail(c, http.StatusForbidden, "该任务不是志愿者帮送任务")
		return
	}
	if status != "assigned" {
		fail(c, http.StatusBadRequest, "该任务已取餐或已结束")
		return
	}
	if delivererID != nil && *delivererID != uid {
		fail(c, http.StatusForbidden, "该任务已被其他志愿者接单")
		return
	}
	eta := time.Now().Add(time.Duration(etaMinutes(req.ETAMinutes)) * time.Minute)
	if _, err := tx.Exec(`UPDATE deliveries SET deliverer_id=$1, thermal_box_no=$2, route_info=$3, status='picked',
		pickup_time=now(), eta=$8, vol_org=$4, relation_to_elder=$5, informed_consent=TRUE, consent_by=$6,
		dispatch_checked=TRUE
		WHERE id=$7`,
		uid, req.ThermalBoxNo, req.RouteInfo, info["org"], req.RelationToElder, req.ConsentBy, id, eta); err != nil {
		fail(c, http.StatusInternalServerError, "取餐登记失败: "+err.Error())
		return
	}
	if _, err := tx.Exec(`UPDATE orders SET status='delivering', updated_at=now() WHERE id=$1`, orderID); err != nil {
		fail(c, http.StatusInternalServerError, "更新餐单状态失败")
		return
	}
	addOrderEvent(tx, orderID, uid, c.GetString("name"), "志愿者取餐帮送",
		"组织："+info["org"].(string)+"；保温箱 "+req.ThermalBoxNo+"；与老人关系："+req.RelationToElder+
			"；知情同意人："+req.ConsentBy+"；预计 "+itoa(etaMinutes(req.ETAMinutes))+" 分钟送达")
	notifyElderParties(tx, elderID, orderID, "志愿者已取餐", orderNo+" 志愿者已取餐正在帮送，预计约 30 分钟送达")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"picked": true, "dispatch": info})
}

// 依据老人认知状态与社区授权判定志愿者签收效力
// 老人本人/家属代签即有效；邻里见证须授权+知情同意且非严格对象；拍照留证一律待社区核实，不能仅凭志愿者自述核销
func volunteerSignEffect(signType, witnessName string, strict, authorized, consent bool) (string, string) {
	switch signType {
	case "elder":
		return "valid", "老人本人签收，签收有效"
	case "family":
		return "valid", "家属代签，签收有效"
	case "neighbor":
		switch {
		case strict:
			return "pending", "认知障碍/独居/行动不便严格对象，邻里见证须社区向老人本人核实"
		case !authorized:
			return "pending", "未经社区授权采用邻里见证，须社区核实"
		case !consent:
			return "pending", "缺少老人/家属知情同意，须社区核实"
		case strings.TrimSpace(witnessName) == "":
			return "pending", "邻里见证须登记见证人姓名，待社区核实"
		default:
			return "valid", "社区授权且老人/家属知情的邻里见证，签收有效"
		}
	case "photo":
		return "pending", "志愿者拍照留证不能仅凭自述完成核销，须社区核实老人实际收到"
	}
	return "pending", "签收方式待社区核实"
}

// 志愿者送达签收（效力区别于骑手）
func (s *Server) volunteerDeliver(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		SignType       string `json:"sign_type" binding:"required"`
		SignedByName   string `json:"signed_by_name" binding:"required"`
		SignPhotoURL   string `json:"sign_photo_url"`
		WitnessName    string `json:"witness_name"`
		WitnessPhone   string `json:"witness_phone"`
		KnockConfirmed bool   `json:"knock_confirmed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写签收方式与签收人")
		return
	}
	switch req.SignType {
	case "elder", "family", "neighbor", "photo":
	default:
		fail(c, http.StatusBadRequest, "志愿者签收方式须为本人/家属代签/邻里见证/拍照留证")
		return
	}
	if req.SignType == "photo" && req.SignPhotoURL == "" {
		fail(c, http.StatusBadRequest, "拍照留证必须上传送达照片")
		return
	}
	uid := c.GetInt("uid")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()

	var orderID, elderID, boxesIssued int
	var orderNo, elderName, status, dtype, consentBy, relation string
	var strict, consent bool
	var delivererID *int
	err = tx.QueryRow(`SELECT d.order_id, o.order_no, d.status, d.deliverer_id, o.elder_id, e.name,
		o.strict_mode, o.boxes_issued, d.deliverer_type, COALESCE(d.consent_by,''), d.relation_to_elder,
		COALESCE(d.informed_consent,FALSE)
		FROM deliveries d JOIN orders o ON o.id=d.order_id JOIN elders e ON e.id=o.elder_id
		WHERE d.id=$1 FOR UPDATE OF d`, id).
		Scan(&orderID, &orderNo, &status, &delivererID, &elderID, &elderName, &strict, &boxesIssued,
			&dtype, &consentBy, &relation, &consent)
	if err != nil {
		fail(c, http.StatusNotFound, "帮送任务不存在")
		return
	}
	if dtype != "volunteer" {
		fail(c, http.StatusForbidden, "该任务不是志愿者帮送任务")
		return
	}
	if status != "picked" {
		fail(c, http.StatusBadRequest, "请先取餐再签收")
		return
	}
	if delivererID == nil || *delivererID != uid {
		fail(c, http.StatusForbidden, "该任务不属于当前志愿者")
		return
	}
	var authorized bool
	tx.QueryRow(`SELECT COALESCE(proxy_sign_authorized,FALSE) FROM elders WHERE id=$1`, elderID).Scan(&authorized)

	eff, reason := volunteerSignEffect(req.SignType, req.WitnessName, strict, authorized, consent)
	newOrderStatus := "signed"
	basis := req.SignType
	anomalyID := 0
	if eff != "valid" {
		newOrderStatus = "verify_pending" // 待社区核实，暂不核销
	}
	if _, err := tx.Exec(`UPDATE deliveries SET status='delivered', delivered_time=now(), sign_photo_url=$1,
		sign_type=$2, signed_by_name=$3, knock_confirmed=$4, witness_name=$5, witness_phone=$6,
		effectiveness=$7, effectiveness_reason=$8 WHERE id=$9`,
		req.SignPhotoURL, req.SignType, req.SignedByName, req.KnockConfirmed, req.WitnessName, req.WitnessPhone,
		eff, reason, id); err != nil {
		fail(c, http.StatusInternalServerError, "签收登记失败")
		return
	}
	if _, err := tx.Exec(`UPDATE orders SET status=$2, sign_effectiveness=$3, sign_basis=$4, updated_at=now() WHERE id=$1`,
		orderID, newOrderStatus, eff, basis); err != nil {
		fail(c, http.StatusInternalServerError, "更新餐单状态失败")
		return
	}
	if boxesIssued > 0 {
		tx.Exec(`INSERT INTO box_records(order_id, elder_id, boxes_issued, boxes_returned, return_method, status)
			VALUES($1,$2,$3,0,'next_delivery','pending') ON CONFLICT (order_id) DO NOTHING`, orderID, elderID, boxesIssued)
	}
	addOrderEvent(tx, orderID, uid, c.GetString("name"), "志愿者帮送签收",
		"签收方式："+signBasisName(req.SignType)+"；签收人："+req.SignedByName+
			func() string {
				if req.SignType == "neighbor" && req.WitnessName != "" {
					return "；见证人：" + req.WitnessName
				}
				return ""
			}()+"；效力判定："+reason)
	if eff == "valid" {
		notifyElderParties(tx, elderID, orderID, "餐单已签收", orderNo+" 已由"+signBasisName(req.SignType)+"完成签收")
	} else {
		// 不能仅凭志愿者自述核销：生成签收待核实工单
		anomalyID, _ = createAnomaly(tx, orderID, elderID, "volunteer_delivery",
			"志愿者帮送餐单 "+orderNo+" 采用「"+signBasisName(req.SignType)+"」，效力待社区核实："+reason+
				"。核实前不纳入补贴核销。", uid)
		tx.Exec(`UPDATE anomalies SET responsible_party='' WHERE id=$1`, anomalyID)
		notify(tx, 0, "community", orderID, "帮送签收待核实",
			orderNo+"（老人「"+elderName+"」）"+signBasisName(req.SignType)+"待核实，请回访老人本人或同住人（工单 #"+itoa(anomalyID)+"）")
		notifyElderParties(tx, elderID, orderID, "餐单签收待社区核实", orderNo+"："+reason)
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"delivered": true, "effectiveness": eff, "effectiveness_reason": reason,
		"order_status": newOrderStatus, "anomaly_id": anomalyID})
}

// 志愿者上报帮送异常：洒漏 / 迟到 / 送错地址 / 老人否认收到；责任先不落到骑手
func (s *Server) volunteerReportException(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Subtype    string `json:"subtype" binding:"required"` // spill/late/wrong_address/not_received
		Note       string `json:"note"`
		FoodSafety bool   `json:"food_safety"`
		PhotoURL   string `json:"sign_photo_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请选择异常情形")
		return
	}
	subName := map[string]string{"spill": "餐品洒漏", "late": "配送迟到", "wrong_address": "送错地址", "not_received": "老人否认收到"}[req.Subtype]
	if subName == "" {
		fail(c, http.StatusBadRequest, "异常情形不合法")
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
	var orderNo, elderName, dtype string
	var delivererID *int
	err = tx.QueryRow(`SELECT d.order_id, o.order_no, o.elder_id, e.name, d.deliverer_type, d.deliverer_id
		FROM deliveries d JOIN orders o ON o.id=d.order_id JOIN elders e ON e.id=o.elder_id
		WHERE d.id=$1 FOR UPDATE OF d`, id).Scan(&orderID, &orderNo, &elderID, &elderName, &dtype, &delivererID)
	if err != nil {
		fail(c, http.StatusNotFound, "帮送任务不存在")
		return
	}
	if dtype != "volunteer" || delivererID == nil || *delivererID != uid {
		fail(c, http.StatusForbidden, "只能上报本人的志愿者帮送任务")
		return
	}
	tx.Exec(`UPDATE deliveries SET status='failed', anomaly_note=$2, sign_photo_url=COALESCE(NULLIF($3,''),sign_photo_url) WHERE id=$1`,
		id, "志愿者帮送异常·"+subName+"："+req.Note, req.PhotoURL)
	tx.Exec(`UPDATE orders SET status='exception', updated_at=now() WHERE id=$1`, orderID)
	desc := "志愿者帮送异常【" + subName + "】餐单 " + orderNo + "（老人「" + elderName + "」）。" + req.Note
	if req.FoodSafety {
		desc += "（涉及食品安全，需志愿者、厨房、社区各自记录责任）"
	}
	anID, _ := createAnomaly(tx, orderID, elderID, "volunteer_delivery", desc, uid)
	tx.Exec(`UPDATE anomalies SET food_safety=$2, responsible_user_id=$3 WHERE id=$1`, anID, req.FoodSafety, uid)
	addOrderEvent(tx, orderID, uid, c.GetString("name"), "志愿者帮送异常上报", subName+"。"+req.Note)
	notify(tx, 0, "community", orderID, "志愿者帮送异常："+subName,
		"请核实路线、取餐时间、送达照片与老人反馈后决定补送/退餐/重新签收（工单 #"+itoa(anID)+"）")
	if req.FoodSafety {
		notify(tx, 0, "kitchen", orderID, "食品安全协查", orderNo+" 涉及食品安全，请配合社区核查出餐与批次留样")
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"reported": true, "anomaly_id": anID})
}

// 社区核实帮送异常：核实路线/取餐时间/照片/老人反馈，划分志愿者/厨房/社区责任，决定补送/退餐/重新签收
func (s *Server) investigateAnomaly(c *gin.Context) {
	anomalyID := c.Param("id")
	var req struct {
		Investigation  string `json:"investigation" binding:"required"`
		Outcome        string `json:"outcome" binding:"required"` // redeliver/refund/resign/none
		Responsible    string `json:"responsible"`               // volunteer/kitchen/community/none
		FoodSafety     bool   `json:"food_safety"`
		ElderUnwell    bool   `json:"elder_unwell"`
		ResolutionNote string `json:"resolution_note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写核实记录与处理结论")
		return
	}
	validOutcome := map[string]bool{"redeliver": true, "refund": true, "resign": true, "none": true}
	if !validOutcome[req.Outcome] {
		fail(c, http.StatusBadRequest, "处理结论须为补送/退餐/重新签收/无责")
		return
	}
	if req.Responsible == "" {
		req.Responsible = "none"
	}
	validParty := map[string]bool{"volunteer": true, "kitchen": true, "community": true, "none": true, "rider": false}
	if !validParty[req.Responsible] {
		fail(c, http.StatusBadRequest, "责任方须为志愿者/厨房/社区/无责（不计骑手考核）")
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
	var aType, aStatus, orderNo string
	var payable float64
	err = tx.QueryRow(`SELECT a.type, a.status, COALESCE(a.order_id,0), COALESCE(a.elder_id,0),
		COALESCE(o.order_no,''), COALESCE(o.payable_amount,0)
		FROM anomalies a LEFT JOIN orders o ON o.id=a.order_id WHERE a.id=$1 FOR UPDATE OF a`, anomalyID).
		Scan(&aType, &aStatus, &orderID, &elderID, &orderNo, &payable)
	if err != nil {
		fail(c, http.StatusNotFound, "异常工单不存在")
		return
	}
	if aStatus == "resolved" {
		fail(c, http.StatusBadRequest, "工单已办结")
		return
	}
	outcomeName := map[string]string{"redeliver": "安排补送", "refund": "退餐退款", "resign": "重新签收", "none": "无责关闭"}[req.Outcome]
	partyName := map[string]string{"volunteer": "志愿者", "kitchen": "厨房", "community": "社区", "none": "无责"}[req.Responsible]
	if _, err := tx.Exec(`UPDATE anomalies SET status='resolved', responsible_party=$2, food_safety=$3,
		elder_unwell=$4, investigation=$5, outcome=$6, resolved_at=now(),
		resolution='社区核实：'||$5||'；责任划分：'||$7||'；处理：'||$8||COALESCE(' 补充：'||NULLIF($9,''),'')
		WHERE id=$1`,
		anomalyID, req.Responsible, req.FoodSafety, req.ElderUnwell, req.Investigation, req.Outcome,
		partyName, outcomeName, req.ResolutionNote); err != nil {
		fail(c, http.StatusInternalServerError, "保存核实结论失败")
		return
	}
	if orderID > 0 {
		switch req.Outcome {
		case "redeliver":
			tx.Exec(`UPDATE orders SET status='ready', sign_effectiveness='', updated_at=now() WHERE id=$1 AND status IN ('exception','verify_pending')`, orderID)
			tx.Exec(`UPDATE deliveries SET status='assigned', deliverer_id=NULL, pickup_time=NULL, delivered_time=NULL,
				sign_photo_url='', signed_by_name='', effectiveness='', effectiveness_reason='',
				witness_name='', witness_phone='', anomaly_note='' WHERE order_id=$1`, orderID)
			addOrderEvent(tx, orderID, uid, c.GetString("name"), "社区核实：安排补送", "责任："+partyName)
			notify(tx, 0, "volunteer", orderID, "补送任务", orderNo+" 经社区核实安排补送，请重新接单")
		case "refund":
			tx.Exec(`UPDATE orders SET status='refunded', refund_amount=payable_amount, sign_effectiveness='invalid', updated_at=now()
				WHERE id=$1 AND status IN ('exception','verify_pending','ready','preparing')`, orderID)
			tx.Exec(`UPDATE deliveries SET effectiveness='invalid', effectiveness_reason='社区核实后退餐' WHERE order_id=$1`, orderID)
			addOrderEvent(tx, orderID, uid, c.GetString("name"), "社区核实：退餐退款", "责任："+partyName)
			notify(tx, 0, "finance", orderID, "帮送异常退餐", orderNo+" 经社区核实退餐，补贴不核销")
		case "resign":
			tx.Exec(`UPDATE orders SET status='delivering', sign_effectiveness='pending', updated_at=now() WHERE id=$1`, orderID)
			tx.Exec(`UPDATE deliveries SET status='picked', effectiveness='pending',
				effectiveness_reason='社区要求重新签收' WHERE order_id=$1`, orderID)
			addOrderEvent(tx, orderID, uid, c.GetString("name"), "社区核实：重新签收", "责任："+partyName)
			notify(tx, 0, "volunteer", orderID, "重新签收", orderNo+" 请重新取得有效签收")
		default:
			addOrderEvent(tx, orderID, uid, c.GetString("name"), "社区核实完成", "无责："+req.Investigation)
		}
		notifyElderParties(tx, elderID, orderID, "帮送异常已核实", orderNo+"："+outcomeName+"，责任："+partyName)
	}

	// 志愿者考核联动：连续 2 次担责或未经培训即暂停帮送资格（不计入骑手考核）
	if req.Responsible == "volunteer" {
		// 责任志愿者优先取快照，其次当前餐单配送员（补送会清空配送员，故落库快照）
		var volID int
		if tx.QueryRow(`SELECT COALESCE(responsible_user_id,0) FROM anomalies WHERE id=$1`, anomalyID).Scan(&volID); volID == 0 && orderID > 0 {
			tx.QueryRow(`SELECT COALESCE(deliverer_id,0) FROM deliveries WHERE order_id=$1`, orderID).Scan(&volID)
		}
		if volID > 0 {
			if orderID > 0 {
				tx.Exec(`UPDATE anomalies SET responsible_user_id=$2 WHERE id=$1 AND COALESCE(responsible_user_id,0)=0`, anomalyID, volID)
			}
			var cnt int
			tx.QueryRow(`SELECT COUNT(*) FROM anomalies WHERE responsible_user_id=$1 AND type='volunteer_delivery'
				AND responsible_party='volunteer'`, volID).Scan(&cnt)
			var trained bool
			tx.QueryRow(`SELECT COALESCE(vol_trained,FALSE) FROM users WHERE id=$1`, volID).Scan(&trained)
			if cnt >= 2 || !trained {
				reason := "连续 " + itoa(cnt) + " 次帮送异常担责，暂停资格待复训"
				if !trained {
					reason = "未完成助餐培训且发生担责异常，暂停帮送资格"
				}
				tx.Exec(`UPDATE users SET vol_eligible=FALSE, vol_suspend_reason=$2 WHERE id=$1`, volID, reason)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"resolved": true, "outcome": req.Outcome, "responsible": req.Responsible})
}

// 老人本人/同住人否认收到：撤销有效签收、配送记核实无效、幂等创建异常、
// draft 核销立即排除、confirmed/archived 档案生成冲正依据（不静默改写历史）。
// 返回异常工单 ID、是否新建异常、是否生成冲正。
func applyReceiptDispute(tx *sql.Tx, orderID, uid int, operator, note string, elderUnwell bool) (int, bool, bool, error) {
	var elderID int
	var prevStatus, orderNo, dtype string
	var carrierID int
	if err := tx.QueryRow(`SELECT o.elder_id, o.status, o.order_no,
		COALESCE(d.deliverer_type,''), COALESCE(d.deliverer_id,0)
		FROM orders o LEFT JOIN deliveries d ON d.order_id=o.id WHERE o.id=$1 FOR UPDATE OF o`, orderID).
		Scan(&elderID, &prevStatus, &orderNo, &dtype, &carrierID); err != nil {
		return 0, false, false, err
	}

	alreadyInvalid := prevStatus == "exception"
	var alreadyInvalidEff bool
	tx.QueryRow(`SELECT COALESCE(sign_effectiveness,'')='invalid' FROM orders WHERE id=$1`, orderID).Scan(&alreadyInvalidEff)

	// 1) 撤销有效签收依据：订单转异常，签收效力置 invalid（保留原签收方式供追溯）
	tx.Exec(`UPDATE orders SET status='exception', sign_effectiveness='invalid', updated_at=now() WHERE id=$1`, orderID)
	tx.Exec(`UPDATE deliveries SET effectiveness='invalid', effectiveness_reason='老人本人/同住人否认收到，签收核实无效，撤销核销依据',
		community_verified_by=$2, community_verified_at=now() WHERE order_id=$1`, orderID, uid)
	addOrderEvent(tx, orderID, uid, operator, "签收核实无效",
		"老人本人/同住人否认收到或签收异常，撤销原签收依据并阻止核销："+note)

	// 2) 幂等创建/复用唯一未办结异常
	anomalyType := "volunteer_delivery"
	if dtype != "volunteer" {
		anomalyType = "other"
	}
	var anID int
	created := false
	err := tx.QueryRow(`SELECT id FROM anomalies WHERE order_id=$1 AND type=$2 AND status<>'resolved'
		ORDER BY id LIMIT 1`, orderID, anomalyType).Scan(&anID)
	if err == sql.ErrNoRows {
		var id2 int64
		if e2 := tx.QueryRow(`INSERT INTO anomalies(order_id, elder_id, type, priority, description, reported_by,
			responsible_user_id, elder_unwell) VALUES($1,$2,$3,'high',$4,$5,$6,$7) RETURNING id`,
			orderID, elderID, anomalyType,
			"老人本人/同住人否认收到或签收异常（餐单 "+orderNo+"）："+note+"，撤销原签收依据，待社区调查后补送/退餐/重新签收。",
			uid, func() interface{} { if carrierID > 0 { return carrierID }; return nil }(), elderUnwell).Scan(&id2); e2 != nil {
			return 0, false, false, e2
		}
		anID = int(id2)
		created = true
	} else if err != nil {
		return 0, false, false, err
	} else {
		tx.Exec(`UPDATE anomalies SET elder_unwell=$2, description=COALESCE(description,'')||' 追加回访：'||$3 WHERE id=$1`, anID, elderUnwell, note)
	}

	reversalGenerated := false
	if !(alreadyInvalid && alreadyInvalidEff) {
		var subsidy float64
		tx.QueryRow(`SELECT subsidy_amount FROM orders WHERE id=$1`, orderID).Scan(&subsidy)

		// 3a) draft 核销单：明细立即排除并重算汇总（下一次生成也会从订单重算排除）
		tx.Exec(`UPDATE reconciliation_items SET included=FALSE, sign_effectiveness='invalid',
			reason='老人本人/同住人否认收到，撤销签收依据，不发放补贴'
			WHERE order_id=$1 AND included=TRUE
			AND reconciliation_id IN (SELECT id FROM reconciliations WHERE status='draft')`, orderID)
		tx.Exec(`UPDATE reconciliations r SET
			signed_orders=(SELECT COUNT(*) FROM reconciliation_items WHERE reconciliation_id=r.id AND included),
			subsidy_total=(SELECT COALESCE(SUM(subsidy_amount),0) FROM reconciliation_items WHERE reconciliation_id=r.id AND included)
			WHERE r.status='draft' AND r.id IN (SELECT DISTINCT reconciliation_id FROM reconciliation_items WHERE order_id=$1)`, orderID)

		// 3b) confirmed/archived 档案：不改写历史，生成冲正依据
		var res sql.Result
		res, err = tx.Exec(`INSERT INTO reconciliation_reversals(reconciliation_id, order_id, month, subsidy_amount, reason, created_by)
			SELECT ri.reconciliation_id, $1, r.month, $2, $3, $4
			FROM reconciliation_items ri JOIN reconciliations r ON r.id=ri.reconciliation_id
			WHERE ri.order_id=$1 AND ri.included=TRUE AND r.status IN ('confirmed','archived')
			  AND NOT EXISTS (SELECT 1 FROM reconciliation_reversals x WHERE x.order_id=$1 AND x.reconciliation_id=ri.reconciliation_id)`,
			orderID, subsidy, "老人本人/同住人否认收到，冲正已计入补贴："+note, uid)
		if err != nil {
			return 0, false, false, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			reversalGenerated = true
		}
	}
	return anID, created, reversalGenerated, nil
}

// 社区核实签收效力（邻里见证/拍照留证）：回访老人本人或同住人后给出核实结论
func (s *Server) verifyVolunteerSign(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Effective      *bool  `json:"effective"` // 指针：可明确表达 false（老人否认收到）
		ConfirmerRole  string `json:"confirmer_role"` // elder/cohabitant
		ConfirmerName  string `json:"confirmer_name"`
		Method         string `json:"method"`
		TasteFeedback  string `json:"taste_feedback"`
		BodyDiscomfort bool   `json:"body_discomfort"`
		ReceiptDispute bool   `json:"receipt_dispute"`
		Note           string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请给出核实结论")
		return
	}
	if req.ConfirmerRole == "" {
		req.ConfirmerRole = "elder"
	}
	if req.Method == "" {
		req.Method = "phone"
	}
	// 结论：显式 effective=false 或勾选否认收到，均视为否认
	effective := req.Effective != nil && *req.Effective && !req.ReceiptDispute
	uid := c.GetInt("uid")
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var orderID, elderID int
	var orderNo string
	err = tx.QueryRow(`SELECT d.order_id, o.elder_id, o.order_no
		FROM deliveries d JOIN orders o ON o.id=d.order_id WHERE d.id=$1 FOR UPDATE OF d`, id).
		Scan(&orderID, &elderID, &orderNo)
	if err != nil {
		fail(c, http.StatusNotFound, "配送记录不存在")
		return
	}
	// 幂等：同一结论重复回访不重复异常/冲正
	if effective {
		// 记录老人本人/同住人回访
		if _, err := tx.Exec(`INSERT INTO elder_confirmations(order_id, elder_id, confirmer_role, confirmer_name, method,
			confirms_received, taste_feedback, body_discomfort, receipt_dispute, note, community_id)
			VALUES($1,$2,$3,$4,$5,TRUE,$6,$7,FALSE,$8,$9)`,
			orderID, elderID, req.ConfirmerRole, req.ConfirmerName, req.Method,
			req.TasteFeedback, req.BodyDiscomfort, req.Note, uid); err != nil {
			fail(c, http.StatusInternalServerError, "记录回访失败")
			return
		}
		tx.Exec(`UPDATE deliveries SET effectiveness='valid', effectiveness_reason='社区回访'||$2||'核实实际收到用餐',
			community_verified_by=$3, community_verified_at=now() WHERE id=$1`, id,
			map[string]string{"elder": "老人本人", "cohabitant": "同住人"}[req.ConfirmerRole], uid)
		tx.Exec(`UPDATE orders SET status='signed', sign_effectiveness='valid', sign_basis=COALESCE(NULLIF(sign_basis,''),'community'), updated_at=now() WHERE id=$1`, orderID)
		tx.Exec(`UPDATE anomalies SET status='resolved', responsible_party=COALESCE(NULLIF(responsible_party,''),'none'), investigation=$2, outcome='none',
			resolution='社区回访核实老人实际收到，签收有效，纳入核销', resolved_at=now()
			WHERE order_id=$1 AND type='volunteer_delivery' AND status<>'resolved'`, orderID, req.Note)
		addOrderEvent(tx, orderID, uid, c.GetString("name"), "社区核实签收有效",
			"回访"+map[string]string{"elder": "老人本人", "cohabitant": "同住人"}[req.ConfirmerRole]+"确认收到用餐")
	} else {
		// 否认收到：撤销有效签收、配送核实无效、幂等异常、核销回退/冲正
		if _, err := tx.Exec(`INSERT INTO elder_confirmations(order_id, elder_id, confirmer_role, confirmer_name, method,
			confirms_received, taste_feedback, body_discomfort, receipt_dispute, note, community_id)
			VALUES($1,$2,$3,$4,$5,FALSE,$6,$7,TRUE,$8,$9)`,
			orderID, elderID, req.ConfirmerRole, req.ConfirmerName, req.Method,
			req.TasteFeedback, req.BodyDiscomfort, req.Note, uid); err != nil {
			fail(c, http.StatusInternalServerError, "记录回访失败")
			return
		}
		_, created, reversal, err := applyReceiptDispute(tx, orderID, uid, c.GetString("name"), req.Note, req.BodyDiscomfort)
		if err != nil {
			fail(c, http.StatusInternalServerError, "签收回退失败: "+err.Error())
			return
		}
		if created {
			notify(tx, 0, "community", orderID, "签收待重新核实", orderNo+" 老人/同住人否认收到，已撤销签收并生成异常，请调查后补送/退餐/重新签收")
		}
		if reversal {
			notify(tx, 0, "finance", orderID, "财政核销冲正", orderNo+" 老人否认收到，已对已确认/归档档案生成冲正依据")
		}
	}
	if req.BodyDiscomfort {
		notify(tx, 0, "kitchen", orderID, "老人用餐后不适协查", orderNo+" 回访发现老人用餐后身体不适，请核查批次与食品安全")
		notify(tx, 0, "community", orderID, "老人身体不适预警", orderNo+" 老人用餐后不适，请跟进就医与关怀")
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"verified": true, "effective": effective})
}

// 家属代订餐单的老人本人/同住人回访（代订人不能代老人放弃权益）
func (s *Server) elderConfirmation(c *gin.Context) {
	orderID := atoi(c.Param("id"))
	var req struct {
		ConfirmerRole  string `json:"confirmer_role"`
		ConfirmerName  string `json:"confirmer_name" binding:"required"`
		Method         string `json:"method"`
		ConfirmsReceived bool `json:"confirms_received"`
		TasteFeedback  string `json:"taste_feedback"`
		BodyDiscomfort bool   `json:"body_discomfort"`
		ReceiptDispute bool   `json:"receipt_dispute"`
		Note           string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写回访对象与结果")
		return
	}
	if req.ConfirmerRole == "" {
		req.ConfirmerRole = "elder"
	}
	if req.ConfirmerRole != "elder" && req.ConfirmerRole != "cohabitant" {
		fail(c, http.StatusBadRequest, "回访对象须为老人本人或同住人")
		return
	}
	uid := c.GetInt("uid")
	// 结论：明确确认收到且未否认才算有效（家属代订不得代老人放弃权益）
	effective := req.ConfirmsReceived && !req.ReceiptDispute
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()
	var elderID int
	var orderNo, status string
	if err := tx.QueryRow(`SELECT elder_id, order_no, status FROM orders WHERE id=$1 FOR UPDATE`, orderID).
		Scan(&elderID, &orderNo, &status); err != nil {
		fail(c, http.StatusNotFound, "餐单不存在")
		return
	}
	if _, err := tx.Exec(`INSERT INTO elder_confirmations(order_id, elder_id, confirmer_role, confirmer_name, method,
		confirms_received, taste_feedback, body_discomfort, receipt_dispute, note, community_id)
		VALUES($1,$2,$3,$4,COALESCE(NULLIF($5,''),'phone'),$6,$7,$8,$9,$10,$11)`,
		orderID, elderID, req.ConfirmerRole, req.ConfirmerName, req.Method,
		effective, req.TasteFeedback, req.BodyDiscomfort, req.ReceiptDispute, req.Note, uid); err != nil {
		fail(c, http.StatusInternalServerError, "记录回访失败")
		return
	}
	addOrderEvent(tx, orderID, uid, c.GetString("name"), "回访老人本人/同住人",
		"确认收到："+map[bool]string{true: "是", false: "否"}[effective]+
			"；口味："+req.TasteFeedback+
			map[bool]string{true: "；老人身体不适", false: ""}[req.BodyDiscomfort]+
			map[bool]string{true: "；否认收到/签收异常", false: ""}[req.ReceiptDispute])

	if effective {
		// 待核实签收经本人/同住人确认后转为有效签收
		if status == "verify_pending" {
			tx.Exec(`UPDATE orders SET status='signed', sign_effectiveness='valid', sign_basis=COALESCE(NULLIF(sign_basis,''),'community'), updated_at=now() WHERE id=$1`, orderID)
			tx.Exec(`UPDATE deliveries SET effectiveness='valid', effectiveness_reason='回访老人本人确认收到',
				community_verified_by=$2, community_verified_at=now() WHERE order_id=$1`, orderID, uid)
			tx.Exec(`UPDATE anomalies SET status='resolved', resolution='回访老人本人确认实际收到用餐，签收有效', resolved_at=now()
				WHERE order_id=$1 AND type='volunteer_delivery' AND status<>'resolved'`, orderID)
		}
	} else {
		// 否认收到：待核实单、家属代订单或已签收单一律撤销有效签收依据并阻止核销
		_, created, reversal, err := applyReceiptDispute(tx, orderID, uid, c.GetString("name"), req.Note, req.BodyDiscomfort)
		if err != nil {
			fail(c, http.StatusInternalServerError, "签收回退失败: "+err.Error())
			return
		}
		if created {
			notify(tx, 0, "community", orderID, "签收待重新核实", orderNo+" 老人本人/同住人否认收到，已撤销签收并生成异常，请调查")
		}
		if reversal {
			notify(tx, 0, "finance", orderID, "财政核销冲正", orderNo+" 老人否认收到，已对已确认/归档档案生成冲正依据")
		}
	}
	if req.BodyDiscomfort {
		notify(tx, 0, "kitchen", orderID, "老人用餐后不适协查", orderNo+" 回访发现老人用餐后身体不适，请核查食品安全")
	}
	notifyElderParties(tx, elderID, orderID, "老人用餐回访已记录", orderNo+" 已回访老人本人/同住人")
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"recorded": true, "effective": effective})
}
