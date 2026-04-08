package threadstore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryBuilder_New(t *testing.T) {
	qb := NewQueryBuilder()
	assert.NotNil(t, qb)
	assert.Len(t, qb.filters, 0)
	assert.Len(t, qb.sorts, 0)
	assert.Equal(t, 0, qb.limit)
	assert.Equal(t, 0, qb.offset)
}

func TestQueryBuilder_Where(t *testing.T) {
	qb := NewQueryBuilder()
	qb.Where("status", "=", "idle").
		Where("created_at", ">", 1000)

	assert.Len(t, qb.filters, 2)
	assert.Equal(t, "status", qb.filters[0].Field)
	assert.Equal(t, "=", qb.filters[0].Operator)
	assert.Equal(t, "idle", qb.filters[0].Value)
}

func TestQueryBuilder_WhereStatus(t *testing.T) {
	qb := NewQueryBuilder()
	qb.WhereStatus("busy")

	assert.Len(t, qb.filters, 1)
	assert.Equal(t, "status", qb.filters[0].Field)
	assert.Equal(t, "=", qb.filters[0].Operator)
	assert.Equal(t, "busy", qb.filters[0].Value)
}

func TestQueryBuilder_WhereStatusIn(t *testing.T) {
	qb := NewQueryBuilder()
	statuses := []string{"idle", "busy", "running"}
	qb.WhereStatusIn(statuses)

	assert.Len(t, qb.filters, 1)
	assert.Equal(t, "status", qb.filters[0].Field)
	assert.Equal(t, "in", qb.filters[0].Operator)
}

func TestQueryBuilder_WhereCreatedAfter(t *testing.T) {
	qb := NewQueryBuilder()
	qb.WhereCreatedAfter(1234567890)

	assert.Len(t, qb.filters, 1)
	assert.Equal(t, "created_at", qb.filters[0].Field)
	assert.Equal(t, ">", qb.filters[0].Operator)
}

func TestQueryBuilder_WhereCreatedBefore(t *testing.T) {
	qb := NewQueryBuilder()
	qb.WhereCreatedBefore(1234567890)

	assert.Len(t, qb.filters, 1)
	assert.Equal(t, "created_at", qb.filters[0].Field)
	assert.Equal(t, "<", qb.filters[0].Operator)
}

func TestQueryBuilder_WhereUpdatedAfter(t *testing.T) {
	qb := NewQueryBuilder()
	qb.WhereUpdatedAfter(1234567890)

	assert.Len(t, qb.filters, 1)
	assert.Equal(t, "updated_at", qb.filters[0].Field)
	assert.Equal(t, ">", qb.filters[0].Operator)
}

func TestQueryBuilder_WhereTitleContains(t *testing.T) {
	qb := NewQueryBuilder()
	qb.WhereTitleContains("test")

	assert.Len(t, qb.filters, 1)
	assert.Equal(t, "title", qb.filters[0].Field)
	assert.Equal(t, "contains", qb.filters[0].Operator)
}

func TestQueryBuilder_OrderBy(t *testing.T) {
	qb := NewQueryBuilder()
	qb.OrderBy("created_at", true).
		OrderBy("updated_at", false)

	assert.Len(t, qb.sorts, 2)
	assert.Equal(t, "created_at", qb.sorts[0].Field)
	assert.True(t, qb.sorts[0].Desc)
	assert.Equal(t, "updated_at", qb.sorts[1].Field)
	assert.False(t, qb.sorts[1].Desc)
}

func TestQueryBuilder_OrderByCreatedDesc(t *testing.T) {
	qb := NewQueryBuilder()
	qb.OrderByCreatedDesc()

	assert.Len(t, qb.sorts, 1)
	assert.Equal(t, "created_at", qb.sorts[0].Field)
	assert.True(t, qb.sorts[0].Desc)
}

func TestQueryBuilder_OrderByCreatedAsc(t *testing.T) {
	qb := NewQueryBuilder()
	qb.OrderByCreatedAsc()

	assert.Len(t, qb.sorts, 1)
	assert.Equal(t, "created_at", qb.sorts[0].Field)
	assert.False(t, qb.sorts[0].Desc)
}

func TestQueryBuilder_OrderByUpdatedDesc(t *testing.T) {
	qb := NewQueryBuilder()
	qb.OrderByUpdatedDesc()

	assert.Len(t, qb.sorts, 1)
	assert.Equal(t, "updated_at", qb.sorts[0].Field)
	assert.True(t, qb.sorts[0].Desc)
}

func TestQueryBuilder_Limit(t *testing.T) {
	qb := NewQueryBuilder()
	qb.Limit(10)

	assert.Equal(t, 10, qb.limit)
}

func TestQueryBuilder_Offset(t *testing.T) {
	qb := NewQueryBuilder()
	qb.Offset(20)

	assert.Equal(t, 20, qb.offset)
}

func TestQueryBuilder_Page(t *testing.T) {
	qb := NewQueryBuilder()
	qb.Page(2, 10)

	assert.Equal(t, 10, qb.offset) // (2-1) * 10
	assert.Equal(t, 10, qb.limit)
}

func TestQueryBuilder_Page_FirstPage(t *testing.T) {
	qb := NewQueryBuilder()
	qb.Page(1, 20)

	assert.Equal(t, 0, qb.offset)
	assert.Equal(t, 20, qb.limit)
}

func TestQueryBuilder_Page_ZeroPage(t *testing.T) {
	qb := NewQueryBuilder()
	qb.Page(0, 10) // Should be treated as page 1

	assert.Equal(t, 0, qb.offset)
	assert.Equal(t, 10, qb.limit)
}

func TestQueryBuilder_Build(t *testing.T) {
	qb := NewQueryBuilder()
	qb.WhereStatus("idle").
		OrderByCreatedDesc().
		Limit(10).
		Offset(5)

	query := qb.Build()

	assert.Equal(t, "idle", query.Status)
	assert.Equal(t, 10, query.Limit)
	assert.Equal(t, 5, query.Offset)
}

func TestQueryBuilder_Execute_Simple(t *testing.T) {
	idx := NewThreadIndex()

	// Add threads
	for i := 1; i <= 5; i++ {
		status := "idle"
		if i%2 == 0 {
			status = "busy"
		}
		idx.Add(&ThreadMetadata{
			ThreadID:  string(rune('a' + i)),
			Status:    status,
			CreatedAt: int64(i * 1000),
		})
	}

	// Simple query
	qb := NewQueryBuilder().WhereStatus("idle")
	results, total, err := qb.Execute(idx)

	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, results, 3)
}

func TestQueryBuilder_Execute_WithPagination(t *testing.T) {
	idx := NewThreadIndex()

	// Add 10 threads
	for i := 1; i <= 10; i++ {
		idx.Add(&ThreadMetadata{
			ThreadID:  string(rune('a' + i)),
			Status:    "idle",
			CreatedAt: int64(i * 1000),
		})
	}

	// Query with pagination
	qb := NewQueryBuilder().Limit(5).Offset(0)
	results, total, err := qb.Execute(idx)

	require.NoError(t, err)
	assert.Equal(t, 10, total)
	assert.Len(t, results, 5)
}

func TestQueryBuilder_Execute_AdvancedFilters(t *testing.T) {
	idx := NewThreadIndex()

	// Add threads with different attributes
	idx.Add(&ThreadMetadata{
		ThreadID:  "thread-1",
		Title:     "Test Thread",
		Status:    "idle",
		CreatedAt: 1000,
		UpdatedAt: 2000,
	})
	idx.Add(&ThreadMetadata{
		ThreadID:  "thread-2",
		Title:     "Another Thread",
		Status:    "busy",
		CreatedAt: 2000,
		UpdatedAt: 3000,
	})

	// Query with multiple filters
	qb := NewQueryBuilder().
		WhereStatus("idle").
		WhereCreatedAfter(500)
	results, total, err := qb.Execute(idx)

	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, results, 1)
	assert.Equal(t, "thread-1", results[0].ThreadID)
}

func TestQueryBuilder_CompareValues_Equals(t *testing.T) {
	qb := NewQueryBuilder()

	assert.True(t, qb.compareValues("idle", "=", "idle"))
	assert.False(t, qb.compareValues("idle", "=", "busy"))
	assert.True(t, qb.compareValues(100, "=", 100))
}

func TestQueryBuilder_CompareValues_NotEquals(t *testing.T) {
	qb := NewQueryBuilder()

	assert.True(t, qb.compareValues("idle", "!=", "busy"))
	assert.False(t, qb.compareValues("idle", "!=", "idle"))
}

func TestQueryBuilder_CompareValues_GreaterThan(t *testing.T) {
	qb := NewQueryBuilder()

	assert.True(t, qb.compareValues(200, ">", 100))
	assert.False(t, qb.compareValues(100, ">", 200))
	assert.False(t, qb.compareValues(100, ">", 100))
}

func TestQueryBuilder_CompareValues_GreaterThanOrEqual(t *testing.T) {
	qb := NewQueryBuilder()

	assert.True(t, qb.compareValues(200, ">=", 100))
	assert.True(t, qb.compareValues(100, ">=", 100))
	assert.False(t, qb.compareValues(50, ">=", 100))
}

func TestQueryBuilder_CompareValues_LessThan(t *testing.T) {
	qb := NewQueryBuilder()

	assert.True(t, qb.compareValues(50, "<", 100))
	assert.False(t, qb.compareValues(100, "<", 50))
	assert.False(t, qb.compareValues(100, "<", 100))
}

func TestQueryBuilder_CompareValues_LessThanOrEqual(t *testing.T) {
	qb := NewQueryBuilder()

	assert.True(t, qb.compareValues(50, "<=", 100))
	assert.True(t, qb.compareValues(100, "<=", 100))
	assert.False(t, qb.compareValues(150, "<=", 100))
}

func TestQueryBuilder_CompareValues_In(t *testing.T) {
	qb := NewQueryBuilder()

	assert.True(t, qb.compareValues("idle", "in", []string{"idle", "busy"}))
	assert.False(t, qb.compareValues("running", "in", []string{"idle", "busy"}))
	assert.False(t, qb.compareValues("idle", "in", "not-a-slice"))
}

func TestQueryBuilder_CompareValues_Contains(t *testing.T) {
	qb := NewQueryBuilder()

	assert.True(t, qb.compareValues("Test Thread", "contains", "test"))
	assert.True(t, qb.compareValues("Test Thread", "contains", "THREAD"))
	assert.False(t, qb.compareValues("Test Thread", "contains", "other"))
}

func TestQueryBuilder_CompareValues_UnknownOperator(t *testing.T) {
	qb := NewQueryBuilder()

	assert.False(t, qb.compareValues("value", "unknown", "value"))
}

func TestQueryBuilder_CompareNumbers(t *testing.T) {
	qb := NewQueryBuilder()

	assert.Equal(t, -1, qb.compareNumbers(50, 100))
	assert.Equal(t, 0, qb.compareNumbers(100, 100))
	assert.Equal(t, 1, qb.compareNumbers(150, 100))
}

func TestQueryBuilder_CompareNumbers_DifferentTypes(t *testing.T) {
	qb := NewQueryBuilder()

	// Test different numeric types
	assert.Equal(t, 0, qb.compareNumbers(int(100), int64(100)))
	assert.Equal(t, 0, qb.compareNumbers(int64(100), float64(100)))
	assert.Equal(t, 0, qb.compareNumbers(float32(100), float64(100)))
	assert.Equal(t, 0, qb.compareNumbers(uint(100), uint64(100)))
}

func TestQueryBuilder_CompareNumbers_InvalidTypes(t *testing.T) {
	qb := NewQueryBuilder()

	// Invalid types should return 0
	assert.Equal(t, 0, qb.compareNumbers("string", 100))
	assert.Equal(t, 0, qb.compareNumbers(100, "string"))
}

func TestQueryBuilder_ToFloat64(t *testing.T) {
	qb := NewQueryBuilder()

	// Test various numeric types
	val, ok := qb.toFloat64(int(100))
	assert.True(t, ok)
	assert.Equal(t, 100.0, val)

	val, ok = qb.toFloat64(int64(100))
	assert.True(t, ok)
	assert.Equal(t, 100.0, val)

	val, ok = qb.toFloat64(float32(100.5))
	assert.True(t, ok)
	assert.Equal(t, 100.5, val)

	val, ok = qb.toFloat64(float64(100.5))
	assert.True(t, ok)
	assert.Equal(t, 100.5, val)

	val, ok = qb.toFloat64(uint(100))
	assert.True(t, ok)
	assert.Equal(t, 100.0, val)

	val, ok = qb.toFloat64(uint64(100))
	assert.True(t, ok)
	assert.Equal(t, 100.0, val)

	// Invalid type
	_, ok = qb.toFloat64("string")
	assert.False(t, ok)
}

func TestQueryBuilder_MatchFilter(t *testing.T) {
	qb := NewQueryBuilder()
	meta := &ThreadMetadata{
		ThreadID:  "thread-001",
		Title:     "Test Thread",
		Status:    "idle",
		CreatedAt: 1000,
		UpdatedAt: 2000,
		Metadata: map[string]any{
			"custom_field": "custom_value",
		},
	}

	// Test various fields
	assert.True(t, qb.matchFilter(meta, Filter{Field: "status", Operator: "=", Value: "idle"}))
	assert.True(t, qb.matchFilter(meta, Filter{Field: "created_at", Operator: ">", Value: 500}))
	assert.True(t, qb.matchFilter(meta, Filter{Field: "updated_at", Operator: "<", Value: 3000}))
	assert.True(t, qb.matchFilter(meta, Filter{Field: "title", Operator: "contains", Value: "test"}))
	assert.True(t, qb.matchFilter(meta, Filter{Field: "thread_id", Operator: "=", Value: "thread-001"}))
	assert.True(t, qb.matchFilter(meta, Filter{Field: "custom_field", Operator: "=", Value: "custom_value"}))
}

func TestQueryBuilder_MatchFilter_MetadataField(t *testing.T) {
	qb := NewQueryBuilder()
	meta := &ThreadMetadata{
		ThreadID: "thread-001",
		Metadata: map[string]any{
			"assistant": "claude",
		},
	}

	assert.True(t, qb.matchFilter(meta, Filter{Field: "assistant", Operator: "=", Value: "claude"}))
	assert.False(t, qb.matchFilter(meta, Filter{Field: "nonexistent", Operator: "=", Value: "value"}))
}

func TestQueryBuilder_MatchFilter_NoMetadata(t *testing.T) {
	qb := NewQueryBuilder()
	meta := &ThreadMetadata{
		ThreadID: "thread-001",
	}

	// Should return false when metadata field doesn't exist
	assert.False(t, qb.matchFilter(meta, Filter{Field: "custom_field", Operator: "=", Value: "value"}))
}

func TestQueryBuilder_Matches(t *testing.T) {
	qb := NewQueryBuilder().
		WhereStatus("idle").
		WhereCreatedAfter(500)

	meta := &ThreadMetadata{
		ThreadID:  "thread-001",
		Status:    "idle",
		CreatedAt: 1000,
	}

	assert.True(t, qb.matches(meta))
}

func TestQueryBuilder_Matches_Fails(t *testing.T) {
	qb := NewQueryBuilder().
		WhereStatus("idle").
		WhereCreatedAfter(2000)

	meta := &ThreadMetadata{
		ThreadID:  "thread-001",
		Status:    "idle",
		CreatedAt: 1000, // Less than 2000
	}

	assert.False(t, qb.matches(meta))
}

func TestQueryBuilder_HasAdvancedFilters(t *testing.T) {
	// Simple status filter
	qb := NewQueryBuilder().WhereStatus("idle")
	assert.False(t, qb.hasAdvancedFilters())

	// Multiple filters
	qb = NewQueryBuilder().WhereStatus("idle").WhereCreatedAfter(1000)
	assert.True(t, qb.hasAdvancedFilters())

	// Non-equals operator on status
	qb = NewQueryBuilder().Where("status", "!=", "idle")
	assert.True(t, qb.hasAdvancedFilters())
}

func TestQueryBuilder_Execute_WithAdvancedFilters(t *testing.T) {
	idx := NewThreadIndex()

	// Add threads
	idx.Add(&ThreadMetadata{
		ThreadID:  "thread-1",
		Title:     "Test Alpha",
		Status:    "idle",
		CreatedAt: 1000,
	})
	idx.Add(&ThreadMetadata{
		ThreadID:  "thread-2",
		Title:     "Test Beta",
		Status:    "idle",
		CreatedAt: 2000,
	})
	idx.Add(&ThreadMetadata{
		ThreadID:  "thread-3",
		Title:     "Different",
		Status:    "idle",
		CreatedAt: 3000,
	})

	// Query with title contains filter (advanced)
	qb := NewQueryBuilder().
		WhereStatus("idle").
		WhereTitleContains("Test")

	results, total, err := qb.Execute(idx)

	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, results, 2)
}

func TestDefaultQueryOptions(t *testing.T) {
	opts := DefaultQueryOptions()

	assert.True(t, opts.UseIndex)
	assert.False(t, opts.CacheResults)
	assert.Equal(t, 0, opts.MaxScanCount)
	assert.Equal(t, time.Duration(0), opts.Timeout)
}
