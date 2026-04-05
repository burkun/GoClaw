package memory

// FactCategory represents the category of a fact.
// These categories match DeerFlow's fact categorization system.
type FactCategory string

const (
	// CategoryPreference represents user preferences (likes/dislikes, styles, tools).
	// Example: "Prefers Go over Python for backend services"
	CategoryPreference FactCategory = "preference"
	
	// CategoryKnowledge represents user's expertise or knowledge areas.
	// Example: "Expert in Rust and WebAssembly"
	CategoryKnowledge FactCategory = "knowledge"
	
	// CategoryContext represents background context (location, job, projects).
	// Example: "Works at ByteDance as a senior engineer"
	CategoryContext FactCategory = "context"
	
	// CategoryBehavior represents behavioral patterns.
	// Example: "Habit of writing tests before implementation"
	CategoryBehavior FactCategory = "behavior"
	
	// CategoryGoal represents user's goals or objectives.
	// Example: "Planning to release v2.0 in Q2"
	CategoryGoal FactCategory = "goal"
	
	// CategoryCorrection represents explicit corrections or mistakes to avoid.
	// Example: "User corrected: prefers dark mode over light mode"
	CategoryCorrection FactCategory = "correction"
)

// IsValid returns true if the category is one of the valid categories.
func (fc FactCategory) IsValid() bool {
	switch fc {
	case CategoryPreference, CategoryKnowledge, CategoryContext, CategoryBehavior, CategoryGoal, CategoryCorrection:
		return true
	default:
		return false
	}
}

// String returns the string representation of the category.
func (fc FactCategory) String() string {
	return string(fc)
}

// Fact represents a structured fact extracted from conversation.
type Fact struct {
	Content    string        `json:"content"`
	Category   FactCategory  `json:"category"`
	Confidence float64       `json:"confidence"`
}

// FactExtractor extracts facts from conversation messages using an LLM.
type FactExtractor interface {
	// Extract returns facts derived from the given messages.
	// correctionDetected indicates the user corrected a previous response.
	Extract(messages []map[string]any, correctionDetected bool) ([]Fact, error)
}
