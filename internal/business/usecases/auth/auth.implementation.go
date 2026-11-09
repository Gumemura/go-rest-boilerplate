package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rest-boilerplate/internal/business/usecases/players"
	"github.com/go-rest-boilerplate/internal/datasources/caches"
	"github.com/go-rest-boilerplate/pkg/jwt"
	"github.com/go-rest-boilerplate/internal/business/domain"	
)

type (
	CreateAnonymousPlayerRequest struct {
		InstallationId string
	}
	CreateAnonymousPlayerResponse struct {
		Player       domain.Player
		AccessToken  string
		RefreshToken string
	}

	RefreshRequest struct {
		RefreshToken string
	}
	RefreshResponse struct {
		Player       domain.Player
		AccessToken  string
		RefreshToken string
	}
)

type Usecase interface {
	CreateAnonymousPlayer(ctx context.Context, req CreateAnonymousPlayerRequest) (CreateAnonymousPlayerResponse, error)
	Refresh(ctx context.Context, req RefreshRequest) (RefreshResponse, error)
}

type Config struct {
}

type usecase struct {
	players    players.Usecase
	jwtService jwt.JWTService
	redisCache caches.RedisCache
	cfg        Config
}

func NewUsecase(playersUC players.Usecase, jwtService jwt.JWTService, redisCache caches.RedisCache, cfg Config) Usecase {
	return &usecase{
		players:    playersUC,
		jwtService: jwtService,
		redisCache: redisCache,
		cfg:        cfg,
	}
}

const dummyBcryptHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

const tokenCutoffTTL = 24 * time.Hour

func (uc *usecase) rememberRefresh(ctx context.Context, pair jwt.TokenPair) error {
	ttl := time.Until(pair.RefreshExpiresAt)
	if ttl <= 0 {
		return fmt.Errorf("refresh token already expired")
	}
	if err := uc.redisCache.Set(ctx, RefreshKey(pair.RefreshJTI), pair.RefreshJTI); err != nil {
		return err
	}
	return uc.redisCache.Expire(ctx, RefreshKey(pair.RefreshJTI), ttl)
}
