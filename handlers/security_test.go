package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"meetup/db"
)

func TestAdminAuthRequiresAdminRole(t *testing.T) {
	if err := db.Init(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	defer db.Close()

	adminID, err := db.CreateUser("admin", "hash", "admin")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	viewerID, err := db.CreateUser("viewer", "hash", "viewer")
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	if err := db.CreateSession("admin-token", adminID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("admin session: %v", err)
	}
	if err := db.CreateSession("viewer-token", viewerID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("viewer session: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

	cases := []struct {
		name string
		role string
		want int
	}{
		{"admin", "admin-token", http.StatusNoContent},
		{"viewer", "viewer-token", http.StatusForbidden},
		{"anonymous", "", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/admin/events", nil)
			if tc.role != "" {
				req.AddCookie(&http.Cookie{Name: "meetup_session", Value: tc.role})
			}
			w := httptest.NewRecorder()
			AdminAuth(next).ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("got %d, want %d (%s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestAllowAnonymousWriteRateLimit(t *testing.T) {
	ip := "203.0.113.77"
	req := httptest.NewRequest("POST", "/api/events/x/answers", nil)
	req.RemoteAddr = ip + ":1234"
	for i := 0; i < anonymousWriteLimit; i++ {
		if !AllowAnonymousWrite(req) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if AllowAnonymousWrite(req) {
		t.Fatal("expected the request over the limit to be rejected")
	}
}

func TestSecureRequestDetection(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	if secureRequest(req) {
		t.Fatal("plain request should not be secure")
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	if !secureRequest(req) {
		t.Fatal("forwarded https should be secure")
	}
	t.Setenv("COOKIE_SECURE", "true")
	req.Header.Del("X-Forwarded-Proto")
	if !secureRequest(req) {
		t.Fatal("COOKIE_SECURE should force secure cookies")
	}
}
