package main

import (
	"context"
	"log"
	"strings"

	"github.com/bookerbai/goclaw/internal/agent"
	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/pkg/gateway"
)

func main() {
	ctx := context.Background()

	cfg, err := config.GetAppConfig()
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	leadAgent, err := agent.New(ctx)
	if err != nil {
		log.Fatalf("initialize lead agent failed: %v", err)
	}

	addr := ":8001"
	if cfg != nil && strings.TrimSpace(cfg.Server.Address) != "" {
		addr = strings.TrimSpace(cfg.Server.Address)
	}

	srv := gateway.New(cfg, leadAgent)
	log.Printf("goclaw gateway listening on %s", addr)
	if err := srv.Run(addr); err != nil {
		log.Fatalf("gateway run failed: %v", err)
	}
}
