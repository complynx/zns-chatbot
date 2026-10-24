package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestStructuredRegistrationDetailsFormRestartAndExplicitContinuation(t *testing.T) {
	t.Parallel()
	h, p, session := detailsApp(t)
	_, admin := registrationUser(t, h, 95701, true)
	event := requireEventDetails(t, h, registrationEvent(t, h, admin))
	p.mu.Lock()
	p.adminRole = false
	p.loomDown = true
	p.mu.Unlock()
	scope := ownerProfileScope(t, h, session.UserID)
	prompt := h.submitText(t, 95710, "/register "+event.ID.String())
	h.await(t, prompt, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	role := p.sent[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup).InlineKeyboard[0][0].CallbackData
	p.mu.Unlock()
	click := registrationButton(t, h, 95711, 1, 456, role)
	h.await(t, click, "completed")
	h.awaitReplies(t, 2)
	p.mu.Lock()
	form := p.sent[1].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	text := p.sent[1].Text
	p.mu.Unlock()
	require.Equal(t, appTestOrigin+"/app/", form.InlineKeyboard[0][0].WebApp.URL)
	require.Empty(t, form.InlineKeyboard[0][0].CallbackData)
	require.Contains(t, text, "legal full name")
	require.Contains(t, text, "passport number")
	require.Contains(t, text, "Europe/Minsk")
	resume := form.InlineKeyboard[1][0].CallbackData
	request, err := h.store.DetailsRequestOperation(t.Context(), scope, click)
	require.NoError(t, err)
	require.Equal(t, "pending", request.State)
	require.Equal(t, event.Document.Registration.ClosesAt, request.Request.ExpiresAt)
	_, err = h.store.Registration(t.Context(), db.EventScope{BotID: 123, Organization: "org"}, event.ID, session.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	// A second click preserves the same expectation and original deadline.
	again := registrationButton(t, h, 95712, 1, 456, role)
	h.await(t, again, "completed")
	h.awaitReplies(t, 3)
	replay, err := h.store.DetailsRequestOperation(t.Context(), scope, again)
	require.NoError(t, err)
	require.Equal(t, request.Request, replay.Request)
	h.stop(t)
	h.start(t)
	for index, field := range []string{"legal-name", "passport-number"} {
		body := appDetailsBody(t, uuid.New(), int64(index), "SYNTHETIC-NATIVE-FORM-DETAIL")
		code, raw, _ := appCall(t, h, http.MethodPost, appDetailsPath+"/"+field, body, session.Token, appTestOrigin)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Equal(t, int64(index+1), appDetailsStatus(t, raw).Version)
	}
	complete, err := h.store.DetailsRequest(t.Context(), scope, request.Request.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", complete.State)
	_, err = h.store.Registration(t.Context(), db.EventScope{BotID: 123, Organization: "org"}, event.ID, session.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	back := registrationButton(t, h, 95713, 2, 456, resume)
	h.await(t, back, "completed")
	h.awaitReplies(t, 4)
	p.mu.Lock()
	freshRole := p.sent[3].ReplyMarkup.(*telegram.InlineKeyboardMarkup).InlineKeyboard[0][0].CallbackData
	p.mu.Unlock()
	join := registrationButton(t, h, 95714, 4, 456, freshRole)
	h.await(t, join, "completed")
	h.awaitReplies(t, 5)
	registered, err := h.store.Registration(
		t.Context(),
		db.EventScope{BotID: 123, Organization: "org"},
		event.ID,
		session.UserID,
	)
	require.NoError(t, err)
	require.Equal(t, registrations.Waitlisted, registered.State)
	require.Equal(t, "leader", registered.Role)
	require.Nil(t, registered.Pass)
	duplicate := registrationButton(t, h, 95715, 4, 456, freshRole)
	h.await(t, duplicate, "completed")
	h.awaitReplies(t, 6)
	current, err := h.store.Registration(
		t.Context(),
		db.EventScope{BotID: 123, Organization: "org"},
		event.ID,
		session.UserID,
	)
	require.NoError(t, err)
	require.Equal(t, registered.ID, current.ID)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.runs)
	require.Empty(t, p.inputs)
	require.Zero(t, p.loomUnavailableCalls)
}

func TestStructuredRegistrationDetailsUnavailableFormStillKeepsExpectation(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.loomDown = true
	h := setup(t, p)
	_, admin := registrationUser(t, h, 95702, true)
	event := requireEventDetails(t, h, registrationEvent(t, h, admin))
	p.mu.Lock()
	p.adminRole = false
	p.mu.Unlock()
	prompt := h.submitText(t, 95810, "/register "+event.ID.String())
	h.await(t, prompt, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	role := p.sent[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup).InlineKeyboard[0][0].CallbackData
	p.mu.Unlock()
	click := registrationButton(t, h, 95811, 1, 456, role)
	h.await(t, click, "completed")
	h.awaitReplies(t, 2)
	task, err := h.store.GetTask(t.Context(), click)
	require.NoError(t, err)
	request, err := h.store.DetailsRequestOperation(t.Context(), ownerProfileScope(t, h, task.UserID), click)
	require.NoError(t, err)
	require.Equal(t, "pending", request.State)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Contains(t, p.sent[1].Text, "form is currently unavailable")
	keyboard := p.sent[1].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.Len(t, keyboard.InlineKeyboard, 1)
	require.Nil(t, keyboard.InlineKeyboard[0][0].WebApp)
	require.Empty(t, p.runs)
	require.Zero(t, p.loomUnavailableCalls)
}

func TestStructuredRegistrationDetailsInterruptedPromptCannotAutoRegister(t *testing.T) {
	t.Parallel()
	h, p, session := detailsApp(t)
	_, admin := registrationUser(t, h, 95703, true)
	event := requireEventDetails(t, h, registrationEvent(t, h, admin))
	p.mu.Lock()
	p.adminRole = false
	p.loomDown = true
	p.mu.Unlock()
	scope := ownerProfileScope(t, h, session.UserID)
	prompt := h.submitText(t, 95910, "/register "+event.ID.String())
	h.await(t, prompt, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	role := p.sent[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup).InlineKeyboard[0][0].CallbackData
	p.mu.Unlock()
	_, err := h.store.Pool().
		Exec(t.Context(), "CREATE FUNCTION reject_details_prompt() RETURNS trigger LANGUAGE plpgsql" +
" AS $$ BEGIN\n IF NEW.payload->>'text' LIKE 'Before registering,%' THEN R" +
"AISE EXCEPTION 'synthetic interrupted details prompt'; END IF; RETURN NE" +
"W; END $$;\n CREATE TRIGGER reject_details_prompt BEFORE INSERT ON shuttl" +
"e_outbox FOR EACH ROW EXECUTE FUNCTION reject_details_prompt()")
	require.NoError(t, err)
	click := registrationButton(t, h, 95911, 1, 456, role)
	require.Eventually(t, func() bool {
		var ready bool
		readErr := h.store.Pool().
			QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM shuttle_registration_details_request_operati" +
"ons WHERE operation_id=$1) AND EXISTS(SELECT 1 FROM shuttle_tasks WHERE " +
"id=$1 AND state<>'completed')", click).
			Scan(&ready)
		return readErr == nil && ready
	}, 5*time.Second, 10*time.Millisecond)
	h.stop(t)
	version := int64(0)
	for _, field := range []registrationdetails.Field{registrationdetails.LegalName, registrationdetails.PassportNumber} {
		result, writeErr := h.store.MutateRegistrationDetails(
			t.Context(),
			scope,
			registrationdetails.Mutation{
				OperationID:     uuid.New(),
				ExpectedVersion: version,
				Field:           field,
				Value:           "SYNTHETIC-INTERRUPTED-DETAIL",
			},
			p.registrationDetails,
		)
		require.NoError(t, writeErr)
		version = result.Version
	}
	_, err = h.store.Pool().
		Exec(t.Context(), "DROP TRIGGER reject_details_prompt ON shuttle_outbox; DROP FUNCTION reject_details_prompt()")
	require.NoError(t, err)
	h.start(t)
	h.await(t, click, "completed")
	h.awaitReplies(t, 2)
	_, err = h.store.Registration(t.Context(), db.EventScope{BotID: 123, Organization: "org"}, event.ID, session.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	request, err := h.store.DetailsRequestOperation(t.Context(), scope, click)
	require.NoError(t, err)
	require.Equal(t, "pending", request.State)
	current, err := h.store.DetailsRequest(t.Context(), scope, request.Request.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", current.State)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Contains(t, p.sent[1].Text, "Required details are saved")
	require.Contains(t, p.sent[1].Text, "Return to role selection")
	require.Empty(t, p.runs)
	require.Zero(t, p.loomUnavailableCalls)
}
