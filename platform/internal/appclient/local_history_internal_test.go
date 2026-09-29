package appclient

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestLocalHistoryFreshAuthorizationAndZeroFailures(t *testing.T) {
	t.Parallel()
	exchange := &registrationExchange{}
	failure := error(nil)
	verifications, effects := 0, 0
	client := Client{Exchange: exchange, Links: registrationLinks{}, LocalHistory: &LocalHistory{
		Authorizer: applicationauth.Authorizer{
			DB: permittedOrderOwner{},
			Verify: func(_ context.Context, token string) (string, error) {
				verifications++
				require.Equal(t, "subject-alice", token)
				return "alice", failure
			},
		},
	}}
	ctx, owner, err := client.AuthenticateTelegram(t.Context(), 101)
	require.NoError(t, err)
	operation := func(conversation.Service, string) (conversation.Page, error) {
		effects++
		return conversation.Page{Generation: 7}, nil
	}
	value, err := directHistory(ctx, client, owner, operation)
	require.NoError(t, err)
	require.EqualValues(t, 7, value.Generation)
	host := Host{LocalHistory: client.LocalHistory, UserToken: client.UserToken}
	_, err = hostHistory(ctx, host, owner, operation)
	require.NoError(t, err)
	failure = identity.ErrZitadelIdentity
	value, err = hostHistory(ctx, host, owner, operation)
	require.Empty(t, value)
	requireHistoryProblem(t, err, http.StatusUnauthorized)
	require.Equal(t, 2, effects)
	require.Equal(t, 3, verifications)
	require.Equal(t, 3, exchange.calls)
	failure = nil
	_, err = hostHistory(ctx, host, "bob", operation)
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	for _, failure := range []error{errors.New("private SQL detail"), context.Canceled, context.DeadlineExceeded} {
		value, err = hostHistory(ctx, host, owner, func(conversation.Service, string) (conversation.Page, error) {
			return conversation.Page{Generation: 99}, failure
		})
		require.Empty(t, value)
		if errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded) {
			require.ErrorIs(t, err, failure)
		} else {
			requireHistoryProblem(t, err, http.StatusInternalServerError)
			require.NotContains(t, err.Error(), "private")
		}
	}
}

func TestEveryLocalHistoryOperationUsesCurrentAuthorization(t *testing.T) {
	t.Parallel()
	tokens, verifications := 0, 0
	local := &LocalHistory{
		Authorizer: applicationauth.Authorizer{Verify: func(context.Context, string) (string, error) {
			verifications++
			return "", identity.ErrZitadelIdentity
		}},
	}
	client := Client{LocalHistory: local, SandboxToken: func(string) string { tokens++; return "fresh" }}
	host := Host{
		LocalHistory: local,
		UserToken:    func(context.Context, string) (string, error) { tokens++; return "fresh", nil },
	}
	ctx := t.Context()
	for _, operation := range []func() error{
		func() error { v, e := client.ConversationWindow(ctx, "alice", 1); require.Empty(t, v); return e },
		func() error {
			v, e := client.ConversationHistory(ctx, "alice", conversation.Query{Limit: 1})
			require.Empty(t, v)
			return e
		},
		func() error { v, e := client.HistoryGeneration(ctx, "alice"); require.Zero(t, v); return e },
		func() error {
			v, e := client.ConversationText(ctx, "alice", 1, 0, 1, "")
			require.Empty(t, v)
			return e
		},
		func() error { return host.ArchiveOriginal(ctx, "alice", "key", "user", "text") },
		func() error { return host.ArchiveOutcome(ctx, "alice", "key", "text") },
		func() error {
			return host.ArchiveDerived(ctx, "alice", "tg-assistant-1", "text", 1, false, 0, []readsource.Authority{})
		},
		func() error { v, e := host.HistorySummaryBatch(ctx, "alice", 1); require.Empty(t, v); return e },
		func() error { return host.CommitHistorySummary(ctx, "alice", 0, []int64{1}, "summary") },
		func() error { return host.CheckReadAuthorities(ctx, "alice", []readsource.Authority{}) },
	} {
		requireHistoryProblem(t, operation(), http.StatusUnauthorized)
	}
	require.Equal(t, 10, tokens)
	require.Equal(t, 10, verifications)
}

func TestLocalHistoryOwnerMismatchAndProviderFailure(t *testing.T) {
	t.Parallel()
	local := &LocalHistory{Authorizer: applicationauth.Authorizer{DB: permittedOrderOwner{},
		Verify: func(context.Context, string) (string, error) { return "bob", nil }}}
	client := Client{LocalHistory: local, SandboxToken: func(string) string { return "token" }}
	_, err := client.ConversationWindow(t.Context(), "alice", 1)
	requireHistoryProblem(t, err, http.StatusForbidden)
	host := Host{LocalHistory: local, UserToken: client.UserToken}
	requireHistoryProblem(t, host.ArchiveOutcome(t.Context(), "alice", "key", "text"), http.StatusUnauthorized)
	host.UserToken = func(context.Context, string) (string, error) { return "", errors.New("provider private detail") }
	err = host.CheckReadAuthorities(t.Context(), "alice", nil)
	requireHistoryProblem(t, err, http.StatusInternalServerError)
	require.NotContains(t, err.Error(), "private")
	host.UserToken = nil
	require.ErrorIs(t, host.ArchiveOutcome(t.Context(), "alice", "key", "text"), identity.ErrZitadelIdentity)
}

func requireHistoryProblem(t *testing.T, err error, status int) {
	t.Helper()
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, status, problem.Status)
}

func TestLocalHistoryPreservesUserAndHostErrorMapping(t *testing.T) {
	t.Parallel()
	client := Client{LocalHistory: &LocalHistory{Authorizer: applicationauth.Authorizer{
		DB: permittedOrderOwner{}, Verify: func(context.Context, string) (string, error) { return "alice", nil },
	}}, SandboxToken: func(string) string { return "token" }}
	operation := func(conversation.Service, string) (conversation.Page, error) {
		return conversation.Page{Generation: 7}, readsource.ErrLimit
	}
	value, err := directHistory(t.Context(), client, "alice", operation)
	require.Empty(t, value)
	requireHistoryProblem(t, err, http.StatusInternalServerError)
	host := Host{LocalHistory: client.LocalHistory, UserToken: client.UserToken}
	value, err = hostHistory(t.Context(), host, "alice", operation)
	require.Empty(t, value)
	requireHistoryProblem(t, err, http.StatusUnprocessableEntity)
}
