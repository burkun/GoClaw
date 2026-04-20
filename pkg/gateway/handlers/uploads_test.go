package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goclaw/internal/config"
)

func TestValidateThreadID(t *testing.T) {
	cases := []struct {
		id      string
		wantErr bool
	}{
		{id: "thread-1", wantErr: false},
		{id: "thread_1", wantErr: false},
		{id: "abc123", wantErr: false},
		{id: "", wantErr: true},
		{id: "../x", wantErr: true},
		{id: "x/y", wantErr: true},
		{id: "x.y", wantErr: true},
	}
	for _, tc := range cases {
		err := validateThreadID(tc.id)
		if (err != nil) != tc.wantErr {
			t.Fatalf("validateThreadID(%q) error=%v wantErr=%v", tc.id, err, tc.wantErr)
		}
	}
}

func TestUploadFiles_Success(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("files", "hello.txt")
	if err != nil {
		t.Fatalf("create form file failed: %v", err)
	}
	if _, err := fw.Write([]byte("hello world")); err != nil {
		t.Fatalf("write form file failed: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/threads/thread-1/uploads", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-1"})

	h := NewUploadsHandler(&config.AppConfig{})
	h.UploadFiles(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp UploadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if !resp.Success || len(resp.Files) != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}

	hostPath := resp.Files[0].HostPath
	if _, err := os.Stat(hostPath); err != nil {
		t.Fatalf("uploaded file not found at %s: %v", hostPath, err)
	}
	data, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatalf("read uploaded file failed: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("unexpected uploaded content: %q", string(data))
	}
	wantDir := filepath.Join(".goclaw", "threads", "thread-1", "user-data", "uploads")
	if filepath.Dir(hostPath) != wantDir {
		t.Fatalf("unexpected host path: %s", hostPath)
	}
}

func TestUploadFiles_InvalidThreadID(t *testing.T) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("files", "hello.txt")
	if err != nil {
		t.Fatalf("create form file failed: %v", err)
	}
	_, _ = fw.Write([]byte("hello"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/threads/../x/uploads", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "../x"})

	h := NewUploadsHandler(&config.AppConfig{})
	h.UploadFiles(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUploadFiles_MissingThreadID(t *testing.T) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("files", "hello.txt")
	if err != nil {
		t.Fatalf("create form file failed: %v", err)
	}
	_, _ = fw.Write([]byte("hello"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/threads//uploads", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": ""})

	h := NewUploadsHandler(&config.AppConfig{})
	h.UploadFiles(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUploadFiles_NoFiles(t *testing.T) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/threads/thread-1/uploads", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-1"})

	h := NewUploadsHandler(&config.AppConfig{})
	h.UploadFiles(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUploadFiles_InvalidMultipart(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/threads/thread-1/uploads", strings.NewReader("invalid multipart"))
	req.Header.Set("Content-Type", "multipart/form-data")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-1"})

	h := NewUploadsHandler(&config.AppConfig{})
	h.UploadFiles(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUploadFiles_MultipleFiles(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	// First file
	fw1, _ := mw.CreateFormFile("files", "file1.txt")
	fw1.Write([]byte("content1"))

	// Second file
	fw2, _ := mw.CreateFormFile("files", "file2.txt")
	fw2.Write([]byte("content2"))

	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/threads/thread-multi/uploads", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-multi"})

	h := NewUploadsHandler(&config.AppConfig{})
	h.UploadFiles(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp UploadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if len(resp.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(resp.Files))
	}
}

func TestSanitiseFilename(t *testing.T) {
	cases := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{name: "safe.txt", want: "safe.txt", wantErr: false},
		{name: "path/to/file.txt", want: "file.txt", wantErr: false},
		{name: ".", want: "", wantErr: true},
		{name: "..", want: "", wantErr: true},
		{name: "file/with/slash.txt", want: "slash.txt", wantErr: false},
	}
	for _, tc := range cases {
		got, err := sanitiseFilename(tc.name)
		if (err != nil) != tc.wantErr {
			t.Errorf("sanitiseFilename(%q) error=%v wantErr=%v", tc.name, err, tc.wantErr)
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("sanitiseFilename(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestListUploadedFiles(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	// Create uploads directory with test file
	uploadsDir := filepath.Join(".goclaw", "threads", "thread-list", "user-data", "uploads")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadsDir, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-list/uploads/list", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-list"})

	h := NewUploadsHandler(&config.AppConfig{})
	h.ListUploadedFiles(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestListUploadedFiles_EmptyDirectory(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-empty/uploads/list", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-empty"})

	h := NewUploadsHandler(&config.AppConfig{})
	h.ListUploadedFiles(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	files, ok := resp["files"].([]interface{})
	if !ok {
		t.Fatal("expected files array")
	}
	if len(files) != 0 {
		t.Fatalf("expected empty files array, got %d", len(files))
	}
}

func TestListUploadedFiles_InvalidThreadID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/threads/../x/uploads/list", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "../x"})

	h := NewUploadsHandler(&config.AppConfig{})
	h.ListUploadedFiles(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestListUploadedFiles_MissingThreadID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/threads//uploads/list", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": ""})

	h := NewUploadsHandler(&config.AppConfig{})
	h.ListUploadedFiles(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDeleteUploadedFile(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	// Create uploads directory with test file
	uploadsDir := filepath.Join(".goclaw", "threads", "thread-del", "user-data", "uploads")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadsDir, "delete-me.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/threads/thread-del/uploads/delete-me.txt", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-del", "filename": "delete-me.txt"})

	h := NewUploadsHandler(&config.AppConfig{})
	h.DeleteUploadedFile(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeleteUploadedFile_NotFound(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	req := httptest.NewRequest(http.MethodDelete, "/api/threads/thread-del/uploads/nonexistent.txt", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-del", "filename": "nonexistent.txt"})

	h := NewUploadsHandler(&config.AppConfig{})
	h.DeleteUploadedFile(c)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestDeleteUploadedFile_InvalidThreadID(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/api/threads/../x/uploads/file.txt", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "../x", "filename": "file.txt"})

	h := NewUploadsHandler(&config.AppConfig{})
	h.DeleteUploadedFile(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDeleteUploadedFile_MissingThreadID(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/api/threads//uploads/file.txt", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "", "filename": "file.txt"})

	h := NewUploadsHandler(&config.AppConfig{})
	h.DeleteUploadedFile(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDeleteUploadedFile_MissingFilename(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/api/threads/thread-1/uploads/", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-1", "filename": ""})

	h := NewUploadsHandler(&config.AppConfig{})
	h.DeleteUploadedFile(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDeleteUploadedFile_UnsafeFilename(t *testing.T) {
	// Note: sanitiseFilename strips path components, so "../file.txt" becomes "file.txt"
	// and is then treated as a normal filename, which results in 404 if file doesn't exist
	req := httptest.NewRequest(http.MethodDelete, "/api/threads/thread-1/uploads/../file.txt", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-1", "filename": "../file.txt"})

	h := NewUploadsHandler(&config.AppConfig{})
	h.DeleteUploadedFile(c)

	// After sanitisation, filename becomes "file.txt", which doesn't exist -> 404
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}
