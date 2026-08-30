// Package task 异步任务运行器：单 worker 消费 tasks 表（docs/ARCHITECTURE.md §3
// 横切设施"任务队列"）。kind→handler 注册表使 scan/index_refresh（M2）与
// match/materialize/rework/reconcile（M3–M5）纯增量接入。
package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hinnyuu/roxy/internal/domain"
)

// ErrNotFound 任务不存在。
var ErrNotFound = errors.New("task not found")

// Report 向 tasks.progress 写入进度 JSON。
type Report func(progressJSON string)

// Handler 执行任务；返回 error 则任务标记 failed。
type Handler func(ctx context.Context, payload string, report Report) error

type Runner struct {
	db       *sql.DB
	handlers map[string]Handler

	mu      sync.Mutex
	wake    chan struct{}
	running map[int64]context.CancelFunc
}

func NewRunner(db *sql.DB) *Runner {
	return &Runner{
		db:       db,
		handlers: map[string]Handler{},
		wake:     make(chan struct{}, 1),
		running:  map[int64]context.CancelFunc{},
	}
}

// Register 注册任务类型处理器（重复注册为编程错误，直接 panic）。
func (r *Runner) Register(kind string, h Handler) {
	if _, dup := r.handlers[kind]; dup {
		panic("task: duplicate handler for kind " + kind)
	}
	r.handlers[kind] = h
}

// Enqueue 入队并唤醒 worker。
func (r *Runner) Enqueue(ctx context.Context, kind, payload string) (int64, error) {
	if _, ok := r.handlers[kind]; !ok {
		return 0, fmt.Errorf("task: 未注册的任务类型 %q", kind)
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO tasks (kind, payload, state, created_at) VALUES (?, ?, ?, ?)`,
		kind, payload, domain.TaskQueued, domain.Now())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return id, nil
}

// Cancel 取消任务：排队中直接标记 cancelled；运行中先标记再取消 ctx，
// worker 收尾时保留 cancelled 状态。
func (r *Runner) Cancel(ctx context.Context, id int64) error {
	r.mu.Lock()
	cancel, isRunning := r.running[id]
	r.mu.Unlock()

	if isRunning {
		res, err := r.db.ExecContext(ctx,
			`UPDATE tasks SET state = ?, finished_at = ? WHERE id = ? AND state = ?`,
			domain.TaskCancelled, domain.Now(), id, domain.TaskRunning)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			cancel()
			return nil
		}
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET state = ?, finished_at = ? WHERE id = ? AND state = ?`,
		domain.TaskCancelled, domain.Now(), id, domain.TaskQueued)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var state string
	err = r.db.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id = ?`, id).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if state == domain.TaskCancelled {
		return nil
	}
	return fmt.Errorf("task: 状态 %s 不可取消", state)
}

// Run 启动 worker 循环直到 ctx 结束；启动时把上次进程遗留的 running 标记 failed。
func (r *Runner) Run(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET state = ?, error = ?, finished_at = ? WHERE state = ?`,
		domain.TaskFailed, "interrupted by restart", domain.Now(), domain.TaskRunning); err != nil {
		return fmt.Errorf("task: startup recovery: %w", err)
	}

	for {
		id, kind, payload, found, err := r.next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if !found {
			select {
			case <-ctx.Done():
				return nil
			case <-r.wake:
			case <-time.After(time.Second):
			}
			continue
		}
		r.process(ctx, id, kind, payload)
	}
}

func (r *Runner) next(ctx context.Context) (int64, string, string, bool, error) {
	var (
		id      int64
		kind    string
		payload sql.NullString
	)
	err := r.db.QueryRowContext(ctx,
		`SELECT id, kind, payload FROM tasks WHERE state = ? ORDER BY id LIMIT 1`,
		domain.TaskQueued).Scan(&id, &kind, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", "", false, nil
	}
	if err != nil {
		return 0, "", "", false, err
	}
	return id, kind, payload.String, true, nil
}

func (r *Runner) process(parent context.Context, id int64, kind, payload string) {
	h, ok := r.handlers[kind]
	if !ok {
		r.finish(id, domain.TaskFailed, fmt.Sprintf("unregistered handler %s", kind))
		return
	}
	// 取消可能发生在 next 与标记 running 之间：先检查再更新。
	var state string
	if err := r.db.QueryRowContext(parent, `SELECT state FROM tasks WHERE id = ?`, id).Scan(&state); err != nil || state != domain.TaskQueued {
		return
	}
	if _, err := r.db.ExecContext(parent,
		`UPDATE tasks SET state = ?, started_at = ? WHERE id = ? AND state = ?`,
		domain.TaskRunning, domain.Now(), id, domain.TaskQueued); err != nil {
		slog.Error("task: mark running failed", "id", id, "err", err)
		return
	}

	tctx, cancel := context.WithCancel(parent)
	r.mu.Lock()
	r.running[id] = cancel
	r.mu.Unlock()
	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.running, id)
		r.mu.Unlock()
	}()

	report := func(progressJSON string) {
		if _, err := r.db.ExecContext(context.Background(),
			`UPDATE tasks SET progress = ? WHERE id = ?`, progressJSON, id); err != nil {
			slog.Error("task: progress update failed", "id", id, "err", err)
		}
	}

	err := h(tctx, payload, report)
	switch {
	case err == nil:
		r.finish(id, domain.TaskDone, "")
	default:
		r.finish(id, domain.TaskFailed, err.Error())
	}
}

// finish 写入终态；已被 Cancel 标记 cancelled 的行不被覆盖。
func (r *Runner) finish(id int64, state, errMsg string) {
	if _, err := r.db.ExecContext(context.Background(),
		`UPDATE tasks SET state = ?, error = ?, finished_at = ? WHERE id = ? AND state != ?`,
		state, errMsg, domain.Now(), id, domain.TaskCancelled); err != nil {
		slog.Error("task: finish failed", "id", id, "err", err)
	}
}
