# Native registration runtime QA — 2026-10-19

The native path from required legal fields to a solo waiting-list registration
passes on one isolated mock-Telegram/real-Zitadel stand. Loom was stopped for the
whole participant flow. This is not full pass/payment or four-path acceptance.

## Exercised flow

Four fresh synthetic users were created through Mini App bootstrap and real IAM.
Fixture SQL configured one event with legal-name/passport requirements, capacity,
price, currency and event timezone `Europe/Minsk`. It inserted no participant
fields, registrations or passes. Only one user completed this native gate; the
other three were reserved for later agent/mixed gates.

1. Send `/register`, select the event and select Leader through the delivered
   inline buttons. The bot creates the required-fields expectation and no
   registration. Its response displays the event timezone and deadline.
2. Stop the bot gracefully and start the same container. Stop took 0.875 seconds,
   exit0, no OOM. The original expectation and its deadline remain unchanged.
3. Open the actual embedded form in a fresh headless Edge context with signed
   mock Telegram initData. Save synthetic legal name and passport through their
   forms. Observe exactly two field writes. Values are hidden after saving;
   the form says that filling fields does not register the user. Sign out.
4. Verify no registration exists yet. Click the delivered return-to-role button,
   then the fresh Leader button. One leader waiting-list registration is created,
   with no pass. Click that role button again: the same registration remains.
5. Send `/registrations`; observe the event, role and `Waitlisted` status.
   The Loom database still has zero runs, and its container stayed stopped until
   after this result was observed.

The screenshot was inspected. This browser uses a mock Telegram SDK and a
context-local TLS exception; it does not establish real Telegram Web acceptance.
The primary stand and the user's browser were not changed by this gate.

## Retained observer failure

The first final-status observer expected `Waiting list`; the actual valid caption
is `Waitlisted`. It timed out after the bot had already replied correctly. The
failure is retained. A second observer read that original response and database
state without resending the command or registration. No product patch was made
for this QA expectation error.

## Other evidence and remaining work

A separate limited PostgreSQL component run passed 24 behavioral tests, with no
failures or skips. It covers native registration/replay/restart, stale and foreign
choices, shared requirements, navigation across outages, confirmed-versus-pending
payment balance, partial payment and top-up. Its PostgreSQL stopped with exit0,
no OOM and was removed. These tests use mock peers and are component evidence.

Agent-only and both mixed required-fields/registration flows remain open, followed
by pair-to-pass assignment and participant payment submission/review/top-up. See
[the complete-workflow plan](shuttle-registration-pass-parity-plan.md). Production
readiness and unattended model comprehension are not claimed.

Evidence is under ignored `.cache/shuttle-work/registration-parity-baseline-v1/`
and `.cache/shuttle-work/registration-parity-runtime-v1-qa/`. The runtime archive
and cleanup result are recorded separately in `cleanup-proof.json`; check it
before treating the stand as removed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
