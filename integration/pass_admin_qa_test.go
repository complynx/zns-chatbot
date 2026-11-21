package integration_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/passcopy"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func passAdminAppCall(t *testing.T, h *harness, session appSession, method string, event uuid.UUID,
	action string, input any,
) (int, []byte, http.Header) {
	t.Helper()
	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		require.NoError(t, err)
	}
	return appCall(t, h, method, "/app/pass-admin/events/"+event.String()+"/"+action,
		string(body), session.Token, appTestOrigin)
}

func makePassAdminChange(t *testing.T, h *harness, session appSession, event uuid.UUID, action string,
	change db.PassAdminChange, confirmed bool, status int,
) []byte {
	t.Helper()
	code, raw, _ := passAdminAppCall(t, h, session, http.MethodPost, event, action,
		struct {
			db.PassAdminChange

			Confirmed bool `json:"confirmed"`
		}{change, confirmed})
	require.Equal(t, status, code, string(raw))
	return raw
}

func passAdminRowsByID(t *testing.T, h *harness, session appSession, event uuid.UUID) map[uuid.UUID]db.PassAdminRow {
	t.Helper()
	status, body, _ := passAdminAppCall(t, h, session, http.MethodGet, event, "registrations", nil)
	require.Equal(t, http.StatusOK, status, string(body))
	var view db.PassAdminList
	require.NoError(t, json.Unmarshal(body, &view))
	require.Equal(t, event, view.EventID)
	rows := make(map[uuid.UUID]db.PassAdminRow, len(view.Rows))
	for _, row := range view.Rows {
		require.NotContains(t, rows, row.RegistrationID)
		rows[row.RegistrationID] = row
	}
	return rows
}

func TestPassAdminRolePositionExcludesRevokedParticipants(t *testing.T) {
	t.Parallel()
	f := setupAssignmentApp(t)
	owner, token := registrationUser(t, f.h, 98703, false)
	second := registerForEvent(t, f.h, token, f.event.ID, "second-leader", registrations.Leader)
	_, err := f.h.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.entry.UserID)
	require.NoError(t, err)
	participant, err := f.h.store.PassQueuePosition(t.Context(), paymentScope(t, f.h, owner, false), f.event.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, participant.Role)
	rows := passAdminRowsByID(t, f.h, f.session, f.event.ID)
	require.Contains(t, rows, f.entry.ID)
	require.Zero(t, rows[f.entry.ID].RolePosition)
	require.Equal(t, participant.Role, rows[second.ID].RolePosition)
	require.False(t, rows[second.ID].PendingProof)
}

func TestPassAdminTelegramPairAssignmentReturnRefundAndWorkbook(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := setupPairAssignmentApp(t)
	p := f.H.peers
	changeProfile(t, f.H, ownerProfileScope(t, f.H, f.admin.UserID), personName(0, "Event", "Operator"))
	unrelated := registrationEvent(t, f.H, f.token)
	manual := registrationEvent(t, f.H, f.token)
	_, grantErr := f.H.store.ChangeAuthority(ctx, f.admin.EventScope, f.admin.Subject, uuid.NewString(),
		db.AuthorityChange{EventID: manual.ID, UserID: f.admin.UserID, Role: db.AuthorityAdministrator}, uuid.Nil)
	require.NoError(t, grantErr)
	for _, grant := range []struct {
		user uuid.UUID
		role string
	}{{f.admin.UserID, db.AuthorityAdministrator}, {f.Other.UserID, db.AuthorityAmbassador}} {
		_, err := f.H.store.ChangeAuthority(ctx, f.admin.EventScope, f.admin.Subject, uuid.NewString(),
			db.AuthorityChange{EventID: f.EventID, UserID: grant.user, Role: grant.role}, uuid.Nil)
		require.NoError(t, err)
	}
	p.mu.Lock()
	p.adminRole = false
	p.loomDown = true
	p.mu.Unlock()
	f.H.iam.Invalidate(f.admin.Subject)

	telegramEntry := passQAMessage(t, f.H, 97100, 12005, "/pairs")
	replies := passQAReplies(t, f.H, telegramEntry, 12005)
	require.Len(t, replies, 1)
	message := replies[0]
	markup, ok := message.ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.True(t, ok)
	require.Equal(t, appTestOrigin+"/app/#pairs", markup.InlineKeyboard[0][0].WebApp.URL)

	owner := detailsAppSession(t, f.H, 12002, 111)
	ambassador := detailsAppSession(t, f.H, 12004, 112)
	for _, denied := range []appSession{owner, ambassador} {
		for _, action := range []string{"registrations", "export.xlsx"} {
			status, raw, headers := passAdminAppCall(t, f.H, denied, http.MethodGet, f.EventID, action, nil)
			require.Equal(t, http.StatusForbidden, status, string(raw))
			require.Empty(t, headers.Get("Content-Disposition"))
		}
	}
	status, raw, _ := passAdminAppCall(t, f.H, f.session, http.MethodGet, unrelated.ID, "export.xlsx", nil)
	require.Equal(t, http.StatusForbidden, status, string(raw))

	operation := uuid.NewString()
	prep := pairAssignmentAppPrepare(t, f, operation)
	status, raw = pairAssignmentAppCall(t, f, f.session, http.MethodPost, "/confirm", map[string]any{
		"operation_id": operation, "arguments": f.args, "proposal_id": prep.ID, "confirmed": true,
	})
	require.Equal(t, http.StatusOK, status, string(raw))
	var assigned db.PairAssignmentResult
	require.NoError(t, json.Unmarshal(raw, &assigned))
	require.NotNil(t, assigned.Leader.Pass)
	require.NotNil(t, assigned.Follower.Pass)
	rows := passAdminRowsByID(t, f.H, f.session, f.EventID)
	require.Len(t, rows, 2)
	require.False(t, rows[assigned.Leader.ID].PendingProof)
	require.Equal(t, &assigned.Follower.ID, rows[assigned.Leader.ID].PartnerRegistrationID)
	require.Equal(t, &assigned.Leader.ID, rows[assigned.Follower.ID].PartnerRegistrationID)

	const paid int64 = 3087
	const refund int64 = 2087
	evidence, err := f.H.store.SubmitPaymentEvidence(ctx, f.A, "pass-admin-proof", payments.Submission{
		EventID: f.EventID, PassID: assigned.Leader.Pass.ID, AmountMinor: paid,
		FileID:   paymentFile(t, f.H, f.A.UserID, 97101, "pass-admin-proof", time.Now().UTC().Truncate(time.Second)),
		Currency: assigned.Leader.Pass.Currency, CurrencyScale: assigned.Leader.Pass.CurrencyScale, Purpose: payments.Initial,
	})
	require.NoError(t, err)
	change := db.PassAdminChange{
		OperationID: uuid.New(), RegistrationID: assigned.Leader.ID, ExpectedVersion: assigned.Leader.Version,
		Reason: "Participant requested a return",
	}
	makePassAdminChange(t, f.H, f.session, f.EventID, "return", change, true, http.StatusConflict)
	require.True(t, passAdminRowsByID(t, f.H, f.session, f.EventID)[assigned.Leader.ID].PendingProof)
	review := payments.ReviewChange{
		EventID: f.EventID, EvidenceID: evidence.ID, Decision: payments.Confirmed, AmountMinor: paid,
	}
	approval, err := f.H.store.PreparePaymentReview(ctx, f.admin, "pass-admin-proof-review", review)
	require.NoError(t, err)
	_, err = f.H.store.ReviewPaymentEvidence(ctx, f.admin, "pass-admin-proof-review", review, approval.ID)
	require.NoError(t, err)
	beforeReturn, err := f.H.store.PaymentEvidence(ctx, f.A, f.EventID, evidence.ID)
	require.NoError(t, err)
	require.Equal(t, payments.Confirmed, beforeReturn.State)
	makePassAdminChange(t, f.H, f.session, f.EventID, "return", change, true, http.StatusConflict)
	change.ConfirmedMinor, change.RefundMinor = paid, refund
	makePassAdminChange(t, f.H, owner, f.EventID, "return", change, true, http.StatusForbidden)
	makePassAdminChange(t, f.H, ambassador, f.EventID, "return", change, true, http.StatusForbidden)
	makePassAdminChange(t, f.H, f.session, f.EventID, "return", change, false, http.StatusBadRequest)
	first := makePassAdminChange(t, f.H, f.session, f.EventID, "return", change, true, http.StatusOK)
	replay := makePassAdminChange(t, f.H, f.session, f.EventID, "return", change, true, http.StatusOK)
	require.JSONEq(t, string(first), string(replay))

	rows = passAdminRowsByID(t, f.H, f.session, f.EventID)
	returned, partner := rows[assigned.Leader.ID], rows[assigned.Follower.ID]
	require.Equal(t, "returned", returned.State)
	require.Equal(t, &assigned.Leader.Pass.ID, returned.PassID)
	require.Equal(t, paid, returned.ConfirmedMinor)
	require.Equal(t, refund, returned.RefundDueMinor)
	require.Zero(t, returned.RefundedMinor)
	require.NotNil(t, returned.ReturnedAt)
	require.Equal(t, &f.admin.UserID, returned.ReturnedBy)
	require.Nil(t, returned.PartnerRegistrationID)
	require.Nil(t, partner.PartnerRegistrationID)
	require.Equal(t, registrations.Assigned, partner.State)
	currentPartner, err := f.H.store.Registration(ctx, f.B.EventScope, f.EventID, f.B.UserID)
	require.NoError(t, err)
	require.Equal(t, assigned.Follower.Pass, currentPartner.Pass)
	require.Equal(t, assigned.Follower.RequestedAt, currentPartner.RequestedAt)
	for _, member := range []struct {
		scope   db.PaymentScope
		allowed bool
	}{{f.A, false}, {f.B, true}} {
		allowed, accessErr := f.H.store.ModuleAvailable(
			ctx, member.scope.EventScope, f.EventID, member.scope.UserID, "orders",
		)
		require.NoError(t, accessErr)
		require.Equal(t, member.allowed, allowed)
	}
	current, err := f.H.store.PaymentEvidence(ctx, f.A, f.EventID, evidence.ID)
	require.NoError(t, err)
	require.Equal(t, beforeReturn, current)

	change.OperationID, change.ExpectedVersion = uuid.New(), returned.Version
	makePassAdminChange(t, f.H, f.session, f.EventID, "return", change, true, http.StatusConflict)
	_, err = f.H.store.Pool().Exec(ctx,
		"UPDATE shuttle_events SET modules=array_remove(modules,'registrations') WHERE id=$1", f.EventID)
	require.NoError(t, err)
	status, raw = pairAssignmentAppCall(t, f, f.session, http.MethodGet, "/events", nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var picker db.MiniAppAssignmentEvents
	require.NoError(t, json.Unmarshal(raw, &picker))
	require.Len(t, picker.Events, 2)
	for _, event := range picker.Events {
		switch event.ID {
		case f.EventID:
			require.NotContains(t, event.Document.Modules, "registrations")
		case manual.ID:
			require.Contains(t, event.Document.Modules, "registrations")
		default:
			t.Fatalf("unexpected event %s", event.ID)
		}
	}
	scopedAdmin := f.admin
	scopedAdmin.GlobalAdmin = false
	agentEvents, err := f.H.store.PairAssignmentEvents(ctx, scopedAdmin, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, agentEvents, 1)
	require.Equal(t, manual.ID, agentEvents[0].ID)
	status, raw = pairAssignmentAppCall(t, f, f.session, http.MethodGet, "/events/"+f.EventID.String()+"/candidates", nil)
	require.Equal(t, http.StatusConflict, status, string(raw))
	status, raw = pairAssignmentAppCall(t, f, ambassador, http.MethodGet, "/events", nil)
	require.Equal(t, http.StatusForbidden, status, string(raw))
	change.Reason = "External transfer completed"
	change.RefundMinor--
	makePassAdminChange(t, f.H, f.session, f.EventID, "refund", change, true, http.StatusConflict)
	change.RefundMinor++
	first = makePassAdminChange(t, f.H, f.session, f.EventID, "refund", change, true, http.StatusOK)
	replay = makePassAdminChange(t, f.H, f.session, f.EventID, "refund", change, true, http.StatusOK)
	require.JSONEq(t, string(first), string(replay))
	change.OperationID = uuid.New()
	makePassAdminChange(t, f.H, f.session, f.EventID, "refund", change, true, http.StatusConflict)
	rows = passAdminRowsByID(t, f.H, f.session, f.EventID)
	require.Equal(t, refund, rows[assigned.Leader.ID].RefundedMinor)
	require.NotNil(t, rows[assigned.Leader.ID].RefundedAt)
	require.Equal(t, &f.admin.UserID, rows[assigned.Leader.ID].RefundedBy)
	require.Equal(t, partner, rows[assigned.Follower.ID])

	_, err = f.H.store.Pool().Exec(ctx,
		"UPDATE shuttle_events SET modules=array_append(modules,'registrations') WHERE id=$1", f.EventID)
	require.NoError(t, err)
	status, raw, _ = appCall(t, f.H, http.MethodGet, "/app/account", "", owner.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, status, string(raw))
	var account db.AccountEvents
	require.NoError(t, json.Unmarshal(raw, &account))
	require.Len(t, account.Events, 1)
	entry := account.Events[0]
	require.Equal(t, f.EventID, entry.EventID)
	require.Equal(t, registrations.Returned, entry.Registration.State)
	require.Nil(t, entry.Pass)
	require.NotNil(t, entry.PassReturn)
	require.Equal(t, assigned.Leader.Pass.PriceMinor, entry.PassReturn.FrozenPriceMinor)
	require.Equal(t, assigned.Leader.Pass.Currency, entry.PassReturn.Currency)
	require.Equal(t, paid, entry.PassReturn.ConfirmedMinor)
	require.Equal(t, refund, entry.PassReturn.RefundDueMinor)
	require.Equal(t, refund, entry.PassReturn.RefundedMinor)
	require.NotNil(t, entry.PassReturn.RefundedAt)
	require.NotContains(t, string(raw), assigned.Follower.ID.String())

	status, raw, headers := passAdminAppCall(t, f.H, f.session, http.MethodGet, f.EventID, "export.xlsx", nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	require.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", headers.Get("Content-Type"))
	require.Contains(t, headers.Get("Content-Disposition"), ".xlsx")
	workbook := readPassAdminWorkbook(t, raw)
	require.Contains(t, workbook, "[Content_Types].xml")
	require.Contains(t, workbook, "xl/workbook.xml")
	require.Contains(t, workbook, "xl/worksheets/sheet1.xml")
	require.Contains(t, workbook, "xl/worksheets/sheet2.xml")
	sheet := workbook["xl/worksheets/sheet1.xml"]
	require.NotContains(t, sheet, evidence.FileID.String())
	require.NotContains(t, sheet, unrelated.ID.String())
	worksheet := passAdminWorksheet(t, sheet)
	require.Len(t, worksheet, 5)
	require.Equal(t, "event_id", worksheet[0].Cells[0].Text)
	require.Equal(t, f.EventID.String(), worksheet[0].Cells[1].Text)
	require.Equal(t, "exported_at", worksheet[1].Cells[0].Text)
	_, err = time.Parse(time.RFC3339Nano, worksheet[1].Cells[1].Text)
	require.NoError(t, err)
	exported := make(map[string]map[string]passAdminWorkbookCell)
	for _, byColumn := range passAdminWorksheetData(t, worksheet[2:]) {
		exported[byColumn["registration_id"].Text] = byColumn
	}
	leaderCells := exported[assigned.Leader.ID.String()]
	followerCells := exported[assigned.Follower.ID.String()]
	require.Equal(t, "Alex Leader", leaderCells["display_name"].Text)
	require.Equal(t, "Sam Follower", followerCells["display_name"].Text)
	require.Equal(t, "returned", leaderCells["state"].Text)
	require.Equal(t, "assigned", followerCells["state"].Text)
	require.Equal(t, "false", followerCells["pending_proof"].Text)
	require.Equal(t, "12002", leaderCells["telegram_id"].Text)
	require.Equal(t, "inlineStr", leaderCells["telegram_id"].Type)
	require.Equal(t, "BYN", leaderCells["currency"].Text)
	require.Equal(t, assigned.Follower.Pass.ID.String(), followerCells["pass_id"].Text)
	for header, value := range map[string]string{
		"price_minor": "12345", "currency_scale": "2", "confirmed_minor": "3087",
		"refund_due_minor": "2087", "refunded_minor": "2087",
	} {
		require.Equal(t, value, leaderCells[header].Number, header)
	}
	require.Equal(t, f.admin.UserID.String(), leaderCells["returned_by"].Text)
	require.Equal(t, f.admin.UserID.String(), leaderCells["refunded_by"].Text)

	paymentSheet := workbook["xl/worksheets/sheet2.xml"]
	require.NotContains(t, paymentSheet, evidence.FileID.String())
	paymentRows := passAdminWorksheetData(t, passAdminWorksheet(t, paymentSheet))
	require.Len(t, paymentRows, 1)
	cells := paymentRows[0]
	require.Equal(t, evidence.ID.String(), cells["evidence_id"].Text)
	require.Equal(t, assigned.Leader.ID.String(), cells["registration_id"].Text)
	require.Equal(t, payments.Confirmed, cells["decision"].Text)
	require.Equal(t, "3087", cells["reported_minor"].Number)
	require.Equal(t, "3087", cells["confirmed_minor"].Number)
	require.Equal(t, f.admin.UserID.String(), cells["reviewer_id"].Text)
	require.Equal(t, "Event Operator", cells["reviewer_name"].Text)
	require.Empty(t, cells["actual_receiver"].Text)

	authority, err := f.H.store.Authority(ctx, f.admin.EventScope, f.EventID, f.admin.UserID)
	require.NoError(t, err)
	_, err = f.H.store.ChangeAuthority(ctx, f.admin.EventScope, f.admin.Subject, uuid.NewString(),
		db.AuthorityChange{EventID: f.EventID, UserID: f.admin.UserID, Role: db.AuthorityAmbassador,
			ExpectedVersion: authority.Version}, uuid.Nil)
	require.NoError(t, err)
	status, raw, headers = passAdminAppCall(t, f.H, f.session, http.MethodGet, f.EventID, "export.xlsx", nil)
	require.Equal(t, http.StatusForbidden, status, string(raw))
	require.Empty(t, headers.Get("Content-Disposition"))
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.runs)
	require.Zero(t, p.loomUnavailableCalls)
}

func TestPassAdminManualAssignmentOverridesToleranceButNotCapacity(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := setupAssignmentApp(t)
	admin := paymentScope(t, f.h, f.adminID, true)
	protector := detailsProtector(t, 97)
	saveLegalName := func(scope db.ProfileScope) {
		_, err := f.h.store.MutateRegistrationDetails(ctx, scope,
			registrationdetails.Mutation{OperationID: uuid.New(), Field: registrationdetails.LegalName,
				Value: "Synthetic Participant"}, protector)
		require.NoError(t, err)
	}
	saveLegalName(ownerProfileScope(t, f.h, f.entry.UserID))
	f.h.peers.mu.Lock()
	f.h.peers.loomDown = true
	f.h.peers.mu.Unlock()
	f.event.Document.Registration.AutoAssign = true
	f.event.Document.Registration.FollowerCapacity = 2
	event, err := f.h.store.ChangeEvent(ctx, admin.EventScope, admin.Subject, "enable-pass-tolerance", db.EventChange{
		ID: f.event.ID, ExpectedVersion: f.event.Version, Document: f.event.Document,
	})
	require.NoError(t, err)
	f.event = event
	_, err = f.h.store.AllocatePassQueue(ctx, admin.EventScope, event.ID)
	require.NoError(t, err)
	initial := passQARegistration(t, f.h, paymentScope(t, f.h, f.entry.UserID, false), event.ID)
	require.Nil(t, initial.Pass, "automatic allocation applies the default role limit to a lone leader")
	f.entry = initial

	operation := uuid.NewString()
	preparation := assignmentAppProposal(t, f, operation)
	input := assignmentAppInput(f, operation, preparation.ID, true)
	status, raw := assignmentAppCall(t, f, f.session, http.MethodPost, "/confirm", input)
	require.Equal(t, http.StatusOK, status, string(raw))
	assigned := passQARegistration(t, f.h, paymentScope(t, f.h, f.entry.UserID, false), event.ID)
	require.NotNil(t, assigned.Pass)
	recipients := map[uuid.UUID]int64{assigned.Pass.ID: 98702}
	requireAssignmentNotices(t, f.h, event.Document.Name, recipients)
	status, replay := assignmentAppCall(t, f, f.session, http.MethodPost, "/confirm", input)
	require.Equal(t, http.StatusOK, status, string(replay))
	require.JSONEq(t, string(raw), string(replay))

	second := passQANamedUser(t, f.h, 98703)
	saveLegalName(ownerProfileScope(t, f.h, second.UserID))
	message := passQAMessage(t, f.h, 97110, 98703, "/passes "+event.ID.String()+" --role leader --solo")
	passQAReplies(t, f.h, message, 98703)
	f.entry = passQARegistration(t, f.h, second, event.ID)
	require.Nil(t, f.entry.Pass)
	f.h.peers.mu.Lock()
	f.h.peers.adminRole = true
	f.h.peers.mu.Unlock()
	f.h.iam.Invalidate(admin.Subject)
	operation = uuid.NewString()
	status, raw = assignmentAppCall(t, f, f.session, http.MethodPost, "/prepare",
		assignmentAppInput(f, operation, uuid.Nil, false))
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Equal(t, f.entry, passQARegistration(t, f.h, second, event.ID))

	firstFollowerID, firstFollowerToken := registrationUser(t, f.h, 98704, false)
	secondFollowerID, secondFollowerToken := registrationUser(t, f.h, 98705, false)
	saveLegalName(ownerProfileScope(t, f.h, firstFollowerID))
	saveLegalName(ownerProfileScope(t, f.h, secondFollowerID))
	registerForEvent(t, f.h, firstFollowerToken, event.ID, "pass-first-follower", registrations.Follower)
	registerForEvent(t, f.h, secondFollowerToken, event.ID, "pass-second-follower", registrations.Follower)
	_, err = f.h.store.AllocatePassQueue(ctx, admin.EventScope, event.ID)
	require.NoError(t, err)
	firstFollower := paymentScope(t, f.h, firstFollowerID, false)
	secondFollower := paymentScope(t, f.h, secondFollowerID, false)
	firstPass := passQARegistration(t, f.h, firstFollower, event.ID).Pass
	require.NotNil(t, firstPass)
	require.Nil(t, passQARegistration(t, f.h, secondFollower, event.ID).Pass)

	event.Document.Registration.MaxFollowerPercent = 67
	event, err = f.h.store.ChangeEvent(ctx, admin.EventScope, admin.Subject, "raise-follower-tolerance", db.EventChange{
		ID: event.ID, ExpectedVersion: event.Version, Document: event.Document,
	})
	require.NoError(t, err)
	_, err = f.h.store.AllocatePassQueue(ctx, admin.EventScope, event.ID)
	require.NoError(t, err)
	secondPass := passQARegistration(t, f.h, secondFollower, event.ID).Pass
	require.NotNil(t, secondPass)
	require.Nil(t, passQARegistration(t, f.h, second, event.ID).Pass)
	recipients[firstPass.ID] = 98704
	recipients[secondPass.ID] = 98705
	requireAssignmentNotices(t, f.h, event.Document.Name, recipients)
}

func requireAssignmentNotices(t *testing.T, h *harness, event string, recipients map[uuid.UUID]int64) {
	t.Helper()
	for passID, expectedChat := range recipients {
		require.EventuallyWithT(t, func(check *assert.CollectT) {
			var state string
			var chatID, messageID int64
			err := h.store.Pool().QueryRow(t.Context(), `
SELECT state,COALESCE((receipt->>'chat_id')::bigint,0),COALESCE((receipt->>'message_id')::bigint,0)
FROM shuttle_pass_assignment_notices WHERE pass_id=$1`, passID).Scan(&state, &chatID, &messageID)
			require.NoError(check, err)
			require.Equal(check, "delivered", state)
			require.Equal(check, expectedChat, chatID)
			require.Positive(check, messageID)
			h.peers.mu.Lock()
			defer h.peers.mu.Unlock()
			require.LessOrEqual(check, messageID, int64(len(h.peers.sent)))
			message := h.peers.sent[messageID-1]
			require.Equal(check, expectedChat, message.ChatID)
			require.Contains(check, message.Text, event)
			require.Contains(check, message.Text, "123.45 BYN")
		}, 10*time.Second, 25*time.Millisecond)
	}
	var count int
	require.NoError(
		t,
		h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_pass_assignment_notices").Scan(&count),
	)
	require.Equal(t, len(recipients), count)
	actual := make(map[int64]int)
	h.peers.mu.Lock()
	defer h.peers.mu.Unlock()
	for _, message := range h.peers.sent {
		if strings.Contains(message.Text, event) && strings.Contains(message.Text, "123.45 BYN") {
			actual[message.ChatID]++
		}
	}
	require.Len(t, actual, len(recipients))
	for _, chatID := range recipients {
		require.Equal(t, 1, actual[chatID])
	}
}

func TestPassAdminTelegramWaitlistedReplacementCanBeCanceledAndReissued(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, admin, args := acceptedPairForAssignment(t)
	h := f.H
	h.peers.mu.Lock()
	h.peers.loomDown = true
	h.peers.mu.Unlock()
	leader := passQARegistration(t, h, f.A, f.EventID)
	follower := passQARegistration(t, h, f.B, f.EventID)
	for _, member := range []db.PaymentScope{f.A, f.B, f.Other} {
		changeProfile(t, h, ownerProfileScope(t, h, member.UserID), personName(0, "Pair", "Participant"))
	}
	const leaderTG, replacementTG = int64(12002), int64(12004)
	command := "/passes " + f.EventID.String()
	menu := passQAReplies(t, h, passQAMessage(t, h, 97120, leaderTG, command), leaderTG)
	click := passQAClick(t, h, 97121, leaderTG, menu, passcopy.Text("en", "entry.replace_pair"))
	passQAReplies(t, h, click, leaderTG)
	first, err := h.store.ActivePassInvite(ctx, f.A, f.EventID)
	require.NoError(t, err)
	require.Nil(t, first.PairID)
	var oldState string
	require.NoError(
		t,
		h.store.Pool().QueryRow(ctx, "SELECT state FROM shuttle_pairs WHERE id=$1", args.PairID).Scan(&oldState),
	)
	require.Equal(t, registrations.Waitlisted, oldState)
	require.Equal(t, follower, passQARegistration(t, h, f.B, f.EventID))
	currentArgs := args
	currentArgs.ExpectedLeaderVersion = passQARegistration(t, h, f.A, f.EventID).Version
	_, err = h.store.PreparePairAssignment(ctx, admin, "assign-while-replacing", currentArgs)
	require.ErrorIs(t, err, db.ErrConflict)

	menu = passQAReplies(t, h, passQAMessage(t, h, 97122, leaderTG, command), leaderTG)
	click = passQAClick(t, h, 97123, leaderTG, menu, passcopy.Text("en", "entry.revoke_invite"))
	passQAReplies(t, h, click, leaderTG)
	_, err = h.store.ActivePassInvite(ctx, f.A, f.EventID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	stale := passQAMessage(t, h, 97124, replacementTG, "/passes "+first.Code)
	passQAReplies(t, h, stale, replacementTG)
	_, err = h.store.Registration(ctx, f.Other.EventScope, f.EventID, f.Other.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.NoError(
		t,
		h.store.Pool().QueryRow(ctx, "SELECT state FROM shuttle_pairs WHERE id=$1", args.PairID).Scan(&oldState),
	)
	require.Equal(t, registrations.Waitlisted, oldState)
	currentArgs.ExpectedLeaderVersion = passQARegistration(t, h, f.A, f.EventID).Version
	_, err = h.store.PreparePairAssignment(ctx, admin, "assign-after-canceling-replacement", currentArgs)
	require.NoError(t, err)

	menu = passQAReplies(t, h, passQAMessage(t, h, 97125, leaderTG, command), leaderTG)
	click = passQAClick(t, h, 97126, leaderTG, menu, passcopy.Text("en", "entry.replace_pair"))
	passQAReplies(t, h, click, leaderTG)
	second, err := h.store.ActivePassInvite(ctx, f.A, f.EventID)
	require.NoError(t, err)
	require.NotEqual(t, first.Code, second.Code)
	claim := passQAMessage(t, h, 97127, replacementTG, "/passes "+second.Code)
	passQAReplies(t, h, claim, replacementTG)
	require.NoError(
		t,
		h.store.Pool().QueryRow(ctx, "SELECT state FROM shuttle_pairs WHERE id=$1", args.PairID).Scan(&oldState),
	)
	require.Equal(t, "dissolved", oldState)

	successor := passQARegistration(t, h, f.Other, f.EventID)
	require.Equal(t, registrations.Follower, successor.Role)
	require.Equal(t, registrations.Waitlisted, successor.State)
	require.Nil(t, successor.Pass)
	currentLeader := passQARegistration(t, h, f.A, f.EventID)
	currentFollower := passQARegistration(t, h, f.B, f.EventID)
	require.Equal(t, leader.RequestedAt, currentLeader.RequestedAt)
	require.Equal(t, follower.RequestedAt, currentFollower.RequestedAt)
	require.Equal(t, registrations.Waitlisted, currentFollower.State)
	require.Nil(t, currentFollower.Pass)
	var newPair, newLeaderRegistration, newFollowerRegistration uuid.UUID
	require.NoError(t, h.store.Pool().QueryRow(ctx, `
SELECT p.id,p.leader_registration_id,p.follower_registration_id
FROM shuttle_pass_invites i JOIN shuttle_pairs p ON p.id=i.pair_id
WHERE i.code=$1 AND p.state='waitlisted'`, second.Code).
		Scan(&newPair, &newLeaderRegistration, &newFollowerRegistration))
	require.NotEqual(t, args.PairID, newPair)
	require.Equal(t, leader.ID, newLeaderRegistration)
	require.Equal(t, successor.ID, newFollowerRegistration)
	_, err = h.store.PreparePairAssignment(ctx, admin, "assign-obsolete-partner", args)
	require.ErrorIs(t, err, db.ErrConflict)
}

type passAdminWorkbookCell struct {
	Ref     string  `xml:"r,attr"`
	Type    string  `xml:"t,attr"`
	Text    string  `xml:"is>t"`
	Number  string  `xml:"v"`
	Formula *string `xml:"f"`
}

type passAdminWorkbookRow struct {
	Number string                  `xml:"r,attr"`
	Cells  []passAdminWorkbookCell `xml:"c"`
}

func passAdminWorksheet(t *testing.T, sheet string) []passAdminWorkbookRow {
	t.Helper()
	var worksheet struct {
		Rows []passAdminWorkbookRow `xml:"sheetData>row"`
	}
	require.NoError(t, xml.Unmarshal([]byte(sheet), &worksheet))
	return worksheet.Rows
}

// Cell references preserve column names even when a blank cell is omitted.
func passAdminWorksheetData(t *testing.T, rows []passAdminWorkbookRow) []map[string]passAdminWorkbookCell {
	t.Helper()
	require.NotEmpty(t, rows)
	columns := make(map[string]string)
	for _, cell := range rows[0].Cells {
		columns[strings.TrimSuffix(cell.Ref, rows[0].Number)] = cell.Text
	}
	data := make([]map[string]passAdminWorkbookCell, 0, len(rows)-1)
	for _, row := range rows[1:] {
		record := make(map[string]passAdminWorkbookCell)
		for _, cell := range row.Cells {
			require.Nil(t, cell.Formula, cell.Ref)
			record[columns[strings.TrimSuffix(cell.Ref, row.Number)]] = cell
		}
		data = append(data, record)
	}
	return data
}

// Open the downloaded ZIP and validate its XML parts before checking cells.
func readPassAdminWorkbook(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	book, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	parts := make(map[string]string, len(book.File))
	for _, part := range book.File {
		in, openErr := part.Open()
		require.NoError(t, openErr)
		content, readErr := io.ReadAll(in)
		require.NoError(t, in.Close())
		require.NoError(t, readErr)
		if strings.HasSuffix(part.Name, ".xml") || strings.HasSuffix(part.Name, ".rels") {
			var root any
			require.NoError(t, xml.Unmarshal(content, &root), part.Name)
		}
		parts[part.Name] = string(content)
	}
	return parts
}
