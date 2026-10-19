# Avatar operator UI QA, 2026-10-19

## Scope

Schema59 / manifest mvp68 source only. No primary installation, new schema or
manifest revision. The operator form uses the existing native administrator API.
The primary remains schema49/mvp56 and the retained image candidate is mvp65.

## Behavioral and browser evidence

All225 UI behavioral tests pass, including18 focused avatar-operator tests.
Coverage includes explicit retry/dismiss, original canonical proposal discovery,
response loss before/after commit, reload without replacement POSTs, explicit
same-operation confirmation after a request never arrived, storage failure,
corrupt/unreadable identifiers, current role loss, account-switch late success
and late401 fences, pagination, en/ru, safe text and expiry after an idle form.
The new suite is included in the existing CI command. No CI run was published.

The final isolated Linux API run passes19 root tests, including the new embedded
asset path, cache headers and existing opt-in/public-asset boundary. Its source
freeze is unchanged. The2GiB/no-swap,2CPU SDK container was removed without OOM.

Owned headless Edge passed12 checks with the shipped operator HTML/module/CSS
and a localhost mock HTTP backend. It exercised explicit retry and dismiss,
original agent-origin proposal confirmation, immutable-result/current-state
separation, lost prepare/confirm responses followed by reload, idle expiry,
role loss and a390px Russian viewport. Four repair effects were committed across
nine POST attempts: one initial retry, two dismiss scenarios and one agent-origin
dismiss. Repeated transport attempts used the same operation; reloads added no
POST. Browser and HTTP server were closed. English and Russian screenshots were
inspected; neither injected HTML nor horizontal overflow occurred.

These are fixture/browser and source tests. The browser backend does not prove
real IAM, Loom, worker, broker approval or Telegram WebView behavior. Previously
accepted native HTTP/transaction tests remain separate. The next gate is a new
candidate image and real-IAM/Loom both-direction repair handoff plus graceful
restart on an owned isolated stand.

## Retained failures and corrections

The first browser fault injection dropped only one response. Chromium retried the
same POST after disconnect, so the intended unknown-result assertion timed out.
There was no second repair effect. The corrected fixture drops all responses for
that request until the explicit read-back stage. The failed log/checkpoint is
retained; it is not counted as a pass.

A Linux run started before the final UI edit passed its19 tests but failed the
source-freeze check because the source changed during the run. It is superseded,
not accepted. The final unchanged-source run passed and removed its container.

Cross-checking the runtime contract exposed a source/fixture error: the form used
unknown/failed/blocked instead of the API's delivery_unknown/delivery_failed/
delivery_blocked. Earlier mock-only passes were insufficient. The source and
fixtures now use the real enums; all three repairable states have behavioral
coverage, and final browser/Linux evidence follows this correction.

A later browser probe read an already visible local-expiry message before the
server recovery finished. The observer now waits for the completed read-back's
hidden confirmation control. This failed checkpoint is retained separately.

The first scoped lint reported goimports errors caused by CRLF in two edited Go
files. Formatter output was proved text-identical after LF normalization. The
final scoped API lint reports0 issues; the final Linux run uses these bytes.

Review found one actionable usability issue: a form left open past expiry silently
rejected confirmation without explaining why. The guard now renders the expiry
explanation. Both a focused behavioral test and headless browser check cover it.

Local evidence lives in the ignored avatar-admin-ui-v1-qa directory under
.cache/shuttle-work: node-state-contract-final.log, linux-api-v4.jsonl and its checkpoint,
browser-v6-checkpoint.json, screenshots, review disposition and source-gate.json.
Historical failed and superseded runs are retained. No production readiness,
actual-runtime handoff or automatic resend acceptance is claimed here.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
