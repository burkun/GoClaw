package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bookerbai/goclaw/internal/config"
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
