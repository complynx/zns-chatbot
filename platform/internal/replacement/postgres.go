package replacement

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
)

// PostgresSessions uses a dedicated inventory connection, never a runtime role.
type PostgresSessions struct {
	Conn  *pgx.Conn
	Roles []string
}

// Names reads all managed-role sessions; missing visibility and prepared transactions fail closed.
func (p PostgresSessions) Names(ctx context.Context) ([]string, error) {
	limited, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	var visible bool
	var prepared int
	var roles int
	var current string
	err := p.Conn.QueryRow(limited, `
 SELECT current_user, pg_has_role(current_user, 'pg_read_all_stats', 'MEMBER')
 OR (SELECT rolsuper FROM pg_roles WHERE rolname=current_user),
 (SELECT count(*) FROM pg_prepared_xacts WHERE database=current_database()),
 (SELECT count(*) FROM pg_roles WHERE rolname=ANY($1::text[]))
 `, p.Roles).Scan(&current, &visible, &prepared, &roles)
	if err != nil || !visible || prepared != 0 || roles != len(p.Roles) || len(p.Roles) == 0 ||
		slices.Contains(p.Roles, current) {
		return nil, ErrUnknown
	}
	rows, err := p.Conn.Query(limited, `
 SELECT application_name FROM pg_stat_activity
 WHERE datname=current_database() AND usename=ANY($1::text[])
 `, p.Roles)
	if err != nil {
		return nil, errors.New("database session inventory unavailable")
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, ErrUnknown
		}
		names = append(names, name)
	}
	if rows.Err() != nil {
		return nil, ErrUnknown
	}
	return names, nil
}
