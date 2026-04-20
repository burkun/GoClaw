package builder

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"goclaw/internal/config"
	"goclaw/internal/middleware"
	"goclaw/internal/sandbox"
)

// mockSandboxProvider implements sandbox.SandboxProvider for testing.
type mockSandboxProvider struct{}

func (m *mockSandboxProvider) Acquire(_ context.Context, _ string) (string, error) {
	return "sandbox-1", nil
}

func (m *mockSandboxProvider) Get(_ string) sandbox.Sandbox {
	return nil
}

func (m *mockSandboxProvider) Release(_ context.Context, _ string) error {
	return nil
}

func (m *mockSandboxProvider) Shutdown(_ context.Context) error {
	return nil
}

// mockToolCallingChatModel implements model.ToolCallingChatModel for testing.
type mockToolCallingChatModel struct {
	model.BaseChatModel
}

func (m *mockToolCallingChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage("mock response", nil), nil
}

func (m *mockToolCallingChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (m *mockToolCallingChatModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func TestBuildMiddlewaresFromBuilder_NilConfig(t *testing.T) {
	mws := BuildMiddlewaresFromBuilder(nil)
	if len(mws) == 0 {
		t.Error("expected some middlewares even with nil config")
	}
}

func TestBuildMiddlewaresFromBuilder_EmptyConfig(t *testing.T) {
	mws := BuildMiddlewaresFromBuilder(&BuilderConfig{})
	if len(mws) == 0 {
		t.Error("expected some middlewares with empty config")
	}
}

func TestBuildMiddlewaresFromBuilder_WithSandbox(t *testing.T) {
	mws := BuildMiddlewaresFromBuilder(&BuilderConfig{
		SandboxProvider: &mockSandboxProvider{},
	})

	// Should have sandbox middleware
	found := false
	for _, mw := range mws {
		if mw.Name() == "SandboxMiddleware" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected SandboxMiddleware when provider is set")
	}
}

func TestBuildMiddlewaresFromBuilder_WithoutSandbox(t *testing.T) {
	mws := BuildMiddlewaresFromBuilder(&BuilderConfig{
		SandboxProvider: nil,
	})

	// Should NOT have sandbox middleware
	for _, mw := range mws {
		if mw.Name() == "SandboxMiddleware" {
			t.Error("expected no SandboxMiddleware when provider is nil")
			break
		}
	}
}

func TestBuildMiddlewaresFromBuilder_MemoryEnabled(t *testing.T) {
	appCfg := &config.AppConfig{
		Memory: config.MemoryConfig{
			Enabled:                 true,
			StoragePath:             "/tmp/test-memory.json",
			DebounceSeconds:         10,
			FactConfidenceThreshold: 0.8,
			MaxFacts:                50,
			InjectionEnabled:        true,
			MaxInjectionTokens:      1000,
		},
	}

	mws := BuildMiddlewaresFromBuilder(&BuilderConfig{
		AppConfig: appCfg,
	})

	// Should have memory middleware
	found := false
	for _, mw := range mws {
		if mw.Name() == "MemoryMiddleware" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected MemoryMiddleware when enabled")
	}
}

func TestBuildMiddlewaresFromBuilder_MemoryDisabled(t *testing.T) {
	appCfg := &config.AppConfig{
		Memory: config.MemoryConfig{Enabled: false},
	}

	mws := BuildMiddlewaresFromBuilder(&BuilderConfig{
		AppConfig: appCfg,
	})

	for _, mw := range mws {
		if mw.Name() == "MemoryMiddleware" {
			t.Error("expected no MemoryMiddleware when disabled")
			break
		}
	}
}

func TestBuildMiddlewaresFromBuilder_TitleEnabled(t *testing.T) {
	appCfg := &config.AppConfig{
		Title: config.TitleConfig{Enabled: true},
	}

	mws := BuildMiddlewaresFromBuilder(&BuilderConfig{
		AppConfig: appCfg,
	})

	found := false
	for _, mw := range mws {
		if mw.Name() == "TitleMiddleware" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected TitleMiddleware when enabled")
	}
}

func TestBuildMiddlewaresFromBuilder_TitleDisabled(t *testing.T) {
	appCfg := &config.AppConfig{
		Title: config.TitleConfig{Enabled: false},
	}

	mws := BuildMiddlewaresFromBuilder(&BuilderConfig{
		AppConfig: appCfg,
	})

	for _, mw := range mws {
		if mw.Name() == "TitleMiddleware" {
			t.Error("expected no TitleMiddleware when disabled")
			break
		}
	}
}

func TestBuildMiddlewaresFromBuilder_SummarizeEnabled(t *testing.T) {
	appCfg := &config.AppConfig{
		Summarization: config.SummarizationConfig{Enabled: true},
	}

	mws := BuildMiddlewaresFromBuilder(&BuilderConfig{
		AppConfig: appCfg,
	})

	found := false
	for _, mw := range mws {
		if mw.Name() == "SummarizationMiddleware" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected SummarizationMiddleware when enabled")
	}
}

func TestBuildMiddlewaresFromBuilder_SummarizeDisabled(t *testing.T) {
	appCfg := &config.AppConfig{
		Summarization: config.SummarizationConfig{Enabled: false},
	}

	mws := BuildMiddlewaresFromBuilder(&BuilderConfig{
		AppConfig: appCfg,
	})

	for _, mw := range mws {
		if mw.Name() == "SummarizationMiddleware" {
			t.Error("expected no SummarizationMiddleware when disabled")
			break
		}
	}
}

func TestBuildMiddlewaresFromBuilder_TokenUsageEnabled(t *testing.T) {
	appCfg := &config.AppConfig{
		TokenUsage: config.TokenUsageConfig{Enabled: true},
	}

	mws := BuildMiddlewaresFromBuilder(&BuilderConfig{
		AppConfig: appCfg,
	})

	found := false
	for _, mw := range mws {
		if mw.Name() == "TokenUsageMiddleware" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected TokenUsageMiddleware when enabled")
	}
}

func TestBuildMiddlewaresFromBuilder_AlwaysPresentMiddlewares(t *testing.T) {
	mws := BuildMiddlewaresFromBuilder(&BuilderConfig{})

	required := []string{
		"ThreadDataMiddleware",
		"UploadsMiddleware",
		"DanglingToolCallMiddleware",
		"GuardrailMiddleware",
		"SandboxAuditMiddleware",
		"TodoMiddleware",
		"ViewImageMiddleware",
		"SubagentLimitMiddleware",
		"LoopDetectionMiddleware",
		"DeferredToolFilterMiddleware",
		"LLMErrorHandlingMiddleware",
		"ToolErrorHandlingMiddleware",
		"ClarificationMiddleware",
	}

	found := make(map[string]bool)
	for _, mw := range mws {
		found[mw.Name()] = true
	}

	for _, name := range required {
		if !found[name] {
			t.Errorf("expected required middleware %s to be present", name)
		}
	}
}

func TestBuildMiddlewaresFromBuilder_Order(t *testing.T) {
	mws := BuildMiddlewaresFromBuilder(&BuilderConfig{})

	// Check order of some critical middlewares
	order := make(map[string]int)
	for i, mw := range mws {
		order[mw.Name()] = i
	}

	// ThreadData should come before Uploads
	if order["ThreadDataMiddleware"] >= order["UploadsMiddleware"] {
		t.Error("ThreadDataMiddleware should come before UploadsMiddleware")
	}

	// DanglingToolCall should come before Guardrail
	if order["DanglingToolCallMiddleware"] >= order["GuardrailMiddleware"] {
		t.Error("DanglingToolCallMiddleware should come before GuardrailMiddleware")
	}

	// ViewImage should come before SubagentLimit
	if order["ViewImageMiddleware"] >= order["SubagentLimitMiddleware"] {
		t.Error("ViewImageMiddleware should come before SubagentLimitMiddleware")
	}
}

func TestBuildMiddlewaresFromBuilder_WithModelCreator(t *testing.T) {
	creator := middleware.ModelCreator(func(_ context.Context, _ string) (model.ToolCallingChatModel, error) {
		return &mockToolCallingChatModel{}, nil
	})

	appCfg := &config.AppConfig{
		Memory: config.MemoryConfig{
			Enabled:   true,
			ModelName: "test-model",
		},
		Title: config.TitleConfig{
			Enabled:   true,
			ModelName: "test-model",
		},
		Summarization: config.SummarizationConfig{
			Enabled:   true,
			ModelName: "test-model",
		},
	}

	mws := BuildMiddlewaresFromBuilder(&BuilderConfig{
		AppConfig:       appCfg,
		CreateChatModel: creator,
	})

	// Should still build all middlewares
	if len(mws) == 0 {
		t.Error("expected middlewares to be built")
	}
}

func TestRegisterMiddlewares(t *testing.T) {
	registry := middleware.NewRegistry()

	mws := []middleware.Middleware{
		&mockMW{name: "test1"},
		&mockMW{name: "test2"},
	}

	if err := RegisterMiddlewares(registry, mws); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(registry.List()) != 2 {
		t.Errorf("expected 2 registered middlewares, got %d", len(registry.List()))
	}
}

func TestRegisterMiddlewares_DuplicateName(t *testing.T) {
	registry := middleware.NewRegistry()

	mws := []middleware.Middleware{
		&mockMW{name: "test"},
		&mockMW{name: "test"}, // duplicate
	}

	err := RegisterMiddlewares(registry, mws)
	if err == nil {
		t.Error("expected error for duplicate names")
	}
}

// mockMW implements middleware.Middleware for testing.
type mockMW struct {
	middleware.MiddlewareWrapper
	name string
}

func (m *mockMW) Name() string { return m.name }
