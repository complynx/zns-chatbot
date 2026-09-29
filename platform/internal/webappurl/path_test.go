package webappurl_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/webappurl"
)

func TestRoute(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ entry, want string }{
		{"https://example.test", "https://example.test/menu"},
		{"https://example.test/old?ignored=yes#old", "https://example.test/menu"},
		{"https://example.test/miniapp/?ignored=yes#old", "https://example.test/menu"},
		{"https://example.test/bot/miniapp?ignored=yes#old", "https://example.test/bot/menu"},
		{"https://example.test/bot/miniapp/?ignored=yes#old", "https://example.test/bot/menu"},
		{"https://example.test/a%2Fb/miniapp/", "https://example.test/a%2Fb/menu"},
	} {
		t.Run(test.entry, func(t *testing.T) {
			t.Parallel()
			address, err := webappurl.Route(test.entry, "/menu")
			require.NoError(t, err)
			require.Equal(t, test.want, address.String())
			require.Equal(t, address.EscapedPath(), webappurl.Path(test.entry, "/menu"))
		})
	}
}
