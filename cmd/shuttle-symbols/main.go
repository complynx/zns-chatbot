// Shuttle-symbols fills one retained visual description with a dedicated Loom account.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/config"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/symboldescribe"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const procedureTimeout = 5 * time.Minute
const descriptorDonePhase = "done"

type arguments struct {
	config, kind, id, journal, actorToken, subjectToken string
	watch                                               bool
	concurrency                                         int
	generation                                          uuid.UUID
}
type dependencies struct {
	store *db.Store
	flow  symboldescribe.Flow
	bot   int64
	auth  loom.Auth
}
type ownership struct {
	Procedure uuid.UUID `json:"journal_procedure"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parse() arguments {
	var a arguments
	flag.StringVar(&a.config, "config", "config/shuttle.yaml", "Startup YAML")
	flag.StringVar(&a.kind, "kind", "", "sticker or emoji")
	flag.StringVar(&a.id, "id", "", "Unique sticker ID or numeric custom emoji ID")
	flag.StringVar(&a.journal, "journal", "", "Private procedure directory; reuse to observe original work")
	flag.StringVar(&a.actorToken, "actor-token-file", "", "Protected Loom service-account or delegation actor token")
	flag.StringVar(&a.subjectToken, "subject-token-file", "", "Optional protected delegated descriptor token")
	flag.BoolVar(&a.watch, "watch", false, "Process retained cache misses; journal is a private root directory")
	flag.IntVar(&a.concurrency, "concurrency", 4, "Maximum simultaneous description procedures (1-4)")
	flag.Parse()
	return a
}

func connect(ctx context.Context, a arguments) (dependencies, error) {
	var d dependencies
	c, err := config.Load(a.config)
	if err != nil {
		return d, err
	}
	actor, err := token(a.actorToken)
	if err != nil {
		return d, err
	}
	var subject string
	if a.subjectToken != "" {
		subject, err = token(a.subjectToken)
		if err != nil {
			return d, err
		}
	}
	tg, err := telegram.New(c.Telegram.BaseURL, c.Telegram.Token, nil)
	if err != nil {
		return d, err
	}
	lc, err := loom.New(c.Loom.BaseURL, nil)
	if err != nil {
		return d, err
	}
	store, err := db.Open(ctx, c.DatabaseURL)
	if err != nil {
		return d, errors.New("open catalog database")
	}
	return dependencies{store, symboldescribe.Flow{Telegram: tg, Loom: lc}, c.Telegram.BotID,
		loom.Auth{ServiceToken: actor, SubjectToken: subject}}, nil
}

func run() error {
	a := parse()
	if a.watch {
		return watch(a)
	}
	return runOnce(context.Background(), a)
}

func runOnce(parent context.Context, a arguments) error {
	if a.journal == "" || a.kind == "" || a.id == "" {
		return errors.New("kind, id and private journal directory required")
	}
	ctx, cancel := context.WithTimeout(parent, procedureTimeout)
	defer cancel()
	d, err := connect(ctx, a)
	if err != nil {
		return err
	}
	defer d.store.Close()
	return runEntry(ctx, a, d)
}

func runEntry(ctx context.Context, a arguments, d dependencies) error {
	entry, lookupErr := d.store.PendingSymbol(ctx, d.bot, a.kind, a.id)
	statePath := filepath.Join(a.journal, "journal.json")
	s, err := readJournal(a, statePath, entry, lookupErr)
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(a.journal, "procedure.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("procedure owned or requires stale-lock reconciliation")
	}
	defer func() { _ = lock.Close(); _ = os.Remove(lock.Name()) }()
	// Another process may have finished between the first read and this lock.
	// Use only the current journal and catalog receipt while owning the procedure.
	entry, lookupErr = d.store.PendingSymbol(ctx, d.bot, a.kind, a.id)
	s, err = readJournal(a, statePath, entry, lookupErr)
	if err != nil {
		return err
	}
	if err = claim(ctx, d, &s, &entry, lookupErr); err != nil {
		return err
	}
	if err = d.flow.Step(ctx, d.auth, &s, func(v symboldescribe.State) error { return save(statePath, v) }); err != nil {
		return err
	}
	if err = apply(ctx, d, s, entry, lookupErr); err != nil {
		return err
	}
	if !a.watch || s.Phase == descriptorDonePhase {
		fmt.Fprintln(os.Stdout, s.Phase)
	}
	return nil
}

func readJournal(a arguments, path string, entry db.SymbolRecord, lookupErr error) (symboldescribe.State, error) {
	var s symboldescribe.State
	raw, err := os.ReadFile(path)
	if err == nil {
		if json.Unmarshal(raw, &s) != nil || s.Reference.Kind != a.kind || s.Reference.ID != a.id ||
			s.ProcedureID == uuid.Nil || a.generation != uuid.Nil && s.Generation != a.generation {
			return s, errors.New("journal does not match requested symbol")
		}
		return s, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return s, errors.New("cannot read procedure journal")
	}
	if lookupErr != nil || entry.Described {
		return s, errors.New("symbol absent or already described")
	}
	if a.generation != uuid.Nil && a.generation != entry.Generation {
		return s, db.ErrConflict
	}
	if err = os.Mkdir(a.journal, 0700); err != nil {
		return s, errors.New("fresh exclusive private journal directory required")
	}
	s = symboldescribe.State{Generation: entry.Generation, ProcedureID: uuid.New(), Reference: entry.Reference}
	return s, save(path, s)
}

func claim(ctx context.Context, d dependencies, s *symboldescribe.State, r *db.SymbolRecord, lookupErr error) error {
	if lookupErr != nil || r.Generation != s.Generation {
		if s.Phase == "running" || s.Phase == descriptorDonePhase {
			return nil
		}
		return errors.New("catalog changed before run creation; original journal retained")
	}
	var owner ownership
	if err := json.Unmarshal(r.Workflow, &owner); err != nil {
		return err
	}
	if owner.Procedure == s.ProcedureID {
		return nil
	}
	if owner.Procedure != uuid.Nil {
		return errors.New("catalog entry owned by another procedure")
	}
	marker, _ := json.Marshal(ownership{s.ProcedureID})
	if err := d.store.SaveSymbolWorkflow(ctx, d.bot, *r, r.Workflow, marker); err != nil {
		return err
	}
	r.Workflow = marker
	return nil
}

func apply(ctx context.Context, d dependencies, s symboldescribe.State, r db.SymbolRecord, lookupErr error) error {
	if s.Phase != descriptorDonePhase {
		return nil
	}
	if lookupErr != nil || r.Generation != s.Generation {
		return errors.New("run completed; catalog evicted or changed, original journal retained")
	}
	if s.Result == nil {
		return errors.New("completed journal has no description")
	}
	return d.store.CompleteSymbolDescription(ctx, d.bot, r, *s.Result, r.Workflow)
}

func token(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 16384 || len(strings.TrimSpace(string(raw))) == 0 {
		return "", errors.New("cannot read protected access token")
	}
	return strings.TrimSpace(string(raw)), nil
}

func save(path string, s symboldescribe.State) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path+".next", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(path+".next", path)
}
