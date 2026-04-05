// Package web implements web search, web fetch, and image search tools for GoClaw.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ImageSearchTool searches for images using DuckDuckGo.
// Implements tools.Tool.
type ImageSearchTool struct {
	cfg WebToolConfig
}

// NewImageSearchTool creates an ImageSearchTool with the given config.
func NewImageSearchTool(cfg WebToolConfig) *ImageSearchTool {
	defaults := defaultWebToolConfig()
	if cfg.Timeout == 0 {
		cfg.Timeout = defaults.Timeout
	}
	if cfg.MaxSearchResults <= 0 {
		cfg.MaxSearchResults = defaults.MaxSearchResults
	}
	return &ImageSearchTool{cfg: cfg}
}

type imageSearchInput struct {
	// Query is the search query string.
	Query string `json:"query"`
}

// ImageSearchResult represents a single image search result.
type ImageSearchResult struct {
	Title       string `json:"title"`
	ImageURL    string `json:"image_url"`
	ThumbnailURL string `json:"thumbnail_url"`
	SourceURL   string `json:"source_url"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

func (t *ImageSearchTool) Name() string { return "image_search" }

func (t *ImageSearchTool) Description() string {
	return `Search for images on the web using DuckDuckGo.
Returns a list of images with URLs and metadata.
Use this tool when you need to find reference images, illustrations, or visual content.`
}

func (t *ImageSearchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "The image search query."}
  }
}`)
}

// Execute performs the image search using DuckDuckGo's Instant Answer API.
func (t *ImageSearchTool) Execute(ctx context.Context, input string) (string, error) {
	var in imageSearchInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("image_search: invalid input JSON: %w", err)
	}

	if strings.TrimSpace(in.Query) == "" {
		return "", fmt.Errorf("image_search: query is required")
	}

	results, err := t.searchDuckDuckGo(ctx, in.Query)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}

	if len(results) == 0 {
		return "No images found.", nil
	}

	// Format results
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d images:\n\n", len(results)))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("   Image: %s\n", r.ImageURL))
		if r.ThumbnailURL != "" {
			sb.WriteString(fmt.Sprintf("   Thumbnail: %s\n", r.ThumbnailURL))
		}
		if r.SourceURL != "" {
			sb.WriteString(fmt.Sprintf("   Source: %s\n", r.SourceURL))
		}
		if r.Width > 0 && r.Height > 0 {
			sb.WriteString(fmt.Sprintf("   Size: %dx%d\n", r.Width, r.Height))
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// searchDuckDuckGo searches for images using DuckDuckGo.
func (t *ImageSearchTool) searchDuckDuckGo(ctx context.Context, query string) ([]ImageSearchResult, error) {
	client := newHTTPClient(t.cfg.Timeout)

	// DuckDuckGo Instant Answer API endpoint for images
	apiURL := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&iax=images&ia=images",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Parse DuckDuckGo response
	var ddgResp struct {
		RelatedTopics []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
			Icon     struct {
				URL    string `json:"URL"`
				Width  int    `json:"Width"`
				Height int    `json:"Height"`
			} `json:"Icon"`
		} `json:"RelatedTopics"`
		ImageResults []struct {
			Text     string `json:"text"`
			Image    string `json:"image"`
			Thumbnail string `json:"thumbnail"`
			URL      string `json:"url"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
		} `json:"ImageResults"`
	}

	if err := json.Unmarshal(body, &ddgResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	results := make([]ImageSearchResult, 0)

	// Add ImageResults if available
	for _, ir := range ddgResp.ImageResults {
		results = append(results, ImageSearchResult{
			Title:        ir.Text,
			ImageURL:     ir.Image,
			ThumbnailURL: ir.Thumbnail,
			SourceURL:    ir.URL,
			Width:        ir.Width,
			Height:       ir.Height,
		})
	}

	// Fall back to RelatedTopics with images
	if len(results) == 0 {
		for _, rt := range ddgResp.RelatedTopics {
			if rt.Icon.URL != "" {
				results = append(results, ImageSearchResult{
					Title:        rt.Text,
					ImageURL:     rt.Icon.URL,
					ThumbnailURL: rt.Icon.URL,
					SourceURL:    rt.FirstURL,
					Width:        rt.Icon.Width,
					Height:       rt.Icon.Height,
				})
			}
		}
	}

	// Limit results
	if len(results) > t.cfg.MaxSearchResults {
		results = results[:t.cfg.MaxSearchResults]
	}

	return results, nil
}
