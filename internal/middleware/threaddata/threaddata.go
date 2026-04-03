// Package threaddata implements ThreadDataMiddleware which ensures per-thread
// directory structure exists before the agent turn begins.
package threaddata

import (
	"context"
	"os"
	"path/filepath"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// Config holds configuration for ThreadDataMiddleware.
type Config struct {
	// BaseDir is the root directory under which per-thread directories are created.
	// Defaults to ".goclaw/threads" relative to current working directory.
	BaseDir string
}

// DefaultConfig returns reasonable defaults.
func DefaultConfig() Config {
	return Config{
		BaseDir: ".goclaw/threads",
	}
}

// ThreadDataMiddleware creates per-thread directory structure on Before hook.
// Directory layout:
//
//	{BaseDir}/{thread_id}/user-data/workspace/
//	{BaseDir}/{thread_id}/user-data/uploads/
//	{BaseDir}/{thread_id}/user-data/outputs/
type ThreadDataMiddleware struct {
	cfg Config
}

// New creates a ThreadDataMiddleware with the given config.
func New(cfg Config) *ThreadDataMiddleware {
	return &ThreadDataMiddleware{cfg: cfg}
}

// Name implements middleware.Middleware.
func (m *ThreadDataMiddleware) Name() string {
	return "ThreadDataMiddleware"
}

// Before ensures thread directories exist, then populates state.Extra with resolved paths.
func (m *ThreadDataMiddleware) Before(_ context.Context, state *middleware.State) error {
	threadDir := filepath.Join(m.cfg.BaseDir, state.ThreadID, "user-data")
	dirs := []string{
		filepath.Join(threadDir, "workspace"),
		filepath.Join(threadDir, "uploads"),
		filepath.Join(threadDir, "outputs"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	if state.Extra == nil {
		state.Extra = make(map[string]any)
	}
	state.Extra["thread_dir"] = threadDir
	state.Extra["workspace_path"] = dirs[0]
	state.Extra["uploads_path"] = dirs[1]
	state.Extra["outputs_path"] = dirs[2]
	return nil
}

// After is a no-op for ThreadDataMiddleware.
func (m *ThreadDataMiddleware) After(_ context.Context, _ *middleware.State, _ *middleware.Response) error {
	return nil
}

var _ middleware.Middleware = (*ThreadDataMiddleware)(nil)
