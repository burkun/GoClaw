// Package uploads implements UploadsMiddleware which scans the uploads directory
// and injects the list of uploaded files into state.Extra["uploads"].
package uploads

import (
	"context"
	"os"
	"path/filepath"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// UploadsMiddleware injects uploaded file list into state.Extra["uploads"].
// Depends on ThreadDataMiddleware having run first to populate state.Extra["uploads_path"].
type UploadsMiddleware struct {
	middleware.MiddlewareWrapper
}

// New creates an UploadsMiddleware.
func New() *UploadsMiddleware {
	return &UploadsMiddleware{}
}

// Name implements middleware.Middleware.
func (m *UploadsMiddleware) Name() string {
	return "UploadsMiddleware"
}

// Before scans uploads directory and populates state.Extra["uploads"].
func (m *UploadsMiddleware) Before(_ context.Context, state *middleware.State) error {
	uploadsPath, ok := state.Extra["uploads_path"].(string)
	if !ok || uploadsPath == "" {
		return nil
	}

	entries, err := os.ReadDir(uploadsPath)
	if err != nil {
		if os.IsNotExist(err) {
			state.Extra["uploads"] = []string{}
			return nil
		}
		return err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, filepath.Join("/mnt/user-data/uploads", e.Name()))
		}
	}
	if files == nil {
		files = []string{}
	}
	state.Extra["uploads"] = files
	return nil
}

// After is a no-op.
func (m *UploadsMiddleware) After(_ context.Context, _ *middleware.State, _ *middleware.Response) error {
	return nil
}

var _ middleware.Middleware = (*UploadsMiddleware)(nil)
