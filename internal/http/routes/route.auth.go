package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/go-rest-boilerplate/internal/business/usecases/auth"
	authhandler "github.com/go-rest-boilerplate/internal/http/handlers/v1/auth"
	"github.com/go-rest-boilerplate/internal/http/middlewares"
)

type authRoute struct {
	handler           authhandler.Handler
	router            *gin.RouterGroup
	rateLimiter       *middlewares.RateLimiter
	authMiddleware    gin.HandlerFunc
}

func NewAuthRoute(router *gin.RouterGroup, authUC auth.Usecase, authMiddleware gin.HandlerFunc, rateLimiter *middlewares.RateLimiter) *authRoute {
	return &authRoute{
		handler:        authhandler.NewHandler(authUC),
		router:         router,
		rateLimiter:    rateLimiter,
		authMiddleware: authMiddleware,
	}
}

func (r *authRoute) Routes() {
	v1 := r.router.Group("/v1")
	authGrp := v1.Group("/auth")
	authGrp.Use(middlewares.BodySizeLimitMiddleware(middlewares.AuthBodyMaxBytes))
	authGrp.Use(r.rateLimiter.Middleware())

	authGrp.POST("/anonymous", r.handler.CreateAnonymousPlayer)
	authGrp.POST("/refresh", r.handler.Refresh)
}
