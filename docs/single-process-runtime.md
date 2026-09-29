# Single-process runtime

`zns app` hosts the Telegram poller, authenticated Core API, model adapter and
Mini App in one process. Media and script decoders remain isolated helpers.
The API client calls the same process over its bound HTTP listener, with the
same signed actor or delivery token and permission checks as standalone modes.
The process uses one configured database pool. Production Zitadel integration
is still pending: the current launcher requires `env: sandbox`.

Use `model.provider: scripted` for deterministic acceptance, `codex` for local
synthetic real-model checks, or `openai` with the configured API key. `remote`
remains available for separately hosted model fixtures. The API and Mini App
share `server.port`; `/miniapp/` routes to the editor and `/v1/` to Core.

On shutdown the bot stops accepting work and cancels its current operation.
Every received Telegram batch has already been persisted before offset advance;
unfinished rows resume on restart. HTTP requests get `shutdown.drain` to finish,
then their contexts and connections are cancelled. The server joins shutdown
before the caller closes the database. Maintenance and poller goroutines are
joined. This implementation chooses durable recovery rather than trying to
finish an arbitrarily long model turn during shutdown.

Deployment sequence: stop the old process, wait for exit, then start the new
process. The database poller lock also rejects accidental concurrent ownership.
No rolling overlap or distributed worker coordination is required.

Initial evidence: `qa.local/app-runtime-check/evidence.json` records a clean
synthetic database run through manual selection, agent continuation, manual
confirmation and graceful restart. The test's app and fake processes and test
database were removed afterwards. Server tests cover both completed requests
and cancellation when the drain budget expires. Independent Code QA and
browser Functional QA are still required for runtime acceptance.
