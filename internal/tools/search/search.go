// Package search implements file-content and file-path search tools for
// GoClaw, mirroring DeerFlow's `grep` and `glob` sandbox tools.
//
// GrepTool searches for lines matching a pattern inside text files under a
// root directory. GlobTool finds files or directories matching a glob pattern.
//
// Both tools operate on virtual /mnt/user-data/ paths that are translated to
// host paths before execution.
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DefaultGlobMaxResults caps the number of paths returned by a single glob call.
const DefaultGlobMaxResults = 200

// DefaultGrepMaxResults caps the number of matching lines returned by grep.
const DefaultGrepMaxResults = 100

// GrepMatch is a single matching line returned by GrepTool.
type GrepMatch struct {
	// Path is the virtual path of the file containing the match.
	Path string
	// LineNumber is the 1-indexed line number of the match.
	LineNumber int
	// Line is the content of the matching line.
	Line string
}

// PathResolver translates virtual /mnt/user-data/* paths to host paths.
// Implementations are provided by the sandbox layer.
type PathResolver interface {
	// Resolve returns the host path for virtualPath.
	// Returns an error when virtualPath is not allowed or contains traversal.
	Resolve(virtualPath string) (string, error)
	// MaskHostPaths replaces host-side paths in output with virtual equivalents.
	MaskHostPaths(output string) string
}

// SandboxSearcher is the sandbox-layer search capability used by tools.
type SandboxSearcher interface {
	Glob(ctx context.Context, path string, pattern string, includeDirs bool, maxResults int) ([]string, bool, error)
	Grep(ctx context.Context, path string, pattern string, glob string, literal bool, caseSensitive bool, maxResults int) ([]GrepMatch, bool, error)
}

// ---------------------------------------------------------------------------
// GrepTool
// ---------------------------------------------------------------------------

// GrepTool searches file contents for lines matching a pattern.
// Implements tools.Tool.
type GrepTool struct {
	Resolver      PathResolver
	SandboxGetter func(ctx context.Context) (SandboxSearcher, error)
	MaxResults    int
}

type grepInput struct {
	// Description is the model's rationale for the search.
	Description string `json:"description"`
	// Pattern is a regex (or literal string when Literal=true) to search for.
	Pattern string `json:"pattern"`
	// Path is the virtual root directory to search under.
	Path string `json:"path"`
	// Glob is an optional file-name filter applied before searching (e.g. "**/*.go").
	Glob string `json:"glob,omitempty"`
	// Literal treats Pattern as a plain string, disabling regex interpretation.
	Literal bool `json:"literal,omitempty"`
	// CaseSensitive enables case-sensitive matching (default false).
	CaseSensitive bool `json:"case_sensitive,omitempty"`
	// MaxResults overrides the tool default (capped at 500).
	MaxResults int `json:"max_results,omitempty"`
}

func (t *GrepTool) Name() string { return "grep" }

func (t *GrepTool) Description() string {
	return `Search for matching lines inside text files under a root directory.
path must be an absolute virtual path under /mnt/user-data/.
Use glob to restrict searched files (e.g. "**/*.go"). Pattern supports RE2 regex by default.`
}

func (t *GrepTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["description", "pattern", "path"],
  "properties": {
    "description":    {"type": "string"},
    "pattern":        {"type": "string", "description": "Regex or literal string to search for."},
    "path":           {"type": "string", "description": "Virtual root directory to search under."},
    "glob":           {"type": "string", "description": "Optional file-name filter (e.g. **/*.go)."},
    "literal":        {"type": "boolean", "description": "Treat pattern as a literal string (default false)."},
    "case_sensitive": {"type": "boolean", "description": "Case-sensitive matching (default false)."},
    "max_results":    {"type": "integer", "description": "Max matching lines to return (default 100, max 500)."}
  }
}`)
}

// Execute searches files under path for lines matching pattern.
// This operation ALWAYS goes through the sandbox layer to maintain isolation.
func (t *GrepTool) Execute(ctx context.Context, input string) (string, error) {
	var in grepInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("grep: invalid input JSON: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" || strings.TrimSpace(in.Pattern) == "" {
		return "", fmt.Errorf("grep: path and pattern are required")
	}
	effectiveMax := clampMax(in.MaxResults, t.MaxResults, DefaultGrepMaxResults, 500)

	// ALWAYS use sandbox layer - no fallback to direct filesystem access.
	if t.SandboxGetter == nil {
		return "", fmt.Errorf("grep: sandbox getter is required for sandbox isolation")
	}

	sb, err := t.SandboxGetter(ctx)
	if err != nil {
		return "", fmt.Errorf("grep: failed to get sandbox: %w", err)
	}
	if sb == nil {
		return "", fmt.Errorf("grep: sandbox is not available")
	}

	matches, truncated, callErr := sb.Grep(ctx, in.Path, in.Pattern, in.Glob, in.Literal, in.CaseSensitive, effectiveMax)
	if callErr != nil {
		return fmt.Sprintf("Error: %v", callErr), nil
	}
	return formatGrepResults(in.Path, matches, truncated), nil
}

// formatGrepResults formats a slice of GrepMatch results for the model.
func formatGrepResults(root string, matches []GrepMatch, truncated bool) string {
	if len(matches) == 0 {
		return fmt.Sprintf("No matches found under %s", root)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d matches under %s", len(matches), root))
	for _, m := range matches {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("%s:%d: %s", m.Path, m.LineNumber, m.Line))
	}
	if truncated {
		b.WriteString("\n... results truncated")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// GlobTool
// ---------------------------------------------------------------------------

// GlobTool finds files and optionally directories matching a glob pattern
// under a root directory.
// Implements tools.Tool.
type GlobTool struct {
	Resolver      PathResolver
	SandboxGetter func(ctx context.Context) (SandboxSearcher, error)
	MaxResults    int
}

type globInput struct {
	// Description is the model's rationale.
	Description string `json:"description"`
	// Pattern is a glob pattern relative to path (e.g. "**/*.go").
	Pattern string `json:"pattern"`
	// Path is the virtual root directory to search under.
	Path string `json:"path"`
	// IncludeDirs includes matching directories in results (default false).
	IncludeDirs bool `json:"include_dirs,omitempty"`
	// MaxResults caps the number of returned paths (default 200, max 1000).
	MaxResults int `json:"max_results,omitempty"`
}

func (t *GlobTool) Name() string { return "glob" }

func (t *GlobTool) Description() string {
	return `Find files or directories that match a glob pattern under a root directory.
path must be an absolute virtual path under /mnt/user-data/.
pattern is relative to path (e.g. "**/*.go").`
}

func (t *GlobTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["description", "pattern", "path"],
  "properties": {
    "description":  {"type": "string"},
    "pattern":      {"type": "string", "description": "Glob pattern relative to path."},
    "path":         {"type": "string", "description": "Virtual root directory to search under."},
    "include_dirs": {"type": "boolean", "description": "Include directories in results (default false)."},
    "max_results":  {"type": "integer", "description": "Max paths to return (default 200, max 1000)."}
  }
}`)
}

// Execute walks the directory tree and collects paths matching the glob pattern.
// This operation ALWAYS goes through the sandbox layer to maintain isolation.
func (t *GlobTool) Execute(ctx context.Context, input string) (string, error) {
	var in globInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("glob: invalid input JSON: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" || strings.TrimSpace(in.Pattern) == "" {
		return "", fmt.Errorf("glob: path and pattern are required")
	}
	effectiveMax := clampMax(in.MaxResults, t.MaxResults, DefaultGlobMaxResults, 1000)

	// ALWAYS use sandbox layer - no fallback to direct filesystem access.
	if t.SandboxGetter == nil {
		return "", fmt.Errorf("glob: sandbox getter is required for sandbox isolation")
	}

	sb, err := t.SandboxGetter(ctx)
	if err != nil {
		return "", fmt.Errorf("glob: failed to get sandbox: %w", err)
	}
	if sb == nil {
		return "", fmt.Errorf("glob: sandbox is not available")
	}

	matches, truncated, callErr := sb.Glob(ctx, in.Path, in.Pattern, in.IncludeDirs, effectiveMax)
	if callErr != nil {
		return fmt.Sprintf("Error: %v", callErr), nil
	}
	sort.Strings(matches)
	return formatGlobResults(in.Path, matches, truncated), nil
}

// formatGlobResults formats a slice of matching paths for the model.
func formatGlobResults(root string, matches []string, truncated bool) string {
	if len(matches) == 0 {
		return fmt.Sprintf("No files matched under %s", root)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d paths under %s", len(matches), root))
	for _, p := range matches {
		b.WriteString("\n")
		b.WriteString(p)
	}
	if truncated {
		b.WriteString("\n... results truncated")
	}
	return b.String()
}

var errStopWalk = fmt.Errorf("stop walk")

func clampMax(input, configured, def, upper int) int {
	v := input
	if v <= 0 {
		v = configured
	}
	if v <= 0 {
		v = def
	}
	if v > upper {
		v = upper
	}
	return v
}

func newLineMatcher(in grepInput) (func(string) bool, error) {
	if in.Literal {
		needle := in.Pattern
		if in.CaseSensitive {
			return func(line string) bool { return strings.Contains(line, needle) }, nil
		}
		needle = strings.ToLower(needle)
		return func(line string) bool { return strings.Contains(strings.ToLower(line), needle) }, nil
	}

	pattern := in.Pattern
	if !in.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	r, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return func(line string) bool { return r.MatchString(line) }, nil
}

func matchPattern(relPath, pattern string) bool {
	rel := filepath.ToSlash(relPath)
	p := filepath.ToSlash(strings.TrimSpace(pattern))
	if p == "" {
		return true
	}
	if strings.Contains(p, "**") {
		re := globToRegexp(p)
		return re.MatchString(rel)
	}
	ok, err := filepath.Match(p, rel)
	if err != nil {
		return false
	}
	return ok
}

func globToRegexp(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '(', ')', '+', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteString("\\")
			b.WriteByte(ch)
		default:
			b.WriteByte(ch)
		}
	}
	b.WriteString("$")
	r, err := regexp.Compile(b.String())
	if err != nil {
		return regexp.MustCompile("^$")
	}
	return r
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
