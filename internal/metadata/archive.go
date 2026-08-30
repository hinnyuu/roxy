package metadata

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hinnyuu/roxy/internal/domain"
)

const (
	latestJSONURL   = "https://raw.githubusercontent.com/bangumi/Archive/master/aux/latest.json"
	memberSubjects  = "subject.jsonlines"
	memberEpisodes  = "episode.jsonlines"
	memberRelations = "subject-relations.jsonlines"
)

// Importer Archive dump 导入器（D-022）：下载/校验 zip → 流式两趟过滤
// type=2 动画条目 → bgm_subjects/bgm_episodes/bgm_relations + FTS5。
type Importer struct {
	db      *sql.DB
	httpc   *http.Client
	agent   string
	staging string // DATA_DIR：下载 zip 的中转目录
}

func NewImporter(db *sql.DB, stagingDir, userAgent string) *Importer {
	return &Importer{
		db:      db,
		httpc:   &http.Client{Timeout: 30 * time.Minute},
		agent:   userAgent,
		staging: stagingDir,
	}
}

// Report 进度回调（写入 tasks.progress）。
type Report func(progressJSON string)

type ImportStats struct {
	Version   string `json:"version"`
	Subjects  int    `json:"subjects"`
	Episodes  int    `json:"episodes"`
	Relations int    `json:"relations"`
}

type latestEntry struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

// Import localPath 为空 → 应用内下载最新 dump（sha256 校验，导入后删除）；
// 否则从本地 zip 导入（不动用户文件）。
func (im *Importer) Import(ctx context.Context, localPath string, report Report) (*ImportStats, error) {
	if report == nil {
		report = func(string) {}
	}
	zipPath, version, digest, sourceURL, downloaded, err := im.resolveSource(ctx, localPath, report)
	if err != nil {
		return nil, err
	}
	if downloaded {
		defer os.Remove(zipPath)
	}

	f, err := os.Open(zipPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("open dump zip: %w", err)
	}
	report(`{"phase":"verify"}`)
	if digest != "" {
		if err := verifySHA(zipPath, digest); err != nil {
			return nil, err
		}
	}

	members := map[string]*zip.File{}
	for _, mf := range zr.File {
		members[filepath.Base(mf.Name)] = mf
	}
	for _, want := range []string{memberSubjects, memberEpisodes, memberRelations} {
		if _, ok := members[want]; !ok {
			return nil, fmt.Errorf("dump zip 缺少成员 %s", want)
		}
	}

	stats := &ImportStats{Version: version}

	// 分段提交（每阶段一个事务）：避免长写事务横跨进度上报，
	// 且中断后的半成品索引会在下次刷新整体重建（派生数据，可自愈）。

	// 趟 1：清空 + 动画条目（type=2）
	tx, err := im.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// bgm_subjects 的 ad 触发器同步清理 FTS（外部内容表禁止直接 DELETE）。
	for _, tbl := range []string{"bgm_relations", "bgm_episodes", "bgm_subjects"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+tbl); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	animeIDs, err := importSubjects(ctx, tx, members[memberSubjects], stats)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	stats.Subjects = len(animeIDs)
	if err := upsertMeta(ctx, tx, "dump_version", version); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	report(fmt.Sprintf(`{"phase":"episodes","subjects":%d}`, stats.Subjects))

	// 趟 2：章节（仅动画条目）
	tx, err = im.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	epCount, err := importEpisodes(ctx, tx, members[memberEpisodes], animeIDs)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	stats.Episodes = epCount
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	report(fmt.Sprintf(`{"phase":"relations","episodes":%d}`, stats.Episodes))

	// 趟 3：关联（任一端为动画即保留，M3 franchise 遍历用）+ 元信息
	tx, err = im.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	relCount, err := importRelations(ctx, tx, members[memberRelations], animeIDs)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	stats.Relations = relCount
	if err := upsertMeta(ctx, tx, "imported_at", domain.Now()); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := upsertMeta(ctx, tx, "source_url", sourceURL); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return stats, nil
}

func (im *Importer) resolveSource(ctx context.Context, localPath string, report Report) (zipPath, version, digest, sourceURL string, downloaded bool, err error) {
	if localPath != "" {
		abs, aerr := filepath.Abs(localPath)
		if aerr != nil {
			return "", "", "", "", false, aerr
		}
		if _, serr := os.Stat(abs); serr != nil {
			return "", "", "", "", false, fmt.Errorf("本地 dump 不可读: %w", serr)
		}
		return abs, filepath.Base(abs), "", "local:" + abs, false, nil
	}
	report(`{"phase":"resolve"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestJSONURL, nil)
	if err != nil {
		return "", "", "", "", false, err
	}
	req.Header.Set("User-Agent", im.agent)
	resp, err := im.httpc.Do(req)
	if err != nil {
		return "", "", "", "", false, fmt.Errorf("解析 latest.json: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", "", "", false, fmt.Errorf("latest.json HTTP %d", resp.StatusCode)
	}
	var latest latestEntry
	if err := json.NewDecoder(resp.Body).Decode(&latest); err != nil {
		return "", "", "", "", false, err
	}
	if latest.BrowserDownloadURL == "" {
		return "", "", "", "", false, errors.New("latest.json 缺少下载地址")
	}
	if err := os.MkdirAll(im.staging, 0o755); err != nil {
		return "", "", "", "", false, err
	}
	zipPath = filepath.Join(im.staging, latest.Name)
	tmp := zipPath + ".tmp"
	report(fmt.Sprintf(`{"phase":"download","target":%q}`, latest.Name))
	if err := download(ctx, im.httpc, im.agent, latest.BrowserDownloadURL, tmp); err != nil {
		os.Remove(tmp)
		return "", "", "", "", false, err
	}
	if err := os.Rename(tmp, zipPath); err != nil {
		return "", "", "", "", false, err
	}
	return zipPath, latest.Name, latest.Digest, latest.BrowserDownloadURL, true, nil
}

func download(ctx context.Context, httpc *http.Client, agent, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", agent)
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 %s: HTTP %d", url, resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return f.Close()
}

func verifySHA(path, digest string) error {
	want, ok := strings.CutPrefix(digest, "sha256:")
	if !ok {
		return fmt.Errorf("未知 digest 格式: %q", digest)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("sha256 校验失败: got %s want %s", got, want)
	}
	return nil
}

// ---- JSONL 流式解码 ----

type dumpSubject struct {
	ID       int64   `json:"id"`
	Type     int     `json:"type"`
	Name     string  `json:"name"`
	NameCn   string  `json:"name_cn"`
	Platform any     `json:"platform"`
	Date     string  `json:"date"`
	Score    float64 `json:"score"`
	Rank     int     `json:"rank"`
	NSFW     bool    `json:"nsfw"`
	Summary  string  `json:"summary"`
}

type dumpEpisode struct {
	ID        int64   `json:"id"`
	SubjectID int64   `json:"subject_id"`
	Name      string  `json:"name"`
	NameCn    string  `json:"name_cn"`
	Sort      float64 `json:"sort"`
	Type      int     `json:"type"`
	Airdate   string  `json:"airdate"`
}

type dumpRelation struct {
	SubjectID    int64           `json:"subject_id"`
	RelatedID    int64           `json:"related_subject_id"`
	RelationType json.RawMessage `json:"relation_type"`
}

func scanJSONL(r io.Reader, fn func(line []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 16<<20)
	for sc.Scan() {
		if err := fn(sc.Bytes()); err != nil {
			return err
		}
	}
	return sc.Err()
}

func importSubjects(ctx context.Context, tx *sql.Tx, mf *zip.File, stats *ImportStats) (map[int64]bool, error) {
	f, err := mf.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ids := map[int64]bool{}
	var buf []dumpSubject
	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		ph := make([]string, 0, len(buf))
		args := make([]any, 0, len(buf)*10)
		for _, s := range buf {
			ph = append(ph, "(?,?,?,?,?,?,?,?,?,?)")
			args = append(args, s.ID, s.Type, s.Name, s.NameCn,
				stringify(s.Platform), s.Date, s.Score, s.Rank, b2i(s.NSFW), s.Summary)
		}
		q := `INSERT OR REPLACE INTO bgm_subjects (id, type, name, name_cn, platform, date, score, rank, nsfw, summary) VALUES ` +
			strings.Join(ph, ",")
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return err
		}
		buf = buf[:0]
		return nil
	}

	err = scanJSONL(f, func(line []byte) error {
		var s dumpSubject
		if err := json.Unmarshal(line, &s); err != nil {
			return fmt.Errorf("subject 行损坏: %w", err)
		}
		if s.Type != 2 {
			return nil
		}
		ids[s.ID] = true
		buf = append(buf, s)
		if len(buf) >= 500 {
			return flush()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, flush()
}

func importEpisodes(ctx context.Context, tx *sql.Tx, mf *zip.File, animeIDs map[int64]bool) (int, error) {
	f, err := mf.Open()
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	var buf []dumpEpisode
	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		ph := make([]string, 0, len(buf))
		args := make([]any, 0, len(buf)*7)
		for _, e := range buf {
			ph = append(ph, "(?,?,?,?,?,?,?)")
			args = append(args, e.ID, e.SubjectID, e.Name, e.NameCn, e.Sort, e.Type, e.Airdate)
		}
		q := `INSERT OR REPLACE INTO bgm_episodes (id, subject_id, name, name_cn, sort, ep_type, airdate) VALUES ` +
			strings.Join(ph, ",")
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return err
		}
		buf = buf[:0]
		return nil
	}

	err = scanJSONL(f, func(line []byte) error {
		var e dumpEpisode
		if err := json.Unmarshal(line, &e); err != nil {
			return fmt.Errorf("episode 行损坏: %w", err)
		}
		if !animeIDs[e.SubjectID] {
			return nil
		}
		count++
		buf = append(buf, e)
		if len(buf) >= 1000 {
			return flush()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, flush()
}

func importRelations(ctx context.Context, tx *sql.Tx, mf *zip.File, animeIDs map[int64]bool) (int, error) {
	f, err := mf.Open()
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	var buf [][3]any
	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		ph := make([]string, 0, len(buf))
		args := make([]any, 0, len(buf)*3)
		for _, r := range buf {
			ph = append(ph, "(?,?,?)")
			args = append(args, r[0], r[1], r[2])
		}
		q := `INSERT INTO bgm_relations (subject_id, related_subject_id, relation_type) VALUES ` +
			strings.Join(ph, ",")
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return err
		}
		buf = buf[:0]
		return nil
	}

	err = scanJSONL(f, func(line []byte) error {
		var r dumpRelation
		if err := json.Unmarshal(line, &r); err != nil {
			return fmt.Errorf("relation 行损坏: %w", err)
		}
		if !animeIDs[r.SubjectID] && !animeIDs[r.RelatedID] {
			return nil
		}
		count++
		buf = append(buf, [3]any{r.SubjectID, r.RelatedID, stringifyRaw(r.RelationType)})
		if len(buf) >= 1000 {
			return flush()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, flush()
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}

func stringifyRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func upsertMeta(ctx context.Context, tx *sql.Tx, key, value string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO bgm_meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- 索引状态 ----

type IndexStatus struct {
	Version    string `json:"dump_version"`
	ImportedAt string `json:"imported_at"`
	SourceURL  string `json:"source_url"`
	Subjects   int    `json:"subjects"`
	Episodes   int    `json:"episodes"`
	Relations  int    `json:"relations"`
}

func (im *Importer) Status(ctx context.Context) (*IndexStatus, error) {
	st := &IndexStatus{}
	for _, k := range []struct {
		key string
		dst *string
	}{
		{"dump_version", &st.Version}, {"imported_at", &st.ImportedAt}, {"source_url", &st.SourceURL},
	} {
		err := im.db.QueryRowContext(ctx, `SELECT value FROM bgm_meta WHERE key = ?`, k.key).Scan(k.dst)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	for _, c := range []struct {
		tbl string
		dst *int
	}{
		{"bgm_subjects", &st.Subjects}, {"bgm_episodes", &st.Episodes}, {"bgm_relations", &st.Relations},
	} {
		if err := im.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+c.tbl).Scan(c.dst); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// ImportedAt 解析（供每周自动刷新判断）。
func (s *IndexStatus) ImportedTime() time.Time {
	t, err := time.Parse(time.RFC3339, s.ImportedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}
