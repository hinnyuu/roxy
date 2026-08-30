package metadata

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hinnyuu/roxy/internal/db"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func makeDumpZip(t *testing.T, omit ...string) string {
	t.Helper()
	members := map[string]string{
		memberSubjects:  "../../testdata/archive/subject.jsonlines",
		memberEpisodes:  "../../testdata/archive/episode.jsonlines",
		memberRelations: "../../testdata/archive/subject-relations.jsonlines",
	}
	zpath := filepath.Join(t.TempDir(), "dump-test.zip")
	f, err := os.Create(zpath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, src := range members {
		skip := false
		for _, o := range omit {
			if o == name {
				skip = true
			}
		}
		if skip {
			continue
		}
		body, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return zpath
}

func TestImportLocalZip(t *testing.T) {
	ctx := context.Background()
	d := newDB(t)
	im := NewImporter(d, t.TempDir(), "test-agent")
	zpath := makeDumpZip(t)

	var progress []string
	stats, err := im.Import(ctx, zpath, func(p string) { progress = append(progress, p) })
	if err != nil {
		t.Fatal(err)
	}
	if stats.Subjects != 3 || stats.Episodes != 5 || stats.Relations != 2 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(progress) == 0 {
		t.Error("expected progress reports")
	}

	st, err := im.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "dump-test.zip" || !strings.HasPrefix(st.SourceURL, "local:") || st.ImportedAt == "" {
		t.Fatalf("status = %+v", st)
	}
	if st.Subjects != 3 || st.Episodes != 5 || st.Relations != 2 {
		t.Fatalf("status counts = %+v", st)
	}

	// 本地导入不得删除用户文件
	if _, err := os.Stat(zpath); err != nil {
		t.Error("local zip must be kept")
	}

	// FTS：精确/前缀
	x := NewIndex(d)
	hits, err := x.Search(ctx, "Re:Zero", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != 12345 {
		t.Fatalf("exact search hits = %+v", hits)
	}
	hits, _ = x.Search(ctx, "从零开始", 10) // 中文前缀
	if len(hits) != 1 || hits[0].ID != 12345 {
		t.Fatalf("cn prefix hits = %+v", hits)
	}

	// LIKE 兜底：任意子串（FTS 无法命中）
	hits, err = x.Search(ctx, "始的异世界", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != 12345 {
		t.Fatalf("like fallback hits = %+v", hits)
	}

	// NSFW 不进候选
	hits, _ = x.Search(ctx, "成人向作品", 10)
	if len(hits) != 0 {
		t.Fatalf("nsfw must not surface: %+v", hits)
	}

	// 非动画条目未导入
	if _, err := x.Subject(ctx, 999); err != ErrNotFound {
		t.Errorf("book subject should be filtered out, got %v", err)
	}

	// 章节列表与排序
	eps, err := x.Episodes(ctx, 12345)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 4 || eps[0].EPType != 0 || eps[0].Sort != 1 || eps[2].EPType != 1 || eps[3].EPType != 2 {
		t.Fatalf("episodes = %+v", eps)
	}

	// 重放导入（刷新）：行数不变
	if _, err := im.Import(ctx, zpath, nil); err != nil {
		t.Fatal(err)
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM bgm_subjects`).Scan(&n)
	if n != 3 {
		t.Fatalf("reimport subjects = %d", n)
	}
	// FTS 触发器与 rebuild 一致性：重放后仍可检索
	hits, _ = x.Search(ctx, "Re:Zero", 10)
	if len(hits) != 1 {
		t.Fatalf("search after reimport: %+v", hits)
	}
}

func TestImportMissingMember(t *testing.T) {
	d := newDB(t)
	im := NewImporter(d, t.TempDir(), "test-agent")
	zpath := makeDumpZip(t, memberRelations)
	if _, err := im.Import(context.Background(), zpath, nil); err == nil {
		t.Fatal("missing member should error")
	}
}

func TestVerifySHA(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	os.WriteFile(p, []byte("hello"), 0o644)
	good := "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if err := verifySHA(p, good); err != nil {
		t.Fatalf("good digest: %v", err)
	}
	if err := verifySHA(p, "sha256:deadbeef"); err == nil {
		t.Fatal("bad digest should error")
	}
	if err := verifySHA(p, "md5:x"); err == nil {
		t.Fatal("unknown format should error")
	}
}

func TestClient(t *testing.T) {
	var searchCalls, detailCalls, epCalls int
	var gotUA, gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/search/subjects", func(w http.ResponseWriter, r *http.Request) {
		searchCalls++
		gotUA = r.Header.Get("User-Agent")
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]any{
			"total": 1,
			"data": []Subject{{ID: 1, Type: 2, Name: "A", NameCn: "甲"},
				{ID: 2, Type: 2, Name: "B", NameCn: "乙"}},
		})
	})
	mux.HandleFunc("/v0/subjects/1", func(w http.ResponseWriter, r *http.Request) {
		detailCalls++
		json.NewEncoder(w).Encode(Subject{ID: 1, Type: 2, Name: "A", NameCn: "甲"})
	})
	mux.HandleFunc("/v0/subjects/404", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/v0/episodes", func(w http.ResponseWriter, r *http.Request) {
		epCalls++
		offset := r.URL.Query().Get("offset")
		if offset == "0" {
			eps := make([]Episode, 200)
			for i := range eps {
				eps[i] = Episode{ID: int64(i), SubjectID: 1, Sort: float64(i)}
			}
			json.NewEncoder(w).Encode(map[string]any{"total": 250, "data": eps})
		} else {
			eps := make([]Episode, 50)
			for i := range eps {
				eps[i] = Episode{ID: int64(200 + i), SubjectID: 1, Sort: float64(200 + i)}
			}
			json.NewEncoder(w).Encode(map[string]any{"total": 250, "data": eps})
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	d := newDB(t)
	cache := NewCache(d)
	c := NewClient(srv.URL, "RyougiShiki-214/roxy (test)", "tok123")

	hits, err := c.SearchSubjects(context.Background(), cache, "A")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d", len(hits))
	}
	if gotUA != "RyougiShiki-214/roxy (test)" {
		t.Errorf("UA = %q", gotUA)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("Auth = %q", gotAuth)
	}
	// 缓存命中：不再打服务器
	hits2, err := c.SearchSubjects(context.Background(), cache, "A")
	if err != nil || len(hits2) != 2 || searchCalls != 1 {
		t.Fatalf("cache miss: calls=%d err=%v", searchCalls, err)
	}

	s, err := c.GetSubject(context.Background(), cache, 1)
	if err != nil || s.NameCn != "甲" || detailCalls != 1 {
		t.Fatalf("detail: %+v err=%v", s, err)
	}
	if _, err := c.GetSubject(context.Background(), cache, 404); err != ErrNotFound {
		t.Errorf("404 = %v", err)
	}

	eps, err := c.ListEpisodes(context.Background(), 1)
	if err != nil || len(eps) != 250 || epCalls != 2 {
		t.Fatalf("episodes: n=%d calls=%d err=%v", len(eps), epCalls, err)
	}
}

func TestCacheTTL(t *testing.T) {
	d := newDB(t)
	c := NewCache(d)
	ctx := context.Background()
	c.Put(ctx, "bangumi", "k", "v", time.Hour)
	if v, ok := c.Get(ctx, "bangumi", "k"); !ok || v != "v" {
		t.Fatalf("cache get = %q ok=%v", v, ok)
	}
	c.Put(ctx, "bangumi", "expired", "v", -time.Hour)
	if _, ok := c.Get(ctx, "bangumi", "expired"); ok {
		t.Fatal("expired entry must miss")
	}
}

func TestStatusEmpty(t *testing.T) {
	d := newDB(t)
	im := NewImporter(d, t.TempDir(), "ua")
	st, err := im.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "" || !st.ImportedTime().IsZero() {
		t.Fatalf("empty status = %+v", st)
	}
}
