package repository

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDialectOf(t *testing.T) {
	t.Run("nil config defaults to postgres", func(t *testing.T) {
		require.Equal(t, config.DatabaseTypePostgres, dialectOf(nil))
	})

	t.Run("normalizes configured type", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Database.Type = " SQLite "
		require.Equal(t, config.DatabaseTypeSQLite, dialectOf(cfg))
	})

	t.Run("unknown type is preserved for validation", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Database.Type = "mysql"
		require.Equal(t, "mysql", dialectOf(cfg))
	})
}

func TestIsSQLiteDBAndIsPostgresDB(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		require.False(t, isSQLiteDB(nil))
		require.False(t, isPostgresDB(nil))
	})

	t.Run("mock driver is neither", func(t *testing.T) {
		db, _, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		require.False(t, isSQLiteDB(db))
		require.False(t, isPostgresDB(db))
	})

	t.Run("real sqlite driver detected", func(t *testing.T) {
		db, err := sql.Open(sqliteDriverName, "file:sqlite_dialect_test?mode=memory&cache=shared")
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		require.True(t, isSQLiteDB(db))
		require.False(t, isPostgresDB(db))
	})
}

func TestSQLitePlaceholders(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "simple",
			in:   "SELECT * FROM users WHERE id = $1 AND name = $2",
			want: "SELECT * FROM users WHERE id = ? AND name = ?",
		},
		{
			name: "double digit placeholders",
			in:   "SELECT * FROM t WHERE a = $10 OR b = $11",
			want: "SELECT * FROM t WHERE a = ? OR b = ?",
		},
		{
			name: "repeated placeholder keeps one slot per occurrence",
			in:   "SELECT * FROM t WHERE a = $1 OR b = $1",
			want: "SELECT * FROM t WHERE a = ? OR b = ?",
		},
		{
			name: "placeholder inside string literal is preserved",
			in:   "SELECT '$1' AS literal, x FROM t WHERE a = $1",
			want: "SELECT '$1' AS literal, x FROM t WHERE a = ?",
		},
		{
			name: "escaped quote inside string literal",
			in:   "SELECT 'it''s $1' AS s, y FROM t WHERE b = $2",
			want: "SELECT 'it''s $1' AS s, y FROM t WHERE b = ?",
		},
		{
			name: "identifier containing dollar is preserved",
			in:   `SELECT "$1" FROM t WHERE c = $3`,
			want: `SELECT "$1" FROM t WHERE c = ?`,
		},
		{
			name: "line comment is skipped",
			in:   "SELECT 1 -- $1 is not a placeholder here\nFROM t WHERE a = $1",
			want: "SELECT 1 -- $1 is not a placeholder here\nFROM t WHERE a = ?",
		},
		{
			name: "block comment is skipped",
			in:   "SELECT /* $9 */ 1 FROM t WHERE a = $1",
			want: "SELECT /* $9 */ 1 FROM t WHERE a = ?",
		},
		{
			name: "no placeholders",
			in:   "SELECT 1",
			want: "SELECT 1",
		},
		{
			name: "bare dollar tag is preserved",
			in:   "SELECT $tag FROM t",
			want: "SELECT $tag FROM t",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, sqlitePlaceholders(tc.in))
		})
	}
}

func TestBindArrayArgsSQLite(t *testing.T) {
	db, err := sql.Open(sqliteDriverName, "file:sqlite_bind_array_test?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	t.Run("expands slice into IN list", func(t *testing.T) {
		query, args, err := bindArrayArgs(db, "SELECT * FROM users WHERE id IN ($1)", []any{[]int64{1, 2, 3}})
		require.NoError(t, err)
		require.Equal(t, "SELECT * FROM users WHERE id IN (?, ?, ?)", query)
		require.Equal(t, []any{int64(1), int64(2), int64(3)}, args)
	})

	t.Run("keeps scalar argument intact", func(t *testing.T) {
		query, args, err := bindArrayArgs(db, "SELECT * FROM t WHERE a = $1 AND b = $2", []any{"x", int64(7)})
		require.NoError(t, err)
		require.Equal(t, "SELECT * FROM t WHERE a = ? AND b = ?", query)
		require.Equal(t, []any{"x", int64(7)}, args)
	})

	t.Run("ANY placeholder gains its own parentheses", func(t *testing.T) {
		query, args, err := bindArrayArgs(db, "SELECT * FROM t WHERE id = ANY($1)", []any{[]int64{4, 5}})
		require.NoError(t, err)
		require.Equal(t, "SELECT * FROM t WHERE id IN (SELECT value FROM json_each(?))", query)
		require.Equal(t, []any{"[4,5]"}, args)
	})

	t.Run("ALL negation is rewritten", func(t *testing.T) {
		query, args, err := bindArrayArgs(db, "SELECT * FROM t WHERE id <> ALL($1)", []any{[]int64{4, 5}})
		require.NoError(t, err)
		require.Equal(t, "SELECT * FROM t WHERE id NOT IN (SELECT value FROM json_each(?))", query)
		require.Equal(t, []any{"[4,5]"}, args)
	})

	t.Run("empty slice becomes always-false condition", func(t *testing.T) {
		query, args, err := bindArrayArgs(db, "SELECT * FROM users WHERE id IN ($1)", []any{[]int64{}})
		require.NoError(t, err)
		require.Equal(t, "SELECT * FROM users WHERE id IN (SELECT NULL WHERE 0)", query)
		require.Empty(t, args)
	})

	t.Run("rejects placeholder without matching argument", func(t *testing.T) {
		_, _, err := bindArrayArgs(db, "SELECT * FROM t WHERE a = $5", []any{int64(1)})
		require.Error(t, err)
	})

	t.Run("rejects duplicated placeholder when expanding array", func(t *testing.T) {
		_, _, err := bindArrayArgs(db,
			"SELECT * FROM t WHERE a IN ($1) OR b IN ($1)",
			[]any{[]int64{1, 2}})
		require.Error(t, err)
	})

	t.Run("non-sqlite db passes through unchanged", func(t *testing.T) {
		pgDB, _, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = pgDB.Close() })

		original := "SELECT * FROM users WHERE id = ANY($1)"
		gotQuery, gotArgs, err := bindArrayArgs(pgDB, original, []any{[]int64{1}})
		require.NoError(t, err)
		require.Equal(t, original, gotQuery)
		require.Len(t, gotArgs, 1)
	})
}

func TestArrayElements(t *testing.T) {
	t.Run("slice is expandable", func(t *testing.T) {
		elements, ok := arrayElements([]string{"a", "b"})
		require.True(t, ok)
		require.Equal(t, []any{"a", "b"}, elements)
	})

	t.Run("byte slice stays scalar", func(t *testing.T) {
		_, ok := arrayElements([]byte("blob"))
		require.False(t, ok, "[]byte binds as BLOB and must not be expanded")
	})

	t.Run("string stays scalar", func(t *testing.T) {
		_, ok := arrayElements("text")
		require.False(t, ok)
	})

	t.Run("nil stays scalar", func(t *testing.T) {
		_, ok := arrayElements(nil)
		require.False(t, ok)
	})
}

func TestKeyedMutexSerializesSameKey(t *testing.T) {
	const key = "sqlite:test:keyed-mutex"

	a := newKeyedMutex(key)
	b := newKeyedMutex(key)

	a.Lock()
	acquired := make(chan struct{})
	go func() {
		b.Lock()
		close(acquired)
		b.Unlock()
	}()

	select {
	case <-acquired:
		t.Fatal("second holder acquired the same-key lock while the first still held it")
	default:
	}

	a.Unlock()
	<-acquired
	a.release()
	b.release()
}

func TestKeyedMutexDifferentKeysDoNotBlock(t *testing.T) {
	a := newKeyedMutex("sqlite:test:key-a")
	b := newKeyedMutex("sqlite:test:key-b")

	a.Lock()
	b.Lock()
	a.Unlock()
	b.Unlock()
	a.release()
	b.release()
}

func TestSQLiteAnyArrayPlaceholder(t *testing.T) {
	got := sqliteAnyArrayPlaceholder("SELECT 1 FROM t WHERE id = ANY($1) AND kind = $2")
	require.Equal(t,
		"SELECT 1 FROM t WHERE id IN (SELECT value FROM json_each(?)) AND kind = ?",
		got)
}
