package players

import (
	"fmt"
	"context"

	"github.com/go-rest-boilerplate/internal/apperror"
	"github.com/go-rest-boilerplate/internal/business/domain"

	"github.com/go-rest-boilerplate/pkg/logger"
	"github.com/go-rest-boilerplate/pkg/observability"
	"github.com/google/uuid"
)

func (uc *usecase) GetByID(ctx context.Context, req GetByIDRequest) (GetByIDResponse, error) {
	const (
		usecaseName = "players"
		funcName    = "GetByID"
		fileName    = "players.get_by_id.go"
	)
	
	if _, err := uuid.Parse(req.Id); err != nil {
		return GetByIDResponse{}, apperror.Unauthorized("invalid player id")
	}

	cacheKey := fmt.Sprintf("player/%s", req.Id)
	if val := uc.ristrettoCache.Get(cacheKey); val != nil {
		if cached, ok := val.(domain.Player); ok {
			observability.ObserveCacheOp("ristretto", "get", "hit")
			return GetByIDResponse{Player: cached}, nil
		}
		observability.ObserveCacheOp("ristretto", "get", "error")
		logger.WarnWithContext(ctx, "Get player by Id: cache type assertion failed", logger.Fields{
			"usecase": usecaseName,
			"method":  funcName,
			"file":    fileName,
			"step":    "ristretto_cache_get",
			"id":      req.Id,
		})
	} else {
		observability.ObserveCacheOp("ristretto", "get", "miss")
	}

	v, sfErr, _ := uc.userByIdGroup.Do(req.Id, func() (any, error) {
		player, repoErr := uc.repo.GetByID(ctx, req.Id)
		if repoErr != nil {
			return domain.Player{}, repoErr
		}
		uc.ristrettoCache.Set(cacheKey, player)
		observability.ObserveCacheOp("ristretto", "set", "ok")
		return player, nil
	})

	if sfErr != nil {
		logger.ErrorWithContext(ctx, "Get player by id failed: repository error", logger.Fields{
			"usecase": usecaseName,
			"method":  funcName,
			"file":    fileName,
			"step":    "repo_get_by_id",
			"error":   sfErr.Error(),
		})
		return GetByIDResponse{}, mapRepoError(sfErr, "get player by id")
	}
	return GetByIDResponse{Player: v.(domain.Player)}, nil
}