# Isolated Telegram QA stands

Each named stand runs real Zinusya, PostgreSQL, Zitadel and Loom. Telegram is a
local simulator; Loom uses its existing synthetic counted mailbox provider.
A separate networkless file worker serves real Loom file tools. There is no
poller, real Telegram token, connection to Telegram, or reuse of the live stand's
IAM, database, mailbox, CA, network or volumes. Production needs none of these
files or extra configuration fields.

This is an agent-operated fixture, not a full Telegram client. The simulator
supports multiple users and private/group/supergroup chat IDs. Whether Zinusya
accepts a group operation remains application policy; a mock group is not proof
of product support for groups.

## Create and start

Requires Docker, Python 3.12+ and reviewed local Linux bot/Loom/file-worker images. The source
uses only Python's standard library. Pass the Loom example configuration from
the same reviewed revision as its image. This command does not build, pull or
modify a Loom checkout. Images are resolved to immutable local IDs at creation.

From the repository root (use your local Python executable):

```powershell
python deploy/shuttle/mock_tg/stand.py create --name qa-a --base-port 20500 --bot-image zinusya-massage-staff-bot:v1-20261018 --loom-image shuttle-test-loom:local --file-worker-image sha256:0a80b34f827cc7db5900f5c6662f62fa83dc8c8073dfd69e3f5fb631b31beb83 --loom-template /path/to/reviewed/loom/deploy/config.example.yaml
python deploy/shuttle/mock_tg/stand.py create --name qa-b --base-port 20600 --bot-image zinusya-massage-staff-bot:v1-20261018 --loom-image shuttle-test-loom:local --file-worker-image sha256:0a80b34f827cc7db5900f5c6662f62fa83dc8c8073dfd69e3f5fb631b31beb83 --loom-template /path/to/reviewed/loom/deploy/config.example.yaml
python deploy/shuttle/mock_tg/stand.py start --name qa-a
python deploy/shuttle/mock_tg/stand.py start --name qa-b
```

Choose a new name for each independent QA owner. Names are lowercase letters,
digits and hyphens, start with a letter and have at most 24 characters. Base ports
are multiples of 10 in 20000..60000. Six loopback ports are reserved: bot HTTP,
internal API, mock control, IAM HTTPS, Loom HTTP and Mini App HTTPS. The command
prints endpoints, not credentials. All services have explicit memory/CPU/PID
limits; no restart policy can silently grow the number of containers.

Generated files, synthetic credentials and evidence live under
`.cache/shuttle-mock/<name>/`. Do not copy `config/shuttle.test.env` into a mock
stand. Use only synthetic users/files. Keep one lifecycle controller per named
stand; parallel QA agents must use different names.

Startup records provisioning and migration stages before uncertain writes. If an
operation times out, observe its existing process/container first. Do not replay
identity creation or migration merely because the observer timed out. Inspect the
ignored manifest/private diagnostic; an owned disposable stand can be destroyed
instead of trying to reuse uncertain fixture state.

## Messages, users, chats and files

```powershell
python deploy/shuttle/mock_tg/qa.py --name qa-a send --user 101 --first-name Alice --language en --text /menu
python deploy/shuttle/mock_tg/qa.py --name qa-a send --user 202 --first-name Bob --language ru --text /menu
python deploy/shuttle/mock_tg/qa.py --name qa-a send --user 101 --chat -100123 --chat-type supergroup --text hello
python deploy/shuttle/mock_tg/qa.py --name qa-a transcript --after 0
python deploy/shuttle/mock_tg/qa.py --name qa-a upload --file /path/to/synthetic.pdf --mime-type application/pdf
```

`transcript` returns an ordered journal, current message snapshots, attempts and
webhook outcomes. Attempts and accepted effects are separate. Follow `cursor`
while `truncated` is true. Snapshot lists also report truncation; use the complete
journal for long sessions. SQLite state and one-shot fault consumption survive a
mock restart. An edit updates the visible message but retains previous content
in the journal. No Telegram exactly-once guarantee is invented.

Text `sendMessage` responses and stored snapshots preserve explicit `entities`,
including UTF-16 offsets and nested `text_mention.user` values. `editMessageText`
replaces them; omitted or empty entities clear the prior text formatting. The
original attempts and pre-edit journal remain intact. The fixture forwards
explicit entities; Markdown/HTML `parse_mode` parsing remains outside its scope.
See the [entity HTTP acceptance report](../../../docs/shuttle-mock-telegram-entities-qa-2026-10-19.md).


Structured operations take a JSON file (`--json /path/to/input.json`):

- `click`: `{"user":{"id":101,"first_name":"Alice"},"chat_id":101,"message_id":2,"row":0,"column":0}`. Indices are zero-based. The actual stored keyboard determines a callback, ordinary reply or Mini App URL.
- `media`: `{"user":{"id":101,"first_name":"Alice"},"kind":"document","file_id":"<returned fixture ID>"}`. Optional `chat`, `caption` and explicit `update_id` are supported.
- `update`: `{"update":{...}}` forwards the exact update. Replay the same stored object/ID to test bot deduplication; the mock records every delivery attempt.
- `fault`: `{"method":"sendMessage","kind":"reject_429","retry_after":2}`. Other faults: `reject_entities`, `reject_500`, `accepted_then_disconnect`, `file_gone`, `file_truncated`. File-download faults use method `file_download`.
- `init-data`: `{"user":{"id":101,"first_name":"Alice","language_code":"en"}}` creates an HMAC proof using only the fake bot token; optional `auth_date` permits expiry tests.

For a deterministic process-restart check, arm
`{"method":"sendMessage","kind":"accepted_then_hold","hold_seconds":60}`.
The simulator commits the message and attempt, then withholds the HTTP response
without holding its database lock. `transcript` exposes `holds` keyed by
`attempt_id`; messages and control requests remain usable. Release the exact
attempt with `qa.py --name qa-a release --json /path/to/release.json`, containing
`{"attempt_id":123}`. Releasing twice produces no second effect or journal entry.

The hold expires after1..60 seconds (default60), then disconnects without an
acknowledgment; the accepted message remains durable. At most16 unreleased holds
can be registered by accepted sends; release expired or abandoned holds before
continuing. Hold/release records survive simulator restart, but an unreleased
record alone does not prove an active socket. To prove bot recovery, observe its
actual `send_inflight` state and the accepted mock effect before stopping that
same bot process. This fault does not itself prove recovery or prevent the bot
from issuing a duplicate. Existing `accepted_then_disconnect` behavior is unchanged.

File fixtures are bounded to 4 MiB each; request and proxy response bodies to
8 MiB. The journal is bounded to 10,000 entries and fails visibly at capacity.
Use a fresh isolated stand for another long session rather than hiding old
receipts. HTTP control endpoints require the generated QA bearer. `qa.py` reads
it privately; do not put it in commands, screenshots or reports.

## Provider lifecycle

New stands give the synthetic provider its own project-private network namespace.
Loom uses `https://provider.zinusya.localhost:18089/v1`; the provider publishes no
host port. Restarting Loom does not require stopping or restarting the provider. The endpoint uses TLS with the stand's own CA
and a project-private DNS alias.
The mock-only entrypoint handles SIGTERM with a bounded process exit. An unanswered request remains in the
mailbox without a fabricated response; this is not a request-drain guarantee.
The shared counted-provider source and primary stand are unchanged.

Manifests record this layout. `start` refuses older manifests before changing any
container, because their saved Loom configuration uses the former loopback route.
Preserve their evidence and create a fresh named stand. `status`, `stop` and
`destroy` remain available for legacy projects; no automatic topology conversion
or credential replacement occurs.

The [lifecycle acceptance](../../../docs/shuttle-mock-provider-lifecycle-qa-2026-10-18.md)
records verified TLS, two real IAM/Loom task deliveries across a Loom restart,
clean provider termination, failure history and complete temporary cleanup.

## Agent and mixed paths

```powershell
python deploy/shuttle/mock_tg/qa.py --name qa-a send --user 101 --text "Show my profile"
python deploy/shuttle/mock_tg/qa.py --name qa-a pending-model
python deploy/shuttle/mock_tg/qa.py --name qa-a pending-model --request <request-id>
python deploy/shuttle/mock_tg/qa.py --name qa-a answer-model --request <same-id> --json /path/to/response.json
```

Read the actual model request, instructions, input and offered tool schemas.
Respond through that exact pending request. Do not substitute a new run when one
waits. Answers publish atomically and cannot replace an existing answer. A
response (at most 2 MiB) uses the existing Loom shape, for example:

```json
{"ID":"qa-response-1","Text":"Hello","Calls":[],"Usage":{"InputTokens":1,"OutputTokens":1,"CachedInputTokens":0,"CacheWriteTokens":0}}
```

For tool work use `Calls` entries with `ID`, an offered tool `Name` and object
`Arguments`; read the next pending request for the real tool result. The fixture
validates names and basic synthetic usage; Loom validates the real contract.
Synthetic counters do not prove model quality, billing, inference latency or
vision. Record native/agent/mixed acceptance separately; a canned final response
alone does not prove a business operation.

The Mini App is served through a fixed mock-only HTTPS proxy to the bot. Use the
manifest's `miniapp` URL plus `/app/`, and the stand's own public CA certificate
when making HTTPS QA requests. No automatic browser trust installation occurs.
The proxy preserves Authorization, Origin, cookies, errors and request bodies. Use `init-data`
for normal application session bootstrap; it is a synthetic proof, not evidence
of Telegram WebView authentication or rendering.

Production Mini App configuration needs only `mini_app_origin`; the launch link
defaults to `<origin>/app/`. For **real Telegram testing through a local redirect**,
set `mini_app_launch_url` only in that test configuration; see
[the existing local-launch recipe](../../../docs/shuttle-miniapp-local-launch-qa-2026-10-16.md).
No override is needed on production.

## Native browser QA

`browser.cjs` exports `openMiniApp(browser, {origin, initData, locale})`. Use it
inside an isolated Playwright QA driver. `origin` is the manifest's Mini App HTTPS
origin on localhost:20000..60005; `initData` comes from a fresh `qa.py init-data`
response for that synthetic user. Keep signed proofs in ignored private files.
Do not reuse one consumed proof with a new context/nonce; reloads in the same
context preserve their existing sessionStorage.

```javascript
const { openMiniApp } = require("./deploy/shuttle/mock_tg/browser.cjs");
// In your async QA driver, with a fresh Playwright browser owned by this test:
const { context, page } = await openMiniApp(browser, { origin, initData });
await page.goto(origin + "/app/");
// Exercise the actual form, then close the context and owned browser in finally.
```

Each call creates a separate browser context and session storage. It replaces
only the exact Telegram SDK request with a synthetic WebApp object and blocks
other page traffic outside the stand origin. Product HTML/JS/CSS and API responses
are not substituted. Service workers are blocked. Fixture TLS acceptance applies
only to that disposable context; no CA is installed in the user's browser.
Use only fresh automation browser profiles, never attach an existing user profile.

Playwright and a supported browser are QA-host dependencies supplied by the
caller. This helper adds no bot/server image, Go dependency or production setting.
The [cross-person acceptance](../../../docs/shuttle-cross-person-browser-qa-2026-10-18.md)
uses real IAM/Loom/backend, separate payer/reviewer contexts, original proof bytes
and a lost successful confirmation response followed by reload recovery. It does
not replace Telegram WebView, user-browser CA or model-interpretation acceptance.

## Evidence and cleanup

```powershell
python deploy/shuttle/mock_tg/qa.py --name qa-a evidence --output /path/to/ignored/evidence.json
python deploy/shuttle/mock_tg/stand.py status --name qa-a
python deploy/shuttle/mock_tg/stand.py stop --name qa-a
python deploy/shuttle/mock_tg/stand.py destroy --name qa-a
```

Stop releases runtime memory while retaining state. Destroy removes only the
exact named project's containers, network and volumes after ownership checks;
it keeps ignored evidence, private credentials and mailbox files for diagnosis.
Destroy is not a cleanup command for `shuttle-live-test` or other developers'
projects. The supplied name cannot select those projects. Retained files are
private and should be removed deliberately when their evidence is no longer
needed.

A final release still needs a small real-Telegram gate for transport, Mini App
launch/verified Telegram data and actual client rendering. This fixture does not
replace that gate or prove production readiness.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
