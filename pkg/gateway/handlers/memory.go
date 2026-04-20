package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"goclaw/internal/config"
	memorymw "goclaw/internal/middleware/memory"
)

// MemoryHandler serves memory API endpoints.
// Mirrors DeerFlow's memory router with full CRUD operations.
type MemoryHandler struct {
	cfg *config.AppConfig

	mu          sync.RWMutex
	cachedPath  string
	cachedMod   time.Time
	cachedValue MemoryResponse
	cacheReady  bool
}

// NewMemoryHandler creates a MemoryHandler.
func NewMemoryHandler(cfg *config.AppConfig) *MemoryHandler {
	return &MemoryHandler{cfg: cfg}
}

// ---------------------------------------------------------------------------
// Response types (mirrors DeerFlow's memory schema)
// ---------------------------------------------------------------------------

// ContextSection holds one summary section with update timestamp.
type ContextSection struct {
	Summary   string `json:"summary"`
	UpdatedAt string `json:"updatedAt"`
}

// UserContext groups user-related context sections.
type UserContext struct {
	WorkContext     ContextSection `json:"workContext"`
	PersonalContext ContextSection `json:"personalContext"`
	TopOfMind       ContextSection `json:"topOfMind"`
}

// HistoryContext groups history-related context sections.
type HistoryContext struct {
	RecentMonths       ContextSection `json:"recentMonths"`
	EarlierContext     ContextSection `json:"earlierContext"`
	LongTermBackground ContextSection `json:"longTermBackground"`
}

// MemoryFact is the persisted structured fact schema.
type MemoryFact struct {
	ID          string  `json:"id"`
	Content     string  `json:"content"`
	Category    string  `json:"category"`
	Confidence  float64 `json:"confidence"`
	CreatedAt   string  `json:"createdAt"`
	Source      string  `json:"source"`
	SourceError *string `json:"sourceError,omitempty"`
}

// MemoryResponse is the persisted memory document.
type MemoryResponse struct {
	Version     string         `json:"version"`
	LastUpdated string         `json:"lastUpdated"`
	User        UserContext    `json:"user"`
	History     HistoryContext `json:"history"`
	Facts       []MemoryFact   `json:"facts"`
}

// MemoryConfigResponse is the memory configuration response.
type MemoryConfigResponse struct {
	Enabled                 bool    `json:"enabled"`
	StoragePath             string  `json:"storage_path"`
	DebounceSeconds         int     `json:"debounce_seconds"`
	MaxFacts                int     `json:"max_facts"`
	FactConfidenceThreshold float64 `json:"fact_confidence_threshold"`
	InjectionEnabled        bool    `json:"injection_enabled"`
	MaxInjectionTokens      int     `json:"max_injection_tokens"`
}

// MemoryStatusResponse combines config and data.
type MemoryStatusResponse struct {
	Config MemoryConfigResponse `json:"config"`
	Data   MemoryResponse       `json:"data"`
}

// FactCreateRequest is the request to create a fact.
type FactCreateRequest struct {
	Content    string  `json:"content" binding:"required"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
}

// FactPatchRequest is the request to patch a fact.
type FactPatchRequest struct {
	Content    *string  `json:"content"`
	Category   *string  `json:"category"`
	Confidence *float64 `json:"confidence"`
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func (h *MemoryHandler) getMemoryPath() string {
	if h.cfg != nil && h.cfg.Memory.StoragePath != "" {
		return h.cfg.Memory.StoragePath
	}
	return memorymw.DefaultMemoryPath
}

func parseMemoryResponse(data []byte) (MemoryResponse, error) {
	var resp MemoryResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return MemoryResponse{}, err
	}
	if resp.Version == "" {
		resp.Version = "1.0"
	}
	if resp.Facts == nil {
		resp.Facts = []MemoryFact{}
	}
	return resp, nil
}

func emptyMemoryResponse() MemoryResponse {
	return MemoryResponse{
		Version: "1.0",
		Facts:   []MemoryFact{},
	}
}

func (h *MemoryHandler) loadMemory() (MemoryResponse, error) {
	memoryPath := h.getMemoryPath()

	info, err := os.Stat(memoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyMemoryResponse(), nil
		}
		return MemoryResponse{}, err
	}

	h.mu.RLock()
	if h.cacheReady && h.cachedPath == memoryPath && info.ModTime().Equal(h.cachedMod) {
		resp := h.cachedValue
		h.mu.RUnlock()
		return resp, nil
	}
	h.mu.RUnlock()

	data, err := os.ReadFile(memoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyMemoryResponse(), nil
		}
		return MemoryResponse{}, err
	}

	resp, err := parseMemoryResponse(data)
	if err != nil {
		return MemoryResponse{}, err
	}

	h.mu.Lock()
	h.cachedPath = memoryPath
	h.cachedMod = info.ModTime()
	h.cachedValue = resp
	h.cacheReady = true
	h.mu.Unlock()

	return resp, nil
}

func (h *MemoryHandler) saveMemory(resp MemoryResponse) error {
	memoryPath := h.getMemoryPath()

	resp.LastUpdated = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(memoryPath), 0755); err != nil {
		return err
	}

	// Write atomically
	tmpPath := memoryPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, memoryPath); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Update cache
	h.mu.Lock()
	h.cachedPath = memoryPath
	h.cachedMod = time.Now()
	h.cachedValue = resp
	h.cacheReady = true
	h.mu.Unlock()

	return nil
}

func (h *MemoryHandler) invalidateCache() {
	h.mu.Lock()
	h.cacheReady = false
	h.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Handler methods
// ---------------------------------------------------------------------------

// GetMemory handles GET /api/memory.
func (h *MemoryHandler) GetMemory(c *gin.Context) {
	resp, err := h.loadMemory()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read memory file"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ReloadMemory handles POST /api/memory/reload.
func (h *MemoryHandler) ReloadMemory(c *gin.Context) {
	// Invalidate cache and reload
	h.invalidateCache()
	resp, err := h.loadMemory()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reload memory file"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ClearMemory handles DELETE /api/memory.
func (h *MemoryHandler) ClearMemory(c *gin.Context) {
	resp := emptyMemoryResponse()
	resp.LastUpdated = time.Now().UTC().Format(time.RFC3339)

	if err := h.saveMemory(resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear memory data"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// CreateFact handles POST /api/memory/facts.
func (h *MemoryHandler) CreateFact(c *gin.Context) {
	var req FactCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate confidence
	if req.Confidence < 0 || req.Confidence > 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid confidence value; must be between 0 and 1."})
		return
	}

	// Validate content
	if req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Memory fact content cannot be empty."})
		return
	}

	resp, err := h.loadMemory()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read memory file"})
		return
	}

	// Set defaults
	category := req.Category
	if category == "" {
		category = "context"
	}
	confidence := req.Confidence
	if confidence == 0 {
		confidence = 0.5
	}

	fact := MemoryFact{
		ID:         uuid.New().String(),
		Content:    req.Content,
		Category:   category,
		Confidence: confidence,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Source:     "manual",
	}

	resp.Facts = append(resp.Facts, fact)

	if err := h.saveMemory(resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create memory fact"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// DeleteFact handles DELETE /api/memory/facts/:fact_id.
func (h *MemoryHandler) DeleteFact(c *gin.Context) {
	factID := c.Param("fact_id")

	resp, err := h.loadMemory()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read memory file"})
		return
	}

	// Find and remove fact
	found := false
	newFacts := make([]MemoryFact, 0, len(resp.Facts))
	for _, fact := range resp.Facts {
		if fact.ID == factID {
			found = true
			continue
		}
		newFacts = append(newFacts, fact)
	}

	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Memory fact '%s' not found.", factID)})
		return
	}

	resp.Facts = newFacts

	if err := h.saveMemory(resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete memory fact"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// PatchFact handles PATCH /api/memory/facts/:fact_id.
func (h *MemoryHandler) PatchFact(c *gin.Context) {
	factID := c.Param("fact_id")

	var req FactPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate confidence if provided
	if req.Confidence != nil && (*req.Confidence < 0 || *req.Confidence > 1) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid confidence value; must be between 0 and 1."})
		return
	}

	// Validate content if provided
	if req.Content != nil && *req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Memory fact content cannot be empty."})
		return
	}

	resp, err := h.loadMemory()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read memory file"})
		return
	}

	// Find and update fact
	found := false
	for i, fact := range resp.Facts {
		if fact.ID == factID {
			found = true
			if req.Content != nil {
				resp.Facts[i].Content = *req.Content
			}
			if req.Category != nil {
				resp.Facts[i].Category = *req.Category
			}
			if req.Confidence != nil {
				resp.Facts[i].Confidence = *req.Confidence
			}
			break
		}
	}

	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Memory fact '%s' not found.", factID)})
		return
	}

	if err := h.saveMemory(resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update memory fact"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ExportMemory handles GET /api/memory/export.
func (h *MemoryHandler) ExportMemory(c *gin.Context) {
	resp, err := h.loadMemory()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read memory file"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ImportMemory handles POST /api/memory/import.
func (h *MemoryHandler) ImportMemory(c *gin.Context) {
	var req MemoryResponse
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Ensure version
	if req.Version == "" {
		req.Version = "1.0"
	}
	if req.Facts == nil {
		req.Facts = []MemoryFact{}
	}

	if err := h.saveMemory(req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to import memory data"})
		return
	}

	c.JSON(http.StatusOK, req)
}

// GetMemoryConfig handles GET /api/memory/config.
func (h *MemoryHandler) GetMemoryConfig(c *gin.Context) {
	cfg := h.getMemoryConfig()
	c.JSON(http.StatusOK, cfg)
}

func (h *MemoryHandler) getMemoryConfig() MemoryConfigResponse {
	if h.cfg == nil {
		return MemoryConfigResponse{
			Enabled:                 false,
			StoragePath:             memorymw.DefaultMemoryPath,
			DebounceSeconds:         30,
			MaxFacts:                100,
			FactConfidenceThreshold: 0.7,
			InjectionEnabled:        false,
			MaxInjectionTokens:      2000,
		}
	}

	return MemoryConfigResponse{
		Enabled:                 h.cfg.Memory.Enabled,
		StoragePath:             h.cfg.Memory.StoragePath,
		DebounceSeconds:         h.cfg.Memory.DebounceSeconds,
		MaxFacts:                h.cfg.Memory.MaxFacts,
		FactConfidenceThreshold: h.cfg.Memory.FactConfidenceThreshold,
		InjectionEnabled:        h.cfg.Memory.InjectionEnabled,
		MaxInjectionTokens:      h.cfg.Memory.MaxInjectionTokens,
	}
}

// GetMemoryStatus handles GET /api/memory/status.
func (h *MemoryHandler) GetMemoryStatus(c *gin.Context) {
	cfg := h.getMemoryConfig()
	data, err := h.loadMemory()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read memory file"})
		return
	}

	resp := MemoryStatusResponse{
		Config: cfg,
		Data:   data,
	}
	c.JSON(http.StatusOK, resp)
}

// RegisterMemoryRoutes registers all memory routes.
func RegisterMemoryRoutes(router *gin.RouterGroup, handler *MemoryHandler) {
	// GET /api/memory — get memory data
	router.GET("/memory", handler.GetMemory)
	// POST /api/memory/reload — reload memory from file
	router.POST("/memory/reload", handler.ReloadMemory)
	// DELETE /api/memory — clear all memory
	router.DELETE("/memory", handler.ClearMemory)

	// POST /api/memory/facts — create fact
	router.POST("/memory/facts", handler.CreateFact)
	// DELETE /api/memory/facts/:fact_id — delete fact
	router.DELETE("/memory/facts/:fact_id", handler.DeleteFact)
	// PATCH /api/memory/facts/:fact_id — patch fact
	router.PATCH("/memory/facts/:fact_id", handler.PatchFact)

	// GET /api/memory/export — export memory
	router.GET("/memory/export", handler.ExportMemory)
	// POST /api/memory/import — import memory
	router.POST("/memory/import", handler.ImportMemory)

	// GET /api/memory/config — get memory config
	router.GET("/memory/config", handler.GetMemoryConfig)
	// GET /api/memory/status — get memory status (config + data)
	router.GET("/memory/status", handler.GetMemoryStatus)
}
