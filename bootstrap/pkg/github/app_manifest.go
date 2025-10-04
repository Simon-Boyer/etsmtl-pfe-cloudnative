package github

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/goccy/go-json"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/browser"
	"github.com/stolos-cloud/stolos-bootstrap/pkg/logger"
)

type HookAttributes struct {
	Url    string `json:"url"`
	Active bool   `json:"active"`
}

// AppManifestParams is the payload you send to GitHub when exchanging the manifest code
type AppManifestParams struct {
	Name                  string            `json:"name,omitempty"`
	URL                   string            `json:"url,omitempty"`
	HookAttributes        HookAttributes    `json:"hook_attributes,omitempty"`
	RedirectURL           string            `json:"redirect_url,omitempty"`
	CallbackURLs          []string          `json:"callback_urls,omitempty"`
	SetupURL              string            `json:"setup_url,omitempty"`
	Description           string            `json:"description,omitempty"`
	Public                bool              `json:"public,omitempty"`
	DefaultEvents         []string          `json:"default_events,omitempty"`
	DefaultPermissions    map[string]string `json:"default_permissions,omitempty"`
	RequestOAuthOnInstall bool              `json:"request_oauth_on_install,omitempty"`
	SetupOnUpdate         bool              `json:"setup_on_update,omitempty"`
	// You might also include a `State` field if you generate a CSRF token.
	State string `json:"state,omitempty"`
}

// AppManifest is what GitHub returns from the POST /app-manifests/{code}/conversions endpoint
type AppManifest struct {
	ID                 int64             `json:"id"`
	NodeID             string            `json:"node_id"`
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	ExternalURL        string            `json:"external_url"`
	HTMLURL            string            `json:"html_url"`
	Slug               string            `json:"slug"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	ClientID           string            `json:"client_id"`
	ClientSecret       string            `json:"client_secret"`
	WebhookSecret      string            `json:"webhook_secret"`
	PEM                string            `json:"pem"` // the private key in PEM format
	Events             []string          `json:"events"`
	DefaultPermissions map[string]string `json:"default_permissions"`
	InstallationsCount int               `json:"installations_count"`
	Owner              User              `json:"owner"`
}

// AppInstallation represents a GitHub App installation object (simplified)
type AppInstallation struct {
	ID     int64  `json:"id"`
	NodeID string `json:"node_id"`
	// Account holds either user or org info
	Account struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
		Type  string `json:"type"` // "User" or "Organization"
	} `json:"account"`
	RepositorySelection string    `json:"repository_selection"` // "selected" or "all"
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func CreateGitHubManifestParameters(remoteBaseUrl string, webhookEndpoint string, additionalCallbackEndpoint string, listenAddr string) *AppManifestParams {
	return &AppManifestParams{
		Name: "Stolos Platform",
		URL:  "https://stolos.cloud",
		HookAttributes: HookAttributes{
			Url:    remoteBaseUrl + webhookEndpoint, // WebHook events endpoint
			Active: true,
		},
		RedirectURL: listenAddr + "/_github_app_manifest_callback", // hit After manifest register, set in next phase
		CallbackURLs: []string{
			"http://" + listenAddr + "/_github_app_install_callback", //TODO, we will have to remove this later somehow.
			remoteBaseUrl + additionalCallbackEndpoint,
		}, // hit After app installation
		//SetupURL:    "", // hit After app install if more setup needed (?)
		Description: "The Stolos Platform app allows Stolos to make commits to the GitOps Repository.",
		Public:      false,
		DefaultEvents: []string{
			// TODO : Check full event list and see what we want to subscribe to.
			"workflow_run",
			"workflow_dispatch",
		},
		DefaultPermissions: map[string]string{
			"contents":              "write", // commits, file edits, wiki
			"issues":                "write", // create/update issues
			"organization_projects": "write", // project boards
			"workflows":             "write", // trigger workflows
			"actions":               "write", // check workflow run status
			"discussions":           "write",
			"pages":                 "write",
			"pull_requests":         "write",
			"secrets":               "write",
			"repository_hooks":      "write",
		},
		RequestOAuthOnInstall: true, // "Set to true to request the user to authorize the GitHub App, after the GitHub App is installed."
		SetupOnUpdate:         true, // If the app is updated, redirect to the portal
		// State:                 generateRandom, //TODO : Generate CSRF Token
	}
}

// GitHubAppManifestFlow starts a HTTP server and does the manifest flow.
// It returns the created AppManifest or error.
func GitHubAppManifestFlow(ctx context.Context, listenAddr string, logger logger.Logger, ghManifestParams *AppManifestParams, user User) (*AppManifest, error) {
	manifestResultCh := make(chan *AppManifest, 1)
	//installResultCh := make(chan *AppInstallation)
	errCh := make(chan error, 1)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on port: %w", err)
	}
	defer listener.Close()

	htmlPath := "/post_form"
	redirectPath := "/_github_app_manifest_callback"

	// build the GitHub manifest redirect URL
	_, formHTML, err := buildGitHubManifestRedirect(listener.Addr().String(), redirectPath, ghManifestParams, user)
	if err != nil {
		return nil, fmt.Errorf("failed to build redirect: %w", err)
	}

	//logger.Infof("Please open this URL in browser to register your GitHub App: %s", ghURL)

	mux := http.NewServeMux()
	server := &http.Server{
		Handler: mux,
	}

	// Handler for HTML POST redirect page
	// This is the first step
	mux.HandleFunc(htmlPath, func(w http.ResponseWriter, r *http.Request) {
		logger.Infof("Served redirect page...")
		w.Header().Set("Content-Type", "text/html")
		_, err := fmt.Fprintf(w, formHTML)
		if err != nil {
			logger.Errorf("failed serving html: %w", err)
			return
		}
	})

	// Handler for MANIFEST CODE
	mux.HandleFunc(redirectPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code := q.Get("code")
		//state := q.Get("state") //TODO : Implement CSRF Token

		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			errCh <- fmt.Errorf("callback missing code")
			return
		}

		// Exchange code for the manifest result
		go func() {
			manifest, err := exchangeManifestCode(ctx, code)
			if err != nil {
				errCh <- fmt.Errorf("failed manifest exchange: %w", err)
				return
			}
			// signal success
			manifestResultCh <- manifest
		}()

		// inform the user
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<html><body>GitHub App creation in progress. You may close this window.</body></html>")
	})

	// run server in background
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Errorf("HTTP server error: %v", err)
		}
	}()

	// Open browser to the redirect HTML Form
	if err := browser.OpenURL("http://" + listenAddr + htmlPath); err != nil {
		logger.Errorf("failed to open browser for GitHub manifest redirect %s", err.Error())
		logger.Warnf("Copy-paste link in your browser to continue: %s", "http://"+listenAddr+htmlPath)
	}

	// Wait for either result or error or context done
	select {
	case <-ctx.Done():
		server.Close()
		return nil, ctx.Err()
	case err := <-errCh:
		server.Close()
		return nil, err
	case manifest := <-manifestResultCh:
		server.Close()
		return manifest, nil
	}
}

// AppInstallCallbackResult holds the information GitHub sends during the post-install callback.
type AppInstallCallbackResult struct {
	Code           string
	InstallationID string
	SetupAction    string
}

// GitHubAppInstallFlow handles post install redirect to get installation ID.
func GitHubAppInstallFlow(ctx context.Context, listenAddr string, user *User, app *AppManifest, logger logger.Logger) (*AppInstallCallbackResult, error) {
	resultCh := make(chan *AppInstallCallbackResult, 1)
	errCh := make(chan error, 1)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on port: %w", err)
	}
	defer listener.Close()

	installCallbackPath := "/_github_app_install_callback"

	mux := http.NewServeMux()
	server := &http.Server{
		Handler: mux,
	}

	// Handler for POST-INSTALL
	mux.HandleFunc(installCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code := q.Get("code")
		installationID := q.Get("installation_id")
		setupAction := q.Get("setup_action")

		logger.Successf("GitHub App installed installed!. code=%s, installation_id=%s, setup_action=%s", code, installationID, setupAction)

		// Push result into channel
		resultCh <- &AppInstallCallbackResult{
			Code:           code,
			InstallationID: installationID,
			SetupAction:    setupAction,
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<html><body>GitHub App installed successfully. You may close this window.</body></html>")
	})

	// Run server in background
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Errorf("HTTP server error: %v", err)
		}
	}()

	var appInstallLink = BuildGitHubAppUrl(user, "install", app.Slug)
	err = browser.OpenURL(appInstallLink)
	if err != nil {
		logger.Warnf("Failed to open browser, please install the app via: %s", appInstallLink)
	}

	// Wait for either result or error or context done
	select {
	case <-ctx.Done():
		server.Close()
		return nil, ctx.Err()
	case err := <-errCh:
		server.Close()
		return nil, err
	case result := <-resultCh:
		server.Close()
		return result, nil
	}
}

// buildGitHubManifestRedirect returns the manifest creation URL, an html form that POSTs the manifest or error.
func buildGitHubManifestRedirect(addr, path string, params *AppManifestParams, user User) (string, string, error) {
	manifestJSON, err := json.Marshal(params)
	if err != nil {
		return "", "", err
	}

	redirectURL := fmt.Sprintf("http://%s%s", addr, path)
	params.RedirectURL = redirectURL

	// Now rebuild manifest JSON with redirect_url field set
	manifestJSON, err = json.Marshal(params)
	if err != nil {
		return "", "", err
	}

	var baseURL string
	baseURL = BuildGitHubAppUrl(&user, "create", params.Name)

	// TODO : Implement CSRF
	//ghURL := fmt.Sprintf("%s?state=%s", baseURL, params.State)

	// Create an HTML form that auto-submits:

	// document.forms[0].submit()

	htmlForm := fmt.Sprintf(`
<html>
  <body onload="document.forms[0].submit()">
    <form action="%s" method="post">
      <input type="hidden" name="manifest" value='%s'/>
      <input type="hidden" name="state" value="%s"/>
    </form>
    <p>Redirecting to GitHub...</p>
  </body>
</html>
`, baseURL, string(manifestJSON), params.State)

	return baseURL, htmlForm, nil
}

// BuildGitHubAppUrl returns apps url based on user type, mode is either "create", or "install"
func BuildGitHubAppUrl(user *User, mode string, nameOrSlug string) string {

	var baseUrl string

	if user.Type == "user" {
		baseUrl = "https://github.com/settings/apps"
	} else {
		baseUrl = fmt.Sprintf("https://github.com/organizations/%s/settings/apps", user.Login)
	}

	switch mode {
	case "create":
		return fmt.Sprintf("%s/new", baseUrl)
	case "install":
		return fmt.Sprintf("%s/%s/installations", baseUrl, nameOrSlug)
	case "manage":
		return fmt.Sprintf("%s/%s/", baseUrl, nameOrSlug)
	}

	return "invalid" // unreachable
}

// exchangeManifestCode calls GitHub API to exchange code -> manifest result
func exchangeManifestCode(ctx context.Context, code string) (*AppManifest, error) {
	apiURL := fmt.Sprintf("https://api.github.com/app-manifests/%s/conversions", code)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body failed: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("unexpected status %d: body %s", resp.StatusCode, string(body))
	}

	var manifest AppManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("unmarshal manifest failed: %w", err)
	}
	return &manifest, nil
}

// ListAppInstallations queries /app/installations and returns all installations for the given App.
func ListAppInstallations(ctx context.Context, appID int64, privateKeyPEM string) ([]AppInstallation, error) {
	jwtToken, err := GenerateAppJWT(appID, privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JWT: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/app/installations", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var installs []AppInstallation
	if err := json.NewDecoder(resp.Body).Decode(&installs); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return installs, nil
}

// GenerateAppJWT creates a signed JWT for authenticating as a GitHub App
func GenerateAppJWT(appID int64, privateKeyPEM string) (string, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", fmt.Errorf("failed to parse PEM block containing private key")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	now := time.Now().UTC()
	claims := jwt.RegisteredClaims{
		Issuer:    fmt.Sprintf("%d", appID),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)), // must be <= 10m
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(key)
}
