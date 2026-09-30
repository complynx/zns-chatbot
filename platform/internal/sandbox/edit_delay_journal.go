package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
)

const delayEvidenceDeadline = time.Second

type delayJournalSink interface {
	io.WriteCloser
	Sync() error
}

type delayJournalOperation struct {
	body []byte
	done chan error
}

// Exactly one finite worker owns all regular-file calls. A stuck syscall can
// strand this worker until process exit, but cannot strand state/control or
// create further workers. Timeout invalidates the case permanently.
type delayJournal struct {
	operations chan delayJournalOperation
	gate       chan struct{}
	deadline   time.Duration
	ended      chan struct{}
}

func newDelayJournal(ctx context.Context, sink delayJournalSink) *delayJournal {
	j := &delayJournal{
		operations: make(chan delayJournalOperation),
		gate:       make(chan struct{}, 1),
		deadline:   delayEvidenceDeadline,
		ended:      make(chan struct{}),
	}
	go func() {
		defer close(j.ended)
		defer func() { _ = sink.Close() }()
		for {
			select {
			case <-ctx.Done():
				return
			case operation := <-j.operations:
				n, err := sink.Write(operation.body)
				if err == nil && n != len(operation.body) {
					err = io.ErrShortWrite
				}
				if err == nil {
					err = sink.Sync()
				}
				operation.done <- err
			}
		}
	}()
	return j
}

func (d *editDelay) invalidate() {
	d.mu.Lock()
	d.state = delayStateInvalidated
	d.mu.Unlock()
	d.invalidOnce.Do(func() { close(d.invalidated) })
}

// No regular-file operation or wait owns the state mutex. The single deadline
// covers gate admission, worker handoff, Write and Sync together.
func (d *editDelay) record(e delayEvent) error {
	j := d.journal
	deadline := time.NewTimer(j.deadline)
	defer deadline.Stop()
	failure := func() error { d.invalidate(); return errors.New("synthetic evidence unavailable or timed out") }
	select {
	case j.gate <- struct{}{}:
	case <-deadline.C:
		return failure()
	case <-d.ctx.Done():
		return failure()
	}
	defer func() { <-j.gate }()
	d.mu.Lock()
	if d.state == delayStateInvalidated || len(d.events) >= 512 {
		d.mu.Unlock()
		return failure()
	}
	e.Sequence, e.TimeNS = len(d.events)+1, time.Now().UnixNano()
	d.mu.Unlock()
	raw, err := json.Marshal(e)
	if err != nil || len(raw) > 512 {
		return failure()
	}
	op := delayJournalOperation{body: append(raw, '\n'), done: make(chan error, 1)}
	select {
	case j.operations <- op:
	case <-deadline.C:
		return failure()
	case <-d.ctx.Done():
		return failure()
	}
	select {
	case err = <-op.done:
		if err != nil {
			return failure()
		}
	case <-deadline.C:
		return failure()
	case <-d.ctx.Done():
		return failure()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state == delayStateInvalidated {
		return errors.New("synthetic case invalidated")
	}
	d.events = append(d.events, e)
	return nil
}
