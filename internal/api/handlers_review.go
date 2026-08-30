package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/hinnyuu/roxy/internal/review"
)

func (s *Server) registerReview(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/review", s.requireAuth(s.handleReviewList))
	mux.HandleFunc("POST /api/review/{id}/approve", s.requireAuth(s.handleReviewApprove))
	mux.HandleFunc("POST /api/review/{id}/reject", s.requireAuth(s.handleReviewReject))
}

func (s *Server) handleReviewList(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.Review.List(r.Context(), r.URL.Query().Get("state"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if items == nil {
		items = []review.Item{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) reviewAction(w http.ResponseWriter, r *http.Request, fn func(context.Context, int64) error) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id 非法")
		return
	}
	err = fn(r.Context(), id)
	switch {
	case errors.Is(err, review.ErrNotFound):
		writeError(w, http.StatusNotFound, "工单不存在")
	case err != nil:
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (s *Server) handleReviewApprove(w http.ResponseWriter, r *http.Request) {
	s.reviewAction(w, r, s.deps.Review.Approve)
}

func (s *Server) handleReviewReject(w http.ResponseWriter, r *http.Request) {
	s.reviewAction(w, r, s.deps.Review.Reject)
}
