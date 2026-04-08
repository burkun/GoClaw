package threadstore

import (
	"fmt"
	"strings"
	"time"
)

// QueryBuilder provides a fluent interface for building thread queries.
type QueryBuilder struct {
	filters []Filter
	sorts   []Sort
	limit   int
	offset  int
}

// Filter represents a query filter condition.
type Filter struct {
	Field    string
	Operator string
	Value    any
}

// Sort represents a sort order.
type Sort struct {
	Field string
	Desc  bool // true for descending, false for ascending
}

// NewQueryBuilder creates a new query builder.
func NewQueryBuilder() *QueryBuilder {
	return &QueryBuilder{
		filters: make([]Filter, 0),
		sorts:   make([]Sort, 0),
		limit:   0,
		offset:  0,
	}
}

// Where adds a filter condition.
// Supported operators: "=", "!=", ">", ">=", "<", "<=", "in", "contains"
func (qb *QueryBuilder) Where(field, operator string, value any) *QueryBuilder {
	qb.filters = append(qb.filters, Filter{
		Field:    field,
		Operator: strings.ToLower(operator),
		Value:    value,
	})
	return qb
}

// WhereStatus filters by status (convenience method).
func (qb *QueryBuilder) WhereStatus(status string) *QueryBuilder {
	return qb.Where("status", "=", status)
}

// WhereStatusIn filters by multiple statuses.
func (qb *QueryBuilder) WhereStatusIn(statuses []string) *QueryBuilder {
	return qb.Where("status", "in", statuses)
}

// WhereCreatedAfter filters threads created after a timestamp.
func (qb *QueryBuilder) WhereCreatedAfter(timestamp int64) *QueryBuilder {
	return qb.Where("created_at", ">", timestamp)
}

// WhereCreatedBefore filters threads created before a timestamp.
func (qb *QueryBuilder) WhereCreatedBefore(timestamp int64) *QueryBuilder {
	return qb.Where("created_at", "<", timestamp)
}

// WhereUpdatedAfter filters threads updated after a timestamp.
func (qb *QueryBuilder) WhereUpdatedAfter(timestamp int64) *QueryBuilder {
	return qb.Where("updated_at", ">", timestamp)
}

// WhereTitleContains filters threads with title containing text.
func (qb *QueryBuilder) WhereTitleContains(text string) *QueryBuilder {
	return qb.Where("title", "contains", text)
}

// OrderBy adds a sort order.
func (qb *QueryBuilder) OrderBy(field string, desc bool) *QueryBuilder {
	qb.sorts = append(qb.sorts, Sort{
		Field: field,
		Desc:  desc,
	})
	return qb
}

// OrderByCreatedDesc sorts by creation time descending (newest first).
func (qb *QueryBuilder) OrderByCreatedDesc() *QueryBuilder {
	return qb.OrderBy("created_at", true)
}

// OrderByCreatedAsc sorts by creation time ascending (oldest first).
func (qb *QueryBuilder) OrderByCreatedAsc() *QueryBuilder {
	return qb.OrderBy("created_at", false)
}

// OrderByUpdatedDesc sorts by update time descending (most recently updated first).
func (qb *QueryBuilder) OrderByUpdatedDesc() *QueryBuilder {
	return qb.OrderBy("updated_at", true)
}

// Limit sets the maximum number of results.
func (qb *QueryBuilder) Limit(limit int) *QueryBuilder {
	qb.limit = limit
	return qb
}

// Offset sets the offset for pagination.
func (qb *QueryBuilder) Offset(offset int) *QueryBuilder {
	qb.offset = offset
	return qb
}

// Page sets pagination using page number and page size.
// Page numbers start from 1.
func (qb *QueryBuilder) Page(pageNum, pageSize int) *QueryBuilder {
	if pageNum < 1 {
		pageNum = 1
	}
	qb.offset = (pageNum - 1) * pageSize
	qb.limit = pageSize
	return qb
}

// Build converts QueryBuilder to SearchQuery for backward compatibility.
func (qb *QueryBuilder) Build() SearchQuery {
	query := SearchQuery{
		Limit:  qb.limit,
		Offset: qb.offset,
	}

	// Extract status filter for SearchQuery compatibility
	for _, filter := range qb.filters {
		if filter.Field == "status" && filter.Operator == "=" {
			if status, ok := filter.Value.(string); ok {
				query.Status = status
			}
		}
	}

	return query
}

// matches checks if a thread matches all filters.
func (qb *QueryBuilder) matches(meta *ThreadMetadata) bool {
	for _, filter := range qb.filters {
		if !qb.matchFilter(meta, filter) {
			return false
		}
	}
	return true
}

// matchFilter checks if a thread matches a single filter.
func (qb *QueryBuilder) matchFilter(meta *ThreadMetadata, filter Filter) bool {
	switch filter.Field {
	case "status":
		return qb.compareValues(meta.Status, filter.Operator, filter.Value)
	case "created_at":
		return qb.compareValues(meta.CreatedAt, filter.Operator, filter.Value)
	case "updated_at":
		return qb.compareValues(meta.UpdatedAt, filter.Operator, filter.Value)
	case "title":
		return qb.compareValues(meta.Title, filter.Operator, filter.Value)
	case "thread_id":
		return qb.compareValues(meta.ThreadID, filter.Operator, filter.Value)
	default:
		// Check metadata fields
		if meta.Metadata != nil {
			if val, ok := meta.Metadata[filter.Field]; ok {
				return qb.compareValues(val, filter.Operator, filter.Value)
			}
		}
		return false
	}
}

// compareValues compares values based on operator.
func (qb *QueryBuilder) compareValues(actual any, operator string, expected any) bool {
	switch operator {
	case "=":
		return fmt.Sprintf("%v", actual) == fmt.Sprintf("%v", expected)
	case "!=":
		return fmt.Sprintf("%v", actual) != fmt.Sprintf("%v", expected)
	case ">":
		return qb.compareNumbers(actual, expected) > 0
	case ">=":
		return qb.compareNumbers(actual, expected) >= 0
	case "<":
		return qb.compareNumbers(actual, expected) < 0
	case "<=":
		return qb.compareNumbers(actual, expected) <= 0
	case "in":
		if slice, ok := expected.([]string); ok {
			actualStr := fmt.Sprintf("%v", actual)
			for _, s := range slice {
				if s == actualStr {
					return true
				}
			}
		}
		return false
	case "contains":
		actualStr := fmt.Sprintf("%v", actual)
		expectedStr := fmt.Sprintf("%v", expected)
		return strings.Contains(strings.ToLower(actualStr), strings.ToLower(expectedStr))
	default:
		return false
	}
}

// compareNumbers compares two numeric values.
func (qb *QueryBuilder) compareNumbers(a, b any) int {
	// Convert to float64 for comparison
	aFloat, aOk := qb.toFloat64(a)
	bFloat, bOk := qb.toFloat64(b)

	if !aOk || !bOk {
		return 0
	}

	if aFloat < bFloat {
		return -1
	} else if aFloat > bFloat {
		return 1
	}
	return 0
}

// toFloat64 converts a value to float64.
func (qb *QueryBuilder) toFloat64(val any) (float64, bool) {
	switch v := val.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	default:
		return 0, false
	}
}

// Execute runs the query against the index.
func (qb *QueryBuilder) Execute(idx *ThreadIndex) ([]*ThreadMetadata, int, error) {
	// For now, use the simple SearchQuery for backward compatibility
	// Future optimization: implement advanced query execution with all filters
	query := qb.Build()
	results, total := idx.Search(query)

	// Apply additional filters not supported by SearchQuery
	if len(qb.filters) > 1 || qb.hasAdvancedFilters() {
		filtered := make([]*ThreadMetadata, 0, len(results))
		for _, meta := range results {
			if qb.matches(meta) {
				filtered = append(filtered, meta)
			}
		}
		results = filtered
		total = len(filtered)

		// Apply pagination after filtering
		offset := qb.offset
		if offset < 0 {
			offset = 0
		}
		if offset > len(results) {
			offset = len(results)
		}

		limit := qb.limit
		if limit <= 0 {
			limit = len(results)
		}

		end := offset + limit
		if end > len(results) {
			end = len(results)
		}

		results = results[offset:end]
	}

	return results, total, nil
}

// hasAdvancedFilters checks if there are filters beyond simple status filter.
func (qb *QueryBuilder) hasAdvancedFilters() bool {
	for _, filter := range qb.filters {
		if filter.Field != "status" || filter.Operator != "=" {
			return true
		}
	}
	return false
}

// QueryOptions represents query execution options.
type QueryOptions struct {
	// UseIndex forces the query to use indexes when available (default: true)
	UseIndex bool
	// CacheResults enables result caching for repeated queries
	CacheResults bool
	// MaxScanCount limits the number of items to scan (0 = unlimited)
	MaxScanCount int
	// Timeout sets a query timeout (0 = no timeout)
	Timeout time.Duration
}

// DefaultQueryOptions returns default query options.
func DefaultQueryOptions() QueryOptions {
	return QueryOptions{
		UseIndex:     true,
		CacheResults: false,
		MaxScanCount: 0,
		Timeout:      0,
	}
}
