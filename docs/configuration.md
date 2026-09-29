# Typed process configuration

`platform/internal/config.Load(command, yamlData, environ)` returns a validated
`Config`. The caller supplies the command, optional YAML bytes and an environment
snapshot, normally `os.Environ()`. Loading does not read files, start services,
contact collectors or inspect executables. The command adapter owns optional
file selection and reading; `ZNS_CONFIG_FILE` is reserved for that adapter and
is ignored by the loader. The loader returns no partially valid configuration
on failure.

Precedence is **defaults → YAML → legacy environment aliases → canonical ZNS_
environment variables**. Canonical variables always win their alias, regardless
of environment enumeration order. Nested YAML keys use `__`, for example
`sticker.cache.half_life` becomes `ZNS_STICKER__CACHE__HALF_LIFE`. Names are
case-sensitive. Unknown `ZNS_` names and duplicate configuration environment
entries are errors. Unrelated unprefixed process variables are ignored.
Canonical empty values override YAML; empty legacy aliases remain absent, as in
the existing command helpers. Booleans must be exactly `true` or `false`.

The YAML input is limited to 1 MiB and one document. It accepts only the named
struct fields, plain mappings and correctly typed scalar values. Duplicate keys,
unknown fields, nulls, sequences, custom tags, anchors, aliases and merge keys
are rejected. Durations use strings such as `15s` or `48h`; an unadorned integer
is not a duration. The parser's YAML 1.1 boolean coercions are not enabled by this
configuration contract. Invalid parser/value errors omit raw input values.

Use the [secret-free example](../platform/internal/config/example.yaml) with
`bot` mode. Supply the database connection and signing key through canonical
environment variables; the example contains no credentials. Native worker
settings are optional: configure both URL and secret, or neither. Worker URLs
must be HTTP(S) origins without credentials, query strings or paths.

The loader's `Secret` type exposes values only through `.Value()` at consuming
boundaries. `fmt`, `slog`, JSON and YAML formatting redact the database connection
string, signing key, Telegram token, OpenAI key and worker secrets. This does not
replace application-wide redaction: once a consumer calls `.Value()`, it owns
safe handling of that plain string. Do not dump entire process environments.

## Existing runtime migration map

The current `cmd/zns` options were inspected before creating this schema. Existing
aliases remain accepted. The package does not replace command/runtime wiring by
itself.

| Existing environment variable | YAML field | Canonical variable |
| --- | --- | --- |
| `ZNS_ENV` | `env` | `ZNS_ENV` |
| `ZNS_PARENT_STDIN` | `parent_stdin` | `ZNS_PARENT_STDIN` |
| `DATABASE_URL` | `database.url` | `ZNS_DATABASE__URL` |
| `SANDBOX_SIGNING_KEY` | `auth.signing_key` | `ZNS_AUTH__SIGNING_KEY` |
| `BIND_HOST` | `server.host` | `ZNS_SERVER__HOST` |
| `PORT` | `server.port` | `ZNS_SERVER__PORT` |
| `CORE_URL` | `core.url` | `ZNS_CORE__URL` |
| `TELEGRAM_BASE_URL` | `telegram.base_url` | `ZNS_TELEGRAM__BASE_URL` |
| `TELEGRAM_TOKEN` | `telegram.token` | `ZNS_TELEGRAM__TOKEN` |
| `WEBAPP_URL` | `telegram.web_app_url` | `ZNS_TELEGRAM__WEB_APP_URL` |
| `MINIAPP_URL` | `sandbox.mini_app_url` | `ZNS_SANDBOX__MINI_APP_URL` |
| `MODEL_PROVIDER` | `model.provider` | `ZNS_MODEL__PROVIDER` |
| `MODEL_URL` | `model.url` | `ZNS_MODEL__URL` |
| `OPENAI_API_KEY` | `model.openai_key` | `ZNS_MODEL__OPENAI_KEY` |
| `CODEX_EXECUTABLE` | `model.codex_executable` | `ZNS_MODEL__CODEX_EXECUTABLE` |
| `SYNTHETIC_ONLY` | `synthetic_only` | `ZNS_SYNTHETIC_ONLY` |
| `MEDIA_WORKER_URL` | `media.url` | `ZNS_MEDIA__URL` |
| `MEDIA_WORKER_SECRET` | `media.secret` | `ZNS_MEDIA__SECRET` |
| `STICKER_WORKER_URL` | `sticker.worker.url` | `ZNS_STICKER__WORKER__URL` |
| `STICKER_WORKER_SECRET` | `sticker.worker.secret` | `ZNS_STICKER__WORKER__SECRET` |
| `STICKER_CACHE_CAPACITY` | `sticker.cache.capacity` | `ZNS_STICKER__CACHE__CAPACITY` |
| `STICKER_CACHE_HALF_LIFE` | `sticker.cache.half_life` | `ZNS_STICKER__CACHE__HALF_LIFE` |
| `ORDER_REMINDER_AFTER` | `orders.reminder_after` | `ZNS_ORDERS__REMINDER_AFTER` |

Defaults preserve port 8080, an empty bind host, read-header/read/write/idle
timeouts of 5/15/45/30 seconds, health timeout 3 seconds, sandbox Telegram token,
the existing web-app URLs, a 5000-entry sticker cache with seven-day half-life,
and 48-hour order reminders. No environment default silently opts a process into
sandbox: `env: sandbox` or `ZNS_ENV=sandbox` is required except for `health`.
The database string must be nonempty where required; its PostgreSQL DSN syntax
and connectivity remain the database driver's startup checks.

`bot` defaults to `remote`; `model` defaults to `openai`, preserving their
different historical defaults. Bot mode requires a Core URL and signing key;
API mode requires the signing key. Model mode requires no database and requires
an OpenAI key only for the OpenAI provider. Codex retains the synthetic-only,
`127.0.0.1` bind and absolute-executable-path conditions; the runtime must still
check that the executable exists. Fixture/migrate modes require only the database
and sandbox environment among service credentials. Health mode needs neither.

The planned single-process `app` mode defaults to OpenAI and also accepts
`scripted`, `codex` and `remote`. It requires the database and signing key, but
does not require an external Core URL. This is configuration support; it does
not claim the single-process launcher is implemented.

Fixture payloads remain in the existing dedicated parsers, which must still run
after loading process configuration. Their unprefixed variables are deliberately
not consumed by this package:

- Order fixture: `ORDER_FIXTURE_ID`, `ORDER_FIXTURE_ADMIN_COUNTRY`,
  `ORDER_FIXTURE_AGE`, `ORDER_FIXTURE_DEADLINE`, `ORDER_FIXTURE_EXTRA`,
  `ORDER_FIXTURE_REMAINING` (integer or `default`).
- Export fixture: `EXPORT_FIXTURE_ORDER_ID`, `EXPORT_FIXTURE_BATCH_TAG`,
  `EXPORT_FIXTURE_CHOICE` (bounded JSON), `EXPORT_FIXTURE_ADMIN_ENABLED`,
  `EXPORT_FIXTURE_BATCH_COUNT`.

## Telemetry and shutdown fields

`otel.enabled` defaults to false; `otel.endpoint` is an optional absolute
OTLP/HTTP URL, required when enabled, with no embedded credentials or query.
`otel.sample_ratio` defaults to 0.1 and must be finite and within `[0,1]`.
Canonical names are `ZNS_OTEL__ENABLED`, `ZNS_OTEL__ENDPOINT` and
`ZNS_OTEL__SAMPLE_RATIO`. Disabled export does not require a collector or key.
The loader performs no reachability checks, including when export is enabled.

`shutdown.drain` defaults to 5 seconds, preserving the existing HTTP shutdown
deadline. `shutdown.telemetry_flush` defaults to a separate 5 seconds. Both must
be positive. Their canonical names are `ZNS_SHUTDOWN__DRAIN` and
`ZNS_SHUTDOWN__TELEMETRY_FLUSH`. Runtime intake draining, helper termination and
telemetry flushing still need wiring and independent acceptance; parsing these
fields does not implement that behavior.

## YAML dependency assessment

Assessment date: 2026-09-26. Reuse `go.yaml.in/yaml/v3 v3.0.5`, already pinned as an
indirect dependency. The integration change should make it a direct requirement
without changing its version. The loader adds no configuration framework or
environment dependency.

The pinned package's [license](https://github.com/yaml/go-yaml/blob/v3.0.5/LICENSE)
assigns MIT to the libyaml-derived files and Apache-2.0 to the remaining files;
both are in the user's permitted set. Its
[pinned README](https://github.com/yaml/go-yaml/blob/v3.0.5/README.md) identifies
the YAML organization as the maintainer after the original go-yaml project was
marked unmaintained. The
[v3.0.5 release](https://github.com/yaml/go-yaml/releases/tag/v3.0.5) is a recent
published patch release. These establish a maintained upstream and tagged
provenance; they are not a support guarantee.

Real downstream adoption is visible in the projects' own manifests:
[Helm](https://github.com/helm/helm/blob/main/go.mod) directly requires v3.0.5,
and [Kubernetes](https://github.com/kubernetes/kubernetes/blob/master/go.mod)
lists it indirectly. These are dated observations of moving branches, not
claims about every released version of those products.

The [upstream security page](https://github.com/yaml/go-yaml/security) currently
shows no published advisories and no SECURITY.md policy. This is a limitation,
not proof of absence of vulnerabilities. Configuration is operator-supplied,
bounded before parsing, and excludes aliases and complex/custom values. The
strict node/type check compensates for documented YAML 1.1 coercions; the
underlying library remains responsible for parsing. Run the repository's pinned
`govulncheck` gate at integration/release, and re-evaluate upstream advisories
before upgrades. No vulnerability scan result is claimed by this assessment.

Focused tests cover precedence, legacy migration, required settings per command,
YAML ambiguity, invalid environment types, Codex isolation conditions, worker
pairing, telemetry bounds and secret-safe errors/formatting. The unit run passed;
startup health mode has also been exercised against a running synthetic stand.
The launcher reads `ZNS_CONFIG_FILE` with a 1 MiB bound and passes one validated
configuration through runtime modes. Independent Code QA of that integration
reported no actionable findings. Full Functional QA remains required.

JS scripting is opt-in: `script.enabled` (`ZNS_SCRIPT__ENABLED`, default false)
and `script.socket` (`ZNS_SCRIPT__SOCKET`, an absolute Unix socket path when
enabled). Both app and bot use the isolated worker client; the model process
receives no socket or application identity. See [agent-scripting.md](agent-scripting.md).

Conversation context: history.recent (ZNS_HISTORY__RECENT), default 12, accepts 1–30. See [conversation history](conversation-history.md).

### Active orders and massage event

`orders.active_event` (`ZNS_ORDERS__ACTIVE_EVENT`) selects the event for new bot
orders, exports, receipt candidates, massage menus, and MiniApp requests without
an existing order. This corresponds to Python `orders.event_key`. Only sandbox
configuration supplies the `sandbox-festival` default; set an imported event ID
explicitly when testing an imported dataset. The production startup gate remains
closed until production identity is accepted.

Changing the value and restarting selects the new event for new requests. Old
opaque callbacks, pending receipts, cached commands, and update retries keep their
original event. Existing MiniApp order links resolve the order's immutable event
and recheck the signed-in owner's Core permission. Changing the current event does
not change or migrate historical orders. Migration 050 preserves preconfiguration
paging state under `sandbox-festival` and adds independent per-event pages. To
roll back selection, restore the previous config value and restart; keep the
additive migration in place. No event or order data is deleted.
