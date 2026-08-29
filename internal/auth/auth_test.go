package auth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hinnyuu/roxy/internal/db"
)

func setup(t *testing.T) (*Service, context.Context) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return NewService(d), context.Background()
}

func TestBootstrapDefaultCredentials(t *testing.T) {
	s, ctx := setup(t)
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !s.UsingDefaultCredentials(ctx) {
		t.Error("default credentials flag should be set")
	}
	if _, err := s.Login(ctx, "admin", "admin"); err != nil {
		t.Errorf("login admin/admin: %v", err)
	}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap idempotent: %v", err)
	}
}

func TestBootstrapWithEnvPassword(t *testing.T) {
	t.Setenv("ROXY_ADMIN_PASSWORD", "s3cret")
	s, ctx := setup(t)
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if s.UsingDefaultCredentials(ctx) {
		t.Error("default credentials flag must not be set when env password used")
	}
	if _, err := s.Login(ctx, "admin", "s3cret"); err != nil {
		t.Errorf("login with env password: %v", err)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	s, ctx := setup(t)
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := s.Login(ctx, "admin", "wrong"); err == nil {
		t.Error("expected error for wrong password")
	}
	if _, err := s.Login(ctx, "nobody", "admin"); err == nil {
		t.Error("expected error for unknown user")
	}
}

func TestChangeCredentialsClearsDefaultFlag(t *testing.T) {
	s, ctx := setup(t)
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	err := s.ChangeCredentials(ctx, "admin", "admin", "shiki", "newpass")
	if err != nil {
		t.Fatalf("change credentials: %v", err)
	}
	if s.UsingDefaultCredentials(ctx) {
		t.Error("default credentials flag should be cleared after change")
	}
	if _, err := s.Login(ctx, "shiki", "newpass"); err != nil {
		t.Errorf("login with new credentials: %v", err)
	}
	if _, err := s.Login(ctx, "admin", "admin"); err == nil {
		t.Error("old credentials must no longer work")
	}
}

func TestChangeCredentialsRequiresOldPassword(t *testing.T) {
	s, ctx := setup(t)
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if err := s.ChangeCredentials(ctx, "admin", "wrong", "", "newpass"); err == nil {
		t.Error("expected error for wrong old password")
	}
}

func TestSessionStore(t *testing.T) {
	st := NewSessionStore()
	token, err := st.Create("admin")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if sess, ok := st.Validate(token); !ok || sess.Username != "admin" {
		t.Errorf("validate session: ok=%v sess=%+v", ok, sess)
	}
	st.Revoke(token)
	if _, ok := st.Validate(token); ok {
		t.Error("session should be invalid after revoke")
	}
}
