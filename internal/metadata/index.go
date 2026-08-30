package metadata

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/hinnyuu/roxy/internal/domain"
)

// Index 本地 Bangumi 索引检索（D-022）。中文策略两级（§8.3）：
// FTS 前缀查询为主，LIKE 子串兜底；NSFW 条目不进候选。
type Index struct{ db *sql.DB }

func NewIndex(db *sql.DB) *Index { return &Index{db: db} }

// Hit 检索命中。
type Hit struct {
	ID       int64
	Name     string
	NameCn   string
	Platform string
	Date     string
	Score    float64
}

// Subject 按 ID 取条目。
func (x *Index) Subject(ctx context.Context, id int64) (*domain.BgmSubject, error) {
	var (
		s        domain.BgmSubject
		nameCn   sql.NullString
		platform sql.NullString
		date     sql.NullString
		score    sql.NullFloat64
		rank     sql.NullInt64
		summary  sql.NullString
		nsfw     int
	)
	err := x.db.QueryRowContext(ctx,
		`SELECT id, type, name, name_cn, platform, date, score, rank, nsfw, summary
		 FROM bgm_subjects WHERE id = ?`, id).
		Scan(&s.ID, &s.Type, &s.Name, &nameCn, &platform, &date, &score, &rank, &nsfw, &summary)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.NameCn, s.Platform, s.Date = nameCn.String, platform.String, date.String
	s.Score, s.Rank, s.NSFW = score.Float64, int(rank.Int64), nsfw != 0
	s.Summary = summary.String
	return &s, nil
}

// Episodes 某条目的章节列表（按 ep_type, sort 排序）。
func (x *Index) Episodes(ctx context.Context, subjectID int64) ([]domain.BgmEpisode, error) {
	rows, err := x.db.QueryContext(ctx,
		`SELECT id, subject_id, name, name_cn, sort, ep_type, airdate
		 FROM bgm_episodes WHERE subject_id = ? ORDER BY ep_type, sort`, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BgmEpisode
	for rows.Next() {
		var (
			e      domain.BgmEpisode
			name   sql.NullString
			nameCn sql.NullString
			air    sql.NullString
		)
		if err := rows.Scan(&e.ID, &e.SubjectID, &name, &nameCn, &e.Sort, &e.EPType, &air); err != nil {
			return nil, err
		}
		e.Name, e.NameCn, e.Airdate = name.String, nameCn.String, air.String
		out = append(out, e)
	}
	return out, rows.Err()
}

// Search 候选检索：FTS 前缀为主、LIKE 子串兜底。limit<=0 用 20。
func (x *Index) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	if limit <= 0 {
		limit = 20
	}
	q := domain.NormalizeKey(query)
	if q == "" {
		return nil, nil
	}
	tokens := strings.Fields(q)
	if len(tokens) > 0 {
		parts := make([]string, 0, len(tokens))
		for _, t := range tokens {
			parts = append(parts, `"`+strings.ReplaceAll(t, `"`, `""`)+`"*`)
		}
		expr := strings.Join(parts, " ")
		hits, err := x.queryHits(ctx,
			`SELECT bgm_subjects_fts.rowid, s.name, s.name_cn, s.platform, s.date, s.score
			 FROM bgm_subjects_fts JOIN bgm_subjects s ON s.id = bgm_subjects_fts.rowid
			 WHERE bgm_subjects_fts MATCH ? AND s.nsfw = 0
			 ORDER BY bgm_subjects_fts.rank LIMIT ?`, expr, limit)
		if err != nil {
			return nil, err
		}
		if len(hits) > 0 {
			return hits, nil
		}
	}
	like := "%" + escapeLike(strings.ReplaceAll(q, " ", "")) + "%"
	return x.queryHits(ctx,
		`SELECT id, name, name_cn, platform, date, score
		 FROM bgm_subjects
		 WHERE nsfw = 0 AND (REPLACE(LOWER(name), ' ', '') LIKE ? ESCAPE '\'
			OR REPLACE(IFNULL(LOWER(name_cn), ''), ' ', '') LIKE ? ESCAPE '\')
		 ORDER BY score DESC LIMIT ?`, like, like, limit)
}

func (x *Index) queryHits(ctx context.Context, q string, args ...any) ([]Hit, error) {
	rows, err := x.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var (
			h      Hit
			nameCn sql.NullString
			plat   sql.NullString
			date   sql.NullString
			score  sql.NullFloat64
		)
		if err := rows.Scan(&h.ID, &h.Name, &nameCn, &plat, &date, &score); err != nil {
			return nil, err
		}
		h.NameCn, h.Platform, h.Date, h.Score = nameCn.String, plat.String, date.String, score.Float64
		out = append(out, h)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
