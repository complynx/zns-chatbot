package main

import (
	"flag"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
)

func registerLimits(flags *flag.FlagSet, limits *migrate.Limits) {
	flags.IntVar(&limits.Files, "max-files", limits.Files, "maximum files")
	flags.Int64Var(&limits.Records, "max-records", limits.Records, "maximum records")
	flags.Int64Var(&limits.TotalBytes, "max-total-bytes", limits.TotalBytes, "maximum total bytes")
	flags.Int64Var(&limits.FileBytes, "max-file-bytes", limits.FileBytes, "maximum file bytes")
	flags.Int64Var(&limits.BlobBytes, "max-blob-bytes", limits.BlobBytes, "maximum receipt bytes")
	flags.IntVar(&limits.RecordBytes, "max-record-bytes", limits.RecordBytes, "maximum JSONL record bytes")
}
