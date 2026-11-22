package integration_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
	"github.com/complynx/zns-chatbot/migrations"
)

const identityExportAction = "identity.zip"

func TestPassIdentityExportUsesCurrentEventRequirementsAndAuthority(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.registrationDetails = detailsProtector(t, 88)
	h := setup(t, p)
	firstID, firstToken := registrationUser(t, h, 98802, false)
	_, returnedToken := registrationUser(t, h, 98803, false)
	waitlistedID, waitlistedToken := registrationUser(t, h, 98804, false)
	_, otherToken := registrationUser(t, h, 98805, false)
	operatorID, operatorToken := registrationUser(t, h, 98801, true)
	operator := paymentScope(t, h, operatorID, true)
	operatorSession := detailsAppSession(t, h, 98801, 151)
	fullPolicy := registrationdetails.Requirements{
		PassportNumber: true, PassportCountries: []string{"BY", "RU"}, PassportPhoto: true,
	}
	fullEvent := identityExportEvent(t, h, operator, operatorToken, fullPolicy)
	nameEvent := identityExportEvent(t, h, operator, operatorToken, registrationdetails.Requirements{LegalName: true})
	firstSession := detailsAppSession(t, h, 98802, 152)
	const firstName, firstNumber = "Synthetic Identity & First", "000000000123456789"
	const returnedName, returnedNumber = "Synthetic Identity Returned", "RETURNED-390176"
	const waitlistedName, waitlistedNumber = "Synthetic Identity Waiting", "WAITING-847593"
	const otherName, otherNumber = "Synthetic Identity Other Event", "OTHER-461295"
	firstPhoto := saveIdentityExportDetails(t, h, firstSession, 3, firstName, firstNumber, "NL")
	saveIdentityExportDetails(t, h, detailsAppSession(t, h, 98803, 153), 4, returnedName, returnedNumber, "BY")
	ambassadorSession := detailsAppSession(t, h, 98804, 154)
	saveIdentityExportDetails(t, h, ambassadorSession, 5, waitlistedName, waitlistedNumber, "RU")
	saveIdentityExportDetails(t, h, detailsAppSession(t, h, 98805, 155), 6, otherName, otherNumber, "RU")
	changeProfile(t, h, ownerProfileScope(t, h, firstID), personName(0, "Public", "Display"))

	first := assignIdentityExportParticipant(t, h, operatorToken, firstToken, fullEvent, registrations.Leader)
	returned := assignIdentityExportParticipant(t, h, operatorToken, returnedToken, fullEvent, registrations.Follower)
	waitlisted := registerForEvent(t, h, waitlistedToken, fullEvent.ID, "identity-waitlisted", registrations.Leader)
	require.Equal(t, registrations.Waitlisted, waitlisted.State)
	nameOnly := assignIdentityExportParticipant(t, h, operatorToken, otherToken, nameEvent, registrations.Follower)

	makePassAdminChange(t, h, operatorSession, fullEvent.ID, "return", db.PassAdminChange{
		OperationID: uuid.New(), RegistrationID: returned.ID, ExpectedVersion: returned.Version,
		Reason: "Return fixture before identity export",
	}, true, http.StatusOK)
	grant := func(event, user uuid.UUID, role string, version int64) db.EventAuthority {
		authority, err := h.store.ChangeAuthority(t.Context(), operator.EventScope, operator.Subject,
			uuid.NewString(), db.AuthorityChange{EventID: event, UserID: user, Role: role, ExpectedVersion: version}, uuid.Nil)
		require.NoError(t, err)
		return authority
	}
	fullAuthority := grant(fullEvent.ID, operatorID, db.AuthorityAdministrator, 0)
	grant(fullEvent.ID, waitlistedID, db.AuthorityAmbassador, 0)
	p.mu.Lock()
	p.adminRole = false
	p.mu.Unlock()
	h.iam.Invalidate(operator.Subject)
	for _, userID := range []uuid.UUID{firstSession.UserID, ambassadorSession.UserID} {
		h.iam.Invalidate(paymentScope(t, h, userID, false).Subject)
	}

	for _, session := range []appSession{firstSession, ambassadorSession} {
		status, body, headers := passAdminAppCall(t, h, session, http.MethodGet, fullEvent.ID, identityExportAction, nil)
		requireIdentityExportDenied(t, http.StatusForbidden, status, body, headers)
	}
	status, body, headers := passAdminAppCall(
		t,
		h,
		operatorSession,
		http.MethodGet,
		nameEvent.ID,
		identityExportAction,
		nil,
	)
	requireIdentityExportDenied(t, http.StatusForbidden, status, body, headers)

	status, body, _ = passAdminAppCall(t, h, operatorSession, http.MethodGet, fullEvent.ID, "registrations", nil)
	require.Equal(t, http.StatusOK, status, string(body))
	require.Contains(t, string(body), "Public Display")
	require.Contains(t, string(body), returned.ID.String())
	require.Contains(t, string(body), waitlisted.ID.String())
	assertNoIdentityExportValues(t, string(body), firstName, firstNumber, returnedName, returnedNumber,
		waitlistedName, waitlistedNumber, otherName, otherNumber, "passport_number", "passport_country", "passport_photo")
	status, body, _ = passAdminAppCall(t, h, operatorSession, http.MethodGet, fullEvent.ID, "export.xlsx", nil)
	require.Equal(t, http.StatusOK, status, string(body))
	ordinaryParts := readPassAdminWorkbook(t, body)
	require.Contains(t, ordinaryParts["xl/worksheets/sheet1.xml"], "Public Display")
	for _, part := range ordinaryParts {
		assertNoIdentityExportValues(t, part, firstNumber, returnedName, returnedNumber, waitlistedName,
			waitlistedNumber, otherName, otherNumber, "Synthetic Identity", "passport_country", "passport_photo")
	}

	files, parts, rows := downloadIdentityExport(t, h, operatorSession, fullEvent.ID)
	require.Len(t, files, 2)
	photoPath := "photos/" + first.ID.String() + ".png"
	require.Equal(t, string(firstPhoto), files[photoPath])
	require.Len(t, rows, 1)
	require.Equal(t, first.ID.String(), rows[0]["registration_id"].Text)
	require.Equal(t, firstName, rows[0]["legal_name"].Text)
	require.Equal(t, firstNumber, rows[0]["passport_number"].Text)
	require.Equal(t, "NL", rows[0]["passport_country"].Text)
	require.Equal(t, "foreign", rows[0]["country_category"].Text)
	require.Equal(t, photoPath, rows[0]["passport_photo"].Text)
	for _, part := range parts {
		assertNoIdentityExportValues(t, part, returnedName, returnedNumber, waitlistedName, waitlistedNumber,
			otherName, otherNumber, returned.ID.String(), waitlisted.ID.String(), nameOnly.ID.String(), "Public Display")
	}

	grant(nameEvent.ID, operatorID, db.AuthorityAdministrator, 0)
	files, parts, rows = downloadIdentityExport(t, h, operatorSession, nameEvent.ID)
	require.Len(t, files, 1)
	require.Len(t, rows, 1)
	require.Equal(t, nameOnly.ID.String(), rows[0]["registration_id"].Text)
	require.Equal(t, otherName, rows[0]["legal_name"].Text)
	for _, field := range []string{"passport_number", "passport_country", "country_category", "passport_photo"} {
		require.Empty(t, rows[0][field].Text, field)
	}
	for _, part := range parts {
		assertNoIdentityExportValues(t, part, otherNumber, firstNumber, "Synthetic Identity &amp; First",
			first.ID.String(), ">RU<", "photos/", "passport_number", "passport_country", "country_category", "passport_photo")
	}

	// An event cannot acquire previously saved private data by expanding its policy after enrollment.
	changeIdentityExportPolicy(t, h, operator, nameEvent, fullPolicy)
	files, parts, rows = downloadIdentityExport(t, h, operatorSession, nameEvent.ID)
	require.Len(t, files, 1)
	require.Len(t, rows, 1)
	require.Equal(t, nameOnly.ID.String(), rows[0]["registration_id"].Text)
	require.Equal(t, otherName, rows[0]["legal_name"].Text)
	for _, field := range []string{"passport_number", "passport_country", "country_category", "passport_photo"} {
		require.Empty(t, rows[0][field].Text, field)
	}
	for _, part := range parts {
		assertNoIdentityExportValues(t, part, otherNumber, ">RU<", "photos/")
	}

	grant(fullEvent.ID, operatorID, db.AuthorityNone, fullAuthority.Version)
	status, body, headers = passAdminAppCall(
		t,
		h,
		operatorSession,
		http.MethodGet,
		fullEvent.ID,
		identityExportAction,
		nil,
	)
	requireIdentityExportDenied(t, http.StatusForbidden, status, body, headers)
	requireIdentityExportLogoutDuringEventWait(t, h, operatorSession, nameEvent.ID)
}

func identityExportEvent(
	t *testing.T, h *harness, operator db.PaymentScope, token string, requirements registrationdetails.Requirements,
) events.Event {
	t.Helper()
	event := registrationEvent(t, h, token)
	return changeIdentityExportPolicy(t, h, operator, event, requirements)
}

func changeIdentityExportPolicy(
	t *testing.T, h *harness, operator db.PaymentScope, event events.Event, requirements registrationdetails.Requirements,
) events.Event {
	t.Helper()
	event.Document.Registration.DetailsRequirements = &requirements
	event, err := h.store.ChangeEvent(t.Context(), operator.EventScope, operator.Subject, uuid.NewString(),
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: event.Document})
	require.NoError(t, err)
	return event
}

func saveIdentityExportDetails(
	t *testing.T,
	h *harness,
	session appSession,
	width int,
	name, number, country string,
) []byte {
	t.Helper()
	var photo bytes.Buffer
	require.NoError(t, png.Encode(&photo, image.NewGray(image.Rect(0, 0, width, 2))))
	for i, field := range []struct{ name, value string }{
		{"legal-name", name},
		{"passport-number", number},
		{"passport-country", country},
		{"passport-photo", base64.StdEncoding.EncodeToString(photo.Bytes())},
	} {
		body := appDetailsBody(t, uuid.New(), int64(i), field.value)
		code, raw, _ := appCall(t, h, http.MethodPost, appDetailsPath+"/"+field.name, body, session.Token, appTestOrigin)
		require.Equal(t, http.StatusOK, code, string(raw))
	}
	return photo.Bytes()
}

func assignIdentityExportParticipant(
	t *testing.T, h *harness, operator, participant string, event events.Event, role string,
) registrations.Registration {
	t.Helper()
	registration := registerForEvent(t, h, participant, event.ID, uuid.NewString(), role)
	return assignIdentityExportRegistration(t, h, operator, event, registration)
}

func assignIdentityExportRegistration(
	t *testing.T, h *harness, operator string, event events.Event, registration registrations.Registration,
) registrations.Registration {
	t.Helper()
	prepared := prepareAssignment(t, h, operator, uuid.NewString(), event, registration)
	status, body := executeAssignment(t, h, operator, prepared)
	require.Equal(t, http.StatusOK, status, string(body))
	assigned := assignmentResult(t, body)
	require.Equal(t, registrations.Assigned, assigned.State)
	require.NotNil(t, assigned.Pass)
	return assigned
}

func assertNoIdentityExportValues(t *testing.T, body string, values ...string) {
	t.Helper()
	for _, value := range values {
		require.NotContains(t, body, value)
	}
}

func downloadIdentityExport(t *testing.T, h *harness, session appSession, event uuid.UUID) (
	map[string]string, map[string]string, []map[string]passAdminWorkbookCell,
) {
	t.Helper()
	status, body, headers := passAdminAppCall(t, h, session, http.MethodGet, event, identityExportAction, nil)
	require.Equal(t, http.StatusOK, status, string(body))
	require.Equal(t, "application/zip", headers.Get("Content-Type"))
	require.Contains(t, headers.Get("Cache-Control"), "no-store")
	require.Equal(t, "nosniff", headers.Get("X-Content-Type-Options"))
	require.Equal(t, `attachment; filename="identity-`+event.String()+`.zip"`, headers.Get("Content-Disposition"))
	files := readPassAdminWorkbook(t, body)
	require.Contains(t, files, "participants.xlsx")
	workbook := readPassAdminWorkbook(t, []byte(files["participants.xlsx"]))
	require.Contains(t, workbook["xl/workbook.xml"], "Participants")
	worksheet := passAdminWorksheet(t, workbook["xl/worksheets/sheet1.xml"])
	return files, workbook, passAdminWorksheetData(t, worksheet)
}

func requireIdentityExportDenied(t *testing.T, expected, actual int, body []byte, headers http.Header) {
	t.Helper()
	require.Equal(t, expected, actual, string(body))
	require.Empty(t, headers.Get("Content-Disposition"))
	require.True(t, strings.HasPrefix(headers.Get("Content-Type"), "application/json"))
	require.Contains(t, headers.Get("Cache-Control"), "no-store")
}

func requireIdentityExportLogoutDuringEventWait(t *testing.T, h *harness, session appSession, event uuid.UUID) {
	t.Helper()
	blocking, err := h.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(context.Background()) }()
	_, err = blocking.Exec(t.Context(), "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", event)
	require.NoError(t, err)
	type response struct {
		status  int
		body    []byte
		headers http.Header
		err     error
	}
	done := make(chan response, 1)
	go func() {
		status, body, headers, requestErr := appHTTPCall(h, http.MethodGet,
			"/app/pass-admin/events/"+event.String()+"/"+identityExportAction, "", session.Token, appTestOrigin)
		done <- response{status, body, headers, requestErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		waitErr := h.store.Pool().QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity "+
			"WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))",
			blocking.Conn().PgConn().PID()).Scan(&waiting)
		return waitErr == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	code, body, _ := appCall(t, h, http.MethodPost, "/app/session/logout", "{}",
		session.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code, string(body))
	require.NoError(t, blocking.Rollback(t.Context()))
	select {
	case result := <-done:
		require.NoError(t, result.err)
		requireIdentityExportDenied(t, http.StatusUnauthorized, result.status, result.body, result.headers)
	case <-time.After(8 * time.Second):
		t.Fatal("identity export did not finish after releasing the event lock")
	}
}

func TestPassIdentityExportBackfillsOnlyDocumentedLegacyRequirements(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.registrationDetails = detailsProtector(t, 89)
	h := prepareHarness(t, p)
	driver := stdlib.OpenDBFromPool(h.store.Pool())
	t.Cleanup(func() { require.NoError(t, driver.Close()) })
	provider, err := goose.NewProvider(goose.DialectPostgres, driver, migrations.Files)
	require.NoError(t, err)
	_, err = provider.DownTo(t.Context(), 90)
	require.NoError(t, err)
	version, err := provider.GetDBVersion(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 90, version)
	h.start(t)
	t.Cleanup(func() { h.stop(t) })

	const completeName, completeNumber = "Synthetic Historical Complete", "HISTORICAL-43015"
	const lateName, lateNumber = "Synthetic Historical Late", "HISTORICAL-56093"
	const staleName, staleNumber = "Synthetic Historical Stale", "HISTORICAL-81064"
	const missingName, missingNumber = "Synthetic Historical Missing", "HISTORICAL-26031"
	completeID, _ := registrationUser(t, h, 98902, false)
	lateID, _ := registrationUser(t, h, 98903, false)
	staleID, _ := registrationUser(t, h, 98904, false)
	missingID, _ := registrationUser(t, h, 98905, false)
	operatorID, operatorToken := registrationUser(t, h, 98901, true)
	operator := paymentScope(t, h, operatorID, true)
	session := detailsAppSession(t, h, 98901, 161)
	policy := registrationdetails.Requirements{PassportNumber: true}
	event := identityExportEvent(t, h, operator, operatorToken, policy)
	saveIdentityExportDetails(t, h, detailsAppSession(t, h, 98902, 162), 3, completeName, completeNumber, "NL")
	saveIdentityExportDetails(t, h, detailsAppSession(t, h, 98903, 163), 4, lateName, lateNumber, "BY")
	saveIdentityExportDetails(t, h, detailsAppSession(t, h, 98904, 164), 5, staleName, staleNumber, "RU")
	saveIdentityExportDetails(t, h, detailsAppSession(t, h, 98905, 165), 6, missingName, missingNumber, "NL")

	completedAt := func(owner uuid.UUID, selected events.Event) time.Time {
		t.Helper()
		receipt := beginDetailsRequest(t, h, ownerProfileScope(t, h, owner), selected, uuid.New())
		require.Equal(t, "completed", receipt.State)
		require.NotNil(t, receipt.Request.CompletedAt)
		return *receipt.Request.CompletedAt
	}
	staleAt := completedAt(staleID, event)
	event = changeIdentityExportPolicy(t, h, operator, event, policy)
	completeAt := completedAt(completeID, event)
	lateAt := completedAt(lateID, event)
	complete := insertLegacyAssignedIdentityRegistration(t, h, event.ID, completeID, completeAt.Add(time.Minute))
	late := insertLegacyAssignedIdentityRegistration(t, h, event.ID, lateID, lateAt.Add(-time.Minute))
	stale := insertLegacyAssignedIdentityRegistration(t, h, event.ID, staleID, staleAt.Add(time.Minute))
	missing := insertLegacyAssignedIdentityRegistration(t, h, event.ID, missingID, completeAt.Add(time.Minute))

	require.NoError(t, h.store.Migrate(t.Context()))
	version, err = provider.GetDBVersion(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 92, version)
	assertExport := func() {
		t.Helper()
		files, parts, rows := downloadIdentityExport(t, h, session, event.ID)
		require.Len(t, files, 1)
		require.Len(t, rows, 4)
		names := map[string]string{
			complete.String(): completeName,
			late.String():     "",
			stale.String():    "",
			missing.String():  "",
		}
		for _, row := range rows {
			id := row["registration_id"].Text
			require.Contains(t, names, id)
			require.Equal(t, names[id], row["legal_name"].Text)
			delete(names, id)
			if id == complete.String() {
				require.Equal(t, completeNumber, row["passport_number"].Text)
			} else {
				require.Empty(t, row["passport_number"].Text)
			}
			require.Empty(t, row["passport_country"].Text)
			require.Empty(t, row["country_category"].Text)
			require.Empty(t, row["passport_photo"].Text)
		}
		for _, part := range parts {
			assertNoIdentityExportValues(t, part, lateName, lateNumber, staleName, staleNumber, missingName, missingNumber,
				">NL<", ">BY<", ">RU<", "photos/")
		}
	}
	assertExport()

	policy.PassportCountries = []string{"BY", "RU"}
	policy.PassportPhoto = true
	changeIdentityExportPolicy(t, h, operator, event, policy)
	assertExport()
	requireIdentityHistorySurvivesDowngrade(t, h, provider, complete)
}

func requireIdentityHistorySurvivesDowngrade(t *testing.T, h *harness, provider *goose.Provider, id uuid.UUID) {
	t.Helper()
	pool, ctx := h.store.Pool(), t.Context()
	// Recreate the first enrollment write, still uncommitted when rollback starts.
	_, err := pool.Exec(ctx, "DELETE FROM shuttle_registration_identity_fields WHERE registration_id=$1", id)
	require.NoError(t, err)
	enroll, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = enroll.Rollback(ctx) }()
	_, err = enroll.Exec(ctx, "INSERT INTO shuttle_registration_identity_fields VALUES($1,true,false,false,false)", id)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, downErr := provider.ApplyVersion(ctx, 91, false)
		done <- downErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		waitErr := pool.QueryRow(ctx, "SELECT EXISTS(SELECT FROM pg_stat_activity "+
			"WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))",
			enroll.Conn().PgConn().PID()).Scan(&waiting)
		return waitErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, enroll.Commit(ctx))
	require.ErrorContains(t, <-done, "preserve registration identity field history")
	var preserved bool
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT legal_name FROM shuttle_registration_identity_fields WHERE registration_id=$1", id).Scan(&preserved))
	require.True(t, preserved)
}

// v90 registrations and issued passes had no enrollment-time field metadata.
func insertLegacyAssignedIdentityRegistration(
	t *testing.T, h *harness, event, owner uuid.UUID, at time.Time,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := h.store.Pool().Exec(t.Context(), `
WITH enrollment AS (
	INSERT INTO shuttle_registrations(id,event_id,user_id,role,state,version,requested_at)
	VALUES($1,$2,$3,'leader','assigned',2,$4::timestamptz) RETURNING id
)
INSERT INTO shuttle_passes(id,registration_id,price_minor,currency,currency_scale,assigned_at,payment_due_at)
SELECT $5,id,12345,'BYN',2,$4,$4+interval '2 hours' FROM enrollment`, id, event, owner, at, uuid.New())
	require.NoError(t, err)
	return id
}

func TestPassIdentityExportGrantsNewFieldsOnlyThroughSelectedEventRequest(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.registrationDetails = detailsProtector(t, 92)
	h := setup(t, p)
	_, firstToken := registrationUser(t, h, 99102, false)
	_, secondToken := registrationUser(t, h, 99103, false)
	operatorID, operatorToken := registrationUser(t, h, 99101, true)
	operator := paymentScope(t, h, operatorID, true)
	operatorSession := detailsAppSession(t, h, 99101, 171)
	first := detailsAppSession(t, h, 99102, 172)
	second := detailsAppSession(t, h, 99103, 173)

	const firstName, sharedNumber = "Synthetic Event Request Participant", "REQUEST-SYNTHETIC-47387"
	const secondName, genericNumber = "Synthetic Other Request Participant", "GENERIC-SYNTHETIC-72154"
	const otherNumber = "OTHER-REQUEST-SYNTHETIC-83305"
	saveIdentityExportDetails(t, h, first, 5, firstName, "GLOBALLY-SAVED-FIRST", "RU")
	secondPhoto := saveIdentityExportDetails(t, h, second, 7, secondName, "GLOBALLY-SAVED-SECOND", "NL")
	nameOnly := registrationdetails.Requirements{LegalName: true}
	current := changeIdentityExportPolicy(t, h, operator, passQAEvent(t, h, operator, "identity-grant"), nameOnly)
	other := identityExportEvent(t, h, operator, operatorToken, nameOnly)
	firstEntry := registerForEvent(t, h, firstToken, current.ID, "identity-grant-first", registrations.Leader)
	secondEntry := registerForEvent(t, h, secondToken, current.ID, "identity-grant-second", registrations.Follower)
	otherEntry := registerForEvent(t, h, secondToken, other.ID, "identity-grant-other", registrations.Follower)
	for _, entry := range []registrations.Registration{firstEntry, secondEntry, otherEntry} {
		require.Equal(t, registrations.Waitlisted, entry.State)
	}

	expanded := registrationdetails.Requirements{PassportNumber: true, PassportPhoto: true}
	current = changeIdentityExportPolicy(t, h, operator, current, expanded)
	other = changeIdentityExportPolicy(t, h, operator, other, expanded)
	openRequest := telegramIdentityExportRequest(t, h, first, second, current, 99102, 99121)
	secondScope := ownerProfileScope(t, h, second.UserID)
	passiveRequest := beginDetailsRequest(t, h, secondScope, current, uuid.New())
	require.Equal(t, "completed", passiveRequest.State, "globally present fields still require explicit submission")
	secondRequest := beginDetailsRequest(t, h, secondScope, other, uuid.New()).Request.ID

	secondStatus := writePassIdentityGrantField(t, h, second, uuid.New(), uuid.Nil, 4, "passport-number", genericNumber)
	secondStatus = writePassIdentityGrantField(
		t, h, second, uuid.New(), secondRequest, secondStatus.Version, "passport-number", otherNumber,
	)
	writePassIdentityGrantField(t, h, second, uuid.New(), secondRequest, secondStatus.Version,
		"passport-photo", base64.StdEncoding.EncodeToString(secondPhoto))

	assignIdentityExportRegistration(t, h, operatorToken, current, firstEntry)
	assignIdentityExportRegistration(t, h, operatorToken, current, secondEntry)
	assignIdentityExportRegistration(t, h, operatorToken, other, otherEntry)

	// Opening a completed form with globally saved values does not share them.
	previewFiles, _, previewRows := downloadIdentityExport(t, h, operatorSession, current.ID)
	require.Len(t, previewFiles, 1)
	require.Len(t, previewRows, 2)
	for _, row := range previewRows {
		require.NotEmpty(t, row["legal_name"].Text)
		require.Empty(t, row["passport_number"].Text)
		require.Empty(t, row["passport_photo"].Text)
	}

	// The established registration still has a private details form after sales close.
	current.Document.Registration.ClosesAt = time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	current = changeIdentityExportPolicy(t, h, operator, current, expanded)
	require.Contains(t, current.Document.Modules, "registrations")
	firstRequest := telegramIdentityExportRequest(t, h, first, second, current, 99102, 99122)
	require.NotEqual(t, openRequest, firstRequest)

	numberOperation := uuid.New()
	firstStatus := writePassIdentityGrantField(
		t,
		h,
		first,
		numberOperation,
		firstRequest,
		4,
		"passport-number",
		sharedNumber,
	)
	var firstPhoto bytes.Buffer
	require.NoError(t, png.Encode(&firstPhoto, image.NewGray(image.Rect(0, 0, 8, 2))))
	photoStatus := writePassIdentityGrantField(t, h, first, uuid.New(), firstRequest, firstStatus.Version,
		"passport-photo", base64.StdEncoding.EncodeToString(firstPhoto.Bytes()))
	require.Greater(t, photoStatus.Version, firstStatus.Version)
	replayed := writePassIdentityGrantField(t, h, first, numberOperation, firstRequest, 4, "passport-number", sharedNumber)
	require.Equal(t, firstStatus, replayed)

	files, parts, rows := downloadIdentityExport(t, h, operatorSession, current.ID)
	require.Len(t, files, 2)
	require.Equal(t, firstPhoto.String(), files["photos/"+firstEntry.ID.String()+".png"])
	require.Len(t, rows, 2)
	byID := make(map[string]map[string]passAdminWorkbookCell, len(rows))
	for _, row := range rows {
		byID[row["registration_id"].Text] = row
	}
	require.Equal(t, firstName, byID[firstEntry.ID.String()]["legal_name"].Text)
	require.Equal(t, sharedNumber, byID[firstEntry.ID.String()]["passport_number"].Text)
	require.Equal(t, "photos/"+firstEntry.ID.String()+".png", byID[firstEntry.ID.String()]["passport_photo"].Text)
	require.Equal(t, secondName, byID[secondEntry.ID.String()]["legal_name"].Text)
	require.Empty(t, byID[secondEntry.ID.String()]["passport_number"].Text)
	require.Empty(t, byID[secondEntry.ID.String()]["passport_photo"].Text)
	for _, part := range parts {
		assertNoIdentityExportValues(t, part, otherNumber, genericNumber, "GLOBALLY-SAVED-FIRST", "GLOBALLY-SAVED-SECOND",
			"passport_country", "country_category", otherEntry.ID.String())
	}

	files, _, rows = downloadIdentityExport(t, h, operatorSession, other.ID)
	require.Len(t, rows, 1)
	require.Equal(t, otherEntry.ID.String(), rows[0]["registration_id"].Text)
	require.Equal(t, otherNumber, rows[0]["passport_number"].Text)
	require.Equal(t, string(secondPhoto), files["photos/"+otherEntry.ID.String()+".png"])
}

func writePassIdentityGrantField(
	t *testing.T, h *harness, session appSession, operation, request uuid.UUID, version int64, field, value string,
) registrationdetails.Status {
	t.Helper()
	input := map[string]any{"operation_id": operation, "expected_version": version, "value": value}
	if request != uuid.Nil {
		input["details_request"] = request
	}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	code, raw, _ := appCall(t, h, http.MethodPost, appDetailsPath+"/"+field, string(body), session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var status registrationdetails.Status
	require.NoError(t, json.Unmarshal(raw, &status))
	return status
}

func telegramIdentityExportRequest(
	t *testing.T, h *harness, owner, other appSession, event events.Event, telegramID, updateID int64,
) uuid.UUID {
	t.Helper()
	taskID := passQAMessage(t, h, updateID, telegramID, "/passes "+event.Document.Slug)
	replies := passQAReplies(t, h, taskID, telegramID)
	var links []string
	for _, reply := range replies {
		keyboard, ok := reply.ReplyMarkup.(*telegram.InlineKeyboardMarkup)
		if !ok {
			continue
		}
		for _, row := range keyboard.InlineKeyboard {
			for _, button := range row {
				if button.WebApp != nil && strings.Contains(button.WebApp.URL, "details_request=") {
					links = append(links, button.WebApp.URL)
				}
			}
		}
	}
	require.Len(t, links, 1, "the existing participant should get a Telegram details form for %s", event.Document.Slug)
	formURL, err := url.Parse(links[0])
	require.NoError(t, err)
	requestID, err := uuid.Parse(formURL.Query().Get("details_request"))
	require.NoError(t, err)
	formURL.RawQuery = ""
	require.Equal(t, appTestOrigin+"/app/", formURL.String())

	path := appDetailsPath + "/requests/" + requestID.String()
	status, body, headers := appCall(t, h, http.MethodGet, path, "", owner.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, status, string(body))
	require.Contains(t, headers.Get("Cache-Control"), "no-store")
	var form db.DetailsRequestStatus
	require.NoError(t, json.Unmarshal(body, &form))
	require.Equal(t, requestID, form.Request.ID)
	require.Equal(t, event.ID, form.Request.EventID)
	require.Equal(t, event.Version, form.Request.EventVersion)
	require.True(t, form.Request.Requirements.PassportNumber)
	require.True(t, form.Request.Requirements.PassportPhoto)
	status, body, _ = appCall(t, h, http.MethodGet, path, "", other.Token, appTestOrigin)
	require.Equal(t, http.StatusUnauthorized, status, string(body))
	return requestID
}
