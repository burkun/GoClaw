package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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

func TestChannelsHandler_WithManager(t *testing.T) {
	mgr := &fakeChannelsManager{channels: map[string]bool{"feishu": false}}
	h := NewChannelsHandler(mgr)

	// restart
	{
		req := httptest.NewRequest(http.MethodPost, "/api/channels/feishu/restart", nil)
		rr := httptest.NewRecorder()
		ctx, _ := newGinContext(rr, req, map[string]string{"name": "feishu"})
		h.RestartChannel(ctx)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
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
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	}

	// get status
	{
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		rr := httptest.NewRecorder()
		ctx, _ := newGinContext(rr, req, nil)
		h.GetChannelsStatus(ctx)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
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

	// stop
	{
		req := httptest.NewRequest(http.MethodPost, "/api/channels/feishu/stop", nil)
		rr := httptest.NewRecorder()
		ctx, _ := newGinContext(rr, req, map[string]string{"name": "feishu"})
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
	RegisterChannelsRoutes(api, NewChannelsHandler(nil))

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
