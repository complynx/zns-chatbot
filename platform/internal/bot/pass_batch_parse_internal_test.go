package bot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPassBatchCommandQuotedOptions(t *testing.T) {
	t.Parallel()
	result, err := parsePassBatch(
		`/passes_assign 101 --pass_key dance --leader --create_name "Ada Example" --price 0 --comment 'Two words' --append_to_tier 1 202`,
	)
	require.NoError(t, err)
	assert.Equal(t, []int64{101, 202}, result.Recipients)
	require.NotNil(t, result.Options.Create)
	assert.Equal(t, "Ada Example", *result.Options.Create.LegalName)
	assert.Equal(t, "Two words", *result.Options.Comment)
	assert.Zero(t, *result.Options.TotalPrice)
	assert.Equal(t, 1, *result.Options.AppendTier)
	for _, input := range []string{
		`/passes_assign --pass_key dance --leader --follower 101`,
		`/passes_assign --pass_key dance --create_last --create_name "Ada" 101`,
		`/passes_assign --pass_key dance --comment "unclosed 101`,
		`/passes_assign 101`,
		`/passes_cancel --price 0 101`,
		`/passes_tier 101`,
		`/passes_uncouple dance 101 202`,
	} {
		_, err = parsePassBatch(input)
		assert.ErrorIs(t, err, errPassBatchSyntax, input)
	}
}

type passBatchClock struct {
	now time.Time
	err error
}

func (c passBatchClock) Now(context.Context) (time.Time, error) { return c.now, c.err }
func TestPassBatchTrustedClock(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	value, err := (&Bot{RegistrationClock: passBatchClock{now: now}}).registrationBatchTime(t.Context())
	require.NoError(t, err)
	require.Equal(t, now, value)
	value, err = (&Bot{RegistrationClock: passBatchClock{err: context.Canceled}}).registrationBatchTime(t.Context())
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, value)
	before := time.Now()
	value, err = (&Bot{}).registrationBatchTime(t.Context())
	require.NoError(t, err)
	require.WithinRange(t, value, before, time.Now())
}
