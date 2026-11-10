//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-rest-boilerplate/internal/business/domain"
	authuc "github.com/go-rest-boilerplate/internal/business/usecases/auth"
	"github.com/go-rest-boilerplate/internal/http/middlewares"
	"github.com/go-rest-boilerplate/internal/test/mocks"
	"github.com/go-rest-boilerplate/pkg/clock"
	"github.com/go-rest-boilerplate/pkg/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

const refreshPath = "/api/v1/auth/refresh"

func refreshBody(token string) string {
	return fmt.Sprintf(`{"refresh_token":%q}`, token)
}

func postRefresh(e *mocks.TestEnv, token string) *httptest.ResponseRecorder {
	return e.Post(refreshPath, refreshBody(token))
}

// registerPlayer cria um player anônimo via API e devolve o par de tokens.
func registerPlayer(t *testing.T, e *mocks.TestEnv) mocks.CreateData {
	t.Helper()
	w := e.Post(mocks.AnonymousPath, mocks.BodyFor(mocks.InstallationIdField, uuid.NewString()))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return mocks.Decode[mocks.CreateData](t, mocks.Decode[mocks.ApiResponse](t, w.Body.Bytes()).Data)
}

func assertAPIError(t *testing.T, w *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	resp := mocks.Decode[mocks.ApiResponse](t, w.Body.Bytes())
	assert.False(t, resp.Status)
	assert.Equal(t, msg, resp.Message)
	assert.Empty(t, resp.Data, "erro não deve vazar tokens nem player")
}

func TestRefresh_Success(t *testing.T) {
	e := mocks.NewEnv(t, nil)
	created := registerPlayer(t, e)

	w := postRefresh(e, created.RefreshToken)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	resp := mocks.Decode[mocks.ApiResponse](t, w.Body.Bytes())
	assert.True(t, resp.Status)
	assert.Equal(t, "token refreshed", resp.Message)
	assert.NotEmpty(t, resp.RequestID)
	assert.Equal(t, resp.RequestID, w.Header().Get("X-Request-ID"))

	data := mocks.Decode[mocks.CreateData](t, resp.Data)

	// Mesmo player.
	assert.Equal(t, created.Player.ID, data.Player.ID)
	assert.Equal(t, created.Player.InstallationID, data.Player.InstallationID)
	assert.Equal(t, domain.RoleAnonymousPlayer, data.Player.RoleID)

	// Novo par, diferente do anterior.
	require.NotEmpty(t, data.AccessToken)
	require.NotEmpty(t, data.RefreshToken)
	assert.NotEqual(t, created.AccessToken, data.AccessToken)
	assert.NotEqual(t, created.RefreshToken, data.RefreshToken)

	// Claims do novo access token.
	access, err := e.JWT.ParseToken(data.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, created.Player.ID, access.UserID)
	assert.False(t, access.IsAdmin)
	assert.Equal(t, jwt.KindAccess, access.Kind)
	assert.WithinDuration(t, time.Now().Add(time.Hour), access.ExpiresAt.Time, 10*time.Second)

	// Claims do novo refresh token (7 dias).
	refresh, err := e.JWT.ParseRefreshToken(data.RefreshToken)
	require.NoError(t, err)
	assert.Equal(t, created.Player.ID, refresh.UserID)
	assert.Equal(t, jwt.KindRefresh, refresh.Kind)
	assert.WithinDuration(t, time.Now().Add(7*24*time.Hour), refresh.ExpiresAt.Time, 10*time.Second)

	// O novo access token não pode ser aceito como refresh (e vice-versa).
	_, err = e.JWT.ParseRefreshToken(data.AccessToken)
	assert.ErrorIs(t, err, jwt.ErrWrongTokenKind)
	_, err = e.JWT.ParseToken(data.RefreshToken)
	assert.ErrorIs(t, err, jwt.ErrWrongTokenKind)

	// O novo jti foi persistido no Redis.
	_, err = e.RC.Get(context.Background(), authuc.RefreshKey(refresh.ID))
	assert.NoError(t, err, "jti do novo refresh token deve estar vivo no Redis")

	// O novo access token realmente funciona numa rota protegida.
	me := e.Get(mocks.AnonymousMePath, data.AccessToken)
	require.Equal(t, http.StatusOK, me.Code, me.Body.String())
}

func TestRefresh_ChainedRotation(t *testing.T) {
	e := mocks.NewEnv(t, nil)
	current := registerPlayer(t, e).RefreshToken

	// Cada refresh devolve um token que pode ser usado no próximo.
	for i := 1; i <= 3; i++ {
		w := postRefresh(e, current)
		require.Equal(t, http.StatusOK, w.Code, "rotação %d: %s", i, w.Body.String())
		data := mocks.Decode[mocks.CreateData](t, mocks.Decode[mocks.ApiResponse](t, w.Body.Bytes()).Data)
		require.NotEmpty(t, data.RefreshToken)
		require.NotEqual(t, current, data.RefreshToken)
		current = data.RefreshToken
	}
}

// Replay de um refresh token já rotacionado deve ser rejeitado.
// Documentado em auth.refresh.go / swagger: "Replaying an already-rotated token returns 401".
func TestRefresh_ReplayOfRotatedTokenIsRejected(t *testing.T) {
	e := mocks.NewEnv(t, nil)
	created := registerPlayer(t, e)

	oldClaims, err := e.JWT.ParseRefreshToken(created.RefreshToken)
	require.NoError(t, err)

	first := postRefresh(e, created.RefreshToken)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	rotated := mocks.Decode[mocks.CreateData](t, mocks.Decode[mocks.ApiResponse](t, first.Body.Bytes()).Data)

	// O jti antigo precisa ter sido removido do Redis.
	_, err = e.RC.Get(context.Background(), authuc.RefreshKey(oldClaims.ID))
	assert.Error(t, err, "jti antigo deve ser revogado após a rotação")

	// Reutilizar o token antigo -> 401.
	replay := postRefresh(e, created.RefreshToken)
	assertAPIError(t, replay, http.StatusUnauthorized, "refresh token has been revoked")

	// O token novo continua válido.
	again := postRefresh(e, rotated.RefreshToken)
	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
}

func TestRefresh_RevokedJTI(t *testing.T) {
	e := mocks.NewEnv(t, nil)
	created := registerPlayer(t, e)

	claims, err := e.JWT.ParseRefreshToken(created.RefreshToken)
	require.NoError(t, err)

	// Simula o logout / revogação: o jti some do Redis.
	require.NoError(t, e.RC.Del(context.Background(), authuc.RefreshKey(claims.ID)))

	w := postRefresh(e, created.RefreshToken)
	assertAPIError(t, w, http.StatusUnauthorized, "refresh token has been revoked")
}

func TestRefresh_InvalidTokens(t *testing.T) {
	e := mocks.NewEnv(t, nil)
	created := registerPlayer(t, e)

	cases := []struct {
		name  string
		token func(t *testing.T) string
	}{
		{"string aleatória", func(*testing.T) string { return "not-a-jwt" }},
		{"access token no lugar do refresh", func(*testing.T) string { return created.AccessToken }},
		{"assinado com outra chave", func(t *testing.T) string {
			other := jwt.NewJWTServiceWithRefresh("another-secret-thirty-two-chars-long!!", "integration-test", 1, 7)
			pair, err := other.GenerateTokenPair(created.Player.ID, false, "")
			require.NoError(t, err)
			return pair.RefreshToken
		}},
		{"refresh token expirado", func(t *testing.T) string {
			// Emitido há 8 dias com TTL de 7 dias -> expirou há 1 dia.
			past := jwt.WithClock(e.JWT, clock.Frozen(time.Now().Add(-8*24*time.Hour)))
			pair, err := past.GenerateTokenPair(created.Player.ID, false, "")
			require.NoError(t, err)
			return pair.RefreshToken
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name+" -> 401", func(t *testing.T) {
			w := postRefresh(e, tc.token(t))
			assertAPIError(t, w, http.StatusUnauthorized, "invalid refresh token")
		})
	}
}

func TestRefresh_PlayerNoLongerExists(t *testing.T) {
	ctx := context.Background()

	t.Run("player nunca existiu -> 401", func(t *testing.T) {
		e := mocks.NewEnv(t, nil)

		// Token válido e jti vivo no Redis, mas o player não está no banco.
		pair, err := e.JWT.GenerateTokenPair(uuid.NewString(), false, "")
		require.NoError(t, err)
		require.NoError(t, e.RC.Set(ctx, authuc.RefreshKey(pair.RefreshJTI), pair.RefreshJTI))

		w := postRefresh(e, pair.RefreshToken)
		assertAPIError(t, w, http.StatusUnauthorized, "player no longer exists")
	})

	t.Run("player removido (soft delete) -> 401", func(t *testing.T) {
		e := mocks.NewEnv(t, nil)
		created := registerPlayer(t, e)

		_, err := e.DB.ExecContext(ctx, `UPDATE players SET deleted_at = now() WHERE id = $1`, created.Player.ID)
		require.NoError(t, err)

		w := postRefresh(e, created.RefreshToken)
		assertAPIError(t, w, http.StatusUnauthorized, "player no longer exists")
	})
}

func TestRefresh_InvalidRequests(t *testing.T) {
	e := mocks.NewEnv(t, nil)

	t.Run("JSON malformado -> 400", func(t *testing.T) {
		w := e.Post(refreshPath, `{"refresh_token":`)
		assertAPIError(t, w, http.StatusBadRequest, "invalid request body")
	})

	t.Run("body vazio -> 400", func(t *testing.T) {
		w := e.Post(refreshPath, ``)
		assertAPIError(t, w, http.StatusBadRequest, "invalid request body")
	})

	t.Run("body acima de 4KiB -> 400", func(t *testing.T) {
		big := refreshBody(strings.Repeat("a", int(middlewares.AuthBodyMaxBytes)+1))
		w := e.Post(refreshPath, big)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})

	validationCases := []struct {
		name string
		body string
	}{
		{"campo ausente", `{}`},
		{"string vazia", `{"refresh_token":""}`},
	}
	for _, tc := range validationCases {
		t.Run(tc.name+" -> 422", func(t *testing.T) {
			w := e.Post(refreshPath, tc.body)
			require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())

			resp := mocks.Decode[mocks.ApiResponse](t, w.Body.Bytes())
			assert.False(t, resp.Status)
			assert.Equal(t, "validation failed", resp.Message)

			ve := mocks.Decode[mocks.ValidationData](t, resp.Data)
			require.Len(t, ve.Errors, 1)
			assert.Equal(t, "refreshtoken", ve.Errors[0].Field)
			assert.Equal(t, "required", ve.Errors[0].Tag)
		})
	}
}

func TestRefresh_RateLimited(t *testing.T) {
	// Mesma config do server.go: 5 req/min por IP, burst 5.
	rl := middlewares.NewRateLimiter(rate.Limit(5.0/60.0), 5)
	e := mocks.NewEnv(t, rl)

	for i := 0; i < 5; i++ {
		w := postRefresh(e, "not-a-jwt")
		require.Equal(t, http.StatusUnauthorized, w.Code, "request %d: %s", i+1, w.Body.String())
	}

	w := postRefresh(e, "not-a-jwt")
	require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
	assert.Equal(t, "5", w.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "0", w.Header().Get("X-RateLimit-Remaining"))

	resp := mocks.Decode[mocks.ApiResponse](t, w.Body.Bytes())
	assert.False(t, resp.Status)
	assert.Equal(t, "too many requests, please try again later", resp.Message)
}

// setPasswordChangedAt grava players.password_changed_at (em segundos unix)
// diretamente no banco, simulando uma troca/reset de senha.
func setPasswordChangedAt(t *testing.T, e *mocks.TestEnv, playerID string, unix int64) {
	t.Helper()
	res, err := e.DB.ExecContext(context.Background(),
		`UPDATE players SET password_changed_at = to_timestamp($1::double precision) WHERE id = $2`,
		unix, playerID)
	require.NoError(t, err)
	n, err := res.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "player %s deve existir", playerID)
}
 
// seedRefresh registra o jti do par no Redis, como o login/refresh faria.
func seedRefresh(t *testing.T, e *mocks.TestEnv, pair jwt.TokenPair) {
	t.Helper()
	require.NoError(t, e.RC.Set(context.Background(), authuc.RefreshKey(pair.RefreshJTI), pair.RefreshJTI))
}
 
// Refresh tokens emitidos antes da troca de senha devem ser rejeitados;
// tokens emitidos depois continuam válidos.
func TestRefresh_PasswordRotationRevokesOlderTokens(t *testing.T) {
	e := mocks.NewEnv(t, nil)
	created := registerPlayer(t, e)
 
	// Sessão antiga: token emitido há 1h (ainda dentro dos 7 dias de TTL),
	// com jti vivo no Redis -- só o corte por troca de senha pode barrá-lo.
	past := jwt.WithClock(e.JWT, clock.Frozen(time.Now().Add(-time.Hour)))
	old, err := past.GenerateTokenPair(created.Player.ID, false, "")
	require.NoError(t, err)
	seedRefresh(t, e, old)
 
	cutoff := time.Now().Unix()
	setPasswordChangedAt(t, e, created.Player.ID, cutoff)
 
	t.Run("token emitido antes da troca -> 401", func(t *testing.T) {
		w := postRefresh(e, old.RefreshToken)
		assertAPIError(t, w, http.StatusUnauthorized, "refresh token has been revoked")
	})
 
	t.Run("token emitido depois da troca -> 200", func(t *testing.T) {
		fresh, err := e.JWT.GenerateTokenPair(created.Player.ID, false, "")
		require.NoError(t, err)
		seedRefresh(t, e, fresh)
 
		w := postRefresh(e, fresh.RefreshToken)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
 
		data := mocks.Decode[mocks.CreateData](t, mocks.Decode[mocks.ApiResponse](t, w.Body.Bytes()).Data)
		assert.Equal(t, created.Player.ID, data.Player.ID)
		assert.NotEmpty(t, data.AccessToken)
		assert.NotEmpty(t, data.RefreshToken)
	})
}
 
// Fronteira do corte: a comparação é estrita (iat < cutoff), então
// iat == cutoff ainda é aceito e iat < cutoff é rejeitado.
func TestRefresh_CutoffBoundary(t *testing.T) {
	cases := []struct {
		name       string
		offsetSecs int64 // cutoff = iat + offset
		wantStatus int
	}{
		{"cutoff 1s antes do iat -> 200", -1, http.StatusOK},
		{"cutoff igual ao iat -> 200", 0, http.StatusOK},
		{"cutoff 1s depois do iat -> 401", 1, http.StatusUnauthorized},
	}
 
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := mocks.NewEnv(t, nil)
			created := registerPlayer(t, e)
 
			claims, err := e.JWT.ParseRefreshToken(created.RefreshToken)
			require.NoError(t, err)
			require.NotNil(t, claims.IssuedAt)
 
			setPasswordChangedAt(t, e, created.Player.ID, claims.IssuedAt.Unix()+tc.offsetSecs)
 
			w := postRefresh(e, created.RefreshToken)
			if tc.wantStatus == http.StatusOK {
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				return
			}
			assertAPIError(t, w, tc.wantStatus, "refresh token has been revoked")
		})
	}
}
 
// A troca de senha de um player não pode invalidar a sessão de outro.
func TestRefresh_PasswordRotationIsPerPlayer(t *testing.T) {
	e := mocks.NewEnv(t, nil)
	a := registerPlayer(t, e)
	b := registerPlayer(t, e)
 
	claimsA, err := e.JWT.ParseRefreshToken(a.RefreshToken)
	require.NoError(t, err)
	// Corte no futuro próximo para o player A: seu token atual fica revogado.
	setPasswordChangedAt(t, e, a.Player.ID, claimsA.IssuedAt.Unix()+60)
 
	wa := postRefresh(e, a.RefreshToken)
	assertAPIError(t, wa, http.StatusUnauthorized, "refresh token has been revoked")
 
	wb := postRefresh(e, b.RefreshToken)
	require.Equal(t, http.StatusOK, wb.Code, fmt.Sprintf("player B não deve ser afetado: %s", wb.Body.String()))
}