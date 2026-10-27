package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The global byte admission lock follows the owner lock and precedes settings.
// It is schema-scoped so independent test databases/schemas do not share a quota.
func lockAvatarStorage(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended(current_schema() || ':zinusya:avatar-originals',0))`,
	)
	return err
}

func expireAvatarSources(ctx context.Context, tx pgx.Tx, owner uuid.UUID) error {
	policy, now, err := avatarSourcePolicy(ctx, tx)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		"UPDATE shuttle_avatar_drafts SET original=NULL,original_expired_at=$2 WHERE "+
			"user_id=$1 AND original IS NOT NULL AND created_at <= $3",
		owner,
		now,
		now.Add(-policy.Retention),
	)
	return err
}

func avatarDraftQuota(ctx context.Context, tx pgx.Tx, owner uuid.UUID, additional int) error {
	if err := lockAvatarStorage(ctx, tx); err != nil {
		return err
	}
	// Admission can reclaim this owner's expired bytes without waiting for the sweep.
	if err := expireAvatarSources(ctx, tx, owner); err != nil {
		return err
	}
	policy, now, err := avatarSourcePolicy(ctx, tx)
	if err != nil {
		return err
	}
	var count int
	var size int64
	err = tx.QueryRow(ctx, "SELECT count(*) FILTER (WHERE source_file_id IS NOT NULL OR history_record_id IS NOT "+
		"NULL OR (original_expired_at IS NULL AND created_at > $2)),"+
		"coalesce(sum(source_size_bytes) FILTER (WHERE original IS NOT NULL),0)::bigint FROM "+
		"shuttle_avatar_drafts WHERE user_id=$1", owner, now.Add(-policy.Retention)).
		Scan(&count, &size)
	if err != nil {
		return err
	}
	if count >= MaxAvatarDraftsPerOwner || size+int64(additional) > MaxAvatarOriginalBytesPerOwner {
		return ErrAvatarDraftQuota
	}
	if additional > 0 {
		var total int64
		if err = tx.QueryRow(ctx, "SELECT coalesce(sum(source_size_bytes),0)::bigint FROM shuttle_avatar_drafts WHERE "+
			"original IS NOT NULL").
			Scan(&total); err != nil {
			return err
		}
		if int64(additional) > policy.LimitBytes-total {
			return ErrAvatarDraftQuota
		}
	}
	return nil
}

const avatarSourceExpiryPageSize = 4

// ExpireAvatarSources clears only expired browser bytes in a bounded owner page.
// Rows, versions, operations and completed artifacts survive. Busy owners are
// skipped and revisited after cursor wrap; no pool connection spans another tx.
func (s *Store) ExpireAvatarSources(ctx context.Context, scope EventScope, after uuid.UUID) (uuid.UUID, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return after, err
	}
	policy, err := settings.Document.AvatarBrowserPolicy()
	if err != nil {
		return after, err
	}
	rows, err := s.pool.Query(
		ctx,
		"SELECT DISTINCT u.id FROM shuttle_users u JOIN shuttle_avatar_drafts d ON "+
			"d.user_id=u.id WHERE u.bot_id=$1 AND u.zitadel_org=$2 AND u.id>$3 AND d.original IS "+
			"NOT NULL AND d.created_at <= clock_timestamp()-make_interval(secs=>$4) ORDER BY u.id "+
			"LIMIT $5",
		scope.BotID,
		scope.Organization,
		after,
		policy.Retention.Seconds(),
		avatarSourceExpiryPageSize,
	)
	if err != nil {
		return after, err
	}
	owners := make([]uuid.UUID, 0, avatarSourceExpiryPageSize)
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return after, err
		}
		owners = append(owners, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return after, err
	}
	for _, owner := range owners {
		if err = s.expireOwnerAvatarSources(ctx, scope, owner); err != nil {
			return after, err
		}
	}
	if len(owners) < avatarSourceExpiryPageSize {
		return uuid.Nil, nil
	}
	return owners[len(owners)-1], nil
}

func (s *Store) expireOwnerAvatarSources(ctx context.Context, scope EventScope, owner uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackLegacyArchive(ctx, tx)
	var locked uuid.UUID
	err = tx.QueryRow(ctx, "SELECT id FROM shuttle_users WHERE id=$1 AND bot_id=$2 AND zitadel_org=$3 FOR UPDATE "+
		"SKIP LOCKED", owner, scope.BotID, scope.Organization).
		Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = lockAvatarStorage(ctx, tx); err != nil {
		return err
	}
	if err = expireAvatarSources(ctx, tx, owner); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
