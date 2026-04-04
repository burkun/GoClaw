// Package summarize implements SummarizationMiddleware for GoClaw.
//
// SummarizationMiddleware monitors the approximate token count of the message
// history. When it approaches the configured context-window limit, it compresses
// older messages into a single summary message, keeping recent exchanges intact.
//
// This mirrors DeerFlow's SummarizationMiddleware behaviour:
//   - Triggered when TokenCount > ThresholdTokens (default: 80 % of the limit).
//   - Keeps the last KeepRecentMessages messages verbatim.
//   - Summarises the earlier messages using an LLM.
//   - Replaces the older messages with a single "system" summary message.
//   - The summary message is prefixed with a sentinel so subsequent runs can
//     detect that summarisation has already occurred for that window.
package summarize

import (
	"context"
	"fmt"
	"strings"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// DefaultTokenLimit is the conservative estimate of the model's context window.
// Adjust per model; GPT-4o supports 128 k, Claude supports 200 k, etc.
const DefaultTokenLimit = 128_000

// DefaultThresholdRatio is the fraction of DefaultTokenLimit at which
// summarisation is triggered (0.8 = 80 %).
const DefaultThresholdRatio = 0.8

// DefaultKeepRecentMessages is the number of recent messages preserved
// verbatim after summarisation.
const DefaultKeepRecentMessages = 10

// SummarySentinel is prepended to the content of the injected summary message
// so subsequent runs can detect a pre-existing summary.
const SummarySentinel = "[conversation_summary]"

// Summarizer is the narrow LLM interface used for compression.
type Summarizer interface {
	// Summarize sends the formatted history to an LLM and returns a concise
	// summary string. ctx carries the caller's cancellation / deadline.
	Summarize(ctx context.Context, history string) (string, error)
}

// Config holds tunables for SummarizationMiddleware.
type Config struct {
	// Enabled controls whether summarisation runs at all.
	Enabled bool

	// TokenLimit is the model's hard context-window limit in tokens.
	// Summarisation triggers when State.TokenCount exceeds ThresholdTokens.
	TokenLimit int

	// ThresholdRatio is the fraction of TokenLimit that triggers summarisation.
	// Must be in (0, 1]. Default: 0.8.
	ThresholdRatio float64

	// KeepRecentMessages is the number of recent messages kept verbatim after
	// summarisation. Must be ≥ 1. Default: 10.
	KeepRecentMessages int

	// PromptTemplate is the instruction prepended when asking the LLM to
	// summarise the conversation history. If empty, a default is used.
	PromptTemplate string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:            true,
		TokenLimit:         DefaultTokenLimit,
		ThresholdRatio:     DefaultThresholdRatio,
		KeepRecentMessages: DefaultKeepRecentMessages,
		PromptTemplate: `Summarise the conversation below concisely, preserving the key decisions, ` +
			`facts, and user preferences. Write in third-person present tense. ` +
			`Keep the summary under 300 words.`,
	}
}

// thresholdTokens returns the computed threshold token count.
func (c *Config) thresholdTokens() int {
	return int(float64(c.TokenLimit) * c.ThresholdRatio)
}

// SummarizationMiddleware compresses the message history when it approaches
// the context-window limit. It implements middleware.Middleware.
// Before checks whether compression is needed and applies it.
// After is a no-op.
type SummarizationMiddleware struct {
	middleware.MiddlewareWrapper
	cfg  Config
	summ Summarizer
}

// NewSummarizationMiddleware constructs a SummarizationMiddleware.
// Pass a nil Summarizer to disable LLM compression (useful in tests); the
// middleware will still run the heuristic but replace with a placeholder.
func NewSummarizationMiddleware(cfg Config, summ Summarizer) *SummarizationMiddleware {
	return &SummarizationMiddleware{cfg: cfg, summ: summ}
}

// Name implements middleware.Middleware.
func (s *SummarizationMiddleware) Name() string { return "SummarizationMiddleware" }

// After is a no-op for SummarizationMiddleware.
func (s *SummarizationMiddleware) After(_ context.Context, _ *middleware.State, _ *middleware.Response) error {
	return nil
}

// Before checks whether the token budget has been exceeded and, if so,
// compresses older messages into a summary.
//
// Implementation steps:
//  1. Return early if !s.cfg.Enabled.
//  2. Estimate token count: if state.TokenCount == 0, approximate via
//     estimateTokens(state.Messages).
//  3. If count ≤ thresholdTokens, return nil (no action needed).
//  4. Partition state.Messages into older (to summarise) and recent (to keep):
//     recent = last s.cfg.KeepRecentMessages messages.
//     older  = everything before that.
//  5. If any existing message already starts with SummarySentinel, remove it
//     from older (it was a prior summary; include its content in the new one).
//  6. Format older as a conversation transcript string.
//  7. If s.summ != nil, call s.summ.Summarize(ctx, transcript).
//     On error, log and use a fallback summary string.
//  8. Build a single system summary message:
//     content = SummarySentinel + "\n" + summaryText
//  9. Replace state.Messages with [summaryMsg] + recent.
// 10. Update state.TokenCount = estimateTokens(state.Messages).
func (s *SummarizationMiddleware) Before(ctx context.Context, state *middleware.State) error {
	// TODO: implement all 10 steps above.

	if !s.cfg.Enabled {
		return nil
	}

	tokenCount := state.TokenCount
	if tokenCount == 0 {
		tokenCount = estimateTokens(state.Messages)
		state.TokenCount = tokenCount
	}

	if tokenCount <= s.cfg.thresholdTokens() {
		return nil
	}

	// --- Step 4: partition messages ---
	keep := s.cfg.KeepRecentMessages
	if keep <= 0 {
		keep = DefaultKeepRecentMessages
	}

	var older, recent []map[string]any
	if len(state.Messages) > keep {
		older = state.Messages[:len(state.Messages)-keep]
		recent = state.Messages[len(state.Messages)-keep:]
	} else {
		// Nothing to summarise — all messages are "recent".
		recent = state.Messages
	}

	if len(older) == 0 {
		return nil
	}

	// --- Step 5: extract and strip prior summary sentinel from older ---
	var priorSummary string
	var filteredOlder []map[string]any
	for _, msg := range older {
		content, _ := msg["content"].(string)
		if strings.HasPrefix(content, SummarySentinel) {
			// Peel off the sentinel and collect the prior summary text.
			priorSummary = strings.TrimPrefix(content, SummarySentinel+"\n")
			continue
		}
		filteredOlder = append(filteredOlder, msg)
	}

	// --- Step 6: format older as transcript ---
	transcript := formatTranscript(filteredOlder, priorSummary, s.cfg.PromptTemplate)

	// --- Step 7: LLM summarisation ---
	summaryText := "[Conversation history summarised — details omitted.]"
	if s.summ != nil {
		if text, err := s.summ.Summarize(ctx, transcript); err == nil && text != "" {
			summaryText = text
		}
		// TODO: log error when summarisation fails.
	}

	// --- Steps 8–9: rebuild message list ---
	summaryMsg := map[string]any{
		"role":    "system",
		"content": fmt.Sprintf("%s\n%s", SummarySentinel, summaryText),
	}
	state.Messages = append([]map[string]any{summaryMsg}, recent...)

	// --- Step 10: update token count ---
	state.TokenCount = estimateTokens(state.Messages)

	return nil
}

// estimateTokens approximates the token count of a message list.
// The heuristic (characters / 4) is intentionally conservative; replace with
// a tiktoken binding for production accuracy.
func estimateTokens(messages []map[string]any) int {
	// TODO: replace with a proper tokeniser binding (e.g. tiktoken-go).
	total := 0
	for _, msg := range messages {
		content, _ := msg["content"].(string)
		total += len(content) / 4
	}
	return total
}

// formatTranscript converts a message slice (and optional prior summary) into
// a plain-text transcript suitable for the summarisation LLM prompt.
func formatTranscript(messages []map[string]any, priorSummary, promptTemplate string) string {
	// TODO:
	// 1. Start with promptTemplate + "\n\n".
	// 2. If priorSummary != "", prepend "[Previous summary]\n" + priorSummary + "\n\n".
	// 3. Append each message as "Role: content\n".

	var b strings.Builder
	b.WriteString(promptTemplate)
	b.WriteString("\n\n")

	if priorSummary != "" {
		b.WriteString("[Previous summary]\n")
		b.WriteString(priorSummary)
		b.WriteString("\n\n")
	}

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "" {
			role = "message"
		}
		b.WriteString(strings.ToUpper(role[:1]))
		b.WriteString(role[1:])
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteString("\n")
	}

	return b.String()
}
