package integration_test

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func openExport(t *testing.T, body []byte) *excelize.File {
	t.Helper()
	file, err := excelize.OpenReader(bytes.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	return file
}

func exportRows(t *testing.T, file *excelize.File, sheet string) [][]string {
	t.Helper()
	rows, err := file.GetRows(sheet)
	require.NoError(t, err)
	return rows
}

func seedExportStates(t *testing.T, f *fixture) map[string]string {
	t.Helper()
	ids := map[string]string{}
	for _, state := range []string{"unpaid", "cash", "proof", "paid", "deleted"} {
		order, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
			EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: state, Choice: orderChoice("preparty"),
		})
		require.NoError(t, err)
		// Import-like historical prices and unknown retired items, distinct from the live menu.
		choice := order.Choice
		choice.Customer = "=HYPERLINK(\"https://example.invalid\",\"Клиент A�B\")"
		choice.Days = map[string]orders.Day{"friday": {Mealtimes: map[string]orders.Meal{"dinner": {
			Dishes: []orders.Line{
				{Name: "caesar", Count: 2, Price: 123, Total: 246},
				{Name: "@retired\x01", Count: 1, Price: 50, Total: 50},
			},
			Service: orders.ServiceItems{
				Items: []orders.Line{{Name: "fork", Count: 2, Price: 10, Total: 20}},
				Total: 20,
			},
			Total: 316,
		}}, Total: 316}}
		choice.Total = 3816
		_, err = f.db.Exec(
			t.Context(),
			`UPDATE core.orders SET state=$2,choice=$3,payment_admin='bob',country='be' WHERE id=$1`,
			order.ID,
			state,
			choice,
		)
		require.NoError(t, err)
		ids[state] = order.ID
	}
	return ids
}

func TestExportUsesHistoricalPricesAndPaymentSemantics(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ids := seedExportStates(t, f)
	body, err := f.b.API.ExportOrders(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	file := openExport(t, body)
	assert.Equal(t, []string{"Заказы", "Содержимое", "Итоги", "Посуда", "Активности"}, file.GetSheetList())
	rows := exportRows(t, file, "Заказы")
	require.Len(t, rows, 5)
	byID := map[string][]string{}
	for index, row := range rows[1:] {
		byID[row[0]] = row
		assert.Equal(t, "101", row[1])
		assert.Equal(t, "=HYPERLINK(\"https://example.invalid\",\"Клиент A�B\")", row[2])
		assert.Equal(t, "202", row[6])
		assert.Equal(t, "38.16", row[9])
		assert.Equal(t, "1144.80", row[10])
		formula, formulaError := file.GetCellFormula("Заказы", "C"+strconv.Itoa(index+2))
		require.NoError(t, formulaError)
		assert.Empty(t, formula)
	}
	assert.NotContains(t, byID, ids["deleted"])
	assert.Equal(t, "FALSE", byID[ids["unpaid"]][7])
	assert.Equal(t, "FALSE", byID[ids["cash"]][7])
	assert.Equal(t, "TRUE", byID[ids["proof"]][7])
	assert.Equal(t, "FALSE", byID[ids["proof"]][8])
	assert.Equal(t, "TRUE", byID[ids["paid"]][8])
	assert.Contains(t, exportRows(t, file, "Итоги"), []string{"Цезарь", "4", "4.92"})
	assert.Contains(t, exportRows(t, file, "Посуда"), []string{"Вилка", "4", "0.40"})
	assert.Contains(t, exportRows(t, file, "Активности"), []string{"Препати", "2"})
	details := exportRows(t, file, "Содержимое")
	require.Len(t, details, 17)
	assert.Equal(t, "@retired", details[2][5])
	panes, err := file.GetPanes("Заказы")
	require.NoError(t, err)
	assert.True(t, panes.Freeze)
	assert.Equal(t, "A2", panes.TopLeftCell)
}

func TestExportEnforcesCurrentEventAndUserPermissions(t *testing.T) {
	t.Parallel()
	f := setup(t)
	for _, actor := range []string{"alice", "visitor"} {
		_, err := f.b.API.ExportOrders(t.Context(), actor, "sandbox-festival")
		requireCode(t, err, "forbidden")
	}
	_, err := f.b.API.ExportOrders(t.Context(), "bob", "different-event")
	requireCode(t, err, "forbidden")
	body, err := f.b.API.ExportOrders(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	assert.Len(t, exportRows(t, openExport(t, body), "Заказы"), 1)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='bob'`)
	require.NoError(t, err)
	_, err = f.b.API.ExportOrders(t.Context(), "bob", "sandbox-festival")
	requireCode(t, err, "forbidden")
}

func TestTelegramAndAgentExportShareRightsAndReceipt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	callback := modernExportCallback(t, f)
	handle(t, f.b, callback)
	handle(t, f.b, callback)
	files := exportDocuments(t, f, 202)
	require.Len(t, files, 1, "completed update replay must not send again")
	body, err := f.b.TG.Download(t.Context(), telegram.Document{FileID: files[0], Filename: "orders.xlsx"})
	require.NoError(t, err)
	assert.Equal(
		t,
		[]string{"Заказы", "Содержимое", "Итоги", "Посуда", "Активности"},
		openExport(t, body).GetSheetList(),
	)
	f.model.plan = agent.Plan{View: "orders", OrderAction: &agent.OrderProposal{Name: "export"}}
	handle(t, f.b, message(2, 202, "экспорт заказов"))
	assert.Len(t, exportDocuments(t, f, 202), 2)
	for index, user := range []int64{101, 303} {
		handle(t, f.b, message(int64(index+3), user, "экспорт заказов"))
		denied := aliceCallback(int64(index+5), callback.Callback.Message.ID, callback.Callback.Data)
		denied.Callback.From.ID = user
		denied.Callback.Message.Chat.ID = user
		handle(t, f.b, denied)
		assert.Empty(t, exportDocuments(t, f, user))
	}
	var content json.RawMessage
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=2 AND kind='order_export'`).
			Scan(&content),
	)
	assert.Contains(t, string(content), `"origin": "agent"`)
	assert.NotContains(t, string(content), "customer")
}

func TestExportRefusesOversizedSnapshotWithoutPartialFile(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.orders(id,event_id,owner,version,choice,state)
	 SELECT 'export-size-'||n,'sandbox-festival','alice',1,'{"days":{},"total":0}'::jsonb,'unpaid'
	 FROM generate_series(1,10001) n`)
	require.NoError(t, err)
	_, err = f.b.API.ExportOrders(t.Context(), "bob", "sandbox-festival")
	requireCode(t, err, "export_too_large")
}

func TestExportRejectsHistoricalTextBeyondExcelCellLimit(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "long-text",
		Choice:  orderChoice("preparty"),
	})
	require.NoError(t, err)
	for _, customer := range []string{strings.Repeat("a", 32768), strings.Repeat("🎉", 16384)} {
		_, err = f.db.Exec(
			t.Context(),
			`UPDATE core.orders SET choice=jsonb_set(choice,'{customer}',to_jsonb($2::text)) WHERE id=$1`,
			order.ID,
			customer,
		)
		require.NoError(t, err)
		_, err = f.b.API.ExportOrders(t.Context(), "bob", "sandbox-festival")
		requireCode(t, err, "export_too_large")
	}
	boundary := strings.Repeat("🎉", 16383) + "a"
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.orders SET choice=jsonb_set(choice,'{customer}',to_jsonb($2::text)) WHERE id=$1`,
		order.ID,
		boundary,
	)
	require.NoError(t, err)
	body, err := f.b.API.ExportOrders(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	value, err := openExport(t, body).GetCellValue("Заказы", "C2")
	require.NoError(t, err)
	assert.Equal(t, boundary, value, "boundary text must not be truncated")
}

func TestExportDoesNotMixEventsAndRetriesTelegramFailure(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.order_events(id,deadline,menu,extras)
	 SELECT 'other-event',deadline,menu,extras FROM core.order_events WHERE id='sandbox-festival';
	 INSERT INTO core.orders(id,event_id,owner,version,choice,state)
	 VALUES('foreign-order','other-event','alice',1,'{"days":{},"customer":"Foreign private name","total":0}'::jsonb,'unpaid')`)
	require.NoError(t, err)
	body, err := f.b.API.ExportOrders(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	assert.Len(t, exportRows(t, openExport(t, body), "Заказы"), 1)
	callback := modernExportCallback(t, f)
	post(t, f.fake.URL+"/lab/fault", map[string]string{"mode": "transient"})
	require.Error(t, f.b.Handle(t.Context(), callback))
	assert.Empty(t, exportDocuments(t, f, 202))
	handle(t, f.b, callback)
	assert.Len(t, exportDocuments(t, f, 202), 1)
}

func modernExportCallback(t *testing.T, f *fixture) telegram.Update {
	t.Helper()
	handle(t, f.b, message(100, 202, "/orders"))
	var token string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT token FROM bot.order_buttons
WHERE owner='bob' AND command->>'name'='export' LIMIT 1`).Scan(&token))
	callback := aliceCallback(1, 1, "o:"+token)
	callback.Callback.From.ID = 202
	callback.Callback.Message.Chat.ID = 202
	return callback
}

func exportDocuments(t *testing.T, f *fixture, user int64) []string {
	t.Helper()
	files := []string{}
	for _, message := range chatMessages(t, f, user) {
		if message.Document != nil && message.Document.Filename == "orders.xlsx" {
			files = append(files, message.Document.FileID)
		}
	}
	return files
}
