package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) registerIndex(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/index", s.requireAuth(s.handleIndexStatus))
	mux.HandleFunc("POST /api/index/refresh", s.requireAuth(s.handleIndexRefresh))
}

func (s *Server) handleIndexStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.deps.Importer.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dump_version": st.Version,
		"imported_at":  st.ImportedAt,
		"source_url":   st.SourceURL,
		"subjects":     st.Subjects,
		"episodes":     st.Episodes,
		"relations":    st.Relations,
		"task_id":      s.runningRefresh(r.Context()),
	})
}

func (s *Server) handleIndexRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LocalPath string `json:"local_path"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "请求体必须是 JSON")
			return
		}
	}
	payload, _ := json.Marshal(map[string]string{"local_path": req.LocalPath})
	id, err := s.deps.Tasks.Enqueue(r.Context(), "index_refresh", string(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"task_id": id})
}
