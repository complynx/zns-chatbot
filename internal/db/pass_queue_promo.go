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
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type PromoPriority struct {
	SelectedAt time.Time `json:"selected_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	TierID     string    `json:"tier_id"`
}

func selectPassTicketTx(ctx context.Context, tx pgx.Tx, event events.Event, userID uuid.UUID, role string) error {
	q := sqlc.New(tx)
	eventID := event.ID
	rules := event.Document.Registration
	if !rules.AutoAssign || rules.Pricing == nil {
		return nil
	}
	now, err := registrationMutationTime(ctx, q, rules.ClosesAt, pgtype.Timestamptz{})
	if err != nil {
		return err
	}
	var eligible bool
	err = tx.QueryRow(ctx, `
 SELECT NOT EXISTS(SELECT 1 FROM shuttle_registrations WHERE event_id=$1 AND user_id=$2)
 AND NOT EXISTS(SELECT 1 FROM shuttle_pass_invites WHERE event_id=$1 AND claimant_user_id=$2
   AND NOT revoked AND pair_id IS NULL)
`, eventID, userID).Scan(&eligible)
	if err != nil || !eligible {
		return err
	}
	if err = awardPassPromo(ctx, tx, event, now.Time); err != nil {
		return err
	}
	tiers, err := activePromoTiers(ctx, q, event, now.Time, role)
	if err != nil || len(tiers) == 0 {
		return err
	}
	expires := now.Time.Add(time.Hour)
	if rules.ClosesAt.Before(expires) {
		expires = rules.ClosesAt
	}
	_, err = tx.Exec(ctx, `
 INSERT INTO shuttle_pass_promo_choices(event_id,user_id,tier_id,selected_at,expires_at,eligible_role)
 SELECT $1,$2,t.tier_id,$3,$4,CASE WHEN count(*)=1 THEN min(t.role) END
 FROM (VALUES ('leader'::text,$5::text),('follower',$6::text)) t(role,tier_id)
 WHERE t.tier_id<>'' AND NOT EXISTS (
   SELECT 1 FROM shuttle_pass_promo_choices WHERE event_id=$1 AND user_id=$2 AND NOT forfeited
   AND expires_at>$3
 ) GROUP BY t.tier_id ON CONFLICT DO NOTHING
`, eventID, userID, now.Time, expires, tiers[registrations.Leader], tiers[registrations.Follower])
	return err
}

// Each role keeps the tier available at the click, even if the participant
// chooses their role later. The completed registration can use only one.
func activePromoTiers(
	ctx context.Context, q *sqlc.Queries, event events.Event, now time.Time, selectedRole string,
) (map[string]string, error) {
	rules := *event.Document.Registration
	rules.AutoAssign = false // Include promo tiers in the single-role price resolver.
	usage, err := tierUsage(ctx, q, event.ID)
	if err != nil {
		return nil, err
	}
	tiers := make(map[string]string)
	for _, role := range []string{registrations.Leader, registrations.Follower} {
		if selectedRole != "" && selectedRole != role {
			continue
		}
		prices, quoteErr := registrations.ResolvePrices(rules, now, usage, []string{role})
		if errors.Is(quoteErr, registrations.ErrCapacity) {
			continue
		}
		if quoteErr != nil {
			return nil, quoteErr
		}
		tier := prices[0].Tier
		if rules.Pricing.Tiers[tier.Number-1].Promo {
			tiers[role] = tier.ID
		}
	}
	return tiers, nil
}

func relinquishPassPromo(ctx context.Context, tx pgx.Tx, eventID, userID uuid.UUID) error {
	var awarded bool
	if err := tx.QueryRow(ctx, `
 SELECT EXISTS(SELECT 1 FROM shuttle_pass_promo_choices WHERE event_id=$1 AND user_id=$2
   AND awarded_role IS NOT NULL)
`, eventID, userID).Scan(&awarded); err != nil {
		return err
	}
	if awarded {
		return ErrConflict
	}
	_, err := tx.Exec(ctx, `
 UPDATE shuttle_pass_promo_choices SET forfeited=true WHERE event_id=$1 AND user_id=$2 AND NOT forfeited
`, eventID, userID)
	return err
}

type promoContender struct {
	user               uuid.UUID
	role, eligibleRole string
}

func awardPassPromo(ctx context.Context, tx pgx.Tx, event events.Event, now time.Time) error {
	rules := event.Document.Registration
	if !rules.AutoAssign || rules.Pricing == nil ||
		!slices.ContainsFunc(rules.Pricing.Tiers, func(t registrations.Tier) bool { return t.Promo }) {
		return nil
	}
	usage, err := tierUsage(ctx, sqlc.New(tx), event.ID)
	if err != nil {
		return err
	}
	for i, tier := range rules.Pricing.Tiers {
		if tier.Promo {
			quota := promoQuotaFor(tier, rules.Pricing.Mode, usage)
			if awardErr := awardPromoTier(ctx, tx, event, tier, i+1, now, quota); awardErr != nil {
				return awardErr
			}
		}
	}
	return nil
}

const pairRoleCount = 2

type promoQuota struct {
	capacity   int64
	split      bool
	sold, held map[string]int64
}

func promoQuotaFor(tier registrations.Tier, mode string, usage []registrations.TierUsage) promoQuota {
	q := promoQuota{capacity: int64(tier.Amount), split: mode == registrations.PricingPaired,
		sold: map[string]int64{}, held: map[string]int64{}}
	if q.split {
		q.capacity = q.capacity / pairRoleCount * pairRoleCount
	}
	for _, row := range usage {
		if row.TierID == tier.ID {
			q.sold[row.Role] += row.Count
		}
	}
	return q
}

// available returns the remaining places and the earlier clicks that could claim them.
func (q promoQuota) available(role string) (int64, int64) {
	if q.split {
		return q.capacity/pairRoleCount - q.sold[role], q.held[""] + q.held[role]
	}
	return q.capacity - q.sold[registrations.Leader] - q.sold[registrations.Follower],
		q.held[""] + q.held[registrations.Leader] + q.held[registrations.Follower]
}

func awardPromoTier(
	ctx context.Context,
	tx pgx.Tx,
	event events.Event,
	tier registrations.Tier,
	ordinal int,
	now time.Time,
	quota promoQuota,
) error {
	rows, err := tx.Query(ctx, `
 SELECT c.user_id,COALESCE(r.role,''),COALESCE(c.eligible_role,'')
 FROM shuttle_pass_promo_choices c JOIN shuttle_users u ON u.id=c.user_id AND NOT u.revoked
 LEFT JOIN shuttle_registrations r ON r.event_id=c.event_id AND r.user_id=c.user_id
 WHERE c.event_id=$1 AND c.tier_id=$2 AND NOT c.forfeited AND c.awarded_role IS NULL
 AND ((r.id IS NULL AND c.expires_at>$3) OR (r.state='waitlisted' AND r.requested_at>=c.selected_at
   AND r.requested_at<c.expires_at))
 AND NOT EXISTS(SELECT 1 FROM shuttle_active_pair_members m WHERE m.registration_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM shuttle_pair_invitations p WHERE p.inviter_registration_id=r.id
   AND p.state='pending')
 AND NOT EXISTS(SELECT 1 FROM shuttle_pass_invites p WHERE p.event_id=c.event_id AND NOT p.revoked
   AND p.pair_id IS NULL AND (p.inviter_registration_id=r.id OR p.claimant_user_id=c.user_id))
 ORDER BY c.selected_at,c.sequence
`, event.ID, tier.ID, now)
	if err != nil {
		return err
	}
	contenders, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (promoContender, error) {
		var c promoContender
		scanErr := row.Scan(&c.user, &c.role, &c.eligibleRole)
		return c, scanErr
	})
	if err != nil {
		return err
	}
	err = awardContenders(ctx, tx, event, tier, ordinal, contenders, quota)
	if err != nil {
		return err
	}
	if quota.sold[registrations.Leader]+quota.sold[registrations.Follower] >= quota.capacity {
		_, err = tx.Exec(ctx, `
 UPDATE shuttle_pass_promo_choices SET forfeited=true WHERE event_id=$1 AND tier_id=$2 AND NOT forfeited
   AND awarded_role IS NULL
`, event.ID, tier.ID)
		if err != nil {
			return err
		}
	}
	return nil
}

func awardContenders(
	ctx context.Context,
	tx pgx.Tx,
	event events.Event,
	tier registrations.Tier,
	ordinal int,
	contenders []promoContender,
	quota promoQuota,
) error {
	var err error
	for _, c := range contenders {
		if c.role == "" {
			quota.held[c.eligibleRole]++
			continue
		}
		available, earlier := quota.available(c.role)
		if available <= 0 || (c.eligibleRole != "" && c.eligibleRole != c.role) {
			_, err = tx.Exec(ctx, `
 UPDATE shuttle_pass_promo_choices SET forfeited=true WHERE event_id=$1 AND user_id=$2 AND tier_id=$3
`, event.ID, c.user, tier.ID)
		} else if earlier < available {
			_, err = tx.Exec(ctx, `
 UPDATE shuttle_pass_promo_choices SET awarded_role=$4,tier_number=$5,price_minor=$6 WHERE event_id=$1
   AND user_id=$2 AND tier_id=$3
`, event.ID, c.user, tier.ID, c.role, ordinal, tier.PriceMinor)
			quota.sold[c.role]++
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func soloPassPrices(
	ctx context.Context,
	tx pgx.Tx,
	q *sqlc.Queries,
	event events.Event,
	now time.Time,
	row sqlc.ShuttleRegistration,
) ([]registrations.PriceQuote, error) {
	// A completed promo award survives later changes to automatic allocation and tier policy.
	var price registrations.PriceQuote
	var tier registrations.TierSnapshot
	err := tx.QueryRow(ctx, `
 SELECT tier_id,tier_number,price_minor FROM shuttle_pass_promo_choices WHERE event_id=$1 AND user_id=$2
   AND awarded_role=$3
`, event.ID, row.UserID, row.Role).Scan(&tier.ID, &tier.Number, &price.PriceMinor)
	if err == nil {
		price.Tier = &tier
		return []registrations.PriceQuote{price}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if event.Document.Registration.AutoAssign {
		var waiting bool
		err = tx.QueryRow(ctx, `
 SELECT EXISTS(SELECT 1 FROM shuttle_pass_promo_choices WHERE event_id=$1 AND user_id=$2 AND NOT forfeited
   AND (eligible_role IS NULL OR eligible_role=$4)
   AND awarded_role IS NULL AND $3>=selected_at AND $3<expires_at AND tier_id IN(SELECT t->>'id'
   FROM shuttle_event_registration_rules e CROSS
   JOIN LATERAL jsonb_array_elements(e.document->'pricing'->'tiers') t WHERE e.event_id=$1
   AND t->>'promo'='true'))
`, event.ID, row.UserID, row.RequestedAt.Time, row.Role).Scan(&waiting)
		if err != nil {
			return nil, err
		}
		if waiting {
			return nil, registrations.ErrCapacity
		}
	}
	return assignmentPrices(ctx, q, event, now, []string{row.Role}, false)
}
