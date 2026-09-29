package credits_test

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestPriceDisjointCategoriesAndReasoning(t *testing.T) {
	t.Parallel()
	price := credits.Price{
		Version:           "synthetic-v1",
		ConversionVersion: "identity-v1",
		Input:             "0.000002",
		Cached:            "0.000001",
		CacheWrite:        "0.000003",
		Output:            "0.000004",
		Conversion:        "1",
		CacheWrites:       true,
	}
	usage := credits.Usage{
		Basis:      "reported",
		Input:      new(int64(100)),
		Cached:     new(int64(30)),
		CacheWrite: new(int64(20)),
		Output:     new(int64(40)),
		Reasoning:  new(int64(25)),
	}
	amount, err := price.Estimate(usage)
	require.NoError(t, err)
	require.Equal(t, int64(350000), amount)
	usage.Reasoning = new(int64(40))
	same, err := price.Estimate(usage)
	require.NoError(t, err)
	require.Equal(t, amount, same)
	usage.CacheWrite = new(int64(71))
	_, err = price.Estimate(usage)
	require.ErrorIs(t, err, credits.ErrInvalid)
}

func TestPriceRoundsOnceAndRejectsUnknownOrOverflow(t *testing.T) {
	t.Parallel()
	price := credits.Price{
		Version:           "synthetic-v2",
		ConversionVersion: "fx-v1",
		Input:             "0.0000000001",
		Cached:            "0",
		CacheWrite:        "0",
		Output:            "0.0000000001",
		Conversion:        "1/3",
	}
	usage := credits.Usage{Basis: "reported", Input: new(int64(3)), Cached: new(int64(0)), Output: new(int64(3))}
	amount, err := price.Estimate(usage)
	require.NoError(t, err)
	require.Equal(t, int64(1), amount)
	usage.Cached = nil
	_, err = price.Estimate(usage)
	require.ErrorIs(t, err, credits.ErrInvalid)
	usage.Cached = new(int64(0))
	usage.Input = new(int64(math.MaxInt64))
	price.Input = "1"
	_, err = price.Estimate(usage)
	require.ErrorIs(t, err, credits.ErrInvalid)
	price.Input = strings.Repeat("1", 65)
	_, err = price.Estimate(usage)
	require.ErrorIs(t, err, credits.ErrInvalid)
	price.Input = "1e999999999"
	_, err = price.Estimate(usage)
	require.ErrorIs(t, err, credits.ErrInvalid)
}

func TestUsageReceiptsPreserveUnknownAndFailedOutput(t *testing.T) {
	t.Parallel()
	receipt := credits.ResponsesUsage(
		[]byte(
			`{"id":"req-test","model":"synthetic","status":"incomplete","usage":{"input_tokens":100,"output_tokens":20,"input_tokens_details":{"cached_tokens":30},"output_tokens_details":{"reasoning_tokens":19}},"output":[]}`,
		),
	)
	require.Equal(t, "reported", receipt.Basis)
	require.Equal(t, int64(19), *receipt.Reasoning)
	require.Nil(t, receipt.CacheWrite)
	written := credits.ResponsesUsage(
		[]byte(
			`{"model":"synthetic","usage":{"input_tokens":100,"output_tokens":1,"input_tokens_details":{"cached_tokens":30,"cache_write_tokens":20}}}`,
		),
	)
	require.Equal(t, int64(20), *written.CacheWrite)
	unknown := credits.ResponsesUsage([]byte(`{"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":2}}}`))
	require.Equal(t, "unknown", unknown.Basis)
	require.Nil(t, unknown.Input)
	missing := credits.ResponsesUsage([]byte(`{"id":"req-missing"}`))
	require.Equal(t, "unknown", missing.Basis)
	require.Equal(t, "req-missing", missing.ResponseID)
	duration := credits.TranscriptionUsage(
		[]byte(`{"text":"private","usage":{"type":"duration","seconds":1.250}}`),
		"synthetic",
	)
	require.Equal(t, "reported", duration.Basis)
	require.Equal(t, "1.250", *duration.AudioSeconds)
}

func TestPriceRejectsWrongTierAndContextRange(t *testing.T) {
	t.Parallel()
	price := credits.Price{
		Version:           "v1",
		ConversionVersion: "identity",
		Input:             "1",
		Cached:            "0",
		CacheWrite:        "0",
		Output:            "1",
		Conversion:        "1",
		ServiceTier:       "default",
		MaxInput:          100,
	}
	usage := credits.Usage{
		Basis:       "reported",
		Input:       new(int64(100)),
		Cached:      new(int64(0)),
		Output:      new(int64(0)),
		ServiceTier: "priority",
	}
	_, err := price.Estimate(usage)
	require.ErrorIs(t, err, credits.ErrInvalid)
	usage.ServiceTier = "default"
	_, err = price.Estimate(usage)
	require.NoError(t, err)
	usage.Input = new(int64(101))
	_, err = price.Estimate(usage)
	require.ErrorIs(t, err, credits.ErrInvalid)
}
