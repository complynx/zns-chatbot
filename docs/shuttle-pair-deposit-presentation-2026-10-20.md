# Native pair deposit presentation — 2026-10-20

Source mvp76 fixes one presentation omission found during the
[joined mixed gate](shuttle-joined-mixed-runtime-qa-2026-10-20.md): the pair
assignment form displayed the effective required deposit but omitted its original
configured minimum amount and percentage. Preview and historical receipt now show
all three values in English and Russian, including fractional percentages.

The values come from the frozen preparation/pass snapshots, not current event
settings. The view does not change the original proposal, result or confirmation
arguments. Old receipts without policy fields remain readable; absent minimum
values are not invented. Invalid supplied policy values block proposal recovery
before consent. The existing canonical description remains visible: replacing
its internal IDs/raw seconds requires a separate structured presentation of the
original event and payment-window terms. That issue is not closed here.

Two language-specific behavioral cases fail before the change. After the change,
all 19 pair-assignment UI cases and all 307 UI cases pass. They include pending
preview, explicit consent, historical receipt recovery, unchanged preparation and
invalid policy values. The first malformed-response test tried selecting an item
before its malformed list could be accepted; the corrected case corrupts the
subsequent proposal response instead. Both results are retained.

An isolated headless Edge component check uses the real section markup, CSS and
JavaScript with the existing test backend. English and Russian preview/receipt
screenshots show 21.50 BYN and 25.50 percent from the supplied snapshot. Each language
performs one confirmation and preserves the original preparation, without page
errors. This is component browser evidence, not IAM, Telegram WebView or installed
candidate acceptance. Manual inspection of the Russian preview confirms the
amounts and event-local deadline.

Affected API/task unit tests and Linux cross-build pass. Lint initially reports
manifest goimports formatting; after formatting that file, the final lint run
passes. Independent ordinary and quality reviews finish clean.
Evidence is retained locally in
`.cache/shuttle-work/pair-deposit-presentation-v1`.

The owned primary stays schema64/mvp74. The accepted mixed runtime image remains
mvp75; source mvp76 is not installed or packaged by this check. No new PostgreSQL,
full Go/race, remote CI or production gate is claimed for this UI-only change.


The subsequent [mvp77 component gate](shuttle-pair-assignment-terms-2026-10-20.md)
adds frozen structured event/window/cap display for new proposals. Legacy proposals
retain their original description. Installation acceptance remains separate.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
