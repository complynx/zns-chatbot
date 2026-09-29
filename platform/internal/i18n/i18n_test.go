package i18n_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

func TestNormalizeLocale(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		want  i18n.Locale
	}{
		{"ru", i18n.Russian},
		{"ru-RU", i18n.Russian},
		{" RU-by ", i18n.Russian},
		{"ru-Cyrl-RU", i18n.Russian},
		{"ru-RU-u-nu-latn", i18n.Russian},
		{"ru_RU", i18n.Russian},
		{"en", i18n.English},
		{"en-US", i18n.English},
		{"", i18n.English},
		{"de-DE", i18n.English},
		{"russian", i18n.English},
		{"ru-INVALID", i18n.English},
		{"ru-", i18n.English},
		{"ru,en;q=0.5", i18n.English},
		{"und-RU", i18n.English},
		{"x-ru", i18n.English},
	} {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, i18n.NormalizeLocale(test.input))
		})
	}
}

func TestTranslatePaymentSummary(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		"order": "order-123", "state": "Paid", "version": "3", "byn": "12.50", "rub": "375.00",
	}
	text, err := i18n.Translate("en-US", i18n.PaymentSummary, values)
	require.NoError(t, err)
	require.Equal(t, "Payment for order order-123\nStatus: Paid · version 3\nTotal: 12.50 BYN / 375.00 RUB", text)
	values["state"], err = i18n.Translate("ru-RU", i18n.OrderStatePaid, nil)
	require.NoError(t, err)
	text, err = i18n.Translate("ru-RU", i18n.PaymentSummary, values)
	require.NoError(t, err)
	require.Equal(t, "Оплата заказа order-123\nСтатус: Оплачен · версия 3\nИтого: 12.50 BYN / 375.00 RUB", text)
}

func TestTranslateRejectsMissingValuesAndUnknownMessages(t *testing.T) {
	t.Parallel()
	text, err := i18n.Translate("en", i18n.PaymentContact, map[string]string{"name": "Daniel"})
	require.ErrorIs(t, err, i18n.ErrMissingValue)
	require.Empty(t, text)
	text, err = i18n.Translate("ru", i18n.ID("missing"), nil)
	require.ErrorIs(t, err, i18n.ErrUnknownMessage)
	require.Empty(t, text)
}

func TestTranslateKeepsValuesLiteral(t *testing.T) {
	t.Parallel()
	text, err := i18n.Translate("ru", i18n.PaymentContact, map[string]string{
		"name": "{region} <b>Daniel</b> %s", "region": "", "unused": "ignored",
	})
	require.NoError(t, err)
	require.Equal(t, "Связаться: {region} <b>Daniel</b> %s · ", text)
}

func TestTranslateFallback(t *testing.T) {
	t.Parallel()
	for _, locale := range []string{"", "fr-FR"} {
		text, err := i18n.Translate(locale, i18n.PaymentMethods, nil)
		require.NoError(t, err)
		require.Equal(t, "Payment methods", text)
	}
}
