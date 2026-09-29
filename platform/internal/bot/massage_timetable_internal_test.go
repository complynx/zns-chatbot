package bot

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTimetablePublicMount(t *testing.T) {
	t.Parallel()
	renderer := massageRenderer{
		bot:      &Bot{WebAppURL: "https://example.test/bot/miniapp/?order_id=old#old"},
		language: "ru", state: massageView{Event: "event&name"},
	}
	renderer.webTimetable()
	require.Len(t, renderer.choices, 1)
	address, err := url.Parse(renderer.choices[0].webApp.URL)
	require.NoError(t, err)
	require.Equal(t, "/bot/miniapp/massage", address.Path)
	require.Empty(t, address.Fragment)
	require.Equal(t, url.Values{"event": {"event&name"}, "lang": {"ru"}}, address.Query())
}
