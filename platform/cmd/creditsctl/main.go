// creditsctl is an operator-only JSON interface. Database credentials and the
// actor flag belong to a trusted operator, never a model or uploaded document.
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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

const operatorTimeout = 30 * time.Second
const maxOperatorInput = 65536

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "credit operation failed:", err)
		os.Exit(1)
	}
}

func run() error {
	actor := flag.String("actor", "", "current administrator identity, supplied by trusted operator")
	report := flag.String("report-month", "", "UTC month YYYY-MM for aggregate report")
	cursor := flag.String("cursor", "", "aggregate continuation cursor")
	flag.Parse()
	if *actor == "" || flag.NArg() != 0 {
		return errors.New("actor required; mutation JSON is read from stdin")
	}
	ctx, cancel := context.WithTimeout(context.Background(), operatorTimeout)
	defer cancel()
	db, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("accounting database unavailable")
	}
	defer db.Close()
	service := credits.Service{DB: db, Enforce: true}
	encoder := json.NewEncoder(os.Stdout)
	if *report != "" {
		period, parseErr := time.Parse("2006-01", *report)
		if parseErr != nil {
			return errors.New("invalid UTC report month")
		}
		page, readErr := service.Aggregate(ctx, *actor, period, *cursor)
		if readErr != nil {
			return errors.New("aggregate report unavailable")
		}
		return encoder.Encode(page)
	}
	var change credits.OperatorChange
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, maxOperatorInput+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&change) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) {
		return credits.ErrInvalid
	}
	if err = service.Operate(ctx, *actor, change); err != nil {
		return errors.New("operator change rejected; verify authority, immutable key, observed state and evidence")
	}
	return encoder.Encode(map[string]bool{"saved": true})
}
