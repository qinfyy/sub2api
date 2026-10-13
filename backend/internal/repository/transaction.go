package repository

import (
	"context"
	"errors"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type entTransactionRunner struct{ client *dbent.Client }

type borrowedEntTransactionKey struct{}

func transactionClientFromContext(ctx context.Context) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	client, _ := ctx.Value(borrowedEntTransactionKey{}).(*dbent.Client)
	return client
}

func NewTransactionRunner(client *dbent.Client) service.TransactionRunner {
	return &entTransactionRunner{client: client}
}

func (r *entTransactionRunner) WithinTransaction(ctx context.Context, fn func(context.Context) error) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if fn == nil {
		return errors.New("nil transaction callback")
	}
	if transactionClientFromContext(ctx) != nil {
		return fn(ctx)
	}
	if r.client == nil {
		return errors.New("nil transaction client")
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		// A repository may already have been constructed with tx.Client().
		return fn(context.WithValue(ctx, borrowedEntTransactionKey{}, r.client))
	}
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Rollback also runs during panic unwinding. Only this owner may commit.
	defer func() { _ = tx.Rollback() }()
	if err := fn(dbent.NewTxContext(ctx, tx)); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return tx.Commit()
}
