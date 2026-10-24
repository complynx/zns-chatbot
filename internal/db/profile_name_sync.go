package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ProfileNameSync struct {
	UserID              uuid.UUID
	Version             int64
	FirstName, LastName string
}

// ProfileNameRetryError keeps a pending River job alive during persisted IAM backoff.
type ProfileNameRetryError struct{ After time.Duration }

func (*ProfileNameRetryError) Error() string { return "profile name sync not due" }

// ReadyProfileNames repairs queue insertion after a committed profile change.
// Jobs carry IDs, never name text. The persisted desired state is authoritative.
func (s *Store) ReadyProfileNames(ctx context.Context, botID int64, organization string) ([]ProfileNameSync, error) {
	rows, err := s.pool.Query(ctx, `SELECT n.user_id,n.desired_version
FROM shuttle_profile_name_sync n JOIN shuttle_users u ON u.id=n.user_id
WHERE u.bot_id=$1 AND u.zitadel_org=$2 AND u.zitadel_subject<>'' AND NOT u.revoked
 AND n.next_attempt_at<=clock_timestamp()
ORDER BY n.next_attempt_at,n.user_id LIMIT 100`, botID, organization)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ProfileNameSync, 0)
	for rows.Next() {
		var value ProfileNameSync
		if err = rows.Scan(&value.UserID, &value.Version); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// DispatchProfileName serializes bounded IAM dispatch with local edits and
// revocation. A callback must honor ctx and confirm the remote name by read-back.
// Remote requests already admitted cannot be recalled by local revocation.
func (s *Store) DispatchProfileName(
	ctx context.Context, scope ProfileScope, dispatch func(ProfileNameSync) error,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockProfileOwner(ctx, tx, scope, true); err != nil {
		return err
	}
	var value ProfileNameSync
	var nextAttempt, observed time.Time
	var pending bool
	value.UserID = scope.UserID
	err = tx.QueryRow(ctx, "SELECT n.desired_version,p.first_name,p.last_name,n.next_attempt_at,cloc"+
		"k_timestamp(),n.desired_version>n.synced_version\nFROM shuttle_profile_na"+
		"me_sync n JOIN shuttle_user_profiles p ON p.user_id=n.user_id\nWHERE n.us"+
		"er_id=$1\nFOR UPDATE OF n", scope.UserID).
		Scan(&value.Version, &value.FirstName, &value.LastName, &nextAttempt, &observed, &pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if nextAttempt.After(observed) {
		if !pending {
			return nil
		}
		return &ProfileNameRetryError{After: nextAttempt.Sub(observed)}
	}
	dispatchErr := dispatch(value)
	if dispatchErr == nil {
		_, err = tx.Exec(
			ctx,
			"UPDATE shuttle_profile_name_sync SET synced_version=$2,next_attempt_at=c"+
				"lock_timestamp()+interval '10 minutes',updated_at=clock_timestamp()\nWHER"+
				"E user_id=$1 AND desired_version=$2",
			scope.UserID,
			value.Version,
		)
	} else {
		// Keep reconciliation across revisions: a timed-out old HTTP request can
		// still overwrite a newer acknowledged name. Later reads repair that.
		_, err = tx.Exec(
			ctx,
			"UPDATE shuttle_profile_name_sync SET synced_version=0,next_attempt_at=cl"+
				"ock_timestamp()+interval '30 seconds',updated_at=clock_timestamp()\nWHERE"+
				" user_id=$1 AND desired_version=$2",
			scope.UserID,
			value.Version,
		)
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return dispatchErr
}
