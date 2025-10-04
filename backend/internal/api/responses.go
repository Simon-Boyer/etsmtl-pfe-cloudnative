package api

import (
	"github.com/google/uuid"
	"github.com/stolos-cloud/stolos/backend/internal/models"
)

type UserResponse struct {
	ID    uuid.UUID   `json:"id"`
	Email string      `json:"email"`
	Role  models.Role `json:"role"`
	Teams []TeamInfo  `json:"teams"`
}

type TeamInfo struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type TeamResponse struct {
	ID    uuid.UUID      `json:"id"`
	Name  string         `json:"name"`
	Users []UserResponse `json:"users,omitempty"`
}

func ToUserResponse(user *models.User) UserResponse {
	teams := make([]TeamInfo, len(user.Teams))
	for i, team := range user.Teams {
		teams[i] = TeamInfo{
			ID:   team.ID,
			Name: team.Name,
		}
	}

	return UserResponse{
		ID:    user.ID,
		Email: user.Email,
		Role:  user.Role,
		Teams: teams,
	}
}

func ToTeamResponse(team *models.Team, includeUsers bool) TeamResponse {
	response := TeamResponse{
		ID:   team.ID,
		Name: team.Name,
	}

	if includeUsers {
		users := make([]UserResponse, len(team.Users))
		for i, user := range team.Users {
			users[i] = ToUserResponse(&user)
		}
		response.Users = users
	}

	return response
}
