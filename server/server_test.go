package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"musicbot/cache"
	"musicbot/player"
	"musicbot/queue"
)

func newTestServer(t *testing.T, password string) *Server {
	t.Helper()
	q := queue.New("")
	c := cache.New(t.TempDir(), "")
	p := player.New(q, c, 50, "default", "", "", 0)
	sm := NewSessionManager(password, 1*time.Hour)
	sm.Start()
	t.Cleanup(sm.Close)
	return New("127.0.0.1:0", p, sm)
}

func TestAuthFlow_LoginBearerLogout(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "changeme")

	token := login(t, s, "changeme")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/clients", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.authMiddleware(s.handleAdminClients)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	s.authMiddleware(s.handleAdminLogout)(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/clients", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	s.authMiddleware(s.handleAdminClients)(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", w.Code)
	}
}

func TestAuthFlow_LoginWrongPassword_401(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "changeme")

	body := `{"password":"wrongpass"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleAdminLogin(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Invalid credentials") {
		t.Fatalf("expected 'Invalid credentials', got %q", w.Body.String())
	}
}

func TestAuthFlow_LoginInvalidJSON_400(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "changeme")

	body := `{not json`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleAdminLogin(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAuthFlow_LoginMethodNotAllowed(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "changeme")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/login", nil)
	w := httptest.NewRecorder()
	s.handleAdminLogin(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestAuthFlow_LoginBodyTooLarge_400(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "changeme")

	payload := make([]byte, 2<<10)
	for i := range payload {
		payload[i] = 'x'
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleAdminLogin(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized body, got %d", w.Code)
	}
}

func TestAuthFlow_NoBearer_401(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "changeme")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/clients", nil)
	w := httptest.NewRecorder()
	s.authMiddleware(s.handleAdminClients)(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthFlow_QueryParamPassword_Rejected(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "changeme")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/clients?password=changeme", nil)
	w := httptest.NewRecorder()
	s.authMiddleware(s.handleAdminClients)(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for query param password (old fallback removed), got %d", w.Code)
	}
}

func TestAuthFlow_LogoutWithoutToken_401(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "changeme")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil)
	w := httptest.NewRecorder()
	s.authMiddleware(s.handleAdminLogout)(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthFlow_NilSessions_AllowsAll(t *testing.T) {
	t.Parallel()
	q := queue.New("")
	c := cache.New(t.TempDir(), "")
	p := player.New(q, c, 50, "default", "", "", 0)
	s := New("127.0.0.1:0", p, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/clients", nil)
	w := httptest.NewRecorder()
	s.authMiddleware(s.handleAdminClients)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 when sessions is nil, got %d", w.Code)
	}
}

func TestAuthFlow_LoginResponseFormat(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "changeme")

	body := `{"password":"changeme"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleAdminLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	tok, ok := resp["token"].(string)
	if !ok {
		t.Fatalf("missing or invalid token field")
	}
	if len(tok) != 64 {
		t.Errorf("expected token of length 64, got %d", len(tok))
	}

	expStr, ok := resp["expires_at"].(string)
	if !ok {
		t.Fatalf("missing or invalid expires_at field")
	}
	if _, err := time.Parse(time.RFC3339, expStr); err != nil {
		t.Errorf("expires_at is not valid RFC3339: %v", err)
	}
}

func TestAuthFlow_LoginThenExpired(t *testing.T) {
	t.Parallel()
	q := queue.New("")
	c := cache.New(t.TempDir(), "")
	p := player.New(q, c, 50, "default", "", "", 0)
	sm := NewSessionManager("changeme", 1*time.Millisecond)
	sm.Start()
	t.Cleanup(sm.Close)
	s := New("127.0.0.1:0", p, sm)

	token := login(t, s, "changeme")

	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/clients", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.authMiddleware(s.handleAdminClients)(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after TTL expiry, got %d", w.Code)
	}
}

func TestAuthFlow_NilSessions_LogoutDoesNotPanic(t *testing.T) {
	t.Parallel()
	q := queue.New("")
	c := cache.New(t.TempDir(), "")
	p := player.New(q, c, 50, "default", "", "", 0)
	s := New("127.0.0.1:0", p, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	w := httptest.NewRecorder()
	s.handleAdminLogout(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 when sessions is nil, got %d", w.Code)
	}
}

func TestAuthFlow_AdminRouteUnauthenticated(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "changeme")

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	w := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized {
		t.Fatalf("/admin should not require authentication; got 401")
	}
}

func TestAuthFlow_LoginReturnsTypedResponse(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "changeme")

	body := `{"password":"changeme"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleAdminLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode typed response: %v", err)
	}
	if len(resp.Token) != 64 {
		t.Errorf("expected token of length 64, got %d", len(resp.Token))
	}
	if _, err := time.Parse(time.RFC3339, resp.ExpiresAt); err != nil {
		t.Errorf("expires_at is not valid RFC3339: %v", err)
	}
}

func TestAuthFlow_NegativeSessionTTL(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig("testdata/config_negative_ttl.yaml")
	if err != nil {
		t.Skip("testdata not available, skipping config test")
	}
	if cfg.Admin.SessionTTL <= 0 {
		t.Errorf("expected positive SessionTTL after correction, got %v", cfg.Admin.SessionTTL)
	}
}

func TestAuthFlow_ClientsRejectsNonGet(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "changeme")
	token := login(t, s, "changeme")

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/api/admin/clients", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.authMiddleware(s.handleAdminClients)(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 for %s, got %d", method, w.Code)
		}
	}
}

func TestAuthFlow_DevicesRejectsNonGet(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "changeme")
	token := login(t, s, "changeme")

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/api/admin/devices", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.authMiddleware(s.handleAdminDevices)(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 for %s, got %d", method, w.Code)
		}
	}
}

func login(t *testing.T, s *Server, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleAdminLogin(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	return resp["token"].(string)
}
