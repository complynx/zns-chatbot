package botdelivery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestPassPreparationLockWindowKeepsSeparateSourceBudgets(t *testing.T) {
	t.Parallel()
	record := func(prefix string, count int) []readsource.Authority {
		refs := make([]readsource.Authority, count)
		for index := range refs {
			refs[index].Registration = passbooking.ReadAuthority{Kind: passbooking.ReadOwnerMenu,
				Event: fmt.Sprintf("%s-%03d", prefix, index)}
		}
		return refs
	}
	current, previous := record("current", 129), record("previous", 129)
	require.True(t, readsource.Valid(current))
	require.True(t, readsource.Valid(previous))
	require.False(t, readsource.Valid(append(append([]readsource.Authority{}, current...), previous...)))
	prior := Intent{Reference: Reference{Source: &readsource.Derivation{Authorities: previous}}}
	records, err := passPreparationLocks(current, prior)
	require.NoError(t, err)
	require.Len(t, records, 3)
	require.Equal(t, current, records[0])
	require.Equal(t, previous, records[2])
	// Both records reach SQL locking, rather than a false aggregate limit.
	require.ErrorIs(t, readsource.LockRegistrationMutationPrelude(t.Context(), receiptSQLFailure{err: io.EOF},
		records[0], nil, []string{"alice"}, records[1:]...), core.ErrDatabase)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, readsource.LockRegistrationMutationPrelude(ctx, receiptSQLFailure{err: context.Canceled},
		records[0], nil, []string{"alice"}, records[1:]...), context.Canceled)
	oversized := record("oversized", readsource.MaxAuthorities+1)
	_, err = passPreparationLocks(oversized, prior)
	require.ErrorIs(t, err, readsource.ErrLimit)
	prior.Reference.Source.Authorities = oversized
	_, err = passPreparationLocks(current, prior)
	require.ErrorIs(t, err, readsource.ErrLimit)
	require.ErrorIs(t, readsource.LockRegistrationMutationPrelude(t.Context(), nil, current, nil, nil, oversized),
		readsource.ErrLimit)
}

func TestPredecessorPassReceiptRequiresTrustedCanonicalMetadata(t *testing.T) {
	t.Parallel()
	generation := int64(0)
	i := Intent{BotID: 1, Owner: "alice", Chat: 101, MessageID: 42, Attempt: 1,
		State: delivery.Succeeded, Phase: phaseEdit, Target: 42,
		Reference: Reference{
			Kind:       CardIntent,
			Family:     familyPasses,
			CardKey:    familyPasses,
			Revision:   2,
			Generation: &generation,
			Source:     &readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}},
			Continuation: Continuation{Kind: passCardReceiptKind, Revision: 2, ViewHash: strings.Repeat("a", 64),
				Tokens: []string{strings.Repeat("b", 32)}},
		},
		Receipt: Continuation{Kind: passCardReceiptKind, Revision: 2, ViewHash: strings.Repeat("c", 64),
			Tokens: []string{strings.Repeat("d", 32)}}}
	require.True(t, validPassReceipt(i), "genuine fresh rendering can differ from the original queued hash")
	for _, mutation := range []struct {
		name   string
		change func(*Intent)
	}{
		{"foreign_family", func(candidate *Intent) { candidate.Reference.Family = familyStatic }},
		{"foreign_card", func(candidate *Intent) { candidate.Reference.CardKey = "orders" }},
		{"wrong_revision", func(candidate *Intent) { candidate.Receipt.Revision++ }},
		{"missing_generation", func(candidate *Intent) { candidate.Reference.Generation = nil }},
		{"bad_source", func(candidate *Intent) { candidate.Reference.Source = &readsource.Derivation{} }},
		{"bad_payload_hash", func(candidate *Intent) { candidate.Receipt.ViewHash = "not-a-hash" }},
		{"bad_queued_hash", func(candidate *Intent) { candidate.Reference.Continuation.ViewHash = "not-a-hash" }},
		{"bad_token", func(candidate *Intent) { candidate.Receipt.Tokens = []string{"not-a-token"} }},
		{"lost_current_metadata", func(candidate *Intent) { candidate.Reference.Continuation.Pass = &PassCardReceipt{} }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			t.Parallel()
			candidate := i
			mutation.change(&candidate)
			require.False(t, validPassReceipt(candidate))
		})
	}
}

func TestPassReceiptRetirementDatabaseFailure(t *testing.T) {
	t.Parallel()
	s := Service{}
	i := Intent{Owner: "alice", MessageID: 42}
	require.ErrorIs(t, s.retirePassReceipt(t.Context(), receiptSQLFailure{err: io.EOF}, i, nil, 0), core.ErrDatabase)
	require.ErrorIs(
		t,
		s.retirePassReceipt(t.Context(), receiptSQLFailure{err: context.Canceled}, i, nil, 0),
		core.ErrDatabase,
	)
}

func TestPassMenuDefinitiveDenialKeepsCauseAndDatabaseProvenance(t *testing.T) {
	t.Parallel()
	i := Intent{Reference: Reference{Family: familyPasses, Source: &readsource.Derivation{}}}
	cause := &core.ProblemError{Status: http.StatusConflict, Code: "history_stale"}
	denied := passMenuDenied(i, cause)
	_, typed := errors.AsType[*PassMenuDeniedError](denied)
	require.True(t, typed)
	require.ErrorIs(t, denied, cause)
	problem, ok := errors.AsType[*core.ProblemError](denied)
	require.True(t, ok)
	require.Equal(t, PassMenuDeniedCode, problem.Code, "the HTTP host must preserve definitive denial")
	require.True(t, sourceDenied(denied))
	database := core.DatabaseOperationError(io.EOF)
	_, typed = errors.AsType[*PassMenuDeniedError](passMenuDenied(i, database))
	require.False(t, typed)
	require.ErrorIs(t, passMenuDenied(i, database), core.ErrDatabase)
	_, typed = errors.AsType[*PassMenuDeniedError](ErrStale)
	require.False(t, typed, "an obsolete view binding alone is not a definitive denial")
}

func TestPassPreparationReceiptSQLProvenanceAndCancellation(t *testing.T) {
	t.Parallel()
	s := Service{}
	pending := Intent{Owner: "alice", Target: 42, Phase: phaseEdit,
		Reference: Reference{Family: familyPasses, Source: &readsource.Derivation{}}}
	previous, err := s.previousPassReceipt(t.Context(), receiptSQLFailure{err: io.EOF}, pending)
	require.Nil(t, previous)
	require.ErrorIs(t, err, core.ErrDatabase)
	previous, err = s.previousPassReceipt(t.Context(), receiptSQLFailure{err: context.Canceled}, pending)
	require.Nil(t, previous)
	require.ErrorIs(t, err, core.ErrDatabase, "driver cancellation is not request cancellation")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	previous, err = s.previousPassReceipt(ctx, receiptSQLFailure{err: context.Canceled}, pending)
	require.Nil(t, previous)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPassRetirementRequestCancellationAndSQLProvenance(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s := Service{}
	i := Intent{Owner: "alice", MessageID: 42}
	require.ErrorIs(t, s.retirePassReceipt(ctx, receiptSQLFailure{err: context.Canceled}, i, nil, 0), context.Canceled)
	statement := &pgconn.PgError{Code: "40001"}
	require.ErrorIs(
		t,
		s.retirePassReceipt(ctx, receiptSQLFailure{err: statement}, i, nil, 0),
		core.ErrDatabaseSerialization,
	)
}

func TestPassReceiptReadSQLProvenanceAndCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cases := []struct {
		name          string
		ctx           context.Context
		failure, want error
	}{
		{"live_driver_cancel", t.Context(), context.Canceled, core.ErrDatabase},
		{"live_driver_deadline", t.Context(), context.DeadlineExceeded, core.ErrDatabase},
		{"request_cancel", ctx, context.Canceled, context.Canceled},
		{
			"cancelled_request_SQL",
			ctx,
			&pgconn.PgError{Code: "40001", Message: "private statement"},
			core.ErrDatabaseSerialization,
		},
		{
			"private_SQL_redacted",
			t.Context(),
			&pgconn.PgError{Code: "42601", Message: "private statement"},
			core.ErrDatabase,
		},
		{"expected_absence", t.Context(), pgx.ErrNoRows, pgx.ErrNoRows},
	}
	for _, testcase := range cases {
		t.Run(testcase.name, func(t *testing.T) {
			t.Parallel()
			_, err := Read(
				testcase.ctx,
				receiptSQLFailure{err: testcase.failure},
				1,
				delivery.Reference{Owner: delivery.Bot, Key: "card:1", Effect: "view"},
				false,
			)
			require.ErrorIs(t, err, testcase.want)
			require.NotContains(t, err.Error(), "private statement")
		})
	}
}

func TestPassFamilyReadSQLProvenanceAndCancellation(t *testing.T) {
	t.Parallel()
	current := Intent{Owner: "alice", Reference: Reference{Kind: CardIntent, Family: familyPasses, Revision: 1}}
	_, err := readPassMenuFamily(t.Context(), receiptSQLFailure{err: context.Canceled}, current)
	require.ErrorIs(t, err, core.ErrDatabase)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = readPassMenuFamily(ctx, receiptSQLFailure{err: context.Canceled}, current)
	require.ErrorIs(t, err, context.Canceled)
	_, err = readPassMenuFamily(
		ctx,
		receiptSQLFailure{err: &pgconn.PgError{Code: "40001", Message: "private statement"}},
		current,
	)
	require.ErrorIs(t, err, core.ErrDatabaseSerialization)
	require.NotContains(t, err.Error(), "private statement")
	_, err = readPassMenuFamily(t.Context(), receiptSQLFailure{err: pgx.ErrNoRows}, current)
	require.ErrorIs(t, err, ErrStale)
	require.False(t, core.IsDatabaseFailure(err))
}
