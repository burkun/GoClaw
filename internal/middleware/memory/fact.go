package memory

// Fact represents a structured fact extracted from conversation.
type Fact struct {
	Content    string  `json:"content"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
}

// FactExtractor extracts facts from conversation messages using an LLM.
type FactExtractor interface {
	// Extract returns facts derived from the given messages.
	// correctionDetected indicates the user corrected a previous response.
	Extract(messages []map[string]any, correctionDetected bool) ([]Fact, error)
}
