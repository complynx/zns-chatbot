package passes

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// Change describes a successful profile operation without identity values.
type Change struct {
	Version int64     `json:"version"`
	Action  string    `json:"action"`
	Field   string    `json:"field"`
	Origin  string    `json:"origin"`
	At      time.Time `json:"at"`
}

// History returns the owner's latest 30 successful operations, oldest first.
// It includes API and Telegram operations, independently of bot conversation logs.
func (s Service) History(ctx context.Context, actor string) ([]Change, error) {
	var exists bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users WHERE id=$1)`, actor).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, problem(http.StatusForbidden, "forbidden")
	}
	rows, err := s.DB.Query(ctx, `SELECT version,action,field,origin,at FROM
	(SELECT version,action,field,origin,at FROM core.pass_profile_history WHERE owner=$1 ORDER BY version DESC LIMIT 30) recent
	ORDER BY version`, actor)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Change])
}

func recordChange(ctx context.Context, tx pgx.Tx, actor string, version int64, c Command) error {
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.pass_profile_history(owner,version,action,field,origin) VALUES($1,$2,$3,$4,$5)`,
		actor,
		version,
		c.Name,
		c.Field,
		c.Origin,
	)
	return err
}
