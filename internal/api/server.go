package api

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/hinnyuu/roxy/internal/auth"
	"github.com/hinnyuu/roxy/internal/config"
	"github.com/hinnyuu/roxy/internal/matcher"
	"github.com/hinnyuu/roxy/internal/metadata"
	"github.com/hinnyuu/roxy/internal/review"
	"github.com/hinnyuu/roxy/internal/scanner"
	"github.com/hinnyuu/roxy/internal/task"
)

//go:embed ui/index.html
var uiFS embed.FS

const sessionCookie = "roxy_session"

// Deps M2 起的服务依赖（main.go 装配）。
type Deps struct {
	DB       *sql.DB
	Sources  *scanner.Store
	Scanner  *scanner.Scanner
	Matcher  *matcher.Matcher
	Review   *review.Service
	Tasks    *task.Runner
	Importer *metadata.Importer
	Index    *metadata.Index
}

type Server struct {
	cfg      *config.Config
	auth     *auth.Service
	sessions *auth.SessionStore
	version  string
	deps     Deps
}

func NewServer(cfg *config.Config, authSvc *auth.Service, sessions *auth.SessionStore, version string, deps Deps) *Server {
	return &Server{cfg: cfg, auth: authSvc, sessions: sessions, version: version, deps: deps}
}

func (s *Server) db() *sql.DB { return s.deps.DB }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("GET /api/auth/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("PUT /api/auth/credentials", s.requireAuth(s.handleCredentials))
	mux.HandleFunc("GET /api/health", s.handleHealth)

	s.registerSources(mux)
	s.registerReview(mux)
	s.registerTasks(mux)
	s.registerIndex(mux)

	ui, _ := uiFS.ReadFile("ui/index.html")
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(ui)
	})

	return withLogging(mux)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体必须是 JSON")
		return
	}
	u, err := s.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "用户名或密码错误")
			return
		}
		slog.Error("login failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	token, err := s.sessions.Create(u.Username)
	if err != nil {
		slog.Error("create session failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((7 * 24 * time.Hour).Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"username": u.Username, "role": u.Role})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.Revoke(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value(ctxKeyUsername).(string)
	writeJSON(w, http.StatusOK, map[string]any{
		"username":                  username,
		"using_default_credentials": s.auth.UsingDefaultCredentials(r.Context()),
		"version":                   s.version,
	})
}

type credentialsRequest struct {
	OldPassword string `json:"old_password"`
	NewUsername string `json:"new_username,omitempty"`
	NewPassword string `json:"new_password,omitempty"`
}

func (s *Server) handleCredentials(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value(ctxKeyUsername).(string)
	var req credentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体必须是 JSON")
		return
	}
	if req.NewPassword == "" && req.NewUsername == "" {
		writeError(w, http.StatusBadRequest, "new_username 与 new_password 至少提供一个")
		return
	}
	err := s.auth.ChangeCredentials(r.Context(), username, req.OldPassword, req.NewUsername, req.NewPassword)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "旧密码错误")
			return
		}
		slog.Error("change credentials failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": s.version})
}

type ctxKey int

const ctxKeyUsername ctxKey = iota

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "未登录")
			return
		}
		sess, ok := s.sessions.Validate(c.Value)
		if !ok {
			writeError(w, http.StatusUnauthorized, "会话无效或已过期")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyUsername, sess.Username)
		next(w, r.WithContext(ctx))
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("http", "method", r.Method, "path", r.URL.Path,
			"dur", time.Since(start).Round(time.Microsecond).String())
	})
}
