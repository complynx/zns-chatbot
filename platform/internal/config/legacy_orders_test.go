package config_test

import (
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyOrderBotNamespace(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, configured, token, mode string
		want                          int64
		bad                           bool
	}{
		{name: "disabled sandbox"},
		{name: "trusted prefix", token: "77:synthetic", want: 77},
		{name: "configured", configured: "77", want: 77},
		{name: "matching", configured: "77", token: "77:synthetic", want: 77},
		{name: "mismatch", configured: "77", token: "88:synthetic", bad: true},
		{name: "invalid configured", configured: "abc", bad: true},
		{name: "invalid prefix", token: "-77:synthetic"},
		{name: "missing suffix", token: "77:"},
		{name: "zitadel needs config", token: "77:synthetic", mode: "zitadel"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var cfg config.Config
			cfg.Auth.Zitadel.BotID, cfg.Auth.Mode = test.configured, test.mode
			cfg.Telegram.Token = config.Secret(test.token)
			got, err := cfg.LegacyOrderBotID()
			if test.bad {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}
