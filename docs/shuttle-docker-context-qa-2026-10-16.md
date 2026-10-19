# Docker build-context acceptance, 2026-10-16

The Go release policy in deploy/shuttle/Dockerfile.dockerignore now includes only
Go module files,
cmd/internal source trees, migrations, LICENSE, runtime dependency notices and
avatar dependency notices. No Go code, Dockerfile, dependency or running service
changed. The owned stand still uses its accepted schema38 image3b447e5.

The earlier image build succeeded and contained all three avatar notices despite
no explicit avatar subtree in the old ignore file. A bounded copy-context canary
reproduced the cause: !docs/ admitted an unrelated synthetic document as well as
both notice subtrees. Root and .cache canaries were already excluded. This was an
over-broad context rule, not an observed missing-license or build failure.

The corrected policy removes ancestor-directory negations and explicitly admits
the two notice subtrees. Docker-specific ignore semantics are documented in the
[official context reference](https://docs.docker.com/build/concepts/context/).

Verification used a dedicated Linux BuildKit builder limited to512 MiB RAM,
no additional swap and one CPU, with a scratch COPY-context export, without running Go or accessing a production service. The
baseline contained only synthetic project files, public notices and harmless
canaries. The candidate selected all719 payload paths from the accepted image
build's721-input manifest. A scratch Dockerfile and corrected ignore policy served
as build controls. COPY exported exactly those719 payload paths with matching
hashes; all three canaries were excluded. This checks
all source, embedded assets, migrations and licenses required by the actual
release layout, rather than only an invented subset.

Evidence: .cache/shuttle-work/docker-context-canary-qa/final-proof.json and
the baseline-final/candidate-final exports and logs. A final bounded run retained
the inspected memory/swap/CPU limits omitted from the original checkpoint, and
verified all ten main-service fingerprints unchanged. The original checkpoint.json,
baseline-export and candidate-export remain intact. The original source policy
is retained as Dockerfile.dockerignore.before. The exact own builder/cache was
removed without OOM; no extra image or runtime container was retained. Canary
exports contain only the deliberately selected public release inputs and harmless
synthetic markers; no real secret was used.

No full image rebuild or Go test rerun is claimed for this config-only correction.
The prior component/runtime receipts describe their original frozen inputs; the
new ignore-file hash is established by this independent context-export check.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
