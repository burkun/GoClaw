package shell

import (
	"fmt"
	"strings"
)

// validateCommand checks the command against the dangerous-path denylist and
// rejects any absolute paths not covered by allowedSystemPathPrefixes or the
// virtual /mnt/user-data/ prefix.
// It also performs security checks for command injection attempts.
func validateCommand(command string) error {
	// Security checks for shell metacharacters that could enable injection
	if err := validateCommandSafety(command); err != nil {
		return err
	}

	// Path validation - validate all paths in the command
	for _, p := range extractAbsolutePathTokens(command) {
		if p == "/mnt/user-data" || strings.HasPrefix(p, "/mnt/user-data/") {
			continue
		}
		if hasAnyPrefix(p, allowedSystemPathPrefixes) {
			continue
		}
		if hasAnyPrefix(p, dangerousPathPrefixes) {
			return fmt.Errorf("permission denied: path not allowed: %s", p)
		}
		return fmt.Errorf("permission denied: absolute path not allowed: %s", p)
	}
	return nil
}

// validateCommandSafety checks for dangerous shell metacharacters and syntax
// that could be used to bypass security restrictions or perform injection attacks.
func validateCommandSafety(command string) error {
	// Check for command substitution - this is the most dangerous as it can
	// dynamically construct paths that bypass our validation
	if strings.Contains(command, "$(") {
		return fmt.Errorf("permission denied: command substitution $() is not allowed")
	}
	if strings.Contains(command, "`") {
		return fmt.Errorf("permission denied: command substitution (backticks) is not allowed")
	}

	// Check for environment variable expansion that could bypass path checks
	if strings.Contains(command, "$") {
		// Check for any ${...} pattern
		if strings.Contains(command, "${") {
			return fmt.Errorf("permission denied: environment variable expansion ${} is not allowed")
		}
		// Check for common dangerous environment variables
		dangerEnvVars := []string{"$HOME", "$USER", "$PATH"}
		for _, envVar := range dangerEnvVars {
			if strings.Contains(command, envVar) {
				return fmt.Errorf("permission denied: environment variable %s is not allowed", envVar)
			}
		}
	}

	// Note: We allow pipes (|), redirections (<, >), and command chaining (;, &&, ||)
	// because these are legitimate shell features. Security is ensured by strict
	// path validation on ALL paths appearing in the command, including those
	// after pipes and redirections.

	return nil
}
