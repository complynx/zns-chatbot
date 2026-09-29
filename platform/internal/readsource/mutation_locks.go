package readsource

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// LockEvents acquires the source registration events before target event locks.
// The caller must subsequently authorize sources; these locks grant no access.
func LockEvents(ctx context.Context, tx pgx.Tx, authorities []Authority) error {
	expanded, err := ExpandProposalSources(ctx, tx, authorities)
	if err != nil {
		return err
	}
	return lockEvents(ctx, tx, expanded)
}

func lockEvents(ctx context.Context, tx pgx.Tx, authorities []Authority) error {
	for _, a := range authorities {
		if !Valid([]Authority{a}) {
			return errors.New("invalid source authority")
		}
	}
	refs := []passbooking.ReadAuthority{}
	for _, a := range sourceLeaves(authorities) {
		if !Valid([]Authority{a}) {
			return errors.New("invalid source authority")
		}
		if a.Registration != (passbooking.ReadAuthority{}) {
			refs = append(refs, a.Registration)
		}
	}
	_, err := passbooking.LockReadAuthorityEvents(ctx, tx, refs)
	return err
}

// LockActors locks mutation actors, causal origin actors and source targets in
// one canonical order. NO KEY UPDATE prevents later upgrades without blocking
// foreign-key checks on unrelated notification inserts.
func LockActors(ctx context.Context, tx pgx.Tx, actors []string, authorities []Authority) error {
	expanded, err := ExpandProposalSources(ctx, tx, authorities)
	if err != nil {
		return err
	}
	return lockActors(ctx, tx, actors, expanded, true)
}

func lockActors(ctx context.Context, tx pgx.Tx, actors []string, authorities []Authority, mutation bool) error {
	for _, a := range authorities {
		if !Valid([]Authority{a}) {
			return errors.New("invalid source authority")
		}
	}
	actors = append(append([]string{}, actors...), causalActors(authorities)...)

	targets := []int64{}
	for _, a := range sourceLeaves(authorities) {
		if !Valid([]Authority{a}) {
			return errors.New("invalid source authority")
		}
		if a.Knowledge.Owner != "" {
			actors = append(actors, a.Knowledge.Owner)
		}
		if a.Registration.TargetTelegramID != 0 {
			targets = append(targets, a.Registration.TargetTelegramID)
		}
	}
	query := `SELECT id FROM core.users WHERE id=ANY($1::text[]) OR telegram_id=ANY($2::bigint[]) ORDER BY id FOR SHARE`
	if mutation {
		query = `SELECT id FROM core.users WHERE id=ANY($1::text[]) OR telegram_id=ANY($2::bigint[]) ORDER BY id FOR NO KEY UPDATE`
	}
	rows, err := tx.Query(ctx, query, actors, targets)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return err
		}
	}
	return rows.Err()
}
