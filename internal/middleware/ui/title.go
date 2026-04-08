// Package ui implements UI-related middleware for GoClaw.
//
// This package contains middlewares that handle user interface concerns,
// including title generation and image viewing.
package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// MaxTitleChars is the maximum byte length of a generated title.
// Matches DeerFlow's TitleConfig.max_chars default (P2 alignment).
const MaxTitleChars = 60

// MaxFallbackChars is the maximum length of the fallback title derived from
// the first user message when LLM generation fails.
const MaxFallbackChars = 50

// TitleGenerator is the narrow interface used by TitleMiddleware to invoke an
// LLM. Callers inject a concrete implementation (e.g. an Eino ChatModel
// wrapper) so TitleMiddleware has no direct dependency on any model SDK.
type TitleGenerator interface {
	// Generate sends prompt to the LLM and returns the raw response text.
	// ctx carries the caller's deadline / cancellation.
	Generate(ctx context.Context, prompt string) (string, error)
}

// TitleConfig holds tunables for TitleMiddleware.
type TitleConfig struct {
	// Enabled controls whether title generation runs at all.
	// Set to false to disable in environments without LLM access.
	Enabled bool

	// PromptTemplate is a fmt-style template with three named placeholders:
	//   {max_words}      — the MaxWords limit hint sent to the LLM
	//   {user_msg}       — the first user message (truncated to 500 chars)
	//   {assistant_msg}  — the first assistant reply (truncated to 500 chars)
	// If empty, a default template is used.
	PromptTemplate string

	// MaxWords is the word-count hint included in the prompt.
	MaxWords int
}

// DefaultTitleConfig returns a TitleConfig with sensible defaults.
// Aligned with DeerFlow's default values (P2 alignment).
func DefaultTitleConfig() TitleConfig {
	return TitleConfig{
		Enabled:  true,
		MaxWords: 6, // Aligned with DeerFlow's default
		PromptTemplate: `Generate a concise title for the following conversation in at most {max_words} words.
Reply with only the title, no quotes, no punctuation at the end.

User: {user_msg}
Assistant: {assistant_msg}`,
	}
}

// TitleMiddleware generates a conversation title after the first exchange.
// It implements middleware.Middleware; Before is a no-op.
type TitleMiddleware struct {
	middleware.MiddlewareWrapper
	cfg TitleConfig
	gen TitleGenerator
}

// NewTitleMiddleware constructs a TitleMiddleware with the given config and
// LLM generator. Pass a nil generator to disable generation (useful in tests).
func NewTitleMiddleware(cfg TitleConfig, gen TitleGenerator) *TitleMiddleware {
	return &TitleMiddleware{cfg: cfg, gen: gen}
}

// Name implements middleware.Middleware.
func (t *TitleMiddleware) Name() string { return "TitleMiddleware" }

// BeforeModel is a no-op for TitleMiddleware — title generation happens AfterModel.
func (t *TitleMiddleware) BeforeModel(_ context.Context, _ *middleware.State) error { return nil }

// AfterModel generates the conversation title and writes it to state.Title.
func (t *TitleMiddleware) AfterModel(ctx context.Context, state *middleware.State, _ *middleware.Response) error {
	if !t.cfg.Enabled || state.Title != "" {
		return nil
	}

	// --- Step 2: check message counts ---
	var humanCount, assistantCount int
	var firstUserMsg, firstAssistantMsg string

	for _, msg := range state.Messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		switch role {
		case "human":
			humanCount++
			if humanCount == 1 {
				firstUserMsg = content
			}
		case "assistant":
			assistantCount++
			if assistantCount == 1 {
				firstAssistantMsg = content
			}
		}
	}

	if humanCount != 1 || assistantCount < 1 {
		return nil
	}

	// --- Step 4: build prompt ---
	truncate := func(s string, n int) string {
		if len(s) > n {
			return s[:n]
		}
		return s
	}

	prompt := t.cfg.PromptTemplate
	prompt = strings.ReplaceAll(prompt, "{max_words}", strconv.Itoa(t.cfg.MaxWords))
	prompt = strings.ReplaceAll(prompt, "{user_msg}", truncate(firstUserMsg, 500))
	prompt = strings.ReplaceAll(prompt, "{assistant_msg}", truncate(firstAssistantMsg, 500))

	// --- Steps 5–8: generate or fallback ---
	title := t.fallback(firstUserMsg)

	if t.gen != nil {
		if raw, err := t.gen.Generate(ctx, prompt); err == nil {
			cleaned := t.parseTitle(raw)
			if cleaned != "" {
				title = cleaned
			}
		}
	}

	state.Title = title
	return nil
}

// parseTitle strips surrounding whitespace and quotes, then truncates.
func (t *TitleMiddleware) parseTitle(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, `"'`)
	s = strings.TrimSpace(s)
	if len(s) > MaxTitleChars {
		s = s[:MaxTitleChars]
	}
	return s
}

// fallback returns a title derived from the first user message.
func (t *TitleMiddleware) fallback(userMsg string) string {
	if userMsg == "" {
		return "New Conversation"
	}
	if len(userMsg) > MaxFallbackChars {
		return strings.TrimRight(userMsg[:MaxFallbackChars], " ") + "..."
	}
	return userMsg
}
