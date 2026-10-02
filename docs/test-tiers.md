# Test tiers

Run these commands inside Linux Docker or WSL, from `platform`:

```sh
npm run test:fast
npm run test:slow
npm run test:changed -- <integration-base-commit>
npm run test:all
npm run test:changed -- <integration-base-commit> --plan
npm run test:tiers
```

`test:fast` excludes only the explicit slow scenarios in
`platform/scripts/slow-tests.json`. `test:slow` runs those scenarios with all
their subtests. Examples and fuzz seed cases remain in the fast tier.
Both tiers together cover every package in the platform Go module through
`go list ./...`, including `importdelivery`. The runner excludes packages under
`node_modules` from test discovery; some Node dependencies bundle Go source.
Go excludes nested modules from the package pattern. The legacy `quality:all`
runner keeps its existing package selection and required checks.
The migration tool module runs in every tier. Race detection, uncached execution,
package parallelism and test parallelism match `quality:all`.

`test:changed` always runs the fast tier. It adds slow scenarios for the affected
domain. Changes include the merge-base diff against the supplied commit, staged
and unstaged edits independently, and repository-wide untracked files with
repository-relative paths. The manifest records domain dependencies, including
notification generation from food, orders and pass registration. Identity,
conversation and scripting runtime paths serve multiple domains and select all slow scenarios.
Any unknown path selects all slow tests. This includes shared database, delivery,
query, configuration, clock, schema, build and test-runner changes. Missing
manifest selectors and discovery failures stop the run.

These commands are feedback tools. `quality:all` keeps its complete existing
checks. Run both tiers for critical acceptance, final refactoring and the final
PR CI check, even when `test:changed` selected fewer slow tests. Run the existing
full quality gate for acceptance; it also covers lint, formatting, vulnerabilities,
SQL generation, fuzzing and browser flows. Independent Code QA and Functional QA
remain required.

Actual test runs require `TEST_DATABASE_URL` and
`TEST_CREDIT_UPGRADE_DATABASE_URL`. The credit upgrade URL must use a separate
PostgreSQL cluster, as required by `docs/coordination-process.md`. Do not use a
stand occupied by Functional QA. `--plan` prints the selected commands without
running test bodies. Go discovery compiles and invokes test binaries, including
package initialization and `TestMain`: assign a Linux heavy-check slot and owned
caches for a cold run.

## Intentional wait inventory

The manifest covers these waits on the current integration source:

- Actor renewal: two one-second token expiry boundaries.
- Script worker: blocked-input process lifetime and unresolved promise execution
  budget.
- Script client: two 2.7-second callbacks that prove a composed RPC beyond five seconds.
- Bot delivery: persisted transport retries, fallback cooldowns, resend budgets,
  recovered attempts and leases. The transport-retry family is kept together.
- Inbox: retry FIFO, control cooldown, crash recovery, history rejection,
  onboarding and provider recovery after a committed deadline.
- Food and exports: CSV, derived export continuation, XLSX and reminder retries.
- Pass and presentation: atomic notice lease recovery, pass-plan retry,
  pass-redaction cooldown, terminal refusal recovery, language/workflow notice
  and Telegram metadata retries.
- Orders: payment retirement cooldown and modern export authority recovery.
- Event boundaries: sales finish after a profile lock, persisted registration
  tier opening and knowledge classification after the actual event end.
- Admin messages: provider recovery after the configured retry deadline.
- Bot receipts: authenticated recovery and denied-owner batch fairness after
  persisted continuation retry deadlines.
- Notifications: actual provider cooldown, transport eligibility and independent
  exhaustion deadlines. The notification retry family is kept together.
- Message delivery: canonical negative replay after its persisted deadline and
  actual database-clock advancement before recovered replay.

Some fast siblings remain in a slow family to keep shared recovery scenarios
together. Ordinary assertion watchdogs, short polling, cancelled fixture sleeps
and SQL waits interrupted by cancellation are not long tests. Node stand timers
already use mocked timers where appropriate; browser/FQA polling remains in the
existing full gate. A slow compilation or database setup receipt alone does not
make a scenario slow. Retain separate cold build and test execution budgets.

Persisted retry/cooldown, lease-expiry and event-boundary waits belong to the slow
tier regardless of duration, including one-second provider cooldowns and short
configured receipt retries. Ordinary fixture pacing only orders successful
deliveries and remains fast.

When adding a scenario that deliberately waits for eligibility, expiry, sales
opening or event finish, add its top-level selector and domain dependencies to
the manifest. Keep the original
test and its budgets intact. Review the inventory when shared helpers change.

Written by slow_test_categories (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
