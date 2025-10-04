package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/stolos-cloud/stolos/backend/internal/config"
	"github.com/stolos-cloud/stolos/backend/internal/models"
	"github.com/stolos-cloud/stolos/backend/pkg/gcp"
	"gorm.io/gorm"
)

type NodeService struct {
	db  *gorm.DB
	cfg *config.Config
}

func NewNodeService(db *gorm.DB, cfg *config.Config) *NodeService {
	return &NodeService{db: db, cfg: cfg}
}

func (s *NodeService) QueryGCPInstances(ctx context.Context) error {
	// Sample: Create GCP client from environment and list all instances
	client, err := gcp.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create GCP client: %w", err)
	}

	// List all instances across all zones
	allInstances, err := client.ListAllInstances(ctx)
	if err != nil {
		return fmt.Errorf("failed to list instances: %w", err)
	}

	// Log the results
	for zone, instances := range allInstances {
		fmt.Printf("Zone %s: %d instances\n", zone, len(instances))
		for _, instance := range instances {
			fmt.Printf("  - %s (%s)\n", instance.Name, instance.Status)
		}
	}

	return nil
}

func (s *NodeService) CreateNode(name, architecture, provider string, clusterID uuid.UUID) (*models.Node, error) {
	node := &models.Node{
		ID:           uuid.New(),
		Name:         name,
		Status:       models.StatusPending,
		Architecture: architecture,
		Provider:     provider,
		ClusterID:    clusterID,
	}

	if err := s.db.Create(node).Error; err != nil {
		return nil, err
	}

	return node, nil
}

func (s *NodeService) GetNode(id uuid.UUID) (*models.Node, error) {
	var node models.Node
	if err := s.db.First(&node, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

func (s *NodeService) UpdateNodeConfig(id uuid.UUID, role string, labels []string) (*models.Node, error) {
	var node models.Node
	if err := s.db.First(&node, "id = ?", id).Error; err != nil {
		return nil, err
	}

	node.Role = role
	if len(labels) > 0 {
		labelsJSON, err := json.Marshal(labels)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal labels: %w", err)
		}
		node.Labels = string(labelsJSON)
	}

	node.Status = models.StatusActive // todo this is just for sample testing

	if err := s.db.Save(&node).Error; err != nil {
		return nil, err
	}

	return &node, nil
}

type NodeConfigUpdate struct {
	ID     uuid.UUID `json:"id"`
	Role   string    `json:"role"`
	Labels []string  `json:"labels"`
}

func (s *NodeService) UpdateNodesConfig(updates []NodeConfigUpdate) ([]models.Node, error) {
	var updatedNodes []models.Node

	for _, update := range updates {
		node, err := s.UpdateNodeConfig(update.ID, update.Role, update.Labels)
		if err != nil {
			return nil, fmt.Errorf("failed to update node %s: %w", update.ID, err)
		}
		updatedNodes = append(updatedNodes, *node)
	}

	return updatedNodes, nil
}

// ListNodes lists nodes with optional status filter, offset, and limit.
// Pass empty string for status to list all nodes.
// Pass 0 for offset and limit to get all results without pagination.
func (s *NodeService) ListNodes(status string, offset, limit int) ([]models.Node, error) {
	var nodes []models.Node
	query := s.db.Order("created_at DESC")

	if status != "" {
		query = query.Where("status = ?", status)
	}

	if limit > 0 {
		query = query.Offset(offset).Limit(limit)
	}

	if err := query.Find(&nodes).Error; err != nil {
		return nil, err
	}
	return nodes, nil
}

// CreateSamplePendingNodes creates sample pending nodes if none exist in the database
func (s *NodeService) CreateSamplePendingNodes() error {
	// Check if there are any pending nodes
	var count int64
	if err := s.db.Model(&models.Node{}).Where("status = ?", models.StatusPending).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to count pending nodes: %w", err)
	}

	// If pending nodes already exist, skip creation
	if count > 0 {
		return nil
	}

	// Get or create default cluster
	var cluster models.Cluster
	err := s.db.Where("name = ?", "sample-cluster").First(&cluster).Error
	if err == gorm.ErrRecordNotFound {
		cluster = models.Cluster{
			ID:   uuid.New(),
			Name: "sample-cluster",
		}
		if err := s.db.Create(&cluster).Error; err != nil {
			return fmt.Errorf("failed to create cluster: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to query cluster: %w", err)
	}

	// Create sample nodes with auto-generated names
	sampleNodes := []models.Node{
		{
			ID:           uuid.New(),
			Name:         fmt.Sprintf("node-%s", uuid.New().String()[:8]),
			Status:       models.StatusPending,
			Architecture: "amd64",
			Provider:     "onprem",
			ClusterID:    cluster.ID,
		},
		{
			ID:           uuid.New(),
			Name:         fmt.Sprintf("node-%s", uuid.New().String()[:8]),
			Status:       models.StatusPending,
			Architecture: "arm64",
			Provider:     "onprem",
			ClusterID:    cluster.ID,
		},
	}

	for _, node := range sampleNodes {
		if err := s.db.Create(&node).Error; err != nil {
			return fmt.Errorf("failed to create node %s: %w", node.Name, err)
		}
	}

	return nil
}
