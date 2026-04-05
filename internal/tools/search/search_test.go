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

type fakeSandboxSearcher struct {
	globResults []string
	globCut     bool
	grepResults []GrepMatch
	grepCut     bool
}

func (f *fakeSandboxSearcher) Glob(ctx context.Context, path string, pattern string, includeDirs bool, maxResults int) ([]string, bool, error) {
	_ = ctx
	_ = path
	_ = pattern
	_ = includeDirs
	_ = maxResults
	return append([]string(nil), f.globResults...), f.globCut, nil
}

func (f *fakeSandboxSearcher) Grep(ctx context.Context, path string, pattern string, glob string, literal bool, caseSensitive bool, maxResults int) ([]GrepMatch, bool, error) {
	_ = ctx
	_ = path
	_ = pattern
	_ = glob
	_ = literal
	_ = caseSensitive
	_ = maxResults
	return append([]GrepMatch(nil), f.grepResults...), f.grepCut, nil
}

func TestGrepToolExecute(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "a.txt")
	if err := os.WriteFile(file, []byte("hello\nworld\nhello go\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	tool := &GrepTool{
		Resolver: &fakeResolver{virtual: "/mnt/user-data/workspace", host: tmp},
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{grepResults: []GrepMatch{
				{Path: "/mnt/user-data/workspace/a.txt", LineNumber: 1, Line: "hello"},
				{Path: "/mnt/user-data/workspace/a.txt", LineNumber: 3, Line: "hello go"},
			}}, nil
		},
	}
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

	tool := &GlobTool{
		Resolver: &fakeResolver{virtual: "/mnt/user-data/workspace", host: tmp},
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{globResults: []string{"/mnt/user-data/workspace/dir/a.go"}}, nil
		},
	}
	out, err := tool.Execute(context.Background(), `{"description":"x","pattern":"**/*.go","path":"/mnt/user-data/workspace"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "a.go") {
		t.Fatalf("unexpected output: %s", out)
	}
}
