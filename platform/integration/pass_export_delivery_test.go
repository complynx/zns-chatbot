package integration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestPassExportTelegramDeliveryAndReplay(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) VALUES('export-event',now()+interval '1 day');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('export-event','bob')`,
	)
	require.NoError(t, err)
	for index, language := range []string{"en", "ru"} {
		_, err = f.b.API.SetLanguage(t.Context(), "bob", language, false)
		require.NoError(t, err)
		update := message(int64(8000+index), 202, "/passes_table")
		handle(t, f.b, update)
		handle(t, f.b, update)
		drainPassNotices(t, f)
		files := passExportDocuments(t, f, 202)
		require.Len(t, files, index+1)
		body, downloadErr := f.b.TG.Download(
			t.Context(),
			telegram.Document{FileID: files[index], Filename: "passes.xlsx"},
		)
		require.NoError(t, downloadErr)
		assert.Equal(t, []string{"Passes"}, openExport(t, body).GetSheetList())
		text, translateErr := i18n.Translate(language, i18n.RegistrationExported, nil)
		require.NoError(t, translateErr)
		var found bool
		for _, card := range chatMessages(t, f, 202) {
			if card.Document == nil {
				found = found || strings.Contains(card.Text, text)
			}
		}
		assert.True(t, found)
	}
	label, err := i18n.Translate("ru", i18n.RegistrationExport, nil)
	require.NoError(t, err)
	click := orderClick(t, f, 202, 8003, label)
	handlePassVisible(t, f, click)
	assert.Len(t, passExportDocuments(t, f, 202), 3, "the pass menu uses the same authorized exporter")
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
	require.NoError(t, err)
	handlePassVisible(t, f, message(8004, 202, "/passes_table"))
	assert.Len(t, passExportDocuments(t, f, 202), 3, "revoked role prevents another download")
	stale, err := i18n.Translate("ru", i18n.RegistrationStale, nil)
	require.NoError(t, err)
	assert.Contains(t, passMenuCard(t, f, 202).Text, stale)
	handlePassVisible(t, f, message(8010, 101, "/passes_table"))
	assert.Empty(t, passExportDocuments(t, f, 101))
	prefs, err := f.b.API.Preferences(t.Context(), "alice")
	require.NoError(t, err)
	stale, err = i18n.Translate(prefs.Language, i18n.RegistrationStale, nil)
	require.NoError(t, err)
	assert.Contains(t, passMenuCard(t, f, 101).Text, stale)
	_, err = f.b.API.ExportPasses(t.Context(), "alice")
	requireCode(t, err, "forbidden")
}

func passExportDocuments(t *testing.T, f *fixture, user int64) []string {
	t.Helper()
	var files []string
	for _, message := range chatMessages(t, f, user) {
		if message.Document != nil && message.Document.Filename == "passes.xlsx" {
			files = append(files, message.Document.FileID)
		}
	}
	return files
}
