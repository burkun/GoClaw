package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"goclaw/internal/config"
)

type channelsManager interface {
	IsRunning() bool
	GetChannelStatus() map[string]bool
	RestartChannel(ctx context.Context, name string) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// ChannelsHandler handles channels API endpoints.
type ChannelsHandler struct {
	manager channelsManager
}

// NewChannelsHandler creates a ChannelsHandler.
func NewChannelsHandler(manager channelsManager) *ChannelsHandler {
	return &ChannelsHandler{manager: manager}
}

// ChannelInfo represents a single channel's status.
type ChannelInfo struct {
	Enabled bool `json:"enabled"`
	Running bool `json:"running"`
}

// ChannelsStatusResponse is the response for GET /api/channels.
type ChannelsStatusResponse struct {
	ServiceRunning bool                   `json:"service_running"`
	Channels       map[string]ChannelInfo `json:"channels"`
}

// ChannelRestartResponse is the response for POST /api/channels/:name/restart.
type ChannelRestartResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type ChannelConfigResponse struct {
	Name         string         `json:"name"`
	Enabled      bool           `json:"enabled"`
	Running      bool           `json:"running"`
	LangGraphURL string         `json:"langgraph_url,omitempty"`
	GatewayURL   string         `json:"gateway_url,omitempty"`
	Config       map[string]any `json:"config"`
}

type ChannelOAuthStatusResponse struct {
	Name          string   `json:"name"`
	Configured    bool     `json:"configured"`
	MissingFields []string `json:"missing_fields"`
	Running       bool     `json:"running"`
}

var sensitiveKeyParts = []string{"token", "secret", "password", "api_key", "app_key"}

// GetChannelsStatus handles GET /api/channels.
func (h *ChannelsHandler) GetChannelsStatus(c *gin.Context) {
	resp := ChannelsStatusResponse{
		ServiceRunning: false,
		Channels:       map[string]ChannelInfo{},
	}
	if h.manager != nil {
		resp.ServiceRunning = h.manager.IsRunning()
		for name, running := range h.manager.GetChannelStatus() {
			resp.Channels[name] = ChannelInfo{Enabled: true, Running: running}
		}
	}
	c.JSON(http.StatusOK, resp)
}

// GetChannelConfig handles GET /api/channels/:name/config.
func (h *ChannelsHandler) GetChannelConfig(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel name required"})
		return
	}

	appCfg, err := config.GetAppConfig()
	if err != nil {
		NewServiceUnavailableError("config unavailable").Render(c, http.StatusServiceUnavailable)
		return
	}

	rawCfg, enabled, ok := channelConfigByName(appCfg, name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not configured"})
		return
	}

	running := false
	if h.manager != nil {
		running = h.manager.GetChannelStatus()[name]
	}

	resp := ChannelConfigResponse{
		Name:         name,
		Enabled:      enabled,
		Running:      running,
		LangGraphURL: strings.TrimSpace(appCfg.Channels.LangGraphURL),
		GatewayURL:   strings.TrimSpace(appCfg.Channels.GatewayURL),
		Config:       redactConfig(rawCfg),
	}
	c.JSON(http.StatusOK, resp)
}

// GetChannelOAuthStatus handles GET /api/channels/:name/oauth-status.
func (h *ChannelsHandler) GetChannelOAuthStatus(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel name required"})
		return
	}

	appCfg, err := config.GetAppConfig()
	if err != nil {
		NewServiceUnavailableError("config unavailable").Render(c, http.StatusServiceUnavailable)
		return
	}

	rawCfg, _, ok := channelConfigByName(appCfg, name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not configured"})
		return
	}

	missing := missingOAuthFields(name, rawCfg)
	running := false
	if h.manager != nil {
		running = h.manager.GetChannelStatus()[name]
	}

	c.JSON(http.StatusOK, ChannelOAuthStatusResponse{
		Name:          name,
		Configured:    len(missing) == 0,
		MissingFields: missing,
		Running:       running,
	})
}

// RestartChannel handles POST /api/channels/:name/restart.
func (h *ChannelsHandler) RestartChannel(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel name required"})
		return
	}
	if h.manager == nil {
		NewServiceUnavailableError("channel service not initialized").Render(c, http.StatusServiceUnavailable)
		return
	}
	if err := h.manager.RestartChannel(c.Request.Context(), name); err != nil {
		NewServiceUnavailableError("restart failed").Render(c, http.StatusServiceUnavailable)
		return
	}
	c.JSON(http.StatusOK, ChannelRestartResponse{Success: true, Message: "channel restarted"})
}

// StartChannel handles POST /api/channels/:name/start.
func (h *ChannelsHandler) StartChannel(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel name required"})
		return
	}
	if h.manager == nil {
		NewServiceUnavailableError("channel service not initialized").Render(c, http.StatusServiceUnavailable)
		return
	}
	if err := h.manager.Start(c.Request.Context()); err != nil {
		NewServiceUnavailableError("start failed").Render(c, http.StatusServiceUnavailable)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "channel service started"})
}

// StopChannel handles POST /api/channels/:name/stop.
func (h *ChannelsHandler) StopChannel(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel name required"})
		return
	}
	if h.manager == nil {
		NewServiceUnavailableError("channel service not initialized").Render(c, http.StatusServiceUnavailable)
		return
	}
	if err := h.manager.Stop(c.Request.Context()); err != nil {
		NewServiceUnavailableError("stop failed").Render(c, http.StatusServiceUnavailable)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "channel service stopped"})
}

func channelConfigByName(appCfg *config.AppConfig, name string) (map[string]any, bool, bool) {
	if appCfg == nil || appCfg.Channels == nil {
		return nil, false, false
	}
	ch := appCfg.Channels
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "feishu":
		if ch.Feishu == nil {
			return nil, false, false
		}
		return map[string]any{
			"enabled":    ch.Feishu.Enabled,
			"app_id":     ch.Feishu.AppID,
			"app_secret": ch.Feishu.AppSecret,
			"domain":     ch.Feishu.Domain,
		}, ch.Feishu.Enabled, true
	case "slack":
		if ch.Slack == nil {
			return nil, false, false
		}
		return map[string]any{
			"enabled":       ch.Slack.Enabled,
			"bot_token":     ch.Slack.BotToken,
			"app_token":     ch.Slack.AppToken,
			"allowed_users": ch.Slack.AllowedUsers,
		}, ch.Slack.Enabled, true
	case "telegram":
		if ch.Telegram == nil {
			return nil, false, false
		}
		return map[string]any{
			"enabled":       ch.Telegram.Enabled,
			"bot_token":     ch.Telegram.BotToken,
			"allowed_users": ch.Telegram.AllowedUsers,
			"session":       ch.Telegram.Session,
			"users":         ch.Telegram.Users,
		}, ch.Telegram.Enabled, true
	default:
		return nil, false, false
	}
}

func missingOAuthFields(name string, cfg map[string]any) []string {
	required := map[string][]string{
		"slack":    {"bot_token", "app_token"},
		"feishu":   {"app_id", "app_secret"},
		"telegram": {"bot_token"},
	}
	fields := required[strings.ToLower(strings.TrimSpace(name))]
	if len(fields) == 0 {
		return nil
	}
	missing := make([]string, 0, len(fields))
	for _, field := range fields {
		v, ok := cfg[field]
		s, okStr := v.(string)
		if !ok || !okStr || strings.TrimSpace(s) == "" {
			missing = append(missing, field)
		}
	}
	return missing
}

func redactConfig(cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if shouldMaskKey(k) {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				out[k] = "***"
				continue
			}
		}
		switch vv := v.(type) {
		case map[string]any:
			out[k] = redactConfig(vv)
		default:
			out[k] = v
		}
	}
	return out
}

func shouldMaskKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, part := range sensitiveKeyParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

// RegisterChannelsRoutes registers channels routes on the router.
func RegisterChannelsRoutes(r *gin.RouterGroup, handler *ChannelsHandler) {
	channels := r.Group("/channels")
	{
		channels.GET("", handler.GetChannelsStatus)
		channels.GET("/:name/config", handler.GetChannelConfig)
		channels.GET("/:name/oauth-status", handler.GetChannelOAuthStatus)
		channels.POST("/:name/restart", handler.RestartChannel)
		channels.POST("/:name/start", handler.StartChannel)
		channels.POST("/:name/stop", handler.StopChannel)
	}
}
