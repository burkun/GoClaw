package agent

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	_ "github.com/mattn/go-sqlite3"

	"goclaw/internal/config"
)

func TestNewCheckPointStore_NilConfig(t *testing.T) {
	store, err := newCheckPointStore(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store != nil {
		t.Fatalf("expected nil store when config is nil")
	}
}

func TestNewCheckPointStore_Memory(t *testing.T) {
	store, err := newCheckPointStore(&config.AppConfig{
		Checkpointer: &config.CheckpointerConfig{Type: "memory"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store == nil {
		t.Fatalf("expected non-nil memory store")
	}

	ctx := context.Background()
	if err := store.Set(ctx, "cp-1", []byte("abc")); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	got, ok, err := store.Get(ctx, "cp-1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !ok || string(got) != "abc" {
		t.Fatalf("unexpected get result ok=%v value=%q", ok, string(got))
	}
}

func TestNewCheckPointStore_SQLite_Persistent(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "checkpoints.db")

	store, err := newCheckPointStore(&config.AppConfig{
		Checkpointer: &config.CheckpointerConfig{Type: "sqlite", ConnectionString: path},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx := context.Background()
	if err := store.Set(ctx, "cp-sqlite", []byte("state-a")); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open sqlite db failed: %v", err)
	}
	defer db.Close()
	var rowCount int
	if err := db.QueryRow("SELECT COUNT(1) FROM checkpoints WHERE id = ?", "cp-sqlite").Scan(&rowCount); err != nil {
		t.Fatalf("query sqlite row failed: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected rowCount=1, got %d", rowCount)
	}

	store2, err := newCheckPointStore(&config.AppConfig{
		Checkpointer: &config.CheckpointerConfig{Type: "sqlite", ConnectionString: path},
	})
	if err != nil {
		t.Fatalf("unexpected error creating second store: %v", err)
	}
	got, ok, err := store2.Get(ctx, "cp-sqlite")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !ok || string(got) != "state-a" {
		t.Fatalf("unexpected persistent get result ok=%v value=%q", ok, string(got))
	}
}

func TestNewCheckPointStore_Postgres_RequiresConnectionString(t *testing.T) {
	_, err := newCheckPointStore(&config.AppConfig{
		Checkpointer: &config.CheckpointerConfig{Type: "postgres", ConnectionString: ""},
	})
	if err == nil {
		t.Fatalf("expected error when postgres connection_string is empty")
	}
}

func TestPostgresCheckPointStore_SetGet_SQLMock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new failed: %v", err)
	}
	defer db.Close()

	store := &postgresCheckPointStore{db: db}
	ctx := context.Background()
	payload := []byte("state-b")

	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO goclaw_checkpoints(id, payload, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT(id) DO UPDATE SET
	payload = EXCLUDED.payload,
	updated_at = NOW();
`)).
		WithArgs("cp-pg", payload).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := store.Set(ctx, "cp-pg", payload); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT payload FROM goclaw_checkpoints WHERE id = $1")).
		WithArgs("cp-pg").
		WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(payload))

	got, ok, err := store.Get(ctx, "cp-pg")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !ok || string(got) != "state-b" {
		t.Fatalf("unexpected get result ok=%v value=%q", ok, string(got))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}
}

func TestNewCheckPointStore_Unsupported(t *testing.T) {
	_, err := newCheckPointStore(&config.AppConfig{
		Checkpointer: &config.CheckpointerConfig{Type: "redis", ConnectionString: "redis://127.0.0.1:6379/0"},
	})
	if err == nil {
		t.Fatalf("expected error for unsupported checkpointer type")
	}
}

// --- Tests for fileCheckPointStore ---

func TestNewFileCheckPointStore(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "checkpoints.json")
	
	store := newFileCheckPointStore(path)
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if store.path != path {
		t.Errorf("expected path %s, got %s", path, store.path)
	}
	if store.data == nil {
		t.Error("expected initialized data map")
	}
}

func TestFileCheckPointStore_Get_FileNotExist(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "nonexistent.json")
	
	store := newFileCheckPointStore(path)
	ctx := context.Background()
	
	// Get should return not found when file doesn't exist
	got, ok, err := store.Get(ctx, "cp-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false when checkpoint doesn't exist")
	}
	if got != nil {
		t.Error("expected nil data when checkpoint doesn't exist")
	}
}

func TestFileCheckPointStore_SetAndGet(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "checkpoints.json")
	
	store := newFileCheckPointStore(path)
	ctx := context.Background()
	
	// Set a checkpoint
	payload := []byte("test-state-data")
	if err := store.Set(ctx, "cp-1", payload); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	
	// Verify file was created
	if _, err := filepath.Glob(path); err != nil {
		t.Errorf("expected file to be created at %s", path)
	}
	
	// Get the checkpoint
	got, ok, err := store.Get(ctx, "cp-1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !ok {
		t.Error("expected ok=true when checkpoint exists")
	}
	if string(got) != "test-state-data" {
		t.Errorf("expected 'test-state-data', got %s", string(got))
	}
}

func TestFileCheckPointStore_MultipleCheckpoints(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "checkpoints.json")
	
	store := newFileCheckPointStore(path)
	ctx := context.Background()
	
	// Set multiple checkpoints
	payloads := map[string][]byte{
		"cp-1": []byte("state-1"),
		"cp-2": []byte("state-2"),
		"cp-3": []byte("state-3"),
	}
	
	for id, payload := range payloads {
		if err := store.Set(ctx, id, payload); err != nil {
			t.Fatalf("set %s failed: %v", id, err)
		}
	}
	
	// Get all checkpoints
	for id, expected := range payloads {
		got, ok, err := store.Get(ctx, id)
		if err != nil {
			t.Fatalf("get %s failed: %v", id, err)
		}
		if !ok {
			t.Errorf("expected checkpoint %s to exist", id)
		}
		if string(got) != string(expected) {
			t.Errorf("checkpoint %s: expected %s, got %s", id, string(expected), string(got))
		}
	}
}

func TestFileCheckPointStore_PersistAndReload(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "checkpoints.json")
	
	// First store - set data
	store1 := newFileCheckPointStore(path)
	ctx := context.Background()
	
	if err := store1.Set(ctx, "cp-persist", []byte("persistent-state")); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	
	// Second store - reload from file
	store2 := newFileCheckPointStore(path)
	
	got, ok, err := store2.Get(ctx, "cp-persist")
	if err != nil {
		t.Fatalf("get from reloaded store failed: %v", err)
	}
	if !ok {
		t.Error("expected checkpoint to persist across store instances")
	}
	if string(got) != "persistent-state" {
		t.Errorf("expected 'persistent-state', got %s", string(got))
	}
}

func TestFileCheckPointStore_UpdateExisting(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "checkpoints.json")
	
	store := newFileCheckPointStore(path)
	ctx := context.Background()
	
	// Set initial value
	if err := store.Set(ctx, "cp-update", []byte("initial")); err != nil {
		t.Fatalf("initial set failed: %v", err)
	}
	
	// Update value
	if err := store.Set(ctx, "cp-update", []byte("updated")); err != nil {
		t.Fatalf("update set failed: %v", err)
	}
	
	// Verify updated value
	got, ok, err := store.Get(ctx, "cp-update")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !ok {
		t.Error("expected checkpoint to exist")
	}
	if string(got) != "updated" {
		t.Errorf("expected 'updated', got %s", string(got))
	}
}

func TestFileCheckPointStore_EmptyPayload(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "checkpoints.json")
	
	store := newFileCheckPointStore(path)
	ctx := context.Background()
	
	// Set empty payload
	if err := store.Set(ctx, "cp-empty", []byte{}); err != nil {
		t.Fatalf("set empty payload failed: %v", err)
	}
	
	// Get empty payload
	got, ok, err := store.Get(ctx, "cp-empty")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !ok {
		t.Error("expected checkpoint to exist")
	}
	if len(got) != 0 {
		t.Errorf("expected empty payload, got %v", got)
	}
}

func TestFileCheckPointStore_BinaryPayload(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "checkpoints.json")
	
	store := newFileCheckPointStore(path)
	ctx := context.Background()
	
	// Set binary payload with all byte values
	binaryPayload := make([]byte, 256)
	for i := 0; i < 256; i++ {
		binaryPayload[i] = byte(i)
	}
	
	if err := store.Set(ctx, "cp-binary", binaryPayload); err != nil {
		t.Fatalf("set binary payload failed: %v", err)
	}
	
	// Get binary payload
	got, ok, err := store.Get(ctx, "cp-binary")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !ok {
		t.Error("expected checkpoint to exist")
	}
	if len(got) != len(binaryPayload) {
		t.Errorf("expected length %d, got %d", len(binaryPayload), len(got))
	}
	for i := range binaryPayload {
		if got[i] != binaryPayload[i] {
			t.Errorf("byte mismatch at index %d: expected %d, got %d", i, binaryPayload[i], got[i])
		}
	}
}

func TestFileCheckPointStore_ConcurrentAccess(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "checkpoints.json")
	
	store := newFileCheckPointStore(path)
	ctx := context.Background()
	
	// Concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()
			cpID := fmt.Sprintf("cp-concurrent-%d", id)
			payload := []byte(fmt.Sprintf("state-%d", id))
			if err := store.Set(ctx, cpID, payload); err != nil {
				t.Errorf("concurrent set %d failed: %v", id, err)
				return
			}
		}(i)
	}
	
	// Wait for all writes
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for concurrent writes")
		}
	}
	
	// Verify all checkpoints
	for i := 0; i < 10; i++ {
		cpID := fmt.Sprintf("cp-concurrent-%d", i)
		expected := fmt.Sprintf("state-%d", i)
		got, ok, err := store.Get(ctx, cpID)
		if err != nil {
			t.Errorf("get %s failed: %v", cpID, err)
			continue
		}
		if !ok {
			t.Errorf("expected checkpoint %s to exist", cpID)
			continue
		}
		if string(got) != expected {
			t.Errorf("checkpoint %s: expected %s, got %s", cpID, expected, string(got))
		}
	}
}

func TestFileCheckPointStore_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "invalid.json")
	
	// Write invalid JSON
	if err := os.WriteFile(path, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	
	store := newFileCheckPointStore(path)
	ctx := context.Background()
	
	// Get should return error for invalid JSON
	_, _, err := store.Get(ctx, "cp-1")
	if err == nil {
		t.Error("expected error for invalid JSON file")
	}
}

func TestFileCheckPointStore_CorruptBase64(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "corrupt.json")
	
	// Write JSON with corrupt base64
	corruptJSON := `{"cp-1": "!!!invalid-base64!!!"}`
	if err := os.WriteFile(path, []byte(corruptJSON), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	
	store := newFileCheckPointStore(path)
	ctx := context.Background()
	
	// Get should return error for corrupt base64
	_, _, err := store.Get(ctx, "cp-1")
	if err == nil {
		t.Error("expected error for corrupt base64")
	}
}

func TestFileCheckPointStore_NilCheck(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "checkpoints.json")
	
	store := newFileCheckPointStore(path)
	ctx := context.Background()
	
	// Test Get for non-existent key
	got, ok, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for nonexistent key")
	}
	if got != nil {
		t.Error("expected nil for nonexistent key")
	}
}
