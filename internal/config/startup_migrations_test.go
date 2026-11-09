package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/config"
)

func TestStartupMigrationConnectionConfiguration(t *testing.T) {
	path := configFile(t, "\nmigration_database_url: postgres://migrator:synthetic@db/zinusya\n")
	c, err := config.Load(path)
	require.NoError(t, err)
	require.Contains(t, c.MigrationDatabaseURL, "migrator:synthetic")
	t.Setenv("ZNS_CHATBOT_MIGRATION_DATABASE_URL", "postgres://owner:override@db/zinusya")
	c, err = config.Load(path)
	require.NoError(t, err)
	require.Contains(t, c.MigrationDatabaseURL, "owner:override")
	t.Setenv("ZNS_CHATBOT_MIGRATION_DATABASE_URL", "https://owner:private@db/zinusya")
	_, err = config.Load(path)
	require.ErrorContains(t, err, "migration PostgreSQL URL")
	require.NotContains(t, err.Error(), "private")
}
