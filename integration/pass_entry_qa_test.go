package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/passcopy"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestPassEntryDirectPrefillAndCodeNeedNoOnboarding(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.registrationDetails = detailsProtector(t, 75)
	p.loomDown = true
	h := setup(t, p)
	const namedID, firstID, partnerID = int64(29100), int64(29101), int64(29102)
	named := passQANamedUser(t, h, namedID)
	event := passQAEvent(t, h, named, "direct-pass")
	doc := event.Document
	doc.Registration.DetailsRequirements = &registrationdetails.Requirements{PassportNumber: true}
	event, err := h.store.ChangeEvent(t.Context(), named.EventScope, named.Subject, uuid.NewString(), db.EventChange{
		ID: event.ID, ExpectedVersion: event.Version, Document: doc,
	})
	require.NoError(t, err)

	namedTask := passQAMessage(t, h, 29101, namedID,
		`/passes direct-pass --role leader --legal-name "Private Legal Name" --passport-number AA0000123`)
	namedReplies := passQAReplies(t, h, namedTask, namedID)
	require.Contains(t, namedReplies[0].Text, passcopy.Text("en", "ops.waitlistedState"))
	require.NotContains(t, namedReplies[0].Text, passcopy.Text("en", "entry.saved_waiting"))
	reg := passQARegistration(t, h, named, event.ID)
	require.Equal(t, registrations.Leader, reg.Role)
	details, err := h.store.RegistrationDetailsStatus(t.Context(), ownerProfileScope(t, h, named.UserID))
	require.NoError(t, err)
	require.True(t, details.LegalNamePresent)
	require.True(t, details.PassportNumberPresent)
	native, err := h.store.GetTask(t.Context(), namedTask)
	require.NoError(t, err)
	require.NotContains(t, string(native.Request), "Private Legal Name")
	require.NotContains(t, string(native.Request), "AA0000123")

	issuerTask := passQAMessage(t, h, 29102, firstID,
		`/passes direct-pass --role leader --pair --legal-name "Private Invite Issuer" --passport-number AA0000124`)
	passQAReplies(t, h, issuerTask, firstID)
	issuerNative, err := h.store.GetTask(t.Context(), issuerTask)
	require.NoError(t, err)
	issuer := paymentScope(t, h, issuerNative.UserID, false)
	invite, err := h.store.ActivePassInvite(t.Context(), issuer, event.ID)
	require.NoError(t, err)
	partnerTask := passQAMessage(t, h, 29103, partnerID,
		`/passes `+invite.Code+` --legal-name "Private Invite Claimant" --passport-number AA0000125`)
	partnerReplies := passQAReplies(t, h, partnerTask, partnerID)
	require.Contains(t, partnerReplies[0].Text, passcopy.Text("en", "entry.invite_paired"))
	partnerNative, err := h.store.GetTask(t.Context(), partnerTask)
	require.NoError(t, err)
	partner := paymentScope(t, h, partnerNative.UserID, false)
	require.Equal(t, registrations.Follower, passQARegistration(t, h, partner, event.ID).Role)
	for _, telegramID := range []int64{firstID, partnerID} {
		hasName, lookupErr := h.store.HasTelegramProfileName(t.Context(), named.BotID, telegramID, named.Organization)
		require.NoError(t, lookupErr)
		require.False(t, hasName)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.runs)
	require.Zero(t, p.loomUnavailableCalls)
}

func TestWaitlistedInviteParticipantsUpdatePassportsBeforePairing(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.registrationDetails = detailsProtector(t, 75)
	h := setup(t, p)
	const issuerID, claimantID = int64(29110), int64(29111)
	issuer := passQANamedUser(t, h, issuerID)
	claimant := passQANamedUser(t, h, claimantID)
	event := passQAEvent(t, h, issuer, "pair-correction")
	doc := event.Document
	doc.Registration.DetailsRequirements = &registrationdetails.Requirements{PassportNumber: true}
	event, err := h.store.ChangeEvent(t.Context(), issuer.EventScope, issuer.Subject, uuid.NewString(), db.EventChange{
		ID: event.ID, ExpectedVersion: event.Version, Document: doc,
	})
	require.NoError(t, err)
	join := passQAMessage(t, h, 29111, claimantID,
		`/passes pair-correction --role follower --legal-name "Claimant" --passport-number OLD-123`)
	passQAReplies(t, h, join, claimantID)
	require.Equal(t, registrations.Waitlisted, passQARegistration(t, h, claimant, event.ID).State)
	leader := passQAMessage(t, h, 29112, issuerID,
		`/passes pair-correction --role leader --legal-name "Issuer" --passport-number OLD-456`)
	passQAReplies(t, h, leader, issuerID)
	issue := passQAMessage(t, h, 29114, issuerID,
		`/passes pair-correction --pair --passport-number ISSUER-456`)
	passQAReplies(t, h, issue, issuerID)
	invite, err := h.store.ActivePassInvite(t.Context(), issuer, event.ID)
	require.NoError(t, err)
	claim := passQAMessage(t, h, 29113, claimantID,
		"/passes pair-correction --invite "+invite.Code+" --passport-number CORRECTED-789")
	reply := passQAReplies(t, h, claim, claimantID)
	require.Contains(t, reply[0].Text, passcopy.Text("en", "entry.invite_paired"))

	for id, expected := range map[uuid.UUID]string{issuer.UserID: "ISSUER-456", claimant.UserID: "CORRECTED-789"} {
		var ciphertext []byte
		require.NoError(t, h.store.Pool().QueryRow(t.Context(),
			"SELECT encrypted_record FROM shuttle_registration_details WHERE user_id=$1", id).Scan(&ciphertext))
		raw, openErr := p.registrationDetails.Open(id, ciphertext)
		require.NoError(t, openErr)
		var record registrationdetails.Record
		require.NoError(t, json.Unmarshal([]byte(raw), &record))
		require.Equal(t, expected, record.PassportNumber)
	}
}
