package i18n_test

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

func TestFormatNumber(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value string
		en    string
		ru    string
	}{
		{"0", "0", "0"},
		{"12345.6700", "12,345.6700", "12\u00a0345,6700"},
		{"-0.01", "-0.01", "-0,01"},
		{"-0.00", "-0.00", "-0,00"},
		{"18446744073709551615.01", "18,446,744,073,709,551,615.01", "18\u00a0446\u00a0744\u00a0073\u00a0709\u00a0551\u00a0615,01"},
		{"0.123456789012345678", "0.123456789012345678", "0,123456789012345678"},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Parallel()
			en, err := i18n.FormatNumber("en", test.value)
			require.NoError(t, err)
			require.Equal(t, test.en, en)
			ru, err := i18n.FormatNumber("ru", test.value)
			require.NoError(t, err)
			require.Equal(t, test.ru, ru)
		})
	}
	text, err := i18n.FormatNumber("fr-FR", "1234.50")
	require.NoError(t, err)
	require.Equal(t, "1,234.50", text)
}

func TestRejectInvalidDecimals(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"", "-", "+1", ".5", "1.", " 1", "1 ", "1,5", "1,000", "1e3", "NaN", "Inf", "--1", "١",
		"1.2.3", "18446744073709551616", strings.Repeat("1", 21), "0." + strings.Repeat("1", 19),
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			text, err := i18n.FormatNumber("en", input)
			require.ErrorIs(t, err, i18n.ErrInvalidNumber)
			require.Empty(t, text)
			text, err = i18n.TranslateCount("ru", i18n.Places, input)
			require.ErrorIs(t, err, i18n.ErrInvalidNumber)
			require.Empty(t, text)
		})
	}
}

func TestFormatMoney(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		minor int64
		en    string
		ru    string
	}{
		{0, "0.00 BYN", "0,00 RUB"},
		{1, "0.01 BYN", "0,01 RUB"},
		{10, "0.10 BYN", "0,10 RUB"},
		{100, "1.00 BYN", "1,00 RUB"},
		{-1, "-0.01 BYN", "-0,01 RUB"},
		{-123456, "-1,234.56 BYN", "-1\u00a0234,56 RUB"},
		{math.MaxInt64, "92,233,720,368,547,758.07 BYN", "92\u00a0233\u00a0720\u00a0368\u00a0547\u00a0758,07 RUB"},
		{math.MinInt64, "-92,233,720,368,547,758.08 BYN", "-92\u00a0233\u00a0720\u00a0368\u00a0547\u00a0758,08 RUB"},
	} {
		en, err := i18n.FormatMoney("en", test.minor, i18n.BYN)
		require.NoError(t, err)
		require.Equal(t, test.en, en)
		ru, err := i18n.FormatMoney("ru", test.minor, i18n.RUB)
		require.NoError(t, err)
		require.Equal(t, test.ru, ru)
	}
	text, err := i18n.FormatMoney("en", 100, i18n.Currency("JPY"))
	require.ErrorIs(t, err, i18n.ErrUnsupportedCurrency)
	require.Empty(t, text)
}
