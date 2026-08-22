package models

import (
	"time"

	"github.com/google/uuid"
)

type OAuthConnection struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID         uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	Provider       string    `gorm:"type:varchar(50);not null" json:"provider"`
	ProviderUserID string    `gorm:"type:varchar(255);not null" json:"provider_user_id"`
	// ProviderUsername is the login used on change requests. Empty for
	// connections created before migration 029, and backfilled on the next
	// login — a verified step reports "not confirmed yet" rather than failing.
	ProviderUsername string    `gorm:"column:provider_username;type:varchar(255)" json:"provider_username,omitempty"`
	AccessToken      string    `gorm:"type:bytea;serializer:enc" json:"-"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (o *OAuthConnection) TableName() string {
	return "oauth_connections"
}
