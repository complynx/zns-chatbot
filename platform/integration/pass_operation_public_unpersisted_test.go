package integration_test

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
)

type unavailablePublicBatch struct{}

func (unavailablePublicBatch) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path == "/internal/derived/pass-batches" {
		return nil, errors.New("synthetic batch unavailable before persistence")
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestPassOperationPublicUnpersistedPrivacy(t *testing.T) {
	t.Parallel()
	for _, revoked := range []bool{false, true} {
		t.Run(fmt.Sprintf("revoked-%t", revoked), func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.pass_profiles SET legal_name='Synthetic Name'`)
			require.NoError(t, err)
			f.b.Host.HTTP = &http.Client{Transport: unavailablePublicBatch{}}
			admitted := runPassVM(t, f, 68000, 202, "Assign 101 to dance", `
return tools.passes.batch.assign({event:"dance",recipients:[101],assignment:{create:true,from_profile:true}});`)
			require.Empty(t, admitted.Error)
			var pending struct {
				ID       string `json:"operation_id"`
				Complete bool   `json:"complete"`
			}
			require.NoError(t, json.Unmarshal(admitted.Result, &pending))
			require.NotEmpty(t, pending.ID)
			require.False(t, pending.Complete)
			f.b.Host.HTTP = f.b.API.HTTP
			ledger := agenthost.ScriptStore{DB: f.db}
			original, err := ledger.ReadRegistrationOperations(t.Context(), "bob", pending.ID)
			require.NoError(t, err)
			require.Len(t, original, 1)
			require.NotNil(t, original[0].Source)
			if revoked {
				_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
				require.NoError(t, err)
			}
			poll := runPassVM(t, f, 68001, 202, "Check my operation status", fmt.Sprintf(`
async function read(id) {
  try { return {value:await tools.passes.operations({operation_id:id})}; }
  catch (error) { return {error:String(error)}; }
}
return {exact:await read(%q),absent:await read(%q),recent:await tools.passes.operations({})};`,
				pending.ID, rand.Text()))
			require.Empty(t, poll.Error)
			var observed struct {
				Exact struct {
					Value []struct {
						Status  string          `json:"status"`
						Context json.RawMessage `json:"context"`
					} `json:"value"`
					Error string `json:"error"`
				} `json:"exact"`
				Absent struct {
					Error string `json:"error"`
				} `json:"absent"`
				Recent []json.RawMessage `json:"recent"`
			}
			require.NoError(t, json.Unmarshal(poll.Result, &observed))
			require.NotEmpty(t, observed.Absent.Error)
			if revoked {
				require.Equal(t, observed.Absent.Error, observed.Exact.Error)
				require.Empty(t, observed.Exact.Value)
				require.Empty(t, observed.Recent)
			} else {
				require.Empty(t, observed.Exact.Error)
				require.Len(t, observed.Exact.Value, 1)
				require.Equal(t, "not_committed", observed.Exact.Value[0].Status)
				require.NotEmpty(t, observed.Exact.Value[0].Context)
				require.Len(t, observed.Recent, 1)
			}
			current, err := ledger.ReadRegistrationOperations(t.Context(), "bob", pending.ID)
			require.NoError(t, err)
			require.Equal(t, original, current, "polling does not rewrite the admitted derivation")
			var effects int
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM core.pass_admin_batches) +
 (SELECT count(*) FROM core.pass_booking_operations) +
 (SELECT count(*) FROM core.pass_bookings)`).Scan(&effects))
			require.Zero(t, effects, "status reads must not execute the interrupted batch")
		})
	}
}
