package services

import (
	"fmt"

	"github.com/stolos-cloud/stolos/backend/internal/config"
	"gorm.io/gorm"
)

type ISOService struct {
	db  *gorm.DB
	cfg *config.Config
}

type ISOResponse struct {
	DownloadURL string `json:"download_url"`
	Filename    string `json:"filename"`
}

func NewISOService(db *gorm.DB, cfg *config.Config) *ISOService {
	return &ISOService{db: db, cfg: cfg}
}

func (s *ISOService) GenerateISO(name, architecture string) (*ISOResponse, error) {
	// TODO: Implement actual Talos Image Factory integration

	return &ISOResponse{
		DownloadURL: fmt.Sprintf("https://factory.talos.dev/image/example-id/v1.11.1/metal-%s.iso", architecture),
		Filename:    fmt.Sprintf("%s-talos-%s.iso", name, architecture),
	}, nil
}
