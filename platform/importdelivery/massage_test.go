package importdelivery_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/importdelivery"
)

func TestMassageRegistrationValidatesBeforeWriting(t *testing.T) {
	t.Parallel()
	// A nil transaction proves that a bad later entry is rejected before any
	// earlier valid entry can allocate a lane sequence.
	for _, bad := range []importdelivery.MassageNotice{{ID: 0, Chat: 202}, {ID: 2, Chat: 0}, {ID: 2, Chat: -1}} {
		err := importdelivery.RegisterMassage(t.Context(), nil, 77, []importdelivery.MassageNotice{
			{ID: 1, Chat: 202}, bad,
		})
		require.EqualError(t, err, "massage_import_delivery_invalid")
	}
	require.Error(
		t,
		importdelivery.RegisterMassage(t.Context(), nil, 0, []importdelivery.MassageNotice{{ID: 1, Chat: 202}}),
	)
	require.NoError(t, importdelivery.RegisterMassage(t.Context(), nil, 0, nil))
}
