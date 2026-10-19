# Avatar worker dependency audit — 2026-10-14

The frame renderer is a separate Go executable. The bot imports the small
process/protocol package; it does not import the renderer or its image-processing
library. There is no Python, Torch, Pillow or native image runtime in this path.

`github.com/disintegration/imaging` is pinned to `v1.6.2`. Its public
repository snapshot has about 5.8k stars and declares MIT. It supplies Lanczos
resampling and EXIF orientation, avoiding a custom resampler or TIFF/EXIF parser.
This is an old stable release, not evidence of active release maintenance. The
accepted risk is limited by current Go decoders, explicit JPEG/PNG and pixel
limits, a bounded leaf process, tests and a reachable-vulnerability check.
See the [official repository](https://github.com/disintegration/imaging) and
[pinned source](https://github.com/disintegration/imaging/tree/v1.6.2).

The transitive Go image module is explicitly pinned to
`golang.org/x/image v0.47.0`, rather than retaining imaging's 2019 minimum.
It has the Go project's BSD-3-Clause license. Module selection also advances
`golang.org/x/sys` from `v0.48.0` to `v0.49.0`; its pinned BSD notice is included.
See the [Go image documentation](https://pkg.go.dev/golang.org/x/image@v0.47.0)
and [Go sys source](https://github.com/golang/sys/tree/v0.49.0).

`selection.json` records the exact module versions, official Go proxy origin,
repository activity/popularity snapshot, SPDX labels and SHA-256 of pinned
license texts. `notices/` contains those texts. The Docker image copies them
under `/licenses/avatars` alongside the existing dependency and Go notices.
The copied frame is byte-identical to the project's existing MIT-licensed
`frame/zns_frame.png`; its hash is recorded in renderer provenance and tested.

The existing pinned audit tool `govulncheck v1.8.0` checked both Linux entry
points, `./cmd/zinusya-avatar` and `./cmd/shuttle`, on Go 1.27.2. It exited zero
and reported no findings. `govulncheck-summary.json` records the tool/config and
result; `govulncheck-linux.json.gz` preserves the full output, including Go tool
download diagnostics. Historical OSV records in that stream are not themselves
findings against reachable code. This is a dated scan, not a guarantee against
unknown vulnerabilities.

The supplemental three-module review is reflected in `tools/dependencies.lock.json`.
The imaging commit was verified against the official repository's `v1.6.2` tag;
the Go image/sys commits come from official proxy VCS metadata. The existing CI
checker accepted all 236 resolved module versions and checksums. `go mod download
all` also added checksums for previously locked, unused transitive modules; no
existing checksum was replaced. The original audit date and supplemental audit
reference remain distinct. The test-stand ingress/egress tests now run in CI too;
the current unpublished changes have only local verification, not a new GitHub run.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
