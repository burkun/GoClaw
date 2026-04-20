package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"goclaw/internal/config"
)

func TestOAuthTokenManager_GetToken_Cache(t *testing.T) {
	var hit int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "abc123",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer ts.Close()

	mgr := NewOAuthTokenManager(nil)
	cfg := &config.MCPOAuthConfig{TokenURL: ts.URL, ClientID: "id", ClientSecret: "secret"}

	tok1, err := mgr.GetToken(context.Background(), "srv", cfg)
	if err != nil {
		t.Fatalf("first GetToken failed: %v", err)
	}
	tok2, err := mgr.GetToken(context.Background(), "srv", cfg)
	if err != nil {
		t.Fatalf("second GetToken failed: %v", err)
	}
	if tok1 != "Bearer abc123" || tok2 != tok1 {
		t.Fatalf("unexpected tokens: %q %q", tok1, tok2)
	}
	if atomic.LoadInt32(&hit) != 1 {
		t.Fatalf("expected token endpoint hit once, got %d", hit)
	}
}

func TestApplyOAuthHeader(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "xyz",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer ts.Close()

	globalOAuthTokenManager = NewOAuthTokenManager(nil)
	headers, err := applyOAuthHeader(context.Background(), "srv", config.MCPServerConfig{
		OAuth: &config.MCPOAuthConfig{TokenURL: ts.URL, ClientID: "id", ClientSecret: "secret"},
	}, map[string]string{"X-Test": "1"})
	if err != nil {
		t.Fatalf("applyOAuthHeader failed: %v", err)
	}
	if headers["Authorization"] == "" {
		t.Fatalf("expected Authorization header")
	}
}
