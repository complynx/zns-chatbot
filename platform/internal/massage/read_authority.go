package massage

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ReadAuthority retains the exact practitioner event that exposed private data.
type ReadAuthority struct {
	Event string `json:"event"`
	Owner string `json:"owner"`
}

func (a ReadAuthority) Valid() bool {
	return a.Event != "" && len(a.Event) <= 200 && !strings.ContainsRune(a.Event, 0) && a.Owner != "" &&
		len(a.Owner) <= 200 &&
		!strings.ContainsRune(a.Owner, 0)
}

func LockReadAuthority(ctx context.Context, tx pgx.Tx, actor string, a ReadAuthority) (bool, error) {
	if !a.Valid() {
		return false, errors.New("invalid practitioner source")
	}
	if actor != a.Owner {
		return false, nil
	}
	return lockPractitionerRole(ctx, tx, actor, a.Event)
}

func lockPractitionerRole(ctx context.Context, tx pgx.Tx, actor, event string) (bool, error) {
	var owner string
	err := tx.QueryRow(ctx, `SELECT owner FROM core.massage_specialists WHERE event_id=$1 AND owner=$2 FOR SHARE`, event, actor).
		Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
