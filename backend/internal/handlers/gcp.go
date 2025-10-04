package handlers

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/stolos-cloud/stolos/backend/internal/config"
	"github.com/stolos-cloud/stolos/backend/internal/services"
	"gorm.io/gorm"
)

type GCPHandlers struct {
	db               *gorm.DB
	gcpService       *services.GCPService
	nodeService      *services.NodeService
	terraformService *services.TerraformService
}

func NewGCPHandlers(db *gorm.DB, cfg *config.Config) *GCPHandlers {
	return &GCPHandlers{
		db:               db,
		gcpService:       services.NewGCPService(db, cfg),
		nodeService:      services.NewNodeService(db, cfg),
		terraformService: services.NewTerraformService(db, cfg),
	}
}

// GetGCPStatus godoc
// @Summary Get GCP configuration status
// @Description Retrieve the current GCP configuration status
// @Tags gcp
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /gcp/status [get]
// @Security BearerAuth
func (h *GCPHandlers) GetGCPStatus(c *gin.Context) {
	config, err := h.gcpService.GetCurrentConfig()
	if err != nil {
		if err.Error() == "record not found" {
			c.JSON(http.StatusOK, gin.H{
				"configured": false,
				"message":    "GCP not initialized",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"configured": true,
		"config":     config,
	})
}

// ConfigureGCP godoc
// @Summary Configure GCP
// @Description Configure GCP with provided project ID, region, and service account JSON
// @Tags gcp
// @Accept json
// @Produce json
// @Param config body object{region=string,service_account_json=string} true "GCP configuration"
// @Success 200 {object} models.GCPConfig
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /gcp/configure [post]
// @Security BearerAuth
func (h *GCPHandlers) ConfigureGCP(c *gin.Context) {
	var req struct {
		Region             string `json:"region" binding:"required"`
		ServiceAccountJSON string `json:"service_account_json" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ProjectID, err := h.gcpService.ExtractProjectID([]byte(req.ServiceAccountJSON))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service account JSON"})
		return
	}

	gcpConfig, err := h.gcpService.ConfigureGCP(c.Request.Context(), ProjectID, req.Region, req.ServiceAccountJSON)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gcpConfig)
}

// ConfigureGCPUpload godoc
// @Summary Configure GCP with file upload
// @Description Configure GCP by uploading service account JSON file
// @Tags gcp
// @Accept multipart/form-data
// @Produce json
// @Param region formData string true "GCP Region"
// @Param service_account_file formData file true "Service Account JSON file"
// @Success 200 {object} models.GCPConfig
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /gcp/configure/upload [post]
// @Security BearerAuthAuth
func (h *GCPHandlers) ConfigureGCPUpload(c *gin.Context) {
	region := c.PostForm("region")

	if region == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "region is required"})
		return
	}

	file, err := c.FormFile("service_account_file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service_account_file is required"})
		return
	}

	// Read file contents
	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to open file"})
		return
	}
	defer f.Close()

	serviceAccountJSON, err := io.ReadAll(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file contents"})
		return
	}

	projectID, err := h.gcpService.ExtractProjectID(serviceAccountJSON)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service account JSON"})
		return
	}

	gcpConfig, err := h.gcpService.ConfigureGCP(c.Request.Context(), projectID, region, string(serviceAccountJSON))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gcpConfig)
}

// CreateTerraformBucket godoc
// @Summary Create GCP Terraform bucket
// @Description Create a GCP bucket for storing Terraform state files
// @Tags gcp
// @Accept json
// @Produce json
// @Param config body object{project_id=string,region=string} true "GCP project ID and region"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /gcp/bucket [post]
// @Security BearerAuth
func (h *GCPHandlers) CreateTerraformBucket(c *gin.Context) {
	var req struct {
		ProjectID string `json:"project_id" binding:"required"`
		Region    string `json:"region" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	bucketName, err := h.gcpService.CreateTerraformBucket(c.Request.Context(), req.ProjectID, req.Region)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"bucket_name": bucketName,
		"message":     "Terraform bucket created successfully",
	})
}

// QueryGCPInstances godoc
// @Summary Query GCP instances
// @Description Query and store GCP instances in the database
// @Tags gcp
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /gcp/query-instances [post]
// @Security BearerAuth
func (h *GCPHandlers) QueryGCPInstances(c *gin.Context) {
	err := h.nodeService.QueryGCPInstances(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Sample : successfully queried GCP instances"})
}

// InitInfra godoc
// @Summary Initialize Terraform infrastructure
// @Description Initialize the Terraform infrastructure on GCP
// @Tags gcp
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /gcp/init-infra [post]
// @Security BearerAuth
func (h *GCPHandlers) InitInfra(c *gin.Context) {
	err := h.terraformService.InitializeInfrastructure(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Terraform infrastructure initialized successfully"})
}

// DeleteInfra godoc
// @Summary Destroy Terraform infrastructure
// @Description Destroy the Terraform infrastructure on GCP
// @Tags gcp
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /gcp/delete-infra [post]
// @Security BearerAuth
func (h *GCPHandlers) DeleteInfra(c *gin.Context) {
	err := h.terraformService.DestroyInfrastructure(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Terraform infrastructure destroyed successfully"})
}
