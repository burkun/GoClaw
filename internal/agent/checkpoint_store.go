package agent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		path := checkpointStorePath(cp, typ)
		return newFileCheckPointStore(path), nil
	default:
		return nil, fmt.Errorf("agent.New: unknown checkpointer type %q", cp.Type)
	}
}

func checkpointStorePath(cp *config.CheckpointerConfig, typ string) string {
	conn := strings.TrimSpace(cp.ConnectionString)
	baseDir := ".goclaw"

	if typ == "sqlite" {
		if conn == "" {
			return filepath.Join(baseDir, "checkpoints", "sqlite-checkpoints.json")
		}
		if strings.Contains(conn, "://") || strings.Contains(conn, "?") {
			h := sha256.Sum256([]byte(conn))
			return filepath.Join(baseDir, "checkpoints", "sqlite-"+hex.EncodeToString(h[:8])+".json")
		}
		return conn + ".checkpoints.json"
	}

	if conn == "" {
		return filepath.Join(baseDir, "checkpoints", "postgres-checkpoints.json")
	}
	h := sha256.Sum256([]byte(conn))
	return filepath.Join(baseDir, "checkpoints", "postgres-"+hex.EncodeToString(h[:8])+".json")
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

type fileCheckPointStore struct {
	mu     sync.RWMutex
	path   string
	loaded bool
	data   map[string][]byte
}

func newFileCheckPointStore(path string) *fileCheckPointStore {
	return &fileCheckPointStore{path: path, data: make(map[string][]byte)}
}

func (s *fileCheckPointStore) Get(_ context.Context, checkPointID string) ([]byte, bool, error) {
	s.mu.RLock()
	if s.loaded {
		v, ok := s.data[checkPointID]
		s.mu.RUnlock()
		if !ok {
			return nil, false, nil
		}
		out := make([]byte, len(v))
		copy(out, v)
		return out, true, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, false, err
	}
	v, ok := s.data[checkPointID]
	if !ok {
		return nil, false, nil
	}
	out := make([]byte, len(v))
	copy(out, v)
	return out, true, nil
}

func (s *fileCheckPointStore) Set(_ context.Context, checkPointID string, checkPoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	v := make([]byte, len(checkPoint))
	copy(v, checkPoint)
	s.data[checkPointID] = v
	return s.persistLocked()
}

func (s *fileCheckPointStore) loadLocked() error {
	if s.loaded {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.loaded = true
			return nil
		}
		return fmt.Errorf("checkpoint store read failed: %w", err)
	}

	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("checkpoint store unmarshal failed: %w", err)
	}

	s.data = make(map[string][]byte, len(raw))
	for k, v := range raw {
		decoded, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return fmt.Errorf("checkpoint store decode failed for %s: %w", k, err)
		}
		s.data[k] = decoded
	}
	s.loaded = true
	return nil
}

func (s *fileCheckPointStore) persistLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("checkpoint store mkdir failed: %w", err)
	}

	raw := make(map[string]string, len(s.data))
	for k, v := range s.data {
		raw[k] = base64.StdEncoding.EncodeToString(v)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("checkpoint store marshal failed: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("checkpoint store write tmp failed: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("checkpoint store rename failed: %w", err)
	}
	return nil
}
