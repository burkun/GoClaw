package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeResolver struct {
	virtual string
	host    string
}

func (r *fakeResolver) Resolve(virtualPath string) (string, error) {
	if virtualPath != r.virtual {
		return "", os.ErrNotExist
	}
	return r.host, nil
}

func (r *fakeResolver) MaskHostPaths(output string) string {
	return strings.ReplaceAll(output, r.host, r.virtual)
}

func TestGrepToolExecute(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "a.txt")
	if err := os.WriteFile(file, []byte("hello\nworld\nhello go\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	tool := &GrepTool{Resolver: &fakeResolver{virtual: "/mnt/user-data/workspace", host: tmp}}
	out, err := tool.Execute(context.Background(), `{"description":"x","pattern":"hello","path":"/mnt/user-data/workspace","literal":true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Found 2 matches") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestGlobToolExecute(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "dir"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "dir", "a.go"), []byte("package x"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	tool := &GlobTool{Resolver: &fakeResolver{virtual: "/mnt/user-data/workspace", host: tmp}}
	out, err := tool.Execute(context.Background(), `{"description":"x","pattern":"**/*.go","path":"/mnt/user-data/workspace"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "a.go") {
		t.Fatalf("unexpected output: %s", out)
	}
}
