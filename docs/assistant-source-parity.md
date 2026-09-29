# Assistant source parity

Source contract inspected 2026-09-26. Implementation is present; independent
Code QA and Functional QA are pending. This document does not replace the memory
and knowledge contracts.

## Active Python inputs

`zns-chatbot/plugins/assistant.py:204–217` loads `static/rag_data.yaml` once at
startup. Each `collection` entry contributes its optional `question`, joined
`alt_questions`, and `answer`, in source order. The assistant receives this QA
text together with the about document on each ordinary request (`:317`).
Archiving the YAML as a migration resource does not make its contents available
to the Go assistant. This static input needs a converter or runtime loader as
well as the Drive refresh.

`assistant.py:232–264` immediately starts a background refresh, exports the
configured Google document as `text/markdown`, and repeats after 3600 seconds.
Authentication uses service-account credentials stored as base64 JSON. On a
refresh failure the previous successful value remains available. The source
unescapes Markdown outside backtick spans before constructing assistant context.
The content is event information, not authority to change agent permissions.

## Required Go behavior

- Read the configured document with a maintained Google authentication library;
  do not implement JWT signing, token renewal or service-account authentication
  manually. Keep credentials in secret configuration and out of migration
  reports, model context and logs. Read-only Drive access is sufficient.
- Refresh at startup and hourly, with request timeout and shutdown cancellation.
  An unavailable upstream must not discard the last successful document. Show
  explicit source availability and freshness internally; never claim a failed
  refresh succeeded. Handle the first fetch failing without a nil context or
  fabricated document. Concurrent requests must see one complete version.
- Preserve all static QA entries and alternate questions in source order.
  Keep the source digest and provenance so migration reconciliation can prove
  which resource was loaded. Do not mark the knowledge resource converted merely
  because its original bytes were archived.
- Integrate both inputs with the existing shared knowledge hierarchy and agent
  read interfaces. Reuse bounded search, pages and exact chunks for large source
  documents; do not silently truncate them to fit the provider input limit.
  Source-managed data must not overwrite unrelated curated facts or private
  memory. If cached across restarts, bind the cache to the configured document
  and source version so changing configuration cannot serve another document.
- Imported text remains untrusted data. It cannot grant access, change the tool
  registry or bypass confirmation. Log only operational status, counts, timing
  and bounded failure codes; never document text or upstream credential errors.
- Run on CPU-only Linux Docker. No embedding service or GPU is required to read
  the document. Embeddings, if added separately, do not replace exact retrieval.

## Acceptance

Use a synthetic OAuth/export server or an injected authenticated transport;
never use real Drive credentials merely to run a test. Verify an initial fetch,
refresh after a changed document, unchanged content, invalid UTF-8, oversized
response, timeout, non-2xx response, token refresh, cancellation and recovery.
Check parallel reads cannot observe a partial update. If persistence is used,
verify restart and document-configuration changes. Test static QA entries with
missing optional fields, alternate questions and Unicode without silent loss.

Fresh Code QA must inspect authentication, source ownership, bounds, redaction,
concurrency and lifecycle. Separate source-blind Functional QA must demonstrate
through the Telegram-like UI that answers can retrieve both source types, see
updated data, and retain the last good version during a temporary source failure.
Use content/credential canaries to prove logs and unrelated private memories do
not acquire source content. Report synthetic-provider limitations explicitly.

## Runtime and retrieval

Core `api` and combined `app` start the source loader; the split bot reads the
existing authenticated Core memory APIs. Source settings are optional and owned
by Core. Configure `assistant_sources.static_qa_path`, `about_document` and
`credentials`, or their `ZNS_ASSISTANT_SOURCES__...` equivalents. Credentials
contain the original base64 service-account JSON and use `config.Secret`.
Mount the existing `static/rag_data.yaml` read-only and set its container path;
the loader does not treat an archived migration resource as converted input.
Do not put credentials in an image or expose them to the bot/model process.

Static QA loads once at startup. Drive exports `text/markdown` immediately and
hourly, using `google.JWTConfigFromJSON` and `drive.readonly`. The OAuth library
owns JWT signing and token exchange. Both token/export requests use the fetch's
30-second context and shutdown cancellation; redirects are refused. Endpoint
selection is fixed in production. Tests inject a transport without live Google
credentials or network requests. The existing CPU-only Docker image includes CA
certificates and requires no embedding service or GPU.

Source-managed rows use separate tables and `source_kind: source`. They are
visible through shared memory summary/index/search/read under topics
`assistant_qa` and `assistant_about`. They cannot replace curated facts or private
documents, even when a curated record has the same topic/key. QA ordering is
preserved in stable entry/segment keys; optional questions, alternate questions
and answers remain intact. About text retains the Python Markdown unescape rule.

Input and rendered content are limited to 4 MiB per source, with at most 10,000
QA entries. An oversized resource fails explicitly and retains the previous
version. Content is never silently cut off. Storage divides long entries into
ordered segments of at most 16,000 Unicode characters. Follow all index pages,
then each segment's existing 2,000-character memory-read continuations for exact
reconstruction. Search remains bounded and can return incomplete pages; follow
their cursors. A literal spanning a storage-segment boundary is not a match in
either segment, so exact full-source inspection must use index/read.

Every changed source is published atomically with a new monotonic version,
original-source SHA-256, rendered-text SHA-256 and capture time. Identical upstream
and rendered input refreshes freshness without inventing another revision. A
corrected converter publishes a new revision even when upstream bytes stay the
same. Current exact reads
include provenance and `ready`/`stale` status. Internal source status distinguishes
initial unavailability from a failed refresh retaining the last good version;
logs contain only source labels, counts, byte sizes and finite failure codes.

Cache identity binds the static file's absolute path or the Drive document ID
and credential digest. Startup applies identity selection before consumers run.
Changing/removing configuration immediately removes old content from current
navigation and rejects its retained exact/history references. In-flight writes
from the previous identity cannot publish. Historical revisions are accessible
only for keys belonging to the currently configured source; they are explicitly
historical and do not substitute for current reads.

## Migration and rollback

Migration 057 only adds source-owned tables. It preserves all existing shared and
private data. Older binaries ignore these tables, so rolling back the binary
does not require a destructive schema rollback. Source records become visible
when the configured loader publishes them, not merely when the schema exists.
Disabling the source configuration in the new binary hides it without deleting
its stored revisions. Core serializes each publication/identity switch with a
source row lock; readers see complete committed snapshots. Running Core replicas
must use the same source configuration.

## Dependency evidence

`go list -m -json golang.org/x/oauth2@latest` resolved v0.37.0 on 2026-09-26,
published 2026-08-25; this exact version is pinned. The official
[Google OAuth package](https://pkg.go.dev/golang.org/x/oauth2/google#JWTConfigFromJSON)
supports the required service-account configuration. The existing YAML library
handles static QA; no new framework or Google API client service is needed for
the single export endpoint.

OAuth2 v0.37.0 is BSD-3-Clause. Its added Google metadata dependency v0.9.0 is
Apache-2.0; both downloaded module licenses were inspected. Retain copyright,
license and applicable notices in distributions. The metadata library is linked
through Google's package; this implementation never selects ambient credentials
or invokes instance metadata discovery. The
[Google API Go client manifest](https://github.com/googleapis/google-api-go-client/blob/main/go.mod)
uses OAuth2 v0.36.0 and metadata v0.9.0, establishing downstream use, not validation
of this exact OAuth2 patch or our security boundary. No popularity metric is used
as adoption evidence. The repository-pinned `govulncheck` reported no
vulnerabilities for `./internal/assistantsource/...` on 2026-09-26. This checks
known reachable advisories, not correctness of the application boundary.

## Builder verification

Frozen candidate21 is based on candidate19 plus only this source slice. Its full
pinned lint gate reports zero issues. Focused tests against real PostgreSQL pass
for full Unicode retrieval, static ordering, unchanged/changed revisions,
last-good retention, source-identity changes (including old history references),
curated/private isolation and parallel atomic publication. Synthetic OAuth tests
cover token exchange/renewal, read-only scope, invalid UTF-8, response limits,
timeout/cancellation, failure recovery and content/credential log canaries.
Focused vet, module verification and the CPU-only production image build pass.
The final frozen source passes Linux race tests for the source package and
real-PostgreSQL source integration tests. Independent review and
Telegram-like functional acceptance remain separate required gates.
