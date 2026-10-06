# Linux acceptance operator

This is a trusted synthetic-stand operator, not a sandboxed product tool. Code QA
must accept this boundary before its Docker socket is admitted. Docker socket
access can control the daemon; resource flags do not restrict its API authority.
Use only the ROOT-pinned daemon b2697dd0-5f0f-47ba-b248-15cb1ee4e1a4 and an
independently approved owned project. Never expose the socket to source helpers.

## Host dispatch and resources

Use cached Linux image
sha256:8a1a0aa958483ecef53dbf2aab6d60b85365c0a8268f00ffb7e7d0877edde916.
The thin Windows host only starts the trusted operator; all checks and Docker CLI
calls execute on Linux. Exact dispatch is bound by a future ROOT technical grant:

```text
docker --context desktop-linux run --name <owned-operator-name>
  --cpus 0.1 --memory 128m --memory-swap 128m --pids-limit 64
  --network none --read-only --cap-drop ALL --security-opt no-new-privileges
  --restart no --no-healthcheck --user 0:0
  --mount type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock
  --mount type=bind,src=<pinned-runner-source>,dst=/runner,readonly
  --mount type=bind,src=<pinned-public-inputs>,dst=/input,readonly
  --mount type=bind,src=<owned-private-output>,dst=/output
  --tmpfs /tmp:rw,noexec,nosuid,size=16m
  --entrypoint python3 <pinned-image> -B /runner/run.py
  --approval /input/approval.json --approval-sha256 <raw-sha256>
  --output /output/<fresh-outcome-directory>
```

The literal socket mount must be verified against this daemon before admission;
its Linux-host source is not a Windows path. Root UID is needed for the socket,
not permission to change foreign resources. Mounts/output/name/source/argv and
operator image are fixed in the admission. No package install, pull, new VM,
production operation, image build or arbitrary Docker command is supported.

One monotonic plus Unix origin is anchored before inputs. These two prepared
source/preflight outcomes preserve one original90 active +30 cleanup window.
Ordinary native controls are bounded15 including physical capture, combined128KiB.
Only the existing D test attachment uses the remaining original90 active window.
Failure is sticky. Raw channels are registered before independent durable writes.
File content and newly owned receipt names are synchronized with Linux fsync;
unsupported or failed directory sync is a task failure, not a portability promise.
Temporary receipts are not a power-loss recovery framework or user-data durability.
The earliest task fault and independent later native/operator faults are retained.
Reap/actual EOF and task success are separate. Unknown physical custody keeps the
owning host/handles strictly passive, without commands or byte pumping; ROOT must
explicitly admit this risk. Terminal storage faults cannot produce success.
Publication checks both original clocks before and after writes/synchronization.
Native acceptance also checks its original per-call cutoff after both raw channels
have been independently retained. The immutable terminal is a pre-publication
snapshot: pass is null and eligible_before_publication is not final acceptance.
Only actual exit0 after the final publication cutoff check accepts the outcome;
late publication fails without rewriting a previously saved receipt.

## Current consumers

- `C_RENDER`: read-only actual Compose rendering and daemon identity. Returns
  private raw rendered JSON for ROOT binding, never lifecycle readiness.
- `C_CREATED_PREFLIGHT`: exact pinned source/rendered config and explicit project
  service set, including dependencies. Creates but never starts the real graph;
  verifies current image, actual Config/HostConfig/Mounts/security/resources and
  never-started state; compares the complete render and native security/resource/
  argv/env/source projection before CREATE. Declared networks/volumes must be
  fresh, exact owned identities and approved driver/options. Removes only
  individually validated born containers, then unused born networks/volumes,
  proving ID/name/resource absence. No generic compose down or reused resource.
  CREATE reads only the exclusively saved validated normalized JSON, with dollar
  values escaped for Compose's second interpolation; authored includes/env inputs
  are not reopened. Isolated network_mode none is allowed only with no networks
  and exact native none. Decimal rendered memory becomes strictly typed native
  bytes. Native Env is complete key/value meaning, not list order. Mount authority
  is exact Type, volume Name or bind Source, Destination and RW; engine-assigned
  volume mountpoints remain raw observations, not invented prebirth values.
  Compose null entrypoint/command inherit the actual cached image defaults;
  explicit empty arrays clear them. The network contract also binds the complete
  published port map, including each loopback HostIp and HostPort.
  Each service's network_contract declares mode owned or none, the sorted complete
  owned network names and identical allowed primary names. Compare the complete
  native NetworkSettings network-name set and require NetworkMode within it;
  an equal-priority primary is engine-selected, never guessed. No extra/foreign
  network is accepted. Mode none declares no owned/allowed networks and requires
  native mode none with no connectivity (empty or intrinsic none network record).
  Absent network/volume options allow only null or an empty map; nonempty options
  retain complete exact typed equality.
- `D_MAINTENANCE_CONTROLS`: unchanged actual-main six tests from maintenance3,
  one c777 .2CPU256 UID1000 source helper, networknone/rootRO/no caps/four public
  RO file binds/no socket. Local image inspection and explicit pullnever precede
  CREATE. Trusted immutable helper argv hashes the actual four mounted files
  before and after running unchanged actual-main; operator-side paths alone are
  not source identity. Exact six-control marker, exit0, zero skipped and mounted
  byte proof are mandatory.
  Native Tmpfs must be the exact admitted bounded /tmp mount, not an image default.

The no-socket source control can also consume a hash-pinned captured twelve-role
render, native plan, actual cached image defaults and a recorded D constructor.
It calls the same plan/render/isolation validators and emits only a scope marker
or controlled field names. Captured generations are not current installation or
daemon delivery evidence. Private environment values remain in their RO inputs.

Approval is a separate immutable ROOT record consumed by raw hash. It declares
`authority`, `operation`, `passive_native_custody_allowed=true`, `project`,
`daemon_id`; C adds `compose_file`, `compose_sha256`, and after genuine render
`owner`, `rendered_config_sha256`, `services`, `inactive_services`, `networks`,
`volumes`, `delivery_files`, `delivery_directories`, and
`no_host_source_writers=true`. Every delivery file declares its exact daemon-visible
`path`, integer `bytes` and `sha256`; directory entries declare the same `path`
plus sorted relative `members` and `directories`. All directory members must have
file byte entries, including the complete fourteen private inputs, config sources
and current binary. Regular no-link/single-link file identity and complete topology
are checked before birth and after cleanup under the original dual clock.
The trusted operator must have exact ROOT-approved RO mounts at these identical
daemon-visible paths. That external physical delivery/writer exclusion is mandatory:
an operator path hash alone is not a daemon byte-readback claim. No private file
contents enter reports. The four maintenance
profile services are explicitly inactive and never CREATE targets or dependencies.
Every active service has exact `name`, image digest, complete `rendered` object
and typed `invariants` for Config user/argv/Env, all native Mounts and the mandatory
HostConfig fields listed in the source. Per-role PG writable root/default caps
and managed runtime rootRO/capdropALL are distinct admitted behavior, not helper
defaults applied to every service. Network/volume entries each bind complete
`rendered`, `name`, `driver`, `options`; networks also bind `internal`.
C graph including operator is <=2CPU4GiB,
ports only127.0.0.1, no external foreign networks/volumes. D adds exactly four
`inputs` with `target`, `operator_path`, `daemon_source`, `sha256`; no private
maintenance capsule is opened by these tests. Caller bindings are private inputs,
not author approval or installed product claims.

## Mandatory acceptance still open

C preflight is not installation/F03 acceptance. Current36264 binary/source,
original six managed roles, genuine clock authority, data/config/private roles,
EN/RU Telegram-like UI and both independent Code/Functional gates remain required.
E original360 before/STOP/after, original178-table/sequence full read-only baseline,
history/data continuity/security and mandatory full/final checks remain required.
No old failed packet is relabelled as PASS. E runtime is not implemented here.
