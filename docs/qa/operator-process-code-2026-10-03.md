# Independent Senior Code QA400

Verdict: scoped source PASS for both assigned scopes. No actionable findings.
This verdict is not Functional QA, executed-test, native-admission or runtime PASS.

## Scope and identity

Route: Codex. Reviewer: code_qa_400. Actual model slug is not exposed to this reviewer.
Read-only review used the assigned original requirements and test inventory, the
35 files listed in the assigned source manifest, and only
docs/coordination-process.md versus HEAD b39d559. No author outcomes, gate
receipts, prior reviews, tracking, management files or unrelated dirty files were read.

Before and after review, source-manifest.json SHA256 was
5ae187b723355fd5005146014dbc73e70b8d3ead0f7f42b1064676ebac23e85e.
All 35 listed files matched their manifest SHA256 before and after review.
The nested product manifest has 19 entries, 20 actual files including itself,
and zero byte-hash mismatches.

Before and after review, docs/coordination-process.md SHA256 was
fa89c29e09ae2f7354a0491d56613b7a7b9d30b9bf63d0b1604da2e5973c8376.

Key reviewed source identities:
- diagnostic/run-state-proof.ps1: 176e0568a876dc0c83a200c19b7ac1305c9a900fc634fc69c0f996dce8f1cbb0
- diagnostic/test-custody-diagnostics.ps1: 3d2b7be07c1296fe0fff1ba721239e5159cab584567f34bfe0b45dc8f4deeae6
- tests/test-whole-observer.py: 661f8d0535acc82f7511cb2f5c31c03fefe0b23b0beed360dfc8e1558a3cbf9d

## Source assessment

The controller delta at diagnostic/run-state-proof.ps1:494 adds custody reason,
identity, last operation, primary failure type, helper terminal state and complete
command provenance to the finalization fallback. It preserves the first primary
failure type and retains the separate finalization receipt. The alias branches
fail before helper START, so their captured last operation is preserved through
this fallback. Exact constant reasons, typed mount RW/Type, approved target role
and complete mount hashes remain private-safe. Raw unrelated mount paths and
native exception messages are not added to the receipts.

The actual current/original controller comparison agrees with the supplied diff:
no truth predicate, resource limit, source pin, identity admission, deadline,
cleanup rule or conditional STOP branch changes. The PowerShell harness retains
the four original actual-controller cases and adds an extracted actual-finally
catch case that checks custody, first failure, command provenance and privacy.

Static Python discovery is 10 + 9 + 51 test methods, with no skip declarations.
The fixture change at tests/test-whole-observer.py:21 and :220 preserves a concrete
Linux path class and routes capture_io's already-imported Path through the same
synthetic path adapter. Assertions and test cases are not removed or weakened.
The supplied packet contains the gate supervisor, complete product input closure
and accepted-block reference consumed by these suites.

The new rule at docs/coordination-process.md:70-79 grants a bounded corrective
outcome for new assignments. A successor requires terminal failure, verified
custody release, causally changed frozen inputs and one affected execution under
the original per-candidate limits. It preserves failed evidence and existing
frozen grants, forbids unchanged-source retries and deadline reanchoring, retains
scope/security/ownership/data/approval stop conditions, and leaves runtime
admission and fresh independent final-source review explicit. This is coherent
with the existing Linux-only, independent QA, ownership and deadline rules.

## Limitations

No Docker, SQL, parser invocation, tests or runtime action was executed. Static
inventory is not proof of 70 executed tests or five executed PowerShell cases.
The controller's external operational inputs and live native state were not
qualified; this source packet does not grant their execution. Remaining Linux
gate and Functional/runtime acceptance must be established independently.

Written by code_qa_400 (model unavailable/Codex)
on behalf of Daniel Drizhuk
