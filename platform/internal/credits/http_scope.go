package credits

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
)

const scopeHeader = "X-Zns-Accounting-Scope"
const maxScopeHeaderBytes = 2048

func SetRequestMode(request *http.Request, enforce bool) {
	mode := "shadow"
	if enforce {
		mode = "enforce"
	}
	request.Header.Set("X-Zns-Credits-Mode", mode)
}
func RequestMode(request *http.Request) (bool, error) {
	switch request.Header.Get("X-Zns-Credits-Mode") {
	case "", "shadow":
		return false, nil
	case "enforce":
		return true, nil
	default:
		return false, ErrInvalid
	}
}

// SetRequestScope is for the authenticated application-to-media-broker channel.
// A receiver must authenticate the host before calling AuthenticatedScope.
func SetRequestScope(request *http.Request) {
	raw, _ := json.Marshal(ScopeFromContext(request.Context()))
	request.Header.Set(scopeHeader, base64.RawURLEncoding.EncodeToString(raw))
}

func AuthenticatedScope(request *http.Request) (Scope, error) {
	value := request.Header.Get(scopeHeader)
	if value == "" {
		return ScopeFromContext(request.Context()), nil
	}
	if len(value) > maxScopeHeaderBytes {
		return Scope{}, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Scope{}, ErrInvalid
	}
	var scope Scope
	if json.Unmarshal(raw, &scope) != nil {
		return Scope{}, ErrInvalid
	}
	for _, part := range []string{scope.Actor, scope.Payer, scope.Key} {
		if part == "" || len(part) > 256 {
			return Scope{}, ErrInvalid
		}
	}
	return scope, nil
}
