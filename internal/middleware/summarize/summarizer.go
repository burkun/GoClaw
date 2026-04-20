package summarize

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"goclaw/pkg/errors"
)

// EinoSummarizer adapts an Eino BaseChatModel to Summarizer.
type EinoSummarizer struct {
	chatModel model.BaseChatModel
}

// NewEinoSummarizer creates a summarizer adapter.
func NewEinoSummarizer(chatModel model.BaseChatModel) *EinoSummarizer {
	return &EinoSummarizer{chatModel: chatModel}
}

// Summarize implements Summarizer.
func (s *EinoSummarizer) Summarize(ctx context.Context, history string) (string, error) {
	if s == nil || s.chatModel == nil {
		return "", errors.ConfigError("summarizer: chat model is nil")
	}
	resp, err := s.chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage("Summarize conversation context while preserving decisions, constraints, and user preferences."),
		schema.UserMessage(history),
	})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.InternalError("summarizer: empty model response")
	}
	return strings.TrimSpace(resp.Content), nil
}

var _ Summarizer = (*EinoSummarizer)(nil)
