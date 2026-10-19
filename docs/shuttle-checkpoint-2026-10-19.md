# Development checkpoint: 2026-10-19

This is a source checkpoint on `codex/shuttle`, not a production release.
The project remains zns-chatbot / Zinusya; Shuttle names the rewrite.

## Accepted owned-runtime scope

The installed primary uses schema 59 and manifest `mvp-68`. Pair membership
forms and their session checks are installed. The primary retains 134 Loom
runs and 11 services; disposable acceptance stands were archived and removed.

Required legal details followed by solo leader waitlisting passed native-only,
agent-only, and both mixed interface paths. The native-only flow completed with
Loom stopped. These results do not establish complete registration, pass,
payment, orders, or massage parity.

See [native registration acceptance](shuttle-registration-native-runtime-qa-2026-10-19.md),
[mixed registration acceptance](shuttle-registration-mixed-runtime-qa-2026-10-19.md),
and [pair membership](shuttle-native-pair-membership.md).

## Source ahead of the installed image

Schema 60 and manifest `mvp-69` add assignment proposal discovery and transport
operation aliases. An actor can adopt an existing pending proposal using
`resume_proposal_id`; adoption preserves its original terms, expiry, and
canonical business receipt. It grants no approval. Execution still requires
fresh exact approval and current authorization.

Namespace guards prevent aliases from colliding with preparation or registration
receipt keys, including writes by an older binary. An empty alias table permits
migration rollback; populated aliases require export and reconciliation before
schema downgrade. Binary rollback must retain the expanded schema.

Four focused PostgreSQL root scenarios pass on the final source, covering
concurrent canonical/alias confirmation, restart, ownership and key collisions,
expiry after lock waits, and migration/legacy writer guards. The selected
`internal/db`, `internal/api`, and migration package checks pass. Lint reports
zero issues. Independent source reviews found two defects, both fixed; fresh
reviews have no actionable findings.

This foundation is not installed. Native assignment session wrappers, API/form
integration, and native/agent handoff acceptance remain to implement.

## Remaining acceptance and limits

Continue the native pass assignment vertical slice, then registration/pass and
payment parity, orders, massage, Mini App/HTML integration, Zitadel/zitadeltg
linking, and Loom recovery hardening. Keep every business operation available
through native controls and agent tools, with mixed continuation.

The complete integration suite has not been rerun for this checkpoint. A stale
manifest assertion in `registration_details_api_test.go` remains to reconcile.
Focused test success must not be reported as a full-suite or production pass.
Interactive certificate trust and joint zitadeltg browser acceptance remain
pending. Local secrets, runtime evidence, and nested worktrees are excluded.
