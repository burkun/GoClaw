package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bookerbai/goclaw/internal/config"
)

func TestServerCORS_DefaultAllowAll(t *testing.T) {
	s := New(&config.AppConfig{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://foo.example")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected wildcard cors origin, got %q", got)
	}
}

func TestServerCORS_AllowOriginsFromConfig(t *testing.T) {
	s := New(&config.AppConfig{Server: config.ServerConfig{CORSOrigins: []string{"https://allowed.example"}}}, nil)

	allowedReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	allowedReq.Header.Set("Origin", "https://allowed.example")
	allowedRR := httptest.NewRecorder()
	s.Handler().ServeHTTP(allowedRR, allowedReq)
	if got := allowedRR.Header().Get("Access-Control-Allow-Origin"); got != "https://allowed.example" {
		t.Fatalf("expected allowed origin, got %q", got)
	}

	blockedReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	blockedReq.Header.Set("Origin", "https://blocked.example")
	blockedRR := httptest.NewRecorder()
	s.Handler().ServeHTTP(blockedRR, blockedReq)
	if got := blockedRR.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS allow header for blocked origin, got %q", got)
	}
}
