package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// InputHandler handles user input from the terminal.
type InputHandler struct {
	reader *bufio.Reader
}

// NewInputHandler creates a new input handler.
func NewInputHandler() *InputHandler {
	return &InputHandler{
		reader: bufio.NewReader(os.Stdin),
	}
}

// ReadLine reads a line of input from the user.
func (h *InputHandler) ReadLine(threadID string) (string, error) {
	// Create prompt
	prompt := "> "
	if threadID != "" {
		// Show first 8 chars of thread ID
		shortID := threadID
		if len(threadID) > 8 {
			shortID = threadID[:8]
		}
		prompt = fmt.Sprintf("[%s] > ", shortID)
	}

	return h.readSimpleLine(prompt)
}

// readSimpleLine reads a line with a prompt.
func (h *InputHandler) readSimpleLine(prompt string) (string, error) {
	fmt.Print(prompt)
	line, err := h.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// Close closes the input handler.
func (h *InputHandler) Close() error {
	return nil
}

// ReadMultiline reads multiple lines until an empty line or EOF.
func (h *InputHandler) ReadMultiline(prompt string) (string, error) {
	var lines []string
	for {
		line, err := h.ReadLine("")
		if err != nil {
			if err.Error() == "EOF" && len(lines) > 0 {
				break
			}
			return "", err
		}
		if line == "" {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}
