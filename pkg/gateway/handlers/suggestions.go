// Suggestions handler exposes POST /api/threads/:thread_id/suggestions
// for generating conversation continuation suggestions.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/models"
)

// SuggestionsHandler generates follow-up suggestions.
type SuggestionsHandler struct {
	cfg *config.AppConfig
}

// NewSuggestionsHandler creates a SuggestionsHandler.
func NewSuggestionsHandler(cfg *config.AppConfig) *SuggestionsHandler {
	return &SuggestionsHandler{cfg: cfg}
}

type suggestionsRequest struct {
	Messages []map[string]any `json:"messages"`
	Count    int              `json:"count,omitempty"`
}

// GenerateSuggestions produces suggested follow-up prompts.
func (h *SuggestionsHandler) GenerateSuggestions(c *gin.Context) {
	var req suggestionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	count := req.Count
	if count <= 0 {
		count = 3
	}
	if count > 5 {
		count = 5
	}

	// Quick fallback if no model configured.
	if h.cfg == nil || len(h.cfg.Models) == 0 {
		c.JSON(http.StatusOK, gin.H{"suggestions": fallbackSuggestions(count)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	modelReq := models.CreateRequest{}
	if dm := h.cfg.DefaultModel(); dm != nil {
		modelReq.ModelName = dm.Name
	}
	chatModel, err := models.CreateChatModel(ctx, h.cfg, modelReq)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"suggestions": fallbackSuggestions(count)})
		return
	}

	prompt := buildSuggestionPrompt(req.Messages, count)
	resp, err := chatModel.Generate(ctx, prompt)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"suggestions": fallbackSuggestions(count)})
		return
	}

	suggestions := parseSuggestions(resp.Content, count)
	c.JSON(http.StatusOK, gin.H{"suggestions": suggestions})
}

func fallbackSuggestions(count int) []string {
	all := []string{
		"Can you explain more about this?",
		"What are the next steps?",
		"Are there any alternatives?",
		"Can you provide an example?",
		"How does this work under the hood?",
	}
	if count > len(all) {
		count = len(all)
	}
	return all[:count]
}

func buildSuggestionPrompt(_ []map[string]any, count int) []*schema.Message {
	return []*schema.Message{
		schema.SystemMessage("You are a helpful assistant that generates brief follow-up question suggestions based on conversation context."),
		schema.UserMessage("Generate " + string(rune('0'+count)) + " brief follow-up questions the user might want to ask. Return them as a numbered list."),
	}
}

func parseSuggestions(content string, count int) []string {
	// Simple line split for now.
	lines := splitLines(content)
	var suggestions []string
	for _, line := range lines {
		line = trimListPrefix(line)
		if line != "" {
			suggestions = append(suggestions, line)
			if len(suggestions) >= count {
				break
			}
		}
	}
	if len(suggestions) == 0 {
		return fallbackSuggestions(count)
	}
	return suggestions
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimListPrefix(s string) string {
	// Remove leading "1. ", "- ", etc.
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' || c == '.' || c == '-' || c == ' ' || c == ')' {
			continue
		}
		return s[i:]
	}
	return ""
}
