package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	gcpconfig "github.com/stolos-cloud/stolos-bootstrap/pkg/gcp"
	"github.com/stolos-cloud/stolos/backend/internal/config"
	"github.com/stolos-cloud/stolos/backend/internal/models"
	"github.com/stolos-cloud/stolos/backend/pkg/gcp"
	"gorm.io/gorm"
)

type GCPService struct {
	db  *gorm.DB
	cfg *config.Config
}

func NewGCPService(db *gorm.DB, cfg *config.Config) *GCPService {
	return &GCPService{
		db:  db,
		cfg: cfg,
	}
}

func (s *GCPService) IsConfiguredFromDatabase() bool {
	config, err := s.GetCurrentConfig()
	return err == nil && config != nil && config.IsConfigured
}

func (s *GCPService) IsConfiguredFromEnv() bool {
	return s.cfg.GCP.ProjectID != "" && s.cfg.GCP.ServiceAccountJSON != ""
}

// InitializeGCP initializes GCP configuration on server startup.
// if already configured in DB, it returns existing config.
// If env vars are set but no DB config exists, it creates bucket and saves config.
func (s *GCPService) InitializeGCP(ctx context.Context) (*models.GCPConfig, error) {
	// Return existing config if already set up
	if s.IsConfiguredFromDatabase() {
		return s.GetCurrentConfig()
	}

	// If no DB config and no env config, skip initialization
	if !s.IsConfiguredFromEnv() {
		return nil, nil
	}

	// Initialize from env config
	return s.ConfigureGCP(ctx, s.cfg.GCP.ProjectID, s.cfg.GCP.Region, s.cfg.GCP.ServiceAccountJSON)
}

func (s *GCPService) ConfigureGCP(ctx context.Context, projectID, region, serviceAccountJSON string) (*models.GCPConfig, error) {
	// Get existing config to check if bucket already exists
	config, err := s.GetCurrentConfig()

	var bucketName string
	// Only create bucket if it doesn't exist in DB
	if err != nil || config.BucketName == "" {
		bucketName, err = s.CreateTerraformBucket(ctx, projectID, region)
		if err != nil {
			return nil, fmt.Errorf("failed to create terraform bucket: %w", err)
		}
	} else {
		bucketName = config.BucketName
	}

	// Always update service account
	gcpConfig, err := s.UpdateServiceAccount(ctx, projectID, region, serviceAccountJSON, bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to save GCP config: %w", err)
	}

	return gcpConfig, nil
}

func (s *GCPService) GetCurrentConfig() (*models.GCPConfig, error) {
	var config models.GCPConfig
	err := s.db.Where("is_configured = ?", true).First(&config).Error
	if err != nil {
		return nil, err
	}

	// clear key before returning
	config.ServiceAccountKeyJSON = ""
	return &config, nil
}

// returns config including service account credentials
func (s *GCPService) GetCurrentConfigWithCredentials() (*models.GCPConfig, error) {
	var config models.GCPConfig
	err := s.db.Where("is_configured = ?", true).First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (s *GCPService) GetTerraformBackendConfig() (map[string]string, error) {
	config, err := s.GetCurrentConfig()
	if err != nil {
		return nil, fmt.Errorf("GCP not configured: %w", err)
	}

	return map[string]string{
		"backend": "gcs",
		"bucket":  config.BucketName,
		"prefix":  "terraform/state",
	}, nil
}

func (s *GCPService) UpdateServiceAccount(ctx context.Context, projectID, region, serviceAccountJSON string, bucketName ...string) (*models.GCPConfig, error) {
	gcpConfig, err := gcpconfig.NewConfig(projectID, region, serviceAccountJSON, "")
	if err != nil {
		return nil, fmt.Errorf("invalid service account configuration: %w", err)
	}

	_, err = gcp.NewClientFromConfig(gcpConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCP client: %w", err)
	}

	var dbConfig models.GCPConfig
	err = s.db.Where("is_configured = ?", true).First(&dbConfig).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("failed to fetch existing config: %w", err)
	}

	if err == gorm.ErrRecordNotFound {
		dbConfig = models.GCPConfig{
			ID:           uuid.New(),
			IsConfigured: true,
		}
	}

	dbConfig.ProjectID = projectID
	dbConfig.Region = region
	dbConfig.ServiceAccountEmail = gcpConfig.ServiceAccountEmail
	dbConfig.ServiceAccountKeyJSON = serviceAccountJSON

	// Set bucket name if provided
	if len(bucketName) > 0 && bucketName[0] != "" {
		dbConfig.BucketName = bucketName[0]
	}

	if err == gorm.ErrRecordNotFound {
		err = s.db.Create(&dbConfig).Error
	} else {
		err = s.db.Save(&dbConfig).Error
	}

	if err != nil {
		return nil, fmt.Errorf("failed to save service account config: %w", err)
	}

	dbConfig.ServiceAccountKeyJSON = ""
	return &dbConfig, nil
}

// getGCPClient creates a GCP client from database config or falls back to env config
func (s *GCPService) getGCPClient(projectID, region string) (*gcp.Client, error) {
	if s.IsConfiguredFromDatabase() {
		config, err := s.GetCurrentConfigWithCredentials()
		if err != nil {
			return nil, fmt.Errorf("failed to get database config: %w", err)
		}
		gcpCfg, err := gcpconfig.NewConfig(config.ProjectID, config.Region, config.ServiceAccountKeyJSON, config.ServiceAccountEmail)
		if err != nil {
			return nil, fmt.Errorf("failed to create GCP config from database: %w", err)
		}
		return gcp.NewClientFromConfig(gcpCfg)
	}

	if s.IsConfiguredFromEnv() {
		gcpCfg, err := gcpconfig.NewConfig(projectID, region, s.cfg.GCP.ServiceAccountJSON, "")
		if err != nil {
			return nil, fmt.Errorf("failed to create GCP config from env: %w", err)
		}
		return gcp.NewClientFromConfig(gcpCfg)
	}

	return nil, fmt.Errorf("GCP not configured in database or environment")
}

func (s *GCPService) ExtractProjectID(serviceAccountJSON []byte) (string, error) {
	projectID, err := gcp.ExtractProjectIDFromServiceAccount(serviceAccountJSON)
	if err != nil {
		return "", fmt.Errorf("failed to extract project ID: %w", err)
	}
	return projectID, nil
}

func (s *GCPService) CreateTerraformBucket(ctx context.Context, projectID, region string) (string, error) {
	gcpClient, err := s.getGCPClient(projectID, region)
	if err != nil {
		return "", err
	}

	bucketName, err := gcpClient.CreateTerraformBucket(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create terraform bucket: %w", err)
	}

	return bucketName, nil
}
