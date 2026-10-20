# Payment consent primary installation QA, 2026-10-20

Status: accepted on the owned local primary. The exact candidate from the
[isolated IAM/Loom runtime gate](shuttle-payment-consent-runtime-qa-2026-10-20.md)
is installed, with binary rollback verified. This is not a production deployment.

## Images and unchanged scope

The preceding primary image was `sha256:b376c67f95dc42adfd0238ac627083db7ed5e198764fdd1f7406c161d714de10` and the fixed image is `sha256:9ea106cda26f7d0e58674de3dfd611c35655711b08783914f17c2b2bf7b9a68a`.
The manifest remains `zinusya/mvp-78` and database schema64. The digest distinguishes
this fix from the preceding mvp78 image. The existing configuration is preserved;
the rendered Compose model differs only in the Shuttle application image.

The gate keeps the original PostgreSQL cluster/database identity, eleven services,
business records, durable tasks, outbox and scheduler queue state. The nine
companions retain their original fingerprints. Only the bot is recreated for each
binary leg; the original poller container is stopped and started once. No
Telegram message, IAM grant or new Loom run is submitted by the controller.

## Executed sequence and results

The original Telegram poller is stopped, accepted work is allowed to finish, and
the original bot is stopped. With these two writers stopped, separate custom-format
archives are retained for Shuttle, Loom and Zitadel. IAM/Loom companions continue
running; each dump is its own database-consistent snapshot, not an atomic
cross-database backup. Each archive is fully decoded with `pg_restore`; sizes and
SHA-256 digests are recorded. No database is restored by this gate.

The bot then runs fixed/original/fixed images. Each leg reaches ready state and
reports the expected zinusya/mvp78 manifest. Schema64 remains in place: no old
Goose Up or Down migration is executed during rollback. Business snapshots, exact
River jobs/queue state and SQL function/trigger definitions are compared after
each leg. PostgreSQL identity is unchanged.

Loom preservation now checks contents, not only the count of 134 original runs.
A read-only repeatable-read snapshot covers all 37 public tables and sequence state;
the schema-definition digest comes from `pg_dump --schema-only --no-owner --no-acl
--no-comments` with a fixed restrict key. Ownership/ACL metadata is not separately
audited by that digest. Table rows are counted and hashed in deterministic order.
Only `river_leader.expires_at` and `river_queue.updated_at` are omitted from row
hashes. Existing scheduler rows must otherwise match; deletion is allowed only for
old completed/cancelled jobs after one day, or discarded jobs after seven days.
Other row changes, replacements, new jobs and schema-definition changes fail.

Before installation, two actual read-only snapshots matched. A probe with a
changed run-content hash at the same run count was rejected. The same preservation
check runs at every binary leg and is carried into the new installed observer.
All 134 run contents and associated domain-table hashes remain preserved.

All eight journalled Docker lifecycle commands finish with exit zero. The three
healthy bot-stop CLI calls take 0.656, 0.641 and 0.672 seconds; these include Docker CLI
overhead and are not matched SIGTERM-to-die measurements or busy-worker/forced-kill
acceptance. All observed stops have exit zero and no OOM. No uncertain lifecycle
outcome or automatic retry occurred.

The final fixed image stays running. The original poller starts once and logs its
startup plus one completed polling cycle within the bounded observation window.
The new `installed_primary_observer_v24.py` and epoch are published after those
checks; historical observer23 and its epoch remain unchanged. Full Loom contents
and schema-definition checks are part of observer24, alongside original runtime,
mount/configuration, PostgreSQL identity, domain and queue checks.

Installed readback verifies all 17 mini-app HTML/CSS/JavaScript resources byte for
byte with `Cache-Control: no-store`. Three missing-session/token requests receive
401: native pass-payment targets, pair-assignment proposals and the agent payment
operation read. No logged-in business write or form action is performed by this
readback. The owned primary remains eleven services / 134 existing Loom runs.

## Review, evidence and limits

Two independent reviews took two rounds. The first identified count-only Loom
preservation as insufficient; complete content/schema-definition checks replaced
it before primary changes. The final reviewers found no actionable issues.
Product source and the Git index are unchanged during installation. Preparation,
all three binary legs, ingress recovery, epoch finalization and HTTP readback pass.

Evidence is retained under ignored
`.cache/shuttle-work/payment-consent-install-v1/`: `review-gate.json`,
`checkpoint.json`, `effects.json`, three `*-before.private.dump` archives,
`installed-epoch.json`, `http-assets-and-custody.json` and final readback. The
read-only snapshot probe is `payment-consent-loom-integrity-probe-v1.json`.
The shared reader is `loom_integrity_snapshot_v1.py`, whose digest is pinned in the
new epoch. No temporary QA stand is left running.

This gate proves this local binary transition and its stated preservation/auth
boundaries. It does not prove a restored database service, real Telegram WebView
rendering, another business-flow acceptance, token expiry, long simultaneous
outages, unattended model interpretation, full Linux/race coverage or production
readiness. Next: remaining IAM/session-expiry and long-outage integration gates;
joint CA/zitadeltg checks remain deferred for the agreed user session.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
