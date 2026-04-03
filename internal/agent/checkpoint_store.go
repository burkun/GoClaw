package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"

	"github.com/bookerbai/goclaw/internal/config"
)

func newCheckPointStore(appCfg *config.AppConfig) (adk.CheckPointStore, error) {
	if appCfg == nil || appCfg.Checkpointer == nil {
		return nil, nil
	}

	cp := appCfg.Checkpointer
	typ := strings.ToLower(strings.TrimSpace(cp.Type))
	if typ == "" {
		typ = "memory"
	}

	switch typ {
	case "memory":
		return newInMemoryCheckPointStore(), nil
	case "sqlite", "postgres":
		return nil, fmt.Errorf("agent.New: checkpointer type %q is not implemented yet", cp.Type)
	default:
		return nil, fmt.Errorf("agent.New: unknown checkpointer type %q", cp.Type)
	}
}

type inMemoryCheckPointStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newInMemoryCheckPointStore() *inMemoryCheckPointStore {
	return &inMemoryCheckPointStore{data: make(map[string][]byte)}
}

func (s *inMemoryCheckPointStore) Get(_ context.Context, checkPointID string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[checkPointID]
	if !ok {
		return nil, false, nil
	}
	out := make([]byte, len(v))
	copy(out, v)
	return out, true, nil
}

func (s *inMemoryCheckPointStore) Set(_ context.Context, checkPointID string, checkPoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := make([]byte, len(checkPoint))
	copy(v, checkPoint)
	s.data[checkPointID] = v
	return nil
}
