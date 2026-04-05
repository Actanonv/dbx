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

type ListOptions struct {
	Where string
	Args  []any
	Limit int
}

type IDB interface {
	Db() (db bun.IDB)
	Start(opt *sql.TxOptions) error
	Commit() error
	Rollback() error
	Transaction(opt *sql.TxOptions, fn TransactFunc) (err error)
	Ctx() context.Context
}

var _ IDB = (*Transact)()

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

func NewTransact(ctx context.Context, db *bun.DB) (tsx *Transact, err error) {
	if db == nil {
		return nil, errors.New("dbx: NewTransact with nil db")
	}
	tsx = new(Transact)
	tsx.db = db
	tsx.ctx = ctx

	return tsx, nil
}

func (t *Transact) Db() (db bun.IDB) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.active {
		return t.db
	}
	return t.tx
}

func (t *Transact) Ctx() context.Context {
	return t.ctx
}

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

func (t *Transact) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
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

	t.tx = bun.Tx{}
	t.active = false
	t.stack = nil
	t.nested = 0
	return nil
}

func (t *Transact) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
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
		t.stack = t.stack[:parentIdx]
	} else {
		// Should not happen, but safeguard.
		t.tx = bun.Tx{}
		t.active = false
	}
	t.nested--
}

type TransactFunc func(ctx context.Context) error

func (t *Transact) Transaction(opt *sql.TxOptions, fn TransactFunc) (err error) {
	ctx := t.ctx
	if err = t.Start(opt); err != nil {
		return err
	}

	committed := false

	defer func() {
		if r := recover(); r != nil {
			_ = t.Rollback()

			stack := debug.Stack()
			err = fmt.Errorf("panic recovered in Transaction: %v\nStack trace:\n%s", r, stack)
			return
		}

		// Handle normal rollback if committed is false (due to fn() or Commit() error)
		if !committed {
			rbErr := t.Rollback()
			if rbErr != nil {
				if err != nil {
					err = errors.Join(err, fmt.Errorf("rollback failed: %w", rbErr))
				} else {
					err = rbErr
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
