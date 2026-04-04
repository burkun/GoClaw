package agent

import (
	"context"
	"database/sql"
	"path/filepath"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	_ "github.com/mattn/go-sqlite3"

	"github.com/bookerbai/goclaw/internal/config"
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
