package writers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestWriteReturnsContextErrorAfterAutomaticRollback(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"cancelled", context.Canceled},
		{"expired", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := openStore(t, Options{})
			var ctx context.Context
			var cancel context.CancelFunc
			if tc.want == context.Canceled {
				ctx, cancel = context.WithCancel(context.Background())
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			}
			defer cancel()
			err := store.Write(ctx, func(tx *sql.Tx) error {
				if err := insertItem(tx, 0, 0); err != nil {
					return err
				}
				if tc.want == context.Canceled {
					cancel()
				}
				select {
				case <-ctx.Done():
				case <-time.After(2 * time.Second):
					return errors.New("context did not expire")
				}
				// database/sql's rollback goroutine races Commit. Wait for it to win,
				// so the regression exercises ErrTxDone rather than the other outcome.
				until := time.Now().Add(time.Second)
				for time.Now().Before(until) {
					_, err := tx.Exec("SELECT 1")
					if errors.Is(err, sql.ErrTxDone) {
						return nil
					}
					if err != nil {
						return fmt.Errorf("rollback probe: %w", err)
					}
					time.Sleep(time.Millisecond)
				}
				return errors.New("automatic rollback did not finish")
			})
			if !errors.Is(err, tc.want) {
				t.Errorf("Write returned %v, want %v", err, tc.want)
			}
			if n := countItems(t, store.Reader); n != 0 {
				t.Errorf("rolled-back rows = %d, want 0", n)
			}
		})
	}
}
