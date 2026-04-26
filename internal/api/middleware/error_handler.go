package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		defer func() {
			if err := recover(); err != nil {
				utils.Error(
					"Panic recovered",
					"error", err,
					"path", path,
				)
				c.JSON(500, gin.H{
					"error": "Internal server error",
				})
			}
		}()

		c.Next()

		if len(c.Errors) > 0 {
			lastError := c.Errors.Last()
			utils.Error(
				"Request error",
				"error", lastError.Error(),
				"path", path,
			)
		}
	}
}
