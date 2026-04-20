// Package memory provides memory update logic for User/History contexts and facts.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"goclaw/pkg/errors"
)

// SectionUpdate represents a single section update with shouldUpdate flag.
type SectionUpdate struct {
	Summary      string `json:"summary"`
	ShouldUpdate bool   `json:"shouldUpdate"`
}

// MemoryUpdate represents a complete memory update from LLM extraction.
// Matches DeerFlow's expected output format.
type MemoryUpdate struct {
	User          UserUpdateSections    `json:"user"`
	History       HistoryUpdateSections `json:"history"`
	NewFacts      []NewFact             `json:"newFacts"`
	FactsToRemove []string              `json:"factsToRemove"`
}

// UserUpdateSections contains updates to user context sections.
type UserUpdateSections struct {
	WorkContext     SectionUpdate `json:"workContext"`
	PersonalContext SectionUpdate `json:"personalContext"`
	TopOfMind       SectionUpdate `json:"topOfMind"`
}

// HistoryUpdateSections contains updates to history context sections.
type HistoryUpdateSections struct {
	RecentMonths       SectionUpdate `json:"recentMonths"`
	EarlierContext     SectionUpdate `json:"earlierContext"`
	LongTermBackground SectionUpdate `json:"longTermBackground"`
}

// NewFact represents a new fact to be added.
type NewFact struct {
	Content     string  `json:"content"`
	Category    string  `json:"category"`
	Confidence  float64 `json:"confidence"`
	SourceError *string `json:"sourceError,omitempty"`
}

// HasUpdates returns true if any context section has an update or facts changed.
func (u *MemoryUpdate) HasUpdates() bool {
	return u.User.WorkContext.ShouldUpdate ||
		u.User.PersonalContext.ShouldUpdate ||
		u.User.TopOfMind.ShouldUpdate ||
		u.History.RecentMonths.ShouldUpdate ||
		u.History.EarlierContext.ShouldUpdate ||
		u.History.LongTermBackground.ShouldUpdate ||
		len(u.NewFacts) > 0 ||
		len(u.FactsToRemove) > 0
}

// LLMMemoryUpdater uses an LLM to extract full memory updates including
// User/History context summaries and facts.
type LLMMemoryUpdater struct {
	chatModel     model.BaseChatModel
	minConfidence float64
	timeout       time.Duration
}

// NewLLMMemoryUpdater creates a new LLM-based memory updater.
func NewLLMMemoryUpdater(chatModel model.BaseChatModel, minConfidence float64) *LLMMemoryUpdater {
	if minConfidence <= 0 {
		minConfidence = 0.7
	}
	return &LLMMemoryUpdater{
		chatModel:     chatModel,
		minConfidence: minConfidence,
		timeout:       30 * time.Second,
	}
}

// uploadSentenceRe matches sentences describing file upload events.
// Matches DeerFlow's _UPLOAD_SENTENCE_RE.
var uploadSentenceRe = regexp.MustCompile(`(?i)[^.!?]*\b(?:` +
	`upload(?:ed|ing)?(?:\s+\w+){0,3}\s+(?:file|files?|document|documents?|attachment|attachments?)` +
	`|file\s+upload` +
	`|/mnt/user-data/uploads/` +
	`|<uploaded_files>` +
	`)[^.!?]*[.!?]?\s*`)

// ExtractMemoryUpdate analyzes conversation and extracts complete memory updates.
func (u *LLMMemoryUpdater) ExtractMemoryUpdate(
	ctx context.Context,
	currentMem *Memory,
	messages []map[string]any,
	correctionDetected bool,
) (*MemoryUpdate, error) {
	if u.chatModel == nil {
		return nil, errors.ConfigError("memory updater: chat model is nil")
	}

	ctx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	// Format conversation, stripping uploaded_files tags.
	conversationText := formatConversationForUpdate(messages)

	// Build correction hint.
	correctionHint := ""
	if correctionDetected {
		correctionHint = "IMPORTANT: Explicit correction signals were detected in this conversation. " +
			"Pay special attention to what the agent got wrong, what the user corrected, " +
			"and record the correct approach as a fact with category \"correction\" and confidence >= 0.95 when appropriate."
	}

	// Build the prompt.
	currentMemJSON, _ := json.MarshalIndent(currentMem, "", "  ")
	prompt := fmt.Sprintf(MemoryUpdatePromptTemplate, string(currentMemJSON), conversationText, correctionHint)

	resp, err := u.chatModel.Generate(ctx, []*schema.Message{
		schema.UserMessage(prompt),
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		return nil, nil
	}

	return parseMemoryUpdate(resp.Content, u.minConfidence)
}

// formatConversationForUpdate formats messages for memory update prompt.
// Strips uploaded_files tags to avoid persisting ephemeral paths.
func formatConversationForUpdate(messages []map[string]any) string {
	var lines []string
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)

		if strings.TrimSpace(content) == "" {
			continue
		}

		// Strip uploaded_files tags from human messages.
		if role == "user" || role == "human" {
			content = regexp.MustCompile(`(?s)<uploaded_files>.*?</uploaded_files>\n*`).ReplaceAllString(content, "")
			content = strings.TrimSpace(content)
			if content == "" {
				continue
			}
		}

		// Truncate very long messages.
		if len(content) > 1000 {
			content = content[:1000] + "..."
		}

		if role == "user" || role == "human" {
			lines = append(lines, fmt.Sprintf("User: %s", content))
		} else if role == "assistant" || role == "ai" {
			lines = append(lines, fmt.Sprintf("Assistant: %s", content))
		}
	}
	return strings.Join(lines, "\n\n")
}

// ApplyUpdates applies the extracted updates to the memory document.
// Returns true if any changes were made.
func ApplyUpdates(mem *Memory, update *MemoryUpdate, threadID string) bool {
	if mem == nil || update == nil {
		return false
	}

	now := time.Now().UTC().Format(time.RFC3339)
	updated := false

	// Apply User context updates.
	if update.User.WorkContext.ShouldUpdate && strings.TrimSpace(update.User.WorkContext.Summary) != "" {
		mem.User.WorkContext = ContextSection{
			Summary:   update.User.WorkContext.Summary,
			UpdatedAt: now,
		}
		updated = true
	}
	if update.User.PersonalContext.ShouldUpdate && strings.TrimSpace(update.User.PersonalContext.Summary) != "" {
		mem.User.PersonalContext = ContextSection{
			Summary:   update.User.PersonalContext.Summary,
			UpdatedAt: now,
		}
		updated = true
	}
	if update.User.TopOfMind.ShouldUpdate && strings.TrimSpace(update.User.TopOfMind.Summary) != "" {
		mem.User.TopOfMind = ContextSection{
			Summary:   update.User.TopOfMind.Summary,
			UpdatedAt: now,
		}
		updated = true
	}

	// Apply History context updates.
	if update.History.RecentMonths.ShouldUpdate && strings.TrimSpace(update.History.RecentMonths.Summary) != "" {
		mem.History.RecentMonths = ContextSection{
			Summary:   update.History.RecentMonths.Summary,
			UpdatedAt: now,
		}
		updated = true
	}
	if update.History.EarlierContext.ShouldUpdate && strings.TrimSpace(update.History.EarlierContext.Summary) != "" {
		mem.History.EarlierContext = ContextSection{
			Summary:   update.History.EarlierContext.Summary,
			UpdatedAt: now,
		}
		updated = true
	}
	if update.History.LongTermBackground.ShouldUpdate && strings.TrimSpace(update.History.LongTermBackground.Summary) != "" {
		mem.History.LongTermBackground = ContextSection{
			Summary:   update.History.LongTermBackground.Summary,
			UpdatedAt: now,
		}
		updated = true
	}

	// Remove facts.
	if len(update.FactsToRemove) > 0 {
		removeSet := make(map[string]struct{})
		for _, id := range update.FactsToRemove {
			removeSet[id] = struct{}{}
		}
		filtered := make([]MemoryFact, 0, len(mem.Facts))
		for _, f := range mem.Facts {
			if _, ok := removeSet[f.ID]; !ok {
				filtered = append(filtered, f)
			}
		}
		if len(filtered) != len(mem.Facts) {
			mem.Facts = filtered
			updated = true
		}
	}

	// Add new facts.
	if len(update.NewFacts) > 0 {
		// Build existing content keys for deduplication.
		existingKeys := make(map[string]struct{})
		for _, f := range mem.Facts {
			key := strings.ToLower(strings.TrimSpace(f.Content))
			if key != "" {
				existingKeys[key] = struct{}{}
			}
		}

		for _, f := range update.NewFacts {
			content := strings.TrimSpace(f.Content)
			if content == "" {
				continue
			}
			confidence := f.Confidence
			if confidence <= 0 {
				confidence = 0.8
			}
			if confidence < 0.7 {
				continue
			}

			// Check for duplicate.
			key := strings.ToLower(content)
			if _, ok := existingKeys[key]; ok {
				continue
			}

			fact := MemoryFact{
				ID:         generateFactID(),
				Content:    content,
				Category:   f.Category,
				Confidence: confidence,
				CreatedAt:  now,
				Source:     threadID,
			}
			if f.SourceError != nil && strings.TrimSpace(*f.SourceError) != "" {
				se := strings.TrimSpace(*f.SourceError)
				fact.SourceError = &se
			}
			mem.Facts = append(mem.Facts, fact)
			existingKeys[key] = struct{}{}
			updated = true
		}
	}

	// Strip upload mentions from all summaries before saving.
	stripUploadMentionsFromMemory(mem)

	if updated {
		mem.LastUpdated = now
	}
	return updated
}

// stripUploadMentionsFromMemory removes upload-related sentences from memory.
// Matches DeerFlow's _strip_upload_mentions_from_memory.
func stripUploadMentionsFromMemory(mem *Memory) {
	// Scrub summaries in user sections.
	mem.User.WorkContext.Summary = cleanUploadMentions(mem.User.WorkContext.Summary)
	mem.User.PersonalContext.Summary = cleanUploadMentions(mem.User.PersonalContext.Summary)
	mem.User.TopOfMind.Summary = cleanUploadMentions(mem.User.TopOfMind.Summary)

	// Scrub summaries in history sections.
	mem.History.RecentMonths.Summary = cleanUploadMentions(mem.History.RecentMonths.Summary)
	mem.History.EarlierContext.Summary = cleanUploadMentions(mem.History.EarlierContext.Summary)
	mem.History.LongTermBackground.Summary = cleanUploadMentions(mem.History.LongTermBackground.Summary)

	// Remove facts that describe upload events.
	if len(mem.Facts) > 0 {
		filtered := make([]MemoryFact, 0, len(mem.Facts))
		for _, f := range mem.Facts {
			if !uploadSentenceRe.MatchString(f.Content) {
				filtered = append(filtered, f)
			}
		}
		mem.Facts = filtered
	}
}

// cleanUploadMentions removes upload-related sentences from text.
func cleanUploadMentions(text string) string {
	cleaned := uploadSentenceRe.ReplaceAllString(text, "")
	cleaned = regexp.MustCompile(`\s{2,}`).ReplaceAllString(cleaned, " ")
	return strings.TrimSpace(cleaned)
}

func generateFactID() string {
	return fmt.Sprintf("fact_%d_%d", time.Now().Unix(), time.Now().Nanosecond()%1000)
}

func parseMemoryUpdate(raw string, minConfidence float64) (*MemoryUpdate, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, nil
	}

	// Strip markdown code fences.
	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```")
		if idx := strings.Index(clean, "\n"); idx >= 0 {
			clean = clean[idx+1:]
		}
		if end := strings.LastIndex(clean, "```"); end >= 0 {
			clean = clean[:end]
		}
		clean = strings.TrimSpace(clean)
	}

	// Extract JSON object if wrapped in text.
	start := strings.Index(clean, "{")
	end := strings.LastIndex(clean, "}")
	if start >= 0 && end > start {
		clean = clean[start : end+1]
	}

	if clean == "" || clean == "{}" {
		return nil, nil
	}

	var update MemoryUpdate
	if err := json.Unmarshal([]byte(clean), &update); err != nil {
		return nil, errors.WrapInternalError(err, "parse memory update")
	}

	// Filter new facts by confidence.
	filtered := make([]NewFact, 0, len(update.NewFacts))
	for _, f := range update.NewFacts {
		if f.Confidence <= 0 {
			f.Confidence = 0.8
		}
		if f.Confidence >= minConfidence {
			filtered = append(filtered, f)
		}
	}
	update.NewFacts = filtered

	return &update, nil
}

// MemoryUpdatePromptTemplate matches DeerFlow's MEMORY_UPDATE_PROMPT.
const MemoryUpdatePromptTemplate = `You are a memory management system. Your task is to analyze a conversation and update the user's memory profile.

Current Memory State:
<current_memory>
%s
</current_memory>

New Conversation to Process:
<conversation>
%s
</conversation>

Instructions:
1. Analyze the conversation for important information about the user
2. Extract relevant facts, preferences, and context with specific details (numbers, names, technologies)
3. Update the memory sections as needed following the detailed length guidelines below

Before extracting facts, perform a structured reflection on the conversation:
1. Error/Retry Detection: Did the agent encounter errors, require retries, or produce incorrect results?
   If yes, record the root cause and correct approach as a high-confidence fact with category "correction".
2. User Correction Detection: Did the user correct the agent's direction, understanding, or output?
   If yes, record the correct interpretation or approach as a high-confidence fact with category "correction".
   Include what went wrong in "sourceError" only when category is "correction" and the mistake is explicit in the conversation.
3. Project Constraint Discovery: Were any project-specific constraints discovered during the conversation?
   If yes, record them as facts with the most appropriate category and confidence.

%s

Memory Section Guidelines:

**User Context** (Current state - concise summaries):
- workContext: Professional role, company, key projects, main technologies (2-3 sentences)
  Example: Core contributor, project names with metrics (16k+ stars), technical stack
- personalContext: Languages, communication preferences, key interests (1-2 sentences)
  Example: Bilingual capabilities, specific interest areas, expertise domains
- topOfMind: Multiple ongoing focus areas and priorities (3-5 sentences, detailed paragraph)
  Example: Primary project work, parallel technical investigations, ongoing learning/tracking
  Include: Active implementation work, troubleshooting issues, market/research interests
  Note: This captures SEVERAL concurrent focus areas, not just one task

**History** (Temporal context - rich paragraphs):
- recentMonths: Detailed summary of recent activities (4-6 sentences or 1-2 paragraphs)
  Timeline: Last 1-3 months of interactions
  Include: Technologies explored, projects worked on, problems solved, interests demonstrated
- earlierContext: Important historical patterns (3-5 sentences or 1 paragraph)
  Timeline: 3-12 months ago
  Include: Past projects, learning journeys, established patterns
- longTermBackground: Persistent background and foundational context (2-4 sentences)
  Timeline: Overall/foundational information
  Include: Core expertise, longstanding interests, fundamental working style

**Facts Extraction**:
- Extract specific, quantifiable details (e.g., "16k+ GitHub stars", "200+ datasets")
- Include proper nouns (company names, project names, technology names)
- Preserve technical terminology and version numbers
- Categories:
  * preference: Tools, styles, approaches user prefers/dislikes
  * knowledge: Specific expertise, technologies mastered, domain knowledge
  * context: Background facts (job title, projects, locations, languages)
  * behavior: Working patterns, communication habits, problem-solving approaches
  * goal: Stated objectives, learning targets, project ambitions
  * correction: Explicit agent mistakes or user corrections, including the correct approach
- Confidence levels:
  * 0.9-1.0: Explicitly stated facts ("I work on X", "My role is Y")
  * 0.7-0.8: Strongly implied from actions/discussions
  * 0.5-0.6: Inferred patterns (use sparingly, only for clear patterns)

**What Goes Where**:
- workContext: Current job, active projects, primary tech stack
- personalContext: Languages, personality, interests outside direct work tasks
- topOfMind: Multiple ongoing priorities and focus areas user cares about recently (gets updated most frequently)
  Should capture 3-5 concurrent themes: main work, side explorations, learning/tracking interests
- recentMonths: Detailed account of recent technical explorations and work
- earlierContext: Patterns from slightly older interactions still relevant
- longTermBackground: Unchanging foundational facts about the user

**Multilingual Content**:
- Preserve original language for proper nouns and company names
- Keep technical terms in their original form (DeepSeek, LangGraph, etc.)
- Note language capabilities in personalContext

Output Format (JSON):
{
  "user": {
    "workContext": { "summary": "...", "shouldUpdate": true/false },
    "personalContext": { "summary": "...", "shouldUpdate": true/false },
    "topOfMind": { "summary": "...", "shouldUpdate": true/false }
  },
  "history": {
    "recentMonths": { "summary": "...", "shouldUpdate": true/false },
    "earlierContext": { "summary": "...", "shouldUpdate": true/false },
    "longTermBackground": { "summary": "...", "shouldUpdate": true/false }
  },
  "newFacts": [
    { "content": "...", "category": "preference|knowledge|context|behavior|goal|correction", "confidence": 0.0-1.0 }
  ],
  "factsToRemove": ["fact_id_1", "fact_id_2"]
}

Important Rules:
- Only set shouldUpdate=true if there's meaningful new information
- Follow length guidelines: workContext/personalContext are concise (1-3 sentences), topOfMind and history sections are detailed (paragraphs)
- Include specific metrics, version numbers, and proper nouns in facts
- Only add facts that are clearly stated (0.9+) or strongly implied (0.7+)
- Use category "correction" for explicit agent mistakes or user corrections; assign confidence >= 0.95 when the correction is explicit
- Include "sourceError" only for explicit correction facts when the prior mistake or wrong approach is clearly stated; omit it otherwise
- Remove facts that are contradicted by new information
- When updating topOfMind, integrate new focus areas while removing completed/abandoned ones
  Keep 3-5 concurrent focus themes that are still active and relevant
- For history sections, integrate new information chronologically into appropriate time period
- Preserve technical accuracy - keep exact names of technologies, companies, projects
- Focus on information useful for future interactions and personalization
- IMPORTANT: Do NOT record file upload events in memory. Uploaded files are
  session-specific and ephemeral — they will not be accessible in future sessions.
  Recording upload events causes confusion in subsequent conversations.

Return ONLY valid JSON, no explanation or markdown.`
