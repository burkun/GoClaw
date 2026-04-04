package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ChannelsHandler handles channels API endpoints.
type ChannelsHandler struct {
	// manager would be injected from channels.Manager in production.
}

// NewChannelsHandler creates a ChannelsHandler.
func NewChannelsHandler() *ChannelsHandler {
	return &ChannelsHandler{}
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
	// TODO: integrate with channels.Manager when available.
	resp := ChannelsStatusResponse{
		ServiceRunning: false,
		Channels:       map[string]ChannelInfo{},
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

	// TODO: integrate with channels.Manager when available.
	resp := ChannelRestartResponse{
		Success: false,
		Message: "channel service not initialized",
	}
	c.JSON(http.StatusOK, resp)
}

// StartChannel handles POST /api/channels/:name/start.
func (h *ChannelsHandler) StartChannel(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel name required"})
		return
	}

	// TODO: integrate with channels.Manager.
	c.JSON(http.StatusOK, gin.H{"success": false, "message": "not implemented"})
}

// StopChannel handles POST /api/channels/:name/stop.
func (h *ChannelsHandler) StopChannel(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel name required"})
		return
	}

	// TODO: integrate with channels.Manager.
	c.JSON(http.StatusOK, gin.H{"success": false, "message": "not implemented"})
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
