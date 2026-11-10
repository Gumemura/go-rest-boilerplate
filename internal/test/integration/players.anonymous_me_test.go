//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/go-rest-boilerplate/internal/business/domain"
	"github.com/go-rest-boilerplate/internal/test/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAnonymousPlayerMe(t *testing.T) {
	e := mocks.NewEnv(t, nil)
	installationID := uuid.NewString()

	created := e.Post(mocks.AnonymousPath, mocks.BodyFor(mocks.InstallationIdField, installationID))
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	createData := mocks.Decode[mocks.CreateData](t, mocks.Decode[mocks.ApiResponse](t, created.Body.Bytes()).Data)

	t.Run("token válido -> 200 com o mesmo player", func(t *testing.T) {
		w := e.Get(mocks.AnonymousMePath, createData.AccessToken)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		resp := mocks.Decode[mocks.ApiResponse](t, w.Body.Bytes())
		assert.True(t, resp.Status)
		data := mocks.Decode[mocks.CreateData](t, resp.Data)
		assert.Equal(t, createData.Player.ID, data.Player.ID)
		assert.Equal(t, installationID, data.Player.InstallationID)
		assert.Equal(t, domain.RoleAnonymousPlayer, data.Player.RoleID)
	})

	t.Run("sem token -> 401", func(t *testing.T) {
		w := e.Get(mocks.AnonymousMePath, "")
		assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	})

	t.Run("token inválido -> 401", func(t *testing.T) {
		w := e.Get(mocks.AnonymousMePath, "not-a-jwt")
		assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	})

	t.Run("token válido de player inexistente -> 404", func(t *testing.T) {
		tok, err := e.JWT.GenerateToken(uuid.NewString(), false, "")
		require.NoError(t, err)
		w := e.Get(mocks.AnonymousMePath, tok)
		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})
}