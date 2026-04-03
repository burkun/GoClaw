package skills

import (
	"context"
	"testing"

	"github.com/bookerbai/goclaw/internal/config"
)

type fakePlugin struct {
	name     string
	onLoad   int
	onReload int
	onUnload int
}

func (p *fakePlugin) Name() string { return p.name }
func (p *fakePlugin) OnLoad(_ context.Context, _ *config.AppConfig) error {
	p.onLoad++
	return nil
}
func (p *fakePlugin) OnUnload(_ context.Context) error {
	p.onUnload++
	return nil
}
func (p *fakePlugin) OnConfigReload(_ *config.AppConfig) error {
	p.onReload++
	return nil
}

func TestRegistryLifecycle(t *testing.T) {
	r := NewRegistry()
	p := &fakePlugin{name: "skill-a"}

	if err := r.Register(&Skill{Metadata: SkillMetadata{Name: "skill-a"}, Plugin: p}); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if err := r.OnLoad(context.Background(), &config.AppConfig{}); err != nil {
		t.Fatalf("onload failed: %v", err)
	}
	if err := r.OnConfigReload(&config.AppConfig{}); err != nil {
		t.Fatalf("onreload failed: %v", err)
	}
	if err := r.OnUnload(context.Background()); err != nil {
		t.Fatalf("onunload failed: %v", err)
	}

	if p.onLoad != 1 || p.onReload != 1 || p.onUnload != 1 {
		t.Fatalf("unexpected lifecycle counts: load=%d reload=%d unload=%d", p.onLoad, p.onReload, p.onUnload)
	}
}

func TestRegistryAllowedToolSet(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&Skill{Metadata: SkillMetadata{Name: "skill-a", AllowedTools: []string{"bash", "read"}}, Plugin: &fakePlugin{name: "skill-a"}}); err != nil {
		t.Fatalf("register skill-a failed: %v", err)
	}
	if err := r.Register(&Skill{Metadata: SkillMetadata{Name: "skill-b", AllowedTools: []string{"read", "write"}}, Plugin: &fakePlugin{name: "skill-b"}}); err != nil {
		t.Fatalf("register skill-b failed: %v", err)
	}

	allowed := r.AllowedToolSet()
	if len(allowed) != 3 {
		t.Fatalf("expected 3 allowed tools, got %d", len(allowed))
	}
	for _, name := range []string{"bash", "read", "write"} {
		if _, ok := allowed[name]; !ok {
			t.Fatalf("expected allowed tool %q", name)
		}
	}
}

func TestRegistryAllowedToolSet_NoRestrictions(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&Skill{Metadata: SkillMetadata{Name: "skill-a"}, Plugin: &fakePlugin{name: "skill-a"}}); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if got := r.AllowedToolSet(); got != nil {
		t.Fatalf("expected nil allowed set when no restrictions, got %+v", got)
	}
}
