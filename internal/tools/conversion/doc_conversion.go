// Package conversion provides document format conversion utilities.
// It supports converting Office documents (PDF, PPT, Word, Excel) to Markdown
// using external tools like pandoc when available.
package conversion

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Converter converts documents to Markdown format.
type Converter struct {
	pandocPath string
	mu         sync.RWMutex
}

var (
	defaultConverterOnce sync.Once
	defaultConverter     *Converter
)

// DefaultConverter returns a shared Converter instance.
func DefaultConverter() *Converter {
	defaultConverterOnce.Do(func() {
		defaultConverter = &Converter{}
		defaultConverter.detectPandoc()
	})
	return defaultConverter
}

// detectPandoc checks if pandoc is available on the system.
func (c *Converter) detectPandoc() {
	path, err := exec.LookPath("pandoc")
	if err == nil {
		c.mu.Lock()
		c.pandocPath = path
		c.mu.Unlock()
	}
}

// HasPandoc returns true if pandoc is available.
func (c *Converter) HasPandoc() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pandocPath != ""
}

// CanConvert returns true if the file extension is supported for conversion.
func (c *Converter) CanConvert(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx":
		return c.HasPandoc()
	case ".md", ".markdown", ".txt", ".rst", ".html", ".htm":
		return true
	default:
		return false
	}
}

// NeedsConversion returns true if the file needs conversion to Markdown.
func (c *Converter) NeedsConversion(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".md", ".markdown", ".txt":
		return false
	default:
		return c.CanConvert(filename)
	}
}

// ConvertToMarkdown converts a document file to Markdown format.
// Returns the Markdown content or an error.
// For plain text and Markdown files, it returns the content directly.
func (c *Converter) ConvertToMarkdown(ctx context.Context, filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	// Read plain text files directly
	switch ext {
	case ".md", ".markdown", ".txt":
		content, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("read file: %w", err)
		}
		return string(content), nil
	}

	// Use pandoc for document conversion
	if c.HasPandoc() {
		return c.convertWithPandoc(ctx, filePath)
	}

	// Fallback: read as text
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	// Check if content is binary
	if isBinary(content) {
		return "", fmt.Errorf("unsupported binary file format: %s (install pandoc for conversion)", ext)
	}

	return string(content), nil
}

// convertWithPandoc uses pandoc to convert a document to Markdown.
func (c *Converter) convertWithPandoc(ctx context.Context, filePath string) (string, error) {
	c.mu.RLock()
	pandocPath := c.pandocPath
	c.mu.RUnlock()

	if pandocPath == "" {
		return "", fmt.Errorf("pandoc not available")
	}

	// Run pandoc to convert to Markdown
	cmd := exec.CommandContext(ctx, pandocPath, "-f", "auto", "-t", "markdown", filePath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pandoc conversion failed: %w, stderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// isBinary checks if content appears to be binary data.
func isBinary(content []byte) bool {
	// Check first 512 bytes for null bytes (common binary indicator)
	checkLen := len(content)
	if checkLen > 512 {
		checkLen = 512
	}
	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			return true
		}
	}
	return false
}

// ConvertFileToMarkdown is a convenience function that converts a file to Markdown.
func ConvertFileToMarkdown(ctx context.Context, filePath string) (string, error) {
	return DefaultConverter().ConvertToMarkdown(ctx, filePath)
}

// SupportsExtension returns true if the extension is supported.
func SupportsExtension(ext string) bool {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	switch ext {
	case ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx":
		return true
	case ".md", ".markdown", ".txt", ".rst", ".html", ".htm":
		return true
	default:
		return false
	}
}
