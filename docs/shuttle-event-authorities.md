# Event authority contract

Event roles are stored in PostgreSQL, separate from IAM roles and assigned passes.
A role is `ambassador`, `administrator`, or `none`. `none` records revocation;
versions and immutable operation receipts are retained. Roles apply only to the
specified event and active local account in the same bot/organization.

Only a caller with the current API-authorized `zinusya.admin` IAM role can grant,
change or revoke event roles. Event authorities cannot grant themselves roles or
administer the whole bot. No IAM role is created by this operation.

## Agent API

The static `zinusya_authorities.me` tool accepts `event_id` and derives the local
user from the caller token. Its result contains event_id, user_id, role and version.
An active user with no stored assignment receives role `none`, version 0.

The permission-filtered `zinusya_administration` module exposes `authorities` and
`authority` to global administrators. `authorities` lists active local accounts'
stored roles, including explicit revocations, in user UUID order. Default and
maximum page size are 20; follow `next` as `after` with the same event_id.

`authority` takes event_id, user_id, role and expected_version. It uses Loom's
exact_parameters prepare/execute/status protocol:

- POST /internal/agent/administration/authority/prepare
- POST /internal/agent/administration/authority
- POST /internal/agent/administration/authority/status

Preparation stores an immutable actor/operation/argument-bound proposal, displays
all role-change identifiers and expires after five minutes. It grants no rights.
Execution requires the original proposal and idempotency_key equal to operation_id.
The API rechecks global administrator authorization. The transaction locks the
event, then active actor/target accounts, checks current role version and samples
DB time after locks before accepting an unexpired proposal.

Concurrent changes with the same expected version permit only one success. Role
change and original receipt commit atomically. Exact replay returns that receipt
without restoring its old role after a subsequent revocation. Changed input or
proposal is rejected; missing status is unknown, not proof of non-execution.
Known business failures return rejected with terminal_rejection; uncertain storage
failures return 503. A receipt is historical evidence, not current authority.

## Payment integration boundary

These tools do not submit evidence, credit money, or give general access to another
user's files. The [payment slice](shuttle-payments.md) checks the confirming user's current local
event role under the same event lock as the effect. Evidence reads must be scoped
to an explicit submitted proof and event authority. No authority cache is used.

## Migration and verification

Migration 00011 adds role assignments, proposals and receipts without changing
existing users, passes or event configuration. Apply it explicitly with -migrate
before using the new endpoints. Export these records before an intentional down
migration. Earlier binaries cannot expose or enforce these new roles.

Behavioral PostgreSQL tests cover preparation without grant, owner reads, roleless
self-escalation denial, event isolation, revoke/replay/restart, changed arguments,
stale versions, revoked/foreign targets, expiry, foreign actors and competing
changes. Review snapshots and runtime evidence are recorded in the ignored local
work directory. Final bounded serial golangci-lint exited 0 with 0 issues.
Three fresh independent source reviewers found no actionable issues. Full Linux
race tests passed (integration 29.479s); named container
zinusya-authorities-complete-qa-20261013 exited 0. The first full run exposed a test
expectation that allowed only one administrative write. A subsequent run exposed
an anonymous Huma confirmation-schema collision with context selection. A distinct
named confirmation type fixed that conflict; the original failures are retained.

Native real-IAM/Loom acceptance passed on the owned seal fixture: exact approval
showed event/user/role, preparation granted no authority, the broker committed one
grant, owner reads reflected it, roleless escalation was denied, and revocation
changed current authority. An actual bot restart preserved the current revoked
role and original receipts even after proposal expiry. Evidence:
.cache/shuttle-live-test/native-authorities-proof.json. The provider was the manual
scenario mailbox; no real Telegram messages were sent. Migration 00011 was applied
explicitly after an initial 503 from its absence; that failure is not rewritten
as successful pre-migration behavior.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
