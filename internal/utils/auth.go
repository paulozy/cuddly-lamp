package utils

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

const (
	AuthorizationHeader = "Authorization"
	BearerScheme        = "Bearer"
	ContextKeyUser      = "user"
	ContextKeyOrganization = "organization"
	ContextKeyClaims    = "claims"
)

func ExtractToken(c *gin.Context) (string, error) {
	authHeader := c.GetHeader(AuthorizationHeader)
	if authHeader == "" {
		return "", fmt.Errorf("missing authorization header")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != BearerScheme {
		return "", fmt.Errorf("invalid authorization header format")
	}

	return parts[1], nil
}

func HasPermission(userRole, requiredRole models.UserRole) bool {
	roleHierarchy := map[models.UserRole]int{
		models.RoleViewer:     1,
		models.RoleDeveloper:  2,
		models.RoleMaintainer: 3,
		models.RoleAdmin:      4,
	}
	return roleHierarchy[userRole] >= roleHierarchy[requiredRole]
}

func GetUserIDFromContext(c *gin.Context) (string, error) {
	userID, ok := c.Request.Context().Value(ContextKeyUser).(string)
	if !ok {
		return "", errors.New("user not found in context")
	}
	return userID, nil
}

func GetOrganizationIDFromContext(c *gin.Context) (string, error) {
	orgID, ok := c.Request.Context().Value(ContextKeyOrganization).(string)
	if !ok || orgID == "" {
		return "", errors.New("organization not found in context")
	}
	return orgID, nil
}

func GetClaimsFromContext(c *gin.Context) (*models.TokenClaims, error) {
	claims, ok := c.Request.Context().Value(ContextKeyClaims).(*models.TokenClaims)
	if !ok {
		return nil, errors.New("claims not found in context")
	}
	return claims, nil
}

func ExtractTokenFromHeader(c *gin.Context) (string, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return "", ErrorMissingAuthHeader
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return "", ErrorInvalidAuthHeader
	}

	return parts[1], nil
}

var (
	ErrorMissingAuthHeader = fmt.Errorf("missing authorization header")
	ErrorInvalidAuthHeader = fmt.Errorf("invalid authorization header")
)
