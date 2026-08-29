package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hinnyuu/roxy/internal/auth"
	"github.com/hinnyuu/roxy/internal/config"
	"github.com/hinnyuu/roxy/internal/db"
)

func setupServer(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	svc := auth.NewService(d)
	if err := svc.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	cfg := config.Default()
	srv := NewServer(cfg, svc, auth.NewSessionStore(), "test")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	jar := newCookieClient()
	return ts, jar
}

func newCookieClient() *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		panic(err)
	}
	return &http.Client{Jar: jar}
}

func doJSON(t *testing.T, client *http.Client, method, url string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestHealthIsPublic(t *testing.T) {
	ts, client := setupServer(t)
	resp, body := doJSON(t, client, http.MethodGet, ts.URL+"/api/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}
	if body["status"] != "ok" {
		t.Errorf("health body = %v", body)
	}
}

func TestMeRequiresAuth(t *testing.T) {
	ts, client := setupServer(t)
	resp, _ := doJSON(t, client, http.MethodGet, ts.URL+"/api/auth/me", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me without login = %d, want 401", resp.StatusCode)
	}
}

func TestLoginFlow(t *testing.T) {
	ts, client := setupServer(t)

	resp, _ := doJSON(t, client, http.MethodPost, ts.URL+"/api/auth/login",
		map[string]string{"username": "admin", "password": "wrong"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", resp.StatusCode)
	}

	resp, body := doJSON(t, client, http.MethodPost, ts.URL+"/api/auth/login",
		map[string]string{"username": "admin", "password": "admin"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d, want 200 (body=%v)", resp.StatusCode, body)
	}

	resp, body = doJSON(t, client, http.MethodGet, ts.URL+"/api/auth/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me after login = %d, want 200", resp.StatusCode)
	}
	if body["username"] != "admin" {
		t.Errorf("me body = %v", body)
	}
	if body["using_default_credentials"] != true {
		t.Errorf("using_default_credentials should be true, body=%v", body)
	}

	resp, _ = doJSON(t, client, http.MethodPost, ts.URL+"/api/auth/logout", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout = %d, want 200", resp.StatusCode)
	}
	resp, _ = doJSON(t, client, http.MethodGet, ts.URL+"/api/auth/me", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout = %d, want 401", resp.StatusCode)
	}
}

func TestCredentialsChangeEndpoint(t *testing.T) {
	ts, client := setupServer(t)
	resp, _ := doJSON(t, client, http.MethodPost, ts.URL+"/api/auth/login",
		map[string]string{"username": "admin", "password": "admin"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d", resp.StatusCode)
	}

	resp, _ = doJSON(t, client, http.MethodPut, ts.URL+"/api/auth/credentials",
		map[string]string{"old_password": "wrong", "new_password": "x"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("credentials with wrong old password = %d, want 401", resp.StatusCode)
	}

	resp, _ = doJSON(t, client, http.MethodPut, ts.URL+"/api/auth/credentials",
		map[string]string{"old_password": "admin", "new_password": "newpass"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("credentials change = %d, want 200", resp.StatusCode)
	}

	resp, body := doJSON(t, client, http.MethodGet, ts.URL+"/api/auth/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me = %d", resp.StatusCode)
	}
	if body["using_default_credentials"] != false {
		t.Errorf("default flag should be cleared, body=%v", body)
	}
}

func TestRootServesUI(t *testing.T) {
	ts, client := setupServer(t)
	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get / = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
}
