package migrate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPassUnmappedNoMorePassesFieldFailsClosed(t *testing.T) {
	t.Parallel()
	_, err := convertPass([]byte(`{"notified_no_more_passes":{"unknown":true}}`))
	require.EqualError(t, err, "pass_field_unmapped")
}
