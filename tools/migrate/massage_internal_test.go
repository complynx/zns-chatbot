package migrate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMassageSourceExistenceAndUnresolvedFields(t *testing.T) {
	t.Parallel()
	raw := `{"_id":{"$oid":"000000000000000000000001"},"user_id":202,"pass_key":"event","length":1,"party":1,"slot":0,"specialist":101,"start":"2026-12-01T18:00:00+03:00","finalized_at":"2026-12-01T17:00:00+03:00","deleted":false,"client_notified":null,"specialist_notified":false}`
	b, err := convertMassage([]byte(raw), 77)
	require.NoError(t, err)
	assert.True(t, b.Deleted)
	assert.True(t, b.ShortSent)
	assert.True(t, b.SpecialistSent)
	assert.False(t, b.LongSent)
	_, err = convertMassage([]byte(strings.Replace(raw, `"slot":0`, `"slot":0,"receipt":"unknown"`, 1)), 77)
	require.EqualError(t, err, "massage_field_unmapped")
	_, err = convertMassage([]byte(strings.Replace(raw, `2026-12-01T18:00:00+03:00`, `2026-12-01T18:00:00`, 1)), 77)
	require.Error(t, err)
}
func TestMassageUnknownSpecialistDoesNotReleaseUserObligation(t *testing.T) {
	t.Parallel()
	var user map[string]json.RawMessage
	require.NoError(
		t,
		json.Unmarshal(
			[]byte(
				`{"_id":"u","bot_id":77,"user_id":101,"print_name":"Name","massage_specialist":{"about":"Bodywork","work_hours":[],"custom_rule":true}}`,
			),
			&user,
		),
	)
	row := convertUser(user, 77)
	deferUserMassageFields(user, &row)
	assert.Empty(t, row.DeferredMassage)
	assert.Contains(t, row.Blockers, "specialist_event_mapping_unresolved")
}
