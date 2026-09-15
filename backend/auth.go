package main

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Claims struct {
	UID  int    `json:"uid"`
	Role string `json:"role"`
	Name string `json:"name"`
	jwt.RegisteredClaims
}

func (s *Server) signToken(uid int, role, name string) (string, error) {
	claims := Claims{
		UID: uid, Role: role, Name: name,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(72 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
}

func (s *Server) authRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			return
		}
		tokenStr := strings.TrimPrefix(h, "Bearer ")
		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(s.cfg.JWTSecret), nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已过期，请重新登录"})
			return
		}
		c.Set("uid", claims.UID)
		c.Set("role", claims.Role)
		c.Set("name", claims.Name)
		c.Next()
	}
}

func (s *Server) requireRole(roles ...string) gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *gin.Context) {
		role := c.GetString("role")
		if !allowed[role] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "当前角色无权执行此操作"})
			return
		}
		c.Next()
	}
}

func hashPassword(pw string) string {
	b, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b)
}

func (s *Server) login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请输入用户名和密码")
		return
	}
	var (
		id       int
		hash     string
		name     string
		role     string
		active   bool
	)
	err := s.db.QueryRow(`SELECT id, password_hash, name, role, active FROM users WHERE username=$1`, req.Username).
		Scan(&id, &hash, &name, &role, &active)
	if err == sql.ErrNoRows {
		fail(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "登录失败")
		return
	}
	if !active {
		fail(c, http.StatusForbidden, "账号已停用")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		fail(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	token, err := s.signToken(id, role, name)
	if err != nil {
		fail(c, http.StatusInternalServerError, "签发令牌失败")
		return
	}
	ok(c, gin.H{"token": token, "user": gin.H{"id": id, "username": req.Username, "name": name, "role": role}})
}

func (s *Server) me(c *gin.Context) {
	uid := c.GetInt("uid")
	var u struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
		Name     string `json:"name"`
		Phone    string `json:"phone"`
		Role     string `json:"role"`
	}
	err := s.db.QueryRow(`SELECT id, username, name, phone, role FROM users WHERE id=$1`, uid).
		Scan(&u.ID, &u.Username, &u.Name, &u.Phone, &u.Role)
	if err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	// 家属/老人账号附带关联老人
	elders := []gin.H{}
	rows, err := s.db.Query(`SELECT id, name FROM elders WHERE family_user_id=$1 OR user_id=$1`, uid)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int
			var name string
			rows.Scan(&id, &name)
			elders = append(elders, gin.H{"id": id, "name": name})
		}
	}
	ok(c, gin.H{"user": u, "linked_elders": elders})
}
