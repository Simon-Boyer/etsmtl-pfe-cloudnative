package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/stolos-cloud/stolos/backend/internal/config"
	"github.com/stolos-cloud/stolos/backend/internal/services"
	"gorm.io/gorm"
)

type ISOHandlers struct {
	db         *gorm.DB
	isoService *services.ISOService
}

func NewISOHandlers(db *gorm.DB, cfg *config.Config) *ISOHandlers {
	return &ISOHandlers{
		db:         db,
		isoService: services.NewISOService(db, cfg),
	}
}

// GenerateISO godoc
// @Summary Generate a custom ISO
// @Description Generate a custom ISO image for node provisioning
// @Tags iso
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /iso/generate [post]
// @Security BearerAuth
func (h *ISOHandlers) GenerateISO(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Generate ISO - TODO"})
}
