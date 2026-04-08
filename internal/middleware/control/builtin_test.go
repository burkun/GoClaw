package control

import (
	"context"
	"testing"
)

func TestAllowlistProvider_Name(t *testing.T) {
	p := NewAllowlistProvider(AllowlistProviderConfig{})
	if p.Name() != "allowlist" {
		t.Errorf("expected name 'allowlist', got %s", p.Name())
	}
}

func TestAllowlistProvider_EmptyConfig(t *testing.T) {
	p := NewAllowlistProvider(AllowlistProviderConfig{})

	// With no allowlist or denylist, all tools should be allowed
	decision, err := p.Evaluate(context.Background(), GuardrailRequest{ToolName: "any_tool"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Allow {
		t.Error("expected tool to be allowed")
	}
}

func TestAllowlistProvider_AllowlistOnly(t *testing.T) {
	p := NewAllowlistProvider(AllowlistProviderConfig{
		AllowedTools: []string{"read_file", "write_file"},
	})

	// Tool in allowlist should be allowed
	decision, err := p.Evaluate(context.Background(), GuardrailRequest{ToolName: "read_file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Allow {
		t.Error("expected read_file to be allowed")
	}

	// Tool not in allowlist should be denied
	decision, err = p.Evaluate(context.Background(), GuardrailRequest{ToolName: "bash"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Allow {
		t.Error("expected bash to be denied")
	}
	if len(decision.Reasons) == 0 {
		t.Fatal("expected reasons for denial")
	}
	if decision.Reasons[0].Code != ReasonToolNotAllowed {
		t.Errorf("expected code %s, got %s", ReasonToolNotAllowed, decision.Reasons[0].Code)
	}
}

func TestAllowlistProvider_DenylistOnly(t *testing.T) {
	p := NewAllowlistProvider(AllowlistProviderConfig{
		DeniedTools: []string{"bash", "rm"},
	})

	// Tool in denylist should be denied
	decision, err := p.Evaluate(context.Background(), GuardrailRequest{ToolName: "bash"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Allow {
		t.Error("expected bash to be denied")
	}

	// Tool not in denylist should be allowed
	decision, err = p.Evaluate(context.Background(), GuardrailRequest{ToolName: "read_file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Allow {
		t.Error("expected read_file to be allowed")
	}
}

func TestAllowlistProvider_BothLists(t *testing.T) {
	p := NewAllowlistProvider(AllowlistProviderConfig{
		AllowedTools: []string{"read_file", "write_file", "bash"},
		DeniedTools:  []string{"bash"},
	})

	// Allowlist is checked first, but denylist should still apply
	decision, err := p.Evaluate(context.Background(), GuardrailRequest{ToolName: "bash"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Allow {
		t.Error("expected bash to be denied (in denylist)")
	}

	// Tool in allowlist but not in denylist
	decision, err = p.Evaluate(context.Background(), GuardrailRequest{ToolName: "read_file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Allow {
		t.Error("expected read_file to be allowed")
	}
}

func TestAllowlistProvider_DenylistOverridesAllowlist(t *testing.T) {
	p := NewAllowlistProvider(AllowlistProviderConfig{
		AllowedTools: []string{"read_file"},
		DeniedTools:  []string{"read_file"},
	})

	// Tool in both lists - allowlist checked first (passes), then denylist (fails)
	decision, err := p.Evaluate(context.Background(), GuardrailRequest{ToolName: "read_file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Allow {
		t.Error("expected read_file to be denied (denylist should override)")
	}
}
