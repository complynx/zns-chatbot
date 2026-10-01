# Registration clock scope assessment

Read-only technical scope assessment; not Code QA or Functional acceptance.
Source inspected at integration HEAD `ff5750421afd8975e5ae1a5657389d6ef06ab229`.
Proposal: `.worktrees/c-registration-clock-plan/docs/qa/c-next-capability-plan-2026-10-01.md`
(its stated source baseline is `f56bebd3ecdffaa258058a47f04a340171b26302`).
Owned output: this report only. No product, schema, clock, stand or data changes.

## Recommendation

Approve the proposed **19 product paths plus four focused test paths** for the
combined-app clock seam, with the obligations below. No meaningful smaller
complete scope emerged: intake, admission, locked mutation, maintenance
selection, tier/admin reads and event discovery each observe registration time
independently. Reducing the file count by inlining helpers is not a reduction
in behavior or risk. This approval recommendation does not include a completed
operator adapter, recovery capability or registration-wide acceptance.

One composition boundary needs explicit ownership before implementation:
`platform/cmd/zns/main.go:111` dispatches `api` and `bot` without `runApp`.
An app-only environment loader silently ignores configured time in those modes.
For binary-wide rejection of unsupported clock configuration, add **main.go as
a twentieth product path**, with coverage in the proposed composition test.
Alternatively the allocated control contract must be app-only and its reviewed
launcher must reject a clock-configured unsupported mode visibly. Do not claim
binary-wide fail-closed behavior from app.go alone. No standalone service wiring
is necessary for this acceptance slice.

## Required same-path corrections

- `registrationingress/ingress.go:85` inserts application ingress with a database
  receive-time default. Bind new application ingress as well as Telegram ingress
  to the trusted timeline. Preserve the existing advisory lock and immutable
  conflict behavior. `admission.go:171` reaches this path for direct application
  requests; manual/agent continuity must not depend on native classification.
- `store/migrations/078_registration_admission.sql:21` gives intents a real-time
  `checked_at` default. New synthetic admission must explicitly stamp its actual
  decision observation through `admission.go`; retention initialization uses
  `checked_at` in `queries/registration_retention.sql:4`. Original native receive
  time still owns the native first deadline. Non-native admission's fallback
  deadline must use the controlled admission observation. No migration or
  historical timestamp rewrite is needed.
- Carry the binding into `PreparedCommand` and `PreparedAssignment`, not just
  `Service`: their Apply/Capture methods observe time after acquisition of the
  existing event, permission and profile locks. A request-context timestamp or
  prepare-time capture would lose the current post-lock semantics.
- `deadlines.go:71-76` selects candidate events using real SQL time before
  `deadlines.go:110` samples under the event lock. Override both selection and
  locked observation. Preserve keyset bounds and normal due effects. Invitation
  selection/expiry stays strict `>` at 58 hours; unfinished rotation stays `<=`.
- Set the shared binding before the registration value is copied into the
  native resolver in `appservices/services.go:78`. The host, API, direct bot
  intake, batch services and maintenance must share one authority.

## Small control and persistence boundary

Prefer one case clock with a small typed read contract. A dedicated operator
control file on a persistent stand volume is sufficient if updates are atomic
and operator-only; no generic clock framework or production business ledger is
needed. An existing allocated control service is also acceptable. The developer
must select and document the concrete adapter with the stand owner before
editing excluded engineer paths. Its read result must represent the current
value, not a value cached when the process or request started.

Validate the exact `synthetic-qa-zns-registration-fixture` stand, dedicated
`synthetic_qa_zns_registration_fixture` database and owning role, synthetic
environment, case identity and original three users. Exact names alone do not
distinguish an older allocation: bind the new pack to its reserved runtime and
case identifier. Preserve the ce427 stands and their controls.

Persist anchor, current instant, case identity and monotonic revision. Anchor
once to an observed registration instant, then allow only monotonic advances
within a fixed sufficient horizon (60 hours is adequate). Use PostgreSQL
microsecond precision so exact equality and the next supported instant are
reproducible. Serialize updates or use compare-and-set on the revision; reject
rewind, overflow, wrong case, malformed or missing state. Restart must reuse the
same case and current value. Startup and later read failures must be visible;
never fall back to wall time while configured. Preserve cancellation and error
identity. Ordinary users, callbacks, scripts and model proposals receive no
clock mutation grant or arbitrary timestamp input.

## Production and domain boundaries

Nil/default binding must retain the current SQL-time expression, query count,
lock order, ACL reads and errors at every existing observation point. Keep
default SQL branches when necessary; replacing a SQL-only filter with an extra
clock query would violate that constraint. Test this behavior explicitly.

Keep authentication, inbox receive/lease state, profile workflows, delivery
availability/retries and unrelated domains on real time. Notification enqueue
already obtains real availability through defaults; do not stamp its transport
availability with synthetic decision time. The registration announcement and
passport-reminder workers contain additional real-time event-finish filters.
They can remain outside this slice only if the allocated cases keep event
finishes beyond the whole clock horizon and the excluded workers are not
claimed as accelerated-time coverage. Crossing event finish and testing those
workers requires an explicitly widened scope, not hidden mixed-clock behavior.

Use genuine runtime maintenance and existing transactional mutations for all
outcomes. The controller must never age bookings, rewrite original ingress or
ranks, manufacture receipts, or invoke a snapshot as a substitute for due-event
selection. Replays must preserve original evidence and recheck current rights.
Event B remains a negative ACL case under its existing role set.

## Required focused proof

The four proposed test files are a reasonable initial ownership boundary.
Prove a new native and application competitor after advance; immutable replay;
actual queued invitation start; isolated maintenance selection at exactly 58
hours and one microsecond later; ten-minute exact expiry/tail rotation with
original ingress retained; post-lock observation; default query/lock behavior;
clock read/cancellation failure; restart persistence and operator guard denial.
Exercise transactions against real PostgreSQL. Keep process/provider recovery
and genuine in-app role invalidation as separate acceptance obligations.

Written by registration_clock_scope (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
