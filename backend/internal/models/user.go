package models

import (
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Role string

const (
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
	RoleViewer    Role = "viewer" // to discuss I thought it could be useful
)

type User struct {
	ID           uuid.UUID      `json:"id" gorm:"type:uuid;primary_key"`
	Email        string         `json:"email" gorm:"not null;uniqueIndex"`
	PasswordHash string         `json:"-" gorm:"not null"`
	Role         Role           `json:"role" gorm:"not null;default:'developer'"`
	Teams        []Team         `json:"teams,omitempty" gorm:"many2many:user_teams;"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"index"`
}

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == (uuid.UUID{}) {
		u.ID = uuid.New()
	}
	return nil
}

type Team struct {
	ID        uuid.UUID      `json:"id" gorm:"type:uuid;primary_key"`
	Name      string         `json:"name" gorm:"not null;uniqueIndex"`
	Users     []User         `json:"users,omitempty" gorm:"many2many:user_teams;"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (t *Team) BeforeCreate(tx *gorm.DB) error {
	if t.ID == (uuid.UUID{}) {
		t.ID = uuid.New()
	}
	return nil
}

// UserTeam is the join table for many-to-many relationship
type UserTeam struct {
	UserID uuid.UUID `gorm:"type:uuid;primaryKey"`
	TeamID uuid.UUID `gorm:"type:uuid;primaryKey"`
	User   User      `gorm:"foreignKey:UserID"`
	Team   Team      `gorm:"foreignKey:TeamID"`
}

func (u *User) SetPassword(password string) error {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hashedPassword)
	return nil
}

func (u *User) CheckPassword(password string) error {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password))
}
