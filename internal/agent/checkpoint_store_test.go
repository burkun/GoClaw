package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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

func TestNewCheckPointStore_Postgres_Persistent(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	dsn := "postgres://user:pass@localhost:5432/app"
	store, err := newCheckPointStore(&config.AppConfig{
		Checkpointer: &config.CheckpointerConfig{Type: "postgres", ConnectionString: dsn},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx := context.Background()
	if err := store.Set(ctx, "cp-pg", []byte("state-b")); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	store2, err := newCheckPointStore(&config.AppConfig{
		Checkpointer: &config.CheckpointerConfig{Type: "postgres", ConnectionString: dsn},
	})
	if err != nil {
		t.Fatalf("unexpected error creating second store: %v", err)
	}
	got, ok, err := store2.Get(ctx, "cp-pg")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !ok || string(got) != "state-b" {
		t.Fatalf("unexpected persistent get result ok=%v value=%q", ok, string(got))
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
