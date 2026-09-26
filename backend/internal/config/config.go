package config

import (
	"os"
)

type Config struct {
	Port        string
	DatabaseURL string
	JWTSecret   string
	UploadDir   string
	StaticDir   string
}

func Get() Config {
	c := Config{
		Port:        env("PORT", "8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://postgres@127.0.0.1:5433/skirental?sslmode=disable"),
		JWTSecret:   env("JWT_SECRET", "ski-rental-dev-secret"),
		UploadDir:   env("UPLOAD_DIR", "./data/uploads"),
		StaticDir:   env("STATIC_DIR", "../frontend/dist"),
	}
	return c
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
