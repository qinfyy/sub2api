package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/durablestate"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
)

// entDurableState borrows the primary database. The resource owner closes it.
type entDurableState struct {
	client  *dbent.Client
	dialect string
}

func newMemoryKVBackend(core *memoryCacheCore, db *sql.DB) *memoryKVBackend {
	b := &memoryKVBackend{core: memoryAssertCore(core), db: memoryKVStoreFromDB(db)}
	if db != nil {
		name := dialect.SQLite
		if isPostgresDB(db) {
			name = dialect.Postgres
		}
		b.state = &entDurableState{client: dbent.NewClient(dbent.Driver(entsql.OpenDB(name, db))), dialect: name}
	}
	return b
}

func (s *entDurableState) put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	var expiry *int64
	if ttl > 0 {
		x := time.Now().Add(ttl).UnixMilli()
		expiry = &x
	}
	return s.putAt(ctx, key, value, expiry)
}

func (s *entDurableState) putAt(ctx context.Context, key string, value []byte, expiry *int64) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
	return clientFromContext(ctx, s.client).DurableState.Create().SetID(key).
		SetValue(value).SetUpdatedAt(now).SetNillableExpiresAt(expiry).
		OnConflict(entsql.ConflictColumns(durablestate.FieldID)).
		Update(func(u *dbent.DurableStateUpsert) {
			u.SetValue(value).SetUpdatedAt(now)
			if expiry == nil {
				u.ClearExpiresAt()
			} else {
				u.SetExpiresAt(*expiry)
			}
		}).Exec(ctx)
}

func (s *entDurableState) get(ctx context.Context, key string) ([]byte, error) {
	record, err := clientFromContext(ctx, s.client).DurableState.Query().
		Where(durablestate.IDEQ(key), durablestate.Or(durablestate.ExpiresAtIsNil(), durablestate.ExpiresAtGT(time.Now().UnixMilli()))).
		Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, errMemoryCacheMiss
	}
	if err != nil {
		return nil, err
	}
	return record.Value, nil
}

func (s *entDurableState) delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	_, err := clientFromContext(ctx, s.client).DurableState.Delete().Where(durablestate.IDIn(keys...)).Exec(ctx)
	return err
}

func (s *entDurableState) list(ctx context.Context, prefix string, limit int) ([]string, error) {
	return clientFromContext(ctx, s.client).DurableState.Query().
		Where(predicate.DurableState(func(selector *entsql.Selector) {
			selector.Where(entsql.HasPrefix(selector.C(durablestate.FieldID), prefix))
			// SQLite LIKE folds ASCII case by default; opaque keys do not.
			selector.Where(entsql.P(func(b *entsql.Builder) {
				b.WriteString("substr(").Ident(selector.C(durablestate.FieldID)).WriteString(", 1, ").
					Arg(utf8.RuneCountInString(prefix)).WriteString(") = ").Arg(prefix)
			}))
		}), durablestate.Or(durablestate.ExpiresAtIsNil(), durablestate.ExpiresAtGT(time.Now().UnixMilli()))).
		Order(dbent.Asc(durablestate.FieldID)).Limit(limit).IDs(ctx)
}

func (s *entDurableState) lockedRecord(ctx context.Context, key string) (*dbent.DurableState, error) {
	query := clientFromContext(ctx, s.client).DurableState.Query().Where(durablestate.IDEQ(key))
	if s.dialect == dialect.Postgres {
		query.ForUpdate()
	}
	return query.Only(ctx)
}

func (s *entDurableState) addMember(ctx context.Context, key, member string, ttl time.Duration) error {
	return NewTransactionRunner(s.client).WithinTransaction(ctx, func(ctx context.Context) error {
		client := clientFromContext(ctx, s.client)
		var expiry *int64
		if ttl > 0 {
			x := time.Now().Add(ttl).UnixMilli()
			expiry = &x
		}
		// Exec avoids Ent's INSERT RETURNING expecting a row on DO NOTHING.
		if _, err := client.ExecContext(ctx,
			`INSERT INTO kv_store(key,value,expires_at,updated_at) VALUES($1,$2,$3,$4) ON CONFLICT(key) DO NOTHING`,
			key, []byte("{}"), expiry, time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")); err != nil {
			return err
		}
		record, err := s.lockedRecord(ctx, key)
		if err != nil {
			return err
		}
		members := make(map[string]struct{})
		if record.ExpiresAt == nil || *record.ExpiresAt > time.Now().UnixMilli() {
			if err := json.Unmarshal(record.Value, &members); err != nil {
				return err
			}
			if record.ExpiresAt == nil {
				expiry = nil
			} else if expiry != nil && *record.ExpiresAt > *expiry {
				expiry = record.ExpiresAt
			}
		}
		if members == nil {
			members = make(map[string]struct{})
		}
		members[member] = struct{}{}
		value, err := json.Marshal(members)
		if err != nil {
			return err
		}
		return s.putAt(ctx, key, value, expiry)
	})
}

func (s *entDurableState) deleteSetMembers(ctx context.Context, key, memberPrefix string) error {
	return NewTransactionRunner(s.client).WithinTransaction(ctx, func(ctx context.Context) error {
		record, err := s.lockedRecord(ctx, key)
		if dbent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		var members map[string]struct{}
		if err := json.Unmarshal(record.Value, &members); err != nil {
			return err
		}
		keys := make([]string, 0, len(members)+1)
		for member := range members {
			keys = append(keys, memberPrefix+member)
		}
		keys = append(keys, key)
		for len(keys) > 0 {
			n := min(len(keys), 500)
			if err := s.delete(ctx, keys[:n]...); err != nil {
				return err
			}
			keys = keys[n:]
		}
		return nil
	})
}

func (s *entDurableState) setNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	var expiry any
	if ttl > 0 {
		expiry = time.Now().Add(ttl).UnixMilli()
	}
	rows, err := clientFromContext(ctx, s.client).QueryContext(ctx, `INSERT INTO kv_store(key,value,expires_at,updated_at) VALUES($1,$2,$3,$4)
ON CONFLICT(key) DO UPDATE SET value=excluded.value,expires_at=excluded.expires_at,updated_at=excluded.updated_at
WHERE kv_store.expires_at IS NOT NULL AND kv_store.expires_at<=$5 RETURNING key`, key, value, expiry, time.Now().UTC().Format("2006-01-02T15:04:05.000000Z"), time.Now().UnixMilli())
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, rows.Err()
	}
	var result string
	if err := rows.Scan(&result); err != nil {
		return false, err
	}
	if rows.Next() {
		return false, errors.New("duplicate durable state key")
	}
	return rows.Err() == nil, rows.Err()
}

// Consume deliberately uses DELETE RETURNING, so independent connections and
// processes cannot both redeem the same state. It participates in an outer Ent tx.
func (s *entDurableState) consume(ctx context.Context, key string) ([]byte, error) {
	rows, err := clientFromContext(ctx, s.client).QueryContext(ctx,
		`DELETE FROM kv_store WHERE key=$1 RETURNING value, expires_at`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, errMemoryCacheMiss
	}
	var value []byte
	var expiry sql.NullInt64
	if err := rows.Scan(&value, &expiry); err != nil {
		return nil, err
	}
	// Finish the statement before returning a consumed value to the caller.
	if rows.Next() {
		return nil, errors.New("duplicate durable state key")
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if expiry.Valid && expiry.Int64 <= time.Now().UnixMilli() {
		return nil, errMemoryCacheMiss
	}
	return value, nil
}
