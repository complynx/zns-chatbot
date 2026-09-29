# Product localization foundation

`platform/internal/i18n` supplies typed message IDs, English and Russian catalogs,
CLDR cardinal selection, exact number formatting, and a shared locale fallback
policy. It does not by itself complete product localization or Functional QA.

## Library decision

Reuse the existing pinned `golang.org/x/text v0.41.0` dependency, now marked direct.
Its [plural package](https://pkg.go.dev/golang.org/x/text@v0.41.0/feature/plural)
provides CLDR cardinal rules; its
[language package](https://pkg.go.dev/golang.org/x/text@v0.41.0/language) parses
BCP 47; its [number package](https://pkg.go.dev/golang.org/x/text@v0.41.0/number)
formats localized digits and grouping. No second localization dependency is needed.
The plural package identifies its data as CLDR 32 and documents an evolving API.
Keep its version pinned and rerun the language fixtures on dependency upgrades.

The library's public number API accepts built-in Go numeric types, but not exact
decimal strings. This wrapper splits validated decimals into bounded integers and
uses library formatting for both parts. Decimal marks and minus signs are locale
registry data, checked against library output. No custom modulo-based language
rules and no binary floating-point conversion of payment amounts are used.

## Catalogs and extension

`localeRegistry` registers a locale's canonical code, language tag, punctuation,
required CLDR cardinal forms, and catalog factory. `catalog_en.go` and
`catalog_ru.go` are the initial catalogs. Adding a language means adding its
catalog and registry entry; consumers do not gain language-specific branches.
All six CLDR cardinal categories are supported by the catalog structure.

Tests iterate the registry automatically. They check message/count key parity
against English, nonempty text, placeholder parity, all declared plural forms,
and number punctuation against library output. The pinned exhaustive linter also
checks typed message-key maps. A runtime fallback does not waive completeness in
CI. An extension test registers Ukrainian and a regional catalog to prove that
exact translations win before compatibility fallback.

```go
text, err := i18n.Translate(language, i18n.PaymentContact, map[string]string{
    "name": contact.Name,
    "region": contact.Region,
})
days, err := i18n.TranslateCount(language, i18n.MealDays, strconv.Itoa(dayCount))
amount, err := i18n.FormatMoney(language, minorUnits, i18n.BYN)
```

Handle each error before delivering output. Unknown IDs return
`ErrUnknownMessage`; omitted values return `ErrMissingValue`; malformed numbers
return `ErrInvalidNumber`. Each returns no partial text. Empty supplied values
are valid; unused values are ignored. Interpolation inserts values once without
interpreting their braces, percent signs, or markup. Callers must escape values
for surfaces that enable HTML/Markdown and preserve existing length limits.

`PaymentSummary` takes `order`, `state`, `version`, `byn`, and `rub`; currency
codes are already in that template, so use `FormatNumber` for its amounts.
`PaymentUnavailable` takes `code`; `PaymentAdmin` and `PaymentContact` take `name`
and `region`. `LanguageCurrent` and `LanguageSaved` take `language`. Other plain
messages take no values. Map domain states explicitly to the `OrderState*` IDs.
API error codes remain stable; localized explanations require catalog entries.

## Locale resolution and preference storage

`SupportedLocales() []Locale` lists registered codes in lexical order.
`IsSupported(string) bool` validates an exact preference code: `ru` is valid,
while `RU`, `ru-RU`, and unknown codes are not. Use this for explicit selection.
`NormalizeLocale` resolves external tags such as Telegram's `ru-RU` or `en-US` to
the first registered fallback locale. Parsing accepts BCP 47 case variants and
the underscore separators supported by Unicode locale identifiers.

`FallbackLocales(raw) []Locale` returns the shared ordered lookup chain:

1. Exact canonical tag, then its base language.
2. Explicitly configured compatible language, when any.
3. English, without duplicates.

The only compatibility mappings are Belarusian `be` to `ru` and Ukrainian `uk`
to `ru`. Legacy input aliases `by` and `ua` become `be` and `uk`. Thus `be-BY`
returns `[be-BY, be, ru, en]`; `ua` returns `[uk, ru, en]`; `pl-PL` returns
`[pl-PL, pl, en]`. There is no generic Slavic, geographic, or fuzzy matching.
Invalid/empty tags fall back to English. HTTP Accept-Language lists are not tags.

Candidates may not have registered product catalogs. Event-admin content can
look up its own localized map with this same chain, including an exact Ukrainian
text before the Russian fallback. Catalog messages use the chain per message;
missing individual text or plural forms try the next candidate. Plural grammar
and number formatting are recomputed for the language actually used.
Unknown IDs and missing interpolation variables remain explicit errors.

Persist language codes as text, without an en/ru database enum. Ideally preserve
the canonical requested Telegram language separately from the resolved catalog
language: when a Ukrainian catalog is added, a Ukrainian user's original request
can then take precedence. An explicit user selection overrides the initial
Telegram preference. Country and currency do not choose presentation language.
The catalog package does not implement persistence or preference precedence.

## Numbers, quantities, and money

`TranslateCount(locale, CountID, decimalString)` currently supports `MealDays` and
`Places`. The decimal string is deliberate: `1`, `1.0`, and `1.00` carry different
visible-fraction information for CLDR. English uses `one` for integer 1; Russian
uses `one/few/many/other`. Tests cover 0, 1, 2, 5, 11, 21, 22, 25, 101, 111,
fractions, visible trailing zeros, grouping, and negative values.
Negative values use absolute magnitude for grammar but retain their sign. Domain
validation must still reject negative or fractional booking quantities.

`FormatNumber(locale, decimalString)` preserves exact fractional digits, including
trailing zeros. Input is `[-]digits[.digits]`: integer magnitude must fit uint64,
with at most 18 fractional digits. It rejects whitespace, exponents, localized
separators, NaN, and infinity. This is an output formatter, not a localized input
parser. Grouping and digit glyphs come from x/text. Russian grouping uses NBSP.

`FormatMoney(locale, minor int64, Currency)` supports the product's BYN and RUB,
both with two fraction digits. It returns amount plus ISO currency code, preserves
the full int64 range, and does not round or convert currencies. Other currencies
return `ErrUnsupportedCurrency`; adding one requires its actual minor-unit policy.
Currencies are independent of which language catalogs are registered.

## Dates and remaining acceptance

Store instants in UTC. Business deadlines and meal calendar dates use the event's
explicit IANA time zone, never the server's local zone or a locale-derived zone.
Convert an instant into that zone before rendering; keep calendar-only meal dates
as dates. Show a zone/offset where a cross-zone deadline could be ambiguous.
Use ISO 8601 in machine interfaces. Do not assume all localized dates use the same
day/month order, and do not silently parse ambiguous user dates.

There is no date renderer in this package yet because there is no integrated date
consumer. The [x/text message API](https://pkg.go.dev/golang.org/x/text@v0.41.0/message)
does not provide general time.Time localization; its documentation lists that as
planned. The first date renderer needs explicit
event-zone input, locale catalog layouts or an assessed date library, and tests
for day boundaries and DST gaps/overlaps. A regional date policy must preserve
the requested region if it matters; the current base-language number defaults
are not a claim of region-specific date support.

Full product acceptance still requires commands/help, remaining order cards,
validation, reminders, administrator flows, the Mini App, and agent responses.
Stored free text is not machine-translated. Pass the selected preference through
all renderers and agent calls; refresh existing cards on a language change.
Independent Functional QA must exercise both initial locales, fallback, selection,
changed-language refresh, and manual/agent flows in the Telegram-like stand.
