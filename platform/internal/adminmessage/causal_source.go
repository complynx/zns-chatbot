package adminmessage

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

const sourceOriginal = "original"
const sourceDerived = "derived"
const sourceRevoked = "source_revoked"
const stateCancelled = "cancelled"

type sourceBinding struct {
	Origin      string
	Authorities []readsource.Authority
	Revoked     bool
}

func originalSource() sourceBinding {
	return sourceBinding{Origin: sourceOriginal, Authorities: []readsource.Authority{}}
}
func captureSource(actor string, source readsource.Derivation) (sourceBinding, error) {
	refs, err := readsource.Capture(actor, source)
	if err != nil {
		return sourceBinding{}, problem(http.StatusBadRequest, "invalid_derivation")
	}
	return sourceBinding{Origin: sourceDerived, Authorities: refs}, nil
}
func mergeSources(left, right sourceBinding) (sourceBinding, error) {
	refs, err := readsource.Merge(left.Authorities, right.Authorities)
	if err != nil {
		return sourceBinding{}, problem(http.StatusBadRequest, "invalid_derivation")
	}
	origin := sourceOriginal
	if left.Origin == sourceDerived || right.Origin == sourceDerived {
		origin = sourceDerived
	}
	return sourceBinding{Origin: origin, Authorities: refs, Revoked: left.Revoked || right.Revoked}, nil
}
func (s sourceBinding) prelock(ctx context.Context, tx pgx.Tx, actor string) error {
	if s.Origin == sourceOriginal {
		return nil
	}
	if err := readsource.LockEvents(ctx, tx, s.Authorities); err != nil {
		return err
	}
	return readsource.LockActors(ctx, tx, []string{actor}, s.Authorities)
}
func (s sourceBinding) validity(ctx context.Context, tx pgx.Tx, actor string) (bool, error) {
	if s.Origin == sourceOriginal {
		return len(s.Authorities) == 0 && !s.Revoked, nil
	}
	if s.Origin != sourceDerived || s.Revoked || len(s.Authorities) == 0 || !readsource.Valid(s.Authorities) {
		return false, nil
	}
	validity, err := readsource.LockValidity(ctx, tx, actor, s.Authorities)
	if err != nil {
		return false, err
	}
	if slices.Contains(validity.Origin, false) {
		return false, nil
	}
	if slices.Contains(validity.Reader, false) {
		return true, problem(http.StatusForbidden, "source_forbidden")
	}
	return true, nil
}

// A privacy tombstone must survive the refusal that it causes. This helper is
// used only before any business mutation, so committing cannot accept an effect.
type revokedSourceError struct{ problem *core.ProblemError }

func (e *revokedSourceError) Error() string { return e.problem.Error() }
func (e *revokedSourceError) Unwrap() error { return e.problem }
func revokedSource() error {
	return &revokedSourceError{&core.ProblemError{Status: http.StatusConflict, Code: sourceRevoked}}
}
func preserveSourceRefusal(ctx context.Context, tx pgx.Tx, err error) error {
	if _, ok := errors.AsType[*revokedSourceError](err); ok {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return commitErr
		}
	}
	return err
}

func messageSource(ctx context.Context, tx pgx.Tx, actor string, id int64) (sourceBinding, error) {
	var source sourceBinding
	err := tx.QueryRow(ctx, `SELECT source_origin,source_authorities,source_revoked FROM core.admin_messages WHERE id=$1 AND actor=$2`, id, actor).
		Scan(&source.Origin, &source.Authorities, &source.Revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return source, problem(http.StatusNotFound, "not_found")
	}
	return source, err
}
func guardMessage(ctx context.Context, tx pgx.Tx, actor string, id int64) error {
	source, err := messageSource(ctx, tx, actor, id)
	if err != nil {
		return err
	}
	if err = source.prelock(ctx, tx, actor); err != nil {
		return err
	}
	if err = authorize(ctx, tx, actor); err != nil {
		return err
	}
	valid, err := source.validity(ctx, tx, actor)
	if err != nil {
		return err
	}
	if valid {
		return nil
	}
	if err = retireMessage(ctx, tx, id); err != nil {
		return err
	}
	return revokedSource()
}
func retireMessage(ctx context.Context, tx pgx.Tx, id int64) error {
	_, err := tx.Exec(
		ctx,
		`UPDATE core.admin_messages SET source_revoked=true,state='cancelled',request='{"destinations":[],"content":{}}',command=NULL WHERE id=$1 AND source_origin='derived'`,
		id,
	)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.admin_message_recipients SET content='{}',profile=NULL,render_state='done',failure='source_revoked' WHERE message_id=$1`,
		id,
	)
	if err != nil {
		return err
	}
	items, err := lockAdminPublication(ctx, tx, id)
	if err != nil {
		return err
	}
	// Redact every payload, but retain dispatched attempts and their lane fence
	// until completion or lease expiry establishes the delivery outcome.
	_, err = tx.Exec(
		ctx,
		`UPDATE core.admin_message_deliveries SET content='{}',state=CASE WHEN state='pending' THEN 'cancelled' ELSE state END,failure=CASE WHEN state='pending' THEN 'source_revoked' ELSE failure END WHERE message_id=$1`,
		id,
	)
	if err != nil {
		return err
	}
	return projectAdminCancellation(ctx, tx, items)
}
func inputSource(ctx context.Context, tx pgx.Tx, actor string, id int64) (sourceBinding, error) {
	var source sourceBinding
	err := tx.QueryRow(ctx, `SELECT source_origin,source_authorities,source_revoked FROM core.admin_message_inputs WHERE id=$1 AND actor=$2`, id, actor).
		Scan(&source.Origin, &source.Authorities, &source.Revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return source, problem(http.StatusNotFound, "not_found")
	}
	return source, err
}
func guardInput(ctx context.Context, tx pgx.Tx, actor string, id int64) (sourceBinding, error) {
	source, err := inputSource(ctx, tx, actor, id)
	if err != nil {
		return source, err
	}
	if err = source.prelock(ctx, tx, actor); err != nil {
		return source, err
	}
	if err = authorize(ctx, tx, actor); err != nil {
		return source, err
	}
	valid, err := source.validity(ctx, tx, actor)
	if err != nil {
		return source, err
	}
	if valid {
		return source, nil
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.admin_message_inputs SET source_revoked=true,state='cancelled',command='' WHERE id=$1 AND source_origin='derived'`,
		id,
	)
	if err != nil {
		return source, err
	}
	return source, revokedSource()
}

// CheckPublication validates the frozen draft and current sender before an
// external publication. No database lock remains held across the network call.
func (s Service) CheckPublication(ctx context.Context, actor string, id int64) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = guardMessage(ctx, tx, actor, id); err != nil {
		return preserveSourceRefusal(ctx, tx, err)
	}
	return tx.Commit(ctx)
}

func prelockMessageKey(ctx context.Context, tx pgx.Tx, actor, key string, source sourceBinding) error {
	var old []readsource.Authority
	err := tx.QueryRow(ctx, `SELECT source_authorities FROM core.admin_messages WHERE actor=$1 AND key=$2`, actor, key).
		Scan(&old)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if old == nil {
		old = []readsource.Authority{}
	}
	refs, err := readsource.Merge(old, source.Authorities)
	if err != nil {
		return err
	}
	return (sourceBinding{Origin: sourceDerived, Authorities: refs}).prelock(ctx, tx, actor)
}
func fenceNewSource(ctx context.Context, tx pgx.Tx, actor string, source sourceBinding) error {
	valid, err := source.validity(ctx, tx, actor)
	if err != nil {
		return err
	}
	if !valid {
		return problem(http.StatusConflict, "source_stale")
	}
	return nil
}
func saveMessageSource(ctx context.Context, tx pgx.Tx, id int64, source sourceBinding) error {
	_, err := tx.Exec(
		ctx,
		`UPDATE core.admin_messages SET source_origin=$2,source_authorities=$3 WHERE id=$1`,
		id,
		source.Origin,
		source.Authorities,
	)
	return err
}

func prelockInputKey(ctx context.Context, tx pgx.Tx, actor, key string, source sourceBinding) error {
	var old []readsource.Authority
	err := tx.QueryRow(ctx, `SELECT source_authorities FROM core.admin_message_inputs WHERE actor=$1 AND key=$2`, actor, key).
		Scan(&old)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if old == nil {
		old = []readsource.Authority{}
	}
	refs, err := readsource.Merge(old, source.Authorities)
	if err != nil {
		return err
	}
	return (sourceBinding{Origin: sourceDerived, Authorities: refs}).prelock(ctx, tx, actor)
}

func prelockPendingInputs(ctx context.Context, tx pgx.Tx, actor string, chat int64) error {
	rows, err := tx.Query(
		ctx,
		`SELECT source_authorities FROM core.admin_message_inputs WHERE actor=$1 AND chat_id=$2 AND state='pending' AND expires_at>clock_timestamp()`,
		actor,
		chat,
	)
	if err != nil {
		return err
	}
	groups, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([]readsource.Authority, error) {
		var refs []readsource.Authority
		scanErr := row.Scan(&refs)
		return refs, scanErr
	})
	if err != nil {
		return err
	}
	refs := []readsource.Authority{}
	for _, group := range groups {
		refs = append(refs, group...)
	}
	return (sourceBinding{Origin: sourceDerived, Authorities: refs}).prelock(ctx, tx, actor)
}
