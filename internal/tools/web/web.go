// Package web implements web search and web fetch tools for GoClaw.
//
// Two tools are provided:
//
//	WebSearchTool – searches the web via the Tavily Search API and returns a
//	                list of results with title, URL, and snippet.
//	WebFetchTool  – fetches and extracts the readable text from a web page via
//	                the Jina Reader API (r.jina.ai).
//
// Both tools share a WebToolConfig for API key and timeout configuration.
// The config is loaded from the application configuration system; API keys
// are never hard-coded.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WebToolConfig holds configuration shared by WebSearchTool and WebFetchTool.
type WebToolConfig struct {
	// TavilyAPIKey is the API key for Tavily Search (env: TAVILY_API_KEY).
	TavilyAPIKey string
	// JinaAPIKey is the API key for Jina Reader (optional; env: JINA_API_KEY).
	// When empty, Jina Reader is used without authentication (rate-limited).
	JinaAPIKey string
	// Timeout is the HTTP client timeout per request. Default: 10 s.
	Timeout time.Duration
	// MaxSearchResults caps the number of search results returned. Default: 5.
	MaxSearchResults int
	// MaxFetchChars caps the number of characters returned by WebFetchTool. Default: 4096.
	MaxFetchChars int
}

// defaultWebToolConfig returns sensible defaults for WebToolConfig.
func defaultWebToolConfig() WebToolConfig {
	return WebToolConfig{
		Timeout:          10 * time.Second,
		MaxSearchResults: 5,
		MaxFetchChars:    4096,
	}
}

// newHTTPClient creates an *http.Client with the configured timeout.
func newHTTPClient(timeout time.Duration) *http.Client {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

// ---------------------------------------------------------------------------
// WebSearchTool – Tavily Search API
// ---------------------------------------------------------------------------

// SearchResult represents a single web search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// WebSearchTool searches the web using the Tavily Search API.
// Implements tools.Tool.
type WebSearchTool struct {
	cfg WebToolConfig
}

// NewWebSearchTool creates a WebSearchTool with the given config.
// Zero-value fields in cfg are filled with defaults.
func NewWebSearchTool(cfg WebToolConfig) *WebSearchTool {
	defaults := defaultWebToolConfig()
	if cfg.Timeout == 0 {
		cfg.Timeout = defaults.Timeout
	}
	if cfg.MaxSearchResults <= 0 {
		cfg.MaxSearchResults = defaults.MaxSearchResults
	}
	if cfg.MaxFetchChars <= 0 {
		cfg.MaxFetchChars = defaults.MaxFetchChars
	}
	return &WebSearchTool{cfg: cfg}
}

type webSearchInput struct {
	// Query is the search query string.
	Query string `json:"query"`
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
	return `Search the web and return a list of results with title, URL, and snippet.
Use this tool to find current information, facts, or documentation online.`
}

func (t *WebSearchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "The search query."}
  }
}`)
}

// Execute calls the Tavily Search API and returns JSON-formatted results.
//
// TODO: implementation steps
//  1. json.Unmarshal input into webSearchInput.
//  2. Validate that t.cfg.TavilyAPIKey is set; return an error if not.
//  3. Build a POST request to https://api.tavily.com/search with body:
//     {"api_key": key, "query": in.Query, "max_results": cfg.MaxSearchResults}.
//  4. Use newHTTPClient(t.cfg.Timeout).Do(req).
//  5. Read and json.Unmarshal the response body.
//  6. Normalize results into []SearchResult{Title, URL, Snippet}.
//  7. json.MarshalIndent and return the JSON string.
//  8. On HTTP or JSON error, return "Error: <message>".
func (t *WebSearchTool) Execute(ctx context.Context, input string) (string, error) {
	var in webSearchInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("web_search: invalid input JSON: %w", err)
	}

	if t.cfg.TavilyAPIKey == "" {
		return "Error: TAVILY_API_KEY is not configured.", nil
	}

	// TODO: implement – see doc comment above.
	_ = ctx
	return "", fmt.Errorf("web_search: not implemented")
}

// ---------------------------------------------------------------------------
// WebFetchTool – Jina Reader API
// ---------------------------------------------------------------------------

// WebFetchTool fetches and extracts readable text from a web page using the
// Jina Reader API (https://r.jina.ai/<url>).
// Implements tools.Tool.
type WebFetchTool struct {
	cfg WebToolConfig
}

// NewWebFetchTool creates a WebFetchTool with the given config.
func NewWebFetchTool(cfg WebToolConfig) *WebFetchTool {
	defaults := defaultWebToolConfig()
	if cfg.Timeout == 0 {
		cfg.Timeout = defaults.Timeout
	}
	if cfg.MaxFetchChars <= 0 {
		cfg.MaxFetchChars = defaults.MaxFetchChars
	}
	return &WebFetchTool{cfg: cfg}
}

type webFetchInput struct {
	// URL is the page to fetch. Must include the scheme (https://...).
	URL string `json:"url"`
}

func (t *WebFetchTool) Name() string { return "web_fetch" }

func (t *WebFetchTool) Description() string {
	return `Fetch the readable text content of a web page at a given URL via Jina Reader.
Only fetch EXACT URLs provided by the user or returned by web_search.
URLs must include the schema (https://example.com). Do NOT add www. to URLs that lack it.
This tool cannot access pages that require authentication.`
}

func (t *WebFetchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["url"],
  "properties": {
    "url": {"type": "string", "description": "The URL to fetch (must include https:// or http://)."}
  }
}`)
}

// Execute fetches the page via the Jina Reader API and returns extracted text.
//
// TODO: implementation steps
//  1. json.Unmarshal input into webFetchInput.
//  2. Validate that in.URL starts with "http://" or "https://".
//  3. Build a GET request to "https://r.jina.ai/" + url.
//  4. Set Accept: text/markdown header to get Markdown output from Jina.
//  5. If t.cfg.JinaAPIKey != "", set Authorization: Bearer <key> header.
//  6. Use newHTTPClient(t.cfg.Timeout).Do(req).
//  7. Read response body; truncate to t.cfg.MaxFetchChars characters.
//  8. Return the Markdown string.
//  9. On HTTP or I/O error, return "Error: <message>".
func (t *WebFetchTool) Execute(ctx context.Context, input string) (string, error) {
	var in webFetchInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("web_fetch: invalid input JSON: %w", err)
	}

	// TODO: implement – see doc comment above.
	_ = ctx
	return "", fmt.Errorf("web_fetch: not implemented")
}

// Silence unused import warning during skeleton phase.
var _ = http.MethodGet
