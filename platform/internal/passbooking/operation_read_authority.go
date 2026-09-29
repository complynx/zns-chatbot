package passbooking

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// OperationReadAuthorities binds receipt metadata to current domain permission
// and exact target identities. Original value provenance is retained separately.
func OperationReadAuthorities(
	ctx context.Context,
	tx pgx.Tx,
	actor, event, action string,
	targets []string,
) ([]ReadAuthority, error) {
	refs := []ReadAuthority{{Kind: ReadCapability, Event: event, Action: action}}
	owners := slices.Clone(targets)
	slices.Sort(owners)
	for _, owner := range slices.Compact(owners) {
		if owner == "" {
			continue
		}
		var telegramID int64
		err := tx.QueryRow(ctx, `SELECT telegram_id FROM core.users WHERE id=$1 FOR SHARE`, owner).Scan(&telegramID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, forbidden()
		}
		if err != nil {
			return nil, err
		}
		refs = append(refs, ReadAuthority{Kind: ReadOperationTarget, Event: event, Action: action,
			Owner: owner, TargetTelegramID: telegramID})
	}
	return checkedOperationAuthorities(ctx, tx, actor, refs)
}

// ExportOperationAuthority selects an actual live export-permission witness.
// It does not describe the contents or delivery of a historical export file.
func ExportOperationAuthority(ctx context.Context, tx pgx.Tx, actor string) ([]ReadAuthority, error) {
	var event string
	err := tx.QueryRow(ctx, exportEvents+`SELECT id FROM allowed_events ORDER BY id LIMIT 1`, actor).Scan(&event)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, forbidden()
	}
	if err != nil {
		return nil, err
	}
	return checkedOperationAuthorities(ctx, tx, actor, []ReadAuthority{{Kind: ReadExportPermission, Event: event}})
}

func checkedOperationAuthorities(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	refs []ReadAuthority,
) ([]ReadAuthority, error) {
	valid, err := LockReadAuthorities(ctx, tx, actor, refs)
	if err != nil {
		return nil, err
	}
	if slices.Contains(valid, false) {
		return nil, forbidden()
	}
	return refs, nil
}

func lockExportPermission(ctx context.Context, tx pgx.Tx, actor, event string) (bool, error) {
	_, err := authorize(ctx, tx, actor, commandAdminAssign, event)
	if operationPermissionDenied(err) {
		_, err = authorize(ctx, tx, actor, commandProofAccept, event)
	}
	if operationPermissionDenied(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var allowed bool
	err = tx.QueryRow(ctx, exportEvents+`SELECT EXISTS(SELECT 1 FROM allowed_events WHERE id=$2)`, actor, event).
		Scan(&allowed)
	return allowed, err
}

func operationPermissionDenied(err error) bool {
	problem, ok := errors.AsType[*core.ProblemError](err)
	return ok && problem.Status == http.StatusForbidden
}
