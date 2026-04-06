package main

import (
	"context"
	"os"
	"strings"

	"github.com/bookerbai/goclaw/internal/agent"
	"github.com/bookerbai/goclaw/internal/agentconfig"
	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/logging"
	"github.com/bookerbai/goclaw/pkg/gateway"
)

func main() {
	ctx := context.Background()

	// Initialize logging with default level first (will be updated after config load)
	logging.Init("info")

	cfg, err := config.GetAppConfig()
	if err != nil {
		logging.Error("load config failed", "error", err)
		os.Exit(1)
	}

	// Update log level from config
	if cfg != nil && cfg.LogLevel != "" {
		logging.SetLevel(cfg.LogLevel)
	}

	// P1 fix: 预加载所有enabled agents
	agents := make(map[string]agent.LeadAgent)

	// 1. 从文件系统加载自定义agents
	agentLoader := agentconfig.DefaultLoader
	fileAgents, err := agentLoader.ListAgents()
	if err != nil {
		logging.Warn("failed to list file agents", "error", err)
	} else {
		for _, agentName := range fileAgents {
			// 加载per-agent配置检查是否enabled
			_, err := agentLoader.LoadConfig(agentName)
			if err != nil {
				logging.Warn("failed to load agent config", "agent", agentName, "error", err)
				continue
			}

			// 检查主配置中的enabled状态
			enabled := true // 默认enabled
			if cfg != nil && cfg.Agents != nil {
				if mainCfg, ok := cfg.Agents[agentName]; ok {
					enabled = mainCfg.Enabled
				}
			}

			if !enabled {
				logging.Info("agent is disabled, skipping", "agent", agentName)
				continue
			}

			// 创建agent实例
			leadAgent, err := agent.NewWithName(ctx, agentName)
			if err != nil {
				logging.Warn("failed to create agent", "agent", agentName, "error", err)
				continue
			}

			agents[agentName] = leadAgent
			logging.Info("loaded agent", "agent", agentName)
		}
	}

	// 2. 从主配置加载agents（如果文件系统中没有）
	if cfg != nil && cfg.Agents != nil {
		for name, agentCfg := range cfg.Agents {
			if !agentCfg.Enabled {
				continue
			}

			// 检查是否已从文件系统加载
			if _, exists := agents[name]; exists {
				continue
			}

			// 创建agent实例
			leadAgent, err := agent.NewWithName(ctx, name)
			if err != nil {
				logging.Warn("failed to create agent from config", "agent", name, "error", err)
				continue
			}

			agents[name] = leadAgent
			logging.Info("loaded agent from config", "agent", name)
		}
	}

	// 3. 确保至少有一个默认agent
	if len(agents) == 0 {
		leadAgent, err := agent.New(ctx)
		if err != nil {
			logging.Error("initialize default agent failed", "error", err)
			os.Exit(1)
		}
		agents["default"] = leadAgent
		logging.Info("created default agent")
	}

	// 获取默认agent（第一个或名为default的）
	var defaultAgent agent.LeadAgent
	if a, ok := agents["default"]; ok {
		defaultAgent = a
	} else {
		// 使用第一个agent作为默认
		for _, a := range agents {
			defaultAgent = a
			break
		}
	}

	addr := ":8001"
	if cfg != nil && strings.TrimSpace(cfg.Server.Address) != "" {
		addr = strings.TrimSpace(cfg.Server.Address)
	}

	// P1 fix: 传递agents map给gateway
	srv := gateway.NewWithAgents(cfg, defaultAgent, agents)
	logging.Info("goclaw gateway listening", "address", addr, "agents", getAgentNames(agents))
	if err := srv.Run(addr); err != nil {
		logging.Error("gateway run failed", "error", err)
		os.Exit(1)
	}
}

func getAgentNames(agents map[string]agent.LeadAgent) []string {
	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	return names
}
