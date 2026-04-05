// Package memory provides fact extraction prompts for memory middleware.
package memory

// MemoryUpdatePrompt is the system prompt used to extract structured facts
// from conversation messages, matching deer-flow's MEMORY_UPDATE_PROMPT.
const MemoryUpdatePrompt = `You are analyzing a conversation to extract memorable facts about the user.

TASK: Extract any new, specific facts about the user from the recent conversation.

OUTPUT FORMAT: Return a JSON array of fact objects. Each fact has:
- "content": The factual statement about the user (string)
- "category": One of: "preference", "knowledge", "context", "behavior", "goal", "correction"
- "confidence": A number from 0.0 to 1.0 indicating certainty

CATEGORIES:
- preference: User's likes, dislikes, or preferred ways of doing things
- knowledge: User's expertise, background, or domain knowledge
- context: User's current project, environment, or situation
- behavior: User's habits, patterns, or typical approaches
- goal: User's objectives, targets, or desired outcomes
- correction: User corrected a previous assistant response

GUIDELINES:
1. Only extract facts directly stated or strongly implied by the user
2. Prefer specific facts over vague generalizations
3. Skip facts that are temporary or session-specific
4. If no new facts are found, return an empty array []
5. Confidence should be high (0.8+) for explicit statements, lower for inferences

CONFIDENCE LEVELS:
- 0.9-1.0: Explicitly stated facts ("I work on X", "My role is Y")
- 0.7-0.8: Strongly implied from actions/discussions
- 0.5-0.6: Inferred patterns (use sparingly, only for clear patterns)

Example input:
Human: I prefer Go over Python for backend services
Assistant: I'll use Go then.

Example output:
[{"content":"User prefers Go over Python for backend services","category":"preference","confidence":0.95}]
`
