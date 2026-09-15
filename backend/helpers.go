package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func ok(c *gin.Context, v interface{}) {
	c.JSON(http.StatusOK, gin.H{"data": v})
}

func fail(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"error": msg})
}

// 记录餐单状态流转事件（各方看到一致的时间线）
func addOrderEvent(tx *sql.Tx, orderID int, actorID int, actorName, action, detail string) {
	_, _ = tx.Exec(`INSERT INTO order_events(order_id, actor_id, actor_name, action, detail) VALUES($1,$2,$3,$4,$5)`,
		orderID, actorID, actorName, action, detail)
}

// 站内通知：userID>0 指定用户；role 非空按角色广播
func notify(tx *sql.Tx, userID int, role string, orderID int, title, content string) {
	var uid, oid interface{}
	if userID > 0 {
		uid = userID
	}
	if orderID > 0 {
		oid = orderID
	}
	_, _ = tx.Exec(`INSERT INTO notifications(user_id, role, order_id, title, content) VALUES($1,$2,$3,$4,$5)`,
		uid, role, oid, title, content)
}

// 通知与某老人相关的各方：绑定家属 + 社区
func notifyElderParties(tx *sql.Tx, elderID int, orderID int, title, content string) {
	var familyID sql.NullInt64
	_ = tx.QueryRow(`SELECT family_user_id FROM elders WHERE id=$1`, elderID).Scan(&familyID)
	if familyID.Valid {
		notify(tx, int(familyID.Int64), "", orderID, title, content)
	}
	notify(tx, 0, "community", orderID, title, content)
}

func orderNo(mealDate string, id int) string {
	d := mealDate
	if len(d) >= 10 {
		d = d[:10]
	}
	return fmt.Sprintf("M%s-%04d", replaceDash(d), id)
}

func replaceDash(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '-' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

func today() string {
	return time.Now().Format("2006-01-02")
}

func currentMonth() string {
	return time.Now().Format("2006-01")
}

// 创建异常工单；认知障碍/独居/行动不便老人自动升为高优先级
func createAnomaly(tx *sql.Tx, orderID, elderID int, atype, description string, reportedBy int) (int, error) {
	priority := "normal"
	if elderID > 0 {
		var cog, alone, mob bool
		if err := tx.QueryRow(`SELECT cognitive_impairment, living_alone, mobility_impaired FROM elders WHERE id=$1`, elderID).
			Scan(&cog, &alone, &mob); err == nil && (cog || alone || mob) {
			priority = "high"
		}
	}
	var oid, eid, rb interface{}
	if orderID > 0 {
		oid = orderID
	}
	if elderID > 0 {
		eid = elderID
	}
	if reportedBy > 0 {
		rb = reportedBy
	}
	var id int
	err := tx.QueryRow(`INSERT INTO anomalies(order_id, elder_id, type, priority, description, reported_by)
		VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, oid, eid, atype, priority, description, rb).Scan(&id)
	return id, err
}

// 餐单操作权限：家属/老人仅限其绑定老人的餐单，社区/管理员保持协同处置权限，
// 其余角色（厨房/骑手/志愿者/财政）无权操作餐单内容
func (s *Server) canOperateOrder(c *gin.Context, elderID int) bool {
	role := c.GetString("role")
	uid := c.GetInt("uid")
	switch role {
	case "community", "admin":
		return true
	case "family":
		var n int
		s.db.QueryRow(`SELECT COUNT(*) FROM elders WHERE id=$1 AND family_user_id=$2`, elderID, uid).Scan(&n)
		return n > 0
	case "elder":
		var n int
		s.db.QueryRow(`SELECT COUNT(*) FROM elders WHERE id=$1 AND user_id=$2`, elderID, uid).Scan(&n)
		return n > 0
	default:
		return false
	}
}

// 配送员角色与任务类型匹配：骑手↔rider，志愿者↔volunteer
func delivererTypeForRole(role string) string {
	if role == "volunteer" {
		return "volunteer"
	}
	return "rider"
}

func anomalyTypeName(t string) string {
	switch t {
	case "no_answer":
		return "老人未开门"
	case "box_not_returned":
		return "餐盒未回收"
	case "family_change":
		return "家属临时改餐"
	case "kitchen_shortage":
		return "厨房少做"
	case "rider_timeout":
		return "骑手超时"
	case "eligibility_changed":
		return "补贴资格变更"
	case "meal_unsuitable":
		return "饭菜不适合"
	default:
		return "其他异常"
	}
}
