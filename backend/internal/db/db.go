package db

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"

	_ "github.com/lib/pq"
)

//go:embed migrations_sql/*.sql
var migrationFS embed.FS

func Open(databaseURL string) (*sql.DB, error) {
	d, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	if err := d.Ping(); err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(10)
	return d, nil
}

// Migrate 启动时按文件名顺序执行未应用的迁移
func Migrate(d *sql.DB) error {
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("migrations_sql")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var exists bool
		if err := d.QueryRow(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		content, err := migrationFS.ReadFile("migrations_sql/" + name)
		if err != nil {
			return err
		}
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES($1)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
