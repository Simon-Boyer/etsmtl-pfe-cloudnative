package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type NodeStatus string

const (
	StatusPending NodeStatus = "pending"
	StatusActive  NodeStatus = "active"
	StatusFailed  NodeStatus = "failed"
)

var ValidNodeStatuses = map[NodeStatus]bool{
	StatusPending: true,
	StatusActive:  true,
	StatusFailed:  true,
}

type Node struct {
	ID           uuid.UUID      `json:"id" gorm:"type:uuid;primary_key"`
	Name         string         `json:"name" gorm:"not null;uniqueIndex"`
	Status       NodeStatus     `json:"status" gorm:"type:varchar(50);not null;default:'pending'"`
	Role         string         `json:"role" gorm:"not null;default:'worker'"` // worker, control-plane
	Labels       string         `json:"labels"`                                // JSON string for labels
	Architecture string         `json:"architecture" gorm:"not null"`          // amd64, arm64
	Provider     string         `json:"provider" gorm:"not null"`              // onprem, gcp
	IPAddress    string         `json:"ip_address"`
	MACAddress   string         `json:"mac_address"`
	InstanceID   string         `json:"instance_id,omitempty"` // GCP instance ID
	ClusterID    uuid.UUID      `json:"cluster_id" gorm:"type:uuid;index"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"index"`
}

func (n *Node) BeforeCreate(tx *gorm.DB) error {
	if n.ID == (uuid.UUID{}) {
		n.ID = uuid.New()
	}
	return nil
}

type Cluster struct {
	ID        uuid.UUID      `json:"id" gorm:"type:uuid;primary_key"`
	Name      string         `json:"name" gorm:"not null;uniqueIndex"`
	Nodes     []Node         `json:"nodes" gorm:"foreignKey:ClusterID"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (c *Cluster) BeforeCreate(tx *gorm.DB) error {
	if c.ID == (uuid.UUID{}) {
		c.ID = uuid.New()
	}
	return nil
}

type GCPConfig struct {
	ID                    uuid.UUID      `json:"id" gorm:"type:uuid;primary_key"`
	ProjectID             string         `json:"project_id" gorm:"not null"`
	BucketName            string         `json:"bucket_name" gorm:"not null"`
	ServiceAccountEmail   string         `json:"service_account_email" gorm:"not null"`
	ServiceAccountKeyJSON string         `json:"-" gorm:"type:text"`
	Region                string         `json:"region" gorm:"default:'us-central1'"`
	IsConfigured          bool           `json:"is_configured" gorm:"default:false"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
	DeletedAt             gorm.DeletedAt `json:"-" gorm:"index"`
}

func (g *GCPConfig) BeforeCreate(tx *gorm.DB) error {
	if g.ID == (uuid.UUID{}) {
		g.ID = uuid.New()
	}
	return nil
}
