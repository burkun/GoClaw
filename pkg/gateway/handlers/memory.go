package handlers

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
)

// MemoryHandler serves GET /api/memory.
type MemoryHandler struct {
	cfg *config.AppConfig
}

// NewMemoryHandler creates a MemoryHandler.
func NewMemoryHandler(cfg *config.AppConfig) *MemoryHandler {
	return &MemoryHandler{cfg: cfg}
}

// ---------------------------------------------------------------------------
// Response types (mirrors DeerFlow's memory schema)
// ---------------------------------------------------------------------------

// ContextSection holds a text summary and an update timestamp for one context
// dimension (e.g. work context, personal context, top-of-mind).
type ContextSection struct {
	Summary   string `json:"summary"`
	UpdatedAt string `json:"updatedAt"`
}

// UserContext groups the three user-facing context dimensions.
type UserContext struct {
	WorkContext     ContextSection `json:"workContext"`
	PersonalContext ContextSection `json:"personalContext"`
	TopOfMind       ContextSection `json:"topOfMind"`
}

// HistoryContext groups the three temporal history dimensions.
type HistoryContext struct {
	RecentMonths       ContextSection `json:"recentMonths"`
	EarlierContext     ContextSection `json:"earlierContext"`
	LongTermBackground ContextSection `json:"longTermBackground"`
}

// MemoryFact is a single extracted fact stored in memory.
type MemoryFact struct {
	// ID is a unique identifier for this fact (e.g. "fact_abc123").
	ID string `json:"id"`
	// Content is the prose text of the fact.
	Content string `json:"content"`
	// Category classifies the fact (e.g. "preference", "context").
	Category string `json:"category"`
	// Confidence is a [0, 1] score indicating how certain the LLM was.
	Confidence float64 `json:"confidence"`
	// CreatedAt is the ISO-8601 timestamp of fact creation.
	CreatedAt string `json:"createdAt"`
	// Source is the thread_id that produced this fact.
	Source string `json:"source"`
	// SourceError optionally describes a prior mistake the fact corrects.
	SourceError *string `json:"sourceError,omitempty"`
}

// MemoryResponse is the wire format for GET /api/memory.
// It mirrors DeerFlow's MemoryResponse schema for frontend compatibility.
type MemoryResponse struct {
	// Version is the schema version of the memory file (currently "1.0").
	Version     string         `json:"version"`
	LastUpdated string         `json:"lastUpdated"`
	User        UserContext    `json:"user"`
	History     HistoryContext `json:"history"`
	Facts       []MemoryFact   `json:"facts"`
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

const defaultMemoryPath = ".goclaw/memory.json"

// GetMemory handles GET /api/memory.
//
// It reads the persisted memory.json file from disk and returns its contents.
// If the file does not exist an empty-but-valid MemoryResponse is returned.
func (h *MemoryHandler) GetMemory(c *gin.Context) {
	memoryPath := defaultMemoryPath
	if h.cfg != nil && h.cfg.Memory.StoragePath != "" {
		memoryPath = h.cfg.Memory.StoragePath
	}

	// TODO: Replace the block below with a thread-safe in-memory cache that is
	//   populated at startup and refreshed on file-change events (fsnotify) to
	//   avoid repeated disk reads on hot paths.

	data, err := os.ReadFile(memoryPath) //nolint:gosec // path is config-controlled
	if err != nil {
		if os.IsNotExist(err) {
			// Return an empty but structurally valid response; no error to the client.
			c.JSON(http.StatusOK, emptyMemoryResponse())
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read memory file"})
		return
	}

	var resp MemoryResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse memory file"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// emptyMemoryResponse returns a zero-value MemoryResponse with an empty Facts
// slice (not nil) so the JSON output always has "facts": [].
func emptyMemoryResponse() MemoryResponse {
	return MemoryResponse{
		Version: "1.0",
		Facts:   []MemoryFact{},
	}
}
