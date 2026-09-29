package bot

import (
	"testing"

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
