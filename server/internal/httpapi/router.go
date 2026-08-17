package httpapi

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/idle-zero/arxveil/server/internal/health"
)

func NewRouter(database health.Pinger) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	handler := health.NewHandler(database, 2*time.Second)

	router.GET("/health/live", handler.Live)
	router.GET("/health/ready", handler.Ready)

	return router
}
