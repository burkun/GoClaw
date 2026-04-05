package docker

import (
	"testing"

	"github.com/bookerbai/goclaw/internal/sandbox"
)

func TestEffectiveContainerPrefix(t *testing.T) {
	if got := effectiveContainerPrefix(sandbox.SandboxConfig{}); got != defaultContainerNamePrefix {
		t.Fatalf("default prefix: got %q, want %q", got, defaultContainerNamePrefix)
	}

	cfgNoDash := sandbox.SandboxConfig{Docker: sandbox.DockerConfig{ContainerPrefix: "custom-prefix"}}
	if got := effectiveContainerPrefix(cfgNoDash); got != "custom-prefix-" {
		t.Fatalf("custom prefix without dash: got %q, want %q", got, "custom-prefix-")
	}

	cfgWithDash := sandbox.SandboxConfig{Docker: sandbox.DockerConfig{ContainerPrefix: "custom-prefix-"}}
	if got := effectiveContainerPrefix(cfgWithDash); got != "custom-prefix-" {
		t.Fatalf("custom prefix with dash: got %q, want %q", got, "custom-prefix-")
	}
}

func TestBuildContainerEnvSorted(t *testing.T) {
	env := map[string]string{
		"B": "2",
		"A": "1",
		"":  "skip",
	}
	got := buildContainerEnv(env)
	if len(got) != 2 {
		t.Fatalf("len(got)=%d, want 2; got=%v", len(got), got)
	}
	if got[0] != "A=1" || got[1] != "B=2" {
		t.Fatalf("unexpected env order/content: %v", got)
	}
}
