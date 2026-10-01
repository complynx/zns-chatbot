# Current worker acknowledgment — local QA build rule

Root read the complete current docs/code-quality.md, including the new Windows
Firewall section. The user-authored edit remains separate from root's changes.

Explicit full-file read acknowledgments received from:

- c_registration_developer
- clock_authority_developer
- menu_retirement_fix
- e_import_developer
- fqa_lead
- stand_engineer
- kanban_manager
- d_admission_code_qa_281
- payment_code_qa_282
- fqa_current_flows_a (fresh independent reviewer, before execution)
- fqa_current_recovery_b (fresh independent reviewer, before execution)
- profile_callback_ack_developer (new defect owner, before investigation)

e_probe_guard_developer has no initialized assignment. The instruction was queued
for acknowledgment before any activation; no execution has been assigned to it.
Future fresh reviewers must read the current rule before execution as well.

Native Windows executable outputs and networked Go test/run GOTMPDIR must use an
owned path under the repository tree. The AppID allow applies only to Private
networks. Explicit blocks override it. External worktrees and Python/Node runtimes,
Docker and WSL networking are outside this rule. Firewall configuration does not
prove Functional QA. Existing owned native processes can restart only in an idle
coordinated window; no occupied or frozen stand was restarted for this update.

Completed pure-unit root process54178 was observed to terminate successfully;
it was not restarted. This notification does not allocate a fresh Code QA number,
repeat accepted reviews or modify firewall policy.

Written by root (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
