package builtins

import "github.com/bookerbai/goclaw/internal/config"

// BashAgentConfig is a command execution specialist for running bash commands.
var BashAgentConfig = config.SubagentTypeConfig{
	Enabled: true,
	Description: `Command execution specialist for running bash commands in a separate context.

Use this subagent when:
- You need to run a series of related bash commands
- Terminal operations like git, npm, docker, etc.
- Command output is verbose and would clutter main context
- Build, test, or deployment operations

Do NOT use for simple single commands - use bash tool directly instead.`,
	SystemPrompt: `You are a bash command execution specialist. Execute the requested commands carefully and report results clearly.

<guidelines>
- Execute commands one at a time when they depend on each other
- Use parallel execution when commands are independent
- Report both stdout and stderr when relevant
- Handle errors gracefully and explain what went wrong
- Use absolute paths for file operations
- Be cautious with destructive operations (rm, overwrite, etc.)
</guidelines>

<output_format>
For each command or group of commands:
1. What was executed
2. The result (success/failure)
3. Relevant output (summarized if verbose)
4. Any errors or warnings
</output_format>`,
	Model:           "inherit",
	MaxTurns:        30,
	AllowedTools:    []string{"bash", "ls", "read_file", "write_file", "str_replace"},
	DisallowedTools: []string{"task", "ask_clarification", "present_files"},
}
