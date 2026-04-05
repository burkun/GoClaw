package main

import (
	"context"
	"log"
	"strings"

	"github.com/bookerbai/goclaw/internal/agent"
	"github.com/bookerbai/goclaw/internal/agentconfig"
	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/pkg/gateway"
)

func main() {
	ctx := context.Background()

	cfg, err := config.GetAppConfig()
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	// P1 fix: 预加载所有enabled agents
	agents := make(map[string]agent.LeadAgent)
	
	// 1. 从文件系统加载自定义agents
	agentLoader := agentconfig.DefaultLoader
	fileAgents, err := agentLoader.ListAgents()
	if err != nil {
		log.Printf("[WARN] failed to list file agents: %v", err)
	} else {
		for _, agentName := range fileAgents {
			// 加载per-agent配置检查是否enabled
			_, err := agentLoader.LoadConfig(agentName)
			if err != nil {
				log.Printf("[WARN] failed to load agent config %s: %v", agentName, err)
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
				log.Printf("[INFO] agent %s is disabled, skipping", agentName)
				continue
			}

			// 创建agent实例
			leadAgent, err := agent.NewWithName(ctx, agentName)
			if err != nil {
				log.Printf("[WARN] failed to create agent %s: %v", agentName, err)
				continue
			}
			
			agents[agentName] = leadAgent
			log.Printf("[INFO] loaded agent: %s", agentName)
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
				log.Printf("[WARN] failed to create agent %s from config: %v", name, err)
				continue
			}
			
			agents[name] = leadAgent
			log.Printf("[INFO] loaded agent from config: %s", name)
		}
	}

	// 3. 确保至少有一个默认agent
	if len(agents) == 0 {
		leadAgent, err := agent.New(ctx)
		if err != nil {
			log.Fatalf("initialize default agent failed: %v", err)
		}
		agents["default"] = leadAgent
		log.Printf("[INFO] created default agent")
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
	log.Printf("goclaw gateway listening on %s", addr)
	log.Printf("loaded %d agents: %v", len(agents), getAgentNames(agents))
	if err := srv.Run(addr); err != nil {
		log.Fatalf("gateway run failed: %v", err)
	}
}

func getAgentNames(agents map[string]agent.LeadAgent) []string {
	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	return names
}
