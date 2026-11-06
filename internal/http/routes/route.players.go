package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/go-rest-boilerplate/internal/business/usecases/players"
	playershandler "github.com/go-rest-boilerplate/internal/http/handlers/v1/players"
	"github.com/go-rest-boilerplate/internal/http/middlewares"
)

type playersRoute struct {
	handler           playershandler.Handler
	router            *gin.RouterGroup
	rateLimiter       *middlewares.RateLimiter
	authMiddleware    gin.HandlerFunc
}

func NewPlayersRoute(router *gin.RouterGroup, playersUC players.Usecase, authMiddleware gin.HandlerFunc, rateLimiter *middlewares.RateLimiter) *playersRoute {
	return &playersRoute{
		handler:        playershandler.NewHandler(playersUC),
		router:         router,
		rateLimiter:    rateLimiter,
		authMiddleware: authMiddleware,
	}
}

func (r *playersRoute) Routes() {
	v1 := r.router.Group("/v1")
	playersGrp := v1.Group("/players")
	playersGrp.Use(middlewares.BodySizeLimitMiddleware(middlewares.AuthBodyMaxBytes))
	playersGrp.Use(r.rateLimiter.Middleware())

	playersGrp.POST("/anonymous", r.handler.CreateAnonymousPlayer)
	playersGrp.GET("/anonymous/me", r.authMiddleware, r.handler.GetAnonymousPlayerMe)
}
