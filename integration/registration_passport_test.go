package integration_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

func TestProtectedPassportFormUsesEventGroupsAndPhoto(t *testing.T) {
	t.Parallel()
	h, _, session := detailsApp(t)
	scope := ownerProfileScope(t, h, session.UserID)
	_, admin := registrationUser(t, h, 95721, true)
	event := registrationEvent(t, h, admin)
	event.Document.Registration.DetailsRequirements = &registrationdetails.Requirements{
		PassportCountries: []string{"RU", "BY"},
		PassportPhoto:     true,
	}
	event, err := h.store.ChangeEvent(
		t.Context(),
		db.EventScope{BotID: 123, Organization: "org"},
		"fixture",
		uuid.NewString(),
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: event.Document},
	)
	require.NoError(t, err)
	req := beginDetailsRequest(t, h, scope, event, uuid.New())
	getForm := func() (string, []registrationdetails.Field) {
		code, raw, _ := appCall(
			t,
			h,
			http.MethodGet,
			appDetailsPath+"/requests/"+req.Request.ID.String(),
			"",
			session.Token,
			"",
		)
		require.Equal(t, http.StatusOK, code, string(raw))
		var form struct {
			db.DetailsRequestStatus

			CountryCategory string   `json:"country_category"`
			Countries       []string `json:"countries"`
		}
		require.NoError(t, json.Unmarshal(raw, &form))
		require.Contains(t, form.Countries, "BY")
		require.Contains(t, form.Countries, "NL")
		return form.CountryCategory, form.MissingFields
	}
	var version int64
	save := func(field, value string) {
		body := appDetailsBody(t, uuid.New(), version, value)
		code, raw, _ := appCall(t, h, http.MethodPost, appDetailsPath+"/"+field, body, session.Token, appTestOrigin)
		require.Equal(t, http.StatusOK, code, string(raw))
		var status registrationdetails.Status
		require.NoError(t, json.Unmarshal(raw, &status))
		version = status.Version
	}
	save("legal-name", "Synthetic Passport Owner")
	save("passport-country", "BY")
	category, missing := getForm()
	require.Equal(t, "BY", category)
	require.Equal(t, []registrationdetails.Field{registrationdetails.PassportPhoto}, missing)
	save("passport-country", "NL")
	category, _ = getForm()
	require.Equal(t, "foreign", category)
	var photo bytes.Buffer
	require.NoError(t, png.Encode(&photo, image.NewGray(image.Rect(0, 0, 3, 2))))
	save("passport-photo", base64.StdEncoding.EncodeToString(photo.Bytes()))
	category, missing = getForm()
	require.Equal(t, "foreign", category)
	require.Empty(t, missing)
	ready, err := h.store.DetailsRequest(t.Context(), scope, req.Request.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", ready.State)
}
