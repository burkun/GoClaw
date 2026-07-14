package threadstore

import (
	"sort"
	"sync"
	"time"
)

// ThreadIndex provides O(1) lookup and indexed queries for threads.
type ThreadIndex struct {
	mu sync.RWMutex

	// Primary index: thread_id -> *ThreadMetadata
	byID map[string]*ThreadMetadata

	// Secondary indexes for common queries
	byStatus map[string]map[string]*ThreadMetadata // status -> thread_id -> metadata

	// Sorted slice for efficient pagination and ordering
	// Updated on insert/delete, kept sorted by CreatedAt descending
	sorted []*ThreadMetadata

	// Stats tracking
	stats IndexStats
}

// IndexStats tracks index performance metrics.
type IndexStats struct {
	TotalThreads   int64
	QueriesServed  int64
	IndexHits      int64
	IndexMisses    int64
	SlowQueries    int64
	LastUpdateTime int64
}

// NewThreadIndex creates a new thread index.
func NewThreadIndex() *ThreadIndex {
	return &ThreadIndex{
		byID:     make(map[string]*ThreadMetadata),
		byStatus: make(map[string]map[string]*ThreadMetadata),
		sorted:   make([]*ThreadMetadata, 0),
	}
}

// Add inserts a thread into the index.
// Time complexity: O(log n) for insertion into sorted slice.
func (idx *ThreadIndex) Add(meta *ThreadMetadata) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Primary index
	idx.byID[meta.ThreadID] = meta

	// Status index
	if idx.byStatus[meta.Status] == nil {
		idx.byStatus[meta.Status] = make(map[string]*ThreadMetadata)
	}
	idx.byStatus[meta.Status][meta.ThreadID] = meta

	// Insert into sorted slice (sorted by CreatedAt descending, newest first)
	idx.sorted = append(idx.sorted, meta)
	sort.Slice(idx.sorted, func(i, j int) bool {
		return idx.sorted[i].CreatedAt > idx.sorted[j].CreatedAt
	})

	// Update stats
	idx.stats.TotalThreads++
	idx.stats.LastUpdateTime = time.Now().UnixMilli()
}

// Get retrieves a thread by ID.
// Time complexity: O(1)
func (idx *ThreadIndex) Get(threadID string) (*ThreadMetadata, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	meta, exists := idx.byID[threadID]
	if exists {
		idx.stats.IndexHits++
	} else {
		idx.stats.IndexMisses++
	}
	idx.stats.QueriesServed++

	return meta, exists
}

// Update updates a thread in the index.
// Time complexity: O(1) for update, O(n) if status changed (need to rebuild sorted)
func (idx *ThreadIndex) Update(threadID string, meta *ThreadMetadata) bool {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Get existing metadata
	oldMeta, exists := idx.byID[threadID]
	if !exists {
		return false
	}

	// Update primary index
	idx.byID[threadID] = meta

	// Update status index if status changed
	if oldMeta.Status != meta.Status {
		// Remove from old status
		if statusMap, ok := idx.byStatus[oldMeta.Status]; ok {
			delete(statusMap, threadID)
		}
		// Add to new status
		if idx.byStatus[meta.Status] == nil {
			idx.byStatus[meta.Status] = make(map[string]*ThreadMetadata)
		}
		idx.byStatus[meta.Status][threadID] = meta
	} else {
		// Update in place
		if statusMap, ok := idx.byStatus[meta.Status]; ok {
			statusMap[threadID] = meta
		}
	}

	// Update in sorted slice
	for i, m := range idx.sorted {
		if m.ThreadID == threadID {
			idx.sorted[i] = meta
			break
		}
	}

	// Re-sort if needed (CreatedAt might change)
	sort.Slice(idx.sorted, func(i, j int) bool {
		return idx.sorted[i].CreatedAt > idx.sorted[j].CreatedAt
	})

	idx.stats.LastUpdateTime = time.Now().UnixMilli()
	return true
}

// Delete removes a thread from the index.
// Time complexity: O(n) for removal from sorted slice
func (idx *ThreadIndex) Delete(threadID string) bool {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	meta, exists := idx.byID[threadID]
	if !exists {
		return false
	}

	// Remove from primary index
	delete(idx.byID, threadID)

	// Remove from status index
	if statusMap, ok := idx.byStatus[meta.Status]; ok {
		delete(statusMap, threadID)
		// Clean up empty status maps
		if len(statusMap) == 0 {
			delete(idx.byStatus, meta.Status)
		}
	}

	// Remove from sorted slice
	for i, m := range idx.sorted {
		if m.ThreadID == threadID {
			idx.sorted = append(idx.sorted[:i], idx.sorted[i+1:]...)
			break
		}
	}

	idx.stats.TotalThreads--
	idx.stats.LastUpdateTime = time.Now().UnixMilli()
	return true
}

// Search performs indexed search with filters and pagination.
// Time complexity: O(k) where k is the result set size, much better than O(n)
func (idx *ThreadIndex) Search(query SearchQuery) ([]*ThreadMetadata, int) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	startTime := time.Now()
	idx.stats.QueriesServed++

	var results []*ThreadMetadata

	// Use status index if filtering by status
	if query.Status != "" {
		if statusMap, ok := idx.byStatus[query.Status]; ok {
			idx.stats.IndexHits++
			results = make([]*ThreadMetadata, 0, len(statusMap))
			for _, meta := range statusMap {
				results = append(results, meta)
			}
		} else {
			idx.stats.IndexHits++
			results = make([]*ThreadMetadata, 0)
		}
	} else {
		// No filter, use sorted slice
		idx.stats.IndexMisses++
		results = make([]*ThreadMetadata, len(idx.sorted))
		copy(results, idx.sorted)
	}

	// Sort results by CreatedAt descending (newest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt > results[j].CreatedAt
	})

	total := len(results)

	// Apply pagination
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(results) {
		offset = len(results)
	}

	limit := query.Limit
	if limit <= 0 {
		limit = len(results)
	}

	end := offset + limit
	if end > len(results) {
		end = len(results)
	}

	// Track slow queries (>10ms)
	elapsed := time.Since(startTime)
	if elapsed > 10*time.Millisecond {
		idx.stats.SlowQueries++
	}

	return results[offset:end], total
}

// List returns all threads sorted by CreatedAt descending.
// Time complexity: O(n) for copy
func (idx *ThreadIndex) List() []*ThreadMetadata {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make([]*ThreadMetadata, len(idx.sorted))
	copy(result, idx.sorted)
	return result
}

// GetStats returns current index statistics.
func (idx *ThreadIndex) GetStats() IndexStats {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return idx.stats
}

// Rebuild rebuilds the index from a list of threads.
// Useful for initializing from persisted data.
// Time complexity: O(n log n)
func (idx *ThreadIndex) Rebuild(threads []*ThreadMetadata) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Clear existing indexes
	idx.byID = make(map[string]*ThreadMetadata)
	idx.byStatus = make(map[string]map[string]*ThreadMetadata)
	idx.sorted = make([]*ThreadMetadata, 0, len(threads))

	// Rebuild all indexes
	for _, meta := range threads {
		// Primary index
		idx.byID[meta.ThreadID] = meta

		// Status index
		if idx.byStatus[meta.Status] == nil {
			idx.byStatus[meta.Status] = make(map[string]*ThreadMetadata)
		}
		idx.byStatus[meta.Status][meta.ThreadID] = meta

		// Sorted slice
		idx.sorted = append(idx.sorted, meta)
	}

	// Sort by CreatedAt descending
	sort.Slice(idx.sorted, func(i, j int) bool {
		return idx.sorted[i].CreatedAt > idx.sorted[j].CreatedAt
	})

	idx.stats.TotalThreads = int64(len(threads))
	idx.stats.LastUpdateTime = time.Now().UnixMilli()
}

// Count returns the total number of threads.
// Time complexity: O(1)
func (idx *ThreadIndex) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return len(idx.byID)
}

// CountByStatus returns the count of threads with a specific status.
// Time complexity: O(1)
func (idx *ThreadIndex) CountByStatus(status string) int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if statusMap, ok := idx.byStatus[status]; ok {
		return len(statusMap)
	}
	return 0
}
