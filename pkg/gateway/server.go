// Package gateway provides the HTTP API server for GoClaw.
// It exposes the P0 API contract: models, threads/runs (SSE), uploads, and memory.
package gateway

import (
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/agent"
	"github.com/bookerbai/goclaw/internal/channels"
	feishuch "github.com/bookerbai/goclaw/internal/channels/feishu"
	slackch "github.com/bookerbai/goclaw/internal/channels/slack"
	telegramch "github.com/bookerbai/goclaw/internal/channels/telegram"
	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/pkg/gateway/handlers"
)

// Server holds all dependencies for the HTTP gateway.
type Server struct {
	router *gin.Engine
	cfg    *config.AppConfig
	agent  agent.LeadAgent
}

// New creates a new Server with the given config and agent.
// It registers all middleware and routes.
func New(cfg *config.AppConfig, leadAgent agent.LeadAgent) *Server {
	router := gin.New()

	s := &Server{
		router: router,
		cfg:    cfg,
		agent:  leadAgent,
	}

	s.registerMiddleware()
	s.registerRoutes()

	return s
}

// Run starts the HTTP server on the given address (e.g. ":8001").
func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

// Handler returns the underlying http.Handler for embedding or testing.
func (s *Server) Handler() http.Handler {
	return s.router
}

// registerMiddleware attaches global middleware to the router.
func (s *Server) registerMiddleware() {
	// Recovery: converts panics into 500 responses, prevents server crashes.
	s.router.Use(gin.Recovery())

	// Structured logger: logs method, path, status, latency for every request.
	s.router.Use(gin.Logger())

	// CORS: allow all origins in development; use allowlist when configured.
	corsConfig := cors.Config{
		AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Accept"},
		ExposeHeaders:    []string{"Content-Length", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}
	if s.cfg != nil && len(s.cfg.Server.CORSOrigins) > 0 {
		corsConfig.AllowAllOrigins = false
		corsConfig.AllowOrigins = append([]string(nil), s.cfg.Server.CORSOrigins...)
	}
	s.router.Use(cors.New(corsConfig))
}

// registerRoutes wires API endpoints to their handler functions.
func (s *Server) registerRoutes() {
	// Build handler instances that hold references to config / agent.
	modelsH := handlers.NewModelsHandler(s.cfg)
	threadsH := handlers.NewThreadsHandler(s.cfg, s.agent)
	uploadsH := handlers.NewUploadsHandler(s.cfg)
	memoryH := handlers.NewMemoryHandler(s.cfg)
	mcpH := handlers.NewMCPHandler(s.cfg)
	skillsH := handlers.NewSkillsHandler(s.cfg, nil) // registry injected separately if needed
	artifactsH := handlers.NewArtifactsHandler(s.cfg, "")
	suggestionsH := handlers.NewSuggestionsHandler(s.cfg)
	agentsH := handlers.NewAgentsHandler(s.cfg)
	channelsH := handlers.NewChannelsHandler(buildChannelsManager(s.cfg))

	api := s.router.Group("/api")
	{
		// GET /api/models — list all configured models.
		api.GET("/models", modelsH.ListModels)

		// GET /api/memory — return the persisted memory snapshot.
		api.GET("/memory", memoryH.GetMemory)

		// MCP configuration routes.
		api.GET("/mcp/config", mcpH.GetConfig)
		api.PUT("/mcp/config", mcpH.UpdateConfig)

		// Skills routes.
		api.GET("/skills", skillsH.ListSkills)
		api.GET("/skills/:name", skillsH.GetSkill)
		api.PUT("/skills/:name", skillsH.UpdateSkill)
		api.POST("/skills/install", skillsH.InstallSkill)

		// Agents routes.
		api.GET("/agents", agentsH.ListAgents)
		api.GET("/agents/:name", agentsH.GetAgent)
		api.POST("/agents", agentsH.CreateAgent)
		api.PUT("/agents/:name", agentsH.UpdateAgent)
		api.DELETE("/agents/:name", agentsH.DeleteAgent)
		api.GET("/agents/check", agentsH.CheckAgentName)
		api.GET("/user-profile", agentsH.GetUserProfile)

		handlers.RegisterChannelsRoutes(api, channelsH)

		threads := api.Group("/threads/:thread_id")
		{
			// POST /api/threads/:thread_id/runs — run the lead agent and stream SSE.
			threads.POST("/runs", threadsH.RunThread)
			// POST /api/threads/:thread_id/runs/:run_id/cancel — cancel a running stream.
			threads.POST("/runs/:run_id/cancel", threadsH.CancelRun)

			// POST /api/threads/:thread_id/uploads — receive multipart files.
			threads.POST("/uploads", uploadsH.UploadFiles)

			// GET /api/threads/:thread_id/artifacts/*path — download artifacts.
			threads.GET("/artifacts/*path", artifactsH.GetArtifact)

			// POST /api/threads/:thread_id/suggestions — generate follow-up suggestions.
			threads.POST("/suggestions", suggestionsH.GenerateSuggestions)
		}
	}

	// Health check endpoint.
	s.router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy", "service": "goclaw-gateway"})
	})
}

func buildChannelsManager(cfg *config.AppConfig) *channels.Manager {
	mgr := channels.NewManager(nil, nil)
	if cfg == nil {
		return mgr
	}
	if cfg.Channels != nil {
		if cfg.Channels.Feishu != nil && cfg.Channels.Feishu.Enabled {
			_ = mgr.RegisterChannel(feishuch.New())
		}
		if cfg.Channels.Slack != nil && cfg.Channels.Slack.Enabled {
			_ = mgr.RegisterChannel(slackch.New())
		}
		if cfg.Channels.Telegram != nil && cfg.Channels.Telegram.Enabled {
			_ = mgr.RegisterChannel(telegramch.New())
		}
	}
	return mgr
}
