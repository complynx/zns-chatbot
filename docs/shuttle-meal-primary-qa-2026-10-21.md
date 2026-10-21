# Meal financial installation acceptance, 2026-10-21

Status: the owned shuttle-live-test stand runs schema69 / zinusya mvp-89.
The controlled installation and installed HTTP checks pass. This is a local
installation result, not production readiness or a production deployment.

## Accepted source and build

Source commit: f78f70960e9afd97593f60b58029f831fb55bc0b.
Installed image: sha256:85acc5a4c5e488947ce1e633eb6c301c88545be26a019efdf6ef036ffc608d69.

[CI run39600531384](https://github.com/complynx/zns-chatbot/actions/runs/39600531384)
finishes successfully:864 integration roots across nine bounded batches,
367 UI cases without failures/skips/cancellations, zero lint issues, dependency,
guard/simulator, operational rules/dashboard, generated SQL and Linux/container
build checks. All required steps complete successfully.

GitHub checks merge commit925838982347e89156439edd1b8eb573ba942e4b.
Comparison with the source commit shows only four legacy Python asset/content
changes: frame/zns_frame.png, i18n/en/bot.ftl, i18n/ru/bot.ftl and
static/rag_data.yaml. These paths are excluded from the Go Docker build context;
the tested Go build inputs match this installed candidate.

The [local full gate](shuttle-meal-release-qa-2026-10-21.md),
[schema-copy rehearsal](shuttle-meal-schema69-rehearsal-2026-10-21.md),
[financial runtime QA](shuttle-meal-financial-runtime-qa-2026-10-21.md) and
[final-image browser QA](shuttle-meal-final-browser-qa-2026-10-21.md) remain
separate evidence with their own failures and scope limits.

## Installation and preservation

The reviewed controller first stops the original Telegram poller, drains accepted
work and gracefully stops the original bot. Both exit normally. Other services
keep running. A fresh custom-format database backup is saved and fully decoded
before the candidate migration runs once.

Migration68→69 succeeds. Checks retain existing business rows, application
sequences, database identity, queues and Loom state. Only the reviewed schema
changes, migration bookkeeping and age-qualified terminal River cleanup are
allowed. A copied-database downgrade/re-up rehearsal passed separately; no
downgrade is attempted on the primary.

Only the bot container is replaced. Readiness, schema69, service_id zinusya and
revision mvp-89 pass before ingress resumes. The completed migration container
is removed. The original poller container starts once and its new runtime epoch
logs a completed polling cycle. Its identity and the nine other companion
identities are retained. The installed observer verifies the new eleven-service
baseline and domain preservation.

## Installed HTTP acceptance

All18 public Mini App files match the frozen build hashes and return the expected
privacy headers. Six financial read/write routes reject syntactically valid
requests without a session with401. Discovery exposes the nine current meal
methods, including submission, cancellation and original-operation recovery.
The11 existing service orders and4 service proofs are unchanged; no meal order
is created on the primary by these checks.

The first HTTP checker sent empty JSON to preparation and incorrectly expected
401. The handler validates required arguments first and returned400. The original
checker and failure are retained. The corrected checker supplies valid arguments
without a session; all six auth boundaries pass. Fresh independent review reports
no actionable findings. Product code and the installed image are unchanged.

These checks establish installation, assets, anonymous authorization boundaries,
discovery and custody. They do not establish an authenticated meal workflow on
the primary. Actual browser/native and controlled Loom financial workflows are
accepted separately on retired isolated stands. Joint real-Telegram/CA/ZitadelTG
acceptance and exact native-preparation adoption remain open.

Private evidence is under .cache/shuttle-work/meal-financial-flow-v1/:
release-v2/ci-accepted.json and ci-job-excerpt.log retain CI acceptance;
primary-upgrade-v1/checkpoint.json, installed-epoch-v37.json and decoded backup
retain installation; installed-http-qa.json and installed-http-failure-v1.json
retain the separate corrected HTTP check and original failure. Credentials,
database dumps and raw runtime logs stay ignored.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
