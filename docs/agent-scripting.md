# Agent scripting

`cmd/scriptworker` is a one-request JavaScript helper. The synchronous data-only
`Evaluate` API described below remains compatible. The optional Execute API adds
host-authorized tool calls through Sobek and the bounded RPC transport described
in the next section. No second JavaScript engine or code-mode framework is used.

For Evaluate, the host sends one JSON
request on stdin, closes stdin, reads one JSON response from stdout, and waits
for exit. It has no HTTP listener. It receives no API keys, database credentials,
Telegram tokens, application objects or caller identity. The application uses
the Unix client and isolated service described in [script-worker.md](script-worker.md).
The model tool is opt-in; independent acceptance remains a separate gate.

```json
{"code":"return {total: input.items.reduce((n, x) => n + x.price, 0)};","input":{"items":[{"price":2},{"price":3}]}}
```

The response is `{"result":{"total":5}}`. Code is a synchronous function body
with one `input` argument and an explicit `return`. Input can be any JSON value.

## Host tool execution and discovery

`scriptclient.Client.Execute(ctx, Request, []Tool, Callback)` runs an async
function body with `input` and `tools`. The host supplies only descriptors visible
to the authenticated caller. Dotted names form namespaces, for example:

```javascript
const available = tools.$list();
const help = tools.orders.get.$help();
const order = await tools.orders.get({order_id: input.id});
return {order, help};
```

The host registry defines names and parameter shapes. The worker has no complete application
catalog. Unknown tools are absent. Prototype names, helper collisions, duplicate
names and namespace/function collisions are rejected. Help is JSON data with
name, description and input_schema. A descriptor may also supply result_schema
and example; these optional fields are not present for every tool. Read the
description and documented examples for tools without a result schema. The same
host descriptors supply binding names and discovery content.

Order choice drafts bind their event when created. For `orders.choice`, pass
`event`, `order_id` and `empty` only with `operation: "begin"`. Subsequent
`read` and `patch` calls use `choice_ref` and their operation-specific fields;
do not repeat `event`. Use the latest returned reference with `orders.quote`
or `orders.update`, supplying the event required by those tools. Read each
tool's current `$help()` before composing a call; shared schema properties
are not necessarily valid for every operation.

The callback receives `ToolCall{Name, Arguments}` under the captured host run
context, never a worker-selected actor, role, tenant or idempotency key. `$list`
with `{}` and `$help` with `{"name":"orders.get"}` are reserved discovery calls.
The host must authenticate again and return only currently authorized metadata.
Initially supplied namespaces remain a snapshot; a retained JavaScript function
reference never bypasses current call-time authorization. Discovery has a separate
32-call budget and does not consume the eight business calls. Host callbacks
must reserve durable mutation receipts before effects and recheck current ACL,
versions and typed inputs. Script completion or failure is not a transaction:
previously committed calls remain committed and must not be blindly retried.

Bindings are synchronous host calls returning parsed JSON. Sobek's native promise
jobs permit `await`, immediate Promise resolution and `Promise.all`, but all host
calls execute sequentially. There is no background task, timer or general async
IO service. A promise that remains pending fails the run. Unhandled rejected promises, including
detached async jobs, also fail the run. Explicitly caught errors may be handled by
the script. Returned data uses the
same captured strict serializer as Evaluate after the outer promise settles.
Function bodies, descriptor data and results remain untrusted.

The existing Unix socket accepts `CONNECT /execute`. The supervisor relays one
bounded jrpc2 connection to a fresh child in `rpc` mode. The host is the JSON-RPC
client; the child issues `tool.call` callbacks. No credentials or extra sockets
enter the child, and the supervisor never executes business tools. Each incoming
stream is bounded before JSON-RPC framing allocates; total traffic is 512 KiB in
each direction, arguments 128 KiB, individual results 64 KiB, catalog 64 KiB and
at most 128 tool bindings. The application sends only authorized names to the worker; full schemas remain in the host and are fetched by live `$help()`. The unchanged catalog byte limit still applies. Names support up to three segments.

Execute has a cumulative 200 ms VM-active elapsed-time budget, paused during
synchronous host IO. This includes serialization and scheduling delay; it is not
a CPU-time budget. Host operations have a ten-second limit inside one shared
60-second execution deadline, with a 61-second child watchdog and a 62-second
transport deadline. An earlier parent deadline wins; callbacks cannot renew the
shared deadline. These are the existing C5 limits in
`platform/internal/scriptprotocol/budgets.go`, replacing the earlier 5/6/7-second
Execute limits. Daniel superseded the separate five-second D-001 normal-response
criterion on 7 October 2026: a complete agent response may take longer. Preserve
the full scenarios and bounded completion within existing model, execution,
transport, Go/scenario and stand budgets. Historical failures are not PASS.
Graceful restart remains at most five seconds without forced termination.
Hard container memory and network restrictions
still apply. Cancellation closes the per-run connection;
callbacks use the original host run context rather than the RPC library's
connection-only callback context. The legacy Evaluate budgets below are unchanged.

`github.com/creachadair/jrpc2 v1.3.5` provides request correlation, callback
handling and framing; it is not a security sandbox. Its BSD-3-Clause license and
that of `github.com/creachadair/mds v0.26.1` were inspected in downloaded module
sources. The selected existing `golang.org/x/sync v0.22.0` is BSD-3-Clause. See
[jrpc2 source](https://github.com/creachadair/jrpc2/tree/v1.3.5) and
[mds source](https://github.com/creachadair/mds/tree/v0.26.1). Preserve notices.
The repository-pinned govulncheck reported no vulnerabilities for the worker,
client, supervisor and commands on 2026-09-26. This is a known-vulnerability scan,
not independent sandbox or product acceptance.


Dependency acceptance evidence (checked 2026-09-26): the maintainer's
[v1.3.5 release commit](https://github.com/creachadair/jrpc2/commit/d89bd5d417712af18e08539547276b70eb00be37)
is dated 2026-03-04 and identifies a maintenance release. The repository continued
receiving [dependency maintenance on 2026-09-20](https://github.com/creachadair/jrpc2/commit/ff69a768059169609fb4561fe762208b36941267).
The [README version policy](https://github.com/creachadair/jrpc2/tree/v1.3.5#versioning)
commits to stable v1 public APIs; this is not a support or security guarantee.
Identifiable downstream manifests include
[Snyk Language Server](https://github.com/snyk/snyk-ls/blob/4c482eb524c68220a85a075ab74c2b55b4767416/go.mod)
with a direct jrpc2 dependency and
[Snyk CLI](https://github.com/snyk/cli/blob/2543307acffce806166a8c6920f8ab78eca29cf5/cliv2/go.mod)
with an indirect dependency. Both use v1.3.0, so this establishes library adoption,
not their validation of our v1.3.5 pin or our sandbox configuration.
The implementation-owner vulnerability scan above passed. A later independent
reviewer scan did not run: automatic approval review rejected possible local
module-metadata export. That separate limitation was recorded without retrying
through another route; it is not an independent passing scan.

## Agent tool integration

The on-demand `scripting` skill is selected by the model for calculations,
data transformations and authorized host operations. The nullable plan field is
`script_action: {"code":"return input.a+input.b;","input_json":"{\"a\":2,\"b\":3}"}`.
`input_json` is a JSON string because both provider schemas use a fixed typed
contract. The host validates and decodes it into the worker's `input` JSON value.
It adds no actor, credential or environment to input. The bot
imports only `scriptclient`, never the VM package.

The application uses Execute with the current host registry. Evaluate remains
available for legacy data-only clients. The current host registry contains 27 tools (25 when booking is disabled):

| Tool | Arguments | Behavior |
| --- | --- | --- |
| `workflow.get` | `{}` | Read current workflow |
| `workflow.catalog` | `{}` | Read available workflow slots |
| `workflow.select` | `{slot_id}` | Select a catalog slot as a draft; confirmation stays manual |
| `orders.list` | `{}` | Read own active-event order summaries |
| `orders.get` | `{order_id}` | Read an own order summary |
| `orders.change` | `{name, order_id?, extra?}` | `create`, `add_extra`, or `remove_extra`, grounded in the current user request |
| `history.page` | `{cursor?}` | Read a byte-bounded page of own sanitized archive events |
| `orders.page` | `{cursor?}` | Read own active-event order IDs, versions, states and totals with continuation |
| `orders.read` | `{order_id, cursor?}` | Read full own order JSON in version-bound Unicode-safe chunks |
| `passes.events` | `{cursor?}` | Browse active events with explicit en/ru title excerpts |
| `passes.get` | `{event}` | Read own current pass booking |
| `passes.invitations` | `{event, cursor?}` | Read invitations addressed to the current actor |
| `passes.event.read` | `{event, cursor?}` | Read complete localized event JSON in chunks |
| `massage.parties` | `{event?, cursor?}` | Browse parties; default to the current event |
| `massage.slots` | `{event?, party, length, cursor?}` | Browse eligible slots with provider name/icon excerpts |
| `massage.bookings` | `{event?, party?, cursor?}` | Read only own appointments |
| `massage.provider.read` | `{event?, provider, cursor?}` | Read complete public provider JSON in chunks |
| `preferences.get` | `{}` | Read own current language preference |
| `profile.get` | `{}` | Read own pass profile, including saved personal details |
| `memory.summary`, `memory.index`, `memory.search` | `{namespace?, event?, topic?, text?, mode?, cursor?}` | Authorized shared/private summary, index and search; follow continuation markers |
| `memory.read`, `memory.history`, `memory.revision`, `memory.sources` | `{ref, cursor?}` | Current chunks, historical revisions and owner-authorized provenance |
| `memory.write` | `{name, topic, key, text?, ref?}` | Owner-private `document_set` / `document_delete`; existing documents require a current host-observed read |

Unknown arguments are rejected. The host binds identity, event, versions and
durable replay keys. `workflow.select` and `orders.change` are absent when `can_book` is false; owner-private memory writes remain available.
`tools.$list()` and each supplied method's `$help()` refresh current permissions;
every business call checks permissions again. A shared authenticated registry supplies descriptors, dispatch adapters and per-tool result budgets; existing typed Core clients and domain command preparation remain authoritative. All-domain API parity is incomplete; see [script-api-parity-plan.md](script-api-parity-plan.md).

The tool accepts at most 4 KiB of JSON-escaped code and 128 KiB of explicit JSON
input. Returned JSON must fit 4 KiB including model JSON escaping. These tighter
model budgets are independent of the helper's larger protocol limits. Invalid
or oversized results become a fixed error, never partial JSON. Worker diagnostics
are not returned to the model. Execute has a seven-second host deadline; the
worker retains its CPU interrupt, process watchdog and container memory limit.

At most two runs are allowed per authenticated Telegram update. The host stores
the request and an `interrupted` reservation before worker I/O under an
actor/update advisory lock. Results and consumed budget survive retries and
process restarts. Cancellation returns to the caller and leaves the reserved
slot consumed. The next model prompt receives code and result/error with the
remaining budget; duplicate input is retained only in the durable host record.
Script records are excluded from generic conversation history and other actors'
contexts. They are owner-private retry records, not an erasure mechanism.

A script plan cannot include a business mutation. After the result, the normal
planning loop resumes with the original request and existing media/knowledge
evidence intact. Script output is untrusted data; strings claiming admin rights,
payment or publication do not change application state. Any subsequent business
proposal uses the same current authorization, version and idempotency checks.

Within Execute a script may perform the supplied mutations. Each call reserves
its bound command and replay key before effects. Completed outcomes and uncertain
interrupted admissions remain in the owner-private run record. An interrupted
run is not automatically executed again. After confirmed mutations, subsequent
calls and ordinary planning use the refreshed workflow/order state while retaining
the original user request as grounding evidence. The host limits workflow/order/preferences call results to 2 KiB, pass profile results to 8 KiB and memory/history/order-page/order-chunk/domain-read results to 32 KiB; oversized results fail explicitly. Memory already supports paginated index/search/history and chunked reads, with 24 KiB serialized page budgets. See [memory-hierarchy.md](memory-hierarchy.md). Legacy `orders.list` still collects all pages before projection; use `orders.page` and `orders.read` for bounded navigation and complete details. Restricted domain operations and further domain coverage remain follow-up work.

Configure either YAML `script.enabled: true` and `script.socket: /absolute/path`,
or `ZNS_SCRIPT__ENABLED=true` and `ZNS_SCRIPT__SOCKET=/absolute/path`. Default is
disabled; there is no in-process fallback. The same wiring applies to `app` and
`bot`. The runtime records only the bounded `js.run` operation label, closes the
Unix client during shutdown and drains through the existing application lifecycle.
The socket must point to the separately memory-limited, networkless helper.

## Pass and massage navigation

These eight reads require a current authenticated actor, including actors whose
booking capability is disabled. They expose public navigation and owner data;
admin queues, other owners' appointments, provider work schedules and notification
settings are not exposed. Existing manual confirmation remains authoritative.

Pages return `{items, more, next_cursor}`. Pass events and massage slots use narrow
Core page endpoints with at most 20 items and 24 KiB serialized output. Event
navigation includes only en/ru title excerpts (160 characters each), marked
`titles_excerpt`; provider navigation marks `name_excerpt` and `icon_excerpt`.
Stable IDs and `detail_available` identify the corresponding detail operation.
Invitations, parties and own appointments use typed existing Core clients and
20-item / 32 KiB script pages. Those existing Core responses retain their 1 MiB
transport bound; larger resources fail explicitly rather than imply completion.

```javascript
const page = tools.passes.events({cursor: input.cursor || ""});
return {events: page.items, more: page.more, next_cursor: page.next_cursor};
```

`passes.event.read` and `massage.provider.read` return `{json, more, next_cursor}`.
Join ordinary JS strings and call native `JSON.parse` after the last chunk; no
UTF-8 decoding is needed. A chunk contains at most 4000 Unicode characters.
Full serialized resources must fit 1 MiB; `{error:"result_limit"}` explicitly
means the complete detail is unavailable at that size. Public provider details
include full localized about text but no private scheduling/notification fields.

```javascript
let cursor = "", text = "", complete = false;
for (let i = 0; i < 8; i++) {
  const part = tools.passes.event.read({event: input.event, cursor});
  if (part.error) return {error: part.error, restart: part.error === "stale"};
  text += part.json;
  if (!part.more) { complete = true; break; }
  cursor = part.next_cursor;
}
if (!complete) return {incomplete: true, next_cursor: cursor};
const event = JSON.parse(text);
return {id: event.id, titles: event.titles};
```

The final JSON still must fit 4 KiB, and all business calls share the eight-call
run budget. The example reports incompleteness when that budget cannot finish;
a cursor alone cannot recover earlier chunks. Keep partial text only in an
explicit bounded continuation workflow. Never claim a complete read after an
incomplete result. For large title/about fields, return a useful bounded summary.

Cursors bind actor, operation and query/resource. Detail and slot snapshots detect
changes and return `{error:"stale",restart:true}`: discard accumulated chunks or
pages and restart. Pass-event navigation uses live keyset ordering. Successful
intermediate payloads stay in owner-private receipts and are omitted from the
next model context; errors remain visible. Diagnostics record only finite tool
names, item counts and `no_results` for complete empty pages.
## Bounded history and complete order reads

`history.page({})` returns `{events, more, next_cursor}`, newest first. It reuses
Core owner-private archive filtering and sensitive-text omission. Continue with
`history.page({cursor: page.next_cursor})`; pages stop at both the existing
20-event limit and 32 KiB serialized JSON budget. `orders.page({})` has the same
continuation fields and an `orders` array containing IDs, versions, states and
totals for the active event. It reads one Core page at a time, at most 25 orders,
without copying full meal choices into the script navigation result.

```javascript
const page = tools.orders.page({});
return {orders: page.orders, more: page.more, next_cursor: page.next_cursor};
```

`orders.read` resolves an own order by ID, including an order from an older event.
It returns `{order_id, version, json, more, next_cursor}`. `json` is an ordinary
JavaScript string containing part of the serialized full order. Chunks split on
Unicode rune boundaries, at most 6000 runes; scripts need no UTF-8 decoder. Join
the strings in order and use native `JSON.parse` only after the last chunk.

```javascript
let cursor = "", text = "", complete = false;
for (let i = 0; i < 8; i++) {
  const chunk = tools.orders.read({order_id: input.order_id, cursor});
  if (chunk.error === "stale") return {error: "stale", restart: true};
  text += chunk.json;
  if (!chunk.more) { complete = true; break; }
  cursor = chunk.next_cursor;
}
if (!complete) return {incomplete: true, next_cursor: cursor};
const order = JSON.parse(text);
return {id: order.id, state: order.state, total: order.choice.total};
```

The eight-call budget includes other business tools used in that run. For very
large orders, return explicit incompleteness and retain partial text plus cursor
in the caller's bounded continuation workflow; a cursor alone does not contain
earlier chunks. Do not claim full coverage when a budget stops the read. A new
read can target just the evidence needed rather than returning a full order to
the model, whose final result remains limited to 4 KiB.

Order continuations bind owner, operation, ID, version, full serialized snapshot
hash and rune offset. Every chunk rechecks Core ownership. A changed version or
snapshot returns the finite result `{error: "stale", restart: true}`; discard accumulated chunks and restart from the first chunk. The host persists `Outcome.Error="stale"`, keeps that recovery evidence visible to the next model prompt, and records a conflict diagnostic. Other execution failures retain their existing generic error contract.
History and order-index cursors bind owner, operation and query scope. These are
navigation tokens, not permission grants; each call reauthenticates. Pages are
live keyset reads, not a snapshot across concurrent edits.

Successful intermediate page/chunk payloads remain in owner-private receipts;
the next model prompt gets completion metadata and the script's selected final
result. Failures remain visible. A failed script or read does not roll back prior
mutations and must not trigger blind replay.

## Worker value contract

Results can be finite numbers, strings, booleans, null, dense arrays and plain
objects. Undefined, functions, symbols, BigInt, nonfinite numbers, cycles,
accessors, custom prototypes, Date, Promise and sparse arrays are rejected.
Objects with extra nonenumerable or symbol properties are also rejected.
The serializer captures its intrinsics before scripts run, so changing
`JSON.stringify`, Object helpers or prototypes does not replace validation.

Every request starts a fresh VM. `Date.now()` starts at the fixed Unix epoch and
`Math.random()` returns zero: there is no clock or entropy service. Supply time
or seeded data explicitly if a calculation needs them. There is no Node.js
compatibility layer, module loader, console, timer/event loop, filesystem,
network, environment or process execution exposed to JavaScript. Execute exposes
only the host-supplied authorized tool bindings described above.
Standard ECMAScript computation is available; ES module loading is not enabled.
Both runtimes disable Sobek source-map loading before parsing, including eval
and Function constructors, so sourceMappingURL comments cannot trigger the
default parser filesystem loader.

## Authorization boundary

Scripts compute or transform data already made available by the host. They do
not receive a credential or choose a user, tenant, chat or administrator. Script
output is untrusted data. Any later business action must use the existing host
tools with the authenticated actor, current permissions, input validation and
idempotency checks. Do not interpret output as an executable tool plan without
those checks. This keeps the user's existing rights; scripting grants no new
business authority.

Each running script has the callable names captured when its VM was created.
Discovery and help intersect that set with current permissions on every call.
Revocation removes a name from discovery immediately and rejects calls through
previously saved function references. A grant adds new bindings on the next
script execution; the current run never advertises a newly granted name it
cannot invoke. Separate runs capture independent sets.

## Budgets and process contract

| Resource | Limit |
| --- | --- |
| JavaScript source | 16 KiB UTF-8 |
| Input JSON | 128 KiB UTF-8 |
| Complete request envelope | 256 KiB |
| Result JSON | 64 KiB UTF-8 |
| Complete response envelope | 64 KiB + 64 bytes |
| Result nesting / visited values | 32 / 8192 |
| VM call stack | 256 frames |
| VM execution including serialization | 200 ms, or earlier caller cancellation |
| Whole helper lifetime, including blocking stdin/stdout | 2 seconds |
| Go runtime memory target | 64 MiB, **soft only** |

JSON parse/compile work and native builtins may not yield immediately to the VM
interrupt. The executable therefore has a separate process watchdog that exits
with status 124 after two seconds. The host must also impose its own deadline
and kill/reap the process on timeout or cancellation. A killed process can have
truncated or absent output; never treat a partial response as success.

Neither Sobek nor Go's `debug.SetMemoryLimit` provides a hard heap bound. Untrusted
scripts must never run through `Evaluate` in the main application process. Run
the helper in a dedicated restricted container or equivalent OS sandbox:

- No network namespace connectivity (`--network=none`), no published ports.
- Hard memory and swap budget, for example `--memory=128m --memory-swap=128m`;
  CPU quota, for example `--cpus=0.5`; bounded pids, for example `--pids-limit=32`.
- Read-only root filesystem, unprivileged UID, all capabilities dropped and
  
o-new-privileges`; no Docker socket, host filesystem or credential mounts.
- Empty application environment. The command clears its inherited environment,
  but the launcher must avoid passing secrets in the first place.
- One active job per worker admission slot; one process per request. Close input,
  bound stdout to `MaxResponseBytes`, bound/discard stderr,
  and kill/reap on overflow. A host must validate the response schema and JSON
  before using the result. Configure a hard parent deadline independent of the
  internal watchdog. Do not retry business actions from script failures.

The protocol uses stable error codes: `invalid_request`, `execution_failed`,
`invalid_result`, `timeout`, `canceled`. Script exceptions, stack traces and
input contents are not returned or logged. Unknown request fields and trailing
JSON documents are rejected. The host should send canonical JSON from a typed
request and must not accept output fields beyond `result` or `error`.

`internal/scriptworker.Evaluate(ctx, Request)` is the isolated worker core.
`Serve(ctx, reader, writer)` implements the wire protocol. `Serve` itself cannot
cancel an arbitrary blocking Go reader/writer: executable and host watchdogs
provide that process boundary. A subprocess without OS memory/network controls
is not an acceptable production isolation boundary.

## Dependency decision and evidence

Assessment date: 2026-09-26. Use
`github.com/grafana/sobek v0.0.0-20260908083152-4698bc773ae7`, the exact revision
in [k6's current module manifest](https://github.com/grafana/k6/blob/master/go.mod).
This is a dated observation of a moving branch, not a claim about every released
k6 version. Grafana's [k6 documentation](https://grafana.com/docs/k6/latest/using-k6/modules/)
also identifies Sobek as its embedded engine, establishing actual downstream
usage. The [Sobek README](https://github.com/grafana/sobek) identifies the Goja
fork and k6-driven maintenance. Its recent September revision, upstream merges
and active k6 use are maintenance evidence, not a support guarantee.

Goja was evaluated too. [PocketBase's manifest](https://github.com/pocketbase/pocketbase/blob/master/go.mod)
uses `github.com/dop251/goja v0.0.0-20260901132549-43234fa61381`, so Goja has real
downstream adoption. k6 now uses Sobek; it must not be cited as current Goja
adoption. Sobek was chosen for its maintained fork, direct k6 dependency and
published [security reporting policy](https://github.com/grafana/sobek/security).
The policy describes Grafana's coordinated vulnerability-reporting process;
the page showed no published advisories when checked. This is not proof that
the engine has no vulnerabilities.

The downloaded pinned modules' license files were inspected. Sobek's
[MIT license](https://github.com/grafana/sobek/blob/4698bc773ae7/LICENSE) is allowed
by the project's dependency policy. `go list -deps ./cmd/scriptworker` identifies
these linked third-party modules:

| Module | Version | License |
| --- | --- | --- |
| grafana/sobek | v0.0.0-20260908083152-4698bc773ae7 | MIT |
| dlclark/regexp2/v2 | v2.7.2 | MIT |
| go-sourcemap/sourcemap | v2.1.4+incompatible | BSD-2-Clause |
| google/pprof | v0.0.0-20230207041349-798e818bf904 | Apache-2.0 |
| golang.org/x/text | v0.41.0 | BSD-3-Clause |

License sources: [regexp2](https://github.com/dlclark/regexp2/blob/v2.7.2/LICENSE),
[sourcemap](https://github.com/go-sourcemap/sourcemap/blob/v2.1.4/LICENSE),
[pprof](https://github.com/google/pprof/blob/798e818bf904/LICENSE),
[x/text](https://cs.opensource.google/go/x/text/+/v0.41.0:LICENSE).
Sobek's module manifest also mentions test-only dependencies; these are not
linked into the helper. Preserve upstream notices when distributing the image.

The repository-pinned `govulncheck` (`golang.org/x/vuln v1.8.0`) was built from `platform/tools` and run over
`./internal/scriptworker/... ./cmd/scriptworker/...`; it reported **No
vulnerabilities found** on 2026-09-26. Re-run the scan before releases and engine
updates. This reports known reachable vulnerabilities, not a sandbox proof.

Focused tests cover deterministic transforms, fresh runtimes, absent host
globals, infinite loops, cancellation, malformed and oversized requests/results,
non-JSON values, cyclic/deep data, getters, intrinsic mutation and the executable
watchdog while stdin is blocked. Unit tests and static checks do not substitute
for container hard-memory/network tests or independent product QA.





