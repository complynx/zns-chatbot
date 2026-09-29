package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFixtureProviderRequiresExplicitSyntheticConfiguration(t *testing.T) {
	t.Parallel()
	value := Config{Model: Model{Provider: "fixture", URL: "http://telegram:8091/lab/model"}}
	require.Error(t, value.validateModel("app"))
	value.SyntheticOnly = true
	require.NoError(t, value.validateModel("app"))
	require.NoError(t, value.validateModel("bot"))
	require.Error(t, value.validateModel("model"))
	value.Model.URL = ""
	require.Error(t, value.validateModel("app"))
}
