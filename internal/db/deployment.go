package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

var ErrDeploymentScope = errors.New("database belongs to a different deployment")

// BindDeployment pins the schema before recovery can dispatch persisted effects.
// Existing data must match on first binding. Scope changes need an explicit migration.
func (s *Store) BindDeployment(ctx context.Context, botID int64, organization, issuer string) error {
	if botID <= 0 || organization == "" || issuer == "" {
		return ErrDeploymentScope
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	existing, err := q.GetDeployment(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		matches, checkErr := q.DeploymentDataMatches(
			ctx,
			sqlc.DeploymentDataMatchesParams{BotID: botID, ZitadelOrg: organization},
		)
		if checkErr != nil {
			return checkErr
		}
		if !matches {
			return ErrDeploymentScope
		}
		if err = q.BindDeployment(
			ctx,
			sqlc.BindDeploymentParams{BotID: botID, OrganizationID: organization, Issuer: issuer},
		); err != nil {
			return err
		}
		existing, err = q.GetDeployment(ctx)
	}
	if err != nil {
		return err
	}
	if existing.BotID != botID || existing.OrganizationID != organization || existing.Issuer != issuer {
		return ErrDeploymentScope
	}
	return tx.Commit(ctx)
}
