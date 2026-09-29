package agent

import "context"

// RequestScope is host-supplied transport metadata for deterministic sandbox
// fixtures. It is not model input, an authorization claim or an action field.
type RequestScope struct {
	Owner    string
	UpdateID int64
	Turn     int
}

type requestScopeKey struct{}

func WithRequestScope(ctx context.Context, scope RequestScope) context.Context {
	return context.WithValue(ctx, requestScopeKey{}, scope)
}

func RequestScopeFromContext(ctx context.Context) (RequestScope, bool) {
	scope, ok := ctx.Value(requestScopeKey{}).(RequestScope)
	return scope, ok
}
