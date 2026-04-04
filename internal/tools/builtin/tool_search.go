package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ToolEntry represents a searchable tool in the registry.
type ToolEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Deferred    bool     `json:"deferred,omitempty"`
}

// ToolSearchTool searches the deferred tool registry by keywords.
type ToolSearchTool struct {
	registry []ToolEntry
}

// NewToolSearchTool creates a ToolSearchTool with the given registry.
func NewToolSearchTool(registry []ToolEntry) *ToolSearchTool {
	return &ToolSearchTool{registry: registry}
}

// Name returns the tool name.
func (t *ToolSearchTool) Name() string { return "tool_search" }

// Description returns the tool description.
func (t *ToolSearchTool) Description() string {
	return "Search for available tools by keyword. Returns matching tool names and descriptions."
}

// InputSchema returns the JSON schema for the tool input.
func (t *ToolSearchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Search query (keywords)"},
    "limit": {"type": "integer", "description": "Maximum number of results (default 10)"}
  },
  "required": ["query"]
}`)
}

type toolSearchInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// Execute runs the tool.
func (t *ToolSearchTool) Execute(ctx context.Context, input string) (string, error) {
	var in toolSearchInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("tool_search: invalid input: %w", err)
	}

	query := strings.TrimSpace(in.Query)
	if query == "" {
		return "", fmt.Errorf("tool_search: query is required")
	}

	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	// Search registry.
	matches := t.search(query, limit)

	result := map[string]any{
		"query":   query,
		"count":   len(matches),
		"results": matches,
	}

	out, _ := json.Marshal(result)
	return string(out), nil
}

func (t *ToolSearchTool) search(query string, limit int) []ToolEntry {
	queryLower := strings.ToLower(query)
	queryTerms := strings.Fields(queryLower)

	type scored struct {
		entry ToolEntry
		score int
	}
	var results []scored

	for _, entry := range t.registry {
		score := 0
		nameLower := strings.ToLower(entry.Name)
		descLower := strings.ToLower(entry.Description)

		for _, term := range queryTerms {
			// Exact name match.
			if nameLower == term {
				score += 100
			}
			// Name contains term.
			if strings.Contains(nameLower, term) {
				score += 50
			}
			// Description contains term.
			if strings.Contains(descLower, term) {
				score += 20
			}
			// Keywords match.
			for _, kw := range entry.Keywords {
				if strings.Contains(strings.ToLower(kw), term) {
					score += 30
				}
			}
			// Category match.
			if strings.Contains(strings.ToLower(entry.Category), term) {
				score += 15
			}
		}

		if score > 0 {
			results = append(results, scored{entry: entry, score: score})
		}
	}

	// Sort by score descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// Apply limit.
	if len(results) > limit {
		results = results[:limit]
	}

	// Extract entries.
	matches := make([]ToolEntry, len(results))
	for i, r := range results {
		matches[i] = r.entry
	}

	return matches
}

// AddTool adds a tool entry to the registry.
func (t *ToolSearchTool) AddTool(entry ToolEntry) {
	t.registry = append(t.registry, entry)
}

// SetRegistry replaces the tool registry.
func (t *ToolSearchTool) SetRegistry(registry []ToolEntry) {
	t.registry = registry
}

// DefaultDeferredToolRegistry returns the standard deferred tools.
func DefaultDeferredToolRegistry() []ToolEntry {
	return []ToolEntry{
		{Name: "ImageGen", Description: "Generate images from text descriptions using AI models", Category: "media", Keywords: []string{"image", "generate", "create", "picture"}, Deferred: true},
		{Name: "ImageEdit", Description: "Edit or modify an existing image using AI models", Category: "media", Keywords: []string{"image", "edit", "modify", "transform"}, Deferred: true},
		{Name: "NotebookEdit", Description: "Edit Jupyter notebook cells", Category: "notebook", Keywords: []string{"jupyter", "notebook", "cell", "ipynb"}, Deferred: true},
		{Name: "LSP", Description: "Interact with Language Server Protocol for code intelligence", Category: "code", Keywords: []string{"lsp", "language", "server", "completion", "hover"}, Deferred: true},
		{Name: "CronCreate", Description: "Schedule a prompt to run at a future time", Category: "scheduling", Keywords: []string{"cron", "schedule", "timer", "recurring"}, Deferred: true},
		{Name: "CronDelete", Description: "Cancel a scheduled cron job", Category: "scheduling", Keywords: []string{"cron", "cancel", "delete", "remove"}, Deferred: true},
		{Name: "CronList", Description: "List all scheduled cron jobs", Category: "scheduling", Keywords: []string{"cron", "list", "jobs", "scheduled"}, Deferred: true},
		{Name: "EnterWorktree", Description: "Create an isolated git worktree", Category: "git", Keywords: []string{"git", "worktree", "branch", "isolate"}, Deferred: true},
		{Name: "LeaveWorktree", Description: "Leave the current worktree session", Category: "git", Keywords: []string{"git", "worktree", "exit", "leave"}, Deferred: true},
		{Name: "TeamCreate", Description: "Create a new team for multi-agent coordination", Category: "team", Keywords: []string{"team", "agent", "swarm", "create"}, Deferred: true},
		{Name: "TeamDelete", Description: "Remove a team and its task directories", Category: "team", Keywords: []string{"team", "delete", "remove", "cleanup"}, Deferred: true},
	}
}
