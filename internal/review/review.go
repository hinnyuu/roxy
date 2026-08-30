// Package review 审核队列骨架（M2）：工单列表 + 批准/驳回最小闭环。
// 附提示返工、改派、反馈笔记回流为 M3/M5 交付（docs/ROADMAP.md）。
// 设计见 docs/ARCHITECTURE.md §5 状态机与 §10。
package review

import (
	"context"
	"database/sql"
	"errors"

	"github.com/hinnyuu/roxy/internal/domain"
)

// Service 审核服务。
type Service struct{ db *sql.DB }

func New(db *sql.DB) *Service { return &Service{db: db} }

// Item 工单联查视图（文件、系列、槽位信息一次带回，UI 只读渲染用）。
type Item struct {
	CaseID      int64  `json:"case_id"`
	PlacementID int64  `json:"placement_id"`
	Reason      string `json:"reason"`
	State       string `json:"state"`
	CreatedAt   string `json:"created_at"`
	ResolvedAt  string `json:"resolved_at,omitempty"`

	FilePath    string   `json:"file_path"`
	SeriesID    int64    `json:"series_id"`
	SeriesTitle string   `json:"series_title"`
	SlotType    string   `json:"slot_type"`
	Season      *int     `json:"season,omitempty"`
	Episode     *float64 `json:"episode,omitempty"`
	EpisodeEnd  *float64 `json:"episode_end,omitempty"`
	VersionKey  string   `json:"version_key,omitempty"`
	Confidence  float64  `json:"confidence"`
	DecisionSrc string   `json:"decision_source"`
	Evidence    string   `json:"evidence,omitempty"`
	ManualLock  bool     `json:"manual_lock"`
}

// List 工单列表；state 为空返回全部（UI 默认 open）。
func (s *Service) List(ctx context.Context, state string) ([]Item, error) {
	q := `SELECT rc.id, rc.placement_id, IFNULL(rc.reason, ''), rc.state, rc.created_at, IFNULL(rc.resolved_at, ''),
		f.abs_path, p.series_id, se.title, p.slot_type, p.season, p.episode, p.episode_end,
		IFNULL(p.version_key, ''), IFNULL(p.confidence, 0), p.decision_source, IFNULL(p.evidence, ''), p.manual_lock
		FROM review_cases rc
		JOIN placements p ON p.id = rc.placement_id
		JOIN source_files f ON f.id = p.source_file_id
		JOIN series se ON se.id = p.series_id`
	var args []any
	if state != "" {
		q += ` WHERE rc.state = ?`
		args = append(args, state)
	}
	q += ` ORDER BY rc.id DESC LIMIT 500`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var (
			it        Item
			season    sql.NullInt64
			ep, epEnd sql.NullFloat64
			lock      int
		)
		if err := rows.Scan(&it.CaseID, &it.PlacementID, &it.Reason, &it.State, &it.CreatedAt, &it.ResolvedAt,
			&it.FilePath, &it.SeriesID, &it.SeriesTitle, &it.SlotType, &season, &ep, &epEnd,
			&it.VersionKey, &it.Confidence, &it.DecisionSrc, &it.Evidence, &lock); err != nil {
			return nil, err
		}
		if season.Valid {
			v := int(season.Int64)
			it.Season = &v
		}
		if ep.Valid {
			v := ep.Float64
			it.Episode = &v
		}
		if epEnd.Valid {
			v := epEnd.Float64
			it.EpisodeEnd = &v
		}
		it.ManualLock = lock != 0
		out = append(out, it)
	}
	return out, rows.Err()
}

// Approve 批准：工单 open→approved，placement pending_review→approved。
func (s *Service) Approve(ctx context.Context, caseID int64) error {
	return s.resolve(ctx, caseID, domain.ReviewApproved, domain.PlacementApproved)
}

// Reject 驳回：工单 open→rejected，placement pending_review→rejected。
func (s *Service) Reject(ctx context.Context, caseID int64) error {
	return s.resolve(ctx, caseID, domain.ReviewRejected, domain.PlacementRejected)
}

func (s *Service) resolve(ctx context.Context, caseID int64, toCase, toPlacement string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var (
		placementID int64
		state       string
	)
	err = tx.QueryRowContext(ctx,
		`SELECT placement_id, state FROM review_cases WHERE id = ?`, caseID).Scan(&placementID, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := domain.ReviewCaseTransitionOK(state, toCase); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE review_cases SET state = ?, resolved_at = ? WHERE id = ?`,
		toCase, domain.Now(), caseID); err != nil {
		return err
	}
	var pstate string
	if err := tx.QueryRowContext(ctx,
		`SELECT review_state FROM placements WHERE id = ?`, placementID).Scan(&pstate); err != nil {
		return err
	}
	if err := domain.PlacementTransitionOK(pstate, toPlacement); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE placements SET review_state = ?, updated_at = ? WHERE id = ?`,
		toPlacement, domain.Now(), placementID); err != nil {
		return err
	}
	return tx.Commit()
}

// ErrNotFound 工单不存在。
var ErrNotFound = errors.New("review: 工单不存在")
