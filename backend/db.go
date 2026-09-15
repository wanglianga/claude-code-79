package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed schema.sql
var schemaSQL string

func connectDB(cfg Config) *sql.DB {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName)
	var db *sql.DB
	var err error
	for i := 0; i < 30; i++ {
		db, err = sql.Open("pgx", dsn)
		if err == nil {
			err = db.Ping()
		}
		if err == nil {
			break
		}
		log.Printf("等待数据库就绪 (%d/30): %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("无法连接数据库: %v", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	return db
}

func migrate(db *sql.DB) {
	if _, err := db.Exec(schemaSQL); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	log.Println("数据库结构迁移完成")
}
