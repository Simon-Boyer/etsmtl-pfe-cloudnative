package services_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stolos-cloud/stolos/backend/internal/config"
	"github.com/stolos-cloud/stolos/backend/internal/models"
	"github.com/stolos-cloud/stolos/backend/internal/services"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to setup test database: %v", err)
	}

	// Migrate test tables
	err = db.AutoMigrate(&models.Node{}, &models.Cluster{}, &models.GCPConfig{})
	if err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	return db
}

func TestNodeService_CreateNode(t *testing.T) {
	db := setupTestDB(t)
	cfg := &config.Config{}
	service := services.NewNodeService(db, cfg)

	clusterID := uuid.New()

	// Create node using service method
	node, err := service.CreateNode("test-node", "amd64", "gcp", clusterID)
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	// Verify node was created
	if node.ID == (uuid.UUID{}) {
		t.Error("CreateNode() should set ID after creation")
	}

	// Check fields
	if node.Name != "test-node" {
		t.Errorf("Name = %v, want %v", node.Name, "test-node")
	}
	if node.Architecture != "amd64" {
		t.Errorf("Architecture = %v, want %v", node.Architecture, "amd64")
	}
	if node.Provider != "gcp" {
		t.Errorf("Provider = %v, want %v", node.Provider, "gcp")
	}
	if node.Status != "pending" {
		t.Errorf("Status = %v, want %v", node.Status, "pending")
	}
}

func TestNodeService_GetNode(t *testing.T) {
	db := setupTestDB(t)
	cfg := &config.Config{}
	service := services.NewNodeService(db, cfg)

	clusterID := uuid.New()

	// Create test node
	createdNode, err := service.CreateNode("existing-node", "amd64", "onprem", clusterID)
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	// Test GetNode
	foundNode, err := service.GetNode(createdNode.ID)
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}

	if foundNode.Name != "existing-node" {
		t.Errorf("GetNode() Name = %v, want %v", foundNode.Name, "existing-node")
	}
}

func TestNodeService_GetNode_NotFound(t *testing.T) {
	db := setupTestDB(t)
	cfg := &config.Config{}
	service := services.NewNodeService(db, cfg)

	randomID := uuid.New()

	// Test getting non-existent node
	_, err := service.GetNode(randomID)
	if err == nil {
		t.Error("GetNode() expected error for non-existent node, got nil")
	}
}
