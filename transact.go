package dbx

import (
	"context"
	"database/sql"
	"errors"
	"runtime/debug"

	"fmt"
	"github.com/uptrace/bun"
	"sync"
)

// IDB provides an interface for interacting with databases and transactions seamlessly.
// It allows business logic to execute queries against Db() without needing to know whether
// an active transaction is in progress.
type IDB interface {
	// Db returns the active bun.IDB (either *bun.DB when idle, or bun.Tx when a transaction is active).
	Db() (db bun.IDB)
	// Start begins a transaction or creates a savepoint if a transaction is already active.
	Start(opt *sql.TxOptions) error
	// Commit commits the current transaction level or releases the active savepoint.
	Commit() error
	// Rollback rolls back the current transaction level or reverts to the previous savepoint.
	Rollback() error
	// Transaction executes fn inside a transaction block with automatic commit, rollback, and panic recovery.
	Transaction(opt *sql.TxOptions, fn TransactFunc) (err error)
	// Ctx returns the context associated with this transaction manager.
	Ctx() context.Context
}

var _ IDB = (*Transact)(nil)

// Transact manages database transactions and nested savepoints with automatic rollback and panic recovery.
type Transact struct {
	db     *bun.DB
	tx     bun.Tx
	ctx    context.Context
	active bool
	// stack holds parent transactions when using savepoints for nesting.
	stack  []bun.Tx
	mu     sync.RWMutex
	nested int
}

// NewTransact initializes a new transaction manager bound to the provided context and database instance.
func NewTransact(ctx context.Context, db *bun.DB) (tsx *Transact, err error) {
	if db == nil {
		return nil, errors.New("dbx: NewTransact with nil db")
	}
	tsx = new(Transact)
	tsx.db = db
	tsx.ctx = ctx

	return tsx, nil
}

// Db returns the current active bun.IDB executor.
// If a transaction is active, it returns the current bun.Tx (or savepoint-backed Tx).
// If no transaction is active, it returns the underlying *bun.DB.
func (t *Transact) Db() (db bun.IDB) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.active {
		return t.db
	}
	return t.tx
}

// Ctx returns the context associated with this Transact instance.
func (t *Transact) Ctx() context.Context {
	return t.ctx
}

// Start begins a transaction or creates a new nested savepoint if a transaction is already in progress.
func (t *Transact) Start(opt *sql.TxOptions) error {
	ctx := t.ctx
	t.mu.Lock()
	defer t.mu.Unlock()

	// If a transaction is already active, create a savepoint and switch to it.
	if t.active {
		// Create a savepoint (bun.Tx.BeginTx on a Tx creates a savepoint-backed Tx).
		sp, err := t.tx.BeginTx(ctx, opt)
		if err != nil {
			return err
		}
		// Push current tx to stack and switch active tx to the savepoint.
		t.stack = append(t.stack, t.tx)
		t.tx = sp
		t.nested++
		return nil
	}

	// No active transaction: start a new DB transaction.
	tx, err := t.db.BeginTx(ctx, opt)
	if err != nil {
		return err
	}

	t.tx = tx
	t.active = true
	t.nested = 1
	t.stack = nil

	return nil
}

// Commit commits the current transaction level. If nested inside a savepoint,
// it releases the savepoint and reverts to the parent transaction.
func (t *Transact) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return errors.New("cannot commit: no tx active")
	}

	if t.nested > 1 {
		// Commit current savepoint and revert to parent tx.
		err := t.tx.Commit()
		t.popTx()
		return err
	}

	// Outermost transaction commit.
	err := t.tx.Commit()
	t.tx = bun.Tx{}
	t.active = false
	t.stack = nil
	t.nested = 0
	return err
}

// Rollback rolls back the current transaction level. If nested inside a savepoint,
// it rolls back to the savepoint and reverts to the parent transaction.
func (t *Transact) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return errors.New("cannot rollback: no tx active")
	}

	if t.nested > 1 {
		// Rollback to the current savepoint and revert to parent tx.
		err := t.tx.Rollback()
		t.popTx()
		return err
	}

	// Outermost transaction rollback.
	err := t.tx.Rollback()
	t.tx = bun.Tx{}
	t.active = false
	t.stack = nil
	t.nested = 0
	return err
}

func (t *Transact) popTx() {
	// Pop parent from the stack.
	parentIdx := len(t.stack) - 1
	if parentIdx >= 0 {
		t.tx = t.stack[parentIdx]
		t.stack[parentIdx] = bun.Tx{}
		t.stack = t.stack[:parentIdx]
	} else {
		// Should not happen, but safeguard.
		t.tx = bun.Tx{}
		t.active = false
	}
	t.nested--
}

// TransactFunc is a function executed within a managed transaction block.
type TransactFunc func(ctx context.Context) error

// Transaction runs fn inside a transaction block. If fn returns an error or panics,
// the transaction (or current nested savepoint) is rolled back. If fn returns nil,
// the transaction is committed.
func (t *Transact) Transaction(opt *sql.TxOptions, fn TransactFunc) (err error) {
	ctx := t.ctx
	if err = t.Start(opt); err != nil {
		return err
	}

	committed := false

	defer func() {
		if r := recover(); r != nil {
			func() {
				defer func() { _ = recover() }()
				_ = t.Rollback()
			}()

			stack := debug.Stack()
			err = fmt.Errorf("panic recovered in Transaction: %v\nStack trace:\n%s", r, stack)
			return
		}

		// Handle normal rollback if committed is false and tx is still active
		if !committed {
			t.mu.RLock()
			active := t.active
			t.mu.RUnlock()

			if active {
				rbErr := t.Rollback()
				if rbErr != nil {
					if err != nil {
						err = errors.Join(err, fmt.Errorf("rollback failed: %w", rbErr))
					} else {
						err = rbErr
					}
				}
			}
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
