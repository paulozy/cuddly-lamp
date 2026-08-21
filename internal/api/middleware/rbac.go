package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// RequireRole gates a route on the caller's organization role, using the
// hierarchy viewer < developer < maintainer < admin. It must be mounted after
// the auth middleware, which is what puts the claims on the request context.
//
// Authorization in this API has two independent layers and both are required:
// this one answers "is your role allowed to do this at all?", while the
// service layer answers "does this resource belong to your organization?".
// Neither substitutes for the other.
func RequireRole(minRole models.UserRole) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := c.Request.Context().Value(utils.ContextKeyClaims).(*models.TokenClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, models.ErrorResponse{
				Error:            "unauthorized",
				ErrorDescription: "missing or invalid authentication",
			})
			c.Abort()
			return
		}

		if !utils.HasPermission(claims.OrganizationRole, minRole) {
			c.JSON(http.StatusForbidden, models.ErrorResponse{
				Error:            "forbidden",
				ErrorDescription: "your organization role does not allow this action",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
