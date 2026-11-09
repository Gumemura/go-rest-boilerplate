package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rest-boilerplate/internal/apperror"
	"github.com/go-rest-boilerplate/internal/business/usecases/players"
	"github.com/go-rest-boilerplate/pkg/logger"
)

// Refresh verifies the supplied refresh token, mints a new
// access+refresh pair, and revokes the old jti in Redis. Replay of
// an already-used refresh token fails because rememberRefresh → Del
// makes the old jti unknown.
func (uc *usecase) Refresh(ctx context.Context, req RefreshRequest) (resp RefreshResponse, err error) {
	const (
		usecaseName = "auth-usecase"
		funcName    = "Refresh"
		fileName    = "auth.refresh.go"
	)
	startTime := time.Now()
	refreshToken := req.RefreshToken

	logger.InfoWithContext(ctx, fmt.Sprintf("Upper %s", funcName), logger.Fields{
		"usecase": usecaseName,
		"method":  funcName,
		"file":    fileName,
		"request": logger.Fields{
			"has_refresh_token": refreshToken != "",
		},
	})

	defer func() {
		duration := time.Since(startTime)
		fields := logger.Fields{
			"usecase":  usecaseName,
			"method":   funcName,
			"file":     fileName,
			"duration": duration.Milliseconds(),
		}
		if err == nil {
			fields["response"] = logger.Fields{"player_id": resp.Player.Id}
		}
		logger.InfoWithContext(ctx, fmt.Sprintf("Lower %s", funcName), fields)
	}()

	claims, parseErr := uc.jwtService.ParseRefreshToken(refreshToken)
	if parseErr != nil {
		err = apperror.Unauthorized("invalid refresh token")
		logger.ErrorWithContext(ctx, "Refresh failed: invalid token", logger.Fields{
			"usecase": usecaseName,
			"method":  funcName,
			"file":    fileName,
			"step":    "parse_refresh_token",
			"error":   parseErr.Error(),
		})
		return RefreshResponse{}, err
	}

	// Verify the jti is still live server-side; logout / previous
	// rotation would have removed it.
	if _, getErr := uc.redisCache.Get(ctx, RefreshKey(claims.ID)); getErr != nil {
		err = apperror.Unauthorized("refresh token has been revoked")
		logger.ErrorWithContext(ctx, "Refresh failed: token revoked", logger.Fields{
			"usecase": usecaseName,
			"method":  funcName,
			"file":    fileName,
			"step":    "check_jti_alive",
			"error":   getErr.Error(),
			"jti":     claims.UserID,
		})
		return RefreshResponse{}, err
	}

	// Fresh identity lookup so revoked / deactivated accounts stop
	// getting new access tokens even while their refresh is live.
	lookupResp, lookupErr := uc.players.GetByID(ctx, players.GetByIDRequest{Id: claims.UserID})
	if lookupErr != nil {
		err = apperror.Unauthorized("player no longer exists")
		logger.ErrorWithContext(ctx, "Refresh failed: player lookup error", logger.Fields{
			"usecase": usecaseName,
			"method":  funcName,
			"file":    fileName,
			"step":    "get_player_by_id",
			"error":   lookupErr.Error(),
			"email":   claims.Email,
		})
		return RefreshResponse{}, err
	}
	player := lookupResp.Player

	// Reject tokens issued before the most recent password change —
	// rotating the password must close pre-existing sessions.
	if cutoff := player.TokensRevokedBefore(); !cutoff.IsZero() &&
		claims.IssuedAt != nil && claims.IssuedAt.Before(cutoff) {
		err = apperror.Unauthorized("refresh token has been revoked")
		logger.ErrorWithContext(ctx, "Refresh failed: token issued before password rotation", logger.Fields{
			"usecase": usecaseName,
			"method":  funcName,
			"file":    fileName,
			"step":    "check_revocation_cutoff",
			"error":   err.Error(),
			"player_id": player.Id,
		})
		return RefreshResponse{}, err
	}

	pair, mintErr := uc.jwtService.GenerateTokenPair(player.Id, player.IsAdmin(), player.Email)
	if mintErr != nil {
		err = apperror.InternalCause(fmt.Errorf("generate token: %w", mintErr))
		logger.ErrorWithContext(ctx, "Refresh failed: token generation error", logger.Fields{
			"usecase": usecaseName,
			"method":  funcName,
			"file":    fileName,
			"step":    "generate_token_pair",
			"error":   mintErr.Error(),
			"player_id": player.Id,
		})
		return RefreshResponse{}, err
	}

	// Rotate: remove the old jti, record the new one. Do this after
	// the new pair is minted so a mint failure doesn't leave the
	// player with no valid refresh token at all.
	if persistErr := uc.rememberRefresh(ctx, pair); persistErr != nil {
		err = apperror.InternalCause(fmt.Errorf("persist refresh: %w", persistErr))
		logger.ErrorWithContext(ctx, "Refresh failed: persist refresh error", logger.Fields{
			"usecase": usecaseName,
			"method":  funcName,
			"file":    fileName,
			"step":    "persist_refresh",
			"error":   persistErr.Error(),
			"player_id": player.Id,
		})
		return RefreshResponse{}, err
	}
	_ = uc.redisCache.Del(ctx, RefreshKey(claims.UserID))

	resp = RefreshResponse{
		Player:       player,
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
	}
	return resp, nil
}
