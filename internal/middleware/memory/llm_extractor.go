package memory

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"goclaw/pkg/errors"
)

// EinoFactExtractor uses an Eino chat model to extract structured memory facts.
type EinoFactExtractor struct {
	chatModel     model.BaseChatModel
	minConfidence float64
	timeout       time.Duration
}

// NewEinoFactExtractor creates a FactExtractor backed by an Eino chat model.
func NewEinoFactExtractor(chatModel model.BaseChatModel, minConfidence float64) *EinoFactExtractor {
	if minConfidence <= 0 {
		minConfidence = 0.7
	}
	return &EinoFactExtractor{
		chatModel:     chatModel,
		minConfidence: minConfidence,
		timeout:       20 * time.Second,
	}
}

// Extract implements FactExtractor.
func (e *EinoFactExtractor) Extract(messages []map[string]any, correctionDetected bool) ([]Fact, error) {
	if e == nil || e.chatModel == nil {
		return nil, errors.ConfigError("memory extractor: chat model is nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	prompt := buildMemoryExtractionUserPrompt(messages, correctionDetected)
	resp, err := e.chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage(MemoryUpdatePrompt),
		schema.UserMessage(prompt),
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		return nil, nil
	}

	facts, err := parseMemoryFactsOutput(resp.Content)
	if err != nil {
		return nil, err
	}

	// Apply confidence threshold and content cleanup.
	filtered := make([]Fact, 0, len(facts)+1)
	seen := map[string]struct{}{}
	for _, f := range facts {
		f.Content = strings.TrimSpace(f.Content)
		if f.Content == "" {
			continue
		}
		if f.Confidence <= 0 {
			f.Confidence = 0.8
		}
		if f.Confidence < e.minConfidence {
			continue
		}
		key := strings.ToLower(f.Content)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		filtered = append(filtered, f)
	}

	if correctionDetected {
		key := strings.ToLower("User corrected a previous assistant response.")
		if _, ok := seen[key]; !ok {
			filtered = append(filtered, Fact{
				Content:    "User corrected a previous assistant response.",
				Category:   CategoryCorrection,
				Confidence: 0.95,
			})
		}
	}

	return filtered, nil
}

func buildMemoryExtractionUserPrompt(messages []map[string]any, correctionDetected bool) string {
	var b strings.Builder
	b.WriteString("Analyze the following conversation and extract durable user facts. Return JSON array only.\n\n")
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if strings.TrimSpace(content) == "" {
			continue
		}
		b.WriteString(strings.ToUpper(role))
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteString("\n")
	}
	if correctionDetected {
		b.WriteString("\nNOTE: user correction detected in this turn.\n")
	}
	return b.String()
}

func parseMemoryFactsOutput(raw string) ([]Fact, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, nil
	}

	// Strip markdown code fences if present.
	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```")
		if idx := strings.Index(clean, "\n"); idx >= 0 {
			clean = clean[idx+1:]
		}
		if end := strings.LastIndex(clean, "```"); end >= 0 {
			clean = clean[:end]
		}
		clean = strings.TrimSpace(clean)
	}

	// Extract JSON array segment if extra text exists.
	start := strings.Index(clean, "[")
	end := strings.LastIndex(clean, "]")
	if start >= 0 && end > start {
		clean = clean[start : end+1]
	}

	if strings.TrimSpace(clean) == "[]" {
		return nil, nil
	}

	var facts []Fact
	if err := json.Unmarshal([]byte(clean), &facts); err != nil {
		return nil, errors.WrapInternalError(err, "memory extractor: parse facts json")
	}
	return facts, nil
}

var _ FactExtractor = (*EinoFactExtractor)(nil)
