package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := loadConfig()
	db := connectDB(cfg)
	defer db.Close()
	migrate(db)
	if err := seed(db); err != nil {
		log.Fatalf("种子数据初始化失败: %v", err)
	}
	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		log.Fatalf("无法创建上传目录: %v", err)
	}

	s := &Server{db: db, cfg: cfg}
	r := gin.Default()
	r.MaxMultipartMemory = 8 << 20

	api := r.Group("/api")
	api.GET("/health", s.health)
	api.POST("/login", s.login)

	auth := api.Group("")
	auth.Use(s.authRequired())
	{
		auth.GET("/me", s.me)
		auth.GET("/dashboard", s.dashboard)
		auth.POST("/uploads", s.upload)

		auth.GET("/dishes", s.listDishes)
		auth.POST("/dishes", s.requireRole("admin", "kitchen"), s.createDish)
		auth.PUT("/dishes/:id", s.requireRole("admin", "kitchen"), s.updateDish)

		auth.GET("/elders", s.listElders)
		auth.POST("/elders", s.requireRole("admin", "community"), s.createElder)
		auth.GET("/elders/:id", s.getElder)
		auth.PUT("/elders/:id", s.requireRole("admin", "community"), s.updateElder)
		auth.POST("/elders/:id/subsidy", s.requireRole("admin", "community", "finance"), s.changeSubsidy)

		auth.GET("/orders", s.listOrders)
		auth.POST("/orders", s.requireRole("elder", "family", "community", "admin"), s.createOrder)
		auth.GET("/orders/:id", s.getOrder)
		auth.PUT("/orders/:id/items", s.requireRole("family", "community", "admin", "elder"), s.modifyOrder)
		auth.POST("/orders/:id/cancel", s.requireRole("family", "elder", "community", "admin"), s.cancelOrder)
		auth.POST("/orders/:id/pickup-confirm", s.requireRole("community", "admin"), s.confirmPickup)
		auth.POST("/orders/:id/feedback", s.requireRole("family", "elder", "community", "admin"), s.createFeedback)

		auth.GET("/kitchen/summary", s.requireRole("kitchen", "admin"), s.kitchenSummary)
		auth.GET("/kitchen/batches", s.requireRole("kitchen", "admin", "community"), s.listBatches)
		auth.POST("/kitchen/batches", s.requireRole("kitchen", "admin"), s.createBatch)
		auth.GET("/kitchen/batches/:id", s.requireRole("kitchen", "admin", "community"), s.getBatch)
		auth.POST("/kitchen/batches/:id/release", s.requireRole("kitchen", "admin"), s.releaseBatch)

		auth.GET("/delivery/tasks", s.requireRole("rider", "volunteer", "admin"), s.deliveryTasks)
		auth.POST("/delivery/:id/claim", s.requireRole("rider", "volunteer"), s.claimDelivery)
		auth.POST("/delivery/:id/pickup", s.requireRole("rider", "volunteer"), s.pickupDelivery)
		auth.POST("/delivery/:id/deliver", s.requireRole("rider", "volunteer"), s.completeDelivery)
		auth.POST("/delivery/:id/fail", s.requireRole("rider", "volunteer"), s.failDelivery)

		auth.GET("/anomalies", s.listAnomalies)
		auth.POST("/anomalies/:id/followups", s.requireRole("community", "admin"), s.createFollowUp)
		auth.POST("/anomalies/:id/resolve", s.requireRole("community", "admin"), s.resolveAnomaly)

		auth.GET("/boxes", s.listBoxRecords)
		auth.POST("/boxes/:id/return", s.requireRole("community", "rider", "volunteer", "admin"), s.returnBoxes)
		auth.POST("/boxes/:id/urge", s.requireRole("community", "admin"), s.urgeBoxReturn)

		auth.GET("/finance/reconciliations", s.requireRole("finance", "admin"), s.listReconciliations)
		auth.POST("/finance/reconciliations", s.requireRole("finance", "admin"), s.createReconciliation)
		auth.GET("/finance/reconciliations/:id", s.requireRole("finance", "admin"), s.getReconciliation)
		auth.POST("/finance/reconciliations/:id/confirm", s.requireRole("finance", "admin"), s.confirmReconciliation)
		auth.POST("/finance/reconciliations/:id/archive", s.requireRole("finance", "admin"), s.archiveReconciliation)
		auth.GET("/finance/subsidy-changes", s.requireRole("finance", "admin", "community"), s.listSubsidyChanges)

		auth.GET("/notifications", s.listNotifications)
		auth.GET("/notifications/unread-count", s.unreadCount)
		auth.POST("/notifications/read-all", s.readAllNotifications)

		auth.GET("/admin/users", s.requireRole("admin"), s.listUsers)
		auth.POST("/admin/users", s.requireRole("admin"), s.createUser)
		auth.PUT("/admin/users/:id", s.requireRole("admin"), s.updateUser)
	}

	r.Static("/uploads", cfg.UploadDir)
	// SPA 静态资源与回退
	r.Static("/assets", filepath.Join(cfg.WebDir, "assets"))
	r.NoRoute(func(c *gin.Context) {
		p := filepath.Join(cfg.WebDir, c.Request.URL.Path)
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			c.File(p)
			return
		}
		c.File(filepath.Join(cfg.WebDir, "index.html"))
	})

	log.Printf("服务启动于 :%s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}

type Server struct {
	db  *sql.DB
	cfg Config
}

func (s *Server) health(c *gin.Context) {
	if err := s.db.Ping(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false, "db": "down"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "db": "up", "service": "养老助餐配送餐盒回收与补贴核销平台"})
}
