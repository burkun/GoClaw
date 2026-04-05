package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMergeArtifacts(t *testing.T) {
	tests := []struct {
		name     string
		existing any
		new      any
		want     []string
	}{
		{
			name:     "both nil",
			existing: nil,
			new:      nil,
			want:     nil,
		},
		{
			name:     "existing nil",
			existing: nil,
			new:      []string{"a.md", "b.md"},
			want:     []string{"a.md", "b.md"},
		},
		{
			name:     "new nil",
			existing: []string{"a.md"},
			new:      nil,
			want:     []string{"a.md"},
		},
		{
			name:     "merge with deduplication",
			existing: []string{"a.md", "b.md"},
			new:      []string{"b.md", "c.md"},
			want:     []string{"a.md", "b.md", "c.md"},
		},
		{
			name:     "preserve order",
			existing: []string{"x.md", "y.md"},
			new:      []string{"z.md", "x.md"},
			want:     []string{"x.md", "y.md", "z.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeArtifacts(tt.existing, tt.new)
			got, ok := result.([]string)
			if !ok && result != nil {
				t.Fatalf("expected []string, got %T", result)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMergeViewedImages(t *testing.T) {
	tests := []struct {
		name     string
		existing any
		new      any
		want     map[string]ViewedImage
	}{
		{
			name:     "both nil",
			existing: nil,
			new:      nil,
			want:     nil,
		},
		{
			name:     "existing nil",
			existing: nil,
			new:      map[string]ViewedImage{"img1": {Base64: "abc", MIMEType: "image/png"}},
			want:     map[string]ViewedImage{"img1": {Base64: "abc", MIMEType: "image/png"}},
		},
		{
			name:     "new nil",
			existing: map[string]ViewedImage{"img1": {Base64: "abc", MIMEType: "image/png"}},
			new:      nil,
			want:     map[string]ViewedImage{"img1": {Base64: "abc", MIMEType: "image/png"}},
		},
		{
			name:     "merge with override",
			existing: map[string]ViewedImage{"img1": {Base64: "old", MIMEType: "image/png"}},
			new:      map[string]ViewedImage{"img1": {Base64: "new", MIMEType: "image/png"}},
			want:     map[string]ViewedImage{"img1": {Base64: "new", MIMEType: "image/png"}},
		},
		{
			name:     "empty map clears all",
			existing: map[string]ViewedImage{"img1": {Base64: "abc", MIMEType: "image/png"}},
			new:      map[string]ViewedImage{},
			want:     map[string]ViewedImage{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeViewedImages(tt.existing, tt.new)
			got, ok := result.(map[string]ViewedImage)
			if !ok && result != nil {
				t.Fatalf("expected map[string]ViewedImage, got %T", result)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestApplyReducers(t *testing.T) {
	tests := []struct {
		name          string
		state         *State
		pending       map[string]any
		wantArtifacts []string
		wantImages    map[string]ViewedImage
	}{
		{
			name: "apply artifacts reducer",
			state: &State{
				Artifacts: []string{"a.md"},
			},
			pending: map[string]any{
				"artifacts": []string{"b.md", "a.md"},
			},
			wantArtifacts: []string{"a.md", "b.md"},
			wantImages:    nil,
		},
		{
			name: "apply viewed_images reducer with clear",
			state: &State{
				ViewedImages: map[string]ViewedImage{
					"img1": {Base64: "abc", MIMEType: "image/png"},
				},
			},
			pending: map[string]any{
				"viewed_images": map[string]ViewedImage{},
			},
			wantArtifacts: nil,
			wantImages:    map[string]ViewedImage{},
		},
		{
			name: "apply both reducers",
			state: &State{
				Artifacts: []string{"old.md"},
				ViewedImages: map[string]ViewedImage{
					"img1": {Base64: "old", MIMEType: "image/png"},
				},
			},
			pending: map[string]any{
				"artifacts": []string{"new.md"},
				"viewed_images": map[string]ViewedImage{
					"img1": {Base64: "new", MIMEType: "image/png"},
					"img2": {Base64: "added", MIMEType: "image/jpeg"},
				},
			},
			wantArtifacts: []string{"old.md", "new.md"},
			wantImages: map[string]ViewedImage{
				"img1": {Base64: "new", MIMEType: "image/png"},
				"img2": {Base64: "added", MIMEType: "image/jpeg"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ApplyReducers(tt.state, tt.pending)
			assert.Equal(t, tt.wantArtifacts, tt.state.Artifacts)
			assert.Equal(t, tt.wantImages, tt.state.ViewedImages)
		})
	}
}
