package mocks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/go-rest-boilerplate/internal/business/usecases/players"
	playerspostgres "github.com/go-rest-boilerplate/internal/datasources/repositories/postgres/players"
	"github.com/go-rest-boilerplate/internal/http/middlewares"
	"github.com/go-rest-boilerplate/internal/datasources/caches"
	"github.com/go-rest-boilerplate/internal/http/routes"
	"github.com/go-rest-boilerplate/internal/test/testenv"
	"github.com/stretchr/testify/require"
	"github.com/go-rest-boilerplate/pkg/jwt"
	"github.com/jmoiron/sqlx"
	"golang.org/x/time/rate"
)

type ApiResponse struct {
	Status    bool            `json:"status"`
	Message   string          `json:"message"`
	Data      json.RawMessage `json:"data"`
	RequestID string          `json:"request_id"`
}

type CreateData struct {
	Player struct {
		ID             string    `json:"id"`
		RoleID         int       `json:"role_id"`
		InstallationID string    `json:"installation_id"`
		CreatedAt      time.Time `json:"created_at"`
	} `json:"player"`
	AccessToken string `json:"accessToken"`
}

type ValidationData struct {
	Errors []struct {
		Field   string `json:"field"`
		Tag     string `json:"tag"`
		Message string `json:"message"`
	} `json:"errors"`
}

type testEnv struct {
	router *gin.Engine
	DB     *sqlx.DB
	JWT    jwt.JWTService
}

func NewEnv(t *testing.T, rl *middlewares.RateLimiter) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := testenv.StartPostgres(t)

	jwtSvc := jwt.NewJWTServiceWithRefresh("integration-test-secret-thirty-two-chars!", "integration-test", 1, 7)
	ristrettoCache, _ := caches.NewRistrettoCache()

	uc := players.NewUsecase(playerspostgres.NewPlayersRepository(db), jwtSvc, ristrettoCache, players.Config{BcryptCost: 4})

	if rl == nil {
		rl = middlewares.NewRateLimiter(rate.Limit(1000), 1000)
	}
	t.Cleanup(rl.Stop)

	router := gin.New()
	router.Use(middlewares.RequestIDMiddleware())
	api := router.Group("api")
	authMw := middlewares.NewAuthMiddleware(jwtSvc, nil, false)
	routes.NewPlayersRoute(api, uc, authMw, rl).Routes()

	return &testEnv{router: router, DB: db, JWT: jwtSvc}
}

func (e *testEnv) Post(path string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func (e *testEnv) Get(path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	if token != "" {
		req.Header.Set("Authorization", "Bearer " + token)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func (e *testEnv) CountPlayersBy(
    t *testing.T,
    field string,
    value string,
) int {
    t.Helper()

    allowedFields := map[string]struct{}{
        "id":              {},
        "installation_id": {},
        "role_id":         {},
    }

    _, ok := allowedFields[field]
    require.True(t, ok, "unsupported field: %s", field)

    query := fmt.Sprintf(
        "SELECT COUNT(*) FROM players WHERE %s = $1",
        field,
    )

    var count int

    require.NoError(t, e.DB.GetContext(
        context.Background(),
        &count,
        query,
        value,
    ))

    return count
}