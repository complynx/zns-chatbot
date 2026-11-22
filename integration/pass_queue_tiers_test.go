package integration_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/migrations"
)

func TestLoweredRoleCapacityStillAdmitsAnOppositeSolo(t *testing.T) {
	t.Parallel()
	tier := registrations.Tier{
		ID: "regular", Amount: 10, PriceMinor: 10000, StartsAt: time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute),
	}
	f := newPassQAFixture(t, []registrations.Tier{tier}, false)
	register := func(id int64, role string) passQAUser {
		user := f.namedUser(t, id)
		task, _ := f.accept(t, user, "/passes synthetic --role "+role+" --solo", 0, "")
		f.h.await(t, task, "completed")
		return user
	}
	for _, id := range []int64{26771, 26772} {
		f.pass(t, register(id, registrations.Leader), tier.ID, tier.PriceMinor)
	}
	doc := f.event.Document
	doc.Registration.LeaderCapacity = 1
	doc.Registration.MaxLeaderPercent = 52
	doc.Registration.MaxFollowerPercent = 52
	f.saveEvent(t, doc)
	waiting := register(26773, registrations.Leader)
	follower := register(26774, registrations.Follower)
	f.pass(t, follower, tier.ID, tier.PriceMinor)
	require.Equal(t, registrations.Waitlisted, f.registration(t, waiting).State)
}

func TestNextPromoTierOpensWhenFirstPromoFills(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{registrations.PricingDistributed, registrations.PricingPaired} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
			places := int32(1)
			if mode == registrations.PricingPaired {
				places = 2
			}
			tiers := []registrations.Tier{
				{ID: "first", Amount: places, PriceMinor: 10000, Promo: true, StartsAt: start},
				{ID: "next", Amount: places, PriceMinor: 12000, Promo: true, StartsAt: start.Add(time.Hour)},
			}
			f := newPassQAFixture(t, tiers, false)
			doc := f.event.Document
			doc.Registration.Pricing.Mode = mode
			f.saveEvent(t, doc)
			for i, tier := range tiers {
				user := f.namedUser(t, int64(26701+i))
				f.solo(t, user, f.click(t, user, f.menu(t, user)))
				f.pass(t, user, tier.ID, tier.PriceMinor)
			}
			if mode == registrations.PricingPaired {
				follower := f.namedUser(t, 26703)
				task, _ := f.accept(t, follower, "/passes synthetic --role follower --solo", 0, "")
				f.h.await(t, task, "completed")
				f.pass(t, follower, tiers[0].ID, tiers[0].PriceMinor)
			}
		})
	}
}

func TestAwardedPromoKeepsItsMonetaryTermsUntilAssignment(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	f := newPassQAFixture(t, passQAPromoTiers(start), false)
	doc := f.event.Document
	doc.Registration.LeaderCapacity = 0
	f.saveEvent(t, doc)
	owner := f.namedUser(t, 26711)
	f.solo(t, owner, f.click(t, owner, f.menu(t, owner)))
	entry := f.registration(t, owner)
	require.Nil(t, entry.Pass)
	var price int64
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(),
		"SELECT price_minor FROM shuttle_pass_promo_choices WHERE event_id=$1 AND user_id=$2 AND awarded_role=$3",
		f.event.ID, owner.app.UserID, entry.Role).Scan(&price))
	require.EqualValues(t, 7000, price)

	for _, edit := range []func(*events.Document){
		func(d *events.Document) { d.Registration.Currency = "USD" },
		func(d *events.Document) { d.Registration.CurrencyScale = 0 },
		func(d *events.Document) { d.Registration = nil },
		func(d *events.Document) {
			d.Registration.Pricing = nil
			d.Registration.PriceMinor = 23456
			d.Deposit = &registrations.DepositPolicy{MinimumMinor: price + 1}
		},
	} {
		changed := f.event.Document
		rules := *changed.Registration
		changed.Registration = &rules
		edit(&changed)
		_, err := f.h.store.ChangeEvent(t.Context(), f.scope, "sale-qa", uuid.NewString(),
			db.EventChange{ID: f.event.ID, ExpectedVersion: f.event.Version, Document: changed})
		require.ErrorIs(t, err, db.ErrConflict)
	}

	doc = f.event.Document
	doc.Registration.AutoAssign = false
	doc.Registration.LeaderCapacity = 1
	doc.Registration.Pricing = nil
	doc.Registration.PriceMinor = 23456
	doc.Deposit = &registrations.DepositPolicy{MinimumMinor: price}
	f.saveEvent(t, doc)
	args := db.PassAssignment{
		EventID:              f.event.ID,
		RegistrationID:       entry.ID,
		ExpectedEventVersion: f.event.Version,
		ExpectedVersion:      entry.Version,
	}
	operation := uuid.NewString()
	prepared, err := f.h.store.PrepareAssignment(t.Context(), f.scope, "sale-qa", operation, args)
	require.NoError(t, err)
	assigned, err := f.h.store.ConfirmAssignment(t.Context(), f.scope, "sale-qa", operation, args, prepared.ID)
	require.NoError(t, err)
	require.Equal(t, price, assigned.Pass.PriceMinor)
	require.NotNil(t, assigned.Pass.Deposit)
	require.Equal(t, price, assigned.Pass.Deposit.RequiredMinor)
	require.Equal(t, "BYN", assigned.Pass.Currency)
	require.EqualValues(t, 2, assigned.Pass.CurrencyScale)
	require.Equal(t, "promo", assigned.Pass.Tier.ID)

	doc = f.event.Document
	doc.Registration.Currency = "USD"
	doc.Registration.CurrencyScale = 0
	f.saveEvent(t, doc)
	require.Equal(t, assigned.Pass, f.registration(t, owner).Pass)
}

func TestManualAssignmentCountsPromoAwardAcrossItsLifecycle(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{registrations.PricingDistributed, registrations.PricingPaired} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f, winner := waitlistedPromoFixture(t, mode)
			join := func(role string, id int64) registrations.Registration {
				user := f.namedUser(t, id)
				input, _ := f.accept(t, user, "/passes synthetic --role "+role+" --solo", 0, "")
				f.h.await(t, input, "completed")
				return f.registration(t, user)
			}
			same := join(registrations.Leader, 26722)
			opposite := join(registrations.Follower, 26723)
			args := func(r registrations.Registration) db.PassAssignment {
				return db.PassAssignment{
					EventID:              f.event.ID,
					RegistrationID:       r.ID,
					ExpectedVersion:      r.Version,
					ExpectedEventVersion: f.event.Version,
				}
			}
			checkQuota := func() {
				t.Helper()
				_, err := f.h.store.PrepareAssignment(t.Context(), f.scope, "sale-qa", uuid.NewString(), args(same))
				require.ErrorIs(t, err, registrations.ErrCapacity)
				quote, err := f.h.store.PrepareAssignment(t.Context(), f.scope, "sale-qa", uuid.NewString(), args(opposite))
				if mode == registrations.PricingDistributed {
					require.ErrorIs(t, err, registrations.ErrCapacity)
				} else {
					require.NoError(t, err)
					require.Equal(t, "promo", quote.Quotes[0].Tier.ID)
				}
				usage, err := sqlc.New(f.h.store.Pool()).ListTierUsage(t.Context(), f.event.ID)
				require.NoError(t, err)
				require.Equal(t, []sqlc.ListTierUsageRow{{Role: registrations.Leader, TierID: "promo", Count: 1}}, usage)
			}
			checkQuota()
			op := uuid.NewString()
			quote, err := f.h.store.PrepareAssignment(t.Context(), f.scope, "sale-qa", op, args(winner))
			require.NoError(t, err)
			issued, err := f.h.store.ConfirmAssignment(t.Context(), f.scope, "sale-qa", op, args(winner), quote.ID)
			require.NoError(t, err)
			require.Equal(t, "promo", issued.Pass.Tier.ID)
			checkQuota()
			f.p.mu.Lock()
			f.p.adminRole = true
			f.p.mu.Unlock()
			admin := detailsAppSession(t, f.h, 26729, 238)
			makePassAdminChange(t, f.h, admin, f.event.ID, "return", db.PassAdminChange{
				RegistrationID:  issued.ID,
				OperationID:     uuid.New(),
				ExpectedVersion: issued.Version,
				Reason:          "Canceled fixture booking",
			}, true, http.StatusOK)
			checkQuota()
		})
	}
}

func waitlistedPromoFixture(t *testing.T, mode string) (*passQAFixture, registrations.Registration) {
	t.Helper()
	tiers := passQAPromoTiers(time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute))
	if mode == registrations.PricingPaired {
		tiers[0].Amount = 2
	}
	f := newPassQAFixture(t, tiers, false)
	doc := f.event.Document
	doc.Registration.Pricing.Mode = mode
	doc.Registration.LeaderCapacity = 0
	f.saveEvent(t, doc)
	owner := f.namedUser(t, 26721)
	f.solo(t, owner, f.click(t, owner, f.menu(t, owner)))
	winner := f.registration(t, owner)
	require.Nil(t, winner.Pass)
	doc = f.event.Document
	doc.Registration.AutoAssign = false
	doc.Registration.LeaderCapacity = 20
	f.saveEvent(t, doc)
	return f, winner
}

func TestManualCodePairUsesOrdinaryOwnerPrices(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		dated bool
	}{{"dated", true}, {"no_sale_flags", false}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkManualCodePairPrices(t, test.dated)
		})
	}
}

func checkManualCodePairPrices(t *testing.T, dated bool) {
	t.Helper()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	f := newPassQAFixture(t, []registrations.Tier{
		{ID: "early", Amount: 1, PriceMinor: 8000, StartsAt: start},
		{ID: "regular", Amount: 8, PriceMinor: 12000, StartsAt: start.Add(time.Hour)},
	}, false)
	doc := f.event.Document
	doc.Registration.AutoAssign = false
	if !dated {
		doc.Registration.SalesOpensAt = nil
	}
	f.saveEvent(t, doc)
	follower := f.namedUser(t, 26801)
	leader := f.namedUser(t, 26802)
	send := func(u passQAUser, command string) {
		task, _ := f.accept(t, u, command, 0, "")
		f.h.await(t, task, "completed")
	}
	send(follower, "/passes synthetic --role follower --solo")
	send(leader, "/passes synthetic --role leader --pair")
	invite, err := f.h.store.ActivePassInvite(t.Context(), paymentScope(t, f.h, leader.app.UserID, false), f.event.ID)
	require.NoError(t, err)
	send(follower, "/passes synthetic --invite "+invite.Code)
	leaderEntry, followerEntry := f.registration(t, leader), f.registration(t, follower)
	require.True(t, followerEntry.RequestedAt.Before(leaderEntry.RequestedAt))
	args := db.PairAssignment{EventID: f.event.ID, ExpectedEventVersion: f.event.Version,
		ExpectedLeaderVersion: leaderEntry.Version, ExpectedFollowerVersion: followerEntry.Version}
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(),
		"SELECT p.id,p.version FROM shuttle_pairs p JOIN shuttle_pass_invites i ON i.pair_id=p.id "+
			"WHERE i.event_id=$1 AND i.code=$2",
		f.event.ID, invite.Code).Scan(&args.PairID, &args.ExpectedPairVersion))
	adminID, _ := registrationUser(t, f.h, 26803, true)
	admin := paymentScope(t, f.h, adminID, true)
	op := uuid.NewString()
	prep, err := f.h.store.PreparePairAssignment(t.Context(), admin, op, args)
	require.NoError(t, err)
	require.EqualValues(t, 12000, prep.Quotes[0].PriceMinor)
	require.EqualValues(t, 8000, prep.Quotes[1].PriceMinor)
	assigned, err := f.h.store.ConfirmPairAssignment(t.Context(), admin, op, args, prep.ID)
	require.NoError(t, err)
	require.EqualValues(t, 12000, assigned.Leader.Pass.PriceMinor)
	require.EqualValues(t, 8000, assigned.Follower.Pass.PriceMinor)
}

func TestManualPromoReturnKeepsConsumptionButReleasesRoleCapacity(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	tiers := []registrations.Tier{
		{ID: "limited", Amount: 1, PriceMinor: 7000, Promo: true, StartsAt: start},
		{ID: "ordinary", Amount: 1, PriceMinor: 9000, StartsAt: start.Add(time.Hour)},
		{ID: "later", Amount: 10, PriceMinor: 11000, StartsAt: start.Add(2 * time.Hour)},
	}
	f := newPassQAFixture(t, tiers, false)
	doc := f.event.Document
	doc.Registration.AutoAssign = false
	doc.Registration.LeaderCapacity = 1
	f.saveEvent(t, doc)
	issue := func(id int64, tier string, price int64) registrations.Registration {
		t.Helper()
		owner := f.namedUser(t, id)
		task, _ := f.accept(t, owner, "/passes synthetic --role leader --solo", 0, "")
		f.h.await(t, task, "completed")
		entry := f.registration(t, owner)
		args := db.PassAssignment{
			EventID:              f.event.ID,
			ExpectedEventVersion: f.event.Version,
			RegistrationID:       entry.ID,
			ExpectedVersion:      entry.Version,
		}
		op := uuid.NewString()
		prep, err := f.h.store.PrepareAssignment(t.Context(), f.scope, "sale-qa", op, args)
		require.NoError(t, err)
		pass, err := f.h.store.ConfirmAssignment(t.Context(), f.scope, "sale-qa", op, args, prep.ID)
		require.NoError(t, err)
		require.Equal(t, tier, pass.Pass.Tier.ID)
		require.Equal(t, price, pass.Pass.PriceMinor)
		return pass
	}
	promo := issue(26901, "limited", 7000)
	f.p.mu.Lock()
	f.p.adminRole = true
	f.p.mu.Unlock()
	admin := detailsAppSession(t, f.h, 26909, 240)
	cancel := func(r registrations.Registration) {
		t.Helper()
		makePassAdminChange(t, f.h, admin, f.event.ID, "return", db.PassAdminChange{RegistrationID: r.ID,
			OperationID: uuid.New(), ExpectedVersion: r.Version, Reason: "Synthetic return"}, true, http.StatusOK)
	}
	edit := func(index int, newID string, promoTier bool) {
		t.Helper()
		d := f.event.Document
		d.Registration.Pricing.Tiers[index].ID = newID
		d.Registration.Pricing.Tiers[index].Promo = promoTier
		f.saveEvent(t, d)
	}
	consumed := func() {
		t.Helper()
		usage, err := sqlc.New(f.h.store.Pool()).ListTierUsage(t.Context(), f.event.ID)
		require.NoError(t, err)
		require.Equal(t, []sqlc.ListTierUsageRow{{Role: registrations.Leader, TierID: "limited", Count: 1}}, usage)
	}
	edit(0, "renamed-first", false)
	cancel(promo)
	consumed()
	regular := issue(26902, "ordinary", 9000)
	edit(1, "renamed-second", true)
	cancel(regular)
	consumed()
	edit(0, "limited", true)
	edit(1, "ordinary", false)
	doc = f.event.Document
	doc.Registration.AutoAssign = true
	f.saveEvent(t, doc)
	next := f.namedUser(t, 26903)
	task, _ := f.accept(t, next, "/passes synthetic --role leader --solo", 0, "")
	f.h.await(t, task, "completed")
	automatic := f.pass(t, next, "ordinary", 9000)
	var clicks int
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_pass_promo_choices WHERE event_id=$1", f.event.ID).
			Scan(&clicks),
	)
	require.Zero(t, clicks)
	edit(0, "archived-first", false)
	edit(1, "ordinary", true)
	requireHistoricalPromoClassification(t, f, promo.Pass.ID, regular.Pass.ID, automatic.Pass.ID)
}

func requireHistoricalPromoClassification(t *testing.T, f *passQAFixture, promo, ordinary, unverified uuid.UUID) {
	t.Helper()
	f.h.stop(t)
	ctx := t.Context()
	pool := f.h.store.Pool()
	// Simulate v89 records, which have the original receipts but no issuance classification.
	_, err := pool.Exec(ctx, "UPDATE shuttle_pass_tiers SET promo=NULL WHERE pass_id=ANY($1::uuid[])",
		[]uuid.UUID{promo, ordinary, unverified})
	require.NoError(t, err)
	driver := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { require.NoError(t, driver.Close()) })
	provider, err := goose.NewProvider(goose.DialectPostgres, driver, migrations.Files)
	require.NoError(t, err)
	// Exercise v90 in isolation when newer, unrelated migrations are already installed.
	_, err = provider.ApplyVersion(ctx, 90, false)
	require.NoError(t, err)
	_, err = provider.ApplyVersion(ctx, 90, true)
	require.NoError(t, err)
	require.NoError(t, f.h.store.Migrate(ctx))
	read := func(passID uuid.UUID) *bool {
		t.Helper()
		var isPromo *bool
		require.NoError(t, pool.QueryRow(ctx, "SELECT promo FROM shuttle_pass_tiers WHERE pass_id=$1", passID).Scan(&isPromo))
		return isPromo
	}
	promoFlag, ordinaryFlag := read(promo), read(ordinary)
	require.NotNil(t, promoFlag)
	require.True(t, *promoFlag)
	require.NotNil(t, ordinaryFlag)
	require.False(t, *ordinaryFlag)
	require.Nil(t, read(unverified))
}

func TestPassQueueSkipsMissingDetailsBeforeLockingCandidates(t *testing.T) {
	t.Parallel()
	tier := registrations.Tier{
		ID:         "ordinary",
		Amount:     20,
		PriceMinor: 7000,
		StartsAt:   time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour),
	}
	f := newPassQAFixture(t, []registrations.Tier{tier}, false)
	doc := f.event.Document
	doc.Registration.LeaderCapacity = 0
	f.saveEvent(t, doc)
	first := f.namedUser(t, 27011)
	next := f.namedUser(t, 27012)
	for _, u := range []passQAUser{first, next} {
		task, _ := f.accept(t, u, "/passes synthetic --role leader --solo", 0, "")
		f.h.await(t, task, "completed")
	}
	status, raw, _ := appCall(t, f.h, http.MethodPost, appDetailsPath+"/passport-number",
		appDetailsBody(t, uuid.New(), 1, "QUEUE-TEST-123"), next.app.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, status, string(raw))
	f.h.stop(t)
	doc = f.event.Document
	doc.Registration.DetailsRequirements = &registrationdetails.Requirements{PassportNumber: true}
	doc.Registration.LeaderCapacity = 1
	f.saveEvent(t, doc)
	// Holding the ineligible owner's row makes retrying the queue prefix exhaust a tick on every visit.
	tx, err := f.h.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback(t.Context())) }()
	_, err = tx.Exec(t.Context(), "SELECT id FROM shuttle_users WHERE id=$1 FOR UPDATE", first.app.UserID)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	n, err := f.h.store.AllocatePassQueue(ctx, f.scope, f.event.ID)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Nil(t, f.registration(t, first).Pass)
	require.NotNil(t, f.registration(t, next).Pass)
}

func TestBalancedSoloQueueChecksBothPricesBeforeIssuingPasses(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	tiers := []registrations.Tier{
		{ID: "last", Amount: 1, PriceMinor: 7000, StartsAt: start},
		{ID: "later", Amount: 4, PriceMinor: 9000, StartsAt: start.Add(time.Hour), BlockedByDate: true},
	}
	f := newPassQAFixture(t, tiers, false)
	doc := f.event.Document
	doc.Registration.AutoAssign = false
	doc.Registration.MaxLeaderPercent = 50
	doc.Registration.MaxFollowerPercent = 50
	f.saveEvent(t, doc)
	roles := []string{registrations.Leader, registrations.Leader, registrations.Follower, registrations.Follower}
	for i, role := range roles {
		u := f.namedUser(t, 27110+int64(i))
		task, _ := f.accept(t, u, "/passes synthetic --role "+role+" --solo", 0, "")
		f.h.await(t, task, "completed")
	}
	f.h.stop(t)
	doc = f.event.Document
	doc.Registration.AutoAssign = true
	f.saveEvent(t, doc)
	ctx := t.Context()
	// Sequence increments survive savepoint rollback and count even speculative pass inserts.
	_, err := f.h.store.Pool().Exec(ctx, `CREATE SEQUENCE qa_pass_attempts;
 CREATE FUNCTION qa_count_pass_insert() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM nextval('qa_pass_attempts'); RETURN NEW; END $$;
 CREATE TRIGGER qa_pass_insert BEFORE INSERT ON shuttle_passes FOR EACH ROW EXECUTE FUNCTION qa_count_pass_insert()`)
	require.NoError(t, err)
	advance := func(want int) {
		t.Helper()
		n, allocErr := f.h.store.AllocatePassQueue(ctx, f.scope, f.event.ID)
		require.NoError(t, allocErr)
		require.Equal(t, want, n)
	}
	advance(0)
	attempts := func() int {
		t.Helper()
		var count int
		require.NoError(
			t,
			f.h.store.Pool().
				QueryRow(ctx, "SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM qa_pass_attempts").
				Scan(&count),
		)
		return count
	}
	require.Zero(t, attempts())
	doc = f.event.Document
	doc.Registration.Pricing.Tiers[1].BlockedByDate = false
	f.saveEvent(t, doc)
	advance(2)
	require.Equal(t, 2, attempts())
}
