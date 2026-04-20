package subagents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	lcTool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"goclaw/internal/agent/subagents/builtins"
	"goclaw/internal/config"
	"goclaw/internal/models"
	toolruntime "goclaw/internal/tools"
)

var agentWorkerLogger = slog.Default().With("component", "subagent-worker")

// AgentWorkerConfig holds configuration for the agent worker.
type AgentWorkerConfig struct {
	// MaxIterations is the default max iterations if not specified in TaskRequest.
	MaxIterations int
}

// AgentWorker executes subagent tasks with full agent loop support.
type AgentWorker struct {
	cfg AgentWorkerConfig
}

// NewAgentWorker creates a new AgentWorker.
func NewAgentWorker(cfg AgentWorkerConfig) *AgentWorker {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 50
	}
	return &AgentWorker{cfg: cfg}
}

// loggerWithTrace returns a logger with trace_id if available.
func loggerWithTrace(traceID string) *slog.Logger {
	if traceID == "" {
		return agentWorkerLogger
	}
	return agentWorkerLogger.With("trace_id", traceID)
}

// Execute runs the subagent task with tool calling loop.
func (w *AgentWorker) Execute(ctx context.Context, req TaskRequest) (string, error) {
	result, err := w.ExecuteWithMessages(ctx, req)
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

// ExecuteWithMessages runs the subagent task and returns AI messages.
func (w *AgentWorker) ExecuteWithMessages(ctx context.Context, req TaskRequest) (WorkerResult, error) {
	log := loggerWithTrace(req.TraceID)
	appCfg, err := config.GetAppConfig()
	if err != nil {
		return WorkerResult{Output: fallbackSubagentOutput(req)}, nil
	}

	// Resolve model name
	modelName := strings.TrimSpace(req.ModelName)
	if modelName == "" || strings.EqualFold(modelName, "inherit") {
		if dm := appCfg.DefaultModel(); dm != nil {
			modelName = dm.Name
		}
	}

	// Create chat model
	chatModel, err := models.CreateChatModel(ctx, appCfg, models.CreateRequest{ModelName: modelName})
	if err != nil {
		log.Warn("failed to create chat model, using fallback", "error", err)
		return WorkerResult{Output: fallbackSubagentOutput(req)}, nil
	}

	// Build tools for subagent
	tools, err := w.buildTools(ctx, appCfg, req)
	if err != nil {
		log.Warn("failed to build tools, using simple model call", "error", err)
		return w.simpleModelCallWithMessages(ctx, chatModel, req)
	}

	// Build system prompt
	systemPrompt := strings.TrimSpace(req.SystemPrompt)
	if systemPrompt == "" {
		typeCfg := builtins.GetEffectiveConfig(req.SubagentType, appCfg)
		systemPrompt = typeCfg.SystemPrompt
	}
	if systemPrompt == "" {
		systemPrompt = "You are a focused subagent. Solve the task directly and return concise actionable output."
	}

	// Determine max iterations
	maxIter := req.MaxTurns
	if maxIter <= 0 {
		maxIter = w.cfg.MaxIterations
	}

	// Check context before creating agent
	select {
	case <-ctx.Done():
		return WorkerResult{}, ctx.Err()
	default:
	}

	// Create subagent using adk
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "subagent_" + req.SubagentType,
		Description: "Subagent for " + req.SubagentType,
		Instruction: systemPrompt,
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		MaxIterations: maxIter,
	})
	if err != nil {
		log.Warn("failed to create subagent, using simple model call", "error", err)
		return w.simpleModelCallWithMessages(ctx, chatModel, req)
	}

	// Run the agent
	input := &adk.AgentInput{
		Messages: []adk.Message{
			schema.UserMessage(req.Prompt),
		},
		EnableStreaming: false,
	}

	opts := []adk.AgentRunOption{
		adk.WithSessionValues(map[string]any{
			"thread_id":      req.ThreadID,
			"is_subagent":    true,
			"workspace_path": req.WorkspacePath,
			"uploads_path":   req.UploadsPath,
			"outputs_path":   req.OutputsPath,
		}),
	}

	iter := agent.Run(ctx, input, opts...)
	return w.extractResultWithMessages(ctx, iter)
}

// extractResult extracts the final assistant text from an agent event iterator.
// It checks ctx.Done() on each iteration to support timeout cancellation.
// Deprecated: Use extractResultWithMessages for AI message collection.
func (w *AgentWorker) extractResult(ctx context.Context, iter *adk.AsyncIterator[*adk.AgentEvent]) (string, error) {
	result, err := w.extractResultWithMessages(ctx, iter)
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

// extractResultWithMessages extracts the final assistant text and AI messages from an agent event iterator.
// It checks ctx.Done() on each iteration to support timeout cancellation.
func (w *AgentWorker) extractResultWithMessages(ctx context.Context, iter *adk.AsyncIterator[*adk.AgentEvent]) (WorkerResult, error) {
	if iter == nil {
		return WorkerResult{Output: "No response generated"}, nil
	}

	final := ""
	aiMessages := make([]map[string]interface{}, 0)
	seenMsgIDs := make(map[string]bool) // Dedup by message ID

	for {
		// Check for context cancellation (timeout)
		select {
		case <-ctx.Done():
			return WorkerResult{}, ctx.Err()
		default:
		}

		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev == nil {
			continue
		}
		if ev.Err != nil {
			return WorkerResult{}, fmt.Errorf("subagent event error: %w", ev.Err)
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mv := ev.Output.MessageOutput
		if mv.Role != schema.Assistant || mv.Message == nil {
			continue
		}

		// Collect AI message
		msg := mv.Message
		msgDict := map[string]interface{}{
			"type":    "ai",
			"content": msg.Content,
		}
		if msg.Name != "" {
			msgDict["name"] = msg.Name
		}
		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]map[string]interface{}, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   tc.ID,
					"name": tc.Function.Name,
					"args": tc.Function.Arguments,
				})
			}
			msgDict["tool_calls"] = toolCalls
		}

		// Dedup by ID if available
		msgID, _ := msgDict["id"].(string)
		if msgID != "" && seenMsgIDs[msgID] {
			continue
		}
		if msgID != "" {
			seenMsgIDs[msgID] = true
		}

		aiMessages = append(aiMessages, msgDict)

		// Update final content
		content := strings.TrimSpace(msg.Content)
		if content != "" {
			final = content
		}
	}

	if strings.TrimSpace(final) == "" {
		return WorkerResult{Output: "No response generated", AIMessages: aiMessages}, nil
	}
	return WorkerResult{Output: extractTextContent(final), AIMessages: aiMessages}, nil
}

// simpleModelCall performs a single model call without tool loop.
func (w *AgentWorker) simpleModelCall(ctx context.Context, chatModel model.BaseChatModel, req TaskRequest) (string, error) {
	result, err := w.simpleModelCallWithMessages(ctx, chatModel, req)
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

// simpleModelCallWithMessages performs a single model call without tool loop and returns AI messages.
func (w *AgentWorker) simpleModelCallWithMessages(ctx context.Context, chatModel model.BaseChatModel, req TaskRequest) (WorkerResult, error) {
	systemPrompt := strings.TrimSpace(req.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = "You are a focused subagent. Solve the task directly and return concise actionable output."
	}

	resp, err := chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(req.Prompt),
	})
	if err != nil {
		return WorkerResult{}, err
	}

	out := strings.TrimSpace(resp.Content)
	if out == "" {
		return WorkerResult{Output: fallbackSubagentOutput(req)}, nil
	}

	// Create AI message dict
	aiMessage := map[string]interface{}{
		"type":    "ai",
		"content": out,
	}

	return WorkerResult{Output: out, AIMessages: []map[string]interface{}{aiMessage}}, nil
}

// buildTools creates tools available to the subagent.
func (w *AgentWorker) buildTools(ctx context.Context, appCfg *config.AppConfig, req TaskRequest) ([]lcTool.BaseTool, error) {
	// Get all default tools
	allTools := toolruntime.AdaptDefaultRegistryToEinoTools()

	// Filter tools
	allowedSet := make(map[string]bool)
	if len(req.AllowedTools) > 0 {
		for _, name := range req.AllowedTools {
			allowedSet[name] = true
		}
	}

	disallowedSet := make(map[string]bool)
	for _, name := range req.DisallowedTools {
		disallowedSet[name] = true
	}

	// Also disallow task tool to prevent nesting
	disallowedSet["task"] = true

	filtered := make([]lcTool.BaseTool, 0, len(allTools))
	for _, t := range allTools {
		info, err := t.Info(ctx)
		if err != nil {
			continue
		}

		// Check denylist first
		if disallowedSet[info.Name] {
			continue
		}

		// Check allowlist if specified
		if len(allowedSet) > 0 && !allowedSet[info.Name] {
			continue
		}

		filtered = append(filtered, t)
	}

	return filtered, nil
}

// extractTextContent extracts text from message content.
func extractTextContent(content string) string {
	// Try to parse as JSON array (multi-part content)
	var parts []map[string]interface{}
	if err := json.Unmarshal([]byte(content), &parts); err == nil {
		textParts := make([]string, 0, len(parts))
		for _, part := range parts {
			if text, ok := part["text"].(string); ok {
				textParts = append(textParts, text)
			}
		}
		if len(textParts) > 0 {
			return strings.Join(textParts, "\n")
		}
	}

	return content
}

// WorkerFunc returns a WorkerFunc that uses AgentWorker.
func (w *AgentWorker) WorkerFunc() WorkerFunc {
	return func(ctx context.Context, req TaskRequest) (string, error) {
		return w.Execute(ctx, req)
	}
}

// WorkerFuncWithMessages returns a WorkerFuncWithMessages that uses AgentWorker.
func (w *AgentWorker) WorkerFuncWithMessages() WorkerFuncWithMessages {
	return func(ctx context.Context, req TaskRequest) (WorkerResult, error) {
		return w.ExecuteWithMessages(ctx, req)
	}
}

// DefaultAgentWorker is the default agent worker instance.
var defaultAgentWorkerOnce sync.Once
var defaultAgentWorker *AgentWorker

// DefaultAgentWorker returns a shared AgentWorker instance.
func DefaultAgentWorker() *AgentWorker {
	defaultAgentWorkerOnce.Do(func() {
		defaultAgentWorker = NewAgentWorker(AgentWorkerConfig{})
	})
	return defaultAgentWorker
}

// AgentWorkerFunc returns a WorkerFunc using the default agent worker.
func AgentWorkerFunc() WorkerFunc {
	return DefaultAgentWorker().WorkerFunc()
}

// AgentWorkerFuncWithMessages returns a WorkerFuncWithMessages using the default agent worker.
func AgentWorkerFuncWithMessages() WorkerFuncWithMessages {
	return DefaultAgentWorker().WorkerFuncWithMessages()
}

// Ensure AgentWorker implements the pattern for use as WorkerFunc.
var _ WorkerFunc = (&AgentWorker{}).Execute
var _ WorkerFuncWithMessages = (&AgentWorker{}).ExecuteWithMessages
