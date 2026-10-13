package repository

import (
	"context"
	"database/sql"
	"errors"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/lib/pq"
	sqlite "modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

type persistenceErrorKind uint8

const (
	persistenceUnknown persistenceErrorKind = iota
	persistenceNotFound
	persistenceUnique
	persistenceForeignKey
	persistenceCheck
	persistenceNotNull
	persistenceBusy
	persistenceSerialization
	persistenceCanceled
	persistenceDeadline
)

// Classification is not a retry policy. In particular a lost connection after
// COMMIT is ambiguous and must not cause an automatic replay of billing.
func classifyPersistenceError(err error) persistenceErrorKind {
	switch {
	case err == nil:
		return persistenceUnknown
	case errors.Is(err, context.Canceled):
		return persistenceCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return persistenceDeadline
	case errors.Is(err, sql.ErrNoRows), dbent.IsNotFound(err):
		return persistenceNotFound
	}
	var pg *pq.Error
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			return persistenceUnique
		case "23503":
			return persistenceForeignKey
		case "23514":
			return persistenceCheck
		case "23502":
			return persistenceNotNull
		case "55P03":
			return persistenceBusy
		case "40001", "40P01":
			return persistenceSerialization
		}
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() {
		case sqlitelib.SQLITE_CONSTRAINT_UNIQUE, sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY:
			return persistenceUnique
		case sqlitelib.SQLITE_CONSTRAINT_FOREIGNKEY:
			return persistenceForeignKey
		case sqlitelib.SQLITE_CONSTRAINT_CHECK:
			return persistenceCheck
		case sqlitelib.SQLITE_CONSTRAINT_NOTNULL:
			return persistenceNotNull
		case sqlitelib.SQLITE_BUSY_SNAPSHOT:
			return persistenceSerialization
		}
		switch se.Code() & 0xff {
		case sqlitelib.SQLITE_BUSY, sqlitelib.SQLITE_LOCKED:
			return persistenceBusy
		}
	}
	return persistenceUnknown
}
