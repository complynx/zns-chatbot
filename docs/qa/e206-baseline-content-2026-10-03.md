# E206 baseline content acceptance successor

Result: PASS, scoped to completeness and internal integrity of the captured baseline content against the independent catalog inventory. This is not E206 import acceptance or Functional38 acceptance.

The unchanged validator completed with zero issues in one explicitly authorized causal launcher successor. It verified Python 3.11.2 and interpreter SHA256 6d972cf21be56fe3c947ab6ba257ff8d08c342dd2714442986791bd9a6dfabfe inside the pinned image before executing validation. The first launch failure remains immutable in report.md and terminal.json. Only the successor launcher executable and its interpreter preflight changed; validator and private inputs remained unchanged.

Verified content:

- Exact independent permanent physical inventory: 178 distinct object fingerprints, 156 tables and 22 sequences. All five schema totals match the prescribed inventory. The two catalog views are excluded.
- 232 complete stored row records: 210 table rows and 22 sequence records. 73 objects are nonempty, including all 22 sequences; 105 tables are empty and have zero-count empty-content fingerprints.
- Every table row contains exactly the nondropped catalog column set, including stored proof/history fields. Every sequence has exactly one record containing integer last_value, integer log_cnt and Boolean is_called.
- Every fingerprint count matches observed records. All 178 SHA256 fingerprints recompute exactly from sorted original row JSON, with UTF8 byte-length prefixes and no normalization or JSON reserialization. Row output also follows the required bytewise C order.
- No unknown sections or non-JSON stdout records. Inventory, all counts, ordering, row fields and fingerprints were checked independently with structured parsing.
- The frozen query hash, query-after hash, terminal query identity, all four terminal file hashes and lengths, size metadata digest and lengths, exit file and terminal completion flags are mutually consistent. The catalog identity also matches the independent assessment.

Verified query SHA256: 4a58efe57b7b11f961a266426eb05c07c09034a141535f3734637a3b84f3b732.
Verified baseline stdout SHA256: cb41d9309a1b70debe061fe4ad07b94a6077ba1b761ddae4c50c0fd371c5cb1f, 194397 bytes.
Verified catalog SHA256: 9cdf9196d7ae42aa27090b8323be619e0f1b52b5376f60014517457c0af5b877, 459944 bytes.

Successor helper ID: 0d14983a3b8094da437dc1502a5fd5bcf1851ac3211ad89f9837d78c83cfceb6. Exact Created admission confirmed the approved pinned Linux image, 0.2 CPU, 256 MiB/equal swap, PidsLimit 64, UID 1000, read-only root, drop ALL, no-new-privileges, network none and no socket. Eight exact approved input files and the original validator were mounted read-only; only the owned successor result directory was writable. Docker endpoint remained desktop-linux via the configured docker.exe application. Terminal exit 0, no OOM, start 2026-10-03T16:35:17.144487383Z, finish 2026-10-03T16:35:17.712718625Z. Exact helper removal succeeded, with no active handles. Full checker output is result-successor/summary.json; terminal receipt is successor-terminal.json.

Limitations: this proves supplied capture completeness and integrity against the independent catalog, not business fixture correctness, importer identity preservation or live database authenticity. Installation/generation identity and exclusive snapshot custody are supplied preflight claims. The baseline query uses read-only transactions but does not explicitly establish REPEATABLE READ, so unchanged live state across row and fingerprint queries depends on custody; matching fingerprints show the delivered observations agree. No database/SQL access, imported business counts, UI, EN/RU behavior, ACL, reset or Functional38 flows were exercised. No product/controller implementation, developer handoff or prior QA findings were consulted. No input writes, stand lifecycle operations, production access or external transfer occurred.

Written by e206_baseline_acceptance (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
