# Isolated Loom write QA

Use a fresh synthetic participant and event on an owned disposable fixture. Exercise the application through actual Loom tools rather than inserting the resulting business records directly.

For an owner write, inspect the advertised contract, call the operation with one durable UUID, and read its original receipt. For an administrator write, verify that preparation changes no business state, present complete frozen terms, obtain explicit consent, then execute and reconcile that same operation.

Check canonical JSON argument/precondition hashing, current authority, approval identity, expiry, lease and dispatch fences. Different JSON object key order must preserve the same operation. Numeric spelling normalization is a separate compatibility concern; do not infer arbitrary numeric-lexeme equivalence from object-key canonicalization.

Retain failed attempts and uncertainty before cleanup. Controlled provider responses prove orchestration, not autonomous model judgment. Follow the [general QA procedure](shuttle-qa.md).
