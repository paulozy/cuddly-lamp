package models

import (
	"time"

	"gorm.io/datatypes"
)

type UserRole string

const (
	RoleAdmin      UserRole = "admin"
	RoleMaintainer UserRole = "maintainer"
	RoleDeveloper  UserRole = "developer"
	RoleViewer     UserRole = "viewer"
)

type User struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	Email    string   `gorm:"uniqueIndex;type:varchar(255)" json:"email"`
	FullName string   `gorm:"type:varchar(255)" json:"full_name"`
	Avatar   string   `gorm:"type:text" json:"avatar"`
	Role     UserRole `gorm:"type:varchar(50);default:'developer'" json:"role"`

	GitHubID    string `gorm:"type:varchar(255);index" json:"github_id,omitempty"`
	GitLabID    string `gorm:"type:varchar(255);index" json:"gitlab_id,omitempty"`
	GithubToken string `gorm:"type:text" json:"-"`
	GitlabToken string `gorm:"type:text" json:"-"`

	IsActive bool      `gorm:"default:true" json:"is_active"`
	LastSeen time.Time `json:"last_seen,omitempty"`

	CreatedAt time.Time                     `json:"created_at"`
	UpdatedAt time.Time                     `json:"updated_at"`
	DeletedAt datatypes.JSONQueryExpression `gorm:"index" json:"deleted_at,omitempty"`

	Repositories []Repository `gorm:"many2many:user_repositories;" json:"repositories,omitempty"`
}

func (User) TableName() string {
	return "users"
}

func (u *User) IsValid() bool {
	return u.Email != "" && u.FullName != "" && u.Role != ""
}

func (u *User) GetRole() UserRole {
	return u.Role
}

func (u *User) HasPermission(requiredRole UserRole) bool {
	roleLevels := map[UserRole]int{
		RoleViewer:     1,
		RoleDeveloper:  2,
		RoleMaintainer: 3,
		RoleAdmin:      4,
	}

	userLevel := roleLevels[u.Role]
	requiredLevel := roleLevels[requiredRole]

	return userLevel >= requiredLevel
}
