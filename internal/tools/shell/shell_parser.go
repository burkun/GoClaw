package shell

import (
	"strings"
)

// extractAbsolutePathTokens extracts all absolute path tokens from a command string.
// It handles paths in various contexts including:
// - Simple paths: /path/to/file
// - Paths with options: --path=/path/to/file
// - Paths in redirections: > /path/to/file
// - Paths after pipes: | /path/to/file
//
// The function is conservative and may extract some false positives, but that's
// acceptable for security validation purposes.
func extractAbsolutePathTokens(command string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)

	for i := 0; i < len(command); i++ {
		// Look for absolute paths starting with /
		if command[i] != '/' {
			continue
		}

		// Check if this could be the start of an absolute path
		// Allow / after whitespace, operators, quotes, or at the start
		if i > 0 {
			prev := command[i-1]
			// Skip if this / is part of a path (previous char is part of path)
			if isPathChar(prev) || prev == '/' {
				continue
			}
			// Skip if this / is after a colon that's part of a URL-like pattern
			// (but allow paths after other colons like in redirections)
			if prev == ':' && i > 1 {
				// Check if this looks like a URL (http://, https://, etc.)
				lookback := i - 7
				if lookback < 0 {
					lookback = 0
				}
				prefix := command[lookback:i]
				if strings.Contains(prefix, "http") || strings.Contains(prefix, "ftp") ||
					strings.Contains(prefix, "git") || strings.Contains(prefix, "ssh") {
					continue
				}
			}
		}

		// Extract the path token
		j := i
		for j < len(command) && !isPathDelimiter(command[j]) {
			j++
		}

		token := command[i:j]

		// Skip if it's just "/" or too short to be meaningful
		if len(token) <= 1 {
			continue
		}

		// Skip if we've already seen this path
		if _, ok := seen[token]; ok {
			continue
		}

		seen[token] = struct{}{}
		out = append(out, token)
	}

	return out
}

func isPathChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '-' || c == '.'
}

func isPathDelimiter(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '"', '\'', '`', ';', '&', '|', '<', '>', '(', ')':
		return true
	default:
		return false
	}
}

func hasAnyPrefix(v string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	return false
}
