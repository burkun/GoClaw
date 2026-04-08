package memory

import (
	"sort"
	"strings"
)

// selectFactsForInjection selects facts for injection into the system prompt.
// It filters by maxFacts and maxTokens limits.
func selectFactsForInjection(allFacts []MemoryFact, maxFacts, maxTokens int) []MemoryFact {
	if len(allFacts) == 0 {
		return nil
	}
	if maxFacts <= 0 {
		maxFacts = len(allFacts)
	}
	facts := allFacts
	if len(facts) > maxFacts {
		facts = facts[len(facts)-maxFacts:]
	}
	if maxTokens <= 0 {
		return append([]MemoryFact(nil), facts...)
	}

	selectedRev := make([]MemoryFact, 0, len(facts))
	used := 0
	for i := len(facts) - 1; i >= 0; i-- {
		c := strings.TrimSpace(facts[i].Content)
		if c == "" {
			continue
		}
		tokens := estimateTokenCount(c)
		if tokens <= 0 {
			continue
		}
		if used+tokens > maxTokens {
			if len(selectedRev) == 0 {
				selectedRev = append(selectedRev, facts[i])
			}
			break
		}
		selectedRev = append(selectedRev, facts[i])
		used += tokens
	}

	out := make([]MemoryFact, 0, len(selectedRev))
	for i := len(selectedRev) - 1; i >= 0; i-- {
		out = append(out, selectedRev[i])
	}
	return out
}

// formatMemoryForInjection formats the complete memory structure for injection.
// It includes User Context, History, and Facts in a structured XML format.
// This mirrors DeerFlow's format_memory_for_injection() from prompt.py.
func formatMemoryForInjection(mem *Memory, factContents []string) string {
	if mem == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<memory>\n")

	hasContent := false

	// User Context section
	userHasContent := mem.User.WorkContext.Summary != "" ||
		mem.User.PersonalContext.Summary != "" ||
		mem.User.TopOfMind.Summary != ""

	if userHasContent {
		hasContent = true
		sb.WriteString("User Context:\n")
		if mem.User.WorkContext.Summary != "" {
			sb.WriteString("- Work: ")
			sb.WriteString(mem.User.WorkContext.Summary)
			sb.WriteString("\n")
		}
		if mem.User.PersonalContext.Summary != "" {
			sb.WriteString("- Personal: ")
			sb.WriteString(mem.User.PersonalContext.Summary)
			sb.WriteString("\n")
		}
		if mem.User.TopOfMind.Summary != "" {
			sb.WriteString("- Current Focus: ")
			sb.WriteString(mem.User.TopOfMind.Summary)
			sb.WriteString("\n")
		}
	}

	// History section
	historyHasContent := mem.History.RecentMonths.Summary != "" ||
		mem.History.EarlierContext.Summary != "" ||
		mem.History.LongTermBackground.Summary != ""

	if historyHasContent {
		hasContent = true
		if userHasContent {
			sb.WriteString("\n")
		}
		sb.WriteString("History:\n")
		if mem.History.RecentMonths.Summary != "" {
			sb.WriteString("- Recent: ")
			sb.WriteString(mem.History.RecentMonths.Summary)
			sb.WriteString("\n")
		}
		if mem.History.EarlierContext.Summary != "" {
			sb.WriteString("- Earlier: ")
			sb.WriteString(mem.History.EarlierContext.Summary)
			sb.WriteString("\n")
		}
		if mem.History.LongTermBackground.Summary != "" {
			sb.WriteString("- Long-term: ")
			sb.WriteString(mem.History.LongTermBackground.Summary)
			sb.WriteString("\n")
		}
	}

	// Facts section
	if len(factContents) > 0 {
		hasContent = true
		if userHasContent || historyHasContent {
			sb.WriteString("\n")
		}
		sb.WriteString("Facts:\n")
		for _, fact := range factContents {
			if fact = strings.TrimSpace(fact); fact != "" {
				sb.WriteString("- ")
				sb.WriteString(fact)
				sb.WriteString("\n")
			}
		}
	}

	sb.WriteString("</memory>")

	if !hasContent {
		return ""
	}

	return sb.String()
}

// estimateTokenCount estimates the token count for a given text.
// This uses an improved algorithm that considers both ASCII and non-ASCII characters:
// - ASCII characters (English, numbers, symbols): ~4 chars per token
// - Non-ASCII characters (Chinese, Japanese, etc.): ~1.5 chars per token
// This provides better accuracy for mixed-language content without requiring tiktoken.
func estimateTokenCount(text string) int {
	text = strings.TrimSpace(text)
	if len(text) == 0 {
		return 0
	}

	asciiCount := 0
	nonAsciiCount := 0

	for _, r := range text {
		if r < 128 {
			asciiCount++
		} else {
			nonAsciiCount++
		}
	}

	// ASCII: ~4 chars per token (GPT tokenizer behavior)
	// Non-ASCII: ~1.5 chars per token (CJK characters typically use more tokens)
	asciiTokens := (asciiCount + 3) / 4
	nonAsciiTokens := (nonAsciiCount*2 + 2) / 3 // *2/3 ≈ /1.5

	return asciiTokens + nonAsciiTokens
}

// sortFactsByConfidence sorts facts by confidence (descending) for retention.
func sortFactsByConfidence(facts []MemoryFact) []MemoryFact {
	sorted := make([]MemoryFact, len(facts))
	copy(sorted, facts)
	// Use standard library sort for O(n log n) efficiency
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Confidence > sorted[j].Confidence
	})
	return sorted
}
