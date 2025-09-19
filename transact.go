package db

import (
	"context"
	"database/sql"
	"errors"

	"fmt"
	"github.com/uptrace/bun"
	"sync"
)

var dbCache Cache

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

func NewTransactFor(dbName string) (tx *Transact, err error) {
	tx = new(Transact)
	tx.db, err = dbCache.Get(dbName)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

func NewTransactWithDb(db *bun.DB) (tx *Transact, err error) {
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
		// Pop parent from stack.
		parentIdx := len(t.stack) - 1
		if parentIdx >= 0 {
			t.tx = t.stack[parentIdx]
			t.stack = t.stack[:parentIdx]
		} else {
			// Should not happen, but safeguard.
			t.tx = nil
		}
		t.nested--
		return nil
	}

	// Outermost transaction commit.
	if err := t.tx.Commit(); err != nil {
		return err
	}

	t.tx = nil
	t.stack = nil
	t.nested--
	return nil
}

func (t *Transact) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tx == nil {
		return errors.New("cannot rollback: no tx active")
	}

	if t.nested > 1 {
		// Rollback to current savepoint and revert to parent tx.
		if err := t.tx.Rollback(); err != nil {
			return err
		}
		parentIdx := len(t.stack) - 1
		if parentIdx >= 0 {
			t.tx = t.stack[parentIdx]
			t.stack = t.stack[:parentIdx]
		} else {
			// Should not happen, but safeguard.
			t.tx = nil
		}
		t.nested--
		return nil
	}

	// Outermost transaction rollback.
	t.nested--
	err := t.tx.Rollback()
	t.tx = nil
	t.stack = nil
	return err
}

type TransactFunc func(ctx context.Context) error

func (t *Transact) Transaction(ctx context.Context, opt *sql.TxOptions, fn TransactFunc) (err error) {

	if err = t.Start(ctx, opt); err != nil {
		return err
	}

	var done bool

	defer func() {
		r := recover()
		if !done || r != nil {
			if rErr := t.Rollback(); rErr != nil {
				err = errors.Join(err, fmt.Errorf("rollback failed: %w", rErr))
			}
		}
		if r != nil {
			panic(r)
		}
	}()

	if fErr := fn(ctx); fErr != nil {
		return fErr
	}

	done = true
	if cErr := t.Commit(); cErr != nil {
		done = false
		err = fmt.Errorf("failed to commit: %w", cErr)
		return err
	}

	return nil
}
