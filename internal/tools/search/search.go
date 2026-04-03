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

// ---------------------------------------------------------------------------
// GrepTool
// ---------------------------------------------------------------------------

// GrepTool searches file contents for lines matching a pattern.
// Implements tools.Tool.
type GrepTool struct {
	Resolver   PathResolver
	MaxResults int
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
//
// TODO: implementation steps
//  1. json.Unmarshal input into grepInput.
//  2. Resolve effective max_results: clamp(in.MaxResults, default=100, upper=500).
//  3. Call t.Resolver.Resolve(in.Path) to get hostPath.
//  4. Build a list of candidate file paths under hostPath using filepath.WalkDir.
//     If in.Glob is set, filter with filepath.Match.
//  5. For each candidate, read the file and scan lines:
//     a. If in.Literal, use strings.Contains (or strings.EqualFold for case-insensitive).
//     b. Otherwise compile the regex once with regexp.Compile (or MustCompile).
//        Wrap in (?i) if !in.CaseSensitive.
//  6. Collect GrepMatch{Path: virtualPath, LineNumber: n, Line: line}.
//     Stop collecting when len(matches) >= effectiveMax (set truncated=true).
//  7. Call formatGrepResults(virtualPath, matches, truncated) and return.
func (t *GrepTool) Execute(_ context.Context, input string) (string, error) {
	var in grepInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("grep: invalid input JSON: %w", err)
	}

	// TODO: implement – see doc comment above.
	_ = in
	return "", fmt.Errorf("grep: not implemented")
}

// formatGrepResults formats a slice of GrepMatch results for the model.
//
// TODO: return "No matches found under <root>" when matches is empty.
// Otherwise return "Found N matches under <root>" followed by one
// "<path>:<line_number>: <line>" entry per match, with a truncation notice
// appended when truncated is true.
func formatGrepResults(root string, matches []GrepMatch, truncated bool) string {
	if len(matches) == 0 {
		return fmt.Sprintf("No matches found under %s", root)
	}
	// TODO: implement full formatted output.
	return fmt.Sprintf("Found %d matches under %s (formatting TODO)", len(matches), root)
}

// ---------------------------------------------------------------------------
// GlobTool
// ---------------------------------------------------------------------------

// GlobTool finds files and optionally directories matching a glob pattern
// under a root directory.
// Implements tools.Tool.
type GlobTool struct {
	Resolver   PathResolver
	MaxResults int
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
//
// TODO: implementation steps
//  1. json.Unmarshal input into globInput.
//  2. Resolve effective max_results: clamp(in.MaxResults, default=200, upper=1000).
//  3. Call t.Resolver.Resolve(in.Path) to get hostPath.
//  4. Use filepath.WalkDir(hostPath, ...) to traverse the tree.
//     a. For each entry: skip directories unless in.IncludeDirs.
//     b. Compute relPath = strings.TrimPrefix(entry.Path, hostPath+"/").
//     c. Match with filepath.Match(in.Pattern, relPath) — or use
//        github.com/bmatcuk/doublestar/v4 for `**` support.
//     d. If matched, replace hostPath prefix with the original virtual path
//        (t.Resolver.MaskHostPaths) and append to matches.
//     e. Stop when len(matches) >= effectiveMax (set truncated=true).
//  5. Call formatGlobResults(virtualPath, matches, truncated) and return.
func (t *GlobTool) Execute(_ context.Context, input string) (string, error) {
	var in globInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("glob: invalid input JSON: %w", err)
	}

	// TODO: implement – see doc comment above.
	_ = in
	return "", fmt.Errorf("glob: not implemented")
}

// formatGlobResults formats a slice of matching paths for the model.
//
// TODO: return "No files matched under <root>" when matches is empty.
// Otherwise return "Found N paths under <root>" with one path per line,
// and a truncation notice when truncated is true.
func formatGlobResults(root string, matches []string, truncated bool) string {
	if len(matches) == 0 {
		return fmt.Sprintf("No files matched under %s", root)
	}
	// TODO: implement full formatted output.
	return fmt.Sprintf("Found %d paths under %s (formatting TODO)", len(matches), root)
}

// Silence unused import warnings during skeleton phase.
var _ = filepath.Join
