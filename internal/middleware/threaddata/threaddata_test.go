package threaddata

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"goclaw/internal/middleware"
)

func TestThreadDataMiddleware_Before_CreatesDirectories(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{BaseDir: tmp}
	mw := New(cfg)

	state := &middleware.State{ThreadID: "thread-abc"}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	expected := []string{
		filepath.Join(tmp, "thread-abc", "user-data", "workspace"),
		filepath.Join(tmp, "thread-abc", "user-data", "uploads"),
		filepath.Join(tmp, "thread-abc", "user-data", "outputs"),
	}
	for _, d := range expected {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("expected directory %s to exist, got error: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", d)
		}
	}

	if state.Extra["workspace_path"] != expected[0] {
		t.Errorf("workspace_path mismatch: got %v", state.Extra["workspace_path"])
	}
}

func TestThreadDataMiddleware_Before_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{BaseDir: tmp}
	mw := New(cfg)

	state := &middleware.State{ThreadID: "thread-xyz"}
	_ = mw.BeforeModel(context.Background(), state)

	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Fatalf("second Before failed: %v", err)
	}
}
