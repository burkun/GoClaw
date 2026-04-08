package threadstore

import (
	"fmt"
	"testing"
	"time"
)

// BenchmarkGet benchmarks the Get method performance.
// Expected: O(1) with hash index
func BenchmarkGet(b *testing.B) {
	// Setup: create store with different thread counts
	sizes := []int{100, 1000, 10000, 100000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("threads_%d", size), func(b *testing.B) {
			store := createStoreWithThreads(b, size)
			threadID := fmt.Sprintf("thread-%d", size/2) // Middle thread

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = store.Get(threadID)
			}
		})
	}
}

// BenchmarkSearch benchmarks the Search method performance.
// Expected: O(k) where k is result size
func BenchmarkSearch(b *testing.B) {
	// Setup: create store with different thread counts
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("threads_%d", size), func(b *testing.B) {
			store := createStoreWithThreads(b, size)

			b.Run("no_filter", func(b *testing.B) {
				query := SearchQuery{Limit: 10}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, _, _ = store.Search(query)
				}
			})

			b.Run("status_filter", func(b *testing.B) {
				query := SearchQuery{Status: "idle", Limit: 10}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, _, _ = store.Search(query)
				}
			})

			b.Run("with_pagination", func(b *testing.B) {
				query := SearchQuery{Offset: size / 2, Limit: 10}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, _, _ = store.Search(query)
				}
			})
		})
	}
}

// BenchmarkCreate benchmarks the Create method performance.
func BenchmarkCreate(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("existing_%d", size), func(b *testing.B) {
			store := createStoreWithThreads(b, size)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				threadID := fmt.Sprintf("new-thread-%d", i)
				meta := &ThreadMetadata{
					ThreadID: threadID,
					Status:   "idle",
				}
				_ = store.Create(meta)
			}
		})
	}
}

// BenchmarkUpdate benchmarks the Update method performance.
// Expected: O(1) with hash index
func BenchmarkUpdate(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("threads_%d", size), func(b *testing.B) {
			store := createStoreWithThreads(b, size)
			threadID := fmt.Sprintf("thread-%d", size/2)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				meta := &ThreadMetadata{
					ThreadID: threadID,
					Status:   "busy",
				}
				_ = store.Update(threadID, meta)
			}
		})
	}
}

// BenchmarkDelete benchmarks the Delete method performance.
func BenchmarkDelete(b *testing.B) {
	b.Run("delete", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			store := createStoreWithThreads(b, 1000)
			threadID := fmt.Sprintf("thread-%d", 500)
			b.StartTimer()

			_ = store.Delete(threadID)
		}
	})
}

// BenchmarkMixedWorkload benchmarks a realistic mixed workload.
func BenchmarkMixedWorkload(b *testing.B) {
	store := createStoreWithThreads(b, 10000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		switch i % 10 {
		case 0, 1, 2, 3, 4: // 50% reads
			threadID := fmt.Sprintf("thread-%d", i%10000)
			_, _ = store.Get(threadID)
		case 5, 6: // 20% searches
			query := SearchQuery{Status: "idle", Limit: 10}
			_, _, _ = store.Search(query)
		case 7, 8: // 20% updates
			threadID := fmt.Sprintf("thread-%d", i%10000)
			meta := &ThreadMetadata{ThreadID: threadID, Status: "busy"}
			_ = store.Update(threadID, meta)
		case 9: // 10% creates
			threadID := fmt.Sprintf("new-thread-%d", i)
			meta := &ThreadMetadata{ThreadID: threadID, Status: "idle"}
			_ = store.Create(meta)
		}
	}
}

// BenchmarkIndexOperations benchmarks individual index operations.
func BenchmarkIndexOperations(b *testing.B) {
	idx := NewThreadIndex()

	// Pre-populate index
	for i := 0; i < 10000; i++ {
		threadID := fmt.Sprintf("thread-%d", i)
		status := "idle"
		if i%3 == 0 {
			status = "busy"
		}
		meta := &ThreadMetadata{
			ThreadID:  threadID,
			Status:    status,
			CreatedAt: time.Now().UnixMilli() - int64(i*1000),
		}
		idx.Add(meta)
	}

	b.Run("Get", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = idx.Get("thread-5000")
		}
	})

	b.Run("Search_NoFilter", func(b *testing.B) {
		query := SearchQuery{Limit: 10}
		for i := 0; i < b.N; i++ {
			_, _ = idx.Search(query)
		}
	})

	b.Run("Search_StatusFilter", func(b *testing.B) {
		query := SearchQuery{Status: "idle", Limit: 10}
		for i := 0; i < b.N; i++ {
			_, _ = idx.Search(query)
		}
	})

	b.Run("Update", func(b *testing.B) {
		meta := &ThreadMetadata{
			ThreadID:  "thread-5000",
			Status:    "busy",
			CreatedAt: time.Now().UnixMilli(),
		}
		for i := 0; i < b.N; i++ {
			_ = idx.Update("thread-5000", meta)
		}
	})
}

// BenchmarkQueryBuilder benchmarks the query builder performance.
func BenchmarkQueryBuilder(b *testing.B) {
	store := createStoreWithThreads(b, 10000)

	b.Run("SimpleStatus", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			qb := NewQueryBuilder().WhereStatus("idle").Limit(10)
			query := qb.Build()
			_, _, _ = store.Search(query)
		}
	})

	b.Run("ComplexQuery", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			qb := NewQueryBuilder().
				WhereStatusIn([]string{"idle", "busy"}).
				WhereCreatedAfter(time.Now().Add(-24 * time.Hour).UnixMilli()).
				OrderByCreatedDesc().
				Limit(10)
			query := qb.Build()
			_, _, _ = store.Search(query)
		}
	})
}

// Helper function to create a store with specified number of threads
func createStoreWithThreads(tb testing.TB, count int) *FileStore {
	tb.Helper()
	tmpDir := tb.TempDir()

	store, err := NewFileStore(tmpDir)
	if err != nil {
		tb.Fatalf("create store: %v", err)
	}

	// Create threads
	for i := 0; i < count; i++ {
		threadID := fmt.Sprintf("thread-%d", i)
		status := "idle"
		if i%3 == 0 {
			status = "busy"
		}
		meta := &ThreadMetadata{
			ThreadID:  threadID,
			Status:    status,
			CreatedAt: time.Now().UnixMilli() - int64(i*1000),
		}
		if err := store.Create(meta); err != nil {
			tb.Fatalf("create thread: %v", err)
		}
	}

	return store
}

// TestIndexPerformance verifies the performance improvements.
func TestIndexPerformance(t *testing.T) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			store := createStoreWithThreads(t, size)

			// Test Get performance (should be O(1))
			threadID := fmt.Sprintf("thread-%d", size/2)
			start := time.Now()
			for i := 0; i < 1000; i++ {
				_, _ = store.Get(threadID)
			}
			getDuration := time.Since(start)
			t.Logf("Get(%d threads): %v for 1000 ops = %v/op", size, getDuration, getDuration/1000)

			// Test Search performance with status filter (should use index)
			start = time.Now()
			for i := 0; i < 1000; i++ {
				_, _, _ = store.Search(SearchQuery{Status: "idle", Limit: 10})
			}
			searchDuration := time.Since(start)
			t.Logf("Search with status filter(%d threads): %v for 1000 ops = %v/op", size, searchDuration, searchDuration/1000)

			// Verify index stats
			stats := store.idx.GetStats()
			t.Logf("Index stats: TotalThreads=%d, QueriesServed=%d, IndexHits=%d",
				stats.TotalThreads, stats.QueriesServed, stats.IndexHits)
		})
	}
}

// TestScalability tests that performance scales linearly or better.
func TestScalability(t *testing.T) {
	// Get should be constant time regardless of thread count
	get100Time := measureGetTime(t, 100)
	get1000Time := measureGetTime(t, 1000)
	get10000Time := measureGetTime(t, 10000)

	t.Logf("Get(100): %v/op", get100Time)
	t.Logf("Get(1000): %v/op", get1000Time)
	t.Logf("Get(10000): %v/op", get10000Time)

	// Get time should not increase significantly with thread count
	// With O(1), the ratio should be close to 1
	ratio1000 := float64(get1000Time) / float64(get100Time)
	ratio10000 := float64(get10000Time) / float64(get100Time)

	t.Logf("Ratio (1000/100): %.2f", ratio1000)
	t.Logf("Ratio (10000/100): %.2f", ratio10000)

	// Allow for some variance, but should not be linear
	if ratio10000 > 3.0 {
		t.Errorf("Get performance does not scale well: ratio %.2f > 3.0", ratio10000)
	}
}

func measureGetTime(t *testing.T, count int) time.Duration {
	t.Helper()
	store := createStoreWithThreads(t, count)
	threadID := fmt.Sprintf("thread-%d", count/2)

	start := time.Now()
	for i := 0; i < 1000; i++ {
		_, _ = store.Get(threadID)
	}
	return time.Since(start) / 1000
}
