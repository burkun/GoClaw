// Package ui implements UI-related middleware for GoClaw.
//
// This package contains middlewares that handle user interface concerns,
// including title generation and image viewing.
package ui

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/bookerbai/goclaw/pkg/errors"
)

// EinoTitleGenerator adapts an Eino BaseChatModel to TitleGenerator.
type EinoTitleGenerator struct {
	chatModel model.BaseChatModel
}

// NewEinoTitleGenerator creates a title generator adapter.
func NewEinoTitleGenerator(chatModel model.BaseChatModel) *EinoTitleGenerator {
	return &EinoTitleGenerator{chatModel: chatModel}
}

// Generate implements TitleGenerator.
func (g *EinoTitleGenerator) Generate(ctx context.Context, prompt string) (string, error) {
	if g == nil || g.chatModel == nil {
		return "", errors.ConfigError("title generator: chat model is nil")
	}
	resp, err := g.chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage("You generate concise, descriptive conversation titles."),
		schema.UserMessage(prompt),
	})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.InternalError("title generator: empty model response")
	}
	return strings.TrimSpace(resp.Content), nil
}

var _ TitleGenerator = (*EinoTitleGenerator)(nil)
