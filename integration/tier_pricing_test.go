package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func tierPolicy(mode string, firstAmount int32) *registrations.TierPolicy {
	return &registrations.TierPolicy{Mode: mode, Tiers: []registrations.Tier{
		{
			ID:         "early",
			Amount:     firstAmount,
			PriceMinor: 10000,
			StartsAt:   time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour),
		},
		{
			ID:         "regular",
			Amount:     20,
			PriceMinor: 20000,
			StartsAt:   time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour),
		},
	}}
}

func configureTierPair(
	t *testing.T,
	f pairFixture,
	admin db.PaymentScope,
	args *db.PairAssignment,
	policy *registrations.TierPolicy,
) events.Event {
	t.Helper()
	ctx := context.Background()
	event, err := f.H.store.Event(ctx, admin.EventScope, f.EventID)
	require.NoError(t, err)
	event.Document.Registration.LeaderCapacity = 10
	event.Document.Registration.FollowerCapacity = 10
	event.Document.Registration.Pricing = policy
	event.Document.Deposit = &registrations.DepositPolicy{MinimumMinor: 3000, MinimumBPS: 2500}
	event, err = f.H.store.ChangeEvent(ctx, admin.EventScope, admin.Subject, "tier-config", db.EventChange{
		ID: event.ID, ExpectedVersion: event.Version, Document: event.Document,
	})
	require.NoError(t, err)
	args.ExpectedEventVersion = event.Version
	return event
}

func tierAssignOther(
	t *testing.T,
	f pairFixture,
	admin db.PaymentScope,
	event events.Event,
	role string,
) registrations.Registration {
	t.Helper()
	ctx := context.Background()
	user, err := f.H.store.GetUser(ctx, f.Other.UserID)
	require.NoError(t, err)
	entry, err := f.H.store.ChangeRegistration(
		ctx,
		f.Other.EventScope,
		user.Subject,
		"tier-other-join",
		db.RegistrationChange{
			Kind: "register", EventID: event.ID, UserID: f.Other.UserID, Role: role,
		},
	)
	require.NoError(t, err)
	args := db.PassAssignment{
		EventID:              event.ID,
		RegistrationID:       entry.ID,
		ExpectedEventVersion: event.Version,
		ExpectedVersion:      entry.Version,
	}
	prep, err := f.H.store.PrepareAssignment(ctx, admin.EventScope, admin.Subject, "tier-other-assign", args)
	require.NoError(t, err)
	assigned, err := f.H.store.ConfirmAssignment(
		ctx,
		admin.EventScope,
		admin.Subject,
		"tier-other-assign",
		args,
		prep.ID,
	)
	require.NoError(t, err)
	return assigned
}

func requireTierPairUnchanged(t *testing.T, f pairFixture, before []registrations.Registration, passCount int) {
	t.Helper()
	ctx := context.Background()
	for _, entry := range before {
		after, err := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, entry.UserID)
		require.NoError(t, err)
		require.Equal(t, entry, after)
	}
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, `SELECT count(*) FROM shuttle_passes p
JOIN shuttle_registrations r ON r.id=p.registration_id WHERE r.event_id=$1`, f.EventID).Scan(&count))
	require.Equal(t, passCount, count)
}

func tierPairBefore(t *testing.T, f pairFixture) []registrations.Registration {
	t.Helper()
	var result []registrations.Registration
	for _, owner := range []db.PaymentScope{f.A, f.B} {
		entry, err := f.H.store.Registration(context.Background(), owner.EventScope, f.EventID, owner.UserID)
		require.NoError(t, err)
		result = append(result, entry)
	}
	return result
}

func TestTierPairedAssignmentFreezesOwnerPricesDepositsAndReplay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, admin, args := acceptedPairForAssignment(t)
	event := configureTierPair(t, f, admin, &args, tierPolicy("paired", 2))
	tierAssignOther(t, f, admin, event, registrations.Leader)
	prep, err := f.H.store.PreparePairAssignment(ctx, admin, "tier-pair", args)
	require.NoError(t, err)
	require.Contains(t, prep.Description, "200.00 BYN")
	require.Contains(t, prep.Description, "100.00 BYN")
	assigned, err := f.H.store.ConfirmPairAssignment(ctx, admin, "tier-pair", args, prep.ID)
	require.NoError(t, err)
	require.EqualValues(t, 20000, assigned.Leader.Pass.PriceMinor)
	require.EqualValues(t, 10000, assigned.Follower.Pass.PriceMinor)
	require.Equal(t, &registrations.TierSnapshot{ID: "regular", Number: 2}, assigned.Leader.Pass.Tier)
	require.Equal(t, &registrations.TierSnapshot{ID: "early", Number: 1}, assigned.Follower.Pass.Tier)
	require.EqualValues(t, 5000, assigned.Leader.Pass.Deposit.RequiredMinor)
	require.EqualValues(t, 3000, assigned.Follower.Pass.Deposit.RequiredMinor)
	event.Document.Registration.Pricing = tierPolicy("distributed", 10)
	event.Document.Registration.Pricing.Tiers[0].PriceMinor = 40000
	event.Document.Deposit.MinimumBPS = 5000
	_, err = f.H.store.ChangeEvent(ctx, admin.EventScope, admin.Subject, "tier-edit", db.EventChange{
		ID: event.ID, ExpectedVersion: event.Version, Document: event.Document,
	})
	require.NoError(t, err)
	f.H.stop(t)
	f.H.start(t)
	replayed, err := f.H.store.ConfirmPairAssignment(ctx, admin, "tier-pair", args, prep.ID)
	require.NoError(t, err)
	require.Equal(t, assigned, replayed)
	for _, original := range []registrations.Registration{assigned.Leader, assigned.Follower} {
		recovered, readErr := f.H.store.Registration(ctx, admin.EventScope, f.EventID, original.UserID)
		require.NoError(t, readErr)
		require.Equal(t, original, recovered)
	}
}

func TestTierDistributedPairAllowsOnlyOnePlaceOverflowWithEligibleSuccessor(t *testing.T) {
	t.Parallel()
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprintf("blocked=%t", blocked), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f, admin, args := acceptedPairForAssignment(t)
			policy := tierPolicy("distributed", 1)
			policy.Tiers[1].BlockedByDate = blocked
			event := configureTierPair(t, f, admin, &args, policy)
			before := tierPairBefore(t, f)
			prep, err := f.H.store.PreparePairAssignment(ctx, admin, "boundary-pair", args)
			if blocked {
				require.Error(t, err)
				requireTierPairUnchanged(t, f, before, 0)
				return
			}
			require.NoError(t, err)
			assigned, err := f.H.store.ConfirmPairAssignment(ctx, admin, "boundary-pair", args, prep.ID)
			require.NoError(t, err)
			for _, entry := range []registrations.Registration{assigned.Leader, assigned.Follower} {
				require.EqualValues(t, 10000, entry.Pass.PriceMinor)
				require.Equal(t, &registrations.TierSnapshot{ID: "early", Number: 1}, entry.Pass.Tier)
			}
			// Explicit usage remains two after the permitted amount-one overshoot.
			next := tierAssignOther(t, f, admin, event, registrations.Leader)
			require.EqualValues(t, 20000, next.Pass.PriceMinor)
			require.Equal(t, &registrations.TierSnapshot{ID: "regular", Number: 2}, next.Pass.Tier)
		})
	}
}

func TestTierPairRejectsChangedUsageQuoteBeforeEitherPassWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, admin, args := acceptedPairForAssignment(t)
	event := configureTierPair(t, f, admin, &args, tierPolicy("paired", 2))
	prep, err := f.H.store.PreparePairAssignment(ctx, admin, "stale-tier-pair", args)
	require.NoError(t, err)
	before := tierPairBefore(t, f)
	// The follower changes tier; the leader quote remains valid. Neither may be written.
	tierAssignOther(t, f, admin, event, registrations.Follower)
	_, err = f.H.store.ConfirmPairAssignment(ctx, admin, "stale-tier-pair", args, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	requireTierPairUnchanged(t, f, before, 1)
	fresh, err := f.H.store.PreparePairAssignment(ctx, admin, "fresh-tier-pair", args)
	require.NoError(t, err)
	assigned, err := f.H.store.ConfirmPairAssignment(ctx, admin, "fresh-tier-pair", args, fresh.ID)
	require.NoError(t, err)
	require.EqualValues(t, 10000, assigned.Leader.Pass.PriceMinor)
	require.EqualValues(t, 20000, assigned.Follower.Pass.PriceMinor)
}

func TestTierPairOverflowAfterUnsoldDateTierBlocksAnotherPass(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, admin, args := acceptedPairForAssignment(t)
	policy := tierPolicy(registrations.PricingDistributed, 1)
	policy.Tiers[1].Amount = 1
	old := policy.Tiers[0]
	old.ID, old.Amount = "expired", 100
	old.StartsAt = old.StartsAt.Add(-time.Hour)
	policy.Tiers = append([]registrations.Tier{old}, policy.Tiers...)
	event := configureTierPair(t, f, admin, &args, policy)
	prep, err := f.H.store.PreparePairAssignment(ctx, admin, "dated-overflow-pair", args)
	require.NoError(t, err)
	assigned, err := f.H.store.ConfirmPairAssignment(ctx, admin, "dated-overflow-pair", args, prep.ID)
	require.NoError(t, err)
	for _, entry := range []registrations.Registration{assigned.Leader, assigned.Follower} {
		require.Equal(t, "early", entry.Pass.Tier.ID)
	}
	user, err := f.H.store.GetUser(ctx, f.Other.UserID)
	require.NoError(t, err)
	entry, err := f.H.store.ChangeRegistration(ctx, f.Other.EventScope, user.Subject, "dated-overflow-join",
		db.RegistrationChange{Kind: "register", EventID: event.ID, UserID: f.Other.UserID, Role: registrations.Leader})
	require.NoError(t, err)
	_, err = f.H.store.PrepareAssignment(ctx, admin.EventScope, admin.Subject, "dated-overflow-solo", db.PassAssignment{
		EventID:              event.ID,
		RegistrationID:       entry.ID,
		ExpectedEventVersion: event.Version,
		ExpectedVersion:      entry.Version,
	})
	require.ErrorIs(t, err, registrations.ErrCapacity)
	after, err := f.H.store.Registration(ctx, f.Other.EventScope, event.ID, f.Other.UserID)
	require.NoError(t, err)
	require.Equal(t, entry, after)
	requireTierPairUnchanged(t, f, []registrations.Registration{assigned.Leader, assigned.Follower}, 2)
}

func configureTierEvent(
	t *testing.T,
	h *harness,
	operator string,
	event events.Event,
	policy *registrations.TierPolicy,
) events.Event {
	t.Helper()
	event.Document.Registration.LeaderCapacity = 10
	event.Document.Registration.FollowerCapacity = 10
	event.Document.Registration.Pricing = policy
	code, data := adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		"/internal/admin/events/"+event.ID.String(),
		map[string]any{
			"operation_id": "tier-config", "expected_version": event.Version, "document": event.Document,
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &event))
	return event
}

func TestTierCompetingSoloApprovalsDoNotReuseLastEarlyPlace(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 15001, true)
	_, a := registrationUser(t, h, 15002, false)
	_, b := registrationUser(t, h, 15003, false)
	event := configureTierEvent(t, h, operator, registrationEvent(t, h, operator), tierPolicy("distributed", 1))
	entries := []registrations.Registration{
		registerForEvent(t, h, a, event.ID, "join-a", registrations.Leader),
		registerForEvent(t, h, b, event.ID, "join-b", registrations.Follower),
	}
	proposals := []assignmentProposal{
		prepareAssignment(t, h, operator, "assign-a", event, entries[0]),
		prepareAssignment(t, h, operator, "assign-b", event, entries[1]),
	}
	type response struct {
		code int
		data []byte
	}
	results := make(chan response, 2)
	var workers sync.WaitGroup
	for _, proposal := range proposals {
		workers.Go(func() {
			code, data := executeAssignment(t, h, operator, proposal)
			results <- response{code, data}
		})
	}
	workers.Wait()
	close(results)
	succeeded, rejected := 0, 0
	for result := range results {
		require.Equal(t, http.StatusOK, result.code, string(result.data))
		var status struct {
			Status string `json:"status"`
		}
		require.NoError(t, json.Unmarshal(result.data, &status))
		switch status.Status {
		case "succeeded":
			succeeded++
			require.EqualValues(t, 10000, assignmentResult(t, result.data).Pass.PriceMinor)
		case "rejected":
			rejected++
		default:
			t.Fatalf("unexpected response: %s", result.data)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, rejected)
	var count int
	require.NoError(t, h.store.Pool().QueryRow(context.Background(), `SELECT count(*) FROM shuttle_passes p
JOIN shuttle_registrations r ON r.id=p.registration_id WHERE r.event_id=$1`, event.ID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestTierLegacyPreparationWithoutQuotesRequiresFlatPolicy(t *testing.T) {
	t.Parallel()
	for _, tiered := range []bool{false, true} {
		t.Run(fmt.Sprintf("tiered=%t", tiered), func(t *testing.T) {
			t.Parallel()
			h := setup(t, newPeers(t))
			_, operator := registrationUser(t, h, 15101, true)
			owner, token := registrationUser(t, h, 15102, false)
			event := registrationEvent(t, h, operator)
			if tiered {
				event = configureTierEvent(t, h, operator, event, tierPolicy("distributed", 2))
			}
			entry := registerForEvent(t, h, token, event.ID, "legacy-join", registrations.Leader)
			proposal := prepareAssignment(t, h, operator, "legacy-assign", event, entry)
			_, err := h.store.Pool().Exec(context.Background(), `UPDATE shuttle_assignment_preparations
SET document=document-'quotes' WHERE proposal_id=$1`, proposal.Preconditions.ProposalID)
			require.NoError(t, err)
			code, data := executeAssignment(t, h, operator, proposal)
			require.Equal(t, http.StatusOK, code, string(data))
			if tiered {
				require.Contains(t, string(data), "rejected")
				unchanged, readErr := h.store.Registration(
					context.Background(),
					db.EventScope{BotID: 123, Organization: "org"},
					event.ID,
					owner,
				)
				require.NoError(t, readErr)
				require.Equal(t, entry, unchanged)
				return
			}
			assigned := assignmentResult(t, data)
			require.EqualValues(t, 12345, assigned.Pass.PriceMinor)
			require.Nil(t, assigned.Pass.Tier)
			raw, err := json.Marshal(assigned.Pass)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "tier")
			h.stop(t)
			h.start(t)
			code, data = executeAssignment(t, h, operator, proposal)
			require.Equal(t, http.StatusOK, code, string(data))
			require.Equal(t, assigned, assignmentResult(t, data))
		})
	}
}

func TestTierDateFloorInvalidatesApprovedPairWithoutPartialWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, admin, args := acceptedPairForAssignment(t)
	policy := tierPolicy("distributed", 10)
	event := configureTierPair(t, f, admin, &args, policy)
	var floor time.Time
	var prep db.PairAssignmentPreparation
	var operation string
	// Establish an early quote before testing its expiry. If a loaded runner
	// crosses the floor during setup, retry setup with a new version and key.
	for attempt := range 3 {
		var now time.Time
		require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now))
		floor = now.Add(time.Duration(attempt+1) * 3 * time.Second).UTC().Truncate(time.Microsecond)
		policy.Tiers[1].StartsAt = floor
		event.Document.Registration.Pricing = policy
		var err error
		event, err = f.H.store.ChangeEvent(
			ctx,
			admin.EventScope,
			admin.Subject,
			fmt.Sprintf("date-policy-%d", attempt),
			db.EventChange{
				ID: event.ID, ExpectedVersion: event.Version, Document: event.Document,
			},
		)
		require.NoError(t, err)
		args.ExpectedEventVersion = event.Version
		operation = fmt.Sprintf("date-floor-pair-%d", attempt)
		prep, err = f.H.store.PreparePairAssignment(ctx, admin, operation, args)
		require.NoError(t, err)
		if prep.Quotes[0].PriceMinor == 10000 {
			break
		}
	}
	require.Contains(t, prep.Description, "100.00 BYN")
	before := tierPairBefore(t, f)
	// Wait against the same database clock that decides the effective tier.
	_, err := f.H.store.Pool().Exec(ctx, `SELECT pg_sleep(GREATEST(0,
EXTRACT(EPOCH FROM ($1::timestamptz-clock_timestamp())))+0.01)`, floor)
	require.NoError(t, err)
	_, err = f.H.store.ConfirmPairAssignment(ctx, admin, operation, args, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	requireTierPairUnchanged(t, f, before, 0)
	fresh, err := f.H.store.PreparePairAssignment(ctx, admin, "date-floor-fresh", args)
	require.NoError(t, err)
	assigned, err := f.H.store.ConfirmPairAssignment(ctx, admin, "date-floor-fresh", args, fresh.ID)
	require.NoError(t, err)
	require.EqualValues(t, 20000, assigned.Leader.Pass.PriceMinor)
	require.EqualValues(t, 20000, assigned.Follower.Pass.PriceMinor)
}

func TestTierLegacyPairPreparationWithoutQuotesRequiresFlatPolicy(t *testing.T) {
	t.Parallel()
	for _, tiered := range []bool{false, true} {
		t.Run(fmt.Sprintf("tiered=%t", tiered), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f, admin, args := acceptedPairForAssignment(t)
			if tiered {
				configureTierPair(t, f, admin, &args, tierPolicy("distributed", 2))
			}
			before := tierPairBefore(t, f)
			prep, err := f.H.store.PreparePairAssignment(ctx, admin, "legacy-pair", args)
			require.NoError(t, err)
			_, err = f.H.store.Pool().Exec(ctx, `UPDATE shuttle_pair_preparations
SET document=document-'quotes' WHERE proposal_id=$1`, prep.ID)
			require.NoError(t, err)
			assigned, err := f.H.store.ConfirmPairAssignment(ctx, admin, "legacy-pair", args, prep.ID)
			if tiered {
				require.ErrorIs(t, err, db.ErrConflict)
				requireTierPairUnchanged(t, f, before, 0)
				return
			}
			require.NoError(t, err)
			for _, entry := range []registrations.Registration{assigned.Leader, assigned.Follower} {
				require.EqualValues(t, 12345, entry.Pass.PriceMinor)
				require.Nil(t, entry.Pass.Tier)
			}
		})
	}
}

func TestTierDepositConfigurationChecksEveryTierPriceAndIgnoresUnusedFlatPrice(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 15201, true)
	event := registrationEvent(t, h, operator)
	rawDocument, err := json.Marshal(event.Document)
	require.NoError(t, err)
	var document events.Document
	require.NoError(t, json.Unmarshal(rawDocument, &document))
	document.Registration.Pricing = tierPolicy("distributed", 2)
	document.Deposit = &registrations.DepositPolicy{MinimumMinor: 15000, MinimumBPS: 0}
	path := "/internal/admin/events/" + event.ID.String()
	// The later tier permits this deposit, but the early tier does not.
	code, data := adminRequest(t, h, operator, http.MethodPut, path, map[string]any{
		"operation_id": "invalid-tier-deposit", "expected_version": event.Version, "document": document,
	})
	require.Contains(t, []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, code, string(data))
	code, data = adminRequest(t, h, operator, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, code, string(data))
	var unchanged events.Event
	require.NoError(t, json.Unmarshal(data, &unchanged))
	require.Equal(t, event, unchanged)
	document.Registration.PriceMinor = 0
	document.Deposit.MinimumMinor = 3000
	code, data = adminRequest(t, h, operator, http.MethodPut, path, map[string]any{
		"operation_id": "valid-tier-deposit", "expected_version": event.Version, "document": document,
	})
	require.Equal(t, http.StatusOK, code, string(data))
	var configured events.Event
	require.NoError(t, json.Unmarshal(data, &configured))
	require.Zero(t, configured.Document.Registration.PriceMinor)
	require.EqualValues(t, 3000, configured.Document.Deposit.MinimumMinor)
	_, token := registrationUser(t, h, 15202, false)
	entry := registerForEvent(t, h, token, event.ID, "tier-deposit-join", registrations.Leader)
	proposal := prepareAssignment(t, h, operator, "tier-deposit-assign", configured, entry)
	code, data = executeAssignment(t, h, operator, proposal)
	require.Equal(t, http.StatusOK, code, string(data))
	assigned := assignmentResult(t, data)
	require.EqualValues(t, 10000, assigned.Pass.PriceMinor)
	require.EqualValues(t, 3000, assigned.Pass.Deposit.RequiredMinor)
}
