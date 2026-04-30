package services

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/oauth"
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
	providers   map[string]oauth.OAuthProvider
}

func NewAuthService(repo storage.Repository, jwtSecret, jwtIssuer, jwtAudience string, accessTTL, refreshTTL time.Duration) *AuthService {
	return &AuthService{
		repo:        repo,
		jwtSecret:   jwtSecret,
		jwtIssuer:   jwtIssuer,
		jwtAudience: jwtAudience,
		accessTTL:   accessTTL,
		refreshTTL:  refreshTTL,
		providers:   make(map[string]oauth.OAuthProvider),
	}
}

var (
	ErrInvalidToken = fmt.Errorf("invalid token")
	ErrTokenExpired = fmt.Errorf("token expired")
	ErrTokenRevoked = fmt.Errorf("token revoked")
)

type loginSelectionClaims struct {
	UserID          string   `json:"user_id"`
	OrganizationIDs []string `json:"organization_ids"`
	Purpose         string   `json:"purpose"`
	jwt.RegisteredClaims
}

func (s *AuthService) LoginWithEmail(ctx context.Context, email, password, orgSlug string) (*models.TokenResponse, *models.OrganizationSelectionResponse, error) {
	user, err := s.repo.GetUserByEmail(ctx, email)
	if user == nil || err != nil {
		return nil, nil, fmt.Errorf("invalid email or password")
	}

	if !verifyPasswordHash(user.PasswordHash, password) {
		return nil, nil, fmt.Errorf("invalid email or password")
	}

	if !user.IsActive {
		return nil, nil, fmt.Errorf("account is inactive")
	}

	org, member, selection, err := s.resolveLoginOrganization(ctx, user.ID, orgSlug)
	if err != nil {
		return nil, nil, err
	}
	if selection != nil {
		return nil, selection, nil
	}

	resp, err := s.generateTokenPair(ctx, user, org, member.Role)
	return resp, nil, err
}

func (s *AuthService) SelectOrganization(ctx context.Context, loginTicket, organizationID string) (*models.TokenResponse, error) {
	claims, err := s.verifyLoginSelectionTicket(loginTicket)
	if err != nil {
		return nil, err
	}

	if !containsString(claims.OrganizationIDs, organizationID) {
		return nil, fmt.Errorf("organization is not available for this login")
	}

	user, err := s.repo.GetUser(ctx, claims.UserID)
	if err != nil || user == nil || !user.IsActive {
		return nil, fmt.Errorf("invalid user")
	}

	org, err := s.repo.GetOrganization(ctx, organizationID)
	if err != nil || org == nil || !org.IsActive {
		return nil, fmt.Errorf("invalid organization")
	}

	member, err := s.repo.GetOrganizationMember(ctx, org.ID, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve organization membership: %w", err)
	}
	if member == nil {
		return nil, fmt.Errorf("user does not belong to organization")
	}

	return s.generateTokenPair(ctx, user, org, member.Role)
}

func (s *AuthService) RegisterWithEmail(ctx context.Context, email, fullName, password, organizationName, organizationSlug string) (*models.TokenResponse, error) {
	if err := validatePasswordStrength(password); err != nil {
		return nil, fmt.Errorf("password validation failed: %w", err)
	}

	org, err := s.getOrCreateOrganization(ctx, organizationName, organizationSlug)
	if err != nil {
		return nil, err
	}

	existingUser, err := s.repo.GetUserByEmail(ctx, email)
	if existingUser != nil {
		member, err := s.ensureOrganizationMember(ctx, org.ID, existingUser.ID)
		if err != nil {
			return nil, err
		}
		return s.generateTokenPair(ctx, existingUser, org, member.Role)
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

	member, err := s.ensureOrganizationMember(ctx, org.ID, user.ID)
	if err != nil {
		return nil, err
	}

	return s.generateTokenPair(ctx, user, org, member.Role)
}

func (s *AuthService) generateTokenPair(ctx context.Context, user *models.User, org *models.Organization, orgRole models.UserRole) (*models.TokenResponse, error) {
	return s.generateTokenPairWithFamily(ctx, user, org, orgRole, uuid.New(), nil)
}

func (s *AuthService) generateTokenPairWithFamily(ctx context.Context, user *models.User, org *models.Organization, orgRole models.UserRole, familyID uuid.UUID, parentJTI *string) (*models.TokenResponse, error) {
	signedToken, accessRecord, err := s.generateAccessToken(ctx, user, org, orgRole, familyID)
	if err != nil {
		return nil, err
	}

	rawRefresh, err := s.generateRefreshToken(ctx, uuid.MustParse(user.ID), uuid.MustParse(org.ID), familyID, parentJTI)
	if err != nil {
		return nil, err
	}

	_ = accessRecord

	return &models.TokenResponse{
		AccessToken:      signedToken,
		TokenType:        "Bearer",
		ExpiresIn:        int64(s.accessTTL.Seconds()),
		RefreshToken:     rawRefresh,
		RefreshExpiresIn: int64(s.refreshTTL.Seconds()),
		User: models.UserInfo{
			ID:       user.ID,
			Email:    user.Email,
			FullName: user.FullName,
			Role:     orgRole,
			Organization: &models.OrganizationInfo{
				ID:   org.ID,
				Name: org.Name,
				Slug: org.Slug,
				Role: orgRole,
			},
		},
		Organization: models.OrganizationInfo{
			ID:   org.ID,
			Name: org.Name,
			Slug: org.Slug,
			Role: orgRole,
		},
	}, nil
}

func (s *AuthService) generateAccessToken(ctx context.Context, user *models.User, org *models.Organization, orgRole models.UserRole, familyID uuid.UUID) (string, *models.Token, error) {
	jti := uuid.New().String()
	now := time.Now().UTC()
	expiresAt := now.Add(s.accessTTL)

	claims := models.TokenClaims{
		UserID:           user.ID,
		Email:            user.Email,
		FullName:         user.FullName,
		Role:             orgRole,
		OrganizationID:   org.ID,
		OrganizationSlug: org.Slug,
		OrganizationRole: orgRole,
		JTI:              jti,
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
		return "", nil, fmt.Errorf("failed to sign token: %w", err)
	}

	fid := familyID
	tokenRecord := &models.Token{
		ID:             uuid.New(),
		UserID:         uuid.MustParse(user.ID),
		OrganizationID: uuid.MustParse(org.ID),
		JTI:            jti,
		TokenHash:      hashToken(signedToken),
		Type:           "access",
		FamilyID:       &fid,
		IsRevoked:      false,
		CreatedAt:      now,
		ExpiresAt:      expiresAt,
	}

	if err := s.repo.CreateToken(ctx, tokenRecord); err != nil {
		return "", nil, fmt.Errorf("failed to store access token: %w", err)
	}

	return signedToken, tokenRecord, nil
}

func (s *AuthService) generateRefreshToken(ctx context.Context, userID uuid.UUID, orgID uuid.UUID, familyID uuid.UUID, parentJTI *string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate refresh token: %w", err)
	}
	rawToken := base64.RawURLEncoding.EncodeToString(raw)

	now := time.Now().UTC()
	fid := familyID
	tokenRecord := &models.Token{
		ID:             uuid.New(),
		UserID:         userID,
		OrganizationID: orgID,
		JTI:            uuid.New().String(),
		TokenHash:      hashToken(rawToken),
		Type:           "refresh",
		FamilyID:       &fid,
		ParentJTI:      parentJTI,
		IsRevoked:      false,
		CreatedAt:      now,
		ExpiresAt:      now.Add(s.refreshTTL),
	}

	if err := s.repo.CreateToken(ctx, tokenRecord); err != nil {
		return "", fmt.Errorf("failed to store refresh token: %w", err)
	}

	return rawToken, nil
}

func (s *AuthService) RefreshTokens(ctx context.Context, rawRefreshToken string) (*models.TokenResponse, error) {
	tokenHash := hashToken(rawRefreshToken)

	tokenRecord, err := s.repo.GetTokenByHash(ctx, tokenHash)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve token: %w", err)
	}
	if tokenRecord == nil {
		return nil, ErrInvalidToken
	}
	if tokenRecord.Type != "refresh" {
		return nil, ErrInvalidToken
	}

	if tokenRecord.IsRevoked {
		if tokenRecord.FamilyID != nil {
			_ = s.repo.RevokeTokenFamily(ctx, *tokenRecord.FamilyID, "reuse_detected")
		}
		return nil, ErrTokenRevoked
	}

	if time.Now().UTC().After(tokenRecord.ExpiresAt.UTC()) {
		return nil, ErrTokenExpired
	}

	if err := s.repo.RevokeToken(ctx, tokenRecord.JTI, "rotated"); err != nil {
		return nil, fmt.Errorf("failed to rotate token: %w", err)
	}

	user, err := s.repo.GetUser(ctx, tokenRecord.UserID.String())
	if err != nil || user == nil {
		return nil, fmt.Errorf("failed to retrieve user: %w", err)
	}

	var familyID uuid.UUID
	if tokenRecord.FamilyID != nil {
		familyID = *tokenRecord.FamilyID
	} else {
		familyID = uuid.New()
	}

	org, err := s.repo.GetOrganization(ctx, tokenRecord.OrganizationID.String())
	if err != nil || org == nil {
		return nil, fmt.Errorf("failed to retrieve organization: %w", err)
	}
	member, err := s.repo.GetOrganizationMember(ctx, org.ID, user.ID)
	if err != nil || member == nil {
		return nil, fmt.Errorf("failed to retrieve organization membership: %w", err)
	}

	return s.generateTokenPairWithFamily(ctx, user, org, member.Role, familyID, &tokenRecord.JTI)
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
		if tokenRecord.Type != "access" {
			return nil, fmt.Errorf("token type not allowed as bearer credential")
		}

		// Use UTC explicitly on both sides to avoid timezone issues
		nowUTC := time.Now().UTC()
		expiresAtUTC := tokenRecord.ExpiresAt.UTC()

		if tokenRecord.IsRevoked || nowUTC.After(expiresAtUTC) {
			return nil, fmt.Errorf("token is revoked or expired (is_revoked: %v, expired: %v, now: %v, expires_at: %v)",
				tokenRecord.IsRevoked, nowUTC.After(expiresAtUTC), nowUTC, expiresAtUTC)
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

func (s *AuthService) RevokeRefreshTokenFamily(ctx context.Context, rawRefreshToken string) error {
	tokenHash := hashToken(rawRefreshToken)
	tokenRecord, err := s.repo.GetTokenByHash(ctx, tokenHash)
	if err != nil || tokenRecord == nil {
		return nil // best-effort: if not found, nothing to revoke
	}
	if tokenRecord.FamilyID == nil {
		return s.repo.RevokeToken(ctx, tokenRecord.JTI, "logged out")
	}
	return s.repo.RevokeTokenFamily(ctx, *tokenRecord.FamilyID, "logged out")
}

func (s *AuthService) GetOrganizationBySlug(ctx context.Context, slug string) (*models.Organization, error) {
	org, err := s.repo.GetOrganizationBySlug(ctx, normalizeSlug(slug))
	if err != nil {
		return nil, err
	}
	if org == nil || !org.IsActive {
		return nil, fmt.Errorf("invalid organization")
	}
	return org, nil
}

type refreshOrganization struct {
	Organization *models.Organization
	Role         models.UserRole
}

func (s *AuthService) resolveLoginOrganization(ctx context.Context, userID, orgSlug string) (*models.Organization, *models.OrganizationMember, *models.OrganizationSelectionResponse, error) {
	if normalizeSlug(orgSlug) != "" {
		org, err := s.repo.GetOrganizationBySlug(ctx, normalizeSlug(orgSlug))
		if err != nil || org == nil || !org.IsActive {
			return nil, nil, nil, fmt.Errorf("invalid organization")
		}

		member, err := s.repo.GetOrganizationMember(ctx, org.ID, userID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to retrieve organization membership: %w", err)
		}
		if member == nil {
			return nil, nil, nil, fmt.Errorf("user does not belong to organization")
		}
		return org, member, nil, nil
	}

	members, err := s.repo.ListOrganizationMembersForUser(ctx, userID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to retrieve organization memberships: %w", err)
	}
	if len(members) == 0 {
		return nil, nil, nil, fmt.Errorf("user does not belong to any organization")
	}
	if len(members) > 1 {
		selection, err := s.buildOrganizationSelectionResponse(userID, members)
		if err != nil {
			return nil, nil, nil, err
		}
		return nil, nil, selection, nil
	}
	member := members[0]
	if member.Organization.ID == "" || !member.Organization.IsActive {
		return nil, nil, nil, fmt.Errorf("invalid organization")
	}
	return &member.Organization, &member, nil, nil
}

func (s *AuthService) buildOrganizationSelectionResponse(userID string, members []models.OrganizationMember) (*models.OrganizationSelectionResponse, error) {
	orgs := make([]models.OrganizationInfo, 0, len(members))
	orgIDs := make([]string, 0, len(members))
	for _, member := range members {
		if member.Organization.ID == "" || !member.Organization.IsActive {
			continue
		}
		orgIDs = append(orgIDs, member.Organization.ID)
		orgs = append(orgs, models.OrganizationInfo{
			ID:   member.Organization.ID,
			Name: member.Organization.Name,
			Slug: member.Organization.Slug,
			Role: member.Role,
		})
	}
	if len(orgs) == 0 {
		return nil, fmt.Errorf("user does not belong to any active organization")
	}

	ticket, err := s.generateLoginSelectionTicket(userID, orgIDs)
	if err != nil {
		return nil, err
	}

	return &models.OrganizationSelectionResponse{
		RequiresOrganizationSelection: true,
		LoginTicket:                   ticket,
		Organizations:                 orgs,
	}, nil
}

func (s *AuthService) generateLoginSelectionTicket(userID string, organizationIDs []string) (string, error) {
	now := time.Now().UTC()
	claims := loginSelectionClaims{
		UserID:          userID,
		OrganizationIDs: organizationIDs,
		Purpose:         "organization_selection",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.jwtIssuer,
			Audience:  []string{s.jwtAudience},
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        uuid.New().String(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return "", fmt.Errorf("failed to sign login ticket: %w", err)
	}
	return signedToken, nil
}

func (s *AuthService) verifyLoginSelectionTicket(ticket string) (*loginSelectionClaims, error) {
	claims := &loginSelectionClaims{}
	token, err := jwt.ParseWithClaims(ticket, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid login ticket: %w", err)
	}
	if claims.Purpose != "organization_selection" || claims.UserID == "" || len(claims.OrganizationIDs) == 0 {
		return nil, fmt.Errorf("invalid login ticket")
	}
	return claims, nil
}

func (s *AuthService) getOrCreateOrganization(ctx context.Context, name, slug string) (*models.Organization, error) {
	normalized, err := normalizeOrDeriveSlug(name, slug)
	if err != nil {
		return nil, err
	}
	orgName := strings.TrimSpace(name)
	if normalized == "" {
		return nil, fmt.Errorf("organization slug is required")
	}
	if orgName == "" {
		orgName = normalized
	}

	org, err := s.repo.GetOrganizationBySlug(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve organization: %w", err)
	}
	if org != nil {
		return org, nil
	}

	org = &models.Organization{
		ID:        uuid.New().String(),
		Name:      orgName,
		Slug:      normalized,
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.repo.CreateOrganization(ctx, org); err != nil {
		return nil, fmt.Errorf("failed to create organization: %w", err)
	}

	cfg := &models.OrganizationConfig{
		OrganizationID: org.ID,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	cfg.ApplyDefaults()
	_ = s.repo.UpsertOrganizationConfig(ctx, cfg)

	return org, nil
}

func (s *AuthService) ensureOrganizationMember(ctx context.Context, orgID, userID string) (*models.OrganizationMember, error) {
	existing, err := s.repo.GetOrganizationMember(ctx, orgID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve organization membership: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	count, err := s.repo.CountOrganizationMembers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	role := models.RoleDeveloper
	if count == 0 {
		role = models.RoleAdmin
	}

	member := &models.OrganizationMember{
		OrganizationID: orgID,
		UserID:         userID,
		Role:           role,
		IsActive:       true,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	if err := s.repo.CreateOrganizationMember(ctx, member); err != nil {
		return nil, err
	}
	return member, nil
}

func normalizeSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

func normalizeOrDeriveSlug(name, slug string) (string, error) {
	source := slug
	if strings.TrimSpace(source) == "" {
		source = name
	}
	normalized := slugify(source)
	if normalized == "" {
		return "", fmt.Errorf("organization slug is required")
	}
	return normalized, nil
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastHyphen := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		case unicode.IsSpace(r), r == '-', r == '_':
			if b.Len() > 0 && !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// ============================================
// OAUTH
// ============================================

func (s *AuthService) RegisterProvider(provider oauth.OAuthProvider) {
	s.providers[provider.Name()] = provider
}

func (s *AuthService) GetOAuthAuthURL(ctx context.Context, orgSlug, provider, state string) string {
	var (
		p   oauth.OAuthProvider
		err error
	)
	if normalizeSlug(orgSlug) == "" {
		p = s.providers[provider]
		if p == nil {
			return ""
		}
	} else {
		p, err = s.oauthProviderForOrganization(ctx, orgSlug, provider)
	}
	if err != nil {
		return ""
	}
	return p.GetAuthURL(state)
}

type OAuthStateInput struct {
	OrganizationID   string
	OrganizationName string
	OrganizationSlug string
}

type oauthStatePayload struct {
	Nonce            string `json:"nonce"`
	OrganizationID   string `json:"organization_id,omitempty"`
	OrganizationName string `json:"organization_name,omitempty"`
	OrganizationSlug string `json:"organization_slug,omitempty"`
	Exp              int64  `json:"exp"`
}

func (s *AuthService) GenerateOAuthState(input OAuthStateInput) (string, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	payload := oauthStatePayload{
		Nonce:            base64.RawURLEncoding.EncodeToString(nonce),
		OrganizationID:   input.OrganizationID,
		OrganizationName: input.OrganizationName,
		OrganizationSlug: input.OrganizationSlug,
		Exp:              time.Now().Add(10 * time.Minute).Unix(),
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	h := hmac.New(sha256.New, []byte(s.jwtSecret))
	h.Write([]byte(payloadB64))
	signature := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	return payloadB64 + "." + signature, nil
}

func (s *AuthService) VerifyOAuthState(state string) (*oauthStatePayload, error) {
	parts := len(state)
	if parts < 100 {
		return nil, fmt.Errorf("invalid state format")
	}

	idx := len(state) - len(state)
	for i := len(state) - 1; i >= 0; i-- {
		if state[i] == '.' {
			idx = i
			break
		}
	}

	if idx == 0 {
		return nil, fmt.Errorf("invalid state format: no signature")
	}

	payloadB64 := state[:idx]
	signatureB64 := state[idx+1:]

	h := hmac.New(sha256.New, []byte(s.jwtSecret))
	h.Write([]byte(payloadB64))
	expectedSignature := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(signatureB64), []byte(expectedSignature)) {
		return nil, fmt.Errorf("invalid state signature")
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode payload: %w", err)
	}

	var payload oauthStatePayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	if payload.Exp == 0 {
		return nil, fmt.Errorf("invalid exp in payload")
	}
	if time.Now().Unix() > payload.Exp {
		return nil, fmt.Errorf("state has expired")
	}

	return &payload, nil
}

func (s *AuthService) LoginWithOAuth(ctx context.Context, orgSlug, provider, code, state string) (*models.TokenResponse, error) {
	statePayload, err := s.VerifyOAuthState(state)
	if err != nil {
		return nil, fmt.Errorf("invalid oauth state: %w", err)
	}

	org, oauthProvider, err := s.resolveOAuthOrganizationAndProvider(ctx, orgSlug, provider, statePayload)
	if err != nil {
		return nil, err
	}

	userInfo, err := oauthProvider.ExchangeCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange oauth code: %w", err)
	}

	conn, err := s.repo.GetOAuthConnection(ctx, provider, userInfo.ProviderUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get oauth connection: %w", err)
	}

	var user *models.User
	if conn != nil {
		user, err = s.repo.GetUser(ctx, conn.UserID.String())
		if err != nil {
			return nil, fmt.Errorf("failed to get user: %w", err)
		}
		if user == nil {
			return nil, fmt.Errorf("oauth connection points to non-existent user")
		}
	} else {
		user, err = s.repo.GetUserByEmail(ctx, userInfo.Email)
		if err != nil {
			return nil, fmt.Errorf("failed to check email: %w", err)
		}

		if user == nil {
			user = &models.User{
				ID:        uuid.New().String(),
				Email:     userInfo.Email,
				FullName:  userInfo.Name,
				Role:      models.RoleDeveloper,
				IsActive:  true,
				Avatar:    "",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			if err := s.repo.CreateUser(ctx, user); err != nil {
				return nil, fmt.Errorf("failed to create user: %w", err)
			}
		}

		parsedUserID, err := uuid.Parse(user.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse user id: %w", err)
		}

		conn = &models.OAuthConnection{
			UserID:         parsedUserID,
			Provider:       provider,
			ProviderUserID: userInfo.ProviderUserID,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
	}

	if err := s.repo.UpsertOAuthConnection(ctx, conn); err != nil {
		return nil, fmt.Errorf("failed to upsert oauth connection: %w", err)
	}

	if !user.IsActive {
		return nil, fmt.Errorf("account is inactive")
	}

	member, err := s.ensureOrganizationMember(ctx, org.ID, user.ID)
	if err != nil {
		return nil, err
	}

	return s.generateTokenPair(ctx, user, org, member.Role)
}

func (s *AuthService) resolveOAuthOrganizationAndProvider(ctx context.Context, orgSlug, provider string, statePayload *oauthStatePayload) (*models.Organization, oauth.OAuthProvider, error) {
	if normalizeSlug(orgSlug) != "" {
		org, err := s.repo.GetOrganizationBySlug(ctx, normalizeSlug(orgSlug))
		if err != nil || org == nil || !org.IsActive {
			return nil, nil, fmt.Errorf("invalid organization")
		}
		if statePayload.OrganizationID == "" || statePayload.OrganizationID != org.ID {
			return nil, nil, fmt.Errorf("invalid organization in state")
		}
		oauthProvider, err := s.oauthProviderForOrganization(ctx, org.Slug, provider)
		if err != nil {
			return nil, nil, err
		}
		return org, oauthProvider, nil
	}

	if statePayload.OrganizationID != "" {
		org, err := s.repo.GetOrganization(ctx, statePayload.OrganizationID)
		if err != nil || org == nil || !org.IsActive {
			return nil, nil, fmt.Errorf("invalid organization")
		}
		oauthProvider, err := s.oauthProviderForOrganization(ctx, org.Slug, provider)
		if err != nil {
			return nil, nil, err
		}
		return org, oauthProvider, nil
	}

	oauthProvider := s.providers[provider]
	if oauthProvider == nil {
		return nil, nil, fmt.Errorf("unknown oauth provider: %s", provider)
	}
	org, err := s.getOrCreateOrganization(ctx, statePayload.OrganizationName, statePayload.OrganizationSlug)
	if err != nil {
		return nil, nil, err
	}
	return org, oauthProvider, nil
}

func (s *AuthService) oauthProviderForOrganization(ctx context.Context, orgSlug, provider string) (oauth.OAuthProvider, error) {
	org, err := s.repo.GetOrganizationBySlug(ctx, normalizeSlug(orgSlug))
	if err != nil || org == nil {
		return nil, fmt.Errorf("invalid organization")
	}
	cfg, err := s.repo.GetOrganizationConfig(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("organization oauth is not configured")
	}

	switch provider {
	case "github":
		if cfg.GitHubClientID == "" || cfg.GitHubClientSecret == "" {
			return nil, fmt.Errorf("github oauth is not configured")
		}
		return oauth.NewGitHubProvider(cfg.GitHubClientID, cfg.GitHubClientSecret, cfg.GitHubCallbackURL), nil
	case "gitlab":
		if cfg.GitLabClientID == "" || cfg.GitLabClientSecret == "" {
			return nil, fmt.Errorf("gitlab oauth is not configured")
		}
		return oauth.NewGitLabProvider(cfg.GitLabClientID, cfg.GitLabClientSecret, cfg.GitLabCallbackURL), nil
	default:
		return nil, fmt.Errorf("unknown oauth provider: %s", provider)
	}
}
