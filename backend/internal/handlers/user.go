package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stolos-cloud/stolos/backend/internal/api"
	"github.com/stolos-cloud/stolos/backend/internal/models"
	"gorm.io/gorm"
)

type UserHandlers struct {
	db *gorm.DB
}

func NewUserHandlers(db *gorm.DB) *UserHandlers {
	return &UserHandlers{db: db}
}

type UpdateUserRoleRequest struct {
	Role models.Role `json:"role" binding:"required"`
}

// ListUsers godoc
// @Summary List all users
// @Description Get a list of all users
// @Tags users
// @Produce json
// @Success 200 {object} map[string][]api.UserResponse
// @Failure 500 {object} map[string]string
// @Router /users [get]
// @Security BearerAuth
func (h *UserHandlers) ListUsers(c *gin.Context) {
	var users []models.User
	if err := h.db.Preload("Teams").Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch users"})
		return
	}

	response := make([]api.UserResponse, len(users))
	for i, user := range users {
		response[i] = api.ToUserResponse(&user)
	}

	c.JSON(http.StatusOK, gin.H{"users": response})
}

// GetUser godoc
// @Summary Get user by ID
// @Description Get details of a user by their ID
// @Tags users
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} map[string]api.UserResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /users/{id} [get]
// @Security BearerAuth
func (h *UserHandlers) GetUser(c *gin.Context) {
	userID := c.Param("id")
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	var user models.User
	if err := h.db.Preload("Teams").First(&user, "id = ?", userUUID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": api.ToUserResponse(&user)})
}

// UpdateUserRole godoc
// @Summary Update user role
// @Description Update the role of a user (e.g., admin, user)
// @Tags users
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Param role body UpdateUserRoleRequest true "New role"
// @Success 200 {object} map[string]api.UserResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /users/{id}/role [put]
// @Security BearerAuth
func (h *UserHandlers) UpdateUserRole(c *gin.Context) {
	userID := c.Param("id")
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	var req UpdateUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user models.User
	if err := h.db.First(&user, "id = ?", userUUID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	user.Role = req.Role
	if err := h.db.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user role"})
		return
	}

	// Reload user with teams
	h.db.Preload("Teams").First(&user, user.ID)

	c.JSON(http.StatusOK, gin.H{"user": api.ToUserResponse(&user)})
}

// UpdateUserRole godoc
// @Summary Update user role
// @Description Update the role of a user (e.g., admin, user)
// @Tags users
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Param role body UpdateUserRoleRequest true "New role"
// @Success 200 {object} map[string]api.UserResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /users/{id}/role [put]
// @Security BearerAuth
func (h *UserHandlers) DeleteUser(c *gin.Context) {
	userID := c.Param("id")
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	var user models.User
	if err := h.db.First(&user, "id = ?", userUUID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// Remove user from all teams
	if err := h.db.Model(&user).Association("Teams").Clear(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove user from teams"})
		return
	}

	if err := h.db.Delete(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User deleted successfully"})
}

// CreateUser godoc
// @Summary Create a new user
// @Description Register a new user
// @Tags users
// @Accept json
// @Produce json
// @Param user body RegisterRequest true "User registration data"
// @Success 201 {object} map[string]api.UserResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /users/create [post]
// @Security BearerAuth
func (h *UserHandlers) CreateUser(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if user already exists
	var existingUser models.User
	if err := h.db.First(&existingUser, "email = ?", strings.ToLower(req.Email)).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
		return
	}

	user := models.User{
		Email: strings.ToLower(req.Email),
		Role:  req.Role,
	}

	if err := user.SetPassword(req.Password); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// Reload user with teams
	h.db.Preload("Teams").First(&user, user.ID)

	c.JSON(http.StatusCreated, gin.H{"user": api.ToUserResponse(&user)})
}
