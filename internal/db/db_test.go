package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrateIdempotent(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("second migrate (idempotent replay): %v", err)
	}

	applied, err := AppliedMigrations(ctx, d)
	if err != nil {
		t.Fatalf("applied migrations: %v", err)
	}
	want := []string{"0001_initial.sql", "0002_add_vault_flag.sql"}
	if len(applied) != len(want) {
		t.Fatalf("applied migrations = %v, want %v", applied, want)
	}
	for i := range want {
		if applied[i] != want[i] {
			t.Fatalf("applied migrations = %v, want %v", applied, want)
		}
	}
}

func TestVaultColumnPresent(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var n int
	err = d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('placements') WHERE name='vault'`).Scan(&n)
	if err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	if n != 1 {
		t.Error("placements.vault column missing (migration 0002)")
	}
}

func TestCoreTablesExist(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tables := []string{
		"users", "sources", "source_files", "series", "series_aliases",
		"placements", "ledger", "review_cases", "feedback_notes",
		"llm_logs", "search_cache", "tasks", "settings",
		"bgm_meta", "bgm_subjects", "bgm_episodes", "bgm_relations",
	}
	for _, tbl := range tables {
		var n int
		err := d.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n)
		if err != nil {
			t.Fatalf("query sqlite_master for %s: %v", tbl, err)
		}
		if n != 1 {
			t.Errorf("table %s missing", tbl)
		}
	}
}

func TestFTSIndexWorks(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := d.ExecContext(ctx,
		`INSERT INTO bgm_subjects (id, type, name, name_cn) VALUES (1, 2, 'Mushoku Tensei', '无职转生')`); err != nil {
		t.Fatalf("insert subject: %v", err)
	}

	var id int
	err = d.QueryRowContext(ctx,
		`SELECT rowid FROM bgm_subjects_fts WHERE bgm_subjects_fts MATCH ?`, "无职转生").Scan(&id)
	if err != nil {
		t.Fatalf("fts match: %v", err)
	}
	if id != 1 {
		t.Fatalf("expected fts hit on subject 1, got %d", id)
	}
}
