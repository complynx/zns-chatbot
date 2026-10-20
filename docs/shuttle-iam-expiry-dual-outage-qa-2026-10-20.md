# IAM expiry and simultaneous Loom outage QA, 2026-10-20

Status: accepted on an isolated owned stand using real Zitadel and Loom,
synthetic Telegram and a controlled model provider. The current local primary
remains unchanged. This is a bounded recovery gate, not production acceptance.

## Runtime and natural expiry

The tested bot is the installed payment-consent image
`sha256:9ea106cda26f7d0e58674de3dfd611c35655711b08783914f17c2b2bf7b9a68a`,
manifest `zinusya/mvp-78`, schema64. The gate uses the frozen owned Loom image;
no developer checkout or stand is changed.

Only the disposable Zitadel instance receives a 120-second access-token
lifetime. All four OIDC settings are read before the update; the other three
values are preserved, and the resulting settings are read back. The API is
[UpdateOIDCSettings](https://zitadel.com/docs/reference/api/admin/zitadel.admin.v1.AdminService.UpdateOIDCSettings);
[Zitadel's lifetime guide](https://help.zitadel.com/configure-oidc-token-lifetimes-in-zitadel)
requires all four fields in that update. The gate verifies the resulting actual
Loom authorization-session expiry rather than accepting configuration alone.
It does not separately decode the JWT in this controller.

Four fixture users first authenticate through signed Mini App bootstrap. After
the lifetime update, the bot restarts to clear its token cache. A leader's
read-only profile request creates one actual pending Loom run and auth session.
Both upstreams then stop while that session is still valid: its actual expiry
is 94.456 seconds after the recorded both-stopped observation. Host clocks and
stored expiration values are not changed.

After natural expiry is observed in the Loom database, the bot stops and starts
while both upstreams remain unavailable. A bootstrapped follower and a new
Telegram user each submit one message. All three original tasks are observed in
persisted `awaiting_authorization` state before either upstream returns. There
is still exactly one Loom run, and no profile change has occurred.

## Recovery and verified bindings

The same IAM and Loom containers start again. The controlled provider performs
one actual `zinusya_profile.get` tool call per request and supplies one final
reply per user. The original task, run and auth-session IDs remain unchanged;
the original session version advances from1 to2. The two queued messages create
their own runs and complete, including automatic IAM provisioning for the new
user. Exactly three Loom runs and all accepted tasks complete; the outbox drains.

For each recovered request, the gate checks the Telegram user and Shuttle user,
verified synthetic email read from real IAM, unique subject, task identity,
auth-session subject/organization/actor, run owner/organization/actor, and the
run document's actual auth-session ID. The transcript independently confirms
one final bot reply per user, with no copy of that text in another chat.
No profile row, pass or payment review is created. This proves read-only tool
continuation, not financial mutation recovery or natural-language interpretation.

The two bot stop commands take0.828s and0.859s, including Docker CLI overhead.
These are healthy `docker stop --time 5` observations with exit0, not matched
SIGTERM-to-exit timing or a forced-kill/busy-worker acceptance claim.

## Original failure, reviews and cleanup

The first attempt fails in a reused QA observer before the outage phase. The
observer treats the default all-zero `target_task_id` as a real target while the
original `run_id` has not appeared yet. Its target lookup returns no row. The
message is not resent; the actual Loom run already created by the application
is preserved in that attempt's archives. No product defect is established.

The failed attempt's scripts, finding, transcript state and logs remain retained.
A fresh controller and stand wait for the original run receipt and handle the
zero-UUID sentinel. Fresh ordinary and adversarial reviews finish clean after
strengthening identity/session checks and global reply routing. The original
failure is not rewritten as a successful result.

Each stand's eight writers stop before three separate database archives are
captured and fully decoded with `pg_restore --file=/dev/null`. Both nine-service
stands are removed with their exact owned containers, volumes and networks.
Six archives are retained overall. This is archive validation, not a tested
restore or atomic cross-database backup. Final custody verifies the primary's
eleven services, data, queues and134 Loom runs and their contents remain
preserved; source hashes and the empty Git index are unchanged.

## Evidence and remaining scope

Ignored local evidence is under `.cache/shuttle-work/iam-expiry-runtime-v1/`
for the original failure and `iam-expiry-runtime-v2/` for accepted execution:
`checkpoint.json`, `runtime-acceptance.json`, `manual-observation.json`,
`telegram-transcript.private.json`, `cleanup-proof.json` and `final-readback.json`.
The final reviewed controller/cleanup hashes are in `iam-expiry-review-v2.json`.
Four focused identity/Loom tests separately pass in
`iam-expiry-investigation-tests-v1.jsonl`.

Long multi-hour outages, expired business approvals, revocation races, uncertain
reauthorization receipts, forced kill, unattended model interpretation and real
Telegram WebView acceptance are separate gates. This run changes no product
code, primary configuration or production service. Joint CA/zitadeltg browser
acceptance and the unresolved meal-finance policy remain pending.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
