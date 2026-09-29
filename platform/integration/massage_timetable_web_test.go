package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestMassageTimetableWebIdentityRightsAndFreshness(t *testing.T) {
	t.Parallel()
	f := massageBotFixture(t)
	ctx := t.Context()
	service := massage.Service{DB: f.db}
	booking, err := service.Execute(ctx, "alice", massageBook("web-timetable", "bob", 2, 1))
	require.NoError(t, err)
	handler := (miniapp.Gateway{API: f.b.API, Token: "test-token", EventID: "sandbox-festival"}).Handler()
	const path = "/miniapp/api/massage/timetable?event=sandbox-festival"
	for _, actor := range []int64{101, 9999} {
		response := webRequest(t, handler, http.MethodGet, path, actor, nil)
		assert.Equal(t, http.StatusForbidden, response.Code)
		assert.NotContains(t, response.Body.String(), booking.Owner)
	}
	response := webRequest(t, handler, http.MethodGet, path, 202, nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	var state struct {
		Owner    string           `json:"owner"`
		Calendar massage.Calendar `json:"calendar"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &state))
	assert.Equal(t, "bob", state.Owner)
	require.Len(t, state.Calendar.Bookings, 1)
	assert.Equal(t, booking.ID, state.Calendar.Bookings[0].ID)
	assert.NotEmpty(t, state.Calendar.Clients)
	assert.NotEmpty(t, state.Calendar.Providers[0].Work)
	other := webRequest(t, handler, http.MethodGet, "/miniapp/api/massage/timetable?event=other", 202, nil)
	assert.Equal(t, http.StatusForbidden, other.Code)
	_, err = f.db.Exec(ctx, `INSERT INTO core.order_admins(event_id,owner,country)
 VALUES('sandbox-festival','visitor','be') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, webRequest(t, handler, http.MethodGet, path, 303, nil).Code)
	_, err = service.Execute(ctx, "alice", massage.Command{
		Key: "cancel-web", Action: "cancel", Event: booking.Event, Booking: booking.ID, Version: booking.Version,
	})
	require.NoError(t, err)
	response = webRequest(t, handler, http.MethodGet, path, 202, nil)
	require.Equal(t, http.StatusOK, response.Code)
	assert.NotContains(t, response.Body.String(), booking.ID)
	_, err = f.db.Exec(ctx, `DELETE FROM core.order_admins WHERE event_id='sandbox-festival' AND owner='visitor'`)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, webRequest(t, handler, http.MethodGet, path, 303, nil).Code)
	identities := []string{
		"", sandbox.WebAppInitData(telegram.User{ID: 202}, "wrong-token", time.Now()),
		sandbox.WebAppInitData(telegram.User{ID: 202}, "test-token", time.Now().Add(-2*time.Hour)),
	}
	for _, auth := range identities {
		request := httptest.NewRequest(http.MethodGet, path+"&owner=bob", nil)
		request.Header.Set("Authorization", "tma "+auth)
		request.Header.Set("X-Miniapp-Owner", "bob")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code)
		assert.NotContains(t, recorder.Body.String(), booking.Owner)
	}
}

func TestMassageTimetableWebEntryUsesAuthorizedEvent(t *testing.T) {
	t.Parallel()
	f := massageBotFixture(t)
	f.b.WebAppURL = "https://bot.example/miniapp/?order_id=old"
	handle(t, f.b, message(9901, 202, "/massage"))
	card := massageCard(t, f, 202)
	found := false
	for _, row := range card.Markup.Rows {
		for _, button := range row {
			if button.WebApp != nil {
				found = true
				assert.Equal(t, "https://bot.example/miniapp/massage?event=sandbox-festival&lang=ru", button.WebApp.URL)
			}
		}
	}
	assert.True(t, found)
	handle(t, f.b, message(9902, 101, "/massage"))
	for _, row := range massageCard(t, f, 101).Markup.Rows {
		for _, button := range row {
			assert.Nil(t, button.WebApp)
		}
	}
}
