// Command agent-log exports bounded metadata from an operator-owned JSON log.
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

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, output, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("agent-log", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("file", "", "Operator-owned JSONL log file")
	since := flags.String("since", "", "Inclusive RFC3339 timestamp")
	until := flags.String("until", "", "Exclusive RFC3339 timestamp")
	offset := flags.Int64("offset", 0, "Byte cursor from previous manifest in the same unchanged file")
	const defaultLimit = 100
	limit := flags.Int("limit", defaultLimit, "Maximum records, 1..1000")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *path == "" {
		return errors.New("usage: agent-log -file LOG [-since RFC3339] [-until RFC3339] [-offset N] [-limit N]")
	}
	options := observability.AgentExportOptions{Offset: *offset, Limit: *limit}
	var err error
	if *since != "" {
		options.Since, err = time.Parse(time.RFC3339, *since)
		if err != nil {
			return errors.New("invalid since timestamp")
		}
	}
	if *until != "" {
		options.Until, err = time.Parse(time.RFC3339, *until)
		if err != nil {
			return errors.New("invalid until timestamp")
		}
	}
	file, err := os.Open(*path)
	if err != nil {
		return errors.New("cannot open diagnostic log")
	}
	defer func() { _ = file.Close() }()
	page, err := observability.ExportAgentLog(context.Background(), file, output, options)
	if encodeErr := json.NewEncoder(diagnostics).Encode(page); encodeErr != nil {
		return errors.New("cannot write export manifest")
	}
	return err
}
