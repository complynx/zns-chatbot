package appclient

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestLocalKnowledgeFreshUserAndHostAuthorization(t *testing.T) {
	t.Parallel()
	exchange := &registrationExchange{}
	failure := error(nil)
	verifications, effects := 0, 0
	c := Client{Exchange: exchange, Links: registrationLinks{}, LocalKnowledge: &LocalKnowledge{
		Authorizer: applicationauth.Authorizer{
			DB: permittedOrderOwner{},
			Verify: func(_ context.Context, token string) (string, error) {
				verifications++
				require.Equal(t, "subject-alice", token)
				return "alice", failure
			},
		},
	}}
	ctx, owner, err := c.AuthenticateTelegram(t.Context(), 101)
	require.NoError(t, err)
	operation := func(knowledge.Service, string) (knowledge.Result, error) { effects++; return knowledge.Result{}, nil }
	_, err = directKnowledge(ctx, c, owner, operation)
	require.NoError(t, err)
	host := Host{LocalKnowledge: c.LocalKnowledge, UserToken: c.UserToken}
	_, err = hostKnowledge(ctx, host, owner, operation)
	require.NoError(t, err)
	failure = identity.ErrZitadelIdentity
	_, err = hostKnowledge(ctx, host, owner, operation)
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, http.StatusUnauthorized, problem.Status)
	require.Equal(t, 2, effects)
	require.Equal(t, 3, verifications)
	require.Equal(t, 3, exchange.calls)
	_, err = directKnowledge(ctx, c, "bob", operation)
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	require.Equal(t, 3, exchange.calls)
}

func TestLocalKnowledgeFailuresReturnNoPartialData(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		failure error
		status  int
	}{
		{"provider", errors.New("private provider detail"), http.StatusInternalServerError},
		{"denied", identity.ErrZitadelIdentity, http.StatusUnauthorized},
		{"canceled", context.Canceled, 0}, {"deadline", context.DeadlineExceeded, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := Client{SandboxToken: (identity.Signer{}).Token, LocalKnowledge: &LocalKnowledge{
				Authorizer: applicationauth.Authorizer{
					DB:     permittedOrderOwner{},
					Verify: func(context.Context, string) (string, error) { return "alice", test.failure },
				},
			}}
			value, err := directKnowledge(t.Context(), c, "alice", func(knowledge.Service, string) (string, error) {
				t.Fatal("unauthorized domain execution")
				return "", nil
			})
			require.Empty(t, value)
			if test.status == 0 {
				require.ErrorIs(t, err, test.failure)
			} else {
				var problem *core.ProblemError
				require.ErrorAs(t, err, &problem)
				require.Equal(t, test.status, problem.Status)
				require.NotContains(t, err.Error(), "private")
			}
		})
	}
}

func TestLocalKnowledgePreservesExactEvidenceAndUnionLimit(t *testing.T) {
	t.Parallel()
	c := Client{SandboxToken: (identity.Signer{}).Token, LocalKnowledge: &LocalKnowledge{
		Authorizer: applicationauth.Authorizer{
			DB:     permittedOrderOwner{},
			Verify: func(context.Context, string) (string, error) { return "alice", nil },
		},
	}}
	authority := func(generation int64) readsource.Authority {
		return readsource.Authority{
			Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.PrivateMemory, Generation: generation},
		}
	}
	first, second := []readsource.Authority{authority(1)}, []readsource.Authority{authority(2)}
	page := knowledge.FactPage{
		Facts: []knowledge.Fact{{Text: "one", ReadAuthorities: first}, {Text: "two", ReadAuthorities: second}},
	}
	actual, err := directKnowledge(
		t.Context(),
		c,
		"alice",
		func(knowledge.Service, string) (knowledge.FactPage, error) { return page, nil },
	)
	require.NoError(t, err)
	require.Equal(t, first, actual.Facts[0].ReadAuthorities)
	require.Equal(t, second, actual.Facts[1].ReadAuthorities)
	require.Len(t, actual.ReadAuthorities, 2)
	require.Equal(t, first, page.Facts[0].ReadAuthorities)
	wire, err := knowledge.EvidenceEnvelope(actual)
	require.NoError(t, err)
	projected, ok := wire.(knowledge.FactPage)
	require.True(t, ok)
	require.Empty(t, projected.Facts[0].ReadAuthorities)
	for i := int64(3); i <= int64(readsource.MaxAuthorities)+1; i++ {
		page.Facts[1].ReadAuthorities = append(page.Facts[1].ReadAuthorities, authority(i))
	}
	actual, err = directKnowledge(
		t.Context(),
		c,
		"alice",
		func(knowledge.Service, string) (knowledge.FactPage, error) { return page, nil },
	)
	require.Empty(t, actual)
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, http.StatusUnprocessableEntity, problem.Status)
	require.Equal(t, "source_authority_limit", problem.Code)
}

func TestLocalKnowledgeOwnerAndDomainFailure(t *testing.T) {
	t.Parallel()
	c := Client{SandboxToken: (identity.Signer{}).Token, LocalKnowledge: &LocalKnowledge{
		Authorizer: applicationauth.Authorizer{
			DB:     permittedOrderOwner{},
			Verify: func(context.Context, string) (string, error) { return "alice", nil },
		},
	}}
	operation := func(knowledge.Service, string) (string, error) {
		return "private body", errors.New("private database detail")
	}
	value, err := directKnowledge(t.Context(), c, "alice", operation)
	require.Empty(t, value)
	require.NotContains(t, err.Error(), "private")
	_, err = directKnowledge(t.Context(), c, "bob", operation)
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, http.StatusForbidden, problem.Status)
	_, err = hostKnowledge(
		t.Context(),
		Host{LocalKnowledge: c.LocalKnowledge, UserToken: c.UserToken},
		"bob",
		operation,
	)
	require.ErrorAs(t, err, &problem)
	require.Equal(t, http.StatusUnauthorized, problem.Status)
	_, err = (Host{LocalKnowledge: c.LocalKnowledge}).ExecuteDerivedKnowledge(
		t.Context(),
		"alice",
		knowledge.Command{},
		readsource.Derivation{},
	)
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
}

func TestLocalKnowledgeTokenFailureIsSanitized(t *testing.T) {
	t.Parallel()
	host := Host{
		LocalKnowledge: &LocalKnowledge{},
		UserToken:      func(context.Context, string) (string, error) { return "", errors.New("private token endpoint detail") },
	}
	_, err := host.ExecuteDerivedKnowledge(t.Context(), "alice", knowledge.Command{}, readsource.Derivation{})
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, http.StatusInternalServerError, problem.Status)
	require.NotContains(t, err.Error(), "private")
}
