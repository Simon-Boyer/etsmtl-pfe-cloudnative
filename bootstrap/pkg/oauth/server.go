package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/pkg/browser"
	"github.com/stolos-cloud/stolos-bootstrap/pkg/logger"
	"github.com/stolos-cloud/stolos-bootstrap/pkg/oauth/providers"
	"golang.org/x/oauth2"
)

type Provider interface {
	GetConfig() *oauth2.Config
	GetName() string
	GetCallbackPath() string
}

type Server struct {
	port          string
	server        *http.Server
	providers     map[string]Provider
	tokens        map[string]*oauth2.Token
	errors        map[string]error
	channels      map[string]chan string
	errorChannels map[string]chan error
	mu            sync.RWMutex
	logger        logger.Logger
}

func NewServer(port string, logger logger.Logger) *Server {
	return &Server{
		port:          port,
		providers:     make(map[string]Provider),
		tokens:        make(map[string]*oauth2.Token),
		errors:        make(map[string]error),
		channels:      make(map[string]chan string),
		errorChannels: make(map[string]chan error),
		logger:        logger,
	}
}

// adds an OAuth provider to the server
func (s *Server) RegisterProvider(provider Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers[provider.GetName()] = provider
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Register callback handlers for each provider
	s.mu.RLock()
	for _, provider := range s.providers {
		mux.HandleFunc(provider.GetCallbackPath(), s.handleCallback(provider))
	}
	s.mu.RUnlock()

	s.server = &http.Server{
		Addr:    "localhost:" + s.port,
		Handler: mux,
	}

	go func() {
		err := s.server.ListenAndServe()
		if err != nil {
			s.logger.Errorf("Failed to start OAuth server: %v", err)
		}
	}()

	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *Server) handleCallback(provider Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		queryParts, _ := url.ParseQuery(r.URL.RawQuery)
		providerName := provider.GetName()

		if errorParam := queryParts.Get("error"); errorParam != "" {
			errorMsg := fmt.Errorf("OAuth error: %s", errorParam)

			// Send error through channel if available
			s.mu.RLock()
			errorChan, hasErrorChan := s.errorChannels[providerName]
			s.mu.RUnlock()

			if hasErrorChan {
				select {
				case errorChan <- errorMsg:
				default:
				}
			}

			http.Error(w, fmt.Sprintf("Authentication failed: %s", errorParam), http.StatusBadRequest)
			return
		}

		code := queryParts.Get("code")
		if code == "" {
			errorMsg := fmt.Errorf("no authorization code received")

			// Send error through channel if available
			s.mu.RLock()
			errorChan, hasErrorChan := s.errorChannels[providerName]
			s.mu.RUnlock()

			if hasErrorChan {
				select {
				case errorChan <- errorMsg:
				default:
				}
			}

			http.Error(w, "No authorization code received", http.StatusBadRequest)
			return
		}

		// Send code through channel if available
		s.mu.RLock()
		codeChan, hasCodeChan := s.channels[providerName]
		s.mu.RUnlock()

		if hasCodeChan {
			select {
			case codeChan <- code:
			default:
			}
		}

		// Send success response
		msg := fmt.Sprintf("<p><strong>%s Authentication successful</strong>. You may now close this tab.</p>", provider.GetName())
		fmt.Fprint(w, msg)
	}
}

// Authenticate starts OAuth flow for a provider and returns the token
func (s *Server) Authenticate(ctx context.Context, providerName string) (*oauth2.Token, error) {
	s.mu.RLock()
	provider, exists := s.providers[providerName]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("provider %s not registered", providerName)
	}

	/*--- Code emprunté: https://www.iamyadav.com/blogs/how-to-authenticate-cli-using-oauth ---
	  Des modifications y sont apportées afin de le rendre compatible avec notre structure multi-provider */

	// Clear any previous tokens/errors
	s.mu.Lock()
	delete(s.tokens, providerName)
	delete(s.errors, providerName)
	s.mu.Unlock()

	// Create a channel to receive the authorization code
	codeChan := make(chan string)
	errorChan := make(chan error)

	// Set up temporary callback to communicate with the handler
	s.mu.Lock()
	s.channels = map[string]chan string{providerName: codeChan}
	s.errorChannels = map[string]chan error{providerName: errorChan}
	s.mu.Unlock()

	// Build redirect URL dynamically
	redirectURL := fmt.Sprintf("http://localhost:%s%s", s.port, provider.GetCallbackPath())

	// Get OAuth config and update redirect URL
	config := provider.GetConfig()
	config.RedirectURL = redirectURL

	// Get the OAuth authorization URL
	oauthURL := config.AuthCodeURL("state", oauth2.AccessTypeOnline)

	// Redirect user to consent page to ask for permission for the scopes specified
	s.logger.Infof("Your browser has been opened to visit::\n%s\n", oauthURL)

	// Open user's browser to login page
	if err := browser.OpenURL(oauthURL); err != nil {
		s.logger.Errorf("failed to open browser for authentication %s", err.Error())
		s.logger.Infof("Copy-paste link in your browser to continue: %s", oauthURL)
	}

	// Wait for the authorization code to be received
	var code string
	var authError error
	select {
	case code = <-codeChan:
	case authError = <-errorChan:
		return nil, authError
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Exchange the authorization code for an access token (using context.Background() like the working version)
	exchangeConfig := provider.GetConfig()
	exchangeConfig.RedirectURL = redirectURL

	// Debug: Log what we're sending to the token exchange
	s.logger.Infof("Token exchange for %s: ClientID=%s, RedirectURL=%s", providerName, exchangeConfig.ClientID, exchangeConfig.RedirectURL)

	token, err := exchangeConfig.Exchange(context.Background(), code)
	if err != nil {
		s.logger.Errorf("Failed to exchange authorization code for token: %v", err)
		return nil, err
	}

	if !token.Valid() {
		return nil, fmt.Errorf("can't get source information without accessToken")
	}

	s.logger.Success("Authentication successful")

	// Clean up channels
	s.mu.Lock()
	delete(s.channels, providerName)
	delete(s.errorChannels, providerName)
	s.mu.Unlock()

	_ = s.server.Shutdown(context.Background())

	return token, nil

	/*--- Fin du code emprunté --- */
}

func NewGitHubProvider(clientID, clientSecret string) Provider {
	return providers.NewGitHubProvider(clientID, clientSecret)
}

func NewGCPProvider(clientID, clientSecret string) Provider {
	return providers.NewGCPProvider(clientID, clientSecret)
}
