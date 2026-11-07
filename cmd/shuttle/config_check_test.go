package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckConfigurationDoesNotConnect(t *testing.T) {
	t.Parallel()
	// The example has no reachable database or IAM. Validation must still finish.
	var output bytes.Buffer
	err := runArgs(&output, []string{"-config", "../../config/shuttle.example.yaml", "-check-config"})
	require.NoError(t, err)
	require.Contains(t, output.String(), "dependencies and database not checked")
	require.NotContains(t, output.String(), "fake-placeholder")
	require.NotContains(t, output.String(), "postgres://")
}

func TestCheckConfigurationRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"-config", "missing.yaml", "-check-config"},
		{"-check-config", "-migrate"},
		{"-check-config", "unexpected"},
	} {
		var output bytes.Buffer
		require.Error(t, runArgs(&output, args))
		require.NotContains(t, output.String(), "startup configuration valid")
	}
}
