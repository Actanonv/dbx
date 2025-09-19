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
	db     *bun.DB
	tx     *bun.Tx
	mu     sync.RWMutex
	nested int
	doomed bool
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

	if t.tx != nil {
		t.nested++
		return nil
	}

	var err error
	tx, err := t.db.BeginTx(ctx, opt)
	if err != nil {
		return err
	}

	t.tx = &tx
	t.nested = 1

	return nil
}

func (t *Transact) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tx == nil {
		return errors.New("cannot commit: no tx active")
	}

	if t.nested > 1 {
		t.nested--
		return nil
	}

	if err := t.tx.Commit(); err != nil {
		return err
	}

	t.tx = nil
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
		t.nested--
		return nil
	}

	t.nested--
	err := t.tx.Rollback()
	t.tx = nil
	return err
}

type TransactFunc func(ctx context.Context) error

func (t *Transact) Transaction(ctx context.Context, opt *sql.TxOptions, fn TransactFunc) (err error) {

	if err = t.Start(ctx, opt); err != nil {
		return err
	}

	var done bool

	defer func() {
		if !done {
			if rErr := t.Rollback(); rErr != nil {
				err = fmt.Errorf("failed to rollback: %w! || db error: %w", rErr, err)
			}
		}
	}()

	if fErr := fn(ctx); fErr != nil {
		return fErr
	}

	done = true
	if cErr := t.Commit(); cErr != nil {
		done = false
		err = fmt.Errorf("failed to commit: %w || db error: %w", cErr, err)
		return err
	}

	return nil
}
