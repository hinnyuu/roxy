package api

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/hinnyuu/roxy/internal/auth"
	"github.com/hinnyuu/roxy/internal/config"
	"github.com/hinnyuu/roxy/internal/db"
	"github.com/hinnyuu/roxy/internal/domain"
	"github.com/hinnyuu/roxy/internal/matcher"
	"github.com/hinnyuu/roxy/internal/metadata"
	"github.com/hinnyuu/roxy/internal/parser"
	"github.com/hinnyuu/roxy/internal/review"
	"github.com/hinnyuu/roxy/internal/scanner"
	"github.com/hinnyuu/roxy/internal/task"
)

func makeDumpZip(t *testing.T) string {
	t.Helper()
	zpath := filepath.Join(t.TempDir(), "dump.zip")
	f, _ := os.Create(zpath)
	zw := zip.NewWriter(f)
	for _, m := range [][2]string{
		{"subject.jsonlines", "../../testdata/archive/subject.jsonlines"},
		{"episode.jsonlines", "../../testdata/archive/episode.jsonlines"},
		{"subject-relations.jsonlines", "../../testdata/archive/subject-relations.jsonlines"},
	} {
		body, err := os.ReadFile(m[1])
		if err != nil {
			t.Fatal(err)
		}
		w, _ := zw.Create(m[0])
		w.Write(body)
	}
	zw.Close()
	f.Close()
	return zpath
}

type testEnv struct {
	ts  *httptest.Server
	jar http.CookieJar
	db  *sql.DB
}

func (e *testEnv) do(t *testing.T, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rdr)
	resp, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	all, _ := io.ReadAll(resp.Body)
	return resp, all
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	svc := auth.NewService(database)
	if err := svc.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}

	store := scanner.NewStore(database)
	idx := metadata.NewIndex(database)
	importer := metadata.NewImporter(database, cfg.DataDir, "test-agent")
	mp := matcher.New(database, parser.New(nil), idx, nil, metadata.NewCache(database), 0.90, "vault", false)
	sc := scanner.NewScanner(store)
	runner := task.NewRunner(database)
	runner.Register("scan", func(ctx context.Context, payload string, report task.Report) error {
		var p scanner.ScanPayload
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return err
		}
		src, err := store.GetSource(ctx, p.SourceID)
		if err != nil {
			return err
		}
		stats, serr := sc.ScanSource(ctx, *src, mp.ProcessEvent)
		if stats != nil {
			b, _ := json.Marshal(stats)
			report(string(b))
		}
		return serr
	})
	runner.Register("index_refresh", func(ctx context.Context, payload string, report task.Report) error {
		var p struct {
			LocalPath string `json:"local_path"`
		}
		if payload != "" {
			json.Unmarshal([]byte(payload), &p)
		}
		stats, err := importer.Import(ctx, p.LocalPath, func(s string) { report(s) })
		if stats != nil {
			b, _ := json.Marshal(stats)
			report(string(b))
		}
		return err
	})
	ctx, cancel := context.WithCancel(context.Background())
	go runner.Run(ctx)
	t.Cleanup(func() { cancel(); runner.Wait(); database.Close() })

	deps := Deps{DB: database, Sources: store, Scanner: sc, Matcher: mp,
		Review: review.New(database), Tasks: runner, Importer: importer, Index: idx}
	ts := httptest.NewServer(NewServer(cfg, svc, auth.NewSessionStore(), "test", deps).Handler())
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	ts.Client().Jar = jar
	return &testEnv{ts: ts, jar: jar, db: database}
}

func (e *testEnv) login(t *testing.T) {
	t.Helper()
	resp, body := e.do(t, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "admin"})
	if resp.StatusCode != 200 {
		t.Fatalf("login: %d %s", resp.StatusCode, body)
	}
}

func waitTask(t *testing.T, e *testEnv, id int64, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		_, body := e.do(t, "GET", "/api/tasks/"+itoa(id), nil)
		var tv taskView
		if err := json.Unmarshal(body, &tv); err != nil {
			t.Fatalf("task json: %v (%s)", err, body)
		}
		if tv.State == want {
			return
		}
		if tv.State == "failed" {
			t.Fatalf("task %d failed: %s", id, tv.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task %d not %s in time", id, want)
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func TestFullPipeline(t *testing.T) {
	e := newTestEnv(t)

	// 未登录 401
	resp, _ := e.do(t, "GET", "/api/sources", nil)
	if resp.StatusCode != 401 {
		t.Fatalf("unauth = %d", resp.StatusCode)
	}
	e.login(t)

	// 索引：空 → 本地导入 → 有数据
	_, body := e.do(t, "GET", "/api/index", nil)
	var st map[string]any
	json.Unmarshal(body, &st)
	if st["dump_version"] != "" {
		t.Fatalf("index not empty: %s", body)
	}
	zpath := makeDumpZip(t)
	resp, body = e.do(t, "POST", "/api/index/refresh", map[string]string{"local_path": zpath})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("refresh: %d %s", resp.StatusCode, body)
	}
	var tid struct {
		TaskID int64 `json:"task_id"`
	}
	json.Unmarshal(body, &tid)
	waitTask(t, e, tid.TaskID, domain.TaskDone)

	_, body = e.do(t, "GET", "/api/index", nil)
	st = map[string]any{}
	json.Unmarshal(body, &st)
	if st["dump_version"] != "dump.zip" || st["subjects"].(float64) != 5 {
		t.Fatalf("index status: %s", body)
	}

	// 源：创建 → 扫描 → 文件/队列
	dir := t.TempDir()
	for _, name := range []string{
		"[SubA] Re:Zero [01][1080p].mkv", // auto_approved
		"[SubA] Re:Zero [99][1080p].mkv", // 集号未验证 → 队列
		"[ZZZ] No Such Show [01].mkv",    // 无候选 → parsed
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	resp, body = e.do(t, "POST", "/api/sources", map[string]string{"name": "dl", "path": dir})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create source: %d %s", resp.StatusCode, body)
	}
	var src sourceView
	json.Unmarshal(body, &src)

	// path 非法 → 400
	if resp, _ = e.do(t, "POST", "/api/sources", map[string]string{"name": "bad", "path": "/nonexistent-xyz"}); resp.StatusCode != 400 {
		t.Fatalf("bad path = %d", resp.StatusCode)
	}
	// 重复 path → 409
	if resp, _ = e.do(t, "POST", "/api/sources", map[string]string{"name": "dup", "path": dir}); resp.StatusCode != 409 {
		t.Fatalf("dup path = %d", resp.StatusCode)
	}
	// 不存在的源扫描 → 404
	if resp, _ = e.do(t, "POST", "/api/sources/999/scan", nil); resp.StatusCode != 404 {
		t.Fatalf("scan missing = %d", resp.StatusCode)
	}

	resp, body = e.do(t, "POST", "/api/sources/"+itoa(src.ID)+"/scan", nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("scan: %d %s", resp.StatusCode, body)
	}
	json.Unmarshal(body, &tid)
	waitTask(t, e, tid.TaskID, domain.TaskDone)

	_, body = e.do(t, "GET", "/api/sources/"+itoa(src.ID)+"/files", nil)
	var files []fileView
	json.Unmarshal(body, &files)
	if len(files) != 3 {
		t.Fatalf("files = %d (%s)", len(files), body)
	}
	placed, parsed := 0, 0
	for _, f := range files {
		switch f.Status {
		case domain.SourceFilePlaced:
			placed++
		case domain.SourceFileParsed:
			parsed++
		}
	}
	if placed != 2 || parsed != 1 {
		t.Fatalf("placed=%d parsed=%d", placed, parsed)
	}

	_, body = e.do(t, "GET", "/api/review?state=open", nil)
	var items []review.Item
	json.Unmarshal(body, &items)
	if len(items) != 1 || items[0].SeriesTitle != "Re：从零开始的异世界生活" {
		t.Fatalf("review items: %s", body)
	}

	// 批准闭环
	caseID := items[0].CaseID
	resp, body = e.do(t, "POST", "/api/review/"+itoa(caseID)+"/approve", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("approve: %d %s", resp.StatusCode, body)
	}
	_, body = e.do(t, "GET", "/api/review?state=open", nil)
	items = nil
	json.Unmarshal(body, &items)
	if len(items) != 0 {
		t.Fatalf("queue not empty after approve: %s", body)
	}
	// 重复批准 → 409
	if resp, _ = e.do(t, "POST", "/api/review/"+itoa(caseID)+"/approve", nil); resp.StatusCode != 409 {
		t.Fatalf("double approve = %d", resp.StatusCode)
	}

	// 源非空删除 → 409
	if resp, _ = e.do(t, "DELETE", "/api/sources/"+itoa(src.ID), nil); resp.StatusCode != 409 {
		t.Fatalf("delete non-empty = %d", resp.StatusCode)
	}

	// 源更新（禁用）
	enabled := false
	if resp, _ = e.do(t, "PUT", "/api/sources/"+itoa(src.ID), map[string]any{"enabled": enabled}); resp.StatusCode != 200 {
		t.Fatalf("update = %d", resp.StatusCode)
	}
	_, body = e.do(t, "GET", "/api/sources", nil)
	var srcs []sourceView
	json.Unmarshal(body, &srcs)
	if len(srcs) != 1 || srcs[0].Enabled || srcs[0].FileCount != 3 {
		t.Fatalf("sources: %s", body)
	}
}
