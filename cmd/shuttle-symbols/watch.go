package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/symboldescribe"
)

const descriptorPoll = 2 * time.Second

// watch reuses the one-entry procedure and its exact journal. Only one entry can
// have active work here; errors retain a blocked journal instead of starting a replacement run.
func watch(a arguments) error {
	if a.journal == "" || a.kind != "" || a.id != "" {
		return errors.New("watch requires a private journal root and no kind/id")
	}
	if err := os.MkdirAll(a.journal, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(a.journal, "worker.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("descriptor worker owned or stale lock requires reconciliation")
	}
	defer func() { _ = lock.Close(); _ = os.Remove(lock.Name()) }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	d, err := connect(ctx, a)
	if err != nil {
		return err
	}
	defer d.store.Close()
	return watchQueue(ctx, a, d, runOnce, descriptorPoll)
}

type descriptorQueue struct {
	active *db.SymbolRecord
	cursor uuid.UUID
}

func watchQueue(ctx context.Context, a arguments, d dependencies,
	step func(context.Context, arguments) error, interval time.Duration,
) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	active, err := restoreActive(a.journal)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	queue := descriptorQueue{active: active}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err = queue.tick(ctx, a, d, step); err != nil && ctx.Err() == nil {
				return err
			}
		}
	}
}

func (q *descriptorQueue) tick(ctx context.Context, a arguments, d dependencies,
	step func(context.Context, arguments) error,
) error {
	if q.active == nil {
		var err error
		q.active, q.cursor, err = nextPending(ctx, d, a.journal, q.cursor)
		if err != nil {
			return err
		}
		if q.active == nil {
			return nil
		}
		if err = saveActive(a.journal, *q.active); err != nil {
			return err
		}
	}
	done, err := advanceEntry(ctx, a, *q.active, step)
	if err != nil {
		return err
	}
	if done {
		if err = os.Remove(filepath.Join(a.journal, "active.json")); err != nil {
			return err
		}
		q.active = nil
	}
	return nil
}

func nextPending(ctx context.Context, d dependencies, root string, cursor uuid.UUID) (
	*db.SymbolRecord, uuid.UUID, error,
) {
	entries, err := d.store.PendingSymbols(ctx, d.bot, cursor)
	if err != nil {
		return nil, cursor, err
	}
	if len(entries) == 0 {
		return nil, uuid.Nil, nil
	}
	for _, entry := range entries {
		cursor = entry.Generation
		_, statErr := os.Stat(filepath.Join(root, entry.Generation.String(), "blocked.json"))
		if statErr == nil {
			continue
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return nil, cursor, statErr
		}
		return &entry, cursor, nil
	}
	return nil, cursor, nil
}

func advanceEntry(ctx context.Context, a arguments, entry db.SymbolRecord,
	step func(context.Context, arguments) error,
) (bool, error) {
	next := a
	next.kind, next.id = entry.Reference.Kind, entry.Reference.ID
	next.generation = entry.Generation
	next.journal = filepath.Join(a.journal, entry.Generation.String())
	stepErr := step(ctx, next)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if stepErr != nil {
		// Never start a replacement for an uncertain mutation or rejected result.
		if err := markBlocked(next.journal); err != nil {
			return false, err
		}
		fmt.Fprintln(os.Stderr, "descriptor entry requires reconciliation; original journal retained")
		return true, nil
	}
	state, err := descriptorState(next.journal)
	return state.Phase == descriptorDonePhase, err
}

// The pointer is durable before any external effect, including when the catalog
// entry is evicted before a graceful restart. It never adopts a new generation.
func saveActive(root string, entry db.SymbolRecord) error {
	return save(filepath.Join(root, "active.json"), symboldescribe.State{
		Generation: entry.Generation, Reference: entry.Reference,
	})
}

func restoreActive(root string) (*db.SymbolRecord, error) {
	raw, err := os.ReadFile(filepath.Join(root, "active.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	var state symboldescribe.State
	if json.Unmarshal(raw, &state) != nil || state.Generation == uuid.Nil || !state.Reference.Valid() {
		return nil, errors.New("invalid active descriptor pointer; reconcile before restart")
	}
	if _, err = os.Stat(filepath.Join(root, state.Generation.String(), "blocked.json")); err == nil {
		if err = os.Remove(filepath.Join(root, "active.json")); err != nil {
			return nil, err
		}
		return nil, os.ErrNotExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return &db.SymbolRecord{Generation: state.Generation, Reference: state.Reference}, nil
}

func descriptorState(directory string) (symboldescribe.State, error) {
	var state symboldescribe.State
	raw, err := os.ReadFile(filepath.Join(directory, "journal.json"))
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(raw, &state)
	return state, err
}

func markBlocked(directory string) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(directory, "blocked.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("{\"requires_reconciliation\":true}\n")
	if err != nil {
		return err
	}
	return f.Sync()
}
