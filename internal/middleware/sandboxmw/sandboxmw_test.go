package sandboxmw

import (
	"context"
	"errors"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
	"github.com/bookerbai/goclaw/internal/sandbox"
)

type stubSandbox struct{ id string }

func (s *stubSandbox) ID() string { return s.id }
func (s *stubSandbox) Execute(_ context.Context, _ string) (sandbox.ExecuteResult, error) {
	return sandbox.ExecuteResult{}, nil
}
func (s *stubSandbox) ReadFile(_ context.Context, _ string) (string, error) { return "", nil }
func (s *stubSandbox) WriteFile(_ context.Context, _ string, _ string, _ bool) error {
	return nil
}
func (s *stubSandbox) ListDir(_ context.Context, _ string, _ int) ([]sandbox.FileInfo, error) {
	return nil, nil
}
func (s *stubSandbox) StrReplace(_ context.Context, _ string, _ string, _ string, _ bool) error {
	return nil
}
func (s *stubSandbox) Glob(_ context.Context, _ string, _ string, _ bool, _ int) ([]string, bool, error) {
	return nil, false, nil
}
func (s *stubSandbox) Grep(_ context.Context, _ string, _ string, _ string, _ bool, _ bool, _ int) ([]sandbox.GrepMatch, bool, error) {
	return nil, false, nil
}
func (s *stubSandbox) UpdateFile(_ context.Context, _ string, _ []byte) error {
	return nil
}

type stubProvider struct {
	sb      *stubSandbox
	acqErr  error
	acqID   string
	getResp sandbox.Sandbox
}

func (p *stubProvider) Acquire(_ context.Context, threadID string) (string, error) {
	if p.acqErr != nil {
		return "", p.acqErr
	}
	if p.acqID != "" {
		return p.acqID, nil
	}
	return "sb-" + threadID, nil
}
func (p *stubProvider) Get(id string) sandbox.Sandbox {
	if p.getResp != nil {
		return p.getResp
	}
	if p.sb != nil && p.sb.id == id {
		return p.sb
	}
	return nil
}
func (p *stubProvider) Release(_ context.Context, _ string) error { return nil }
func (p *stubProvider) Shutdown(_ context.Context) error          { return nil }

func TestSandboxMiddleware_Before_AcquiresAndStores(t *testing.T) {
	sb := &stubSandbox{id: "sb-thread1"}
	provider := &stubProvider{sb: sb, acqID: sb.id, getResp: sb}
	mw := New(provider)

	state := &middleware.State{ThreadID: "thread1"}
	if err := mw.BeforeAgent(context.Background(), state); err != nil {
		t.Fatalf("BeforeAgent failed: %v", err)
	}

	got, ok := state.Extra["sandbox"].(*stubSandbox)
	if !ok || got.ID() != sb.id {
		t.Errorf("expected sandbox %s, got %v", sb.id, state.Extra["sandbox"])
	}
}

func TestSandboxMiddleware_Before_AcquireError(t *testing.T) {
	provider := &stubProvider{acqErr: errors.New("boom")}
	mw := New(provider)

	state := &middleware.State{ThreadID: "thread2"}
	err := mw.BeforeAgent(context.Background(), state)
	if err == nil || !errors.Is(err, provider.acqErr) {
		t.Errorf("expected acquire error, got %v", err)
	}
}

func TestSandboxMiddleware_Before_NilProvider(t *testing.T) {
	mw := New(nil)
	state := &middleware.State{ThreadID: "thread3"}
	if err := mw.BeforeAgent(context.Background(), state); err != nil {
		t.Errorf("expected no error with nil provider, got %v", err)
	}
}
