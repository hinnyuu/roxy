package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	defaultUsername    = "admin"
	defaultPassword    = "admin"
	settingDefaultCred = "auth.default_credentials"
	sessionTTL         = 7 * 24 * time.Hour
)

var (
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrUserNotFound       = errors.New("用户不存在")
)

type User struct {
	ID       int64
	Username string
	Role     string
}

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// Bootstrap 在 users 表为空时创建初始管理员。
// 密码取 ROXY_ADMIN_PASSWORD，缺省为 "admin"；使用缺省密码时打默认凭据标记，
// WebUI 据此显示常驻警告（docs/ARCHITECTURE.md §11）。
func (s *Service) Bootstrap(ctx context.Context) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		return nil
	}
	pw := os.Getenv("ROXY_ADMIN_PASSWORD")
	usingDefault := false
	if pw == "" {
		pw = defaultPassword
		usingDefault = true
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash initial password: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role, created_at, updated_at)
		 VALUES (?, ?, 'admin', ?, ?)`,
		defaultUsername, string(hash), now, now); err != nil {
		return fmt.Errorf("create initial admin: %w", err)
	}
	if usingDefault {
		if _, err := s.db.ExecContext(ctx,
			`INSERT OR REPLACE INTO settings (key, value) VALUES (?, '1')`,
			settingDefaultCred); err != nil {
			return fmt.Errorf("mark default credentials: %w", err)
		}
		slog.Warn("使用默认密码 admin/admin 初始化，请尽快在设置页修改")
	}
	return nil
}

func (s *Service) Login(ctx context.Context, username, password string) (*User, error) {
	var u User
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, role, password_hash FROM users WHERE username = ?`,
		username).Scan(&u.ID, &u.Username, &u.Role, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return &u, nil
}

// ChangeCredentials 修改当前用户的用户名与/或密码。验证旧密码后更新；
// 成功后清除默认凭据标记。
func (s *Service) ChangeCredentials(ctx context.Context, username, oldPassword, newUsername, newPassword string) error {
	u, err := s.Login(ctx, username, oldPassword)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if newPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash new password: %w", err)
		}
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
			string(hash), now, u.ID); err != nil {
			return fmt.Errorf("update password: %w", err)
		}
	}
	if newUsername != "" && newUsername != username {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users SET username = ?, updated_at = ? WHERE id = ?`,
			newUsername, now, u.ID); err != nil {
			return fmt.Errorf("update username: %w", err)
		}
	}
	if newPassword != "" || newUsername != "" {
		if _, err := s.db.ExecContext(ctx,
			`DELETE FROM settings WHERE key = ?`, settingDefaultCred); err != nil {
			return fmt.Errorf("clear default credentials flag: %w", err)
		}
	}
	return nil
}

func (s *Service) UsingDefaultCredentials(ctx context.Context) bool {
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM settings WHERE key = ?`, settingDefaultCred).Scan(&v)
	return err == nil && v == "1"
}

type Session struct {
	Username  string
	ExpiresAt time.Time
}

// SessionStore 是内存会话存储（M0 单用户场景足够；后续可换持久化）。
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]Session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]Session)}
}

func (st *SessionStore) Create(username string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	token := hex.EncodeToString(buf)
	st.mu.Lock()
	st.sessions[token] = Session{Username: username, ExpiresAt: time.Now().Add(sessionTTL)}
	st.mu.Unlock()
	return token, nil
}

func (st *SessionStore) Validate(token string) (Session, bool) {
	st.mu.RLock()
	sess, ok := st.sessions[token]
	st.mu.RUnlock()
	if !ok || time.Now().After(sess.ExpiresAt) {
		return Session{}, false
	}
	return sess, true
}

func (st *SessionStore) Revoke(token string) {
	st.mu.Lock()
	delete(st.sessions, token)
	st.mu.Unlock()
}
