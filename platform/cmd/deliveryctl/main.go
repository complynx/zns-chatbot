// deliveryctl is a trusted local operator interface, never an agent tool.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

const (
	maxInput           = 4096
	inspectCommand     = "inspect"
	operationTimeout   = 30 * time.Second
	resolutionFallback = 5 * time.Second
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(
			os.Stderr,
			"delivery operation rejected; verify operator authority, joined sender, exact attempt and evidence",
		)
		os.Exit(1)
	}
}

func decode(input io.Reader, out any) error {
	raw, err := io.ReadAll(io.LimitReader(input, maxInput+1))
	if err != nil || len(raw) > maxInput || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return botdelivery.ErrBinding
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) {
		return botdelivery.ErrBinding
	}
	return nil
}

func run(args []string, input io.Reader, output io.Writer) error {
	if len(args) == 0 || (args[0] != inspectCommand && args[0] != "resolve") {
		return botdelivery.ErrBinding
	}
	flags := flag.NewFlagSet("deliveryctl", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	actor := flags.String("actor", "", "trusted current global operator identity")
	botID := flags.Int64("bot-id", 0, "exact configured bot namespace")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *actor == "" || *botID <= 0 {
		return botdelivery.ErrBinding
	}
	var key botdelivery.IntentKey
	var resolution botdelivery.Resolution
	if args[0] == inspectCommand {
		if err := decode(input, &key); err != nil {
			return err
		}
	} else if err := decode(input, &resolution); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	db, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return botdelivery.ErrBinding
	}
	defer db.Close()
	service := botdelivery.Service{
		DB: db,
		Delivery: delivery.Settings{
			BotID:        *botID,
			BotInterval:  time.Second,
			ChatInterval: time.Second,
			Fallback:     resolutionFallback,
		},
	}
	var result botdelivery.Inspection
	if args[0] == inspectCommand {
		result, err = service.Inspect(ctx, *actor, key)
	} else {
		result, err = service.Resolve(ctx, *actor, resolution)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}
