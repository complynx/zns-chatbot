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

## Corrected candidate wire run

Root ran frozen one-file candidate903a32d0 on the same qualified local instance.
Process31025 terminated exit0: both positive subjects and all six named negatives
completed. Parent0.260s, package1.116s; eight PASS events, no FAIL or SKIP.
Raw ignored successor stream SHA256:
BE798B6D5A854C6A9FDACC7DDB48933764A23DBD4C8EEAC2A66C55F8A639477F.

Fresh Code QA276 separately returned one missing test prerequisite: invalid-token
introspection must first prove successful exchange on that fresh client. The
same-file two-line successor is being validated. Candidate903 is preserved;
the successful wire result does not remove the review finding or establish
current integrated adapter/Functional acceptance.

Root terminal process: 82316, exit1. Ignored raw test stream:
`qa.local/identity-prerequisites-20261001/root-native-proof/tests.jsonl`, SHA256
A42DC459937EA73E6C76FC3F621E84F7B9AEE3A913C1716A65B291C84694FCBB.
Readiness receipt SHA256:
1341B6E35950C7C9358D0AEFDFAE77021911B57D822D72F82DD3FFC21CA7044F.
Private credentials were injected only into process-local paths/environment;
the test stream and public report contain no credential export.

Final successor 1c3bab626f821e416ff46bdb10595c8a8fa5bcf6 passed fresh independent
Code QA278 and the real root adapter run: six negatives plus positive exchanges,
eight PASS events, zero FAIL/SKIP, package0.38s. Raw final stream SHA256
AB4B24A745F633F48583E9C1DA9795D358181307FCA1EE6F1FDC270C12E5A7AC.
Integrated as 6bee90039ee7ce34ffa3ec0208bfe32cec4d1994; exact tested file retained.
See code-qa-2026-10-01-278.md. Earlier failed candidates remain historical evidence.

Still open: scoped provisioner proof, current runtime
identity mapping and first-contact convergence, real browser authorization,
availability/cache/revocation behavior and independent Functional QA.

Written by root (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
