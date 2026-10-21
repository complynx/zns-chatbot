# Local real-service test stand

This stand runs actual Shuttle, Loom and Zitadel in a separate Docker Compose
project, `shuttle-live-test`. It uses dedicated PostgreSQL databases and volumes.
It does not change production IAM or sibling QA projects. Requires Docker Desktop,
Python 3.14 and the sibling `../clx-scripter` checkout. The wrapper builds the
current source, including local changes, rather than downloading an old Loom image.

## Start

Put the token for `@complynx_test_bot` in the ignored file
`config/shuttle.test.env`:

```dotenv
SHUTTLE_TELEGRAM_TOKEN=your-test-bot-token
TEST_TELEGRAM_USER_ID=your-authorized-account-numeric-id
```

The account ID is the human Telegram user ID, not the bot ID or the number in the
bot chat URL. The test poller accepts only private updates from that account.
Other updates are ignored and acknowledged by advancing the poll offset.

The bot uses the stand-only telegram-guard service for outbound Bot API calls.
Its HTTPS address is telegram.zinusya.localhost:8080, a Docker network alias covered
by the stand's existing wildcard leaf certificate. The bot verifies it with the
existing CA bundle; no insecure HTTP exception or browser trust change is required.
The guard runs as UID/GID 1000, matching the private leaf key's owner in this stand.
It does not change key permissions or add capabilities to read it.
It permits sendMessage only to that account and rejects other send methods before
contacting Telegram. Metadata/file reads and callback acknowledgements remain
available. Callback IDs originate from the account-filtered ingress. The gateway
checks the configured bot token path, does not follow upstream redirects, suppresses
credential-bearing URL logs and bounds request/response sizes. It streams file
responses; an oversized unknown-length body ends as an incomplete chunked response,
so clients cannot mistake truncated bytes for a successful download. It has a 96 MiB
memory limit, no swap and no host port. This is test infrastructure, not production
authorization or protection against a deliberately reconfigured bot bypassing it.

Missing account configuration stops the guard and poller. The bot depends on a
healthy guard. Keep TEST_TELEGRAM_USER_ID in the ignored test env file alongside the
token. Counterpart fixture identities may use local IAM/API, but their Telegram IDs
must not receive real sends. Add future Bot API write methods to this allowlist only
with equivalent recipient checks and tests.

Then run from the repository root:

```powershell
./deploy/shuttle/test/run.ps1 Start
./deploy/shuttle/test/run.ps1 Status
./deploy/shuttle/test/run.ps1 Smoke
```

Start generates stand-only credentials/config in `.cache/shuttle-live-test`,
starts actual Zitadel, provisions separate Loom/Shuttle projects and introspection
clients, and enables impersonation only in that disposable instance. The actor
has organization owner/impersonator privileges for user provisioning. These broad
fixture privileges are not a production policy template. API user roles are not
assigned automatically. Do not copy fixture secrets to production.

Zitadel terminates TLS itself. The stand has one five-year local root CA and
90-day server leaves. Import only `.cache/shuttle-live-test/root-ca.cer` once
into the browser trust store. Never import or share `root-ca.key`. No trust store
is changed automatically, and browser certificate warnings must not be bypassed.

The server leaf covers `host.docker.internal`, `localhost`, `127.0.0.1`,
`zinusya.localhost` and `*.zinusya.localhost`. Future one-level subdomains, such as
`auth.zinusya.localhost` and `bot.zinusya.localhost`, need no CA reinstallation.
Wildcard certificates do not cover deeper names. Routing/DNS and service ingress
must still be configured; certificate trust alone creates no endpoint.

Use `python deploy/shuttle/test/tls.py --replace-leaf` for explicit renewal, then
recreate the owned services that mount the leaf or trust bundle. The issuer URL
and IAM/database/session keys remain unchanged. An existing root is never rotated
automatically. Renewal preserves the previous leaf/key for local rollback; the
old leaf temporarily remains in the container/probe trust bundle. A new root
requires a fresh browser import. Preserve CA, issuer, IAM and database state.

Start is initial bootstrap only. It refuses a stand containing `identity.json`,
`loom.yaml` or `shuttle.yaml` before building images or changing services/config.
Direct `prepare.py` calls enforce the same guard. CA-only preparation is allowed.
For an initialized stand, preserve runtime settings and use its complete Compose
model, including any browser/canonical-issuer overlays, to restart services.
See [browser setup](browser.md) for that overlay; base-only bootstrap must not
recreate IAM after this cutover. A failed partial initial provisioning may require
operator repair; it does not blindly replay unknown creation requests.
Never delete state/volumes as a routine retry.
Runtime files and the bot token must remain ignored/private. Do not publish
`docker compose config` output: it expands secrets.

## Interfaces

- Zitadel: `https://host.docker.internal:18443`, native TLS.
- Loom: `http://127.0.0.1:18381`; API paths start with `/v1`.
- Shuttle ingress/health: `http://127.0.0.1:18382`.
- Shuttle private API/metrics: `http://127.0.0.1:19382`.
- Loom metrics: `http://127.0.0.1:19381`.
- Test PostgreSQL: `127.0.0.1:55476`.

All published ports bind loopback. Containers use Docker DNS; Python control
scripts map only `host.docker.internal` to local loopback while still validating
its certificate hostname. Shuttle's Loom base URL is `http://loom:8080`, without
`/v1`: the client adds the version itself. Docker discovery registers Shuttle's
actual manifest through its read-only Docker API proxy.

The Telegram poller is test infrastructure, not a new product ingress mode. It
checks the expected bot ID (6087685431) and refuses an existing webhook before
the wrapper starts Shuttle. It checks again when polling starts, and forwards
real `getUpdates` responses to Shuttle's authenticated local webhook. It advances
its persisted offset only after HTTP success. Failed acceptance is retried;
crash between acceptance and offset persistence is covered by Shuttle's update
idempotency. Group updates and callbacks without a valid private message are
explicitly ignored and their offset is persisted, so they cannot block the queue.
Do not run another poller for this bot. It never calls deleteWebhook
or drops pending updates. Telegram acknowledgements do not wait for inference.

## Model provider

The default is Loom's opt-in manual mailbox provider. The IAM, delegation,
registry, scheduler, database and agent loop remain real. Files in
`.cache/shuttle-live-test/mailbox` contain model requests with instructions,
conversation input and tool schemas. Treat them as private conversation data.

During an active Codex-assisted QA session, Codex can read the pending request
and atomically write the matching `.response.json`. This is manual inference;
it does not create an unattended Codex worker. Answer within Loom's two-minute
inference deadline. Calls must use the supplied tool names/schemas, and responses
must contain Usage. Mock usage is scenario data, not measured Codex usage or
OpenAI billing. The smoke script uses a deterministic text response and explicit
scenario usage; it does not claim a live model invocation.

Loom also has a `kind: codex` subprocess transport, described in the sibling
`docs/test-providers.md`. It requires an operator-authenticated dedicated Codex
profile and executable in its runtime. This stand does not copy personal Codex
credentials or install that transport automatically. An unattended model
backend is a separate explicit setup choice.

## Optional file processing

Shuttle keeps durable opaque Telegram references. Loom imports files on demand,
then processes temporary artifacts in its isolated media worker. Enable this
lane only on this owned stand; no provider credential, Telegram token, Docker
socket or artifact-store mount goes to the worker. Paid transcription remains
disabled.

Build the `file-worker` target from the same verified Loom source snapshot as
this stand's API. Record the resulting immutable local image ID in the session:

```powershell
$env:TEST_FILE_WORKER_IMAGE = docker image inspect zinusya-test-loom:files-fixture --format '{{.Id}}'
```

Use the existing Loom worker Dockerfile and its source/checksum/license gates.
The media image contains GPL components; process isolation is not a license
exemption. Do not publish it as LGPL-only. See Loom's `docs/file-worker.md`.
Add the following top-level section to the ignored `loom.yaml` configuration,
without replacing existing IAM/provider settings. Do this only when no section
exists; otherwise update its existing keys:

```yaml
file_tools:
  enabled: true
  socket: /run/loom-files/processor.sock
  timeout_ms: 30000
```

Add `-f deploy/shuttle/test/files.compose.yaml --profile files` to this stand's
Compose command, preserving any existing owned fixture/resource overlays. Start
`file-worker` first. Then recreate `loom` with `--no-deps` to mount its private
socket. A newly created socket volume inherits UID/GID 10001 from the worker;
check ownership before reusing a volume. The overlay sets memory/no-swap, CPU,
PIDs, temp storage and five-second shutdown limits. It grants no network access.

QA must separately prove real Telegram upload/import, bounded PDF text extraction,
PDF-to-image, owner access and source-reference reuse after bot/worker restart.
A successful import alone does not prove processing or receipt interpretation.
Stop/remove only this project's unused worker after saving evidence. Include this
overlay and the files profile when checking or stopping the optional lane; the
base `run.ps1 Stop` command does not select it.

## Verification and stop

The smoke uses real TLS discovery, a roleless test human, a real delegated token,
the real IAM user API, a Loom auth session and duplicate run creation. It waits
for a provider request, atomically supplies a fixture response and verifies the
final result. It prints no tokens. Smoke data is retained in test databases.

The real Telegram lane must be checked separately in Telegram Web. Current
acceptance/evidence is recorded in `docs/shuttle-live-test-2026-10-13.md`.
The original restart failure is retained in that report. Its authorization recovery
fix passed repeat real-service acceptance: a temporary discovery-related Loom 401
now waits with a durable deadline and resumes the same run after recovery.
This specific pass does not establish full production recovery acceptance.
Role-gated administrative agent reads were checked with a separate fixture
administrator through the real Loom broker; see `docs/shuttle-agent-administration.md`.
Assign `zinusya.admin` in both IAM projects for such a fixture. The operator's
Telegram account is not elevated automatically. The stand does not add missing
business flows, file ingestion or zitadeltg linking.

```powershell
./deploy/shuttle/test/run.ps1 Stop
```

Stop affects only this Compose project, keeps volumes/credentials/history, and
allows at most five seconds before killing its processes. No reset/down-with-
volumes command is supplied. Restart individual owned services with the same
Compose file to test outages; do not stop shared production services.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk

## Disposable approval display fixture

`approval_fixture.py` supports manual acceptance of full-parameter Telegram
approvals without elevating the operator's account. It advertises one synthetic
`zinusya_qa_approval.display` method, no Caller identity or permissions, and always
refuses execution. It only prepares the exact expected arguments and records
operation IDs, an argument digest and execute-attempt count. It does not log
credentials. Loom authorizes the user; the fixture itself is not a business API.

The CLI requires `--expected /expected.json --state /state/state.json`. Expected
JSON has exactly `case_id` and `text` string fields; use only synthetic data.
The state path must be fresh: an existing journal stops startup so previous QA
evidence is not overwritten. A repeated preparation in the same process retains
its original arguments and expiry. Fixture restart recovery is not implemented.

Run it only in a disposable container on `shuttle-live-test_default`, with
read-only root/script/expected data, a separate writable journal, nonroot user,
64 MiB memory/no swap, bounded CPU/PIDs and no published host port. Discovery
uses a network alias matching the `clx.harness.service` label URL, for example
`http://zinusya-approval-fixture:8080/manifest`. No IAM grant or Loom configuration
edit is needed. Inspect exact container ownership before stopping/removing it;
retain the journal and never repeat an uncertain Telegram send or provider reply.
The fixture requires a bounded stop; no graceful-stop guarantee is claimed.

The actual two-chunk display and Telegram rejection check is recorded in
[the dated QA report](../../../docs/shuttle-approval-display-qa-2026-10-15.md).
It uses real local IAM/Loom/Zinusya/Telegram with this mock RPC leaf, not a real
administrative business write. The fixture is not part of the production image
or an automatically enabled module.

## Synthetic counted mailbox fixture (not enabled by default)

`counted_provider.py` is an optional test-only Responses HTTP adapter for the
existing manual request/response mailbox. The original mailbox provider has no
input-token counter; [the mixed-flow checkpoint](../../../docs/shuttle-order-mixed-context-qa-2026-10-17.md)
records context pauses without labeling them a proven upstream defect.

The adapter exposes `/v1/responses` and `/v1/responses/input_tokens`. Counts are
synthetic, defined as four canonical UTF-8 JSON bytes per unit, rounded up. They
are not real model tokenization. Manual replies must supply all four usage fields;
exceeded output bounds return incomplete. Native `/compact` is explicitly unsupported.
Do not use it for production, model quality, real billing, real context-limit,
vision, or native-compaction acceptance. It makes no upstream provider requests.

Startup requires `ZINUSYA_SYNTHETIC_COUNTED_QA=1`, a private
`ZINUSYA_COUNTED_QA_KEY`, and `--mailbox` pointing to an existing private directory.
There is no automatic Compose wiring. The owned stand has a reviewed manual HTTPS
activation documented in the mixed-flow checkpoint above. Before another
activation, preserve the actual Loom config and database/run state, review the
config-only change, and retain a rollback path. Use a dedicated nonroot container
on `shuttle-live-test_default`, no host ports, read-only root/script, only its
private mailbox writable, 128 MiB/no swap, bounded CPU/PIDs, and exact ownership
labels. For HTTPS, use a verified hostname and trusted CA. Mount a dedicated secret
volume read-only with files owned by the fixture UID; preserve original key
permissions. Keep the Telegram account guard unchanged. Remove only the exact
owned fixture and its secret volume when no longer needed. Socket inactivity timeouts, an absolute request-body deadline, bounded body sizes,
four active slots and a manual-response deadline limit waits. A body timeout can
close/reset TCP before an HTTP error reaches a still-transmitting client. No
application restart SLA or absolute header-transfer deadline is claimed.

Run the behavioral HTTP checks without Docker or extra Python dependencies:

```powershell
python -m unittest discover -s deploy/shuttle/test -p test_counted_provider.py -v
```

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
