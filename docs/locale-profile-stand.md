# Synthetic locale catalog startup profiles

These profiles make catalog gaps controllable for original Functional F04.
They change only catalog data through the Go build overlay. Production locale
selection, alias handling, fallback, preference persistence and errors execute
unchanged source. This preparation is not Functional acceptance.

The real `/language` preference menu supports EN and RU only. The real synthetic
Telegram input accepts `be`, `by`, `uk`, `ua` and `pl` as incoming `language_code`.
Those two controls are different. If F04 requires five additional preference
menu choices, that remains a product capability gap; these fixtures do not add it.

| Profile | Deliberate catalog contents | LanguageChoose resolution |
| --- | --- | --- |
| primary-gap | Original EN/RU; no BE/UK/PL catalog | be/by/uk/ua use RU; pl uses EN |
| fallback-gap | Remove only LanguageChoose from RU | All five inputs use EN |
| missing-all | Remove only LanguageChoose from RU and EN | ErrUnknownMessage, no partial text |

`LanguageChoose` is the visible prompt produced by `/language` with no argument.
Other catalog entries remain unchanged, including successful selection receipts
and the current-language label. The missing-all profile deliberately prevents
this prompt; QA records actual delivery/error behavior, not an inferred pass.
The fixtures do not translate BE/UK/PL or alter model responses.

## Exact Linux preparation

Mount the immutable candidate root read-only at `/src`, an empty owned receipts
directory at `/receipts`, and owned Linux caches/scratch at the existing qualified
check paths. Run one profile per receipts directory. Source hashes are checked
before preparation; a changed catalog needs a reviewed successor fixture.

```sh
bash /src/platform/testdata/locale-profiles/prepare.sh fallback-gap
cd /src/platform
ZNS_TEST_LOCALE_PROFILE=fallback-gap go test \
  -overlay=/receipts/locale-profile/overlay.json ./internal/i18n \
  -run '^TestSyntheticLocaleCatalogProfile$' -count=1
go build -overlay=/receipts/locale-profile/overlay.json -o /receipts/zns ./cmd/zns
sha256sum /receipts/zns > /receipts/zns.sha256
```

Use the same literal profile in preparation and validation. `primary-gap` and
`missing-all` have the same command shape. There is no runtime switch, mutation
endpoint or environment-dependent catalog behavior. Bind the executable hash,
source commit, profile.txt and overlay hashes in the startup access annex. Only
the sole stand owner may install the compiled binary or replace a stand process;
do not modify a frozen Functional QA stand.

Run ordinary catalog completeness tests and pinned lint against the unchanged
candidate without the overlay. Overlay validation checks intentionally missing
data; it cannot stand in for the ordinary required gates.

## Actor input and preparation boundary

Use the existing Telegram-like UI and its messages/callbacks for Functional QA.
The existing input transport is `POST /lab/input` with `X-Sandbox: 1`:

```json
{"user":101,"text":"/language","language_code":"by"}
```

Sandbox identities remain Alice/101, Bob/202 and Visitor/303. An incoming tag does
not override an existing persisted preference. Before each initial-tag scenario,
the stand owner must provide an isolated synthetic starting state with the chosen
actor's `core.users.language` empty and no prior scenario activity. Initialize
stores the canonical first fallback candidate: `by` becomes `be`, `ua` becomes
`uk`; `be`, `uk` and `pl` remain themselves. QA must observe persisted/displayed
behavior and use actual callback controls for subsequent EN/RU selection.

Stand data reset, lifecycle permissions and actor custody are owned separately;
this source delivery grants none of those operations. EN/RU manual and agent
flows still require independent Functional QA on the actual released composition.
