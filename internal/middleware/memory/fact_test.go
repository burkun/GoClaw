package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFactCategory_IsValid(t *testing.T) {
	tests := []struct {
		category FactCategory
		expected bool
	}{
		{CategoryPreference, true},
		{CategoryKnowledge, true},
		{CategoryContext, true},
		{CategoryBehavior, true},
		{CategoryGoal, true},
		{CategoryCorrection, true},
		{FactCategory("invalid"), false},
		{FactCategory(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.category.IsValid())
		})
	}
}

func TestFactCategory_String(t *testing.T) {
	tests := []struct {
		category FactCategory
		expected string
	}{
		{CategoryPreference, "preference"},
		{CategoryKnowledge, "knowledge"},
		{CategoryContext, "context"},
		{CategoryBehavior, "behavior"},
		{CategoryGoal, "goal"},
		{CategoryCorrection, "correction"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.category.String())
		})
	}
}

func TestAllCategories(t *testing.T) {
	// Ensure all six categories are defined
	categories := []FactCategory{
		CategoryPreference,
		CategoryKnowledge,
		CategoryContext,
		CategoryBehavior,
		CategoryGoal,
		CategoryCorrection,
	}

	assert.Len(t, categories, 6, "should have 6 categories matching DeerFlow")

	for _, cat := range categories {
		assert.True(t, cat.IsValid(), "category %s should be valid", cat)
	}
}
