package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/symboldescribe"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const descriptorPoll = 2 * time.Second

// watch reuses each one-entry procedure and its exact journal. Bounded slots
// overlap independent entries; errors block that generation without replacement work.
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
	active      *db.SymbolRecord
	cursor      uuid.UUID
	pointerRoot string
	busy        bool
}

type descriptorResult struct {
	slot int
	done bool
	err  error
}

// Each slot persists its original generation before work starts. Steps for
// different slots overlap; a slot never overlaps itself or replaces its run.
func watchQueue(ctx context.Context, a arguments, d dependencies,
	step func(context.Context, arguments) error, interval time.Duration,
) error {
	limit := a.concurrency
	if limit == 0 {
		limit = 1
	}
	if limit < 1 || limit > 4 {
		return errors.New("descriptor concurrency must be 1-4")
	}
	queues, reserved, err := restoreQueues(a.journal, limit)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	results := make(chan descriptorResult, limit)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case result := <-results:
			if err = finishDescriptor(queues, reserved, result); err != nil && ctx.Err() == nil {
				return err
			}
		case <-ticker.C:
			err = startDescriptors(ctx, a, d, queues, reserved, step, results, &workers)
			if err != nil && ctx.Err() == nil {
				return err
			}
		}
	}
}

func restoreQueues(root string, limit int) ([]descriptorQueue, map[uuid.UUID]bool, error) {
	queues := make([]descriptorQueue, limit)
	reserved := make(map[uuid.UUID]bool)
	for slot := range 4 {
		pointerRoot := root
		if slot > 0 {
			pointerRoot = filepath.Join(root, fmt.Sprintf("parallel-%d", slot))
		}
		active, err := restoreActiveFrom(root, pointerRoot)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		if active != nil {
			if slot >= limit || reserved[active.Generation] {
				return nil, nil, errors.New("active descriptor slots require reconciliation before changing concurrency")
			}
			reserved[active.Generation] = true
		}
		if slot < limit {
			queues[slot] = descriptorQueue{active: active, pointerRoot: pointerRoot}
		}
	}
	return queues, reserved, nil
}

func (q *descriptorQueue) fill(ctx context.Context, a arguments, d dependencies,
	reserved map[uuid.UUID]bool,
) error {
	if q.active != nil {
		return nil
	}
	var err error
	q.active, q.cursor, err = nextPending(ctx, d, a.journal, q.cursor, reserved)
	if err != nil || q.active == nil {
		return err
	}
	if err = os.MkdirAll(q.pointerRoot, 0700); err != nil {
		return err
	}
	if err = saveActive(q.pointerRoot, *q.active); err != nil {
		return err
	}
	reserved[q.active.Generation] = true
	return nil
}

func startDescriptors(ctx context.Context, a arguments, d dependencies,
	queues []descriptorQueue, reserved map[uuid.UUID]bool,
	step func(context.Context, arguments) error, results chan<- descriptorResult, workers *sync.WaitGroup,
) error {
	for slot := range queues {
		q := &queues[slot]
		if q.busy {
			continue
		}
		if err := q.fill(ctx, a, d, reserved); err != nil {
			return err
		}
		if q.active == nil {
			continue
		}
		q.busy = true
		entry := *q.active
		workers.Go(func() {
			done, err := advanceEntry(ctx, a, entry, step)
			results <- descriptorResult{slot: slot, done: done, err: err}
		})
	}
	return nil
}

func finishDescriptor(queues []descriptorQueue, reserved map[uuid.UUID]bool, result descriptorResult) error {
	q := &queues[result.slot]
	q.busy = false
	if result.err != nil {
		return result.err
	}
	if !result.done {
		return nil
	}
	if err := os.Remove(filepath.Join(q.pointerRoot, "active.json")); err != nil {
		return err
	}
	delete(reserved, q.active.Generation)
	q.active = nil
	return nil
}

func nextPending(ctx context.Context, d dependencies, root string, cursor uuid.UUID, reserved map[uuid.UUID]bool) (
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
		if reserved[entry.Generation] {
			continue
		}
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
		if err := markBlocked(next.journal, stepErr); err != nil {
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

func restoreActiveFrom(root, pointerRoot string) (*db.SymbolRecord, error) {
	raw, err := os.ReadFile(filepath.Join(pointerRoot, "active.json"))
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
		if err = os.Remove(filepath.Join(pointerRoot, "active.json")); err != nil {
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

// Retain safe transport diagnostics, never raw errors that may contain credential URLs.
func markBlocked(directory string, cause error) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(directory, "blocked.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = json.NewEncoder(f).Encode(blockedReason(cause)); err != nil {
		return err
	}
	return f.Sync()
}

type blockedFailure struct {
	Reconcile bool   `json:"requires_reconciliation"`
	Type      string `json:"error_type"`
	Status    int    `json:"status,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
}

func blockedReason(cause error) blockedFailure {
	result := blockedFailure{Reconcile: true, Type: "descriptor"}
	var tg *telegram.Error
	var upstream *loom.Error
	switch {
	case errors.As(cause, &tg):
		result.Type, result.Status, result.Outcome = "telegram", tg.StatusCode, string(tg.Outcome)
	case errors.As(cause, &upstream):
		result.Type, result.Status, result.Outcome = "loom", upstream.StatusCode, string(upstream.Outcome)
	case errors.Is(cause, context.DeadlineExceeded):
		result.Type = "deadline"
	}
	return result
}
