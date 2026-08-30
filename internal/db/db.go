package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	// WAL 允许并发读；长事务（如 dump 导入）期间进度上报与 API 读取需要其他连接，
	// 写写冲突由 busy_timeout 兜底。单 worker 任务运行器保证写重任务串行。
	d.SetMaxOpenConns(4)
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, fmt.Errorf("ping sqlite %s: %w", path, err)
	}
	return d, nil
}

func Migrate(ctx context.Context, d *sql.DB) error {
	if _, err := d.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		id         INTEGER PRIMARY KEY,
		name       TEXT NOT NULL UNIQUE,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := isApplied(ctx, d, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`,
			name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}

func isApplied(ctx context.Context, d *sql.DB, name string) (bool, error) {
	var n int
	err := d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("query schema_migrations for %s: %w", name, err)
	}
	return n > 0, nil
}

func AppliedMigrations(ctx context.Context, d *sql.DB) ([]string, error) {
	rows, err := d.QueryContext(ctx, `SELECT name FROM schema_migrations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
