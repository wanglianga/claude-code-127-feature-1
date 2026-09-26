package main

import (
	"flag"
	"fmt"
	"os"

	"skirental/internal/config"
	"skirental/internal/db"
	"skirental/internal/seed"
)

func main() {
	reset := flag.Bool("reset", false, "清空并重建所有表后再写入种子数据")
	flag.Parse()

	cfg := config.Get()
	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "数据库连接失败:", err)
		os.Exit(1)
	}
	defer database.Close()

	if *reset {
		if _, err := database.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
			fmt.Fprintln(os.Stderr, "重置失败:", err)
			os.Exit(1)
		}
		fmt.Println("数据库已清空")
	}
	if err := db.Migrate(database); err != nil {
		fmt.Fprintln(os.Stderr, "迁移失败:", err)
		os.Exit(1)
	}
	if err := seed.Run(database); err != nil {
		fmt.Fprintln(os.Stderr, "种子写入失败:", err)
		os.Exit(1)
	}
}
