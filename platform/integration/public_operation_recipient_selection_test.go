package integration_test

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPublicPassOperationRecipientSelection(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), `
INSERT INTO core.users(id,telegram_id,name,can_book) VALUES
 ('selection-common',404,'Common recipient',true),
 ('selection-third',707,'Third common recipient',true),
 ('selection-wanted',505,'Wanted recipient',true),
 ('selection-other',606,'Other recipient',true);
INSERT INTO core.pass_profiles(owner,role) VALUES
 ('selection-common','leader'),('selection-third','leader'),
 ('selection-wanted','leader'),('selection-other','leader');
UPDATE core.pass_profiles SET legal_name='Synthetic selection name'`)
	require.NoError(t, err)
	f.b.Host.HTTP = &http.Client{Transport: unavailablePublicBatch{}}
	for index, recipients := range []string{"[101,404,707,505]", "[101,404,707,606]"} {
		admitted := runPassVM(t, f, 69000+int64(index), 202,
			"Assign dance passes from profiles for Telegram recipients "+recipients,
			fmt.Sprintf(`return tools.passes.batch.assign({event:"dance",recipients:%s,
assignment:{create:true,from_profile:true}});`, recipients))
		require.Empty(t, admitted.Error)
		var pending struct {
			ID       string `json:"operation_id"`
			Complete bool   `json:"complete"`
		}
		require.NoError(t, json.Unmarshal(admitted.Result, &pending))
		require.NotEmpty(t, pending.ID)
		require.False(t, pending.Complete)
	}
	f.b.Host.HTTP = f.b.API.HTTP

	// Selection uses only public returned references, not IDs saved by the fixture.
	discovered := runPassVM(t, f, 69002, 202, "Find the earlier dance batch for recipient 505", `
const recent = (await tools.passes.operations({})).filter(o => o.tool === "passes.batch.assign");
const exact = [];
for (const operation of recent) {
  exact.push(...await tools.passes.operations({operation_id:operation.operation_id}));
}
const selected = exact.find(o => o.context && o.context.recipients.includes(505));
const other = exact.find(o => o.context && o.context.recipients.includes(606));
if (!selected || !other) throw new Error("requested recipients not discoverable");
return {recent, exact, selected:selected.operation_id, other:other.operation_id};`)
	require.Empty(t, discovered.Error)
	var selection struct {
		Recent   []interaction.RegistrationOperationSummary `json:"recent"`
		Exact    []interaction.RegistrationOperationSummary `json:"exact"`
		Selected string                                     `json:"selected"`
		Other    string                                     `json:"other"`
	}
	require.NoError(t, json.Unmarshal(discovered.Result, &selection))
	require.Len(t, selection.Recent, 2)
	require.Len(t, selection.Exact, 2)
	require.NotNil(t, selection.Recent[0].Context)
	require.NotNil(t, selection.Recent[1].Context)
	require.ElementsMatch(t, selection.Recent[0].Context.Recipients, selection.Recent[1].Context.Recipients,
		"bounded previews cannot distinguish these requests")
	require.NotContains(t, selection.Recent[0].Context.Recipients, int64(505))
	require.NotContains(t, selection.Recent[1].Context.Recipients, int64(606))
	require.NotEqual(t, selection.Selected, selection.Other)
	var effects int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM core.pass_admin_batches) +
 (SELECT count(*) FROM core.pass_booking_operations) +
 (SELECT count(*) FROM core.pass_bookings)`).Scan(&effects))
	require.Zero(t, effects, "recent and exact discovery must not execute either batch")

	resume := fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, selection.Selected)
	completed := runPassVM(t, f, 69003, 202, "Resume only the discovered batch for recipient 505", resume)
	require.Empty(t, completed.Error)
	var receipt struct {
		ID       string `json:"operation_id"`
		Complete bool   `json:"complete"`
		Result   []struct {
			TelegramID int64                        `json:"telegram_id"`
			Status     passbooking.AdminBatchStatus `json:"status"`
			Code       string                       `json:"code"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(completed.Result, &receipt))
	require.Equal(t, selection.Selected, receipt.ID)
	require.True(t, receipt.Complete)
	// Completion means attempted; each public recipient outcome must also succeed.
	require.Len(t, receipt.Result, 4, "public batch receipt: %s", completed.Result)
	for _, item := range receipt.Result {
		require.Equal(t, passbooking.AdminBatchSucceeded, item.Status,
			"recipient %d rejected with %s; public receipt: %s", item.TelegramID, item.Code, completed.Result)
	}
	service := passbooking.Service{DB: f.db}
	owners := []string{"alice", "selection-common", "selection-third", "selection-wanted", "selection-other", "bob"}
	beforeReplay := make([]passbooking.Booking, len(owners))
	for index, owner := range owners {
		beforeReplay[index], err = service.Get(t.Context(), owner, "dance")
		require.NoError(t, err)
		if owner == "selection-other" || owner == "bob" {
			require.Zero(t, beforeReplay[index].Version, "unselected recipient and acting admin must stay untouched")
		} else {
			require.Equal(t, "assigned", beforeReplay[index].State)
		}
	}
	replayed := runPassVM(t, f, 69004, 202, "Retry the same discovered batch", resume)
	require.Empty(t, replayed.Error)
	require.JSONEq(t, string(completed.Result), string(replayed.Result))
	for index, owner := range owners {
		after, readErr := service.Get(t.Context(), owner, "dance")
		require.NoError(t, readErr)
		require.Equal(t, beforeReplay[index], after, "retry cannot duplicate or execute sibling effects")
	}

	visible := runPassVM(t, f, 69005, 202, "Read the unselected batch while recipient access is allowed",
		fmt.Sprintf(`return tools.passes.operations({operation_id:%q});`, selection.Other))
	require.Empty(t, visible.Error)
	var available []interaction.RegistrationOperationSummary
	require.NoError(t, json.Unmarshal(visible.Result, &available))
	require.Len(t, available, 1)
	require.Equal(t, selection.Other, available[0].ID)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='selection-other'`)
	require.NoError(t, err)
	denied := runPassVM(t, f, 69006, 202, "Read the other batch after recipient authorization changed",
		fmt.Sprintf(`async function read(id) {
  try { return {value:await tools.passes.operations({operation_id:id})}; }
  catch (error) { return {error:String(error)}; }
}
return {exact:await read(%q), absent:await read(%q), recent:await tools.passes.operations({})};`,
			selection.Other, rand.Text()))
	require.Empty(t, denied.Error)
	var privacy struct {
		Exact struct {
			Value json.RawMessage `json:"value"`
			Error string          `json:"error"`
		} `json:"exact"`
		Absent struct {
			Error string `json:"error"`
		} `json:"absent"`
		Recent []interaction.RegistrationOperationSummary `json:"recent"`
	}
	require.NoError(t, json.Unmarshal(denied.Result, &privacy))
	require.NotEmpty(t, privacy.Absent.Error)
	require.Equal(t, privacy.Absent.Error, privacy.Exact.Error)
	require.Empty(t, privacy.Exact.Value)
	for _, operation := range privacy.Recent {
		require.NotEqual(t, selection.Other, operation.ID, "revoked batch must not remain discoverable")
	}
	blocked := runPassVM(t, f, 69007, 202, "Resume the other batch after recipient authorization changed",
		fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, selection.Other))
	require.NotEmpty(t, blocked.Error)
	for index, owner := range owners {
		after, readErr := service.Get(t.Context(), owner, "dance")
		require.NoError(t, readErr)
		require.Equal(t, beforeReplay[index], after, "revoked discovery/resume must remain read-only")
	}
}
