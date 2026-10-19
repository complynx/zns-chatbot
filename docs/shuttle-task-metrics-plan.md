# Unresolved-task metric plan

Expose counts and oldest accepted-task age by a fixed operational state vocabulary
on the existing private OTel/Prometheus endpoint. This includes intentional user
waits and authorization/delivery uncertainty. It does not represent runnable River
latency, time in state, worker utilization or outbox backlog.

Architecture and Go consultation selected a shared lazy snapshot rather than a
background collector. Refresh at most once per 30 seconds after each completed
attempt, including failures. A 250 ms context covers pool acquisition and the
aggregate query. The pinned OTel pipeline serializes callbacks and Prometheus
does not propagate HTTP request cancellation into collection. Concurrent scrapes
may therefore share the bounded refresh delay; they are not wait-free.

Use one immutable published generation for rows, validity, last attempt completion
and last successful collection time. Success fills empty states with zero. Failure
suppresses task counts/ages, reports collection failure and retains the last-success
timestamp. It must not emit a false zero backlog or hide failure behind stale data.
Return no callback error so independent metrics continue to export. Preserve signed
future ages until observation, then clamp to zero after adding elapsed time.

The SQL projection folds unknown states to `other` before grouping. Its output is
bounded even though the persisted state column has no enum constraint. Count is
`bigint`; accepted age is a signed `double precision`. Exclude completed, failed,
cancelled, delivery_failed and closed tasks; include auth_unknown, delivery_unknown
and awaiting_user. Reject invalid/nonfinite samples. No identity, event, payload,
address, credential or error text is exported.

Migration 26 adds a partial `(state,created_at)` index with exactly the aggregate's
exclusion predicate. It changes no rows or data formats. Old applications continue
to read/write with the new index. Down removes only the index; test Up/Down/Up with
existing tasks and original operation receipts. This is an additive transition,
without data migration or destructive contraction. Exact aggregate work still
depends on unresolved population size; timeout is an honest failed collection,
not permission to truncate counts.

Acceptance requires real PostgreSQL state/folding/age checks, behavioral scrape
tests for caching/failure/recovery/omission, concurrent refresh and acquisition
timeout, independent reviews, lint/race gates, and owned native migration/private
scrape/restart/rollback proof. Preserve failures separately from later passes.
No outbox, River schema, agent API, dashboards, token cache, new configuration,
dependency or worker/instance-lock changes are included in this stage.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
