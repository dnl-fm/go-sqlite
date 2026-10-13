package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ErrQueueClosed is returned by Submit after Close.
var ErrQueueClosed = errors.New("database: queue is closed")

// QueueOptions tunes NewQueue. The zero value is the recommended default.
type QueueOptions struct {
	// MaxBatch is the most ops committed in one transaction. Default 64.
	MaxBatch int
	// MaxWait is how long a batch stays open for more ops after its first one
	// arrives. Default 1ms. It is latency you pay on every write to buy a
	// larger batch: worth it when a commit costs an fsync (synchronous=FULL),
	// a pure tax when it does not.
	MaxWait time.Duration
}

// Queue is a batching writer (group commit). One goroutine owns the writer and
// commits many submitted ops in a single transaction, so N tiny writes pay for
// one commit instead of N.
//
// What the ack means: Submit returns nil only after the batch holding the op
// has committed. How durable a commit is comes from Config.Pragmas["synchronous"] on the
// Database. With NORMAL (default) an acked write survives an application crash
// but the most recent acks can be lost on OS crash or power loss. With FULL
// the commit is fsynced before the ack, so it survives power loss too.
//
// Database.Write still works while a Queue runs: it simply takes its turn on the
// writer connection between batches.
type Queue struct {
	store *Database
	ops   chan *op
	done  chan struct{}
	opts  QueueOptions

	mu     sync.RWMutex // held shared while sending on ops, exclusive to close it
	closed bool
}

const (
	opQueued int32 = iota
	opStarted
	opCancelled
)

type op struct {
	fn    func(*sql.Tx) error
	res   chan error
	state atomic.Int32
}

// NewQueue starts the queue goroutine. Call Close to stop it; Close does not
// close the Database. On an OpenReadOnly store every Submit returns ErrReadOnly.
func NewQueue(store *Database, opts QueueOptions) *Queue {
	if opts.MaxBatch <= 0 {
		opts.MaxBatch = 64
	}
	if opts.MaxWait <= 0 {
		opts.MaxWait = time.Millisecond
	}
	q := &Queue{store: store, opts: opts, ops: make(chan *op, opts.MaxBatch), done: make(chan struct{})}
	go q.loop()
	return q
}

// Submit queues fn and blocks until its batch has committed or failed.
//
// fn runs inside its own SAVEPOINT. If fn returns an error or panics, only
// fn's changes are rolled back and Submit returns that error; the rest of the
// batch still commits. If the batch itself fails (for example the commit),
// every op in it gets the batch error and none of them is applied.
//
// Cancellation: if ctx is done while the op is still queued, the op is never
// executed and Submit returns ctx.Err(). Once the op has started, ctx is no
// longer consulted: Submit waits for the batch and returns the real result, so
// the caller always knows whether the write happened. An op is never
// half-applied.
//
// Inside fn, use only the tx, and use it without a cancellable context.
// SQLite rolls back the whole transaction when a write statement is
// interrupted, which turns one canceled op into a failed batch. Calling
// Submit or Database.Write from inside fn waits forever on the writer connection.
func (q *Queue) Submit(ctx context.Context, fn func(*sql.Tx) error) error {
	if q.store.readOnly {
		return ErrReadOnly
	}
	o := &op{fn: fn, res: make(chan error, 1)}

	q.mu.RLock()
	if q.closed {
		q.mu.RUnlock()
		return ErrQueueClosed
	}
	select {
	case q.ops <- o:
		q.mu.RUnlock()
	case <-ctx.Done():
		q.mu.RUnlock()
		return ctx.Err()
	}

	select {
	case err := <-o.res:
		return err
	case <-ctx.Done():
		if o.state.CompareAndSwap(opQueued, opCancelled) {
			return ctx.Err() // the queue goroutine will skip it
		}
		return <-o.res // already started: report what really happened
	}
}

// Close stops accepting ops, runs everything already queued, and returns once
// the queue goroutine has exited. It is safe to call more than once.
func (q *Queue) Close() error {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.ops)
	}
	q.mu.Unlock()
	<-q.done
	return nil
}

func (q *Queue) loop() {
	defer close(q.done)
	for first := range q.ops {
		batch := []*op{first}
		timer := time.NewTimer(q.opts.MaxWait)
	collect:
		for len(batch) < q.opts.MaxBatch {
			select {
			case o, ok := <-q.ops:
				if !ok {
					break collect
				}
				batch = append(batch, o)
			case <-timer.C:
				break collect
			}
		}
		timer.Stop()
		q.commit(batch)
	}
}

// commit runs one batch in one transaction and delivers every op's result
// only after the outcome of the whole batch is known.
func (q *Queue) commit(batch []*op) {
	live := batch[:0]
	for _, o := range batch {
		if o.state.CompareAndSwap(opQueued, opStarted) {
			live = append(live, o)
		}
	}
	if len(live) == 0 {
		return
	}
	errs := make([]error, len(live))
	err := q.run(live, errs)
	for i, o := range live {
		if err != nil {
			o.res <- fmt.Errorf("database: batch failed: %w", err)
		} else {
			o.res <- errs[i]
		}
	}
}

// run returns a non-nil error only for a batch-level failure. Per-op errors go
// into errs.
func (q *Queue) run(live []*op, errs []error) error {
	// Background, not an op's context: the batch belongs to all of its ops.
	tx, err := q.store.writer.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() //nolint:errcheck // GO-SQLITE-1: best-effort cleanup; no-op after commit.

	for i, o := range live {
		_, err = tx.Exec("SAVEPOINT op")
		if err != nil {
			return err
		}
		errs[i] = call(o.fn, tx)
		if errs[i] != nil {
			_, err = tx.Exec("ROLLBACK TO op")
			if err != nil {
				return errors.Join(errs[i], err)
			}
		}
		_, err = tx.Exec("RELEASE op")
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// call runs fn and turns a panic into an error so one bad op cannot kill the
// queue goroutine.
func call(fn func(*sql.Tx) error, tx *sql.Tx) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("database: op panicked: %v", p)
		}
	}()
	return fn(tx)
}
