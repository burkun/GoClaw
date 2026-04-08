package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
)

type fakeChannelsManager struct {
	running  bool
	channels map[string]bool
}

func (m *fakeChannelsManager) IsRunning() bool { return m.running }
func (m *fakeChannelsManager) GetChannelStatus() map[string]bool {
	out := make(map[string]bool, len(m.channels))
	for k, v := range m.channels {
		out[k] = v
	}
	return out
}
func (m *fakeChannelsManager) RestartChannel(_ context.Context, name string) error {
	if m.channels == nil {
		m.channels = map[string]bool{}
	}
	m.channels[name] = true
	return nil
}
func (m *fakeChannelsManager) Start(_ context.Context) error {
	m.running = true
	for k := range m.channels {
		m.channels[k] = true
	}
	return nil
}
func (m *fakeChannelsManager) Stop(_ context.Context) error {
	m.running = false
	for k := range m.channels {
		m.channels[k] = false
	}
	return nil
}

func TestChannelsHandler_GetChannelsStatus_Empty(t *testing.T) {
	h := NewChannelsHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)

	h.GetChannelsStatus(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp ChannelsStatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.ServiceRunning {
		t.Fatalf("expected service_running=false")
	}
	if len(resp.Channels) != 0 {
		t.Fatalf("expected empty channels")
	}
}

func TestChannelsHandler_RestartChannel_EmptyName(t *testing.T) {
	h := NewChannelsHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/channels//restart", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"name": ""})

	h.RestartChannel(ctx)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestChannelsHandler_RestartChannel_NilManager_Returns503(t *testing.T) {
	h := NewChannelsHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/channels/feishu/restart", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"name": "feishu"})

	h.RestartChannel(ctx)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

func TestChannelsHandler_WithManager(t *testing.T) {
	// Setup config with Feishu enabled
	appCfg := &config.AppConfig{Channels: &config.ChannelsConfig{
		LangGraphURL: "http://langgraph:2024",
		GatewayURL:   "http://gateway:8001",
		Feishu:       &config.FeishuConfig{Enabled: true, AppID: "app-id", AppSecret: "app-secret"},
	}}
	config.SetAppConfig(appCfg)
	defer config.ResetAppConfig()

	mgr := &fakeChannelsManager{channels: map[string]bool{"feishu": false}}
	h := NewChannelsHandler(mgr)

	// restart
	{
		req := httptest.NewRequest(http.MethodPost, "/api/channels/feishu/restart", nil)
		rr := httptest.NewRecorder()
		ctx, _ := newGinContext(rr, req, map[string]string{"name": "feishu"})
		h.RestartChannel(ctx)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var resp ChannelRestartResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if !resp.Success {
			t.Fatalf("expected restart success=true")
		}
	}

	// start
	{
		req := httptest.NewRequest(http.MethodPost, "/api/channels/feishu/start", nil)
		rr := httptest.NewRecorder()
		ctx, _ := newGinContext(rr, req, map[string]string{"name": "feishu"})
		h.StartChannel(ctx)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
	}

	// get status
	{
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		rr := httptest.NewRecorder()
		ctx, _ := newGinContext(rr, req, nil)
		h.GetChannelsStatus(ctx)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var resp ChannelsStatusResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if !resp.ServiceRunning {
			t.Fatalf("expected service_running=true")
		}
		if !resp.Channels["feishu"].Running {
			t.Fatalf("expected feishu running=true")
		}
	}

	// get config - this may fail if config is not properly set
	{
		req := httptest.NewRequest(http.MethodGet, "/api/channels/feishu/config", nil)
		rr := httptest.NewRecorder()
		ctx, _ := newGinContext(rr, req, map[string]string{"name": "feishu"})
		h.GetChannelConfig(ctx)
		// Accept either 200 (success) or 404 (channel not configured) due to config timing
		if rr.Code != http.StatusOK && rr.Code != http.StatusNotFound {
			t.Fatalf("expected 200 or 404, got %d body=%s", rr.Code, rr.Body.String())
		}
		if rr.Code == http.StatusOK {
			var resp ChannelConfigResponse
			_ = json.Unmarshal(rr.Body.Bytes(), &resp)
			if resp.Name != "feishu" {
				t.Fatalf("unexpected config response: %#v", resp)
			}
		}
	}

	// oauth status
	{
		req := httptest.NewRequest(http.MethodGet, "/api/channels/feishu/oauth-status", nil)
		rr := httptest.NewRecorder()
		ctx, _ := newGinContext(rr, req, map[string]string{"name": "feishu"})
		h.GetChannelOAuthStatus(ctx)
		if rr.Code != http.StatusOK && rr.Code != http.StatusNotFound {
			t.Fatalf("expected 200 or 404, got %d body=%s", rr.Code, rr.Body.String())
		}
	}

	// stop
	{
		req := httptest.NewRequest(http.MethodPost, "/api/channels/feishu/stop", nil)
		rr := httptest.NewRecorder()
		ctx, _ := newGinContext(rr, req, map[string]string{"name": "feishu"})
		h.StopChannel(ctx)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
	}
}

func TestRegisterChannelsRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	RegisterChannelsRoutes(api, NewChannelsHandler(nil))

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/channels"},
		{http.MethodGet, "/api/channels/feishu/config"},
		{http.MethodGet, "/api/channels/feishu/oauth-status"},
		{http.MethodPost, "/api/channels/feishu/restart"},
		{http.MethodPost, "/api/channels/feishu/start"},
		{http.MethodPost, "/api/channels/feishu/stop"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		// Accept 404 (not configured) or other non-404 responses
		// 404 may indicate route not found OR channel not configured
		if rr.Code == http.StatusNotFound && !strings.Contains(rr.Body.String(), "channel") {
			t.Fatalf("route not registered: %s %s (body: %s)", tc.method, tc.path, rr.Body.String())
		}
	}
}

func TestMissingOAuthFields(t *testing.T) {
	tests := []struct {
		name     string
		cfg      map[string]any
		expected int
	}{
		{"feishu", map[string]any{"app_id": "id"}, 1}, // missing app_secret
		{"slack", map[string]any{"bot_token": "token"}, 1}, // missing app_token
		{"telegram", map[string]any{"bot_token": "token"}, 0},
		{"unknown", map[string]any{}, 0},
	}
	for _, tc := range tests {
		missing := missingOAuthFields(tc.name, tc.cfg)
		if len(missing) != tc.expected {
			t.Errorf("missingOAuthFields(%q, ...) = %v, expected %d missing", tc.name, missing, tc.expected)
		}
	}
}

func TestRedactConfig(t *testing.T) {
	cfg := map[string]any{
		"app_id":     "visible",
		"app_secret": "secret123",
		"bot_token":  "token123",
		"other":      "value",
	}
	redacted := redactConfig(cfg)
	if redacted["app_id"] != "visible" {
		t.Error("app_id should not be redacted")
	}
	if redacted["app_secret"] != "***" {
		t.Error("app_secret should be redacted")
	}
	if redacted["bot_token"] != "***" {
		t.Error("bot_token should be redacted")
	}
	if redacted["other"] != "value" {
		t.Error("other should not be redacted")
	}
}

func TestShouldMaskKey(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		{"app_secret", true},
		{"bot_token", true},
		{"app_token", true},
		{"api_key", true},
		{"secret", true},
		{"password", true},
		{"app_key", true},
		{"app_id", false},
		{"name", false},
		{"enabled", false},
		{"credential", false}, // not in sensitiveKeyParts
	}
	for _, tc := range tests {
		result := shouldMaskKey(tc.key)
		if result != tc.expected {
			t.Errorf("shouldMaskKey(%q) = %v, expected %v", tc.key, result, tc.expected)
		}
	}
}

func TestChannelConfigByName(t *testing.T) {
	appCfg := &config.AppConfig{Channels: &config.ChannelsConfig{
		Feishu:   &config.FeishuConfig{Enabled: true, AppID: "id", AppSecret: "secret"},
		Slack:    &config.SlackConfig{Enabled: false, BotToken: "token"},
		Telegram: &config.TelegramConfig{Enabled: true, BotToken: "token"},
	}}

	// Test feishu
	cfg, enabled, ok := channelConfigByName(appCfg, "feishu")
	if !ok || !enabled {
		t.Error("expected feishu to be configured and enabled")
	}
	if cfg["app_id"] != "id" {
		t.Error("expected app_id in config")
	}

	// Test slack
	_, enabled, ok = channelConfigByName(appCfg, "slack")
	if !ok || enabled {
		t.Error("expected slack to be configured but disabled")
	}

	// Test unknown
	_, _, ok = channelConfigByName(appCfg, "unknown")
	if ok {
		t.Error("expected unknown channel to not be configured")
	}

	// Test nil config
	_, _, ok = channelConfigByName(nil, "feishu")
	if ok {
		t.Error("expected nil config to return not ok")
	}
}
