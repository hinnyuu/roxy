package review

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hinnyuu/roxy/internal/db"
	"github.com/hinnyuu/roxy/internal/domain"
)

func seed(t *testing.T) (*Service, *sql.DB, int64, int64) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	now := domain.Now()
	d.Exec(`INSERT INTO sources (name, path, kind, provider_type, enabled, created_at) VALUES ('s', '/tmp/s', 'mixed', 'dirscan', 1, ?)`, now)
	d.Exec(`INSERT INTO source_files (source_id, abs_path, size, mtime, kind, status, created_at, updated_at)
		VALUES (1, '/tmp/s/x.mkv', 1, '2026-01-01T00:00:00Z', 'video', 'placed', ?, ?)`, now, now)
	d.Exec(`INSERT INTO series (bgm_subject_id, title, created_at, updated_at) VALUES (1, 'T', ?, ?)`, now, now)
	d.Exec(`INSERT INTO placements (source_file_id, series_id, slot_type, decision_source, review_state, created_at, updated_at)
		VALUES (1, 1, 'episode', 'rule', 'pending_review', ?, ?)`, now, now)
	if _, err := d.Exec(`INSERT INTO review_cases (placement_id, reason, state, created_at) VALUES (1, '低置信', 'open', ?)`, now); err != nil {
		t.Fatal(err)
	}
	return New(d), d, 1, 1
}

func TestListOpen(t *testing.T) {
	s, _, _, _ := seed(t)
	items, err := s.List(context.Background(), "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SeriesTitle != "T" || items[0].Reason != "低置信" {
		t.Fatalf("items = %+v", items)
	}
	if items[0].FilePath != "/tmp/s/x.mkv" {
		t.Errorf("file path = %s", items[0].FilePath)
	}
}

func TestApprove(t *testing.T) {
	s, d, caseID, placementID := seed(t)
	if err := s.Approve(context.Background(), caseID); err != nil {
		t.Fatal(err)
	}
	var cs, ps string
	d.QueryRow(`SELECT state FROM review_cases WHERE id = ?`, caseID).Scan(&cs)
	d.QueryRow(`SELECT review_state FROM placements WHERE id = ?`, placementID).Scan(&ps)
	if cs != domain.ReviewApproved || ps != domain.PlacementApproved {
		t.Fatalf("case=%s placement=%s", cs, ps)
	}
	// 重复审批被状态机拒绝
	if err := s.Approve(context.Background(), caseID); err == nil {
		t.Error("double approve must fail")
	}
}

func TestReject(t *testing.T) {
	s, d, caseID, placementID := seed(t)
	if err := s.Reject(context.Background(), caseID); err != nil {
		t.Fatal(err)
	}
	var ps string
	d.QueryRow(`SELECT review_state FROM placements WHERE id = ?`, placementID).Scan(&ps)
	if ps != domain.PlacementRejected {
		t.Fatalf("placement = %s", ps)
	}
	// 驳回的工单不可再批准
	if err := s.Approve(context.Background(), caseID); err == nil {
		t.Error("approve after reject must fail")
	}
}

func TestNotFound(t *testing.T) {
	s, _, _, _ := seed(t)
	if err := s.Approve(context.Background(), 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}
