package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/tools"
)

func TestRegisterDefaultTools_RegistersCoreSearchTools(t *testing.T) {
	if err := RegisterDefaultTools(&config.AppConfig{}); err != nil {
		t.Fatalf("register default tools failed: %v", err)
	}
	for _, name := range []string{"read_file", "write_file", "glob", "grep", "bash", "ask_clarification"} {
		if _, ok := tools.Get(name); !ok {
			t.Fatalf("expected tool %q to be registered", name)
		}
	}
}

func TestRegisterDefaultTools_GlobAndGrepExecute(t *testing.T) {
	cwd, _ := os.Getwd()
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	workspace := filepath.Join(".goclaw", "threads", "default", "user-data", "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("hello\nworld\nhello\n"), 0o644); err != nil {
		t.Fatalf("write fixture failed: %v", err)
	}

	if err := RegisterDefaultTools(&config.AppConfig{}); err != nil {
		t.Fatalf("register default tools failed: %v", err)
	}

	globTool, _ := tools.Get("glob")
	globOut, err := globTool.Execute(context.Background(), `{"description":"test","pattern":"*.txt","path":"/mnt/user-data/workspace"}`)
	if err != nil {
		t.Fatalf("glob execute failed: %v", err)
	}
	if !strings.Contains(globOut, "a.txt") {
		t.Fatalf("expected glob output to contain a.txt, got: %s", globOut)
	}

	grepTool, _ := tools.Get("grep")
	grepOut, err := grepTool.Execute(context.Background(), `{"description":"test","pattern":"hello","path":"/mnt/user-data/workspace","literal":true}`)
	if err != nil {
		t.Fatalf("grep execute failed: %v", err)
	}
	if !strings.Contains(grepOut, "Found") || !strings.Contains(grepOut, "hello") {
		t.Fatalf("unexpected grep output: %s", grepOut)
	}
}
