# Integration review and test fidelity, 2026-10-21

The review sweep read all 812 non-generated Go files in the current rewrite,
including all 241 integration files in 13 independent groups. The three generated
sqlc files remain subject to the generated-SQL CI check. Full source coverage
is separate from runtime acceptance and production readiness.

Seven integration-test findings required focused corrections:

- Registration reconciliation now clears the decoded response before each read
  and requires a successful status. Missing JSON fields cannot retain an earlier
  successful registration and make an incorrect response look valid.
- Four session-revalidation tests observe waiters blocked by their own PostgreSQL
  backend PID. Parallel fixtures use separate schemas but share the database;
  a database-wide lock observer could previously accept another test's waiter.
- Pair notification restart acceptance reads persisted intents and checks their
  immutable identity, invitation version, recipient and kind. Background delivery
  may already have consumed the ready queue when the assertion runs.
- Two native-menu continuation tests allow the original active agent task to
  poll an unavailable Loom. They still require the original run/question identity,
  no replacement run or input, and successful continuation. Dedicated menu tests
  without an active run retain their zero-Loom-call assertion.

## Focused runtime evidence

A frozen copy of the old pair-notification test, with a 1.5-second scheduling
pause after restart, fails all four decision cases: the ready queue has been
consumed. The corrected test passes the same four cases with the same pause.
Both records are retained; the old failure is not rewritten as success.

The subsequent Linux/PostgreSQL/race selection passes 43 roots and 68 cases,
including all eight required roots for these fixes, without failures or skips.
That snapshot precedes the final persisted-intent helper extraction and flatter
condition. The final changed-file follow-up passes one root and five cases; all
other Go files match the broad snapshot. Final strict lint reports zero issues.
Both the initial fix and final source pass fresh independent reviews.

An initial runner launch failed before a test container was created because its
command duplicated the Docker executable. The same owned PostgreSQL was reused
after checking its owner label. Initial lint findings are retained separately.

The temporary resources use an internal network, a 512 MiB PostgreSQL container
and sequential 3 GiB Go runners with no swap. The original primary's eleven
container IDs, images and start epochs remained unchanged during the broad gate.
All six owned containers, their volume and network are now absent. PostgreSQL
was exported and fully decoded before retirement; the 1,986,520-byte archive has
SHA-256 `c9dfd683c2b485fd97c238cc434e15a08ff7b6ad488e9abf397bd8ed5614276b`.
The primary is preserved; resources belonging to the Loom developer were not
modified.

## Remote CI and installed boundary

[CI45](https://github.com/complynx/zns-chatbot/actions/runs/39655949519) passes
870 integration roots, 368 UI tests, generated SQL, lint and Linux/container build.
Its source is `a7ba9287`; the tested PR merge is
`ab02483a4de9739f0ac9a62f470907f3a52358cc`. Later local test fixes are not included
in that result. The primary still runs the image built from source `f78f7096`,
`mvp-89/schema69`; no new primary installation or real Telegram acceptance is
claimed here.

Private frozen sources, source hashes, JSON test streams, process states and
failure evidence are retained under `.cache/shuttle-work/integration-fidelity-v1`.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk