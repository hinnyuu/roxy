package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/hinnyuu/roxy/internal/domain"
	"github.com/hinnyuu/roxy/internal/scanner"
)

func (s *Server) registerSources(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sources", s.requireAuth(s.handleSourcesList))
	mux.HandleFunc("POST /api/sources", s.requireAuth(s.handleSourcesCreate))
	mux.HandleFunc("PUT /api/sources/{id}", s.requireAuth(s.handleSourcesUpdate))
	mux.HandleFunc("DELETE /api/sources/{id}", s.requireAuth(s.handleSourcesDelete))
	mux.HandleFunc("POST /api/sources/{id}/scan", s.requireAuth(s.handleSourcesScan))
	mux.HandleFunc("GET /api/sources/{id}/files", s.requireAuth(s.handleSourceFiles))
}

type sourceView struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Provider  string `json:"provider_type"`
	Enabled   bool   `json:"enabled"`
	FileCount int    `json:"file_count"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) handleSourcesList(w http.ResponseWriter, r *http.Request) {
	srcs, err := s.deps.Sources.ListSources(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]sourceView, 0, len(srcs))
	for _, src := range srcs {
		var n int
		if err := s.db().QueryRow(`SELECT COUNT(*) FROM source_files WHERE source_id = ?`, src.ID).Scan(&n); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out = append(out, sourceView{src.ID, src.Name, src.Path, src.Kind, src.ProviderType,
			src.Enabled, n, src.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

type sourceRequest struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Kind    string `json:"kind,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

func (req *sourceRequest) validate() string {
	switch req.Kind {
	case "", "mixed", "video", "subtitle":
	default:
		return "kind 必须是 mixed|video|subtitle"
	}
	if req.Name == "" || req.Path == "" {
		return "name 与 path 必填"
	}
	return ""
}

func (s *Server) handleSourcesCreate(w http.ResponseWriter, r *http.Request) {
	var req sourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体必须是 JSON")
		return
	}
	if msg := req.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if fi, err := os.Stat(req.Path); err != nil || !fi.IsDir() {
		writeError(w, http.StatusBadRequest, "path 必须是已存在的目录")
		return
	}
	kind := req.Kind
	if kind == "" {
		kind = "mixed"
	}
	src := &domain.Source{Name: req.Name, Path: req.Path, Kind: kind,
		ProviderType: "dirscan", Enabled: true}
	if err := s.deps.Sources.CreateSource(r.Context(), src); err != nil {
		if isUniqueErr(err) {
			writeError(w, http.StatusConflict, "该 path 已注册")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, sourceView{src.ID, src.Name, src.Path, src.Kind,
		src.ProviderType, src.Enabled, 0, src.CreatedAt})
}

func (s *Server) handleSourcesUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id 非法")
		return
	}
	src, err := s.deps.Sources.GetSource(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "源不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var req sourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体必须是 JSON")
		return
	}
	if req.Name != "" {
		src.Name = req.Name
	}
	if req.Path != "" {
		if fi, err := os.Stat(req.Path); err != nil || !fi.IsDir() {
			writeError(w, http.StatusBadRequest, "path 必须是已存在的目录")
			return
		}
		src.Path = req.Path
	}
	if req.Kind != "" {
		src.Kind = req.Kind
	}
	if req.Enabled != nil {
		src.Enabled = *req.Enabled
	}
	switch src.Kind {
	case "mixed", "video", "subtitle":
	default:
		writeError(w, http.StatusBadRequest, "kind 必须是 mixed|video|subtitle")
		return
	}
	if err := s.deps.Sources.UpdateSource(r.Context(), src); err != nil {
		if isUniqueErr(err) {
			writeError(w, http.StatusConflict, "该 path 已注册")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSourcesDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id 非法")
		return
	}
	err = s.deps.Sources.DeleteSource(r.Context(), id)
	switch {
	case errors.Is(err, scanner.ErrNotEmpty):
		writeError(w, http.StatusConflict, "源下仍有登记文件，不可删除")
		return
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "源不存在")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSourcesScan(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id 非法")
		return
	}
	if _, err := s.deps.Sources.GetSource(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "源不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	taskID, err := s.deps.Tasks.Enqueue(r.Context(), "scan", scanner.ScanPayload{SourceID: id}.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"task_id": taskID})
}

type fileView struct {
	ID          int64  `json:"id"`
	AbsPath     string `json:"abs_path"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	Size        int64  `json:"size"`
	MTime       string `json:"mtime"`
	ParseResult any    `json:"parse_result,omitempty"`
}

func (s *Server) handleSourceFiles(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id 非法")
		return
	}
	rows, err := s.db().QueryContext(r.Context(),
		`SELECT id, abs_path, kind, status, size, mtime, parse_result
		 FROM source_files WHERE source_id = ? ORDER BY id DESC LIMIT 500`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()
	out := make([]fileView, 0)
	for rows.Next() {
		var (
			fv    fileView
			parse sql.NullString
		)
		if err := rows.Scan(&fv.ID, &fv.AbsPath, &fv.Kind, &fv.Status, &fv.Size, &fv.MTime, &parse); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if parse.Valid {
			var v any
			if json.Unmarshal([]byte(parse.String), &v) == nil {
				fv.ParseResult = v
			}
		}
		out = append(out, fv)
	}
	writeJSON(w, http.StatusOK, out)
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func isUniqueErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE")
}
