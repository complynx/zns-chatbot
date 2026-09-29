package sandbox

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

// FixtureRemote is an explicit synthetic-only alternative to a real model.
// Production Remote never adds these lab headers or reads request scope.
type FixtureRemote struct {
	URL  string
	HTTP *http.Client
}

func (m FixtureRemote) Plan(ctx context.Context, input agent.Input) (agent.Plan, error) {
	scope, ok := agent.RequestScopeFromContext(ctx)
	if !ok || !syntheticFixtureOwner(scope.Owner) || scope.UpdateID <= 0 || scope.Turn < 0 {
		return agent.Plan{}, errors.New("fixture request scope missing")
	}
	const timeout = 10 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := http.Client{Timeout: timeout}
	if m.HTTP != nil {
		client = *m.HTTP
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = fixtureScopeTransport{base: transport, scope: scope}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return (agent.Remote{URL: m.URL, HTTP: &client}).Plan(ctx, input)
}

type fixtureScopeTransport struct {
	base  http.RoundTripper
	scope agent.RequestScope
}

func (t fixtureScopeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copyRequest := request.Clone(request.Context())
	copyRequest.Header = request.Header.Clone()
	copyRequest.Header.Set("X-Sandbox", "1")
	copyRequest.Header.Set("X-Sandbox-Actor", t.scope.Owner)
	copyRequest.Header.Set("X-Sandbox-Update", strconv.FormatInt(t.scope.UpdateID, 10))
	copyRequest.Header.Set("X-Sandbox-Turn", strconv.Itoa(t.scope.Turn))
	return t.base.RoundTrip(copyRequest)
}
