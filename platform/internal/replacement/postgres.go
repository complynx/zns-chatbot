package replacement

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// PostgresSessions uses a dedicated inventory connection, never a runtime role.
type PostgresSessions struct {
	Conn  *pgx.Conn
	Roles []string
}

// Names reads all managed-role sessions; missing visibility and prepared transactions fail closed.
func (p PostgresSessions) Names(ctx context.Context) ([]string, error) {
	start := time.Now()
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
	if err != nil {
		return nil, observationError(
			limited, start, "postgres_visibility", "query", inventoryFailure(err, ErrUnknown),
		)
	}
	if !visible || prepared != 0 || roles != len(p.Roles) || len(p.Roles) == 0 || slices.Contains(p.Roles, current) {
		predicate := observationPredicate("role_binding")
		if !visible {
			predicate = "visibility"
		} else if prepared != 0 {
			predicate = "prepared_transactions"
		}
		return nil, observationError(limited, start, "postgres_visibility", predicate, ErrUnknown)
	}
	start = time.Now()
	rows, err := p.Conn.Query(limited, `
 SELECT application_name FROM pg_stat_activity
 WHERE datname=current_database() AND usename=ANY($1::text[])
 `, p.Roles)
	if err != nil {
		return nil, observationError(
			limited,
			start,
			"postgres_sessions",
			"query",
			inventoryFailure(err, errors.New("database session inventory unavailable")),
		)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, observationError(
				limited, start, "postgres_sessions", "scan", inventoryFailure(err, ErrUnknown),
			)
		}
		names = append(names, name)
	}
	if err = rows.Err(); err != nil {
		return nil, observationError(
			limited, start, "postgres_sessions", "iteration", inventoryFailure(err, ErrUnknown),
		)
	}
	return names, nil
}

// inventoryFailure retains context failures without exposing database error text.
func inventoryFailure(err, fallback error) error {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, cause) {
			return errors.Join(fallback, cause)
		}
	}
	return fallback
}
