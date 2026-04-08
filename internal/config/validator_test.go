package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// TestValidate
// ---------------------------------------------------------------------------

func TestValidate_EmptyModels(t *testing.T) {
	cfg := &AppConfig{
		Models: []ModelConfig{},
		Sandbox: SandboxConfig{
			Use: "local",
		},
		Memory: MemoryConfig{
			Enabled: false,
		},
	}

	// Empty models is valid
	err := cfg.Validate()
	require.NoError(t, err)
}

func TestValidate_InvalidModel(t *testing.T) {
	cfg := &AppConfig{
		Models: []ModelConfig{
			{Name: "", Use: "openai", Model: "gpt-4o"},
		},
		Sandbox: SandboxConfig{
			Use: "local",
		},
		Memory: MemoryConfig{
			Enabled: false,
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestValidate_InvalidSandbox(t *testing.T) {
	cfg := &AppConfig{
		Models: []ModelConfig{
			{Name: "test", Use: "openai", Model: "gpt-4o"},
		},
		Sandbox: SandboxConfig{
			Use: "",
		},
		Memory: MemoryConfig{
			Enabled: false,
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox")
}

func TestValidate_InvalidMemory(t *testing.T) {
	cfg := &AppConfig{
		Models: []ModelConfig{
			{Name: "test", Use: "openai", Model: "gpt-4o"},
		},
		Sandbox: SandboxConfig{
			Use: "local",
		},
		Memory: MemoryConfig{
			Enabled:           true,
			DebounceSeconds:   500, // invalid: > 300
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "debounce_seconds")
}

func TestValidate_InvalidCheckpointer(t *testing.T) {
	cfg := &AppConfig{
		Models: []ModelConfig{
			{Name: "test", Use: "openai", Model: "gpt-4o"},
		},
		Sandbox: SandboxConfig{
			Use: "local",
		},
		Memory: MemoryConfig{
			Enabled: false,
		},
		Checkpointer: &CheckpointerConfig{
			Type: "",
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checkpointer")
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &AppConfig{
		Models: []ModelConfig{
			{Name: "test", Use: "openai", Model: "gpt-4o"},
		},
		Sandbox: SandboxConfig{
			Use: "local",
		},
		Memory: MemoryConfig{
			Enabled: true,
			DebounceSeconds: 30,
			MaxFacts: 100,
			FactConfidenceThreshold: 0.7,
		},
		Checkpointer: &CheckpointerConfig{
			Type: "sqlite",
			ConnectionString: "checkpoints.db",
		},
	}

	err := cfg.Validate()
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// TestValidateModelConfig
// ---------------------------------------------------------------------------

func TestValidateModelConfig_MissingName(t *testing.T) {
	m := &ModelConfig{Name: "", Use: "openai", Model: "gpt-4o"}
	err := m.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestValidateModelConfig_MissingUse(t *testing.T) {
	m := &ModelConfig{Name: "test", Use: "", Model: "gpt-4o"}
	err := m.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use is required")
}

func TestValidateModelConfig_MissingModel(t *testing.T) {
	m := &ModelConfig{Name: "test", Use: "openai", Model: ""}
	err := m.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model is required")
}

func TestValidateModelConfig_Valid(t *testing.T) {
	m := &ModelConfig{Name: "test", Use: "openai", Model: "gpt-4o"}
	err := m.validate()
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// TestValidateSandboxConfig
// ---------------------------------------------------------------------------

func TestValidateSandboxConfig_MissingUse(t *testing.T) {
	s := &SandboxConfig{Use: ""}
	err := s.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use is required")
}

func TestValidateSandboxConfig_InvalidUse(t *testing.T) {
	s := &SandboxConfig{Use: "invalid"}
	err := s.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be 'local' or 'docker'")
}

func TestValidateSandboxConfig_DockerWithoutImage(t *testing.T) {
	s := &SandboxConfig{Use: "docker", Image: ""}
	err := s.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image is required when use=docker")
}

func TestValidateSandboxConfig_DockerWithImage(t *testing.T) {
	s := &SandboxConfig{Use: "docker", Image: "python:3.11"}
	err := s.validate()
	require.NoError(t, err)
}

func TestValidateSandboxConfig_Local(t *testing.T) {
	s := &SandboxConfig{Use: "local"}
	err := s.validate()
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// TestValidateMemoryConfig
// ---------------------------------------------------------------------------

func TestValidateMemoryConfig_Disabled(t *testing.T) {
	m := &MemoryConfig{Enabled: false}
	err := m.validate()
	require.NoError(t, err)
}

func TestValidateMemoryConfig_InvalidDebounceSeconds(t *testing.T) {
	tests := []struct {
		name   string
		value  int
		errMsg string
	}{
		{"negative", -1, "debounce_seconds must be between 0 and 300"},
		{"too_large", 301, "debounce_seconds must be between 0 and 300"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &MemoryConfig{
				Enabled:         true,
				DebounceSeconds: tc.value,
			}
			err := m.validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errMsg)
		})
	}
}

func TestValidateMemoryConfig_InvalidMaxFacts(t *testing.T) {
	m := &MemoryConfig{
		Enabled:  true,
		MaxFacts: -1,
	}
	err := m.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_facts must be non-negative")
}

func TestValidateMemoryConfig_InvalidFactConfidenceThreshold(t *testing.T) {
	tests := []struct {
		name   string
		value  float64
		errMsg string
	}{
		{"negative", -0.1, "fact_confidence_threshold must be between 0 and 1"},
		{"too_large", 1.1, "fact_confidence_threshold must be between 0 and 1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &MemoryConfig{
				Enabled:                  true,
				FactConfidenceThreshold: tc.value,
			}
			err := m.validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errMsg)
		})
	}
}

func TestValidateMemoryConfig_Valid(t *testing.T) {
	m := &MemoryConfig{
		Enabled:                  true,
		DebounceSeconds:          30,
		MaxFacts:                 100,
		FactConfidenceThreshold:  0.7,
	}
	err := m.validate()
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// TestValidateCheckpointerConfig
// ---------------------------------------------------------------------------

func TestValidateCheckpointerConfig_MissingType(t *testing.T) {
	c := &CheckpointerConfig{Type: ""}
	err := c.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "type is required")
}

func TestValidateCheckpointerConfig_InvalidType(t *testing.T) {
	c := &CheckpointerConfig{Type: "invalid"}
	err := c.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be 'memory', 'sqlite', or 'postgres'")
}

func TestValidateCheckpointerConfig_Memory(t *testing.T) {
	c := &CheckpointerConfig{Type: "memory"}
	err := c.validate()
	require.NoError(t, err)
}

func TestValidateCheckpointerConfig_SqliteWithoutConnectionString(t *testing.T) {
	c := &CheckpointerConfig{Type: "sqlite", ConnectionString: ""}
	err := c.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection_string is required for type sqlite")
}

func TestValidateCheckpointerConfig_SqliteWithConnectionString(t *testing.T) {
	c := &CheckpointerConfig{
		Type:             "sqlite",
		ConnectionString: "checkpoints.db",
	}
	err := c.validate()
	require.NoError(t, err)
}

func TestValidateCheckpointerConfig_PostgresWithoutConnectionString(t *testing.T) {
	c := &CheckpointerConfig{Type: "postgres", ConnectionString: ""}
	err := c.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection_string is required for type postgres")
}

func TestValidateCheckpointerConfig_PostgresWithConnectionString(t *testing.T) {
	c := &CheckpointerConfig{
		Type:             "postgres",
		ConnectionString: "postgres://user:pass@localhost/db",
	}
	err := c.validate()
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// TestValidationError
// ---------------------------------------------------------------------------

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{
		Field:   "test_field",
		Message: "test message",
	}
	assert.Equal(t, "config validation error in test_field: test message", err.Error())
}

func TestNewValidationError(t *testing.T) {
	err := newValidationError("field", "value %s is invalid", "test")
	require.NotNil(t, err)
	assert.Equal(t, "field", err.Field)
	assert.Equal(t, "value test is invalid", err.Message)
}
