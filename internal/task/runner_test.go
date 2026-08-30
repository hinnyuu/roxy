package task

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hinnyuu/roxy/internal/db"
	"github.com/hinnyuu/roxy/internal/domain"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "task.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func getTask(t *testing.T, d *sql.DB, id int64) (state, progress, errMsg string) {
	t.Helper()
	var prog, er sql.NullString
	err := d.QueryRow(`SELECT state, progress, error FROM tasks WHERE id = ?`, id).Scan(&state, &prog, &er)
	if err != nil {
		t.Fatal(err)
	}
	return state, prog.String, er.String
}

// waitFor 轮询任务进入期望终态（超时即失败）。
func waitFor(t *testing.T, d *sql.DB, id int64, want string) (string, string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, prog, er := getTask(t, d, id)
		if state == want {
			return prog, er
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %d did not reach state %s", id, want)
	return "", ""
}

func TestRunDoneAndFailed(t *testing.T) {
	d := newDB(t)
	r := NewRunner(d)
	r.Register("scan", func(ctx context.Context, payload string, report Report) error {
		report(`{"files":3}`)
		if payload == "boom" {
			return context.DeadlineExceeded
		}
		return nil
	})
	okID, err := r.Enqueue(context.Background(), "scan", "ok")
	if err != nil {
		t.Fatal(err)
	}
	badID, err := r.Enqueue(context.Background(), "scan", "boom")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	waitFor(t, d, okID, domain.TaskDone)
	_, errMsg := waitFor(t, d, badID, domain.TaskFailed)
	if !strings.Contains(errMsg, "deadline") {
		t.Errorf("failed task error = %q", errMsg)
	}
	if _, prog, _ := getTask(t, d, okID); prog != `{"files":3}` {
		t.Errorf("progress = %q", prog)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("run: %v", err)
	}
}

func TestCancelQueued(t *testing.T) {
	d := newDB(t)
	r := NewRunner(d)
	release := make(chan struct{})
	r.Register("reconcile", func(ctx context.Context, payload string, report Report) error {
		<-release
		return nil
	})
	blocker, err := r.Enqueue(context.Background(), "reconcile", "")
	if err != nil {
		t.Fatal(err)
	}
	victim, err := r.Enqueue(context.Background(), "reconcile", "")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer close(release)
	go r.Run(ctx)

	// blocker 先占用 worker；victim 仍在排队，可直接取消。
	waitFor(t, d, blocker, domain.TaskRunning)
	if err := r.Cancel(context.Background(), victim); err != nil {
		t.Fatalf("cancel queued: %v", err)
	}
	if state, _, _ := getTask(t, d, victim); state != domain.TaskCancelled {
		t.Fatalf("victim state = %s", state)
	}
}

func TestCancelRunning(t *testing.T) {
	d := newDB(t)
	r := NewRunner(d)
	started := make(chan struct{})
	r.Register("rework", func(ctx context.Context, payload string, report Report) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	id, err := r.Enqueue(context.Background(), "rework", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	<-started
	if err := r.Cancel(context.Background(), id); err != nil {
		t.Fatalf("cancel running: %v", err)
	}
	waitFor(t, d, id, domain.TaskCancelled)
}

func TestStartupRecovery(t *testing.T) {
	d := newDB(t)
	if _, err := d.Exec(`INSERT INTO tasks (kind, state, created_at) VALUES ('scan', 'running', ?)`,
		domain.Now()); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(d)
	r.Register("scan", func(ctx context.Context, payload string, report Report) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	prog, errMsg := waitFor(t, d, 1, domain.TaskFailed)
	_ = prog
	if !strings.Contains(errMsg, "interrupted") {
		t.Errorf("recovery error = %q", errMsg)
	}
	cancel()
	<-done
}

func TestEnqueueUnknownKind(t *testing.T) {
	d := newDB(t)
	r := NewRunner(d)
	if _, err := r.Enqueue(context.Background(), "nope", ""); err == nil {
		t.Error("unknown kind should error")
	}
}
