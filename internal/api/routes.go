package api

import (
	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/api/middleware"
)

func RegisterRoutes(router *gin.Engine) {
	router.Use(middleware.Logger())
	router.Use(middleware.ErrorHandler())

	router.GET("/health", healthCheck)
}

func healthCheck(c *gin.Context) {
	c.JSON(200, gin.H{
		"status":  "ok",
		"service": "IDP Backend",
	})
}
