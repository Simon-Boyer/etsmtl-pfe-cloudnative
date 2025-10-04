package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/viper"
)

type Config struct {
	Database DatabaseConfig `mapstructure:"database"`
	GitOps   GitOpsConfig   `mapstructure:"gitops"`
	GCP      GCPConfig      `mapstructure:"gcp"`
	GitHub   GitHubConfig   `mapstructure:"github"`
	JWT      JWTConfig      `mapstructure:"jwt"`
}

type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`
	SSLMode  string `mapstructure:"sslmode"`
}

type GitOpsConfig struct {
	RepoURL    string `mapstructure:"repo_url"`
	Branch     string `mapstructure:"branch"`
	WorkingDir string `mapstructure:"working_dir"`
	RepoOwner  string `mapstructure:"repo_owner"`
	RepoName   string `mapstructure:"repo_name"`
	Username   string `mapstructure:"username"`
	Email      string `mapstructure:"email"`
}

type GitHubConfig struct {
	AppID          int64  `mapstructure:"app_id"`
	PrivateKey     string `mapstructure:"private_key"`
	InstallationID int64  `mapstructure:"installation_id"`
}

type GCPConfig struct {
	ProjectID          string `mapstructure:"project_id"`
	Region             string `mapstructure:"region"`
	ServiceAccountJSON string `mapstructure:"service_account_json"`
}

type JWTConfig struct {
	SecretKey     string `mapstructure:"secret_key"`
	Issuer        string `mapstructure:"issuer"`
	ExpiryMinutes int    `mapstructure:"expiry_minutes"`
}

func Load() (*Config, error) {
	// setDefaults()

	viper.AutomaticEnv()

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if dbHost := os.Getenv("DB_HOST"); dbHost != "" {
		config.Database.Host = dbHost
	}
	if dbPort := os.Getenv("DB_PORT"); dbPort != "" {
		if port, err := strconv.Atoi(dbPort); err == nil {
			config.Database.Port = port
		}
	}
	if dbUser := os.Getenv("DB_USER"); dbUser != "" {
		config.Database.User = dbUser
	}
	if dbPassword := os.Getenv("DB_PASSWORD"); dbPassword != "" {
		config.Database.Password = dbPassword
	}
	if dbName := os.Getenv("DB_NAME"); dbName != "" {
		config.Database.Database = dbName
	}
	if gcpSAJSON := os.Getenv("GCP_SERVICE_ACCOUNT_JSON"); gcpSAJSON != "" {
		config.GCP.ServiceAccountJSON = gcpSAJSON
	}
	if gcpProject := os.Getenv("GCP_PROJECT_ID"); gcpProject != "" {
		config.GCP.ProjectID = gcpProject
	}
	if gcpRegion := os.Getenv("GCP_REGION"); gcpRegion != "" {
		config.GCP.Region = gcpRegion
	}

	if ghAppID := os.Getenv("GITHUB_APP_ID"); ghAppID != "" {
		if appID, err := strconv.ParseInt(ghAppID, 10, 64); err == nil {
			config.GitHub.AppID = appID
		}
	}
	if ghPrivateKey := os.Getenv("GITHUB_PRIVATE_KEY"); ghPrivateKey != "" {
		config.GitHub.PrivateKey = ghPrivateKey
	}
	if ghInstallationID := os.Getenv("GITHUB_INSTALLATION_ID"); ghInstallationID != "" {
		if installationID, err := strconv.ParseInt(ghInstallationID, 10, 64); err == nil {
			config.GitHub.InstallationID = installationID
		}
	}

	// JWT Config
	if jwtSecret := os.Getenv("JWT_SECRET_KEY"); jwtSecret != "" {
		config.JWT.SecretKey = jwtSecret
	}
	if jwtIssuer := os.Getenv("JWT_ISSUER"); jwtIssuer != "" {
		config.JWT.Issuer = jwtIssuer
	} else {
		config.JWT.Issuer = "stolos-backend" // default issuer
	}
	if jwtExpiry := os.Getenv("JWT_EXPIRY_MINUTES"); jwtExpiry != "" {
		if expiry, err := strconv.Atoi(jwtExpiry); err == nil {
			config.JWT.ExpiryMinutes = expiry
		}
	}
	if config.JWT.ExpiryMinutes == 0 {
		config.JWT.ExpiryMinutes = 1440 // default 24 hours
	}

	return &config, nil
}

// Left if we ever need it
// func setDefaults() {

// 	viper.SetDefault("database.host", "localhost")
// 	viper.SetDefault("database.port", 5432)
// 	viper.SetDefault("database.user", "postgres")
// 	viper.SetDefault("database.database", "stolos")
// 	viper.SetDefault("database.sslmode", "disable")

// }
