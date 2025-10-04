package database

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/stolos-cloud/stolos/backend/internal/config"
	"github.com/stolos-cloud/stolos/backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Initialize(cfg config.DatabaseConfig) (*gorm.DB, error) {
	var db *gorm.DB
	var err error

	if cfg.Host != "" {
		dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=%s",
			cfg.Host, cfg.User, cfg.Password, cfg.Database, cfg.Port, cfg.SSLMode)

		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Info),
		})

		if err == nil {
			log.Println("Connected to PostgreSQL database")
		} else {
			log.Printf("PostgreSQL connection failed: %v", err)
		}
	}

	// Fallback to SQLite
	if db == nil {
		log.Println("Using SQLite as database (development mode)")
		db, err = gorm.Open(sqlite.Open("./data.db"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Info),
		})

		if err != nil {
			return nil, fmt.Errorf("failed to connect to SQLite: %w", err)
		}
	}

	if err := runMigrations(db); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Auto-create admin user if environment variables are set
	if err := createDefaultAdmin(db); err != nil {
		log.Printf("Warning: Failed to create admin user: %v", err)
	}

	return db, nil
}

func createDefaultAdmin(db *gorm.DB) error {
	adminEmail := os.Getenv("ADMIN_EMAIL")
	adminPassword := os.Getenv("ADMIN_PASSWORD")

	if adminEmail == "" || adminPassword == "" {
		return nil
	}

	adminEmail = strings.ToLower(adminEmail)

	// Check if admin user already exists
	var existingUser models.User
	err := db.Where("email = ?", adminEmail).First(&existingUser).Error
	if err == nil {
		// Admin already exists
		log.Printf("Admin user already exists: %s", adminEmail)
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("database error checking for admin: %w", err)
	}

	// Create admin user
	admin := models.User{
		Email: adminEmail,
		Role:  models.RoleAdmin,
	}

	if err := admin.SetPassword(adminPassword); err != nil {
		return fmt.Errorf("failed to hash admin password: %w", err)
	}

	if err := db.Create(&admin).Error; err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}

	// Create administrators team if it doesn't exist and add admin to it
	var adminTeam models.Team
	err = db.Where("name = ?", "administrators").First(&adminTeam).Error
	if err == gorm.ErrRecordNotFound {
		adminTeam = models.Team{Name: "administrators"}
		if err := db.Create(&adminTeam).Error; err != nil {
			log.Printf("Warning: Failed to create administrators team: %v", err)
		} else {
			// Add admin to team
			if err := db.Model(&admin).Association("Teams").Append(&adminTeam); err != nil {
				log.Printf("Warning: Failed to add admin to administrators team: %v", err)
			}
		}
	} else if err == nil {
		// Team exists, add admin to it
		if err := db.Model(&admin).Association("Teams").Append(&adminTeam); err != nil {
			log.Printf("Warning: Failed to add admin to existing administrators team: %v", err)
		}
	}

	log.Printf("Admin user created successfully: %s", adminEmail)
	return nil
}

func runMigrations(db *gorm.DB) error {
	// UUID extension no needed for SQLite
	if db.Dialector.Name() == "postgres" {
		if err := db.Exec("CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\"").Error; err != nil {
			return fmt.Errorf("failed to create uuid extension: %w", err)
		}
	}

	// auto-migrations
	return db.AutoMigrate(
		&models.Cluster{},
		&models.Node{},
		&models.GCPConfig{},
		&models.Team{},
		&models.User{},
		&models.UserTeam{},
		&models.Deployment{},
	)
}

func Seed(db *gorm.DB) error {
	// TODO: Add any initial data seeding
	return nil
}
