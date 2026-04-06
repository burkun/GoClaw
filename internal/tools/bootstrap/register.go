package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"

	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/sandbox"
	"github.com/bookerbai/goclaw/internal/tools"
	"github.com/bookerbai/goclaw/internal/tools/builtin"
	fstools "github.com/bookerbai/goclaw/internal/tools/fs"
	"github.com/bookerbai/goclaw/internal/tools/media"
	"github.com/bookerbai/goclaw/internal/tools/search"
	"github.com/bookerbai/goclaw/internal/tools/shell"
	"github.com/bookerbai/goclaw/internal/tools/web"
)

// RegisterDefaultTools rebuilds the default tools registry using runtime-aware wrappers.
// For backward compatibility, it registers all tools including view_image.
// Use RegisterDefaultToolsWithModel for conditional registration based on model capabilities.
func RegisterDefaultTools(cfg *config.AppConfig) error {
	return RegisterDefaultToolsWithModel(cfg, nil)
}

// RegisterDefaultToolsWithModel rebuilds the default tools registry with model-aware tool selection.
// modelCfg is optional; if provided, tools like view_image are only registered when the model supports them.
func RegisterDefaultToolsWithModel(cfg *config.AppConfig, modelCfg *config.ModelConfig) error {
	tools.ResetDefaultRegistry()

	entries := []tools.Tool{
		&readFileRuntimeTool{cfg: cfg},
		&writeFileRuntimeTool{},
		&editFileRuntimeTool{},
		&listDirRuntimeTool{},
		&globRuntimeTool{cfg: cfg},
		&grepRuntimeTool{cfg: cfg},
		&bashRuntimeTool{cfg: cfg},
		web.NewWebSearchTool(web.WebToolConfig{
			TavilyAPIKey:     strings.TrimSpace(os.Getenv("TAVILY_API_KEY")),
			Timeout:          10 * time.Second,
			MaxSearchResults: toolIntExtra(cfg, "web_search", "max_results", 5),
		}),
		web.NewWebFetchTool(web.WebToolConfig{
			JinaAPIKey:    strings.TrimSpace(os.Getenv("JINA_API_KEY")),
			Timeout:       10 * time.Second,
			MaxFetchChars: 4096,
		}),
		web.NewImageSearchTool(web.WebToolConfig{
			Timeout:          10 * time.Second,
			MaxSearchResults: toolIntExtra(cfg, "image_search", "max_results", 5),
		}),
		&mediaPresentFileRuntimeTool{},
		builtin.NewClarificationTool(),
		builtin.NewToolSearchTool(builtin.DefaultDeferredToolRegistry()),
		builtin.NewTaskToolWithDefaults(), // Subagent delegation tool
		builtin.NewSetupAgentTool(""),     // Agent Creator tool (P2 fix)
	}

	// Only register view_image if the model supports vision
	if modelCfg == nil || modelCfg.SupportsVision {
		entries = append(entries, &mediaViewImageRuntimeTool{})
	}

	for _, t := range entries {
		if err := tools.Register(t); err != nil {
			return fmt.Errorf("register tool %s: %w", t.Name(), err)
		}
	}
	return nil
}

type runtimePathResolver struct {
	paths *fstools.PathMapping
}

func (r *runtimePathResolver) Resolve(virtualPath string) (string, error) {
	if r == nil || r.paths == nil {
		return "", fmt.Errorf("resolver not initialized")
	}
	return fstools.ResolveVirtualPath(virtualPath, r.paths)
}

func (r *runtimePathResolver) MaskHostPaths(output string) string {
	if r == nil || r.paths == nil {
		return output
	}
	result := output
	pairs := [][2]string{
		{r.paths.WorkspacePath, fstools.VirtualPathPrefix + "/workspace"},
		{r.paths.UploadsPath, fstools.VirtualPathPrefix + "/uploads"},
		{r.paths.OutputsPath, fstools.VirtualPathPrefix + "/outputs"},
	}
	for _, p := range pairs {
		if p[0] != "" {
			result = strings.ReplaceAll(result, p[0], p[1])
		}
	}
	return result
}

func threadPathsFromContext(ctx context.Context) *fstools.PathMapping {
	threadID := "default"
	if vals := adk.GetSessionValues(ctx); vals != nil {
		if v, ok := vals["thread_id"].(string); ok && strings.TrimSpace(v) != "" {
			threadID = strings.TrimSpace(v)
		}
	}
	base := filepath.Join(".goclaw", "threads", threadID, "user-data")
	return &fstools.PathMapping{
		ThreadID:      threadID,
		WorkspacePath: filepath.Join(base, "workspace"),
		UploadsPath:   filepath.Join(base, "uploads"),
		OutputsPath:   filepath.Join(base, "outputs"),
	}
}

type sandboxSearchAdapter struct {
	sb sandbox.Sandbox
}

func (a sandboxSearchAdapter) Glob(ctx context.Context, path string, pattern string, includeDirs bool, maxResults int) ([]string, bool, error) {
	if a.sb == nil {
		return nil, false, fmt.Errorf("sandbox is not available")
	}
	return a.sb.Glob(ctx, path, pattern, includeDirs, maxResults)
}

func (a sandboxSearchAdapter) Grep(ctx context.Context, path string, pattern string, glob string, literal bool, caseSensitive bool, maxResults int) ([]search.GrepMatch, bool, error) {
	if a.sb == nil {
		return nil, false, fmt.Errorf("sandbox is not available")
	}
	matches, truncated, err := a.sb.Grep(ctx, path, pattern, glob, literal, caseSensitive, maxResults)
	if err != nil {
		return nil, false, err
	}
	out := make([]search.GrepMatch, 0, len(matches))
	for _, m := range matches {
		out = append(out, search.GrepMatch{Path: m.Path, LineNumber: m.LineNumber, Line: m.Line})
	}
	return out, truncated, nil
}

func sandboxSearcherFromContext(ctx context.Context) (search.SandboxSearcher, error) {
	threadID := "default"
	if vals := adk.GetSessionValues(ctx); vals != nil {
		if v, ok := vals["thread_id"].(string); ok && strings.TrimSpace(v) != "" {
			threadID = strings.TrimSpace(v)
		}
	}
	provider := sandbox.DefaultProvider()
	if provider == nil {
		return nil, fmt.Errorf("sandbox provider not configured")
	}
	sandboxID, err := provider.Acquire(ctx, threadID)
	if err != nil {
		return nil, fmt.Errorf("acquire sandbox failed: %w", err)
	}
	sb := provider.Get(sandboxID)
	if sb == nil {
		return nil, fmt.Errorf("sandbox not found: %s", sandboxID)
	}
	return sandboxSearchAdapter{sb: sb}, nil
}

// read/write/edit/list wrappers

type readFileRuntimeTool struct {
	cfg *config.AppConfig
}

func (t *readFileRuntimeTool) Name() string        { return (&fstools.ReadFileTool{}).Name() }
func (t *readFileRuntimeTool) Description() string { return (&fstools.ReadFileTool{}).Description() }
func (t *readFileRuntimeTool) InputSchema() json.RawMessage {
	return (&fstools.ReadFileTool{}).InputSchema()
}
func (t *readFileRuntimeTool) Execute(ctx context.Context, input string) (string, error) {
	maxChars := fstools.DefaultReadFileMaxChars
	// Priority: tool config > sandbox config > default
	if tc := t.cfg.GetToolConfig("read_file"); tc != nil && tc.Extra != nil {
		if v, ok := tc.Extra["max_chars"]; ok {
			switch vv := v.(type) {
			case int:
				maxChars = vv
			case int64:
				maxChars = int(vv)
			case float64:
				maxChars = int(vv)
			case string:
				if n, err := strconv.Atoi(strings.TrimSpace(vv)); err == nil {
					maxChars = n
				}
			}
		}
	}
	// Fallback to sandbox config if tool config not set
	if maxChars == fstools.DefaultReadFileMaxChars && t.cfg.Sandbox.ReadFileOutputMaxChars > 0 {
		maxChars = t.cfg.Sandbox.ReadFileOutputMaxChars
	}
	inner := &fstools.ReadFileTool{
		Paths:    threadPathsFromContext(ctx),
		MaxChars: maxChars,
	}
	return inner.Execute(ctx, input)
}

type writeFileRuntimeTool struct{}

func (t *writeFileRuntimeTool) Name() string        { return (&fstools.WriteFileTool{}).Name() }
func (t *writeFileRuntimeTool) Description() string { return (&fstools.WriteFileTool{}).Description() }
func (t *writeFileRuntimeTool) InputSchema() json.RawMessage {
	return (&fstools.WriteFileTool{}).InputSchema()
}
func (t *writeFileRuntimeTool) Execute(ctx context.Context, input string) (string, error) {
	inner := &fstools.WriteFileTool{Paths: threadPathsFromContext(ctx)}
	return inner.Execute(ctx, input)
}

type editFileRuntimeTool struct{}

func (t *editFileRuntimeTool) Name() string        { return (&fstools.EditFileTool{}).Name() }
func (t *editFileRuntimeTool) Description() string { return (&fstools.EditFileTool{}).Description() }
func (t *editFileRuntimeTool) InputSchema() json.RawMessage {
	return (&fstools.EditFileTool{}).InputSchema()
}
func (t *editFileRuntimeTool) Execute(ctx context.Context, input string) (string, error) {
	inner := &fstools.EditFileTool{Paths: threadPathsFromContext(ctx)}
	return inner.Execute(ctx, input)
}

type listDirRuntimeTool struct{}

func (t *listDirRuntimeTool) Name() string        { return (&fstools.ListDirTool{}).Name() }
func (t *listDirRuntimeTool) Description() string { return (&fstools.ListDirTool{}).Description() }
func (t *listDirRuntimeTool) InputSchema() json.RawMessage {
	return (&fstools.ListDirTool{}).InputSchema()
}
func (t *listDirRuntimeTool) Execute(ctx context.Context, input string) (string, error) {
	inner := &fstools.ListDirTool{Paths: threadPathsFromContext(ctx)}
	return inner.Execute(ctx, input)
}

// search wrappers

type globRuntimeTool struct {
	cfg *config.AppConfig
}

func (t *globRuntimeTool) Name() string                 { return (&search.GlobTool{}).Name() }
func (t *globRuntimeTool) Description() string          { return (&search.GlobTool{}).Description() }
func (t *globRuntimeTool) InputSchema() json.RawMessage { return (&search.GlobTool{}).InputSchema() }
func (t *globRuntimeTool) Execute(ctx context.Context, input string) (string, error) {
	inner := &search.GlobTool{
		Resolver:      &runtimePathResolver{paths: threadPathsFromContext(ctx)},
		SandboxGetter: sandboxSearcherFromContext,
		MaxResults:    toolIntExtra(t.cfg, "glob", "max_results", 200),
	}
	return inner.Execute(ctx, input)
}

type grepRuntimeTool struct {
	cfg *config.AppConfig
}

func (t *grepRuntimeTool) Name() string                 { return (&search.GrepTool{}).Name() }
func (t *grepRuntimeTool) Description() string          { return (&search.GrepTool{}).Description() }
func (t *grepRuntimeTool) InputSchema() json.RawMessage { return (&search.GrepTool{}).InputSchema() }
func (t *grepRuntimeTool) Execute(ctx context.Context, input string) (string, error) {
	inner := &search.GrepTool{
		Resolver:      &runtimePathResolver{paths: threadPathsFromContext(ctx)},
		SandboxGetter: sandboxSearcherFromContext,
		MaxResults:    toolIntExtra(t.cfg, "grep", "max_results", 100),
	}
	return inner.Execute(ctx, input)
}

// media wrappers

type mediaViewImageRuntimeTool struct{}

func (t *mediaViewImageRuntimeTool) Name() string { return (&media.ViewImageTool{}).Name() }
func (t *mediaViewImageRuntimeTool) Description() string {
	return (&media.ViewImageTool{}).Description()
}
func (t *mediaViewImageRuntimeTool) InputSchema() json.RawMessage {
	return (&media.ViewImageTool{}).InputSchema()
}
func (t *mediaViewImageRuntimeTool) Execute(ctx context.Context, input string) (string, error) {
	inner := &media.ViewImageTool{Resolver: &runtimePathResolver{paths: threadPathsFromContext(ctx)}}
	return inner.Execute(ctx, input)
}

type mediaPresentFileRuntimeTool struct{}

func (t *mediaPresentFileRuntimeTool) Name() string { return (&media.PresentFileTool{}).Name() }
func (t *mediaPresentFileRuntimeTool) Description() string {
	return (&media.PresentFileTool{}).Description()
}
func (t *mediaPresentFileRuntimeTool) InputSchema() json.RawMessage {
	return (&media.PresentFileTool{}).InputSchema()
}
func (t *mediaPresentFileRuntimeTool) Execute(ctx context.Context, input string) (string, error) {
	inner := &media.PresentFileTool{Resolver: &runtimePathResolver{paths: threadPathsFromContext(ctx)}}
	return inner.Execute(ctx, input)
}

// bash wrapper

type bashRuntimeTool struct {
	cfg *config.AppConfig
}

func (t *bashRuntimeTool) Name() string { return shell.NewBashTool(shell.Config{}).Name() }
func (t *bashRuntimeTool) Description() string {
	return shell.NewBashTool(shell.Config{}).Description()
}
func (t *bashRuntimeTool) InputSchema() json.RawMessage {
	return shell.NewBashTool(shell.Config{}).InputSchema()
}
func (t *bashRuntimeTool) Execute(ctx context.Context, input string) (string, error) {
	paths := threadPathsFromContext(ctx)
	enabled := false
	maxOut := 20000
	if t.cfg != nil {
		enabled = t.cfg.Sandbox.AllowHostBash || strings.EqualFold(strings.TrimSpace(t.cfg.Sandbox.Use), "docker")
		if t.cfg.Sandbox.BashOutputMaxChars > 0 {
			maxOut = t.cfg.Sandbox.BashOutputMaxChars
		}
	}
	cfg := shell.Config{
		Enabled:        enabled,
		MaxOutputChars: maxOut,
		WorkspacePath:  paths.WorkspacePath,
		VirtualToHostReplacer: func(cmd string) (string, error) {
			return replaceVirtualPaths(cmd, paths), nil
		},
	}
	inner := shell.NewBashTool(cfg)
	return inner.Execute(ctx, input)
}

func replaceVirtualPaths(cmd string, paths *fstools.PathMapping) string {
	if paths == nil {
		return cmd
	}
	pairs := [][2]string{
		{fstools.VirtualPathPrefix + "/workspace", paths.WorkspacePath},
		{fstools.VirtualPathPrefix + "/uploads", paths.UploadsPath},
		{fstools.VirtualPathPrefix + "/outputs", paths.OutputsPath},
	}
	out := cmd
	for _, p := range pairs {
		out = strings.ReplaceAll(out, p[0], p[1])
	}
	return out
}

func toolIntExtra(cfg *config.AppConfig, toolName, key string, fallback int) int {
	if cfg == nil {
		return fallback
	}
	tc := cfg.GetToolConfig(toolName)
	if tc == nil || tc.Extra == nil {
		return fallback
	}
	v, ok := tc.Extra[key]
	if !ok {
		return fallback
	}
	switch vv := v.(type) {
	case int:
		return vv
	case int64:
		return int(vv)
	case float64:
		return int(vv)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(vv)); err == nil {
			return n
		}
	}
	return fallback
}
