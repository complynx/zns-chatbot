// shuttle-import inspects local legacy snapshots without applying them.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

const (
	exitInvalidInput    = 2
	verificationTimeout = 30 * time.Second
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, output, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("shuttle-import", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	manifest := flags.String("manifest", "", "Local snapshot manifest path")
	archive := flags.String("output", "", "Create a verified private archive at a new path")
	verify := flags.String("verify", "", "Verify a received archive without importing it")
	digest := flags.String("sha256", "", "Expected lowercase SHA-256 of the received archive")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 ||
		!validMode(*manifest, *archive, *verify, *digest) {
		_, _ = fmt.Fprintln(
			diagnostics,
			"usage: shuttle-import -manifest snapshot.json [-output new-archive.tar.gz] | -verify archive.tar.gz -sha256 digest",
		)
		return exitInvalidInput
	}
	if *verify != "" {
		return verifyArchive(*verify, *digest, output, diagnostics)
	}
	if *archive != "" {
		return createArchive(*manifest, *archive, output, diagnostics)
	}
	report, err := legacyimport.Inspect(*manifest)
	if err != nil {
		_, _ = fmt.Fprintln(diagnostics, "Snapshot manifest cannot be inspected; no data was imported.")
		return exitInvalidInput
	}
	if err = json.NewEncoder(output).Encode(report); err != nil {
		return exitInvalidInput
	}
	if !report.ValidationPassed {
		return 1
	}
	return 0
}

func validMode(manifest, archive, verify, digest string) bool {
	if verify != "" {
		return manifest == "" && archive == "" && digest != ""
	}
	return manifest != "" && digest == ""
}

func verifyArchive(path, digest string, output, diagnostics io.Writer) int {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		_, _ = fmt.Fprintln(diagnostics, "Archive cannot be opened; no data was imported.")
		return exitInvalidInput
	}
	input, err := os.Open(path) // #nosec G304 -- explicit operator-selected local archive.
	if err != nil {
		_, _ = fmt.Fprintln(diagnostics, "Archive cannot be opened; no data was imported.")
		return exitInvalidInput
	}
	defer func() { _ = input.Close() }()
	opened, err := input.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return exitInvalidInput
	}
	ctx, cancel := context.WithTimeout(context.Background(), verificationTimeout)
	defer cancel()
	result, err := legacyimport.ValidateBundle(ctx, input, digest)
	if err != nil {
		_, _ = fmt.Fprintln(diagnostics, "Archive validation failed; no data was imported.")
		return 1
	}
	if err = json.NewEncoder(output).Encode(result); err != nil {
		return exitInvalidInput
	}
	return 0
}

func createArchive(manifest, destination string, output, diagnostics io.Writer) int {
	result, err := legacyimport.CreateBundle(manifest, destination)
	if err != nil {
		if errors.Is(err, legacyimport.ErrArchiveCleanup) {
			if encodeErr := json.NewEncoder(output).Encode(result); encodeErr != nil {
				return exitInvalidInput
			}
			_, _ = fmt.Fprintln(
				diagnostics,
				"Temporary archive cleanup failed; inspect the output directory. The result identifies any published archive. No data was imported.",
			)
			return exitInvalidInput
		}
		_, _ = fmt.Fprintln(diagnostics, "Snapshot archive could not be published; no data was imported.")
		return exitInvalidInput
	}
	if err = json.NewEncoder(output).Encode(result); err != nil {
		return exitInvalidInput
	}
	if !result.Inspection.ValidationPassed {
		return 1
	}
	return 0
}
