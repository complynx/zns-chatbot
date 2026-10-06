# Clean C acceptance stand

Preparation only. No native composition, installation or Functional acceptance
is claimed. This stand replaces the old C delivery process, not the product or
the original acceptance requirements.

## Ownership and identity

Project `synthetic-qa-c-current` owns new PostgreSQL data, networks and IPC/clock
volumes. No original C/E database, Docker volume, network or private input is
reused. One developer writes the stand until ROOT releases it to one fresh
Functional reviewer. There is no rebuild or technical writer during that review.

Product base: `36264bb8ef6e1a603cc0c4a0cc7e32b412c12cb8`. Current zns binary:
`488a8accc0f4072236a6b779837a28fa6312bf0769573023271ad85d767e1261`.
App, Fake and maintenance commands bind that actual binary read-only over the
cached base executable. Base-image identity is not current-product identity.
The support operator must verify the executable bytes and native mount before
any start. Worker images remain exact original pinned dependencies, not rebuilt
or silently renamed current product images.

The isolated product's supported fixed fixture guards remain installation
`010400000204`, stand `synthetic-qa-zns-registration-fixture`, database
`synthetic_qa_zns_registration_fixture` and clock case
`c-registration-clock-20261001-v1`. Launch is `c00362642026100500000001`.
These constants authorize nothing outside this new namespace.

The default graph has PostgreSQL, Fake and the six original runtime roles:
app, evaluator, media-decoder, media-broker, sticker-decoder, sticker-broker.
Eight live services use 1.2 CPU / 3200 MiB. Including the trusted operator
(0.2 CPU / 256 MiB) and at most one maintenance tool (0.1 CPU / 256 MiB),
the whole graph is bounded by 1.5 CPU / 3712 MiB, below 2 CPU / 4096 MiB.
Ports 58118 and 58119 bind only 127.0.0.1 and require actual free-port admission.

## Inputs and bootstrap

Compose requires exact immutable cached image references and absolute
daemon-visible source/private/binary paths. `/input` aliases are not daemon bind
sources. The trusted Linux operator alone has the Docker socket. Product and
untrusted qualification containers never receive that socket.

`prepare_private.py` creates fresh random role passwords, shared signing key,
synthetic Telegram token, media and sticker secrets. ROOT first prepares and
admits an empty contained private directory with an exact protected ACL. No
private file is tracked, included in source review, or printed. Its env files use
Compose raw format; the ordinary tool's env has no controlled-clock variables.
Private execution/readback receipts remain private; public evidence contains
only identities, predicates, hashes, counts and release status.

For the current Linux successor, unchanged private bytes are copied into a new
owned native input volume, not consumed through Windows UID1000/mode0600 aliases.
`prepare_private.py --admit-native-copy MANIFEST --private-dir DIRECTORY` admits
the exact fourteen pinned files after the copy. The preparation process uses
UID0 with only supplementary groups70 and10001, dropped capabilities and no
network/socket. It changes ownership groups of its own files without CHOWN or
DAC capabilities. Directory root:10001/mode0750 contains app.env and owner.env
root:10001/mode0640, six password files root:70/mode0640, and six remaining env
files root:0/mode0600. The immutable PostgreSQL17-alpine image's postgres identity
is70:70; this identity must be authenticated against the admitted image.

The installer stays UID0/dropALL. Its exact canonical daemon directory is also
the Compose client alias and the readonly probe source. PostgreSQL receives only
the six explicit RO password-file binds at their original /run/secrets paths;
no Compose secret-target normalization may change those paths.
`C_ACCEPTANCE_PRIVATE_FILE_PROPAGATION` defaults to `rprivate` for host delivery.
The fixed native private-volume installer sets `rslave`, as required for bind
sources under the daemon root. The consumer requires that exact propagation
for the six exact file identities; `rshared` or writable binds are rejected.
The UID10001
probe must read its app/owner files, reject reads of the other twelve private
files, and preserve all five original RO-open checks. UID0, UID10001 and PG70
access, untrusted-user denial, whole source topology and absence of any writable
alias require actual Linux proof before installation admission. This source
preparation does not claim that proof or authorize a product start.

The four bootstrap files are byte-identical copies of the original registration
role contract. PostgreSQL initializes the independent roles once on a new empty
data volume. Start PostgreSQL alone and establish genuine readiness first. Run
the following product commands serially with the `tool` service:

1. `migrate`
2. `fixture`
3. `product-fixture`
4. `product-fixture` with `REGISTRATION_FIXTURE_ACTION=init`, original stand guard,
   opening `2030-10-02T13:00:00Z` and anchor `2030-10-02T12:00:00Z`.
5. `roles` executes the original runtime SQL as the product owner, after checking
   all three fixture markers and actors Alice101, Bob202 and Visitor303.
6. `clock-init` creates the new genuine serialized clock as product owner.
7. `clock-read` reads it as the independent clock operator, with a serialization
   RW mount and no logical clock mutation. App sees the same clock directory RO.

Start Fake only after these steps have installed its schema, rows and grants.
Do not start the six runtime roles before bootstrap and the six prerequisites
below pass. New clock revision and fresh row state are measured, not inferred
from historical C snapshots. Each step has its own actual native profile and
source/auth/data predicates. A failed command stops productive continuation.

## Original Acceptance Mapping

All six original pre-F30 prerequisites remain mandatory:

1. Current source/file proof: immutable base, complete product closure, current
   binary hash and affected source checks; fresh independent Code QA. Existing
   source107+5 and original14+7 assertions remain mapped requirements, not implied
   successes of a new Compose parse.
2. Genuine bounded clock read: original guard constants, serialized RW operator
   access, original anchor/opening and no read mutation; app clock mount RO.
3. Private config/role/row continuity: fresh isolated credentials and original
   role grants, original fixture actors/markers/rows. This successor proves clean
   creation and persistence across its own lifecycle, not old C row continuity.
4. Six complete native prelaunch constructors: actual create/inspect of all six
   runtime roles plus the two prerequisites. Check exact image/executable/argv,
   typed unique env, mount access, UID/capabilities/root RO, no-new-privileges,
   networks, loopback ports and aggregate bounds before any runtime start.
5. Native RO app/owner bind probes: actual current executable/config bytes and
   owner command mount access. Never-started constructors do not establish these
   executable reads or installed/running state.
6. Separately bounded current-provider delivery: new Fake uses the exact current
   binary; genuine model/Telegram/provider reachability and supported behaviors
   are verified independently. A constructor does not establish provider health.

The approved trusted runner preserves one original 90+30 composition window,
native15 and 128 KiB capture bounds, strict EOF/reap and exact owned cleanup.
Prerequisite dispatch carries the authenticated original host window through
the existing UTC/remaining-time clamp before grant intake or output creation;
the runtime phase keeps its separately authenticated same-Linux ROOT anchor.
The product acceptance mapping retains original F30/G600/child22332/outer22344
and readiness90 budgets and the mandatory full/final gate classifications. A new
execution mechanism does not extend or reset those budgets. Controlled product
time covers supported opening/expiry/cooldown scenarios; it does not replace
real full/final checks or their independent QA classification.

Original Functional38-v2 requirements remain in
`qa.local/fqa-active38-20261003-v2/requirements.md`. Full F03 is the first product
milestone, not whole C or remaining37 acceptance. F05-F07/F09-F10 follow only
their own green prerequisites, without waiting on unrelated E/import work.
Fresh Functional QA receives requirements plus an immutable actual access annex
only. It exercises EN/RU Telegram-like messages, keyboards, callbacks, edits,
uploads, retries, old cards, persistence, permissions and failures as required,
with mouse and emulated touch. Physical touch remains explicitly unverified.

## Gates

Real Linux composition and never-started native preflight precede the immutable
source review. That preflight is not installation or product PASS. Developers
finish affected Linux checks, pinned lint/format, a clean local commit and a full
source-only review intake. Fresh Code QA and ROOT exact technical admission
precede bootstrap/runtime. All six prerequisites, actual current installation,
stable source-blind access, reviewer ACK and ROOT RELEASE precede Functional use.
Skipped, pending and historical evidence are never promoted to current PASS.
