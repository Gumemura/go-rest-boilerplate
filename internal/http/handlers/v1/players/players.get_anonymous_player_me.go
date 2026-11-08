package players

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-rest-boilerplate/internal/business/usecases/players"
	httpauth "github.com/go-rest-boilerplate/internal/http/auth"
	"github.com/go-rest-boilerplate/internal/http/datatransfers/responses"
	v1 "github.com/go-rest-boilerplate/internal/http/handlers/v1"
	"github.com/go-rest-boilerplate/pkg/logger"
)

// @Summary      Returns the current anonymous player
// @Description  Reads the player id from the access token issued by /players/anonymous and returns the associated player.
// @Tags         players
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} v1.BaseResponse
// @Failure      401 {object} v1.BaseResponse
// @Failure      404 {object} v1.BaseResponse
// @Router       /players/anonymous/me [get]
func (h Handler) GetAnonymousPlayerMe(ctx *gin.Context) {
	const (
		controllerName = "players-handler"
		funcName       = "GetAnonymousPlayerMe"
		fileName       = "players.get_anonymous_player_me.go"
	)

	current, err := httpauth.CurrentUserFromContext(ctx)
	if err != nil {
		if errors.Is(err, httpauth.ErrNotAuthenticated) {
			v1.NewErrorResponse(ctx, http.StatusUnauthorized, "unauthorized")
			return
		}
		v1.RespondWithError(ctx, err)
		return
	}

	resp, err := h.usecase.GetByID(ctx.Request.Context(), players.GetByIDRequest{Id: current.ID})
	if err != nil {
		logger.WarnWithContext(ctx.Request.Context(), "GetAnonymousPlayerMe failed in controller", logger.Fields{
			"controller": controllerName,
			"method":     funcName,
			"file":       fileName,
			"player_id":  current.ID,
			"error":      err.Error(),
		})
		v1.RespondWithError(ctx, err)
		return
	}

	v1.NewSuccessResponse(ctx, http.StatusOK, "anonymous player retrieved", map[string]interface{}{
		"player": responses.PlayerFromV1Domain(resp.Player),
	})
}