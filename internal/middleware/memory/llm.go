package memory

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Correction detection helpers (mirrors DeerFlow detect_correction)
// ---------------------------------------------------------------------------

var correctionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bthat(?:'s| is) (?:wrong|incorrect)\b`),
	regexp.MustCompile(`(?i)\byou misunderstood\b`),
	regexp.MustCompile(`(?i)\btry again\b`),
	regexp.MustCompile(`(?i)\bredo\b`),
	regexp.MustCompile(`不对`),
	regexp.MustCompile(`你理解错了`),
	regexp.MustCompile(`重试`),
}

var memorySentenceSplitRe = regexp.MustCompile(`[\n。！？!?；;]+`)

// detectCorrection returns true if any recent human message contains an
// explicit correction signal. It inspects at most the last 6 messages.
func detectCorrection(messages []map[string]any) bool {
	var recent []map[string]any
	for _, m := range messages {
		if m["role"] == "human" {
			recent = append(recent, m)
		}
	}
	if len(recent) > 6 {
		recent = recent[len(recent)-6:]
	}
	for _, m := range recent {
		content, _ := m["content"].(string)
		for _, re := range correctionPatterns {
			if re.MatchString(content) {
				return true
			}
		}
	}
	return false
}

// filterMessagesForMemory removes intermediate tool messages and AI messages
// that contain tool_calls, keeping only human turns and final AI responses.
// This mirrors DeerFlow's _filter_messages_for_memory.
func filterMessagesForMemory(messages []map[string]any) []map[string]any {
	var filtered []map[string]any
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		switch role {
		case "tool":
			// Intermediate tool result — skip.
			continue
		case "assistant":
			if calls, ok := msg["tool_calls"]; ok && calls != nil {
				// Intermediate step with pending tool calls — skip.
				continue
			}
			filtered = append(filtered, msg)
		case "human":
			// Strip ephemeral <uploaded_files> blocks before persisting.
			content, _ := msg["content"].(string)
			if strings.Contains(content, "<uploaded_files>") {
				cleaned := regexp.MustCompile(`(?s)<uploaded_files>.*?</uploaded_files>\n*`).ReplaceAllString(content, "")
				cleaned = strings.TrimSpace(cleaned)
				if cleaned == "" {
					continue
				}
				msg = copyMsg(msg, cleaned)
			}
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

// copyMsg returns a shallow copy of msg with content overridden.
func copyMsg(msg map[string]any, content string) map[string]any {
	out := make(map[string]any, len(msg))
	for k, v := range msg {
		out[k] = v
	}
	out["content"] = content
	return out
}

// deriveFactsFromMessages extracts facts from messages using rule-based approach.
// This is a fallback when LLM extraction is not available.
func deriveFactsFromMessages(messages []map[string]any, correctionDetected bool) []Fact {
	facts := make([]Fact, 0, 8)
	seen := make(map[string]struct{})
	add := func(content string, category FactCategory, confidence float64) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		key := strings.ToLower(content)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		if string(category) == "" {
			category = CategoryContext
		}
		if confidence <= 0 {
			confidence = 0.8
		}
		facts = append(facts, Fact{Content: content, Category: category, Confidence: confidence})
	}

	if correctionDetected {
		add("User corrected a previous assistant response.", CategoryCorrection, 0.95)
	}

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		role, _ := msg["role"].(string)
		if role != "human" {
			continue
		}
		content, _ := msg["content"].(string)
		for _, seg := range memorySentenceSplitRe.Split(content, -1) {
			seg = strings.TrimSpace(seg)
			if seg == "" || len(seg) < 6 || len(seg) > 240 {
				continue
			}
			if strings.Contains(seg, "?") || strings.Contains(seg, "？") {
				continue
			}
			add(seg, CategoryContext, 0.8)
			if len(facts) >= 8 {
				return facts
			}
		}
	}
	return facts
}
