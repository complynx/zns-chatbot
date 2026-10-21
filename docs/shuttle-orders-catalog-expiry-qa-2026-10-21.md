# Orders catalog consent expiry, 2026-10-21

`ChangeOrdersCatalog` checked exact-proposal expiry before a fresh authority
check. If successful IAM verification crossed the deadline, the first catalog
write still succeeded. This affected both initial publication and replacement.

The store now resamples PostgreSQL time after authority verification and rejects
an expired proposal before changing the catalog or recording success. Existing
successful-operation replay remains before expiry checks. The change matches
the meal catalog's existing rule; it adds no schema or API change.

## Bounded evidence

- The new behavioral test fails on the original implementation for both creation
  and replacement: the call succeeds after expiry instead of returning conflict.
- With the fix, Linux/PostgreSQL tests under the race detector pass 10 roots /
  14 cases, with no failures or skips. The gate includes ordinary catalog edits,
  historical receipt replay, authority checks, meal expiry and capacity limits.
- The new cases verify authority verification was entered while consent was
  valid, wait for the database deadline, then require conflict, an unchanged or
  absent catalog and no successful operation receipt.
- Independent source review of the fix and behavioral test found no actionable
  findings. This is static review, separate from the test result.
- The original failure, frozen sources, JSON test events and fully decoded
  PostgreSQL archive are retained under
  `.cache/shuttle-work/orders-catalog-expiry-v1/`. Both terminal runners,
  PostgreSQL, network and volume were removed; no OOM was observed. The owned
  primary's domain data, queue, Loom state and runtime epochs were preserved.

This gate uses PostgreSQL and an injected authority callback. It does not prove
real IAM latency, model behavior or Telegram interaction. Remote CI and local
primary installation of this fix remain separate gates. Production is unchanged.
