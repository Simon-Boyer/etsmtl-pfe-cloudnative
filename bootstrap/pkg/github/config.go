package github

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/stolos-cloud/stolos-bootstrap/pkg/logger"
	"github.com/stolos-cloud/stolos-bootstrap/pkg/oauth"

	"github.com/goccy/go-yaml"
	"github.com/google/go-github/v74/github"
	"golang.org/x/oauth2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// set via ldflags
var GithubClientId string
var GithubClientSecret string

type Client struct {
	*github.Client
	token *oauth2.Token
}

func NewClient(token *oauth2.Token) *Client {
	client := github.NewClient(nil).WithAuthToken(token.AccessToken)
	return &Client{
		Client: client,
		token:  token,
	}
}

// GitHubInfo contains repository setup information
type GitHubInfo struct {
	RepoOwner      string `json:"RepoOwner" field_label:"Github Organization Name" field_required:"true"`
	RepoName       string `json:"RepoName" field_label:"Github Repository Name" field_required:"true"`
	BaseDomain     string `json:"BaseDomain" field_label:"Base Domain (DNS)" field_required:"true"`
	LoadBalancerIP string `json:"LoadBalancerIP" field_label:"LoadBalancer IP" field_required:"true"`
}

// Config contains GitHub credentials for backend usage
type Config struct {
	AccessToken string `json:"access_token"`
	RepoOwner   string `json:"repo_owner"`
	RepoName    string `json:"repo_name"`
}

func (client *Client) InitRepo(info *GitHubInfo, isPrivate bool) (*github.Repository, error) {
	templateRepoOwner := os.Getenv("GITHUB_TEMPLATE_REPO_OWNER")
	templateRepoName := os.Getenv("GITHUB_TEMPLATE_REPO_NAME")
	if templateRepoOwner == "" {
		templateRepoOwner = "stolos-cloud"
	}
	if templateRepoName == "" {
		templateRepoName = "stolos-k8s-template"
	}

	repo, response, err := client.Repositories.CreateFromTemplate(context.Background(), templateRepoOwner, templateRepoName, &github.TemplateRepoRequest{
		Name:               &info.RepoName,
		Owner:              &info.RepoOwner,
		IncludeAllBranches: github.Ptr(false),
		Private:            &isPrivate,
	})

	if err != nil {
		return nil, err
	}

	if response.StatusCode != 201 {
		return nil, fmt.Errorf("CreateFromTemplate returned %d", response.StatusCode)
	}

	time.Sleep(5 * time.Second) // Wait for github to init repo, as createfile can happen before it is fully initialized

	// Create initial config file
	if err := client.createInitialConfig(info); err != nil {
		return nil, fmt.Errorf("failed to create initial config: %w", err)
	}

	return repo, nil
}

// createInitialConfig creates the initial common.yml configuration file
func (c *Client) createInitialConfig(info *GitHubInfo) error {
	commonConfig := struct {
		BaseDomain string `yaml:"base_domain"`
		LbIP       string `yaml:"lb_ip"`
	}{
		BaseDomain: info.BaseDomain,
		LbIP:       info.LoadBalancerIP,
	}

	commonConfigYaml, err := yaml.Marshal(commonConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	author := github.CommitAuthor{
		Name:  github.Ptr("Bot Stolos"),
		Email: github.Ptr("bot@stolos.cloud"),
		Date: &github.Timestamp{
			Time: time.Now(),
		},
	}

	_, response, err := c.Repositories.CreateFile(
		context.Background(),
		info.RepoOwner,
		info.RepoName,
		"common.yml",
		&github.RepositoryContentFileOptions{
			Message:   github.Ptr("Initial config file"),
			Content:   commonConfigYaml,
			Branch:    github.Ptr("main"),
			Committer: &author,
		},
	)

	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	if response.StatusCode != 201 {
		return fmt.Errorf("CreateFile returned %d", response.StatusCode)
	}

	return nil
}

// GetToken returns the OAuth token
func (c *Client) GetToken() *oauth2.Token {
	return c.token
}

// AuthenticateAndSetup performs OAuth authentication and repository initialization
func AuthenticateAndSetup(oauthServer *oauth.Server, clientID, clientSecret string, info *GitHubInfo, logger logger.Logger) (*Client, error) {
	ctx := context.Background()

	provider := oauth.NewGitHubProvider(clientID, clientSecret)
	oauthServer.RegisterProvider(provider)

	token, err := oauthServer.Authenticate(ctx, "GitHub")
	if err != nil {
		return nil, fmt.Errorf("GitHub authentication failed: %w", err)
	}

	client := NewClient(token)

	_, err = client.InitRepo(info, false)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize repository: %w", err)
	}

	logger.Infof("Repo initialized: https://github.com/%s/%s.git", info.RepoOwner, info.RepoName)
	return client, nil
}

// NewConfig creates a new GitHub configuration
func NewConfig(token *oauth2.Token, repoOwner, repoName string) *Config {
	return &Config{
		AccessToken: token.AccessToken,
		RepoOwner:   repoOwner,
		RepoName:    repoName,
	}
}

// ToSecret serializes GitHub config to Kubernetes secret
func (c *Config) ToSecret(namespace, secretName string) *corev1.Secret {
	data := map[string][]byte{
		"github_access_token": []byte(c.AccessToken),
		"github_repo_owner":   []byte(c.RepoOwner),
		"github_repo_name":    []byte(c.RepoName),
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "stolos-platform",
				"app.kubernetes.io/component": "backend",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}
}

// FromSecret deserializes Kubernetes secret to GitHub config
func FromSecret(secret *corev1.Secret) (*Config, error) {
	if secret.Data == nil {
		return nil, fmt.Errorf("secret data is nil")
	}

	return &Config{
		AccessToken: string(secret.Data["github_access_token"]),
		RepoOwner:   string(secret.Data["github_repo_owner"]),
		RepoName:    string(secret.Data["github_repo_name"]),
	}, nil
}

// CreateOrUpdateSecret creates or updates the GitHub config secret in Kubernetes
func (c *Config) CreateOrUpdateSecret(ctx context.Context, client kubernetes.Interface, namespace, secretName string) error {
	secret := c.ToSecret(namespace, secretName)

	existingSecret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		// Secret doesn't exist, create it
		_, err = client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
		return err
	}

	// Secret exists, update it
	existingSecret.Data = secret.Data
	existingSecret.Labels = secret.Labels
	_, err = client.CoreV1().Secrets(namespace).Update(ctx, existingSecret, metav1.UpdateOptions{})
	return err
}
