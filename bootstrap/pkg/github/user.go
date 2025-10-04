package github

import (
	"context"
	"fmt"
	"net/http"

	"github.com/goccy/go-json"
	"golang.org/x/oauth2"
)

type User struct {
	Login     string `json:"login"`
	avatarURL string `json:"avatar_url"`
	htmlURL   string `json:"html_url"`
	Type      string `json:"type"` // "User" or "Organization"
}

// GetGitHubUser queries the GitHub API to determine if a login is a User or Organization.
// token is optional.
func GetGitHubUser(ctx context.Context, login string, oauthToken oauth2.Token) (*User, error) {
	url := fmt.Sprintf("https://api.github.com/users/%s", login)
	token := oauthToken.AccessToken

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var entity User
	if err := json.NewDecoder(resp.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &entity, nil
}
