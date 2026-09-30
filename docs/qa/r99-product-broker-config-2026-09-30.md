# R99 product fixture media accounting configuration

The new R96 stand's media broker exited with code 1 and the message
`paid media broker requires accounting database`. Product Compose supplied a
synthetic provider credential but omitted the required accounting connection.

The base product Compose now supplies `DATABASE_URL` using the existing
`zns_meter` role. The isolated R96 overlay points that connection to its own
database. Decoder services receive no database or provider credentials. No
accounting, enforcement, schema, or application behavior was changed.

Fresh independent Code QA175 found no issues in the complete two-file diff and
verified existing role grants and layered configuration. Root validated the
resolved R96 configuration and recreated only its media broker. The broker
remained running with exit code 0; app and fake health endpoints returned 200.
The old R71/R64 stands were not modified. This proves startup configuration in
the isolated stand, not audio transcription or accounting Functional acceptance.

Before/final snapshots and hashes are preserved under
`qa.local/go-resume-20260929/r99-product-broker/`. Resolved configuration and
startup evidence are under `qa.local/r96-functional-20260930/`.

Written by Codex (gpt-6-astra/Codex)
on behalf of Daniel Drizhuk
