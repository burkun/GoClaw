// Package web implements web search and web fetch tools for GoClaw.
//
// Two tools are provided:
//
//	WebSearchTool – searches the web via the Tavily Search API and returns a
//	                list of results with title, URL, and snippet.
//	WebFetchTool  – fetches and extracts the readable text from a web page via
//	                the Jina Reader API (r.jina.ai) or Tavily Extract API.
//
// Both tools share a WebToolConfig for API key and timeout configuration.
// The config is loaded from the application configuration system; API keys
// are never hard-coded.
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
	// UseTavilyExtract uses Tavily Extract API instead of Jina Reader for web_fetch.
	UseTavilyExtract bool
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
// Falls back to DuckDuckGo if Tavily API key is not configured.
func (t *WebSearchTool) Execute(ctx context.Context, input string) (string, error) {
	var in webSearchInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("web_search: invalid input JSON: %w", err)
	}

	query := strings.TrimSpace(in.Query)
	if query == "" {
		return "", fmt.Errorf("web_search: query is required")
	}
	if t.cfg.TavilyAPIKey == "" {
		// Fallback to DuckDuckGo when Tavily API key is not configured
		return t.executeDuckDuckGo(ctx, query)
	}

	payload := map[string]any{
		"api_key":     t.cfg.TavilyAPIKey,
		"query":       query,
		"max_results": clampInt(t.cfg.MaxSearchResults, 1, 20),
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tavily.com/search", bytes.NewReader(body))
	if err != nil {
		return fmt.Sprintf("Error: cannot build request: %v", err), nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := newHTTPClient(t.cfg.Timeout).Do(req)
	if err != nil {
		return fmt.Sprintf("Error: tavily request failed: %v", err), nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("Error: failed to read tavily response: %v", err), nil
	}
	if resp.StatusCode >= 300 {
		return fmt.Sprintf("Error: tavily returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))), nil
	}

	var raw struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
			Snippet string `json:"snippet"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return fmt.Sprintf("Error: invalid tavily response: %v", err), nil
	}

	out := make([]SearchResult, 0, len(raw.Results))
	for _, r := range raw.Results {
		snippet := strings.TrimSpace(r.Snippet)
		if snippet == "" {
			snippet = strings.TrimSpace(r.Content)
		}
		out = append(out, SearchResult{
			Title:   strings.TrimSpace(r.Title),
			URL:     strings.TrimSpace(r.URL),
			Snippet: snippet,
		})
	}

	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error: cannot encode search results: %v", err), nil
	}
	return string(encoded), nil
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

// Execute fetches the page via Jina Reader or Tavily Extract API and returns extracted text.
func (t *WebFetchTool) Execute(ctx context.Context, input string) (string, error) {
	var in webFetchInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("web_fetch: invalid input JSON: %w", err)
	}

	rawURL := strings.TrimSpace(in.URL)
	if rawURL == "" {
		return "", fmt.Errorf("web_fetch: url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "Error: invalid URL. Must include http:// or https:// scheme.", nil
	}

	// Prefer Tavily Extract if configured and API key is available
	if t.cfg.UseTavilyExtract && t.cfg.TavilyAPIKey != "" {
		return t.executeTavilyExtract(ctx, rawURL)
	}

	// Fallback to Jina Reader
	return t.executeJinaReader(ctx, rawURL)
}

// executeTavilyExtract fetches content using Tavily Extract API.
func (t *WebFetchTool) executeTavilyExtract(ctx context.Context, rawURL string) (string, error) {
	payload := map[string]any{
		"api_key": t.cfg.TavilyAPIKey,
		"urls":    []string{rawURL},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tavily.com/extract", bytes.NewReader(body))
	if err != nil {
		return fmt.Sprintf("Error: cannot build request: %v", err), nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := newHTTPClient(t.cfg.Timeout).Do(req)
	if err != nil {
		return fmt.Sprintf("Error: tavily extract request failed: %v", err), nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("Error: failed to read tavily response: %v", err), nil
	}
	if resp.StatusCode >= 300 {
		return fmt.Sprintf("Error: tavily extract returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))), nil
	}

	var raw struct {
		Results []struct {
			URL        string `json:"url"`
			RawContent string `json:"raw_content"`
		} `json:"results"`
		FailedResults []struct {
			URL   string `json:"url"`
			Error string `json:"error"`
		} `json:"failed_results"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return fmt.Sprintf("Error: invalid tavily extract response: %v", err), nil
	}

	// Check for failed results
	if len(raw.FailedResults) > 0 {
		return fmt.Sprintf("Error: %s", raw.FailedResults[0].Error), nil
	}

	// Return extracted content
	if len(raw.Results) == 0 {
		return "Error: no content extracted", nil
	}

	content := raw.Results[0].RawContent
	maxChars := t.cfg.MaxFetchChars
	if maxChars <= 0 {
		maxChars = defaultWebToolConfig().MaxFetchChars
	}
	if len(content) > maxChars {
		content = content[:maxChars] + "\n... (truncated)"
	}
	return content, nil
}

// executeJinaReader fetches content using Jina Reader API.
func (t *WebFetchTool) executeJinaReader(ctx context.Context, rawURL string) (string, error) {
	fetchURL := "https://r.jina.ai/" + rawURL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return fmt.Sprintf("Error: cannot build request: %v", err), nil
	}
	req.Header.Set("Accept", "text/markdown")
	if strings.TrimSpace(t.cfg.JinaAPIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(t.cfg.JinaAPIKey))
	}

	resp, err := newHTTPClient(t.cfg.Timeout).Do(req)
	if err != nil {
		return fmt.Sprintf("Error: fetch request failed: %v", err), nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("Error: failed to read response: %v", err), nil
	}
	if resp.StatusCode >= 300 {
		return fmt.Sprintf("Error: fetch returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))), nil
	}

	content := string(respBody)
	maxChars := t.cfg.MaxFetchChars
	if maxChars <= 0 {
		maxChars = defaultWebToolConfig().MaxFetchChars
	}
	if len(content) > maxChars {
		content = content[:maxChars] + "\n... (truncated)"
	}
	return content, nil
}

// ---------------------------------------------------------------------------
// DuckDuckGo Search – Fallback when Tavily is not configured
// ---------------------------------------------------------------------------

// executeDuckDuckGo searches using DuckDuckGo HTML search.
// This is a fallback when Tavily API key is not available.
func (t *WebSearchTool) executeDuckDuckGo(ctx context.Context, query string) (string, error) {
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return fmt.Sprintf("Error: cannot build request: %v", err), nil
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GoClaw/1.0)")

	resp, err := newHTTPClient(t.cfg.Timeout).Do(req)
	if err != nil {
		return fmt.Sprintf("Error: DuckDuckGo request failed: %v", err), nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("Error: failed to read DuckDuckGo response: %v", err), nil
	}
	if resp.StatusCode >= 300 {
		return fmt.Sprintf("Error: DuckDuckGo returned status %d", resp.StatusCode), nil
	}

	results := t.parseDuckDuckGoHTML(string(respBody))
	if len(results) == 0 {
		return "No search results found.", nil
	}

	maxResults := clampInt(t.cfg.MaxSearchResults, 1, 20)
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	encoded, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error: cannot encode search results: %v", err), nil
	}
	return string(encoded), nil
}

// parseDuckDuckGoHTML extracts search results from DuckDuckGo HTML response.
func (t *WebSearchTool) parseDuckDuckGoHTML(html string) []SearchResult {
	var results []SearchResult
	resultPattern := `<a class="result__a" href="`

	for len(results) < 20 {
		idx := strings.Index(html, resultPattern)
		if idx == -1 {
			break
		}
		html = html[idx+len(resultPattern):]

		endURL := strings.Index(html, `"`)
		if endURL == -1 {
			continue
		}
		resultURL := html[:endURL]

		// DuckDuckGo redirect URL - extract actual URL
		if strings.Contains(resultURL, "uddg=") {
			if u, err := url.QueryUnescape(resultURL[strings.Index(resultURL, "uddg=")+5:]); err == nil {
				resultURL = u
			}
		}

		titleStart := strings.Index(html, `>`)
		if titleStart == -1 {
			continue
		}
		html = html[titleStart+1:]
		titleEnd := strings.Index(html, `</a>`)
		if titleEnd == -1 {
			continue
		}
		title := strings.TrimSpace(html[:titleEnd])
		html = html[titleEnd+4:]

		// Look for snippet
		snippet := ""
		snippetIdx := strings.Index(html, `<a class="result__snippet"`)
		if snippetIdx != -1 && snippetIdx < 300 {
			snippetHTML := html[snippetIdx:]
			snippetStart := strings.Index(snippetHTML, `>`)
			if snippetStart != -1 {
				snippetHTML = snippetHTML[snippetStart+1:]
				snippetEnd := strings.Index(snippetHTML, `</a>`)
				if snippetEnd != -1 {
					snippet = strings.TrimSpace(snippetHTML[:snippetEnd])
					snippet = strings.ReplaceAll(snippet, "<b>", "")
					snippet = strings.ReplaceAll(snippet, "</b>", "")
				}
			}
		}

		if title != "" && resultURL != "" {
			results = append(results, SearchResult{
				Title:   title,
				URL:     resultURL,
				Snippet: snippet,
			})
		}
	}
	return results
}

func clampInt(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}
