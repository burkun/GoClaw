package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	lctool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"

	"goclaw/internal/agent/subagents"
	"goclaw/internal/agentconfig"
	"goclaw/internal/config"
	einoruntime "goclaw/internal/eino"
	"goclaw/internal/logging"
	basemw "goclaw/internal/middleware"
	"goclaw/internal/middleware/builder"
	"goclaw/internal/models"
	"goclaw/internal/sandbox"
	dockersandbox "goclaw/internal/sandbox/docker"
	localsandbox "goclaw/internal/sandbox/local"
	skillsruntime "goclaw/internal/skills"
	toolruntime "goclaw/internal/tools"
	toolbootstrap "goclaw/internal/tools/bootstrap"
)

var getAppConfig = config.GetAppConfig
var registerDefaultTools = toolbootstrap.RegisterDefaultToolsWithModel
var invalidateMCPConfigCache = toolruntime.InvalidateMCPConfigCache

const middlewareStateSessionKey = "_goclaw_middleware_state"

// New creates a lead agent with default configuration.
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
	// Default agent has all skills available (nil = no filter)
	instruction := buildSystemPrompt("", skillRegistry, nil)

	a, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "lead_agent",
		Description: "GoClaw lead agent",
		Instruction: instruction,
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		MaxIterations: 100,
		Middlewares:    mws,
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
				logging.Warn("failed to load agent config, using default", "agent", agentName, "error", err)
			} else {
				// Override model if specified
				if agentCfg.Model != "" {
					logging.Info("agent using model", "agent", agentName, "model", agentCfg.Model)
				}

				// Store skills and tool groups for filtering
				agentSkills = agentCfg.Skills
				agentToolGroups = agentCfg.ToolGroups
			}

			// Load SOUL.md
			if soul, err := agentLoader.LoadSoul(agentName); err == nil {
				agentSoul = soul
				logging.Info("loaded SOUL.md for agent", "agent", agentName, "bytes", len(soul))
			}
		} else {
			logging.Warn("agent not found in file system, using default config", "agent", agentName)
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
		logging.Info("agent: filtered skills", "agent", agentName, "count", len(agentSkills), "skills", agentSkills)
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
		logging.Info("agent: filtered tool groups", "agent", agentName, "count", len(agentToolGroups), "groups", agentToolGroups)
	}

	mws := buildMiddlewares(RunConfig{AgentName: agentName})

	// Build availableSkills map from agent config
	var availableSkills map[string]bool
	if len(agentSkills) > 0 {
		availableSkills = make(map[string]bool)
		for _, skill := range agentSkills {
			availableSkills[skill] = true
		}
	}

	// Build system prompt with SOUL.md and skills (P0 fix)
	instruction := buildSystemPrompt(agentSoul, skillRegistry, availableSkills)

	a, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "lead_agent",
		Description: fmt.Sprintf("GoClaw lead agent (%s)", agentName),
		Instruction: instruction,
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		MaxIterations: 100,
		Middlewares:    mws,
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

func buildMiddlewares(cfg RunConfig) []adk.AgentMiddleware {
	appCfg, _ := config.GetAppConfig()
	sbProvider := buildSandboxProvider(appCfg)
	sandbox.SetDefaultProvider(sbProvider)

	// Create model creator function
	createChatModel := func(ctx context.Context, modelName string) (model.ToolCallingChatModel, error) {
		if appCfg == nil {
			return nil, fmt.Errorf("app config not loaded")
		}
		req := models.CreateRequest{}
		if strings.TrimSpace(modelName) != "" {
			req.ModelName = strings.TrimSpace(modelName)
		} else if dm := appCfg.DefaultModel(); dm != nil {
			req.ModelName = dm.Name
		}
		return models.CreateChatModel(ctx, appCfg, req)
	}

	// Build middlewares using builder
	builderCfg := &builder.BuilderConfig{
		AppConfig:       appCfg,
		ModelName:       cfg.ModelName,
		SandboxProvider: sbProvider,
		CreateChatModel: createChatModel,
	}

	middlewares := builder.BuildMiddlewaresFromBuilder(builderCfg)

	if len(middlewares) == 0 {
		return nil
	}

	// Convert basemw.Middleware to adk.AgentMiddleware using adapter
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
		logging.Warn("sandbox.use=docker requested but docker provider init failed, falling back to local sandbox", "error", err)
	}
	return localsandbox.NewLocalSandboxProvider(sbCfg, sbCfg.WorkDir, skillsPath)
}

func filterToolsByAllowed(ctx context.Context, tools []lctool.BaseTool, allowed map[string]struct{}) ([]lctool.BaseTool, error) {
	if len(allowed) == 0 {
		return tools, nil
	}
	out := make([]lctool.BaseTool, 0, len(tools))
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
func filterToolsByToolGroups(ctx context.Context, tools []lctool.BaseTool, groupNames []string, appCfg *config.AppConfig) ([]lctool.BaseTool, error) {
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
	filtered := make([]lctool.BaseTool, 0, len(tools))
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
// If availableSkills is not nil, only skills in the set are included in the prompt.
func buildSystemPrompt(soul string, skillRegistry *skillsruntime.Registry, availableSkills map[string]bool) string {
	basePrompt := "You are GoClaw lead agent."

	var parts []string
	parts = append(parts, basePrompt)

	// Inject skills prompt section (P0 fix)
	if skillRegistry != nil {
		if skillsSection := skillRegistry.GetSkillsPromptSection(availableSkills); skillsSection != "" {
			parts = append(parts, skillsSection)
		}
	}

	// Inject SOUL.md content
	if soul != "" {
		parts = append(parts, fmt.Sprintf("<agent_soul>\n%s\n</agent_soul>", soul))
	}

	return strings.Join(parts, "\n\n")
}
