package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
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

// RegisterChannelsRoutes registers channels routes on the router.
func RegisterChannelsRoutes(r *gin.RouterGroup, handler *ChannelsHandler) {
	channels := r.Group("/channels")
	{
		channels.GET("", handler.GetChannelsStatus)
		channels.POST("/:name/restart", handler.RestartChannel)
		channels.POST("/:name/start", handler.StartChannel)
		channels.POST("/:name/stop", handler.StopChannel)
	}
}
