package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"skirental/internal/api"
	"skirental/internal/config"
	"skirental/internal/db"
)

func main() {
	cfg := config.Get()
	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	defer database.Close()
	if err := db.Migrate(database); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	handler := api.NewServer(database, cfg)

	mux := http.NewServeMux()
	mux.Handle("/api/", handler)
	mux.Handle("/uploads/", handler)

	// 前端静态资源（构建后存在则托管，支持 SPA 回退）
	dist := cfg.StaticDir
	if fi, err := os.Stat(filepath.Join(dist, "index.html")); err == nil && !fi.IsDir() {
		fs := http.FileServer(http.Dir(dist))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			p := filepath.Join(dist, filepath.Clean(r.URL.Path))
			if r.URL.Path != "/" {
				if _, err := os.Stat(p); err != nil {
					r.URL.Path = "/"
				}
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			fs.ServeHTTP(w, r)
		})
		log.Printf("前端静态资源目录: %s", dist)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				handler.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte("滑雪场雪具租赁平台 API 已启动（前端尚未构建）。接口前缀 /api"))
		})
	}

	addr := ":" + cfg.Port
	log.Printf("服务器监听 http://localhost%s", addr)
	if err := http.ListenAndServe(addr, withCORS(mux)); err != nil {
		log.Fatal(err)
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
