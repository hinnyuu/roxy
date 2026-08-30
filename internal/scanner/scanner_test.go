package scanner

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/hinnyuu/roxy/internal/db"
	"github.com/hinnyuu/roxy/internal/domain"
)

func newStore(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "scan.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanSourceLifecycle(t *testing.T) {
	ctx := context.Background()
	d := newStore(t)
	store := NewStore(d)
	src := &domain.Source{Name: "dl", Path: t.TempDir(), Kind: "mixed", ProviderType: "dirscan", Enabled: true}
	if err := store.CreateSource(ctx, src); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(src.Path, "a.mkv"), "v1")
	writeFile(t, filepath.Join(src.Path, "b.zh-CN.srt"), "s1")
	writeFile(t, filepath.Join(src.Path, "tvshow.nfo"), "nfo")
	writeFile(t, filepath.Join(src.Path, "poster.jpg"), "img")
	writeFile(t, filepath.Join(src.Path, ".hidden.mkv"), "h")
	writeFile(t, filepath.Join(src.Path, "c.mkv.part"), "p")
	writeFile(t, filepath.Join(src.Path, "sub", "d.ass"), "s2")

	var events []domain.SourceEvent
	h := func(_ context.Context, ev domain.SourceEvent) error {
		events = append(events, ev)
		return nil
	}
	sc := NewScanner(store)
	stats, err := sc.ScanSource(ctx, *src, h)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Discovered != 3 || stats.New != 3 || stats.Changed != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d", len(events))
	}
	for _, ev := range events {
		if ev.Op != domain.EventUpsert {
			t.Errorf("event op = %s", ev.Op)
		}
	}

	// 幂等：无变化不触发事件
	events = nil
	if _, err := sc.ScanSource(ctx, *src, h); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("re-scan should emit no events, got %d", len(events))
	}

	// mtime/size 变化 → changed 事件
	p := filepath.Join(src.Path, "a.mkv")
	writeFile(t, p, "v2-longer")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatal(err)
	}
	events = nil
	stats, err = sc.ScanSource(ctx, *src, h)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Changed != 1 || len(events) != 1 || events[0].AbsPath != p {
		t.Fatalf("stats=%+v events=%v", stats, events)
	}

	// 消失文件：无 placement → 删除登记
	if err := os.Remove(filepath.Join(src.Path, "b.zh-CN.srt")); err != nil {
		t.Fatal(err)
	}
	stats, err = sc.ScanSource(ctx, *src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Gone != 1 || stats.Stale != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	var cnt int
	if err := d.QueryRow(`SELECT COUNT(*) FROM source_files`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 2 {
		t.Fatalf("rows after vanish = %d, want 2", cnt)
	}

	// 消失但有 placement → 保留（stale，M4 巡检处理）
	var fileID int64
	if err := d.QueryRow(`SELECT id FROM source_files WHERE abs_path = ?`, p).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if _, err := d.Exec(`INSERT INTO series (bgm_subject_id, title, created_at, updated_at) VALUES (1, 'S', ?, ?)`,
		domain.Now(), domain.Now()); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`SELECT id FROM series LIMIT 1`).Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO placements (source_file_id, series_id, slot_type, decision_source, review_state, created_at, updated_at)
		VALUES (?, ?, 'episode', 'rule', 'proposed', ?, ?)`, fileID, seriesID, domain.Now(), domain.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	stats, err = sc.ScanSource(ctx, *src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Gone != 1 || stats.Stale != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM source_files WHERE id = ?`, fileID).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatal("file with placement must not be deleted")
	}
}

func TestScanEventOrderingDeterministic(t *testing.T) {
	ctx := context.Background()
	d := newStore(t)
	store := NewStore(d)
	src := &domain.Source{Name: "dl", Path: t.TempDir(), Kind: "mixed", ProviderType: "dirscan", Enabled: true}
	if err := store.CreateSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"x.mkv", "y.mkv", "z.mkv"} {
		writeFile(t, filepath.Join(src.Path, n), "1")
	}
	var paths []string
	_, err := NewScanner(store).ScanSource(ctx, *src, func(_ context.Context, ev domain.SourceEvent) error {
		paths = append(paths, filepath.Base(ev.AbsPath))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("events should follow walk order, got %v", paths)
	}
}

func TestDeleteSourceGuard(t *testing.T) {
	ctx := context.Background()
	d := newStore(t)
	store := NewStore(d)
	src := &domain.Source{Name: "dl", Path: t.TempDir(), Kind: "mixed", ProviderType: "dirscan", Enabled: true}
	if err := store.CreateSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSource(ctx, src.ID); err != nil {
		t.Fatalf("empty source delete: %v", err)
	}
	if _, err := store.GetSource(ctx, src.ID); err != sql.ErrNoRows {
		t.Fatalf("get deleted = %v", err)
	}

	src2 := &domain.Source{Name: "dl2", Path: t.TempDir(), Kind: "mixed", ProviderType: "dirscan", Enabled: true}
	if err := store.CreateSource(ctx, src2); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src2.Path, "m.mkv"), "1")
	if _, err := NewScanner(store).ScanSource(ctx, *src2, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSource(ctx, src2.ID); err != ErrNotEmpty {
		t.Fatalf("delete non-empty = %v, want ErrNotEmpty", err)
	}
}

func TestUnknownProvider(t *testing.T) {
	ctx := context.Background()
	d := newStore(t)
	store := NewStore(d)
	sc := NewScanner(store)
	_, err := sc.ScanSource(ctx, domain.Source{ProviderType: "qbittorrent"}, nil)
	if err == nil {
		t.Fatal("unknown provider should error")
	}
}
