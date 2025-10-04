package gcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/stolos-cloud/stolos-bootstrap/pkg/logger"
	"github.com/stolos-cloud/stolos-bootstrap/pkg/oauth"

	"golang.org/x/oauth2"
	resourcemanager "google.golang.org/api/cloudresourcemanager/v1"
	iam "google.golang.org/api/iam/v1"
	"google.golang.org/api/option"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// set via ldflags
var GCPClientId string
var GCPClientSecret string

type Config struct {
	ProjectID           string `json:"project_id"`
	Region              string `json:"region"`
	ServiceAccountJSON  string `json:"service_account_json"`
	ServiceAccountEmail string `json:"service_account_email"`
}

func NewConfig(projectID, region, serviceAccountJSON, serviceAccountEmail string) (*Config, error) {
	// Basic validation - ensure it's valid JSON and contains project_id
	var jsonData map[string]any
	if err := json.Unmarshal([]byte(serviceAccountJSON), &jsonData); err != nil {
		return nil, fmt.Errorf("invalid service account JSON: %w", err)
	}

	// Validate project ID matches
	if saProjectID, ok := jsonData["project_id"].(string); ok {
		if saProjectID != projectID {
			return nil, fmt.Errorf("project ID mismatch: provided %s, service account %s", projectID, saProjectID)
		}
	} else {
		return nil, fmt.Errorf("service account JSON missing project_id field")
	}

	return &Config{
		ProjectID:           projectID,
		Region:              region,
		ServiceAccountJSON:  serviceAccountJSON,
		ServiceAccountEmail: serviceAccountEmail,
	}, nil
}

// ToSecret serializes
func (c *Config) ToSecret(namespace, secretName string) *corev1.Secret {
	data := map[string][]byte{
		"gcp_project_id":            []byte(c.ProjectID),
		"gcp_region":                []byte(c.Region),
		"gcp_service_account_json":  []byte(c.ServiceAccountJSON),
		"gcp_service_account_email": []byte(c.ServiceAccountEmail),
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
				"app.kubernetes.io/component": "stolos-backend",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}
}

// FromSecret deserializes secret to GCP config
func FromSecret(secret *corev1.Secret) (*Config, error) {
	if secret.Data == nil {
		return nil, fmt.Errorf("secret data is nil")
	}

	return &Config{
		ProjectID:           string(secret.Data["gcp_project_id"]),
		Region:              string(secret.Data["gcp_region"]),
		ServiceAccountJSON:  string(secret.Data["gcp_service_account_json"]),
		ServiceAccountEmail: string(secret.Data["gcp_service_account_email"]),
	}, nil
}

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

func GetSecretFromCluster(ctx context.Context, client kubernetes.Interface, namespace, secretName string) (*Config, error) {
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	return FromSecret(secret)
}

func CreateServiceAccountWithOAuth(ctx context.Context, projectID, region string, token *oauth2.Token, serviceAccountName string) (*Config, error) {
	client := &http.Client{}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)

	tokenSource := oauth2.StaticTokenSource(token)

	iamService, err := iam.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("failed to create IAM service: %w", err)
	}

	// Create Resource Manager service for IAM policy management
	rmService, err := resourcemanager.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("failed to create Resource Manager service: %w", err)
	}

	projectResource := fmt.Sprintf("projects/%s", projectID)
	serviceAccountResource := fmt.Sprintf("%s/serviceAccounts/%s@%s.iam.gserviceaccount.com", projectResource, serviceAccountName, projectID)

	// Try to get existing service account
	var createdSA *iam.ServiceAccount
	existingSA, err := iamService.Projects.ServiceAccounts.Get(serviceAccountResource).Context(ctx).Do()
	if err != nil {
		sa := &iam.ServiceAccount{
			DisplayName: "Stolos Platform Service Account",
			Description: "Service account for Stolos platform backend operations",
		}

		createdSA, err = iamService.Projects.ServiceAccounts.Create(
			projectResource,
			&iam.CreateServiceAccountRequest{
				AccountId:      serviceAccountName,
				ServiceAccount: sa,
			},
		).Context(ctx).Do()
		if err != nil {
			if strings.Contains(err.Error(), "alreadyExists") {
				// Service account already exists
				existingSA, getErr := iamService.Projects.ServiceAccounts.Get(serviceAccountResource).Context(ctx).Do()
				if getErr != nil {
					return nil, fmt.Errorf("service account exists but cannot retrieve it: %w", getErr)
				}
				createdSA = existingSA
			} else {
				return nil, fmt.Errorf("failed to create service account: %w", err)
			}
		}
	} else {
		// Service account already exists, use it
		createdSA = existingSA
	}

	// Create service account key
	key, err := iamService.Projects.ServiceAccounts.Keys.Create(
		createdSA.Name,
		&iam.CreateServiceAccountKeyRequest{
			KeyAlgorithm:   "KEY_ALG_RSA_2048",
			PrivateKeyType: "TYPE_GOOGLE_CREDENTIALS_FILE",
		},
	).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to create service account key: %w", err)
	}

	// Decode base64 private key data
	decodedKey, err := base64.StdEncoding.DecodeString(key.PrivateKeyData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode service account key: %w", err)
	}

	serviceAccountJSON := string(decodedKey)

	// Assign IAM roles to the service account
	if err := assignServiceAccountRoles(rmService, projectID, createdSA.Email); err != nil {
		return nil, fmt.Errorf("failed to assign IAM roles: %w", err)
	}

	return NewConfig(projectID, region, serviceAccountJSON, createdSA.Email)
}

func assignServiceAccountRoles(rmService *resourcemanager.Service, projectID, serviceAccountEmail string) error {

	roles := []string{
		"roles/storage.admin",          // For bucket operations
		"roles/compute.admin",          // For VM management
		"roles/iam.serviceAccountUser", // For service account operations
	}

	policy, err := rmService.Projects.GetIamPolicy(projectID, &resourcemanager.GetIamPolicyRequest{}).Do()
	if err != nil {
		return fmt.Errorf("failed to get IAM policy: %w", err)
	}

	// Add service account to each role
	serviceAccountMember := fmt.Sprintf("serviceAccount:%s", serviceAccountEmail)

	for _, role := range roles {
		// Find existing binding for this role
		var binding *resourcemanager.Binding
		for _, b := range policy.Bindings {
			if b.Role == role {
				binding = b
				break
			}
		}

		if binding == nil {
			binding = &resourcemanager.Binding{
				Role:    role,
				Members: []string{},
			}
			policy.Bindings = append(policy.Bindings, binding)
		}

		// Add service account if not already present
		found := slices.Contains(binding.Members, serviceAccountMember)
		if !found {
			binding.Members = append(binding.Members, serviceAccountMember)
		}
	}

	// Update IAM policy
	_, err = rmService.Projects.SetIamPolicy(projectID, &resourcemanager.SetIamPolicyRequest{
		Policy: policy,
	}).Do()
	if err != nil {
		return fmt.Errorf("failed to set IAM policy: %w", err)
	}

	return nil
}

func AuthenticateAndSetup(oauthServer *oauth.Server, clientID, clientSecret, projectID, region string, logger logger.Logger) (*Config, error) {
	ctx := context.Background()

	provider := oauth.NewGCPProvider(clientID, clientSecret)
	oauthServer.RegisterProvider(provider)

	token, err := oauthServer.Authenticate(ctx, "GCP")
	if err != nil {
		return nil, fmt.Errorf("GCP authentication failed: %w", err)
	}

	config, err := CreateServiceAccountWithOAuth(
		ctx,
		projectID,
		region,
		token,
		"stolos-platform-sa",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCP service account: %w", err)
	}

	logger.Success("GCP service account created successfully")
	return config, nil
}
