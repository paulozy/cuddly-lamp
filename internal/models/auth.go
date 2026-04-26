package models

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type TokenClaims struct {
	UserID   string   `json:"sub"`
	Email    string   `json:"email"`
	FullName string   `json:"full_name"`
	Role     UserRole `json:"role"`
	JTI      string   `json:"jti"`
	jwt.RegisteredClaims
}

type Token struct {
	ID        uuid.UUID `gorm:"primaryKey" json:"id"`
	UserID    uuid.UUID `gorm:"index" json:"user_id"`
	JTI       string    `gorm:"uniqueIndex" json:"jti"`
	TokenHash string    `json:"-"`

	Type      string     `json:"type"` // "access" or "refresh"
	FamilyID  *uuid.UUID `gorm:"column:family_id;index" json:"family_id,omitempty"`
	ParentJTI *string    `gorm:"column:parent_jti" json:"parent_jti,omitempty"`
	IsRevoked bool       `gorm:"index" json:"is_revoked"`

	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    time.Time  `gorm:"index" json:"expires_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	RevokeReason *string    `json:"revoke_reason"`

	User *User `json:"-" gorm:"foreignKey:UserID"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=1"`
}

type TokenResponse struct {
	AccessToken      string   `json:"access_token"`
	TokenType        string   `json:"token_type"` // "Bearer"
	ExpiresIn        int64    `json:"expires_in"` // seconds
	RefreshToken     string   `json:"refresh_token"`
	RefreshExpiresIn int64    `json:"refresh_expires_in"` // seconds
	User             UserInfo `json:"user"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type UserInfo struct {
	ID       string   `json:"id"`
	Email    string   `json:"email"`
	FullName string   `json:"full_name"`
	Role     UserRole `json:"role"`
}

type ErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}
