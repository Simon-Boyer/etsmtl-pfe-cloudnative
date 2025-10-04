package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/stolos-cloud/stolos/backend/internal/config"
	"github.com/stolos-cloud/stolos/backend/pkg/gcp"
	"github.com/stolos-cloud/stolos/backend/pkg/github"
	"gorm.io/gorm"
)

type TerraformService struct {
	db           *gorm.DB
	cfg          *config.Config
	gcpClient    *gcp.Client
	githubClient *github.Client
	gcpService   *GCPService
}

type NodeConfig struct {
	Name         string
	Zone         string
	MachineType  string
	Architecture string
}

func NewTerraformService(db *gorm.DB, cfg *config.Config) *TerraformService {
	gcpService := NewGCPService(db, cfg)
	return &TerraformService{
		db:         db,
		cfg:        cfg,
		gcpService: gcpService,
	}
}

// initialize the service with GCP and GitHub clients from kubeconfig
func (s *TerraformService) WithCredentials(kubeconfig []byte) error {
	// Initialize GCP client from environment variables
	gcpClient, err := gcp.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create GCP client: %w", err)
	}

	// Initialize GitHub client from environment variables
	githubClient, err := github.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	s.gcpClient = gcpClient
	s.githubClient = githubClient
	return nil
}

// generates Terraform configuration for GCP node
func (s *TerraformService) GenerateGCPNodeConfig(nodeConfig NodeConfig) (string, error) {
	// TODO: Load template, execute with nodeConfig data, return generated .tf content
	// from terraform-templates/gcp/node.tf.tmpl

	return fmt.Sprintf("# Generated Terraform config for node: %s", nodeConfig.Name), nil
}

func (s *TerraformService) CommitToRepo(configContent, commitMessage string) error {
	// TODO: Use cfg.GitOps settings to commit Terraform files

	return nil
}

// sets up the base infrastructure (VPC, subnets, etc.) needed for VM provisioning
func (s *TerraformService) InitializeInfrastructure(ctx context.Context) error {
	// Create a temporary directory for terraform files
	workDir, err := os.MkdirTemp("", "terraform-infra-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(workDir)

	// Generate infrastructure terraform configuration
	if err := s.generateInfrastructureConfig(workDir); err != nil {
		return fmt.Errorf("failed to generate infrastructure config: %w", err)
	}

	tf, err := s.initializeTerraform(workDir)
	if err != nil {
		return fmt.Errorf("failed to initialize terraform: %w", err)
	}

	// yolo
	if err := s.applyInfrastructure(ctx, tf); err != nil {
		return fmt.Errorf("failed to apply infrastructure: %w", err)
	}

	// Mock: Commit terraform files to repository
	if err := s.commitInfrastructureToRepo(workDir); err != nil {
		return fmt.Errorf("failed to commit to repository: %w", err)
	}

	return nil
}

// holds the data for infrastructure template
type InfrastructureTemplateData struct {
	BucketName string
	ProjectID  string
	Region     string
}

// generateInfrastructureConfig creates terraform files for base infrastructure using templates
func (s *TerraformService) generateInfrastructureConfig(workDir string) error {
	// Get GCP backend configuration
	backendConfig, err := s.gcpService.GetTerraformBackendConfig()
	if err != nil {
		return fmt.Errorf("failed to get backend config: %w", err)
	}

	gcpConfig, err := s.gcpService.GetCurrentConfig()
	if err != nil {
		return fmt.Errorf("failed to get GCP config: %w", err)
	}

	// Prepare template data
	templateData := InfrastructureTemplateData{
		BucketName: backendConfig["bucket"],
		ProjectID:  gcpConfig.ProjectID,
		Region:     gcpConfig.Region,
	}

	// Load and execute template
	templatePath := "terraform-templates/gcp/infrastructure.tf.tmpl"
	tmpl, err := loadTemplate(templatePath)
	if err != nil {
		return fmt.Errorf("failed to load infrastructure template: %w", err)
	}

	// Generate main.tf from template
	mainTfPath := filepath.Join(workDir, "main.tf")
	mainTfFile, err := os.Create(mainTfPath)
	if err != nil {
		return fmt.Errorf("failed to create main.tf: %w", err)
	}
	defer mainTfFile.Close()

	if err := tmpl.Execute(mainTfFile, templateData); err != nil {
		return fmt.Errorf("failed to execute infrastructure template: %w", err)
	}

	return nil
}

// initializeTerraform initializes terraform in the working directory
func (s *TerraformService) initializeTerraform(workDir string) (*tfexec.Terraform, error) {
	// Find terraform binary (assuming it's in PATH)
	terraformPath, err := exec.LookPath("terraform")
	if err != nil {
		return nil, fmt.Errorf("terraform binary not found: %w", err)
	}

	tf, err := tfexec.NewTerraform(workDir, terraformPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create terraform instance: %w", err)
	}

	// Set up GCP credentials
	gcpConfig, err := s.gcpService.GetCurrentConfigWithCredentials()
	if err != nil {
		return nil, fmt.Errorf("failed to get GCP config: %w", err)
	}

	envVars := map[string]string{
		"GOOGLE_CREDENTIALS": gcpConfig.ServiceAccountKeyJSON,
		"GOOGLE_PROJECT":     gcpConfig.ProjectID,
	}

	// Preserve PATH
	if path := os.Getenv("PATH"); path != "" {
		envVars["PATH"] = path
	}

	tf.SetEnv(envVars)

	// Initialize terraform
	if err := tf.Init(context.Background()); err != nil {
		return nil, fmt.Errorf("terraform init failed: %w", err)
	}

	return tf, nil
}

// applyInfrastructure plans and applies the infrastructure
func (s *TerraformService) applyInfrastructure(ctx context.Context, tf *tfexec.Terraform) error {
	// Plan the changes
	planHasChanges, err := tf.Plan(ctx)
	if err != nil {
		return fmt.Errorf("terraform plan failed: %w", err)
	}

	if !planHasChanges {
		fmt.Println("No infrastructure changes required")
		return nil
	}

	// Apply the changes
	if err := tf.Apply(ctx); err != nil {
		return fmt.Errorf("terraform apply failed: %w", err)
	}

	fmt.Println("Infrastructure provisioned successfully")
	return nil
}

func (s *TerraformService) commitInfrastructureToRepo(workDir string) error {
	// Mock :
	// 1. Create/update terraform files in the repository
	// 2. Commit changes with appropriate message
	// 3. Push to remote repository

	fmt.Println("Mock: Committing infrastructure configuration to repository")
	fmt.Printf("Mock: Terraform files would be committed from: %s\n", workDir)

	return nil
}

func (s *TerraformService) DestroyInfrastructure(ctx context.Context) error {
	// temporary directory for terraform operations
	workDir, err := os.MkdirTemp("", "terraform-destroy-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(workDir)

	// Generate infrastructure configuration
	if err := s.generateInfrastructureConfig(workDir); err != nil {
		return fmt.Errorf("failed to generate infrastructure config: %w", err)
	}

	// Initialize terraform
	tf, err := s.initializeTerraform(workDir)
	if err != nil {
		return fmt.Errorf("failed to initialize terraform: %w", err)
	}

	// Destroy infrastructure
	if err := tf.Destroy(ctx); err != nil {
		return fmt.Errorf("terraform destroy failed: %w", err)
	}

	fmt.Println("Infrastructure destroyed successfully")
	return nil
}

func (s *TerraformService) GetInfrastructureStatus(ctx context.Context) (map[string]any, error) {
	// Mock .. we could instead do
	// 1. Check terraform state
	// 2. Return resource status and outputs

	return map[string]any{
		"status": "provisioned",
		"vpc":    "main-vpc",
		"subnet": "main-subnet",
		"region": s.gcpService.cfg.GCP.Region,
	}, nil
}

func loadTemplate(templatePath string) (*template.Template, error) {
	return template.ParseFiles(templatePath)
}
