package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"goclaw/internal/config"

	"github.com/cloudwego/eino/schema"
)

// Message represents a conversation message.
type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

// Session manages the conversation session state.
type Session struct {
	cfg       *config.AppConfig
	threadID  string
	messages  []*Message
	modelName string
	threadDir string
}

// NewSession creates a new session.
func NewSession(cfg *config.AppConfig, threadID string) *Session {
	s := &Session{
		cfg:      cfg,
		threadID: threadID,
		messages: make([]*Message, 0),
	}
	s.threadDir = filepath.Join(".goclaw", "threads")
	return s
}

// ThreadID returns the current thread ID.
func (s *Session) ThreadID() string {
	return s.threadID
}

// GetMessages returns all messages in the session as schema.Messages.
func (s *Session) GetMessages() []*schema.Message {
	result := make([]*schema.Message, len(s.messages))
	for i, m := range s.messages {
		switch m.Role {
		case "human":
			result[i] = schema.UserMessage(m.Content)
		case "assistant":
			result[i] = schema.AssistantMessage(m.Content, nil)
		case "system":
			result[i] = schema.SystemMessage(m.Content)
		default:
			result[i] = schema.UserMessage(m.Content)
		}
	}
	return result
}

// AddMessage adds a message to the session.
func (s *Session) AddMessage(role, content string) {
	s.messages = append(s.messages, &Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
	})
	s.save()
}

// LoadThread loads a thread by ID.
func (s *Session) LoadThread(threadID string) error {
	if threadID == "" {
		return nil
	}

	threadPath := filepath.Join(s.threadDir, threadID, "session.json")
	data, err := os.ReadFile(threadPath)
	if err != nil {
		return err
	}

	var loaded struct {
		ThreadID string     `json:"thread_id"`
		Messages []*Message `json:"messages"`
	}
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}

	s.threadID = threadID
	s.messages = loaded.Messages
	return nil
}

// LoadLastThread loads the most recent thread.
func (s *Session) LoadLastThread() error {
	entries, err := os.ReadDir(s.threadDir)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		return nil
	}

	// Find most recent thread
	var threads []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			threads = append(threads, entry)
		}
	}

	if len(threads) == 0 {
		return nil
	}

	// Sort by modification time
	sort.Slice(threads, func(i, j int) bool {
		infoI, _ := threads[i].Info()
		infoJ, _ := threads[j].Info()
		return infoI.ModTime().After(infoJ.ModTime())
	})

	return s.LoadThread(threads[0].Name())
}

// save persists the session to disk.
func (s *Session) save() error {
	if s.threadID == "" {
		return nil
	}

	threadPath := filepath.Join(s.threadDir, s.threadID)
	if err := os.MkdirAll(threadPath, 0755); err != nil {
		return err
	}

	sessionPath := filepath.Join(threadPath, "session.json")
	data := struct {
		ThreadID  string     `json:"thread_id"`
		UpdatedAt int64      `json:"updated_at"`
		Messages  []*Message `json:"messages"`
	}{
		ThreadID:  s.threadID,
		UpdatedAt: time.Now().UnixMilli(),
		Messages:  s.messages,
	}

	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(sessionPath, content, 0644)
}

// Clear clears the session messages.
func (s *Session) Clear() {
	s.messages = make([]*Message, 0)
}

// Export exports the session to a string format.
func (s *Session) Export() string {
	var b strings.Builder
	for _, m := range s.messages {
		b.WriteString("## ")
		b.WriteString(m.Role)
		b.WriteString("\n\n")
		b.WriteString(m.Content)
		b.WriteString("\n\n")
	}
	return b.String()
}
