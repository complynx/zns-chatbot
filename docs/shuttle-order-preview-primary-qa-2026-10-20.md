# Order-review preview primary installation, 2026-10-20

Status: accepted on the owned `shuttle-live-test` stand. Manifest remains
`zinusya/mvp-83` and schema remains 67. This image-only change includes the
[reviewed literal preview](shuttle-order-review-preview.md); it does not change
payment decisions or install new database migrations.

## Source, image and CI

[Run39486263424](https://github.com/complynx/zns-chatbot/actions/runs/39486263424)
at `7fc7bd31` passes 814/814 integration roots with PostgreSQL/race, 347 UI tests,
stand guards, the Telegram simulator, alerts/dashboard checks, pinned tools,
generated SQL, lint and the Linux/container build. All job steps succeed.
The original terminal log is317,203 UTF-8 bytes, SHA-256
`5a579034a2b69701b471843cbffb55b2752d7561ea09f5f4657edaefd82fe959`.
Prior accepted and failed runs retain their own receipts.

The separately packaged installed image is
`sha256:f79dd4609ef3a8cad967fb1b018ffa9064de76bf57a20e33541925db1110f07c`.
Its Linux/amd64 binary is cross-built with Go 1.27.2, CGO disabled, in 19.5 s.
Source hashes remain unchanged. Packaging retains the exact previous runtime
layers/configuration and verifies the binary through a never-started container;
that container is removed. This is separate from the image built by CI.

## Local installation

An independent review finds two installer gaps: incomplete CI-input comparison
and missing complete prepared bot/poller fingerprints. Both are corrected;
fresh review has no actionable findings. The installer requires the same commit
for successful CI, current HEAD and the reviewed build. An actual pre-install
probe rejects the older successful CI at `832c1d7e`. Runtime identities, settings,
configuration hashes and parent custody are pinned.

The original poller and bot stop with exit0. Docker stop command wall times are
0.422 s and 0.656 s respectively. A new 2,980,820-byte PostgreSQL archive is fully
decoded before activation; SHA-256 is
`11d88fb932af409cf535f342813463da0343e46d3867912341da52488ac9f13f`.
No restore or schema migration is performed. Only the bot is recreated; the same
poller is restarted and completes a fresh ingress cycle. Domain data, existing
notice receipt and the contents of 135 Loom runs remain preserved. All 11 services
run without an observed OOM; no extra QA container remains.

The served `order-reviews.js` and `app.css` bytes match the reviewed source.
Readiness, schema 67, manifest identity, a real-IAM account read and an
unauthenticated 401 boundary pass. New custody and read-only observer 35 verify
this installed epoch; the earlier knowledge-installation epoch is retained.

This verifies installed asset identity and integration preservation, alongside
prior [browser presentation evidence](shuttle-order-review-preview.md). It does
not repeat an entire financial workflow, exercise real Telegram WebView, measure
unattended model quality or establish production acceptance. Private receipts are
in `.cache/shuttle-work/order-review-preview-build-v1/`,
`.cache/shuttle-work/order-review-preview-primary-v1/` and
`.cache/shuttle-work/ci-7fc-terminal-v1/`.
