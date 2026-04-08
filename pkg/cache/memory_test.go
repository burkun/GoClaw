package cache

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestNewMemoryCache(t *testing.T) {
	tests := []struct {
		name     string
		maxItems int
		expected int
	}{
		{"default_max_items", 0, 10000},
		{"negative_max_items", -1, 10000},
		{"custom_max_items", 100, 100},
		{"large_max_items", 1000000, 1000000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewMemoryCache(tt.maxItems)
			if cache == nil {
				t.Fatal("expected cache to be created, got nil")
			}
			if cache.maxItems != tt.expected {
				t.Errorf("expected maxItems=%d, got %d", tt.expected, cache.maxItems)
			}
		})
	}
}

func TestMemoryCache_Get_Set(t *testing.T) {
	cache := NewMemoryCache(100)
	ctx := context.Background()

	tests := []struct {
		name  string
		key   string
		value interface{}
		ttl   time.Duration
	}{
		{"string_value", "key1", "value1", 0},
		{"int_value", "key2", 42, 0},
		{"map_value", "key3", map[string]string{"foo": "bar"}, 0},
		{"slice_value", "key4", []int{1, 2, 3}, 0},
		{"with_ttl", "key5", "value5", 1 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cache.Set(ctx, tt.key, tt.value, tt.ttl)
			if err != nil {
				t.Fatalf("failed to set cache: %v", err)
			}

			val, err := cache.Get(ctx, tt.key)
			if err != nil {
				t.Fatalf("failed to get cache: %v", err)
			}

			if !reflect.DeepEqual(val, tt.value) {
				t.Errorf("expected value=%v, got %v", tt.value, val)
			}
		})
	}
}

func TestMemoryCache_Get_NotFound(t *testing.T) {
	cache := NewMemoryCache(100)
	ctx := context.Background()

	_, err := cache.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent key, got nil")
	}

	if !IsCacheError(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryCache_Get_Expired(t *testing.T) {
	cache := NewMemoryCache(100)
	ctx := context.Background()

	// Set with very short TTL
	err := cache.Set(ctx, "expiring_key", "value", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	_, err = cache.Get(ctx, "expiring_key")
	if err == nil {
		t.Fatal("expected error for expired key, got nil")
	}

	if !IsCacheError(err, ErrExpired) {
		t.Errorf("expected ErrExpired, got %v", err)
	}
}

func TestMemoryCache_Delete(t *testing.T) {
	cache := NewMemoryCache(100)
	ctx := context.Background()

	// Set a value
	err := cache.Set(ctx, "key1", "value1", 0)
	if err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// Delete it
	err = cache.Delete(ctx, "key1")
	if err != nil {
		t.Fatalf("failed to delete cache: %v", err)
	}

	// Verify it's deleted
	_, err = cache.Get(ctx, "key1")
	if !IsCacheError(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestMemoryCache_Exists(t *testing.T) {
	cache := NewMemoryCache(100)
	ctx := context.Background()

	// Test nonexistent key
	exists, err := cache.Exists(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected nonexistent key to not exist")
	}

	// Set a value
	err = cache.Set(ctx, "key1", "value1", 0)
	if err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// Test existing key
	exists, err = cache.Exists(ctx, "key1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected key to exist")
	}

	// Set with TTL
	err = cache.Set(ctx, "expiring", "value", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Test expired key
	exists, err = cache.Exists(ctx, "expiring")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected expired key to not exist")
	}
}

func TestMemoryCache_Clear(t *testing.T) {
	cache := NewMemoryCache(100)
	ctx := context.Background()

	// Set multiple values
	for i := 0; i < 10; i++ {
		err := cache.Set(ctx, string(rune('a'+i)), i, 0)
		if err != nil {
			t.Fatalf("failed to set cache: %v", err)
		}
	}

	// Clear cache
	err := cache.Clear(ctx)
	if err != nil {
		t.Fatalf("failed to clear cache: %v", err)
	}

	// Verify all are cleared
	for i := 0; i < 10; i++ {
		_, err := cache.Get(ctx, string(rune('a'+i)))
		if !IsCacheError(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound after clear, got %v", err)
		}
	}

	// Verify stats are reset (MissCount will be 10 from the Get calls above)
	stats, err := cache.Stats(ctx)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}
	if stats.HitCount != 0 || stats.EvictionCount != 0 {
		t.Errorf("expected HitCount and EvictionCount to be reset, got %+v", stats)
	}
}

func TestMemoryCache_Stats(t *testing.T) {
	cache := NewMemoryCache(100)
	ctx := context.Background()

	// Set some values
	_ = cache.Set(ctx, "key1", "value1", 0)
	_ = cache.Set(ctx, "key2", "value2", 0)

	// Get existing values (hits)
	_, _ = cache.Get(ctx, "key1")
	_, _ = cache.Get(ctx, "key2")

	// Get nonexistent value (miss)
	_, _ = cache.Get(ctx, "nonexistent")

	stats, err := cache.Stats(ctx)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if stats.TotalItems != 2 {
		t.Errorf("expected TotalItems=2, got %d", stats.TotalItems)
	}

	if stats.HitCount != 2 {
		t.Errorf("expected HitCount=2, got %d", stats.HitCount)
	}

	if stats.MissCount != 1 {
		t.Errorf("expected MissCount=1, got %d", stats.MissCount)
	}

	expectedHitRate := float64(2) / float64(3)
	if stats.HitRate < expectedHitRate-0.01 || stats.HitRate > expectedHitRate+0.01 {
		t.Errorf("expected HitRate≈%.2f, got %.2f", expectedHitRate, stats.HitRate)
	}
}

func TestMemoryCache_Eviction(t *testing.T) {
	// Small cache to trigger eviction
	cache := NewMemoryCache(5)
	ctx := context.Background()

	// Track evictions
	var evictedKeys []string
	cache.SetOnEvict(func(key string, value interface{}) {
		evictedKeys = append(evictedKeys, key)
	})

	// Add more items than maxItems
	for i := 0; i < 10; i++ {
		err := cache.Set(ctx, string(rune('a'+i)), i, 0)
		if err != nil {
			t.Fatalf("failed to set cache: %v", err)
		}
	}

	// Verify eviction happened
	if len(evictedKeys) == 0 {
		t.Error("expected some evictions to occur")
	}

	// Verify cache size doesn't exceed maxItems
	stats, _ := cache.Stats(ctx)
	if stats.TotalItems > 5 {
		t.Errorf("expected TotalItems<=5, got %d", stats.TotalItems)
	}
}

func TestMemoryCache_LFUEviction(t *testing.T) {
	cache := NewMemoryCache(3)
	ctx := context.Background()

	// Add 3 items
	_ = cache.Set(ctx, "a", 1, 0)
	_ = cache.Set(ctx, "b", 2, 0)
	_ = cache.Set(ctx, "c", 3, 0)

	// Access 'a' multiple times to increase its hit count
	for i := 0; i < 5; i++ {
		_, _ = cache.Get(ctx, "a")
	}

	// Access 'b' a few times
	for i := 0; i < 2; i++ {
		_, _ = cache.Get(ctx, "b")
	}

	// 'c' has 0 hits, 'b' has 2 hits, 'a' has 5 hits
	// When we add a new item, 'c' should be evicted (LFU)

	var evictedKey string
	cache.SetOnEvict(func(key string, value interface{}) {
		evictedKey = key
	})

	_ = cache.Set(ctx, "d", 4, 0)

	// 'c' should be evicted because it has the lowest hit count
	if evictedKey != "c" {
		t.Errorf("expected 'c' to be evicted (lowest hit count), got '%s'", evictedKey)
	}
}

func TestMemoryCache_Concurrent(t *testing.T) {
	cache := NewMemoryCache(1000)
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 10
	numOps := 100

	// Concurrent writes
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				key := string(rune('a' + id))
				_ = cache.Set(ctx, key, id*numOps+j, 0)
			}
		}(i)
	}

	// Concurrent reads
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				key := string(rune('a' + id))
				_, _ = cache.Get(ctx, key)
			}
		}(i)
	}

	wg.Wait()

	// Verify cache is still functional
	_ = cache.Set(ctx, "test", "value", 0)
	val, err := cache.Get(ctx, "test")
	if err != nil || val != "value" {
		t.Errorf("cache not functional after concurrent operations")
	}
}

func TestMemoryCache_OnEvict(t *testing.T) {
	cache := NewMemoryCache(2)
	ctx := context.Background()

	evictedItems := make(map[string]interface{})
	cache.SetOnEvict(func(key string, value interface{}) {
		evictedItems[key] = value
	})

	_ = cache.Set(ctx, "a", 1, 0)
	_ = cache.Set(ctx, "b", 2, 0)
	_ = cache.Set(ctx, "c", 3, 0) // Should trigger eviction

	// Verify eviction callback was called
	if len(evictedItems) == 0 {
		t.Error("expected eviction callback to be called")
	}
}

// Benchmark tests

func BenchmarkMemoryCache_Set(b *testing.B) {
	cache := NewMemoryCache(10000)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.Set(ctx, string(rune(i)), i, 0)
	}
}

func BenchmarkMemoryCache_Get(b *testing.B) {
	cache := NewMemoryCache(10000)
	ctx := context.Background()

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		_ = cache.Set(ctx, string(rune(i)), i, 0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(ctx, string(rune(i%1000)))
	}
}

func BenchmarkMemoryCache_Concurrent(b *testing.B) {
	cache := NewMemoryCache(10000)
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := string(rune(i % 100))
			if i%2 == 0 {
				_ = cache.Set(ctx, key, i, 0)
			} else {
				_, _ = cache.Get(ctx, key)
			}
			i++
		}
	})
}

// Helper function to check cache error type
func IsCacheError(err error, target *CacheError) bool {
	if err == nil {
		return false
	}
	cacheErr, ok := err.(*CacheError)
	if !ok {
		return false
	}
	return cacheErr.Code == target.Code
}
