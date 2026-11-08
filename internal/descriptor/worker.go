package descriptor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/symboldescribe"
)

// Options shares application dependencies; the worker never owns their lifetime.
type Options struct {
	Store       *db.Store
	Flow        symboldescribe.Flow
	Machine     *identity.MachineTokenSource
	Auth        loom.Auth
	BotID       int64
	Journal     string
	Concurrency int
}

// Worker runs bounded descriptor slots in the application process.
type Worker struct {
	options  Options
	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	failures chan error
	state    string
	blocked  int64
}

// New prepares a worker without opening a pool or starting background work.
func New(o Options) (*Worker, error) {
	if o.Concurrency == 0 {
		o.Concurrency = 4
	}
	if o.Store == nil || o.Flow.Loom == nil || o.Flow.Telegram == nil || o.BotID <= 0 || o.Journal == "" ||
		o.Concurrency < 1 || o.Concurrency > 4 || o.Machine == nil && o.Auth.ServiceToken == "" {
		return nil, errors.New("descriptor dependencies, journal and bounded concurrency required")
	}
	return &Worker{options: o, done: make(chan struct{}), failures: make(chan error, 1), state: "stopped"}, nil
}

// Start acquires journal ownership before returning. Reuse requires a new Worker.
func (w *Worker) Start(parent context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		return errors.New("descriptor already started")
	}
	if err := parent.Err(); err != nil {
		return err
	}
	a := arguments{journal: w.options.Journal, concurrency: w.options.Concurrency,
		machine: w.options.Machine, watch: true, quiet: true, blocked: w.recordBlocked}
	lock, err := lockWorker(a)
	if err != nil {
		w.state = "blocked"
		return err
	}
	w.blocked, err = countBlocked(a.journal)
	if err == nil {
		_, _, err = restoreQueues(a.journal, a.concurrency)
	}
	if err != nil {
		w.state = "blocked"
		releaseWorker(lock)
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	w.state = "running"
	d := dependencies{store: w.options.Store, flow: w.options.Flow, bot: w.options.BotID, auth: w.options.Auth}
	go func() {
		defer close(w.done)
		defer releaseWorker(lock)
		const maxDelay = 5 * time.Minute
		loopErr := superviseDescriptor(ctx, descriptorPoll, maxDelay, w.setState, func() error {
			return watchQueue(ctx, a, d, func(parent context.Context, next arguments) error {
				step, stop := context.WithTimeout(parent, procedureTimeout)
				defer stop()
				return runEntry(step, next, d)
			}, descriptorPoll)
		})
		if loopErr != nil && ctx.Err() == nil {
			w.setState("blocked")
			w.failures <- loopErr
		} else {
			w.setState("stopped")
		}
	}()
	return nil
}

// Stop cancels active work and waits for journal locks and slots to be released.
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	cancel := w.cancel
	w.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-w.done:
		return nil
	default:
	}
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Failures reports fatal worker errors to the application's lifecycle.
func (w *Worker) Failures() <-chan error { return w.failures }

// Snapshot reports control-loop state and retained reconciliation markers, never symbol IDs.
func (w *Worker) Snapshot() (string, int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state, w.blocked
}

func (w *Worker) setState(state string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = state
}

func (w *Worker) recordBlocked() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.blocked++
}

// Only a positively classified catalog read can restart the loop. Journal writes,
// unknown external effects and malformed receipts never enter this retry path.
func superviseDescriptor(ctx context.Context, delay, maxDelay time.Duration,
	state func(string), loop func() error,
) error {
	const multiplier = 2
	for {
		state("running")
		err := loop()
		if err == nil || ctx.Err() != nil {
			return err
		}
		readErr, ok := errors.AsType[*descriptorCatalogReadError](err)
		if !ok || !transientCatalogRead(readErr.error) {
			return err
		}
		state("retrying")
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = min(delay*multiplier, maxDelay)
	}
}

func countBlocked(root string) (int64, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	var count int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		_, err = os.Stat(filepath.Join(root, entry.Name(), "blocked.json"))
		if err == nil {
			count++
		} else if !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
	}
	return count, nil
}

func lockWorker(a arguments) (*os.File, error) {
	if a.journal == "" || a.kind != "" || a.id != "" {
		return nil, errors.New("watch requires a private journal root and no kind/id")
	}
	if err := os.MkdirAll(a.journal, 0700); err != nil {
		return nil, err
	}
	lock, err := journalLock(filepath.Join(a.journal, "worker.lock"))
	if err != nil {
		return nil, errors.New("descriptor worker owned or stale lock requires reconciliation")
	}
	return lock, nil
}

func releaseWorker(lock *os.File) {
	_ = lock.Close()
}
