package matcher

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hinnyuu/roxy/internal/db"
	"github.com/hinnyuu/roxy/internal/domain"
	"github.com/hinnyuu/roxy/internal/metadata"
	"github.com/hinnyuu/roxy/internal/parser"
	"github.com/hinnyuu/roxy/internal/scanner"
)

func setup(t *testing.T, client *metadata.Client) (*sql.DB, *scanner.Scanner, *Matcher, *scanner.Store, string) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	// 导入裁剪 dump（与 metadata 测试同一夹具）
	zpath := filepath.Join(t.TempDir(), "dump.zip")
	zf, _ := os.Create(zpath)
	zw := zip.NewWriter(zf)
	for _, m := range []struct{ name, src string }{
		{"subject.jsonlines", "../../testdata/archive/subject.jsonlines"},
		{"episode.jsonlines", "../../testdata/archive/episode.jsonlines"},
		{"subject-relations.jsonlines", "../../testdata/archive/subject-relations.jsonlines"},
	} {
		body, err := os.ReadFile(m.src)
		if err != nil {
			t.Fatal(err)
		}
		w, _ := zw.Create(m.name)
		w.Write(body)
	}
	zw.Close()
	zf.Close()
	im := metadata.NewImporter(d, t.TempDir(), "ua")
	if _, err := im.Import(context.Background(), zpath, nil); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	store := scanner.NewStore(d)
	src := &domain.Source{Name: "dl", Path: dir, Kind: "mixed", ProviderType: "dirscan", Enabled: true}
	if err := store.CreateSource(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	m := New(d, parser.New(nil), metadata.NewIndex(d), client, metadata.NewCache(d), 0.90, "vault")
	return d, scanner.NewScanner(store), m, store, dir
}

func touch(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

type placementRow struct {
	seriesID    int64
	slot        string
	season      sql.NullInt64
	episode     sql.NullFloat64
	episodeEnd  sql.NullFloat64
	versionKey  string
	vault       int
	subtitleOf  sql.NullInt64
	reviewState string
	confidence  float64
	source      string
}

func placements(t *testing.T, d *sql.DB) []placementRow {
	t.Helper()
	rows, err := d.Query(`SELECT series_id, slot_type, season, episode, episode_end, version_key,
		vault, subtitle_of_placement_id, review_state, confidence, decision_source FROM placements ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []placementRow
	for rows.Next() {
		var r placementRow
		if err := rows.Scan(&r.seriesID, &r.slot, &r.season, &r.episode, &r.episodeEnd,
			&r.versionKey, &r.vault, &r.subtitleOf, &r.reviewState, &r.confidence, &r.source); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func fileStatus(t *testing.T, d *sql.DB, path string) string {
	t.Helper()
	var s string
	if err := d.QueryRow(`SELECT status FROM source_files WHERE abs_path = ?`, path).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPipelineAutoApproveAndReview(t *testing.T) {
	ctx := context.Background()
	d, sc, m, store, dir := setup(t, nil)

	touch(t, dir, "[Nekomoe kissaten&VCB-Studio] Re:Zero [01][1080p][JPSC].mkv")
	touch(t, dir, "[Nekomoe kissaten&VCB-Studio] Re:Zero [13][1080p][JPSC].mkv") // 特别篇 sort=13
	touch(t, dir, "[Nekomoe kissaten&VCB-Studio] Re:Zero [99][1080p].mkv")       // 集号未验证
	touch(t, dir, "[ZZZ] Totally Unknown Show [01].mkv")                         // 无候选
	touch(t, dir, "[YYSub] Re:Zero [01-25][1080p].mkv")                          // 合集包

	if _, err := sc.ScanSource(ctx, domain.Source{ID: 1, Path: dir, ProviderType: "dirscan"}, m.ProcessEvent); err != nil {
		t.Fatal(err)
	}

	ps := placements(t, d)
	if len(ps) != 4 {
		t.Fatalf("placements = %d, want 4 (%+v)", len(ps), ps)
	}
	// E01 精确+验证 → auto_approved
	if ps[0].reviewState != domain.PlacementAutoApproved || ps[0].slot != domain.SlotEpisode ||
		ps[0].season.Int64 != 1 || ps[0].episode.Float64 != 1 || ps[0].confidence < 0.99 {
		t.Errorf("E01 = %+v", ps[0])
	}
	// 特别篇 E13 → special/season0/auto
	if ps[1].slot != domain.SlotSpecial || ps[1].season.Int64 != 0 ||
		ps[1].reviewState != domain.PlacementAutoApproved {
		t.Errorf("E13 = %+v", ps[1])
	}
	// E99 未验证 → 人工队列
	if ps[2].reviewState != domain.PlacementPendingReview {
		t.Errorf("E99 = %+v", ps[2])
	}
	// 合集包 → ignored 槽 + 人工
	if ps[3].slot != domain.SlotIgnored || ps[3].reviewState != domain.PlacementPendingReview {
		t.Errorf("batch = %+v", ps[3])
	}
	_ = store

	// review_cases 仅人工工单
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM review_cases WHERE state = 'open'`).Scan(&n)
	if n != 2 {
		t.Errorf("open cases = %d, want 2", n)
	}

	// 未知标题文件留在 parsed、无 placement
	if s := fileStatus(t, d, filepath.Join(dir, "[ZZZ] Totally Unknown Show [01].mkv")); s != domain.SourceFileParsed {
		t.Errorf("unknown file status = %s", s)
	}

	// 系列收敛：全部挂同一 series（bgm 12345）
	var cnt int
	d.QueryRow(`SELECT COUNT(DISTINCT series_id) FROM placements`).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("distinct series = %d", cnt)
	}
	var bgmID sql.NullInt64
	d.QueryRow(`SELECT bgm_subject_id FROM series`).Scan(&bgmID)
	if bgmID.Int64 != 12345 {
		t.Errorf("series bgm id = %v", bgmID)
	}
}

func TestPipelineAliasConvergenceSecondForm(t *testing.T) {
	ctx := context.Background()
	d, sc, m, _, dir := setup(t, nil)

	touch(t, dir, "[SubA] Re:Zero [01][1080p].mkv")
	if _, err := sc.ScanSource(ctx, domain.Source{ID: 1, Path: dir, ProviderType: "dirscan"}, m.ProcessEvent); err != nil {
		t.Fatal(err)
	}
	// 第二个文件用中文名：应经别名收敛到同一系列（不再查索引）
	touch(t, dir, "[SubB] Re：从零开始的异世界生活 [02][1080p].mkv")
	if _, err := sc.ScanSource(ctx, domain.Source{ID: 1, Path: dir, ProviderType: "dirscan"}, m.ProcessEvent); err != nil {
		t.Fatal(err)
	}
	ps := placements(t, d)
	if len(ps) != 2 || ps[0].seriesID != ps[1].seriesID {
		t.Fatalf("alias convergence failed: %+v", ps)
	}
	if ps[1].reviewState != domain.PlacementAutoApproved || ps[1].versionKey != "subb" {
		t.Errorf("second file = %+v", ps[1])
	}
}

func TestPipelineVaultMultiVersion(t *testing.T) {
	ctx := context.Background()
	d, sc, m, _, dir := setup(t, nil)

	touch(t, dir, "[SubA] Re:Zero [01][1080p].mkv")
	touch(t, dir, "[SubB] Re:Zero [01][720p].mkv")
	if _, err := sc.ScanSource(ctx, domain.Source{ID: 1, Path: dir, ProviderType: "dirscan"}, m.ProcessEvent); err != nil {
		t.Fatal(err)
	}
	ps := placements(t, d)
	if len(ps) != 2 {
		t.Fatalf("placements = %d", len(ps))
	}
	if ps[0].vault != 0 || ps[1].vault != 1 {
		t.Fatalf("vault flags = %d/%d (先到为主)", ps[0].vault, ps[1].vault)
	}
}

func TestPipelineSubtitlePairing(t *testing.T) {
	ctx := context.Background()
	d, sc, m, _, dir := setup(t, nil)

	touch(t, dir, "[SubA] Re:Zero [01][1080p].mkv")
	touch(t, dir, "[SubA] Re:Zero [01][1080p].zh-CN.srt")
	if _, err := sc.ScanSource(ctx, domain.Source{ID: 1, Path: dir, ProviderType: "dirscan"}, m.ProcessEvent); err != nil {
		t.Fatal(err)
	}
	ps := placements(t, d)
	if len(ps) != 2 {
		t.Fatalf("placements = %d", len(ps))
	}
	sub := ps[1]
	if sub.slot != domain.SlotSub || !sub.subtitleOf.Valid || sub.subtitleOf.Int64 != 1 {
		t.Fatalf("subtitle pairing = %+v (want subtitle_of_placement=1)", sub)
	}
	if sub.reviewState != domain.PlacementAutoApproved {
		t.Errorf("exact pairing subtitle should auto-approve, got %+v", sub)
	}
}

func TestPipelineSubtitleLevel1VersionKey(t *testing.T) {
	ctx := context.Background()
	d, sc, m, _, dir := setup(t, nil)

	touch(t, dir, "[SubA] Re:Zero [01][1080p].mkv")
	touch(t, dir, "[SubA] Re:Zero [01][CHS].ass") // 不同 basename，version_key 相同
	if _, err := sc.ScanSource(ctx, domain.Source{ID: 1, Path: dir, ProviderType: "dirscan"}, m.ProcessEvent); err != nil {
		t.Fatal(err)
	}
	ps := placements(t, d)
	if len(ps) != 2 || ps[1].slot != domain.SlotSub || !ps[1].subtitleOf.Valid {
		t.Fatalf("level-1 pairing failed: %+v", ps)
	}
}

func TestPipelineMovie(t *testing.T) {
	ctx := context.Background()
	d, sc, m, _, dir := setup(t, nil)

	touch(t, dir, "[DBD-Raws] Movie X (2020) [1080p].mkv")
	if _, err := sc.ScanSource(ctx, domain.Source{ID: 1, Path: dir, ProviderType: "dirscan"}, m.ProcessEvent); err != nil {
		t.Fatal(err)
	}
	ps := placements(t, d)
	if len(ps) != 1 || ps[0].slot != domain.SlotMovie {
		t.Fatalf("movie = %+v", ps)
	}
	if ps[0].reviewState != domain.PlacementPendingReview {
		t.Errorf("movie conf = %.2f state = %s", ps[0].confidence, ps[0].reviewState)
	}
	var lk string
	d.QueryRow(`SELECT library_kind FROM series`).Scan(&lk)
	if lk != "movie" {
		t.Errorf("library_kind = %s", lk)
	}
}

func TestMapPlatform(t *testing.T) {
	cases := []struct {
		in   string
		typ  string
		kind string
	}{
		{"1", "tv", "tv"}, // dump 数字码
		{"2", "ova", "tv"},
		{"3", "movie", "movie"},
		{"4", "special", "tv"},
		{"5", "ona", "tv"},
		{"TV", "tv", "tv"}, // 在线 API 字符串
		{"OVA", "ova", "tv"},
		{"剧场版", "movie", "movie"},
		{"WEB", "ona", "tv"},
		{"", "special", "tv"},
	}
	for _, c := range cases {
		typ, kind := mapPlatform(c.in)
		if typ != c.typ || kind != c.kind {
			t.Errorf("mapPlatform(%q) = %s/%s, want %s/%s", c.in, typ, kind, c.typ, c.kind)
		}
	}
}

func TestOnlineFallbackCreatesSeries(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/search/subjects", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"total": 1,
			"data": []metadata.Subject{{ID: 777, Type: 2, Name: "Online Only"}}})
	})
	mux.HandleFunc("/v0/subjects/777", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(metadata.Subject{ID: 777, Type: 2, Name: "Online Only", NameCn: "仅在线", Platform: "TV"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	d, sc, m, _, dir := setup(t, metadata.NewClient(srv.URL, "ua", ""))
	touch(t, dir, "[SubZ] Online Only [01][1080p].mkv")
	if _, err := sc.ScanSource(ctx, domain.Source{ID: 1, Path: dir, ProviderType: "dirscan"}, m.ProcessEvent); err != nil {
		t.Fatal(err)
	}
	ps := placements(t, d)
	if len(ps) != 1 || ps[0].reviewState != domain.PlacementPendingReview {
		t.Fatalf("online fallback = %+v", ps)
	}
	var bgm sql.NullInt64
	d.QueryRow(`SELECT bgm_subject_id FROM series`).Scan(&bgm)
	if bgm.Int64 != 777 {
		t.Errorf("series bgm = %v", bgm)
	}
}
