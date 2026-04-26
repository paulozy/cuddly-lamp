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
	AccessToken    string    `gorm:"type:text" json:"-"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (o *OAuthConnection) TableName() string {
	return "oauth_connections"
}
