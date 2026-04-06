package models

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/bookerbai/goclaw/internal/config"
)

type stubModel struct{}

func (s *stubModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	_ = ctx
	_ = input
	_ = opts
	return schema.AssistantMessage("ok", nil), nil
}

func (s *stubModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	_ = ctx
	_ = input
	_ = opts
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("ok", nil)}), nil
}

func TestCreateChatModel_UsesDefaultModelAndBuilder(t *testing.T) {
	ResetProviderBuilders()
	t.Cleanup(ResetProviderBuilders)

	called := false
	RegisterProviderBuilder("openai", func(ctx context.Context, cfg config.ModelConfig, opts BuildOptions) (model.BaseChatModel, error) {
		called = true
		require.Equal(t, "gpt-4o", cfg.Name)
		// Note: ThinkingEnabled is false because model doesn't have SupportsThinking=true
		require.False(t, opts.ThinkingEnabled)
		require.Equal(t, "high", opts.ReasoningEffort)
		return &stubModel{}, nil
	})

	appCfg := &config.AppConfig{
		Models: []config.ModelConfig{{
			Name:  "gpt-4o",
			Use:   "openai",
			Model: "gpt-4o",
			// Note: SupportsThinking is false by default, so ThinkingEnabled will be disabled
		}},
	}

	m, err := CreateChatModel(context.Background(), appCfg, CreateRequest{ThinkingEnabled: true, ReasoningEffort: "high"})
	require.NoError(t, err)
	require.NotNil(t, m)
	require.True(t, called)
}

func TestCreateChatModel_ModelNotFound(t *testing.T) {
	ResetProviderBuilders()
	t.Cleanup(ResetProviderBuilders)

	appCfg := &config.AppConfig{Models: []config.ModelConfig{{Name: "a", Use: "openai", Model: "x"}}}
	_, err := CreateChatModel(context.Background(), appCfg, CreateRequest{ModelName: "missing"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestCreateChatModel_ProviderNotRegistered(t *testing.T) {
	ResetProviderBuilders()
	t.Cleanup(ResetProviderBuilders)

	appCfg := &config.AppConfig{Models: []config.ModelConfig{{Name: "a", Use: "anthropic", Model: "x"}}}
	_, err := CreateChatModel(context.Background(), appCfg, CreateRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not registered")
}

func TestCreateChatModel_PropagatesBuilderError(t *testing.T) {
	ResetProviderBuilders()
	t.Cleanup(ResetProviderBuilders)

	RegisterProviderBuilder("openai", func(ctx context.Context, cfg config.ModelConfig, opts BuildOptions) (model.BaseChatModel, error) {
		return nil, errors.New("boom")
	})

	appCfg := &config.AppConfig{Models: []config.ModelConfig{{Name: "a", Use: "openai", Model: "x"}}}
	_, err := CreateChatModel(context.Background(), appCfg, CreateRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "boom")
}
