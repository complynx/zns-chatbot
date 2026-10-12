package migrations

import "embed"

// Files contains the versioned Shuttle schema.
//
//go:embed *.sql
var Files embed.FS
