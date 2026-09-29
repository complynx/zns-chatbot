package bot

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func TestFoodMenuPreservesSupportedLanguage(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ language, want string }{
		{"ru", "ru"}, {"ru-RU", "ru"}, {"en", "en"}, {"unknown", "en"},
	} {
		t.Run(test.language, func(t *testing.T) {
			t.Parallel()
			b := Bot{WebAppURL: "https://example.test/old?ignored=yes"}
			button, err := b.foodWebButton(test.language, legacyfood.Order{EventID: "food-event", ID: "food-order"})
			require.NoError(t, err)
			address, err := url.Parse(button.WebApp.URL)
			require.NoError(t, err)
			assert.Equal(t, "/menu", address.Path)
			assert.Equal(t, test.want, address.Query().Get("lang"))
			assert.Equal(t, "food-event", address.Query().Get("pass_key"))
			assert.Equal(t, "food-order", address.Query().Get("order_id"))
		})
	}
}

func TestFoodMenuUsesPublicMount(t *testing.T) {
	t.Parallel()
	b := Bot{WebAppURL: "https://example.test/bot/miniapp/?ignored=yes#old"}
	button, err := b.foodWebButton("en", legacyfood.Order{EventID: "food&event", ID: "food/order"})
	require.NoError(t, err)
	address, err := url.Parse(button.WebApp.URL)
	require.NoError(t, err)
	assert.Equal(t, "/bot/menu", address.Path)
	assert.Empty(t, address.Fragment)
	assert.Equal(t, "food&event", address.Query().Get("pass_key"))
	assert.Equal(t, "food/order", address.Query().Get("order_id"))
	assert.Empty(t, address.Query().Get("ignored"))
}
