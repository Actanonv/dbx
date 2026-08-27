package dbx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTransact_ConcurrentWorkersCommitAndRollback(t *testing.T) {
	db := setupTestDB(t)
	totalWorkers := 40
	commitCount := int32(0)
	rollbackCount := int32(0)

	var wg sync.WaitGroup
	errCh := make(chan error, totalWorkers)

	for i := 0; i < totalWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			tx, err := NewTransact(context.Background(), db)
			if err != nil {
				errCh <- fmt.Errorf("NewTransact failed: %w", err)
				return
			}

			// Even workers commit, odd workers return error (rollback)
			shouldCommit := (workerID % 2) == 0

			tErr := tx.Transaction(nil, func(ctx context.Context) error {
				_, err := tx.Db().ExecContext(ctx, "INSERT INTO items(name) VALUES (?)", fmt.Sprintf("worker-%d", workerID))
				if err != nil {
					return err
				}
				if !shouldCommit {
					return errors.New("simulated business error to rollback")
				}
				return nil
			})

			if shouldCommit {
				if tErr != nil {
					errCh <- fmt.Errorf("expected worker %d commit, got err: %w", workerID, tErr)
				} else {
					atomic.AddInt32(&commitCount, 1)
				}
			} else {
				if tErr == nil {
					errCh <- fmt.Errorf("expected worker %d rollback error, got nil", workerID)
				} else {
					atomic.AddInt32(&rollbackCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("worker error: %v", err)
	}

	if commitCount != int32(totalWorkers/2) {
		t.Fatalf("expected %d commits, got %d", totalWorkers/2, commitCount)
	}

	finalCount := countItems(t, db)
	if finalCount != int(commitCount) {
		t.Fatalf("database item count (%d) does not match commit count (%d)", finalCount, commitCount)
	}
}

func TestTransact_DeeplyNestedSavepointsUnderConcurrency(t *testing.T) {
	db := setupTestDB(t)
	totalWorkers := 15

	var wg sync.WaitGroup
	errCh := make(chan error, totalWorkers)

	for i := 0; i < totalWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			tx, err := NewTransact(context.Background(), db)
			if err != nil {
				errCh <- err
				return
			}

			// 3 levels of nesting:
			// Outer (L1): inserts "worker-X-L1" (commits)
			//   Inner (L2): inserts "worker-X-L2" (rolls back)
			//     Inner (L3): inserts "worker-X-L3" (commits within L2)
			// Result: only "worker-X-L1" should be committed per worker.
			err = tx.Transaction(nil, func(ctx context.Context) error {
				insertItem(t, tx.Db(), fmt.Sprintf("worker-%d-L1", workerID))

				// Nested L2
				_ = tx.Transaction(nil, func(ctx context.Context) error {
					insertItem(t, tx.Db(), fmt.Sprintf("worker-%d-L2", workerID))

					// Nested L3
					_ = tx.Transaction(nil, func(ctx context.Context) error {
						insertItem(t, tx.Db(), fmt.Sprintf("worker-%d-L3", workerID))
						return nil
					})

					return errors.New("rollback L2 and L3")
				})

				return nil
			})

			if err != nil {
				errCh <- fmt.Errorf("outer transaction failed: %w", err)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("nested worker error: %v", err)
	}

	finalCount := countItems(t, db)
	if finalCount != totalWorkers {
		t.Fatalf("expected exactly %d items (only L1 committed), got %d", totalWorkers, finalCount)
	}
}

func TestTransact_ContextCancellationDuringTx(t *testing.T) {
	db := setupTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	tx, err := NewTransact(ctx, db)
	if err != nil {
		t.Fatalf("NewTransact failed: %v", err)
	}

	err = tx.Transaction(nil, func(txCtx context.Context) error {
		insertItem(t, tx.Db(), "pre-cancel")
		// Wait for context cancellation
		select {
		case <-txCtx.Done():
			return txCtx.Err()
		case <-time.After(500 * time.Millisecond):
			return nil
		}
	})

	if err == nil {
		t.Fatal("expected error due to context cancellation, got nil")
	}

	// Verify rollback happened
	if count := countItems(t, db); count != 0 {
		t.Fatalf("expected 0 items after context cancellation rollback, got %d", count)
	}
}

func TestTransact_PanicRecoveryInConcurrentWorkers(t *testing.T) {
	db := setupTestDB(t)
	totalWorkers := 20

	var wg sync.WaitGroup
	var successfulCommits int32

	for i := 0; i < totalWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			tx, err := NewTransact(context.Background(), db)
			if err != nil {
				return
			}

			shouldPanic := (workerID % 2) != 0

			tErr := tx.Transaction(nil, func(ctx context.Context) error {
				insertItem(t, tx.Db(), fmt.Sprintf("worker-panic-%d", workerID))
				if shouldPanic {
					panic("intentional panic in worker")
				}
				return nil
			})

			if !shouldPanic && tErr == nil {
				atomic.AddInt32(&successfulCommits, 1)
			}
		}(i)
	}

	wg.Wait()

	if successfulCommits != int32(totalWorkers/2) {
		t.Fatalf("expected %d non-panicking commits, got %d", totalWorkers/2, successfulCommits)
	}

	if count := countItems(t, db); count != int(successfulCommits) {
		t.Fatalf("database item count (%d) does not match successful non-panicking commits (%d)", count, successfulCommits)
	}
}

func TestTransact_CtxMethod(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.WithValue(context.Background(), struct{}{}, "test_val")
	tx, err := NewTransact(ctx, db)
	if err != nil {
		t.Fatalf("NewTransact failed: %v", err)
	}

	if tx.Ctx() != ctx {
		t.Errorf("Ctx() returned wrong context")
	}
}
