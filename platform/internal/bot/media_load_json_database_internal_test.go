package bot

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestMediaLoadJSONDatabaseBoundaries(t *testing.T) {
	t.Parallel()
	b := &Bot{DB: foodPendingDatabase(t)}
	_, err := b.DB.Exec(t.Context(), `INSERT INTO bot.media_intake(id,owner,update_id,attachment_id,status)
 VALUES('m','alice',1,'a','done')`)
	require.NoError(t, err)
	loaders := []struct {
		name string
		load func() (mediaIntake, error)
	}{
		{"intake", func() (mediaIntake, error) { return b.loadMediaIntake(t.Context(), "alice", "m") }},
		{"outcome", func() (mediaIntake, error) { return b.loadMediaOutcome(t.Context(), "alice", "m") }},
		{"upload", func() (mediaIntake, error) {
			item, expired, loadErr := b.loadMediaUploadState(t.Context(), "alice", "m")
			require.False(t, expired)
			return item, loadErr
		}},
	}
	for _, test := range []struct {
		values []any
		want   mediaIntake
	}{
		{values: []any{nil, nil, nil, nil}},
		{values: []any{`null`, nil, nil, nil}},
		{values: []any{nil, `null`, nil, nil}},
		{values: []any{nil, nil, `null`, nil}},
		{values: []any{`{"name":"order"}`, nil, nil, `{"generation":2,"authorities":[]}`},
			want: mediaIntake{Command: &orders.Command{Name: "order"}}},
		{values: []any{nil, `{"name":"registration"}`, nil, `{"generation":2,"authorities":[]}`},
			want: mediaIntake{RegistrationCommand: &passbooking.Command{Name: "registration"}}},
		{values: []any{nil, nil, `{"name":"food"}`, `{"generation":2,"authorities":[]}`},
			want: mediaIntake{FoodCommand: &legacyfood.Command{Name: "food"}}},
	} {
		_, err = b.DB.Exec(t.Context(), `UPDATE bot.media_intake SET command=$1,registration_command=$2,
 food_command=$3,command_source=$4 WHERE id='m'`, test.values...)
		require.NoError(t, err)
		for _, loader := range loaders {
			item, loadErr := loader.load()
			require.NoError(t, loadErr, loader.name)
			require.Equal(t, "m", item.ID)
			require.Equal(t, test.want.Command, item.Command)
			require.Equal(t, test.want.RegistrationCommand, item.RegistrationCommand)
			require.Equal(t, test.want.FoodCommand, item.FoodCommand)
			if test.values[3] == nil {
				require.Nil(t, item.CommandSource)
			} else {
				require.NotNil(t, item.CommandSource)
				require.True(t, item.CommandSource.Valid())
			}
		}
	}
	for index := range 4 {
		values := []any{nil, nil, nil, nil}
		values[index] = `"private-invalid-value"`
		if index == 3 {
			// The source column requires an object, but nested field types remain unchecked.
			values[index] = `{"generation":"private-invalid-value"}`
		}
		_, err = b.DB.Exec(t.Context(), `UPDATE bot.media_intake SET command=$1,registration_command=$2,
 food_command=$3,command_source=$4 WHERE id='m'`, values...)
		require.NoError(t, err)
		for _, loader := range loaders {
			_, loadErr := loader.load()
			var invalid *json.UnmarshalTypeError
			require.ErrorAs(t, loadErr, &invalid, loader.name)
			require.False(t, core.IsDatabaseFailure(loadErr))
			require.NotContains(t, loadErr.Error(), "private-invalid-value")
		}
	}
}

func TestMediaLoadJSONDatabaseExpiryAndMissing(t *testing.T) {
	t.Parallel()
	b := &Bot{DB: foodPendingDatabase(t)}
	_, err := b.DB.Exec(t.Context(), `INSERT INTO bot.media_intake(id,owner,update_id,attachment_id,status,expires_at)
 VALUES('m','alice',1,'a','done',now()-interval '1 hour')`)
	require.NoError(t, err)
	_, err = b.loadMediaIntake(t.Context(), "alice", "m")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	item, err := b.loadMediaOutcome(t.Context(), "alice", "m")
	require.NoError(t, err)
	require.Equal(t, mediaDone, item.Status)
	item, expired, err := b.loadMediaUploadState(t.Context(), "alice", "m")
	require.NoError(t, err)
	require.True(t, expired)
	require.Equal(t, "m", item.ID)
	_, err = b.loadMediaOutcome(t.Context(), "bob", "m")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, expired, err = b.loadMediaUploadState(t.Context(), "alice", "missing")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.False(t, expired)
}
