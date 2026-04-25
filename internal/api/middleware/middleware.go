package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/seu-org/idp-with-ai-backend/internal/utils"
)

func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		duration := time.Since(start)
		method := c.Request.Method
		path := c.Request.URL.Path
		status := c.Writer.Status()

		utils.Info(
			"HTTP Request",
			"method", method,
			"path", path,
			"status", status,
			"duration_ms", duration.Milliseconds(),
		)
	}
}

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
