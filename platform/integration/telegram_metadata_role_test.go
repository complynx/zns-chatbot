package integration_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestTelegramMetadataWithSplitBotRole(t *testing.T) {
	t.Parallel()
	f := memorySplitRoleFixture(t)
	_, err := f.b.DB.Exec(t.Context(), `UPDATE core.users SET first_name='forbidden' WHERE id='alice'`)
	var denied *pgconn.PgError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, "42501", denied.Code)
	update := message(19201, 101, "/language en")
	update.Message.From = telegram.User{ID: 101, FirstName: "Alice", LastName: "Example", Username: "alice_example"}
	update.Message.ForwardOrigin = &telegram.ForwardOrigin{
		Type:       "user",
		SenderUser: &telegram.User{ID: 202, FirstName: "Wrong"},
	}
	handle(t, f.b, update)
	assert.Equal(
		t,
		telegramMetadata{"alice_example", "Alice", "Example", "Alice Example", "Alice Example", 19201},
		readTelegramMetadata(t, f),
	)
	callback := aliceCallback(19202, 1, "language:ru")
	callback.Callback.From = telegram.User{ID: 101, FirstName: "Current sender"}
	callback.Callback.Message.From = telegram.User{ID: 202, FirstName: "Wrong author"}
	handle(t, f.b, callback)
	want := telegramMetadata{"", "Current sender", "", "Current sender", "Current sender", 19202}
	assert.Equal(t, want, readTelegramMetadata(t, f))
	handle(t, f.b, update)
	assert.Equal(t, want, readTelegramMetadata(t, f))
}

func TestTelegramMetadataHostBoundary(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handler := api.Handler(
		appservices.NewServices(f.db, appservices.Options{}),
		f.b.Host.Signer,
		slog.New(slog.DiscardHandler),
	)
	body := `{"sender":{"id":101,"first_name":"Trusted"},"update_id":19210}`
	call := func(token, payload string) int {
		request := httptest.NewRequest(http.MethodPost, "/internal/telegram/metadata", strings.NewReader(payload))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}
	before := readTelegramMetadata(t, f)
	assert.Equal(t, http.StatusUnauthorized, call(f.b.Host.Signer.Token("alice"), body))
	assert.Equal(t, http.StatusOK, call(f.b.Host.Signer.MemoryProvenanceToken("bob"), body))
	assert.Equal(t, before, readTelegramMetadata(t, f), "signed Bob cannot rename Alice's Telegram binding")
	assert.Equal(
		t,
		http.StatusBadRequest,
		call(
			f.b.Host.Signer.MemoryProvenanceToken("alice"),
			`{"sender":{"id":101,"first_name":"Injected"},"update_id":19210,"owner":"bob"}`,
		),
	)
	assert.Equal(t, http.StatusOK, call(f.b.Host.Signer.MemoryProvenanceToken("alice"), body))
	assert.Equal(t, "Trusted", readTelegramMetadata(t, f).FirstName)
}
