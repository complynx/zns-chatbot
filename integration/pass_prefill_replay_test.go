package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/passcopy"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

func TestPassEventPickerReplayPreservesProtectedFormCorrections(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	f := newPassQAFixture(t, passQAPromoTiers(start), true)
	user := f.user(t, 29104)
	picker, after := f.accept(t, user,
		`/passes --legal-name "Original Name" --passport-number ORIGINAL-123`, 0, "")
	f.h.await(t, picker, "completed")
	event := f.button(t, user, after, f.event.Document.Name)
	after = f.click(t, user, event)
	f.button(t, user, after, passcopy.Text("en", "entry.leader"))

	const name, passport = "Corrected Name", "CORRECTED-456"
	version := int64(2)
	for _, field := range []struct{ path, value string }{{"legal-name", name}, {"passport-number", passport}} {
		code, raw, _ := appCall(t, f.h, http.MethodPost, appDetailsPath+"/"+field.path,
			appDetailsBody(t, uuid.New(), version, field.value), user.app.Token, appTestOrigin)
		require.Equal(t, http.StatusOK, code, string(raw))
		version++
		require.Equal(t, version, appDetailsStatus(t, raw).Version)
	}
	after = f.click(t, user, event)
	f.button(t, user, after, passcopy.Text("en", "entry.leader"))

	var encrypted []byte
	var current int64
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(),
		"SELECT version,encrypted_record FROM shuttle_registration_details WHERE user_id=$1", user.app.UserID).
		Scan(&current, &encrypted))
	require.Equal(t, version, current)
	raw, err := f.p.registrationDetails.Open(user.app.UserID, encrypted)
	require.NoError(t, err)
	var record registrationdetails.Record
	require.NoError(t, json.Unmarshal([]byte(raw), &record))
	require.Equal(t, name, record.LegalName)
	require.Equal(t, passport, record.PassportNumber)
}

func TestPassEventPickerRejectsPrefillAfterNewerFormEdit(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	f := newPassQAFixture(t, passQAPromoTiers(start), true)
	user := f.user(t, 29105)
	picker, after := f.accept(t, user, `/passes --legal-name "Original Name" --passport-number ORIGINAL-123`, 0, "")
	f.h.await(t, picker, "completed")
	event := f.button(t, user, after, f.event.Document.Name)
	code, raw, _ := appCall(t, f.h, http.MethodPost, appDetailsPath+"/passport-number",
		appDetailsBody(t, uuid.New(), 0, "CORRECTED-456"), user.app.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))

	after = f.click(t, user, event)
	f.text(t, user, after, passcopy.Text("en", "entry.invalid"))
	code, raw, _ = appCall(t, f.h, http.MethodGet, appDetailsPath, "", user.app.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, registrationdetails.Status{Version: 1, PassportNumberPresent: true}, appDetailsStatus(t, raw))
}
