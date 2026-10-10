package writers

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// oneBatch returns a queue whose batch only closes when n ops have arrived, so
// a test that submits n ops concurrently knows they share one transaction.
func oneBatch(t *testing.T, store *Store, n int) *Queue {
	t.Helper()
	q := NewQueue(store, QueueOptions{MaxBatch: n, MaxWait: time.Minute})
	t.Cleanup(func() { _ = q.Close() })
	return q
}

// submitAll submits fns concurrently and returns their errors in order.
func submitAll(q *Queue, fns []func(*sql.Tx) error) []error {
	errs := make([]error, len(fns))
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Go(func() { errs[i] = q.Submit(context.Background(), fn) })
	}
	wg.Wait()
	return errs
}

func TestQueueSavepointIsolation(t *testing.T) {
	store := openStore(t, Options{})
	q := oneBatch(t, store, 64)

	boom := errors.New("boom")
	var mu sync.Mutex
	txs := map[*sql.Tx]bool{}
	fns := make([]func(*sql.Tx) error, 64)
	for i := range fns {
		fns[i] = func(tx *sql.Tx) error {
			mu.Lock()
			txs[tx] = true
			mu.Unlock()
			if err := insertItem(tx, 1, i); err != nil {
				return err
			}
			if i == 17 {
				return boom // after its insert: the savepoint must undo the row
			}
			return nil
		}
	}
	errs := submitAll(q, fns)

	if len(txs) != 1 {
		t.Fatalf("ops ran in %d transactions, want one batch", len(txs))
	}
	for i, err := range errs {
		if i == 17 && !errors.Is(err, boom) {
			t.Errorf("op 17: got %v, want boom", err)
		}
		if i != 17 && err != nil {
			t.Errorf("op %d: %v", i, err)
		}
	}
	if n := countItems(t, store.Reader); n != 63 {
		t.Fatalf("rows = %d, want 63", n)
	}
	var failed int
	err := store.Reader.QueryRow("SELECT count(*) FROM items WHERE seq = 17").Scan(&failed)
	if err != nil || failed != 0 {
		t.Fatalf("failing op's row: count = %d, err = %v; want 0", failed, err)
	}
}

func TestQueuePanicIsolation(t *testing.T) {
	store := openStore(t, Options{})
	q := oneBatch(t, store, 8)

	fns := make([]func(*sql.Tx) error, 8)
	for i := range fns {
		fns[i] = func(tx *sql.Tx) error {
			if err := insertItem(tx, 1, i); err != nil {
				return err
			}
			if i == 3 {
				panic("kaboom")
			}
			return nil
		}
	}
	errs := submitAll(q, fns)
	for i, err := range errs {
		if i == 3 && (err == nil || !strings.Contains(err.Error(), "op panicked: kaboom")) {
			t.Errorf("op 3: got %v, want a panic error", err)
		}
		if i != 3 && err != nil {
			t.Errorf("op %d: %v", i, err)
		}
	}
	if n := countItems(t, store.Reader); n != 7 {
		t.Fatalf("rows = %d, want 7", n)
	}

	// The queue goroutine survived.
	for i := range fns {
		fns[i] = func(tx *sql.Tx) error { return insertItem(tx, 2, i) }
	}
	errs = submitAll(q, fns)
	if err := errors.Join(errs...); err != nil {
		t.Fatalf("after panic: %v", err)
	}
	if n := countItems(t, store.Reader); n != 15 {
		t.Fatalf("rows = %d, want 15", n)
	}
}

// TestQueueBatchFailure kills the batch transaction from inside an op. Every
// op of the batch must fail and none may be applied.
func TestQueueBatchFailure(t *testing.T) {
	store := openStore(t, Options{})
	q := oneBatch(t, store, 4)

	fns := make([]func(*sql.Tx) error, 4)
	for i := range fns {
		fns[i] = func(tx *sql.Tx) error {
			if err := insertItem(tx, 1, i); err != nil {
				return err
			}
			if i == 2 {
				_, err := tx.Exec("ROLLBACK") // what an interrupted write does to the transaction
				return err
			}
			return nil
		}
	}
	for i, err := range submitAll(q, fns) {
		if err == nil || !strings.Contains(err.Error(), "batch failed") {
			t.Errorf("op %d: got %v, want a batch failure", i, err)
		}
	}
	if n := countItems(t, store.Reader); n != 0 {
		t.Fatalf("rows = %d, want 0", n)
	}

	// The writer connection is clean and the queue still works.
	for i := range fns {
		fns[i] = func(tx *sql.Tx) error { return insertItem(tx, 2, i) }
	}
	if err := errors.Join(submitAll(q, fns)...); err != nil {
		t.Fatalf("after batch failure: %v", err)
	}
	if n := countItems(t, store.Reader); n != 4 {
		t.Fatalf("rows = %d, want 4", n)
	}
}

// blockQueue submits an op that parks the queue goroutine inside a batch until
// release is called. Ops submitted afterwards stay queued.
func blockQueue(t *testing.T, q *Queue) (release func(), done <-chan error) {
	t.Helper()
	started, gate, res := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		res <- q.Submit(context.Background(), func(tx *sql.Tx) error {
			close(started)
			<-gate
			return insertItem(tx, 0, 0)
		})
	}()
	<-started
	return sync.OnceFunc(func() { close(gate) }), res
}

func TestQueueCancelWhileQueued(t *testing.T) {
	store := openStore(t, Options{})
	q := NewQueue(store, QueueOptions{})
	release, blocker := blockQueue(t, q)
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	ran := false
	res := make(chan error, 1)
	go func() {
		res <- q.Submit(ctx, func(tx *sql.Tx) error {
			ran = true
			return insertItem(tx, 1, 1)
		})
	}()
	for len(q.ops) != 1 { // wait until the op really is queued
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-res:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Submit did not return while its op was still queued")
	}

	release()
	if err := <-blocker; err != nil {
		t.Fatal(err)
	}
	if err := q.Close(); err != nil { // drains: the cancelled op is reached and skipped
		t.Fatal(err)
	}
	if ran {
		t.Fatal("cancelled op was executed")
	}
	if n := countItems(t, store.Reader); n != 1 {
		t.Fatalf("rows = %d, want 1 (the blocker only)", n)
	}
}

// TestQueueCancelAfterStart pins the documented behavior: once an op has
// started, cancelling its context does not abandon it. Submit reports the
// real outcome.
func TestQueueCancelAfterStart(t *testing.T) {
	store := openStore(t, Options{})
	q := NewQueue(store, QueueOptions{})
	defer func() { _ = q.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	started, cancelled := make(chan struct{}), make(chan struct{})
	res := make(chan error, 1)
	go func() {
		res <- q.Submit(ctx, func(tx *sql.Tx) error {
			close(started)
			<-cancelled
			return insertItem(tx, 1, 1)
		})
	}()
	<-started
	cancel()
	time.Sleep(20 * time.Millisecond) // give Submit every chance to bail out early
	close(cancelled)

	if err := <-res; err != nil {
		t.Fatalf("got %v, want nil: the op committed", err)
	}
	if n := countItems(t, store.Reader); n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
}

func TestQueueCloseDrains(t *testing.T) {
	store := openStore(t, Options{})
	q := NewQueue(store, QueueOptions{})
	release, blocker := blockQueue(t, q)
	defer release()

	const queued = 20
	fns := make([]func(*sql.Tx) error, queued)
	for i := range fns {
		fns[i] = func(tx *sql.Tx) error { return insertItem(tx, 1, i) }
	}
	results := make(chan []error, 1)
	go func() { results <- submitAll(q, fns) }()
	for len(q.ops) != queued {
		time.Sleep(time.Millisecond)
	}

	closed := make(chan error, 1)
	go func() { closed <- q.Close() }()
	select {
	case <-closed:
		t.Fatal("Close returned while ops were still queued")
	case <-time.After(20 * time.Millisecond):
	}
	release()

	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if err := <-blocker; err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(<-results...); err != nil {
		t.Fatalf("queued op failed during Close: %v", err)
	}
	if n := countItems(t, store.Reader); n != queued+1 {
		t.Fatalf("rows = %d, want %d", n, queued+1)
	}
}

func TestQueueSubmitAfterClose(t *testing.T) {
	store := openStore(t, Options{})
	q := NewQueue(store, QueueOptions{})
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	if err := q.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	err := q.Submit(context.Background(), func(tx *sql.Tx) error { return insertItem(tx, 1, 1) })
	if !errors.Is(err, ErrQueueClosed) {
		t.Fatalf("got %v, want ErrQueueClosed", err)
	}
	if n := countItems(t, store.Reader); n != 0 {
		t.Fatalf("rows = %d, want 0", n)
	}
}
