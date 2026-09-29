package migrate

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcastProjectionAllowlist(t *testing.T) {
	t.Parallel()
	var record map[string]json.RawMessage
	require.NoError(
		t,
		json.Unmarshal(
			[]byte(
				`{"bot_id":77,"user_id":101,"print_name":"Visible","username":null,"inner_name_pt-BR":"Nome","inner_name_":true,"unknown":42,"state":{"state":""}}`,
			),
			&record,
		),
	)
	result := convertUser(record, 77)
	require.NotNil(t, result.Candidate)
	assert.Contains(t, result.Blockers, "unmapped_fields")
	assert.Contains(t, result.Candidate.BroadcastFields, "inner_name_pt-BR")
	assert.NotContains(t, result.Candidate.BroadcastFields, "inner_name_")
	assert.NotContains(t, result.Candidate.BroadcastFields, "unknown")
	assert.NotContains(t, result.Candidate.BroadcastFields, "state")
	assert.NotContains(t, result.Candidate.BroadcastFields, "first_name")
	assert.JSONEq(t, `null`, string(result.Candidate.BroadcastFields["username"]))
}
