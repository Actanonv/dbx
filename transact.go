package dbx

import (
	"context"
	"database/sql"
	"errors"

	"fmt"
	"github.com/uptrace/bun"
	"sync"
)

type ListOptions struct {
	Where string
	Args  []any
	Limit int
}

type Transact struct {
	db *bun.DB
	tx *bun.Tx
	// stack holds parent transactions when using savepoints for nesting.
	stack  []*bun.Tx
	mu     sync.RWMutex
	nested int
}

func NewTransact(db *bun.DB) (tx *Transact, err error) {
	if db == nil {
		return nil, errors.New("dbx: NewTransact with nil db")
	}
	tx = new(Transact)
	tx.db = db

	return tx, nil
}

func (t *Transact) Db() (db bun.IDB) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.tx == nil {
		return t.db
	}
	return t.tx
}

func (t *Transact) Start(ctx context.Context, opt *sql.TxOptions) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// If a transaction is already active, create a savepoint and switch to it.
	if t.tx != nil {
		// Create a savepoint (bun.Tx.BeginTx on a Tx creates a savepoint-backed Tx).
		sp, err := t.tx.BeginTx(ctx, opt)
		if err != nil {
			return err
		}
		// Push current tx to stack and switch active tx to the savepoint.
		t.stack = append(t.stack, t.tx)
		t.tx = &sp
		t.nested++
		return nil
	}

	// No active transaction: start a new DB transaction.
	tx, err := t.db.BeginTx(ctx, opt)
	if err != nil {
		return err
	}

	t.tx = &tx
	t.nested = 1
	t.stack = nil

	return nil
}

func (t *Transact) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tx == nil {
		return errors.New("cannot commit: no tx active")
	}

	if t.nested > 1 {
		// Commit current savepoint and revert to parent tx.
		if err := t.tx.Commit(); err != nil {
			return err
		}
		t.popTx()
		return nil
	}

	// Outermost transaction commit.
	if err := t.tx.Commit(); err != nil {
		return err
	}

	t.tx = nil
	t.stack = nil
	t.nested = 0
	return nil
}

func (t *Transact) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tx == nil {
		return errors.New("cannot rollback: no tx active")
	}

	if t.nested > 1 {
		// Rollback to the current savepoint and revert to parent tx.
		if err := t.tx.Rollback(); err != nil {
			return err
		}
		t.popTx()
		return nil
	}

	// Outermost transaction rollback.
	err := t.tx.Rollback()
	t.tx = nil
	t.stack = nil
	t.nested = 0
	return err
}

func (t *Transact) popTx() {
	// Pop parent from the stack.
	parentIdx := len(t.stack) - 1
	if parentIdx >= 0 {
		t.tx = t.stack[parentIdx]
		t.stack[parentIdx] = nil // Avoid memory leak
		t.stack = t.stack[:parentIdx]
	} else {
		// Should not happen, but safeguard.
		t.tx = nil
	}
	t.nested--
}

type TransactFunc func(ctx context.Context) error

func (t *Transact) Transaction(ctx context.Context, opt *sql.TxOptions, fn TransactFunc) (err error) {

	if err = t.Start(ctx, opt); err != nil {
		return err
	}

	var committed bool

	defer func() {
		r := recover()
		if !committed || r != nil {
			rErr := t.Rollback()
			if rErr != nil {
				// If we are already in error or panic, join the rollback error.
				if err != nil {
					err = errors.Join(err, fmt.Errorf("rollback failed: %w", rErr))
				} else if r != nil {
					// If we only have a panic, we might want to log or just ignore rollback error
					// but joining it with nil error doesn't work well if we don't have an error variable.
					// We'll set err so it can be potentially used if we were to return it,
					// but since we re-panic, it's mostly for visibility if someone captures it.
					err = fmt.Errorf("rollback failed during panic: %w", rErr)
				}
			}
		}
		if r != nil {
			panic(r)
		}
	}()

	if fErr := fn(ctx); fErr != nil {
		err = fErr
		return err
	}

	if cErr := t.Commit(); cErr != nil {
		err = fmt.Errorf("failed to commit: %w", cErr)
		return err
	}

	committed = true
	return nil
}
