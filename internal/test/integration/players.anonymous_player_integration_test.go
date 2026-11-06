//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-rest-boilerplate/internal/business/domain"
	"github.com/go-rest-boilerplate/internal/http/middlewares"
	"github.com/go-rest-boilerplate/internal/test/mocks"
	"github.com/go-rest-boilerplate/pkg/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

const anonymousPath = "/api/v1/players/anonymous"
const installationIdField = "installation_id"

func bodyFor(installationID string) string {
	return fmt.Sprintf(`{"installation_id":%q}`, installationID)
}

func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(raw, &v), "body: %s", raw)
	return v
}

func TestCreateAnonymousPlayer_Success(t *testing.T) {
	e := mocks.NewEnv(t, nil)
	installationID := uuid.NewString()

	w := e.Post(anonymousPath, bodyFor(installationID))

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	resp := decode[mocks.ApiResponse](t, w.Body.Bytes())
	assert.True(t, resp.Status)
	assert.Equal(t, "creation anonymous player success", resp.Message)
	assert.NotEmpty(t, resp.RequestID)
	assert.Equal(t, resp.RequestID, w.Header().Get("X-Request-ID"))

	data := decode[mocks.CreateData](t, resp.Data)
	_, err := uuid.Parse(data.Player.ID)
	assert.NoError(t, err, "player id deve ser UUID")
	assert.Equal(t, installationID, data.Player.InstallationID)
	assert.Equal(t, domain.RoleUser, data.Player.RoleID)
	assert.WithinDuration(t, time.Now(), data.Player.CreatedAt, 10*time.Second)

	// Token: access token cujo subject é o player recém-criado.
	require.NotEmpty(t, data.AccessToken)
	claims, err := e.JWT.ParseToken(data.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, data.Player.ID, claims.UserID)
	assert.False(t, claims.IsAdmin)
	assert.Empty(t, claims.Email, "player anônimo não tem e-mail")
	assert.Equal(t, jwt.KindAccess, claims.Kind)
	assert.WithinDuration(t, time.Now().Add(time.Hour), claims.ExpiresAt.Time, 10*time.Second)

	// Não deve ser aceito como refresh token.
	_, err = e.JWT.ParseRefreshToken(data.AccessToken)
	assert.ErrorIs(t, err, jwt.ErrWrongTokenKind)

	// Persistência.
	var roleID int
	require.NoError(t, e.DB.GetContext(context.Background(), &roleID,
		`SELECT role_id FROM players WHERE id = $1 AND installation_id = $2`,
		data.Player.ID, installationID))
	assert.Equal(t, domain.RoleUser, roleID)
}

func TestCreateAnonymousPlayer_DuplicateInstallationID(t *testing.T) {
	e := mocks.NewEnv(t, nil)
	installationID := uuid.NewString()

	first := e.Post(anonymousPath, bodyFor(installationID))
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())

	second := e.Post(anonymousPath, bodyFor(installationID))
	require.Equal(t, http.StatusConflict, second.Code, second.Body.String())

	resp := decode[mocks.ApiResponse](t, second.Body.Bytes())
	assert.False(t, resp.Status)
	assert.Equal(t, "installation id already registered", resp.Message)
	assert.Empty(t, resp.Data, "erro não deve vazar token nem player")

	assert.Equal(t, 1, e.CountPlayersBy(t, installationIdField, installationID))
}

func TestCreateAnonymousPlayer_DistinctInstallationsGetDistinctPlayers(t *testing.T) {
	e := mocks.NewEnv(t, nil)

	var ids []string
	for i := 0; i < 3; i++ {
		w := e.Post(anonymousPath, bodyFor(uuid.NewString()))
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		data := decode[mocks.CreateData](t, decode[mocks.ApiResponse](t, w.Body.Bytes()).Data)
		ids = append(ids, data.Player.ID)
	}
	assert.Len(t, map[string]struct{}{ids[0]: {}, ids[1]: {}, ids[2]: {}}, 3)
}

func TestCreateAnonymousPlayer_InvalidRequests(t *testing.T) {
	e := mocks.NewEnv(t, nil)

	t.Run("malformed JSON -> 400", func(t *testing.T) {
		w := e.Post(anonymousPath, `{"installation_id":`)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		resp := decode[mocks.ApiResponse](t, w.Body.Bytes())
		assert.False(t, resp.Status)
		assert.Equal(t, "invalid request body", resp.Message)
	})

	t.Run("body acima de 4KiB -> 400", func(t *testing.T) {
		big := fmt.Sprintf(`{"installation_id":%q}`, strings.Repeat("a", int(middlewares.AuthBodyMaxBytes)+1))
		w := e.Post(anonymousPath, big)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})

	validationCases := []struct {
		name    string
		body    string
		wantTag string
	}{
		{"campo ausente", `{}`, "required"},
		{"string vazia", `{"installation_id":""}`, "required"},
		{"não é UUID", `{"installation_id":"not-a-uuid"}`, "uuid"},
	}
	for _, tc := range validationCases {
		t.Run(tc.name+" -> 422", func(t *testing.T) {
			w := e.Post(anonymousPath, tc.body)
			require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())

			resp := decode[mocks.ApiResponse](t, w.Body.Bytes())
			assert.False(t, resp.Status)
			assert.Equal(t, "validation failed", resp.Message)

			ve := decode[mocks.ValidationData](t, resp.Data)
			require.Len(t, ve.Errors, 1)
			assert.Equal(t, "installationid", ve.Errors[0].Field)
			assert.Equal(t, tc.wantTag, ve.Errors[0].Tag)
		})
	}

	var total int
	require.NoError(t, e.DB.GetContext(context.Background(), &total, `SELECT COUNT(*) FROM players`))
	assert.Zero(t, total, "requisições inválidas não podem criar players")
}

func TestCreateAnonymousPlayer_RateLimited(t *testing.T) {
	// Mesma config do server.go: 5 req/min por IP, burst 5.
	rl := middlewares.NewRateLimiter(rate.Limit(5.0/60.0), 5)
	e := mocks.NewEnv(t, rl)

	for i := 0; i < 5; i++ {
		w := e.Post(anonymousPath, bodyFor(uuid.NewString()))
		require.Equal(t, http.StatusCreated, w.Code, "request %d: %s", i+1, w.Body.String())
	}

	w := e.Post(anonymousPath, bodyFor(uuid.NewString()))
	require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
	assert.Equal(t, "5", w.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "0", w.Header().Get("X-RateLimit-Remaining"))

	resp := decode[mocks.ApiResponse](t, w.Body.Bytes())
	assert.False(t, resp.Status)
	assert.Equal(t, "too many requests, please try again later", resp.Message)
}