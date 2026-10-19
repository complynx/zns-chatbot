# Readable massage approvals QA, 2026-10-17

Source component, owned image-only installation and a fresh real Telegram
approval/free-text rejection pass within the scope below. The first failed
manual-provider summary attempt remains preserved separately.

## Change and trust boundary

Massage booking and cancellation approvals now display the complete canonical
service description without appending their internal JSON arguments. The service
quote contains the event, specialist, service, local time, price and relevant
cancellation terms. Other methods and empty massage descriptions keep the existing
argument fallback. No approval ID, hash, deadline, permission or decision handling
was changed.

The owned frozen Loom source fc6c574 binds the full preparation, including its
description, arguments, preconditions and expiry. Its operation resolver supplies
the service description as Approval.Text. The provider's confirmation text does
not replace that description. This rendering change relies on that upstream
contract. There is no new local proposal-alias mapping or schema change.

## Behavioral evidence

The first component run reproduced appended JSON in all four massage cases.
The next candidate removed it, but its new test incorrectly expected explicit
reply metadata for an ordinary Telegram message. That failed run is preserved.
Production was not changed to fabricate reply references: ordinary text continues
the current dialogue; an actual Telegram reply carries its delivered presentation.

The corrected test covers eight combinations of booking/cancellation, English/
Russian, fresh/adopted proposals and plain/quoted replies after bot-service
restart. Preparation uses the real HTTP API and an isolated real PostgreSQL;
Loom and Telegram are mock peers. Assertions check meaningful local quote terms,
absence of technical JSON and model confirmation text, preservation of an adopted
proposal's original ID and expiry, and continuation of one existing run. Ordinary
text has no fabricated reply reference. Quoted text retains the delivered approval
ID. Both enter as steering without a protocol decision. Clarification does not
book or cancel a massage.

The shortened Loom approval expiry is fixture setup, not a separately asserted
deadline-preservation test. Existing nearby binding, ownership, adoption-expiry
and generic literal-argument tests were included in the same gate.

The final frozen Linux/race run passed six root tests and fourteen pass events,
with zero failures or skips. Lint passed with zero issues. Two fresh independent
source reviewers found no actionable findings. The initial overcomplex test was
split into preparation and reply helpers before the final run. The Git index
remained empty.

Exact owned QA containers and their volume were removed after checking ownership.
The ten main Compose services retained their fingerprints during component QA.

## Owned installation and first real probe

The unmodified release Dockerfile built the accepted 825 release inputs as
`sha256:904344b47e5852efcacd1020911fcad25ec57969d82a3f6c9a3d9f3755f6adca`.
The dedicated builder had a 2GiB/no-swap limit and was removed with its cache.
Two initial build-driver preflight attempts stopped before creating resources:
the copied guard expected the previous slice's root count and review fields.
Those guards were corrected to the actual six-root/two-review evidence; no
missing checks were represented as passing.

The owned image-only rollout retained schema46 / manifest mvp-49, configuration,
credentials, mounts and security/resource settings. The bot stopped in 0.61
seconds, exit0, without OOM. A private database backup was taken. Compared business
table hashes were identical across startup. Eight companion containers kept their
identities, images and starts, and the same ingress poller resumed. No production
or Loom developer stand was changed.

A new real Telegram request asked only to prepare cancellation of the existing
synthetic 03:00 GMT+01 massage, show conditions and wait for consent. It did not
reach preparation. The manual provider returned a history summary containing raw
context-heavy events: 105377 characters, exceeding Loom's 65536-byte summary
limit. Source inspection confirms SaveSummary rejects that response. This was a
harness error, not evidence of an upstream contract defect. The original request,
response and failed attempt are preserved; none were overwritten as a pass.

Cancellation of only that run was requested through the official Loom endpoint.
A later read confirmed cancelled/client_cancel at version9 after queue retry.
The original massage booking remained booked/version1. No massage operation was
requested during this first attempt.

## Fresh real Telegram acceptance

The second attempt supplied bounded summaries (6992 and7868 bytes), preserving
negation, relevant history and operation provenance without raw profile/context
snapshots. The first failed response was not overwritten. Real Loom then read
the current massage booking through the actual module API and prepared cancellation
against its current version1.

Telegram message398 displayed the canonical event, specialist, service,45.00BYN,
03:00 GMT+01 start, treatment/occupancy bounds and no-refund/no-payment-change terms
without appended internal JSON. Its reply keyboard showed ordinary Approve and
Reject captions. The account sent a plain free-text refusal in message399, without
a reply reference or tags: leave the massage booked and close only the approval.
Real Loom exposed that input as steering and bound its context to the pending
approval. The manual provider matched the exact latest input ID, approval ID and
argument hash before resolving reject. The tool returned status=rejected.

Telegram delivered acknowledgement400 and the Russian final reply401; the reply
keyboard disappeared. Both Shuttle tasks completed on the same Loom run
`e0527733-ab54-4d13-8e1e-8228141feb52`, which completed with140158 active milliseconds.
The massage booking stayed booked/version1. The successful retry has a separate
receipt; the first attempt remains failed in its original receipt.

This is real Telegram/IAM/Loom transport and lifecycle acceptance using manual
inference and synthetic usage accounting. It does not establish unattended model
interpretation. The English service quote follows the saved bot interface language;
the conversational final answer follows the Russian user message.

## Remaining gates

The later [real Mini App precommit cancellation SIGKILL gate](shuttle-massage-hardkill-real-qa-2026-10-17.md)
passes. Full massage lifecycle parity, other full-bot fault boundaries and
unattended model behavior remain open. The later [real Mini App HTTP response-loss
gate](shuttle-massage-response-loss-real-qa-2026-10-17.md) passes with a clean restart. The later [subprocess transaction-boundary gate](shuttle-massage-process-crash-qa-2026-10-17.md)
passes separately; it does not substitute for real Mini App HTTP recovery. No production readiness is
claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
