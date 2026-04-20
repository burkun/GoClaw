// Suggestions handler exposes POST /api/threads/:thread_id/suggestions
// for generating conversation continuation suggestions.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"

	"goclaw/internal/config"
	"goclaw/internal/models"
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
	Messages  []map[string]any `json:"messages"`
	Count     int              `json:"count,omitempty"`
	N         int              `json:"n,omitempty"`
	ModelName string           `json:"model_name,omitempty"`
}

// GenerateSuggestions produces suggested follow-up prompts.
func (h *SuggestionsHandler) GenerateSuggestions(c *gin.Context) {
	var req suggestionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	count := req.Count
	if req.N > 0 {
		count = req.N
	}
	if count <= 0 {
		count = 3
	}
	if count > 5 {
		count = 5
	}

	if len(req.Messages) == 0 {
		c.JSON(http.StatusOK, gin.H{"suggestions": fallbackSuggestions(count)})
		return
	}

	// Quick fallback if no model configured.
	if h.cfg == nil || len(h.cfg.Models) == 0 {
		c.JSON(http.StatusOK, gin.H{"suggestions": fallbackSuggestions(count)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	modelReq := models.CreateRequest{}
	if strings.TrimSpace(req.ModelName) != "" {
		modelReq.ModelName = strings.TrimSpace(req.ModelName)
	} else if dm := h.cfg.DefaultModel(); dm != nil {
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

func buildSuggestionPrompt(messages []map[string]any, count int) []*schema.Message {
	var history strings.Builder
	for i := len(messages) - 1; i >= 0; i-- {
		if i < len(messages)-8 { // keep recent 8
			break
		}
		role, _ := messages[i]["role"].(string)
		content, _ := messages[i]["content"].(string)
		if strings.TrimSpace(content) == "" {
			continue
		}
		history.WriteString(role)
		history.WriteString(": ")
		history.WriteString(content)
		history.WriteString("\n")
	}
	if history.Len() == 0 {
		history.WriteString("(no conversation context)")
	}

	return []*schema.Message{
		schema.SystemMessage("You are a helpful assistant that generates brief follow-up question suggestions based on conversation context. Return JSON array of strings only."),
		schema.UserMessage(fmt.Sprintf("Generate %d brief follow-up questions. Use the same language as the user.\nConversation:\n%s", count, history.String())),
	}
}

func parseSuggestions(content string, count int) []string {
	if jsonList := parseJSONStringList(content); len(jsonList) > 0 {
		if len(jsonList) > count {
			jsonList = jsonList[:count]
		}
		return jsonList
	}

	// Fallback: line split.
	lines := splitLines(content)
	var suggestions []string
	for _, line := range lines {
		line = strings.TrimSpace(trimListPrefix(line))
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

func parseJSONStringList(content string) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}

	// Strip fenced blocks.
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}

	parse := func(s string) []string {
		var arr []string
		if err := json.Unmarshal([]byte(s), &arr); err == nil {
			return compactNonEmpty(arr)
		}
		var anyArr []any
		if err := json.Unmarshal([]byte(s), &anyArr); err == nil {
			out := make([]string, 0, len(anyArr))
			for _, v := range anyArr {
				if str, ok := v.(string); ok {
					out = append(out, strings.TrimSpace(str))
				}
			}
			return compactNonEmpty(out)
		}
		return nil
	}

	if out := parse(trimmed); len(out) > 0 {
		return out
	}

	// Try extracting first JSON array segment.
	start := strings.Index(trimmed, "[")
	end := strings.LastIndex(trimmed, "]")
	if start >= 0 && end > start {
		if out := parse(trimmed[start : end+1]); len(out) > 0 {
			return out
		}
	}
	return nil
}

func compactNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start <= len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimListPrefix(s string) string {
	s = strings.TrimSpace(s)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' || c == ' ' || c == ')' {
			continue
		}
		return s[i:]
	}
	return ""
}
