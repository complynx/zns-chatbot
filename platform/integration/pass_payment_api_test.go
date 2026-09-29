package integration_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassPaymentAPIFileIsolation(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("p", 32))}
	handler := api.Handler(appservices.NewServices(db, appservices.Options{}), signer, slog.New(slog.DiscardHandler))
	request := func(actor, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if actor != "" {
			r.Header.Set("Authorization", "Bearer "+signer.Token(actor))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	assert.Equal(
		t,
		http.StatusUnauthorized,
		request("", http.MethodPost, "/v1/pass-proofs?filename=receipt.txt", "receipt").Code,
	)
	w := request("alice", http.MethodPost, "/v1/pass-proofs?filename=receipt.txt", "synthetic receipt")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var proof orders.Proof
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &proof))
	alice, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	command := bookingCommand("proof", "upload", alice)
	command.ProofID = proof.ID
	_, err = service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	path := "/v1/passes/events/dance/participants/alice/payment/file"
	for _, owner := range []string{"alice", "bob"} {
		w = request(owner, http.MethodGet, path, "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "synthetic receipt", w.Body.String())
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
		assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
		assert.NotEmpty(t, w.Header().Get("X-Payment-Attempt"))
	}
	w = request("visitor", http.MethodGet, path, "")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.NotContains(t, w.Body.String(), "synthetic receipt")
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, request("bob", http.MethodGet, path, "").Code)
}
