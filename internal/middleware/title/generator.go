package title

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
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
		return "", fmt.Errorf("title generator: chat model is nil")
	}
	resp, err := g.chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage("You generate concise, descriptive conversation titles."),
		schema.UserMessage(prompt),
	})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("title generator: empty model response")
	}
	return strings.TrimSpace(resp.Content), nil
}

var _ TitleGenerator = (*EinoTitleGenerator)(nil)
