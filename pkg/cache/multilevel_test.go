package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewMultiLevelCache(t *testing.T) {
	l1 := NewMemoryCache(100)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-test-1"), 10*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-test-1"))

	tests := []struct {
		name       string
		l1         Cache
		l2         Cache
		writeLevel int
		expected   int
	}{
		{"write_to_l1_only", l1, l2, 1, 1},
		{"write_to_both", l1, l2, 2, 2},
		{"default_write_level", l1, l2, 0, 2},
		{"invalid_write_level", l1, l2, 5, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewMultiLevelCache(tt.l1, tt.l2, tt.writeLevel)
			if cache == nil {
				t.Fatal("expected cache to be created, got nil")
			}
			if cache.writeLevel != tt.expected {
				t.Errorf("expected writeLevel=%d, got %d", tt.expected, cache.writeLevel)
			}
		})
	}
}

func TestMultiLevelCache_Get_L1Hit(t *testing.T) {
	l1 := NewMemoryCache(100)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-test-2"), 10*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-test-2"))

	cache := NewMultiLevelCache(l1, l2, 2)
	ctx := context.Background()

	// Set value in L1 only
	_ = l1.Set(ctx, "key1", "value1", 0)

	// Get should hit L1
	val, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("failed to get cache: %v", err)
	}

	if val != "value1" {
		t.Errorf("expected value1, got %v", val)
	}

	// Verify L1 hit count
	stats, _ := cache.Stats(ctx)
	if stats.HitCount != 1 {
		t.Errorf("expected HitCount=1, got %d", stats.HitCount)
	}
}

func TestMultiLevelCache_Get_L2Hit_Backfill(t *testing.T) {
	l1 := NewMemoryCache(100)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-test-3"), 10*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-test-3"))

	cache := NewMultiLevelCache(l1, l2, 2)
	ctx := context.Background()

	// Set value in L2 only
	_ = l2.Set(ctx, "key1", "value1", 0)

	// Get should hit L2 and backfill to L1
	val, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("failed to get cache: %v", err)
	}

	if val != "value1" {
		t.Errorf("expected value1, got %v", val)
	}

	// Verify L2 hit count increased
	if cache.stats.l2HitCount != 1 {
		t.Errorf("expected l2HitCount=1, got %d", cache.stats.l2HitCount)
	}

	// Verify backfill to L1
	exists, _ := l1.Exists(ctx, "key1")
	if !exists {
		t.Error("expected key to be backfilled to L1")
	}
}

func TestMultiLevelCache_Get_Miss(t *testing.T) {
	l1 := NewMemoryCache(100)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-test-4"), 10*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-test-4"))

	cache := NewMultiLevelCache(l1, l2, 2)
	ctx := context.Background()

	// Get nonexistent key
	_, err := cache.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}

	if !IsCacheError(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Verify miss count
	if cache.stats.missCount != 1 {
		t.Errorf("expected missCount=1, got %d", cache.stats.missCount)
	}
}

func TestMultiLevelCache_Set_WriteLevel1(t *testing.T) {
	l1 := NewMemoryCache(100)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-test-5"), 10*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-test-5"))

	cache := NewMultiLevelCache(l1, l2, 1) // Write to L1 only
	ctx := context.Background()

	err := cache.Set(ctx, "key1", "value1", 0)
	if err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// Verify L1 has the value
	l1Val, err := l1.Get(ctx, "key1")
	if err != nil || l1Val != "value1" {
		t.Error("expected value in L1")
	}

	// Verify L2 does NOT have the value
	_, err = l2.Get(ctx, "key1")
	if !IsCacheError(err, ErrNotFound) {
		t.Error("expected L2 to NOT have the value when writeLevel=1")
	}
}

func TestMultiLevelCache_Set_WriteLevel2(t *testing.T) {
	l1 := NewMemoryCache(100)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-test-6"), 10*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-test-6"))

	cache := NewMultiLevelCache(l1, l2, 2) // Write to both
	ctx := context.Background()

	err := cache.Set(ctx, "key1", "value1", 0)
	if err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// Verify L1 has the value
	l1Val, err := l1.Get(ctx, "key1")
	if err != nil || l1Val != "value1" {
		t.Error("expected value in L1")
	}

	// Verify L2 also has the value
	l2Val, err := l2.Get(ctx, "key1")
	if err != nil || l2Val != "value1" {
		t.Error("expected value in L2 when writeLevel=2")
	}
}

func TestMultiLevelCache_Delete(t *testing.T) {
	l1 := NewMemoryCache(100)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-test-7"), 10*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-test-7"))

	cache := NewMultiLevelCache(l1, l2, 2)
	ctx := context.Background()

	// Set in both levels
	_ = cache.Set(ctx, "key1", "value1", 0)

	// Delete
	err := cache.Delete(ctx, "key1")
	if err != nil {
		t.Fatalf("failed to delete cache: %v", err)
	}

	// Verify deleted from L1
	_, err = l1.Get(ctx, "key1")
	if !IsCacheError(err, ErrNotFound) {
		t.Error("expected key to be deleted from L1")
	}

	// Verify deleted from L2
	_, err = l2.Get(ctx, "key1")
	if !IsCacheError(err, ErrNotFound) {
		t.Error("expected key to be deleted from L2")
	}
}

func TestMultiLevelCache_Exists(t *testing.T) {
	l1 := NewMemoryCache(100)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-test-8"), 10*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-test-8"))

	cache := NewMultiLevelCache(l1, l2, 2)
	ctx := context.Background()

	// Test nonexistent key
	exists, err := cache.Exists(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected nonexistent key to not exist")
	}

	// Set in L1 only
	_ = l1.Set(ctx, "l1_key", "value1", 0)
	exists, _ = cache.Exists(ctx, "l1_key")
	if !exists {
		t.Error("expected L1 key to exist")
	}

	// Set in L2 only
	_ = l2.Set(ctx, "l2_key", "value2", 0)
	exists, _ = cache.Exists(ctx, "l2_key")
	if !exists {
		t.Error("expected L2 key to exist")
	}
}

func TestMultiLevelCache_Clear(t *testing.T) {
	l1 := NewMemoryCache(100)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-test-9"), 10*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-test-9"))

	cache := NewMultiLevelCache(l1, l2, 2)
	ctx := context.Background()

	// Set multiple values
	_ = cache.Set(ctx, "key1", "value1", 0)
	_ = cache.Set(ctx, "key2", "value2", 0)

	// Clear
	err := cache.Clear(ctx)
	if err != nil {
		t.Fatalf("failed to clear cache: %v", err)
	}

	// Verify L1 is cleared
	l1Stats, _ := l1.Stats(ctx)
	if l1Stats.TotalItems != 0 {
		t.Error("expected L1 to be cleared")
	}

	// Verify L2 is cleared
	l2Stats, _ := l2.Stats(ctx)
	if l2Stats.TotalItems != 0 {
		t.Error("expected L2 to be cleared")
	}

	// Verify stats are reset
	if cache.stats.l1HitCount != 0 || cache.stats.l2HitCount != 0 || cache.stats.missCount != 0 {
		t.Error("expected stats to be reset")
	}
}

func TestMultiLevelCache_Stats(t *testing.T) {
	l1 := NewMemoryCache(100)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-test-10"), 10*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-test-10"))

	cache := NewMultiLevelCache(l1, l2, 2)
	ctx := context.Background()

	// Set values
	_ = cache.Set(ctx, "key1", "value1", 0)
	_ = cache.Set(ctx, "key2", "value2", 0)

	// Get L1 hits
	_, _ = cache.Get(ctx, "key1")
	_, _ = cache.Get(ctx, "key2")

	// Get L2 hit (after clearing L1)
	_ = l1.Delete(ctx, "key1")
	_, _ = cache.Get(ctx, "key1")

	// Get miss
	_, _ = cache.Get(ctx, "nonexistent")

	stats, err := cache.Stats(ctx)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	// Verify aggregated stats
	if stats.HitCount != 3 { // 2 L1 hits + 1 L2 hit
		t.Errorf("expected HitCount=3, got %d", stats.HitCount)
	}

	if stats.MissCount != 1 {
		t.Errorf("expected MissCount=1, got %d", stats.MissCount)
	}
}

func TestMultiLevelCache_GetL1Stats_GetL2Stats(t *testing.T) {
	l1 := NewMemoryCache(100)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-test-11"), 10*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-test-11"))

	cache := NewMultiLevelCache(l1, l2, 2)
	ctx := context.Background()

	// Set some values
	_ = cache.Set(ctx, "key1", "value1", 0)

	l1Stats, err := cache.GetL1Stats(ctx)
	if err != nil {
		t.Fatalf("failed to get L1 stats: %v", err)
	}
	if l1Stats == nil {
		t.Fatal("expected L1 stats, got nil")
	}

	l2Stats, err := cache.GetL2Stats(ctx)
	if err != nil {
		t.Fatalf("failed to get L2 stats: %v", err)
	}
	if l2Stats == nil {
		t.Fatal("expected L2 stats, got nil")
	}
}

func TestMultiLevelCache_Refresh(t *testing.T) {
	l1 := NewMemoryCache(100)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-test-12"), 10*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-test-12"))

	cache := NewMultiLevelCache(l1, l2, 2)
	ctx := context.Background()

	// Set value in L2 only
	_ = l2.Set(ctx, "key1", "value1", 0)

	// Refresh should load from L2 to L1
	err := cache.Refresh(ctx, "key1")
	if err != nil {
		t.Fatalf("failed to refresh cache: %v", err)
	}

	// Verify L1 now has the value
	exists, _ := l1.Exists(ctx, "key1")
	if !exists {
		t.Error("expected value to be refreshed to L1")
	}
}

func TestMultiLevelCache_Refresh_NoL2(t *testing.T) {
	l1 := NewMemoryCache(100)

	cache := NewMultiLevelCache(l1, nil, 1)
	ctx := context.Background()

	// Refresh with no L2 should fail
	err := cache.Refresh(ctx, "key1")
	if err == nil {
		t.Fatal("expected error when refreshing with no L2 cache")
	}
}

func TestMultiLevelCache_NoL2Cache(t *testing.T) {
	l1 := NewMemoryCache(100)

	cache := NewMultiLevelCache(l1, nil, 2)
	ctx := context.Background()

	// Set should work even without L2
	err := cache.Set(ctx, "key1", "value1", 0)
	if err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// Get should hit L1
	val, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("failed to get cache: %v", err)
	}
	if val != "value1" {
		t.Errorf("expected value1, got %v", val)
	}

	// Stats should work
	stats, err := cache.Stats(ctx)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}
	if stats.TotalItems != 1 {
		t.Errorf("expected TotalItems=1, got %d", stats.TotalItems)
	}
}

func TestMultiLevelCache_TTL(t *testing.T) {
	l1 := NewMemoryCache(100)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-test-13"), 10*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-test-13"))

	cache := NewMultiLevelCache(l1, l2, 2)
	ctx := context.Background()

	// Set with short TTL
	err := cache.Set(ctx, "expiring", "value", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// Wait for L1 to expire
	time.Sleep(100 * time.Millisecond)

	// Get should hit L2 (L1 expired)
	val, err := cache.Get(ctx, "expiring")
	if err != nil {
		// L2 might also expire, which is acceptable
		if IsCacheError(err, ErrExpired) || IsCacheError(err, ErrNotFound) {
			return
		}
		t.Fatalf("unexpected error: %v", err)
	}

	if val != "value" {
		t.Errorf("expected value, got %v", val)
	}
}

// Benchmark tests

func BenchmarkMultiLevelCache_Set(b *testing.B) {
	l1 := NewMemoryCache(10000)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-bench-1"), 100*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-bench-1"))

	cache := NewMultiLevelCache(l1, l2, 2)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.Set(ctx, string(rune(i%1000)), i, 0)
	}
}

func BenchmarkMultiLevelCache_Get_L1Hit(b *testing.B) {
	l1 := NewMemoryCache(10000)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-bench-2"), 100*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-bench-2"))

	cache := NewMultiLevelCache(l1, l2, 2)
	ctx := context.Background()

	// Pre-populate L1
	for i := 0; i < 1000; i++ {
		_ = l1.Set(ctx, string(rune(i)), i, 0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(ctx, string(rune(i%1000)))
	}
}

func BenchmarkMultiLevelCache_Get_L2Hit(b *testing.B) {
	l1 := NewMemoryCache(10000)
	l2, _ := NewFileCache(filepath.Join(os.TempDir(), "mlc-bench-3"), 100*1024*1024)
	defer os.RemoveAll(filepath.Join(os.TempDir(), "mlc-bench-3"))

	cache := NewMultiLevelCache(l1, l2, 2)
	ctx := context.Background()

	// Pre-populate L2 only
	for i := 0; i < 100; i++ {
		_ = l2.Set(ctx, string(rune(i)), i, 0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(ctx, string(rune(i%100)))
	}
}
