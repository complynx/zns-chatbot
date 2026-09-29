package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

const derivedHostHeader = "X-Zns-Derivation"
const maxDerivedOrderCommandBytes = 320 << 10

func derivedMutationRoutes(
	mux *http.ServeMux,
	service derivedmutation.Service,
	authorizer applicationauth.Authorizer,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.Handle("POST /internal/derived/order-actions", authenticatedDerivation(authorizer, signer,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command orders.Command
			source, err := decodeDerivedMutation(w, r, &command, maxDerivedOrderCommandBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.ExecuteOrder(r.Context(), requestOwner(r), command, source)
			respond(logger, w, value, err)
		}), logger))
	mux.Handle("POST /internal/derived/actions", authenticatedDerivation(authorizer, signer,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command workflow.Action
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.ExecuteWorkflow(r.Context(), requestOwner(r), command, source)
			respond(logger, w, value, err)
		}), logger))
}

// Host evidence and a live user credential are both required. Only the shared
// application authorizer resolves the user; the signed host owner must match it.
func authenticatedDerivation(
	authorizer applicationauth.Authorizer,
	signer identity.Signer,
	next http.Handler,
	logger *slog.Logger,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, err := signer.VerifyDerivedMutation(r.Header.Get(derivedHostHeader))
		if err != nil {
			JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
			return
		}
		authenticated(authorizer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if actor != requestOwner(r) {
				JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
				return
			}
			next.ServeHTTP(w, r)
		}), logger).ServeHTTP(w, r)
	})
}

func decodeDerivedMutation(
	w http.ResponseWriter,
	r *http.Request,
	command any,
	commandLimit int,
) (readsource.Derivation, error) {
	// Authority bytes are independent of the existing public command envelope.
	const envelopeBytes = 4096
	limit := int64(readsource.MaxAuthorityBytes + commandLimit + envelopeBytes)
	var input struct {
		Command json.RawMessage        `json:"command"`
		Source  *readsource.Derivation `json:"source"`
	}
	if err := decodeDerivedJSON(http.MaxBytesReader(w, r.Body, limit), &input); err != nil {
		return readsource.Derivation{}, err
	}
	if input.Source == nil || !input.Source.Valid() || len(input.Command) == 0 ||
		len(input.Command) > commandLimit || bytes.Equal(bytes.TrimSpace(input.Command), []byte("null")) {
		return readsource.Derivation{}, errors.New("invalid derived mutation")
	}
	if err := decodeDerivedJSON(bytes.NewReader(input.Command), command); err != nil {
		return readsource.Derivation{}, err
	}
	return *input.Source, nil
}

func decodeDerivedJSON(reader io.Reader, value any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("trailing derived mutation JSON")
	}
	return nil
}
