# Native model-consumption PostgreSQL gate — 2026-10-01

Immutable product candidate df5199ada7eb192b5f613270b392b24d86ca4b16; base33540321d033328742b743f504fd35ffaba8496d. Root exclusively owned native PostgreSQL58441 and control listener8090 for this gate. Locally generated synthetic credentials remained private and were supplied only through process environment; no ACL change or output of values.

Actual unchanged selector TestModelConsumption(DurableRebootAndDeniedReinstallation|SQLDeadlineDoesNotConfirmOrRewind), count1, parallel1, p1, timeout4m: both cases PASS3.83s/5.78s; package PASS10.16s; zero FAIL/SKIP. Parent authenticated readback before/after passed PostgreSQL17.11, loopback/fixed database, existing three nonprivileged roles and zns_app absent. Sorted DB inventories are identical, including pre-existing retained fixture databases. Own test children were removed.8090 released after terminal session70471 exit0.

Raw evidence: .worktrees/model-interruption-controls/qa.local/root-pg/tests.jsonl; SHA256 A495F5CE0FABD57FEE635FC2694018E2A082DD4C76FCB4B121C74A4359B7A948. before.json and after.json retain secret-free authenticated snapshots. A fresh independent full-code review follows; this gate does not prove business saved plans, effects, user-visible recovery or whole F08 acceptance. Controlled provider reboot uses a new exclusive journal path and preserves the old journal.

Written by root (gpt-6/Codex)
on behalf of Daniel Drizhuk