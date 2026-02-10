package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// UserInfo represents Slack user profile data from users.info API.
type UserInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"real_name"`
	Email    string `json:"email"`
}

// UsersClient provides access to Slack's users.* API endpoints with caching.
type UsersClient struct {
	botToken   string
	apiURL     string
	httpClient *http.Client

	mu    sync.RWMutex
	cache map[string]cachedUser
}

// cachedUser holds user info with expiry timestamp.
type cachedUser struct {
	user      *UserInfo
	expiresAt time.Time
}

// cacheTTL defines how long cached user info remains valid.
const cacheTTL = 5 * time.Minute

// NewUsersClient creates a new Slack users API client.
func NewUsersClient(botToken string) *UsersClient {
	return &UsersClient{
		botToken:   botToken,
		apiURL:     slackAPIURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cache:      make(map[string]cachedUser),
	}
}

// NewUsersClientWithBaseURL creates a users client with custom API base URL (for testing).
func NewUsersClientWithBaseURL(botToken, baseURL string) *UsersClient {
	return &UsersClient{
		botToken:   botToken,
		apiURL:     baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cache:      make(map[string]cachedUser),
	}
}

// usersInfoResponse is the JSON response from users.info API.
type usersInfoResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	User  struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		RealName string `json:"real_name"`
		Profile  struct {
			Email    string `json:"email"`
			RealName string `json:"real_name_normalized"`
		} `json:"profile"`
	} `json:"user"`
}

// GetUserInfo fetches user profile info by Slack user ID.
// Results are cached for cacheTTL to reduce API calls.
func (c *UsersClient) GetUserInfo(ctx context.Context, userID string) (*UserInfo, error) {
	// Check cache first
	c.mu.RLock()
	cached, ok := c.cache[userID]
	c.mu.RUnlock()

	if ok && time.Now().Before(cached.expiresAt) {
		return cached.user, nil
	}

	// Fetch from API
	user, err := c.fetchUserInfo(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Cache the result
	c.mu.Lock()
	c.cache[userID] = cachedUser{
		user:      user,
		expiresAt: time.Now().Add(cacheTTL),
	}
	c.mu.Unlock()

	return user, nil
}

// fetchUserInfo calls users.info API without caching.
func (c *UsersClient) fetchUserInfo(ctx context.Context, userID string) (*UserInfo, error) {
	url := fmt.Sprintf("%s/users.info?user=%s", c.apiURL, userID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.botToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("users.info request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("users.info HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result usersInfoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("users.info error: %s", result.Error)
	}

	return &UserInfo{
		ID:       result.User.ID,
		Name:     result.User.Name,
		RealName: result.User.RealName,
		Email:    result.User.Profile.Email,
	}, nil
}

// ClearCache removes all cached entries. Useful for testing.
func (c *UsersClient) ClearCache() {
	c.mu.Lock()
	c.cache = make(map[string]cachedUser)
	c.mu.Unlock()
}
