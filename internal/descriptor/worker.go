package descriptor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

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
	return &Worker{options: o, done: make(chan struct{}), failures: make(chan error, 1)}, nil
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
		machine: w.options.Machine, watch: true, quiet: true}
	lock, err := lockWorker(a)
	if err != nil {
		return err
	}
	if _, _, err = restoreQueues(a.journal, a.concurrency); err != nil {
		releaseWorker(lock)
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	d := dependencies{store: w.options.Store, flow: w.options.Flow, bot: w.options.BotID, auth: w.options.Auth}
	go func() {
		defer close(w.done)
		defer releaseWorker(lock)
		loopErr := watchQueue(ctx, a, d, func(parent context.Context, next arguments) error {
			step, stop := context.WithTimeout(parent, procedureTimeout)
			defer stop()
			return runEntry(step, next, d)
		}, descriptorPoll)
		if loopErr != nil && ctx.Err() == nil {
			w.failures <- loopErr
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

func lockWorker(a arguments) (*os.File, error) {
	if a.journal == "" || a.kind != "" || a.id != "" {
		return nil, errors.New("watch requires a private journal root and no kind/id")
	}
	if err := os.MkdirAll(a.journal, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(a.journal, "worker.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, errors.New("descriptor worker owned or stale lock requires reconciliation")
	}
	return lock, nil
}

func releaseWorker(lock *os.File) {
	_ = lock.Close()
	_ = os.Remove(lock.Name())
}
