package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"golang.org/x/crypto/argon2"
)

type AuthService struct {
	repo        storage.Repository
	jwtSecret   string
	jwtIssuer   string
	jwtAudience string
	accessTTL   time.Duration
	refreshTTL  time.Duration
}

func NewAuthService(repo storage.Repository, jwtSecret, jwtIssuer, jwtAudience string, accessTTL, refreshTTL time.Duration) *AuthService {
	return &AuthService{
		repo:        repo,
		jwtSecret:   jwtSecret,
		jwtIssuer:   jwtIssuer,
		jwtAudience: jwtAudience,
		accessTTL:   accessTTL,
		refreshTTL:  refreshTTL,
	}
}

func (s *AuthService) LoginWithEmail(ctx context.Context, email, password string) (*models.TokenResponse, error) {
	user, err := s.repo.GetUserByEmail(ctx, email)
	if user == nil || err != nil {
		return nil, fmt.Errorf("invalid email or password")
	}

	if !verifyPasswordHash(user.PasswordHash, password) {
		return nil, fmt.Errorf("invalid email or password")
	}

	if !user.IsActive {
		return nil, fmt.Errorf("account is inactive")
	}

	return s.generateToken(ctx, user)
}

func (s *AuthService) RegisterWithEmail(ctx context.Context, email, fullName, password string) (*models.TokenResponse, error) {
	if err := validatePasswordStrength(password); err != nil {
		return nil, fmt.Errorf("password validation failed: %w", err)
	}

	existingUser, err := s.repo.GetUserByEmail(ctx, email)
	if existingUser != nil {
		return nil, fmt.Errorf("email already in use")
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := &models.User{
		ID:           uuid.New().String(),
		Email:        email,
		FullName:     fullName,
		Role:         models.RoleDeveloper,
		PasswordHash: passwordHash,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.repo.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return s.generateToken(ctx, user)
}

func (s *AuthService) generateToken(ctx context.Context, user *models.User) (*models.TokenResponse, error) {
	jti := uuid.New().String()

	now := time.Now()
	expiresAt := now.Add(s.accessTTL)

	claims := models.TokenClaims{
		UserID:   user.ID,
		Email:    user.Email,
		FullName: user.FullName,
		Role:     user.Role,
		JTI:      jti,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.jwtIssuer,
			Audience:  []string{s.jwtAudience},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return nil, fmt.Errorf("failed to sign token: %w", err)
	}

	tokenRecord := &models.Token{
		ID:        uuid.New(),
		UserID:    uuid.MustParse(user.ID),
		JTI:       jti,
		TokenHash: hashToken(signedToken),
		Type:      "access",
		IsRevoked: false,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}

	if err := s.repo.CreateToken(ctx, tokenRecord); err != nil {
		return nil, fmt.Errorf("failed to store token: %w", err)
	}

	return &models.TokenResponse{
		AccessToken: signedToken,
		TokenType:   "Bearer",
		ExpiresIn:   int64(s.accessTTL.Seconds()),
		User: models.UserInfo{
			ID:       user.ID,
			Email:    user.Email,
			FullName: user.FullName,
			Role:     user.Role,
		},
	}, nil
}

func (s *AuthService) ValidateToken(ctx context.Context, tokenString string) (*models.TokenClaims, error) {
	claims := &models.TokenClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.jwtSecret), nil
	})

	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	// TODO: Consider making token database validation optional or add better error handling
	tokenRecord, err := s.repo.GetTokenByJTI(ctx, claims.JTI)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve token record: %w", err)
	}

	// If token not found in DB, it's a new token - allow it (for backward compatibility with argon2 migration)
	if tokenRecord != nil {
		if tokenRecord.IsRevoked || time.Now().After(tokenRecord.ExpiresAt) {
			return nil, fmt.Errorf("token is revoked or expired")
		}
		s.repo.UpdateTokenLastUsed(ctx, claims.JTI)
	}

	return claims, nil
}

func (s *AuthService) RevokeToken(ctx context.Context, tokenString string) error {
	claims := &models.TokenClaims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.jwtSecret), nil
	})

	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	return s.repo.RevokeToken(ctx, claims.JTI, "revoked by user")
}

// ============================================
// HELPERS
// ============================================

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	hash := argon2.IDKey([]byte(password), salt, 2, 65536, 4, 32)
	return fmt.Sprintf("%x$%x", salt, hash), nil
}

func verifyPasswordHash(hash, password string) bool {
	parts := len(hash)
	if parts < 65 {
		return false
	}

	// Parse salt and hash from stored format "salt$hash"
	saltHex := hash[:32]
	storedHashHex := hash[33:]

	var salt [16]byte
	fmt.Sscanf(saltHex, "%x", &salt)

	computedHash := argon2.IDKey([]byte(password), salt[:], 2, 65536, 4, 32)
	computedHashHex := fmt.Sprintf("%x", computedHash)

	return computedHashHex == storedHashHex
}

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func validatePasswordStrength(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}

	if !containsUppercase(password) {
		return fmt.Errorf("password must contain at least one uppercase letter")
	}

	if !containsDigit(password) {
		return fmt.Errorf("password must contain at least one digit")
	}

	return nil
}

func containsUppercase(s string) bool {
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

func containsDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}
