package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-rest-boilerplate/internal/apperror"
	"github.com/go-rest-boilerplate/internal/business/domain"
	"github.com/go-rest-boilerplate/internal/datasources/records"
	"github.com/go-rest-boilerplate/pkg/logger"
)

func (r *postgresPlayersRepository) GetByID(ctx context.Context, id string) (domain.Player, error) {
	const (
		repositoryName = "players"
		funcName       = "GetByID"
		queryName      = "selectPlayerByID"
		fileName       = "players.get_by_id.go"
	)

	var rec records.Player
	err := r.conn.GetContext(ctx, &rec, `
		SELECT id, installation_id, role_id, created_at, updated_at
		FROM players
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Player{}, apperror.NotFound("player not found")
		}
		logger.ErrorWithContext(ctx, "Failed to select player by id", logger.Fields{
			"repository": repositoryName,
			"method":     funcName,
			"query":      queryName,
			"file":       fileName,
			"error":      err.Error(),
			"table":      "players",
		})
		return domain.Player{}, fmt.Errorf("select player by id: %w", err)
	}
	return rec.ToV1Domain(), nil
}