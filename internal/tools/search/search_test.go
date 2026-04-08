package search

import (
	"context"
	"encoding/json"
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
	globErr     error
	grepResults []GrepMatch
	grepCut     bool
	grepErr     error
}

func (f *fakeSandboxSearcher) Glob(ctx context.Context, path string, pattern string, includeDirs bool, maxResults int) ([]string, bool, error) {
	_ = ctx
	_ = path
	_ = pattern
	_ = includeDirs
	_ = maxResults
	if f.globErr != nil {
		return nil, false, f.globErr
	}
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
	if f.grepErr != nil {
		return nil, false, f.grepErr
	}
	return append([]GrepMatch(nil), f.grepResults...), f.grepCut, nil
}

// ---------------------------------------------------------------------------
// GrepTool method tests
// ---------------------------------------------------------------------------

func TestGrepTool_Name(t *testing.T) {
	tool := &GrepTool{}
	if tool.Name() != "grep" {
		t.Errorf("expected name 'grep', got %q", tool.Name())
	}
}

func TestGrepTool_Description(t *testing.T) {
	tool := &GrepTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestGrepTool_InputSchema(t *testing.T) {
	tool := &GrepTool{}
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("input schema is not valid JSON: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GlobTool method tests
// ---------------------------------------------------------------------------

func TestGlobTool_Name(t *testing.T) {
	tool := &GlobTool{}
	if tool.Name() != "glob" {
		t.Errorf("expected name 'glob', got %q", tool.Name())
	}
}

func TestGlobTool_Description(t *testing.T) {
	tool := &GlobTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestGlobTool_InputSchema(t *testing.T) {
	tool := &GlobTool{}
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("input schema is not valid JSON: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GrepTool Execute tests
// ---------------------------------------------------------------------------

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

func TestGrepTool_Execute_InvalidJSON(t *testing.T) {
	tool := &GrepTool{}
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestGrepTool_Execute_EmptyPath(t *testing.T) {
	tool := &GrepTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{}, nil
		},
	}
	_, err := tool.Execute(context.Background(), `{"description":"x","pattern":"test","path":""}`)
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestGrepTool_Execute_EmptyPattern(t *testing.T) {
	tool := &GrepTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{}, nil
		},
	}
	_, err := tool.Execute(context.Background(), `{"description":"x","pattern":"","path":"/mnt/user-data/workspace"}`)
	if err == nil {
		t.Error("expected error for empty pattern")
	}
}

func TestGrepTool_Execute_NilSandboxGetter(t *testing.T) {
	tool := &GrepTool{}
	_, err := tool.Execute(context.Background(), `{"description":"x","pattern":"test","path":"/mnt/user-data/workspace"}`)
	if err == nil {
		t.Error("expected error for nil sandbox getter")
	}
}

func TestGrepTool_Execute_SandboxGetterError(t *testing.T) {
	tool := &GrepTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return nil, os.ErrNotExist
		},
	}
	_, err := tool.Execute(context.Background(), `{"description":"x","pattern":"test","path":"/mnt/user-data/workspace"}`)
	if err == nil {
		t.Error("expected error from sandbox getter")
	}
}

func TestGrepTool_Execute_NilSandbox(t *testing.T) {
	tool := &GrepTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return nil, nil
		},
	}
	_, err := tool.Execute(context.Background(), `{"description":"x","pattern":"test","path":"/mnt/user-data/workspace"}`)
	if err == nil {
		t.Error("expected error for nil sandbox")
	}
}

func TestGrepTool_Execute_SandboxError(t *testing.T) {
	tool := &GrepTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{grepErr: os.ErrPermission}, nil
		},
	}
	out, err := tool.Execute(context.Background(), `{"description":"x","pattern":"test","path":"/mnt/user-data/workspace"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Error:") {
		t.Errorf("expected error output, got: %s", out)
	}
}

func TestGrepTool_Execute_NoMatches(t *testing.T) {
	tool := &GrepTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{grepResults: []GrepMatch{}}, nil
		},
	}
	out, err := tool.Execute(context.Background(), `{"description":"x","pattern":"test","path":"/mnt/user-data/workspace"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No matches found") {
		t.Errorf("expected no matches output, got: %s", out)
	}
}

func TestGrepTool_Execute_Truncated(t *testing.T) {
	tool := &GrepTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{
				grepResults: []GrepMatch{{Path: "a.txt", LineNumber: 1, Line: "test"}},
				grepCut:     true,
			}, nil
		},
	}
	out, err := tool.Execute(context.Background(), `{"description":"x","pattern":"test","path":"/mnt/user-data/workspace"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncated output, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// GlobTool Execute tests
// ---------------------------------------------------------------------------

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

func TestGlobTool_Execute_InvalidJSON(t *testing.T) {
	tool := &GlobTool{}
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestGlobTool_Execute_EmptyPath(t *testing.T) {
	tool := &GlobTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{}, nil
		},
	}
	_, err := tool.Execute(context.Background(), `{"description":"x","pattern":"*.go","path":""}`)
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestGlobTool_Execute_EmptyPattern(t *testing.T) {
	tool := &GlobTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{}, nil
		},
	}
	_, err := tool.Execute(context.Background(), `{"description":"x","pattern":"","path":"/mnt/user-data/workspace"}`)
	if err == nil {
		t.Error("expected error for empty pattern")
	}
}

func TestGlobTool_Execute_NilSandboxGetter(t *testing.T) {
	tool := &GlobTool{}
	_, err := tool.Execute(context.Background(), `{"description":"x","pattern":"*.go","path":"/mnt/user-data/workspace"}`)
	if err == nil {
		t.Error("expected error for nil sandbox getter")
	}
}

func TestGlobTool_Execute_SandboxGetterError(t *testing.T) {
	tool := &GlobTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return nil, os.ErrNotExist
		},
	}
	_, err := tool.Execute(context.Background(), `{"description":"x","pattern":"*.go","path":"/mnt/user-data/workspace"}`)
	if err == nil {
		t.Error("expected error from sandbox getter")
	}
}

func TestGlobTool_Execute_NilSandbox(t *testing.T) {
	tool := &GlobTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return nil, nil
		},
	}
	_, err := tool.Execute(context.Background(), `{"description":"x","pattern":"*.go","path":"/mnt/user-data/workspace"}`)
	if err == nil {
		t.Error("expected error for nil sandbox")
	}
}

func TestGlobTool_Execute_SandboxError(t *testing.T) {
	tool := &GlobTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{globErr: os.ErrPermission}, nil
		},
	}
	out, err := tool.Execute(context.Background(), `{"description":"x","pattern":"*.go","path":"/mnt/user-data/workspace"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Error:") {
		t.Errorf("expected error output, got: %s", out)
	}
}

func TestGlobTool_Execute_NoMatches(t *testing.T) {
	tool := &GlobTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{globResults: []string{}}, nil
		},
	}
	out, err := tool.Execute(context.Background(), `{"description":"x","pattern":"*.go","path":"/mnt/user-data/workspace"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No files matched") {
		t.Errorf("expected no files matched output, got: %s", out)
	}
}

func TestGlobTool_Execute_Truncated(t *testing.T) {
	tool := &GlobTool{
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{
				globResults: []string{"/mnt/user-data/workspace/a.go"},
				globCut:     true,
			}, nil
		},
	}
	out, err := tool.Execute(context.Background(), `{"description":"x","pattern":"*.go","path":"/mnt/user-data/workspace"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncated output, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestClampMax(t *testing.T) {
	tests := []struct {
		input, configured, def, upper, expected int
	}{
		{0, 0, 100, 500, 100},   // use default
		{0, 200, 100, 500, 200}, // use configured
		{300, 0, 100, 500, 300}, // use input
		{600, 0, 100, 500, 500}, // cap at upper
		{50, 200, 100, 500, 50}, // use input (under max)
	}

	for _, tt := range tests {
		got := clampMax(tt.input, tt.configured, tt.def, tt.upper)
		if got != tt.expected {
			t.Errorf("clampMax(%d, %d, %d, %d) = %d, want %d",
				tt.input, tt.configured, tt.def, tt.upper, got, tt.expected)
		}
	}
}

func TestFormatGrepResults(t *testing.T) {
	tests := []struct {
		name      string
		matches   []GrepMatch
		truncated bool
		want      string
	}{
		{
			name:      "no matches",
			matches:   nil,
			truncated: false,
			want:      "No matches found under /test",
		},
		{
			name: "one match",
			matches: []GrepMatch{
				{Path: "/test/a.txt", LineNumber: 1, Line: "hello"},
			},
			truncated: false,
			want:      "Found 1 matches under /test",
		},
		{
			name: "truncated",
			matches: []GrepMatch{
				{Path: "/test/a.txt", LineNumber: 1, Line: "hello"},
			},
			truncated: true,
			want:      "... results truncated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatGrepResults("/test", tt.matches, tt.truncated)
			if !strings.Contains(got, tt.want) {
				t.Errorf("formatGrepResults() = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestFormatGlobResults(t *testing.T) {
	tests := []struct {
		name      string
		matches   []string
		truncated bool
		want      string
	}{
		{
			name:      "no matches",
			matches:   nil,
			truncated: false,
			want:      "No files matched under /test",
		},
		{
			name:      "one match",
			matches:   []string{"/test/a.go"},
			truncated: false,
			want:      "Found 1 paths under /test",
		},
		{
			name:      "truncated",
			matches:   []string{"/test/a.go"},
			truncated: true,
			want:      "... results truncated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatGlobResults("/test", tt.matches, tt.truncated)
			if !strings.Contains(got, tt.want) {
				t.Errorf("formatGlobResults() = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestNewLineMatcher_Literal(t *testing.T) {
	in := grepInput{
		Pattern:       "hello",
		Literal:       true,
		CaseSensitive: true,
	}

	matcher, err := newLineMatcher(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !matcher("hello world") {
		t.Error("expected match for 'hello world'")
	}
	if matcher("HELLO WORLD") {
		t.Error("expected no match for case-sensitive 'HELLO WORLD'")
	}
}

func TestNewLineMatcher_LiteralCaseInsensitive(t *testing.T) {
	in := grepInput{
		Pattern:       "hello",
		Literal:       true,
		CaseSensitive: false,
	}

	matcher, err := newLineMatcher(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !matcher("hello world") {
		t.Error("expected match for 'hello world'")
	}
	if !matcher("HELLO WORLD") {
		t.Error("expected match for case-insensitive 'HELLO WORLD'")
	}
}

func TestNewLineMatcher_Regex(t *testing.T) {
	in := grepInput{
		Pattern:       "hel+o",
		Literal:       false,
		CaseSensitive: true,
	}

	matcher, err := newLineMatcher(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !matcher("hello world") {
		t.Error("expected match for 'hello world'")
	}
	if !matcher("helllo world") {
		t.Error("expected match for 'helllo world'")
	}
}

func TestNewLineMatcher_RegexCaseInsensitive(t *testing.T) {
	in := grepInput{
		Pattern:       "hello",
		Literal:       false,
		CaseSensitive: false,
	}

	matcher, err := newLineMatcher(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !matcher("HELLO WORLD") {
		t.Error("expected match for case-insensitive 'HELLO WORLD'")
	}
}

func TestNewLineMatcher_InvalidRegex(t *testing.T) {
	in := grepInput{
		Pattern: "[invalid",
		Literal: false,
	}

	_, err := newLineMatcher(in)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		relPath  string
		pattern  string
		expected bool
	}{
		{"file.go", "*.go", true},
		{"file.txt", "*.go", false},
		{"dir/file.go", "*.go", false}, // not recursive
		{"dir/file.go", "**/*.go", true},
		{"dir/subdir/file.go", "**/*.go", true},
		{"file.go", "", true}, // empty pattern matches all
		{"file_test.go", "*_test.go", true},
	}

	for _, tt := range tests {
		t.Run(tt.relPath+"_"+tt.pattern, func(t *testing.T) {
			got := matchPattern(tt.relPath, tt.pattern)
			if got != tt.expected {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.relPath, tt.pattern, got, tt.expected)
			}
		})
	}
}

func TestGlobToRegexp(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		match   bool
	}{
		{"*.go", "file.go", true},
		{"*.go", "file.txt", false},
		{"**/*.go", "dir/file.go", true},
		{"**/*.go", "dir/subdir/file.go", true},
		{"test/*.go", "test/file.go", true},
		{"test/*.go", "other/file.go", false},
		{"file?.go", "file1.go", true},
		{"file?.go", "file.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			re := globToRegexp(tt.pattern)
			if re.MatchString(tt.path) != tt.match {
				t.Errorf("globToRegexp(%q).MatchString(%q) = %v, want %v",
					tt.pattern, tt.path, re.MatchString(tt.path), tt.match)
			}
		})
	}
}

func TestMinInt(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{5, 5, 5},
		{0, 10, 0},
		{-1, 1, -1},
	}

	for _, tt := range tests {
		got := minInt(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("minInt(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.expected)
		}
	}
}

func TestGrepTool_Execute_WithMaxResults(t *testing.T) {
	tool := &GrepTool{
		MaxResults: 50,
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{grepResults: []GrepMatch{
				{Path: "a.txt", LineNumber: 1, Line: "test"},
			}}, nil
		},
	}

	// Test with custom max_results that should be capped
	out, err := tool.Execute(context.Background(), `{"description":"x","pattern":"test","path":"/mnt/user-data/workspace","max_results":600}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Found 1 matches") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestGlobTool_Execute_WithMaxResults(t *testing.T) {
	tool := &GlobTool{
		MaxResults: 100,
		SandboxGetter: func(ctx context.Context) (SandboxSearcher, error) {
			return &fakeSandboxSearcher{globResults: []string{"a.go"}}, nil
		},
	}

	out, err := tool.Execute(context.Background(), `{"description":"x","pattern":"*.go","path":"/mnt/user-data/workspace","max_results":1500}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Found 1 paths") {
		t.Errorf("unexpected output: %s", out)
	}
}
