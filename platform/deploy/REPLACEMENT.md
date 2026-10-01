# Single-host replacement owner

This isolated developer implementation is not production authorization or Stage D acceptance.

Build the host CLI with Go: go build -o znsreplace ./cmd/znsreplace. Run it on the same Linux host as Docker. The app receives no Docker socket or deployment credentials. The host service is privileged because stopping containers is deployment authority.

Review replacement.example.json and zns-replacement.service before installation. The service runs continuously, holds the exclusive kernel file lock, starts one generation and monitors all six components. Process/helper death, unhealthy app, failed inventory or admission-loss shutdown retires the group. systemd restarts the CLI through the same durable barrier. On host reboot, the persisted generation is reconciled before starting. Restarting the coordinator never reconnects an old runtime's admission.

Only app, evaluator, media-decoder, media-broker, sticker-decoder and sticker-broker are supported. Instance-tagged app configuration permits the reviewed OpenAI model, media-broker/sticker-broker endpoints and evaluator socket. Remote model servers are outside this contract.

## Sole launcher

Use compose.yaml with compose.replacement.yaml. The override disables independent restarts and isolates IPC volumes by generation. Keep images pinned to digests. The coordinator creates stopped containers, validates labels, process isolation, restart policy, instance environment and database endpoint/roles, persists exact IDs, then starts them. It neither builds nor pulls images during handoff; pull reviewed images separately.

ZNS_INVENTORY_DATABASE_URL comes from the host service environment. Use a dedicated role with CONNECT and pg_read_all_stats, not a managed runtime role. It needs no mutation, termination or schema privilege. managed_roles must exhaustively name the app and paid media accounting roles. runtime_database_host must match their reviewed internal endpoint. Inventory and runtime endpoints must resolve to the same PostgreSQL installation; deployment review must verify that network mapping. Machine-id and Docker daemon identity bind the ledger to its host.

No other launcher may use runtime credentials. External compose up, restart overrides, unlabelled writers and a second host violate this authorization boundary. Unknown/empty session tags under a managed role block replacement; known old sessions must also disappear. Missing roles, inadequate visibility, prepared transactions, changed host/daemon and malformed state block. The coordinator never terminates database sessions.

Proposed server_configs integration, for separate review: add an exclusive-zns-group strategy before rollout.sh dependency recursion; delegate start/restart to this service; reject per-service rollout/recreate for the group. Existing Python/Mongo mappings remain unchanged. Initial handoff requires an explicit legacy process/effect inventory and separately authorized cutover.

## Shutdown and uncertainty

The group gets a 30-second stop budget and five-second verification budget. Docker stop requests TERM with 25 seconds grace for exact IDs; targeted kill handles survivors. Only terminal process state plus zero managed-role DB sessions permits replacement. Application drain and telemetry remain ten and five seconds in runtime.yaml. Helpers currently rely on container TERM/KILL; this does not claim graceful helper joins. The container PID namespace is the termination boundary; privileged/host-PID containers are rejected.

Docker calls and output are bounded. Errors omit output and secrets. Admission-loss detection is not instantaneous: monitoring uses the existing app healthcheck (10-second interval, five-second timeout, six retries). Old accepted work may finish before the barrier; replacement cannot start during that interval.

If verification times out, persist blocked and start nothing. Service-manager retries repeat the barrier. A crash between create and persisting IDs leaves an unknown prepared container and blocks for operator reconciliation; it cannot start accidentally. Do not delete the ledger or guess old identities to bypass this condition.

Ledger updates fsync the new file, atomically rename it, then fsync the directory on Linux. flock excludes concurrent owners and releases after process death. Non-Linux deployment locking is rejected. Portable tests do not prove Linux durability or process isolation.

Generation IPC volumes are never reused. Container removal does not delete volumes; old empty generation volumes can be reclaimed after ledger proof. PostgreSQL data volumes are outside this operation.

Already accepted Telegram/OpenAI requests may continue remotely after local death. This fences owned processes and DB writers, not provider computation. Preserve durable uncertain-delivery/accounting outcomes and avoid blind retries.

## PostgreSQL disconnect detection prerequisite

A dead client does not prove its PostgreSQL backend has ended. A long server query can retain an old transaction after its container is killed. The coordinator must remain blocked while that backend exists.

For predictable operational recovery, configure client_connection_check_interval=100ms as a database-specific default for every dedicated managed runtime/accounting role before launching the group. This is a separately reviewed deployment prerequisite, not an automatic coordinator mutation. Verify the effective setting through fresh connections authenticated as each limited role; inspect pg_settings.setting, unit and context, and prove long-query disconnect detection on the deployed PostgreSQL/OS/network path. Role defaults affect new sessions, so an already running generation must not be assumed to inherit them. Prepared transactions must remain prohibited.

The synthetic positive test sets this default only for its disposable fixture role and verifies it through a limited-role connection. This does not establish deployed deadline compliance. Even with that setting, Docker delays, network failure or surviving backends may exhaust the five-second verification budget; replacement then stays unavailable. No budget is widened and no database session is terminated by the coordinator.

## Required proof

Deterministic tests cover ordering, unknown sessions/containers, process/session survivors, Docker/DB unavailability, host/daemon mismatch, journal failure, launch collision and helper crash. The PostgreSQL test checks tagged old-transaction visibility and unknown-tag visibility. Neither substitutes for physical Linux proof.

Required synthetic Linux scenarios: old DB transaction plus killed admission session; uncooperative app/decoder child; coordinator crash/reboot reconciliation; helper accounting session; Docker/DB uncertainty; no replacement before old identities disappear. Re-run the advisory admission negative control. Fresh Code QA and black-box Functional QA remain required.

Written by c2_final_lint_cleanup (GPT-6/Codex)
on behalf of Daniel Drizhuk
