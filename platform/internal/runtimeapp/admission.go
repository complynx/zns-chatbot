// Package runtimeapp owns runtime admission independently of domain transactions.
package runtimeapp

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Role uint8

const (
	API Role = iota + 1
	Bot
	App
)

const (
	admissionNamespace int32 = 918274
	probeInterval            = time.Second
	probeTimeout             = 2 * time.Second
	cleanupTimeout           = 2 * time.Second
)

var (
	ErrRole        = errors.New("invalid runtime admission role")
	ErrUnavailable = errors.New("runtime admission unavailable")
	ErrBusy        = errors.New("runtime role already owned")
	ErrLost        = errors.New("runtime admission lost")
	ErrClose       = errors.New("runtime admission cleanup incomplete")
)

// Admission excludes conflicting roles while its dedicated session survives.
// It does not fence transactions or effects on any other connection.
type Admission struct {
	stop       context.CancelFunc
	done       chan struct{}
	joined     chan struct{}
	err        error // Published before done closes; immutable afterward.
	cleanupErr error // Published before joined closes; immutable afterward.
}

// Acquire uses a copy of a parsed pgx configuration and never borrows a pool
// connection. The context governs startup only: cancellation after success does
// not release ownership. The runtime must Close after its owned work stops.
func Acquire(ctx context.Context, config *pgx.ConnConfig, role Role) (*Admission, error) {
	keys, err := roleKeys(role)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, ErrUnavailable
	}
	conn, err := pgx.ConnectConfig(ctx, config.Copy())
	if err != nil {
		return nil, admissionFailure(ctx, ErrUnavailable)
	}
	for _, key := range keys {
		var locked bool
		if err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1::int,$2::int)", admissionNamespace, key).
			Scan(&locked); err != nil {
			return nil, errors.Join(admissionFailure(ctx, ErrUnavailable), closeAdmissionConnection(conn))
		}
		if !locked {
			return nil, errors.Join(ErrBusy, closeAdmissionConnection(conn))
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, errors.Join(err, closeAdmissionConnection(conn))
	}
	monitor, stop := context.WithCancel(context.Background())
	a := &Admission{stop: stop, done: make(chan struct{}), joined: make(chan struct{})}
	go a.run(monitor, conn)
	return a, nil
}

func roleKeys(role Role) ([]int32, error) {
	switch role {
	case API:
		return []int32{1}, nil
	case Bot:
		return []int32{2}, nil
	case App:
		return []int32{1, 2}, nil
	default:
		return nil, ErrRole
	}
}

// Done reports terminal admission, not completed connection cleanup. Loss is
// reported before pgx finishes its possibly asynchronous cleanup.
func (a *Admission) Done() <-chan struct{} { return a.done }

// Err is nil while active or after an explicit stop, and ErrLost after loss.
// Its value is stable once Done closes. It never contains connection details.
func (a *Admission) Err() error {
	select {
	case <-a.done:
		return a.err
	default:
		return nil
	}
}

// Close stops monitoring and joins pgx cleanup within the caller's deadline.
// Concurrent calls are safe. A canceled caller still initiates cleanup; its
// error does not mean cleanup completed. A later call can join that cleanup.
// Even successful cleanup is not proof that other runtime writers have stopped.
func (a *Admission) Close(ctx context.Context) error {
	select {
	case <-a.joined:
		return errors.Join(a.Err(), a.cleanupErr)
	default:
	}
	a.stop()
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrClose, err, a.Err())
	}
	select {
	case <-a.joined:
		return errors.Join(a.Err(), a.cleanupErr)
	case <-ctx.Done():
		return errors.Join(ErrClose, ctx.Err(), a.Err())
	}
}

func (a *Admission) run(ctx context.Context, conn *pgx.Conn) {
	a.err = monitorAdmission(ctx, conn)
	close(a.done)
	a.cleanupErr = closeAdmissionConnection(conn)
	// A canceled pgx query may return before its bounded asynchronous cleanup.
	// Do not confuse Conn.Close returning with all connection resources joining.
	<-conn.PgConn().CleanupDone()
	close(a.joined)
}

func monitorAdmission(ctx context.Context, conn *pgx.Conn) error {
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			probe, cancel := context.WithTimeout(ctx, probeTimeout)
			err := conn.Ping(probe)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return ErrLost
			}
		}
	}
}

func closeAdmissionConnection(conn *pgx.Conn) error {
	cleanup, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	if err := conn.Close(cleanup); err != nil || cleanup.Err() != nil {
		return ErrClose
	}
	return nil
}

func admissionFailure(ctx context.Context, reason error) error {
	return errors.Join(reason, ctx.Err())
}
