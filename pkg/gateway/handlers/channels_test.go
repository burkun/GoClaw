package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestChannelsHandler_GetChannelsStatus_Empty(t *testing.T) {
	h := NewChannelsHandler()
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
	h := NewChannelsHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/channels//restart", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"name": ""})

	h.RestartChannel(ctx)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestChannelsHandler_RestartChannel_NotInitialized(t *testing.T) {
	h := NewChannelsHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/channels/feishu/restart", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"name": "feishu"})

	h.RestartChannel(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp ChannelRestartResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected success=false")
	}
}

func TestChannelsHandler_StartStop_NotImplemented(t *testing.T) {
	h := NewChannelsHandler()
	{
		req := httptest.NewRequest(http.MethodPost, "/api/channels/slack/start", nil)
		rr := httptest.NewRecorder()
		ctx, _ := newGinContext(rr, req, map[string]string{"name": "slack"})
		h.StartChannel(ctx)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	}
	{
		req := httptest.NewRequest(http.MethodPost, "/api/channels/telegram/stop", nil)
		rr := httptest.NewRecorder()
		ctx, _ := newGinContext(rr, req, map[string]string{"name": "telegram"})
		h.StopChannel(ctx)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	}
}

func TestRegisterChannelsRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	RegisterChannelsRoutes(api, NewChannelsHandler())

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/channels"},
		{http.MethodPost, "/api/channels/feishu/restart"},
		{http.MethodPost, "/api/channels/feishu/start"},
		{http.MethodPost, "/api/channels/feishu/stop"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code == http.StatusNotFound {
			t.Fatalf("route not registered: %s %s", tc.method, tc.path)
		}
	}
}
