package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestEntTransactionRunnerSharesSQLAndNestedTransaction(t *testing.T) {
	db := newSQLiteTestDB(t)
	_, err := db.Exec(`CREATE TABLE tx_probe(id INTEGER PRIMARY KEY)`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	runner := NewTransactionRunner(client)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	failure := errors.New("rollback all writes")
	err = runner.WithinTransaction(ctx, func(ctx context.Context) error {
		tx := dbent.TxFromContext(ctx)
		require.NotNil(t, tx)
		_, err := txAwareSQLExecutor(ctx, db, client).ExecContext(ctx, `INSERT INTO tx_probe VALUES(1)`)
		require.NoError(t, err)
		err = runner.WithinTransaction(ctx, func(nested context.Context) error {
			require.Same(t, tx, dbent.TxFromContext(nested))
			_, err := clientFromContext(nested, client).ExecContext(nested, `INSERT INTO tx_probe VALUES(2)`)
			return err
		})
		require.NoError(t, err)
		return failure
	})
	require.ErrorIs(t, err, failure)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM tx_probe`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, runner.WithinTransaction(ctx, func(ctx context.Context) error {
		_, err := clientFromContext(ctx, client).ExecContext(ctx, `INSERT INTO tx_probe VALUES(3)`)
		return err
	}))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM tx_probe`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestEntTransactionRunnerRollsBackPanic(t *testing.T) {
	db := newSQLiteTestDB(t)
	_, err := db.Exec(`CREATE TABLE tx_probe(id INTEGER PRIMARY KEY)`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	require.Panics(t, func() {
		_ = NewTransactionRunner(client).WithinTransaction(context.Background(), func(ctx context.Context) error {
			_, err := clientFromContext(ctx, client).ExecContext(ctx, `INSERT INTO tx_probe VALUES(1)`)
			require.NoError(t, err)
			panic("abort")
		})
	})
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM tx_probe`).Scan(&count))
	require.Zero(t, count)
}

func TestEntTransactionRunnerBorrowsTransactionalClient(t *testing.T) {
	db := newSQLiteTestDB(t)
	_, err := db.Exec(`CREATE TABLE tx_probe(id INTEGER PRIMARY KEY)`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	transactional := tx.Client()
	require.NoError(t, NewTransactionRunner(transactional).WithinTransaction(ctx, func(ctx context.Context) error {
		// Another repository with the root client must not borrow a second connection.
		require.Same(t, transactional, clientFromContext(ctx, client))
		_, err := txAwareSQLExecutor(ctx, db, client).ExecContext(ctx, `INSERT INTO tx_probe VALUES(1)`)
		if err != nil {
			return err
		}
		return NewTransactionRunner(client).WithinTransaction(ctx, func(ctx context.Context) error {
			_, err := clientFromContext(ctx, client).ExecContext(ctx, `INSERT INTO tx_probe VALUES(2)`)
			return err
		})
	}))
	require.NoError(t, tx.Rollback(), "the borrowed runner must not commit")
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM tx_probe`).Scan(&count))
	require.Zero(t, count)
}
