package tracing

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLangfuseConfig_IsConfigured(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		publicKey string
		secretKey string
		expected  bool
	}{
		{"disabled", false, "pk", "sk", false},
		{"enabled_no_keys", true, "", "", false},
		{"enabled_only_public", true, "pk", "", false},
		{"enabled_only_secret", true, "", "sk", false},
		{"enabled_both_keys", true, "pk", "sk", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &LangfuseConfig{
				Enabled:   tt.enabled,
				PublicKey: tt.publicKey,
				SecretKey: tt.secretKey,
			}
			assert.Equal(t, tt.expected, cfg.IsConfigured())
		})
	}
}

func TestLangfuseConfig_Validate(t *testing.T) {
	t.Run("disabled returns nil", func(t *testing.T) {
		cfg := &LangfuseConfig{Enabled: false}
		assert.NoError(t, cfg.Validate())
	})

	t.Run("enabled with missing keys returns error", func(t *testing.T) {
		cfg := &LangfuseConfig{Enabled: true, PublicKey: "", SecretKey: ""}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "LANGFUSE_PUBLIC_KEY")
		assert.Contains(t, err.Error(), "LANGFUSE_SECRET_KEY")
	})

	t.Run("enabled with only public key returns error", func(t *testing.T) {
		cfg := &LangfuseConfig{Enabled: true, PublicKey: "pk", SecretKey: ""}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "LANGFUSE_SECRET_KEY")
	})

	t.Run("enabled with both keys returns nil", func(t *testing.T) {
		cfg := &LangfuseConfig{Enabled: true, PublicKey: "pk", SecretKey: "sk"}
		assert.NoError(t, cfg.Validate())
	})
}

func TestTracingConfig_EnabledProviders(t *testing.T) {
	t.Run("no providers configured", func(t *testing.T) {
		cfg := &TracingConfig{}
		assert.Empty(t, cfg.EnabledProviders())
	})

	t.Run("langfuse configured", func(t *testing.T) {
		cfg := &TracingConfig{
			Langfuse: &LangfuseConfig{
				Enabled:   true,
				PublicKey: "pk",
				SecretKey: "sk",
			},
		}
		providers := cfg.EnabledProviders()
		assert.Equal(t, []string{"langfuse"}, providers)
	})

	t.Run("langfuse enabled but not configured", func(t *testing.T) {
		cfg := &TracingConfig{
			Langfuse: &LangfuseConfig{
				Enabled: true,
			},
		}
		assert.Empty(t, cfg.EnabledProviders())
	})
}

func TestGetTracingConfig_FromEnv(t *testing.T) {
	// Reset before test
	ResetTracingConfig()

	// Set environment variables
	os.Setenv("LANGFUSE_TRACING", "true")
	os.Setenv("LANGFUSE_PUBLIC_KEY", "pk-test")
	os.Setenv("LANGFUSE_SECRET_KEY", "sk-test")
	os.Setenv("LANGFUSE_BASE_URL", "https://custom.langfuse.com")
	defer func() {
		os.Unsetenv("LANGFUSE_TRACING")
		os.Unsetenv("LANGFUSE_PUBLIC_KEY")
		os.Unsetenv("LANGFUSE_SECRET_KEY")
		os.Unsetenv("LANGFUSE_BASE_URL")
		ResetTracingConfig()
	}()

	cfg := GetTracingConfig()
	assert.True(t, cfg.Langfuse.Enabled)
	assert.Equal(t, "pk-test", cfg.Langfuse.PublicKey)
	assert.Equal(t, "sk-test", cfg.Langfuse.SecretKey)
	assert.Equal(t, "https://custom.langfuse.com", cfg.Langfuse.Host)
	assert.True(t, cfg.Langfuse.IsConfigured())
}

func TestGetTracingConfig_Defaults(t *testing.T) {
	ResetTracingConfig()

	// Clear all env vars
	os.Unsetenv("LANGFUSE_TRACING")
	os.Unsetenv("LANGFUSE_PUBLIC_KEY")
	os.Unsetenv("LANGFUSE_SECRET_KEY")
	os.Unsetenv("LANGFUSE_BASE_URL")

	cfg := GetTracingConfig()
	assert.False(t, cfg.Langfuse.Enabled)
	assert.Equal(t, "https://cloud.langfuse.com", cfg.Langfuse.Host) // default
	assert.False(t, cfg.Langfuse.IsConfigured())
}

func TestIsEnvTruthy(t *testing.T) {
	truthy := []string{"1", "true", "True", "TRUE", "yes", "Yes", "YES", "on", "On", "ON"}
	for _, v := range truthy {
		t.Run("truthy_"+v, func(t *testing.T) {
			os.Setenv("TEST_VAR", v)
			defer os.Unsetenv("TEST_VAR")
			assert.True(t, isEnvTruthy("TEST_VAR"))
		})
	}

	falsy := []string{"0", "false", "no", "", "random"}
	for _, v := range falsy {
		t.Run("falsy_"+v, func(t *testing.T) {
			os.Setenv("TEST_VAR", v)
			defer os.Unsetenv("TEST_VAR")
			assert.False(t, isEnvTruthy("TEST_VAR"))
		})
	}

	t.Run("not_set", func(t *testing.T) {
		os.Unsetenv("TEST_VAR_NOT_SET")
		assert.False(t, isEnvTruthy("TEST_VAR_NOT_SET"))
	})
}

func TestBuildHandlers_NoConfig(t *testing.T) {
	ResetTracingConfig()
	os.Unsetenv("LANGFUSE_TRACING")
	os.Unsetenv("LANGFUSE_PUBLIC_KEY")
	os.Unsetenv("LANGFUSE_SECRET_KEY")

	handlers, err := BuildHandlers()
	assert.NoError(t, err)
	assert.Empty(t, handlers)
}
