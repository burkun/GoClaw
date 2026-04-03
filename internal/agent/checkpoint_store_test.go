package agent

import (
	"context"
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

func TestNewCheckPointStore_Unsupported(t *testing.T) {
	_, err := newCheckPointStore(&config.AppConfig{
		Checkpointer: &config.CheckpointerConfig{Type: "sqlite", ConnectionString: "tmp.db"},
	})
	if err == nil {
		t.Fatalf("expected error for unsupported sqlite checkpointer")
	}
}
