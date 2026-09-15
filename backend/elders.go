package main

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ---------------- 菜品 ----------------

func (s *Server) listDishes(c *gin.Context) {
	rows, err := s.db.Query(`SELECT id, name, price, low_salt, low_sugar, softness, nutrition, allergens, holiday_only, available
		FROM dishes ORDER BY id`)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询菜品失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		var d struct {
			ID          int
			Name        string
			Price       float64
			LowSalt     bool
			LowSugar    bool
			Softness    string
			Nutrition   string
			Allergens   string
			HolidayOnly bool
			Available   bool
		}
		rows.Scan(&d.ID, &d.Name, &d.Price, &d.LowSalt, &d.LowSugar, &d.Softness, &d.Nutrition, &d.Allergens, &d.HolidayOnly, &d.Available)
		list = append(list, gin.H{
			"id": d.ID, "name": d.Name, "price": d.Price, "low_salt": d.LowSalt, "low_sugar": d.LowSugar,
			"softness": d.Softness, "nutrition": d.Nutrition, "allergens": d.Allergens,
			"holiday_only": d.HolidayOnly, "available": d.Available,
		})
	}
	ok(c, list)
}

func (s *Server) createDish(c *gin.Context) {
	var req struct {
		Name        string  `json:"name" binding:"required"`
		Price       float64 `json:"price" binding:"required"`
		LowSalt     bool    `json:"low_salt"`
		LowSugar    bool    `json:"low_sugar"`
		Softness    string  `json:"softness"`
		Nutrition   string  `json:"nutrition"`
		Allergens   string  `json:"allergens"`
		HolidayOnly bool    `json:"holiday_only"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	if req.Softness == "" {
		req.Softness = "normal"
	}
	var id int
	err := s.db.QueryRow(`INSERT INTO dishes(name, price, low_salt, low_sugar, softness, nutrition, allergens, holiday_only)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		req.Name, req.Price, req.LowSalt, req.LowSugar, req.Softness, req.Nutrition, req.Allergens, req.HolidayOnly).Scan(&id)
	if err != nil {
		fail(c, http.StatusBadRequest, "创建菜品失败：名称可能重复")
		return
	}
	ok(c, gin.H{"id": id})
}

func (s *Server) updateDish(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Price       float64 `json:"price"`
		LowSalt     bool    `json:"low_salt"`
		LowSugar    bool    `json:"low_sugar"`
		Softness    string  `json:"softness"`
		Nutrition   string  `json:"nutrition"`
		Allergens   string  `json:"allergens"`
		HolidayOnly bool    `json:"holiday_only"`
		Available   bool    `json:"available"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	_, err := s.db.Exec(`UPDATE dishes SET price=$1, low_salt=$2, low_sugar=$3, softness=$4, nutrition=$5, allergens=$6, holiday_only=$7, available=$8 WHERE id=$9`,
		req.Price, req.LowSalt, req.LowSugar, req.Softness, req.Nutrition, req.Allergens, req.HolidayOnly, req.Available, id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "更新菜品失败")
		return
	}
	ok(c, gin.H{"updated": true})
}

// ---------------- 老人档案 ----------------

func elderRowToMap(row interface{ Scan(...interface{}) error }) (gin.H, error) {
	var (
		e struct {
			ID, UserID, FamilyUserID                            int
			Name, IDCard, Gender, Phone, Address                string
			SubsidyLevel, Dietary, BoxReturnMethod              string
			EmergName, EmergPhone, CommunityNote                string
			SubsidyPerMeal                                      float64
			NeedKnock, Cog, Alone, Mob, Active                  bool
			BirthDate                                           sql.NullTime
		}
	)
	var userID, familyID sql.NullInt64
	err := row.Scan(&e.ID, &userID, &e.Name, &e.IDCard, &e.Gender, &e.BirthDate, &e.Phone, &e.Address,
		&e.SubsidyLevel, &e.SubsidyPerMeal, &e.Dietary, &e.NeedKnock, &e.BoxReturnMethod,
		&e.EmergName, &e.EmergPhone, &e.Cog, &e.Alone, &e.Mob, &familyID, &e.CommunityNote, &e.Active)
	if err != nil {
		return nil, err
	}
	birth := ""
	if e.BirthDate.Valid {
		birth = e.BirthDate.Time.Format("2006-01-02")
	}
	m := gin.H{
		"id": e.ID, "name": e.Name, "id_card": e.IDCard, "gender": e.Gender, "birth_date": birth,
		"phone": e.Phone, "address": e.Address, "subsidy_level": e.SubsidyLevel,
		"subsidy_per_meal": e.SubsidyPerMeal, "dietary_restrictions": e.Dietary,
		"need_knock_confirm": e.NeedKnock, "box_return_method": e.BoxReturnMethod,
		"emergency_contact_name": e.EmergName, "emergency_contact_phone": e.EmergPhone,
		"cognitive_impairment": e.Cog, "living_alone": e.Alone, "mobility_impaired": e.Mob,
		"community_note": e.CommunityNote, "active": e.Active,
		"strict_mode": e.Cog || e.Alone || e.Mob,
	}
	if userID.Valid {
		m["user_id"] = userID.Int64
	}
	if familyID.Valid {
		m["family_user_id"] = familyID.Int64
	}
	return m, nil
}

const elderCols = `id, user_id, name, id_card, gender, birth_date, phone, address, subsidy_level, subsidy_per_meal,
	dietary_restrictions, need_knock_confirm, box_return_method, emergency_contact_name, emergency_contact_phone,
	cognitive_impairment, living_alone, mobility_impaired, family_user_id, community_note, active`

func (s *Server) listElders(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	role := c.GetString("role")
	uid := c.GetInt("uid")

	var rows *sql.Rows
	var err error
	base := `SELECT ` + elderCols + ` FROM elders`
	switch {
	case role == "family":
		rows, err = s.db.Query(base+` WHERE family_user_id=$1 ORDER BY id`, uid)
	case role == "elder":
		rows, err = s.db.Query(base+` WHERE user_id=$1 ORDER BY id`, uid)
	case q != "":
		rows, err = s.db.Query(base+` WHERE name ILIKE $1 OR id_card ILIKE $1 OR address ILIKE $1 ORDER BY id`, "%"+q+"%")
	default:
		rows, err = s.db.Query(base + ` ORDER BY id`)
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询老人档案失败")
		return
	}
	defer rows.Close()
	list := []gin.H{}
	for rows.Next() {
		m, err := elderRowToMap(rows)
		if err == nil {
			list = append(list, m)
		}
	}
	ok(c, list)
}

func (s *Server) getElder(c *gin.Context) {
	id := c.Param("id")
	m, err := elderRowToMap(s.db.QueryRow(`SELECT `+elderCols+` FROM elders WHERE id=$1`, id))
	if err != nil {
		fail(c, http.StatusNotFound, "老人档案不存在")
		return
	}
	// 近期餐单
	orders := []gin.H{}
	rows, err := s.db.Query(`SELECT id, order_no, meal_date::text, meal_type, status, total_amount, subsidy_amount
		FROM orders WHERE elder_id=$1 ORDER BY meal_date DESC, id DESC LIMIT 20`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var total, sub float64
			var oID int
			var no, date, mt, st string
			rows.Scan(&oID, &no, &date, &mt, &st, &total, &sub)
			orders = append(orders, gin.H{"id": oID, "order_no": no, "meal_date": date[:10], "meal_type": mt,
				"status": st, "total_amount": total, "subsidy_amount": sub})
		}
	}
	m["recent_orders"] = orders
	// 补贴变更记录
	changes := []gin.H{}
	crows, err := s.db.Query(`SELECT sc.id, sc.old_level, sc.new_level, sc.old_amount, sc.new_amount, sc.reason,
		COALESCE(u.name,''), sc.created_at::text FROM subsidy_changes sc LEFT JOIN users u ON u.id=sc.changed_by
		WHERE sc.elder_id=$1 ORDER BY sc.id DESC LIMIT 10`, id)
	if err == nil {
		defer crows.Close()
		for crows.Next() {
			var cid int
			var ol, nl, reason, by, at string
			var oa, na float64
			crows.Scan(&cid, &ol, &nl, &oa, &na, &reason, &by, &at)
			changes = append(changes, gin.H{"id": cid, "old_level": ol, "new_level": nl, "old_amount": oa,
				"new_amount": na, "reason": reason, "changed_by": by, "created_at": at})
		}
	}
	m["subsidy_changes"] = changes
	ok(c, m)
}

type elderReq struct {
	Name                 string  `json:"name" binding:"required"`
	IDCard               string  `json:"id_card" binding:"required"`
	Gender               string  `json:"gender"`
	BirthDate            string  `json:"birth_date"`
	Phone                string  `json:"phone"`
	Address              string  `json:"address" binding:"required"`
	SubsidyLevel         string  `json:"subsidy_level"`
	SubsidyPerMeal       float64 `json:"subsidy_per_meal"`
	DietaryRestrictions  string  `json:"dietary_restrictions"`
	NeedKnockConfirm     bool    `json:"need_knock_confirm"`
	BoxReturnMethod      string  `json:"box_return_method"`
	EmergencyContactName string  `json:"emergency_contact_name"`
	EmergencyContactPhone string `json:"emergency_contact_phone"`
	CognitiveImpairment  bool    `json:"cognitive_impairment"`
	LivingAlone          bool    `json:"living_alone"`
	MobilityImpaired     bool    `json:"mobility_impaired"`
	FamilyUserID         *int    `json:"family_user_id"`
	CommunityNote        string  `json:"community_note"`
}

func (r *elderReq) normalize() {
	if r.Gender == "" {
		r.Gender = "女"
	}
	if r.SubsidyLevel == "" {
		r.SubsidyLevel = "none"
	}
	if r.BoxReturnMethod == "" {
		r.BoxReturnMethod = "next_delivery"
	}
}

func (s *Server) createElder(c *gin.Context) {
	var req elderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写老人姓名、身份证号和送餐地址")
		return
	}
	req.normalize()
	var id int
	err := s.db.QueryRow(`INSERT INTO elders(name, id_card, gender, birth_date, phone, address, subsidy_level, subsidy_per_meal,
		dietary_restrictions, need_knock_confirm, box_return_method, emergency_contact_name, emergency_contact_phone,
		cognitive_impairment, living_alone, mobility_impaired, family_user_id, community_note)
		VALUES($1,$2,$3,NULLIF($4,'')::date,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) RETURNING id`,
		req.Name, req.IDCard, req.Gender, req.BirthDate, req.Phone, req.Address, req.SubsidyLevel, req.SubsidyPerMeal,
		req.DietaryRestrictions, req.NeedKnockConfirm, req.BoxReturnMethod, req.EmergencyContactName, req.EmergencyContactPhone,
		req.CognitiveImpairment, req.LivingAlone, req.MobilityImpaired, req.FamilyUserID, req.CommunityNote).Scan(&id)
	if err != nil {
		fail(c, http.StatusBadRequest, "建档失败：身份证号可能已存在")
		return
	}
	ok(c, gin.H{"id": id})
}

func (s *Server) updateElder(c *gin.Context) {
	id := c.Param("id")
	var req elderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	req.normalize()
	_, err := s.db.Exec(`UPDATE elders SET name=$1, id_card=$2, gender=$3, birth_date=NULLIF($4,'')::date, phone=$5, address=$6,
		dietary_restrictions=$7, need_knock_confirm=$8, box_return_method=$9, emergency_contact_name=$10, emergency_contact_phone=$11,
		cognitive_impairment=$12, living_alone=$13, mobility_impaired=$14, family_user_id=$15, community_note=$16
		WHERE id=$17`,
		req.Name, req.IDCard, req.Gender, req.BirthDate, req.Phone, req.Address,
		req.DietaryRestrictions, req.NeedKnockConfirm, req.BoxReturnMethod, req.EmergencyContactName, req.EmergencyContactPhone,
		req.CognitiveImpairment, req.LivingAlone, req.MobilityImpaired, req.FamilyUserID, req.CommunityNote, id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "更新档案失败")
		return
	}
	ok(c, gin.H{"updated": true})
}

// 补贴资格变更：记录变更、重算在途餐单、生成异常事件并通知财政与社区
func (s *Server) changeSubsidy(c *gin.Context) {
	elderID := c.Param("id")
	var req struct {
		NewLevel  string  `json:"new_level" binding:"required"`
		NewAmount float64 `json:"new_amount"`
		Reason    string  `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写新补贴资格与变更原因")
		return
	}
	if req.NewLevel != "none" && req.NewLevel != "partial" && req.NewLevel != "full" {
		fail(c, http.StatusBadRequest, "补贴等级须为 none/partial/full")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	defer tx.Rollback()

	var oldLevel, elderName string
	var oldAmount float64
	err = tx.QueryRow(`SELECT subsidy_level, subsidy_per_meal, name FROM elders WHERE id=$1 FOR UPDATE`, elderID).
		Scan(&oldLevel, &oldAmount, &elderName)
	if err != nil {
		fail(c, http.StatusNotFound, "老人档案不存在")
		return
	}
	if _, err := tx.Exec(`UPDATE elders SET subsidy_level=$1, subsidy_per_meal=$2 WHERE id=$3`,
		req.NewLevel, req.NewAmount, elderID); err != nil {
		fail(c, http.StatusInternalServerError, "更新补贴资格失败")
		return
	}
	// 重算在途餐单（未出餐的）补贴快照
	rows, err := tx.Query(`SELECT id, total_amount, holiday_extra FROM orders
		WHERE elder_id=$1 AND status IN ('pending','confirmed','preparing') AND meal_date >= CURRENT_DATE`, elderID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "重算餐单补贴失败")
		return
	}
	type pendingOrder struct {
		id     int
		total  float64
		hextra float64
	}
	affected := []pendingOrder{}
	for rows.Next() {
		var p pendingOrder
		rows.Scan(&p.id, &p.total, &p.hextra)
		affected = append(affected, p)
	}
	rows.Close()
	for _, p := range affected {
		sub := req.NewAmount
		if sub > p.total {
			sub = p.total
		}
		sub += p.hextra
		if sub > p.total {
			sub = p.total
		}
		if _, err := tx.Exec(`UPDATE orders SET subsidy_amount=$1, payable_amount=$2, updated_at=now() WHERE id=$3`,
			sub, p.total-sub, p.id); err == nil {
			addOrderEvent(tx, p.id, c.GetInt("uid"), c.GetString("name"), "补贴资格变更",
				"补贴由 "+oldLevel+" 调整为 "+req.NewLevel+"，本单补贴重算")
		}
	}
	var changeID int
	err = tx.QueryRow(`INSERT INTO subsidy_changes(elder_id, old_level, new_level, old_amount, new_amount, reason, changed_by, affected_orders)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		elderID, oldLevel, req.NewLevel, oldAmount, req.NewAmount, req.Reason, c.GetInt("uid"), len(affected)).Scan(&changeID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "记录变更失败")
		return
	}
	// 资格变更作为异常事件进入统一处理视野，通知财政与社区
	anID, _ := createAnomaly(tx, 0, atoi(elderID), "eligibility_changed",
		"老人「"+elderName+"」补贴资格由 "+oldLevel+" 变更为 "+req.NewLevel+"（原因："+req.Reason+"），影响在途餐单 "+
			itoa(len(affected))+" 单，已自动重算补贴。", c.GetInt("uid"))
	notify(tx, 0, "finance", 0, "补贴资格变更", "老人「"+elderName+"」补贴资格变更，影响 "+itoa(len(affected))+" 单在途餐单")
	notify(tx, 0, "community", 0, "补贴资格变更", "老人「"+elderName+"」补贴资格变更，请关注异常工单 #"+itoa(anID))
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "提交失败")
		return
	}
	ok(c, gin.H{"change_id": changeID, "affected_orders": len(affected), "anomaly_id": anID})
}

func atoi(s string) int {
	n := 0
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			n = n*10 + int(ch-'0')
		}
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
