package i18n

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// Currency identifies the two supported payment currencies, each with two decimals.
type Currency string

const (
	BYN Currency = "BYN"
	RUB Currency = "RUB"
)

var (
	ErrInvalidNumber       = errors.New("invalid decimal number")
	ErrUnsupportedCurrency = errors.New("unsupported currency")
)

const maxFractionDigits = 18
const maxIntegerDigits = 20 // uint64 has at most 20 decimal digits.

type decimal struct {
	integer  uint64
	fraction string
	negative bool
	digits   []byte
	exponent int
}

// FormatNumber localizes an exact decimal string without rounding. Input is
// [-]digits[.digits], with a uint64-sized magnitude before the decimal point and
// at most 18 fraction digits. Visible trailing zeros are preserved. Whitespace,
// exponents, grouping separators, NaN, and infinity are rejected.
func FormatNumber(locale, value string) (string, error) {
	parsed, err := parseDecimal(value)
	if err != nil {
		return "", err
	}
	return parsed.format(localeRegistry()[NormalizeLocale(locale)]), nil
}

// FormatMoney renders exact minor units with two fraction digits and an explicit
// currency code. It does not convert currencies, round amounts, or use floats.
func FormatMoney(locale string, minor int64, currency Currency) (string, error) {
	if currency != BYN && currency != RUB {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedCurrency, currency)
	}
	raw := strconv.FormatInt(minor, 10)
	negative := strings.HasPrefix(raw, "-")
	raw = strings.TrimPrefix(raw, "-")
	const fractionDigits = 2
	for len(raw) <= fractionDigits {
		raw = "0" + raw
	}
	point := len(raw) - fractionDigits
	raw = raw[:point] + "." + raw[point:]
	if negative {
		raw = "-" + raw
	}
	formatted, err := FormatNumber(locale, raw)
	if err != nil {
		return "", err
	}
	return formatted + " " + string(currency), nil
}

func parseDecimal(value string) (decimal, error) {
	negative := strings.HasPrefix(value, "-")
	unsigned := strings.TrimPrefix(value, "-")
	integer, fraction, hasPoint := strings.Cut(unsigned, ".")
	if len(integer) == 0 || len(integer) > maxIntegerDigits || len(fraction) > maxFractionDigits ||
		(hasPoint && fraction == "") {
		return decimal{}, ErrInvalidNumber
	}
	digits := []byte(integer + fraction)
	for i, digit := range digits {
		if digit < '0' || digit > '9' {
			return decimal{}, ErrInvalidNumber
		}
		digits[i] = digit - '0'
	}
	magnitude, err := strconv.ParseUint(integer, 10, 64)
	if err != nil {
		return decimal{}, ErrInvalidNumber
	}
	return decimal{
		integer: magnitude, fraction: fraction, negative: negative, digits: digits, exponent: len(integer),
	}, nil
}

func (d decimal) format(spec localeSpec) string {
	printer := message.NewPrinter(spec.tag)
	result := printer.Sprintf("%d", d.integer)
	if d.fraction != "" {
		// Parsing already bounds the fraction to 18 ASCII digits, which fit uint64.
		fraction, _ := strconv.ParseUint(d.fraction, 10, 64)
		formatted := number.Decimal(fraction, number.NoSeparator(), number.MinIntegerDigits(len(d.fraction)))
		result += spec.decimalMark + printer.Sprint(formatted)
	}
	if d.negative {
		result = spec.minusSign + result
	}
	return result
}
