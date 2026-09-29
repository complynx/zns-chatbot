package migrate

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// UserApplySummary contains counts and hashes only, including on partial failure.
type UserApplySummary struct {
	PendingDomains   int    `json:"pending_domains"`
	PlanSHA256       string `json:"plan_sha256"`
	ResolutionSHA256 string `json:"resolution_sha256"`
	Candidates       int64  `json:"candidates"`
	Excluded         int64  `json:"excluded_other_bot"`
	Applied          int64  `json:"applied"`
	Reused           int64  `json:"reused"`
	Reconciled       bool   `json:"reconciled"`
}

// ApplyUsers validates all input before connecting, then commits one complete
// user and its receipt per transaction. Replays verify state without updating it.
func ApplyUsers(ctx context.Context, dsn, stage, plan, resolutions string, limits Limits) (UserApplySummary, error) {
	var summary UserApplySummary
	prepared, err := prepareUsers(stage, plan, resolutions, limits)
	if err != nil {
		return summary, err
	}
	summary = UserApplySummary{
		PlanSHA256:       prepared.planHash,
		ResolutionSHA256: prepared.resolutionHash,
		Candidates:       int64(len(prepared.users)),
		Excluded:         prepared.excluded,
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return summary, errors.New("apply_database_unavailable")
	}
	defer func() { _ = conn.Close(context.Background()) }()
	if err = prepareUserReceipts(ctx, conn); err != nil {
		return summary, err
	}
	for _, user := range prepared.users {
		reused, applyErr := applyOneUser(ctx, conn, prepared, user)
		if applyErr != nil {
			return summary, applyErr
		}
		if reused {
			summary.Reused++
		} else {
			summary.Applied++
		}
	}
	summary.PendingDomains, err = pendingUserDomains(ctx, conn, prepared.users)
	if err != nil {
		return summary, err
	}
	summary.Reconciled = true
	return summary, nil
}

func pendingUserDomains(ctx context.Context, conn *pgx.Conn, users []preparedUser) (int, error) {
	count := 0
	for _, user := range users {
		for domain, raw := range map[string][]byte{"food": user.record.DeferredFood, "passes": user.record.DeferredPasses, "massage": user.record.DeferredMassage} {
			if len(raw) == 0 {
				continue
			}
			var pending bool
			if err := conn.QueryRow(ctx, `SELECT NOT completed FROM core.legacy_user_deferred_domains WHERE source_key=$1 AND domain=$2`, user.record.Legacy.Key, domain).
				Scan(&pending); err != nil {
				return 0, errors.New("apply_deferred_domain_unresolved")
			}
			if pending {
				count++
			}
		}
	}
	return count, nil
}

func prepareUserReceipts(ctx context.Context, conn *pgx.Conn) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return errors.New("apply_schema_unavailable")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// Concurrent first runs must serialize PostgreSQL catalog writes as well as rows.
	if _, err = tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('migrate_import.user_receipts',0))`,
	); err != nil {
		return errors.New("apply_schema_unavailable")
	}
	if _, err = tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS migrate_import;
 CREATE TABLE IF NOT EXISTS migrate_import.user_receipts (
 source_key text PRIMARY KEY REFERENCES core.legacy_user_references(source_key),
 plan_sha256 text NOT NULL, resolution_sha256 text NOT NULL
 )`); err != nil {
		return errors.New("apply_schema_unavailable")
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("apply_schema_unavailable")
	}
	return nil
}

func applyOneUser(ctx context.Context, conn *pgx.Conn, plan preparedUsers, user preparedUser) (bool, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return false, errors.New("apply_transaction_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// Serialize receipt lookup/create for this source record. Uniqueness also
	// prevents different source records from claiming one target identity.
	if _, err = tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		user.record.Legacy.Key,
	); err != nil {
		return false, errors.New("apply_transaction_failed")
	}
	var planHash, resolutionHash string
	err = tx.QueryRow(ctx, `SELECT plan_sha256,resolution_sha256 FROM migrate_import.user_receipts WHERE source_key=$1`, user.record.Legacy.Key).
		Scan(&planHash, &resolutionHash)
	reused := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, errors.New("apply_receipt_unavailable")
	}
	if reused && (planHash != plan.planHash || resolutionHash != plan.resolutionHash) {
		return false, errors.New("apply_receipt_conflict")
	}
	if !reused {
		if err = insertUser(ctx, tx, plan, user); err != nil {
			return false, err
		}
	}
	if err = reconcileUser(ctx, tx, plan, user); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, errors.New("apply_commit_failed")
	}
	return reused, nil
}

func insertUser(ctx context.Context, tx pgx.Tx, plan preparedUsers, user preparedUser) error {
	candidate, link := user.record.Candidate, user.resolution
	// Plain INSERT is deliberate: preexisting accounts/profiles are conflicts,
	// including accounts which happen to have equal imported fields.
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.users(id,telegram_id,name,can_book,language,username,first_name,last_name,print_name,telegram_metadata_update)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		link.Owner,
		candidate.TelegramID,
		candidate.DisplayName,
		link.CanBook,
		candidate.Language,
		candidate.Username,
		candidate.FirstName,
		candidate.LastName,
		candidate.PrintName,
		candidate.TelegramMetadataUpdate,
	); err != nil {
		return errors.New("apply_target_conflict")
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.pass_profiles(owner,role,legal_name,passport,frozen) VALUES($1,$2,$3,$4,$5)`,
		link.Owner,
		candidate.Role,
		candidate.LegalName,
		candidate.Passport,
		candidate.Frozen,
	); err != nil {
		return errors.New("apply_profile_conflict")
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.zitadel_identities(owner,issuer,subject) VALUES($1,$2,$3)`,
		link.Owner,
		link.Issuer,
		link.Subject,
	); err != nil {
		return errors.New("apply_identity_conflict")
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.telegram_identities(bot_id,telegram_id,owner) VALUES($1,$2,$3)`,
		plan.botID,
		candidate.TelegramID,
		link.Owner,
	); err != nil {
		return errors.New("apply_identity_conflict")
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.legacy_user_references(source_key,owner,source_record_sha256) VALUES($1,$2,$3)`,
		user.record.Legacy.Key,
		link.Owner,
		user.record.Legacy.RecordSHA256,
	); err != nil {
		return errors.New("apply_legacy_conflict")
	}
	if err := insertUserMassageDeferred(ctx, tx, user); err != nil {
		return err
	}
	if err := insertBroadcastProfile(ctx, tx, user); err != nil {
		return err
	}
	if len(user.record.DeferredFood) > 0 {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO core.legacy_user_deferred_domains(source_key,domain,source_record) VALUES($1,'food',$2)`,
			user.record.Legacy.Key,
			user.record.DeferredFood,
		); err != nil {
			return errors.New("apply_deferred_domain_conflict")
		}
	}
	if len(user.record.DeferredPasses) > 0 {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO core.legacy_user_deferred_domains(source_key,domain,source_record) VALUES($1,'passes',$2)`,
			user.record.Legacy.Key,
			user.record.DeferredPasses,
		); err != nil {
			return errors.New("apply_deferred_domain_conflict")
		}
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO migrate_import.user_receipts(source_key,plan_sha256,resolution_sha256) VALUES($1,$2,$3)`,
		user.record.Legacy.Key,
		plan.planHash,
		plan.resolutionHash,
	); err != nil {
		return errors.New("apply_receipt_conflict")
	}
	return nil
}

func reconcileUser(ctx context.Context, tx pgx.Tx, plan preparedUsers, user preparedUser) error {
	if err := reconcileUserMassageDeferred(ctx, tx, user); err != nil {
		return err
	}
	if err := reconcileBroadcastProfile(ctx, tx, user); err != nil {
		return err
	}
	candidate, link := user.record.Candidate, user.resolution
	var matched bool
	err := tx.QueryRow(
		ctx,
		`SELECT EXISTS (
 SELECT 1 FROM core.users u
 JOIN core.pass_profiles p ON p.owner=u.id
 JOIN core.zitadel_identities z ON z.owner=u.id
 JOIN core.telegram_identities t ON t.owner=u.id
 JOIN core.legacy_user_references l ON l.owner=u.id
 WHERE u.id=$1 AND u.telegram_id=$2 AND u.name=$3 AND u.can_book=$4
 AND u.language=$5 AND u.username=$6 AND u.first_name=$7 AND u.last_name=$8
 AND u.print_name=$9 AND u.telegram_metadata_update=$10
 AND p.role=$11 AND p.legal_name=$12 AND p.passport=$13 AND p.frozen=$14
 AND p.version=0 AND p.pending='' AND p.expires_at IS NULL AND NOT p.passport_after
 AND z.issuer=$15 AND z.subject=$16 AND z.active
 AND t.bot_id=$17 AND t.telegram_id=$2
 AND l.source_key=$18 AND l.source_record_sha256=$19
 )`,
		link.Owner,
		candidate.TelegramID,
		candidate.DisplayName,
		link.CanBook,
		candidate.Language,
		candidate.Username,
		candidate.FirstName,
		candidate.LastName,
		candidate.PrintName,
		candidate.TelegramMetadataUpdate,
		candidate.Role,
		candidate.LegalName,
		candidate.Passport,
		candidate.Frozen,
		link.Issuer,
		link.Subject,
		plan.botID,
		user.record.Legacy.Key,
		user.record.Legacy.RecordSHA256,
	).Scan(&matched)
	if err != nil || !matched {
		return errors.New("apply_reconciliation_failed")
	}
	if len(user.record.DeferredFood) > 0 {
		if err = tx.QueryRow(ctx, `SELECT source_record=$2::jsonb FROM core.legacy_user_deferred_domains WHERE source_key=$1 AND domain='food'`, user.record.Legacy.Key, user.record.DeferredFood).
			Scan(&matched); err != nil ||
			!matched {
			return errors.New("apply_reconciliation_failed")
		}
	}
	if len(user.record.DeferredPasses) > 0 {
		if err = tx.QueryRow(ctx, `SELECT source_record=$2::jsonb FROM core.legacy_user_deferred_domains WHERE source_key=$1 AND domain='passes'`, user.record.Legacy.Key, user.record.DeferredPasses).
			Scan(&matched); err != nil ||
			!matched {
			return errors.New("apply_reconciliation_failed")
		}
	}
	return nil
}
