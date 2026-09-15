package main

import (
	"os"
)

type Config struct {
	Port      string
	DBHost    string
	DBPort    string
	DBUser    string
	DBPass    string
	DBName    string
	JWTSecret string
	UploadDir string
	WebDir    string
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadConfig() Config {
	return Config{
		Port:      envOr("PORT", "8080"),
		DBHost:    envOr("DB_HOST", "db"),
		DBPort:    envOr("DB_PORT", "5432"),
		DBUser:    envOr("DB_USER", "meal"),
		DBPass:    envOr("DB_PASSWORD", "meal_secret"),
		DBName:    envOr("DB_NAME", "mealdb"),
		JWTSecret: envOr("JWT_SECRET", "dev-secret-change-me"),
		UploadDir: envOr("UPLOAD_DIR", "./uploads"),
		WebDir:    envOr("WEB_DIR", "./web"),
	}
}
