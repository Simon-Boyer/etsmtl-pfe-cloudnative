package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// We can revise the name and model just wanted to provide a starting point
type Deployment struct {
	ID        uuid.UUID      `json:"id" gorm:"type:uuid;primary_key"`
	Name      string         `json:"name" gorm:"not null"`
	TeamID    uuid.UUID      `json:"team_id" gorm:"type:uuid;not null;index"`
	Team      Team           `json:"team" gorm:"foreignKey:TeamID"`
	CreatedBy uuid.UUID      `json:"created_by" gorm:"type:uuid;not null;index"`
	Creator   User           `json:"creator" gorm:"foreignKey:CreatedBy"`
	Status    string         `json:"status" gorm:"not null;default:'pending'"`
	Config    string         `json:"config" gorm:"type:text"` // JSON configuration
	ClusterID *uuid.UUID     `json:"cluster_id,omitempty" gorm:"type:uuid;index"`
	Cluster   *Cluster       `json:"cluster,omitempty" gorm:"foreignKey:ClusterID"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (d *Deployment) BeforeCreate(tx *gorm.DB) error {
	if d.ID == (uuid.UUID{}) {
		d.ID = uuid.New()
	}
	return nil
}
