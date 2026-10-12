# Shuttle manual acceptance on 2026-10-12

The actual Linux release process completed 67 black-box checks: 67 PASS, 0 FAIL. Scope is the first executable messaging stage only. Any FAIL keeps its acceptance gate open. The complete business MVP and production cutover are not accepted.

## Method and isolated stand

The operator drove explicit HTTP requests and process actions through a local PowerShell script. The script did not call go test or private product functions. The application ran from its Linux Docker image against a new PostgreSQL 17 container and controllable HTTP Telegram, signed-JWT IAM/JWKS/introspection, Loom and OTLP peers. The separate GetMe/EditMessageText probe called exported adapter methods; those methods are not exposed by the application.

Image ID: `sha256:4c9ddb38e44c0aef98ff8fa868d547c7d8adccd08f0074655812a3ef9149fbc3`. Image tag: `shuttle:review-qa-final`. Runtime user: `10001:10001`.

Owned containers: shuttle-manual-qa-pg-20261012, shuttle-manual-qa-peers-20261012 and shuttle-manual-qa-app-20261012. Only these containers were changed. PostgreSQL used port 55474; ingress 18081; private API 19091; peer controls 19180. All published ports bound to 127.0.0.1. All credentials, users and attachment IDs were fake. Real Telegram, Zitadel, Loom and production data were not mutated.

Migrations used a dedicated non-superuser database-owner role. The application used a separate runtime role with CONNECT, schema USAGE, table SELECT/INSERT/UPDATE/DELETE and sequence USAGE/SELECT. Runtime had neither database nor schema CREATE. SQL inspection checked durable outcomes; it did not replace the application queue or set task outcomes.

## Observed cases

| Case | Result | Observable evidence |
| --- | --- | --- |
| Q01 | PASS | Actual release image user=10001:10001, ID=sha256:4c9ddb38e44c0aef98ff8fa868d547c7d8adccd08f0074655812a3ef9149fbc3 |
| Q00 | PASS | CLI help lists explicit config/migrate flags, exit 0 |
| Q42 | PASS | Adapter-only manual GetMe + EditMessageText probe checks HTTP response fields/entities; no application exposure claimed |
| Q02 | PASS | Unknown YAML rejected, exit=1 |
| Q03 | PASS | Unmigrated DB startup rejected, exit=1 |
| Q04 | PASS | Explicit migrations through non-superuser DB owner apply and repeat safely, exits=0,0 |
| Q05 | PASS | Actual bot starts using env-overridden CRUD-only runtime role, schema/db CREATE=f\|f |
| Q06 | PASS | Health/readiness 200; metrics private listener only |
| Q07 | PASS | Missing webhook secret rejected 401 |
| Q08 | PASS | Group ingress rejected 400 |
| Q09 | PASS | Committed intake + one run/reply; duplicate retains task ID 93e7f11b-5db7-40dc-bd4d-81d0018e630b |
| Q10 | PASS | Agent Markdown converted to text + UTF16 bold entity without parse_mode |
| Q11 | PASS | account.me resolves verified JWT subject to Telegram user; subject=fe16a9b2-2ce4-470c-b805-e1d891459e95 |
| Q12 | PASS | Own task 200, foreign task 404, ordinary admin 403, no bearer 401 |
| Q49 | PASS | Admin role in Loom namespace and generic flat claim does not grant Shuttle API admin access |
| Q13 | PASS | Admin settings CAS updates once, stale version 409, audit persisted |
| Q14-41 | PASS | Unsupported sticker localized for language 'en': This message type is not supported yet. Please send text. |
| Q14-42 | PASS | Unsupported sticker localized for language 'ru': Этот тип сообщения пока не поддерживается. Пожалуйста, отправьте текст. |
| Q14-43 | PASS | Unsupported sticker localized for language 'be': Этот тип сообщения пока не поддерживается. Пожалуйста, отправьте текст. |
| Q14-44 | PASS | Unsupported sticker localized for language 'uk': Этот тип сообщения пока не поддерживается. Пожалуйста, отправьте текст. |
| Q14-45 | PASS | Unsupported sticker localized for language 'de': This message type is not supported yet. Please send text. |
| Q14-46 | PASS | Unsupported sticker localized for language '': Этот тип сообщения пока не поддерживается. Пожалуйста, отправьте текст. |
| Q15 | PASS | Manifest advertises only implemented account.me; file ID read API absent (target requirement NOT IMPLEMENTED) |
| Q16 | PASS | Question delivers keyboard; free-text answer controls original waiting run |
| Q17 | PASS | Choice callback + duplicate ACK spinner; only one Loom choice application |
| Q18 | PASS | Approval prompt localizes labels and sends explicit approval decision |
| Q19 | PASS | Cancel reaches Loom while question waiting and original task becomes cancelled |
| Q20 | PASS | Permanently malformed callback acknowledged 200 ignored; no poisoned redelivery |
| Q21 | PASS | Lost callback ACK preserves accepted response and does not replay Loom control |
| Q22 | PASS | Lost run creation receipt retries same task key and delivers one logical run |
| Q23 | PASS | Both upstream outages accepted work, bot restart restored same task, shutdown=419 ms exit=0 |
| Q24 | PASS | Freshly verified positive auth cache serves account.me during IAM outage |
| Q25 | PASS | Lost first chunk receipt not resent after restart; suffix blocked: delivery_unknown,delivery_blocked |
| Q26 | PASS | Definite invalid-entities rejection retries once as same plain text |
| Q27 | PASS | Permanent Telegram rejection records delivery_failed without endless resend |
| Q28 | PASS | Admin local revocation invalidates warm cache and writes audit; next user access 403 |
| Q29 | PASS | DB runtime settings survive bot restarts; YAML did not reset them |
| Q30 | PASS | HTTP/runtime/attempt/delivery metrics and W3C trace propagation to mock Loom observed, trace-bearing calls=42 |
| Q34 | PASS | Astral emoji uses UTF16 offsets; only verified current user gets mention entity |
| Q35 | PASS | Boundary code block sends one 4096-unit chunk; no whitespace-only Telegram message |
| Q36 | PASS | Near-expiry Loom session reauthorizes while retaining original run |
| Q37 | PASS | Lost non-idempotent session renewal receipt pauses auth_unknown without blind replay |
| Q38 | PASS | Actual OTLP exporter sends valid protobuf traces; decoded traces exclude user text and credentials |
| Q39 | PASS | Account tool rejects extra arguments; admin API rejects invalid settings bounds |
| Q40-photo | PASS | Current unsupported photo returns explicit localized error; file ingest NOT IMPLEMENTED |
| Q40-document | PASS | Current unsupported document returns explicit localized error; file ingest NOT IMPLEMENTED |
| Q40-audio | PASS | Current unsupported audio returns explicit localized error; file ingest NOT IMPLEMENTED |
| Q40-video | PASS | Current unsupported video returns explicit localized error; file ingest NOT IMPLEMENTED |
| Q41 | PASS | Stale prompt fingerprint is rejected without applying Loom answer |
| Q43 | PASS | Service actor and unlinked subject denied user tool; malformed JWT rejected |
| Q44 | PASS | Custom emoji plain Unicode survives agent input; custom emoji ID is not forwarded as a file |
| Q45 | PASS | Hung callback peer bounded by 1 s ACK deadline; accepted response in 1012 ms |
| Q46 | PASS | SIGKILL in-flight accepted task recovered once, same run key, latency=18962 ms; killed exit=137 |
| Q48 | PASS | Unclosed bold/link Markdown returns visible text without a renderer failure |
| Q50 | PASS | Telegram429 retry_after respected (1595.5824 ms): one rejected attempt, then one success |
| Q51 | PASS | Definitively rejected429 input followed by old-run completion promotes accepted ordinary message to one own run |
| Q52 | PASS | Unknown then429/completion preserves possible first effect; one application, no duplicate run; worker retry gap=1836.8749 ms |
| Q53 | PASS | Steering409 terminal race preserves accepted message and promotes one own run without an old-run effect |
| Q31 | PASS | Second app rejected by schema-wide singleton before touching live work |
| Q54 | PASS | Deferred TG429 result blocked after local revocation; state=delivery_blocked attempts=1; later access-denial notification delivered |
| Q55 | PASS | Deferred question prompt blocked after revoke while denial in same task passes ordering barrier; states=delivery_blocked,delivered |
| Q56 | PASS | External inactive introspection with TTL0 blocks deferred result, local revoked=f state=delivery_blocked attempts=1 |
| Q57 | PASS | TTL0 IAM outage keeps deferred result unsent; state=retryable attempts=1; restore delivers once |
| Q58 | PASS | Warm TTL60 positive authorization permits deferred result during IAM outage without waiting for restore |
| Q47 | PASS | Outbox-only permission failure emits delivery_read SQLSTATE 42501 without user text; restoring permission lets cancel finish |
| Q32 | PASS | Injected runtime permission fault emits SQLSTATE 42501 without message text or credentials |
| Q33 | PASS | Lost dedicated advisory lock backend stops actual process, exit=1 |

## Failures found and rechecked

The original release candidate had 55/56 checks pass. SIGKILL left an accepted task in creating_run while its River job stayed running for the entire 67,414 ms observation window. Evidence is preserved in baseline-before-rescue-fix. Native River rescue was then configured from the task lease and attempt timeout; the final Q46 measures recovery with the same operation key and one logical run.

An intermediate candidate passed 59/59 checks, but an added completion-race probe found a second defect: a definitely rejected HTTP 429 steering request lost the following message after its original run completed. The focused failure is preserved in control-before-fix. Final Q51/Q53 check promotion after definite 429/409 rejection. Q52 checks that an earlier unknown outcome followed by 429 does not create a duplicate run and that retries belong to the durable worker rather than the HTTP transport. The final matrix rechecks all implemented paths after these fixes.

A later review found a third defect after the 62/62 intermediate matrix: a deferred Telegram result could bypass a subsequent local access revocation. The focused old-image Q54 confirmed a rejected send becoming delivered after revoke; evidence remains in revocation-before-fix. Final Q54 checks the completed-result gate and a fresh denial notification. Q55 checks a deferred question prompt is blocked while the same-task denial passes the outbox ordering barrier.

## Evidence and reproduction

The local stand and evidence remain in the ignored .cache/shuttle-manual-qa directory: prepare.ps1, accept.ps1, peers.go, adapters.go, fake config.yaml, results.json, http-evidence.json, image.json, peer-final.json, database-summary.json, metrics.txt and process/fault logs. Peer evidence records deduplication keys, control calls, callback acknowledgements, generated messages and decoded OTLP traces. Bearer credentials are excluded from the HTTP evidence log. This local fixture directory is not part of the product release. The final evidence snapshot is final-67-pass. After the final clean review, the owned application, peer and PostgreSQL containers were removed. Evidence contains no bearer-token values; retained fixture credentials are fake local-only examples.

To reproduce on this workspace, use a freshly reviewed image and fresh owned containers:

```powershell
docker build -f deploy/shuttle/Dockerfile -t shuttle:review-qa-final .
pwsh -NoProfile -File .cache/shuttle-manual-qa/prepare.ps1
pwsh -NoProfile -File .cache/shuttle-manual-qa/accept.ps1 -Image shuttle:review-qa-final
```

Preparation assumes the pinned Go/PostgreSQL images and verified module cache are present locally. For a fresh machine, fetch the exact pinned modules with normal Go checksum verification before the offline helper build. Names and ports must be free. Remove only these identified containers before repeating. The acceptance intentionally terminates the bot and one dedicated lock connection, and temporarily revokes a permission in its own database.

## Not implemented or not accepted

- NOT IMPLEMENTED: file ingestion, durable attachment IDs and authorized file-read API; PDF/audio/video tools, avatar handling and Telegram source-loss behavior. Unsupported-media error handling passed; that does not accept file support.
- NOT IMPLEMENTED: per-user language selection, persistent Loom chat binding and context across completed runs, registrations, passes/pairs, partial payments, ambassador confirmation, event time zones, module availability, old data/history migration, massage and food/order workflows.
- NOT IMPLEMENTED: administrative agent manifests beyond account.me, queue-age/database-pool/token-cache dashboards and alerts, operator reconciliation API for uncertain delivery/authentication. Current HTTP admin settings/revocation APIs and durable uncertainty states were checked.
- NOT ACCEPTED BY THIS MOCK LANE: real Zitadel grants. The [separate real IAM report](shuttle-real-iam-qa-2026-10-12.md) accepts the disposable grant/claim/role contract; production IAM policy and bidirectional zitadeltg linking remain gates. Real Loom behavior and its IAM-outage cache, real Telegram rendering/delivery, server deployment, real collector/dashboard integration, production load and migration/cutover remain NOT ACCEPTED.
- NOT ACCEPTED: the five-second watchdog under every possible hung-process/OS condition. Recorded shutdown and SIGKILL cases establish only their observed process paths.

The source and target scope are described in shuttle-architecture.md, shuttle-run.md and shuttle-spec.md. Successful local mock acceptance does not close real-service contract gates.

Written by Codex (gpt-6/Codex desktop)
on behalf of Daniel Drizhuk
