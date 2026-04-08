package config

import "fmt"

// ---------------------------------------------------------------------------
// Configuration validation
// ---------------------------------------------------------------------------

// Validate checks that the AppConfig is valid and all required fields are set.
// Returns an error if validation fails.
func (c *AppConfig) Validate() error {
	// Validate models
	if len(c.Models) == 0 {
		// Models can be empty in some configurations
		return nil
	}

	for i := range c.Models {
		if err := c.Models[i].validate(); err != nil {
			return err
		}
	}

	// Validate sandbox
	if err := c.Sandbox.validate(); err != nil {
		return err
	}

	// Validate memory
	if err := c.Memory.validate(); err != nil {
		return err
	}

	// Validate checkpointer
	if c.Checkpointer != nil {
		if err := c.Checkpointer.validate(); err != nil {
			return err
		}
	}

	return nil
}

// validate checks that the ModelConfig is valid.
func (m *ModelConfig) validate() error {
	if m.Name == "" {
		return newValidationError("model", "name is required")
	}
	if m.Use == "" {
		return newValidationError("model", "use is required for model %s", m.Name)
	}
	if m.Model == "" {
		return newValidationError("model", "model is required for model %s", m.Name)
	}
	return nil
}

// validate checks that the SandboxConfig is valid.
func (s *SandboxConfig) validate() error {
	if s.Use == "" {
		return newValidationError("sandbox", "use is required")
	}
	if s.Use != "local" && s.Use != "docker" {
		return newValidationError("sandbox", "use must be 'local' or 'docker', got %s", s.Use)
	}
	if s.Use == "docker" && s.Image == "" {
		return newValidationError("sandbox", "image is required when use=docker")
	}
	return nil
}

// validate checks that the MemoryConfig is valid.
func (m *MemoryConfig) validate() error {
	if !m.Enabled {
		return nil
	}
	if m.DebounceSeconds < 0 || m.DebounceSeconds > 300 {
		return newValidationError("memory", "debounce_seconds must be between 0 and 300, got %d", m.DebounceSeconds)
	}
	if m.MaxFacts < 0 {
		return newValidationError("memory", "max_facts must be non-negative, got %d", m.MaxFacts)
	}
	if m.FactConfidenceThreshold < 0 || m.FactConfidenceThreshold > 1 {
		return newValidationError("memory", "fact_confidence_threshold must be between 0 and 1, got %f", m.FactConfidenceThreshold)
	}
	return nil
}

// validate checks that the CheckpointerConfig is valid.
func (c *CheckpointerConfig) validate() error {
	if c.Type == "" {
		return newValidationError("checkpointer", "type is required")
	}
	if c.Type != "memory" && c.Type != "sqlite" && c.Type != "postgres" {
		return newValidationError("checkpointer", "type must be 'memory', 'sqlite', or 'postgres', got %s", c.Type)
	}
	if (c.Type == "sqlite" || c.Type == "postgres") && c.ConnectionString == "" {
		return newValidationError("checkpointer", "connection_string is required for type %s", c.Type)
	}
	return nil
}

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config validation error in %s: %s", e.Field, e.Message)
}

func newValidationError(field, format string, args ...any) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: fmt.Sprintf(format, args...),
	}
}
