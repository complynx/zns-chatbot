package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type PassApplySummary struct {
	PendingDomains   int    `json:"pending_domains"`
	PlanSHA256       string `json:"plan_sha256"`
	ResolutionSHA256 string `json:"resolution_sha256"`
	Events           int    `json:"events"`
	Applied          int    `json:"applied"`
	Reused           bool   `json:"reused"`
	Reconciled       bool   `json:"reconciled"`
}

func ApplyPasses(ctx context.Context, dsn, stage, plan, resolutions string, limits Limits) (PassApplySummary, error) {
	return runPassImport(ctx, dsn, stage, plan, resolutions, limits, false)
}

func ReconcilePasses(
	ctx context.Context,
	dsn, stage, plan, resolutions string,
	limits Limits,
) (PassApplySummary, error) {
	return runPassImport(ctx, dsn, stage, plan, resolutions, limits, true)
}

func runPassImport(
	ctx context.Context,
	dsn, stage, plan, resolutions string,
	limits Limits,
	verifyOnly bool,
) (PassApplySummary, error) {
	var summary PassApplySummary
	p, err := preparePasses(stage, plan, resolutions, limits)
	if err != nil {
		return summary, err
	}
	summary = PassApplySummary{PlanSHA256: p.PlanHash, ResolutionSHA256: p.ResolutionHash, Events: len(p.eventNames())}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return summary, errors.New("apply_database_unavailable")
	}
	defer func() { _ = conn.Close(context.Background()) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return summary, errors.New("apply_transaction_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('migrate_import.user_receipts',0))`,
	); err != nil {
		return summary, errors.New("apply_transaction_failed")
	}
	if !verifyOnly {
		if _, err = tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS migrate_import;
CREATE TABLE IF NOT EXISTS migrate_import.pass_receipts (
 manifest_sha256 text PRIMARY KEY,plan_sha256 text NOT NULL,resolution_sha256 text NOT NULL,
 owners jsonb NOT NULL,snapshot jsonb NOT NULL)`); err != nil {
			return summary, errors.New("apply_schema_unavailable")
		}
	}
	owners, err := resolvePassDependencies(ctx, tx, p)
	if err != nil {
		return summary, err
	}
	reused, err := applyPassReceipt(ctx, tx, p, owners, verifyOnly)
	if err != nil {
		return summary, err
	}
	summary.PendingDomains, err = countPassDeferred(ctx, tx, p)
	if err != nil {
		return summary, err
	}
	if tx.Commit(ctx) != nil {
		return summary, errors.New("apply_commit_failed")
	}
	summary.Reused = reused
	summary.Reconciled = true
	if !reused {
		summary.Applied = len(p.Plan.Records)
	}
	return summary, nil
}

func applyPassReceipt(
	ctx context.Context,
	tx pgx.Tx,
	p preparedPasses,
	owners map[string]string,
	verifyOnly bool,
) (bool, error) {
	var planHash, resolutionHash string
	err := tx.QueryRow(ctx, `SELECT plan_sha256,resolution_sha256 FROM migrate_import.pass_receipts WHERE manifest_sha256=$1`, p.Plan.ManifestSHA256).
		Scan(&planHash, &resolutionHash)
	reused := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, errors.New("apply_receipt_unavailable")
	}
	if reused && (planHash != p.PlanHash || resolutionHash != p.ResolutionHash) {
		return false, errors.New("apply_receipt_conflict")
	}
	if !reused && verifyOnly {
		return false, errors.New("apply_receipt_missing")
	}
	ownerList := make([]string, 0, len(owners))
	for _, owner := range owners {
		ownerList = append(ownerList, owner)
	}
	slices.Sort(ownerList)
	if reused {
		var matched bool
		if err = tx.QueryRow(ctx, `SELECT owners=$2::jsonb AND snapshot=(`+registrationSnapshotSQL+`) FROM migrate_import.pass_receipts WHERE manifest_sha256=$4`, p.eventNames(), owners, ownerList, p.Plan.ManifestSHA256, p.Plan.BotID).
			Scan(&matched); err != nil ||
			!matched {
			return false, errors.New("apply_reconciliation_failed")
		}
	} else {
		if err = insertPassStage(ctx, tx, p, owners); err != nil {
			return false, err
		}
		if _, err = tx.Exec(
			ctx,
			`INSERT INTO migrate_import.pass_receipts(manifest_sha256,plan_sha256,resolution_sha256,owners,snapshot) SELECT $4,$6,$7,$2,(`+registrationSnapshotSQL+`)`,
			p.eventNames(),
			owners,
			ownerList,
			p.Plan.ManifestSHA256,
			p.Plan.BotID,
			p.PlanHash,
			p.ResolutionHash,
		); err != nil {
			return false, errors.New("apply_receipt_conflict")
		}
	}
	return reused, nil
}
func resolvePassDependencies(ctx context.Context, tx pgx.Tx, p preparedPasses) (map[string]string, error) {
	for _, event := range p.eventNames() {
		d := p.Events[event]
		var found string
		if err := tx.QueryRow(ctx, `SELECT e.id FROM core.pass_events e JOIN core.legacy_event_references r ON r.event_id=e.id WHERE e.id=$1 AND r.source_key=$2 AND r.source_record_sha256=$3 FOR UPDATE OF e`, event, d.Legacy.Key, d.Legacy.RecordSHA256).
			Scan(&found); err != nil {
			return nil, errors.New("apply_event_identity_unresolved")
		}
	}
	for _, event := range p.eventNames() {
		if err := reconcilePassCatalog(ctx, tx, p.Catalogs[event], p.Plan.BotID); err != nil {
			return nil, err
		}
	}
	owners := map[string]string{}
	ids := make([]int64, 0, len(p.Users))
	for id := range p.Users {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		d := p.Users[id]
		var owner string
		err := tx.QueryRow(ctx, `SELECT r.owner FROM core.legacy_user_references r JOIN core.telegram_identities t ON t.owner=r.owner JOIN core.users u ON u.id=r.owner WHERE r.source_key=$1 AND r.source_record_sha256=$2 AND t.bot_id=$3 AND t.telegram_id=$4 AND u.telegram_id=$4 FOR SHARE OF r,t,u`, d.Legacy.Key, d.Legacy.RecordSHA256, p.Plan.BotID, id).
			Scan(&owner)
		if err != nil {
			return nil, errors.New("apply_owner_identity_unresolved")
		}
		owners[strconv.FormatInt(id, 10)] = owner
	}
	return owners, nil
}
func passOwner(owners map[string]string, id int64) string { return owners[strconv.FormatInt(id, 10)] }
func insertPassStage(ctx context.Context, tx pgx.Tx, p preparedPasses, owners map[string]string) error {
	if err := insertPassBookings(ctx, tx, p, owners); err != nil {
		return err
	}
	if err := insertPassPayments(ctx, tx, p, owners); err != nil {
		return err
	}
	if err := insertPassAnnouncements(ctx, tx, p, owners); err != nil {
		return err
	}
	if err := insertPassPreferences(ctx, tx, p, owners); err != nil {
		return err
	}
	for _, user := range p.Users {
		if _, err := tx.Exec(
			ctx,
			`UPDATE core.legacy_user_deferred_domains SET completed=true WHERE source_key=$1 AND domain='passes'`,
			user.Legacy.Key,
		); err != nil {
			return errors.New("apply_deferred_domain_failed")
		}
	}
	return nil
}

func insertPassBookings(ctx context.Context, tx pgx.Tx, p preparedPasses, owners map[string]string) error {
	for _, event := range p.eventNames() {
		var count int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM core.pass_bookings WHERE event_id=$1)+(SELECT count(*) FROM core.pass_payment_attempts WHERE event_id=$1)+(SELECT count(*) FROM core.pass_contact_preferences WHERE event_id=$1)`, event).
			Scan(&count); err != nil ||
			count != 0 {
			return errors.New("apply_target_conflict")
		}
		for _, row := range p.Bookings[event] {
			if err := insertPassReference(ctx, tx, p, row, owners); err != nil {
				return err
			}
			if !row.Shadowed {
				if err := insertPassBooking(ctx, tx, row, owners); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func insertPassPreferences(ctx context.Context, tx pgx.Tx, p preparedPasses, owners map[string]string) error {
	for _, row := range p.Preferences {
		if err := insertPassReference(ctx, tx, p, row, owners); err != nil {
			return err
		}
		owner := passOwner(owners, row.TelegramID)
		for event, admin := range row.Preferences {
			if _, err := tx.Exec(
				ctx,
				`INSERT INTO core.pass_contact_preferences(event_id,owner,payment_admin) VALUES($1,$2,$3)`,
				event,
				owner,
				passOwner(owners, admin),
			); err != nil {
				return errors.New("apply_preference_conflict")
			}
		}
		if row.PassportMarker {
			if _, err := tx.Exec(
				ctx,
				`INSERT INTO core.pass_passport_reminders(owner,legacy_source_key) VALUES($1,$2)`,
				owner,
				row.Legacy.Key,
			); err != nil {
				return errors.New("apply_reminder_conflict")
			}
		}
	}
	return nil
}
func insertPassReference(
	ctx context.Context,
	tx pgx.Tx,
	p preparedPasses,
	row PassPlanRecord,
	owners map[string]string,
) error {
	var event *string
	owner := passOwner(owners, row.TelegramID)
	kind := "preference"
	if row.Candidate != nil {
		event = &row.Candidate.Event
		owner = passOwner(owners, row.Candidate.TelegramID)
		kind = "booking"
		if row.Source == usersSource {
			kind = "embedded"
		}
	} else if row.PassportMarker {
		kind = "user_marker"
	}
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.legacy_pass_import_references(source_key,bot_id,event_id,owner,source_kind,source_record_sha256,target_id,source_record) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		row.Legacy.Key,
		p.Plan.BotID,
		event,
		owner,
		kind,
		row.Legacy.RecordSHA256,
		row.Legacy.Key,
		passRuntimeRecord(row),
	)
	if err != nil {
		return errors.New("apply_pass_reference_conflict")
	}
	return deferPassFood(ctx, tx, row)
}
func insertPassBooking(ctx context.Context, tx pgx.Tx, row PassPlanRecord, owners map[string]string) error {
	b := row.Candidate
	owner := passOwner(owners, b.TelegramID)
	partner := ""
	var target int64
	if b.State == registrationPending {
		target = b.Partner
	} else if b.Partner != 0 {
		partner = passOwner(owners, b.Partner)
	}
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,partner,invitation_target,payment_admin,created_at,assigned_at,price,tier_index,skip_balance,comment) VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		b.Event,
		owner,
		b.State,
		b.Role,
		b.Kind,
		partner,
		target,
		passOwner(owners, b.Admin),
		b.Created,
		b.Assigned,
		b.Price,
		b.Tier,
		b.SkipBalance,
		b.Comment,
	)
	if err != nil {
		return errors.New("apply_booking_conflict")
	}
	if b.Assigned == nil {
		return nil
	}
	if err = insertPassAssignmentMetadata(ctx, tx, row, owners); err != nil {
		return err
	}
	if b.FirstReminder != nil || b.SecondReminder != nil {
		if _, err = tx.Exec(
			ctx,
			`INSERT INTO core.pass_deadline_markers(event_id,owner,assigned_at,first_at,second_at) VALUES($1,$2,$3,$4,$5)`,
			b.Event,
			owner,
			b.Assigned,
			b.FirstReminder,
			b.SecondReminder,
		); err != nil {
			return errors.New("apply_deadline_conflict")
		}
	}
	// A receipt from another assignment remains raw/archive evidence only.
	if b.Received == nil || b.Received.Before(*b.Assigned) {
		return nil
	}
	_, _, reviewer := passReview(b, owners)
	if b.ProofFile != "" || b.Received != nil || b.Accepted != nil || b.Rejected != nil {
		if _, err = tx.Exec(
			ctx,
			`INSERT INTO core.legacy_pass_payment_metadata(event_id,owner,assigned_at,source_key,received_at,accepted_at,rejected_at,proof_reference,receiving_admin,reviewed_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			b.Event,
			owner,
			b.Assigned,
			row.Legacy.Key,
			b.Received,
			passTimeSince(b.Accepted, b.Received),
			passTimeSince(b.Rejected, b.Assigned),
			b.ProofFile,
			passActor(owners, b.ReceivingAdmin),
			reviewer,
		); err != nil {
			return errors.New("apply_payment_metadata_conflict")
		}
	}
	if b.ReceivingAdmin != 0 {
		if _, err = tx.Exec(
			ctx,
			`INSERT INTO core.pass_receiver_backfills(event_id,owner,assigned_at,legacy_receiving_admin,recorded_at) VALUES($1,$2,$3,$4,clock_timestamp())`,
			b.Event,
			owner,
			b.Assigned,
			passOwner(owners, b.ReceivingAdmin),
		); err != nil {
			return errors.New("apply_receiver_conflict")
		}
	}
	return nil
}
func insertPassPayments(ctx context.Context, tx pgx.Tx, p preparedPasses, owners map[string]string) error {
	groups := map[string][]PassPlanRecord{}
	for _, rows := range p.Bookings {
		for _, row := range rows {
			b := row.Candidate
			if row.Shadowed || b.ProofFile == "" || b.ProofFile == registrationFree {
				continue
			}
			key := passAttemptKey(b)
			groups[key] = append(groups[key], row)
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if err := insertPassPayment(ctx, tx, p, owners, key, groups[key]); err != nil {
			return err
		}
	}
	return nil
}

func insertPassPayment(
	ctx context.Context,
	tx pgx.Tx,
	p preparedPasses,
	owners map[string]string,
	key string,
	rows []PassPlanRecord,
) error {
	slices.SortFunc(rows, func(a, b PassPlanRecord) int {
		if a.Candidate.TelegramID < b.Candidate.TelegramID {
			return -1
		}
		if a.Candidate.TelegramID > b.Candidate.TelegramID {
			return 1
		}
		return 0
	})
	first := rows[0]
	b := first.Candidate
	payload := p.Proofs[first.Legacy.Key]
	for _, row := range rows {
		other := p.Proofs[row.Legacy.Key]
		if payload.Hash != other.Hash || payload.Reference.Unavailable != other.Reference.Unavailable {
			return errors.New("pass_shared_receipt_conflict")
		}
	}
	attempt := hashBytes([]byte("pass-attempt:" + strconv.FormatInt(p.Plan.BotID, 10) + ":" + key))
	reference := hashBytes([]byte(attempt + ":source"))
	target := attempt
	if !b.ActiveReceipt {
		target = passProofID(p.Plan.BotID, key)
	}
	evidence, _ := json.Marshal(struct {
		Rows   []PassPlanRecord `json:"records"`
		Proofs []Proof          `json:"proofs"`
	}{Rows: passGroupEvidence(rows), Proofs: passGroupProofs(p, rows)})
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.legacy_pass_import_references(source_key,bot_id,event_id,source_kind,source_record_sha256,target_id,source_record) VALUES($1,$2,$3,'proof',$4,$5,$6)`,
		reference,
		p.Plan.BotID,
		b.Event,
		hashBytes(evidence),
		target,
		evidence,
	)
	if err != nil {
		return errors.New("apply_pass_reference_conflict")
	}
	var proofID *string
	if !payload.Reference.Unavailable {
		id := passProofID(p.Plan.BotID, key)
		proofID = &id
		// Custody gives an existing participant file access; it is not uploader attribution.
		if _, err = tx.Exec(
			ctx,
			`INSERT INTO core.order_proofs(id,owner,filename,body,created_at) VALUES($1,$2,'legacy-pass-receipt',$3,$4)`,
			id,
			passOwner(owners, b.TelegramID),
			payload.Body,
			b.Received,
		); err != nil {
			return errors.New("apply_proof_conflict")
		}
	}
	if !b.ActiveReceipt {
		return nil
	}
	decision, reviewed, _ := passReview(b, owners)
	receiver, reviewer := passSharedActors(rows, owners)
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.pass_payment_attempts(id,event_id,submitter,proof_id,receiving_admin,received_at,decision,reviewed_by,reviewed_at,legacy_source_key,proof_unavailable) VALUES($1,$2,NULL,$3,$4,$5,$6,$7,$8,$9,$10)`,
		attempt,
		b.Event,
		proofID,
		receiver,
		b.Received,
		decision,
		reviewer,
		reviewed,
		reference,
		payload.Reference.Unavailable,
	)
	if err != nil {
		return errors.New("apply_payment_conflict")
	}
	return attachPassParticipants(ctx, tx, rows, owners, attempt)
}

func attachPassParticipants(
	ctx context.Context,
	tx pgx.Tx,
	rows []PassPlanRecord,
	owners map[string]string,
	attempt string,
) error {
	for _, row := range rows {
		member := row.Candidate
		owner := passOwner(owners, member.TelegramID)
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO core.pass_payment_participants(attempt,owner,assigned_at) VALUES($1,$2,$3)`,
			attempt,
			owner,
			member.Assigned,
		); err != nil {
			return errors.New("apply_payment_participant_conflict")
		}
		if _, err := tx.Exec(
			ctx,
			`UPDATE core.pass_bookings SET payment_attempt=$3 WHERE event_id=$1 AND owner=$2`,
			member.Event,
			owner,
			attempt,
		); err != nil {
			return errors.New("apply_payment_attachment_failed")
		}
	}
	return nil
}
func passReview(b *PassCandidate, owners map[string]string) (string, *time.Time, *string) {
	decision, reviewed, reviewer := passReviewFact(b)
	return decision, reviewed, passActor(owners, reviewer)
}
func passReviewFact(b *PassCandidate) (string, *time.Time, int64) {
	decision := "pending"
	reviewed := b.Accepted
	if reviewed != nil && reviewed.Before(*b.Received) {
		reviewed = nil
	}
	var reviewer int64
	if reviewed != nil {
		decision = "accepted"
		reviewer = b.ReviewingAdmin
	}
	if b.Rejected != nil && !b.Rejected.Before(*b.Received) && (reviewed == nil || b.Rejected.After(*reviewed)) {
		decision = "rejected"
		reviewed = b.Rejected
		reviewer = 0
	}
	return decision, reviewed, reviewer
}
func passGroupProofs(p preparedPasses, rows []PassPlanRecord) []Proof {
	proofs := make([]Proof, 0, len(rows))
	for _, row := range rows {
		proofs = append(proofs, p.Proofs[row.Legacy.Key].Reference)
	}
	return proofs
}

const registrationSnapshotSQL = `SELECT jsonb_build_object(

 'announcements',COALESCE((SELECT jsonb_agg(to_jsonb(m) ORDER BY m.source_key) FROM core.legacy_pass_announcement_metadata m WHERE m.event_id=ANY($1::text[])),'[]'),
 'bookings',COALESCE((SELECT jsonb_agg(to_jsonb(b) ORDER BY b.event_id,b.owner) FROM core.pass_bookings b WHERE b.event_id=ANY($1::text[])),'[]'),
 'payments',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM core.pass_payment_attempts p WHERE p.event_id=ANY($1::text[])),'[]'),
 'participants',COALESCE((SELECT jsonb_agg(to_jsonb(m) ORDER BY m.attempt,m.owner) FROM core.pass_payment_participants m JOIN core.pass_payment_attempts p ON p.id=m.attempt WHERE p.event_id=ANY($1::text[])),'[]'),
 'deadlines',COALESCE((SELECT jsonb_agg(to_jsonb(d) ORDER BY d.event_id,d.owner,d.assigned_at) FROM core.pass_deadline_markers d WHERE d.event_id=ANY($1::text[])),'[]'),
 'receivers',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.event_id,r.owner,r.assigned_at) FROM core.pass_receiver_backfills r WHERE r.event_id=ANY($1::text[])),'[]'),
 'assignment_metadata',COALESCE((SELECT jsonb_agg(to_jsonb(m) ORDER BY m.event_id,m.owner,m.assigned_at) FROM core.legacy_pass_assignment_metadata m WHERE m.event_id=ANY($1::text[])),'[]'),
 'pass_deferred',COALESCE((SELECT jsonb_agg(to_jsonb(d)-'completed' ORDER BY d.source_key) FROM core.legacy_pass_deferred_domains d JOIN core.legacy_pass_import_references r USING(source_key) WHERE r.bot_id=$5),'[]'),
 'metadata',COALESCE((SELECT jsonb_agg(to_jsonb(m) ORDER BY m.event_id,m.owner,m.assigned_at) FROM core.legacy_pass_payment_metadata m WHERE m.event_id=ANY($1::text[])),'[]'),
 'proofs',COALESCE((SELECT jsonb_agg(to_jsonb(f) ORDER BY f.id) FROM core.order_proofs f WHERE EXISTS(SELECT 1 FROM core.pass_payment_attempts p WHERE p.proof_id=f.id AND p.event_id=ANY($1::text[])) OR EXISTS(SELECT 1 FROM core.legacy_pass_import_references r WHERE r.source_kind='proof' AND r.target_id=f.id AND r.bot_id=$5)),'[]'),
 'references',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.source_key) FROM core.legacy_pass_import_references r WHERE r.bot_id=$5),'[]'),
 'preferences',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.event_id,p.owner) FROM core.pass_contact_preferences p WHERE p.event_id=ANY($1::text[])),'[]'),
 'reminders',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.owner) FROM core.pass_passport_reminders r WHERE r.owner=ANY($3::text[])),'[]'),
 'deferred',COALESCE((SELECT jsonb_agg(CASE WHEN d.domain='passes' THEN to_jsonb(d) ELSE to_jsonb(d)-'completed' END ORDER BY d.source_key,d.domain) FROM core.legacy_user_deferred_domains d JOIN core.legacy_user_references r ON r.source_key=d.source_key WHERE r.owner=ANY($3::text[])),'[]'))`

func passGroupEvidence(rows []PassPlanRecord) []PassPlanRecord {
	result := make([]PassPlanRecord, len(rows))
	for index, row := range rows {
		row.Record = passRuntimeRecord(row)
		result[index] = row
	}
	return result
}

func reconcilePassCatalog(ctx context.Context, tx pgx.Tx, event *EventCandidate, bot int64) error {
	if event == nil {
		return errors.New("apply_event_identity_unresolved")
	}
	titles, _ := json.Marshal(event.Titles)
	var matched bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_events e WHERE e.id=$1 AND e.finishes_at=$2 AND e.passport_required=$3 AND e.assignment_rule=$4 AND e.disable_concurrency_limit=$5 AND e.titles=$6::jsonb AND (SELECT count(*) FROM core.pass_event_tiers WHERE event_id=e.id)=$7 AND (SELECT count(*) FROM core.pass_payment_admins WHERE event_id=e.id)=$8)`, event.ID, event.FinishesAt, event.PassportRequired, event.AssignmentRule, event.DisableConcurrencyLimit, titles, len(event.Tiers), len(event.Admins)).
		Scan(&matched)
	if err != nil || !matched {
		return errors.New("apply_event_reconciliation_failed")
	}
	if err = reconcileEventTiers(ctx, tx, event); err != nil {
		return err
	}
	if err = reconcileEventConfiguration(ctx, tx, event); err != nil {
		return err
	}
	return reconcileEventAdmins(ctx, tx, bot, event)
}

func passTimeSince(at, since *time.Time) *time.Time {
	if at == nil || since == nil || at.Before(*since) {
		return nil
	}
	return at
}

// An attempt has a single actor only when all participant records agree.
func passSharedActors(rows []PassPlanRecord, owners map[string]string) (*string, *string) {
	receiverValue := passOwner(owners, rows[0].Candidate.ReceivingAdmin)
	receiver := passActor(owners, rows[0].Candidate.ReceivingAdmin)
	_, _, reviewer := passReview(rows[0].Candidate, owners)
	for _, row := range rows[1:] {
		if passOwner(owners, row.Candidate.ReceivingAdmin) != receiverValue {
			receiver = nil
		}
		_, _, other := passReview(row.Candidate, owners)
		if reviewer == nil || other == nil || *reviewer != *other {
			reviewer = nil
		}
	}
	return receiver, reviewer
}

func passActor(owners map[string]string, id int64) *string {
	if id == 0 {
		return nil
	}
	actor := passOwner(owners, id)
	return &actor
}
