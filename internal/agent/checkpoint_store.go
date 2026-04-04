package agent

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"

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
	case "sqlite":
		path := checkpointStorePath(cp, typ)
		return newSQLiteCheckPointStore(path)
	case "postgres":
		conn := checkpointStorePath(cp, typ)
		return newPostgresCheckPointStore(conn)
	default:
		return nil, fmt.Errorf("agent.New: unknown checkpointer type %q", cp.Type)
	}
}

func checkpointStorePath(cp *config.CheckpointerConfig, typ string) string {
	conn := strings.TrimSpace(cp.ConnectionString)
	baseDir := ".goclaw"

	if typ == "sqlite" {
		if conn == "" {
			return filepath.Join(baseDir, "checkpoints", "sqlite-checkpoints.db")
		}
		return conn
	}

	return conn
}

type sqliteCheckPointStore struct {
	db *sql.DB
}

func newSQLiteCheckPointStore(path string) (*sqliteCheckPointStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("checkpoint sqlite path is required")
	}
	if !strings.HasPrefix(strings.ToLower(path), "file:") && !strings.Contains(path, ":memory:") {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("checkpoint sqlite mkdir failed: %w", err)
		}
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("checkpoint sqlite open failed: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS checkpoints (
	id TEXT PRIMARY KEY,
	payload BLOB NOT NULL,
	updated_at INTEGER NOT NULL
);
`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("checkpoint sqlite init schema failed: %w", err)
	}
	return &sqliteCheckPointStore{db: db}, nil
}

func (s *sqliteCheckPointStore) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, fmt.Errorf("checkpoint sqlite store is not initialized")
	}
	var payload []byte
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM checkpoints WHERE id = ?", checkPointID).Scan(&payload)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("checkpoint sqlite get failed: %w", err)
	}
	out := make([]byte, len(payload))
	copy(out, payload)
	return out, true, nil
}

func (s *sqliteCheckPointStore) Set(ctx context.Context, checkPointID string, checkPoint []byte) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("checkpoint sqlite store is not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO checkpoints(id, payload, updated_at)
VALUES (?, ?, strftime('%s','now'))
ON CONFLICT(id) DO UPDATE SET
	payload = excluded.payload,
	updated_at = excluded.updated_at;
`, checkPointID, checkPoint)
	if err != nil {
		return fmt.Errorf("checkpoint sqlite set failed: %w", err)
	}
	return nil
}

type postgresCheckPointStore struct {
	db *sql.DB
}

func newPostgresCheckPointStore(connectionString string) (*postgresCheckPointStore, error) {
	conn := strings.TrimSpace(connectionString)
	if conn == "" {
		return nil, fmt.Errorf("checkpoint postgres connection_string is required")
	}
	db, err := sql.Open("pgx", conn)
	if err != nil {
		return nil, fmt.Errorf("checkpoint postgres open failed: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS goclaw_checkpoints (
	id TEXT PRIMARY KEY,
	payload BYTEA NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("checkpoint postgres init schema failed: %w", err)
	}
	return &postgresCheckPointStore{db: db}, nil
}

func (s *postgresCheckPointStore) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, fmt.Errorf("checkpoint postgres store is not initialized")
	}
	var payload []byte
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM goclaw_checkpoints WHERE id = $1", checkPointID).Scan(&payload)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("checkpoint postgres get failed: %w", err)
	}
	out := make([]byte, len(payload))
	copy(out, payload)
	return out, true, nil
}

func (s *postgresCheckPointStore) Set(ctx context.Context, checkPointID string, checkPoint []byte) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("checkpoint postgres store is not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO goclaw_checkpoints(id, payload, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT(id) DO UPDATE SET
	payload = EXCLUDED.payload,
	updated_at = NOW();
`, checkPointID, checkPoint)
	if err != nil {
		return fmt.Errorf("checkpoint postgres set failed: %w", err)
	}
	return nil
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
