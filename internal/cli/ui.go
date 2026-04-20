package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/fatih/color"
)

// UI handles terminal rendering.
type UI struct {
	output    io.Writer
	errOutput io.Writer
	printMode bool

	// Colors
	userColor   *color.Color
	assistantColor *color.Color
	toolColor   *color.Color
	errorColor  *color.Color
	thinkingColor *color.Color
	taskColor   *color.Color

	// State
	mu             sync.Mutex
	inAssistantMsg bool
}

// NewUI creates a new UI instance.
func NewUI(output, errOutput io.Writer, printMode bool) *UI {
	return &UI{
		output:    output,
		errOutput: errOutput,
		printMode: printMode,
		userColor:     color.New(color.FgCyan, color.Bold),
		assistantColor: color.New(color.FgGreen),
		toolColor:     color.New(color.FgYellow),
		errorColor:    color.New(color.FgRed),
		thinkingColor: color.New(color.FgHiBlack),
		taskColor:     color.New(color.FgMagenta),
	}
}

// Print prints a message.
func (u *UI) Print(msg string) {
	fmt.Fprint(u.output, msg)
}

// Println prints a message with newline.
func (u *UI) Println(msg string) {
	fmt.Fprintln(u.output, msg)
}

// Printf prints a formatted message.
func (u *UI) Printf(format string, args ...any) {
	fmt.Fprintf(u.output, format, args...)
}

// PrintError prints an error message.
func (u *UI) PrintError(err error) {
	u.errorColor.Fprintln(u.errOutput, "✗ Error: "+err.Error())
}

// PrintWelcome prints the welcome message.
func (u *UI) PrintWelcome() {
	if u.printMode {
		return
	}
	welcome := `
  ____ _     ___   ____
 / ___| |   / _ \ / ___|
| |  _| |  | | | | |  _
| |_| | |__| |_| | |_| |
 \____|_____\___/ \____|

GoClaw CLI - Interactive AI Agent
Type /help for available commands.

`
	fmt.Fprint(u.output, welcome)
}

// Clear clears the terminal screen.
func (u *UI) Clear() {
	fmt.Fprint(u.output, "\033[2J\033[H")
}

// PrintUserMessage displays a user message.
func (u *UI) PrintUserMessage(content string) {
	if u.printMode {
		return
	}
	u.userColor.Fprintln(u.output, "\n┌─ You ─────────────────────────────────────────")
	fmt.Fprintln(u.output, content)
	u.userColor.Fprintln(u.output, "└───────────────────────────────────────────────")
}

// StartAssistantMessage begins an assistant message block.
func (u *UI) StartAssistantMessage() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.inAssistantMsg = true
	if !u.printMode {
		u.assistantColor.Fprintln(u.output, "\n┌─ Assistant ───────────────────────────────────")
	}
}

// EndAssistantMessage ends an assistant message block.
func (u *UI) EndAssistantMessage() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.inAssistantMsg = false
	if !u.printMode {
		u.assistantColor.Fprintln(u.output, "\n└───────────────────────────────────────────────")
	}
}

// PrintAssistantDelta prints streaming assistant content.
func (u *UI) PrintAssistantDelta(content string) {
	fmt.Fprint(u.output, content)
}

// PrintThinking prints thinking/reasoning content.
func (u *UI) PrintThinking(content string) {
	if u.printMode {
		return
	}
	u.thinkingColor.Fprint(u.output, content)
}

// PrintToolCall displays a tool call.
func (u *UI) PrintToolCall(name, input string) {
	if u.printMode {
		return
	}
	u.toolColor.Fprintf(u.output, "\n  🔧 %s", name)
	if input != "" && len(input) < 200 {
		// Try to pretty-print JSON
		var jsonObj map[string]any
		if err := json.Unmarshal([]byte(input), &jsonObj); err == nil {
			prettyInput, _ := json.MarshalIndent(jsonObj, "     ", "  ")
			fmt.Fprintf(u.output, "\n     %s", string(prettyInput))
		} else {
			fmt.Fprintf(u.output, "\n     %s", truncate(input, 100))
		}
	}
	fmt.Fprintln(u.output)
}

// PrintToolResult displays a tool result.
func (u *UI) PrintToolResult(name, output string, isError bool) {
	if u.printMode {
		return
	}
	if isError {
		u.errorColor.Fprintf(u.output, "  ❌ %s: %s\n", name, truncate(output, 200))
	} else {
		u.toolColor.Fprintf(u.output, "  ✅ %s: %s\n", name, truncate(output, 100))
	}
}

// PrintTaskEvent displays a task event.
func (u *UI) PrintTaskEvent(eventType, taskID, subject, status string) {
	if u.printMode {
		return
	}
	var icon string
	switch eventType {
	case "task_started":
		icon = "▶️"
	case "task_running":
		icon = "🔄"
	case "task_completed":
		icon = "✅"
	case "task_failed":
		icon = "❌"
	case "task_timed_out":
		icon = "⏱️"
	default:
		icon = "📋"
	}
	u.taskColor.Fprintf(u.output, "  %s Task [%s]: %s (%s)\n", icon, taskID[:8], subject, status)
}

// PrintDivider prints a visual divider.
func (u *UI) PrintDivider() {
	if u.printMode {
		return
	}
	fmt.Fprintln(u.output, "\n─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─\n")
}

// Helper functions

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
