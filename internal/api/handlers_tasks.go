package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/hinnyuu/roxy/internal/task"
)

func (s *Server) registerTasks(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks", s.requireAuth(s.handleTasksList))
	mux.HandleFunc("GET /api/tasks/{id}", s.requireAuth(s.handleTaskGet))
	mux.HandleFunc("POST /api/tasks/{id}/cancel", s.requireAuth(s.handleTaskCancel))
}

type taskView struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	State      string `json:"state"`
	Payload    string `json:"payload,omitempty"`
	Progress   any    `json:"progress,omitempty"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"created_at"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type rowScanner interface{ Scan(dest ...any) error }

func scanTask(rows rowScanner) (taskView, error) {
	var (
		tv                taskView
		payload, prog, er sql.NullString
		started, finished sql.NullString
	)
	err := rows.Scan(&tv.ID, &tv.Kind, &tv.State, &payload, &prog, &er,
		&tv.CreatedAt, &started, &finished)
	tv.Payload, tv.Error = payload.String, er.String
	tv.StartedAt, tv.FinishedAt = started.String, finished.String
	if prog.Valid {
		var v any
		if json.Unmarshal([]byte(prog.String), &v) == nil {
			tv.Progress = v
		} else {
			tv.Progress = prog.String
		}
	}
	return tv, err
}

const taskCols = `SELECT id, kind, state, payload, progress, error, created_at, started_at, finished_at FROM tasks`

func (s *Server) handleTasksList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where := ""
	var args []any
	if st := q.Get("state"); st != "" {
		where += ` WHERE state = ?`
		args = append(args, st)
	}
	limit := 100
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 500 {
		limit = n
	}
	rows, err := s.db().QueryContext(r.Context(),
		taskCols+where+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()
	out := make([]taskView, 0)
	for rows.Next() {
		tv, err := scanTask(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out = append(out, tv)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleTaskGet(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id 非法")
		return
	}
	row := s.db().QueryRowContext(r.Context(), taskCols+` WHERE id = ?`, id)
	tv, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, tv)
}

func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id 非法")
		return
	}
	err = s.deps.Tasks.Cancel(r.Context(), id)
	switch {
	case errors.Is(err, task.ErrNotFound):
		writeError(w, http.StatusNotFound, "任务不存在")
	case err != nil:
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// runningRefresh 返回当前排队/运行中的 index_refresh 任务 id（0=无）。
func (s *Server) runningRefresh(ctx context.Context) int64 {
	var id int64
	err := s.db().QueryRowContext(ctx,
		`SELECT id FROM tasks WHERE kind = 'index_refresh' AND state IN ('queued','running') ORDER BY id LIMIT 1`,
	).Scan(&id)
	if err != nil {
		return 0
	}
	return id
}
