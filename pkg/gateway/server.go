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
	"github.com/bookerbai/goclaw/internal/logging"
	"github.com/bookerbai/goclaw/internal/tracing"
	"github.com/bookerbai/goclaw/pkg/gateway/handlers"
)

// Server holds all dependencies for the HTTP gateway.
type Server struct {
	router *gin.Engine
	cfg    *config.AppConfig
	agent  agent.LeadAgent
	agents map[string]agent.LeadAgent // P1 fix: 支持多agent
}

// New creates a new Server with the given config and agent.
// It registers all middleware and routes.
func New(cfg *config.AppConfig, leadAgent agent.LeadAgent) *Server {
	router := gin.New()

	s := &Server{
		router: router,
		cfg:    cfg,
		agent:  leadAgent,
		agents: map[string]agent.LeadAgent{"default": leadAgent}, // 向后兼容
	}

	// Initialize tracing (Langfuse, etc.) if configured
	if err := tracing.AppendGlobalCallbacks(); err != nil {
		logging.Warn("tracing initialization failed, continuing without tracing", "error", err)
	}

	s.registerMiddleware()
	s.registerRoutes()

	return s
}

// NewWithAgents creates a new Server with multiple agents (P1 fix).
func NewWithAgents(cfg *config.AppConfig, defaultAgent agent.LeadAgent, agents map[string]agent.LeadAgent) *Server {
	router := gin.New()

	s := &Server{
		router: router,
		cfg:    cfg,
		agent:  defaultAgent,
		agents: agents,
	}

	s.registerMiddleware()
	s.registerRoutes()

	return s
}

// GetAgent returns the agent by name, or default agent if not found.
func (s *Server) GetAgent(name string) agent.LeadAgent {
	if name == "" {
		return s.agent
	}

	if a, ok := s.agents[name]; ok {
		return a
	}

	return s.agent
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

	// Prometheus metrics: collect HTTP request metrics.
	s.router.Use(handlers.PrometheusMiddleware())

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
	threadsH := handlers.NewThreadsHandler(s.cfg, s.agent, nil) // nil = use default file store
	uploadsH := handlers.NewUploadsHandler(s.cfg)
	memoryH := handlers.NewMemoryHandler(s.cfg)
	mcpH := handlers.NewMCPHandler(s.cfg)
	skillsH := handlers.NewSkillsHandler(s.cfg, nil) // registry injected separately if needed
	artifactsH := handlers.NewArtifactsHandler(s.cfg, "")
	suggestionsH := handlers.NewSuggestionsHandler(s.cfg)
	agentsH := handlers.NewAgentsHandler(s.cfg)
	channelsH := handlers.NewChannelsHandler(buildChannelsManager(s.cfg))

	// P1 fix: 支持多agent
	langgraphH := handlers.NewLangGraphHandlerWithAgents(s.cfg, s.agent, s.agents)

	api := s.router.Group("/api")
	{
		// GET /api/models — list all configured models.
		api.GET("/models", modelsH.ListModels)
		// GET /api/models/:id — get model details.
		api.GET("/models/:id", modelsH.GetModel)
		// POST /api/models/:id/validate — validate model configuration.
		api.POST("/models/:id/validate", modelsH.ValidateModel)

		// Memory routes — all 10 endpoints for full CRUD operations.
		handlers.RegisterMemoryRoutes(api, memoryH)

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

		// Thread-level routes (no thread_id param).
		api.POST("/threads", threadsH.CreateThread)
		api.POST("/threads/search", threadsH.SearchThreads)

		threads := api.Group("/threads/:thread_id")
		{
			// GET /api/threads/:thread_id — get thread metadata.
			threads.GET("", threadsH.GetThread)
			// PATCH /api/threads/:thread_id — update thread metadata.
			threads.PATCH("", threadsH.PatchThread)
			// DELETE /api/threads/:thread_id — delete thread.
			threads.DELETE("", threadsH.DeleteThread)

			// GET /api/threads/:thread_id/state — get thread state.
			threads.GET("/state", threadsH.GetThreadState)
			// POST /api/threads/:thread_id/state — update thread state.
			threads.POST("/state", threadsH.UpdateThreadState)
			// POST /api/threads/:thread_id/history — get checkpoint history.
			threads.POST("/history", threadsH.GetThreadHistory)

			// GET /api/threads/:thread_id/runs — list runs for thread.
			threads.GET("/runs", threadsH.ListRuns)
			// GET /api/threads/:thread_id/runs/:run_id — get run metadata.
			threads.GET("/runs/:run_id", threadsH.GetRun)
			// POST /api/threads/:thread_id/runs — run the lead agent and stream SSE.
			threads.POST("/runs", threadsH.RunThread)
			// POST /api/threads/:thread_id/runs/:run_id/cancel — cancel a running stream.
			threads.POST("/runs/:run_id/cancel", threadsH.CancelRun)

			// POST /api/threads/:thread_id/uploads — receive multipart files.
			threads.POST("/uploads", uploadsH.UploadFiles)
			// GET /api/threads/:thread_id/uploads/list — list uploaded files.
			threads.GET("/uploads/list", uploadsH.ListUploadedFiles)
			// DELETE /api/threads/:thread_id/uploads/:filename — delete uploaded file.
			threads.DELETE("/uploads/:filename", uploadsH.DeleteUploadedFile)

			// GET /api/threads/:thread_id/artifacts/*path — download artifacts.
			threads.GET("/artifacts/*path", artifactsH.GetArtifact)

			// POST /api/threads/:thread_id/suggestions — generate follow-up suggestions.
			threads.POST("/suggestions", suggestionsH.GenerateSuggestions)
		}
	}

	// LangGraph SDK compatible API routes.
	// These endpoints follow the LangGraph Platform API contract.
	lg := api.Group("/langgraph")
	{
		// Assistants (stub for SDK compatibility).
		lg.GET("/assistants", langgraphH.ListAssistants)
		lg.GET("/assistants/:assistant_id", langgraphH.GetAssistant)

		// Threads CRUD.
		lg.POST("/threads", langgraphH.CreateThread)
		lg.GET("/threads/:thread_id", langgraphH.GetThread)
		lg.DELETE("/threads/:thread_id", langgraphH.DeleteThread)
		lg.POST("/threads/search", langgraphH.SearchThreads)

		// Thread state.
		lg.GET("/threads/:thread_id/state", langgraphH.GetThreadState)
		lg.PATCH("/threads/:thread_id/state", langgraphH.UpdateThreadState)

		// Runs (streaming).
		lg.GET("/threads/:thread_id/runs", langgraphH.ListRuns)
		lg.GET("/threads/:thread_id/runs/:run_id", langgraphH.GetRun)
		lg.POST("/threads/:thread_id/runs/stream", langgraphH.StreamRun)
		lg.POST("/threads/:thread_id/runs/:run_id/cancel", langgraphH.CancelRun)

		// Standalone run (creates thread internally).
		lg.POST("/runs/stream", langgraphH.StreamRunStandalone)
	}

	// Health and metrics endpoints.
	metricsH := handlers.NewMetricsHandler()
	healthH := handlers.NewHealthHandler()

	// Prometheus metrics endpoint.
	s.router.GET("/metrics", metricsH.PrometheusHandler())

	// Health check endpoints.
	s.router.GET("/health", healthH.Health)
	s.router.GET("/ready", healthH.Ready)
	s.router.GET("/live", healthH.Live)
}

func buildChannelsManager(cfg *config.AppConfig) *channels.Manager {
	mgr := channels.NewManager(nil, nil)
	if cfg == nil {
		return mgr
	}
	if cfg.Channels != nil {
		if cfg.Channels.Feishu != nil && cfg.Channels.Feishu.Enabled {
			_ = mgr.RegisterChannel(feishuch.New(*cfg.Channels.Feishu))
		}
		if cfg.Channels.Slack != nil && cfg.Channels.Slack.Enabled {
			_ = mgr.RegisterChannel(slackch.New(*cfg.Channels.Slack))
		}
		if cfg.Channels.Telegram != nil && cfg.Channels.Telegram.Enabled {
			_ = mgr.RegisterChannel(telegramch.New(*cfg.Channels.Telegram))
		}
	}
	return mgr
}
