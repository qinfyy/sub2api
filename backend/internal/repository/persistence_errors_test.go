package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestPersistenceErrorClassification(t *testing.T) {
	db := newSQLiteTestDB(t)
	_, err := db.Exec(`CREATE TABLE parent(id INTEGER PRIMARY KEY);
CREATE TABLE child(id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE CHECK(length(name)>1), parent_id INTEGER REFERENCES parent(id));
INSERT INTO parent VALUES(1); INSERT INTO child VALUES(1,'ok',1);`)
	require.NoError(t, err)
	for _, tt := range []struct {
		query string
		kind  persistenceErrorKind
		pg    string
	}{
		{`INSERT INTO child VALUES(1,'second',1)`, persistenceUnique, "23505"},
		{`INSERT INTO child VALUES(2,'ok',1)`, persistenceUnique, "23505"},
		{`INSERT INTO child VALUES(2,'new',99)`, persistenceForeignKey, "23503"},
		{`INSERT INTO child VALUES(2,NULL,1)`, persistenceNotNull, "23502"},
		{`INSERT INTO child VALUES(2,'x',1)`, persistenceCheck, "23514"},
	} {
		_, err := db.Exec(tt.query)
		require.Error(t, err)
		require.Equal(t, tt.kind, classifyPersistenceError(fmt.Errorf("wrapped: %w", err)))
		require.Equal(t, tt.kind, classifyPersistenceError(&pq.Error{Code: pq.ErrorCode(tt.pg)}))
		require.Equal(t, tt.kind == persistenceUnique, isUniqueConstraintViolation(err))
	}
	require.Equal(t, persistenceCanceled, classifyPersistenceError(fmt.Errorf("wrapped: %w", context.Canceled)))
	require.Equal(t, persistenceDeadline, classifyPersistenceError(context.DeadlineExceeded))
	require.Equal(t, persistenceSerialization, classifyPersistenceError(&pq.Error{Code: "40001"}))
	require.Equal(t, persistenceUnknown, classifyPersistenceError(&pq.Error{Code: "08006"}))
}
