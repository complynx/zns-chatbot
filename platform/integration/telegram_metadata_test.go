package integration_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type telegramMetadata struct {
	Username  string
	FirstName string
	LastName  string
	PrintName string
	Name      string
	Update    int64
}

func readTelegramMetadata(t *testing.T, f *fixture) telegramMetadata {
	t.Helper()
	var metadata telegramMetadata
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT username,first_name,last_name,print_name,name,telegram_metadata_update
 FROM core.users WHERE id='alice'`).Scan(&metadata.Username, &metadata.FirstName, &metadata.LastName, &metadata.PrintName, &metadata.Name, &metadata.Update),
	)
	return metadata
}

func TestTelegramMetadataTrustedSenderAndExport(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) VALUES('metadata',now()+interval '1 day');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('metadata','bob');
 INSERT INTO core.pass_profiles(owner,legal_name,passport) VALUES('alice','Synthetic Legal Name','TEST PASSPORT');
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at)
 VALUES('metadata','alice',1,'waitlist','leader','solo','bob',now())`,
	)
	require.NoError(t, err)
	update := message(9200, 101, "/language en")
	update.Message.From.FirstName = "=First"
	update.Message.From.LastName = "+Family"
	update.Message.From.Username = "MiXeD_Name"
	update.Message.Contact = &telegram.Contact{UserID: 202, FirstName: "Wrong contact"}
	update.Message.ForwardOrigin = &telegram.ForwardOrigin{
		Type:       "user",
		SenderUser: &telegram.User{ID: 202, FirstName: "Wrong forward"},
	}
	handle(t, f.b, update)
	assert.Equal(
		t,
		telegramMetadata{"MiXeD_Name", "=First", "+Family", "=First +Family", "=First +Family", 9200},
		readTelegramMetadata(t, f),
	)
	body, err := f.b.API.ExportPasses(t.Context(), "bob")
	require.NoError(t, err)
	file := openExport(t, body)
	rows := exportRows(t, file, "Passes")
	require.Len(t, rows, 2)
	assert.Equal(t, []string{"MiXeD_Name", "=First", "+Family", "=First +Family", "Synthetic Legal Name"}, rows[1][1:6])
	for _, cell := range []string{"C2", "D2", "E2"} {
		formula, formulaErr := file.GetCellFormula("Passes", cell)
		require.NoError(t, formulaErr)
		assert.Empty(t, formula)
	}
	var legal, passport string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT legal_name,passport FROM core.pass_profiles WHERE owner='alice'`).
			Scan(&legal, &passport),
	)
	assert.Equal(t, "Synthetic Legal Name", legal)
	assert.Equal(t, "TEST PASSPORT", passport)
}

func TestTelegramMetadataCallbackRetryAndOptionalClear(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Delivery.Fallback = time.Second
	handleVisible(t, f.b, message(9210, 101, "/language en"))
	assert.Equal(t, "Алиса", readTelegramMetadata(t, f).Name, "synthetic updates omit first_name")
	update := aliceCallback(9211, 1, "language:ru")
	update.Callback.From = telegram.User{ID: 101, FirstName: "Sender", LastName: "Old", Username: "old_name"}
	update.Callback.Message.From = telegram.User{ID: 202, FirstName: "Wrong callback author"}
	post(t, f.fake.URL+"/lab/fault", map[string]string{"mode": "transient"})
	require.NoError(t, f.b.Handle(t.Context(), update))
	failed := assertBotRateLimited(t, f)
	assert.Equal(t, "Sender Old", readTelegramMetadata(t, f).PrintName)
	newer := message(9212, 101, "/language en")
	newer.Message.From.FirstName = "New sender"
	handle(t, f.b, newer)
	waitBotRetryDeadline(t, f, failed)
	pumpBotDeliveries(t, f.b)
	want := telegramMetadata{"", "New sender", "", "New sender", "New sender", 9212}
	assert.Equal(t, want, readTelegramMetadata(t, f))
	before := len(chatMessages(t, f, 101))
	handleVisible(t, f.b, update)
	assert.Len(t, chatMessages(t, f, 101), before, "callback replay must not create a duplicate card")
	assert.Equal(t, want, readTelegramMetadata(t, f), "failed older event replay cannot regress metadata")
	newer.Message.From.FirstName = "Same ID replacement"
	handle(t, f.b, newer)
	assert.Equal(t, want, readTelegramMetadata(t, f))
}

func TestTelegramMetadataRejectsInvalidAndUntrustedUpdates(t *testing.T) {
	t.Parallel()
	f := setup(t)
	want := readTelegramMetadata(t, f)
	for index, sender := range []telegram.User{
		{ID: 101, FirstName: ""}, {ID: 101, FirstName: strings.Repeat("x", 257)},
		{ID: 101, FirstName: "Valid", LastName: "bad\nname"}, {ID: 101, FirstName: "Valid", Username: "@invalid"},
		{ID: 101, FirstName: "Bot", IsBot: true}, {ID: 999, FirstName: "Unknown"},
	} {
		update := message(int64(9220+index), 101, "/language en")
		update.Message.From = sender
		handle(t, f.b, update)
		assert.Equal(t, want, readTelegramMetadata(t, f))
	}
	group := message(9230, 101, "/language en")
	group.Message.From.FirstName = "Group identity"
	group.Message.Chat.Type = "group"
	handle(t, f.b, group)
	assert.Equal(t, want, readTelegramMetadata(t, f))
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET telegram_id=404 WHERE id='alice'`)
	require.NoError(t, err)
	bound := message(9231, 101, "/language en")
	bound.Message.From.FirstName = "Mismatched binding"
	var rejected *core.ProblemError
	require.ErrorAs(t, f.b.Handle(t.Context(), bound), &rejected)
	require.Equal(t, http.StatusConflict, rejected.Status)
	require.Equal(t, "bot_delivery_stale", rejected.Code)
	assert.Equal(t, want, readTelegramMetadata(t, f))
	var users int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.users`).Scan(&users))
	assert.Equal(t, 3, users)
}
