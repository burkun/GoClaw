package config

import (
	"fmt"
	"os"
	"strings"

	"goclaw/internal/logging"
)

// ---------------------------------------------------------------------------
// Environment variable resolution
// ---------------------------------------------------------------------------

// resolveEnvVars replaces bare "$VAR_NAME" string values with the corresponding
// environment variable. Strings that do not start with "$" are returned as-is.
// When failLate is true, missing env vars return empty string instead of error
// (mirrors DeerFlow's extensions_config behavior).
func resolveEnvVars(v string, failLate bool) (string, error) {
	if !strings.HasPrefix(v, "$") {
		return v, nil
	}
	varName := strings.TrimSpace(v[1:])
	if varName == "" {
		return "", fmt.Errorf("config: invalid environment variable reference %q", v)
	}
	if val, ok := os.LookupEnv(varName); ok {
		return val, nil
	}
	if failLate {
		// For extensions_config.json: return empty string with warning (fail-late)
		logging.Warn("Environment variable not found, using empty string (fail-late mode)",
			"var", varName, "hint", "This may cause runtime errors if the value is required")
		return "", nil
	}
	// For config.yaml: return error (fail-fast)
	logging.Warn("Missing environment variable referenced by config", "name", varName)
	return "", fmt.Errorf("config: environment variable %s not found for value %s", varName, v)
}

// resolveEnvVarsInAny recursively traverses a raw decoded YAML value (map,
// slice, or scalar) and replaces "$VAR" strings with env var values.
// When failLate is true, missing env vars return empty string instead of error.
func resolveEnvVarsInAny(v any, failLate bool) (any, error) {
	switch val := v.(type) {
	case string:
		return resolveEnvVars(val, failLate)
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			resolved, err := resolveEnvVarsInAny(child, failLate)
			if err != nil {
				return nil, err
			}
			out[k] = resolved
		}
		return out, nil
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			resolved, err := resolveEnvVarsInAny(child, failLate)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	default:
		return v, nil
	}
}
