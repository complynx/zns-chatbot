package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
)

// Loading the operator template must not require provider traffic or a database.
func TestProductionTemplate(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("runtime.yaml")
	require.NoError(t, err)
	secret := strings.Repeat("a", 40)
	env := []string{
		// Config validates native absolute paths; use one on Windows test hosts too.
		"ZNS_SCRIPT__SOCKET=" + filepath.Join(t.TempDir(), "evaluate.sock"),
		"ZNS_DATABASE__URL=postgres://zns_runtime:" + secret + "@postgres/zns?sslmode=disable",
		"ZNS_AUTH__SIGNING_KEY=" + secret,
		"ZNS_TELEGRAM__TOKEN=123456:" + secret,
		"ZNS_TELEGRAM__WEB_APP_URL=https://bot.example.org/bot/miniapp/",
		"ZNS_MODEL__OPENAI_KEY=" + secret,
		"ZNS_AUTH__ZITADEL__ISSUER=https://identity.example.org",
		"ZNS_AUTH__ZITADEL__AUDIENCE=audience",
		"ZNS_AUTH__ZITADEL__BOT_ID=123456",
		"ZNS_AUTH__ZITADEL__BOT_CLIENT_ID=bot-client",
		"ZNS_AUTH__ZITADEL__BOT_CLIENT_SECRET=" + secret,
		"ZNS_AUTH__ZITADEL__API_CLIENT_ID=api-client",
		"ZNS_AUTH__ZITADEL__API_CLIENT_SECRET=" + secret,
		"ZNS_AUTH__ZITADEL__ACTOR_ID=actor",
		"ZNS_AUTH__ZITADEL__ACTOR_CLIENT_ID=actor-client",
		"ZNS_AUTH__ZITADEL__ACTOR_CLIENT_SECRET=" + secret,
		"ZNS_AUTH__ZITADEL__PROVISIONING__ORGANIZATION_ID=organization",
		"ZNS_AUTH__ZITADEL__PROVISIONING__EMAIL_DOMAIN=telegram.invalid",
		"ZNS_AUTH__ZITADEL__PROVISIONING__CLIENT_ID=management-client",
		"ZNS_AUTH__ZITADEL__PROVISIONING__CLIENT_SECRET=" + secret,
		"ZNS_MEDIA__SECRET=" + secret,
		"ZNS_STICKER__WORKER__SECRET=" + secret,
		"ZNS_ORDERS__ACTIVE_EVENT=event",
	}
	_, err = config.Load("app", data, env)
	require.NoError(t, err)
}
