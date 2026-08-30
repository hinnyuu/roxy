// Package scanner 源发现（docs/ARCHITECTURE.md §3 Scanner 层；D-025）。
// SourceProvider 接口从第一天存在：v1 DirScanProvider，v2 增加下载客户端
// provider 为纯增量。扫描只读源目录（零破坏，AGENTS.md 硬性规则 3）。
package scanner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hinnyuu/roxy/internal/domain"
)

// ErrNotEmpty 源下仍有登记文件时禁止删除（M2 语义，级联清理由 M4 台账驱动）。
var ErrNotEmpty = errors.New("source 仍有登记文件，不可删除")

// Store sources / source_files 表的数据访问。
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) CreateSource(ctx context.Context, src *domain.Source) error {
	src.CreatedAt = domain.Now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sources (name, path, kind, provider_type, provider_config, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		src.Name, src.Path, src.Kind, src.ProviderType, src.ProviderConfig, b2i(src.Enabled), src.CreatedAt)
	if err != nil {
		return err
	}
	src.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdateSource(ctx context.Context, src *domain.Source) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE sources SET name = ?, path = ?, kind = ?, enabled = ? WHERE id = ?`,
		src.Name, src.Path, src.Kind, b2i(src.Enabled), src.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteSource(ctx context.Context, id int64) error {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM source_files WHERE source_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrNotEmpty
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM sources WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) GetSource(ctx context.Context, id int64) (*domain.Source, error) {
	return scanSource(s.db.QueryRowContext(ctx, sourceCols+` WHERE id = ?`, id))
}

func (s *Store) ListSources(ctx context.Context) ([]domain.Source, error) {
	rows, err := s.db.QueryContext(ctx, sourceCols+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Source
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *src)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

const sourceCols = `SELECT id, name, path, kind, provider_type, provider_config, enabled, created_at FROM sources`

func scanSource(rs rowScanner) (*domain.Source, error) {
	var (
		src     domain.Source
		pc      sql.NullString
		enabled int
	)
	if err := rs.Scan(&src.ID, &src.Name, &src.Path, &src.Kind, &src.ProviderType, &pc, &enabled, &src.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("scan source: %w", err)
	}
	src.ProviderConfig = pc.String
	src.Enabled = enabled != 0
	return &src, nil
}

// UpsertFile 登记/更新扫描到的文件；size/mtime 变化时重置状态为 new 并返回 changed。
func (s *Store) UpsertFile(ctx context.Context, sourceID int64, absPath string, size int64, mtime time.Time) (id int64, changed bool, err error) {
	mtimeStr := mtime.UTC().Format(time.RFC3339)
	now := domain.Now()
	var curSize int64
	var curMTime string
	var curID int64
	qerr := s.db.QueryRowContext(ctx,
		`SELECT id, size, mtime FROM source_files WHERE abs_path = ?`, absPath).Scan(&curID, &curSize, &curMTime)
	switch {
	case errors.Is(qerr, sql.ErrNoRows):
		res, ierr := s.db.ExecContext(ctx,
			`INSERT INTO source_files (source_id, abs_path, size, mtime, kind, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 'unknown', ?, ?, ?)`,
			sourceID, absPath, size, mtimeStr, domain.SourceFileNew, now, now)
		if ierr != nil {
			return 0, false, ierr
		}
		id, _ = res.LastInsertId()
		return id, true, nil
	case qerr != nil:
		return 0, false, qerr
	}
	if curSize == size && curMTime == mtimeStr {
		return curID, false, nil
	}
	if _, uerr := s.db.ExecContext(ctx,
		`UPDATE source_files SET size = ?, mtime = ?, status = ?, parse_result = NULL, updated_at = ? WHERE id = ?`,
		size, mtimeStr, domain.SourceFileNew, now, curID); uerr != nil {
		return 0, false, uerr
	}
	return curID, true, nil
}

// ExistingFiles 返回某源已登记文件（abs_path → id/size/mtime）。
func (s *Store) ExistingFiles(ctx context.Context, sourceID int64) (map[string]fileRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, abs_path, size, mtime FROM source_files WHERE source_id = ?`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]fileRow{}
	for rows.Next() {
		var f fileRow
		if err := rows.Scan(&f.id, &f.absPath, &f.size, &f.mtime); err != nil {
			return nil, err
		}
		out[f.absPath] = f
	}
	return out, rows.Err()
}

type fileRow struct {
	id      int64
	absPath string
	size    int64
	mtime   string
}

// ForgetFile 移除登记：仅当无 placement 引用（有引用时保留给 M4 漂移巡检）。
// 返回 removed=true 表示行已删除。
func (s *Store) ForgetFile(ctx context.Context, fileID int64) (bool, error) {
	var has bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM placements WHERE source_file_id = ?)`, fileID).Scan(&has); err != nil {
		return false, err
	}
	if has {
		return false, nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM source_files WHERE id = ?`, fileID)
	return err == nil, err
}

// UpdateFileStatus 按状态机校验后更新 source_files.status。
func (s *Store) UpdateFileStatus(ctx context.Context, fileID int64, from, to string) error {
	if err := domain.SourceFileTransitionOK(from, to); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE source_files SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		to, domain.Now(), fileID, from)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("source_files %d 状态已非 %s", fileID, from)
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
