// Package title implements TitleMiddleware for GoClaw.
//
// TitleMiddleware generates a short conversation title using an LLM after the
// first complete human→assistant exchange. Once a title is set on State it is
// never overwritten, so subsequent turns are fast no-ops.
//
// This mirrors DeerFlow's TitleMiddleware.after_model behaviour:
//  1. Skip if state.Title is already set.
//  2. Skip if there is not exactly one human message and at least one
//     assistant message (i.e. not the end of the first exchange).
//  3. Build a concise prompt with the first user question and assistant reply.
//  4. Call the LLM; strip quotes; truncate to MaxTitleChars.
//  5. Fallback to the first 50 chars of the user message on any error.
//  6. Write the title back to state.Title.
package title

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

// Config holds tunables for TitleMiddleware.
type Config struct {
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

// DefaultConfig returns a Config with sensible defaults.
// Aligned with DeerFlow's default values (P2 alignment).
func DefaultConfig() Config {
	return Config{
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
	cfg Config
	gen TitleGenerator
}

// NewTitleMiddleware constructs a TitleMiddleware with the given config and
// LLM generator. Pass a nil generator to disable generation (useful in tests).
func NewTitleMiddleware(cfg Config, gen TitleGenerator) *TitleMiddleware {
	return &TitleMiddleware{cfg: cfg, gen: gen}
}

// Name implements middleware.Middleware.
func (t *TitleMiddleware) Name() string { return "TitleMiddleware" }

// BeforeModel is a no-op for TitleMiddleware — title generation happens AfterModel.
func (t *TitleMiddleware) BeforeModel(_ context.Context, _ *middleware.State) error { return nil }

// AfterModel generates the conversation title and writes it to state.Title.
//
// Implementation steps:
//  1. Return early if !t.cfg.Enabled || state.Title != "" (already set).
//  2. Count human and assistant messages in state.Messages.
//     Return early unless exactly 1 human and ≥1 assistant message exist
//     (title is generated only once, at the end of the first exchange).
//  3. Extract the text of the first human and first assistant messages.
//  4. Build prompt from t.cfg.PromptTemplate (replace placeholders).
//  5. If t.gen is nil, use fallback immediately.
//  6. Call t.gen.Generate(ctx, prompt); on error use fallback.
//  7. Clean the response: strip surrounding quotes, whitespace.
//     Truncate to MaxTitleChars.
//  8. If cleaned title is empty, use fallback.
//  9. Set state.Title = title.
func (t *TitleMiddleware) AfterModel(ctx context.Context, state *middleware.State, _ *middleware.Response) error {
	// TODO: implement all 9 steps above.
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
		// On error: keep fallback. TODO: log the error.
	}

	state.Title = title
	return nil
}

// parseTitle strips surrounding whitespace and quotes, then truncates.
func (t *TitleMiddleware) parseTitle(raw string) string {
	// TODO:
	// 1. strings.TrimSpace(raw)
	// 2. Strip leading/trailing double and single quotes.
	// 3. Truncate to MaxTitleChars.
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
