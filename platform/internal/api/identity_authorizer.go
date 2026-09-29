package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

const authorizerEnvelopeBytes = 64

type AuthorizerVerifier interface {
	Verify(context.Context, string) (identity.AuthorizerIdentity, error)
}
type ExternalIdentityLinker interface {
	EnsureExternal(context.Context, identityprovision.Telegram, string) (identityprovision.Binding, error)
}

// WithAuthorizerProvisioning accepts only the separate signed authorizer purpose.
func WithAuthorizerProvisioning(
	next http.Handler,
	verifier AuthorizerVerifier,
	linker ExternalIdentityLinker,
) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", next)
	mux.HandleFunc("POST /internal/identity/authorizer", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, identity.MaxAuthorizerAssertionBytes+authorizerEnvelopeBytes)
		var input struct {
			Assertion string `json:"assertion"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		verified, err := verifier.Verify(r.Context(), input.Assertion)
		if err != nil {
			JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
			return
		}
		_, err = linker.EnsureExternal(r.Context(), verified.Telegram, verified.Subject)
		if err != nil {
			status, code := http.StatusServiceUnavailable, "identity_provisioning_unavailable"
			if errors.Is(err, identityprovision.ErrConflict) {
				status, code = http.StatusConflict, "identity_conflict"
			}
			if errors.Is(err, identityprovision.ErrInvalid) {
				status, code = http.StatusBadRequest, invalidJSON
			}
			JSON(w, status, map[string]string{codeField: code})
			return
		}
		JSON(w, http.StatusOK, map[string]bool{"ready": true})
	})
	return mux
}
