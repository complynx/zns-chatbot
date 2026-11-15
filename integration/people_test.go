package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

func peopleRequest(t *testing.T, h *harness, token, method string, args any) (int, []byte) {
	t.Helper()
	return adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		"/internal/agent/people/"+method,
		map[string]any{"protocol_version": 1, "operation_id": uuid.NewString(), "arguments": args},
	)
}

func TestExactPersonResolveIsPublicBasicIdentity(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, caller := registrationUser(t, h, 29301, false)
	target, _ := registrationUser(t, h, 29302, false)
	setPeopleName(t, h, target, "Люба", "Зеленская")
	_, err := h.store.Pool().Exec(t.Context(), `UPDATE shuttle_users
 SET profile='{"first_name":"Telegram name","username":"liubka_z"}' WHERE id=$1`, target)
	require.NoError(t, err)
	for _, state := range []struct{ visible, revoked bool }{{true, false}, {false, false}, {false, true}} {
		personVisible(t, h, target, state.visible)
		require.NoError(t, h.store.RevokeUser(t.Context(), target, state.revoked))
		for _, selector := range []any{
			map[string]any{"user_id": target}, map[string]any{"telegram_user_id": 29302},
			map[string]any{"username": "@LIUBKA_Z"},
		} {
			code, raw := peopleRequest(t, h, caller, "resolve", selector)
			require.Equal(t, http.StatusOK, code, string(raw))
			var response struct {
				Result db.PersonIdentity `json:"result"`
			}
			require.NoError(t, json.Unmarshal(raw, &response))
			require.Equal(t, target, response.Result.UserID)
			require.Equal(t, "Люба Зеленская", response.Result.PreferredName)
			require.Equal(t, "https://t.me/liubka_z", response.Result.ProfileURL)
			for _, private := range []string{"administration", "gender", "pronouns", "zitadel_subject", "revoked"} {
				require.NotContains(t, string(raw), `"`+private+`"`)
			}
		}
	}
	for _, selector := range []any{map[string]any{}, map[string]any{"username": "@"},
		map[string]any{"user_id": target, "username": "liubka_z"}} {
		code, raw := peopleRequest(t, h, caller, "resolve", selector)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	code, raw := peopleRequest(t, h, caller, "resolve", map[string]any{"username": "liubka"})
	require.Equal(t, http.StatusNotFound, code, string(raw))
	duplicate, _ := registrationUser(t, h, 29303, false)
	_, err = h.store.Pool().Exec(t.Context(),
		`UPDATE shuttle_users SET profile='{"username":"LIUBKA_Z"}' WHERE id=$1`, duplicate)
	require.NoError(t, err)
	code, raw = peopleRequest(t, h, caller, "resolve", map[string]any{"username": "liubka_z"})
	require.Equal(t, http.StatusConflict, code, string(raw))
	for _, fallback := range []struct{ profile, name, link string }{
		{`{"username":"only_username"}`, "@only_username", "https://t.me/only_username"},
		{`{}`, "Telegram 29303", "tg://user?id=29303"},
	} {
		_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_users SET profile=$2 WHERE id=$1`,
			duplicate, json.RawMessage(fallback.profile))
		require.NoError(t, err)
		code, raw = peopleRequest(t, h, caller, "resolve", map[string]any{"telegram_user_id": 29303})
		require.Equal(t, http.StatusOK, code, string(raw))
		var response struct {
			Result db.PersonIdentity `json:"result"`
		}
		require.NoError(t, json.Unmarshal(raw, &response))
		require.Equal(t, fallback.name, response.Result.PreferredName)
		require.Equal(t, fallback.link, response.Result.ProfileURL)
	}
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_users SET zitadel_org='foreign' WHERE id=$1`, target)
	require.NoError(t, err)
	code, raw = peopleRequest(t, h, caller, "resolve", map[string]any{"user_id": target})
	require.Equal(t, http.StatusNotFound, code, string(raw))
}

func TestPeopleNameVariantsPassIndexedCandidateSelection(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	owner, _ := registrationUser(t, h, 29201, false)
	for _, pair := range [][2]string{
		{"Иванов Иван", "Иван Петрович Иванов"},
		{"Петрова Анна Сергеевна", "Анна Петрова"},
		{"Иванов И.И.", "Иван Иванович Иванов"}, {"Анна И.", "Анна Иванова"},
		{"Anna Ivanova", "Анна Иванова"},
		{"Alexander", "Александр"}, {"Alexey", "Алексей"}, {"Maxim", "Максим"},
		{"Dmitry", "Дмитрий"}, {"Evgeny", "Евгений"}, {"Andrey", "Андрей"}, {"May", "Май"},
		{"Jose Munoz", "José Muñoz"}, {"łukasz", "ŁUKASZ"}, {"王明", "王明"}, {"Елкина", "Ёлкина"},
		{"Anan", "Anna"}, {"acbdfeg", "abcdefg"},
	} {
		first, last, found := strings.Cut(pair[1], " ")
		if !found {
			last = "Searchtest"
		}
		setPeopleName(t, h, owner, first, last)
		result, err := h.store.SearchPeople(t.Context(), paymentScope(t, h, owner, false), db.PeopleQuery{Query: pair[0]})
		require.NoError(t, err, pair)
		require.Len(t, result.Matches, 1, pair)
		require.Equal(t, owner, result.Matches[0].UserID, pair)
	}
}

func TestPeopleSearchReportsAliasCandidateLimit(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	owner, _ := registrationUser(t, h, 29202, true)
	_, err := h.store.Pool().Exec(t.Context(), `INSERT INTO shuttle_user_names(user_id,name,source)
 SELECT $1,'Anna History'||n,'profile' FROM generate_series(1,1001) n`, owner)
	require.NoError(t, err)
	current, _ := registrationUser(t, h, 29203, false)
	setPeopleName(t, h, current, "Anna", "Current")
	result, err := h.store.SearchPeople(t.Context(), paymentScope(t, h, owner, true), db.PeopleQuery{Query: "Anna"})
	require.NoError(t, err)
	require.True(t, result.CandidatesTruncated)
	require.True(t, result.HasMore)
	require.Len(t, result.Matches, 2, "Historical aliases cannot crowd out current matching names")
	require.Contains(t, []uuid.UUID{result.Matches[0].UserID, result.Matches[1].UserID}, current)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = h.store.SearchPeople(cancelled, paymentScope(t, h, owner, true), db.PeopleQuery{Query: "Anna"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestPeoplePublicSearchUsesLatinAliasWithoutDisclosingItsSpelling(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	owner, _ := registrationUser(t, h, 29204, false)
	first, last := "Anne-Marie", "Ivanoff"
	_, err := h.store.MutateProfile(t.Context(), ownerProfileScope(t, h, owner), db.ProfileMutation{
		OperationID: uuid.New(), Kind: "set_name", FirstName: "Анна", LastName: "Иванова",
		FirstNameLatin: &first, LastNameLatin: &last,
	})
	require.NoError(t, err)
	result, err := h.store.SearchPeople(t.Context(), paymentScope(t, h, owner, false),
		db.PeopleQuery{Query: first + " " + last})
	require.NoError(t, err)
	require.Len(t, result.Matches, 1)
	require.Equal(t, "Анна Иванова", result.Matches[0].MatchedName)
	require.Equal(t, "current", result.Matches[0].NameSource)
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(raw), first)
	require.NotContains(t, string(raw), last)
}

func TestPeopleActivityOrderAndRevokedPublicSearch(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	admin, _ := registrationUser(t, h, 29210, true)
	recent, _ := registrationUser(t, h, 29211, false)
	older, _ := registrationUser(t, h, 29212, false)
	setPeopleName(t, h, recent, "Анна", "Recent")
	setPeopleName(t, h, older, "Анна", "Older")
	for index, telegramID := range []int64{29211, 29212} {
		accepted, _, err := h.store.Intake(t.Context(), db.IntakeRequest{
			BotID: 123, UpdateID: telegramID, TelegramUserID: telegramID, ChatID: telegramID,
			Payload: json.RawMessage(`{"kind":"message","text":"activity"}`),
		}, nil)
		require.NoError(t, err)
		_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET created_at=$2 WHERE id=$1`,
			accepted.ID, time.Date(2001-index, time.January, 1, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
	}
	page, err := h.store.ListPeople(t.Context(), paymentScope(t, h, admin, true),
		db.PeopleListQuery{Sort: "recent_activity", Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Users, 2)
	require.Equal(t, recent, page.Users[0].UserID)
	require.Equal(t, older, page.Users[1].UserID)
	require.NoError(t, h.store.RevokeUser(t.Context(), recent, true))
	public, err := h.store.SearchPeople(t.Context(), paymentScope(t, h, older, false), db.PeopleQuery{Query: "Анна"})
	require.NoError(t, err)
	require.Len(t, public.Matches, 1)
	require.Equal(t, older, public.Matches[0].UserID)
	staff, err := h.store.SearchPeople(t.Context(), paymentScope(t, h, admin, true), db.PeopleQuery{Query: "Анна"})
	require.NoError(t, err)
	require.Len(t, staff.Matches, 2, "Global administrators retain access to revoked accounts")
}

func TestPeopleIndicatorsSummarizePastEventsAndUseConfirmedPayments(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	confirmExpectationPayment(t, f, 3000, payments.Initial, 0)
	scope := paymentScope(t, f.H, f.AmbassadorID, false)
	card, err := f.H.store.PeopleInfo(t.Context(), scope, f.OwnerID, f.Event.ID, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, card.Administration.Events, 1)
	pass := card.Administration.Events[0].Registration.Pass
	require.NotNil(t, pass)
	require.EqualValues(t, 3000, pass.Balance.ConfirmedMinor)
	require.Equal(t, f.Pass.PriceMinor-3000, pass.Balance.RemainingMinor)
	_, err = f.H.store.Pool().
		Exec(t.Context(), `UPDATE shuttle_events SET starts_at=$2,ends_at=$3 WHERE id=$1`,
			f.Event.ID, time.Now().Add(-72*time.Hour), time.Now().Add(-48*time.Hour))
	require.NoError(t, err)
	card, err = f.H.store.PeopleInfo(t.Context(), scope, f.OwnerID, f.Event.ID, uuid.Nil)
	require.NoError(t, err)
	require.Empty(t, card.Administration.Events)
	require.EqualValues(t, 1, card.Administration.PastEvents)
	require.Len(t, card.Administration.PastEventSummaries, 1)
	require.Equal(t, f.Event.ID, card.Administration.PastEventSummaries[0].EventID)
	require.NotNil(t, card.Administration.LatestPastEventAt)
	card, err = f.H.store.PeopleInfo(t.Context(), scope, f.OwnerID, uuid.Nil, f.Event.ID)
	require.NoError(t, err)
	require.Len(t, card.Administration.Events, 1)
	other := registrationEvent(t, f.H, f.Admin)
	_, err = f.H.store.PeopleInfo(t.Context(), scope, f.OwnerID, f.Event.ID, other.ID)
	require.ErrorIs(t, err, db.ErrPeopleInvalid)
	_, err = f.H.store.PeopleInfo(t.Context(), scope, f.OwnerID, uuid.Nil, other.ID)
	require.ErrorIs(t, err, db.ErrPeopleDenied)
}
func setPeopleName(t *testing.T, h *harness, id uuid.UUID, first, last string) db.UserProfile {
	t.Helper()
	scope := ownerProfileScope(t, h, id)
	p, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	p, err = h.store.MutateProfile(
		t.Context(),
		scope,
		db.ProfileMutation{
			OperationID:     uuid.New(),
			ExpectedVersion: p.Version,
			Kind:            "set_name",
			FirstName:       first,
			LastName:        last,
		},
	)
	require.NoError(t, err)
	return p
}
func personVisible(t *testing.T, h *harness, id uuid.UUID, visible bool) {
	t.Helper()
	scope := ownerProfileScope(t, h, id)
	p, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	_, err = h.store.MutateProfile(
		t.Context(),
		scope,
		db.ProfileMutation{OperationID: uuid.New(), ExpectedVersion: p.Version, Kind: "set_visibility", Visible: &visible},
	)
	require.NoError(t, err)
}

func TestPeopleVisibilitySearchInfoAndScopedDirectory(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	admin, adminToken := registrationUser(t, h, 29101, true)
	member, memberToken := registrationUser(t, h, 29102, false)
	ambassador, ambassadorToken := registrationUser(t, h, 29103, false)
	outsider, outsiderToken := registrationUser(t, h, 29104, false)
	second, _ := registrationUser(t, h, 29105, false)
	event := registrationEvent(t, h, adminToken)
	otherEvent := registrationEvent(t, h, adminToken)
	registerForEvent(t, h, memberToken, event.ID, "people-member", "leader")
	registerForEvent(t, h, outsiderToken, otherEvent.ID, "people-outsider", "follower")
	_, err := h.store.Pool().
		Exec(t.Context(), `INSERT INTO shuttle_event_authorities(event_id,user_id,role,version)
 VALUES($1,$2,'ambassador',1)`, event.ID, ambassador)
	require.NoError(t, err)
	setPeopleName(t, h, member, "Анна", "Петрова")
	profile := setPeopleName(t, h, member, "Анна", "Иванова")
	require.True(t, profile.Visible)
	setPeopleName(t, h, outsider, "Анна", "Иванова")
	setPeopleName(t, h, second, "Анна", "Иванова")
	for _, q := range []string{"Иванова Анна", "Anna Ivanova", "Анна Ивнова"} {
		page, searchErr := h.store.SearchPeople(
			t.Context(),
			paymentScope(t, h, ambassador, false),
			db.PeopleQuery{Query: q, EventID: event.ID},
		)
		require.NoError(t, searchErr)
		require.Len(t, page.Matches, 1)
		require.Equal(t, member, page.Matches[0].UserID)
	}
	page, err := h.store.SearchPeople(
		t.Context(),
		paymentScope(t, h, outsider, false),
		db.PeopleQuery{Query: "Анна Иванова", Limit: 1},
	)
	require.NoError(t, err)
	require.Len(t, page.Matches, 1)
	require.True(t, page.HasMore)
	page, err = h.store.SearchPeople(t.Context(), paymentScope(t, h, outsider, false), db.PeopleQuery{Query: "Петрова"})
	require.NoError(t, err)
	require.Empty(t, page.Matches, "Public search must not reveal historical names")
	page, err = h.store.SearchPeople(
		t.Context(),
		paymentScope(t, h, ambassador, false),
		db.PeopleQuery{Query: "Петрова", EventID: event.ID},
	)
	require.NoError(t, err)
	require.Len(t, page.Matches, 1)
	require.True(t, page.Matches[0].Historical)
	code, raw := peopleRequest(t, h, outsiderToken, "info", map[string]any{"user_id": member})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), "administration")
	require.Contains(t, string(raw), "tg://user?id=29102")
	code, raw = peopleRequest(t, h, outsiderToken, "search", map[string]any{"query": "Анна", "event_id": event.ID})
	require.Equal(t, http.StatusForbidden, code, string(raw))
	code, raw = profileRequest(
		t,
		h,
		memberToken,
		"set_visibility",
		uuid.New(),
		map[string]any{"expected_version": profile.Version, "visible": false},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"visible":false`)
	code, raw = peopleRequest(t, h, outsiderToken, "info", map[string]any{"user_id": member})
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw = peopleRequest(t, h, memberToken, "info", map[string]any{"user_id": member})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), "administration")
	code, raw = peopleRequest(t, h, ambassadorToken, "info", map[string]any{"user_id": member, "event_id": event.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "administration")
	require.Contains(t, string(raw), `"state":"waitlisted"`)
	page, err = h.store.SearchPeople(
		t.Context(),
		paymentScope(t, h, outsider, false),
		db.PeopleQuery{Query: "Анна Иванова"},
	)
	require.NoError(t, err)
	for _, match := range page.Matches {
		require.NotEqual(t, member, match.UserID)
	}
	personVisible(t, h, outsider, false)
	code, raw = peopleRequest(t, h, ambassadorToken, "info", map[string]any{"user_id": outsider, "event_id": event.ID})
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw = peopleRequest(t, h, adminToken, "info", map[string]any{"user_id": outsider})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "administration")
	code, raw = peopleRequest(t, h, adminToken, "info", map[string]any{"user_id": outsider, "event_id": event.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	var globalCard struct {
		Result db.PersonCard `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &globalCard))
	require.Equal(t, outsider, globalCard.Result.Profile.UserID)
	require.NotNil(t, globalCard.Result.Administration)
	require.Empty(t, globalCard.Result.Administration.Events, "An event filter limits indicators, not global card access")
	code, raw = peopleRequest(t, h, outsiderToken, "list", map[string]any{})
	require.Equal(t, http.StatusForbidden, code, string(raw))
	staffPage, err := h.store.ListPeople(
		t.Context(),
		paymentScope(t, h, ambassador, false),
		db.PeopleListQuery{EventID: event.ID},
	)
	require.NoError(t, err)
	require.Len(t, staffPage.Users, 1)
	require.Equal(t, member, staffPage.Users[0].UserID)
	for _, sort := range []string{"name", "name_desc", "recent_activity"} {
		var after *db.PeopleCursor
		seen := map[uuid.UUID]bool{}
		for {
			page, listErr := h.store.ListPeople(
				t.Context(),
				paymentScope(t, h, admin, true),
				db.PeopleListQuery{Sort: sort, Limit: 2, After: after},
			)
			require.NoError(t, listErr)
			for _, u := range page.Users {
				require.False(t, seen[u.UserID])
				seen[u.UserID] = true
			}
			if page.Next == nil {
				break
			}
			after = page.Next
		}
		require.Len(t, seen, 5)
	}
	_, err = h.store.Pool().
		Exec(t.Context(), `UPDATE shuttle_event_authorities SET role='administrator',version=version+1
 WHERE event_id=$1 AND user_id=$2`, event.ID, ambassador)
	require.NoError(t, err)
	_, err = h.store.ListPeople(t.Context(), paymentScope(t, h, ambassador, false), db.PeopleListQuery{EventID: event.ID})
	require.NoError(t, err)
	_, err = h.store.Pool().
		Exec(t.Context(), `UPDATE shuttle_event_authorities SET role='none',version=version+1
 WHERE event_id=$1 AND user_id=$2`, event.ID, ambassador)
	require.NoError(t, err)
	_, err = h.store.ListPeople(t.Context(), paymentScope(t, h, ambassador, false), db.PeopleListQuery{EventID: event.ID})
	require.ErrorIs(t, err, db.ErrPeopleDenied)
	foreign := paymentScope(t, h, admin, true)
	foreign.Organization = "foreign"
	_, err = h.store.ListPeople(t.Context(), foreign, db.PeopleListQuery{})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestPeopleMassageClientsAndScopedIndicators(t *testing.T) {
	t.Parallel()
	f, staff, schedule, binding := staffFixture(t)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	other := ownerProfileScope(t, f.H, f.AmbassadorID)
	setPeopleName(t, f.H, owner.UserID, "Private", "Client")
	setPeopleName(t, f.H, other.UserID, "Other", "Client")
	personVisible(t, f.H, owner.UserID, false)
	personVisible(t, f.H, other.UserID, false)
	bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	args := massageBookingArgs(f, schedule, 0)
	args.Selection.SpecialistID = "beth"
	bookMassage(t, f, other, args)
	scope := paymentScope(t, f.H, staff.UserID, false)
	page, err := f.H.store.SearchPeople(t.Context(), scope, db.PeopleQuery{EventID: f.Event.ID, Query: "Client"})
	require.NoError(t, err)
	require.Len(t, page.Matches, 1)
	require.Equal(t, owner.UserID, page.Matches[0].UserID)
	listed, err := f.H.store.ListPeople(t.Context(), scope, db.PeopleListQuery{EventID: f.Event.ID})
	require.NoError(t, err)
	require.Len(t, listed.Users, 1)
	card, err := f.H.store.PeopleInfo(t.Context(), scope, owner.UserID, f.Event.ID, uuid.Nil)
	require.NoError(t, err)
	require.NotNil(t, card.Administration)
	require.Len(t, card.Administration.Events, 1)
	raw, err := json.Marshal(card)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "registration")
	require.NotContains(t, string(raw), "balance")
	require.NotContains(t, string(raw), "service_orders")
	require.EqualValues(t, 1, card.Administration.Events[0].MassageBookings)
	_, err = f.H.store.PeopleInfo(t.Context(), scope, other.UserID, f.Event.ID, uuid.Nil)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	binding.Active = false
	bindingChange(t, f, staff, binding)
	_, err = f.H.store.ListPeople(t.Context(), scope, db.PeopleListQuery{EventID: f.Event.ID})
	require.ErrorIs(t, err, db.ErrPeopleDenied)
	page, err = f.H.store.SearchPeople(t.Context(), scope, db.PeopleQuery{EventID: f.Event.ID, Query: "Client"})
	require.ErrorIs(t, err, db.ErrPeopleDenied)
	require.Empty(t, page.Matches)
}
