# Isolated Loom write QA fixture

Historical fixture record. The owned stand switched to committed, unmodified
Loom on 2026-10-15; [consumer acceptance](shuttle-loom-upstream-qa-2026-10-15.md)
passed operation preparation, restart, approval and duplicate-input checks.
The results below describe the earlier fixture and remain historical.

The unmodified owned Loom image reproduced issue 3: a persisted JSONB operation
failed its binding seal before dispatch. The failed run and approved operation
remain in the owned database; their evidence was not rewritten as successful.

On 2026-10-13, the upstream checkout contained an uncommitted candidate fix.
Our fixture was created inside ignored `.cache/shuttle-live-test/loom-fixture-seal`
from committed base `bbb36052f08a782ccd036ec362550fe57dc43aba`, then only
`internal/operations/coordinator.go` and its test were copied read-only from the
upstream working tree. Their SHA-256 hashes and base commit are saved in
`loom-fixture-seal-provenance.json`. No upstream source or developer stand changed.

The candidate canonicalizes embedded JSON arguments and preparation preconditions
before hashing the original request/preparation structure. It retains authority,
contract, approval, lease and dispatch checks. An independent review found no
new actionable defect. The existing numeric-spelling limitation remains: JSONB
may normalize `1e1` to `10`, while this key-order fix preserves numeric lexemes.
Our registration and context contracts use bounded integer amounts and versions.

The isolated PostgreSQL race suite for operations passed, including actual JSONB
key reordering, nested values, integers above the JavaScript exact range, altered
arguments, persisted tampering, approval and exactly one dispatch. Container
`zinusya-loom-seal-qa-20261013` exited 0; package duration 4.510 seconds.

Separately tagged image `zinusya-test-loom:seal-fixture` is selected only by the
ignored `loom-fixture.override.yaml` for our `shuttle-live-test` Loom, sandbox and
Docker proxy. The original image tag remains available. Do not use this fixture
as a production release or as evidence that the upstream committed version is fixed.

Native acceptance with real Zitadel delegated identities passed:

- Roleless owner: broker solo registration, broker event-context choice, then
  schema-validated script read of the pinned event. Two persisted write operations
  succeeded. Run `989cb16c-7fa3-473c-89c4-531f3edf9ca2`.
- Fixture administrator: side-effect-free assignment preparation, exact rendered
  price and parameters, no pass before approval, native approval input, broker
  assignment and original receipt status. Run
  `786dc222-8117-471d-9c8e-59d5ce9f3666`.

Provider responses were authored by Codex through the manual scenario mailbox.
Usage values are fixture data. IAM, discovery, scheduler, approvals, broker and
operation persistence were real. No new Telegram messages were sent in these
checks. Telegram Web automation timed out twice before access; end-to-end UI
acceptance remains pending. Browser CA import and zitadeltg acceptance remain
explicitly deferred by the user.

Ignored evidence: `native-registration-context-proof.json`,
`native-admin-assignment-proof.json`, fixture provenance and build logs.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
