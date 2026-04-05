package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	lcTool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/bookerbai/goclaw/internal/agent/subagents"
	"github.com/bookerbai/goclaw/internal/agentconfig"
	"github.com/bookerbai/goclaw/internal/config"
	einoruntime "github.com/bookerbai/goclaw/internal/eino"
	basemw "github.com/bookerbai/goclaw/internal/middleware"
	auditmw "github.com/bookerbai/goclaw/internal/middleware/audit"
	clarificationmw "github.com/bookerbai/goclaw/internal/middleware/clarification"
	danglingmw "github.com/bookerbai/goclaw/internal/middleware/dangling"
	deferredtoolmw "github.com/bookerbai/goclaw/internal/middleware/deferredtool"
	guardrailmw "github.com/bookerbai/goclaw/internal/middleware/guardrail"
	llmerrormw "github.com/bookerbai/goclaw/internal/middleware/llmerror"
	loopmw "github.com/bookerbai/goclaw/internal/middleware/loop"
	memorymw "github.com/bookerbai/goclaw/internal/middleware/memory"
	sandboxmw "github.com/bookerbai/goclaw/internal/middleware/sandboxmw"
	subagentlimitmw "github.com/bookerbai/goclaw/internal/middleware/subagentlimit"
	summarizemw "github.com/bookerbai/goclaw/internal/middleware/summarize"
	threaddatamw "github.com/bookerbai/goclaw/internal/middleware/threaddata"
	titlemw "github.com/bookerbai/goclaw/internal/middleware/title"
	todomw "github.com/bookerbai/goclaw/internal/middleware/todo"
	tokenusagemw "github.com/bookerbai/goclaw/internal/middleware/tokenusage"
	toolerrormw "github.com/bookerbai/goclaw/internal/middleware/toolerror"
	uploadsmw "github.com/bookerbai/goclaw/internal/middleware/uploads"
	viewimagemw "github.com/bookerbai/goclaw/internal/middleware/viewimage"
	"github.com/bookerbai/goclaw/internal/models"
	"github.com/bookerbai/goclaw/internal/sandbox"
	dockersandbox "github.com/bookerbai/goclaw/internal/sandbox/docker"
	localsandbox "github.com/bookerbai/goclaw/internal/sandbox/local"
	skillsruntime "github.com/bookerbai/goclaw/internal/skills"
	toolruntime "github.com/bookerbai/goclaw/internal/tools"
	toolbootstrap "github.com/bookerbai/goclaw/internal/tools/bootstrap"
)

type UploadedFile struct {
	Name        string `json:"name"`
	VirtualPath string `json:"virtual_path"`
	MIMEType    string `json:"mime_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
}

type ViewedImageData struct {
	Base64   string `json:"base64"`
	MIMEType string `json:"mime_type"`
}

type ThreadDataState struct {
	WorkspacePath string `json:"workspace_path,omitempty"`
	UploadsPath   string `json:"uploads_path,omitempty"`
	OutputsPath   string `json:"outputs_path,omitempty"`
}

type SandboxState struct {
	SandboxID string `json:"sandbox_id,omitempty"`
}

type ThreadState struct {
	Messages      []*schema.Message          `json:"messages"`
	Sandbox       *SandboxState              `json:"sandbox,omitempty"`
	ThreadData    *ThreadDataState           `json:"thread_data,omitempty"`
	Title         string                     `json:"title,omitempty"`
	Artifacts     []string                   `json:"artifacts,omitempty"`
	Todos         []map[string]any           `json:"todos,omitempty"`
	UploadedFiles []UploadedFile             `json:"uploaded_files,omitempty"`
	ViewedImages  map[string]ViewedImageData `json:"viewed_images,omitempty"`
}

type RunConfig struct {
	ThreadID               string
	ModelName              string
	ThinkingEnabled        bool
	IsPlanMode             bool
	SubagentEnabled        bool
	MaxConcurrentSubagents int
	CheckpointID           string
	AgentName              string
	// RunID is set by the gateway and used to tag events for consistent tracking.
	RunID string
}

type LeadAgent interface {
	Run(ctx context.Context, state *ThreadState, cfg RunConfig) (<-chan Event, error)
	Resume(ctx context.Context, state *ThreadState, cfg RunConfig, checkpointID string) (<-chan Event, error)
}

type leadAgent struct {
	einoAgent   adk.Agent
	tools       []lcTool.BaseTool
	middlewares []adk.ChatModelAgentMiddleware
	runner      *einoruntime.Runner
	skills      *skillsruntime.Registry
}

var getAppConfig = config.GetAppConfig
var registerDefaultTools = toolbootstrap.RegisterDefaultToolsWithModel
var invalidateMCPConfigCache = toolruntime.InvalidateMCPConfigCache

const middlewareStateSessionKey = "_goclaw_middleware_state"

func New(ctx context.Context) (*leadAgent, error) {
	appCfg, err := config.GetAppConfig()
	if err != nil {
		return nil, fmt.Errorf("agent.New: load config failed: %w", err)
	}

	skillLoader := skillsruntime.NewLoader()
	loadedSkills, err := skillLoader.Load(appCfg.Skills.Path, appCfg.Extensions)
	if err != nil {
		return nil, fmt.Errorf("agent.New: load skills failed: %w", err)
	}
	skillRegistry := skillsruntime.NewRegistry()
	for _, skill := range loadedSkills {
		if err := skillRegistry.Register(skill); err != nil {
			return nil, fmt.Errorf("agent.New: register skills failed: %w", err)
		}
	}
	if err := skillRegistry.OnLoad(ctx, appCfg); err != nil {
		return nil, fmt.Errorf("agent.New: skills on_load failed: %w", err)
	}

	req := models.CreateRequest{}
	var defaultModelCfg *config.ModelConfig
	if dm := appCfg.DefaultModel(); dm != nil {
		req.ModelName = dm.Name
		defaultModelCfg = dm
	}

	chatModel, err := models.CreateChatModel(ctx, appCfg, req)
	if err != nil {
		return nil, fmt.Errorf("agent.New: create model failed: %w", err)
	}

	if err := toolbootstrap.RegisterDefaultToolsWithModel(appCfg, defaultModelCfg); err != nil {
		return nil, fmt.Errorf("agent.New: register default tools failed: %w", err)
	}

	tools := toolruntime.AdaptDefaultRegistryToEinoTools()

	// Phase: Discover and register individual MCP tools from configured servers.
	// This replaces the old proxy-style MCP tools with proper per-tool registration.
	discoveredMCPTools := toolruntime.BuildDiscoveredMCPTools(appCfg)
	for _, mcpTool := range discoveredMCPTools {
		tools = append(tools, toolruntime.AdaptToEinoTool(mcpTool))
	}

	// Fallback: if discovery fails or returns empty, use proxy-style MCP tools.
	if len(discoveredMCPTools) == 0 {
		for _, mcpTool := range toolruntime.BuildMCPDynamicTools(appCfg) {
			tools = append(tools, toolruntime.AdaptToEinoTool(mcpTool))
		}
	}

	// Phase7B: add subagent task tool with bounded executor.
	subagentTimeout := 900 * time.Second
	if appCfg.Subagents.TimeoutSeconds > 0 {
		subagentTimeout = time.Duration(appCfg.Subagents.TimeoutSeconds) * time.Second
	}
	maxConcurrentSubagents := 3
	if appCfg.Subagents.MaxConcurrent > 0 {
		maxConcurrentSubagents = appCfg.Subagents.MaxConcurrent
	}
	subagentExec := subagents.NewExecutor(subagents.ExecutorConfig{
		MaxConcurrent:  maxConcurrentSubagents,
		DefaultTimeout: subagentTimeout,
	})
	taskTool := subagents.NewTaskTool(subagents.TaskToolConfig{
		Executor: subagentExec,
	})
	tools = append(tools, toolruntime.AdaptToEinoTool(taskTool))

	allowedTools := skillRegistry.AllowedToolSet()
	if len(allowedTools) > 0 {
		tools, err = filterToolsByAllowed(ctx, tools, allowedTools)
		if err != nil {
			return nil, fmt.Errorf("agent.New: apply skills allowed-tools failed: %w", err)
		}
	}

	mws := buildMiddlewares(RunConfig{})

	// Build system prompt with skills (P0 fix)
	instruction := buildSystemPrompt("", skillRegistry)

	a, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "lead_agent",
		Description: "GoClaw lead agent",
		Instruction: instruction,
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		MaxIterations: 100,
		Handlers:      mws,
	})
	if err != nil {
		return nil, fmt.Errorf("agent.New: build chat model agent failed: %w", err)
	}

	checkpointStore, err := newCheckPointStore(appCfg)
	if err != nil {
		return nil, err
	}

	r, err := einoruntime.NewRunner(ctx, einoruntime.RunnerConfig{
		Agent:           a,
		EnableStreaming: true,
		CheckPointStore: checkpointStore,
	})
	if err != nil {
		return nil, fmt.Errorf("agent.New: create runner failed: %w", err)
	}

	return &leadAgent{einoAgent: a, tools: tools, middlewares: mws, runner: r, skills: skillRegistry}, nil
}

// NewWithName creates a lead agent with per-agent configuration (P0 fix).
// If agentName is empty, it behaves like New() with default configuration.
// If agentName is provided, it loads per-agent config, SOUL.md, and applies skills/tool filtering.
func NewWithName(ctx context.Context, agentName string) (*leadAgent, error) {
	appCfg, err := config.GetAppConfig()
	if err != nil {
		return nil, fmt.Errorf("agent.NewWithName: load config failed: %w", err)
	}

	// Load per-agent configuration if agentName is specified
	var agentSoul string
	var agentSkills []string
	var agentToolGroups []string
	agentLoader := agentconfig.DefaultLoader

	if agentName != "" {
		if agentLoader.AgentExists(agentName) {
			agentCfg, err := agentLoader.LoadConfig(agentName)
			if err != nil {
				log.Printf("[WARN] failed to load agent config for %s: %v, using default", agentName, err)
			} else {
				// Override model if specified
				if agentCfg.Model != "" {
					log.Printf("[INFO] agent %s using model: %s", agentName, agentCfg.Model)
				}

				// Store skills and tool groups for filtering
				agentSkills = agentCfg.Skills
				agentToolGroups = agentCfg.ToolGroups
			}

			// Load SOUL.md
			if soul, err := agentLoader.LoadSoul(agentName); err == nil {
				agentSoul = soul
				log.Printf("[INFO] loaded SOUL.md for agent %s (%d bytes)", agentName, len(soul))
			}
		} else {
			log.Printf("[WARN] agent %s not found in file system, using default config", agentName)
		}
	}

	skillLoader := skillsruntime.NewLoader()
	loadedSkills, err := skillLoader.Load(appCfg.Skills.Path, appCfg.Extensions)
	if err != nil {
		return nil, fmt.Errorf("agent.NewWithName: load skills failed: %w", err)
	}
	skillRegistry := skillsruntime.NewRegistry()
	for _, skill := range loadedSkills {
		if err := skillRegistry.Register(skill); err != nil {
			return nil, fmt.Errorf("agent.NewWithName: register skills failed: %w", err)
		}
	}
	if err := skillRegistry.OnLoad(ctx, appCfg); err != nil {
		return nil, fmt.Errorf("agent.NewWithName: skills on_load failed: %w", err)
	}

	// Determine model to use
	req := models.CreateRequest{}
	var defaultModelCfg *config.ModelConfig

	// If agent has a model override, use it
	if agentName != "" && agentLoader.AgentExists(agentName) {
		agentCfg, _ := agentLoader.LoadConfig(agentName)
		if agentCfg != nil && agentCfg.Model != "" {
			req.ModelName = agentCfg.Model
			defaultModelCfg = appCfg.GetModelConfig(agentCfg.Model)
		}
	}

	// Fallback to default model
	if defaultModelCfg == nil {
		if dm := appCfg.DefaultModel(); dm != nil {
			req.ModelName = dm.Name
			defaultModelCfg = dm
		}
	}

	chatModel, err := models.CreateChatModel(ctx, appCfg, req)
	if err != nil {
		return nil, fmt.Errorf("agent.NewWithName: create model failed: %w", err)
	}

	if err := toolbootstrap.RegisterDefaultToolsWithModel(appCfg, defaultModelCfg); err != nil {
		return nil, fmt.Errorf("agent.NewWithName: register default tools failed: %w", err)
	}

	tools := toolruntime.AdaptDefaultRegistryToEinoTools()
	for _, mcpTool := range toolruntime.BuildMCPDynamicTools(appCfg) {
		tools = append(tools, toolruntime.AdaptToEinoTool(mcpTool))
	}

	// Phase7B: add subagent task tool with bounded executor.
	subagentTimeout := 900 * time.Second
	if appCfg.Subagents.TimeoutSeconds > 0 {
		subagentTimeout = time.Duration(appCfg.Subagents.TimeoutSeconds) * time.Second
	}
	maxConcurrentSubagents := 3
	if appCfg.Subagents.MaxConcurrent > 0 {
		maxConcurrentSubagents = appCfg.Subagents.MaxConcurrent
	}
	subagentExec := subagents.NewExecutor(subagents.ExecutorConfig{
		MaxConcurrent:  maxConcurrentSubagents,
		DefaultTimeout: subagentTimeout,
	})
	taskTool := subagents.NewTaskTool(subagents.TaskToolConfig{
		Executor: subagentExec,
	})
	tools = append(tools, toolruntime.AdaptToEinoTool(taskTool))

	// Apply skills filtering (P0 fix)
	// Priority: per-agent skills > global skills
	allowedTools := skillRegistry.AllowedToolSet()
	if len(agentSkills) > 0 {
		// Filter by per-agent skills
		allowedTools = filterSkillsByName(loadedSkills, agentSkills)
		log.Printf("[INFO] agent %s: filtered to %d skills: %v", agentName, len(agentSkills), agentSkills)
	}
	if len(allowedTools) > 0 {
		tools, err = filterToolsByAllowed(ctx, tools, allowedTools)
		if err != nil {
			return nil, fmt.Errorf("agent.NewWithName: apply skills allowed-tools failed: %w", err)
		}
	}

	// Apply tool groups filtering (P1 fix)
	if len(agentToolGroups) > 0 {
		tools, err = filterToolsByToolGroups(ctx, tools, agentToolGroups, appCfg)
		if err != nil {
			return nil, fmt.Errorf("agent.NewWithName: apply tool groups failed: %w", err)
		}
		log.Printf("[INFO] agent %s: filtered to %d tool groups: %v", agentName, len(agentToolGroups), agentToolGroups)
	}

	mws := buildMiddlewares(RunConfig{AgentName: agentName})

	// Build system prompt with SOUL.md and skills (P0 fix)
	instruction := buildSystemPrompt(agentSoul, skillRegistry)

	a, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "lead_agent",
		Description: fmt.Sprintf("GoClaw lead agent (%s)", agentName),
		Instruction: instruction,
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		MaxIterations: 100,
		Handlers:      mws,
	})
	if err != nil {
		return nil, fmt.Errorf("agent.NewWithName: build chat model agent failed: %w", err)
	}

	checkpointStore, err := newCheckPointStore(appCfg)
	if err != nil {
		return nil, err
	}

	r, err := einoruntime.NewRunner(ctx, einoruntime.RunnerConfig{
		Agent:           a,
		EnableStreaming: true,
		CheckPointStore: checkpointStore,
	})
	if err != nil {
		return nil, fmt.Errorf("agent.NewWithName: create runner failed: %w", err)
	}

	return &leadAgent{einoAgent: a, tools: tools, middlewares: mws, runner: r, skills: skillRegistry}, nil
}

func (a *leadAgent) Run(ctx context.Context, state *ThreadState, cfg RunConfig) (<-chan Event, error) {
	if cfg.ThreadID == "" {
		return nil, fmt.Errorf("thread_id is required")
	}

	ch := make(chan Event, 32)
	if a == nil || a.runner == nil {
		go func() {
			defer close(ch)
			ch <- Event{Type: EventError, ThreadID: cfg.ThreadID, Payload: ErrorPayload{Code: ErrorCodeNotInitialized, Message: "lead agent is not initialized"}, Timestamp: timeUnixMilli()}
		}()
		return ch, nil
	}

	if state == nil {
		state = &ThreadState{}
	}
	if err := a.syncSkillsOnConfigReload(); err != nil {
		return nil, fmt.Errorf("sync skills config failed: %w", err)
	}

	messages := prepareRunMessages(state.Messages, cfg)

	// Build session values for subagent state passing
	sessionValues := map[string]any{
		"thread_id":                cfg.ThreadID,
		"plan_mode":                cfg.IsPlanMode,
		"subagent_enabled":         cfg.SubagentEnabled,
		"max_concurrent_subagents": cfg.MaxConcurrentSubagents,
		"uploaded_files":           state.UploadedFiles,
		"viewed_images":            state.ViewedImages,
		"agent_name":               cfg.AgentName,
		"is_subagent":              strings.TrimSpace(cfg.AgentName) != "",
	}
	// Pass thread data paths for subagent access
	if state.ThreadData != nil {
		sessionValues["workspace_path"] = state.ThreadData.WorkspacePath
		sessionValues["uploads_path"] = state.ThreadData.UploadsPath
		sessionValues["outputs_path"] = state.ThreadData.OutputsPath
	}

	opts := []adk.AgentRunOption{adk.WithSessionValues(sessionValues)}
	if strings.TrimSpace(cfg.CheckpointID) != "" {
		opts = append(opts, adk.WithCheckPointID(cfg.CheckpointID))
	}
	stream := a.runner.Run(ctx, messages, opts...)

	go func() {
		defer close(ch)
		drainIter(ctx, stream, cfg.ThreadID, cfg.RunID, ch)
	}()
	return ch, nil
}

func (a *leadAgent) Resume(ctx context.Context, state *ThreadState, cfg RunConfig, checkpointID string) (<-chan Event, error) {
	if cfg.ThreadID == "" {
		return nil, fmt.Errorf("thread_id is required")
	}
	if strings.TrimSpace(checkpointID) == "" {
		return nil, fmt.Errorf("checkpoint_id is required")
	}

	ch := make(chan Event, 32)
	if a == nil || a.runner == nil {
		go func() {
			defer close(ch)
			ch <- Event{Type: EventError, ThreadID: cfg.ThreadID, Payload: ErrorPayload{Code: ErrorCodeNotInitialized, Message: "lead agent is not initialized"}, Timestamp: timeUnixMilli()}
		}()
		return ch, nil
	}

	if err := a.syncSkillsOnConfigReload(); err != nil {
		return nil, fmt.Errorf("sync skills config failed: %w", err)
	}

	if state == nil {
		state = &ThreadState{}
	}
	stream, err := a.runner.Resume(ctx, checkpointID, adk.WithSessionValues(map[string]any{
		"thread_id":                cfg.ThreadID,
		"plan_mode":                cfg.IsPlanMode,
		"subagent_enabled":         cfg.SubagentEnabled,
		"max_concurrent_subagents": cfg.MaxConcurrentSubagents,
		"uploaded_files":           state.UploadedFiles,
		"viewed_images":            state.ViewedImages,
		"agent_name":               cfg.AgentName,
		"is_subagent":              strings.TrimSpace(cfg.AgentName) != "",
	}))
	if err != nil {
		return nil, fmt.Errorf("resume from checkpoint failed: %w", err)
	}

	go func() {
		defer close(ch)
		drainIter(ctx, stream, cfg.ThreadID, cfg.RunID, ch)
	}()
	return ch, nil
}

func filterToolsByAllowed(ctx context.Context, tools []lcTool.BaseTool, allowed map[string]struct{}) ([]lcTool.BaseTool, error) {
	if len(allowed) == 0 {
		return tools, nil
	}
	out := make([]lcTool.BaseTool, 0, len(tools))
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read tool info failed: %w", err)
		}
		if _, ok := allowed[info.Name]; ok {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no tools matched skills allowed-tools")
	}
	return out, nil
}

func (a *leadAgent) syncSkillsOnConfigReload() error {
	if a == nil {
		return nil
	}
	cfg, err := getAppConfig()
	if err != nil {
		return err
	}
	modelCfg := cfg.DefaultModel()
	if err := registerDefaultTools(cfg, modelCfg); err != nil {
		return fmt.Errorf("reload tools failed: %w", err)
	}
	invalidateMCPConfigCache()
	if a.skills == nil {
		return nil
	}
	return a.skills.OnConfigReload(cfg)
}

func prepareRunMessages(messages []*schema.Message, cfg RunConfig) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages)+1)
	hints := make([]string, 0, 2)
	if cfg.IsPlanMode {
		hints = append(hints, "Plan mode is enabled. Keep task tracking explicit.")
	}
	if cfg.SubagentEnabled {
		hints = append(hints, "Subagent delegation is enabled for this run.")
	}
	if len(hints) > 0 {
		out = append(out, schema.SystemMessage(strings.Join(hints, "\n")))
	}
	if len(messages) == 0 {
		out = append(out, schema.UserMessage(""))
		return out
	}
	out = append(out, messages...)
	return out
}

func drainIter(ctx context.Context, iter *einoruntime.EventStream, threadID, runID string, ch chan<- Event) {
	if iter == nil {
		ch <- Event{Type: EventError, ThreadID: threadID, RunID: runID, Payload: ErrorPayload{Code: ErrorCodeEmptyStream, Message: "empty event stream"}, Timestamp: timeUnixMilli()}
		return
	}

	terminal := false
	var finalMessages []string
	emit := func(ev Event) {
		if terminal {
			return
		}
		if ev.Type == EventMessageDelta {
			if p, ok := ev.Payload.(MessageDeltaPayload); ok && !p.IsThinking {
				finalMessages = append(finalMessages, p.Content)
			}
		}
		ev.RunID = runID
		ch <- ev
		if ev.Type == EventError || ev.Type == EventCompleted {
			terminal = true
		}
	}

	for {
		select {
		case <-ctx.Done():
			emit(Event{Type: EventError, ThreadID: threadID, RunID: runID, Payload: ErrorPayload{Code: ErrorCodeContextCancelled, Message: ctx.Err().Error()}, Timestamp: timeUnixMilli()})
			return
		default:
		}

		ae, ok := iter.Next()
		if !ok {
			break
		}
		for _, ev := range convertAgentEvent(ae, threadID) {
			emit(ev)
			if terminal {
				return
			}
		}
	}

	finalMessage := strings.Join(finalMessages, "")
	emit(Event{Type: EventCompleted, ThreadID: threadID, RunID: runID, Payload: CompletedPayload{FinalMessage: finalMessage}, Timestamp: timeUnixMilli()})
}

func buildMiddlewares(cfg RunConfig) []adk.ChatModelAgentMiddleware {
	appCfg, _ := config.GetAppConfig()
	middlewares := make([]basemw.Middleware, 0, 16)

	memoryEnabled := true
	titleEnabled := true
	summarizeEnabled := true
	memoryPath := memorymw.DefaultMemoryPath
	memoryDebounce := 30 * time.Second
	memoryConfidence := 0.7
	memoryMaxFacts := 100
	memoryInjectionEnabled := true
	memoryMaxInjectionTokens := 2000

	if appCfg != nil {
		memoryEnabled = appCfg.Memory.Enabled
		titleEnabled = appCfg.Title.Enabled
		summarizeEnabled = appCfg.Summarization.Enabled
		if strings.TrimSpace(appCfg.Memory.StoragePath) != "" {
			memoryPath = appCfg.Memory.StoragePath
		}
		if appCfg.Memory.DebounceSeconds > 0 {
			memoryDebounce = time.Duration(appCfg.Memory.DebounceSeconds) * time.Second
		}
		if appCfg.Memory.FactConfidenceThreshold > 0 {
			memoryConfidence = appCfg.Memory.FactConfidenceThreshold
		}
		if appCfg.Memory.MaxFacts > 0 {
			memoryMaxFacts = appCfg.Memory.MaxFacts
		}
		memoryInjectionEnabled = appCfg.Memory.InjectionEnabled
		if appCfg.Memory.MaxInjectionTokens > 0 {
			memoryMaxInjectionTokens = appCfg.Memory.MaxInjectionTokens
		}
	}

	// Core middleware chain aligned with deer-flow order.
	middlewares = append(middlewares, threaddatamw.New(threaddatamw.DefaultConfig()))
	middlewares = append(middlewares, uploadsmw.New())
	sbProvider := buildSandboxProvider(appCfg)
	sandbox.SetDefaultProvider(sbProvider)
	middlewares = append(middlewares, sandboxmw.New(sbProvider))
	middlewares = append(middlewares, danglingmw.New())

	guardrailCfg := guardrailmw.DefaultConfig()
	if appCfg != nil {
		guardrailCfg.Enabled = appCfg.Guardrails.Enabled
		guardrailCfg.FailClosed = appCfg.Guardrails.FailClosed
		if appCfg.Guardrails.Passport != nil {
			guardrailCfg.Passport = *appCfg.Guardrails.Passport
		}

		// Build AllowlistProvider from provider config if configured.
		if appCfg.Guardrails.Provider != nil {
			providerCfg := appCfg.Guardrails.Provider
			// Check if using built-in AllowlistProvider.
			if providerCfg.Use == "" || providerCfg.Use == "allowlist" || providerCfg.Use == "goclaw/internal/middleware/guardrail:AllowlistProvider" {
				allowlistCfg := guardrailmw.AllowlistProviderConfig{}
				if providerCfg.Config != nil {
					if allowed, ok := providerCfg.Config["allowed_tools"]; ok {
						switch vv := allowed.(type) {
						case []any:
							for _, item := range vv {
								if name, ok := item.(string); ok && strings.TrimSpace(name) != "" {
									allowlistCfg.AllowedTools = append(allowlistCfg.AllowedTools, strings.TrimSpace(name))
								}
							}
						case []string:
							for _, name := range vv {
								if strings.TrimSpace(name) != "" {
									allowlistCfg.AllowedTools = append(allowlistCfg.AllowedTools, strings.TrimSpace(name))
								}
							}
						}
					}
					if denied, ok := providerCfg.Config["denied_tools"]; ok {
						switch vv := denied.(type) {
						case []any:
							for _, item := range vv {
								if name, ok := item.(string); ok && strings.TrimSpace(name) != "" {
									allowlistCfg.DeniedTools = append(allowlistCfg.DeniedTools, strings.TrimSpace(name))
								}
							}
						case []string:
							for _, name := range vv {
								if strings.TrimSpace(name) != "" {
									allowlistCfg.DeniedTools = append(allowlistCfg.DeniedTools, strings.TrimSpace(name))
								}
							}
						}
					}
				}
				guardrailCfg.Provider = guardrailmw.NewAllowlistProvider(allowlistCfg)
			}
			// For custom providers, fall back to legacy policy-based approach.
			// Future: support dynamic provider loading via reflection/plugin.
		}
	}
	middlewares = append(middlewares, guardrailmw.NewGuardrailMiddleware(guardrailCfg))
	middlewares = append(middlewares, auditmw.NewSandboxAuditMiddleware(nil))

	createChatModel := func(modelName string) (model.BaseChatModel, error) {
		if appCfg == nil {
			return nil, fmt.Errorf("app config not loaded")
		}
		req := models.CreateRequest{}
		if strings.TrimSpace(modelName) != "" {
			req.ModelName = strings.TrimSpace(modelName)
		} else if dm := appCfg.DefaultModel(); dm != nil {
			req.ModelName = dm.Name
		}
		return models.CreateChatModel(context.Background(), appCfg, req)
	}

	if summarizeEnabled {
		summCfg := summarizemw.DefaultConfig()
		if appCfg != nil {
			if strings.TrimSpace(appCfg.Summarization.SummaryPrompt) != "" {
				summCfg.PromptTemplate = appCfg.Summarization.SummaryPrompt
			}
			if appCfg.Summarization.Keep.Type == "messages" && int(appCfg.Summarization.Keep.Value) > 0 {
				summCfg.KeepRecentMessages = int(appCfg.Summarization.Keep.Value)
			}
			for _, tr := range appCfg.Summarization.Trigger {
				switch strings.ToLower(strings.TrimSpace(tr.Type)) {
				case "fraction":
					if tr.Value > 0 && tr.Value <= 1 {
						summCfg.ThresholdRatio = tr.Value
					}
				case "tokens":
					if tr.Value > 0 {
						summCfg.TokenLimit = int(tr.Value)
					}
				}
			}
		}
		var summarizer summarizemw.Summarizer
		if appCfg != nil {
			if cm, err := createChatModel(appCfg.Summarization.ModelName); err == nil && cm != nil {
				summarizer = summarizemw.NewEinoSummarizer(cm)
			}
		}
		middlewares = append(middlewares, summarizemw.NewSummarizationMiddleware(summCfg, summarizer))
	}

	middlewares = append(middlewares, todomw.NewTodoMiddleware())

	// TitleMiddleware comes after TodoMiddleware (matches DeerFlow order #8)
	if titleEnabled {
		titleCfg := titlemw.DefaultConfig()
		if appCfg != nil {
			if appCfg.Title.MaxWords > 0 {
				titleCfg.MaxWords = appCfg.Title.MaxWords
			}
		}
		var titleGen titlemw.TitleGenerator
		if appCfg != nil {
			if cm, err := createChatModel(appCfg.Title.ModelName); err == nil && cm != nil {
				titleGen = titlemw.NewEinoTitleGenerator(cm)
			}
		}
		middlewares = append(middlewares, titlemw.NewTitleMiddleware(titleCfg, titleGen))
	}

	if memoryEnabled {
		store := memorymw.NewJSONFileStore(memoryPath)
		queue := memorymw.GetGlobalQueue(memoryPath)
		queue.DebounceDelay = memoryDebounce
		queue.SetMaxFacts(memoryMaxFacts)
		if appCfg != nil {
			if cm, err := createChatModel(appCfg.Memory.ModelName); err == nil && cm != nil {
				queue.SetExtractor(memorymw.NewEinoFactExtractor(cm, memoryConfidence))
			}
		}
		middlewares = append(middlewares, memorymw.NewMemoryMiddleware(
			store,
			queue,
			"",
			memorymw.WithInjectionEnabled(memoryInjectionEnabled),
			memorymw.WithMaxInjectionTokens(memoryMaxInjectionTokens),
		))
	}

	middlewares = append(middlewares, viewimagemw.NewViewImageMiddleware())
	middlewares = append(middlewares, subagentlimitmw.New(subagentlimitmw.DefaultConfig()))

	// LoopDetectionMiddleware comes after SubagentLimitMiddleware (matches DeerFlow order #12)
	middlewares = append(middlewares, loopmw.New(loopmw.DefaultConfig()))

	middlewares = append(middlewares, deferredtoolmw.NewDeferredToolFilterMiddleware(deferredtoolmw.DefaultDeferredTools()))
	if appCfg != nil && appCfg.TokenUsage.Enabled {
		middlewares = append(middlewares, tokenusagemw.New())
	}
	llmErrorMaxRetries := 3
	if appCfg != nil {
		var targetModel *config.ModelConfig
		if strings.TrimSpace(cfg.ModelName) != "" {
			targetModel = appCfg.GetModelConfig(strings.TrimSpace(cfg.ModelName))
		}
		if targetModel == nil {
			targetModel = appCfg.DefaultModel()
		}
		if targetModel != nil && targetModel.MaxRetries > 0 {
			llmErrorMaxRetries = targetModel.MaxRetries
		}
	}
	middlewares = append(middlewares, llmerrormw.NewLLMErrorHandlingMiddleware(llmErrorMaxRetries))
	middlewares = append(middlewares, toolerrormw.New())
	middlewares = append(middlewares, clarificationmw.NewClarificationMiddleware())

	if len(middlewares) == 0 {
		return nil
	}

	// Convert basemw.Middleware to adk.ChatModelAgentMiddleware using adapter
	return basemw.AdaptMiddlewares(middlewares)
}

func buildSandboxProvider(appCfg *config.AppConfig) sandbox.SandboxProvider {
	sbCfg := sandbox.SandboxConfig{
		Type:        sandbox.SandboxTypeLocal,
		WorkDir:     ".goclaw",
		ExecTimeout: 30 * time.Second,
	}

	skillsPath := ""
	if appCfg != nil {
		if appCfg.Sandbox.AllowHostBash {
			sbCfg.AllowedCommands = []string{"bash", "sh", "ls", "cat", "pwd", "echo", "grep"}
		}
		// Get skills path from config
		skillsPath = strings.TrimSpace(appCfg.Skills.Path)
		if skillsPath != "" {
			sbCfg.Docker.SkillsMountPath = skillsPath
		}
		if strings.EqualFold(strings.TrimSpace(appCfg.Sandbox.Use), "docker") {
			sbCfg.Type = sandbox.SandboxTypeDocker
			sbCfg.Docker.Image = strings.TrimSpace(appCfg.Sandbox.Image)
			sbCfg.Docker.ContainerPrefix = strings.TrimSpace(appCfg.Sandbox.ContainerPrefix)
			if appCfg.Sandbox.IdleTimeout > 0 {
				sbCfg.Docker.ContainerTTL = time.Duration(appCfg.Sandbox.IdleTimeout) * time.Second
			}
			if len(appCfg.Sandbox.Environment) > 0 {
				sbCfg.Docker.Environment = make(map[string]string, len(appCfg.Sandbox.Environment))
				for k, v := range appCfg.Sandbox.Environment {
					sbCfg.Docker.Environment[k] = v
				}
			}
			if len(appCfg.Sandbox.Mounts) > 0 {
				sbCfg.Docker.Mounts = make([]sandbox.DockerVolumeMount, 0, len(appCfg.Sandbox.Mounts))
				for _, m := range appCfg.Sandbox.Mounts {
					sbCfg.Docker.Mounts = append(sbCfg.Docker.Mounts, sandbox.DockerVolumeMount{
						HostPath:      strings.TrimSpace(m.HostPath),
						ContainerPath: strings.TrimSpace(m.ContainerPath),
						ReadOnly:      m.ReadOnly,
					})
				}
			}
		}
	}

	if sbCfg.Type == sandbox.SandboxTypeDocker {
		provider, err := dockersandbox.NewDockerSandboxProvider(sbCfg, sbCfg.WorkDir)
		if err == nil {
			return provider
		}
		if appCfg != nil && appCfg.Sandbox.StrictDocker {
			panic(fmt.Sprintf("sandbox.use=docker requested and strict_docker=true, but docker provider init failed: %v", err))
		}
		log.Printf("[WARN] sandbox.use=docker requested but docker provider init failed, falling back to local sandbox: %v", err)
	}
	return localsandbox.NewLocalSandboxProvider(sbCfg, sbCfg.WorkDir, skillsPath)
}

func toMiddlewareState(ctx context.Context, st *adk.ChatModelAgentState) *basemw.State {
	vals := adk.GetSessionValues(ctx)
	threadID, _ := vals["thread_id"].(string)
	planMode, _ := vals["plan_mode"].(bool)
	isSubagent, _ := vals["is_subagent"].(bool)

	msgs := make([]map[string]any, 0, len(st.Messages))
	for _, m := range st.Messages {
		msgs = append(msgs, toLegacyMessage(m))
	}

	extra := map[string]any{
		"is_subagent": isSubagent,
	}
	if uploaded, ok := vals["uploaded_files"]; ok {
		extra["uploaded_files"] = uploaded
	}
	if agentName, ok := vals["agent_name"].(string); ok && strings.TrimSpace(agentName) != "" {
		extra["agent_name"] = strings.TrimSpace(agentName)
	}

	// Pre-seed pending_tool_calls from the latest assistant message if present.
	for i := len(msgs) - 1; i >= 0; i-- {
		if role, _ := msgs[i]["role"].(string); role == "assistant" {
			if tcs := parseLegacyToolCalls(msgs[i]["tool_calls"]); len(tcs) > 0 {
				extra["pending_tool_calls"] = tcs
			}
			break
		}
	}

	mwState := &basemw.State{
		ThreadID:     threadID,
		Messages:     msgs,
		PlanMode:     planMode,
		ViewedImages: map[string]basemw.ViewedImage{},
		Extra:        extra,
	}
	if rawImages, ok := vals["viewed_images"]; ok {
		mwState.ViewedImages = parseViewedImages(rawImages)
	}
	return mwState
}

func syncMiddlewareStateToSession(ctx context.Context, state *basemw.State) {
	if state == nil {
		return
	}
	vals := adk.GetSessionValues(ctx)
	if vals == nil {
		return
	}
	vals[middlewareStateSessionKey] = state
	if state.Extra == nil {
		return
	}
	for _, k := range []string{"task_tool_calls_count", "clarification_request", "interrupt", "pending_tool_calls"} {
		if v, ok := state.Extra[k]; ok {
			vals[k] = v
		}
	}
}

func toToolMiddlewareState(ctx context.Context) *basemw.State {
	vals := adk.GetSessionValues(ctx)
	if vals != nil {
		if cached, ok := vals[middlewareStateSessionKey].(*basemw.State); ok && cached != nil {
			if cached.Extra == nil {
				cached.Extra = map[string]any{}
			}
			return cached
		}
	}

	threadID := ""
	planMode := false
	extra := map[string]any{}
	viewedImages := map[string]basemw.ViewedImage{}
	if vals != nil {
		threadID, _ = vals["thread_id"].(string)
		planMode, _ = vals["plan_mode"].(bool)
		isSubagent, _ := vals["is_subagent"].(bool)
		extra["is_subagent"] = isSubagent
		if uploaded, ok := vals["uploaded_files"]; ok {
			extra["uploaded_files"] = uploaded
		}
		if agentName, ok := vals["agent_name"].(string); ok && strings.TrimSpace(agentName) != "" {
			extra["agent_name"] = strings.TrimSpace(agentName)
		}
		for _, k := range []string{"task_tool_calls_count", "clarification_request", "interrupt", "pending_tool_calls"} {
			if v, ok := vals[k]; ok {
				extra[k] = v
			}
		}
		viewedImages = parseViewedImages(vals["viewed_images"])
	}

	state := &basemw.State{
		ThreadID:     threadID,
		PlanMode:     planMode,
		ViewedImages: viewedImages,
		Extra:        extra,
	}
	if vals != nil {
		vals[middlewareStateSessionKey] = state
	}
	return state
}

func toMiddlewareToolCall(input *compose.ToolInput) *basemw.ToolCall {
	if input == nil {
		return &basemw.ToolCall{Input: map[string]any{}}
	}
	return &basemw.ToolCall{
		ID:    input.CallID,
		Name:  input.Name,
		Input: parseToolInputArguments(input.Arguments),
	}
}

func toComposeToolInput(original *compose.ToolInput, toolCall *basemw.ToolCall) *compose.ToolInput {
	if original == nil {
		return &compose.ToolInput{}
	}
	if toolCall == nil {
		return original
	}
	arguments := original.Arguments
	if len(toolCall.Input) > 0 {
		if bs, err := json.Marshal(toolCall.Input); err == nil {
			arguments = string(bs)
		}
	}
	return &compose.ToolInput{
		Name:        toolCall.Name,
		Arguments:   arguments,
		CallID:      toolCall.ID,
		CallOptions: original.CallOptions,
	}
}

func parseToolInputArguments(arguments string) map[string]any {
	args := strings.TrimSpace(arguments)
	if args == "" {
		return map[string]any{}
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(args), &obj); err == nil {
		if obj == nil {
			return map[string]any{}
		}
		return obj
	}
	var raw any
	if err := json.Unmarshal([]byte(args), &raw); err == nil {
		if obj, ok := raw.(map[string]any); ok {
			return obj
		}
		return map[string]any{"input": raw}
	}
	return map[string]any{"input": args}
}

func middlewareToolOutputToString(output any) string {
	switch v := output.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		if bs, err := json.Marshal(v); err == nil {
			return string(bs)
		}
		return fmt.Sprint(v)
	}
}

func runMiddlewareToolChain(ctx context.Context, middlewares []basemw.Middleware, state *basemw.State, toolCall *basemw.ToolCall, base basemw.ToolHandler) (*basemw.ToolResult, error) {
	handler := base
	for i := len(middlewares) - 1; i >= 0; i-- {
		mw := middlewares[i]
		nextHandler := handler
		handler = func(callCtx context.Context, call *basemw.ToolCall) (*basemw.ToolResult, error) {
			return mw.WrapToolCall(callCtx, state, call, nextHandler)
		}
	}
	return handler(ctx, toolCall)
}

func parseLegacyToolCalls(raw any) []map[string]any {
	out := make([]map[string]any, 0)
	switch vv := raw.(type) {
	case []map[string]any:
		out = append(out, vv...)
	case []any:
		for _, item := range vv {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func parseViewedImages(raw any) map[string]basemw.ViewedImage {
	out := make(map[string]basemw.ViewedImage)
	switch vv := raw.(type) {
	case map[string]ViewedImageData:
		for path, img := range vv {
			out[path] = basemw.ViewedImage{Base64: img.Base64, MIMEType: img.MIMEType}
		}
	case map[string]any:
		for path, v := range vv {
			switch img := v.(type) {
			case map[string]any:
				base64Data, _ := img["base64"].(string)
				mimeType, _ := img["mime_type"].(string)
				out[path] = basemw.ViewedImage{Base64: base64Data, MIMEType: mimeType}
			}
		}
	}
	return out
}

func applyMiddlewareState(ms *basemw.State, st *adk.ChatModelAgentState) {
	if ms == nil || st == nil {
		return
	}
	converted := make([]*schema.Message, 0, len(ms.Messages))
	for _, m := range ms.Messages {
		converted = append(converted, fromLegacyMessage(m))
	}
	st.Messages = converted
}

func toLegacyMessage(m *schema.Message) map[string]any {
	out := map[string]any{
		"role":    roleToLegacy(m.Role),
		"content": m.Content,
	}
	if m.Name != "" {
		out["name"] = m.Name
	}
	if len(m.ToolCalls) > 0 {
		calls := make([]map[string]any, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			calls = append(calls, map[string]any{
				"id":        tc.ID,
				"name":      tc.Function.Name,
				"arguments": tc.Function.Arguments,
				"input":     tc.Function.Arguments,
			})
		}
		out["tool_calls"] = calls
	}
	if m.ToolCallID != "" {
		out["tool_call_id"] = m.ToolCallID
	}
	if m.ToolName != "" {
		out["tool_name"] = m.ToolName
	}
	return out
}

func fromLegacyMessage(m map[string]any) *schema.Message {
	role, _ := m["role"].(string)
	content, _ := m["content"].(string)

	switch role {
	case "system":
		return schema.SystemMessage(content)
	case "assistant":
		calls := toToolCalls(m["tool_calls"])
		msg := schema.AssistantMessage(content, calls)
		if name, ok := m["name"].(string); ok {
			msg.Name = name
		}
		return msg
	case "tool":
		toolCallID, _ := m["tool_call_id"].(string)
		toolName, _ := m["tool_name"].(string)
		opts := make([]schema.ToolMessageOption, 0, 1)
		if toolName != "" {
			opts = append(opts, schema.WithToolName(toolName))
		}
		return schema.ToolMessage(content, toolCallID, opts...)
	default:
		return schema.UserMessage(content)
	}
}

func toToolCalls(raw any) []schema.ToolCall {
	toCall := func(v map[string]any) schema.ToolCall {
		id, _ := v["id"].(string)
		name, _ := v["name"].(string)
		args, _ := v["arguments"].(string)
		if args == "" {
			args, _ = v["input"].(string)
		}
		return schema.ToolCall{ID: id, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}
	}

	out := make([]schema.ToolCall, 0)
	switch vv := raw.(type) {
	case []map[string]any:
		for _, v := range vv {
			out = append(out, toCall(v))
		}
	case []any:
		for _, item := range vv {
			if m, ok := item.(map[string]any); ok {
				out = append(out, toCall(m))
			}
		}
	}
	return out
}

func roleToLegacy(role schema.RoleType) string {
	switch role {
	case schema.User:
		return "human"
	case schema.Assistant:
		return "assistant"
	case schema.Tool:
		return "tool"
	case schema.System:
		return "system"
	default:
		return string(role)
	}
}

func toMiddlewareResponse(st *adk.ChatModelAgentState) *basemw.Response {
	resp := &basemw.Response{ToolCalls: make([]map[string]any, 0)}
	if st == nil || len(st.Messages) == 0 {
		return resp
	}

	last := st.Messages[len(st.Messages)-1]
	resp.FinalMessage = last.Content
	for _, tc := range last.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, map[string]any{
			"id":    tc.ID,
			"name":  tc.Function.Name,
			"input": tc.Function.Arguments,
		})
	}
	return resp
}

func convertAgentEvent(event *adk.AgentEvent, threadID string) []Event {
	if event == nil {
		return nil
	}

	now := timeUnixMilli()
	out := make([]Event, 0, 4)

	if event.Err != nil {
		out = append(out, Event{Type: EventError, ThreadID: threadID, Payload: ErrorPayload{Code: ErrorCodeRunFailed, Message: event.Err.Error()}, Timestamp: now})
		return out
	}
	if event.Action != nil && event.Action.Interrupted != nil {
		out = append(out, Event{Type: EventError, ThreadID: threadID, Payload: ErrorPayload{Code: ErrorCodeInterrupted, Message: "run interrupted"}, Timestamp: now})
		return out
	}

	// Handle CustomizedAction for task events
	if event.Action != nil && event.Action.CustomizedAction != nil {
		if data, ok := event.Action.CustomizedAction.(map[string]any); ok {
			eventType, _ := data["type"].(string)
			taskID, _ := data["task_id"].(string)
			if eventType != "" && taskID != "" {
				out = append(out, Event{
					Type:      EventType(eventType),
					ThreadID:  threadID,
					Payload:   data,
					Timestamp: now,
				})
			}
		}
		return out
	}

	if event.Output == nil || event.Output.MessageOutput == nil {
		return out
	}

	msg, err := event.Output.MessageOutput.GetMessage()
	if err != nil || msg == nil {
		return out
	}

	if strings.TrimSpace(msg.ReasoningContent) != "" {
		out = append(out, Event{Type: EventMessageDelta, ThreadID: threadID, Payload: MessageDeltaPayload{Content: msg.ReasoningContent, IsThinking: true}, Timestamp: now})
	}
	if strings.TrimSpace(msg.Content) != "" {
		out = append(out, Event{Type: EventMessageDelta, ThreadID: threadID, Payload: MessageDeltaPayload{Content: msg.Content}, Timestamp: now})
	}
	for _, tc := range msg.ToolCalls {
		out = append(out, Event{Type: EventToolEvent, ThreadID: threadID, Payload: ToolEventPayload{CallID: tc.ID, ToolName: tc.Function.Name, Input: tc.Function.Arguments}, Timestamp: now})
	}
	if msg.Role == schema.Tool {
		out = append(out, Event{Type: EventToolEvent, ThreadID: threadID, Payload: ToolEventPayload{CallID: msg.ToolCallID, ToolName: msg.ToolName, Output: msg.Content, IsError: isToolError(msg)}, Timestamp: now})
		if msg.ToolName == subagents.TaskToolName {
			if taskEv := toTaskEvent(threadID, msg.Content, now); taskEv != nil {
				out = append(out, *taskEv)
			}
		}
	}
	return out
}

// isToolError detects if a tool message represents an error result.
func isToolError(msg *schema.Message) bool {
	if msg == nil || msg.Role != schema.Tool {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(msg.Content))
	return strings.HasPrefix(lower, "error") || strings.HasPrefix(lower, "failed")
}

func toTaskEvent(threadID, raw string, ts int64) *Event {
	var payload struct {
		TaskID  string `json:"task_id"`
		Subject string `json:"subject"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	if payload.TaskID == "" || payload.Status == "" {
		return nil
	}

	evType := EventTaskRunning
	switch payload.Status {
	case string(subagents.StatusPending), string(subagents.StatusQueued):
		evType = EventTaskStarted
	case string(subagents.StatusInProgress):
		evType = EventTaskRunning
	case string(subagents.StatusCompleted):
		evType = EventTaskCompleted
	case string(subagents.StatusFailed), string(subagents.StatusTimedOut):
		evType = EventTaskFailed
	}

	return &Event{
		Type:     evType,
		ThreadID: threadID,
		Payload: TaskPayload{
			TaskID:  payload.TaskID,
			Subject: payload.Subject,
			Status:  payload.Status,
		},
		Timestamp: ts,
	}
}

// timeUnixMilli returns current time in Unix milliseconds.
func timeUnixMilli() int64 {
	return time.Now().UnixMilli()
}

// filterSkillsByName filters skills by a list of skill names (P0 fix).
// Returns the allowed tools set from the specified skills.
func filterSkillsByName(skills []*skillsruntime.Skill, skillNames []string) map[string]struct{} {
	allowed := make(map[string]struct{})

	skillSet := make(map[string]bool)
	for _, name := range skillNames {
		skillSet[strings.ToLower(strings.TrimSpace(name))] = true
	}

	for _, skill := range skills {
		if skillSet[strings.ToLower(skill.Metadata.Name)] {
			for _, tool := range skill.Metadata.AllowedTools {
				allowed[strings.TrimSpace(tool)] = struct{}{}
			}
		}
	}

	return allowed
}

// filterToolsByToolGroups filters tools by tool group names (P1 fix).
func filterToolsByToolGroups(ctx context.Context, tools []lcTool.BaseTool, groupNames []string, appCfg *config.AppConfig) ([]lcTool.BaseTool, error) {
	if len(groupNames) == 0 {
		return tools, nil
	}

	// Build a set of allowed tool groups
	allowedGroups := make(map[string]bool)
	for _, g := range groupNames {
		allowedGroups[strings.TrimSpace(g)] = true
	}

	// Collect tool names that belong to allowed groups
	allowedTools := make(map[string]bool)
	for _, group := range appCfg.ToolGroups {
		if allowedGroups[group.Name] {
			// Tools in this group are allowed
			for _, tool := range appCfg.Tools {
				if tool.Group == group.Name {
					allowedTools[tool.Name] = true
				}
			}
		}
	}

	// Filter tools
	filtered := make([]lcTool.BaseTool, 0, len(tools))
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil {
			continue
		}
		if allowedTools[info.Name] {
			filtered = append(filtered, t)
		}
	}

	return filtered, nil
}

// buildSystemPrompt builds the system prompt with optional SOUL.md content and skills (P1 fix).
func buildSystemPrompt(soul string, skillRegistry *skillsruntime.Registry) string {
	basePrompt := "You are GoClaw lead agent."

	var parts []string
	parts = append(parts, basePrompt)

	// Inject skills prompt section (P0 fix)
	if skillRegistry != nil {
		if skillsSection := skillRegistry.GetSkillsPromptSection(); skillsSection != "" {
			parts = append(parts, skillsSection)
		}
	}

	// Inject SOUL.md content
	if soul != "" {
		parts = append(parts, fmt.Sprintf("<agent_soul>\n%s\n</agent_soul>", soul))
	}

	return strings.Join(parts, "\n\n")
}
