package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvalidArgumentsDoNotEchoPrivateValues(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	assert.Equal(t, 1, run([]string{"verify", "--unknown=private-canary"}, &output))
	assert.NotContains(t, output.String(), "private-canary")
	assert.Contains(t, output.String(), "invalid_arguments")
}

func TestCLIStagesSyntheticSnapshotAndReportsExactReplay(t *testing.T) {
	t.Parallel()
	target := filepath.Join(t.TempDir(), "stage")
	arguments := []string{"stage", "--snapshot", "../../testdata/synthetic", "--out", target}
	var output bytes.Buffer
	require.Zero(t, run(arguments, &output), output.String())
	assert.Contains(t, output.String(), `"verified":true`)
	assert.Contains(t, output.String(), `"import_ready":false`)
	assert.Contains(t, output.String(), `"reused":false`)
	output.Reset()
	require.Zero(t, run(arguments, &output), output.String())
	assert.Contains(t, output.String(), `"reused":true`)
}

func TestCLIUsersPlanKeepsPrivateArtifactSeparate(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	stage := filepath.Join(directory, "stage")
	var output bytes.Buffer
	require.Zero(
		t,
		run([]string{"stage", "--snapshot", "../../testdata/synthetic", "--out", stage}, &output),
		output.String(),
	)
	output.Reset()
	target := filepath.Join(directory, "users.jsonl")
	require.Zero(t, run([]string{"plan", "users", "--stage", stage, "--out", target}, &output), output.String())
	assert.Contains(t, output.String(), `"apply_ready":false`)
	assert.Contains(t, output.String(), `"invalid_records":1`, "unscoped fixture user must be blocked")
	assert.NotContains(t, output.String(), "unmapped_example")
	var first result
	require.NoError(t, json.Unmarshal(output.Bytes(), &first))
	assert.Nil(t, first.Report, "users plan must not report an unrelated failed verification")
	require.NotNil(t, first.Users)
	assert.False(t, first.Reused)
	assert.False(t, first.Users.Reused)
	output.Reset()
	require.Zero(t, run([]string{"plan", "users", "--stage", stage, "--out", target}, &output), output.String())
	var replay result
	require.NoError(t, json.Unmarshal(output.Bytes(), &replay))
	require.NotNil(t, replay.Users)
	assert.True(t, replay.Reused)
	assert.True(t, replay.Users.Reused)
	assert.Nil(t, replay.Report)
	output.Reset()
	require.Equal(
		t,
		1,
		run([]string{"plan", "users", "--stage", stage, "--out", target, "--max-records", "0"}, &output),
	)
	assert.Contains(t, output.String(), "invalid_limits")
}
