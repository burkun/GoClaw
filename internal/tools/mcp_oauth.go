package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bookerbai/goclaw/internal/config"
)

type oauthTokenEntry struct {
	accessToken string
	tokenType   string
	expiresAt   time.Time
}

// OAuthTokenManager caches OAuth tokens per MCP server.
type OAuthTokenManager struct {
	mu         sync.Mutex
	httpClient *http.Client
	cache      map[string]oauthTokenEntry
}

// NewOAuthTokenManager creates an OAuthTokenManager.
func NewOAuthTokenManager(httpClient *http.Client) *OAuthTokenManager {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &OAuthTokenManager{httpClient: httpClient, cache: make(map[string]oauthTokenEntry)}
}

// GetToken returns a cached token or fetches a new one.
// The refreshSkewSeconds parameter controls how many seconds before expiration to refresh.
// If refreshSkewSeconds <= 0, defaults to 60 seconds (aligned with DeerFlow).
func (m *OAuthTokenManager) GetToken(ctx context.Context, serverName string, cfg *config.MCPOAuthConfig) (string, error) {
	if m == nil || cfg == nil {
		return "", nil
	}
	if strings.TrimSpace(cfg.TokenURL) == "" || strings.TrimSpace(cfg.ClientID) == "" {
		return "", nil
	}

	// Determine refresh skew from config or use default (60s aligned with DeerFlow)
	refreshSkew := 60 * time.Second
	if cfg.RefreshSkewSeconds > 0 {
		refreshSkew = time.Duration(cfg.RefreshSkewSeconds) * time.Second
	}

	m.mu.Lock()
	if entry, ok := m.cache[serverName]; ok {
		if entry.accessToken != "" && time.Until(entry.expiresAt) > refreshSkew {
			tok := entry.accessToken
			typeHint := entry.tokenType
			m.mu.Unlock()
			if typeHint == "" {
				typeHint = "Bearer"
			}
			return typeHint + " " + tok, nil
		}
	}
	m.mu.Unlock()

	grant := strings.TrimSpace(cfg.GrantType)
	if grant == "" {
		grant = "client_credentials"
	}

	form := url.Values{}
	form.Set("grant_type", grant)
	form.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	if strings.TrimSpace(cfg.Scope) != "" {
		form.Set("scope", strings.TrimSpace(cfg.Scope))
	}
	if grant == "refresh_token" && strings.TrimSpace(cfg.RefreshToken) != "" {
		form.Set("refresh_token", strings.TrimSpace(cfg.RefreshToken))
	}
	// Add extra token params if configured
	if cfg.ExtraTokenParams != nil {
		for k, v := range cfg.ExtraTokenParams {
			form.Set(k, v)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("oauth build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth token request failed: %w", err)
	}
	defer resp.Body.Close()

	// Determine field names from config or use defaults
	tokenField := "access_token"
	if cfg.TokenField != "" {
		tokenField = cfg.TokenField
	}
	tokenTypeField := "token_type"
	if cfg.TokenTypeField != "" {
		tokenTypeField = cfg.TokenTypeField
	}
	expiresInField := "expires_in"
	if cfg.ExpiresInField != "" {
		expiresInField = cfg.ExpiresInField
	}

	var rawPayload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rawPayload); err != nil {
		return "", fmt.Errorf("oauth decode token response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("oauth token endpoint status=%d", resp.StatusCode)
	}

	var accessToken, tokenType string
	var expiresIn int64
	if v, ok := rawPayload[tokenField]; ok {
		if s, ok := v.(string); ok {
			accessToken = s
		}
	}
	if v, ok := rawPayload[tokenTypeField]; ok {
		if s, ok := v.(string); ok {
			tokenType = s
		}
	}
	if v, ok := rawPayload[expiresInField]; ok {
		switch n := v.(type) {
		case float64:
			expiresIn = int64(n)
		case int64:
			expiresIn = n
		case int:
			expiresIn = int64(n)
		}
	}

	if strings.TrimSpace(accessToken) == "" {
		return "", fmt.Errorf("oauth token response missing %s", tokenField)
	}
	if tokenType == "" {
		if cfg.DefaultTokenType != "" {
			tokenType = cfg.DefaultTokenType
		} else {
			tokenType = "Bearer"
		}
	}
	if expiresIn <= 0 {
		expiresIn = 3600
	}

	entry := oauthTokenEntry{
		accessToken: accessToken,
		tokenType:   tokenType,
		expiresAt:   time.Now().Add(time.Duration(expiresIn) * time.Second),
	}

	m.mu.Lock()
	m.cache[serverName] = entry
	m.mu.Unlock()

	return tokenType + " " + accessToken, nil
}

var globalOAuthTokenManager = NewOAuthTokenManager(nil)

func applyOAuthHeader(ctx context.Context, serverName string, serverCfg config.MCPServerConfig, headers map[string]string) (map[string]string, error) {
	if serverCfg.OAuth == nil {
		return headers, nil
	}
	if headers == nil {
		headers = map[string]string{}
	}
	if strings.TrimSpace(headers["Authorization"]) != "" {
		return headers, nil
	}
	bearer, err := globalOAuthTokenManager.GetToken(ctx, serverName, serverCfg.OAuth)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(bearer) != "" {
		headers["Authorization"] = bearer
	}
	return headers, nil
}
