package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func newSQLiteTriggerClient(t *testing.T) (*dbent.Client, *sql.DB, context.Context) {
	t.Helper()
	db := newSQLiteTestDB(t)
	ctx := context.Background()
	require.NoError(t, applySQLiteV1Schema(ctx, db))
	return dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))), db, ctx
}

func TestSQLiteAuthInvalidationFromEntRawAndUpsert(t *testing.T) {
	client, db, ctx := newSQLiteTriggerClient(t)
	user := client.User.Create().SetEmail("auth@example.test").SetPasswordHash("hash").SaveX(ctx)
	group := client.Group.Create().SetName("private").SetIsExclusive(true).SaveX(ctx)
	key := client.APIKey.Create().SetUserID(user.ID).SetGroupID(group.ID).SetKey("private-raw-key").SetName("key").SaveX(ctx)
	assertEvents := func(want int) {
		t.Helper()
		var count int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM auth_cache_invalidation_outbox`).Scan(&count))
		require.Equal(t, want, count)
		if want > 0 {
			var digest string
			require.NoError(t, db.QueryRow(`SELECT cache_key FROM auth_cache_invalidation_outbox LIMIT 1`).Scan(&digest))
			sum := sha256.Sum256([]byte(key.Key))
			require.Equal(t, hex.EncodeToString(sum[:]), digest)
		}
		_, err := db.Exec(`DELETE FROM auth_cache_invalidation_outbox`)
		require.NoError(t, err)
	}
	assertEvents(0)
	require.NoError(t, client.APIKey.UpdateOneID(key.ID).SetStatus("disabled").Exec(ctx))
	assertEvents(1)
	require.NoError(t, client.APIKey.Create().SetUserID(user.ID).SetGroupID(group.ID).SetKey(key.Key).SetName(key.Name).
		OnConflict(entsql.ConflictColumns(apikey.FieldKey)).Update(func(u *dbent.APIKeyUpsert) { u.SetStatus("active") }).Exec(ctx))
	assertEvents(1)
	_, err := db.Exec(`UPDATE groups SET profit_control_enabled=1 WHERE id=?`, group.ID)
	require.NoError(t, err)
	assertEvents(1)
	_, err = db.Exec(`INSERT INTO user_allowed_groups(user_id,group_id,created_at) VALUES(?,?,CURRENT_TIMESTAMP)`, user.ID, group.ID)
	require.NoError(t, err)
	assertEvents(1)
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.User.UpdateOneID(user.ID).SetRole("admin").Exec(ctx))
	require.NoError(t, tx.Rollback())
	assertEvents(0)
	require.NoError(t, client.APIKey.DeleteOneID(key.ID).Exec(ctx))
	assertEvents(1)
}

func TestSQLiteLongContextTriggerInheritanceAndValidation(t *testing.T) {
	for _, recursive := range []int{0, 1} {
		t.Run(fmt.Sprint(recursive), func(t *testing.T) {
			client, db, ctx := newSQLiteTriggerClient(t)
			_, err := db.Exec(fmt.Sprintf("PRAGMA recursive_triggers=%d", recursive))
			require.NoError(t, err)
			create := func(name string) *dbent.AccountCreate {
				return client.Account.Create().SetName(name).SetPlatform("openai").SetType("apikey").SetCredentials(map[string]any{}).SetExtra(map[string]any{})
			}
			parent := create("parent").SaveX(ctx)
			child := create("child").SetParentAccountID(parent.ID).SetQuotaDimension("spark").SaveX(ctx)
			assertFlag := func(id int64, want bool) {
				t.Helper()
				loaded := client.Account.GetX(ctx, id)
				require.Equal(t, want, loaded.Extra["openai_long_context_billing_enabled"])
			}
			assertFlag(parent.ID, false)
			assertFlag(child.ID, false)
			_, err = create("invalid").SetExtra(map[string]any{"openai_long_context_billing_enabled": "false"}).Save(ctx)
			require.ErrorContains(t, err, "must be a boolean")
			_, err = db.Exec(`UPDATE accounts SET extra='{"openai_long_context_billing_enabled":true}' WHERE id=?`, parent.ID)
			require.NoError(t, err)
			assertFlag(parent.ID, true)
			assertFlag(child.ID, true)
			var events int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=?`, child.ID).Scan(&events))
			require.Equal(t, 1, events)
			_, err = db.Exec(`UPDATE accounts SET extra='{"other":2}' WHERE id=?`, parent.ID)
			require.NoError(t, err)
			assertFlag(parent.ID, true)
			_, err = db.Exec(`UPDATE accounts SET extra='{"openai_long_context_billing_enabled":"ignored"}' WHERE id=?`, child.ID)
			require.NoError(t, err)
			assertFlag(child.ID, true)
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=?`, child.ID).Scan(&events))
			require.Equal(t, 1, events)
		})
	}
}

func TestSQLiteRollupInvalidationFromInsertUpdateDelete(t *testing.T) {
	client, db, ctx := newSQLiteTriggerClient(t)
	user := client.User.Create().SetEmail("rollup@example.test").SetPasswordHash("hash").SaveX(ctx)
	key := client.APIKey.Create().SetUserID(user.ID).SetKey("rollup-key").SetName("key").SaveX(ctx)
	group := client.Group.Create().SetName("rollup").SaveX(ctx)
	account := client.Account.Create().SetName("rollup").SetPlatform("anthropic").SetType("apikey").SetCredentials(map[string]any{}).SaveX(ctx)
	instant := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	reset := func() {
		t.Helper()
		_, err := db.Exec(`UPDATE usage_group_rollup_state SET closed_before='2099-01-01' WHERE id=1`)
		require.NoError(t, err)
	}
	assertDay := func(at time.Time) {
		t.Helper()
		var day string
		require.NoError(t, db.QueryRow(`SELECT closed_before FROM usage_group_rollup_state WHERE id=1`).Scan(&day))
		require.Equal(t, at.In(timezone.Location()).Format("2006-01-02"), day)
	}
	reset()
	log := client.UsageLog.Create().SetRequestID("rollup-request").SetModel("test").SetUserID(user.ID).
		SetAPIKeyID(key.ID).SetAccountID(account.ID).SetGroupID(group.ID).SetCreatedAt(instant).SaveX(ctx)
	assertDay(instant)
	reset()
	earlier := instant.AddDate(0, 0, -2)
	_, err := db.Exec(`UPDATE usage_logs SET created_at=? WHERE id=?`, earlier, log.ID)
	require.NoError(t, err)
	assertDay(earlier)
	reset()
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.UsageLog.DeleteOneID(log.ID).Exec(ctx))
	require.NoError(t, tx.Rollback())
	var day string
	require.NoError(t, db.QueryRow(`SELECT closed_before FROM usage_group_rollup_state WHERE id=1`).Scan(&day))
	require.Equal(t, "2099-01-01", day)
	require.NoError(t, client.UsageLog.DeleteOneID(log.ID).Exec(ctx))
	assertDay(earlier)
}
