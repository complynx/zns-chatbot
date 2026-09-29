# Code quality gates

Every stage needs two independent reviews: Code QA and functional Senior QA.
Count fresh Code QA requests from the 2026-09-29 resumption, starting at 1.
Route requests 3, 6, 9, and so on through `claude-opus-5-5` when the SSH Claude
tunnel and model are available. Record the request number, scope, route and any
availability limit in the developer tracking record before dispatch; add the
result afterward. Do not send that record, audit findings or fix hints to the
reviewer. This routing rule does not replace either independent gate or relax
freshness, read-only scope, required checks or acceptance conditions.
The review procedure is in [the migration plan](go-migration.md).
Implementation agents may be reused across related development tasks so they keep
project context. Acceptance reviewers must be independent of implementation and
fresh for the stage or substantive affected change. Give Code QA the original
requirements, scope and frozen source/diff. Give Functional QA requirements,
public contracts and sandbox access only. Do not supply development conversation,
previous findings or fix hints. A developer replaying an independently authored
test harness produces regression evidence, not a new independent QA verdict.

Real Telegram integration is a final acceptance gate. Complete the main checks
on the synthetic Telegram-like stand first. Use only the user's designated test
account and test bot for that final gate; access to them does not authorize
production deployment or messaging unrelated accounts.
The designated bot is CLX Test Bot, Telegram ID `6087685431`; its matching token
is in the local untracked `config/config.yaml`. Read the token only at the runtime
boundary. Do not include it in reports, command output, browser URLs or Git.
The user authorizes all test interactions with this bot from that test account.
This authorization is specific to that account/bot pair and does not waive the
requirement to finish the main synthetic checks before real Telegram testing.

## Decisions and local test authorization

Daniel's instruction of 2026-09-27 authorizes local synthetic testing, including
disposable Zitadel instances, complete local debug/trace output, copying between
local test databases, and changing local test builds. Keep sensitive logs local;
do not publish credentials in Git, shared reports or messages. This permission
does not authorize production changes or unrelated account access.

Record questions requiring Daniel's decision or participation in the dedicated
section of `PROGRESS.html`. Name the decision, alternatives and consequences,
dependent work, and checks that can proceed independently. Defer only the
dependent work until the answer arrives. Do not treat unanswered questions as
approval, and do not stop unrelated development or synthetic tests.

Limited real-model API integration checks are authorized after the main tests,
using a small explicit request/output bound and recording actual or estimated
spend. Real test Telegram remains the final gate described above.

If all remaining product work depends on recorded decisions, state that clearly
and continue the required architectural refactoring. Once that is complete,
perform a security audit, then review architecture, modularity, maintenance and
extensibility if answers are still pending. These activities do not replace
pending product acceptance.

## Sandbox lifecycle

New synthetic PostgreSQL databases use `synthetic_qa_zns_<scenario>` names.
New Compose projects and containers use `synthetic-qa-zns-<scenario>` where the
resource format permits it. State the synthetic purpose, exact source and
destination, local-only adapters and resource owner in the stand access record.
Use these names only for artificial test data; naming is not authorization or
proof of isolation. Do not rename active stands during QA. Apply the convention
at creation or at an explicitly coordinated idle transition, preserving the
previous evidence and source/destination mapping.

Each QA stand has one owner and a distinct Compose project. Keep the stand
unchanged while QA runs. After QA finishes and its report and evidence are saved,
stop and remove obsolete containers, project networks, and disposable test volumes.
Keep a stand needed for a pending reproduction until that check finishes.
Check project labels and remaining consumers before removal. Preserve shared
PostgreSQL, identity services, active stands, and unrelated projects.

Remove unused superseded project images after their containers are removed.
Prune unused build cache only between builds; retain a bounded working cache.
Do not use a global volume prune. An unattached volume can still contain needed
data; establish its purpose before deleting it. Record cleanup in the progress
history and keep QA reports outside disposable Docker resources.

## Go

`platform/.golangci.yml` starts from the complete Golden configuration, not a
small selection of its linters. `platform/tools/go.mod` pins GolangCI-Lint 2.14.0
separately from runtime dependencies. Dependabot updates both Go modules.

Source comparison on 2026-09-25:

- [Golden config, b57a98e8](https://github.com/maratori/golangci-lint-config/blob/b57a98e8ac187b8c401660d2205caa6523bd86fb/.golangci.yml).
- [Nebius Go SDK, abac7580](https://github.com/nebius/gosdk/blob/abac758009243bedc405d08c9312c9f1145f6ac5/.golangci.yml).

All enabled Nebius linters and formatters are already enabled in this Golden
revision, including `paralleltest` and `clickhouselint`. Golden additionally
enables `embeddedstructfieldcheck`. Nebius-specific path suppressions do not apply
to this project and are not copied. Local import prefixes identify this module.
The third-party `node_modules` tree is excluded from Go linting and formatting.
Do not raise complexity limits or exclude new business code to obtain a pass.

Use testify `require` for prerequisites and `assert` for independent outcomes.

`govulncheck` is pinned in the same tools module and runs in CI and the local
quality gate. It checks runtime entry points against the Go vulnerability
database and fails on reachable known vulnerabilities. Module-level advisories
for packages not imported by the runtime are reported separately. Dependency
updates must preserve this gate; never hide findings with blanket ignores.

Never call `require` from worker goroutines or HTTP handler goroutines; return
their errors to the test goroutine. Tests use isolated PostgreSQL databases.
Automated live sandbox suites remain serial when they share a running stand.
For global Functional QA, independent scenarios may run in parallel under the
coordination rules below; shared-state changes remain serial.
When concurrent Code QA database tests would block an active Functional QA stand,
use a separate bounded disposable PostgreSQL instance for the code reviewer.
Assign its owner, loopback port, output paths and cleanup responsibility explicitly.
Do not share cluster-wide role mutations or reset another reviewer's fixtures.

### SQL generation

SQL query packages use sqlc 1.31.1 with native pgx/v5. The generator is pinned in
`platform/tools/sqlc`, separately from runtime and lint dependencies. Edit the SQL
in the owning module's `queries` directory listed in `platform/sqlc.yaml`, then run
from `platform`:

```sh
go -C tools/sqlc tool sqlc generate -f ../../sqlc.yaml
go -C tools/sqlc tool sqlc diff -f ../../sqlc.yaml
```

The existing migrations supply the schema; no live database or cloud service is
needed to generate code. Commit generated files with their SQL. Local and CI gates
run `sqlc diff` to reject stale output. Keep authorization, paging and public JSON
types and transaction boundaries in the service; generated queries do not replace
those contracts. Schema changes require their own migration and data-preservation
proof. Check every configured generation block after changing shared migrations.

sqlc is MIT-licensed. Its tool-only dependency graph includes database parsers and
cloud clients that do not enter the application binary. The isolated module pins
patched gRPC and x/text versions because sqlc's original dependencies triggered
the vulnerability gate. Both local and CI gates scan the generator entry point as
well as the runtime. Do not remove these pins without a passing scan.

## JavaScript

- ESLint recommended: language correctness, unused code and explicit comparisons.
- Unicorn recommended: modern APIs and maintainability.
- Promise recommended: asynchronous control-flow errors.
- Security recommended: suspicious dynamic operations; findings need context,
  and the plugin is not a substitute for security review.
- Prettier: one deterministic formatter; ESLint's conflicting style rules are off.
- Playwright: browser acceptance with mouse and emulated touch, actual DOM events,
  permission failures and mixed manual/agent workflows.

Versions are exact in package.json and resolved in package-lock.json. Node and
browser globals are scoped separately. Browser tests also declare browser globals
for functions evaluated in the page. The UI script is a separate linted module;
the server embeds it and serves it with a same-origin script CSP.

Run `npm ci`, `npm run quality` and `npm run test:browser` in `platform`.
The first command checks static code and formatting; browser acceptance needs the
running Compose sandbox. Do not interpret lint success as functional acceptance.

`npm run quality:all` is the full local gate. It requires `TEST_DATABASE_URL` and
`SANDBOX_URL`, builds the pinned Go tool, and runs lint, format checks, dependency
verification, vet, build, race/integration tests, fuzzing, live smoke and browser
acceptance. Start Compose first and install Playwright Chromium, or set
`BROWSER_CHANNEL=msedge`. On Windows it uses Docker Desktop's `desktop-linux`
context and the repository's isolated PostgreSQL container for Linux race tests.
The command fails on the first failure; it never treats a missing stand as success.

Start the full media stand with `compose.yaml`, `compose.qa.yaml`, `compose.av.yaml`
and `compose.av.qa.yaml`. The gate checks AV player retention with mouse/touch and
the real isolated decoder on loopback 8097 against exact saved RU/EN transcripts,
sparse/refined frames and duration refusal. No real OpenAI key is used by this
overlay. See `platform/testdata/media/README.md` for fixture scope and limitations.

## Review evidence

## Dependency acceptance

New libraries require MIT, BSD, Apache 2.0 or MPL licensing. Record the exact
version and license obligations, relevant security advisories and a vulnerability
scan, maintenance/release evidence, and identifiable downstream users. Stars and
download totals alone do not establish adoption. Exclude suspicious popularity
signals; state evidence gaps instead of claiming all artificial activity was removed.
Check whether an existing dependency or standard-library facility already fits.

## Review evidence records

Record the exact commands and results per stage. A configured tool is not a passed
gate. A skipped DB or browser test is not a passing acceptance test. Static code
review and black-box QA have different scopes; neither replaces the other.

## Global Functional QA during refactoring

Daniel authorized a coordinated FQA team for broad acceptance during architectural
refactoring. Use one Functional Senior QA lead and fresh source-blind subagents
when the scenario breadth benefits from parallel work. Small scoped gates may
remain with one reviewer.

The lead owns the acceptance matrix, stand configuration and fixture changes.
Assign each subagent separate report/browser paths and disjoint users or event
fixtures before execution. Give reviewers original requirements, public contracts
and sandbox access only; do not pass implementation details or prior findings.

Run independent flows in parallel only when their state and fault controls are
isolated. The lead schedules exclusive barriers for permission changes, shared
provider faults, restarts and other global operations. Pause affected agents at
those barriers. Do not rebuild the frozen candidate during QA.

Subagents return reproducible evidence and limitations. The lead reconciles scope
coverage and cross-domain flows into one acceptance report; a passed slice does
not accept the whole stage, and skipped checks remain untested. Code QA remains
a separate fresh independent gate.

## Permission-filtered agent discovery

Before the model sees a tool, filter names, descriptions, argument enums, examples,
provider schemas, skills and `$list`/`$help` by current authenticated capabilities
and event/resource scope. Execution-time rejection alone is insufficient.

Each affected stage must verify ordinary users, event-specific payment admins,
global admins without that payment role, and revocation between calls. Assert
that unauthorized approval/rejection names are absent from provider input and
script discovery; guessed names/help produce generic unavailability. A role for
event A must not expose event B operations. Recheck authorization at execution.
Keep Code QA and independent Telegram-like Functional QA evidence and limitations.
