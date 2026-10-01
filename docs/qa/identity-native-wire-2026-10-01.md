# Actual local identity wire checks

Product source: 7ff6659bcc8095b17cd41bb34f678e486470bf00. Root ran the
unchanged TestZitadelLocalAdapter and TestSDKLocalZitadelProvisioning against
the explicitly approved synthetic Zitadel v4.16.3 at localhost:8113. Both
tests were selected without changing their assertions or three-minute budget.
No production or external account was addressed.

The guarded dedicated-org bootstrap and subsequent read-only readiness passed.
The actual actor has only ORG_END_USER_IMPERSONATOR and the provisioner only
ORG_USER_MANAGER in the dedicated synthetic organization. They are distinct
from the bootstrap identity and from bot/API clients. Old mistakenly scoped
machine memberships were removed; their objects, secrets and archived state
remain retained. Prior security-policy values were copied by the setup script,
but no pre-change live snapshot exists to independently prove preservation.

## Results

- TestSDKLocalZitadelProvisioning: PASS, 2.130s; package PASS, 2.318s. It used
  the setup administrator PAT. This proves the real SDK/API create/read,
  metadata, duplicate and cleanup path for its own random test user. It does
  not prove least-privileged provisioner-machine authorization.
- TestZitadelLocalAdapter: FAIL, 0.200s; package FAIL, 0.467s. Alice and Bob
  exchange/verification assertions completed before the failure. At line65,
  exchanging `nonexistent-synthetic` returned ErrZitadelUnavailable while the
  live test expected ErrZitadelIdentity. Remaining negative configuration cases
  were not executed after that fatal assertion. This is not full adapter PASS.

The product deliberately classifies unknown OAuth/configuration responses as
retryable unavailable and recognizes one exact inactive-user response. Root
subsequently ran the frozen bounded diagnostic script E41A1FC143391AD206C2B74542173E791AEE0A448AA377E692A70D5BF0AB73D9.
Actual positive grants/exchange/introspection returned HTTP200 with active,
subject and actor checks true. Missing-subject exchange and wrong audience
exchange returned HTTP400/invalid_request without the exact inactive-user
response. Wrong actor credentials returned HTTP400/invalid_request; wrong API
credentials returned HTTP400/unauthorized_client. These remain unavailable by
the current deliberate retry policy. Invalid-token introspection returned
HTTP200 with active=false; altered actor identity mismatches the valid observed
claim. These two cases remain invalid identity.

This evidence confirms stale blanket live-test expectations, rather than a
justification to broaden production rejection handling. A one-file test
correction is assigned in codex/identity-live-contract-fix. It retains both
positive subjects and independently runs every negative case with fresh clients.
It still needs developer gates, actual wire rerun and fresh independent review.

Root terminal process: 82316, exit1. Ignored raw test stream:
`qa.local/identity-prerequisites-20261001/root-native-proof/tests.jsonl`, SHA256
A42DC459937EA73E6C76FC3F621E84F7B9AEE3A913C1716A65B291C84694FCBB.
Readiness receipt SHA256:
1341B6E35950C7C9358D0AEFDFAE77021911B57D822D72F82DD3FFC21CA7044F.
Private credentials were injected only into process-local paths/environment;
the test stream and public report contain no credential export.

Still open: full adapter negatives, scoped provisioner proof, current runtime
identity mapping and first-contact convergence, real browser authorization,
availability/cache/revocation behavior and independent Functional QA.

Written by root (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
