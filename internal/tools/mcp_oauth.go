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
func (m *OAuthTokenManager) GetToken(ctx context.Context, serverName string, cfg *config.MCPOAuthConfig) (string, error) {
	if m == nil || cfg == nil {
		return "", nil
	}
	if strings.TrimSpace(cfg.TokenURL) == "" || strings.TrimSpace(cfg.ClientID) == "" {
		return "", nil
	}

	m.mu.Lock()
	if entry, ok := m.cache[serverName]; ok {
		if entry.accessToken != "" && time.Until(entry.expiresAt) > 30*time.Second {
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

	var payload struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("oauth decode token response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("oauth token endpoint status=%d", resp.StatusCode)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return "", fmt.Errorf("oauth token response missing access_token")
	}
	if payload.TokenType == "" {
		payload.TokenType = "Bearer"
	}
	if payload.ExpiresIn <= 0 {
		payload.ExpiresIn = 3600
	}

	entry := oauthTokenEntry{
		accessToken: payload.AccessToken,
		tokenType:   payload.TokenType,
		expiresAt:   time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second),
	}

	m.mu.Lock()
	m.cache[serverName] = entry
	m.mu.Unlock()

	return payload.TokenType + " " + payload.AccessToken, nil
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
