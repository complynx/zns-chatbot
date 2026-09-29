package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type authorizerVerifier struct{}

func (authorizerVerifier) Verify(_ context.Context, assertion string) (identity.AuthorizerIdentity, error) {
	if assertion != "verified" {
		return identity.AuthorizerIdentity{}, identity.ErrAuthorizer
	}
	return identity.AuthorizerIdentity{
		Telegram: identityprovision.Telegram{ID: 95101},
		Subject:  "telegram:77:opaque",
	}, nil
}

type authorizerLinker struct {
	calls int
	err   error
}

func (l *authorizerLinker) EnsureExternal(
	_ context.Context,
	input identityprovision.Telegram,
	subject string,
) (identityprovision.Binding, error) {
	l.calls++
	if input.ID != 95101 || subject != "telegram:77:opaque" {
		return identityprovision.Binding{}, errors.New("wrong verified identity")
	}
	return identityprovision.Binding{Owner: "opaque", Subject: "reserved"}, l.err
}

func TestAuthorizerEndpointReturnsReadyOnlyAfterTrustedLink(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, body    string
		failure       error
		status, calls int
	}{
		{"ready", `{"assertion":"verified"}`, nil, http.StatusOK, 1},
		{"untrusted", `{"assertion":"forged"}`, nil, http.StatusUnauthorized, 0},
		{"actorReplacement", `{"assertion":"verified","user_id":"202"}`, nil, http.StatusBadRequest, 0},
		{"conflict", `{"assertion":"verified"}`, identityprovision.ErrConflict, http.StatusConflict, 1},
		{"providerUnavailable", `{"assertion":"verified"}`, identityprovision.ErrUnavailable, http.StatusServiceUnavailable, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			linker := &authorizerLinker{err: test.failure}
			handler := api.WithAuthorizerProvisioning(http.NotFoundHandler(), authorizerVerifier{}, linker)
			r := httptest.NewRequest(http.MethodPost, "/internal/identity/authorizer", strings.NewReader(test.body))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			require.Equal(t, test.status, w.Code)
			require.Equal(t, test.calls, linker.calls)
			if test.status == http.StatusOK {
				require.JSONEq(t, `{"ready":true}`, w.Body.String())
			} else {
				require.NotContains(t, w.Body.String(), `"ready":true`)
			}
		})
	}
}
