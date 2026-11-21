package db

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type PassQueuePosition struct{ Individual, Role, Pair int64 }

// Use the same individual order for participant positions and the organizer roster.
const passQueueRanksSQL = `WITH waiting AS (
 SELECT r.id,r.user_id,r.role,COALESCE(w.selected_at,r.requested_at) AS priority
 FROM shuttle_registrations r JOIN shuttle_users u ON u.id=r.user_id AND NOT u.revoked
 LEFT JOIN shuttle_pass_promo_choices w ON w.event_id=r.event_id AND w.user_id=r.user_id
   AND w.awarded_role IS NOT NULL
 WHERE r.event_id=$1 AND r.state='waitlisted'
), ranks AS (
 SELECT id,user_id,row_number() OVER(ORDER BY priority,id) AS pos,
 row_number() OVER(PARTITION BY role ORDER BY priority,id) AS role_pos FROM waiting
) `

func (s *Store) PassQueuePosition(
	ctx context.Context,
	scope PaymentScope,
	eventID uuid.UUID,
) (PassQueuePosition, error) {
	var p PassQueuePosition
	err := s.pool.QueryRow(ctx, passQueueRanksSQL+`
 SELECT owner.pos,owner.role_pos,COALESCE(least(l.pos,f.pos),0) FROM ranks owner
 JOIN shuttle_events e ON e.id=$1 AND e.bot_id=$2 AND e.organization_id=$3
 JOIN shuttle_users u ON u.id=owner.user_id AND u.zitadel_subject=$5
 LEFT JOIN shuttle_active_pair_members m ON m.registration_id=owner.id
 LEFT JOIN shuttle_pairs pair ON pair.id=m.pair_id LEFT JOIN ranks l ON l.id=pair.leader_registration_id
 LEFT JOIN ranks f ON f.id=pair.follower_registration_id WHERE owner.user_id=$4
`, eventID, scope.BotID, scope.Organization, scope.UserID, scope.Subject).Scan(&p.Individual, &p.Role, &p.Pair)
	return p, err
}

type passQueueCandidate struct {
	id       uuid.UUID
	role     string
	pair     PairAssignment
	wonPromo bool
}

func passQueueCandidates(ctx context.Context, tx pgx.Tx, event events.Event) ([]passQueueCandidate, error) {
	var required registrationdetails.Requirements
	if requirements := event.Document.Registration.DetailsRequirements; requirements != nil {
		required = requirements.Normalize()
	}
	rows, err := tx.Query(ctx, `
 SELECT r.id,r.role,p.id,p.version,l.version,f.version,w.awarded_role IS NOT NULL
 FROM shuttle_registrations r
 JOIN shuttle_users u ON u.id=r.user_id AND NOT u.revoked
 LEFT JOIN shuttle_active_pair_members m ON m.registration_id=r.id
 LEFT JOIN shuttle_pairs p ON p.id=m.pair_id
 LEFT JOIN shuttle_registrations l ON l.id=p.leader_registration_id
 LEFT JOIN shuttle_registrations f ON f.id=p.follower_registration_id
 LEFT JOIN shuttle_pass_promo_choices w ON w.event_id=r.event_id AND w.user_id=r.user_id
   AND w.awarded_role IS NOT NULL
 WHERE r.event_id=$1 AND r.state='waitlisted' AND (m.pair_id IS NULL OR (m.role='leader'
   AND p.state='waitlisted'))
 AND NOT EXISTS (SELECT 1 FROM shuttle_pair_invitations i WHERE i.inviter_registration_id=r.id
   AND i.state='pending')
 AND NOT EXISTS (SELECT 1 FROM shuttle_pass_invites i WHERE i.event_id=r.event_id AND NOT i.revoked
   AND i.pair_id IS NULL
 AND (i.inviter_registration_id=r.id OR i.claimant_user_id=r.user_id OR i.replace_pair_id=p.id))
 AND NOT EXISTS (
   SELECT 1 FROM (VALUES (r.user_id), (f.user_id)) AS member(user_id)
   LEFT JOIN shuttle_registration_details d ON d.user_id=member.user_id
   WHERE member.user_id IS NOT NULL AND (
     ($2 AND d.legal_name_present IS NOT TRUE) OR ($3 AND d.passport_number_present IS NOT TRUE)
     OR ($4 AND d.passport_country_present IS NOT TRUE) OR ($5 AND d.passport_photo_present IS NOT TRUE)))
 ORDER BY COALESCE(w.selected_at,least(l.requested_at,f.requested_at),r.requested_at),r.id
`, event.ID, required.LegalName, required.PassportNumber, len(required.PassportCountries) > 0, required.PassportPhoto)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (passQueueCandidate, error) {
		var c passQueueCandidate
		var pairID *uuid.UUID
		var pv, lv, fv *int64
		scanErr := row.Scan(&c.id, &c.role, &pairID, &pv, &lv, &fv, &c.wonPromo)
		if scanErr == nil && pairID != nil {
			c.pair = PairAssignment{
				EventID:                 event.ID,
				PairID:                  *pairID,
				ExpectedEventVersion:    event.Version,
				ExpectedPairVersion:     *pv,
				ExpectedLeaderVersion:   *lv,
				ExpectedFollowerVersion: *fv,
			}
		}
		return c, scanErr
	})
}

func queueBusinessConflict(err error) bool {
	return errors.Is(err, registrations.ErrCapacity) || errors.Is(err, ErrConflict) ||
		errors.Is(err, registrationdetails.ErrRequired) ||
		errors.Is(err, pgx.ErrNoRows)
}

// AllocatePassQueue commits at most one eligible unit under the same event lock as
// manual assignments. Two unrelated solo passes remain independent registrations.
func (s *Store) AllocatePassQueue(ctx context.Context, scope EventScope, eventID uuid.UUID) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	// Let other events take a turn if a concurrent transaction holds a required lock.
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout = '1s'"); err != nil {
		return 0, err
	}
	q := s.q.WithTx(tx)
	event, err := registrationEventForChange(ctx, q, scope, eventID)
	if err != nil {
		return 0, err
	}
	rules := event.Document.Registration
	if !rules.AutoAssign {
		return 0, nil
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return 0, err
	}
	if err = awardPassPromo(ctx, tx, event, now.Time); err != nil {
		return 0, err
	}
	candidates, err := passQueueCandidates(ctx, tx, event)
	if err != nil {
		return 0, err
	}
	leaders, err := q.CountAssignedRole(ctx, sqlc.CountAssignedRoleParams{EventID: eventID, Role: registrations.Leader})
	if err != nil {
		return 0, err
	}
	followers, err := q.CountAssignedRole(
		ctx,
		sqlc.CountAssignedRoleParams{EventID: eventID, Role: registrations.Follower},
	)
	if err != nil {
		return 0, err
	}
	allocator := passQueueAllocator{ctx: ctx, tx: tx, scope: scope, event: event, leaders: leaders, followers: followers,
		priceTime: now.Time, ordinaryPrices: make(map[[2]string]bool)}
	n, err := allocator.allocate(candidates)
	if err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
}

type passQueueAllocator struct {
	ctx                context.Context
	tx                 pgx.Tx
	scope              EventScope
	event              events.Event
	leaders, followers int64
	priceTime          time.Time
	ordinaryPrices     map[[2]string]bool
}

// Ordinary solos share tier availability until this transaction commits its first successful unit.
func (a passQueueAllocator) canPriceOrdinary(members []passQueueCandidate) (bool, error) {
	if a.event.Document.Registration.Pricing == nil || members[0].wonPromo || members[0].pair.PairID != uuid.Nil {
		return true, nil
	}
	var roles [2]string
	for i, m := range members {
		roles[i] = m.role
	}
	if available, known := a.ordinaryPrices[roles]; known {
		return available, nil
	}
	_, err := assignmentPrices(a.ctx, sqlc.New(a.tx), a.event, a.priceTime, roles[:len(members)], true)
	if err != nil && !errors.Is(err, registrations.ErrCapacity) {
		return false, err
	}
	available := err == nil
	a.ordinaryPrices[roles] = available
	return available, nil
}

// try rolls back the whole unit and identifies the member whose assignment failed.
func (a passQueueAllocator) try(members ...passQueueCandidate) (int, uuid.UUID, error) {
	available, err := a.canPriceOrdinary(members)
	if err != nil || !available {
		return 0, uuid.Nil, err
	}
	tx, err := a.tx.Begin(a.ctx)
	if err != nil {
		return 0, uuid.Nil, err
	}
	defer rollbackLegacyArchive(a.ctx, tx)
	count := 0
	for _, member := range members {
		n, assignErr := assignQueueCandidate(a.ctx, tx, a.scope, a.event, member)
		if queueBusinessConflict(assignErr) {
			return 0, member.id, nil
		}
		if assignErr != nil {
			return 0, uuid.Nil, assignErr
		}
		count += n
	}
	return count, uuid.Nil, tx.Commit(a.ctx)
}

func (a passQueueAllocator) allocate(candidates []passQueueCandidate) (int, error) {
	// Index opposite-role solos once. A one-role waiting list is common.
	solos := map[string][]int{}
	for i, c := range candidates {
		if c.pair.PairID == uuid.Nil && !c.wonPromo {
			solos[c.role] = append(solos[c.role], i)
		}
	}
	for i, c := range candidates {
		n, err := a.tryInQueueOrder(i, c, candidates, solos)
		if err != nil || n > 0 {
			return n, err
		}
	}
	return 0, nil
}

func (a passQueueAllocator) tryInQueueOrder(
	i int,
	c passQueueCandidate,
	candidates []passQueueCandidate,
	solos map[string][]int,
) (int, error) {
	rules := a.event.Document.Registration
	dl, df := int64(0), int64(0)
	opposite := registrations.Leader
	switch {
	case c.pair.PairID != uuid.Nil:
		dl, df = 1, 1
	case c.role == registrations.Leader:
		dl, opposite = 1, registrations.Follower
	default:
		df = 1
	}
	if dl > 0 && a.leaders >= int64(rules.LeaderCapacity) ||
		df > 0 && a.followers >= int64(rules.FollowerCapacity) {
		return 0, nil
	}
	if c.wonPromo || rules.Allows(a.leaders, a.followers, dl, df) {
		n, _, err := a.try(c)
		return n, err
	}
	if c.pair.PairID != uuid.Nil || a.leaders >= int64(rules.LeaderCapacity) ||
		a.followers >= int64(rules.FollowerCapacity) ||
		!rules.Allows(a.leaders, a.followers, 1, 1) {
		return 0, nil
	}
	indices := solos[opposite]
	start, _ := slices.BinarySearch(indices, i+1)
	for pos, j := range indices[start:] {
		n, blocked, err := a.try(c, candidates[j])
		if n > 0 || err != nil || blocked != candidates[j].id {
			return n, err
		}
		// This counterpart failed its own checks; later owners need not retry it in this scan.
		solos[opposite] = indices[start+pos+1:]
	}
	return 0, nil
}

func assignQueueCandidate(
	ctx context.Context,
	tx pgx.Tx,
	scope EventScope,
	event events.Event,
	c passQueueCandidate,
) (int, error) {
	q := sqlc.New(tx)
	if c.pair.PairID != uuid.Nil {
		candidate, err := checkPairAssignment(ctx, q, PaymentScope{EventScope: scope}, c.pair, pgtype.Timestamptz{})
		if err != nil {
			return 0, err
		}
		_, err = writePairPasses(ctx, q, candidate)
		return pairRoleCount, err
	}
	row, err := q.GetRegistrationByID(ctx, sqlc.GetRegistrationByIDParams{ID: c.id, EventID: event.ID})
	if err != nil {
		return 0, err
	}
	if row.State != registrations.Waitlisted {
		return 0, ErrConflict
	}
	if _, err = q.LockRegistrationUser(
		ctx,
		sqlc.LockRegistrationUserParams{ID: row.UserID, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	); err != nil {
		return 0, err
	}
	now, err := registrationMutationTime(ctx, q, event.Document.Registration.ClosesAt, pgtype.Timestamptz{})
	if err != nil {
		return 0, err
	}
	prices, err := soloPassPrices(ctx, tx, q, event, now.Time, row)
	if err != nil {
		return 0, err
	}
	quotes, err := assignmentQuotes(event, prices, row.ID)
	if err != nil {
		return 0, err
	}
	_, err = writeAssignedPass(ctx, q, event, row, now, quotes[0])
	return 1, err
}

// AdvancePassQueues visits open events in UUID pages. The next tick rereads durable state.
func (s *Store) AdvancePassQueues(ctx context.Context, scope EventScope, after uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
 SELECT e.id FROM shuttle_events e JOIN shuttle_event_registration_rules x ON x.event_id=e.id
 WHERE e.bot_id=$1 AND e.organization_id=$2 AND e.id>$3 AND x.document->>'auto_assign'='true'
   AND 'registrations'=ANY(e.modules)
 AND (x.document->>'closes_at')::timestamptz>clock_timestamp()
 AND coalesce((x.document->>'sales_opens_at')::timestamptz,'-infinity')<=clock_timestamp()
 AND EXISTS(SELECT 1 FROM shuttle_registrations r WHERE r.event_id=e.id AND r.state='waitlisted')
 ORDER BY e.id LIMIT 1
`, scope.BotID, scope.Organization, after).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return after, err
	}
	// Yield between committed units. Interrupting a slow, ordered scan restarts it from the same prefix.
	start := time.Now()
	for range 16 {
		n, allocateErr := s.AllocatePassQueue(ctx, scope, id)
		if allocateErr != nil {
			return id, allocateErr
		}
		if n == 0 || time.Since(start) >= time.Second {
			break
		}
	}
	return id, nil
}
